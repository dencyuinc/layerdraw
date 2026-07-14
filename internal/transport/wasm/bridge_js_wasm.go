// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build js && wasm

package wasm

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"syscall/js"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
)

const GlobalBridgeName = "__layerdrawEngineWasmV1"

type StaticConfig struct {
	EngineRelease  string
	SourceRevision string
}

type jsBridge struct {
	static    StaticConfig
	session   *Session
	callbacks []js.Func
}

// Run registers the versioned synchronous bridge and retains it for the
// lifetime of the dedicated Worker. The Worker owner terminates the runtime
// after dispose or hard cancellation.
func Run(config StaticConfig) {
	bridge := &jsBridge{static: config}
	bridge.register()
	select {}
}

func (bridge *jsBridge) register() {
	initialize := bridge.callback(PhaseInitialization, bridge.initialize)
	request := bridge.callback(PhaseRequest, bridge.request)
	dispose := bridge.callback(PhaseLifecycle, bridge.dispose)
	bridge.callbacks = []js.Func{initialize, request, dispose}
	api := js.Global().Get("Object").New()
	api.Set("initialize", initialize)
	api.Set("request", request)
	api.Set("dispose", dispose)
	api.Set("workerProtocol", WorkerProtocol)
	api.Set("workerProtocolVersion", WorkerProtocolVersion)
	js.Global().Set(GlobalBridgeName, api)
}

func (bridge *jsBridge) callback(phase string, handler func([]js.Value) any) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) (result any) {
		defer func() {
			if recover() != nil {
				if bridge.session != nil {
					bridge.session.markCrashed()
				}
				result = failureValue(localFailure(FailureCrashed, phase, true))
			}
		}()
		return handler(args)
	})
}

func (bridge *jsBridge) initialize(args []js.Value) any {
	if len(args) != 3 || bridge.session != nil {
		return failureValue(localFailure(FailureInitializationFailed, PhaseInitialization, false))
	}
	generation, endpointID, releaseDigest, ok := threeStrings(args)
	if !ok || !validGeneration(generation) {
		return failureValue(localFailure(FailureInitializationFailed, PhaseInitialization, false))
	}
	authority, err := endpoint.NewCompilerEndpoint(endpoint.CompilerEndpointConfig{
		EngineRelease:         bridge.static.EngineRelease,
		SourceRevision:        bridge.static.SourceRevision,
		ReleaseManifestDigest: releaseDigest,
		EndpointInstanceID:    endpointID,
		Transports:            []string{TransportID},
		Limits:                BrowserCompilerLimitPolicy(),
	})
	if err != nil {
		return failureValue(localFailure(FailureInitializationFailed, PhaseInitialization, false))
	}
	session, err := NewSession(authority, generation, BrowserTransportLimits())
	if err != nil {
		return failureValue(localFailure(FailureInitializationFailed, PhaseInitialization, false))
	}
	bridge.session = session
	result := js.Global().Get("Object").New()
	result.Set("ok", true)
	result.Set("endpoint_generation", generation)
	result.Set("protocol_schema_digest", engineprotocol.SchemaDigest)
	result.Set("transport_limits", limitsValue(session.Limits()))
	return result
}

