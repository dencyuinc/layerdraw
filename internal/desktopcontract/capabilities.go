// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopcontract

import (
	"errors"
	"fmt"
	"sort"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
)

type ComponentID string

const (
	ComponentEngine            ComponentID = "engine"
	ComponentRuntime           ComponentID = "runtime"
	ComponentAccess            ComponentID = "access"
	ComponentNativeQuery       ComponentID = "native_query_search_analysis"
	ComponentSearchIndex       ComponentID = "search_index"
	ComponentEmbeddingProvider ComponentID = "embedding_provider"
	ComponentRegistryClient    ComponentID = "registry_client"
	ComponentReview            ComponentID = "review"
	ComponentNativeExporters   ComponentID = "native_exporters"
	ComponentMCPHost           ComponentID = "mcp_host"
	ComponentLocalStorage      ComponentID = "local_storage"
	ComponentExternalStorage   ComponentID = "external_storage"
	ComponentBindingShell      ComponentID = "wails_binding_shell"
)

var desktopBackendClosure = []ComponentID{
	ComponentEngine, ComponentRuntime, ComponentAccess, ComponentNativeQuery,
	ComponentSearchIndex, ComponentEmbeddingProvider, ComponentRegistryClient,
	ComponentReview, ComponentNativeExporters, ComponentMCPHost,
	ComponentLocalStorage, ComponentExternalStorage, ComponentBindingShell,
}

type FrontendAssetID string

const (
	AssetProtocol           FrontendAssetID = "@layerdraw/protocol"
	AssetEngineClientWails  FrontendAssetID = "@layerdraw/engine-client/wails"
	AssetRegistryClientHost FrontendAssetID = "@layerdraw/registry-client/host"
	AssetComposer           FrontendAssetID = "@layerdraw/composer"
	AssetRender             FrontendAssetID = "@layerdraw/render"
	AssetExport             FrontendAssetID = "@layerdraw/export"
	AssetViewer             FrontendAssetID = "@layerdraw/viewer"
	AssetReview             FrontendAssetID = "@layerdraw/review"
	AssetReact              FrontendAssetID = "@layerdraw/react"
	AssetLibrary            FrontendAssetID = "@layerdraw/library"
)

var desktopFrontendClosure = []FrontendAssetID{
	AssetProtocol, AssetEngineClientWails, AssetRegistryClientHost,
	AssetComposer, AssetRender, AssetExport, AssetViewer, AssetReview,
	AssetReact, AssetLibrary,
}

const DesktopProtocolVersion protocolcommon.ProtocolVersion = "1.0"

const (
	CapabilityAuthoring       protocolcommon.CapabilityID = "desktop.authoring"
	CapabilityQuery           protocolcommon.CapabilityID = "desktop.query"
	CapabilitySearch          protocolcommon.CapabilityID = "desktop.search"
	CapabilityAnalysis        protocolcommon.CapabilityID = "desktop.analysis"
	CapabilityRegistry        protocolcommon.CapabilityID = "desktop.registry"
	CapabilityReview          protocolcommon.CapabilityID = "desktop.review"
	CapabilityExport          protocolcommon.CapabilityID = "desktop.export"
	CapabilityExternalStorage protocolcommon.CapabilityID = "desktop.external_storage"
	CapabilityMCPTools        protocolcommon.CapabilityID = "desktop.mcp.tools"
	CapabilityMCPResources    protocolcommon.CapabilityID = "desktop.mcp.resources"
	CapabilityAgentScope      protocolcommon.CapabilityID = "desktop.agent_scope_management"
)

var requiredCapabilities = []protocolcommon.CapabilityID{
	CapabilityAuthoring, CapabilityQuery, CapabilitySearch, CapabilityAnalysis,
	CapabilityRegistry, CapabilityReview, CapabilityExport, CapabilityMCPTools,
	CapabilityMCPResources, CapabilityAgentScope,
}

var optionalCapabilities = []protocolcommon.CapabilityID{CapabilityExternalStorage}

// Manifest freezes bundle closure and canonical handshake requirements. The
// effective availability itself is read from generated HandshakeResult.
type Manifest struct {
	Version              uint32                        `json:"version"`
	Components           []ComponentID                 `json:"components"`
	Frontend             []FrontendAssetID             `json:"frontend_assets"`
	RequiredCapabilities []protocolcommon.CapabilityID `json:"required_capabilities"`
	OptionalCapabilities []protocolcommon.CapabilityID `json:"optional_capabilities"`
}

func RequiredBackendClosure() []ComponentID {
	return append([]ComponentID(nil), desktopBackendClosure...)
}
func RequiredFrontendClosure() []FrontendAssetID {
	return append([]FrontendAssetID(nil), desktopFrontendClosure...)
}

func DefaultManifest() Manifest {
	return Manifest{
		Version: 1, Components: RequiredBackendClosure(), Frontend: RequiredFrontendClosure(),
		RequiredCapabilities: append([]protocolcommon.CapabilityID(nil), requiredCapabilities...),
		OptionalCapabilities: append([]protocolcommon.CapabilityID(nil), optionalCapabilities...),
	}
}

