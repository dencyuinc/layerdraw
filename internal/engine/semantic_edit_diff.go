// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"reflect"
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

func buildPlannedSourceDiff(before, after map[string][]byte) PlannedSourceDiff {
	pathsMap := map[string]bool{}
	for path := range before {
		pathsMap[path] = true
	}
	for path := range after {
		pathsMap[path] = true
	}
	paths := make([]string, 0, len(pathsMap))
	for path := range pathsMap {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var edits []PlannedSourceEdit
	for _, path := range paths {
		left, leftOK := before[path]
		right, rightOK := after[path]
		switch {
		case !leftOK:
			edits = append(edits, PlannedSourceEdit{Kind: PlannedSourceCreate, ModulePath: path, AfterDigest: semanticDigest(right), Replacement: bytes.Clone(right)})
		case !rightOK:
			edits = append(edits, PlannedSourceEdit{Kind: PlannedSourceDelete, ModulePath: path, BeforeDigest: semanticDigest(left), StartByte: 0, EndByte: len(left)})
		case !bytes.Equal(left, right):
			for _, edit := range minimalModuleEdits(path, left, right) {
				edits = append(edits, edit)
			}
		}
	}
	if edits == nil {
		edits = []PlannedSourceEdit{}
	}
	digestInput := make([]any, 0, len(edits))
	for _, e := range edits {
		digestInput = append(digestInput, map[string]any{"kind": e.Kind, "module_path": e.ModulePath, "start_byte": e.StartByte, "end_byte": e.EndByte, "before_digest": e.BeforeDigest, "after_digest": e.AfterDigest, "replacement_digest": semanticDigest(e.Replacement)})
	}
	return PlannedSourceDiff{Edits: edits, Digest: digestJSON(digestInput)}
}

func minimalModuleEdits(path string, before, after []byte) []PlannedSourceEdit {
	leftLines, leftOffsets := sourceLines(before)
	rightLines, rightOffsets := sourceLines(after)
	if len(leftLines)*len(rightLines) <= 1_000_000 {
		dp := make([]int, (len(leftLines)+1)*(len(rightLines)+1))
		width := len(rightLines) + 1
		for i := len(leftLines) - 1; i >= 0; i-- {
			for j := len(rightLines) - 1; j >= 0; j-- {
				if bytes.Equal(leftLines[i], rightLines[j]) {
					dp[i*width+j] = 1 + dp[(i+1)*width+j+1]
				} else if dp[(i+1)*width+j] >= dp[i*width+j+1] {
					dp[i*width+j] = dp[(i+1)*width+j]
				} else {
					dp[i*width+j] = dp[i*width+j+1]
				}
			}
		}
		var edits []PlannedSourceEdit
		i, j := 0, 0
		for i < len(leftLines) || j < len(rightLines) {
			if i < len(leftLines) && j < len(rightLines) && bytes.Equal(leftLines[i], rightLines[j]) {
				i++
				j++
				continue
			}
			startI, startJ := i, j
			for i < len(leftLines) || j < len(rightLines) {
				if i < len(leftLines) && j < len(rightLines) && bytes.Equal(leftLines[i], rightLines[j]) {
					break
				}
				if j >= len(rightLines) || (i < len(leftLines) && dp[(i+1)*width+j] >= dp[i*width+j+1]) {
					i++
				} else {
					j++
				}
			}
			beforeStart, beforeEnd := leftOffsets[startI], leftOffsets[i]
			afterStart, afterEnd := rightOffsets[startJ], rightOffsets[j]
			bs, be, as, ae := trimEqualBytes(before, beforeStart, beforeEnd, after, afterStart, afterEnd)
			edits = append(edits, PlannedSourceEdit{Kind: PlannedSourceReplace, ModulePath: path, StartByte: bs, EndByte: be, BeforeDigest: semanticDigest(before), AfterDigest: semanticDigest(after), Replacement: bytes.Clone(after[as:ae])})
		}
		if len(edits) > 0 {
			return edits
		}
	}
	prefix := 0
	for prefix < len(before) && prefix < len(after) && before[prefix] == after[prefix] {
		prefix++
	}
	// Keep UTF-8 boundaries and prefer whole changed token/line ranges only when
	// the common byte boundary is already exact. The planner's rewrite stage
	// produces valid UTF-8, so backing over continuation bytes is sufficient.
	for prefix > 0 && prefix < len(before) && before[prefix]&0xc0 == 0x80 {
		prefix--
	}
	suffix := 0
	for suffix < len(before)-prefix && suffix < len(after)-prefix && before[len(before)-1-suffix] == after[len(after)-1-suffix] {
		suffix++
	}
	for suffix > 0 && len(before)-suffix < len(before) && before[len(before)-suffix]&0xc0 == 0x80 {
		suffix--
	}
	replacement := bytes.Clone(after[prefix : len(after)-suffix])
	return []PlannedSourceEdit{{Kind: PlannedSourceReplace, ModulePath: path, StartByte: prefix, EndByte: len(before) - suffix, BeforeDigest: semanticDigest(before), AfterDigest: semanticDigest(after), Replacement: replacement}}
}

func sourceLines(source []byte) ([][]byte, []int) {
	lines := [][]byte{}
	offsets := []int{0}
	start := 0
	for start < len(source) {
		next := bytes.IndexByte(source[start:], '\n')
		if next < 0 {
			lines = append(lines, source[start:])
			start = len(source)
		} else {
			next += start + 1
			lines = append(lines, source[start:next])
			start = next
		}
		offsets = append(offsets, start)
	}
	return lines, offsets
}

func trimEqualBytes(before []byte, bs, be int, after []byte, as, ae int) (int, int, int, int) {
	for bs < be && as < ae && before[bs] == after[as] {
		bs++
		as++
	}
	for bs > 0 && bs < be && before[bs]&0xc0 == 0x80 {
		bs--
		as--
	}
	for be > bs && ae > as && before[be-1] == after[ae-1] {
		be--
		ae--
	}
	for be < len(before) && be > bs && before[be]&0xc0 == 0x80 {
		be++
		ae++
	}
	return bs, be, as, ae
}

type diffSubject struct {
	semantic index.SemanticSubject
	object   map[string]any
	owner    string
	refs     []string
}

func buildPlannedSemanticDiff(before, after Snapshot) PlannedSemanticDiff {
	left := diffSubjects(before)
	right := diffSubjects(after)
	entries := make([]SemanticDiffEntry, 0)
	matchedRight := map[string]bool{}
	for address, prior := range left {
		if next, ok := right[address]; ok {
			matchedRight[address] = true
			if prior.semantic.OwnHash == next.semantic.OwnHash {
				continue
			}
			paths := changedPaths(prior.object, next.object)
			kind := SemanticUpdated
			if len(paths) == 1 && len(paths[0].Tokens) == 1 && paths[0].Tokens[0] == "layer_address" && prior.semantic.Kind == materialize.SubjectEntity {
				kind = SemanticMoved
			} else if !reflect.DeepEqual(prior.refs, next.refs) {
				kind = SemanticReferenceChanged
			}
			entries = append(entries, SemanticDiffEntry{Kind: kind, SubjectKind: prior.semantic.Kind, BeforeAddress: address, AfterAddress: address, BeforeHash: prior.semantic.OwnHash, AfterHash: next.semantic.OwnHash, OwnerAddress: next.owner, ChangedFieldPaths: paths})
		}
	}
	// Pair deterministic identity changes without consulting operation names.
	leftOnly := make([]diffSubject, 0)
	rightOnly := make([]diffSubject, 0)
	for address, subject := range left {
		if _, ok := right[address]; !ok {
			leftOnly = append(leftOnly, subject)
		}
	}
	for address, subject := range right {
		if _, ok := left[address]; !ok {
			rightOnly = append(rightOnly, subject)
		}
	}
	sort.Slice(leftOnly, func(i, j int) bool { return leftOnly[i].semantic.Address < leftOnly[j].semantic.Address })
	sort.Slice(rightOnly, func(i, j int) bool { return rightOnly[i].semantic.Address < rightOnly[j].semantic.Address })
	used := make([]bool, len(rightOnly))
	beforeRoot, afterRoot := rootProjectAddress(before), rootProjectAddress(after)
	for _, prior := range leftOnly {
		match := -1
		for i, next := range rightOnly {
			if used[i] || prior.semantic.Kind != next.semantic.Kind {
				continue
			}
			if identityEquivalent(prior, next, beforeRoot, afterRoot) {
				match = i
				break
			}
		}
		if match < 0 {
			continue
		}
		used[match] = true
		next := rightOnly[match]
		matchedRight[next.semantic.Address] = true
		paths := changedPaths(prior.object, next.object)
		entries = append(entries, SemanticDiffEntry{Kind: SemanticRenamed, SubjectKind: prior.semantic.Kind, BeforeAddress: prior.semantic.Address, AfterAddress: next.semantic.Address, BeforeHash: prior.semantic.OwnHash, AfterHash: next.semantic.OwnHash, OwnerAddress: next.owner, ChangedFieldPaths: paths})
	}
	for address, prior := range left {
		if _, same := right[address]; same {
			continue
		}
		paired := false
		for _, e := range entries {
			if e.BeforeAddress == address {
				paired = true
				break
			}
		}
		if !paired {
			entries = append(entries, SemanticDiffEntry{Kind: SemanticDeleted, SubjectKind: prior.semantic.Kind, BeforeAddress: address, BeforeHash: prior.semantic.OwnHash, OwnerAddress: prior.owner, ChangedFieldPaths: []AuthoredFieldPath{}})
		}
	}
	for address, next := range right {
		if _, same := left[address]; same || matchedRight[address] {
			continue
		}
		entries = append(entries, SemanticDiffEntry{Kind: SemanticCreated, SubjectKind: next.semantic.Kind, AfterAddress: address, AfterHash: next.semantic.OwnHash, OwnerAddress: next.owner, ChangedFieldPaths: []AuthoredFieldPath{}})
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := diffEntryAddress(entries[i]), diffEntryAddress(entries[j])
		if a != b {
			return a < b
		}
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].SubjectKind < entries[j].SubjectKind
	})
	if entries == nil {
		entries = []SemanticDiffEntry{}
	}
	return PlannedSemanticDiff{Entries: entries, Digest: digestJSON(entries)}
}

