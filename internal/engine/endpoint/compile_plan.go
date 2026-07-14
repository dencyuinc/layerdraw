// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"unicode/utf8"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

const (
	// FailureCompilePlanConsumed identifies a second or concurrent Execute call.
	FailureCompilePlanConsumed = "engine.compile.plan_consumed"
)

// BlobRequirement is one unique request attachment required by a CompilePlan.
// Requirements are sorted by BlobID. References reports how many compatible
// logical input bindings share the exact same generated BlobRef.
type BlobRequirement struct {
	Ref        protocolcommon.BlobRef
	References int64
}

// CompileEffectiveLimits is the exact positive compiler-limit set selected
// during preparation. Native transports can admit bytes without interpreting
// generated compile input fields or recreating limit policy.
type CompileEffectiveLimits struct {
	MaxProjectSourceFiles int64
	MaxProjectSourceBytes int64
	MaxPackFiles          int64
	MaxPackBytes          int64
	MaxAssets             int64
	MaxAssetBytes         int64
	MaxRasterDimension    int64
	MaxRasterPixels       int64
	MaxDeclarations       int64
}

// CompileAdmissionBudget contains exact declared input accounting and the
// effective ceilings used to approve a request before any attachment is read.
// RequiredBlobBytes counts each unique BlobID once; category byte counts retain
// every logical use according to the Engine facade's resource accounting.
type CompileAdmissionBudget struct {
	RequiredBlobCount       int64
	RequiredBlobBytes       int64
	ProjectSourceFiles      int64
	ProjectSourceBytes      int64
	InstalledPackFiles      int64
	ResolvedPackFiles       int64
	PackBytes               int64
	Assets                  int64
	AssetBytes              int64
	EffectiveCompilerLimits CompileEffectiveLimits
}

type compilePlanState uint8

const (
	compilePlanReady compilePlanState = iota
	compilePlanExecuting
	compilePlanPublishing
	compilePlanFinished
	compilePlanAborted
)

// CompilePlan is an immutable prepared request with a single-use lifecycle.
// Its exported metadata methods return defensive values. Execute and Abort are
// safe to call concurrently; Abort is idempotent and cancellation wins until
// BlobSink makes its atomic successful commit.
type CompilePlan struct {
	mu             sync.Mutex
	compiler       engine.Engine
	requestID      string
	release        protocolcommon.ReleaseVersion
	prepared       *preparedCompileInput
	requirements   []BlobRequirement
	budget         CompileAdmissionBudget
	state          compilePlanState
	abortRequested bool
	executeCancel  context.CancelFunc
}

