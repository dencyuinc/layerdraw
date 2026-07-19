// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
)

const RegistryWireVersion = "1.0"

type WireOperation string

const (
	WireListSources        WireOperation = "registry.list_sources"
	WireConfigureSource    WireOperation = "registry.configure_source"
	WireConnectSource      WireOperation = "registry.connect_source"
	WireDisconnectSource   WireOperation = "registry.disconnect_source"
	WireSearch             WireOperation = "registry.search"
	WirePlan               WireOperation = "registry.plan_install"
	WireCommit             WireOperation = "registry.commit_plan"
	WireGetTransaction     WireOperation = "registry.get_transaction"
	WireRecoverTransaction WireOperation = "registry.recover_transaction"
	WireAuthorArtifact     WireOperation = "registry.author_artifact"
)

type WireRequest struct {
	WireVersion string          `json:"wire_version"`
	Operation   WireOperation   `json:"operation"`
	RequestID   string          `json:"request_id"`
	Input       json.RawMessage `json:"input"`
}
type WireFailure struct {
	Code       string `json:"code"`
	Subject    string `json:"subject"`
	Actionable bool   `json:"actionable"`
}
type WireResponse struct {
	WireVersion string          `json:"wire_version"`
	Operation   WireOperation   `json:"operation"`
	RequestID   string          `json:"request_id"`
	OK          bool            `json:"ok"`
	Value       json.RawMessage `json:"value,omitempty"`
	Failure     *WireFailure    `json:"failure,omitempty"`
}
type ConfigureSourceInput struct {
	Source RegistrySource `json:"source"`
}
type RegistryConnectionInput struct {
	SourceID      string `json:"source_id"`
	ConnectionRef string `json:"connection_ref"`
}
type SourceIDInput struct {
	SourceID string `json:"source_id"`
}
type TransactionIDInput struct {
	TransactionID string `json:"transaction_id"`
}
type WireCommitInput struct {
	TransactionID  string `json:"transaction_id"`
	PlanDigest     string `json:"plan_digest"`
	OperationID    string `json:"operation_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

// DecodeWireRequest strictly validates the versioned Registry host envelope
// and its exact expected operation without executing Registry semantics. It is
// the mechanical binding guard shared by Wails and other framework shells.
func DecodeWireRequest(wire []byte, expected WireOperation) (WireRequest, error) {
	var request WireRequest
	if err := decodeStrict(wire, &request); err != nil {
		return WireRequest{}, err
	}
	if request.WireVersion != RegistryWireVersion || request.RequestID == "" || !validWireOperation(request.Operation) || request.Operation != expected {
		return WireRequest{}, errors.New("invalid Registry wire request binding")
	}
	return request, nil
}

// DecodeWireResponse is the response-side companion to DecodeWireRequest. It
// rejects cross-operation responses and ambiguous success/failure shapes so a
// framework adapter cannot normalize unvalidated Registry output.
func DecodeWireResponse(wire []byte, expected WireOperation) (WireResponse, error) {
	var response WireResponse
	if err := decodeStrict(wire, &response); err != nil {
		return WireResponse{}, err
	}
	if response.WireVersion != RegistryWireVersion || response.RequestID == "" || !validWireOperation(response.Operation) || response.Operation != expected {
		return WireResponse{}, errors.New("invalid Registry wire response binding")
	}
	if response.OK {
		if response.Failure != nil || len(response.Value) == 0 || string(response.Value) == "null" {
			return WireResponse{}, errors.New("invalid Registry success response")
		}
	} else if response.Failure == nil || len(response.Value) != 0 {
		return WireResponse{}, errors.New("invalid Registry failure response")
	}
	return response, nil
}

// HostBinding is the single strict, versioned Registry dispatcher used by all
// framework shells. Wails and other transports move bytes only.
type HostBinding struct{ registry *Registry }

func NewHostBinding(registry *Registry) (*HostBinding, error) {
	if registry == nil {
		return nil, fail(FailureUnavailable, "registry_binding", true, nil)
	}
	return &HostBinding{registry: registry}, nil
}

func (h *HostBinding) Dispatch(ctx context.Context, wire []byte) []byte {
	var request WireRequest
	if err := decodeStrict(wire, &request); err != nil {
		return encodeWireFailure(WireOperation("registry.invalid"), "", fail(FailureUnsupportedFormat, "wire_request", true, err))
	}
	if request.WireVersion != RegistryWireVersion || request.RequestID == "" || !validWireOperation(request.Operation) {
		return encodeWireFailure(request.Operation, request.RequestID, fail(FailureUnsupportedFormat, "wire_request", true, nil))
	}
	var value any
	var err error
	switch request.Operation {
	case WireListSources:
		var input struct{}
		err = decodeStrict(request.Input, &input)
		if err == nil {
			value = h.registry.Sources()
		}
	case WireConfigureSource:
		var input ConfigureSourceInput
		err = decodeStrict(request.Input, &input)
		if err == nil {
			err = h.registry.ConfigureSource(input.Source)
			value, _ = h.registry.getSource(input.Source.SourceID)
		}
	case WireConnectSource:
		var input RegistryConnectionInput
		err = decodeStrict(request.Input, &input)
		if err == nil {
			err = h.registry.ConnectSource(ctx, input.SourceID, input.ConnectionRef)
			value, _ = h.registry.getSource(input.SourceID)
		}
	case WireDisconnectSource:
		var input SourceIDInput
		err = decodeStrict(request.Input, &input)
		if err == nil {
			err = h.registry.DisconnectSource(input.SourceID)
			value, _ = h.registry.getSource(input.SourceID)
		}
	case WireSearch:
		var input SearchInput
		err = decodeStrict(request.Input, &input)
		if err == nil {
			value, err = h.registry.Search(ctx, input)
		}
	case WirePlan:
		var input PlanRequest
		err = decodeStrict(request.Input, &input)
		if err == nil {
			value, err = h.registry.Plan(ctx, input)
		}
	case WireCommit:
		var input WireCommitInput
		err = decodeStrict(request.Input, &input)
		if err == nil {
			var tx Transaction
			tx, err = h.registry.GetTransaction(ctx, input.TransactionID)
			if err == nil && tx.Plan.PlanDigest != input.PlanDigest {
				err = fail(FailurePlanStale, input.TransactionID, true, nil)
			}
			if err == nil {
				value, err = h.registry.Commit(ctx, RuntimeCommitInput{Plan: tx.Plan, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey})
			}
		}
	case WireGetTransaction:
		var input TransactionIDInput
		err = decodeStrict(request.Input, &input)
		if err == nil {
			value, err = h.registry.GetTransaction(ctx, input.TransactionID)
		}
	case WireRecoverTransaction:
		var input TransactionIDInput
		err = decodeStrict(request.Input, &input)
		if err == nil {
			value, err = h.registry.RecoverTransaction(ctx, input.TransactionID)
		}
	case WireAuthorArtifact:
		var input AuthorArtifactRequest
		err = decodeStrict(request.Input, &input)
		if err == nil {
			var authored AuthoredArtifact
			authored, err = h.registry.AuthorArtifact(ctx, input)
			value = authored.Release
		}
	}
	if err != nil {
		return encodeWireFailure(request.Operation, request.RequestID, err)
	}
	encoded, marshalErr := json.Marshal(value)
	if marshalErr != nil {
		return encodeWireFailure(request.Operation, request.RequestID, fail(FailureUnavailable, "wire_response", true, marshalErr))
	}
	response, _ := json.Marshal(WireResponse{WireVersion: RegistryWireVersion, Operation: request.Operation, RequestID: request.RequestID, OK: true, Value: encoded})
	return response
}
func validWireOperation(value WireOperation) bool {
	switch value {
	case WireListSources, WireConfigureSource, WireConnectSource, WireDisconnectSource, WireSearch, WirePlan, WireCommit, WireGetTransaction, WireRecoverTransaction, WireAuthorArtifact:
		return true
	default:
		return false
	}
}
func encodeWireFailure(operation WireOperation, requestID string, err error) []byte {
	failure := WireFailure{Code: FailureUnavailable, Subject: "registry", Actionable: true}
	var registryFailure *Failure
	if errors.As(err, &registryFailure) {
		failure = WireFailure{Code: registryFailure.Code, Subject: registryFailure.Subject, Actionable: registryFailure.Actionable}
	}
	encoded, _ := json.Marshal(WireResponse{WireVersion: RegistryWireVersion, Operation: operation, RequestID: requestID, OK: false, Failure: &failure})
	return encoded
}
func decodeStrict(data []byte, output any) error {
	if len(data) == 0 {
		return errors.New("empty Registry wire JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing Registry wire JSON")
	}
	return nil
}