func diffSubjects(snapshot Snapshot) map[string]diffSubject {
	out := map[string]diffSubject{}
	for _, subject := range snapshot.SemanticIndex.Subjects {
		owner := ""
		if subject.OwnerAddress != nil {
			owner = *subject.OwnerAddress
		} else if snapshot.NormalizedDocument != nil && subject.Kind != materialize.SubjectProject {
			owner = snapshot.NormalizedDocument.Project.Address
		}
		out[subject.Address] = diffSubject{semantic: subject, object: normalizedSubject(snapshot, subject.Address), owner: owner, refs: subjectRefs(snapshot, subject.Address)}
	}
	return out
}

func identityEquivalent(before, after diffSubject, beforeRoot, afterRoot string) bool {
	if beforeRoot != afterRoot && beforeRoot != "" && afterRoot != "" {
		return strings.TrimPrefix(before.semantic.Address, beforeRoot) == strings.TrimPrefix(after.semantic.Address, afterRoot)
	}
	left := clonePlainMap(before.object)
	right := clonePlainMap(after.object)
	delete(left, "address")
	delete(left, "id")
	delete(right, "address")
	delete(right, "id")
	return reflect.DeepEqual(left, right)
}

func clonePlainMap(value map[string]any) map[string]any {
	return deepClone(value)
}