func (m Manifest) Validate() error {
	if m.Version != 1 {
		return errors.New("desktop contract: unsupported manifest version")
	}
	if err := exactSet("backend component", m.Components, desktopBackendClosure); err != nil {
		return err
	}
	if err := exactSet("frontend asset", m.Frontend, desktopFrontendClosure); err != nil {
		return err
	}
	if err := exactSet("required capability", m.RequiredCapabilities, requiredCapabilities); err != nil {
		return err
	}
	if err := exactSet("optional capability", m.OptionalCapabilities, optionalCapabilities); err != nil {
		return err
	}
	seen := map[protocolcommon.CapabilityID]bool{}
	for _, id := range append(append([]protocolcommon.CapabilityID(nil), m.RequiredCapabilities...), m.OptionalCapabilities...) {
		if seen[id] {
			return fmt.Errorf("desktop contract: duplicate capability %q", id)
		}
		if _, err := protocolcommon.EncodeCapabilityID(id); err != nil {
			return fmt.Errorf("desktop contract: invalid capability %q", id)
		}
		seen[id] = true
	}
	return nil
}

// NegotiateCapabilities accepts only the generated handshake result and
// publishes a generated-codec deep copy. No status or version is reconstructed.
func NegotiateCapabilities(manifest Manifest, handshake protocolcommon.HandshakeResult) Result[protocolcommon.HandshakeResult] {
	if err := manifest.Validate(); err != nil {
		return failedHandshake(FailureProtocolIncompatible, ComponentBindingShell, RecoveryUpgrade)
	}
	encoded, err := protocolcommon.EncodeHandshakeResult(handshake)
	if err != nil {
		return failedHandshake(FailureProtocolIncompatible, ComponentBindingShell, RecoveryUpgrade)
	}
	clone, err := protocolcommon.DecodeHandshakeResult(encoded)
	if err != nil {
		return failedHandshake(FailureProtocolIncompatible, ComponentBindingShell, RecoveryUpgrade)
	}
	if len(clone.NegotiatedProtocols) == 0 {
		return failedHandshake(FailureProtocolIncompatible, ComponentBindingShell, RecoveryUpgrade)
	}
	for _, negotiated := range clone.NegotiatedProtocols {
		if negotiated.Version != DesktopProtocolVersion {
			return failedHandshake(FailureProtocolIncompatible, ComponentBindingShell, RecoveryUpgrade)
		}
	}
	want := append(append([]protocolcommon.CapabilityID(nil), manifest.RequiredCapabilities...), manifest.OptionalCapabilities...)
	if len(clone.CapabilityStatuses) != len(want) {
		return failedHandshake(FailureProtocolIncompatible, ComponentBindingShell, RecoveryUpgrade)
	}
	statuses := make(map[protocolcommon.CapabilityID]protocolcommon.RequestedCapabilityStatus, len(want))
	for _, status := range clone.CapabilityStatuses {
		if status.ProtocolVersion != DesktopProtocolVersion || statuses[status.CapabilityID].CapabilityID != "" {
			return failedHandshake(FailureProtocolIncompatible, ComponentBindingShell, RecoveryUpgrade)
		}
		statuses[status.CapabilityID] = status
	}
	for _, id := range want {
		if statuses[id].CapabilityID == "" {
			return failedHandshake(FailureProtocolIncompatible, ComponentBindingShell, RecoveryUpgrade)
		}
	}
	for _, id := range manifest.RequiredCapabilities {
		if !statuses[id].Enabled {
			return failedHandshake(FailureAdapterUnavailable, ComponentBindingShell, RecoveryConfigureAdapter)
		}
	}
	return Result[protocolcommon.HandshakeResult]{Outcome: protocolcommon.OutcomeSuccess, Value: clone}
}

func failedHandshake(code FailureCode, component ComponentID, recovery RecoveryAction) Result[protocolcommon.HandshakeResult] {
	return Result[protocolcommon.HandshakeResult]{Outcome: protocolcommon.OutcomeFailed, Failure: &Failure{Code: code, Component: component, Recovery: recovery}}
}

type ordered interface{ ~string }

func exactSet[T ordered](kind string, got, want []T) error {
	if len(got) != len(want) {
		return fmt.Errorf("desktop contract: %s closure is incomplete", kind)
	}
	gotCopy, wantCopy := append([]T(nil), got...), append([]T(nil), want...)
	sort.Slice(gotCopy, func(i, j int) bool { return gotCopy[i] < gotCopy[j] })
	sort.Slice(wantCopy, func(i, j int) bool { return wantCopy[i] < wantCopy[j] })
	for index := range wantCopy {
		if gotCopy[index] != wantCopy[index] {
			return fmt.Errorf("desktop contract: unexpected %s %q", kind, gotCopy[index])
		}
	}
	return nil
}
