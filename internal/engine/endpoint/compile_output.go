// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	if snapshot.Mode != engine.CompileProject && snapshot.Mode != engine.CompilePack {
		return engineprotocol.CompileResult{}, nil, fmt.Errorf("invalid successful compile mode")
	}
	limits, err := mapEffectiveLimits(snapshot.EffectiveLimits)
	if err != nil {
		return engineprotocol.CompileResult{}, nil, err
	}
	sourceMap, err := mapSourceMap(snapshot.SourceMap)
	if err != nil {
		return engineprotocol.CompileResult{}, nil, err
	}
	semanticIndex, err := mapSemanticIndex(snapshot.SemanticIndex)
	if err != nil {
		return engineprotocol.CompileResult{}, nil, err
	}
	searchDocuments, err := mapSearchDocuments(snapshot.SearchDocuments)
	if err != nil {
		return engineprotocol.CompileResult{}, nil, err
	}
	normalized, artifactBlobs, err := mapNormalizedArtifact(snapshot, searchDocuments)
	if err != nil {
		return engineprotocol.CompileResult{}, nil, err
	}
	compiledRecipes, recipeBlobs, err := mapCompiledRecipes(snapshot)
	if err != nil {
		return engineprotocol.CompileResult{}, nil, err
	}

	result := engineprotocol.CompileResult{
		AuthoringSubjectClassification: make([]semantic.AuthoringSubjectClassification, len(snapshot.AuthoringSubjectClassification)),
		ChildSetHashes:                 make([]semantic.ChildSetHash, len(snapshot.ChildSetHashes)),
		CompiledRecipes:                compiledRecipes,
		DefinitionHash:                 protocolcommon.Digest(snapshot.DefinitionHash),
		EffectiveLimits:                limits,
		NormalizedArtifact:             normalized,
		SemanticIndex:                  semanticIndex,
		SourceMap:                      sourceMap,
		StableAddresses:                stableAddresses(snapshot.StableAddresses),
		SubjectSemanticHashes:          make([]semantic.SubjectHash, len(snapshot.SubjectSemanticHashes)),
		SubtreeHashes:                  make([]semantic.SubtreeHash, len(snapshot.SubtreeHashes)),
	}
	for i, item := range snapshot.AuthoringSubjectClassification {
		result.AuthoringSubjectClassification[i] = semantic.AuthoringSubjectClassification{
			Address:    semantic.StableAddress(item.Address),
			Capability: semantic.AuthoringCapability(item.Capability),
			Kind:       semantic.SubjectKind(item.Kind),
		}
	}
	for i, item := range snapshot.SubjectSemanticHashes {
		result.SubjectSemanticHashes[i] = semantic.SubjectHash{Address: semantic.StableAddress(item.Address), Hash: protocolcommon.Digest(item.Hash), Kind: semantic.SubjectKind(item.Kind)}
	}
	for i, item := range snapshot.SubtreeHashes {
		result.SubtreeHashes[i] = semantic.SubtreeHash{OwnerAddress: semantic.StableAddress(item.OwnerAddress), Hash: protocolcommon.Digest(item.Hash)}
	}
	for i, item := range snapshot.ChildSetHashes {
		result.ChildSetHashes[i] = semantic.ChildSetHash{
			OwnerAddress:   semantic.StableAddress(item.OwnerAddress),
			ChildKind:      semantic.SubjectKind(item.ChildKind),
			ChildAddresses: stableAddresses(item.Addresses),
			Hash:           protocolcommon.Digest(item.Hash),
		}
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
	result := make([]semantic.Diagnostic, len(input))
	for i, diagnostic := range input {
		arguments := make(map[string]semantic.DiagnosticArgumentValue, len(diagnostic.Arguments))
		for key, value := range diagnostic.Arguments {
			copyValue := value
			arguments[key] = semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindString, StringValue: &copyValue}
		}
		mapped := semantic.Diagnostic{
			Arguments: arguments, Code: diagnostic.Code, MessageKey: diagnostic.MessageKey,
			ProtocolVersion: 1, Related: make([]semantic.DiagnosticRelated, len(diagnostic.Related)),
			Severity: semantic.DiagnosticSeverity(diagnostic.Severity),
		}
		if diagnostic.Message != "" {
			mapped.Message = stringPointer(diagnostic.Message)
		}
		if diagnostic.Range != nil {
			value, rangeErr := mapSourceRange(*diagnostic.Range)
			if rangeErr != nil {
				return nil, rangeErr
			}
			mapped.Range = &value
		}
		mapped.SubjectAddress = stableAddressPointer(diagnostic.SubjectAddress)
		mapped.OwnerAddress = stableAddressPointer(diagnostic.OwnerAddress)
		for relatedIndex, related := range diagnostic.Related {
			mappedRelated := semantic.DiagnosticRelated{Relation: semantic.DiagnosticRelation(related.Relation)}
			if related.Message != "" {
				mappedRelated.Message = stringPointer(related.Message)
			}
			if related.Range != nil {
				value, rangeErr := mapSourceRange(*related.Range)
				if rangeErr != nil {
					return nil, rangeErr
				}
				mappedRelated.Range = &value
			}
			mappedRelated.SubjectAddress = stableAddressPointer(related.SubjectAddress)
			mappedRelated.OwnerAddress = stableAddressPointer(related.OwnerAddress)
			mapped.Related[relatedIndex] = mappedRelated
		}
		result[i] = mapped
	}
	return result, nil
}

