// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"strconv"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

type blobUse struct {
	ref protocolcommon.BlobRef
}

type preparedCompileInput struct {
	input        engineprotocol.CompileInput
	limits       engine.ResourceLimits
	uses         []blobUse
	requirements []BlobRequirement
	budget       CompileAdmissionBudget
}

func mapCompileInput(ctx context.Context, negotiated *NegotiatedContext, input engineprotocol.CompileInput, source BlobSource) (engine.CompileInput, []semantic.Diagnostic, *protocolcommon.ProtocolFailure) {
	prepared, diagnostics, failure := prepareCompileInput(negotiated, input)
	if failure != nil || len(diagnostics) != 0 {
		return engine.CompileInput{}, diagnostics, failure
	}
	owned, lease, failure := acquireBlobUses(ctx, prepared.uses, source)
	if failure != nil {
		return engine.CompileInput{}, nil, failure
	}
	mapped := mapPreparedCompileInput(prepared, owned)
	if releaseFailure := lease.Release(ctx); releaseFailure != nil {
		return engine.CompileInput{}, nil, releaseFailure
	}
	return mapped, nil, nil
}

func prepareCompileInput(negotiated *NegotiatedContext, input engineprotocol.CompileInput) (preparedCompileInput, []semantic.Diagnostic, *protocolcommon.ProtocolFailure) {
	limits, diagnostics, failure := mapRequestLimits(input.ResourceLimits, negotiated.defaultLimits, negotiated.effectiveMaximums)
	if failure != nil || len(diagnostics) != 0 {
		return preparedCompileInput{}, diagnostics, failure
	}

	duplicateDiagnostics := validateLogicalDuplicates(input)
	if len(duplicateDiagnostics) != 0 {
		return preparedCompileInput{}, duplicateDiagnostics, nil
	}

	uses := enumerateBlobUses(input)
	if failure := validateBlobAliases(uses); failure != nil {
		return preparedCompileInput{}, nil, failure
	}
	requirements, requiredBytes, failure := buildBlobRequirements(uses)
	if failure != nil {
		return preparedCompileInput{}, nil, failure
	}
	budget, failure := compileAdmissionBudget(input, limits, int64(len(requirements)), requiredBytes)
	if failure != nil {
		return preparedCompileInput{}, nil, failure
	}
	return preparedCompileInput{input: input, limits: limits, uses: uses, requirements: requirements, budget: budget}, nil, nil
}

func mapPreparedCompileInput(prepared preparedCompileInput, owned map[string][]byte) engine.CompileInput {
	// Attachment slices are already request-owned, so this boundary adopts them
	// without another whole-blob copy. Engine.Compile performs the canonical
	// facade's defensive clone before semantic work begins.
	input := prepared.input
	mapped := engine.CompileInput{
		Mode:              engine.CompileMode(input.Mode),
		EntryPath:         string(input.EntryPath),
		ProjectSourceTree: make(map[string][]byte, len(input.ProjectSourceTree)),
		InstalledPackTree: make(map[string][]byte, len(input.InstalledPackTree)),
		ResolvedDependencies: engine.ResolvedDependencies{
			Format:        string(input.ResolvedDependencies.Format),
			FormatVersion: int(input.ResolvedDependencies.FormatVersion),
			Language:      int(input.ResolvedDependencies.Language),
			Installs:      make([]engine.ResolvedPack, len(input.ResolvedDependencies.Installs)),
		},
		ReferencedAssets: make([]engine.AssetInput, len(input.ReferencedAssets)),
		ResourceLimits:   prepared.limits,
	}
	if input.RootPackID != nil {
		mapped.RootPackID = string(*input.RootPackID)
	}
	for _, file := range input.ProjectSourceTree {
		mapped.ProjectSourceTree[string(file.Path)] = owned[file.Blob.BlobID]
	}
	for _, file := range input.InstalledPackTree {
		mapped.InstalledPackTree[string(file.Path)] = owned[file.Blob.BlobID]
	}
	for index, pack := range input.ResolvedDependencies.Installs {
		mappedPack := engine.ResolvedPack{
			InstallName:  pack.InstallName,
			CanonicalID:  string(pack.CanonicalID),
			Version:      pack.Version,
			Digest:       string(pack.Digest),
			Path:         pack.Path,
			Entry:        pack.Entry,
			ManifestPath: pack.ManifestPath,
			Manifest:     owned[pack.Manifest.BlobID],
			Files:        make([]engine.ResolvedPackFile, len(pack.Files)),
			Dependencies: make([]engine.ResolvedPackDependency, len(pack.Dependencies)),
		}
		for fileIndex, file := range pack.Files {
			mappedPack.Files[fileIndex] = engine.ResolvedPackFile{Path: file.Path, Digest: string(file.Digest)}
		}
		for dependencyIndex, dependency := range pack.Dependencies {
			mappedPack.Dependencies[dependencyIndex] = engine.ResolvedPackDependency{LocalName: dependency.LocalName, InstallName: dependency.InstallName}
		}
		mapped.ResolvedDependencies.Installs[index] = mappedPack
	}
	for index, asset := range input.ReferencedAssets {
		mappedAsset := engine.AssetInput{
			Origin:     engine.SourceOriginKind(asset.Origin),
			Locator:    asset.Locator,
			Bytes:      owned[asset.Blob.BlobID],
			Digest:     string(asset.Digest),
			MediaType:  asset.MediaType,
			ByteLength: int64(len(owned[asset.Blob.BlobID])),
		}
		if asset.PackID != nil {
			mappedAsset.PackID = string(*asset.PackID)
		}
		mapped.ReferencedAssets[index] = mappedAsset
	}
	return mapped
}

