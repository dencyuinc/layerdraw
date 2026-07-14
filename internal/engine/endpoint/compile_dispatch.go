// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

const (
	FailureCompileCancelled          = "engine.compile.cancelled"
	FailureCompileInvariant          = "engine.compile.invariant_failure"
	FailureCompileInvalidRequest     = "engine.compile.invalid_request"
	FailureCompileUnnegotiated       = "engine.compile.unnegotiated_context"
	FailureCompileBlobSource         = "engine.compile.blob_source_failure"
	FailureCompileDuplicateBlob      = "engine.compile.duplicate_blob_definition"
	FailureCompileUnexpectedBlob     = "engine.compile.unexpected_blob_definition"
	FailureCompileConflictingBlobRef = "engine.compile.conflicting_blob_reference"
	FailureCompileMissingBlob        = "engine.compile.missing_blob"
	FailureCompileBlobSizeMismatch   = "engine.compile.blob_size_mismatch"
	FailureCompileBlobDigestMismatch = "engine.compile.blob_digest_mismatch"
	FailureCompileBlobOversized      = "engine.compile.blob_oversized"
	FailureCompileBlobLifetime       = "engine.compile.unsupported_blob_lifetime"
	FailureCompileBlobSink           = "engine.compile.blob_sink_failure"
	FailureCompileControlOutput      = "engine.compile.control_output_exhausted"
)

type compileResponseRepresentation uint8

const (
	compileResponseRepresentable compileResponseRepresentation = iota
	compileResponseControlExhausted
	compileResponseInvalid
)

type compileResponseLimit struct {
	resource string
	limit    int64
	observed int64
}

// OwnedBlob transfers one already-owned Go byte slice into Execute without a
// redundant transport-layer copy. The source must relinquish all access until
// Release is called; Release is required, must not mutate Bytes, and is invoked
// exactly once. A nil Bytes slice is a valid transferred zero-byte blob.
type OwnedBlob struct {
	Bytes   []byte
	Release func()
}

// BlobDefinition is one request-scoped attachment. Exactly one of Reader or
// Owned must be non-nil. Readers are closed and owned buffers are released on
// every acquired execution path.
type BlobDefinition struct {
	BlobID string
	Reader io.ReadCloser
	Owned  *OwnedBlob
}

// BlobSource enumerates the complete request-scoped attachment table. The
// ordered result deliberately preserves duplicate definitions for rejection.
type BlobSource interface {
	Definitions(context.Context) ([]BlobDefinition, error)
}

// OutputBlob owns the exact bytes described by Ref for one atomic publish.
// Bytes are independent of Engine storage and other dispatches.
type OutputBlob struct {
	Ref   protocolcommon.BlobRef
	Bytes []byte
}

// BlobSink atomically publishes a complete request-scoped output set. It must
// either take an independent copy of every byte slice and return nil, or make
// no BlobRef visible and return an error. A configured transport output cap and
// context cancellation must fail without making any response BlobRef visible.
type BlobSink interface {
	Publish(context.Context, []OutputBlob) error
}

// compileDriver is the package-private boundary around the canonical facade's
// one Compile call and one defensive Snapshot. The public constructor always
// installs engineCompileDriver; endpoint tests can supply deterministic
// snapshots without exposing an alternate compiler through the public API.
type compileDriver interface {
	Describe() engine.Descriptor
	CompileSnapshot(context.Context, engine.CompileInput) (engine.Snapshot, error)
}

type engineCompileDriver struct {
	compiler engine.Engine
}

func (driver engineCompileDriver) Describe() engine.Descriptor {
	return driver.compiler.Describe()
}

func (driver engineCompileDriver) CompileSnapshot(ctx context.Context, input engine.CompileInput) (engine.Snapshot, error) {
	result, err := driver.compiler.Compile(ctx, input)
	if err != nil {
		return engine.Snapshot{}, err
	}
	return result.Snapshot(), nil
}

// CompileDispatcher is immutable and safe for concurrent use.
type CompileDispatcher struct {
	compiler compileDriver
}

// NewCompileDispatcher binds the canonical Engine facade used by every
// transport. The facade is invoked exactly once for each mapped request.
func NewCompileDispatcher(compiler engine.Engine) *CompileDispatcher {
	return newCompileDispatcher(engineCompileDriver{compiler: compiler})
}

func newCompileDispatcher(compiler compileDriver) *CompileDispatcher {
	return &CompileDispatcher{compiler: compiler}
}

