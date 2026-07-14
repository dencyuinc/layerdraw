// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

func mapCompileSnapshot(snapshot engine.Snapshot) (engineprotocol.CompileResult, []OutputBlob, error) {
	return mapCompileSnapshotWithBudget(snapshot, newCompileMappingBudget(math.MaxInt64))
}

func mapCompileSnapshotWithBudget(snapshot engine.Snapshot, budget *compileMappingBudget) (engineprotocol.CompileResult, []OutputBlob, error) {
	if snapshot.Mode != engine.CompileProject && snapshot.Mode != engine.CompilePack {
		return engineprotocol.CompileResult{}, nil, fmt.Errorf("invalid successful compile mode")
	}
	limits, err := mapEffectiveLimits(snapshot.EffectiveLimits)
	if err != nil {
		return engineprotocol.CompileResult{}, nil, err
	}
	sourceMap, err := mapSourceMapWithBudget(snapshot.SourceMap, budget)
	if err != nil {
		return engineprotocol.CompileResult{}, nil, err
	}
	semanticIndex, err := mapSemanticIndexWithBudget(snapshot.SemanticIndex, budget)
	if err != nil {
		return engineprotocol.CompileResult{}, nil, err
	}
	searchDocuments, err := mapSearchDocumentsWithBudget(snapshot.SearchDocuments, budget)
	if err != nil {
		return engineprotocol.CompileResult{}, nil, err
	}
	normalized, artifactBlobs, err := mapNormalizedArtifact(snapshot, searchDocuments)
	if err != nil {
		return engineprotocol.CompileResult{}, nil, err
	}
	normalizedAccounting := normalized
	normalizedAccounting.SearchDocuments = []semantic.SearchDocument{}
	if err := budget.claim(normalizedAccounting); err != nil {
		return engineprotocol.CompileResult{}, nil, err
	}
	compiledRecipes, recipeBlobs, err := mapCompiledRecipesWithBudget(snapshot, budget)
	if err != nil {
		return engineprotocol.CompileResult{}, nil, err
	}

	result := engineprotocol.CompileResult{
		AuthoringSubjectClassification: make([]semantic.AuthoringSubjectClassification, 0, min(len(snapshot.AuthoringSubjectClassification), 256)),
		ChildSetHashes:                 make([]semantic.ChildSetHash, 0, min(len(snapshot.ChildSetHashes), 256)),
		CompiledRecipes:                compiledRecipes,
		DefinitionHash:                 protocolcommon.Digest(snapshot.DefinitionHash),
		EffectiveLimits:                limits,
		NormalizedArtifact:             normalized,
		SemanticIndex:                  semanticIndex,
		SourceMap:                      sourceMap,
		StableAddresses:                make([]semantic.StableAddress, 0, min(len(snapshot.StableAddresses), 256)),
		SubjectSemanticHashes:          make([]semantic.SubjectHash, 0, min(len(snapshot.SubjectSemanticHashes), 256)),
		SubtreeHashes:                  make([]semantic.SubtreeHash, 0, min(len(snapshot.SubtreeHashes), 256)),
	}
	for _, item := range snapshot.AuthoringSubjectClassification {
		mapped := semantic.AuthoringSubjectClassification{
			Address:    semantic.StableAddress(item.Address),
			Capability: semantic.AuthoringCapability(item.Capability),
			Kind:       semantic.SubjectKind(item.Kind),
		}
		if err := budget.claim(mapped); err != nil {
			return engineprotocol.CompileResult{}, nil, err
		}
		result.AuthoringSubjectClassification = append(result.AuthoringSubjectClassification, mapped)
	}
	for _, item := range snapshot.StableAddresses {
		mapped := semantic.StableAddress(item)
		if err := budget.claim(mapped); err != nil {
			return engineprotocol.CompileResult{}, nil, err
		}
		result.StableAddresses = append(result.StableAddresses, mapped)
	}
	for _, item := range snapshot.SubjectSemanticHashes {
		mapped := semantic.SubjectHash{Address: semantic.StableAddress(item.Address), Hash: protocolcommon.Digest(item.Hash), Kind: semantic.SubjectKind(item.Kind)}
		if err := budget.claim(mapped); err != nil {
			return engineprotocol.CompileResult{}, nil, err
		}
		result.SubjectSemanticHashes = append(result.SubjectSemanticHashes, mapped)
	}
	for _, item := range snapshot.SubtreeHashes {
		mapped := semantic.SubtreeHash{OwnerAddress: semantic.StableAddress(item.OwnerAddress), Hash: protocolcommon.Digest(item.Hash)}
		if err := budget.claim(mapped); err != nil {
			return engineprotocol.CompileResult{}, nil, err
		}
		result.SubtreeHashes = append(result.SubtreeHashes, mapped)
	}
	for _, item := range snapshot.ChildSetHashes {
		if err := ensureStableAddressArraysWithinWire(budget.maximum, item.Addresses); err != nil {
			return engineprotocol.CompileResult{}, nil, err
		}
		mapped := semantic.ChildSetHash{
			OwnerAddress:   semantic.StableAddress(item.OwnerAddress),
			ChildKind:      semantic.SubjectKind(item.ChildKind),
			ChildAddresses: stableAddresses(item.Addresses),
			Hash:           protocolcommon.Digest(item.Hash),
		}
		if err := budget.claim(mapped); err != nil {
			return engineprotocol.CompileResult{}, nil, err
		}
		result.ChildSetHashes = append(result.ChildSetHashes, mapped)
	}
	blobs := append(artifactBlobs, recipeBlobs...)
	if err := validateUniqueOutputBlobs(blobs); err != nil {
		return engineprotocol.CompileResult{}, nil, err
	}
	return result, blobs, nil
}

