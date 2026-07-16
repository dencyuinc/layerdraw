// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package sourceplanner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type diffPair struct {
	semantic SemanticDiff
	source   SourceDiff
}

func emptyDiffs() diffPair {
	semanticDiff := SemanticDiff{Entries: []SemanticDiffEntry{}}
	semanticDiff.Digest = hashJSON(semanticDiff.Entries)
	sourceDiff := SourceDiff{Edits: []SourceEdit{}}
	sourceDiff.Digest = hashJSON(sourceDiff.Edits)
	return diffPair{semantic: semanticDiff, source: sourceDiff}
}

func invalidPlan(base SourcePlanningBase, diffs diffPair, diagnostics []Diagnostic, conflicts []SemanticConflict) SourcePlan {
	if diagnostics == nil {
		diagnostics = []Diagnostic{}
	}
	if conflicts == nil {
		conflicts = []SemanticConflict{}
	}
	return SourcePlan{Preview: WorkbenchPreviewResult{
		Status: "invalid", BaseGeneration: base.Generation, ChangedSourceFiles: []ModuleRef{},
		SemanticDiff: diffs.semantic, SourceDiff: diffs.source, Conflicts: conflicts, Diagnostics: diagnostics,
	}, Candidate: cloneCompileInput(base.Input), Attachments: PlannerBlobs{}}
}

func checkPreconditions(generation Generation, expected EngineEditPreconditions, before Snapshot) ([]Diagnostic, []SemanticConflict) {
	if generation != expected.Generation {
		return diagnostics("LDL1801", "stale_revision_or_semantic_hash", "document generation does not match the immutable planning base", nil), []SemanticConflict{{Kind: "stale_revision"}}
	}
	files := map[string]string{}
	for _, item := range before.SourceMap.Files {
		if item.Origin.Kind == "project" {
			files[item.ModulePath] = item.Digest
		}
	}
	subjects := map[string]string{}
	for _, item := range before.SubjectSemanticHashes {
		subjects[item.Address] = item.Hash
	}
	subtrees := map[string]string{}
	for _, item := range before.SubtreeHashes {
		subtrees[item.OwnerAddress] = item.Hash
	}
	childSets := map[string]string{}
	for _, item := range before.ChildSetHashes {
		childSets[item.OwnerAddress+"\x00"+string(item.ChildKind)] = item.Hash
	}
	for _, item := range expected.ExpectedSubjectHashes {
		if subjects[string(item.Address)] != string(item.Hash) {
			address := item.Address
			return diagnostics("LDL1801", "stale_revision_or_semantic_hash", "expected subject hash is stale", nil), []SemanticConflict{{Kind: "subject_changed", TargetAddress: &address}}
		}
	}
	for _, item := range expected.ExpectedSubtreeHashes {
		if subtrees[string(item.Address)] != string(item.Hash) {
			address := item.Address
			return diagnostics("LDL1801", "stale_revision_or_semantic_hash", "expected subtree hash is stale", nil), []SemanticConflict{{Kind: "subtree_changed", OwnerAddress: &address}}
		}
	}
	for _, item := range expected.ExpectedChildSets {
		if childSets[string(item.OwnerAddress)+"\x00"+string(item.ChildKind)] != string(item.Hash) {
			owner, kind := item.OwnerAddress, item.ChildKind
			return diagnostics("LDL1801", "stale_revision_or_semantic_hash", "expected child-set hash is stale", nil), []SemanticConflict{{Kind: "child_set_changed", OwnerAddress: &owner, ChildKind: &kind}}
		}
	}
	if expected.ExpectedSourceDigests != nil {
		for _, item := range *expected.ExpectedSourceDigests {
			if item.Module.Origin.Kind != OriginKindProject || files[item.Module.ModulePath] != string(item.Digest) {
				return diagnostics("LDL1801", "stale_revision_or_semantic_hash", "expected source digest is stale", nil), []SemanticConflict{{Kind: "stale_revision"}}
			}
		}
	}
	return nil, nil
}

