// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"encoding/json"
	"maps"
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

// semanticWorkingOverlay is the sole authority used between operation rewrites
// while the private candidate is temporarily semantically invalid. It advances
// only from validated operation facts and diagnostic-free CST; rejected
// compiler output is never merged. The final canonical compile remains the
// only result authority returned to callers.
type semanticWorkingOverlay struct {
	snapshot Snapshot
	authored map[string]map[string]any
	aliases  map[string]string
}

func newSemanticWorkingOverlay(snapshot Snapshot) *semanticWorkingOverlay {
	working := &semanticWorkingOverlay{snapshot: Snapshot{CompileOutput: deepClone(snapshot.CompileOutput)}, authored: map[string]map[string]any{}, aliases: map[string]string{}}
	for _, subject := range snapshot.SemanticIndex.Subjects {
		if object := normalizedSubject(snapshot, subject.Address); object != nil {
			working.authored[subject.Address] = deepClone(object)
		}
	}
	working.syncCanonicalObjects()
	return working
}

func (working *semanticWorkingOverlay) reset(snapshot Snapshot) {
	aliases := maps.Clone(working.aliases)
	replacement := newSemanticWorkingOverlay(snapshot)
	replacement.aliases = aliases
	*working = *replacement
}

func (working *semanticWorkingOverlay) aliasSource(destination string) (string, bool) {
	for source, current := range working.aliases {
		if current == destination {
			return source, true
		}
	}
	return "", false
}

func (working *semanticWorkingOverlay) supports(operation SemanticOperation) bool {
	switch operation.Kind {
	case OperationCreateSubject, OperationCreateRelation, OperationUpsertRow, OperationDeleteRow,
		OperationDeleteSubject, OperationRenameSubject, OperationMigrateProjectIdentity,
		OperationUpdateRelationEndpoint, OperationMoveEntityToLayer, OperationUpdateSubjectField:
		return true
	default:
		return false
	}
}

func (working *semanticWorkingOverlay) advance(before, after map[string][]byte, operation SemanticOperation) *Diagnostic {
	if !working.supports(operation) {
		diagnostic := plannerDiagnostic("LDL1801", "unsupported_working_overlay_operation", "semantic operation has no working-overlay behavior")
		return &diagnostic
	}
	for path, source := range after {
		if bytes.Equal(before[path], source) {
			continue
		}
		if parsed := syntax.Parse(source); len(parsed.Diagnostics) != 0 {
			diagnostic := plannerDiagnostic("LDL1801", "invalid_working_overlay_syntax", "operation rewrite did not produce diagnostic-free LDL syntax")
			return &diagnostic
		}
	}
	working.snapshot = rebaseSnapshotSourceRanges(working.snapshot, before, after)
	switch operation.Kind {
	case OperationDeleteSubject:
		working.remove(operation.TargetAddress)
	case OperationDeleteRow:
		working.remove(operation.RowAddress)
	case OperationCreateSubject:
		address := predictedChildAddress(operation.ParentAddress, operation.SubjectKind, operation.ID)
		if !working.add(after, address, operation.SubjectKind, operation.ParentAddress, operation.ID, semanticFieldsObject(address, operation.ID, operation.Fields)) {
			diagnostic := plannerDiagnostic("LDL1801", "invalid_working_overlay_source", "created subject has no unique diagnostic-free CST declaration")
			return &diagnostic
		}
	case OperationCreateRelation:
		address := predictedChildAddress(operation.ParentAddress, materialize.SubjectRelation, operation.ID)
		object := semanticFieldsObject(address, operation.ID, operation.Fields)
		object["type_address"], object["from_address"], object["to_address"] = operation.TypeAddress, operation.FromAddress, operation.ToAddress
		if !working.add(after, address, materialize.SubjectRelation, operation.ParentAddress, operation.ID, object) {
			diagnostic := plannerDiagnostic("LDL1801", "invalid_working_overlay_source", "created relation has no unique diagnostic-free CST declaration")
			return &diagnostic
		}
	case OperationUpsertRow:
		owner := semanticSubjectsByAddress(working.snapshot)[operation.OwnerAddress]
		kind := rowKindForOwner(owner)
		address := predictedChildAddress(operation.OwnerAddress, kind, operation.ID)
		working.remove(address)
		values := map[string]any{}
		for _, cell := range operation.Values {
			values[cell.ColumnAddress] = semanticValuePlain(cell.Value)
		}
		for _, column := range operation.ExplicitAbsentColumnAddresses {
			values[column] = nil
		}
		if !working.add(after, address, kind, operation.OwnerAddress, operation.ID, map[string]any{"id": operation.ID, "address": address, "values": values}) {
			diagnostic := plannerDiagnostic("LDL1801", "invalid_working_overlay_source", "upserted row has no unique diagnostic-free CST declaration")
			return &diagnostic
		}
	case OperationUpdateSubjectField:
		if !working.updateField(operation) {
			diagnostic := plannerDiagnostic("LDL1801", "invalid_working_overlay_field", "field update target is unavailable in the working overlay")
			return &diagnostic
		}
	case OperationUpdateRelationEndpoint:
		field := operation.Endpoint + "_address"
		if object := working.authored[operation.RelationAddress]; object != nil {
			object[field] = operation.EntityAddress
		} else {
			diagnostic := plannerDiagnostic("LDL1801", "invalid_working_overlay_field", "relation endpoint target is unavailable in the working overlay")
			return &diagnostic
		}
	case OperationMoveEntityToLayer:
		if object := working.authored[operation.EntityAddress]; object != nil {
			object["layer_address"] = operation.LayerAddress
		} else {
			diagnostic := plannerDiagnostic("LDL1801", "invalid_working_overlay_field", "entity target is unavailable in the working overlay")
			return &diagnostic
		}
	case OperationRenameSubject:
		working.rename(operation.TargetAddress, renamedAddress(operation.TargetAddress, operation.NewID), operation.NewID)
	case OperationMigrateProjectIdentity:
		working.rename(operation.ProjectAddress, renamedAddress(operation.ProjectAddress, operation.NewProjectID), operation.NewProjectID)
	}
	working.syncCanonicalObjects()
	return nil
}

