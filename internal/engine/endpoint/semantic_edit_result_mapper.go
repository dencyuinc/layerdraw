// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

type SemanticPreviewIdentity struct {
	BaseGeneration     engineprotocol.DocumentGeneration
	PreviewID          engineprotocol.PreviewID
	ProposedGeneration engineprotocol.DocumentGeneration
}

// MapSemanticEditPlanResult is the generated-contract boundary for the
// planner's complete result. It validates the generated result and accounts
// for both encoded result bytes and every referenced replacement attachment.
func MapSemanticEditPlanResult(plan engine.SemanticEditPlan, identity SemanticPreviewIdentity, limits engine.SemanticPlanLimits) (engineprotocol.WorkbenchPreviewResult, []OutputBlob, error) {
	result := engineprotocol.WorkbenchPreviewResult{Status: plan.Status, BaseGeneration: identity.BaseGeneration, ChangedSourceFiles: []semantic.ModuleRef{}, Conflicts: []engineprotocol.SemanticConflict{}, Diagnostics: []semantic.Diagnostic{}}
	for _, path := range plan.ChangedSourceFiles {
		result.ChangedSourceFiles = append(result.ChangedSourceFiles, generatedModuleRef(engine.PlannedModuleRef{OriginKind: engine.SourceOriginProject, ModulePath: path}))
	}
	result.SourceDiff = engineprotocol.SourceDiff{Digest: protocolcommon.Digest(plan.SourceDiff.Digest), Edits: make([]engineprotocol.SourceEdit, 0, len(plan.SourceDiff.Edits))}
	blobs := make([]OutputBlob, 0)
	blobByID := map[string]OutputBlob{}
	for _, edit := range plan.SourceDiff.Edits {
		mapped := engineprotocol.SourceEdit{Kind: engineprotocol.SourceEditKind(edit.Kind)}
		if edit.BeforeModule != nil {
			value := generatedModuleRef(*edit.BeforeModule)
			mapped.BeforeModule = &value
		}
		if edit.AfterModule != nil {
			value := generatedModuleRef(*edit.AfterModule)
			mapped.AfterModule = &value
		}
		if edit.SourceRange != nil {
			value, err := mapSourceRange(*edit.SourceRange)
			if err != nil {
				return result, nil, err
			}
			mapped.SourceRange = &value
		}
		if edit.BeforeDigest != "" {
			value := protocolcommon.Digest(edit.BeforeDigest)
			mapped.BeforeDigest = &value
		}
		if edit.AfterDigest != "" {
			value := protocolcommon.Digest(edit.AfterDigest)
			mapped.AfterDigest = &value
		}
		if edit.ReplacementBlob != nil {
			ref := protocolcommon.BlobRef{BlobID: edit.ReplacementBlob.BlobID, Digest: protocolcommon.Digest(edit.ReplacementBlob.Digest), Lifetime: protocolcommon.BlobLifetime(edit.ReplacementBlob.Lifetime), MediaType: edit.ReplacementBlob.MediaType, Size: protocolcommon.CanonicalUint64(strconv.FormatUint(edit.ReplacementBlob.Size, 10))}
			mapped.ReplacementBlob = &ref
			blob := OutputBlob{Ref: ref, Bytes: append([]byte(nil), edit.ReplacementBlob.Bytes...)}
			if existing, ok := blobByID[ref.BlobID]; ok {
				if existing.Ref != blob.Ref || !bytes.Equal(existing.Bytes, blob.Bytes) {
					return result, nil, fmt.Errorf("replacement blob ID %q has conflicting definitions", ref.BlobID)
				}
			} else {
				blobByID[ref.BlobID] = blob
				blobs = append(blobs, blob)
			}
		}
		result.SourceDiff.Edits = append(result.SourceDiff.Edits, mapped)
	}
	result.SemanticDiff = semantic.SemanticDiff{Digest: protocolcommon.Digest(plan.SemanticDiff.Digest), Entries: make([]semantic.SemanticDiffEntry, 0, len(plan.SemanticDiff.Entries))}
	for _, entry := range plan.SemanticDiff.Entries {
		mapped := semantic.SemanticDiffEntry{Kind: semantic.SemanticChangeKind(entry.Kind), SubjectKind: semantic.SubjectKind(entry.SubjectKind), ChangedFieldPaths: generatedFieldPaths(entry.ChangedFieldPaths)}
		if entry.BeforeAddress != "" {
			address, digest := semantic.StableAddress(entry.BeforeAddress), protocolcommon.Digest(entry.BeforeHash)
			mapped.BeforeAddress, mapped.BeforeHash = &address, &digest
		}
		if entry.AfterAddress != "" {
			address, digest := semantic.StableAddress(entry.AfterAddress), protocolcommon.Digest(entry.AfterHash)
			mapped.AfterAddress, mapped.AfterHash = &address, &digest
		}
		if entry.OwnerAddress != "" {
			address := semantic.StableAddress(entry.OwnerAddress)
			mapped.OwnerAddress = &address
		}
		result.SemanticDiff.Entries = append(result.SemanticDiff.Entries, mapped)
	}
	mappedDiagnostics, err := mapDiagnostics(plan.Diagnostics)
	if err != nil {
		return result, nil, err
	}
	result.Diagnostics = mappedDiagnostics
	for _, conflict := range plan.Conflicts {
		mapped := engineprotocol.SemanticConflict{Kind: string(conflict.Kind)}
		if conflict.TargetAddress != "" {
			value := semantic.StableAddress(conflict.TargetAddress)
			mapped.TargetAddress = &value
		}
		if conflict.OwnerAddress != "" {
			value := semantic.StableAddress(conflict.OwnerAddress)
			mapped.OwnerAddress = &value
		}
		if conflict.ChildKind != "" {
			value := semantic.SubjectKind(conflict.ChildKind)
			mapped.ChildKind = &value
		}
		if conflict.Path != nil {
			value := append([]string(nil), conflict.Path...)
			mapped.Path = &value
		}
		result.Conflicts = append(result.Conflicts, mapped)
	}
	if plan.Status == "valid" {
		if plan.AuthoringImpact == nil || plan.Result == nil {
			return result, nil, fmt.Errorf("valid semantic plan is incomplete")
		}
		impact, mapErr := generatedAuthoringImpact(*plan.AuthoringImpact)
		if mapErr != nil {
			return result, nil, mapErr
		}
		result.AuthoringImpact = &impact
		impactDigest := protocolcommon.Digest(plan.AuthoringImpact.ImpactDigest)
		result.AuthoringImpactDigest = &impactDigest
		capabilities := make([]semantic.AuthoringCapability, len(plan.AuthoringImpact.RequiredCapabilities))
		for index, capability := range plan.AuthoringImpact.RequiredCapabilities {
			capabilities[index] = semantic.AuthoringCapability(capability)
		}
		result.RequiredAuthoringCapabilities = &capabilities
		result.PreviewID, result.ProposedGeneration = &identity.PreviewID, &identity.ProposedGeneration
		resulting := generatedResultingHashes(*plan.Result)
		result.ResultingHashes = &resulting
		previewDigest := deterministicPreviewDigest(plan, identity)
		result.PreviewDigest = &previewDigest
	}
	encoded, err := engineprotocol.EncodeWorkbenchPreviewResult(result)
	if err != nil {
		return result, nil, fmt.Errorf("encode semantic preview result: %w", err)
	}
	if err := validateUniqueOutputBlobs(blobs); err != nil {
		return result, nil, fmt.Errorf("validate semantic preview output blobs: %w", err)
	}
	items := semanticPreviewLogicalItems(result)
	if limits.MaxItems > 0 && int64(items) > limits.MaxItems {
		return result, nil, fmt.Errorf("semantic preview item limit exceeded: limit=%d observed=%d", limits.MaxItems, items)
	}
	logicalBytes := int64(len(encoded))
	for _, blob := range blobs {
		logicalBytes += int64(len(blob.Bytes))
	}
	if limits.MaxOutputBytes > 0 && logicalBytes > limits.MaxOutputBytes {
		return result, nil, fmt.Errorf("semantic preview output limit exceeded: limit=%d observed=%d", limits.MaxOutputBytes, logicalBytes)
	}
	return result, blobs, nil
}