func mapSourceMap(input engine.SourceMap) (semantic.SourceMap, error) {
	result := semantic.SourceMap{
		SchemaVersion: int64(input.SchemaVersion), Files: make([]semantic.SourceFileRecord, len(input.Files)),
		Subjects: make([]semantic.SourceSubjectRecord, len(input.Subjects)), Bindings: make([]semantic.SourceBindingRecord, len(input.Bindings)),
		Exports: make([]semantic.ExportBindingRecord, len(input.Exports)), Assets: make([]semantic.SourceAssetRecord, len(input.Assets)),
	}
	for i, file := range input.Files {
		origin, err := mapSourceOrigin(file.Origin)
		if err != nil {
			return semantic.SourceMap{}, err
		}
		length, err := canonicalUint64FromInt64(int64(file.ByteLength))
		if err != nil {
			return semantic.SourceMap{}, err
		}
		result.Files[i] = semantic.SourceFileRecord{Origin: origin, ModulePath: file.ModulePath, Digest: protocolcommon.Digest(file.Digest), ByteLength: length}
	}
	for i, subject := range input.Subjects {
		mapped := semantic.SourceSubjectRecord{Address: semantic.StableAddress(subject.Address), Kind: semantic.SubjectKind(subject.Kind), ManifestRoot: subject.ManifestRoot, CommentRanges: make([]semantic.SourceRange, len(subject.CommentRanges))}
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
		for rangeIndex, item := range subject.CommentRanges {
			value, err := mapSourceRange(item)
			if err != nil {
				return semantic.SourceMap{}, err
			}
			mapped.CommentRanges[rangeIndex] = value
		}
		result.Subjects[i] = mapped
	}
	for i, binding := range input.Bindings {
		module, err := mapModuleRef(binding.Module)
		if err != nil {
			return semantic.SourceMap{}, err
		}
		rangeValue, err := mapSourceRange(binding.Range)
		if err != nil {
			return semantic.SourceMap{}, err
		}
		result.Bindings[i] = semantic.SourceBindingRecord{SourceAddress: semantic.StableAddress(binding.SourceAddress), TargetAddress: semantic.StableAddress(binding.TargetAddress), TargetKind: semantic.SubjectKind(binding.TargetKind), TargetOwnerAddress: stableAddressPointer(binding.TargetOwnerAddress), Via: binding.Via, Module: module, Range: rangeValue}
	}
	for i, binding := range input.Exports {
		module, err := mapModuleRef(binding.Module)
		if err != nil {
			return semantic.SourceMap{}, err
		}
		rangeValue, err := mapSourceRange(binding.Range)
		if err != nil {
			return semantic.SourceMap{}, err
		}
		result.Exports[i] = semantic.ExportBindingRecord{PublicName: binding.PublicName, TargetAddress: semantic.StableAddress(binding.TargetAddress), Module: module, Range: rangeValue, ReExport: binding.ReExport}
	}
	for i, asset := range input.Assets {
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
		result.Assets[i] = semantic.SourceAssetRecord{SubjectAddress: semantic.StableAddress(asset.SubjectAddress), AuthoredPath: asset.AuthoredPath, Locator: asset.Locator, Origin: origin, ModulePath: asset.ModulePath, Range: rangeValue, Digest: protocolcommon.Digest(asset.Digest), MediaType: asset.MediaType, ByteLength: length}
	}
	return result, nil
}