// DispatchCompile validates and maps one generated request under an immutable
// negotiated context. Its error result is reserved for caller misuse that
// cannot produce a trustworthy response envelope.
func (d *CompileDispatcher) DispatchCompile(
	ctx context.Context,
	negotiated *NegotiatedContext,
	request engineprotocol.CompileRequestEnvelope,
	source BlobSource,
	sink BlobSink,
) (response engineprotocol.CompileResponseEnvelope, err error) {
	plan, terminal, err := d.PrepareCompile(ctx, negotiated, request)
	if err != nil {
		return response, err
	}
	if terminal != nil {
		return *terminal, nil
	}
	return plan.Execute(ctx, source, sink)
}

func isRejectedCompileSnapshot(snapshot engine.Snapshot) bool {
	if snapshot.Mode != "" || len(snapshot.Diagnostics) == 0 {
		return false
	}
	withoutDiagnostics := snapshot
	withoutDiagnostics.Diagnostics = nil
	return reflect.DeepEqual(withoutDiagnostics, engine.Snapshot{})
}

func mapCompileError(requestID string, release protocolcommon.ReleaseVersion, compileErr error) (engineprotocol.CompileResponseEnvelope, error) {
	var typed *engine.CompileError
	if errors.As(compileErr, &typed) {
		switch typed.Category {
		case engine.ErrorCategoryCancelled:
			return compileCancelledResponse(requestID, release)
		case engine.ErrorCategoryResource:
			details := protocolcommon.JsonObject{}
			if typed.Resource != "" {
				details["resource"] = stringJSON(typed.Resource)
			}
			if typed.Limit >= 0 {
				details["limit"] = stringJSON(fmt.Sprintf("%d", typed.Limit))
			}
			if typed.Observed >= 0 {
				details["observed"] = stringJSON(fmt.Sprintf("%d", typed.Observed))
			}
			return compileFailedResponse(requestID, release, protocolFailure(
				protocolcommon.ProtocolFailureCategoryResource,
				typed.Code,
				"Compilation exceeded an effective resource limit.",
				false,
				details,
			))
		}
	}
	if errors.Is(compileErr, context.Canceled) || errors.Is(compileErr, context.DeadlineExceeded) {
		return compileCancelledResponse(requestID, release)
	}
	return compileFailedResponse(requestID, release, invariantProtocolFailure())
}

func compileSuccessResponse(requestID string, release protocolcommon.ReleaseVersion, payload engineprotocol.CompileResult, diagnostics []semantic.Diagnostic) (engineprotocol.CompileResponseEnvelope, error) {
	return validateCompileResponse(compileSuccessEnvelope(requestID, release, payload, diagnostics))
}

func compileSuccessEnvelope(requestID string, release protocolcommon.ReleaseVersion, payload engineprotocol.CompileResult, diagnostics []semantic.Diagnostic) engineprotocol.CompileResponseEnvelope {
	return engineprotocol.CompileResponseEnvelope{
		Diagnostics:   diagnostics,
		EngineRelease: release,
		Outcome:       protocolcommon.OutcomeSuccess,
		Payload:       &payload,
		Protocol:      bootstrapProtocolRef(),
		RequestID:     requestID,
	}
}

func compileRejectedResponse(requestID string, release protocolcommon.ReleaseVersion, diagnostics []semantic.Diagnostic) (engineprotocol.CompileResponseEnvelope, error) {
	return validateCompileResponse(compileRejectedEnvelope(requestID, release, diagnostics))
}

func compileRejectedEnvelope(requestID string, release protocolcommon.ReleaseVersion, diagnostics []semantic.Diagnostic) engineprotocol.CompileResponseEnvelope {
	return engineprotocol.CompileResponseEnvelope{
		Diagnostics:   diagnostics,
		EngineRelease: release,
		Outcome:       protocolcommon.OutcomeRejected,
		Protocol:      bootstrapProtocolRef(),
		RequestID:     requestID,
	}
}

func compileFailedResponse(requestID string, release protocolcommon.ReleaseVersion, failure protocolcommon.ProtocolFailure) (engineprotocol.CompileResponseEnvelope, error) {
	response := engineprotocol.CompileResponseEnvelope{
		Diagnostics:   []semantic.Diagnostic{},
		EngineRelease: release,
		Failure:       &failure,
		Outcome:       protocolcommon.OutcomeFailed,
		Protocol:      bootstrapProtocolRef(),
		RequestID:     requestID,
	}
	return validateCompileResponse(response)
}

