// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"math"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/sourceplanner"
)

type (
	SourcePlannerBlobs           = sourceplanner.PlannerBlobs
	SourcePlannerBlobRef         = sourceplanner.BlobRef
	SourcePlannerDigest          = sourceplanner.Digest
	SourcePlannerPlan            = sourceplanner.SourcePlan
	SourcePlannerPreconditions   = sourceplanner.EngineEditPreconditions
	SourcePlannerSourceRange     = sourceplanner.SourceRange
	SourcePlannerSourceOrigin    = sourceplanner.SourceOrigin
	SourcePlannerModuleRef       = sourceplanner.ModuleRef
	SourcePlannerGeneration      = sourceplanner.Generation
	SourcePlannerPreviewID       = sourceplanner.PreviewID
	SourcePlannerSourceDiff      = sourceplanner.SourceDiff
	SourcePlannerAuthoringImpact = sourceplanner.AuthoringImpact
	SourcePlannerResultingHashes = sourceplanner.ResultingHashes
	SourcePlannerStableAddress   = sourceplanner.StableAddress
	SourcePlannerSubjectKind     = sourceplanner.SubjectKind
	ExpectedSourceDigest         = sourceplanner.ExpectedSourceDigest
	ExpectedHash                 = sourceplanner.ExpectedHash
	ExpectedChildSet             = sourceplanner.ExpectedChildSet
	SourcePatchInput             = sourceplanner.SourcePatchInput
	SourcePatchBatch             = sourceplanner.SourcePatchBatch
	PlacementHint                = sourceplanner.PlacementHint
	FragmentInput                = sourceplanner.FragmentInput
)

type PreviewSourcePatchInput struct {
	Blobs              SourcePlannerBlobs
	DocumentGeneration DocumentGeneration
	Limits             WorkbenchLimits
	Patch              SourcePatchBatch
	Preconditions      SourcePlannerPreconditions
}

type PreviewFragmentInput struct {
	Blobs              SourcePlannerBlobs
	DocumentGeneration DocumentGeneration
	Fragment           FragmentInput
	Limits             WorkbenchLimits
	Preconditions      SourcePlannerPreconditions
}

type FormatScopeInput struct {
	DocumentGeneration DocumentGeneration
	Limits             WorkbenchLimits
	Preconditions      SourcePlannerPreconditions
	ScopeAddresses     []sourceplanner.StableAddress
}

type OrganizeWorkspaceInput struct {
	DocumentGeneration DocumentGeneration
	Limits             WorkbenchLimits
	Preconditions      SourcePlannerPreconditions
	Strategy           string
}

type ApplyToHandleInput struct {
	BaseGeneration DocumentGeneration
	PreviewDigest  SourcePlannerDigest
	PreviewID      SourcePlannerPreviewID
}

type ApplyToHandleResult struct {
	AuthoringImpact    SourcePlannerAuthoringImpact
	DocumentGeneration DocumentGeneration
	PreviewDigest      SourcePlannerDigest
	ResultingHashes    SourcePlannerResultingHashes
	SourceDiff         SourcePlannerSourceDiff
}

// PreviewSourcePatch runs guarded source patches against one retained Working
// Document generation. It never mutates the handle; callers must explicitly
// apply the returned candidate through ReplaceSourceTree.
func (e Engine) PreviewSourcePatch(ctx context.Context, input PreviewSourcePatchInput) (SourcePlannerPlan, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return SourcePlannerPlan{}, err
	}
	if !snapshot.capabilities.PreviewSourcePatch {
		return SourcePlannerPlan{}, operationDisabled("preview_source_patch")
	}
	request := sourceplanner.PreviewSourcePatchInput{
		Limits:        mapSourcePlannerLimits(input.Limits),
		Preconditions: bindSourcePlannerGeneration(input.Preconditions, document),
		Patch:         input.Patch,
	}
	plan, err := e.sourcePlanner().PreviewSourcePatch(ctx, sourcePlanningBase(document, snapshot), request, clonePlannerBlobs(input.Blobs))
	if err != nil {
		return SourcePlannerPlan{}, err
	}
	if err := e.retainPreview(ctx, document, plan); err != nil {
		return SourcePlannerPlan{}, err
	}
	return plan, nil
}