func finalizePlan(ctx context.Context, base SourcePlanningBase, before Snapshot, candidate CompileInput, after Snapshot) (SourcePlan, error) {
	if err := ctx.Err(); err != nil {
		return SourcePlan{}, err
	}
	attachments := PlannerBlobs{}
	sourceDiff, changed, err := buildSourceDiff(base.Input.ProjectSourceTree, candidate.ProjectSourceTree, attachments)
	if err != nil {
		return SourcePlan{}, err
	}
	semanticDiff := buildSemanticDiff(before, after)
	impact := buildAuthoringImpact(before, after, semanticDiff, sourceDiff)
	hashes := resultingHashes(after)
	next, ok := nextGeneration(base.Generation)
	if !ok {
		return SourcePlan{}, fmt.Errorf("invalid base generation")
	}
	impactDigest := impact.ImpactDigest
	required := make([]AuthoringCapability, len(impact.RequiredCapabilities))
	copy(required, impact.RequiredCapabilities)
	changedModules := make([]ModuleRef, 0, len(changed))
	for _, path := range changed {
		changedModules = append(changedModules, projectModule(path))
	}
	previewSeed := struct {
		Base     Generation      `json:"base_generation"`
		Semantic SemanticDiff    `json:"semantic_diff"`
		Source   SourceDiff      `json:"source_diff"`
		Impact   AuthoringImpact `json:"authoring_impact"`
		Hashes   ResultingHashes `json:"resulting_hashes"`
	}{base.Generation, semanticDiff, sourceDiff, impact, hashes}
	previewDigest := hashJSON(previewSeed)
	previewID := PreviewID{Namespace: base.Generation.Namespace, Value: "preview_" + strings.TrimPrefix(string(previewDigest), "sha256:")}
	preview := WorkbenchPreviewResult{
		Status: "valid", BaseGeneration: base.Generation, ProposedGeneration: &next,
		ChangedSourceFiles: changedModules, SemanticDiff: semanticDiff, SourceDiff: sourceDiff,
		Conflicts: []SemanticConflict{}, Diagnostics: []Diagnostic{},
		AuthoringImpact: &impact, AuthoringImpactDigest: &impactDigest, RequiredAuthoringCapabilities: &required,
		ResultingHashes: &hashes, PreviewDigest: &previewDigest, PreviewID: &previewID,
	}
	return SourcePlan{Preview: preview, Candidate: cloneCompileInput(candidate), Attachments: cloneTree(attachments)}, nil
}

func buildSourceDiff(before, after map[string][]byte, attachments PlannerBlobs) (SourceDiff, []string, error) {
	beforePaths, afterPaths := sortedPaths(before), sortedPaths(after)
	removed, created := map[string]bool{}, map[string]bool{}
	for _, path := range beforePaths {
		if _, ok := after[path]; !ok {
			removed[path] = true
		}
	}
	for _, path := range afterPaths {
		if _, ok := before[path]; !ok {
			created[path] = true
		}
	}
	edits := []SourceEdit{}
	changedSet := map[string]bool{}
	// Equal-byte delete/create pairs are exact semantic-preserving moves.
	for _, oldPath := range beforePaths {
		if !removed[oldPath] {
			continue
		}
		for _, newPath := range afterPaths {
			if created[newPath] && bytes.Equal(before[oldPath], after[newPath]) {
				beforeModule, afterModule := projectModule(oldPath), projectModule(newPath)
				valueDigest := digest(before[oldPath])
				edits = append(edits, SourceEdit{Kind: SourceEditKindMove, BeforeModule: &beforeModule, AfterModule: &afterModule, BeforeDigest: &valueDigest, AfterDigest: &valueDigest})
				delete(removed, oldPath)
				delete(created, newPath)
				changedSet[oldPath], changedSet[newPath] = true, true
				break
			}
		}
	}
	for _, path := range beforePaths {
		if removed[path] {
			module, valueDigest := projectModule(path), digest(before[path])
			edits = append(edits, SourceEdit{Kind: SourceEditKindDelete, BeforeModule: &module, BeforeDigest: &valueDigest})
			changedSet[path] = true
			continue
		}
		newValue, exists := after[path]
		if !exists || bytes.Equal(before[path], newValue) {
			continue
		}
		start, oldEnd, newEnd := minimalChangedRange(before[path], newValue)
		oldBytes, newBytes := before[path][start:oldEnd], newValue[start:newEnd]
		beforeDigest, afterDigest := digest(oldBytes), digest(newBytes)
		rangeValue := SourceRange{Origin: SourceOrigin{Kind: OriginKindProject}, ModulePath: path, StartByte: canonicalUint(start), EndByte: canonicalUint(oldEnd)}
		ref := outputBlob("replace", path, newBytes)
		attachments[ref.BlobID] = bytes.Clone(newBytes)
		edits = append(edits, SourceEdit{Kind: SourceEditKindReplace, BeforeDigest: &beforeDigest, AfterDigest: &afterDigest, SourceRange: &rangeValue, ReplacementBlob: &ref})
		changedSet[path] = true
	}
	for _, path := range afterPaths {
		if !created[path] {
			continue
		}
		module, valueDigest := projectModule(path), digest(after[path])
		ref := outputBlob("create", path, after[path])
		attachments[ref.BlobID] = bytes.Clone(after[path])
		edits = append(edits, SourceEdit{Kind: SourceEditKindCreate, AfterModule: &module, AfterDigest: &valueDigest, ReplacementBlob: &ref})
		changedSet[path] = true
	}
	sort.Slice(edits, func(i, j int) bool { return sourceEditKey(edits[i]) < sourceEditKey(edits[j]) })
	diff := SourceDiff{Edits: edits}
	diff.Digest = hashJSON(edits)
	changed := make([]string, 0, len(changedSet))
	for path := range changedSet {
		changed = append(changed, path)
	}
	sort.Strings(changed)
	return diff, changed, nil
}