var nonAuthoredDiffFields = map[string]bool{"address": true, "id": true, "rows": true, "columns": true, "unique_constraints": true, "parameters": true, "exports": true, "reserved_row_ids": true, "reserved_column_ids": true, "reserved_constraint_ids": true, "reserved_parameter_ids": true, "reserved_table_column_ids": true, "reserved_export_ids": true, "dependencies": true}

func changedPaths(before, after map[string]any) []AuthoredFieldPath {
	keysMap := map[string]bool{}
	for key := range before {
		if !nonAuthoredDiffFields[key] {
			keysMap[key] = true
		}
	}
	for key := range after {
		if !nonAuthoredDiffFields[key] {
			keysMap[key] = true
		}
	}
	keys := make([]string, 0, len(keysMap))
	for key := range keysMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]AuthoredFieldPath, 0)
	for _, key := range keys {
		if !reflect.DeepEqual(before[key], after[key]) {
			out = append(out, AuthoredFieldPath{Tokens: []string{key}})
		}
	}
	return out
}

func diffEntryAddress(entry SemanticDiffEntry) string {
	if entry.AfterAddress != "" {
		return entry.AfterAddress
	}
	return entry.BeforeAddress
}
func subjectRefs(snapshot Snapshot, address string) []string {
	set := map[string]bool{}
	for _, ref := range snapshot.SemanticIndex.References {
		if ref.SourceAddress == address {
			set[ref.TargetAddress] = true
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func buildPlannedAuthoringImpact(before, after Snapshot, source PlannedSourceDiff, semantic PlannedSemanticDiff) PlannedAuthoringImpact {
	classBefore := classificationMap(before)
	classAfter := classificationMap(after)
	entries := make([]AuthoringImpactEntry, 0, len(semantic.Entries))
	caps := map[AuthoringCapability]bool{}
	for _, change := range semantic.Entries {
		address := change.AfterAddress
		if address == "" {
			address = change.BeforeAddress
		}
		classification, ok := classAfter[change.AfterAddress]
		if !ok {
			classification = classBefore[change.BeforeAddress]
		}
		capability := classification.Capability
		caps[capability] = true
		action := AuthoringImpactAction("update")
		switch change.Kind {
		case SemanticCreated:
			action = "create"
		case SemanticDeleted:
			action = "delete"
		case SemanticRenamed:
			action = "rename"
		case SemanticMoved:
			action = "move"
		case SemanticReferenceChanged:
			action = "bind"
		}
		entry := AuthoringImpactEntry{Capability: capability, Action: action, SubjectKind: change.SubjectKind, SubjectAddress: address, OwnerAddress: change.OwnerAddress, ChangedFieldPaths: cloneFieldPaths(change.ChangedFieldPaths), BeforeRefs: subjectRefs(before, change.BeforeAddress), AfterRefs: subjectRefs(after, change.AfterAddress), SourceRefs: impactSourceRefs(before, after, change)}
		if capability == CapabilityGraphWrite {
			entry.GraphFacts = graphFactsForChange(before, after, change, action)
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 && len(source.Edits) != 0 {
		capability := CapabilitySourceMaintain
		caps[capability] = true
		root := rootProjectAddress(after)
		entries = append(entries, AuthoringImpactEntry{Capability: capability, Action: "maintain", SubjectKind: materialize.SubjectProject, SubjectAddress: root, ChangedFieldPaths: []AuthoredFieldPath{}, BeforeRefs: []string{}, AfterRefs: []string{}, SourceRefs: sourceEditRefs(source)})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].SubjectAddress != entries[j].SubjectAddress {
			return entries[i].SubjectAddress < entries[j].SubjectAddress
		}
		if entries[i].Capability != entries[j].Capability {
			return entries[i].Capability < entries[j].Capability
		}
		return entries[i].Action < entries[j].Action
	})
	capOrder := map[AuthoringCapability]int{CapabilityGraphWrite: 1, CapabilityProjectConfigure: 3, CapabilityQueryWrite: 4, CapabilityReferenceWrite: 5, CapabilitySchemaWrite: 6, CapabilitySourceMaintain: 7, CapabilityViewWrite: 8}
	required := make([]AuthoringCapability, 0, len(caps))
	for cap := range caps {
		required = append(required, cap)
	}
	sort.Slice(required, func(i, j int) bool { return capOrder[required[i]] < capOrder[required[j]] })
	impact := PlannedAuthoringImpact{BaseDefinitionHash: before.DefinitionHash, ResultingDefinitionHash: after.DefinitionHash, SemanticDiffHash: semantic.Digest, SourceDiffHash: source.Digest, Entries: entries, RequiredCapabilities: required}
	impact.ImpactDigest = digestJSON(struct {
		Base, Result, Semantic, Source string
		Entries                        []AuthoringImpactEntry
		Capabilities                   []AuthoringCapability
	}{impact.BaseDefinitionHash, impact.ResultingDefinitionHash, impact.SemanticDiffHash, impact.SourceDiffHash, impact.Entries, impact.RequiredCapabilities})
	return impact
}

func classificationMap(snapshot Snapshot) map[string]AuthoringSubjectClassification {
	out := map[string]AuthoringSubjectClassification{}
	for _, value := range snapshot.AuthoringSubjectClassification {
		out[value.Address] = value
	}
	return out
}
func cloneFieldPaths(input []AuthoredFieldPath) []AuthoredFieldPath {
	out := make([]AuthoredFieldPath, len(input))
	for i, path := range input {
		out[i].Tokens = append([]string(nil), path.Tokens...)
	}
	return out
}

func impactSourceRefs(before, after Snapshot, change SemanticDiffEntry) []SourceRange {
	refs := sourceRangesFor(after, change.AfterAddress)
	if len(refs) == 0 {
		refs = sourceRangesFor(before, change.BeforeAddress)
	}
	return refs
}
func sourceRangesFor(snapshot Snapshot, address string) []SourceRange {
	for _, subject := range snapshot.SourceMap.Subjects {
		if subject.Address == address && subject.DeclarationRange != nil {
			return []SourceRange{*subject.DeclarationRange}
		}
	}
	return []SourceRange{}
}
func sourceEditRefs(diff PlannedSourceDiff) []SourceRange {
	out := make([]SourceRange, 0, len(diff.Edits))
	for _, edit := range diff.Edits {
		out = append(out, SourceRange{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: edit.ModulePath, StartByte: edit.StartByte, EndByte: edit.EndByte})
	}
	return out
}

func graphFactsForChange(before, after Snapshot, change SemanticDiffEntry, action AuthoringImpactAction) *GraphAuthoringFacts {
	facts := &GraphAuthoringFacts{EntityTypeAddresses: []string{}, RelationTypeAddresses: []string{}, LayerAddresses: []string{}, ColumnAddresses: []string{}, EndpointEntityAddresses: []string{}, ActionFlags: []string{string(action)}}
	object := normalizedSubject(after, change.AfterAddress)
	if object == nil {
		object = normalizedSubject(before, change.BeforeAddress)
	}
	appendString := func(target *[]string, key string) {
		if value, ok := object[key].(string); ok && value != "" {
			*target = append(*target, value)
		}
	}
	switch change.SubjectKind {
	case materialize.SubjectEntity:
		appendString(&facts.EntityTypeAddresses, "type_address")
		appendString(&facts.LayerAddresses, "layer_address")
	case materialize.SubjectRelation:
		appendString(&facts.RelationTypeAddresses, "type_address")
		appendString(&facts.EndpointEntityAddresses, "from_address")
		appendString(&facts.EndpointEntityAddresses, "to_address")
	case materialize.SubjectEntityRow, materialize.SubjectRelationRow:
		ownerAddress := change.OwnerAddress
		owner := normalizedSubject(after, ownerAddress)
		if owner == nil {
			owner = normalizedSubject(before, ownerAddress)
		}
		if change.SubjectKind == materialize.SubjectEntityRow {
			if value, ok := owner["type_address"].(string); ok {
				facts.EntityTypeAddresses = append(facts.EntityTypeAddresses, value)
			}
			if value, ok := owner["layer_address"].(string); ok {
				facts.LayerAddresses = append(facts.LayerAddresses, value)
			}
		} else {
			if value, ok := owner["type_address"].(string); ok {
				facts.RelationTypeAddresses = append(facts.RelationTypeAddresses, value)
			}
			if value, ok := owner["from_address"].(string); ok {
				facts.EndpointEntityAddresses = append(facts.EndpointEntityAddresses, value)
			}
			if value, ok := owner["to_address"].(string); ok {
				facts.EndpointEntityAddresses = append(facts.EndpointEntityAddresses, value)
			}
		}
		if values, ok := object["values"].(map[string]any); ok {
			for address := range values {
				facts.ColumnAddresses = append(facts.ColumnAddresses, address)
			}
		}
	}
	for _, target := range [][]string{facts.EntityTypeAddresses, facts.RelationTypeAddresses, facts.LayerAddresses, facts.ColumnAddresses, facts.EndpointEntityAddresses} {
		sort.Strings(target)
	}
	return facts
}