func (bridge *jsBridge) request(args []js.Value) any {
	if bridge.session == nil {
		return failureValue(localFailure(FailureInitializationFailed, PhaseInitialization, false))
	}
	if len(args) != 4 || args[0].Type() != js.TypeString || !exactArrayBuffer(args[1]) || !arrayValue(args[2]) || !arrayValue(args[3]) {
		return failureValue(localFailure(FailureMalformedMessage, PhaseRequest, false))
	}
	generation := args[0].String()
	if failure := bridge.session.PreflightGeneration(generation); failure != nil {
		return failureValue(failure)
	}
	ids, buffers, failure := decodeBlobArguments(args[2], args[3], bridge.session.Limits())
	if failure != nil {
		return failureValue(failure)
	}
	control, failure := copyArrayBufferToGo(args[1], bridge.session.Limits().MaxControlBytes)
	if failure != nil {
		return failureValue(failure)
	}
	table := &jsBlobTable{ids: ids, buffers: buffers, lengths: bufferLengths(buffers)}
	response, failure := bridge.session.Dispatch(context.Background(), generation, control, table)
	control = nil
	if failure != nil {
		return failureValue(failure)
	}
	defer response.Release()
	controlBuffer, copyFailure := copyGoBytesToArrayBuffer(response.Control)
	if copyFailure != nil {
		return failureValue(copyFailure)
	}
	blobIDs := js.Global().Get("Array").New(len(response.Blobs))
	blobBuffers := js.Global().Get("Array").New(len(response.Blobs))
	for index, blob := range response.Blobs {
		buffer, blobFailure := copyGoBytesToArrayBuffer(blob.Bytes)
		if blobFailure != nil {
			return failureValue(blobFailure)
		}
		blobIDs.SetIndex(index, blob.BlobID)
		blobBuffers.SetIndex(index, buffer)
	}
	result := js.Global().Get("Object").New()
	result.Set("ok", true)
	result.Set("endpoint_generation", response.EndpointGeneration)
	result.Set("control", controlBuffer)
	result.Set("blob_ids", blobIDs)
	result.Set("blobs", blobBuffers)
	return result
}

func (bridge *jsBridge) dispose(args []js.Value) any {
	if len(args) != 1 || args[0].Type() != js.TypeString {
		return failureValue(localFailure(FailureMalformedMessage, PhaseLifecycle, false))
	}
	if bridge.session == nil {
		return failureValue(localFailure(FailureDisposed, PhaseLifecycle, false))
	}
	if failure := bridge.session.Dispose(args[0].String()); failure != nil {
		return failureValue(failure)
	}
	result := js.Global().Get("Object").New()
	result.Set("ok", true)
	return result
}

func threeStrings(args []js.Value) (string, string, string, bool) {
	for _, value := range args {
		if value.Type() != js.TypeString {
			return "", "", "", false
		}
	}
	return args[0].String(), args[1].String(), args[2].String(), true
}

func exactArrayBuffer(value js.Value) bool {
	if value.Type() != js.TypeObject {
		return false
	}
	arrayBuffer := js.Global().Get("ArrayBuffer")
	if arrayBuffer.Type() != js.TypeFunction || !value.InstanceOf(arrayBuffer) {
		return false
	}
	prototype := js.Global().Get("Object").Call("getPrototypeOf", value)
	if !prototype.Equal(arrayBuffer.Get("prototype")) {
		return false
	}
	resizable := value.Get("resizable")
	return resizable.Type() != js.TypeBoolean || !resizable.Bool()
}

func arrayValue(value js.Value) bool {
	return value.Type() == js.TypeObject && js.Global().Get("Array").Call("isArray", value).Bool()
}

func decodeBlobArguments(idsValue, buffersValue js.Value, limits TransportLimits) ([]string, []js.Value, *LocalFailure) {
	if idsValue.Length() != buffersValue.Length() || int64(idsValue.Length()) > limits.MaxBuffers {
		return nil, nil, localFailure(FailureMalformedMessage, PhaseRequest, false)
	}
	ids := make([]string, idsValue.Length())
	buffers := make([]js.Value, buffersValue.Length())
	var total int64
	for index := range ids {
		idValue, buffer := idsValue.Index(index), buffersValue.Index(index)
		if idValue.Type() != js.TypeString || !exactArrayBuffer(buffer) {
			return nil, nil, localFailure(FailureMalformedMessage, PhaseRequest, false)
		}
		id := idValue.String()
		if id == "" || !utf8.ValidString(id) || int64(len(id)) > limits.MaxBlobIDBytes {
			return nil, nil, localFailure(FailureMalformedMessage, PhaseRequest, false)
		}
		length := int64(buffer.Get("byteLength").Int())
		if length < 0 || length > limits.MaxInputBlobBytes || total > limits.MaxInputTotalBytes-length {
			return nil, nil, localFailure(FailureTransferFailed, PhaseTransfer, false)
		}
		total += length
		ids[index], buffers[index] = id, buffer
	}
	return ids, buffers, nil
}

func bufferLengths(values []js.Value) []int {
	result := make([]int, len(values))
	for index, value := range values {
		result[index] = value.Get("byteLength").Int()
	}
	return result
}

