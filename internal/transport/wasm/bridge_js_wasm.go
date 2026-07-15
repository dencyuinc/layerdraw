// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build js && wasm

package wasm

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sort"
	"strconv"
	"unicode/utf8"

	"syscall/js"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
)

const GlobalBridgeName = "__layerdrawEngineWasmV1"

type StaticConfig struct {
	EngineRelease       string
	SourceRevision      string
	SBOMAuthorityDigest string
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
	initialize := bridge.callback(bridge.initialize)
	request := bridge.callback(bridge.request)
	dispose := bridge.callback(bridge.dispose)
	bridge.callbacks = []js.Func{initialize, request, dispose}
	api := js.Global().Get("Object").New()
	api.Set("initialize", initialize)
	api.Set("request", request)
	api.Set("dispose", dispose)
	api.Set("workerProtocol", WorkerProtocol)
	api.Set("workerProtocolVersion", WorkerProtocolVersion)
	js.Global().Set(GlobalBridgeName, api)
}

func (bridge *jsBridge) callback(handler func([]js.Value) any) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) (result any) {
		defer func() {
			if recover() != nil {
				if bridge.session != nil {
					bridge.session.markCrashed()
				}
				result = failureValue(localFailure(FailureCrashed))
			}
		}()
		return handler(args)
	})
}

func (bridge *jsBridge) initialize(args []js.Value) any {
	if len(args) != 2 || bridge.session != nil {
		return failureValue(localFailure(FailureInitializationFailed))
	}
	generation, releaseDigest, ok := twoStrings(args)
	if !ok || !validOpaqueString(generation, 128) {
		return failureValue(localFailure(FailureInitializationFailed))
	}
	endpointID, err := newEndpointInstanceID()
	if err != nil {
		return failureValue(localFailure(FailureInitializationFailed))
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
		return failureValue(localFailure(FailureInitializationFailed))
	}
	session, err := NewSession(authority, generation, BrowserTransportLimits())
	if err != nil {
		return failureValue(localFailure(FailureInitializationFailed))
	}
	goVersion, modules, ok := linkedBuildInfo()
	if !ok {
		return failureValue(localFailure(FailureInitializationFailed))
	}
	bridge.session = session
	result := js.Global().Get("Object").New()
	result.Set("ok", true)
	result.Set("endpoint_generation", generation)
	result.Set("engine_release", bridge.static.EngineRelease)
	result.Set("source_revision", bridge.static.SourceRevision)
	result.Set("protocol_schema_digest", engineprotocol.SchemaDigest)
	result.Set("go_version", goVersion)
	result.Set("module_build_info", moduleBuildInfoValue(modules))
	result.Set("sbom_authority_digest", bridge.static.SBOMAuthorityDigest)
	result.Set("transport_limits", limitsValue(session.Limits()))
	return result
}

type linkedModule struct {
	Path    string
	Version string
}

func linkedBuildInfo() (string, []linkedModule, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.GoVersion == "" {
		return "", nil, false
	}
	modules := make([]linkedModule, 0, len(info.Deps))
	seen := make(map[string]bool, len(info.Deps))
	for _, dependency := range info.Deps {
		if dependency == nil {
			return "", nil, false
		}
		module := dependency
		if dependency.Replace != nil {
			module = dependency.Replace
		}
		if module.Path == "" || module.Version == "" {
			return "", nil, false
		}
		key := module.Path + "@" + module.Version
		if seen[key] {
			continue
		}
		seen[key] = true
		modules = append(modules, linkedModule{Path: module.Path, Version: module.Version})
	}
	sort.Slice(modules, func(left, right int) bool {
		return modules[left].Path+"@"+modules[left].Version < modules[right].Path+"@"+modules[right].Version
	})
	return info.GoVersion, modules, true
}

func moduleBuildInfoValue(modules []linkedModule) js.Value {
	result := js.Global().Get("Array").New(len(modules))
	for index, module := range modules {
		value := js.Global().Get("Object").New()
		value.Set("path", module.Path)
		value.Set("version", module.Version)
		result.SetIndex(index, value)
	}
	return result
}