// PreviewFragment runs a scoped LDL fragment edit against one retained Working
// Document generation without publishing the candidate.
func (e Engine) PreviewFragment(ctx context.Context, input PreviewFragmentInput) (SourcePlannerPlan, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return SourcePlannerPlan{}, err
	}
	if !snapshot.capabilities.PreviewFragment {
		return SourcePlannerPlan{}, operationDisabled("preview_fragment")
	}
	request := sourceplanner.PreviewFragmentInput{
		Limits:        mapSourcePlannerLimits(input.Limits),
		Preconditions: bindSourcePlannerGeneration(input.Preconditions, document),
		Fragment:      input.Fragment,
	}
	plan, err := e.sourcePlanner().PreviewFragment(ctx, sourcePlanningBase(document, snapshot), request, clonePlannerBlobs(input.Blobs))
	if err != nil {
		return SourcePlannerPlan{}, err
	}
	if err := e.retainPreview(ctx, document, plan); err != nil {
		return SourcePlannerPlan{}, err
	}
	return plan, nil
}

// FormatScope plans a complete-scope, comment-preserving source formatting
// preview for a retained Working Document generation.
func (e Engine) FormatScope(ctx context.Context, input FormatScopeInput) (SourcePlannerPlan, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return SourcePlannerPlan{}, err
	}
	if !snapshot.capabilities.FormatScope {
		return SourcePlannerPlan{}, operationDisabled("format_scope")
	}
	request := sourceplanner.FormatScopeInput{
		Limits:         mapSourcePlannerLimits(input.Limits),
		Preconditions:  bindSourcePlannerGeneration(input.Preconditions, document),
		ScopeAddresses: append([]sourceplanner.StableAddress(nil), input.ScopeAddresses...),
	}
	plan, err := e.sourcePlanner().FormatScope(ctx, sourcePlanningBase(document, snapshot), request)
	if err != nil {
		return SourcePlannerPlan{}, err
	}
	if err := e.retainPreview(ctx, document, plan); err != nil {
		return SourcePlannerPlan{}, err
	}
	return plan, nil
}

// OrganizeWorkspace plans the standard LDL source layout in memory. It returns
// move/edit previews only and performs no filesystem writes.
func (e Engine) OrganizeWorkspace(ctx context.Context, input OrganizeWorkspaceInput) (SourcePlannerPlan, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return SourcePlannerPlan{}, err
	}
	if !snapshot.capabilities.OrganizeWorkspace {
		return SourcePlannerPlan{}, operationDisabled("organize_workspace")
	}
	request := sourceplanner.OrganizeWorkspaceInput{
		Limits:        mapSourcePlannerLimits(input.Limits),
		Preconditions: bindSourcePlannerGeneration(input.Preconditions, document),
		Strategy:      input.Strategy,
	}
	plan, err := e.sourcePlanner().OrganizeWorkspace(ctx, sourcePlanningBase(document, snapshot), request)
	if err != nil {
		return SourcePlannerPlan{}, err
	}
	if err := e.retainPreview(ctx, document, plan); err != nil {
		return SourcePlannerPlan{}, err
	}
	return plan, nil
}

