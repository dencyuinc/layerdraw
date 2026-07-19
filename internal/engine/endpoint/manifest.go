// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

func (d *Descriptor) capabilityManifest(clientScoped bool, selected protocolBinding, effective engine.ResourceLimits) (protocolcommon.CapabilityManifest, error) {
	compileLimits := operationLimits(effective)
	manifestScope := protocolcommon.ManifestScopeEndpoint
	if clientScoped {
		manifestScope = protocolcommon.ManifestScopeEffective
	}
	manifest := protocolcommon.CapabilityManifest{
		EmbeddingProfiles:         []protocolcommon.ProfileCapability{},
		ExporterProfiles:          []protocolcommon.ProfileCapability{},
		Limits:                    manifestLimits(d.limits, effective),
		ManifestEtag:              protocolcommon.ManifestETag("sha256:0000000000000000000000000000000000000000000000000000000000000000"),
		ManifestScope:             manifestScope,
		ManifestVersion:           1,
		Operations:                operationCapabilities(selected.wireVersion, compileLimits),
		QueryAdapters:             []protocolcommon.ProfileCapability{},
		RealtimeProfiles:          []protocolcommon.ProfileCapability{},
		RegistrySources:           []protocolcommon.ProfileCapability{},
		RendererProfiles:          []protocolcommon.ProfileCapability{},
		RequiredLadybugPrimitives: []string{},
		SearchProfiles:            []protocolcommon.ProfileCapability{},
		StorageCapabilities:       []protocolcommon.ProfileCapability{},
		Transports:                append([]string(nil), d.transports...),
	}
	etag, err := manifestETag(manifest)
	if err != nil {
		return protocolcommon.CapabilityManifest{}, err
	}
	manifest.ManifestEtag = etag
	if _, err := protocolcommon.EncodeCapabilityManifest(manifest); err != nil {
		return protocolcommon.CapabilityManifest{}, fmt.Errorf("constructed capability manifest is invalid: %w", err)
	}
	return manifest, nil
}

func operationCapabilities(version protocolcommon.ProtocolVersion, compileLimits protocolcommon.CompileResourceLimitConstraints) map[string]protocolcommon.OperationCapability {
	operations := map[string]protocolcommon.OperationCapability{}
	for _, operation := range []string{
		OperationApplyToHandle,
		OperationCloseDocument,
		OperationCompile,
		OperationExecuteQuery,
		OperationFindSymbols,
		OperationFindUsages,
		OperationFormatScope,
		OperationGetNeighbors,
		OperationHandshake,
		OperationInspectSubgraph,
		OperationListModules,
		OperationListReferences,
		OperationMaterializeView,
		OperationPlanExport,
		OperationOpenDocument,
		OperationOrganizeWorkspace,
		OperationPreviewFragment,
		OperationPreviewOperations,
		OperationPreviewSourcePatch,
		OperationReadDeclarations,
		OperationReadModules,
		OperationReadReferences,
		OperationReadRows,
		OperationReadScope,
		OperationReplaceSourceTree,
	} {
		capability := protocolcommon.OperationCapability{Enabled: true, ProtocolVersion: version}
		if operation == OperationCompile {
			capability.Limits = &compileLimits
		}
		operations[operation] = capability
	}
	return operations
}

func manifestETag(manifest protocolcommon.CapabilityManifest) (protocolcommon.ManifestETag, error) {
	encoded, err := protocolcommon.EncodeCapabilityManifest(manifest)
	if err != nil {
		return "", fmt.Errorf("encode capability manifest projection: %w", err)
	}
	projection := map[string]json.RawMessage{}
	if err := json.Unmarshal(encoded, &projection); err != nil {
		return "", fmt.Errorf("decode capability manifest projection: %w", err)
	}
	delete(projection, "manifest_etag")
	canonical, err := json.Marshal(projection)
	if err != nil {
		return "", fmt.Errorf("canonicalize capability manifest projection: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return protocolcommon.ManifestETag("sha256:" + hex.EncodeToString(digest[:])), nil
}