func mapEffectiveLimits(limits engine.ResourceLimits) (engineprotocol.EffectiveResourceLimits, error) {
	values := []int64{
		limits.MaxAssetBytes, limits.MaxAssets, limits.MaxDeclarations,
		limits.MaxPackBytes, limits.MaxPackFiles, limits.MaxProjectSourceBytes,
		limits.MaxProjectSourceFiles, limits.MaxRasterDimension, limits.MaxRasterPixels,
	}
	for _, value := range values {
		if value <= 0 {
			return engineprotocol.EffectiveResourceLimits{}, fmt.Errorf("non-positive effective limit")
		}
	}
	positive := func(value int64) protocolcommon.CanonicalPositiveInt64 {
		return protocolcommon.CanonicalPositiveInt64(strconv.FormatInt(value, 10))
	}
	return engineprotocol.EffectiveResourceLimits{
		MaxAssetBytes: positive(limits.MaxAssetBytes), MaxAssets: positive(limits.MaxAssets), MaxDeclarations: positive(limits.MaxDeclarations),
		MaxPackBytes: positive(limits.MaxPackBytes), MaxPackFiles: positive(limits.MaxPackFiles),
		MaxProjectSourceBytes: positive(limits.MaxProjectSourceBytes), MaxProjectSourceFiles: positive(limits.MaxProjectSourceFiles),
		MaxRasterDimension: positive(limits.MaxRasterDimension), MaxRasterPixels: positive(limits.MaxRasterPixels),
	}, nil
}

func mapNormalizedArtifact(snapshot engine.Snapshot, searchDocuments []semantic.SearchDocument) (engineprotocol.NormalizedArtifact, []OutputBlob, error) {
	canonical := newOutputBlob("engine.compile/output/normalized/canonical", snapshot.CanonicalJSON)
	artifact := newOutputBlob("engine.compile/output/normalized/artifact", snapshot.ArtifactJSON)
	result := engineprotocol.NormalizedArtifact{Kind: engineprotocol.CompileMode(snapshot.Mode), SearchDocuments: searchDocuments}
	switch snapshot.Mode {
	case engine.CompileProject:
		if snapshot.NormalizedDocument == nil || snapshot.NormalizedPackArtifact != nil || snapshot.GraphHash == nil {
			return engineprotocol.NormalizedArtifact{}, nil, fmt.Errorf("invalid Project success union")
		}
		canonical.Ref.MediaType = string(engineprotocol.NormalizedProjectCanonicalBlobRefMediaTypeValue)
		artifact.Ref.MediaType = string(engineprotocol.NormalizedProjectArtifactBlobRefMediaTypeValue)
		result.GraphHash = digestPointer(*snapshot.GraphHash)
		result.Project = &engineprotocol.NormalizedProjectArtifact{
			ProjectAddress: semantic.StableAddress(snapshot.NormalizedDocument.Project.Address),
			CanonicalJSON:  projectCanonicalRef(canonical.Ref),
			ArtifactJSON:   projectArtifactRef(artifact.Ref),
		}
	case engine.CompilePack:
		if snapshot.NormalizedPackArtifact == nil || snapshot.NormalizedDocument != nil || snapshot.GraphHash != nil || len(searchDocuments) != 0 {
			return engineprotocol.NormalizedArtifact{}, nil, fmt.Errorf("invalid Pack success union")
		}
		canonical.Ref.MediaType = string(engineprotocol.NormalizedPackCanonicalBlobRefMediaTypeValue)
		artifact.Ref.MediaType = string(engineprotocol.NormalizedPackArtifactBlobRefMediaTypeValue)
		result.Pack = &engineprotocol.NormalizedPackArtifact{
			PackAddress:   semantic.StableAddress(snapshot.NormalizedPackArtifact.Pack.Address),
			CanonicalJSON: packCanonicalRef(canonical.Ref),
			ArtifactJSON:  packArtifactRef(artifact.Ref),
		}
	}
	return result, []OutputBlob{canonical, artifact}, nil
}