func semanticFieldsObject(address, id string, fields []SemanticMapEntry) map[string]any {
	object := map[string]any{"id": id, "address": address}
	for _, field := range fields {
		object[field.Key] = semanticValuePlain(field.Value)
	}
	return object
}

func (working *semanticWorkingOverlay) updateField(operation SemanticOperation) bool {
	object := working.authored[operation.TargetAddress]
	if object == nil || len(operation.Path) == 0 || len(operation.Path) > 2 {
		return false
	}
	if len(operation.Path) == 1 {
		if operation.Action == "remove" {
			delete(object, operation.Path[0])
		} else if operation.Value != nil {
			object[operation.Path[0]] = semanticValuePlain(*operation.Value)
		}
		return true
	}
	nested, _ := object[operation.Path[0]].(map[string]any)
	if nested == nil {
		nested = map[string]any{}
		object[operation.Path[0]] = nested
	}
	if operation.Action == "remove" {
		delete(nested, operation.Path[1])
	} else if operation.Value != nil {
		nested[operation.Path[1]] = semanticValuePlain(*operation.Value)
	}
	return true
}

func (working *semanticWorkingOverlay) remove(address string) {
	removed := removeOverlaySubject(&working.snapshot, address)
	for value := range removed {
		delete(working.authored, value)
	}
}

func semanticDescendantSet(snapshot Snapshot, address string) map[string]bool {
	closure := map[string]bool{address: true}
	for changed := true; changed; {
		changed = false
		for _, subject := range snapshot.SemanticIndex.Subjects {
			if !closure[subject.Address] && subject.OwnerAddress != nil && closure[*subject.OwnerAddress] {
				closure[subject.Address] = true
				changed = true
			}
		}
	}
	return closure
}

func (working *semanticWorkingOverlay) add(after map[string][]byte, address string, kind SemanticSubjectKind, owner, id string, object map[string]any) bool {
	if !addOverlaySubject(&working.snapshot, after, address, kind, owner, id) {
		return false
	}
	working.authored[address] = deepClone(object)
	return true
}