func mapRequestLimits(input engineprotocol.ResourceLimits, defaults, maximums engine.ResourceLimits) (engine.ResourceLimits, []semantic.Diagnostic, *protocolcommon.ProtocolFailure) {
	type binding struct {
		name     string
		value    *protocolcommon.CanonicalNonNegativeInt64
		fallback int64
		maximum  int64
		target   *int64
	}
	result := engine.ResourceLimits{}
	bindings := []binding{
		{"max_project_source_files", input.MaxProjectSourceFiles, defaults.MaxProjectSourceFiles, maximums.MaxProjectSourceFiles, &result.MaxProjectSourceFiles},
		{"max_project_source_bytes", input.MaxProjectSourceBytes, defaults.MaxProjectSourceBytes, maximums.MaxProjectSourceBytes, &result.MaxProjectSourceBytes},
		{"max_pack_files", input.MaxPackFiles, defaults.MaxPackFiles, maximums.MaxPackFiles, &result.MaxPackFiles},
		{"max_pack_bytes", input.MaxPackBytes, defaults.MaxPackBytes, maximums.MaxPackBytes, &result.MaxPackBytes},
		{"max_assets", input.MaxAssets, defaults.MaxAssets, maximums.MaxAssets, &result.MaxAssets},
		{"max_asset_bytes", input.MaxAssetBytes, defaults.MaxAssetBytes, maximums.MaxAssetBytes, &result.MaxAssetBytes},
		{"max_raster_dimension", input.MaxRasterDimension, defaults.MaxRasterDimension, maximums.MaxRasterDimension, &result.MaxRasterDimension},
		{"max_raster_pixels", input.MaxRasterPixels, defaults.MaxRasterPixels, maximums.MaxRasterPixels, &result.MaxRasterPixels},
		{"max_declarations", input.MaxDeclarations, defaults.MaxDeclarations, maximums.MaxDeclarations, &result.MaxDeclarations},
	}
	for _, item := range bindings {
		value := item.fallback
		if item.value != nil {
			parsed, err := strconv.ParseInt(string(*item.value), 10, 64)
			if err != nil || parsed < 0 {
				failure := protocolFailure(protocolcommon.ProtocolFailureCategoryTransport, FailureCompileInvalidRequest, "The compile resource limits are invalid.", false, nil)
				return engine.ResourceLimits{}, nil, &failure
			}
			if parsed > 0 {
				value = parsed
			}
		}
		if value > item.maximum {
			return engine.ResourceLimits{}, []semantic.Diagnostic{closedInputDiagnostic(
				"invalid_closed_input_resource_limit_maximum",
				"A compile resource limit exceeds the negotiated effective maximum.",
				map[string]string{"limit": item.name, "maximum": strconv.FormatInt(item.maximum, 10)},
			)}, nil
		}
		*item.target = value
	}
	return result, nil, nil
}

