// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

func buildPlannedSourceDiff(before, after map[string][]byte) PlannedSourceDiff {
	diff, _ := buildPlannedSourceDiffContext(context.Background(), before, after, SemanticPlanLimits{})
	return diff
}

func buildPlannedSourceDiffContext(ctx context.Context, before, after map[string][]byte, limits SemanticPlanLimits) (PlannedSourceDiff, error) {
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
		if err := semanticEditCancellation(ctx, "semantic_edit_source_diff"); err != nil {
			return PlannedSourceDiff{}, err
		}
		left, leftOK := before[path]
		right, rightOK := after[path]
		switch {
		case !leftOK:
			digest := semanticDigest(right)
			module := projectModuleRef(path)
			blob := plannedSourceBlob(path, 0, 0, right)
			edits = append(edits, PlannedSourceEdit{Kind: PlannedSourceCreate, ModulePath: path, AfterModule: &module, AfterDigest: digest, ReplacementBlob: &blob, Replacement: bytes.Clone(right)})
		case !rightOK:
			module := projectModuleRef(path)
			edits = append(edits, PlannedSourceEdit{Kind: PlannedSourceDelete, ModulePath: path, BeforeModule: &module, BeforeDigest: semanticDigest(left), StartByte: 0, EndByte: len(left)})
		case !bytes.Equal(left, right):
			for _, edit := range minimalModuleEdits(path, left, right) {
				blob := plannedSourceBlob(path, 0, len(right), right)
				edit.ReplacementBlob = &blob
				edits = append(edits, edit)
			}
		}
		if limits.MaxItems > 0 && int64(len(edits)) > limits.MaxItems {
			return PlannedSourceDiff{}, semanticPlanLimitError("source_edits", limits.MaxItems, int64(len(edits)))
		}
	}
	// Exact byte identity is sufficient source-file move authority; semantic
	// subject identity remains exclusively driven by authored lineage.
	used := make([]bool, len(edits))
	moves := make([]PlannedSourceEdit, 0)
	for left := range edits {
		if edits[left].Kind != PlannedSourceDelete || used[left] {
			continue
		}
		for right := range edits {
			if used[right] || edits[right].Kind != PlannedSourceCreate || !bytes.Equal(before[edits[left].ModulePath], after[edits[right].ModulePath]) {
				continue
			}
			beforeModule, afterModule := *edits[left].BeforeModule, *edits[right].AfterModule
			moves = append(moves, PlannedSourceEdit{Kind: PlannedSourceMove, BeforeModule: &beforeModule, AfterModule: &afterModule, BeforeDigest: edits[left].BeforeDigest, AfterDigest: edits[right].AfterDigest, ModulePath: beforeModule.ModulePath})
			used[left], used[right] = true, true
			break
		}
	}
	paired := moves
	for index, edit := range edits {
		if !used[index] {
			paired = append(paired, edit)
		}
	}
	edits = paired
	if edits == nil {
		edits = []PlannedSourceEdit{}
	}
	sort.SliceStable(edits, func(i, j int) bool { return compareSourceEdits(edits[i], edits[j]) < 0 })
	digestInput := make([]any, 0, len(edits))
	for _, e := range edits {
		digestInput = append(digestInput, sourceEditWireValue(e))
	}
	return PlannedSourceDiff{Edits: edits, Digest: digestJSON(digestInput)}, nil
}

func projectModuleRef(path string) PlannedModuleRef {
	return PlannedModuleRef{OriginKind: SourceOriginProject, ModulePath: path}
}

func plannedSourceBlob(path string, start, end int, data []byte) PlannedBlobRef {
	digest := semanticDigest(data)
	id := strings.TrimPrefix(digest, "sha256:")
	return PlannedBlobRef{BlobID: "semantic-edit-" + id, Digest: digest, Lifetime: "request", MediaType: "text/plain; charset=utf-8", Size: uint64(len(data)), Bytes: bytes.Clone(data)}
}

