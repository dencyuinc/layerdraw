// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package mcphost

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

const testDigest = protocolcommon.Digest("sha256:0000000000000000000000000000000000000000000000000000000000000000")

type transportStub struct {
	mu       sync.Mutex
	handler  Handler
	starts   int
	stops    int
	startErr error
	stopErr  error
	panic    bool
}

func (t *transportStub) Start(_ context.Context, handler Handler) error {
	if t.panic {
		panic("transport secret")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handler, t.starts = handler, t.starts+1
	return t.startErr
}
func (t *transportStub) Shutdown(context.Context) error {
	if t.panic {
		panic("transport shutdown secret")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handler = nil
	t.stops++
	return t.stopErr
}

type ownerStub struct {
	mu        sync.Mutex
	snapshot  CapabilitySnapshot
	requests  []OwnerRequest
	resources []ResourceRequest
	invoke    func(context.Context, OwnerRequest) (OwnerResponse, error)
	read      func(context.Context, ResourceRequest) (ResourceResponse, error)
	panicCaps bool
	capsErr   error
}

func (o *ownerStub) Capabilities(context.Context) (CapabilitySnapshot, error) {
	if o.panicCaps {
		panic("credential=private")
	}
	if o.capsErr != nil {
		return CapabilitySnapshot{}, o.capsErr
	}
	return o.snapshot, nil
}
func (o *ownerStub) Invoke(ctx context.Context, r OwnerRequest) (OwnerResponse, error) {
	o.mu.Lock()
	o.requests = append(o.requests, r)
	o.mu.Unlock()
	if o.invoke != nil {
		return o.invoke(ctx, r)
	}
	return OwnerResponse{Content: json.RawMessage(`{"ok":true}`), Items: 1}, nil
}
func (o *ownerStub) ReadResource(ctx context.Context, r ResourceRequest) (ResourceResponse, error) {
	o.mu.Lock()
	o.resources = append(o.resources, r)
	o.mu.Unlock()
	if o.read != nil {
		return o.read(ctx, r)
	}
	return ResourceResponse{Content: json.RawMessage(`{"items":[]}`), Items: 0, MimeType: "application/json"}, nil
}

func snapshot(operations ...string) CapabilitySnapshot {
	values := map[string]OperationCapability{}
	for _, operation := range operations {
		values[operation] = OperationCapability{Enabled: true, InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`)}
	}
	return CapabilitySnapshot{ManifestETag: "manifest-1", Operations: values, Resources: []ResourceCapability{}, GrantSummary: accessprotocol.AuthoringGrantSummary{AccessFingerprint: testDigest, ConstrainedCapabilities: []semantic.AuthoringCapability{}, GrantedCapabilities: []semantic.AuthoringCapability{}, PolicyEtag: testDigest}}
}

func newRunning(t *testing.T, owner *ownerStub, limits Limits) (*Host, *transportStub) {
	t.Helper()
	transport := &transportStub{}
	host, err := New(Config{Owner: owner, Transport: transport, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	if err = host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	return host, transport
}
func binding() *Binding {
	return &Binding{DocumentID: "doc-1", RevisionDigest: string(testDigest), AccessFingerprint: string(testDigest)}
}

func TestCatalogMatchesNormativeToolNames(t *testing.T) {
	want := strings.Fields(`layerdraw.get_capabilities layerdraw.list_modules layerdraw.find_symbols layerdraw.search layerdraw.read_declarations layerdraw.read_rows layerdraw.get_neighbors layerdraw.inspect_subgraph layerdraw.find_usages layerdraw.list_references layerdraw.read_references layerdraw.preview_operations layerdraw.preview_fragment layerdraw.preview_source_patch layerdraw.apply_operations layerdraw.apply_source_patch layerdraw.stage_asset layerdraw.format_scope layerdraw.organize_workspace layerdraw.run_query layerdraw.analyze_graph layerdraw.materialize_view layerdraw.plan_export layerdraw.serialize_export layerdraw.import_document layerdraw.export_document layerdraw.list_revisions layerdraw.restore_revision layerdraw.registry_search layerdraw.registry_plan_install layerdraw.registry_apply_install`)
	got := []string{"layerdraw.get_capabilities"}
	for _, mapping := range toolCatalog {
		got = append(got, mapping.name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog mismatch\n got %v\nwant %v", got, want)
	}
}

func TestAdvertisesOnlyOwnerCapabilitiesAndPreservesSchemas(t *testing.T) {
	s := snapshot("engine.list_modules", "runtime.search")
	s.Operations["runtime.search"] = OperationCapability{Enabled: false, InputSchema: json.RawMessage(`{"disabled":true}`), OutputSchema: json.RawMessage(`{}`)}
	host, _ := newRunning(t, &ownerStub{snapshot: s}, DefaultLimits())
	tools, failure := host.ListTools(context.Background())
	if failure != nil {
		t.Fatal(failure)
	}
	if len(tools) != 2 || tools[0].Name != "layerdraw.get_capabilities" || tools[1].Name != "layerdraw.list_modules" {
		t.Fatalf("tools=%+v", tools)
	}
	tools[1].InputSchema[0] = '['
	if s.Operations["engine.list_modules"].InputSchema[0] != '{' {
		t.Fatal("schema was not defensively copied")
	}
}

func TestCallMapsExactlyOnceAndCursorIsOneShotBound(t *testing.T) {
	owner := &ownerStub{snapshot: snapshot("engine.list_modules")}
	owner.invoke = func(_ context.Context, r OwnerRequest) (OwnerResponse, error) {
		if len(r.Continuation) == 0 {
			return OwnerResponse{Content: json.RawMessage(`{"page":1}`), NextCursor: json.RawMessage(`{"owner":"next"}`), Items: 1}, nil
		}
		if string(r.Continuation) != `{"owner":"next"}` {
			t.Fatalf("continuation=%s", r.Continuation)
		}
		return OwnerResponse{Content: json.RawMessage(`{"page":2}`), Items: 1}, nil
	}
	host, _ := newRunning(t, owner, DefaultLimits())
	request := CallToolRequest{Name: "layerdraw.list_modules", RequestID: "r1", Arguments: json.RawMessage(`{"limit":1}`), Binding: binding()}
	first := host.CallTool(context.Background(), request)
	if first.Failure != nil || first.Cursor == "" {
		t.Fatalf("first=%+v", first)
	}
	request.Cursor = first.Cursor
	second := host.CallTool(context.Background(), request)
	if second.Failure != nil || string(second.Content) != `{"page":2}` {
		t.Fatalf("second=%+v", second)
	}
	replay := host.CallTool(context.Background(), request)
	if replay.Failure == nil || replay.Failure.Code != ErrorInvalidCursor {
		t.Fatalf("replay=%+v", replay)
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if len(owner.requests) != 2 || owner.requests[0].Operation != "engine.list_modules" {
		t.Fatalf("requests=%+v", owner.requests)
	}
}

func TestCursorRejectsChangedRequestRevisionAndAccess(t *testing.T) {
	for _, change := range []string{"arguments", "revision", "access"} {
		t.Run(change, func(t *testing.T) {
			owner := &ownerStub{snapshot: snapshot("engine.list_modules")}
			owner.invoke = func(context.Context, OwnerRequest) (OwnerResponse, error) {
				return OwnerResponse{Content: json.RawMessage(`{}`), NextCursor: json.RawMessage(`{"next":1}`)}, nil
			}
			host, _ := newRunning(t, owner, DefaultLimits())
			request := CallToolRequest{Name: "layerdraw.list_modules", RequestID: "r", Arguments: json.RawMessage(`{}`), Binding: binding()}
			first := host.CallTool(context.Background(), request)
			request.Cursor = first.Cursor
			switch change {
			case "arguments":
				request.Arguments = json.RawMessage(`{"changed":true}`)
			case "revision":
				request.Binding.RevisionDigest = "sha256:changed"
			case "access":
				request.Binding.AccessFingerprint = "sha256:changed"
			}
			result := host.CallTool(context.Background(), request)
			if result.Failure == nil || result.Failure.Code != ErrorInvalidCursor {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestBoundsMalformedAndOwnerFailuresFailClosed(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxInputBytes = 32
	limits.MaxOutputBytes = 16
	limits.MaxItems = 2
	limits.MaxJSONDepth = 2
	cases := []struct {
		name   string
		args   json.RawMessage
		invoke func(context.Context, OwnerRequest) (OwnerResponse, error)
		want   ErrorCode
	}{
		{"malformed", json.RawMessage(`{"x":`), nil, ErrorInvalidRequest},
		{"deep", json.RawMessage(`{"a":{"b":{"c":1}}}`), nil, ErrorInvalidRequest},
		{"output", json.RawMessage(`{}`), func(context.Context, OwnerRequest) (OwnerResponse, error) {
			return OwnerResponse{Content: json.RawMessage(`{"too":"large output"}`)}, nil
		}, ErrorResourceExhausted},
		{"items", json.RawMessage(`{}`), func(context.Context, OwnerRequest) (OwnerResponse, error) {
			return OwnerResponse{Content: json.RawMessage(`{}`), Items: 3}, nil
		}, ErrorResourceExhausted},
		{"stale", json.RawMessage(`{}`), func(context.Context, OwnerRequest) (OwnerResponse, error) {
			return OwnerResponse{}, &OwnerError{Code: ErrorStaleBinding}
		}, ErrorStaleBinding},
		{"panic", json.RawMessage(`{}`), func(context.Context, OwnerRequest) (OwnerResponse, error) { panic("/secret/path token") }, ErrorOwnerFailure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner := &ownerStub{snapshot: snapshot("engine.list_modules"), invoke: tc.invoke}
			host, _ := newRunning(t, owner, limits)
			result := host.CallTool(context.Background(), CallToolRequest{Name: "layerdraw.list_modules", RequestID: "r", Arguments: tc.args, Binding: binding()})
			if result.Failure == nil || result.Failure.Code != tc.want {
				t.Fatalf("result=%+v", result)
			}
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), "secret") {
				t.Fatal("private detail leaked")
			}
		})
	}
}

func TestResourceCursorAndCapabilityResource(t *testing.T) {
	s := snapshot()
	s.Resources = []ResourceCapability{{URI: "layerdraw://documents/current/revisions", Name: "Revisions", Description: "Committed revisions", MimeType: "application/json", Schema: json.RawMessage(`{"type":"object"}`), Bound: true}}
	owner := &ownerStub{snapshot: s}
	owner.read = func(_ context.Context, r ResourceRequest) (ResourceResponse, error) {
		if len(r.Continuation) == 0 {
			return ResourceResponse{Content: json.RawMessage(`{"page":1}`), NextCursor: json.RawMessage(`"next"`), Items: 1, MimeType: "application/json"}, nil
		}
		return ResourceResponse{Content: json.RawMessage(`{"page":2}`), Items: 1, MimeType: "application/json"}, nil
	}
	host, _ := newRunning(t, owner, DefaultLimits())
	resources, failure := host.ListResources(context.Background())
	if failure != nil || len(resources) != 2 {
		t.Fatalf("resources=%v failure=%v", resources, failure)
	}
	cap := host.ReadResource(context.Background(), ReadResourceRequest{URI: "layerdraw://capabilities"})
	if cap.Failure != nil || !json.Valid(cap.Content) {
		t.Fatalf("cap=%+v", cap)
	}
	first := host.ReadResource(context.Background(), ReadResourceRequest{URI: s.Resources[0].URI, Binding: binding()})
	if first.Failure != nil || first.Cursor == "" {
		t.Fatalf("first=%+v", first)
	}
	second := host.ReadResource(context.Background(), ReadResourceRequest{URI: s.Resources[0].URI, Binding: binding(), Cursor: first.Cursor})
	if second.Failure != nil || string(second.Content) != `{"page":2}` {
		t.Fatalf("second=%+v", second)
	}
}

func TestCancellationShutdownAndRestartFenceInflight(t *testing.T) {
	started := make(chan struct{})
	owner := &ownerStub{snapshot: snapshot("engine.list_modules")}
	owner.invoke = func(ctx context.Context, _ OwnerRequest) (OwnerResponse, error) {
		close(started)
		<-ctx.Done()
		return OwnerResponse{}, ctx.Err()
	}
	host, transport := newRunning(t, owner, DefaultLimits())
	result := make(chan CallToolResult, 1)
	go func() {
		result <- host.CallTool(context.Background(), CallToolRequest{Name: "layerdraw.list_modules", RequestID: "r", Arguments: json.RawMessage(`{}`), Binding: binding()})
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := host.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	got := <-result
	if got.Failure == nil || got.Failure.Code != ErrorCancelled {
		t.Fatalf("result=%+v", got)
	}
	if unavailable := host.CallTool(context.Background(), CallToolRequest{}); unavailable.Failure == nil || unavailable.Failure.Code != ErrorTransport {
		t.Fatalf("unavailable=%+v", unavailable)
	}
	if err := host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	transport.mu.Lock()
	starts, stops := transport.starts, transport.stops
	transport.mu.Unlock()
	if starts != 2 || stops != 1 {
		t.Fatalf("starts=%d stops=%d", starts, stops)
	}
}

func TestInvalidCompositionAndCapabilityPanic(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("missing composition accepted")
	}
	transport := &transportStub{}
	host, err := New(Config{Owner: &ownerStub{panicCaps: true}, Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	if err = host.Start(context.Background()); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("err=%v", err)
	}
	transport.panic = true
	host, err = New(Config{Owner: &ownerStub{snapshot: snapshot()}, Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	if err = host.Start(context.Background()); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err=%v", err)
	}
	transport.panic = false
	if err = host.Start(context.Background()); err != nil {
		t.Fatalf("restart after panic=%v", err)
	}
	transport.panic = true
	if err = host.Shutdown(context.Background()); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("shutdown panic=%v", err)
	}
	transport.panic = false
	if err = host.Shutdown(context.Background()); err != nil {
		t.Fatalf("cleanup after panic=%v", err)
	}
}

func TestLifecycleAndLimitFailureBranches(t *testing.T) {
	invalid := DefaultLimits()
	invalid.MaxItems = 0
	if _, err := New(Config{Owner: &ownerStub{}, Transport: &transportStub{}, Limits: invalid}); err == nil {
		t.Fatal("invalid limits accepted")
	}
	owner := &ownerStub{snapshot: snapshot()}
	transport := &transportStub{startErr: errors.New("provider detail")}
	host, err := New(Config{Owner: owner, Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	if err = host.Start(context.Background()); err == nil || strings.Contains(err.Error(), "provider") {
		t.Fatalf("start err=%v", err)
	}
	transport.startErr = nil
	if err = host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = host.Start(context.Background()); err == nil {
		t.Fatal("double start accepted")
	}
	transport.stopErr = errors.New("private stop")
	if err = host.Shutdown(context.Background()); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("stop err=%v", err)
	}
	if err = host.Shutdown(context.Background()); err != nil {
		t.Fatalf("idempotent stop=%v", err)
	}
}

func TestInvalidCapabilitySnapshotsFailClosed(t *testing.T) {
	cases := map[string]CapabilitySnapshot{}
	base := snapshot("engine.list_modules")
	missingETag := base
	missingETag.ManifestETag = ""
	cases["etag"] = missingETag
	badGrant := base
	badGrant.GrantSummary.AccessFingerprint = "bad"
	cases["grant"] = badGrant
	badInput := base
	badInput.Operations = map[string]OperationCapability{"engine.list_modules": {Enabled: true, InputSchema: json.RawMessage(`{`), OutputSchema: json.RawMessage(`{}`)}}
	cases["schema"] = badInput
	badResource := base
	badResource.Resources = []ResourceCapability{{URI: "", Name: "x", MimeType: "application/json", Schema: json.RawMessage(`{}`)}}
	cases["resource"] = badResource
	duplicate := base
	duplicate.Resources = []ResourceCapability{{URI: "x", Name: "x", MimeType: "application/json", Schema: json.RawMessage(`{}`)}, {URI: "x", Name: "y", MimeType: "application/json", Schema: json.RawMessage(`{}`)}}
	cases["duplicate"] = duplicate
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			host, err := New(Config{Owner: &ownerStub{snapshot: value}, Transport: &transportStub{}})
			if err != nil {
				t.Fatal(err)
			}
			if err = host.Start(context.Background()); err == nil {
				t.Fatal("invalid snapshot accepted")
			}
		})
	}
	host, err := New(Config{Owner: &ownerStub{capsErr: errors.New("private")}, Transport: &transportStub{}})
	if err != nil {
		t.Fatal(err)
	}
	if err = host.Start(context.Background()); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("err=%v", err)
	}
}

func TestCallAdditionalBoundsAndTypedFailures(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxInputBytes = 4
	owner := &ownerStub{snapshot: snapshot("engine.list_modules")}
	host, _ := newRunning(t, owner, limits)
	oversize := host.CallTool(context.Background(), CallToolRequest{Name: "layerdraw.list_modules", RequestID: "r", Arguments: json.RawMessage(`{"xx":1}`), Binding: binding()})
	if oversize.Failure == nil || oversize.Failure.Code != ErrorInvalidRequest {
		t.Fatalf("oversize=%+v", oversize)
	}
	missing := host.CallTool(context.Background(), CallToolRequest{Name: "layerdraw.list_modules", RequestID: "r", Arguments: json.RawMessage(`{}`)})
	if missing.Failure == nil || missing.Failure.Code != ErrorStaleBinding {
		t.Fatalf("missing=%+v", missing)
	}
	owner.invoke = func(context.Context, OwnerRequest) (OwnerResponse, error) {
		return OwnerResponse{}, &OwnerError{Code: ErrorCancelled}
	}
	cancelled := host.CallTool(context.Background(), CallToolRequest{Name: "layerdraw.list_modules", RequestID: "r", Arguments: json.RawMessage(`{}`), Binding: binding()})
	if cancelled.Failure == nil || cancelled.Failure.Code != ErrorCancelled || !cancelled.Failure.Retryable {
		t.Fatalf("cancelled=%+v", cancelled)
	}
	owner.invoke = func(context.Context, OwnerRequest) (OwnerResponse, error) {
		return OwnerResponse{}, &OwnerError{Code: ErrorTransport}
	}
	closed := host.CallTool(context.Background(), CallToolRequest{Name: "layerdraw.list_modules", RequestID: "r", Arguments: json.RawMessage(`{}`), Binding: binding()})
	if closed.Failure == nil || closed.Failure.Code != ErrorOwnerFailure {
		t.Fatalf("closed=%+v", closed)
	}
}

func TestCapabilityToolAndStructuralValidationBranches(t *testing.T) {
	if (&OwnerError{Code: ErrorStaleBinding}).Error() != string(ErrorStaleBinding) {
		t.Fatal("owner error code changed")
	}
	if !validJSON(json.RawMessage(`[{"a":[1]}]`), 4) || validJSON(json.RawMessage(`[{"a":[1]}]`), 2) {
		t.Fatal("array/map depth bound failed")
	}
	if emptyObject(json.RawMessage(`[]`)) || emptyObject(json.RawMessage(`null`)) || !emptyObject(json.RawMessage(` { } `)) {
		t.Fatal("empty object validation failed")
	}
	owner := &ownerStub{snapshot: snapshot()}
	host, _ := newRunning(t, owner, DefaultLimits())
	good := host.CallTool(context.Background(), CallToolRequest{Name: "layerdraw.get_capabilities", Arguments: json.RawMessage(` { } `)})
	if good.Failure != nil || !json.Valid(good.Content) {
		t.Fatalf("good=%+v", good)
	}
	cursor := host.CallTool(context.Background(), CallToolRequest{Name: "layerdraw.get_capabilities", Cursor: "x"})
	if cursor.Failure == nil || cursor.Failure.Code != ErrorInvalidRequest {
		t.Fatalf("cursor=%+v", cursor)
	}
	badID := host.CallTool(context.Background(), CallToolRequest{Name: "layerdraw.list_modules", RequestID: strings.Repeat("x", 129), Arguments: json.RawMessage(`{}`), Binding: binding()})
	if badID.Failure == nil || badID.Failure.Code != ErrorInvalidRequest {
		t.Fatalf("badID=%+v", badID)
	}
	disabled := host.CallTool(context.Background(), CallToolRequest{Name: "layerdraw.list_modules", RequestID: "x", Arguments: json.RawMessage(`{}`), Binding: binding()})
	if disabled.Failure == nil || disabled.Failure.Code != ErrorCapabilityUnavailable {
		t.Fatalf("disabled=%+v", disabled)
	}
}

func TestInvalidOwnerOutputsAndResourceCursorMismatch(t *testing.T) {
	s := snapshot("engine.list_modules")
	s.Resources = []ResourceCapability{{URI: "layerdraw://r", Name: "r", MimeType: "application/json", Schema: json.RawMessage(`{}`), Bound: true}}
	owner := &ownerStub{snapshot: s}
	owner.invoke = func(context.Context, OwnerRequest) (OwnerResponse, error) {
		return OwnerResponse{Content: json.RawMessage(`{`)}, nil
	}
	host, _ := newRunning(t, owner, DefaultLimits())
	bad := host.CallTool(context.Background(), CallToolRequest{Name: "layerdraw.list_modules", RequestID: "r", Arguments: json.RawMessage(`{}`), Binding: binding()})
	if bad.Failure == nil || bad.Failure.Code != ErrorResourceExhausted {
		t.Fatalf("bad=%+v", bad)
	}
	owner.read = func(context.Context, ResourceRequest) (ResourceResponse, error) {
		return ResourceResponse{Content: json.RawMessage(`{}`), NextCursor: json.RawMessage(`"next"`), MimeType: "application/json"}, nil
	}
	first := host.ReadResource(context.Background(), ReadResourceRequest{URI: "layerdraw://r", Binding: binding()})
	if first.Cursor == "" {
		t.Fatalf("first=%+v", first)
	}
	changed := binding()
	changed.AccessFingerprint = "changed"
	mismatch := host.ReadResource(context.Background(), ReadResourceRequest{URI: "layerdraw://r", Binding: changed, Cursor: first.Cursor})
	if mismatch.Failure == nil || mismatch.Failure.Code != ErrorInvalidCursor {
		t.Fatalf("mismatch=%+v", mismatch)
	}
	owner.read = func(context.Context, ResourceRequest) (ResourceResponse, error) {
		return ResourceResponse{Content: json.RawMessage(`{`), MimeType: "application/json"}, nil
	}
	invalid := host.ReadResource(context.Background(), ReadResourceRequest{URI: "layerdraw://r", Binding: binding()})
	if invalid.Failure == nil || invalid.Failure.Code != ErrorResourceExhausted {
		t.Fatalf("invalid=%+v", invalid)
	}
}

func TestCursorExpiryCapacityAndResourceFailures(t *testing.T) {
	now := time.Now()
	limits := DefaultLimits()
	limits.MaxCursors = 1
	limits.CursorTTL = time.Second
	s := snapshot("engine.list_modules")
	s.Resources = []ResourceCapability{{URI: "layerdraw://r", Name: "r", MimeType: "application/json", Schema: json.RawMessage(`{}`), Bound: true}}
	owner := &ownerStub{snapshot: s}
	owner.invoke = func(context.Context, OwnerRequest) (OwnerResponse, error) {
		return OwnerResponse{Content: json.RawMessage(`{}`), NextCursor: json.RawMessage(`"next"`)}, nil
	}
	host, err := New(Config{Owner: owner, Transport: &transportStub{}, Limits: limits, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if err = host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer host.Shutdown(context.Background())
	req := CallToolRequest{Name: "layerdraw.list_modules", RequestID: "r", Arguments: json.RawMessage(`{}`), Binding: binding()}
	first := host.CallTool(context.Background(), req)
	if first.Cursor == "" {
		t.Fatalf("first=%+v", first)
	}
	capacity := host.CallTool(context.Background(), req)
	if capacity.Failure == nil || capacity.Failure.Code != ErrorResourceExhausted {
		t.Fatalf("capacity=%+v", capacity)
	}
	now = now.Add(2 * time.Second)
	req.Cursor = first.Cursor
	expired := host.CallTool(context.Background(), req)
	if expired.Failure == nil || expired.Failure.Code != ErrorInvalidCursor {
		t.Fatalf("expired=%+v", expired)
	}
	unknown := host.ReadResource(context.Background(), ReadResourceRequest{URI: "nope"})
	if unknown.Failure == nil || unknown.Failure.Code != ErrorCapabilityUnavailable {
		t.Fatalf("unknown=%+v", unknown)
	}
	unbound := host.ReadResource(context.Background(), ReadResourceRequest{URI: "layerdraw://r"})
	if unbound.Failure == nil || unbound.Failure.Code != ErrorStaleBinding {
		t.Fatalf("unbound=%+v", unbound)
	}
	owner.read = func(context.Context, ResourceRequest) (ResourceResponse, error) {
		return ResourceResponse{}, &OwnerError{Code: ErrorStaleBinding}
	}
	stale := host.ReadResource(context.Background(), ReadResourceRequest{URI: "layerdraw://r", Binding: binding()})
	if stale.Failure == nil || stale.Failure.Code != ErrorStaleBinding {
		t.Fatalf("stale=%+v", stale)
	}
	owner.read = func(context.Context, ResourceRequest) (ResourceResponse, error) { panic("private path") }
	panicked := host.ReadResource(context.Background(), ReadResourceRequest{URI: "layerdraw://r", Binding: binding()})
	if panicked.Failure == nil || panicked.Failure.Code != ErrorOwnerFailure {
		t.Fatalf("panicked=%+v", panicked)
	}
}

func TestGetCapabilitiesArgumentsAndUnknownTools(t *testing.T) {
	host, _ := newRunning(t, &ownerStub{snapshot: snapshot()}, DefaultLimits())
	bad := host.CallTool(context.Background(), CallToolRequest{Name: "layerdraw.get_capabilities", Arguments: json.RawMessage(`{"secret":true}`)})
	if bad.Failure == nil || bad.Failure.Code != ErrorInvalidRequest {
		t.Fatalf("bad=%+v", bad)
	}
	unknown := host.CallTool(context.Background(), CallToolRequest{Name: "layerdraw.unknown", RequestID: "x", Arguments: json.RawMessage(`{}`)})
	if unknown.Failure == nil || unknown.Failure.Code != ErrorCapabilityUnavailable {
		t.Fatalf("unknown=%+v", unknown)
	}
}

var _ = errors.New