func buildSemanticDiff(before, after Snapshot) SemanticDiff {
	type subject struct {
		address string
		kind    SubjectKind
		owner   *StableAddress
		hash    Digest
	}
	beforeMap, afterMap := map[string]subject{}, map[string]subject{}
	ownersBefore, _ := subjectMetadata(before)
	ownersAfter, _ := subjectMetadata(after)
	for _, item := range before.SubjectSemanticHashes {
		beforeMap[item.Address] = subject{item.Address, SubjectKind(item.Kind), ownersBefore[item.Address], Digest(item.Hash)}
	}
	for _, item := range after.SubjectSemanticHashes {
		afterMap[item.Address] = subject{item.Address, SubjectKind(item.Kind), ownersAfter[item.Address], Digest(item.Hash)}
	}
	beforeJSON, afterJSON := normalizedByAddress(before.CanonicalJSON), normalizedByAddress(after.CanonicalJSON)
	entries := []SemanticDiffEntry{}
	deleted, created := []subject{}, []subject{}
	for address, old := range beforeMap {
		current, exists := afterMap[address]
		if !exists {
			deleted = append(deleted, old)
			continue
		}
		if old.hash != current.hash {
			beforeAddress, afterAddress, beforeHash, afterHash := StableAddress(address), StableAddress(address), old.hash, current.hash
			entries = append(entries, SemanticDiffEntry{Kind: SemanticChangeKindUpdated, SubjectKind: current.kind, OwnerAddress: current.owner, BeforeAddress: &beforeAddress, AfterAddress: &afterAddress, BeforeHash: &beforeHash, AfterHash: &afterHash, ChangedFieldPaths: changedPaths(beforeJSON[address], afterJSON[address])})
		}
	}
	for address, current := range afterMap {
		if _, exists := beforeMap[address]; !exists {
			created = append(created, current)
		}
	}
	sort.Slice(deleted, func(i, j int) bool {
		return lessStableAddress(deleted[i].address, deleted[j].address)
	})
	sort.Slice(created, func(i, j int) bool {
		return lessStableAddress(created[i].address, created[j].address)
	})
	usedCreated := map[int]bool{}
	for _, old := range deleted {
		paired := -1
		for index, current := range created {
			if usedCreated[index] || old.kind != current.kind {
				continue
			}
			if sameAddressPointer(old.owner, current.owner) {
				paired = index
				break
			}
			if old.hash == current.hash {
				paired = index
			}
		}
		if paired >= 0 {
			current := created[paired]
			usedCreated[paired] = true
			beforeAddress, afterAddress, beforeHash, afterHash := StableAddress(old.address), StableAddress(current.address), old.hash, current.hash
			kind := SemanticChangeKindRenamed
			if !sameAddressPointer(old.owner, current.owner) {
				kind = SemanticChangeKindMoved
			}
			entries = append(entries, SemanticDiffEntry{Kind: kind, SubjectKind: current.kind, OwnerAddress: current.owner, BeforeAddress: &beforeAddress, AfterAddress: &afterAddress, BeforeHash: &beforeHash, AfterHash: &afterHash, ChangedFieldPaths: changedPaths(beforeJSON[old.address], afterJSON[current.address])})
		} else {
			address, valueHash := StableAddress(old.address), old.hash
			entries = append(entries, SemanticDiffEntry{Kind: SemanticChangeKindDeleted, SubjectKind: old.kind, OwnerAddress: old.owner, BeforeAddress: &address, BeforeHash: &valueHash, ChangedFieldPaths: []AuthoredFieldPath{}})
		}
	}
	for index, current := range created {
		if !usedCreated[index] {
			address, valueHash := StableAddress(current.address), current.hash
			entries = append(entries, SemanticDiffEntry{Kind: SemanticChangeKindCreated, SubjectKind: current.kind, OwnerAddress: current.owner, AfterAddress: &address, AfterHash: &valueHash, ChangedFieldPaths: []AuthoredFieldPath{}})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return lessSemanticEntry(entries[i], entries[j]) })
	diff := SemanticDiff{Entries: entries}
	diff.Digest = hashJSON(entries)
	return diff
}