func compileCancelledResponse(requestID string, release protocolcommon.ReleaseVersion) (engineprotocol.CompileResponseEnvelope, error) {
	failure := protocolFailure(
		protocolcommon.ProtocolFailureCategoryCancelled,
		FailureCompileCancelled,
		"Compilation was cancelled.",
		true,
		nil,
	)
	response := engineprotocol.CompileResponseEnvelope{
		Diagnostics:   []semantic.Diagnostic{},
		EngineRelease: release,
		Failure:       &failure,
		Outcome:       protocolcommon.OutcomeCancelled,
		Protocol:      bootstrapProtocolRef(),
		RequestID:     requestID,
	}
	return validateCompileResponse(response)
}

func validateCompileResponse(response engineprotocol.CompileResponseEnvelope) (engineprotocol.CompileResponseEnvelope, error) {
	representation, limit, err := classifyCompileResponse(response)
	if representation == compileResponseControlExhausted {
		return engineprotocol.CompileResponseEnvelope{}, fmt.Errorf("constructed compile response uses %d %s, exceeding the control limit of %d", limit.observed, limit.resource, limit.limit)
	}
	if err != nil {
		return engineprotocol.CompileResponseEnvelope{}, fmt.Errorf("constructed compile response is invalid: %w", err)
	}
	return response, nil
}

func classifyCompileResponse(response engineprotocol.CompileResponseEnvelope) (compileResponseRepresentation, compileResponseLimit, error) {
	_, encodeErr := engineprotocol.EncodeCompileResponseEnvelope(response)
	if encodeErr == nil {
		return compileResponseRepresentable, compileResponseLimit{}, nil
	}
	encoded, marshalErr := json.Marshal(response)
	if marshalErr != nil {
		return compileResponseInvalid, compileResponseLimit{}, encodeErr
	}
	if observed := int64(len(encoded)); observed > int64(engineprotocol.MaxWireJSONBytes) {
		return compileResponseControlExhausted, compileResponseLimit{
			resource: "control_output_bytes",
			limit:    int64(engineprotocol.MaxWireJSONBytes),
			observed: observed,
		}, nil
	}
	if observed := compileResponseJSONDepth(encoded); observed > int64(engineprotocol.MaxWireJSONDepth) {
		return compileResponseControlExhausted, compileResponseLimit{
			resource: "control_output_depth",
			limit:    int64(engineprotocol.MaxWireJSONDepth),
			observed: observed,
		}, nil
	}
	return compileResponseInvalid, compileResponseLimit{}, encodeErr
}

// compileResponseJSONDepth mirrors the generated wire codec's container-depth
// accounting over json.Marshal output. The input is valid JSON by construction.
func compileResponseJSONDepth(encoded []byte) int64 {
	var depth, maximum int64
	inString := false
	escaped := false
	for _, value := range encoded {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if value == '\\' {
				escaped = true
			} else if value == '"' {
				inString = false
			}
			continue
		}
		switch value {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > maximum {
				maximum = depth
			}
		case '}', ']':
			depth--
		}
	}
	return maximum
}

func protocolFailure(category protocolcommon.ProtocolFailureCategory, code, message string, retryable bool, details protocolcommon.JsonObject) protocolcommon.ProtocolFailure {
	failure := protocolcommon.ProtocolFailure{Category: category, Code: code, Message: message, Retryable: retryable}
	if len(details) != 0 {
		failure.SafeDetails = &details
	}
	return failure
}

func invariantProtocolFailure() protocolcommon.ProtocolFailure {
	return protocolFailure(
		protocolcommon.ProtocolFailureCategoryInvariant,
		FailureCompileInvariant,
		"The Engine could not complete compilation.",
		false,
		nil,
	)
}

func controlOutputProtocolFailure(limit compileResponseLimit) protocolcommon.ProtocolFailure {
	details := protocolcommon.JsonObject{
		"limit":    stringJSON(fmt.Sprintf("%d", limit.limit)),
		"observed": stringJSON(fmt.Sprintf("%d", limit.observed)),
		"resource": stringJSON(limit.resource),
	}
	return protocolFailure(
		protocolcommon.ProtocolFailureCategoryResource,
		FailureCompileControlOutput,
		"The complete compile control result exceeds the Engine Protocol limit.",
		false,
		details,
	)
}

func cloneOutputBlobs(input []OutputBlob) []OutputBlob {
	result := make([]OutputBlob, len(input))
	for index := range input {
		result[index] = input[index]
		result[index].Bytes = append([]byte(nil), input[index].Bytes...)
	}
	return result
}