func semanticPlanLimitError(resource string, limit, observed int64) error {
	return &CompileError{Code: "engine.workbench.limit_exceeded", Category: ErrorCategoryResource, Resource: resource, Limit: limit, Observed: observed, Stage: "semantic_edit"}
}

func sourceEditWireValue(edit PlannedSourceEdit) map[string]any {
	value := map[string]any{"kind": string(edit.Kind)}
	if edit.BeforeModule != nil {
		value["before_module"] = moduleRefWireValue(*edit.BeforeModule)
	}
	if edit.AfterModule != nil {
		value["after_module"] = moduleRefWireValue(*edit.AfterModule)
	}
	if edit.BeforeDigest != "" {
		value["before_digest"] = edit.BeforeDigest
	}
	if edit.AfterDigest != "" {
		value["after_digest"] = edit.AfterDigest
	}
	if edit.SourceRange != nil {
		value["source_range"] = sourceRangeWireValue(*edit.SourceRange)
	}
	if edit.ReplacementBlob != nil {
		value["replacement_blob"] = map[string]any{"blob_id": edit.ReplacementBlob.BlobID, "digest": edit.ReplacementBlob.Digest, "lifetime": edit.ReplacementBlob.Lifetime, "media_type": edit.ReplacementBlob.MediaType, "size": strconv.FormatUint(edit.ReplacementBlob.Size, 10)}
	}
	return value
}

func moduleRefWireValue(module PlannedModuleRef) map[string]any {
	origin := map[string]any{"kind": string(module.OriginKind)}
	if module.PackAddress != "" {
		origin["pack_address"] = module.PackAddress
	}
	return map[string]any{"module_path": module.ModulePath, "origin": origin}
}

func sourceRangeWireValue(value SourceRange) map[string]any {
	origin := map[string]any{"kind": string(value.Origin.Kind)}
	if value.Origin.PackAddress != "" {
		origin["pack_address"] = value.Origin.PackAddress
	}
	return map[string]any{"end_byte": strconv.Itoa(value.EndByte), "module_path": value.ModulePath, "origin": origin, "start_byte": strconv.Itoa(value.StartByte)}
}

func compareSourceEdits(left, right PlannedSourceEdit) int {
	leftPath, rightPath := sourceEditPrimaryPath(left), sourceEditPrimaryPath(right)
	if leftPath < rightPath {
		return -1
	}
	if leftPath > rightPath {
		return 1
	}
	if compared := strings.Compare(string(left.Kind), string(right.Kind)); compared != 0 {
		return compared
	}
	if left.SourceRange != nil && right.SourceRange == nil {
		return 1
	}
	if left.SourceRange == nil && right.SourceRange != nil {
		return -1
	}
	if left.StartByte < right.StartByte {
		return -1
	}
	if left.StartByte > right.StartByte {
		return 1
	}
	leftAfter, rightAfter := "", ""
	if left.AfterModule != nil {
		leftAfter = left.AfterModule.ModulePath
	}
	if right.AfterModule != nil {
		rightAfter = right.AfterModule.ModulePath
	}
	return strings.Compare(leftAfter, rightAfter)
}

func sourceEditPrimaryPath(edit PlannedSourceEdit) string {
	if edit.SourceRange != nil {
		return edit.SourceRange.ModulePath
	}
	if edit.BeforeModule != nil {
		return edit.BeforeModule.ModulePath
	}
	if edit.AfterModule != nil {
		return edit.AfterModule.ModulePath
	}
	return edit.ModulePath
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
			replacement := bytes.Clone(after[as:ae])
			rangeValue := SourceRange{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: path, StartByte: bs, EndByte: be}
			blob := plannedSourceBlob(path, bs, be, replacement)
			edits = append(edits, PlannedSourceEdit{Kind: PlannedSourceReplace, ModulePath: path, StartByte: bs, EndByte: be, SourceRange: &rangeValue, BeforeDigest: semanticDigest(before), AfterDigest: semanticDigest(after), ReplacementBlob: &blob, Replacement: replacement})
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
	rangeValue := SourceRange{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: path, StartByte: prefix, EndByte: len(before) - suffix}
	blob := plannedSourceBlob(path, prefix, len(before)-suffix, replacement)
	return []PlannedSourceEdit{{Kind: PlannedSourceReplace, ModulePath: path, StartByte: prefix, EndByte: len(before) - suffix, SourceRange: &rangeValue, BeforeDigest: semanticDigest(before), AfterDigest: semanticDigest(after), ReplacementBlob: &blob, Replacement: replacement}}
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
	diff, _ := buildPlannedSemanticDiffContext(context.Background(), before, after, nil, SemanticPlanLimits{})
	return diff
}