func semanticPreviewLogicalItems(result engineprotocol.WorkbenchPreviewResult) int {
	items := len(result.ChangedSourceFiles) + len(result.SourceDiff.Edits) + len(result.SemanticDiff.Entries) + len(result.Conflicts) + len(result.Diagnostics)
	for _, entry := range result.SemanticDiff.Entries {
		items += len(entry.ChangedFieldPaths)
		for _, path := range entry.ChangedFieldPaths {
			items += len(path.Tokens)
		}
	}
	for _, conflict := range result.Conflicts {
		if conflict.Path != nil {
			items += len(*conflict.Path)
		}
	}
	for _, diagnostic := range result.Diagnostics {
		items += len(diagnostic.Arguments) + len(diagnostic.Related)
	}
	if result.AuthoringImpact != nil {
		items += len(result.AuthoringImpact.Entries) + len(result.AuthoringImpact.RequiredCapabilities)
		for _, entry := range result.AuthoringImpact.Entries {
			items += len(entry.BeforeRefs) + len(entry.AfterRefs) + len(entry.SourceRefs) + len(entry.ChangedFieldPaths)
			for _, path := range entry.ChangedFieldPaths {
				items += len(path.Tokens)
			}
			if entry.GraphFacts != nil {
				items += len(entry.GraphFacts.ActionFlags) + len(entry.GraphFacts.ColumnAddresses) + len(entry.GraphFacts.EndpointEntityAddresses) + len(entry.GraphFacts.EntityTypeAddresses) + len(entry.GraphFacts.LayerAddresses) + len(entry.GraphFacts.RelationTypeAddresses)
			}
		}
	}
	if result.ResultingHashes != nil {
		items += len(result.ResultingHashes.SubjectHashes) + len(result.ResultingHashes.SubtreeHashes) + len(result.ResultingHashes.ChildSetHashes)
		for _, childSet := range result.ResultingHashes.ChildSetHashes {
			items += len(childSet.ChildAddresses)
		}
	}
	return items
}