func validateLogicalDuplicates(input engineprotocol.CompileInput) []semantic.Diagnostic {
	type duplicateCheck struct {
		key     string
		message string
		values  []string
	}
	checks := []duplicateCheck{
		{"invalid_closed_input_duplicate_project_path", "Project source paths must be unique.", sourcePaths(input.ProjectSourceTree)},
		{"invalid_closed_input_duplicate_installed_path", "Installed Pack tree paths must be unique.", sourcePaths(input.InstalledPackTree)},
	}
	installNames := make([]string, len(input.ResolvedDependencies.Installs))
	resolvedFullPaths := make([]string, 0)
	assetKeys := make([]string, len(input.ReferencedAssets))
	for index, pack := range input.ResolvedDependencies.Installs {
		installNames[index] = pack.InstallName
		filePaths := make([]string, len(pack.Files))
		for fileIndex, file := range pack.Files {
			filePaths[fileIndex] = file.Path
			resolvedFullPaths = append(resolvedFullPaths, pack.Path+"/"+file.Path)
		}
		dependencyNames := make([]string, len(pack.Dependencies))
		for dependencyIndex, dependency := range pack.Dependencies {
			dependencyNames[dependencyIndex] = dependency.LocalName
		}
		checks = append(checks,
			duplicateCheck{"invalid_closed_input_duplicate_pack_file", "Resolved Pack file paths must be unique.", filePaths},
			duplicateCheck{"invalid_closed_input_duplicate_pack_dependency", "Resolved Pack dependency-local names must be unique.", dependencyNames},
		)
	}
	for index, asset := range input.ReferencedAssets {
		packID := ""
		if asset.PackID != nil {
			packID = string(*asset.PackID)
		}
		assetKeys[index] = string(asset.Origin) + "\x00" + packID + "\x00" + asset.Locator
	}
	checks = append(checks,
		duplicateCheck{"invalid_closed_input_duplicate_install", "Resolved Pack install names must be unique.", installNames},
		duplicateCheck{"invalid_closed_input_duplicate_pack_path", "Resolved Packs must not claim the same installed path.", resolvedFullPaths},
		duplicateCheck{"invalid_closed_input_duplicate_asset", "Referenced assets must have unique origin, Pack, and locator bindings.", assetKeys},
	)

	diagnostics := make([]semantic.Diagnostic, 0)
	for _, check := range checks {
		values := slices.Clone(check.values)
		slices.Sort(values)
		for index := 1; index < len(values); index++ {
			if values[index] == values[index-1] {
				diagnostics = append(diagnostics, closedInputDiagnostic(check.key, check.message, nil))
				break
			}
		}
	}
	sort.SliceStable(diagnostics, func(i, j int) bool { return diagnostics[i].MessageKey < diagnostics[j].MessageKey })
	return diagnostics
}

func sourcePaths(input []engineprotocol.SourceFileInput) []string {
	result := make([]string, len(input))
	for index, file := range input {
		result[index] = string(file.Path)
	}
	return result
}

func closedInputDiagnostic(key, message string, arguments map[string]string) semantic.Diagnostic {
	messageCopy := message
	mappedArguments := make(map[string]semantic.DiagnosticArgumentValue, len(arguments))
	for name, value := range arguments {
		valueCopy := value
		mappedArguments[name] = semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindString, StringValue: &valueCopy}
	}
	return semantic.Diagnostic{
		Arguments:       mappedArguments,
		Code:            "LDL1201",
		Message:         &messageCopy,
		MessageKey:      key,
		ProtocolVersion: 1,
		Related:         []semantic.DiagnosticRelated{},
		Severity:        semantic.DiagnosticSeverityError,
	}
}

func enumerateBlobUses(input engineprotocol.CompileInput) []blobUse {
	uses := make([]blobUse, 0, len(input.ProjectSourceTree)+len(input.InstalledPackTree)+len(input.ResolvedDependencies.Installs)+len(input.ReferencedAssets))
	for _, file := range input.ProjectSourceTree {
		uses = append(uses, blobUse{ref: file.Blob})
	}
	for _, file := range input.InstalledPackTree {
		uses = append(uses, blobUse{ref: file.Blob})
	}
	for _, pack := range input.ResolvedDependencies.Installs {
		uses = append(uses, blobUse{ref: pack.Manifest})
	}
	for _, asset := range input.ReferencedAssets {
		uses = append(uses, blobUse{ref: asset.Blob})
	}
	return uses
}