// ApplyToHandle commits the last matching valid preview retained for one
// Working Document generation. The preview digest, preview ID, and base
// generation are the commit token; stale handles and superseded previews cannot
// mutate the store.
func (e Engine) ApplyToHandle(ctx context.Context, input ApplyToHandleInput) (ApplyToHandleResult, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.BaseGeneration)
	if err != nil {
		return ApplyToHandleResult{}, err
	}
	if !snapshot.capabilities.ApplyToHandle {
		return ApplyToHandleResult{}, operationDisabled("apply_to_handle")
	}
	if input.PreviewDigest == "" || input.PreviewID.Namespace != input.BaseGeneration.DocumentHandle.EndpointInstanceID || input.PreviewID.Value == "" {
		return ApplyToHandleResult{}, &WorkbenchError{Code: "engine.workbench.preview_invalid", Category: WorkbenchErrorInputInvalid}
	}

	store := e.workbench
	store.mu.RLock()
	retained, err := matchingRetainedPreviewLocked(document, input)
	if err != nil {
		store.mu.RUnlock()
		return ApplyToHandleResult{}, err
	}
	candidate := cloneWorkbenchCompileInput(retained.candidate)
	sourceDiff := retained.sourceDiff
	authoringImpact := retained.authoringImpact
	resultingHashes := retained.resultingHashes
	previewDigest := retained.previewDigest
	store.mu.RUnlock()

	replacement, err := e.compileWorkingSnapshot(ctx, candidate)
	if err != nil {
		return ApplyToHandleResult{}, err
	}
	if replacement.retained > store.config.MaxRetainedBytes {
		return ApplyToHandleResult{}, workbenchLimit("snapshot_bytes", store.config.MaxRetainedBytes, replacement.retained)
	}
	if err := checkWorkbenchContext(ctx); err != nil {
		return ApplyToHandleResult{}, err
	}

	store.mu.Lock()
	current, exists := store.documents[document.handle.Value]
	if !exists || current != document {
		store.mu.Unlock()
		return ApplyToHandleResult{}, invalidHandle()
	}
	if current.generation != input.BaseGeneration.Value {
		store.mu.Unlock()
		return ApplyToHandleResult{}, staleGeneration()
	}
	if _, err := matchingRetainedPreviewLocked(current, input); err != nil {
		store.mu.Unlock()
		return ApplyToHandleResult{}, err
	}
	if current.generation == math.MaxUint64 {
		store.mu.Unlock()
		return ApplyToHandleResult{}, &WorkbenchError{Code: "engine.workbench.generation_overflow", Category: WorkbenchErrorInvariant}
	}
	if err := checkWorkbenchContext(ctx); err != nil {
		store.mu.Unlock()
		return ApplyToHandleResult{}, err
	}
	store.retainedBytes -= current.retainedBytes()
	store.evictToMakeRoomLocked(current.handle.Value, replacement.retained, false)
	current.snapshot = replacement
	current.preview = nil
	current.generation++
	store.retainedBytes += replacement.retained
	newGeneration := current.generation
	store.mu.Unlock()

	return ApplyToHandleResult{
		AuthoringImpact:    authoringImpact,
		DocumentGeneration: DocumentGeneration{DocumentHandle: document.handle, Value: newGeneration},
		PreviewDigest:      SourcePlannerDigest(previewDigest),
		ResultingHashes:    resultingHashes,
		SourceDiff:         sourceDiff,
	}, nil
}

func (e Engine) retainPreview(ctx context.Context, document *workingDocument, plan SourcePlannerPlan) error {
	if err := checkWorkbenchContext(ctx); err != nil {
		return err
	}
	preview := plan.Preview
	if preview.Status != "valid" || preview.PreviewID == nil || preview.PreviewDigest == nil || preview.AuthoringImpact == nil || preview.ResultingHashes == nil || preview.ProposedGeneration == nil {
		return nil
	}
	if preview.BaseGeneration.Namespace != document.handle.EndpointInstanceID || preview.BaseGeneration.DocumentID != document.handle.Value || preview.BaseGeneration.Value != document.generation {
		return &WorkbenchError{Code: "engine.workbench.preview_generation_mismatch", Category: WorkbenchErrorInputInvalid}
	}
	retained := &retainedPreview{
		baseGeneration:  document.generation,
		previewID:       *preview.PreviewID,
		previewDigest:   *preview.PreviewDigest,
		candidate:       mapEngineCompileInput(plan.Candidate),
		sourceDiff:      preview.SourceDiff,
		authoringImpact: *preview.AuthoringImpact,
		resultingHashes: *preview.ResultingHashes,
	}
	retained.retained = retainedPreviewBytes(retained)

	store := e.workbench
	store.mu.Lock()
	current, exists := store.documents[document.handle.Value]
	if !exists || current != document {
		store.mu.Unlock()
		return invalidHandle()
	}
	if current.generation != preview.BaseGeneration.Value {
		store.mu.Unlock()
		return staleGeneration()
	}
	if saturatingAdd(current.snapshot.retained, retained.retained) > store.config.MaxRetainedBytes {
		store.mu.Unlock()
		return workbenchLimit("retained_preview_bytes", store.config.MaxRetainedBytes, saturatingAdd(current.snapshot.retained, retained.retained))
	}
	old := current.preview
	if old != nil {
		store.retainedBytes -= old.retained
	}
	store.evictToMakeRoomLocked(current.handle.Value, retained.retained, false)
	if store.retainedBytes > store.config.MaxRetainedBytes-retained.retained {
		if old != nil {
			store.retainedBytes += old.retained
		}
		store.mu.Unlock()
		return workbenchLimit("retained_preview_bytes", store.config.MaxRetainedBytes, saturatingAdd(store.retainedBytes, retained.retained))
	}
	current.preview = retained
	store.retainedBytes += retained.retained
	store.mu.Unlock()
	return nil
}