func generatedModuleRef(value engine.PlannedModuleRef) semantic.ModuleRef {
	origin := semantic.SourceOrigin{Kind: semantic.OriginKind(value.OriginKind)}
	if value.PackAddress != "" {
		address := semantic.PackRootAddress(value.PackAddress)
		origin.PackAddress = &address
	}
	return semantic.ModuleRef{Origin: origin, ModulePath: value.ModulePath}
}

func generatedFieldPaths(values []engine.AuthoredFieldPath) []semantic.AuthoredFieldPath {
	out := make([]semantic.AuthoredFieldPath, len(values))
	for index, value := range values {
		out[index] = semantic.AuthoredFieldPath{Tokens: append([]string(nil), value.Tokens...)}
	}
	return out
}

func generatedAuthoringImpact(value engine.PlannedAuthoringImpact) (semantic.AuthoringImpact, error) {
	out := semantic.AuthoringImpact{BaseDefinitionHash: protocolcommon.Digest(value.BaseDefinitionHash), ResultingDefinitionHash: protocolcommon.Digest(value.ResultingDefinitionHash), SemanticDiffHash: protocolcommon.Digest(value.SemanticDiffHash), SourceDiffHash: protocolcommon.Digest(value.SourceDiffHash), ImpactDigest: protocolcommon.Digest(value.ImpactDigest), Entries: make([]semantic.AuthoringImpactEntry, 0, len(value.Entries)), RequiredCapabilities: make([]semantic.AuthoringCapability, len(value.RequiredCapabilities))}
	for index, capability := range value.RequiredCapabilities {
		out.RequiredCapabilities[index] = semantic.AuthoringCapability(capability)
	}
	for _, entry := range value.Entries {
		mapped := semantic.AuthoringImpactEntry{Action: semantic.AuthoringAction(entry.Action), Capability: semantic.AuthoringCapability(entry.Capability), SubjectKind: semantic.SubjectKind(entry.SubjectKind), ChangedFieldPaths: generatedFieldPaths(entry.ChangedFieldPaths), BeforeRefs: stableAddresses(entry.BeforeRefs), AfterRefs: stableAddresses(entry.AfterRefs), SourceRefs: []semantic.SourceRange{}}
		if entry.SubjectAddress != "" {
			address := semantic.StableAddress(entry.SubjectAddress)
			mapped.SubjectAddress = &address
		}
		if entry.OwnerAddress != "" {
			address := semantic.StableAddress(entry.OwnerAddress)
			mapped.OwnerAddress = &address
		}
		for _, source := range entry.SourceRefs {
			mappedSource, err := mapSourceRange(source)
			if err != nil {
				return out, err
			}
			mapped.SourceRefs = append(mapped.SourceRefs, mappedSource)
		}
		if entry.GraphFacts != nil {
			facts := semantic.GraphAuthoringFacts{ActionFlags: append([]string(nil), entry.GraphFacts.ActionFlags...), ColumnAddresses: columnAddresses(entry.GraphFacts.ColumnAddresses), EndpointEntityAddresses: entityAddresses(entry.GraphFacts.EndpointEntityAddresses), EntityTypeAddresses: entityTypeAddresses(entry.GraphFacts.EntityTypeAddresses), LayerAddresses: layerAddresses(entry.GraphFacts.LayerAddresses), RelationTypeAddresses: relationTypeAddresses(entry.GraphFacts.RelationTypeAddresses)}
			mapped.GraphFacts = &facts
		}
		out.Entries = append(out.Entries, mapped)
	}
	return out, nil
}