func buildAuthoringImpact(before, after Snapshot, semanticDiff SemanticDiff, sourceDiff SourceDiff) AuthoringImpact {
	classifications := map[string]AuthoringCapability{}
	for _, item := range before.AuthoringSubjectClassification {
		classifications[item.Address] = AuthoringCapability(item.Capability)
	}
	for _, item := range after.AuthoringSubjectClassification {
		classifications[item.Address] = AuthoringCapability(item.Capability)
	}
	beforeSources, afterSources := sourceSubjects(before), sourceSubjects(after)
	entries := []AuthoringImpactEntry{}
	for _, item := range semanticDiff.Entries {
		address := item.AfterAddress
		if address == nil {
			address = item.BeforeAddress
		}
		capability := classifications[string(*address)]
		action := impactAction(item.Kind)
		impactEntry := AuthoringImpactEntry{Capability: capability, Action: action, SubjectKind: item.SubjectKind, OwnerAddress: item.OwnerAddress, SubjectAddress: address, ChangedFieldPaths: item.ChangedFieldPaths, BeforeRefs: []StableAddress{}, AfterRefs: []StableAddress{}, SourceRefs: []SourceRange{}}
		if item.BeforeAddress != nil {
			impactEntry.BeforeRefs = append(impactEntry.BeforeRefs, *item.BeforeAddress)
			if source := beforeSources[string(*item.BeforeAddress)].DeclarationRange; source != nil {
				impactEntry.SourceRefs = append(impactEntry.SourceRefs, *source)
			}
		}
		if item.AfterAddress != nil {
			impactEntry.AfterRefs = append(impactEntry.AfterRefs, *item.AfterAddress)
			if source := afterSources[string(*item.AfterAddress)].DeclarationRange; source != nil && !containsRange(impactEntry.SourceRefs, *source) {
				impactEntry.SourceRefs = append(impactEntry.SourceRefs, *source)
			}
		}
		sortRanges(impactEntry.SourceRefs)
		if capability == AuthoringCapabilityGraphWrite {
			impactEntry.GraphFacts = &GraphAuthoringFacts{EntityTypeAddresses: []EntityTypeAddress{}, RelationTypeAddresses: []RelationTypeAddress{}, LayerAddresses: []LayerAddress{}, ColumnAddresses: []ColumnAddress{}, EndpointEntityAddresses: []EntityAddress{}, ActionFlags: []string{string(action)}}
		}
		entries = append(entries, impactEntry)
	}
	if len(entries) == 0 && len(sourceDiff.Edits) != 0 {
		root := rootAddress(after)
		refs := sourceEditRanges(sourceDiff.Edits)
		entries = append(entries, AuthoringImpactEntry{Capability: AuthoringCapabilitySourceMaintain, Action: AuthoringActionMaintain, SubjectKind: SubjectKindProject, SubjectAddress: &root, ChangedFieldPaths: []AuthoredFieldPath{}, BeforeRefs: []StableAddress{}, AfterRefs: []StableAddress{}, SourceRefs: refs})
	}
	sort.Slice(entries, func(i, j int) bool { return lessImpactEntry(entries[i], entries[j]) })
	requiredSet := map[AuthoringCapability]bool{}
	for _, entry := range entries {
		requiredSet[entry.Capability] = true
	}
	required := []AuthoringCapability{}
	for _, capability := range capabilityOrder() {
		if requiredSet[capability] {
			required = append(required, capability)
		}
	}
	impact := AuthoringImpact{BaseDefinitionHash: Digest(before.DefinitionHash), ResultingDefinitionHash: Digest(after.DefinitionHash), SemanticDiffHash: semanticDiff.Digest, SourceDiffHash: sourceDiff.Digest, Entries: entries, RequiredCapabilities: required}
	impact.ImpactDigest = hashJSON(struct {
		Base     Digest                 `json:"base_definition_hash"`
		Result   Digest                 `json:"resulting_definition_hash"`
		Semantic Digest                 `json:"semantic_diff_hash"`
		Source   Digest                 `json:"source_diff_hash"`
		Entries  []AuthoringImpactEntry `json:"entries"`
		Required []AuthoringCapability  `json:"required_capabilities"`
	}{impact.BaseDefinitionHash, impact.ResultingDefinitionHash, impact.SemanticDiffHash, impact.SourceDiffHash, entries, required})
	return impact
}