func matchingRetainedPreviewLocked(document *workingDocument, input ApplyToHandleInput) (*retainedPreview, error) {
	if document == nil || document.preview == nil {
		return nil, &WorkbenchError{Code: "engine.workbench.preview_not_found", Category: WorkbenchErrorNotFound}
	}
	preview := document.preview
	if preview.baseGeneration != input.BaseGeneration.Value {
		return nil, staleGeneration()
	}
	if preview.previewDigest != sourceplanner.Digest(input.PreviewDigest) || preview.previewID != input.PreviewID {
		return nil, &WorkbenchError{Code: "engine.workbench.preview_mismatch", Category: WorkbenchErrorInputInvalid}
	}
	return preview, nil
}

func retainedPreviewBytes(preview *retainedPreview) int64 {
	if preview == nil {
		return 0
	}
	total := retainedOwnedBytes(preview.baseGeneration)
	total = saturatingAdd(total, retainedOwnedBytes(preview.previewID))
	total = saturatingAdd(total, retainedOwnedBytes(preview.previewDigest))
	total = saturatingAdd(total, retainedOwnedBytes(preview.candidate))
	total = saturatingAdd(total, retainedOwnedBytes(preview.sourceDiff))
	total = saturatingAdd(total, retainedOwnedBytes(preview.authoringImpact))
	total = saturatingAdd(total, retainedOwnedBytes(preview.resultingHashes))
	return total
}

func (e Engine) sourcePlanner() sourceplanner.SourcePlanner {
	return sourceplanner.NewSourcePlanner(sourcePlannerCompiler{engine: e})
}

func sourcePlanningBase(document *workingDocument, snapshot *workingSnapshot) sourceplanner.SourcePlanningBase {
	return sourceplanner.SourcePlanningBase{
		Input:      mapSourcePlannerCompileInput(snapshot.input),
		Generation: sourcePlannerGeneration(document),
	}
}

func bindSourcePlannerGeneration(preconditions SourcePlannerPreconditions, document *workingDocument) SourcePlannerPreconditions {
	preconditions.Generation = sourcePlannerGeneration(document)
	return preconditions
}

func sourcePlannerGeneration(document *workingDocument) sourceplanner.Generation {
	return sourceplanner.Generation{
		Namespace:  document.handle.EndpointInstanceID,
		DocumentID: document.handle.Value,
		Value:      document.generation,
	}
}

func mapSourcePlannerLimits(limits WorkbenchLimits) sourceplanner.WorkbenchLimits {
	return sourceplanner.WorkbenchLimits{
		MaxItems:       uint64(limits.MaxItems),
		MaxOutputBytes: uint64(limits.MaxOutputBytes),
	}
}

func clonePlannerBlobs(blobs SourcePlannerBlobs) SourcePlannerBlobs {
	out := make(SourcePlannerBlobs, len(blobs))
	for id, value := range blobs {
		out[id] = bytes.Clone(value)
	}
	return out
}

type sourcePlannerCompiler struct{ engine Engine }

func (c sourcePlannerCompiler) Compile(ctx context.Context, input sourceplanner.CompileInput) (sourceplanner.CompileResult, error) {
	result, err := c.engine.Compile(ctx, mapEngineCompileInput(input))
	if err != nil {
		return sourceplanner.CompileResult{}, err
	}
	snapshot := result.Snapshot()
	output := mapSourcePlannerSnapshot(snapshot)
	return sourceplanner.CompileResult{
		Output:         output,
		Diagnostics:    output.Diagnostics,
		DefinitionHash: output.DefinitionHash,
	}, nil
}