func validateBlobAliases(input []blobUse) *protocolcommon.ProtocolFailure {
	// An identical BlobRef may be reused by multiple logical inputs. It is
	// verified once, each mapped use receives owned bytes, and resource
	// preflight charges every logical use to its applicable limit.
	uses := slices.Clone(input)
	sort.SliceStable(uses, func(i, j int) bool { return uses[i].ref.BlobID < uses[j].ref.BlobID })
	for index := 1; index < len(uses); index++ {
		if uses[index-1].ref.BlobID == uses[index].ref.BlobID && uses[index-1].ref != uses[index].ref {
			failure := protocolFailure(protocolcommon.ProtocolFailureCategoryTransport, FailureCompileConflictingBlobRef, "One blob identity has conflicting metadata.", false, nil)
			return &failure
		}
	}
	return nil
}

func buildBlobRequirements(input []blobUse) ([]BlobRequirement, int64, *protocolcommon.ProtocolFailure) {
	uses := slices.Clone(input)
	sort.SliceStable(uses, func(i, j int) bool { return uses[i].ref.BlobID < uses[j].ref.BlobID })
	requirements := make([]BlobRequirement, 0, len(uses))
	var requiredBytes int64
	for index := 0; index < len(uses); {
		next := index + 1
		for next < len(uses) && uses[next].ref.BlobID == uses[index].ref.BlobID {
			if uses[next].ref != uses[index].ref {
				failure := protocolFailure(protocolcommon.ProtocolFailureCategoryTransport, FailureCompileConflictingBlobRef, "One blob identity has conflicting metadata.", false, nil)
				return nil, 0, &failure
			}
			next++
		}
		if uses[index].ref.Lifetime != protocolcommon.BlobLifetimeRequest {
			failure := protocolFailure(protocolcommon.ProtocolFailureCategoryTransport, FailureCompileBlobLifetime, "Compile attachments must have request lifetime.", false, nil)
			return nil, 0, &failure
		}
		size, failure := blobSize(uses[index].ref)
		if failure != nil {
			return nil, 0, failure
		}
		if size > math.MaxInt64-requiredBytes {
			failure := protocolFailure(protocolcommon.ProtocolFailureCategoryResource, FailureCompileBlobOversized, "The required blob set exceeds the supported signed byte range.", false, nil)
			return nil, 0, &failure
		}
		requiredBytes += size
		requirements = append(requirements, BlobRequirement{Ref: uses[index].ref, References: int64(next - index)})
		index = next
	}
	return requirements, requiredBytes, nil
}

func preflightBlobResources(input engineprotocol.CompileInput, limits engine.ResourceLimits) *protocolcommon.ProtocolFailure {
	_, failure := compileAdmissionBudget(input, limits, 0, 0)
	return failure
}