func (working *semanticWorkingOverlay) rename(source, destination, newID string) {
	closure := semanticDescendantSet(working.snapshot, source)
	addresses := make([]string, 0, len(closure))
	for address := range closure {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(i, j int) bool { return len(addresses[i]) > len(addresses[j]) })
	remap := func(address string) string {
		if address == source {
			return destination
		}
		if strings.HasPrefix(address, source+":") {
			return destination + strings.TrimPrefix(address, source)
		}
		return address
	}
	for _, address := range addresses {
		if object := working.authored[address]; object != nil {
			delete(working.authored, address)
			object = rewriteOverlayAddresses(object, remap).(map[string]any)
			object["address"] = remap(address)
			if address == source {
				object["id"] = newID
			}
			working.authored[remap(address)] = object
		}
	}
	for i := range working.snapshot.SemanticIndex.Subjects {
		subject := &working.snapshot.SemanticIndex.Subjects[i]
		subject.Address = remap(subject.Address)
		if subject.OwnerAddress != nil {
			value := remap(*subject.OwnerAddress)
			subject.OwnerAddress = &value
		}
	}
	for i := range working.snapshot.SourceMap.Subjects {
		subject := &working.snapshot.SourceMap.Subjects[i]
		subject.Address = remap(subject.Address)
		if subject.OwnerAddress != nil {
			value := remap(*subject.OwnerAddress)
			subject.OwnerAddress = &value
		}
	}
	for i := range working.snapshot.SemanticIndex.References {
		working.snapshot.SemanticIndex.References[i].SourceAddress = remap(working.snapshot.SemanticIndex.References[i].SourceAddress)
		working.snapshot.SemanticIndex.References[i].TargetAddress = remap(working.snapshot.SemanticIndex.References[i].TargetAddress)
	}
	for i := range working.snapshot.SourceMap.Bindings {
		working.snapshot.SourceMap.Bindings[i].SourceAddress = remap(working.snapshot.SourceMap.Bindings[i].SourceAddress)
		working.snapshot.SourceMap.Bindings[i].TargetAddress = remap(working.snapshot.SourceMap.Bindings[i].TargetAddress)
		working.snapshot.SourceMap.Bindings[i].TargetOwnerAddress = remap(working.snapshot.SourceMap.Bindings[i].TargetOwnerAddress)
	}
	for i := range working.snapshot.StableAddresses {
		working.snapshot.StableAddresses[i] = remap(working.snapshot.StableAddresses[i])
	}
	for i := range working.snapshot.SubjectSemanticHashes {
		working.snapshot.SubjectSemanticHashes[i].Address = remap(working.snapshot.SubjectSemanticHashes[i].Address)
	}
	for i := range working.snapshot.SubtreeHashes {
		working.snapshot.SubtreeHashes[i].OwnerAddress = remap(working.snapshot.SubtreeHashes[i].OwnerAddress)
	}
	for i := range working.snapshot.ChildSetHashes {
		working.snapshot.ChildSetHashes[i].OwnerAddress = remap(working.snapshot.ChildSetHashes[i].OwnerAddress)
	}
	for i := range working.snapshot.AuthoringSubjectClassification {
		working.snapshot.AuthoringSubjectClassification[i].Address = remap(working.snapshot.AuthoringSubjectClassification[i].Address)
	}
	remapMembers := func(values []index.OwnerMembers) {
		for i := range values {
			values[i].OwnerAddress = remap(values[i].OwnerAddress)
			for j := range values[i].Addresses {
				values[i].Addresses[j] = remap(values[i].Addresses[j])
			}
		}
	}
	remapMembers(working.snapshot.SemanticIndex.Children)
	remapMembers(working.snapshot.SemanticIndex.Rows)
	remapMembers(working.snapshot.SemanticIndex.Columns)
	remapMembers(working.snapshot.SemanticIndex.TypeMembership)
	remapMembers(working.snapshot.SemanticIndex.LayerMembership)
	for original, current := range working.aliases {
		if current == source {
			working.aliases[original] = destination
		}
	}
	working.aliases[source] = destination
}