func newOutputBlob(id string, value []byte) OutputBlob {
	digest := sha256.Sum256(value)
	return OutputBlob{
		Ref: protocolcommon.BlobRef{
			BlobID:   id,
			Digest:   protocolcommon.Digest("sha256:" + hex.EncodeToString(digest[:])),
			Lifetime: protocolcommon.BlobLifetimeRequest,
			Size:     protocolcommon.CanonicalUint64(strconv.FormatUint(uint64(len(value)), 10)),
		},
		Bytes: append([]byte(nil), value...),
	}
}

func projectCanonicalRef(ref protocolcommon.BlobRef) engineprotocol.NormalizedProjectCanonicalBlobRef {
	return engineprotocol.NormalizedProjectCanonicalBlobRef{BlobID: ref.BlobID, Digest: ref.Digest, Lifetime: ref.Lifetime, MediaType: engineprotocol.NormalizedProjectCanonicalBlobRefMediaType(ref.MediaType), Size: ref.Size}
}

func projectArtifactRef(ref protocolcommon.BlobRef) engineprotocol.NormalizedProjectArtifactBlobRef {
	return engineprotocol.NormalizedProjectArtifactBlobRef{BlobID: ref.BlobID, Digest: ref.Digest, Lifetime: ref.Lifetime, MediaType: engineprotocol.NormalizedProjectArtifactBlobRefMediaType(ref.MediaType), Size: ref.Size}
}

func packCanonicalRef(ref protocolcommon.BlobRef) engineprotocol.NormalizedPackCanonicalBlobRef {
	return engineprotocol.NormalizedPackCanonicalBlobRef{BlobID: ref.BlobID, Digest: ref.Digest, Lifetime: ref.Lifetime, MediaType: engineprotocol.NormalizedPackCanonicalBlobRefMediaType(ref.MediaType), Size: ref.Size}
}

func packArtifactRef(ref protocolcommon.BlobRef) engineprotocol.NormalizedPackArtifactBlobRef {
	return engineprotocol.NormalizedPackArtifactBlobRef{BlobID: ref.BlobID, Digest: ref.Digest, Lifetime: ref.Lifetime, MediaType: engineprotocol.NormalizedPackArtifactBlobRefMediaType(ref.MediaType), Size: ref.Size}
}

func validateUniqueOutputBlobs(blobs []OutputBlob) error {
	seen := make(map[string]bool, len(blobs))
	for _, blob := range blobs {
		if seen[blob.Ref.BlobID] {
			return fmt.Errorf("duplicate output blob ID")
		}
		seen[blob.Ref.BlobID] = true
		digest := sha256.Sum256(blob.Bytes)
		if string(blob.Ref.Digest) != "sha256:"+hex.EncodeToString(digest[:]) || blob.Ref.Size != protocolcommon.CanonicalUint64(strconv.FormatUint(uint64(len(blob.Bytes)), 10)) || blob.Ref.Lifetime != protocolcommon.BlobLifetimeRequest || blob.Ref.MediaType == "" {
			return fmt.Errorf("invalid output blob metadata")
		}
	}
	return nil
}

func mapDiagnostics(input []engine.Diagnostic) ([]semantic.Diagnostic, error) {
	result, err := mapDiagnosticsWithBudget(input, newCompileMappingBudget(math.MaxInt64))
	return result, err
}