// BuildCanonicalAuthoringPlan is the single compiler-authoritative derivation
// of source diff, semantic diff, and authoring impact. Endpoint preview and the
// commit path consume the same before/after snapshots and authored lineage.
func BuildCanonicalAuthoringPlan(ctx context.Context, before, after Snapshot, beforeTree, afterTree map[string][]byte, lineage []IdentityLineage, limits SemanticPlanLimits) (PlannedSourceDiff, PlannedSemanticDiff, PlannedAuthoringImpact, error) {
	source, err := buildPlannedSourceDiffContext(ctx, beforeTree, afterTree, limits)
	if err != nil {
		return PlannedSourceDiff{}, PlannedSemanticDiff{}, PlannedAuthoringImpact{}, err
	}
	semantic, err := buildPlannedSemanticDiffContext(ctx, before, after, lineage, limits)
	if err != nil {
		return PlannedSourceDiff{}, PlannedSemanticDiff{}, PlannedAuthoringImpact{}, err
	}
	impact, err := buildPlannedAuthoringImpactContext(ctx, before, after, source, semantic, limits)
	if err != nil {
		return PlannedSourceDiff{}, PlannedSemanticDiff{}, PlannedAuthoringImpact{}, err
	}
	return source, semantic, impact, nil
}

func buildPlannedSemanticDiffContext(ctx context.Context, before, after Snapshot, lineage []IdentityLineage, limits SemanticPlanLimits) (PlannedSemanticDiff, error) {
	left := diffSubjects(before)
	right := diffSubjects(after)
	authoredKinds := map[string]SemanticChangeKind{}
	for _, edge := range lineage {
		if edge.BeforeAddress == edge.AfterAddress && edge.BeforeAddress != "" {
			authoredKinds[edge.BeforeAddress] = edge.ChangeKind
		}
	}
	entries := make([]SemanticDiffEntry, 0)
	matchedRight := map[string]bool{}
	for address, prior := range left {
		if err := semanticEditCancellation(ctx, "semantic_edit_semantic_diff"); err != nil {
			return PlannedSemanticDiff{}, err
		}
		if next, ok := right[address]; ok {
			matchedRight[address] = true
			if prior.semantic.OwnHash == next.semantic.OwnHash {
				continue
			}
			paths := changedPaths(prior.object, next.object)
			kind := SemanticUpdated
			if authoredKinds[address] != "" {
				kind = authoredKinds[address]
			} else if len(paths) == 1 && len(paths[0].Tokens) == 1 && paths[0].Tokens[0] == "layer_address" && prior.semantic.Kind == materialize.SubjectEntity {
				kind = SemanticMoved
			} else if !reflect.DeepEqual(prior.refs, next.refs) {
				kind = SemanticReferenceChanged
			}
			entries = append(entries, SemanticDiffEntry{Kind: kind, SubjectKind: prior.semantic.Kind, BeforeAddress: address, AfterAddress: address, BeforeHash: prior.semantic.OwnHash, AfterHash: next.semantic.OwnHash, OwnerAddress: next.owner, ChangedFieldPaths: paths})
		}
	}
	// Identity is never guessed from structural similarity. Only an exact
	// authored rename/project-migration lineage edge can pair two addresses.
	lineage = append([]IdentityLineage(nil), lineage...)
	sort.Slice(lineage, func(i, j int) bool {
		if compared := compareStableAddressText(lineage[i].BeforeAddress, lineage[j].BeforeAddress); compared != 0 {
			return compared < 0
		}
		return compareStableAddressText(lineage[i].AfterAddress, lineage[j].AfterAddress) < 0
	})
	pairedLeft := map[string]bool{}
	for _, edge := range lineage {
		if err := semanticEditCancellation(ctx, "semantic_edit_semantic_diff"); err != nil {
			return PlannedSemanticDiff{}, err
		}
		prior, leftOK := left[edge.BeforeAddress]
		next, rightOK := right[edge.AfterAddress]
		if !leftOK || !rightOK || pairedLeft[edge.BeforeAddress] || matchedRight[edge.AfterAddress] || prior.semantic.Kind != next.semantic.Kind || (edge.Kind != "" && edge.Kind != prior.semantic.Kind) {
			continue
		}
		kind := edge.ChangeKind
		if kind == "" {
			kind = SemanticRenamed
		}
		pairedLeft[edge.BeforeAddress] = true
		matchedRight[edge.AfterAddress] = true
		entries = append(entries, SemanticDiffEntry{Kind: kind, SubjectKind: prior.semantic.Kind, BeforeAddress: edge.BeforeAddress, AfterAddress: edge.AfterAddress, BeforeHash: prior.semantic.OwnHash, AfterHash: next.semantic.OwnHash, OwnerAddress: next.owner, ChangedFieldPaths: changedPaths(prior.object, next.object)})
	}
	for address, prior := range left {
		if _, same := right[address]; same {
			continue
		}
		if !pairedLeft[address] {
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
		if compared := compareStableAddressText(a, b); compared != 0 {
			return compared < 0
		}
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].SubjectKind < entries[j].SubjectKind
	})
	if entries == nil {
		entries = []SemanticDiffEntry{}
	}
	if limits.MaxItems > 0 && int64(len(entries)) > limits.MaxItems {
		return PlannedSemanticDiff{}, semanticPlanLimitError("semantic_diff.entries", limits.MaxItems, int64(len(entries)))
	}
	wire := make([]any, 0, len(entries))
	for _, entry := range entries {
		wire = append(wire, semanticDiffEntryWireValue(entry))
	}
	return PlannedSemanticDiff{Entries: entries, Digest: digestJSON(wire)}, nil
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
	if entry.BeforeAddress != "" {
		return entry.BeforeAddress
	}
	return entry.AfterAddress
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
	impact, _ := buildPlannedAuthoringImpactContext(context.Background(), before, after, source, semantic, SemanticPlanLimits{})
	return impact
}