func mapSourcePlannerCompileInput(input CompileInput) sourceplanner.CompileInput {
	assets := make([]sourceplanner.AssetInput, len(input.ReferencedAssets))
	for index, asset := range input.ReferencedAssets {
		assets[index] = sourceplanner.AssetInput{
			Origin: string(asset.Origin), PackID: asset.PackID, Locator: asset.Locator,
			Bytes: bytes.Clone(asset.Bytes), Digest: asset.Digest, MediaType: asset.MediaType, ByteLength: asset.ByteLength,
		}
	}
	installs := make([]sourceplanner.ResolvedPack, len(input.ResolvedDependencies.Installs))
	for index, install := range input.ResolvedDependencies.Installs {
		installs[index] = mapSourcePlannerResolvedPack(install)
	}
	return sourceplanner.CompileInput{
		Mode: CompileModeToSourcePlanner(input.Mode), EntryPath: input.EntryPath, RootPackID: input.RootPackID,
		ProjectSourceTree: clonePlannerTree(input.ProjectSourceTree), InstalledPackTree: clonePlannerTree(input.InstalledPackTree),
		ResolvedDependencies: sourceplanner.ResolvedDependencies{
			Format: input.ResolvedDependencies.Format, FormatVersion: input.ResolvedDependencies.FormatVersion,
			Language: input.ResolvedDependencies.Language, Installs: installs,
		},
		ReferencedAssets: assets,
		ResourceLimits: sourceplanner.ResourceLimits{
			MaxProjectSourceFiles: input.ResourceLimits.MaxProjectSourceFiles,
			MaxProjectSourceBytes: input.ResourceLimits.MaxProjectSourceBytes,
			MaxPackFiles:          input.ResourceLimits.MaxPackFiles,
			MaxPackBytes:          input.ResourceLimits.MaxPackBytes,
			MaxAssets:             input.ResourceLimits.MaxAssets,
			MaxAssetBytes:         input.ResourceLimits.MaxAssetBytes,
			MaxRasterDimension:    input.ResourceLimits.MaxRasterDimension,
			MaxRasterPixels:       input.ResourceLimits.MaxRasterPixels,
			MaxDeclarations:       input.ResourceLimits.MaxDeclarations,
		},
	}
}

func mapEngineCompileInput(input sourceplanner.CompileInput) CompileInput {
	assets := make([]AssetInput, len(input.ReferencedAssets))
	for index, asset := range input.ReferencedAssets {
		assets[index] = AssetInput{
			Origin: SourceOriginKind(asset.Origin), PackID: asset.PackID, Locator: asset.Locator,
			Bytes: bytes.Clone(asset.Bytes), Digest: asset.Digest, MediaType: asset.MediaType, ByteLength: asset.ByteLength,
		}
	}
	installs := make([]ResolvedPack, len(input.ResolvedDependencies.Installs))
	for index, install := range input.ResolvedDependencies.Installs {
		installs[index] = mapEngineResolvedPack(install)
	}
	return CompileInput{
		Mode: CompileModeFromSourcePlanner(input.Mode), EntryPath: input.EntryPath, RootPackID: input.RootPackID,
		ProjectSourceTree: clonePlannerTree(input.ProjectSourceTree), InstalledPackTree: clonePlannerTree(input.InstalledPackTree),
		ResolvedDependencies: ResolvedDependencies{
			Format: input.ResolvedDependencies.Format, FormatVersion: input.ResolvedDependencies.FormatVersion,
			Language: input.ResolvedDependencies.Language, Installs: installs,
		},
		ReferencedAssets: assets,
		ResourceLimits: ResourceLimits{
			MaxProjectSourceFiles: input.ResourceLimits.MaxProjectSourceFiles,
			MaxProjectSourceBytes: input.ResourceLimits.MaxProjectSourceBytes,
			MaxPackFiles:          input.ResourceLimits.MaxPackFiles,
			MaxPackBytes:          input.ResourceLimits.MaxPackBytes,
			MaxAssets:             input.ResourceLimits.MaxAssets,
			MaxAssetBytes:         input.ResourceLimits.MaxAssetBytes,
			MaxRasterDimension:    input.ResourceLimits.MaxRasterDimension,
			MaxRasterPixels:       input.ResourceLimits.MaxRasterPixels,
			MaxDeclarations:       input.ResourceLimits.MaxDeclarations,
		},
	}
}

func mapSourcePlannerResolvedPack(install ResolvedPack) sourceplanner.ResolvedPack {
	files := make([]sourceplanner.ResolvedPackFile, len(install.Files))
	for index, file := range install.Files {
		files[index] = sourceplanner.ResolvedPackFile{Path: file.Path, Digest: file.Digest}
	}
	dependencies := make([]sourceplanner.ResolvedPackDependency, len(install.Dependencies))
	for index, dependency := range install.Dependencies {
		dependencies[index] = sourceplanner.ResolvedPackDependency{LocalName: dependency.LocalName, InstallName: dependency.InstallName}
	}
	return sourceplanner.ResolvedPack{
		InstallName: install.InstallName, CanonicalID: install.CanonicalID, Version: install.Version,
		Digest: install.Digest, Path: install.Path, Entry: install.Entry, Files: files,
		Dependencies: dependencies, ManifestPath: install.ManifestPath, Manifest: bytes.Clone(install.Manifest),
	}
}