func compileAdmissionBudget(input engineprotocol.CompileInput, limits engine.ResourceLimits, requiredBlobCount, requiredBlobBytes int64) (CompileAdmissionBudget, *protocolcommon.ProtocolFailure) {
	budget := CompileAdmissionBudget{
		RequiredBlobCount:  requiredBlobCount,
		RequiredBlobBytes:  requiredBlobBytes,
		ProjectSourceFiles: int64(len(input.ProjectSourceTree)),
		InstalledPackFiles: int64(len(input.InstalledPackTree)),
		Assets:             int64(len(input.ReferencedAssets)),
		EffectiveCompilerLimits: CompileEffectiveLimits{
			MaxProjectSourceFiles: limits.MaxProjectSourceFiles,
			MaxProjectSourceBytes: limits.MaxProjectSourceBytes,
			MaxPackFiles:          limits.MaxPackFiles, MaxPackBytes: limits.MaxPackBytes,
			MaxAssets: limits.MaxAssets, MaxAssetBytes: limits.MaxAssetBytes,
			MaxRasterDimension: limits.MaxRasterDimension, MaxRasterPixels: limits.MaxRasterPixels,
			MaxDeclarations: limits.MaxDeclarations,
		},
	}
	if int64(len(input.ProjectSourceTree)) > limits.MaxProjectSourceFiles {
		return CompileAdmissionBudget{}, resourceFailure(engine.ErrorCodeProjectSourceFilesExceeded, "project_source_files", limits.MaxProjectSourceFiles, int64(len(input.ProjectSourceTree)))
	}
	if int64(len(input.InstalledPackTree)) > limits.MaxPackFiles {
		return CompileAdmissionBudget{}, resourceFailure(engine.ErrorCodePackFilesExceeded, "pack_files", limits.MaxPackFiles, int64(len(input.InstalledPackTree)))
	}
	metadataFiles := int64(len(input.ResolvedDependencies.Installs))
	for _, pack := range input.ResolvedDependencies.Installs {
		if int64(len(pack.Files)) > math.MaxInt64-metadataFiles {
			return CompileAdmissionBudget{}, resourceFailure(engine.ErrorCodePackFilesExceeded, "pack_files", limits.MaxPackFiles, math.MaxInt64)
		}
		metadataFiles += int64(len(pack.Files))
	}
	budget.ResolvedPackFiles = metadataFiles
	if metadataFiles > limits.MaxPackFiles {
		return CompileAdmissionBudget{}, resourceFailure(engine.ErrorCodePackFilesExceeded, "pack_files", limits.MaxPackFiles, metadataFiles)
	}
	if int64(len(input.ReferencedAssets)) > limits.MaxAssets {
		return CompileAdmissionBudget{}, resourceFailure(engine.ErrorCodeAssetsExceeded, "assets", limits.MaxAssets, int64(len(input.ReferencedAssets)))
	}

	projectBytes := int64(0)
	for _, file := range input.ProjectSourceTree {
		value, failure := blobSize(file.Blob)
		if failure != nil {
			return CompileAdmissionBudget{}, failure
		}
		if value > limits.MaxProjectSourceBytes-projectBytes {
			return CompileAdmissionBudget{}, resourceFailure(engine.ErrorCodeProjectSourceBytesExceeded, "project_source_bytes", limits.MaxProjectSourceBytes, saturatedAdd(projectBytes, value))
		}
		projectBytes += value
	}
	budget.ProjectSourceBytes = projectBytes
	packBytes := int64(0)
	for _, file := range input.InstalledPackTree {
		value, failure := blobSize(file.Blob)
		if failure != nil {
			return CompileAdmissionBudget{}, failure
		}
		if value > limits.MaxPackBytes-packBytes {
			return CompileAdmissionBudget{}, resourceFailure(engine.ErrorCodePackBytesExceeded, "pack_bytes", limits.MaxPackBytes, saturatedAdd(packBytes, value))
		}
		packBytes += value
	}
	for _, pack := range input.ResolvedDependencies.Installs {
		value, failure := blobSize(pack.Manifest)
		if failure != nil {
			return CompileAdmissionBudget{}, failure
		}
		if value > limits.MaxPackBytes-packBytes {
			return CompileAdmissionBudget{}, resourceFailure(engine.ErrorCodePackBytesExceeded, "pack_bytes", limits.MaxPackBytes, saturatedAdd(packBytes, value))
		}
		packBytes += value
	}
	assetBytes := int64(0)
	for _, asset := range input.ReferencedAssets {
		if asset.Digest != asset.Blob.Digest || asset.MediaType != asset.Blob.MediaType {
			failure := protocolFailure(protocolcommon.ProtocolFailureCategoryTransport, FailureCompileConflictingBlobRef, "An asset binding conflicts with its blob metadata.", false, nil)
			return CompileAdmissionBudget{}, &failure
		}
		value, failure := blobSize(asset.Blob)
		if failure != nil {
			return CompileAdmissionBudget{}, failure
		}
		if value > limits.MaxAssetBytes-assetBytes {
			return CompileAdmissionBudget{}, resourceFailure(engine.ErrorCodeAssetBytesExceeded, "asset_bytes", limits.MaxAssetBytes, saturatedAdd(assetBytes, value))
		}
		assetBytes += value
	}
	budget.PackBytes = packBytes
	budget.AssetBytes = assetBytes
	return budget, nil
}

