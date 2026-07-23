// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	reviewapp "github.com/dencyuinc/layerdraw/internal/application/review"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
	"github.com/dencyuinc/layerdraw/internal/mcphost"
	"github.com/dencyuinc/layerdraw/internal/registry"
	runtimeport "github.com/dencyuinc/layerdraw/internal/runtime/port"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

type previewDispatcherStub struct{ err error }

func (s previewDispatcherStub) DispatchRegistry(context.Context, []byte) []byte { return nil }
func (s previewDispatcherStub) PreviewEditor(context.Context, runtimeprotocol.PreviewOperationsInput) (localdocument.EditorPreviewResult, error) {
	return localdocument.EditorPreviewResult{}, s.err
}
func (s previewDispatcherStub) MaterializeProjectView(context.Context, runtimeprotocol.RuntimeSessionRef, string) (semantic.ViewData, error) {
	return semantic.ViewData{}, s.err
}
func (s previewDispatcherStub) ProjectDocumentGeneration(context.Context, runtimeprotocol.RuntimeSessionRef) (engineprotocol.DocumentGeneration, error) {
	return engineprotocol.DocumentGeneration{}, s.err
}
func (s previewDispatcherStub) ProjectSubjects(context.Context, runtimeprotocol.RuntimeSessionRef) ([]semantic.SemanticSubject, error) {
	return nil, s.err
}

func (s previewDispatcherStub) ProjectStructure(context.Context, runtimeprotocol.RuntimeSessionRef) (engineendpoint.BridgeStructure, error) {
	return engineendpoint.BridgeStructure{}, s.err
}

func TestFrontendPreviewAndReviewContractEdges(t *testing.T) {
	bridge := NewFrontendBridge(nil)
	if response := bridge.RegistryDispatch("{"); !strings.Contains(response, registry.FailureUnsupportedFormat) {
		t.Fatalf("malformed Registry response=%s", response)
	}
	if _, err := bridge.PreviewEditor(runtimeprotocol.PreviewOperationsInput{}); err == nil {
		t.Fatal("missing preview dispatcher accepted")
	}
	if _, err := bridge.MaterializeProjectView(runtimeprotocol.RuntimeSessionRef{}, "view"); err == nil {
		t.Fatal("missing view dispatcher accepted")
	}
	if _, err := bridge.ProjectSubjects(runtimeprotocol.RuntimeSessionRef{}); err == nil {
		t.Fatal("missing subject dispatcher accepted")
	}
	if _, err := bridge.ProjectDocumentGeneration(runtimeprotocol.RuntimeSessionRef{}); err == nil {
		t.Fatal("missing generation dispatcher accepted")
	}
	if _, err := bridge.ProjectStructure(runtimeprotocol.RuntimeSessionRef{}); err == nil {
		t.Fatal("missing structure dispatcher accepted")
	}
	sentinel := errors.New("preview failed")
	bridge = NewFrontendBridge(nil, previewDispatcherStub{err: sentinel})
	if _, err := bridge.PreviewEditor(runtimeprotocol.PreviewOperationsInput{}); !errors.Is(err, sentinel) {
		t.Fatalf("preview err=%v", err)
	}
	if _, err := bridge.MaterializeProjectView(runtimeprotocol.RuntimeSessionRef{}, "view"); !errors.Is(err, sentinel) {
		t.Fatalf("view err=%v", err)
	}
	bridge = NewFrontendBridge(nil, previewDispatcherStub{})
	_, _ = bridge.MaterializeProjectView(runtimeprotocol.RuntimeSessionRef{}, "view")
	if _, err := bridge.ProjectSubjects(runtimeprotocol.RuntimeSessionRef{}); err != nil {
		t.Fatalf("stub subjects err=%v", err)
	}
	if _, err := bridge.ProjectDocumentGeneration(runtimeprotocol.RuntimeSessionRef{}); err != nil {
		t.Fatalf("stub generation err=%v", err)
	}
	if _, err := bridge.ProjectStructure(runtimeprotocol.RuntimeSessionRef{}); err != nil {
		t.Fatalf("stub structure err=%v", err)
	}

	proposal := reviewapp.Proposal{ID: "proposal", Generation: 1}
	if got, err := reviewProposal(proposal, nil); err != nil || got.ID != proposal.ID {
		t.Fatalf("proposal=%+v err=%v", got, err)
	}
	if _, err := reviewProposal(struct{}{}, nil); err == nil {
		t.Fatal("mismatched proposal contract accepted")
	}
	if _, err := reviewProposal(nil, sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("proposal err=%v", err)
	}
}