func rewriteOverlayAddresses(value any, remap func(string) string) any {
	switch typed := value.(type) {
	case string:
		return remap(typed)
	case []any:
		for i := range typed {
			typed[i] = rewriteOverlayAddresses(typed[i], remap)
		}
	case map[string]any:
		for key := range typed {
			typed[key] = rewriteOverlayAddresses(typed[key], remap)
		}
	}
	return value
}

func (working *semanticWorkingOverlay) syncCanonicalObjects() {
	addresses := make([]string, 0, len(working.authored))
	for address := range working.authored {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(i, j int) bool { return compareStableAddressText(addresses[i], addresses[j]) < 0 })
	objects := make([]map[string]any, 0, len(addresses))
	for _, address := range addresses {
		objects = append(objects, working.authored[address])
	}
	working.snapshot.CanonicalJSON, _ = json.Marshal(objects)
}

func removeOverlaySubject(snapshot *Snapshot, address string) map[string]bool {
	removed := semanticDescendantSet(*snapshot, address)
	snapshot.SemanticIndex.Subjects = removeMatching(snapshot.SemanticIndex.Subjects, func(value index.SemanticSubject) bool { return removed[value.Address] })
	snapshot.SourceMap.Subjects = removeMatching(snapshot.SourceMap.Subjects, func(value index.SourceSubjectRecord) bool { return removed[value.Address] })
	snapshot.SemanticIndex.References = removeMatching(snapshot.SemanticIndex.References, func(value index.SemanticReference) bool { return removed[value.SourceAddress] })
	snapshot.SourceMap.Bindings = removeMatching(snapshot.SourceMap.Bindings, func(value index.SourceBindingRecord) bool { return removed[value.SourceAddress] })
	snapshot.StableAddresses = removeMatching(snapshot.StableAddresses, func(value string) bool { return removed[value] })
	snapshot.SubjectSemanticHashes = removeMatching(snapshot.SubjectSemanticHashes, func(value SubjectHash) bool { return removed[value.Address] })
	snapshot.SubtreeHashes = removeMatching(snapshot.SubtreeHashes, func(value SubtreeHash) bool { return removed[value.OwnerAddress] })
	snapshot.ChildSetHashes = removeMatching(snapshot.ChildSetHashes, func(value ChildSetHash) bool { return removed[value.OwnerAddress] })
	snapshot.AuthoringSubjectClassification = removeMatching(snapshot.AuthoringSubjectClassification, func(value AuthoringSubjectClassification) bool { return removed[value.Address] })
	removeOwnerMembers := func(values []index.OwnerMembers) []index.OwnerMembers {
		values = removeMatching(values, func(value index.OwnerMembers) bool { return removed[value.OwnerAddress] })
		for i := range values {
			values[i].Addresses = removeMatching(values[i].Addresses, func(value string) bool { return removed[value] })
		}
		return values
	}
	snapshot.SemanticIndex.Children = removeOwnerMembers(snapshot.SemanticIndex.Children)
	snapshot.SemanticIndex.Rows = removeOwnerMembers(snapshot.SemanticIndex.Rows)
	snapshot.SemanticIndex.Columns = removeOwnerMembers(snapshot.SemanticIndex.Columns)
	snapshot.SemanticIndex.TypeMembership = removeOwnerMembers(snapshot.SemanticIndex.TypeMembership)
	snapshot.SemanticIndex.LayerMembership = removeOwnerMembers(snapshot.SemanticIndex.LayerMembership)
	return removed
}

func removeMatching[T any](values []T, remove func(T) bool) []T {
	out := values[:0]
	for _, value := range values {
		if !remove(value) {
			out = append(out, value)
		}
	}
	return out
}