func blobSize(ref protocolcommon.BlobRef) (int64, *protocolcommon.ProtocolFailure) {
	value, err := strconv.ParseUint(string(ref.Size), 10, 64)
	if err != nil {
		failure := protocolFailure(protocolcommon.ProtocolFailureCategoryTransport, FailureCompileInvalidRequest, "A blob size is invalid.", false, nil)
		return 0, &failure
	}
	if value > math.MaxInt64 {
		failure := protocolFailure(protocolcommon.ProtocolFailureCategoryResource, FailureCompileBlobOversized, "A blob exceeds the supported signed byte range.", false, nil)
		return 0, &failure
	}
	return int64(value), nil
}

func saturatedAdd(left, right int64) int64 {
	if right > math.MaxInt64-left {
		return math.MaxInt64
	}
	return left + right
}

func resourceFailure(code, resource string, limit, observed int64) *protocolcommon.ProtocolFailure {
	details := protocolcommon.JsonObject{
		"resource": stringJSON(resource),
		"limit":    stringJSON(strconv.FormatInt(limit, 10)),
		"observed": stringJSON(strconv.FormatInt(observed, 10)),
	}
	failure := protocolFailure(protocolcommon.ProtocolFailureCategoryResource, code, "Compilation exceeded an effective resource limit.", false, details)
	return &failure
}

type blobLease struct {
	once        sync.Once
	definitions []BlobDefinition
	failure     *protocolcommon.ProtocolFailure
}

func (lease *blobLease) Release(ctx context.Context) *protocolcommon.ProtocolFailure {
	if lease == nil {
		return nil
	}
	lease.once.Do(func() {
		for index := range lease.definitions {
			definition := lease.definitions[index]
			if definition.Reader != nil {
				if releaseErr := safeBlobRelease(definition.Reader.Close); releaseErr != nil && lease.failure == nil {
					lease.failure = blobSourceFailure(ctx, releaseErr)
				}
			}
			if definition.Owned != nil && definition.Owned.Release != nil {
				if releaseErr := safeBlobRelease(func() error { definition.Owned.Release(); return nil }); releaseErr != nil && lease.failure == nil {
					lease.failure = blobSourceFailure(ctx, releaseErr)
				}
			}
		}
		lease.definitions = nil
	})
	return lease.failure
}

func safeBlobRelease(release func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("blob release panic")
		}
	}()
	return release()
}