// mapDiagnosticsWithBudget maps one diagnostic at a time and accounts for its
// exact generated Go JSON representation before retaining it. Budget failure
// is based on bytes that must occur in the eventual response, so this cannot
// reject a response which would fit merely because it has many items.
func mapDiagnosticsWithBudget(input []engine.Diagnostic, budget *compileMappingBudget) ([]semantic.Diagnostic, error) {
	capacity := min(len(input), 256)
	if budget != nil {
		minimum, err := mapDiagnostic(engine.Diagnostic{})
		if err != nil {
			return nil, err
		}
		stats, err := measureCompileWireJSON(minimum)
		if err != nil {
			return nil, err
		}
		remaining := max(int64(0), budget.maximum-budget.used)
		capacity = min(len(input), int(remaining/max(int64(1), stats.bytes)))
	}
	result := make([]semantic.Diagnostic, 0, capacity)
	for _, diagnostic := range input {
		mapped, err := mapDiagnosticWithMaximum(diagnostic, budget.maximum)
		if err != nil {
			return nil, err
		}
		if err := budget.claim(mapped); err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	return result, nil
}

func mapDiagnostic(diagnostic engine.Diagnostic) (semantic.Diagnostic, error) {
	return mapDiagnosticWithMaximum(diagnostic, math.MaxInt64)
}

func mapDiagnosticWithMaximum(diagnostic engine.Diagnostic, maximum int64) (semantic.Diagnostic, error) {
	mapped := semantic.Diagnostic{
		Arguments: make(map[string]semantic.DiagnosticArgumentValue, min(len(diagnostic.Arguments), 64)), Code: diagnostic.Code, MessageKey: diagnostic.MessageKey,
		ProtocolVersion: 1, Related: make([]semantic.DiagnosticRelated, 0, min(len(diagnostic.Related), 64)),
		Severity: semantic.DiagnosticSeverity(diagnostic.Severity),
	}
	if diagnostic.Message != "" {
		mapped.Message = stringPointer(diagnostic.Message)
	}
	if diagnostic.Range != nil {
		value, rangeErr := mapSourceRange(*diagnostic.Range)
		if rangeErr != nil {
			return semantic.Diagnostic{}, rangeErr
		}
		mapped.Range = &value
	}
	mapped.SubjectAddress = stableAddressPointer(diagnostic.SubjectAddress)
	mapped.OwnerAddress = stableAddressPointer(diagnostic.OwnerAddress)
	localBudget := newCompileMappingBudget(maximum)
	if err := localBudget.claim(mapped); err != nil {
		return semantic.Diagnostic{}, err
	}
	for key, value := range diagnostic.Arguments {
		copyValue := value
		mappedValue := semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindString, StringValue: &copyValue}
		if err := localBudget.claim(key); err != nil {
			return semantic.Diagnostic{}, err
		}
		if err := localBudget.claim(mappedValue); err != nil {
			return semantic.Diagnostic{}, err
		}
		mapped.Arguments[key] = mappedValue
	}
	for _, related := range diagnostic.Related {
		mappedRelated := semantic.DiagnosticRelated{Relation: semantic.DiagnosticRelation(related.Relation)}
		if related.Message != "" {
			mappedRelated.Message = stringPointer(related.Message)
		}
		if related.Range != nil {
			value, rangeErr := mapSourceRange(*related.Range)
			if rangeErr != nil {
				return semantic.Diagnostic{}, rangeErr
			}
			mappedRelated.Range = &value
		}
		mappedRelated.SubjectAddress = stableAddressPointer(related.SubjectAddress)
		mappedRelated.OwnerAddress = stableAddressPointer(related.OwnerAddress)
		if err := localBudget.claim(mappedRelated); err != nil {
			return semantic.Diagnostic{}, err
		}
		mapped.Related = append(mapped.Related, mappedRelated)
	}
	return mapped, nil
}

func mapSourceMap(input engine.SourceMap) (semantic.SourceMap, error) {
	return mapSourceMapWithBudget(input, newCompileMappingBudget(math.MaxInt64))
}