func resultingHashes(snapshot Snapshot) ResultingHashes {
	result := ResultingHashes{Mode: CompileMode(snapshot.Mode), DefinitionHash: Digest(snapshot.DefinitionHash), SubjectHashes: []SubjectHash{}, SubtreeHashes: []SubtreeHash{}, ChildSetHashes: []ChildSetHash{}}
	if snapshot.GraphHash != nil {
		value := Digest(*snapshot.GraphHash)
		result.GraphHash = &value
	}
	if snapshot.Mode == CompileProject {
		value := ProjectRootAddress(snapshot.NormalizedDocument.Project.Address)
		result.ProjectAddress = &value
	} else {
		value := PackRootAddress(snapshot.NormalizedPackArtifact.Pack.Address)
		result.PackAddress = &value
	}
	for _, item := range snapshot.SubjectSemanticHashes {
		result.SubjectHashes = append(result.SubjectHashes, SubjectHash{Address: StableAddress(item.Address), Kind: SubjectKind(item.Kind), Hash: Digest(item.Hash)})
	}
	for _, item := range snapshot.SubtreeHashes {
		result.SubtreeHashes = append(result.SubtreeHashes, SubtreeHash{OwnerAddress: StableAddress(item.OwnerAddress), Hash: Digest(item.Hash)})
	}
	for _, item := range snapshot.ChildSetHashes {
		addresses := make([]StableAddress, len(item.Addresses))
		for i, address := range item.Addresses {
			addresses[i] = StableAddress(address)
		}
		result.ChildSetHashes = append(result.ChildSetHashes, ChildSetHash{OwnerAddress: StableAddress(item.OwnerAddress), ChildKind: SubjectKind(item.ChildKind), ChildAddresses: addresses, Hash: Digest(item.Hash)})
	}
	return result
}

