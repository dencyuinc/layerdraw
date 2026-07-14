// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"unicode/utf8"

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
	FailureCompileConflictingBlobRef = "engine.compile.conflicting_blob_reference"
	FailureCompileMissingBlob        = "engine.compile.missing_blob"
	FailureCompileBlobSizeMismatch   = "engine.compile.blob_size_mismatch"
	FailureCompileBlobDigestMismatch = "engine.compile.blob_digest_mismatch"
	FailureCompileBlobOversized      = "engine.compile.blob_oversized"
	FailureCompileBlobSink           = "engine.compile.blob_sink_failure"
)

// BlobDefinition is one request/session attachment. Reader is consumed at
// most once and is closed on every dispatch path. Implementations must not
// perform persistent lookup or return mutable storage after Close.
type BlobDefinition struct {
	BlobID string
	Reader io.ReadCloser
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
// no BlobRef visible and return an error.
type BlobSink interface {
	Publish(context.Context, []OutputBlob) error
}

// CompileDispatcher is immutable and safe for concurrent use.
type CompileDispatcher struct {
	compiler engine.Engine
}

// NewCompileDispatcher binds the canonical Engine facade used by every
// transport. The facade is invoked exactly once for each mapped request.
func NewCompileDispatcher(compiler engine.Engine) *CompileDispatcher {
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
	if d == nil {
		return response, fmt.Errorf("nil compile dispatcher")
	}
	if ctx == nil {
		return response, fmt.Errorf("nil compile context")
	}
	if request.RequestID == "" || !utf8.ValidString(request.RequestID) {
		return response, fmt.Errorf("compile request ID must be nonempty valid UTF-8")
	}
	requestID := request.RequestID
	engineRelease := protocolcommon.ReleaseVersion(d.compiler.Describe().ReleaseVersion)

	// Caller-owned BlobSource/BlobSink implementations are outside the trusted
	// mapper. Contain their panics without exposing panic text or stacks.
	defer func() {
		if recover() != nil {
			response, err = compileFailedResponse(request.RequestID, engineRelease, protocolFailure(
				protocolcommon.ProtocolFailureCategoryInvariant,
				FailureCompileInvariant,
				"The Engine could not complete compilation.",
				false,
				nil,
			))
		}
	}()

	if ctx.Err() != nil {
		return compileCancelledResponse(request.RequestID, engineRelease)
	}
	encodedRequest, encodeErr := engineprotocol.EncodeCompileRequestEnvelope(request)
	if encodeErr != nil {
		return compileFailedResponse(request.RequestID, engineRelease, protocolFailure(
			protocolcommon.ProtocolFailureCategoryTransport,
			FailureCompileInvalidRequest,
			"The compile request is not a valid generated Engine envelope.",
			false,
			nil,
		))
	}
	// Decode the canonical generated representation so caller-owned slices,
	// maps, and pointers cannot be changed by a reentrant BlobSource callback.
	request, encodeErr = engineprotocol.DecodeCompileRequestEnvelope(encodedRequest)
	if encodeErr != nil {
		return compileFailedResponse(requestID, engineRelease, invariantProtocolFailure())
	}
	if negotiated == nil ||
		negotiated.protocolName != ProtocolName ||
		string(negotiated.protocolVersion) != ProtocolVersion ||
		string(negotiated.protocolSchemaDigest) != engineprotocol.SchemaDigest ||
		!negotiated.SupportsOperation(OperationCompile) ||
		request.Protocol.Name != engineprotocol.EngineProtocolRefNameValue ||
		request.Protocol.Version != engineprotocol.EngineProtocolRefVersionValue ||
		request.Operation != engineprotocol.CompileRequestEnvelopeOperationValue {
		return compileFailedResponse(request.RequestID, engineRelease, protocolFailure(
			protocolcommon.ProtocolFailureCategoryInvariant,
			FailureCompileUnnegotiated,
			"Compilation requires a compatible negotiated Engine context.",
			false,
			nil,
		))
	}
	if engineRelease != negotiated.engineRelease {
		return compileFailedResponse(request.RequestID, engineRelease, protocolFailure(
			protocolcommon.ProtocolFailureCategoryInvariant,
			FailureCompileInvariant,
			"The Engine could not complete compilation.",
			false,
			nil,
		))
	}
	if source == nil || sink == nil {
		return compileFailedResponse(request.RequestID, negotiated.engineRelease, protocolFailure(
			protocolcommon.ProtocolFailureCategoryInvariant,
			FailureCompileInvariant,
			"The Engine could not complete compilation.",
			false,
			nil,
		))
	}

	mapped, diagnostics, failure := mapCompileInput(ctx, negotiated, request.Payload, source)
	if ctx.Err() != nil {
		return compileCancelledResponse(request.RequestID, negotiated.engineRelease)
	}
	if failure != nil {
		if failure.Category == protocolcommon.ProtocolFailureCategoryCancelled {
			return compileCancelledResponse(request.RequestID, negotiated.engineRelease)
		}
		return compileFailedResponse(request.RequestID, negotiated.engineRelease, *failure)
	}
	if len(diagnostics) != 0 {
		return compileRejectedResponse(request.RequestID, negotiated.engineRelease, diagnostics)
	}

	// This is intentionally the only facade invocation in the dispatcher.
	compileResult, compileErr := d.compiler.Compile(ctx, mapped)
	if compileErr != nil {
		return mapCompileError(request.RequestID, negotiated.engineRelease, compileErr)
	}
	// This is intentionally the only Snapshot call in the dispatcher.
	snapshot := compileResult.Snapshot()
	if ctx.Err() != nil {
		return compileCancelledResponse(request.RequestID, negotiated.engineRelease)
	}

	mappedDiagnostics, mapErr := mapDiagnostics(snapshot.Diagnostics)
	if mapErr != nil {
		return compileFailedResponse(request.RequestID, negotiated.engineRelease, invariantProtocolFailure())
	}
	if snapshot.Mode == "" {
		if !isRejectedCompileSnapshot(snapshot) || len(mappedDiagnostics) == 0 {
			return compileFailedResponse(request.RequestID, negotiated.engineRelease, invariantProtocolFailure())
		}
		return compileRejectedResponse(request.RequestID, negotiated.engineRelease, mappedDiagnostics)
	}

	payload, blobs, mapErr := mapCompileSnapshot(snapshot)
	if mapErr != nil {
		return compileFailedResponse(request.RequestID, negotiated.engineRelease, invariantProtocolFailure())
	}
	response, err = compileSuccessResponse(request.RequestID, negotiated.engineRelease, payload, mappedDiagnostics)
	if err != nil {
		return compileFailedResponse(request.RequestID, negotiated.engineRelease, invariantProtocolFailure())
	}
	if ctx.Err() != nil {
		return compileCancelledResponse(request.RequestID, negotiated.engineRelease)
	}
	if publishErr := sink.Publish(ctx, cloneOutputBlobs(blobs)); publishErr != nil {
		if ctx.Err() != nil || errors.Is(publishErr, context.Canceled) || errors.Is(publishErr, context.DeadlineExceeded) {
			return compileCancelledResponse(request.RequestID, negotiated.engineRelease)
		}
		return compileFailedResponse(request.RequestID, negotiated.engineRelease, protocolFailure(
			protocolcommon.ProtocolFailureCategoryIo,
			FailureCompileBlobSink,
			"The compiled output blobs could not be published.",
			true,
			nil,
		))
	}
	return response, nil
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
	response := engineprotocol.CompileResponseEnvelope{
		Diagnostics:   diagnostics,
		EngineRelease: release,
		Outcome:       protocolcommon.OutcomeSuccess,
		Payload:       &payload,
		Protocol:      bootstrapProtocolRef(),
		RequestID:     requestID,
	}
	return validateCompileResponse(response)
}

func compileRejectedResponse(requestID string, release protocolcommon.ReleaseVersion, diagnostics []semantic.Diagnostic) (engineprotocol.CompileResponseEnvelope, error) {
	response := engineprotocol.CompileResponseEnvelope{
		Diagnostics:   diagnostics,
		EngineRelease: release,
		Outcome:       protocolcommon.OutcomeRejected,
		Protocol:      bootstrapProtocolRef(),
		RequestID:     requestID,
	}
	return validateCompileResponse(response)
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
	if _, err := engineprotocol.EncodeCompileResponseEnvelope(response); err != nil {
		return engineprotocol.CompileResponseEnvelope{}, fmt.Errorf("constructed compile response is invalid: %w", err)
	}
	return response, nil
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

func cloneOutputBlobs(input []OutputBlob) []OutputBlob {
	result := make([]OutputBlob, len(input))
	for index := range input {
		result[index] = input[index]
		result[index].Bytes = append([]byte(nil), input[index].Bytes...)
	}
	return result
}