func mapSourceMapWithBudget(input engine.SourceMap, budget *compileMappingBudget) (semantic.SourceMap, error) {
	result := semantic.SourceMap{
		SchemaVersion: int64(input.SchemaVersion), Files: make([]semantic.SourceFileRecord, 0, min(len(input.Files), 256)),
		Subjects: make([]semantic.SourceSubjectRecord, 0, min(len(input.Subjects), 256)), Bindings: make([]semantic.SourceBindingRecord, 0, min(len(input.Bindings), 256)),
		Exports: make([]semantic.ExportBindingRecord, 0, min(len(input.Exports), 256)), Assets: make([]semantic.SourceAssetRecord, 0, min(len(input.Assets), 256)),
	}
	for _, file := range input.Files {
		origin, err := mapSourceOrigin(file.Origin)
		if err != nil {
			return semantic.SourceMap{}, err
		}
		length, err := canonicalUint64FromInt64(int64(file.ByteLength))
		if err != nil {
			return semantic.SourceMap{}, err
		}
		mapped := semantic.SourceFileRecord{Origin: origin, ModulePath: file.ModulePath, Digest: protocolcommon.Digest(file.Digest), ByteLength: length}
		if err := budget.claim(mapped); err != nil {
			return semantic.SourceMap{}, err
		}
		result.Files = append(result.Files, mapped)
	}
	for _, subject := range input.Subjects {
		mapped := semantic.SourceSubjectRecord{Address: semantic.StableAddress(subject.Address), Kind: semantic.SubjectKind(subject.Kind), ManifestRoot: subject.ManifestRoot, CommentRanges: make([]semantic.SourceRange, 0, min(len(subject.CommentRanges), 256))}
		mapped.OwnerAddress = stringStableAddressPointer(subject.OwnerAddress)
		if subject.Module != nil {
			value, err := mapModuleRef(*subject.Module)
			if err != nil {
				return semantic.SourceMap{}, err
			}
			mapped.Module = &value
		}
		if subject.DeclarationRange != nil {
			value, err := mapSourceRange(*subject.DeclarationRange)
			if err != nil {
				return semantic.SourceMap{}, err
			}
			mapped.DeclarationRange = &value
		}
		localBudget := newCompileMappingBudget(budget.maximum)
		if err := localBudget.claim(mapped); err != nil {
			return semantic.SourceMap{}, err
		}
		for _, item := range subject.CommentRanges {
			value, err := mapSourceRange(item)
			if err != nil {
				return semantic.SourceMap{}, err
			}
			if err := localBudget.claim(value); err != nil {
				return semantic.SourceMap{}, err
			}
			mapped.CommentRanges = append(mapped.CommentRanges, value)
		}
		if err := budget.claim(mapped); err != nil {
			return semantic.SourceMap{}, err
		}
		result.Subjects = append(result.Subjects, mapped)
	}
	for _, binding := range input.Bindings {
		module, err := mapModuleRef(binding.Module)
		if err != nil {
			return semantic.SourceMap{}, err
		}
		rangeValue, err := mapSourceRange(binding.Range)
		if err != nil {
			return semantic.SourceMap{}, err
		}
		mapped := semantic.SourceBindingRecord{SourceAddress: semantic.StableAddress(binding.SourceAddress), TargetAddress: semantic.StableAddress(binding.TargetAddress), TargetKind: semantic.SubjectKind(binding.TargetKind), TargetOwnerAddress: stableAddressPointer(binding.TargetOwnerAddress), Via: binding.Via, Module: module, Range: rangeValue}
		if err := budget.claim(mapped); err != nil {
			return semantic.SourceMap{}, err
		}
		result.Bindings = append(result.Bindings, mapped)
	}
	for _, binding := range input.Exports {
		module, err := mapModuleRef(binding.Module)
		if err != nil {
			return semantic.SourceMap{}, err
		}
		rangeValue, err := mapSourceRange(binding.Range)
		if err != nil {
			return semantic.SourceMap{}, err
		}
		mapped := semantic.ExportBindingRecord{PublicName: binding.PublicName, TargetAddress: semantic.StableAddress(binding.TargetAddress), Module: module, Range: rangeValue, ReExport: binding.ReExport}
		if err := budget.claim(mapped); err != nil {
			return semantic.SourceMap{}, err
		}
		result.Exports = append(result.Exports, mapped)
	}
	for _, asset := range input.Assets {
		origin, err := mapSourceOrigin(asset.Origin)
		if err != nil {
			return semantic.SourceMap{}, err
		}
		rangeValue, err := mapSourceRange(asset.Range)
		if err != nil {
			return semantic.SourceMap{}, err
		}
		length, err := canonicalUint64FromInt64(asset.ByteLength)
		if err != nil {
			return semantic.SourceMap{}, err
		}
		mapped := semantic.SourceAssetRecord{SubjectAddress: semantic.StableAddress(asset.SubjectAddress), AuthoredPath: asset.AuthoredPath, Locator: asset.Locator, Origin: origin, ModulePath: asset.ModulePath, Range: rangeValue, Digest: protocolcommon.Digest(asset.Digest), MediaType: asset.MediaType, ByteLength: length}
		if err := budget.claim(mapped); err != nil {
			return semantic.SourceMap{}, err
		}
		result.Assets = append(result.Assets, mapped)
	}
	return result, nil
}

func mapSemanticIndex(input engine.SemanticIndex) (semantic.SemanticIndex, error) {
	return mapSemanticIndexWithBudget(input, newCompileMappingBudget(math.MaxInt64))
}

