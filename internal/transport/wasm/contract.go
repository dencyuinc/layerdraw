// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package wasm implements the byte-only, single-flight Go side of the
// dedicated browser Worker transport. Generated protocol values are decoded
// only in Go and all compiler semantics remain behind endpoint.
package wasm

import (
	"regexp"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
)

const (
	WorkerProtocol        = "layerdraw.engine_worker"
	WorkerProtocolVersion = 1
	TransportID           = "wasm_worker"
)

const (
	FailureUnsupported          = "engine.worker.unsupported"
	FailureInitializationFailed = "engine.worker.initialization_failed"
	FailureArtifactMismatch     = "engine.worker.artifact_mismatch"
	FailureMalformedMessage     = "engine.worker.malformed_message"
	FailureStaleGeneration      = "engine.worker.stale_generation"
	FailureTransferFailed       = "engine.worker.transfer_failed"
	FailureCrashed              = "engine.worker.crashed"
	FailureTerminatedByCaller   = "engine.worker.terminated_by_caller"
	FailureDisposed             = "engine.worker.disposed"
)

const (
	PhaseInitialization = "initialization"
	PhaseRequest        = "request"
	PhaseTransfer       = "transfer"
	PhaseRuntime        = "runtime"
	PhaseLifecycle      = "lifecycle"
)

// TransportLimits is the literal initial browser profile. Values are frozen
// artifact inputs, not browser heuristics or aliases for native defaults.
type TransportLimits struct {
	MaxControlBytes         int64 `json:"max_control_bytes"`
	MaxControlDepth         int64 `json:"max_control_depth"`
	MaxBlobIDBytes          int64 `json:"max_blob_id_bytes"`
	MaxBuffers              int64 `json:"max_buffers"`
	MaxInputBlobBytes       int64 `json:"max_input_blob_bytes"`
	MaxInputTotalBytes      int64 `json:"max_input_total_bytes"`
	MaxOutputBlobBytes      int64 `json:"max_output_blob_bytes"`
	MaxOutputTotalBytes     int64 `json:"max_output_total_bytes"`
	MaxResponsePublishBytes int64 `json:"max_response_publish_bytes"`
}

// BrowserTransportLimits returns the immutable conservative browser profile.
func BrowserTransportLimits() TransportLimits {
	return TransportLimits{
		MaxControlBytes:         engineprotocol.MaxWireJSONBytes,
		MaxControlDepth:         engineprotocol.MaxWireJSONDepth,
		MaxBlobIDBytes:          256,
		MaxBuffers:              2_048,
		MaxInputBlobBytes:       32 << 20,
		MaxInputTotalBytes:      64 << 20,
		MaxOutputBlobBytes:      32 << 20,
		MaxOutputTotalBytes:     64 << 20,
		MaxResponsePublishBytes: (64 << 20) + engineprotocol.MaxWireJSONBytes,
	}
}

// BrowserCompilerLimitPolicy is deliberately lower than native defaults and
// is advertised by the generated handshake manifest through #28.
func BrowserCompilerLimitPolicy() endpoint.LimitPolicy {
	return endpoint.FixedLimitPolicy(endpoint.CompileEffectiveLimits{
		MaxProjectSourceFiles: 512,
		MaxProjectSourceBytes: 16 << 20,
		MaxPackFiles:          1_024,
		MaxPackBytes:          32 << 20,
		MaxAssets:             256,
		MaxAssetBytes:         16 << 20,
		MaxRasterDimension:    8_192,
		MaxRasterPixels:       16 << 20,
		MaxDeclarations:       250_000,
	})
}

// LocalFailure is the closed pre-envelope Worker failure value. It contains
// no exception, stack, path, URL, source byte, or browser-specific detail.
type LocalFailure struct {
	Code      string `json:"code"`
	Phase     string `json:"phase"`
	Retryable bool   `json:"retryable"`
}

func localFailure(code, phase string, retryable bool) *LocalFailure {
	return &LocalFailure{Code: code, Phase: phase, Retryable: retryable}
}

func validGeneration(value string) bool {
	return regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`).MatchString(value)
}