func addOverlaySubject(snapshot *Snapshot, after map[string][]byte, address string, kind SemanticSubjectKind, owner, id string) bool {
	if address == "" || kind == "" || owner == "" {
		return false
	}
	module, start, end, ok := plannedSubjectRange(*snapshot, after, kind, owner, id)
	if !ok {
		return false
	}
	ownerAddress := owner
	moduleRef := index.ModuleRef{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: module}
	snapshot.SemanticIndex.Subjects = append(snapshot.SemanticIndex.Subjects, index.SemanticSubject{
		Address: address, Kind: kind, OwnerAddress: &ownerAddress, Module: &moduleRef,
	})
	snapshot.SourceMap.Subjects = append(snapshot.SourceMap.Subjects, index.SourceSubjectRecord{
		Address: address, Kind: kind, OwnerAddress: &ownerAddress, Module: &moduleRef,
		DeclarationRange: &resolve.SourceRange{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: module, StartByte: start, EndByte: end},
		CommentRanges:    []resolve.SourceRange{},
	})
	snapshot.StableAddresses = append(snapshot.StableAddresses, address)
	sort.Slice(snapshot.SemanticIndex.Subjects, func(i, j int) bool {
		return compareStableAddressText(snapshot.SemanticIndex.Subjects[i].Address, snapshot.SemanticIndex.Subjects[j].Address) < 0
	})
	sort.Slice(snapshot.SourceMap.Subjects, func(i, j int) bool {
		return compareStableAddressText(snapshot.SourceMap.Subjects[i].Address, snapshot.SourceMap.Subjects[j].Address) < 0
	})
	sort.Slice(snapshot.StableAddresses, func(i, j int) bool {
		return compareStableAddressText(snapshot.StableAddresses[i], snapshot.StableAddresses[j]) < 0
	})
	addOwnerMember := func(values *[]index.OwnerMembers) {
		for i := range *values {
			if (*values)[i].OwnerAddress == owner {
				(*values)[i].Addresses = append((*values)[i].Addresses, address)
				sort.Slice((*values)[i].Addresses, func(a, b int) bool {
					return compareStableAddressText((*values)[i].Addresses[a], (*values)[i].Addresses[b]) < 0
				})
				return
			}
		}
		*values = append(*values, index.OwnerMembers{OwnerAddress: owner, Addresses: []string{address}})
	}
	addOwnerMember(&snapshot.SemanticIndex.Children)
	switch kind {
	case materialize.SubjectEntityTypeColumn, materialize.SubjectRelationTypeColumn, materialize.SubjectViewTableColumn:
		addOwnerMember(&snapshot.SemanticIndex.Columns)
	case materialize.SubjectEntityRow, materialize.SubjectRelationRow:
		addOwnerMember(&snapshot.SemanticIndex.Rows)
	}
	return true
}

// plannedSubjectRange derives the exact current declaration from a freshly
// parsed, diagnostic-free CST. It works for insertions and replacements and
// constrains owner-scoped children to the declared owner's current range.
func plannedSubjectRange(snapshot Snapshot, after map[string][]byte, kind SemanticSubjectKind, owner, id string) (string, int, int, bool) {
	ownerModule, ownerStart, ownerEnd := "", 0, 0
	ownerSubject, ownerSource, ownerFound := subjectRecord(snapshot, owner)
	if ownerFound {
		source := ownerSource
		ownerModule, ownerStart, ownerEnd = source.Module.ModulePath, source.DeclarationRange.StartByte, source.DeclarationRange.EndByte
	}
	type match struct {
		module     string
		start, end int
	}
	matches := []match{}
	for path, right := range after {
		if ownerModule != "" && path != ownerModule && isOwnerScopedSubjectKind(kind) {
			continue
		}
		parsed := syntax.Parse(right)
		if len(parsed.Diagnostics) != 0 {
			return "", 0, 0, false
		}
		currentOwnerStart, currentOwnerEnd := ownerStart, ownerEnd
		if ownerFound && path == ownerModule && isOwnerScopedSubjectKind(kind) {
			ownerMatches := []*syntax.Node{}
			syntax.Walk(parsed.Root, func(node *syntax.Node) {
				if plannedNodeMatches(node, ownerSubject.Kind, ownerForSubject(snapshot, ownerSubject), terminalID(owner)) {
					ownerMatches = append(ownerMatches, node)
				}
			})
			if len(ownerMatches) != 1 {
				return "", 0, 0, false
			}
			currentOwnerStart, currentOwnerEnd = ownerMatches[0].Span.Start, ownerMatches[0].Span.End
		}
		syntax.Walk(parsed.Root, func(node *syntax.Node) {
			if ownerModule != "" && path == ownerModule && isOwnerScopedSubjectKind(kind) && (node.Span.Start < currentOwnerStart || node.Span.End > currentOwnerEnd) {
				return
			}
			if plannedNodeMatches(node, kind, owner, id) {
				matches = append(matches, match{module: path, start: node.Span.Start, end: node.Span.End})
			}
		})
	}
	if len(matches) != 1 {
		return "", 0, 0, false
	}
	return matches[0].module, matches[0].start, matches[0].end, true
}

