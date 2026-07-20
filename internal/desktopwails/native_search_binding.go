// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package desktopwails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/host"
)

var nativeSearchOperations = map[string]string{
	"native.execute_search":   host.OperationSearch,
	"native.execute_query":    host.OperationExecuteQuery,
	"native.execute_analysis": host.OperationAnalyzeGraph,
}

func nativeSearchOperation(operation string) (string, bool) {
	mapped, ok := nativeSearchOperations[operation]
	return mapped, ok
}

func invokePackagedNativeSearch(ctx context.Context, endpoint *host.Endpoint, exchange desktopcontract.Exchange) (desktopcontract.ExchangeResult, error, bool) {
	mapped, ok := nativeSearchOperation(exchange.Operation)
	if !ok {
		return desktopcontract.ExchangeResult{}, nil, false
	}
	result, err := invokeMappedSearch(ctx, endpoint, exchange, mapped)
	return result, err, true
}

func packagedNativeSearchDecoder() (any, bool) { return nativeSearchDecoder{}, true }

type nativeSearchEnvelope struct {
	Operation string                             `json:"operation"`
	Payload   json.RawMessage                    `json:"payload"`
	Protocol  runtimeprotocol.RuntimeProtocolRef `json:"protocol"`
	RequestID string                             `json:"request_id"`
}

type nativeSearchResponseEnvelope struct {
	Operation   string                             `json:"operation"`
	Protocol    runtimeprotocol.RuntimeProtocolRef `json:"protocol"`
	RequestID   string                             `json:"request_id"`
	HostRelease protocolcommon.ReleaseVersion      `json:"host_release"`
	Outcome     protocolcommon.Outcome             `json:"outcome"`
	Payload     json.RawMessage                    `json:"payload,omitempty"`
	Failure     *protocolcommon.ProtocolFailure    `json:"failure,omitempty"`
}

func invokeMappedSearch(ctx context.Context, endpoint *host.Endpoint, exchange desktopcontract.Exchange, mapped string) (desktopcontract.ExchangeResult, error) {
	request, err := decodeNativeSearchEnvelope(exchange.Control, exchange.Operation)
	if err != nil {
		return desktopcontract.ExchangeResult{}, err
	}
	request.Operation = mapped
	control, err := json.Marshal(request)
	if err != nil {
		return desktopcontract.ExchangeResult{}, err
	}
	plan, terminal, err := endpoint.Prepare(ctx, mapped, control)
	if err != nil {
		return desktopcontract.ExchangeResult{}, err
	}
	var response engineendpoint.DispatchResponse
	if terminal != nil {
		response = *terminal
	} else {
		sink := &exchangeBlobSink{}
		response, err = plan.ExecuteDispatch(ctx, exchangeBlobSource(exchange.Blobs), sink)
		if err != nil {
			return desktopcontract.ExchangeResult{}, err
		}
		exchange.Blobs = sink.blobs
	}
	var mappedResponse nativeSearchResponseEnvelope
	if err := decodeExactBytes(response.Control, &mappedResponse); err != nil || mappedResponse.Operation != mapped || mappedResponse.RequestID != request.RequestID {
		return desktopcontract.ExchangeResult{}, errors.New("native Search owner response mismatch")
	}
	mappedResponse.Operation = exchange.Operation
	encoded, err := json.Marshal(mappedResponse)
	if err != nil {
		return desktopcontract.ExchangeResult{}, err
	}
	return desktopcontract.ExchangeResult{Operation: exchange.Operation, Control: encoded, Blobs: exchange.Blobs}, nil
}

func decodeNativeSearchEnvelope(control []byte, expected string) (nativeSearchEnvelope, error) {
	var value nativeSearchEnvelope
	if err := decodeExactBytes(control, &value); err != nil || value.Operation != expected || value.RequestID == "" || len(value.Payload) == 0 || !json.Valid(value.Payload) || value.Protocol.Name != runtimeprotocol.RuntimeProtocolRefNameValue || value.Protocol.Version != "1.0" {
		return nativeSearchEnvelope{}, errors.New("native Search owner request mismatch")
	}
	return value, nil
}

func decodeExactBytes(value []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("owner envelope has trailing content")
	}
	return nil
}

type nativeSearchDecoder struct{}

func (nativeSearchDecoder) DecodeRequest(expected string, control []byte) (desktopcontract.OwnerEnvelopeIdentity, error) {
	value, err := decodeNativeSearchEnvelope(control, expected)
	if err != nil {
		return desktopcontract.OwnerEnvelopeIdentity{}, err
	}
	return desktopcontract.OwnerEnvelopeIdentity{Operation: value.Operation, RequestID: value.RequestID}, nil
}

func (nativeSearchDecoder) DecodeResponse(expected string, control []byte) (desktopcontract.OwnerResponseIdentity, error) {
	var value nativeSearchResponseEnvelope
	if err := decodeExactBytes(control, &value); err != nil || value.Operation != expected || value.RequestID == "" {
		return desktopcontract.OwnerResponseIdentity{}, errors.New("native Search owner response mismatch")
	}
	switch value.Outcome {
	case protocolcommon.OutcomeSuccess, protocolcommon.OutcomeRejected, protocolcommon.OutcomeFailed, protocolcommon.OutcomeCancelled:
	default:
		return desktopcontract.OwnerResponseIdentity{}, errors.New("native Search owner outcome is invalid")
	}
	return desktopcontract.OwnerResponseIdentity{Operation: value.Operation, RequestID: value.RequestID, Outcome: string(value.Outcome)}, nil
}