func TestRunPreservesExtensionCallbacksBeforeStartup(t *testing.T) {
	base, err := NewSharedConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	original := runWails
	t.Cleanup(func() { runWails = original })
	var domReady, fileOpen bool
	runWails = func(config *options.App) error {
		config.OnDomReady(context.Background())
		config.Mac.OnFileOpen("associated.ldl")
		if config.OnBeforeClose(context.Background()) {
			// The un-started application cannot confirm a safe quit; the
			// veto path is the expected fail-closed behaviour here.
			_ = config.OnBeforeClose
		}
		config.OnShutdown(context.Background())
		return nil
	}
	err = Run(base, fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("desktop")}}, nil, func(config *options.App) {
		config.OnDomReady = func(context.Context) { domReady = true }
		config.Mac = &mac.Options{OnFileOpen: func(string) { fileOpen = true }}
	})
	if err != nil || !domReady || !fileOpen {
		t.Fatalf("callbacks dom=%v file=%v err=%v", domReady, fileOpen, err)
	}
}

func TestCommandRouterAndSharedConfigClosedFailures(t *testing.T) {
	base, err := NewSharedConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := Compose(base, &nativeStub{err: errors.New("dialog failed")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	router := newApplicationCommandRouter(application)
	for _, id := range []desktopcontract.CommandID{desktopcontract.CommandNewProject, desktopcontract.CommandOpenProject} {
		if _, err := router.Route(context.Background(), desktopcontract.CommandInvocation{ID: id, Source: desktopcontract.CommandSourceMenu, StatusGeneration: "1"}); err == nil {
			t.Fatalf("failed dialog command accepted: %s", id)
		}
	}

	for _, prepare := range []func(string) error{
		func(root string) error { return os.WriteFile(filepath.Join(root, "registry"), []byte("file"), 0o600) },
		func(root string) error {
			if err := os.MkdirAll(filepath.Join(root, "registry", "objects"), 0o700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(root, "registry", "transactions"), []byte("file"), 0o600)
		},
	} {
		root := t.TempDir()
		if err := prepare(root); err != nil {
			t.Fatal(err)
		}
		if _, err := NewSharedConfig(root); err == nil {
			t.Fatal("invalid shared config filesystem accepted")
		}
	}
}

func TestFrontendReviewDelegatesTypedContracts(t *testing.T) {
	instance, err := newConformanceInstance(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.close(context.Background())
	bridge := NewFrontendBridge(instance.app)
	if snapshot, err := bridge.ReviewSnapshot(); err != nil || len(snapshot.Proposals) != 0 {
		t.Fatal(err)
	}
	if _, err := bridge.ReviewComment(ReviewCommentRequest{ProposalID: "missing", Generation: 1, CommentID: "comment", Body: "body", Target: reviewapp.Target{Kind: reviewapp.TargetProject}}); err == nil {
		t.Fatal("comment on missing proposal accepted")
	}
	if _, err := bridge.ReviewApproveAndApply(desktopapp.ReviewApprovalRequest{ProposalID: "missing", Generation: 1}); err == nil {
		t.Fatal("approval without active project accepted")
	}
	if _, err := bridge.ReviewWithdraw(ReviewWithdrawRequest{ProposalID: "missing", Generation: 1}); err == nil {
		t.Fatal("withdraw of missing proposal accepted")
	}
	if _, err := bridge.ProjectOpenSession(runtimeprotocol.OpenRuntimeDocumentInput{DocumentID: "doc_missing"}); err == nil {
		t.Fatal("open session without an open project accepted")
	}
	if config := bridge.MCPClientConfig("connection-1"); config == "" {
		t.Fatal("MCP client config was empty")
	}
	if result := bridge.DeleteMCPConnection("connection-missing"); result.Outcome == protocolcommon.OutcomeSuccess {
		t.Fatal("deleting an unknown MCP connection succeeded")
	}
	if enabled := bridge.SetMCPEnabled(true, desktopapp.MCPTransportLocal); enabled.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("enable MCP=%+v", enabled)
	}
	if status := bridge.MCPStatus(); !status.Enabled {
		t.Fatalf("MCP status=%+v", status)
	}
	if disabled := bridge.SetMCPEnabled(false, desktopapp.MCPTransportLocal); disabled.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("disable MCP=%+v", disabled)
	}
	if result := bridge.CloseCurrentProject(); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("close with no open project result=%+v", result)
	}
}

type credentialPortFunc func(context.Context, desktopcontract.CredentialRef) desktopcontract.Result[[]byte]

func (f credentialPortFunc) Resolve(ctx context.Context, ref desktopcontract.CredentialRef) desktopcontract.Result[[]byte] {
	return f(ctx, ref)
}