func isOwnerScopedSubjectKind(kind SemanticSubjectKind) bool {
	switch kind {
	case materialize.SubjectEntityTypeColumn, materialize.SubjectRelationTypeColumn,
		materialize.SubjectEntityTypeConstraint, materialize.SubjectRelationTypeConstraint,
		materialize.SubjectQueryParameter, materialize.SubjectViewTableColumn, materialize.SubjectViewExport:
		return true
	default:
		return false
	}
}

func plannedNodeMatches(node *syntax.Node, kind SemanticSubjectKind, owner, id string) bool {
	wantKind := syntax.NodeStatement
	wantIdentifiers := []string{id}
	switch kind {
	case materialize.SubjectLayer:
		wantKind = syntax.NodeLayerItem
	case materialize.SubjectEntity:
		wantKind = syntax.NodeEntityItem
	case materialize.SubjectRelation:
		wantKind = syntax.NodeRelationItem
	case materialize.SubjectEntityRow, materialize.SubjectRelationRow:
		wantKind = syntax.NodeRowItem
		wantIdentifiers = []string{terminalID(owner), id}
	case materialize.SubjectEntityTypeConstraint, materialize.SubjectRelationTypeConstraint:
		wantIdentifiers = []string{"unique", id}
	case materialize.SubjectViewTableColumn:
		wantKind = syntax.NodeNestedBlock
		wantIdentifiers = []string{"column", id}
	case materialize.SubjectViewExport:
		wantKind = syntax.NodeNestedBlock
		wantIdentifiers = []string{"export", id}
	case materialize.SubjectEntityType, materialize.SubjectRelationType, materialize.SubjectQuery, materialize.SubjectView, materialize.SubjectReference:
		wantKind = syntax.NodeDeclaration
		keyword := map[SemanticSubjectKind]string{
			materialize.SubjectEntityType: "entity_type", materialize.SubjectRelationType: "relation_type",
			materialize.SubjectQuery: "query", materialize.SubjectView: "view", materialize.SubjectReference: "reference",
		}[kind]
		wantIdentifiers = []string{keyword, id}
	}
	if node.Kind != wantKind {
		return false
	}
	identifiers := nodeIdentifierTokens(node)
	if len(identifiers) < len(wantIdentifiers) {
		return false
	}
	for i, want := range wantIdentifiers {
		if want != "" && identifiers[i] != want {
			return false
		}
	}
	return true
}

func nodeIdentifierTokens(node *syntax.Node) []string {
	values := []struct {
		start int
		raw   string
	}{}
	var collect func(*syntax.Node)
	collect = func(current *syntax.Node) {
		for _, child := range current.Children {
			switch typed := child.(type) {
			case syntax.TokenElement:
				if typed.Token.Kind == syntax.TokenIdentifier {
					values = append(values, struct {
						start int
						raw   string
					}{start: typed.Token.Span.Start, raw: typed.Token.Raw})
				}
			case *syntax.Node:
				collect(typed)
			}
		}
	}
	collect(node)
	sort.Slice(values, func(i, j int) bool { return values[i].start < values[j].start })
	out := make([]string, len(values))
	for i := range values {
		out[i] = values[i].raw
	}
	return out
}