// PrepareCompile validates and owns one generated request without reading any
// attachment or invoking Engine.Compile. Exactly one of plan or terminal is
// non-nil when err is nil; both are nil when caller misuse returns an error.
func (d *CompileDispatcher) PrepareCompile(
	ctx context.Context,
	negotiated *NegotiatedContext,
	request engineprotocol.CompileRequestEnvelope,
) (plan *CompilePlan, terminal *engineprotocol.CompileResponseEnvelope, err error) {
	if d == nil {
		return nil, nil, fmt.Errorf("nil compile dispatcher")
	}
	if ctx == nil {
		return nil, nil, fmt.Errorf("nil compile context")
	}
	if request.RequestID == "" || !utf8.ValidString(request.RequestID) {
		return nil, nil, fmt.Errorf("compile request ID must be nonempty valid UTF-8")
	}
	requestID := request.RequestID
	engineRelease := protocolcommon.ReleaseVersion(d.compiler.Describe().ReleaseVersion)
	if _, releaseErr := protocolcommon.EncodeReleaseVersion(engineRelease); releaseErr != nil {
		return nil, nil, fmt.Errorf("invalid Engine release: %w", releaseErr)
	}
	defer func() {
		if recover() != nil {
			response, responseErr := compileFailedResponse(requestID, engineRelease, invariantProtocolFailure())
			plan, terminal, err = terminalCompilePreparation(response, responseErr)
		}
	}()

	if ctx.Err() != nil {
		response, responseErr := compileCancelledResponse(requestID, engineRelease)
		return terminalCompilePreparation(response, responseErr)
	}
	encodedRequest, encodeErr := engineprotocol.EncodeCompileRequestEnvelope(request)
	if encodeErr != nil {
		response, responseErr := compileFailedResponse(requestID, engineRelease, protocolFailure(
			protocolcommon.ProtocolFailureCategoryTransport,
			FailureCompileInvalidRequest,
			"The compile request is not a valid generated Engine envelope.",
			false,
			nil,
		))
		return terminalCompilePreparation(response, responseErr)
	}
	// The canonical generated round trip severs every caller-owned slice, map,
	// and pointer before the request is retained by the plan.
	request, encodeErr = engineprotocol.DecodeCompileRequestEnvelope(encodedRequest)
	if encodeErr != nil {
		response, responseErr := compileFailedResponse(requestID, engineRelease, invariantProtocolFailure())
		return terminalCompilePreparation(response, responseErr)
	}
	if ctx.Err() != nil {
		response, responseErr := compileCancelledResponse(requestID, engineRelease)
		return terminalCompilePreparation(response, responseErr)
	}
	if negotiated == nil ||
		negotiated.protocolName != ProtocolName ||
		string(negotiated.protocolVersion) != ProtocolVersion ||
		string(negotiated.protocolSchemaDigest) != engineprotocol.SchemaDigest ||
		!negotiated.SupportsOperation(OperationCompile) ||
		request.Protocol.Name != engineprotocol.EngineProtocolRefNameValue ||
		request.Protocol.Version != engineprotocol.EngineProtocolRefVersionValue ||
		request.Operation != engineprotocol.CompileRequestEnvelopeOperationValue {
		response, responseErr := compileFailedResponse(requestID, engineRelease, protocolFailure(
			protocolcommon.ProtocolFailureCategoryInvariant,
			FailureCompileUnnegotiated,
			"Compilation requires a compatible negotiated Engine context.",
			false,
			nil,
		))
		return terminalCompilePreparation(response, responseErr)
	}
	if engineRelease != negotiated.engineRelease {
		response, responseErr := compileFailedResponse(requestID, engineRelease, invariantProtocolFailure())
		return terminalCompilePreparation(response, responseErr)
	}

	prepared, diagnostics, failure := prepareCompileInput(ctx, negotiated, request.Payload)
	if failure != nil {
		if failure.Category == protocolcommon.ProtocolFailureCategoryCancelled {
			response, responseErr := compileCancelledResponse(requestID, engineRelease)
			return terminalCompilePreparation(response, responseErr)
		}
		response, responseErr := compileFailedResponse(requestID, engineRelease, *failure)
		return terminalCompilePreparation(response, responseErr)
	}
	if len(diagnostics) != 0 {
		response, responseErr := compileRejectedResponse(requestID, engineRelease, diagnostics)
		return terminalCompilePreparation(response, responseErr)
	}
	if ctx.Err() != nil {
		response, responseErr := compileCancelledResponse(requestID, engineRelease)
		return terminalCompilePreparation(response, responseErr)
	}
	return &CompilePlan{
		compiler:     d.compiler,
		requestID:    requestID,
		release:      engineRelease,
		prepared:     &prepared,
		requirements: cloneBlobRequirements(prepared.requirements),
		budget:       prepared.budget,
		state:        compilePlanReady,
	}, nil, nil
}

func terminalCompilePreparation(response engineprotocol.CompileResponseEnvelope, responseErr error) (*CompilePlan, *engineprotocol.CompileResponseEnvelope, error) {
	if responseErr != nil {
		return nil, nil, responseErr
	}
	return nil, &response, nil
}

// BlobRequirements returns the exact unique, BlobID-sorted request attachment
// set. Mutation of the returned slice cannot affect the plan.
func (p *CompilePlan) BlobRequirements() []BlobRequirement {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == compilePlanFinished || p.state == compilePlanAborted {
		return nil
	}
	return cloneBlobRequirements(p.requirements)
}

