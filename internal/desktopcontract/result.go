// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopcontract

// Outcome is transport-neutral and preserves cancellation separately from
// failure. Wails bindings must not reconstruct it from thrown exceptions.
type Outcome string

const (
	OutcomeSuccess   Outcome = "success"
	OutcomeCancelled Outcome = "cancelled"
	OutcomeFailed    Outcome = "failed"
)

type FailureCode string

const (
	FailureStartup              FailureCode = "desktop.startup_failed"
	FailureShutdown             FailureCode = "desktop.shutdown_failed"
	FailureCredential           FailureCode = "desktop.credential_unavailable"
	FailureLocalActor           FailureCode = "desktop.local_actor_unavailable"
	FailureAgentDelegation      FailureCode = "desktop.agent_delegation_failed"
	FailureMCPTransport         FailureCode = "desktop.mcp_transport_failed"
	FailureDialogCancelled      FailureCode = "desktop.native_dialog_cancelled"
	FailureBackendPanic         FailureCode = "desktop.backend_panic"
	FailureReconnect            FailureCode = "desktop.reconnect_failed"
	FailureAdapterUnavailable   FailureCode = "desktop.adapter_unavailable"
	FailureProtocolIncompatible FailureCode = "desktop.protocol_incompatible"
)

// Failure contains only stable, non-secret fields. Credentials, document
// bytes, OS paths, panic values, and provider error text never cross bindings.
type Failure struct {
	Code      FailureCode `json:"code"`
	Retryable bool        `json:"retryable"`
	Component ComponentID `json:"component,omitempty"`
	Recovery  string      `json:"recovery,omitempty"`
}

type Result[T any] struct {
	Outcome Outcome  `json:"outcome"`
	Value   *T       `json:"value,omitempty"`
	Failure *Failure `json:"failure,omitempty"`
}

func (r Result[T]) Validate() bool {
	switch r.Outcome {
	case OutcomeSuccess:
		return r.Value != nil && r.Failure == nil
	case OutcomeCancelled:
		return r.Value == nil && r.Failure != nil && r.Failure.Code == FailureDialogCancelled
	case OutcomeFailed:
		return r.Value == nil && r.Failure != nil && r.Failure.Code != FailureDialogCancelled
	default:
		return false
	}
}