// rebaseSnapshotSourceRanges carries only source-location authority across a
// temporarily invalid private overlay. Semantic identities and hashes remain
// those of the last successful compile and can never escape as a result.
func rebaseSnapshotSourceRanges(snapshot Snapshot, before, after map[string][]byte) Snapshot {
	out := Snapshot{CompileOutput: deepClone(snapshot.CompileOutput)}
	byPath := map[string][]PlannedSourceEdit{}
	for path, left := range before {
		right, ok := after[path]
		if !ok || bytes.Equal(left, right) {
			continue
		}
		byPath[path] = minimalModuleEdits(path, left, right)
	}
	for i := range out.SourceMap.Files {
		file := &out.SourceMap.Files[i]
		if file.Origin.Kind != resolve.OriginProject {
			continue
		}
		if source, ok := after[file.ModulePath]; ok {
			file.Digest = semanticDigest(source)
			file.ByteLength = len(source)
		}
	}
	for i := range out.SourceMap.Subjects {
		subject := &out.SourceMap.Subjects[i]
		if subject.Module == nil || subject.Module.Origin.Kind != resolve.OriginProject {
			continue
		}
		edits := byPath[subject.Module.ModulePath]
		if subject.DeclarationRange != nil {
			value := rebaseSourceRange(*subject.DeclarationRange, edits)
			subject.DeclarationRange = &value
		}
		for j := range subject.CommentRanges {
			subject.CommentRanges[j] = rebaseSourceRange(subject.CommentRanges[j], edits)
		}
	}
	for i := range out.SourceMap.Bindings {
		binding := &out.SourceMap.Bindings[i]
		if binding.Module.Origin.Kind == resolve.OriginProject {
			binding.Range = rebaseSourceRange(binding.Range, byPath[binding.Module.ModulePath])
		}
	}
	for i := range out.SourceMap.Exports {
		binding := &out.SourceMap.Exports[i]
		if binding.Module.Origin.Kind == resolve.OriginProject {
			binding.Range = rebaseSourceRange(binding.Range, byPath[binding.Module.ModulePath])
		}
	}
	for i := range out.SourceMap.Assets {
		asset := &out.SourceMap.Assets[i]
		if asset.Origin.Kind == resolve.OriginProject {
			asset.Range = rebaseSourceRange(asset.Range, byPath[asset.ModulePath])
		}
	}
	for i := range out.SemanticIndex.References {
		ref := &out.SemanticIndex.References[i]
		if ref.Range.Origin.Kind == resolve.OriginProject {
			ref.Range = rebaseSourceRange(ref.Range, byPath[ref.Range.ModulePath])
		}
	}
	for i := range out.LosslessSyntaxTree.Files {
		file := &out.LosslessSyntaxTree.Files[i]
		if file.Origin.Kind != resolve.OriginProject {
			continue
		}
		if source, ok := after[file.ModulePath]; ok {
			file.Source = bytes.Clone(source)
			parsed := syntax.Parse(source)
			file.Root = parsed.Root
			file.Tokens = parsed.Tokens
		}
	}
	return out
}

func rebaseSourceRange(value SourceRange, edits []PlannedSourceEdit) SourceRange {
	value.StartByte = rebaseOffset(value.StartByte, edits)
	value.EndByte = rebaseOffset(value.EndByte, edits)
	if value.EndByte < value.StartByte {
		value.EndByte = value.StartByte
	}
	return value
}

func rebaseSyntaxSpan(value syntax.Span, edits []PlannedSourceEdit) syntax.Span {
	value.Start = rebaseOffset(value.Start, edits)
	value.End = rebaseOffset(value.End, edits)
	if value.End < value.Start {
		value.End = value.Start
	}
	return value
}

func rebaseOffset(offset int, edits []PlannedSourceEdit) int {
	delta := 0
	for _, edit := range edits {
		if offset <= edit.StartByte {
			return offset + delta
		}
		if offset >= edit.EndByte {
			delta += len(edit.Replacement) - (edit.EndByte - edit.StartByte)
			continue
		}
		relative := offset - edit.StartByte
		if relative > len(edit.Replacement) {
			relative = len(edit.Replacement)
		}
		return edit.StartByte + delta + relative
	}
	return offset + delta
}