func outputBlob(prefix, path string, value []byte) BlobRef {
	valueDigest := digest(value)
	idSum := sha256.Sum256([]byte(prefix + "\x00" + path + "\x00" + string(valueDigest)))
	return BlobRef{BlobID: "sourceplanner/" + prefix + "/" + hex.EncodeToString(idSum[:]), Digest: valueDigest, Lifetime: BlobLifetimeRequest, MediaType: textMediaType, Size: uint64(len(value))}
}
func hashJSON(value any) Digest {
	encoded, _ := json.Marshal(value)
	return digest(encoded)
}
func sortedPaths(tree map[string][]byte) []string {
	out := make([]string, 0, len(tree))
	for path := range tree {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}
func projectModule(path string) ModuleRef {
	return ModuleRef{Origin: SourceOrigin{Kind: OriginKindProject}, ModulePath: path}
}
func minimalChangedRange(before, after []byte) (int, int, int) {
	start := 0
	for start < len(before) && start < len(after) && before[start] == after[start] {
		start++
	}
	oldEnd, newEnd := len(before), len(after)
	for oldEnd > start && newEnd > start && before[oldEnd-1] == after[newEnd-1] {
		oldEnd--
		newEnd--
	}
	for start > 0 && (!utf8Boundary(before, start) || !utf8Boundary(after, start)) {
		start--
	}
	for oldEnd < len(before) && !utf8Boundary(before, oldEnd) {
		oldEnd++
	}
	for newEnd < len(after) && !utf8Boundary(after, newEnd) {
		newEnd++
	}
	return start, oldEnd, newEnd
}
func sourceEditKey(edit SourceEdit) string {
	module := ""
	if edit.SourceRange != nil {
		module = edit.SourceRange.ModulePath
	}
	if edit.BeforeModule != nil {
		module = edit.BeforeModule.ModulePath
	}
	if edit.AfterModule != nil && module == "" {
		module = edit.AfterModule.ModulePath
	}
	start := 0
	if edit.SourceRange != nil {
		start = edit.SourceRange.StartByte
	}
	return module + "\x00" + string(edit.Kind) + "\x00" + fmt.Sprintf("%020d", start)
}
func nextGeneration(value Generation) (Generation, bool) {
	if value.Value == ^uint64(0) {
		return Generation{}, false
	}
	value.Value++
	return value, true
}

func subjectMetadata(snapshot Snapshot) (map[string]*StableAddress, map[string]SubjectKind) {
	owners, kinds := map[string]*StableAddress{}, map[string]SubjectKind{}
	for _, item := range snapshot.SourceMap.Subjects {
		kinds[item.Address] = SubjectKind(item.Kind)
		if item.OwnerAddress != nil {
			value := StableAddress(*item.OwnerAddress)
			owners[item.Address] = &value
		}
	}
	return owners, kinds
}
func normalizedByAddress(value []byte) map[string]map[string]any {
	var root any
	_ = json.Unmarshal(value, &root)
	out := map[string]map[string]any{}
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if address, ok := typed["address"].(string); ok {
				out[address] = typed
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(root)
	return out
}
func changedPaths(before, after map[string]any) []AuthoredFieldPath {
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	delete(keys, "address")
	delete(keys, "id")
	out := []AuthoredFieldPath{}
	for key := range keys {
		if !jsonEqual(before[key], after[key]) {
			out = append(out, AuthoredFieldPath{Tokens: []string{key}})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tokens[0] < out[j].Tokens[0] })
	return out
}
func jsonEqual(left, right any) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return bytes.Equal(a, b)
}
func sameAddressPointer(left, right *StableAddress) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
func semanticEntryAddress(item SemanticDiffEntry) string {
	address := item.BeforeAddress
	if address == nil {
		address = item.AfterAddress
	}
	return string(*address)
}
func lessSemanticEntry(left, right SemanticDiffEntry) bool {
	leftAddress, rightAddress := semanticEntryAddress(left), semanticEntryAddress(right)
	if leftAddress != rightAddress {
		return lessStableAddress(leftAddress, rightAddress)
	}
	return left.Kind < right.Kind
}
func impactAction(kind SemanticChangeKind) AuthoringAction {
	switch kind {
	case SemanticChangeKindCreated:
		return AuthoringActionCreate
	case SemanticChangeKindDeleted:
		return AuthoringActionDelete
	case SemanticChangeKindRenamed:
		return AuthoringActionRename
	case SemanticChangeKindMoved:
		return AuthoringActionMove
	case SemanticChangeKindReferenceChanged:
		return AuthoringActionBind
	default:
		return AuthoringActionUpdate
	}
}
func containsRange(values []SourceRange, target SourceRange) bool {
	for _, value := range values {
		if fmt.Sprint(value) == fmt.Sprint(target) {
			return true
		}
	}
	return false
}
func sortRanges(values []SourceRange) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].ModulePath != values[j].ModulePath {
			return values[i].ModulePath < values[j].ModulePath
		}
		leftStart, rightStart := values[i].StartByte, values[j].StartByte
		if leftStart != rightStart {
			return leftStart < rightStart
		}
		leftEnd, rightEnd := values[i].EndByte, values[j].EndByte
		return leftEnd < rightEnd
	})
}
func impactEntryAddress(item AuthoringImpactEntry) string {
	address := ""
	if item.SubjectAddress != nil {
		address = string(*item.SubjectAddress)
	} else if item.OwnerAddress != nil {
		address = string(*item.OwnerAddress)
	}
	return address
}
func lessImpactEntry(left, right AuthoringImpactEntry) bool {
	leftAddress, rightAddress := impactEntryAddress(left), impactEntryAddress(right)
	if leftAddress != rightAddress {
		return lessStableAddress(leftAddress, rightAddress)
	}
	if left.Capability != right.Capability {
		return capabilityRank(left.Capability) < capabilityRank(right.Capability)
	}
	return left.Action < right.Action
}
func capabilityOrder() []AuthoringCapability {
	return []AuthoringCapability{AuthoringCapabilityAssetWrite, AuthoringCapabilityGraphWrite, AuthoringCapabilityPackageManage, AuthoringCapabilityProjectConfigure, AuthoringCapabilityQueryWrite, AuthoringCapabilityReferenceWrite, AuthoringCapabilitySchemaWrite, AuthoringCapabilitySourceMaintain, AuthoringCapabilityViewWrite}
}
func capabilityRank(target AuthoringCapability) int {
	for rank, value := range capabilityOrder() {
		if value == target {
			return rank
		}
	}
	return len(capabilityOrder())
}