func acquireBlobUses(ctx context.Context, uses []blobUse, source BlobSource) (owned map[string][]byte, lease *blobLease, failure *protocolcommon.ProtocolFailure) {
	definitions, err := source.Definitions(ctx)
	if err != nil {
		return nil, nil, blobSourceFailure(ctx, err)
	}
	lease = &blobLease{definitions: definitions}
	defer func() {
		if failure != nil {
			_ = lease.Release(ctx)
			lease = nil
			owned = nil
		}
	}()

	definitionIDs := make([]string, len(definitions))
	for index, definition := range definitions {
		if definition.BlobID == "" || (definition.Reader == nil) == (definition.Owned == nil) {
			result := protocolFailure(protocolcommon.ProtocolFailureCategoryTransport, FailureCompileBlobSource, "The request blob source is invalid.", false, nil)
			return nil, lease, &result
		}
		definitionIDs[index] = definition.BlobID
	}
	slices.Sort(definitionIDs)
	for index := 1; index < len(definitionIDs); index++ {
		if definitionIDs[index] == definitionIDs[index-1] {
			result := protocolFailure(protocolcommon.ProtocolFailureCategoryTransport, FailureCompileDuplicateBlob, "The request contains duplicate blob definitions.", false, nil)
			return nil, lease, &result
		}
	}

	uniqueUses := slices.Clone(uses)
	sort.SliceStable(uniqueUses, func(i, j int) bool { return uniqueUses[i].ref.BlobID < uniqueUses[j].ref.BlobID })
	uniqueUses = slices.CompactFunc(uniqueUses, func(a, b blobUse) bool { return a.ref.BlobID == b.ref.BlobID })
	for definitionIndex, useIndex := 0, 0; definitionIndex < len(definitionIDs) || useIndex < len(uniqueUses); {
		if definitionIndex == len(definitionIDs) || useIndex < len(uniqueUses) && uniqueUses[useIndex].ref.BlobID < definitionIDs[definitionIndex] {
			result := protocolFailure(protocolcommon.ProtocolFailureCategoryTransport, FailureCompileMissingBlob, "A required request blob is missing.", false, nil)
			return nil, lease, &result
		}
		if useIndex == len(uniqueUses) || definitionIDs[definitionIndex] < uniqueUses[useIndex].ref.BlobID {
			result := protocolFailure(protocolcommon.ProtocolFailureCategoryTransport, FailureCompileUnexpectedBlob, "The request contains an unreferenced blob definition.", false, nil)
			return nil, lease, &result
		}
		definitionIndex++
		useIndex++
	}

	// Maps are constructed only after all duplicate, missing, and unreferenced
	// definitions and conflicting reference aliases have been enumerated.
	byID := make(map[string]BlobDefinition, len(definitions))
	for _, definition := range definitions {
		byID[definition.BlobID] = definition
	}
	owned = make(map[string][]byte, len(uniqueUses))
	for _, use := range uniqueUses {
		if ctx.Err() != nil {
			return nil, lease, cancelledProtocolFailure()
		}
		definition := byID[use.ref.BlobID]
		expected, sizeFailure := blobSize(use.ref)
		if sizeFailure != nil {
			return nil, lease, sizeFailure
		}
		var value []byte
		if definition.Owned != nil {
			value = definition.Owned.Bytes
		} else {
			var readErr error
			value, readErr = readBounded(ctx, definition.Reader, expected)
			if readErr != nil {
				return nil, lease, blobSourceFailure(ctx, readErr)
			}
		}
		if int64(len(value)) != expected {
			result := protocolFailure(protocolcommon.ProtocolFailureCategoryTransport, FailureCompileBlobSizeMismatch, "A request blob does not match its declared size.", false, nil)
			return nil, lease, &result
		}
		digest := sha256.Sum256(value)
		actualDigest := "sha256:" + hex.EncodeToString(digest[:])
		if actualDigest != string(use.ref.Digest) {
			result := protocolFailure(protocolcommon.ProtocolFailureCategoryTransport, FailureCompileBlobDigestMismatch, "A request blob does not match its declared digest.", false, nil)
			return nil, lease, &result
		}
		owned[use.ref.BlobID] = value
	}
	return owned, lease, nil
}

func resolveBlobUses(ctx context.Context, uses []blobUse, source BlobSource) (map[string][]byte, *protocolcommon.ProtocolFailure) {
	owned, lease, failure := acquireBlobUses(ctx, uses, source)
	if failure != nil {
		return nil, failure
	}
	if releaseFailure := lease.Release(ctx); releaseFailure != nil {
		return nil, releaseFailure
	}
	return owned, nil
}

func readBounded(ctx context.Context, reader io.Reader, expected int64) ([]byte, error) {
	if expected < 0 || expected == math.MaxInt64 {
		return nil, fmt.Errorf("unsupported blob size")
	}
	limit := expected + 1
	buffer := bytes.NewBuffer(make([]byte, 0, min(expected, 32<<10)))
	temporary := make([]byte, 32<<10)
	for int64(buffer.Len()) < limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := limit - int64(buffer.Len())
		chunk := temporary
		if int64(len(chunk)) > remaining {
			chunk = chunk[:remaining]
		}
		count, err := reader.Read(chunk)
		if count > 0 {
			_, _ = buffer.Write(chunk[:count])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		if count == 0 {
			return nil, io.ErrNoProgress
		}
	}
	return buffer.Bytes(), nil
}

func blobSourceFailure(ctx context.Context, err error) *protocolcommon.ProtocolFailure {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return cancelledProtocolFailure()
	}
	failure := protocolFailure(protocolcommon.ProtocolFailureCategoryIo, FailureCompileBlobSource, "The request blobs could not be read.", true, nil)
	return &failure
}

func cancelledProtocolFailure() *protocolcommon.ProtocolFailure {
	failure := protocolFailure(protocolcommon.ProtocolFailureCategoryCancelled, FailureCompileCancelled, "Compilation was cancelled.", true, nil)
	return &failure
}
