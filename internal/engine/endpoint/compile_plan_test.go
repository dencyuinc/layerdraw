// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"bytes"
	"context"
	"encoding/json"
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
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil || terminal != nil {
		t.Fatalf("unexpected preparation: plan=%v terminal=%+v", plan, terminal)
	}
	return plan
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
		{name: "unnegotiated", context: context.Background(), mutate: func(*engineprotocol.CompileRequestEnvelope) {}, outcome: protocolcommon.OutcomeFailed, code: FailureCompileUnnegotiated},
		{name: "cancelled", context: cancelledContext(), contextValue: negotiated, mutate: func(*engineprotocol.CompileRequestEnvelope) {}, outcome: protocolcommon.OutcomeCancelled, code: FailureCompileCancelled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := cloneCompileRequest(t, base)
			test.mutate(&request)
			plan, response, err := dispatcher.PrepareCompile(test.context, test.contextValue, request)
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
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
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
	plan.Abort()
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
	owned, lease, failure := acquireBlobUses(context.Background(), []blobUse{{ref: ref}}, &memoryBlobSource{definitions: []BlobDefinition{{BlobID: ref.BlobID, Owned: &OwnedBlob{Bytes: direct}}}})
	if failure != nil {
		t.Fatal(failure)
	}
	if len(direct) != 0 && &owned[ref.BlobID][0] != &direct[0] {
		t.Fatal("owned Go bytes were redundantly copied")
	}
	if failure := lease.Release(context.Background()); failure != nil {
		t.Fatal(failure)
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
	packSource := []byte("entity_type service \"Service\" { representation shape rect }\nexport { service }\n")
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
	plan := mustPrepareCompile(t, NewCompileDispatcher(engine.New(engine.BuildInfo{})), compileContext(t), request)
	requirements := plan.BlobRequirements()
	budget := plan.AdmissionBudget()
	if len(requirements) != 2 || requirements[0].Ref.BlobID != "a-pack-manifest" || requirements[1].Ref.BlobID != "z-pack-file" || budget.RequiredBlobBytes != int64(len(packSource)+len(manifest)) || budget.InstalledPackFiles != 1 || budget.ResolvedPackFiles != 2 || budget.PackBytes != int64(len(packSource)+len(manifest)) {
		t.Fatalf("requirements=%+v budget=%+v", requirements, budget)
	}
	plan.Abort()
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
	if _, err := (*CompilePlan)(nil).Execute(context.Background(), nil, nil); err == nil {
		t.Fatal("nil plan accepted")
	}
	plan := mustPrepareCompile(t, dispatcher, compileContext(t), compileRequest([]byte("project p \"P\" {}")))
	if _, err := plan.Execute(nil, nil, nil); err == nil {
		t.Fatal("nil Execute context accepted")
	}
	plan.Abort()
}