func columnAddresses(values []string) []semantic.ColumnAddress {
	out := make([]semantic.ColumnAddress, len(values))
	for index, value := range values {
		out[index] = semantic.ColumnAddress(value)
	}
	return out
}
func entityAddresses(values []string) []semantic.EntityAddress {
	out := make([]semantic.EntityAddress, len(values))
	for index, value := range values {
		out[index] = semantic.EntityAddress(value)
	}
	return out
}
func entityTypeAddresses(values []string) []semantic.EntityTypeAddress {
	out := make([]semantic.EntityTypeAddress, len(values))
	for index, value := range values {
		out[index] = semantic.EntityTypeAddress(value)
	}
	return out
}
func layerAddresses(values []string) []semantic.LayerAddress {
	out := make([]semantic.LayerAddress, len(values))
	for index, value := range values {
		out[index] = semantic.LayerAddress(value)
	}
	return out
}
func relationTypeAddresses(values []string) []semantic.RelationTypeAddress {
	out := make([]semantic.RelationTypeAddress, len(values))
	for index, value := range values {
		out[index] = semantic.RelationTypeAddress(value)
	}
	return out
}

func generatedResultingHashes(snapshot engine.Snapshot) engineprotocol.ResultingHashes {
	out := engineprotocol.ResultingHashes{Mode: engineprotocol.CompileMode(snapshot.Mode), DefinitionHash: protocolcommon.Digest(snapshot.DefinitionHash), SubjectHashes: make([]semantic.SubjectHash, len(snapshot.SubjectSemanticHashes)), SubtreeHashes: make([]semantic.SubtreeHash, len(snapshot.SubtreeHashes)), ChildSetHashes: make([]semantic.ChildSetHash, len(snapshot.ChildSetHashes))}
	if snapshot.GraphHash != nil {
		value := protocolcommon.Digest(*snapshot.GraphHash)
		out.GraphHash = &value
	}
	if snapshot.NormalizedDocument != nil {
		value := semantic.ProjectRootAddress(snapshot.NormalizedDocument.Project.Address)
		out.ProjectAddress = &value
	}
	for index, value := range snapshot.SubjectSemanticHashes {
		out.SubjectHashes[index] = semantic.SubjectHash{Address: semantic.StableAddress(value.Address), Kind: semantic.SubjectKind(value.Kind), Hash: protocolcommon.Digest(value.Hash)}
	}
	for index, value := range snapshot.SubtreeHashes {
		out.SubtreeHashes[index] = semantic.SubtreeHash{OwnerAddress: semantic.StableAddress(value.OwnerAddress), Hash: protocolcommon.Digest(value.Hash)}
	}
	for index, value := range snapshot.ChildSetHashes {
		out.ChildSetHashes[index] = semantic.ChildSetHash{OwnerAddress: semantic.StableAddress(value.OwnerAddress), ChildKind: semantic.SubjectKind(value.ChildKind), ChildAddresses: stableAddresses(value.Addresses), Hash: protocolcommon.Digest(value.Hash)}
	}
	return out
}

func deterministicPreviewDigest(plan engine.SemanticEditPlan, identity SemanticPreviewIdentity) protocolcommon.Digest {
	sum := sha256.Sum256([]byte(plan.SourceDiff.Digest + "\x00" + plan.SemanticDiff.Digest + "\x00" + plan.AuthoringImpact.ImpactDigest + "\x00" + string(identity.BaseGeneration.Value)))
	return protocolcommon.Digest("sha256:" + hex.EncodeToString(sum[:]))
}
