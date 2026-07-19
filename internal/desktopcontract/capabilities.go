// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopcontract

import (
	"errors"
	"fmt"
	"sort"
)

// ComponentID is a linked Desktop backend component, not a feature inferred
// from the presence of a generated method.
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

// FrontendAssetID is a package embedded into the Desktop React asset closure.
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

// CapabilityID identifies a user-observable negotiated Desktop capability.
type CapabilityID string

const (
	CapabilityAuthoring       CapabilityID = "desktop.authoring"
	CapabilityQuery           CapabilityID = "desktop.query"
	CapabilitySearch          CapabilityID = "desktop.search"
	CapabilityAnalysis        CapabilityID = "desktop.analysis"
	CapabilityRegistry        CapabilityID = "desktop.registry"
	CapabilityReview          CapabilityID = "desktop.review"
	CapabilityExport          CapabilityID = "desktop.export"
	CapabilityExternalStorage CapabilityID = "desktop.external_storage"
	CapabilityMCPTools        CapabilityID = "desktop.mcp.tools"
	CapabilityMCPResources    CapabilityID = "desktop.mcp.resources"
	CapabilityAgentScope      CapabilityID = "desktop.agent_scope_management"
)

type Requirement string

const (
	Required Requirement = "required"
	Optional Requirement = "optional"
)

// CapabilityRequirement is supplied by the frontend during startup. Required
// capabilities fail negotiation; optional capabilities remain typed disabled.
type CapabilityRequirement struct {
	ID          CapabilityID `json:"capability_id"`
	Requirement Requirement  `json:"requirement"`
}

type Availability string

const (
	Available   Availability = "available"
	Unavailable Availability = "unavailable"
)

type UnavailableReason string

const (
	UnavailableAdapter       UnavailableReason = "adapter_unavailable"
	UnavailableConfiguration UnavailableReason = "not_configured"
	UnavailableCredential    UnavailableReason = "credential_unavailable"
	UnavailablePolicy        UnavailableReason = "policy_denied"
	UnavailableProtocol      UnavailableReason = "protocol_incompatible"
)

type CapabilityStatus struct {
	ID                CapabilityID       `json:"capability_id"`
	Availability      Availability       `json:"availability"`
	ProtocolVersion   string             `json:"protocol_version"`
	UnavailableReason *UnavailableReason `json:"unavailable_reason,omitempty"`
}

// NegotiateCapabilities validates an effective status for every requested
// capability. A missing or unavailable required capability fails closed;
// optional capabilities remain visible with a typed unavailable reason.
func NegotiateCapabilities(manifest Manifest, statuses []CapabilityStatus) Result[[]CapabilityStatus] {
	if err := manifest.Validate(); err != nil {
		return failedCapabilities(FailureProtocolIncompatible)
	}
	if len(statuses) != len(manifest.Capabilities) {
		return failedCapabilities(FailureProtocolIncompatible)
	}
	seen := make(map[CapabilityID]CapabilityStatus, len(statuses))
	for _, status := range statuses {
		if _, duplicate := seen[status.ID]; duplicate || status.ProtocolVersion == "" {
			return failedCapabilities(FailureProtocolIncompatible)
		}
		switch status.Availability {
		case Available:
			if status.UnavailableReason != nil {
				return failedCapabilities(FailureProtocolIncompatible)
			}
		case Unavailable:
			if status.UnavailableReason == nil {
				return failedCapabilities(FailureProtocolIncompatible)
			}
		default:
			return failedCapabilities(FailureProtocolIncompatible)
		}
		seen[status.ID] = status
	}
	for _, requested := range manifest.Capabilities {
		status, exists := seen[requested.ID]
		if !exists {
			return failedCapabilities(FailureProtocolIncompatible)
		}
		if requested.Requirement == Required && status.Availability != Available {
			return failedCapabilities(FailureAdapterUnavailable)
		}
	}
	copy := append([]CapabilityStatus(nil), statuses...)
	return Result[[]CapabilityStatus]{Outcome: OutcomeSuccess, Value: &copy}
}

func failedCapabilities(code FailureCode) Result[[]CapabilityStatus] {
	return Result[[]CapabilityStatus]{Outcome: OutcomeFailed, Failure: &Failure{Code: code}}
}

// Manifest freezes bundle closure separately from effective capability status.
// Linking code is not sufficient to mark a capability available.
type Manifest struct {
	Version      uint32                  `json:"version"`
	Components   []ComponentID           `json:"components"`
	Frontend     []FrontendAssetID       `json:"frontend_assets"`
	Capabilities []CapabilityRequirement `json:"capabilities"`
}

func RequiredBackendClosure() []ComponentID {
	return append([]ComponentID(nil), desktopBackendClosure...)
}

func RequiredFrontendClosure() []FrontendAssetID {
	return append([]FrontendAssetID(nil), desktopFrontendClosure...)
}

func DefaultManifest() Manifest {
	return Manifest{
		Version:    1,
		Components: RequiredBackendClosure(),
		Frontend:   RequiredFrontendClosure(),
		Capabilities: []CapabilityRequirement{
			{CapabilityAuthoring, Required}, {CapabilityQuery, Required},
			{CapabilitySearch, Required}, {CapabilityAnalysis, Required},
			{CapabilityRegistry, Required}, {CapabilityReview, Required},
			{CapabilityExport, Required}, {CapabilityExternalStorage, Optional},
			{CapabilityMCPTools, Required}, {CapabilityMCPResources, Required},
			{CapabilityAgentScope, Required},
		},
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
	want := DefaultManifest().Capabilities
	if len(m.Capabilities) != len(want) {
		return errors.New("desktop contract: capability closure is incomplete")
	}
	seen := map[CapabilityID]Requirement{}
	for _, capability := range m.Capabilities {
		if capability.Requirement != Required && capability.Requirement != Optional {
			return fmt.Errorf("desktop contract: invalid requirement for %q", capability.ID)
		}
		if _, duplicate := seen[capability.ID]; duplicate {
			return fmt.Errorf("desktop contract: duplicate capability %q", capability.ID)
		}
		seen[capability.ID] = capability.Requirement
	}
	for _, capability := range want {
		if seen[capability.ID] != capability.Requirement {
			return fmt.Errorf("desktop contract: capability %q requirement changed", capability.ID)
		}
	}
	return nil
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