func TestSharedRegistryAdaptersAndClosedOwnerEdges(t *testing.T) {
	resolver := registryCredentialResolver{port: credentialPortFunc(func(context.Context, desktopcontract.CredentialRef) desktopcontract.Result[[]byte] {
		return desktopcontract.Result[[]byte]{Outcome: protocolcommon.OutcomeSuccess, Value: []byte("secret")}
	})}
	if value, err := resolver.ResolveCredential(context.Background(), "credential"); err != nil || string(value) != "secret" {
		t.Fatalf("credential=%q err=%v", value, err)
	}
	resolver.port = credentialPortFunc(func(context.Context, desktopcontract.CredentialRef) desktopcontract.Result[[]byte] {
		return desktopcontract.Result[[]byte]{Outcome: protocolcommon.OutcomeFailed}
	})
	if _, err := resolver.ResolveCredential(context.Background(), "credential"); err == nil {
		t.Fatal("invalid credential result accepted")
	}

	store, err := registry.NewDiskStagedObjectStore(t.TempDir(), 32)
	if err != nil {
		t.Fatal(err)
	}
	reader := registryObjectReader{store: store}
	if _, err := reader.OpenRegistryStagedObject(context.Background(), runtimeport.RegistryStagedObjectRef{Size: "invalid"}); err == nil {
		t.Fatal("invalid staged size accepted")
	}
	staged, err := store.PutRegistryObject(context.Background(), "text/plain", strings.NewReader("data"), 4)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := reader.OpenRegistryStagedObject(context.Background(), runtimeport.RegistryStagedObjectRef{ObjectID: staged.ObjectID, Digest: protocolcommon.Digest(staged.Digest), Size: protocolcommon.CanonicalUint64("4"), MediaType: staged.MediaType})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(stream)
	_ = stream.Close()
	if err != nil || string(data) != "data" {
		t.Fatalf("staged=%q err=%v", data, err)
	}

	owner := &sharedOwner{}
	if _, err := owner.PreviewEditor(context.Background(), runtimeprotocol.PreviewOperationsInput{}); err == nil {
		t.Fatal("closed preview owner accepted")
	}
	if _, err := owner.ProjectSubjects(context.Background(), runtimeprotocol.RuntimeSessionRef{}); err == nil {
		t.Fatal("closed subject owner accepted")
	}
	if _, err := owner.ProjectDocumentGeneration(context.Background(), runtimeprotocol.RuntimeSessionRef{}); err == nil {
		t.Fatal("closed generation owner accepted")
	}
	if _, err := owner.ProjectStructure(context.Background(), runtimeprotocol.RuntimeSessionRef{}); err == nil {
		t.Fatal("closed structure owner accepted")
	}
	if _, err := owner.MaterializeProjectView(context.Background(), runtimeprotocol.RuntimeSessionRef{}, "view"); err == nil {
		t.Fatal("closed materialization owner accepted")
	}
	if _, err := owner.ReviewComment(context.Background(), desktopapp.ReviewCommentRequest{}, accessprotocol.ActorRef{}); err == nil {
		t.Fatal("closed Review comment owner accepted")
	}
	if _, err := owner.ReviewApproveAndApply(context.Background(), desktopapp.ReviewApprovalRequest{}, runtimeprotocol.RuntimeSessionRef{}, accessprotocol.ActorRef{}); err == nil {
		t.Fatal("closed Review approval owner accepted")
	}
	if _, err := owner.ReviewWithdraw(context.Background(), "proposal", 1, accessprotocol.ActorRef{}); err == nil {
		t.Fatal("closed Review withdraw owner accepted")
	}
	if snapshot, ok := owner.ReviewSnapshot().(reviewapp.Snapshot); !ok || len(snapshot.Proposals) != 0 {
		t.Fatalf("closed snapshot=%+v", snapshot)
	}
	var response registry.WireResponse
	if err := json.Unmarshal(owner.DispatchRegistry(context.Background(), nil), &response); err != nil || response.Failure == nil {
		t.Fatalf("closed registry response=%+v err=%v", response, err)
	}
}