func (bridge *jsBridge) request(args []js.Value) any {
	if bridge.session == nil {
		return failureValue(localFailure(FailureInitializationFailed))
	}
	if len(args) != 4 || args[0].Type() != js.TypeString || !exactArrayBuffer(args[1]) || !arrayValue(args[2]) || !arrayValue(args[3]) {
		return failureValue(localFailure(FailureMalformedMessage))
	}
	generation := args[0].String()
	if failure := bridge.session.PreflightGeneration(generation); failure != nil {
		return failureValue(failure)
	}
	ids, buffers, failure := decodeBlobArguments(args[1], args[2], args[3], bridge.session.Limits())
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
		return failureValue(localFailure(FailureMalformedMessage))
	}
	if bridge.session == nil {
		return failureValue(localFailure(FailureDisposed))
	}
	if failure := bridge.session.Dispose(args[0].String()); failure != nil {
		return failureValue(failure)
	}
	result := js.Global().Get("Object").New()
	result.Set("ok", true)
	return result
}

func twoStrings(args []js.Value) (string, string, bool) {
	for _, value := range args {
		if value.Type() != js.TypeString {
			return "", "", false
		}
	}
	return args[0].String(), args[1].String(), true
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

func decodeBlobArguments(controlValue, idsValue, buffersValue js.Value, limits TransportLimits) ([]string, []js.Value, *LocalFailure) {
	if idsValue.Length() != buffersValue.Length() || int64(idsValue.Length()) > limits.MaxBuffers {
		return nil, nil, localFailure(FailureMalformedMessage)
	}
	ids := make([]string, idsValue.Length())
	buffers := make([]js.Value, buffersValue.Length())
	var total int64
	for index := range ids {
		idValue, buffer := idsValue.Index(index), buffersValue.Index(index)
		if idValue.Type() != js.TypeString || !exactArrayBuffer(buffer) {
			return nil, nil, localFailure(FailureMalformedMessage)
		}
		id := idValue.String()
		if id == "" || !utf8.ValidString(id) || int64(len(id)) > limits.MaxBlobIDBytes {
			return nil, nil, localFailure(FailureMalformedMessage)
		}
		if index > 0 && ids[index-1] >= id {
			return nil, nil, localFailure(FailureMalformedMessage)
		}
		if buffer.Equal(controlValue) {
			return nil, nil, localFailure(FailureMalformedMessage)
		}
		for previous := 0; previous < index; previous++ {
			if buffer.Equal(buffers[previous]) {
				return nil, nil, localFailure(FailureMalformedMessage)
			}
		}
		length := int64(buffer.Get("byteLength").Int())
		if length < 0 || length > limits.MaxInputBlobBytes || total > limits.MaxInputTotalBytes-length {
			return nil, nil, localFailure(FailureTransferFailed)
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
		return nil, localFailure(FailureTransferFailed)
	}
	result := make([]byte, length)
	view := js.Global().Get("Uint8Array").New(value)
	if copied := js.CopyBytesToGo(result, view); copied != length {
		return nil, localFailure(FailureTransferFailed)
	}
	return result, nil
}

func copyGoBytesToArrayBuffer(value []byte) (js.Value, *LocalFailure) {
	buffer := js.Global().Get("ArrayBuffer").New(len(value))
	view := js.Global().Get("Uint8Array").New(buffer)
	if copied := js.CopyBytesToJS(view, value); copied != len(value) {
		return js.Undefined(), localFailure(FailureTransferFailed)
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

func (table *jsBlobTable) Bind(requirements []endpoint.BlobRequirement, limits TransportLimits) *LocalFailure {
	if table == nil || table.released || table.bound || int64(len(table.ids)) > limits.MaxBuffers {
		return localFailure(FailureMalformedMessage)
	}
	if len(requirements) != len(table.ids) {
		return localFailure(FailureTransferFailed)
	}
	var total int64
	for index, length := range table.lengths {
		if length < 0 || int64(length) > limits.MaxInputBlobBytes || total > limits.MaxInputTotalBytes-int64(length) {
			return localFailure(FailureTransferFailed)
		}
		requirement := requirements[index]
		expected, err := strconv.ParseUint(string(requirement.Ref.Size), 10, 64)
		if err != nil || requirement.References <= 0 || requirement.Ref.BlobID != table.ids[index] || expected != uint64(length) {
			return localFailure(FailureTransferFailed)
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