func copyArrayBufferToGo(value js.Value, maximum int64) ([]byte, *LocalFailure) {
	length := value.Get("byteLength").Int()
	if length < 0 || int64(length) > maximum {
		return nil, localFailure(FailureTransferFailed, PhaseTransfer, false)
	}
	result := make([]byte, length)
	view := js.Global().Get("Uint8Array").New(value)
	if copied := js.CopyBytesToGo(result, view); copied != length {
		return nil, localFailure(FailureTransferFailed, PhaseTransfer, true)
	}
	return result, nil
}

func copyGoBytesToArrayBuffer(value []byte) (js.Value, *LocalFailure) {
	buffer := js.Global().Get("ArrayBuffer").New(len(value))
	view := js.Global().Get("Uint8Array").New(buffer)
	if copied := js.CopyBytesToJS(view, value); copied != len(value) {
		return js.Undefined(), localFailure(FailureTransferFailed, PhaseTransfer, true)
	}
	return buffer, nil
}

type jsBlobTable struct {
	ids      []string
	buffers  []js.Value
	lengths  []int
	bound    bool
	released bool
}

func (table *jsBlobTable) Count() int { return len(table.ids) }

func (table *jsBlobTable) Bind(_ []endpoint.BlobRequirement, limits TransportLimits) *LocalFailure {
	if table == nil || table.released || table.bound || int64(len(table.ids)) > limits.MaxBuffers {
		return localFailure(FailureMalformedMessage, PhaseRequest, false)
	}
	var total int64
	for _, length := range table.lengths {
		if length < 0 || int64(length) > limits.MaxInputBlobBytes || total > limits.MaxInputTotalBytes-int64(length) {
			return localFailure(FailureTransferFailed, PhaseTransfer, false)
		}
		total += int64(length)
	}
	table.bound = true
	return nil
}

func (table *jsBlobTable) Definitions(ctx context.Context) (definitions []endpoint.BlobDefinition, err error) {
	if table == nil || !table.bound || table.released {
		return nil, errors.New("unbound request blob table")
	}
	defer func() {
		if recover() != nil {
			definitions = nil
			err = errors.New("request blob transfer failed")
		}
	}()
	definitions = make([]endpoint.BlobDefinition, len(table.ids))
	for index, id := range table.ids {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		bytes, failure := copyArrayBufferToGo(table.buffers[index], int64(table.lengths[index]))
		if failure != nil {
			return nil, errors.New("request blob transfer failed")
		}
		definitions[index] = endpoint.BlobDefinition{
			BlobID: id,
			Owned:  &endpoint.OwnedBlob{Bytes: bytes},
		}
	}
	return definitions, nil
}

func (table *jsBlobTable) Release() {
	if table == nil || table.released {
		return
	}
	table.released = true
	table.ids = nil
	table.buffers = nil
	table.lengths = nil
}

func limitsValue(limits TransportLimits) js.Value {
	result := js.Global().Get("Object").New()
	result.Set("max_control_bytes", limits.MaxControlBytes)
	result.Set("max_control_depth", limits.MaxControlDepth)
	result.Set("max_blob_id_bytes", limits.MaxBlobIDBytes)
	result.Set("max_buffers", limits.MaxBuffers)
	result.Set("max_input_blob_bytes", limits.MaxInputBlobBytes)
	result.Set("max_input_total_bytes", limits.MaxInputTotalBytes)
	result.Set("max_output_blob_bytes", limits.MaxOutputBlobBytes)
	result.Set("max_output_total_bytes", limits.MaxOutputTotalBytes)
	result.Set("max_response_publish_bytes", limits.MaxResponsePublishBytes)
	return result
}

func failureValue(failure *LocalFailure) js.Value {
	if failure == nil {
		panic(fmt.Errorf("nil local failure"))
	}
	failureObject := js.Global().Get("Object").New()
	failureObject.Set("code", failure.Code)
	failureObject.Set("phase", failure.Phase)
	failureObject.Set("retryable", failure.Retryable)
	result := js.Global().Get("Object").New()
	result.Set("ok", false)
	result.Set("failure", failureObject)
	return result
}