func mapSemanticIndexWithBudget(input engine.SemanticIndex, budget *compileMappingBudget) (semantic.SemanticIndex, error) {
	result := semantic.SemanticIndex{
		SchemaVersion: int64(input.SchemaVersion), Subjects: make([]semantic.SemanticSubject, 0, min(len(input.Subjects), 256)), References: make([]semantic.SemanticReference, 0, min(len(input.References), 256)),
		ReferenceIDs: make([]semantic.ReferenceIdRecord, 0, min(len(input.ReferenceIDs), 256)), Adjacency: make([]semantic.AdjacencyRecord, 0, min(len(input.Adjacency), 256)), Dependencies: make([]semantic.DependencyRecord, 0, min(len(input.Dependencies), 256)),
	}
	ownerGroups := []struct {
		input  []index.OwnerMembers
		target *[]semantic.OwnerMembers
	}{
		{input.Children, &result.Children}, {input.Rows, &result.Rows}, {input.Columns, &result.Columns},
		{input.TypeMembership, &result.TypeMembership}, {input.LayerMembership, &result.LayerMembership},
	}
	for _, group := range ownerGroups {
		mapped, err := mapOwnerMembersWithBudget(group.input, budget)
		if err != nil {
			return semantic.SemanticIndex{}, err
		}
		*group.target = mapped
	}
	for _, subject := range input.Subjects {
		mapped := semantic.SemanticSubject{Address: semantic.StableAddress(subject.Address), Kind: semantic.SubjectKind(subject.Kind), OwnerAddress: stringStableAddressPointer(subject.OwnerAddress), OwnHash: protocolcommon.Digest(subject.OwnHash)}
		if subject.Module != nil {
			value, err := mapModuleRef(*subject.Module)
			if err != nil {
				return semantic.SemanticIndex{}, err
			}
			mapped.Module = &value
		}
		if subject.SubtreeHash != nil {
			mapped.SubtreeHash = digestPointer(*subject.SubtreeHash)
		}
		if err := budget.claim(mapped); err != nil {
			return semantic.SemanticIndex{}, err
		}
		result.Subjects = append(result.Subjects, mapped)
	}
	for _, reference := range input.References {
		rangeValue, err := mapSourceRange(reference.Range)
		if err != nil {
			return semantic.SemanticIndex{}, err
		}
		mapped := semantic.SemanticReference{SourceAddress: semantic.StableAddress(reference.SourceAddress), TargetAddress: semantic.StableAddress(reference.TargetAddress), TargetKind: semantic.SubjectKind(reference.TargetKind), Via: reference.Via, Range: rangeValue}
		if err := budget.claim(mapped); err != nil {
			return semantic.SemanticIndex{}, err
		}
		result.References = append(result.References, mapped)
	}
	for _, record := range input.ReferenceIDs {
		if err := ensureStableAddressArraysWithinWire(budget.maximum, record.Addresses); err != nil {
			return semantic.SemanticIndex{}, err
		}
		mapped := semantic.ReferenceIdRecord{ID: record.ID, Addresses: stableAddresses(record.Addresses)}
		if err := budget.claim(mapped); err != nil {
			return semantic.SemanticIndex{}, err
		}
		result.ReferenceIDs = append(result.ReferenceIDs, mapped)
	}
	for _, record := range input.Adjacency {
		if err := ensureStableAddressArraysWithinWire(budget.maximum, record.Outgoing, record.Incoming); err != nil {
			return semantic.SemanticIndex{}, err
		}
		mapped := semantic.AdjacencyRecord{EntityAddress: semantic.StableAddress(record.EntityAddress), Outgoing: stableAddresses(record.Outgoing), Incoming: stableAddresses(record.Incoming)}
		if err := budget.claim(mapped); err != nil {
			return semantic.SemanticIndex{}, err
		}
		result.Adjacency = append(result.Adjacency, mapped)
	}
	for _, dependency := range input.Dependencies {
		if err := ensureDependencyWithinWire(dependency, budget.maximum); err != nil {
			return semantic.SemanticIndex{}, err
		}
		mapped := mapDependencyRecord(dependency)
		if err := budget.claim(mapped); err != nil {
			return semantic.SemanticIndex{}, err
		}
		result.Dependencies = append(result.Dependencies, mapped)
	}
	var err error
	result.ScopedReads, err = mapScopedReadsWithBudget(input.ScopedReads, budget)
	if err != nil {
		return semantic.SemanticIndex{}, err
	}
	return result, nil
}

func mapDependencyRecord(input index.DependencyRecord) semantic.DependencyRecord {
	return semantic.DependencyRecord{
		Kind: semantic.DependencyKind(input.Kind), SubjectAddress: semantic.StableAddress(input.SubjectAddress), QueryAddresses: stableAddresses(input.QueryAddresses), ParameterAddresses: stableAddresses(input.ParameterAddresses),
		LayerAddresses: stableAddresses(input.LayerAddresses), EntityTypeAddresses: stableAddresses(input.EntityTypeAddresses), RelationTypeAddresses: stableAddresses(input.RelationTypeAddresses),
		EntityAddresses: stableAddresses(input.EntityAddresses), RelationAddresses: stableAddresses(input.RelationAddresses), ColumnAddresses: stableAddresses(input.ColumnAddresses), ExportAddresses: stableAddresses(input.ExportAddresses), StateReads: mapStateReads(input.StateReads),
	}
}

func mapScopedReads(input index.ScopedReadIndexes) (semantic.ScopedReadIndexes, error) {
	return mapScopedReadsWithBudget(input, newCompileMappingBudget(math.MaxInt64))
}