func mapEngineResolvedPack(install sourceplanner.ResolvedPack) ResolvedPack {
	files := make([]ResolvedPackFile, len(install.Files))
	for index, file := range install.Files {
		files[index] = ResolvedPackFile{Path: file.Path, Digest: file.Digest}
	}
	dependencies := make([]ResolvedPackDependency, len(install.Dependencies))
	for index, dependency := range install.Dependencies {
		dependencies[index] = ResolvedPackDependency{LocalName: dependency.LocalName, InstallName: dependency.InstallName}
	}
	return ResolvedPack{
		InstallName: install.InstallName, CanonicalID: install.CanonicalID, Version: install.Version,
		Digest: install.Digest, Path: install.Path, Entry: install.Entry, Files: files,
		Dependencies: dependencies, ManifestPath: install.ManifestPath, Manifest: bytes.Clone(install.Manifest),
	}
}

func clonePlannerTree(tree map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(tree))
	for path, value := range tree {
		out[path] = bytes.Clone(value)
	}
	return out
}

func CompileModeToSourcePlanner(mode CompileMode) sourceplanner.CompileMode {
	return sourceplanner.CompileMode(mode)
}

func CompileModeFromSourcePlanner(mode sourceplanner.CompileMode) CompileMode {
	return CompileMode(mode)
}

func mapSourcePlannerSnapshot(snapshot Snapshot) sourceplanner.Snapshot {
	classifications := make([]sourceplanner.AuthoringSubjectClassification, len(snapshot.AuthoringSubjectClassification))
	for index, item := range snapshot.AuthoringSubjectClassification {
		classifications[index] = sourceplanner.AuthoringSubjectClassification{Address: item.Address, Kind: item.Kind, Capability: sourceplanner.AuthoringCapability(item.Capability)}
	}
	return sourceplanner.Snapshot{
		Mode:               CompileModeToSourcePlanner(snapshot.Mode),
		TypedAST:           sourceplanner.TypedAST{Graph: snapshot.TypedAST.Graph},
		NormalizedDocument: snapshot.NormalizedDocument, NormalizedPackArtifact: snapshot.NormalizedPackArtifact,
		CanonicalJSON: bytes.Clone(snapshot.CanonicalJSON), SourceMap: snapshot.SourceMap,
		StableAddresses: append([]string(nil), snapshot.StableAddresses...), DefinitionHash: snapshot.DefinitionHash, GraphHash: cloneOptionalStringPointer(snapshot.GraphHash),
		SubjectSemanticHashes:          append([]SubjectHash(nil), snapshot.SubjectSemanticHashes...),
		SubtreeHashes:                  append([]SubtreeHash(nil), snapshot.SubtreeHashes...),
		ChildSetHashes:                 append([]ChildSetHash(nil), snapshot.ChildSetHashes...),
		AuthoringSubjectClassification: classifications,
		Diagnostics:                    mapSourcePlannerDiagnostics(snapshot.Diagnostics),
	}
}

func mapSourcePlannerDiagnostics(values []Diagnostic) []sourceplanner.CompileDiagnostic {
	out := make([]sourceplanner.CompileDiagnostic, len(values))
	for index, value := range values {
		out[index] = sourceplanner.CompileDiagnostic{
			Code: value.Code, Severity: string(value.Severity), MessageKey: value.MessageKey, Message: value.Message,
			Range: value.Range, SubjectAddress: value.SubjectAddress, OwnerAddress: value.OwnerAddress, Related: value.Related,
		}
	}
	return out
}

func cloneOptionalStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneWorkbenchCompileInput(input CompileInput) CompileInput {
	out := input
	out.ProjectSourceTree = clonePlannerTree(input.ProjectSourceTree)
	out.InstalledPackTree = clonePlannerTree(input.InstalledPackTree)
	out.ReferencedAssets = append([]AssetInput(nil), input.ReferencedAssets...)
	for index := range out.ReferencedAssets {
		out.ReferencedAssets[index].Bytes = bytes.Clone(out.ReferencedAssets[index].Bytes)
	}
	out.ResolvedDependencies.Installs = append([]ResolvedPack(nil), input.ResolvedDependencies.Installs...)
	for index := range out.ResolvedDependencies.Installs {
		install := &out.ResolvedDependencies.Installs[index]
		install.Files = append([]ResolvedPackFile(nil), install.Files...)
		install.Dependencies = append([]ResolvedPackDependency(nil), install.Dependencies...)
		install.Manifest = bytes.Clone(install.Manifest)
	}
	return out
}