func mapSemanticIndex(input engine.SemanticIndex) (semantic.SemanticIndex, error) {
	result := semantic.SemanticIndex{
		SchemaVersion: int64(input.SchemaVersion), Subjects: make([]semantic.SemanticSubject, len(input.Subjects)), References: make([]semantic.SemanticReference, len(input.References)),
		Children: mapOwnerMembers(input.Children), Rows: mapOwnerMembers(input.Rows), Columns: mapOwnerMembers(input.Columns), TypeMembership: mapOwnerMembers(input.TypeMembership), LayerMembership: mapOwnerMembers(input.LayerMembership),
		ReferenceIDs: make([]semantic.ReferenceIdRecord, len(input.ReferenceIDs)), Adjacency: make([]semantic.AdjacencyRecord, len(input.Adjacency)), Dependencies: make([]semantic.DependencyRecord, len(input.Dependencies)),
	}
	for i, subject := range input.Subjects {
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
		result.Subjects[i] = mapped
	}
	for i, reference := range input.References {
		rangeValue, err := mapSourceRange(reference.Range)
		if err != nil {
			return semantic.SemanticIndex{}, err
		}
		result.References[i] = semantic.SemanticReference{SourceAddress: semantic.StableAddress(reference.SourceAddress), TargetAddress: semantic.StableAddress(reference.TargetAddress), TargetKind: semantic.SubjectKind(reference.TargetKind), Via: reference.Via, Range: rangeValue}
	}
	for i, record := range input.ReferenceIDs {
		result.ReferenceIDs[i] = semantic.ReferenceIdRecord{ID: record.ID, Addresses: stableAddresses(record.Addresses)}
	}
	for i, record := range input.Adjacency {
		result.Adjacency[i] = semantic.AdjacencyRecord{EntityAddress: semantic.StableAddress(record.EntityAddress), Outgoing: stableAddresses(record.Outgoing), Incoming: stableAddresses(record.Incoming)}
	}
	for i, dependency := range input.Dependencies {
		result.Dependencies[i] = mapDependencyRecord(dependency)
	}
	var err error
	result.ScopedReads, err = mapScopedReads(input.ScopedReads)
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
	result := semantic.ScopedReadIndexes{
		ByModule: make([]semantic.ScopeAddresses, len(input.ByModule)), ByKind: make([]semantic.KindAddresses, len(input.ByKind)), ChildrenByOwner: mapOwnerMembers(input.ChildrenByOwner), RowsByOwner: mapOwnerMembers(input.RowsByOwner), ColumnsByOwner: mapOwnerMembers(input.ColumnsByOwner),
		MembersByType: mapOwnerMembers(input.MembersByType), MembersByLayer: mapOwnerMembers(input.MembersByLayer), ReferencesByID: make([]semantic.ReferenceIdRecord, len(input.ReferencesByID)), OutgoingByEntity: mapOwnerMembers(input.OutgoingByEntity), IncomingByEntity: mapOwnerMembers(input.IncomingByEntity),
		UsagesByTarget: mapOwnerMembers(input.UsagesByTarget), QueriesByDependency: mapOwnerMembers(input.QueriesByDependency), ViewsByDependency: mapOwnerMembers(input.ViewsByDependency),
	}
	for i, item := range input.ByModule {
		module, err := mapModuleRef(item.Module)
		if err != nil {
			return semantic.ScopedReadIndexes{}, err
		}
		result.ByModule[i] = semantic.ScopeAddresses{Module: module, Addresses: stableAddresses(item.Addresses)}
	}
	for i, item := range input.ByKind {
		result.ByKind[i] = semantic.KindAddresses{Kind: semantic.SubjectKind(item.Kind), Addresses: stableAddresses(item.Addresses)}
	}
	for i, item := range input.ReferencesByID {
		result.ReferencesByID[i] = semantic.ReferenceIdRecord{ID: item.ID, Addresses: stableAddresses(item.Addresses)}
	}
	return result, nil
}

func mapOwnerMembers(input []index.OwnerMembers) []semantic.OwnerMembers {
	result := make([]semantic.OwnerMembers, len(input))
	for i, item := range input {
		result[i] = semantic.OwnerMembers{OwnerAddress: semantic.StableAddress(item.OwnerAddress), Addresses: stableAddresses(item.Addresses)}
	}
	return result
}

func mapSearchDocuments(input []engine.SearchDocument) ([]semantic.SearchDocument, error) {
	result := make([]semantic.SearchDocument, len(input))
	for i, document := range input {
		mapped := semantic.SearchDocument{SchemaVersion: int64(document.SchemaVersion), SubjectAddress: semantic.StableAddress(document.SubjectAddress), SubjectKind: semantic.SubjectKind(document.SubjectKind), OwnerAddress: stringStableAddressPointer(document.OwnerAddress), GraphEntryAddresses: stableAddresses(document.GraphEntryAddresses), TypeAddresses: stableAddresses(document.TypeAddresses), LayerAddresses: stableAddresses(document.LayerAddresses), Fields: make([]semantic.SearchField, len(document.Fields)), ContentHash: protocolcommon.Digest(document.ContentHash)}
		for fieldIndex, field := range document.Fields {
			mappedField := semantic.SearchField{FieldPath: field.FieldPath, Text: field.Text, LexicalWeight: int64(field.LexicalWeight), IncludeInEmbedding: field.IncludeInEmbedding}
			if field.SourceRef != nil {
				value, err := mapSourceRange(*field.SourceRef)
				if err != nil {
					return nil, err
				}
				mappedField.SourceRef = &value
			}
			mapped.Fields[fieldIndex] = mappedField
		}
		result[i] = mapped
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