func mapScopedReadsWithBudget(input index.ScopedReadIndexes, budget *compileMappingBudget) (semantic.ScopedReadIndexes, error) {
	result := semantic.ScopedReadIndexes{
		ByModule: make([]semantic.ScopeAddresses, 0, min(len(input.ByModule), 256)), ByKind: make([]semantic.KindAddresses, 0, min(len(input.ByKind), 256)), ReferencesByID: make([]semantic.ReferenceIdRecord, 0, min(len(input.ReferencesByID), 256)),
	}
	ownerGroups := []struct {
		input  []index.OwnerMembers
		target *[]semantic.OwnerMembers
	}{
		{input.ChildrenByOwner, &result.ChildrenByOwner}, {input.RowsByOwner, &result.RowsByOwner}, {input.ColumnsByOwner, &result.ColumnsByOwner},
		{input.MembersByType, &result.MembersByType}, {input.MembersByLayer, &result.MembersByLayer}, {input.OutgoingByEntity, &result.OutgoingByEntity},
		{input.IncomingByEntity, &result.IncomingByEntity}, {input.UsagesByTarget, &result.UsagesByTarget}, {input.QueriesByDependency, &result.QueriesByDependency},
		{input.ViewsByDependency, &result.ViewsByDependency},
	}
	for _, group := range ownerGroups {
		mapped, err := mapOwnerMembersWithBudget(group.input, budget)
		if err != nil {
			return semantic.ScopedReadIndexes{}, err
		}
		*group.target = mapped
	}
	for _, item := range input.ByModule {
		module, err := mapModuleRef(item.Module)
		if err != nil {
			return semantic.ScopedReadIndexes{}, err
		}
		if err := ensureStableAddressArraysWithinWire(budget.maximum, item.Addresses); err != nil {
			return semantic.ScopedReadIndexes{}, err
		}
		mapped := semantic.ScopeAddresses{Module: module, Addresses: stableAddresses(item.Addresses)}
		if err := budget.claim(mapped); err != nil {
			return semantic.ScopedReadIndexes{}, err
		}
		result.ByModule = append(result.ByModule, mapped)
	}
	for _, item := range input.ByKind {
		if err := ensureStableAddressArraysWithinWire(budget.maximum, item.Addresses); err != nil {
			return semantic.ScopedReadIndexes{}, err
		}
		mapped := semantic.KindAddresses{Kind: semantic.SubjectKind(item.Kind), Addresses: stableAddresses(item.Addresses)}
		if err := budget.claim(mapped); err != nil {
			return semantic.ScopedReadIndexes{}, err
		}
		result.ByKind = append(result.ByKind, mapped)
	}
	for _, item := range input.ReferencesByID {
		if err := ensureStableAddressArraysWithinWire(budget.maximum, item.Addresses); err != nil {
			return semantic.ScopedReadIndexes{}, err
		}
		mapped := semantic.ReferenceIdRecord{ID: item.ID, Addresses: stableAddresses(item.Addresses)}
		if err := budget.claim(mapped); err != nil {
			return semantic.ScopedReadIndexes{}, err
		}
		result.ReferencesByID = append(result.ReferencesByID, mapped)
	}
	return result, nil
}

func mapOwnerMembers(input []index.OwnerMembers) []semantic.OwnerMembers {
	result, _ := mapOwnerMembersWithBudget(input, newCompileMappingBudget(math.MaxInt64))
	return result
}