func lessStableAddress(left, right string) bool {
	leftOrigin, leftComponents, leftPath := stableTuple(left)
	rightOrigin, rightComponents, rightPath := stableTuple(right)
	if leftOrigin != rightOrigin {
		return leftOrigin < rightOrigin
	}
	for index := 0; index < len(leftComponents) && index < len(rightComponents); index++ {
		if leftComponents[index] != rightComponents[index] {
			return leftComponents[index] < rightComponents[index]
		}
	}
	if len(leftComponents) != len(rightComponents) {
		return len(leftComponents) < len(rightComponents)
	}
	if len(leftPath) != len(rightPath) {
		return len(leftPath) < len(rightPath)
	}
	for index := range leftPath {
		leftRank, rightRank := stableKindRank(leftPath[index][0]), stableKindRank(rightPath[index][0])
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if leftPath[index][1] != rightPath[index][1] {
			return leftPath[index][1] < rightPath[index][1]
		}
	}
	return false
}
func stableTuple(value string) (int, []string, [][2]string) {
	parts := strings.Split(value, ":")
	if len(parts) < 3 {
		return 9, []string{value}, nil
	}
	root := 3
	origin, components := 0, []string{parts[2]}
	if parts[1] == "pack" && len(parts) >= 4 {
		origin, root, components = 1, 4, []string{parts[2], parts[3]}
	}
	path := [][2]string{}
	for index := root; index+1 < len(parts); index += 2 {
		path = append(path, [2]string{parts[index], parts[index+1]})
	}
	return origin, components, path
}
func stableKindRank(kind string) int {
	order := []string{"entity-type", "relation-type", "layer", "entity", "relation", "query", "view", "reference", "column", "constraint", "row", "parameter", "table-column", "export"}
	for rank, value := range order {
		if value == kind {
			return rank
		}
	}
	return len(order)
}
func rootAddress(snapshot Snapshot) StableAddress {
	if snapshot.NormalizedDocument != nil {
		return StableAddress(snapshot.NormalizedDocument.Project.Address)
	}
	return StableAddress(snapshot.NormalizedPackArtifact.Pack.Address)
}
func sourceEditRanges(edits []SourceEdit) []SourceRange {
	out := []SourceRange{}
	for _, edit := range edits {
		if edit.SourceRange != nil {
			out = append(out, *edit.SourceRange)
			continue
		}
		module := edit.BeforeModule
		if module == nil {
			module = edit.AfterModule
		}
		if module != nil {
			out = append(out, SourceRange{Origin: module.Origin, ModulePath: module.ModulePath, StartByte: canonicalUint(0), EndByte: canonicalUint(0)})
		}
	}
	sortRanges(out)
	return out
}
