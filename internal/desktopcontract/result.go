// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopcontract

import "github.com/dencyuinc/layerdraw/gen/go/protocolcommon"

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

type RecoveryAction string

const (
	RecoveryRetry            RecoveryAction = "retry"
	RecoveryReconnect        RecoveryAction = "reconnect"
	RecoveryConfigureAdapter RecoveryAction = "configure_adapter"
	RecoveryOpenRecovery     RecoveryAction = "open_recovery"
	RecoveryUpgrade          RecoveryAction = "upgrade"
	RecoveryExit             RecoveryAction = "exit"
)

// Failure is a closed shell failure. It has no arbitrary message, details,
// path, provider text, panic value, or credential surface.
type Failure struct {
	Code      FailureCode    `json:"code"`
	Retryable bool           `json:"retryable"`
	Component ComponentID    `json:"component"`
	Recovery  RecoveryAction `json:"recovery"`
}

func (f Failure) Validate() bool {
	return validFailureCode(f.Code) && validComponent(f.Component) && validRecovery(f.Recovery)
}

func validFailureCode(value FailureCode) bool {
	switch value {
	case FailureStartup, FailureShutdown, FailureCredential, FailureLocalActor,
		FailureAgentDelegation, FailureMCPTransport, FailureDialogCancelled,
		FailureBackendPanic, FailureReconnect, FailureAdapterUnavailable,
		FailureProtocolIncompatible:
		return true
	default:
		return false
	}
}

func validComponent(value ComponentID) bool {
	for _, candidate := range desktopBackendClosure {
		if value == candidate {
			return true
		}
	}
	return false
}

func validRecovery(value RecoveryAction) bool {
	switch value {
	case RecoveryRetry, RecoveryReconnect, RecoveryConfigureAdapter,
		RecoveryOpenRecovery, RecoveryUpgrade, RecoveryExit:
		return true
	default:
		return false
	}
}

// Result preserves the generated common outcome vocabulary, including
// rejected. Value is by-value; owners of slice-bearing generated values must
// publish a codec round-tripped copy.
type Result[T any] struct {
	Outcome protocolcommon.Outcome `json:"outcome"`
	Value   T                      `json:"value,omitempty"`
	Failure *Failure               `json:"failure,omitempty"`
}

func (r Result[T]) Validate() bool {
	switch r.Outcome {
	case protocolcommon.OutcomeSuccess, protocolcommon.OutcomeRejected:
		return r.Failure == nil
	case protocolcommon.OutcomeCancelled:
		return r.Failure != nil && r.Failure.Validate() && r.Failure.Code == FailureDialogCancelled
	case protocolcommon.OutcomeFailed:
		return r.Failure != nil && r.Failure.Validate() && r.Failure.Code != FailureDialogCancelled
	default:
		return false
	}
}