func buildPlannedAuthoringImpactContext(ctx context.Context, before, after Snapshot, source PlannedSourceDiff, semantic PlannedSemanticDiff, limits SemanticPlanLimits) (PlannedAuthoringImpact, error) {
	classBefore := classificationMap(before)
	classAfter := classificationMap(after)
	entries := make([]AuthoringImpactEntry, 0, len(semantic.Entries))
	caps := map[AuthoringCapability]bool{}
	for _, change := range semantic.Entries {
		if err := semanticEditCancellation(ctx, "semantic_edit_authoring_impact"); err != nil {
			return PlannedAuthoringImpact{}, err
		}
		address := change.AfterAddress
		if address == "" {
			address = change.BeforeAddress
		}
		classification, ok := classAfter[change.AfterAddress]
		if !ok {
			classification, ok = classBefore[change.BeforeAddress]
		}
		if !ok || classification.Capability == "" {
			return PlannedAuthoringImpact{}, fmt.Errorf("missing authoring classification for %s", address)
		}
		capability := classification.Capability
		caps[capability] = true
		actions := []AuthoringImpactAction{"update"}
		switch change.Kind {
		case SemanticCreated:
			actions = []AuthoringImpactAction{"create"}
		case SemanticDeleted:
			actions = []AuthoringImpactAction{"delete"}
		case SemanticRenamed:
			actions = []AuthoringImpactAction{"rename"}
		case SemanticMoved:
			actions = []AuthoringImpactAction{"move"}
		case SemanticReferenceChanged:
			actions = referenceImpactActions(subjectRefs(before, change.BeforeAddress), subjectRefs(after, change.AfterAddress))
		}
		for _, action := range actions {
			entry := AuthoringImpactEntry{Capability: capability, Action: action, SubjectKind: change.SubjectKind, SubjectAddress: address, OwnerAddress: change.OwnerAddress, ChangedFieldPaths: cloneFieldPaths(change.ChangedFieldPaths), BeforeRefs: subjectRefs(before, change.BeforeAddress), AfterRefs: subjectRefs(after, change.AfterAddress), SourceRefs: impactSourceRefs(before, after, change)}
			if capability == CapabilityGraphWrite {
				entry.GraphFacts = graphFactsForChange(before, after, change, action)
			}
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 && len(source.Edits) != 0 {
		capability := CapabilitySourceMaintain
		caps[capability] = true
		root := rootProjectAddress(after)
		entries = append(entries, AuthoringImpactEntry{Capability: capability, Action: "maintain", SubjectKind: materialize.SubjectProject, SubjectAddress: root, ChangedFieldPaths: []AuthoredFieldPath{}, BeforeRefs: []string{}, AfterRefs: []string{}, SourceRefs: sourceEditRefs(source)})
	}
	sort.Slice(entries, func(i, j int) bool {
		if compared := compareStableAddressText(entries[i].SubjectAddress, entries[j].SubjectAddress); compared != 0 {
			return compared < 0
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
	if limits.MaxItems > 0 && int64(len(entries)+len(required)) > limits.MaxItems {
		return PlannedAuthoringImpact{}, semanticPlanLimitError("authoring_impact", limits.MaxItems, int64(len(entries)+len(required)))
	}
	impact.ImpactDigest = digestJSON(authoringImpactWireValue(impact, false))
	return impact, nil
}

func referenceImpactActions(before, after []string) []AuthoringImpactAction {
	beforeSet, afterSet := map[string]bool{}, map[string]bool{}
	for _, value := range before {
		beforeSet[value] = true
	}
	for _, value := range after {
		afterSet[value] = true
	}
	removed, added := false, false
	for value := range beforeSet {
		removed = removed || !afterSet[value]
	}
	for value := range afterSet {
		added = added || !beforeSet[value]
	}
	actions := make([]AuthoringImpactAction, 0, 2)
	if added {
		actions = append(actions, "bind")
	}
	if removed {
		actions = append(actions, "unbind")
	}
	if len(actions) == 0 {
		return []AuthoringImpactAction{"update"}
	}
	return actions
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
		if edit.SourceRange != nil {
			out = append(out, *edit.SourceRange)
		}
	}
	return out
}

func graphFactsForChange(before, after Snapshot, change SemanticDiffEntry, action AuthoringImpactAction) *GraphAuthoringFacts {
	facts := &GraphAuthoringFacts{EntityTypeAddresses: []string{}, RelationTypeAddresses: []string{}, LayerAddresses: []string{}, ColumnAddresses: []string{}, EndpointEntityAddresses: []string{}, ActionFlags: []string{string(action)}}
	objects := []map[string]any{normalizedSubject(before, change.BeforeAddress), normalizedSubject(after, change.AfterAddress)}
	appendString := func(target *[]string, object map[string]any, key string) {
		if value, ok := object[key].(string); ok && value != "" && !slices.Contains(*target, value) {
			*target = append(*target, value)
		}
	}
	switch change.SubjectKind {
	case materialize.SubjectEntity:
		for _, object := range objects {
			appendString(&facts.EntityTypeAddresses, object, "type_address")
			appendString(&facts.LayerAddresses, object, "layer_address")
		}
	case materialize.SubjectRelation:
		for _, object := range objects {
			appendString(&facts.RelationTypeAddresses, object, "type_address")
			appendString(&facts.EndpointEntityAddresses, object, "from_address")
			appendString(&facts.EndpointEntityAddresses, object, "to_address")
		}
	case materialize.SubjectEntityRow, materialize.SubjectRelationRow:
		for snapshotIndex, snapshot := range []Snapshot{before, after} {
			rowAddress := change.BeforeAddress
			if snapshotIndex == 1 {
				rowAddress = change.AfterAddress
			}
			row := normalizedSubject(snapshot, rowAddress)
			ownerAddress := ownerAddressFor(snapshot, rowAddress)
			owner := normalizedSubject(snapshot, ownerAddress)
			if change.SubjectKind == materialize.SubjectEntityRow {
				appendString(&facts.EntityTypeAddresses, owner, "type_address")
				appendString(&facts.LayerAddresses, owner, "layer_address")
			} else {
				appendString(&facts.RelationTypeAddresses, owner, "type_address")
				appendString(&facts.EndpointEntityAddresses, owner, "from_address")
				appendString(&facts.EndpointEntityAddresses, owner, "to_address")
			}
			if values, ok := row["values"].(map[string]any); ok {
				for address := range values {
					if !slices.Contains(facts.ColumnAddresses, address) {
						facts.ColumnAddresses = append(facts.ColumnAddresses, address)
					}
				}
			}
		}
	}
	for _, target := range [][]string{facts.EntityTypeAddresses, facts.RelationTypeAddresses, facts.LayerAddresses, facts.ColumnAddresses, facts.EndpointEntityAddresses} {
		sort.Strings(target)
	}
	return facts
}

func ownerAddressFor(snapshot Snapshot, address string) string {
	for _, subject := range snapshot.SemanticIndex.Subjects {
		if subject.Address == address && subject.OwnerAddress != nil {
			return *subject.OwnerAddress
		}
	}
	return ""
}

func semanticDiffEntryWireValue(entry SemanticDiffEntry) map[string]any {
	value := map[string]any{"kind": string(entry.Kind), "changed_field_paths": fieldPathsWireValue(entry.ChangedFieldPaths), "subject_kind": string(entry.SubjectKind)}
	if entry.BeforeAddress != "" {
		value["before_address"] = entry.BeforeAddress
		value["before_hash"] = entry.BeforeHash
	}
	if entry.AfterAddress != "" {
		value["after_address"] = entry.AfterAddress
		value["after_hash"] = entry.AfterHash
	}
	if entry.OwnerAddress != "" {
		value["owner_address"] = entry.OwnerAddress
	}
	return value
}

func fieldPathsWireValue(paths []AuthoredFieldPath) []any {
	out := make([]any, 0, len(paths))
	for _, path := range paths {
		tokens := make([]any, len(path.Tokens))
		for index, token := range path.Tokens {
			tokens[index] = token
		}
		out = append(out, map[string]any{"tokens": tokens})
	}
	return out
}

func authoringImpactWireValue(impact PlannedAuthoringImpact, includeDigest bool) map[string]any {
	entries := make([]any, 0, len(impact.Entries))
	for _, entry := range impact.Entries {
		value := map[string]any{"action": string(entry.Action), "after_refs": stringsToAny(entry.AfterRefs), "before_refs": stringsToAny(entry.BeforeRefs), "capability": string(entry.Capability), "changed_field_paths": fieldPathsWireValue(entry.ChangedFieldPaths), "source_refs": sourceRangesWireValue(entry.SourceRefs), "subject_address": entry.SubjectAddress, "subject_kind": string(entry.SubjectKind)}
		if entry.OwnerAddress != "" {
			value["owner_address"] = entry.OwnerAddress
		}
		if entry.GraphFacts != nil {
			value["graph_facts"] = graphFactsWireValue(*entry.GraphFacts)
		}
		entries = append(entries, value)
	}
	caps := make([]any, len(impact.RequiredCapabilities))
	for index, capability := range impact.RequiredCapabilities {
		caps[index] = string(capability)
	}
	value := map[string]any{"base_definition_hash": impact.BaseDefinitionHash, "entries": entries, "required_capabilities": caps, "resulting_definition_hash": impact.ResultingDefinitionHash, "semantic_diff_hash": impact.SemanticDiffHash, "source_diff_hash": impact.SourceDiffHash}
	if includeDigest {
		value["impact_digest"] = impact.ImpactDigest
	}
	return value
}

func graphFactsWireValue(facts GraphAuthoringFacts) map[string]any {
	return map[string]any{"action_flags": stringsToAny(facts.ActionFlags), "column_addresses": stringsToAny(facts.ColumnAddresses), "endpoint_entity_addresses": stringsToAny(facts.EndpointEntityAddresses), "entity_type_addresses": stringsToAny(facts.EntityTypeAddresses), "layer_addresses": stringsToAny(facts.LayerAddresses), "relation_type_addresses": stringsToAny(facts.RelationTypeAddresses)}
}

func sourceRangesWireValue(values []SourceRange) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, sourceRangeWireValue(value))
	}
	return out
}

func stringsToAny(values []string) []any {
	out := make([]any, len(values))
	for index, value := range values {
		out[index] = value
	}
	return out
}

func compareStableAddressText(left, right string) int {
	leftOrigin, leftComponents, leftPath, leftOK := stableAddressTupleForPlanner(left)
	rightOrigin, rightComponents, rightPath, rightOK := stableAddressTupleForPlanner(right)
	if !leftOK || !rightOK {
		return strings.Compare(left, right)
	}
	if leftOrigin != rightOrigin {
		return leftOrigin - rightOrigin
	}
	for index := 0; index < len(leftComponents) && index < len(rightComponents); index++ {
		if compared := strings.Compare(leftComponents[index], rightComponents[index]); compared != 0 {
			return compared
		}
	}
	if len(leftComponents) != len(rightComponents) {
		return len(leftComponents) - len(rightComponents)
	}
	if len(leftPath) != len(rightPath) {
		return len(leftPath) - len(rightPath)
	}
	for index := range leftPath {
		leftRank, rightRank := stableAddressKindRankForPlanner(leftPath[index][0]), stableAddressKindRankForPlanner(rightPath[index][0])
		if leftRank != rightRank {
			return leftRank - rightRank
		}
		if compared := strings.Compare(leftPath[index][1], rightPath[index][1]); compared != 0 {
			return compared
		}
	}
	return 0
}

func stableAddressTupleForPlanner(value string) (int, []string, [][2]string, bool) {
	parts := strings.Split(value, ":")
	if len(parts) < 3 || parts[0] != "ldl" {
		return 0, nil, nil, false
	}
	origin, pathStart, components := 0, 3, []string{parts[2]}
	if parts[1] == "pack" {
		if len(parts) < 4 {
			return 0, nil, nil, false
		}
		origin, pathStart, components = 1, 4, []string{parts[2], parts[3]}
	} else if parts[1] != "project" {
		return 0, nil, nil, false
	}
	if (len(parts)-pathStart)%2 != 0 {
		return 0, nil, nil, false
	}
	path := make([][2]string, 0, (len(parts)-pathStart)/2)
	for index := pathStart; index < len(parts); index += 2 {
		path = append(path, [2]string{parts[index], parts[index+1]})
	}
	return origin, components, path, true
}

func stableAddressKindRankForPlanner(kind string) int {
	ranks := map[string]int{"entity-type": 0, "relation-type": 1, "layer": 2, "entity": 3, "relation": 4, "query": 5, "view": 6, "reference": 7, "column": 8, "constraint": 9, "row": 10, "parameter": 11, "table-column": 12, "export": 13}
	if rank, ok := ranks[kind]; ok {
		return rank
	}
	return len(ranks)
}
