// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"sort"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

// advanceInvalidSemanticOverlay keeps the last successfully compiled semantic
// graph as its base, then applies only identity changes proven by an operation
// that already rewrote the private source tree. Rejected compiler output is
// never treated as authority: additions are derived from the operation's
// StableAddress, declared kind and owner, while deletions remove that identity
// and its owner-chain descendants before another operation can resolve a stale
// collapsed range.
func advanceInvalidSemanticOverlay(snapshot Snapshot, before, after map[string][]byte, operation SemanticOperation) Snapshot {
	out := rebaseSnapshotSourceRanges(snapshot, before, after)
	switch operation.Kind {
	case OperationDeleteSubject:
		removeOverlaySubject(&out, operation.TargetAddress)
	case OperationDeleteRow:
		removeOverlaySubject(&out, operation.RowAddress)
	case OperationCreateSubject:
		address := predictedChildAddress(operation.ParentAddress, operation.SubjectKind, operation.ID)
		addOverlaySubject(&out, before, after, address, operation.SubjectKind, operation.ParentAddress, operation.ID)
	case OperationCreateRelation:
		address := predictedChildAddress(operation.ParentAddress, materialize.SubjectRelation, operation.ID)
		addOverlaySubject(&out, before, after, address, materialize.SubjectRelation, operation.ParentAddress, operation.ID)
	case OperationUpsertRow:
		owner := semanticSubjectsByAddress(out)[operation.OwnerAddress]
		kind := rowKindForOwner(owner)
		address := predictedChildAddress(operation.OwnerAddress, kind, operation.ID)
		removeOverlaySubject(&out, address)
		addOverlaySubject(&out, before, after, address, kind, operation.OwnerAddress, operation.ID)
	}
	return out
}

func removeOverlaySubject(snapshot *Snapshot, address string) {
	removed := map[string]bool{address: true}
	for changed := true; changed; {
		changed = false
		for _, subject := range snapshot.SemanticIndex.Subjects {
			if !removed[subject.Address] && subject.OwnerAddress != nil && removed[*subject.OwnerAddress] {
				removed[subject.Address] = true
				changed = true
			}
		}
	}
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

func addOverlaySubject(snapshot *Snapshot, before, after map[string][]byte, address string, kind SemanticSubjectKind, owner, id string) {
	if address == "" || kind == "" || owner == "" {
		return
	}
	module, start, end, ok := plannedSubjectRange(before, after, kind, id)
	if !ok {
		return
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
}

// plannedSubjectRange accepts exactly one source module whose operation is a
// pure insertion, then derives the subject's exact range from the freshly
// parsed CST. The raw insertion bounds only constrain the new-node search; a
// wrapper such as columns { ... } can therefore never become a child range.
func plannedSubjectRange(before, after map[string][]byte, kind SemanticSubjectKind, id string) (string, int, int, bool) {
	module := ""
	insertStart, insertEnd := 0, 0
	for path, right := range after {
		left := before[path]
		if bytes.Equal(left, right) {
			continue
		}
		if module != "" || len(right) <= len(left) {
			return "", 0, 0, false
		}
		prefix := 0
		for prefix < len(left) && left[prefix] == right[prefix] {
			prefix++
		}
		suffix := 0
		for suffix < len(left)-prefix && left[len(left)-1-suffix] == right[len(right)-1-suffix] {
			suffix++
		}
		if len(right)-suffix-prefix != len(right)-len(left) {
			return "", 0, 0, false
		}
		if prefix == len(right)-suffix {
			return "", 0, 0, false
		}
		module, insertStart, insertEnd = path, prefix, len(right)-suffix
	}
	if module == "" {
		return "", 0, 0, false
	}
	parsed := syntax.Parse(after[module])
	var match *syntax.Node
	syntax.Walk(parsed.Root, func(node *syntax.Node) {
		if node.Span.Start < insertStart || node.Span.End > insertEnd || !plannedNodeMatches(node, kind, id) {
			return
		}
		if match == nil || node.Span.End-node.Span.Start < match.Span.End-match.Span.Start {
			match = node
		}
	})
	if match == nil {
		return "", 0, 0, false
	}
	return module, match.Span.Start, match.Span.End, true
}

func plannedNodeMatches(node *syntax.Node, kind SemanticSubjectKind, id string) bool {
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
		wantIdentifiers = []string{"", id}
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
		wantIdentifiers = []string{"", id}
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