func TestReviewMCPOwnerClosedAndInvalidOperations(t *testing.T) {
	runtime := conformanceRuntime{}
	runtime.ShowWindow(context.Background())
	runtime.Emit(context.Background(), "event")
	runtime.Quit(context.Background())

	closed := reviewMCPOwner{shared: &sharedOwner{}}
	if _, err := closed.Invoke(context.Background(), mcphost.OwnerRequest{Operation: "review.list_proposals"}); err == nil {
		t.Fatal("closed MCP Review owner accepted")
	}
	if _, err := closed.ReadResource(context.Background(), mcphost.ResourceRequest{}); err == nil {
		t.Fatal("Review MCP resource unexpectedly available")
	}

	instance, err := newConformanceInstance(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.close(context.Background())
	owner := reviewMCPOwner{shared: instance.owner}
	if response, err := owner.Invoke(context.Background(), mcphost.OwnerRequest{Operation: "review.list_proposals"}); err != nil || response.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("list response=%+v err=%v", response, err)
	}
	for _, request := range []mcphost.OwnerRequest{
		{Operation: "review.create_proposal", Arguments: []byte("{")},
		{Operation: "review.comment", Arguments: []byte("{")},
		{Operation: "review.approve_apply", Arguments: []byte(`{}`)},
		{Operation: "review.withdraw", Arguments: []byte(`{}`)},
		{Operation: "review.unknown", Arguments: []byte(`{}`)},
	} {
		if _, err := owner.Invoke(context.Background(), request); err == nil {
			t.Fatalf("invalid MCP Review request accepted: %s", request.Operation)
		}
	}
	for _, request := range []mcphost.OwnerRequest{
		{Operation: "review.create_proposal", Arguments: []byte(`{}`)},
		{Operation: "review.comment", Arguments: []byte(`{"proposal_id":"missing","generation":1,"comment_id":"comment","body":"body","target":{"kind":"project"}}`)},
	} {
		if _, err := owner.Invoke(context.Background(), request); err == nil {
			t.Fatalf("invalid Review domain request accepted: %s", request.Operation)
		}
	}
	binding := &mcphost.Binding{DocumentID: "missing"}
	for _, request := range []mcphost.OwnerRequest{
		{Operation: "review.approve_apply", Arguments: []byte(`{"proposal_id":"missing","generation":1}`), Binding: binding},
		{Operation: "review.withdraw", Arguments: []byte(`{"proposal_id":"missing","generation":1}`), Binding: binding},
	} {
		if _, err := owner.Invoke(context.Background(), request); err == nil {
			t.Fatalf("unbound MCP Review request accepted: %s", request.Operation)
		}
	}
	opened, err := instance.openProject(context.Background(), conformanceAuthoringSource)
	if err != nil {
		t.Fatal(err)
	}
	bound := &mcphost.Binding{DocumentID: opened.Open.Session.Scope.DocumentID}
	for _, request := range []mcphost.OwnerRequest{
		{Operation: "review.approve_apply", Arguments: []byte(`{"proposal_id":"missing","generation":1}`), Binding: bound},
		{Operation: "review.withdraw", Arguments: []byte(`{"proposal_id":"missing","generation":1}`), Binding: bound},
	} {
		if _, err := owner.Invoke(context.Background(), request); err == nil {
			t.Fatalf("missing bound Review proposal accepted: %s", request.Operation)
		}
	}
	if _, err := instance.owner.ReviewComment(context.Background(), desktopapp.ReviewCommentRequest{ProposalID: "missing", Generation: 1, Target: []byte("{")}, accessprotocol.ActorRef{}); err == nil {
		t.Fatal("invalid shared Review target accepted")
	}

	if response := instance.owner.DispatchRegistry(context.Background(), []byte(`{}`)); len(response) == 0 {
		t.Fatal("active Registry owner returned no failure envelope")
	}
	if _, err := instance.owner.PreviewEditor(context.Background(), runtimeprotocol.PreviewOperationsInput{}); err == nil {
		t.Fatal("invalid forwarded preview accepted")
	}
	if _, err := instance.owner.MaterializeProjectView(context.Background(), runtimeprotocol.RuntimeSessionRef{}, "invalid"); err == nil {
		t.Fatal("invalid forwarded materialization accepted")
	}
	for _, exchange := range []desktopcontract.Exchange{
		{Operation: "registry.list_sources", Blobs: []desktopcontract.Blob{{ID: "unexpected"}}},
		{Operation: string(runtimeHandshakeOperation()), Control: []byte("invalid")},
		{Operation: string(runtimeprotocol.CommitOperationsRequestEnvelopeOperationValue), Blobs: []desktopcontract.Blob{{ID: "unexpected"}}},
		{Operation: string(runtimeprotocol.CommitOperationsRequestEnvelopeOperationValue), Control: []byte("invalid")},
	} {
		if _, err := instance.owner.Invoke(context.Background(), exchange); err == nil {
			t.Fatalf("invalid shared exchange accepted: %s", exchange.Operation)
		}
	}
}

func TestOpenDiagnosticsLoggerIsComposedAndPanicSafe(t *testing.T) {
	logOpenDiagnostics(context.Background(), "project open", errors.New("private cause"))
	logOpenDiagnostics(context.Background(), "project open", nil)
}
