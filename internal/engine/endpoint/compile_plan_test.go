// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

func cloneCompileRequest(t *testing.T, input engineprotocol.CompileRequestEnvelope) engineprotocol.CompileRequestEnvelope {
	t.Helper()
	encoded, err := engineprotocol.EncodeCompileRequestEnvelope(input)
	if err != nil {
		t.Fatal(err)
	}
	result, err := engineprotocol.DecodeCompileRequestEnvelope(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustPrepareCompile(t *testing.T, dispatcher *CompileDispatcher, negotiated *NegotiatedContext, request engineprotocol.CompileRequestEnvelope) *CompilePlan {
	t.Helper()
	plan, terminal, err := dispatcher.PrepareCompile(context.Background(), negotiated, request)
	assertCompilePreparationExclusive(t, plan, terminal, err)
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil || terminal != nil {
		t.Fatalf("unexpected preparation: plan=%v terminal=%+v", plan, terminal)
	}
	return plan
}

func assertCompilePreparationExclusive(t *testing.T, plan *CompilePlan, terminal *engineprotocol.CompileResponseEnvelope, err error) {
	t.Helper()
	selected := 0
	if plan != nil {
		selected++
	}
	if terminal != nil {
		selected++
	}
	if err != nil {
		selected++
	}
	if selected != 1 {
		t.Fatalf("compile preparation outcomes are not exclusive: plan=%v terminal=%+v err=%v", plan, terminal, err)
	}
}

func assertTerminalCompileResponse(t *testing.T, plan *CompilePlan, response *engineprotocol.CompileResponseEnvelope, outcome protocolcommon.Outcome) {
	t.Helper()
	if plan != nil || response == nil || response.Outcome != outcome {
		t.Fatalf("plan=%v response=%+v", plan, response)
	}
	if _, err := engineprotocol.EncodeCompileResponseEnvelope(*response); err != nil {
		t.Fatalf("terminal response is not generated-encodable: %v", err)
	}
}

func TestPrepareCompileReturnsGeneratedTerminalResponsesWithoutBlobIO(t *testing.T) {
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	negotiated := compileContext(t)
	value := []byte("project p \"Project\" {}")
	base := compileRequest(value)

	tests := []struct {
		name         string
		context      context.Context
		contextValue *NegotiatedContext
		mutate       func(*engineprotocol.CompileRequestEnvelope)
		outcome      protocolcommon.Outcome
		code         string
	}{
		{name: "invalid generated envelope", context: context.Background(), contextValue: negotiated, mutate: func(request *engineprotocol.CompileRequestEnvelope) { request.Payload.ProjectSourceTree = nil }, outcome: protocolcommon.OutcomeFailed, code: FailureCompileInvalidRequest},
		{name: "semantic duplicate", context: context.Background(), contextValue: negotiated, mutate: func(request *engineprotocol.CompileRequestEnvelope) {
			request.Payload.ProjectSourceTree = append(request.Payload.ProjectSourceTree, request.Payload.ProjectSourceTree[0])
		}, outcome: protocolcommon.OutcomeRejected},
		{name: "conflicting alias", context: context.Background(), contextValue: negotiated, mutate: func(request *engineprotocol.CompileRequestEnvelope) {
			alias := request.Payload.ProjectSourceTree[0]
			alias.Path = "other.ldl"
			alias.Blob.MediaType = "application/octet-stream"
			request.Payload.ProjectSourceTree = append(request.Payload.ProjectSourceTree, alias)
		}, outcome: protocolcommon.OutcomeFailed, code: FailureCompileConflictingBlobRef},
		{name: "non-request blob lifetime", context: context.Background(), contextValue: negotiated, mutate: func(request *engineprotocol.CompileRequestEnvelope) {
			request.Payload.ProjectSourceTree[0].Blob.Lifetime = protocolcommon.BlobLifetimeSession
		}, outcome: protocolcommon.OutcomeFailed, code: FailureCompileBlobLifetime},
		{name: "unnegotiated", context: context.Background(), mutate: func(*engineprotocol.CompileRequestEnvelope) {}, outcome: protocolcommon.OutcomeFailed, code: FailureCompileUnnegotiated},
		{name: "cancelled", context: cancelledContext(), contextValue: negotiated, mutate: func(*engineprotocol.CompileRequestEnvelope) {}, outcome: protocolcommon.OutcomeCancelled, code: FailureCompileCancelled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := cloneCompileRequest(t, base)
			test.mutate(&request)
			plan, response, err := dispatcher.PrepareCompile(test.context, test.contextValue, request)
			assertCompilePreparationExclusive(t, plan, response, err)
			if err != nil {
				t.Fatal(err)
			}
			assertTerminalCompileResponse(t, plan, response, test.outcome)
			if test.code != "" && (response.Failure == nil || response.Failure.Code != test.code) {
				t.Fatalf("failure=%+v", response.Failure)
			}
		})
	}

	over := cloneCompileRequest(t, base)
	limit := protocolcommon.CanonicalNonNegativeInt64("999999999999")
	over.Payload.ResourceLimits.MaxProjectSourceBytes = &limit
	plan, response, err := dispatcher.PrepareCompile(context.Background(), negotiated, over)
	if err != nil {
		t.Fatal(err)
	}
	assertTerminalCompileResponse(t, plan, response, protocolcommon.OutcomeRejected)

	declaredOver := cloneCompileRequest(t, base)
	declaredOver.Payload.ProjectSourceTree[0].Blob.Size = protocolcommon.CanonicalUint64("999999999999")
	plan, response, err = dispatcher.PrepareCompile(context.Background(), negotiated, declaredOver)
	if err != nil {
		t.Fatal(err)
	}
	assertTerminalCompileResponse(t, plan, response, protocolcommon.OutcomeFailed)
	if response.Failure == nil || response.Failure.Code != engine.ErrorCodeProjectSourceBytesExceeded {
		t.Fatalf("declared aggregate failure=%+v", response.Failure)
	}
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

type cancelAfterChecksContext struct {
	context.Context
	mu       sync.Mutex
	cancelAt int
	checks   int
	done     chan struct{}
	once     sync.Once
}

func newCancelAfterChecksContext(cancelAt int) *cancelAfterChecksContext {
	return &cancelAfterChecksContext{Context: context.Background(), cancelAt: cancelAt, done: make(chan struct{})}
}

func (ctx *cancelAfterChecksContext) Done() <-chan struct{} { return ctx.done }

func (ctx *cancelAfterChecksContext) Err() error {
	ctx.mu.Lock()
	ctx.checks++
	cancelled := ctx.cancelAt > 0 && ctx.checks >= ctx.cancelAt
	ctx.mu.Unlock()
	if cancelled {
		ctx.once.Do(func() { close(ctx.done) })
		return context.Canceled
	}
	return nil
}

func (ctx *cancelAfterChecksContext) checkCount() int {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	return ctx.checks
}

func TestPrepareCompileObservesCancellationThroughoutPreparation(t *testing.T) {
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	negotiated := compileContext(t)
	base := compileRequest([]byte("project p \"P\" {}"))

	afterRoundTrip := newCancelAfterChecksContext(2)
	plan, terminal, err := dispatcher.PrepareCompile(afterRoundTrip, negotiated, base)
	assertCompilePreparationExclusive(t, plan, terminal, err)
	assertTerminalCompileResponse(t, plan, terminal, protocolcommon.OutcomeCancelled)

	large := cloneCompileRequest(t, base)
	for index := 0; index < 256; index++ {
		large.Payload.ProjectSourceTree = append(large.Payload.ProjectSourceTree, engineprotocol.SourceFileInput{
			Path: engineprotocol.CanonicalSourcePath(fmt.Sprintf("generated/%03d.ldl", index)),
			Blob: large.Payload.ProjectSourceTree[0].Blob,
		})
	}
	duringPreflight := newCancelAfterChecksContext(64)
	plan, terminal, err = dispatcher.PrepareCompile(duringPreflight, negotiated, large)
	assertCompilePreparationExclusive(t, plan, terminal, err)
	assertTerminalCompileResponse(t, plan, terminal, protocolcommon.OutcomeCancelled)
	if duringPreflight.checkCount() < 64 {
		t.Fatalf("large preflight did not poll cancellation: checks=%d", duringPreflight.checkCount())
	}

	observed := newCancelAfterChecksContext(0)
	prepared, terminal, err := dispatcher.PrepareCompile(observed, negotiated, base)
	assertCompilePreparationExclusive(t, prepared, terminal, err)
	finalCheck := observed.checkCount()
	prepared.Abort()
	if finalCheck < 2 {
		t.Fatalf("preparation made too few cancellation observations: %d", finalCheck)
	}
	beforePublish := newCancelAfterChecksContext(finalCheck)
	plan, terminal, err = dispatcher.PrepareCompile(beforePublish, negotiated, base)
	assertCompilePreparationExclusive(t, plan, terminal, err)
	assertTerminalCompileResponse(t, plan, terminal, protocolcommon.OutcomeCancelled)
	if beforePublish.checkCount() != finalCheck {
		t.Fatalf("cancellation was not observed at the final plan publication boundary: got=%d want=%d", beforePublish.checkCount(), finalCheck)
	}
}

func TestPrepareCompileRequirementsOrderBudgetAndMutationIsolation(t *testing.T) {
	entry := []byte("project p \"Project\" {}")
	types := []byte("entity_type service \"Service\" { representation shape rect }")
	zRef := testBlobRef("z-entry", "text/plain; charset=utf-8", entry)
	aRef := testBlobRef("a-types", "text/plain; charset=utf-8", types)
	request := compileRequest(entry)
	request.Payload.ProjectSourceTree[0].Blob = zRef
	request.Payload.ProjectSourceTree = append(request.Payload.ProjectSourceTree,
		engineprotocol.SourceFileInput{Path: "types.ldl", Blob: aRef},
		engineprotocol.SourceFileInput{Path: "copy.ldl", Blob: zRef},
	)
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	plan := mustPrepareCompile(t, dispatcher, compileContext(t), request)

	requirements := plan.BlobRequirements()
	if len(requirements) != 2 || requirements[0].Ref.BlobID != "a-types" || requirements[1].Ref.BlobID != "z-entry" || requirements[0].References != 1 || requirements[1].References != 2 {
		t.Fatalf("requirements=%+v", requirements)
	}
	budget := plan.AdmissionBudget()
	if budget.RequiredBlobCount != 2 || budget.RequiredBlobBytes != int64(len(entry)+len(types)) || budget.ProjectSourceFiles != 3 || budget.ProjectSourceBytes != int64(2*len(entry)+len(types)) || budget.EffectiveCompilerLimits.MaxProjectSourceBytes != engine.DefaultResourceLimits().MaxProjectSourceBytes {
		t.Fatalf("budget=%+v", budget)
	}

	request.Payload.ProjectSourceTree[0].Blob.BlobID = "caller-mutated"
	request.Payload.ProjectSourceTree[1].Path = "caller-mutated.ldl"
	requirements[0].Ref.BlobID = "returned-mutated"
	requirements[1].References = 99
	again := plan.BlobRequirements()
	if again[0].Ref.BlobID != "a-types" || again[1].Ref.BlobID != "z-entry" || again[1].References != 2 || plan.AdmissionBudget() != budget {
		t.Fatalf("plan metadata was mutable: requirements=%+v budget=%+v", again, plan.AdmissionBudget())
	}
	response, err := plan.Execute(context.Background(), &memoryBlobSource{definitions: []BlobDefinition{
		{BlobID: "z-entry", Reader: io.NopCloser(bytes.NewReader(entry))},
		{BlobID: "a-types", Reader: io.NopCloser(bytes.NewReader(types))},
	}}, &memoryBlobSink{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome == protocolcommon.OutcomeFailed || response.Outcome == protocolcommon.OutcomeCancelled {
		t.Fatalf("caller mutation crossed into execution: %+v", response)
	}
}

func TestPrepareCompileZeroByteRequirementAndBloblessSchemaTerminal(t *testing.T) {
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	negotiated := compileContext(t)
	request := compileRequest([]byte{})
	plan := mustPrepareCompile(t, dispatcher, negotiated, request)
	if requirements := plan.BlobRequirements(); len(requirements) != 1 || requirements[0].Ref.Size != "0" || plan.AdmissionBudget().RequiredBlobBytes != 0 {
		t.Fatalf("zero-byte plan=%+v budget=%+v", requirements, plan.AdmissionBudget())
	}
	releases := 0
	source := &memoryBlobSource{definitions: []BlobDefinition{{BlobID: "source", Owned: &OwnedBlob{Bytes: nil, Release: func() { releases++ }}}}}
	response, err := plan.Execute(context.Background(), source, &memoryBlobSink{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeRejected || releases != 1 {
		t.Fatalf("zero-byte response=%+v releases=%d", response, releases)
	}

	blobless := compileRequest([]byte("x"))
	blobless.Payload.ProjectSourceTree = []engineprotocol.SourceFileInput{}
	plan, terminal, err := dispatcher.PrepareCompile(context.Background(), negotiated, blobless)
	if err != nil {
		t.Fatal(err)
	}
	assertTerminalCompileResponse(t, plan, terminal, protocolcommon.OutcomeFailed)
}

func TestCompilePlanAdoptsOwnedBytesAndRejectsUnreferencedDefinitions(t *testing.T) {
	value := []byte("project p \"Project\" {}")
	request := compileRequest(value)
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	negotiated := compileContext(t)

	ownedValue := slices.Clone(value)
	releases := 0
	plan := mustPrepareCompile(t, dispatcher, negotiated, request)
	sink := &memoryBlobSink{}
	response, err := plan.Execute(context.Background(), &memoryBlobSource{definitions: []BlobDefinition{{BlobID: "source", Owned: &OwnedBlob{Bytes: ownedValue, Release: func() { releases++ }}}}}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeSuccess || releases != 1 || sink.calls != 1 {
		t.Fatalf("owned execution=%+v releases=%d sink=%d", response, releases, sink.calls)
	}

	requiredRelease, extraRelease := 0, 0
	plan = mustPrepareCompile(t, dispatcher, negotiated, request)
	sink = &memoryBlobSink{}
	response, err = plan.Execute(context.Background(), &memoryBlobSource{definitions: []BlobDefinition{
		{BlobID: "source", Owned: &OwnedBlob{Bytes: slices.Clone(value), Release: func() { requiredRelease++ }}},
		{BlobID: "unreferenced", Owned: &OwnedBlob{Bytes: []byte("x"), Release: func() { extraRelease++ }}},
	}}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeFailed || response.Failure == nil || response.Failure.Code != FailureCompileUnexpectedBlob || requiredRelease != 1 || extraRelease != 1 || sink.calls != 0 {
		t.Fatalf("unexpected definition response=%+v releases=%d/%d sink=%d", response, requiredRelease, extraRelease, sink.calls)
	}

	ref := request.Payload.ProjectSourceTree[0].Blob
	direct := slices.Clone(value)
	directReleases := 0
	owned, lease, failure := acquireBlobUses(context.Background(), []blobUse{{ref: ref}}, &memoryBlobSource{definitions: []BlobDefinition{{BlobID: ref.BlobID, Owned: &OwnedBlob{Bytes: direct, Release: func() { directReleases++ }}}}})
	if failure != nil {
		t.Fatal(failure)
	}
	if len(direct) != 0 && &owned[ref.BlobID][0] != &direct[0] {
		t.Fatal("owned Go bytes were redundantly copied")
	}
	if failure := lease.Release(context.Background()); failure != nil {
		t.Fatal(failure)
	}
	if directReleases != 1 {
		t.Fatalf("adopted bytes releases=%d", directReleases)
	}

	plan = mustPrepareCompile(t, dispatcher, negotiated, request)
	sink = &memoryBlobSink{}
	response, err = plan.Execute(context.Background(), &memoryBlobSource{definitions: []BlobDefinition{{BlobID: ref.BlobID, Owned: &OwnedBlob{Bytes: slices.Clone(value)}}}}, sink)
	if err != nil || response.Outcome != protocolcommon.OutcomeFailed || response.Failure == nil || response.Failure.Code != FailureCompileBlobSource || sink.calls != 0 {
		t.Fatalf("missing OwnedBlob release callback response=%+v err=%v sink=%d", response, err, sink.calls)
	}
}

type partialErrorBlobSource struct {
	definitions []BlobDefinition
	err         error
}

func (source partialErrorBlobSource) Definitions(context.Context) ([]BlobDefinition, error) {
	return source.definitions, source.err
}

func TestCompilePlanReleasesDefinitionsReturnedWithSourceError(t *testing.T) {
	value := []byte("project p \"Project\" {}")
	request := compileRequest(value)
	reader := &trackingReadCloser{reader: bytes.NewReader(value)}
	ownedReleases := 0
	ctx, cancel := context.WithCancel(context.Background())
	plan := mustPrepareCompile(t, NewCompileDispatcher(engine.New(engine.BuildInfo{})), compileContext(t), request)
	response, err := plan.Execute(ctx, partialErrorBlobSource{
		definitions: []BlobDefinition{
			{BlobID: "reader", Reader: reader},
			{BlobID: "owned", Owned: &OwnedBlob{Bytes: value, Release: func() {
				ownedReleases++
				cancel()
				panic("private cleanup panic")
			}}},
		},
		err: errors.New("private partial acquisition error"),
	}, &memoryBlobSink{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeFailed || response.Failure == nil || response.Failure.Code != FailureCompileBlobSource {
		t.Fatalf("source error lost stable precedence: %+v", response)
	}
	if reader.closes != 1 || ownedReleases != 1 {
		t.Fatalf("partial definitions cleanup reader=%d owned=%d", reader.closes, ownedReleases)
	}
}

func TestCompilePlanExecuteIsSingleUseAndOneShotParity(t *testing.T) {
	value := []byte("project p \"Project\" {}")
	request := compileRequest(value)
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	negotiated := compileContext(t)
	plan := mustPrepareCompile(t, dispatcher, negotiated, request)
	source := sourceFor(request.Payload.ProjectSourceTree[0].Blob, value)
	sink := &memoryBlobSink{}
	preparedResponse, err := plan.Execute(context.Background(), source, sink)
	if err != nil {
		t.Fatal(err)
	}
	if source.calls != 1 || sink.calls != 1 || plan.BlobRequirements() != nil || plan.AdmissionBudget() != (CompileAdmissionBudget{}) {
		t.Fatalf("plan not consumed: source=%d sink=%d requirements=%+v", source.calls, sink.calls, plan.BlobRequirements())
	}
	second, err := plan.Execute(context.Background(), sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), &memoryBlobSink{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Outcome != protocolcommon.OutcomeFailed || second.Failure == nil || second.Failure.Code != FailureCompilePlanConsumed {
		t.Fatalf("second execution=%+v", second)
	}

	oneShotSink := &memoryBlobSink{}
	oneShotResponse, err := dispatcher.DispatchCompile(context.Background(), negotiated, request, sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), oneShotSink)
	if err != nil {
		t.Fatal(err)
	}
	preparedBytes, _ := engineprotocol.EncodeCompileResponseEnvelope(preparedResponse)
	oneShotBytes, _ := engineprotocol.EncodeCompileResponseEnvelope(oneShotResponse)
	if !bytes.Equal(preparedBytes, oneShotBytes) || !equalOutputBlobs(sink.blobs, oneShotSink.blobs) {
		t.Fatal("Prepare+Execute differs from DispatchCompile")
	}
}

type gatedBlobSource struct {
	started     chan struct{}
	proceed     chan struct{}
	definitions []BlobDefinition
	once        sync.Once
}

func (source *gatedBlobSource) Definitions(ctx context.Context) ([]BlobDefinition, error) {
	source.once.Do(func() { close(source.started) })
	select {
	case <-source.proceed:
		return source.definitions, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type gatedBlobSink struct {
	started chan struct{}
	once    sync.Once
	calls   int
}

func (sink *gatedBlobSink) Publish(ctx context.Context, _ []OutputBlob) error {
	sink.calls++
	sink.once.Do(func() { close(sink.started) })
	<-ctx.Done()
	return ctx.Err()
}

func TestCompilePlanConcurrentExecuteAndAbortLifecycle(t *testing.T) {
	value := []byte("project p \"Project\" {}")
	request := compileRequest(value)
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	negotiated := compileContext(t)

	plan := mustPrepareCompile(t, dispatcher, negotiated, request)
	gated := &gatedBlobSource{started: make(chan struct{}), proceed: make(chan struct{}), definitions: []BlobDefinition{{BlobID: "source", Reader: io.NopCloser(bytes.NewReader(value))}}}
	firstResult := make(chan engineprotocol.CompileResponseEnvelope, 1)
	go func() {
		response, _ := plan.Execute(context.Background(), gated, &memoryBlobSink{})
		firstResult <- response
	}()
	<-gated.started
	concurrent, err := plan.Execute(context.Background(), sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), &memoryBlobSink{})
	if err != nil {
		t.Fatal(err)
	}
	if concurrent.Failure == nil || concurrent.Failure.Code != FailureCompilePlanConsumed {
		t.Fatalf("concurrent response=%+v", concurrent)
	}
	close(gated.proceed)
	if first := <-firstResult; first.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("first response=%+v", first)
	}

	plan = mustPrepareCompile(t, dispatcher, negotiated, request)
	plan.Abort()
	plan.Abort()
	if plan.BlobRequirements() != nil || plan.AdmissionBudget() != (CompileAdmissionBudget{}) {
		t.Fatal("abort retained plan-owned state")
	}
	aborted, err := plan.Execute(context.Background(), sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), &memoryBlobSink{})
	if err != nil {
		t.Fatal(err)
	}
	if aborted.Outcome != protocolcommon.OutcomeCancelled {
		t.Fatalf("aborted response=%+v", aborted)
	}

	plan = mustPrepareCompile(t, dispatcher, negotiated, request)
	gated = &gatedBlobSource{started: make(chan struct{}), proceed: make(chan struct{})}
	abortResult := make(chan engineprotocol.CompileResponseEnvelope, 1)
	go func() {
		response, _ := plan.Execute(context.Background(), gated, &memoryBlobSink{})
		abortResult <- response
	}()
	<-gated.started
	plan.Abort()
	plan.Abort()
	if response := <-abortResult; response.Outcome != protocolcommon.OutcomeCancelled {
		t.Fatalf("during-source abort=%+v", response)
	}

	plan = mustPrepareCompile(t, dispatcher, negotiated, request)
	publishSink := &gatedBlobSink{started: make(chan struct{})}
	publishResult := make(chan engineprotocol.CompileResponseEnvelope, 1)
	go func() {
		response, _ := plan.Execute(context.Background(), sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), publishSink)
		publishResult <- response
	}()
	<-publishSink.started
	plan.Abort()
	if response := <-publishResult; response.Outcome != protocolcommon.OutcomeCancelled || publishSink.calls != 1 {
		t.Fatalf("during-publish abort=%+v calls=%d", response, publishSink.calls)
	}
}

type panicBlobSink struct{}

func (panicBlobSink) Publish(context.Context, []OutputBlob) error { panic("private sink panic") }

func TestCompilePlanCancellationAndCallbackPanicsAreContained(t *testing.T) {
	value := []byte("project p \"Project\" {}")
	request := compileRequest(value)
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	negotiated := compileContext(t)

	plan := mustPrepareCompile(t, dispatcher, negotiated, request)
	cancelled, err := plan.Execute(cancelledContext(), sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), &memoryBlobSink{})
	if err != nil || cancelled.Outcome != protocolcommon.OutcomeCancelled {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}

	for _, test := range []struct {
		name   string
		source BlobSource
		sink   BlobSink
	}{
		{name: "source", source: panicBlobSource{}, sink: &memoryBlobSink{}},
		{name: "sink", source: sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), sink: panicBlobSink{}},
		{name: "release", source: &memoryBlobSource{definitions: []BlobDefinition{{BlobID: "source", Owned: &OwnedBlob{Bytes: slices.Clone(value), Release: func() { panic("private release panic") }}}}}, sink: &memoryBlobSink{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := mustPrepareCompile(t, dispatcher, negotiated, request)
			response, err := plan.Execute(context.Background(), test.source, test.sink)
			if err != nil {
				t.Fatal(err)
			}
			if response.Outcome != protocolcommon.OutcomeFailed || response.Failure == nil || strings.Contains(response.Failure.Message, "private") {
				t.Fatalf("unsafe response=%+v", response)
			}
		})
	}
}

func TestPrepareCompilePackAdmissionBudget(t *testing.T) {
	packSource := []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nexport { service }\n")
	manifest, err := json.Marshal(map[string]any{"format": "layerdraw-pack", "format_version": 1, "id": "pub/schema", "name": "schema", "version": "1.0.0", "language": 1, "entry": "pack.ldl", "dependencies": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	fileRef := testBlobRef("z-pack-file", "text/plain; charset=utf-8", packSource)
	manifestRef := testBlobRef("a-pack-manifest", "application/json", manifest)
	request := compileRequest([]byte("unused"))
	request.Payload.Mode = engineprotocol.CompileModePack
	request.Payload.EntryPath = "pack.ldl"
	root := engineprotocol.CanonicalPackSelector("pub/schema")
	request.Payload.RootPackID = &root
	request.Payload.ProjectSourceTree = []engineprotocol.SourceFileInput{}
	request.Payload.InstalledPackTree = []engineprotocol.SourceFileInput{{Path: "pack/schema/pack.ldl", Blob: fileRef}}
	request.Payload.ResolvedDependencies.Installs = []engineprotocol.ResolvedPack{{InstallName: "schema", CanonicalID: root, Version: "1.0.0", Digest: protocolcommon.Digest("sha256:" + strings.Repeat("a", 64)), Path: "pack/schema", Entry: "pack.ldl", Files: []engineprotocol.ResolvedPackFile{{Path: "pack.ldl", Digest: fileRef.Digest}}, Dependencies: []engineprotocol.ResolvedPackDependency{}, ManifestPath: "manifest.json", Manifest: manifestRef}}
	alias := request.Payload.ResolvedDependencies.Installs[0]
	alias.InstallName = "schema_alias"
	request.Payload.ResolvedDependencies.Installs = append(request.Payload.ResolvedDependencies.Installs, alias)
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	negotiated := compileContext(t)
	plan := mustPrepareCompile(t, dispatcher, negotiated, request)
	requirements := plan.BlobRequirements()
	budget := plan.AdmissionBudget()
	if len(requirements) != 2 || requirements[0].Ref.BlobID != "a-pack-manifest" || requirements[0].References != 2 || requirements[1].Ref.BlobID != "z-pack-file" || budget.RequiredBlobBytes != int64(len(packSource)+len(manifest)) || budget.InstalledPackFiles != 1 || budget.ResolvedPackFiles != 4 || budget.PackBytes != int64(len(packSource)+2*len(manifest)) {
		t.Fatalf("requirements=%+v budget=%+v", requirements, budget)
	}
	response, err := plan.Execute(context.Background(), &memoryBlobSource{definitions: []BlobDefinition{
		{BlobID: fileRef.BlobID, Reader: io.NopCloser(bytes.NewReader(packSource))},
		{BlobID: manifestRef.BlobID, Reader: io.NopCloser(bytes.NewReader(manifest))},
	}}, &memoryBlobSink{})
	if err != nil || response.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("same-canonical alias execution response=%+v err=%v", response, err)
	}

	collision := cloneCompileRequest(t, request)
	collision.Payload.ResolvedDependencies.Installs[1].CanonicalID = engineprotocol.CanonicalPackSelector("pub/other")
	plan, terminal, err := dispatcher.PrepareCompile(context.Background(), negotiated, collision)
	assertCompilePreparationExclusive(t, plan, terminal, err)
	assertTerminalCompileResponse(t, plan, terminal, protocolcommon.OutcomeRejected)
	if len(terminal.Diagnostics) != 1 || terminal.Diagnostics[0].MessageKey != "invalid_closed_input_duplicate_pack_path" {
		t.Fatalf("different-canonical installed path collision=%+v", terminal.Diagnostics)
	}
}

func TestPrepareCompileCallerMisuse(t *testing.T) {
	request := compileRequest([]byte("project p \"P\" {}"))
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	if _, _, err := (*CompileDispatcher)(nil).PrepareCompile(context.Background(), nil, request); err == nil {
		t.Fatal("nil dispatcher accepted")
	}
	if _, _, err := dispatcher.PrepareCompile(nil, nil, request); err == nil {
		t.Fatal("nil context accepted")
	}
	request.RequestID = ""
	if _, _, err := dispatcher.PrepareCompile(context.Background(), nil, request); err == nil {
		t.Fatal("empty request ID accepted")
	}
	request = compileRequest([]byte("project p \"P\" {}"))
	plan, terminal, err := NewCompileDispatcher(engine.Engine{}).PrepareCompile(context.Background(), compileContext(t), request)
	assertCompilePreparationExclusive(t, plan, terminal, err)
	if err == nil || plan != nil || terminal != nil {
		t.Fatalf("invalid Engine release outcome plan=%v terminal=%+v err=%v", plan, terminal, err)
	}
	invalidResponse, responseErr := compileFailedResponse(request.RequestID, "", invariantProtocolFailure())
	plan, terminal, err = terminalCompilePreparation(invalidResponse, responseErr)
	assertCompilePreparationExclusive(t, plan, terminal, err)
	if err == nil || plan != nil || terminal != nil {
		t.Fatalf("response construction failure outcome plan=%v terminal=%+v err=%v", plan, terminal, err)
	}
	if _, err := (*CompilePlan)(nil).Execute(context.Background(), nil, nil); err == nil {
		t.Fatal("nil plan accepted")
	}
	if (*CompilePlan)(nil).BlobRequirements() != nil || (*CompilePlan)(nil).AdmissionBudget() != (CompileAdmissionBudget{}) {
		t.Fatal("nil plan metadata was nonzero")
	}
	plan = mustPrepareCompile(t, dispatcher, compileContext(t), compileRequest([]byte("project p \"P\" {}")))
	if _, err := plan.Execute(nil, nil, nil); err == nil {
		t.Fatal("nil Execute context accepted")
	}
	plan.Abort()
}

func TestCompilePlanningEdgeAccountingAndReleasePaths(t *testing.T) {
	(*CompilePlan)(nil).Abort()
	if requirements, bytes, failure := buildBlobRequirements(context.Background(), nil); failure != nil || len(requirements) != 0 || bytes != 0 {
		t.Fatalf("blobless accounting=%+v bytes=%d failure=%+v", requirements, bytes, failure)
	}
	first := protocolcommon.BlobRef{BlobID: "a", Digest: protocolcommon.Digest("sha256:" + strings.Repeat("a", 64)), Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: "application/octet-stream", Size: protocolcommon.CanonicalUint64("9223372036854775807")}
	second := first
	second.BlobID = "b"
	if _, _, failure := buildBlobRequirements(context.Background(), []blobUse{{ref: first}, {ref: second}}); failure == nil || failure.Code != FailureCompileBlobOversized {
		t.Fatalf("aggregate overflow=%+v", failure)
	}
	conflict := first
	conflict.MediaType = "text/plain"
	if _, _, failure := buildBlobRequirements(context.Background(), []blobUse{{ref: first}, {ref: conflict}}); failure == nil || failure.Code != FailureCompileConflictingBlobRef {
		t.Fatalf("conflicting requirement=%+v", failure)
	}

	value := []byte("x")
	ref := testBlobRef("x", "application/octet-stream", value)
	if owned, failure := resolveBlobUses(context.Background(), []blobUse{{ref: ref}}, &memoryBlobSource{definitions: []BlobDefinition{{BlobID: "x", Reader: io.NopCloser(bytes.NewReader(value))}}}); failure != nil || !bytes.Equal(owned["x"], value) {
		t.Fatalf("resolved=%q failure=%+v", owned["x"], failure)
	}
	reader := &trackingReadCloser{reader: bytes.NewReader(value)}
	ownedReleases := 0
	_, _, failure := acquireBlobUses(context.Background(), []blobUse{{ref: ref}}, &memoryBlobSource{definitions: []BlobDefinition{{BlobID: "x", Reader: reader, Owned: &OwnedBlob{Bytes: value, Release: func() { ownedReleases++ }}}}})
	if failure == nil || reader.closes != 1 || ownedReleases != 1 {
		t.Fatalf("invalid mixed definition failure=%+v closes=%d releases=%d", failure, reader.closes, ownedReleases)
	}

	request := compileRequest([]byte("project p \"P\" {}"))
	plan := mustPrepareCompile(t, NewCompileDispatcher(engine.New(engine.BuildInfo{})), compileContext(t), request)
	if plan.claimPublication(context.Background()) {
		t.Fatal("unstarted plan claimed publication")
	}
	plan.Abort()
	_, _, releaseFailure := mapCompileInput(context.Background(), compileContext(t), request.Payload, &memoryBlobSource{definitions: []BlobDefinition{{BlobID: "source", Owned: &OwnedBlob{Bytes: []byte("project p \"P\" {}"), Release: func() { panic("release") }}}}})
	if releaseFailure == nil || releaseFailure.Code != FailureCompileBlobSource {
		t.Fatalf("mapping release failure=%+v", releaseFailure)
	}
}

func TestCompilePreparationLoopCancellationBoundaries(t *testing.T) {
	value := []byte("x")
	ref := testBlobRef("x", "application/octet-stream", value)
	packID := engineprotocol.CanonicalPackSelector("pub/p")
	file := engineprotocol.SourceFileInput{Path: "x.ldl", Blob: ref}
	pack := engineprotocol.ResolvedPack{CanonicalID: packID, Files: []engineprotocol.ResolvedPackFile{{Path: "x.ldl"}}, Manifest: ref}
	asset := engineprotocol.AssetInput{Origin: engineprotocol.SourceOriginKindProject, Locator: "x", Blob: ref, Digest: ref.Digest, MediaType: ref.MediaType}

	if _, _, failure := mapCompileInput(cancelledContext(), compileContext(t), compileRequest(value).Payload, nil); failure == nil || failure.Category != protocolcommon.ProtocolFailureCategoryCancelled {
		t.Fatalf("cancelled input mapping failure=%+v", failure)
	}

	duplicateCases := []struct {
		name     string
		input    engineprotocol.CompileInput
		cancelAt int
	}{
		{name: "installed paths", input: engineprotocol.CompileInput{InstalledPackTree: []engineprotocol.SourceFileInput{file}}, cancelAt: 1},
		{name: "pack", input: engineprotocol.CompileInput{ResolvedDependencies: engineprotocol.ResolvedDependencies{Installs: []engineprotocol.ResolvedPack{pack}}}, cancelAt: 1},
		{name: "pack file", input: engineprotocol.CompileInput{ResolvedDependencies: engineprotocol.ResolvedDependencies{Installs: []engineprotocol.ResolvedPack{pack}}}, cancelAt: 2},
		{name: "pack dependency", input: engineprotocol.CompileInput{ResolvedDependencies: engineprotocol.ResolvedDependencies{Installs: []engineprotocol.ResolvedPack{{CanonicalID: packID, Dependencies: []engineprotocol.ResolvedPackDependency{{LocalName: "x"}}}}}}, cancelAt: 2},
		{name: "asset", input: engineprotocol.CompileInput{ReferencedAssets: []engineprotocol.AssetInput{asset}}, cancelAt: 1},
		{name: "duplicate class", input: engineprotocol.CompileInput{}, cancelAt: 1},
		{name: "sorted values", input: engineprotocol.CompileInput{ProjectSourceTree: []engineprotocol.SourceFileInput{file, file}}, cancelAt: 4},
	}
	for _, test := range duplicateCases {
		t.Run("duplicates/"+test.name, func(t *testing.T) {
			if _, failure := validateLogicalDuplicates(newCancelAfterChecksContext(test.cancelAt), test.input); failure == nil || failure.Category != protocolcommon.ProtocolFailureCategoryCancelled {
				t.Fatalf("failure=%+v", failure)
			}
		})
	}

	enumerationCases := []engineprotocol.CompileInput{
		{ProjectSourceTree: []engineprotocol.SourceFileInput{file}},
		{InstalledPackTree: []engineprotocol.SourceFileInput{file}},
		{ResolvedDependencies: engineprotocol.ResolvedDependencies{Installs: []engineprotocol.ResolvedPack{pack}}},
		{ReferencedAssets: []engineprotocol.AssetInput{asset}},
	}
	for index, input := range enumerationCases {
		if _, failure := enumerateBlobUses(cancelledContext(), input); failure == nil || failure.Category != protocolcommon.ProtocolFailureCategoryCancelled {
			t.Fatalf("enumeration %d failure=%+v", index, failure)
		}
	}
	if failure := validateBlobAliases(cancelledContext(), []blobUse{{ref: ref}, {ref: ref}}); failure == nil || failure.Category != protocolcommon.ProtocolFailureCategoryCancelled {
		t.Fatalf("alias failure=%+v", failure)
	}
	if _, _, failure := buildBlobRequirements(cancelledContext(), []blobUse{{ref: ref}}); failure == nil || failure.Category != protocolcommon.ProtocolFailureCategoryCancelled {
		t.Fatalf("requirement failure=%+v", failure)
	}
	if _, _, failure := buildBlobRequirements(newCancelAfterChecksContext(2), []blobUse{{ref: ref}, {ref: ref}}); failure == nil || failure.Category != protocolcommon.ProtocolFailureCategoryCancelled {
		t.Fatalf("aliased requirement failure=%+v", failure)
	}

	limits := engine.DefaultResourceLimits()
	admissionCases := []struct {
		name     string
		input    engineprotocol.CompileInput
		cancelAt int
	}{
		{name: "start", input: engineprotocol.CompileInput{}, cancelAt: 1},
		{name: "metadata", input: engineprotocol.CompileInput{ResolvedDependencies: engineprotocol.ResolvedDependencies{Installs: []engineprotocol.ResolvedPack{pack}}}, cancelAt: 2},
		{name: "project", input: engineprotocol.CompileInput{ProjectSourceTree: []engineprotocol.SourceFileInput{file}}, cancelAt: 2},
		{name: "installed", input: engineprotocol.CompileInput{InstalledPackTree: []engineprotocol.SourceFileInput{file}}, cancelAt: 2},
		{name: "manifest", input: engineprotocol.CompileInput{ResolvedDependencies: engineprotocol.ResolvedDependencies{Installs: []engineprotocol.ResolvedPack{pack}}}, cancelAt: 3},
		{name: "asset", input: engineprotocol.CompileInput{ReferencedAssets: []engineprotocol.AssetInput{asset}}, cancelAt: 2},
	}
	for _, test := range admissionCases {
		t.Run("admission/"+test.name, func(t *testing.T) {
			if _, failure := compileAdmissionBudget(newCancelAfterChecksContext(test.cancelAt), test.input, limits, 0, 0); failure == nil || failure.Category != protocolcommon.ProtocolFailureCategoryCancelled {
				t.Fatalf("failure=%+v", failure)
			}
		})
	}
}