// AdmissionBudget returns the exact preparation-time accounting value. A zero
// value is returned after the plan has been executed or aborted.
func (p *CompilePlan) AdmissionBudget() CompileAdmissionBudget {
	if p == nil {
		return CompileAdmissionBudget{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == compilePlanFinished || p.state == compilePlanAborted {
		return CompileAdmissionBudget{}
	}
	return p.budget
}

// Execute resolves one complete request-scoped source, invokes the canonical
// Engine facade exactly once, snapshots exactly once, and atomically publishes
// the complete output set. Every Execute attempt consumes the plan.
func (p *CompilePlan) Execute(ctx context.Context, source BlobSource, sink BlobSink) (response engineprotocol.CompileResponseEnvelope, err error) {
	if p == nil {
		return response, fmt.Errorf("nil compile plan")
	}
	if ctx == nil {
		return response, fmt.Errorf("nil compile context")
	}

	p.mu.Lock()
	requestID, release := p.requestID, p.release
	switch p.state {
	case compilePlanReady:
		prepared := p.prepared
		executeContext, cancel := context.WithCancel(ctx)
		p.state = compilePlanExecuting
		p.executeCancel = cancel
		p.mu.Unlock()
		return p.executePrepared(executeContext, prepared, source, sink)
	case compilePlanAborted:
		p.mu.Unlock()
		return compileCancelledResponse(requestID, release)
	default:
		p.mu.Unlock()
		return compileFailedResponse(requestID, release, protocolFailure(
			protocolcommon.ProtocolFailureCategoryInvariant,
			FailureCompilePlanConsumed,
			"The prepared compile request has already been consumed.",
			false,
			nil,
		))
	}
}

func (p *CompilePlan) executePrepared(ctx context.Context, prepared *preparedCompileInput, source BlobSource, sink BlobSink) (response engineprotocol.CompileResponseEnvelope, err error) {
	var lease *blobLease
	defer func() {
		if lease != nil {
			_ = lease.Release(ctx)
		}
		if recover() != nil {
			if ctx.Err() != nil || p.abortWasRequested() {
				response, err = compileCancelledResponse(p.requestID, p.release)
			} else {
				response, err = compileFailedResponse(p.requestID, p.release, invariantProtocolFailure())
			}
		}
		p.finishExecution(ctx)
	}()

	if source == nil || sink == nil || prepared == nil {
		return compileFailedResponse(p.requestID, p.release, invariantProtocolFailure())
	}
	if ctx.Err() != nil {
		return compileCancelledResponse(p.requestID, p.release)
	}
	owned, acquiredLease, failure := acquireBlobUses(ctx, prepared.uses, source)
	lease = acquiredLease
	if failure != nil {
		if failure.Category == protocolcommon.ProtocolFailureCategoryCancelled {
			return compileCancelledResponse(p.requestID, p.release)
		}
		return compileFailedResponse(p.requestID, p.release, *failure)
	}
	mapped := mapPreparedCompileInput(*prepared, owned)
	if ctx.Err() != nil {
		return compileCancelledResponse(p.requestID, p.release)
	}

	// These are intentionally the only facade and Snapshot invocations.
	compileResult, compileErr := p.compiler.Compile(ctx, mapped)
	var snapshot engine.Snapshot
	if compileErr == nil {
		snapshot = compileResult.Snapshot()
	}
	if releaseFailure := lease.Release(ctx); releaseFailure != nil {
		lease = nil
		if releaseFailure.Category == protocolcommon.ProtocolFailureCategoryCancelled {
			return compileCancelledResponse(p.requestID, p.release)
		}
		return compileFailedResponse(p.requestID, p.release, *releaseFailure)
	}
	lease = nil
	if ctx.Err() != nil {
		return compileCancelledResponse(p.requestID, p.release)
	}
	if compileErr != nil {
		return mapCompileError(p.requestID, p.release, compileErr)
	}

	mappedDiagnostics, mapErr := mapDiagnostics(snapshot.Diagnostics)
	if mapErr != nil {
		return compileFailedResponse(p.requestID, p.release, invariantProtocolFailure())
	}
	if snapshot.Mode == "" {
		if !isRejectedCompileSnapshot(snapshot) || len(mappedDiagnostics) == 0 {
			return compileFailedResponse(p.requestID, p.release, invariantProtocolFailure())
		}
		return compileRejectedResponse(p.requestID, p.release, mappedDiagnostics)
	}
	payload, blobs, mapErr := mapCompileSnapshot(snapshot)
	if mapErr != nil {
		return compileFailedResponse(p.requestID, p.release, invariantProtocolFailure())
	}
	response, err = compileSuccessResponse(p.requestID, p.release, payload, mappedDiagnostics)
	if err != nil {
		return compileFailedResponse(p.requestID, p.release, invariantProtocolFailure())
	}
	if !p.claimPublication(ctx) {
		return compileCancelledResponse(p.requestID, p.release)
	}
	publishErr := sink.Publish(ctx, cloneOutputBlobs(blobs))
	if publishErr == nil {
		// A nil return is the sink's atomic commit point. A compliant sink must
		// return an error if ctx was cancelled before that point; Abort racing
		// after it is therefore later than publication and cannot rewrite success.
		p.finishPublication(ctx, true)
		return response, nil
	}
	aborted := p.finishPublication(ctx, false)
	if aborted || errors.Is(publishErr, context.Canceled) || errors.Is(publishErr, context.DeadlineExceeded) {
		return compileCancelledResponse(p.requestID, p.release)
	}
	return compileFailedResponse(p.requestID, p.release, protocolFailure(
		protocolcommon.ProtocolFailureCategoryIo,
		FailureCompileBlobSink,
		"The compiled output blobs could not be published.",
		true,
		nil,
	))
}

// Abort is idempotent. It releases retained preparation state immediately for
// an unstarted plan and cancels an executing/publishing plan.
func (p *CompilePlan) Abort() {
	if p == nil {
		return
	}
	p.mu.Lock()
	var cancel context.CancelFunc
	switch p.state {
	case compilePlanReady:
		p.state = compilePlanAborted
		p.abortRequested = true
		p.clearOwnedStateLocked()
	case compilePlanExecuting, compilePlanPublishing:
		p.abortRequested = true
		cancel = p.executeCancel
	}
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (p *CompilePlan) claimPublication(ctx context.Context) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != compilePlanExecuting || p.abortRequested || ctx.Err() != nil {
		return false
	}
	p.state = compilePlanPublishing
	return true
}

func (p *CompilePlan) finishPublication(ctx context.Context, committed bool) bool {
	p.mu.Lock()
	aborted := !committed && (p.abortRequested || ctx.Err() != nil)
	if aborted {
		p.state = compilePlanAborted
	} else {
		p.state = compilePlanFinished
	}
	cancel := p.executeCancel
	p.executeCancel = nil
	p.clearOwnedStateLocked()
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return aborted
}

func (p *CompilePlan) finishExecution(ctx context.Context) {
	p.mu.Lock()
	if p.state == compilePlanExecuting || p.state == compilePlanPublishing {
		if p.abortRequested || ctx.Err() != nil {
			p.state = compilePlanAborted
		} else {
			p.state = compilePlanFinished
		}
	}
	cancel := p.executeCancel
	p.executeCancel = nil
	p.clearOwnedStateLocked()
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (p *CompilePlan) abortWasRequested() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.abortRequested
}

func (p *CompilePlan) clearOwnedStateLocked() {
	p.prepared = nil
	p.requirements = nil
	p.budget = CompileAdmissionBudget{}
}

func cloneBlobRequirements(input []BlobRequirement) []BlobRequirement {
	return append([]BlobRequirement(nil), input...)
}