func mapOwnerMembersWithBudget(input []index.OwnerMembers, budget *compileMappingBudget) ([]semantic.OwnerMembers, error) {
	result := make([]semantic.OwnerMembers, 0, min(len(input), 256))
	for _, item := range input {
		if err := ensureStableAddressArraysWithinWire(budget.maximum, item.Addresses); err != nil {
			return nil, err
		}
		mapped := semantic.OwnerMembers{OwnerAddress: semantic.StableAddress(item.OwnerAddress), Addresses: stableAddresses(item.Addresses)}
		if err := budget.claim(mapped); err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	return result, nil
}

func mapSearchDocuments(input []engine.SearchDocument) ([]semantic.SearchDocument, error) {
	return mapSearchDocumentsWithBudget(input, newCompileMappingBudget(math.MaxInt64))
}

func mapSearchDocumentsWithBudget(input []engine.SearchDocument, budget *compileMappingBudget) ([]semantic.SearchDocument, error) {
	result := make([]semantic.SearchDocument, 0, min(len(input), 256))
	for _, document := range input {
		if err := ensureStableAddressArraysWithinWire(budget.maximum, document.GraphEntryAddresses, document.TypeAddresses, document.LayerAddresses); err != nil {
			return nil, err
		}
		mapped := semantic.SearchDocument{SchemaVersion: int64(document.SchemaVersion), SubjectAddress: semantic.StableAddress(document.SubjectAddress), SubjectKind: semantic.SubjectKind(document.SubjectKind), OwnerAddress: stringStableAddressPointer(document.OwnerAddress), GraphEntryAddresses: stableAddresses(document.GraphEntryAddresses), TypeAddresses: stableAddresses(document.TypeAddresses), LayerAddresses: stableAddresses(document.LayerAddresses), Fields: make([]semantic.SearchField, 0, min(len(document.Fields), 256)), ContentHash: protocolcommon.Digest(document.ContentHash)}
		localBudget := newCompileMappingBudget(budget.maximum)
		if err := localBudget.claim(mapped); err != nil {
			return nil, err
		}
		for _, field := range document.Fields {
			mappedField := semantic.SearchField{FieldPath: field.FieldPath, Text: field.Text, LexicalWeight: int64(field.LexicalWeight), IncludeInEmbedding: field.IncludeInEmbedding}
			if field.SourceRef != nil {
				value, err := mapSourceRange(*field.SourceRef)
				if err != nil {
					return nil, err
				}
				mappedField.SourceRef = &value
			}
			if err := localBudget.claim(mappedField); err != nil {
				return nil, err
			}
			mapped.Fields = append(mapped.Fields, mappedField)
		}
		if err := budget.claim(mapped); err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	return result, nil
}

func mapModuleRef(input index.ModuleRef) (semantic.ModuleRef, error) {
	origin, err := mapSourceOrigin(input.Origin)
	if err != nil {
		return semantic.ModuleRef{}, err
	}
	return semantic.ModuleRef{Origin: origin, ModulePath: input.ModulePath}, nil
}

func mapSourceOrigin(input resolve.SourceOrigin) (semantic.SourceOrigin, error) {
	result := semantic.SourceOrigin{Kind: semantic.OriginKind(input.Kind)}
	switch input.Kind {
	case resolve.OriginProject:
		if input.PackAddress != "" {
			return semantic.SourceOrigin{}, fmt.Errorf("Project origin has Pack address")
		}
	case resolve.OriginPack:
		if input.PackAddress == "" {
			return semantic.SourceOrigin{}, fmt.Errorf("Pack origin lacks Pack address")
		}
		result.PackAddress = stableAddressPointer(input.PackAddress)
	default:
		return semantic.SourceOrigin{}, fmt.Errorf("unknown source origin")
	}
	return result, nil
}

func mapSourceRange(input resolve.SourceRange) (semantic.SourceRange, error) {
	if input.StartByte < 0 || input.EndByte < input.StartByte {
		return semantic.SourceRange{}, fmt.Errorf("invalid source range")
	}
	origin, err := mapSourceOrigin(input.Origin)
	if err != nil {
		return semantic.SourceRange{}, err
	}
	start, err := canonicalUint64FromInt64(int64(input.StartByte))
	if err != nil {
		return semantic.SourceRange{}, err
	}
	end, err := canonicalUint64FromInt64(int64(input.EndByte))
	if err != nil {
		return semantic.SourceRange{}, err
	}
	return semantic.SourceRange{Origin: origin, ModulePath: input.ModulePath, StartByte: start, EndByte: end}, nil
}

func mapStateReads(input []query.StateReadDependency) []semantic.StateReadDependency {
	result := make([]semantic.StateReadDependency, len(input))
	for index, item := range input {
		result[index] = semantic.StateReadDependency{
			SubjectKind: semantic.StateSubjectKind(item.SubjectKind),
			FieldPath:   string(item.FieldPath),
			ValueType:   semantic.ValueType(item.ValueType),
		}
	}
	return result
}

func ensureDependencyWithinWire(input index.DependencyRecord, maximum int64) error {
	budget := newCompileMappingBudget(maximum)
	if err := claimStableAddressArrays(budget,
		input.QueryAddresses, input.ParameterAddresses, input.LayerAddresses, input.EntityTypeAddresses,
		input.RelationTypeAddresses, input.EntityAddresses, input.RelationAddresses, input.ColumnAddresses,
		input.ExportAddresses,
	); err != nil {
		return err
	}
	for _, item := range input.StateReads {
		mapped := semantic.StateReadDependency{SubjectKind: semantic.StateSubjectKind(item.SubjectKind), FieldPath: string(item.FieldPath), ValueType: semantic.ValueType(item.ValueType)}
		if err := budget.claim(mapped); err != nil {
			return err
		}
	}
	return nil
}

func ensureStableAddressArraysWithinWire(maximum int64, inputs ...[]string) error {
	return claimStableAddressArrays(newCompileMappingBudget(maximum), inputs...)
}

func claimStableAddressArrays(budget *compileMappingBudget, inputs ...[]string) error {
	for _, input := range inputs {
		for _, item := range input {
			if err := budget.claim(semantic.StableAddress(item)); err != nil {
				return err
			}
		}
	}
	return nil
}

func canonicalUint64FromInt64(value int64) (protocolcommon.CanonicalUint64, error) {
	if value < 0 {
		return "", fmt.Errorf("negative unsigned value")
	}
	return protocolcommon.CanonicalUint64(strconv.FormatInt(value, 10)), nil
}

func stableAddresses(input []string) []semantic.StableAddress {
	result := make([]semantic.StableAddress, len(input))
	for i, value := range input {
		result[i] = semantic.StableAddress(value)
	}
	return result
}

func stableAddressPointer(input string) *semantic.StableAddress {
	if input == "" {
		return nil
	}
	value := semantic.StableAddress(input)
	return &value
}

func stringStableAddressPointer(input *string) *semantic.StableAddress {
	if input == nil {
		return nil
	}
	value := semantic.StableAddress(*input)
	return &value
}

func digestPointer(input string) *protocolcommon.Digest {
	value := protocolcommon.Digest(input)
	return &value
}
func stringPointer(input string) *string { value := input; return &value }
