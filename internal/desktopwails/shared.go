// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/user"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	"github.com/dencyuinc/layerdraw/internal/adapter/registryengine"
	"github.com/dencyuinc/layerdraw/internal/adapter/registrysource"
	reviewapp "github.com/dencyuinc/layerdraw/internal/application/review"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/host"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
	"github.com/dencyuinc/layerdraw/internal/mcphost"
	"github.com/dencyuinc/layerdraw/internal/registry"
	runtimeport "github.com/dencyuinc/layerdraw/internal/runtime/port"
)

const (
	desktopRelease  = "0.0.0-dev"
	desktopDigest   = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	desktopEndpoint = "layerdraw-desktop"
)

var basePackagedCapabilities = []protocolcommon.CapabilityID{
	desktopcontract.CapabilityAuthoring,
	desktopcontract.CapabilityExport,
	desktopcontract.CapabilityMCPTools,
	desktopcontract.CapabilityMCPResources,
	desktopcontract.CapabilityAgentScope,
	desktopcontract.CapabilityExternalStorage,
	desktopcontract.CapabilityRegistry,
	desktopcontract.CapabilityReview,
}

var basePackagedRequiredCapabilities = []protocolcommon.CapabilityID{
	desktopcontract.CapabilityAuthoring,
	desktopcontract.CapabilityRegistry,
	desktopcontract.CapabilityReview,
	desktopcontract.CapabilityExport,
	desktopcontract.CapabilityMCPTools,
	desktopcontract.CapabilityMCPResources,
	desktopcontract.CapabilityAgentScope,
}

func packagedCapabilities() []protocolcommon.CapabilityID {
	result := append([]protocolcommon.CapabilityID(nil), basePackagedCapabilities...)
	if packagedNativeSearchEnabled() {
		result = append(result, desktopcontract.CapabilityQuery, desktopcontract.CapabilitySearch, desktopcontract.CapabilityAnalysis)
	}
	return result
}

func packagedRequiredCapabilities() []protocolcommon.CapabilityID {
	result := append([]protocolcommon.CapabilityID(nil), basePackagedRequiredCapabilities...)
	if packagedNativeSearchEnabled() {
		result = append(result, desktopcontract.CapabilityQuery, desktopcontract.CapabilitySearch, desktopcontract.CapabilityAnalysis)
	}
	return result
}

// NewSharedConfig wires the owners that are actually packaged in Desktop.
// Build-tagged native owners fail closed at startup when their verified bundle
// is absent; unavailable owners remain disabled and undiscoverable.
func NewSharedConfig(root string) (desktopapp.Config, error) {
	objects, err := registry.NewDiskStagedObjectStore(filepath.Join(root, "registry", "objects"), registry.DefaultMaxStagedObjectBytes)
	if err != nil {
		return desktopapp.Config{}, err
	}
	transactions, err := registry.NewDiskTransactionStore(filepath.Join(root, "registry", "transactions"))
	if err != nil {
		return desktopapp.Config{}, err
	}
	sources, err := registry.NewDiskSourceStateStore(filepath.Join(root, "registry", "sources.json"))
	if err != nil {
		return desktopapp.Config{}, err
	}
	credentials := newPlatformCredentialPort()
	external, err := desktopapp.NewReferenceExternalStorage(desktopapp.ReferenceExternalStorageConfig{Root: root, Credentials: credentials})
	if err != nil {
		return desktopapp.Config{}, err
	}
	owner := &sharedOwner{root: root, objects: objects, transactions: transactions, sources: sources, credentials: credentials}
	clients, err := packagedClients(owner)
	if err != nil {
		return desktopapp.Config{}, err
	}
	adapters := map[desktopcontract.ComponentID]desktopapp.Adapter{}
	disabled := []desktopcontract.ComponentID{
		desktopcontract.ComponentNativeExporters,
	}
	if !packagedNativeSearchEnabled() {
		disabled = append(disabled, desktopcontract.ComponentNativeQuery, desktopcontract.ComponentSearchIndex, desktopcontract.ComponentEmbeddingProvider)
	}
	for _, id := range disabled {
		adapters[id] = disabledComponent{}
	}
	if packagedNativeSearchEnabled() {
		for _, id := range []desktopcontract.ComponentID{desktopcontract.ComponentNativeQuery, desktopcontract.ComponentSearchIndex, desktopcontract.ComponentEmbeddingProvider} {
			adapters[id] = owner
		}
	}
	adapters[desktopcontract.ComponentRegistryClient] = enabledComponent{}
	adapters[desktopcontract.ComponentReview] = enabledComponent{}
	adapters[desktopcontract.ComponentExternalStorage] = external
	adapters[desktopcontract.ComponentBindingShell] = owner
	return desktopapp.Config{
		Root: root, ReleaseVersion: desktopRelease, EndpointInstanceID: desktopEndpoint,
		ReleaseManifestDigest: desktopDigest, Adapters: adapters, Bindings: clients,
		Capabilities:                  nativeCapabilities{},
		ExternalPublication:           owner,
		ExternalLifecycle:             external,
		EffectiveRequiredCapabilities: packagedRequiredCapabilities(),
		DisabledComponents:            append([]desktopcontract.ComponentID(nil), disabled...),
		HostPorts: desktopcontract.HostPorts{
			Credentials: credentials, LocalActor: platformActor{},
			LocalOwner: unavailableOwner{}, Delegations: unavailableDelegations{},
		},
		MCPCapabilities:       owner,
		NativeSearchLifecycle: packagedNativeSearchLifecycle(owner),
		MCPApplicationOwner:   reviewMCPOwner{shared: owner},
		RegistryStagedObjects: registryObjectReader{store: objects},
		ReviewOwner:           owner,
	}, nil
}

type enabledComponent struct{}

func (enabledComponent) Start(context.Context) error    { return nil }
func (enabledComponent) Shutdown(context.Context) error { return nil }

type disabledComponent struct{}

func (disabledComponent) Start(context.Context) error    { return nil }
func (disabledComponent) Shutdown(context.Context) error { return nil }

// sharedOwner owns the in-process endpoint used by generated Wails Engine and
// Runtime bindings. It is started with the binding shell and closed before the
// application-local project host shuts down.
type sharedOwner struct {
	mu           sync.RWMutex
	root         string
	local        *localdocument.Host
	endpoint     *host.Endpoint
	engine       *engineendpoint.HostEngineFacade
	nativeSearch bool
	searchLife   host.SearchDocumentLifecycle
	closeSearch  func()
	objects      *registry.DiskStagedObjectStore
	transactions *registry.DiskTransactionStore
	sources      *registry.DiskSourceStateStore
	credentials  desktopcontract.CredentialPort
	registry     *registry.Registry
	registryWire *registry.HostBinding
	review       *reviewapp.Application
	application  *desktopapp.Application
}

type registryObjectReader struct{ store registry.StagedObjectStore }

// desktopReviewRuntime publishes an approved Review proposal through the
// Desktop application commit path so durable storage and project lifecycle
// state advance atomically.
type desktopReviewRuntime struct{ owner *sharedOwner }

func (r desktopReviewRuntime) Repreview(ctx context.Context, input reviewapp.RepreviewInput) (reviewapp.RepreviewResult, error) {
	r.owner.mu.RLock()
	local := r.owner.local
	r.owner.mu.RUnlock()
	if local == nil {
		return reviewapp.RepreviewResult{}, errors.New("desktop Review runtime is unavailable")
	}
	return local.Repreview(ctx, input)
}

func (r desktopReviewRuntime) Commit(ctx context.Context, input runtimeprotocol.RuntimeCommitInput) (runtimeprotocol.RuntimeCommitResult, error) {
	r.owner.mu.RLock()
	application := r.owner.application
	r.owner.mu.RUnlock()
	if application == nil {
		return runtimeprotocol.RuntimeCommitResult{}, errors.New("desktop Review application is unavailable")
	}
	result := application.Commit(ctx, input)
	if !result.Validate() || result.Outcome != protocolcommon.OutcomeSuccess {
		return runtimeprotocol.RuntimeCommitResult{}, errors.New("desktop Review commit failed closed")
	}
	return result.Value, nil
}

func (o *sharedOwner) bindApplication(application *desktopapp.Application) {
	o.mu.Lock()
	o.application = application
	o.mu.Unlock()
}

func (r registryObjectReader) OpenRegistryStagedObject(ctx context.Context, ref runtimeport.RegistryStagedObjectRef) (io.ReadCloser, error) {
	size, err := strconv.ParseInt(string(ref.Size), 10, 64)
	if err != nil || size < 0 {
		return nil, errors.New("Registry staged object size is invalid")
	}
	return r.store.OpenRegistryObject(ctx, registry.StagedObjectRef{ObjectID: ref.ObjectID, Digest: string(ref.Digest), Size: size, MediaType: ref.MediaType})
}

type registryCredentialResolver struct {
	port desktopcontract.CredentialPort
}

func (r registryCredentialResolver) ResolveCredential(ctx context.Context, ref string) ([]byte, error) {
	result := r.port.Resolve(ctx, desktopcontract.CredentialRef{ID: ref})
	if !result.Validate() || result.Outcome != protocolcommon.OutcomeSuccess || len(result.Value) == 0 {
		return nil, errors.New("Registry credential is unavailable")
	}
	return result.Value, nil
}

func (o *sharedOwner) RevalidateExternalPublication(ctx context.Context, intent desktopapp.ExternalPublicationIntent) desktopcontract.Result[struct{}] {
	o.mu.RLock()
	local := o.local
	o.mu.RUnlock()
	if local == nil || intent.Binding.BindingID == "" || intent.Binding.DocumentID != intent.Session.Scope.DocumentID || intent.Revision.DocumentID != intent.Session.Scope.DocumentID {
		return closedFailure[struct{}](desktopcontract.FailurePermissionDenied)
	}
	refs := []string{intent.Binding.BindingID, intent.Binding.RemoteItemID}
	if intent.Plan != nil {
		refs = append(refs, intent.Plan.PlanID)
	}
	if err := local.AuthorizeHostOperation(ctx, intent.Session, intent.Revision, accessprotocol.HostOperationKindBackendConfigure, "update", refs); err != nil {
		return closedFailure[struct{}](desktopcontract.FailurePermissionDenied)
	}
	return desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeSuccess, Value: struct{}{}}
}

func (o *sharedOwner) ReviewSnapshot() any {
	o.mu.RLock()
	owner := o.review
	o.mu.RUnlock()
	if owner == nil {
		return reviewapp.Snapshot{Version: 1, Proposals: []reviewapp.Proposal{}}
	}
	return owner.Snapshot()
}

func (o *sharedOwner) ReviewComment(ctx context.Context, input desktopapp.ReviewCommentRequest, actor accessprotocol.ActorRef) (any, error) {
	o.mu.RLock()
	owner := o.review
	o.mu.RUnlock()
	if owner == nil {
		return nil, errors.New("desktop Review owner is unavailable")
	}
	var target reviewapp.Target
	if err := json.Unmarshal(input.Target, &target); err != nil {
		return nil, reviewapp.ErrInvalid
	}
	return owner.Comment(ctx, reviewapp.CommentInput{ProposalID: input.ProposalID, Generation: input.Generation, CommentID: input.CommentID, Author: actor, Body: input.Body, Target: target})
}

func (o *sharedOwner) ReviewApproveAndApply(ctx context.Context, input desktopapp.ReviewApprovalRequest, session runtimeprotocol.RuntimeSessionRef, actor accessprotocol.ActorRef) (any, error) {
	o.mu.RLock()
	owner := o.review
	o.mu.RUnlock()
	if owner == nil {
		return nil, errors.New("desktop Review owner is unavailable")
	}
	operation := runtimeprotocol.OperationID(fmt.Sprintf("review_%s_%d", input.ProposalID, input.Generation))
	return owner.ApproveAndApply(ctx, reviewapp.ApprovalInput{ProposalID: input.ProposalID, Generation: input.Generation, Session: session, Approver: actor, OperationID: operation, IdempotencyKey: runtimeprotocol.IdempotencyKey(operation), Trigger: runtimeprotocol.CommitTriggerAgentApply})
}

func (o *sharedOwner) ReviewWithdraw(ctx context.Context, id string, generation uint64, actor accessprotocol.ActorRef) (any, error) {
	o.mu.RLock()
	owner := o.review
	o.mu.RUnlock()
	if owner == nil {
		return nil, errors.New("desktop Review owner is unavailable")
	}
	return owner.Withdraw(ctx, id, generation, actor)
}

func (o *sharedOwner) Snapshot(context.Context) (mcphost.CapabilitySnapshot, error) {
	operations := map[string]mcphost.OperationCapability{}
	generated := map[string]bool{}
	for _, binding := range desktopcontract.GeneratedBindingTable() {
		generated[binding.Operation] = true
	}
	schema := json.RawMessage(`{"type":"object","additionalProperties":true}`)
	for _, route := range mcphost.ToolRoutes() {
		for _, operation := range append([]string{route.Operation, route.PreviewOperation}, route.RequiredOperations...) {
			native := strings.HasPrefix(operation, "native.") && o.nativeSearch
			registryOperation := strings.HasPrefix(operation, "registry.")
			if operation != "" && generated[operation] && (strings.HasPrefix(operation, "engine.") || strings.HasPrefix(operation, "runtime.") || registryOperation || native) {
				operations[operation] = mcphost.OperationCapability{Enabled: true, InputSchema: append(json.RawMessage(nil), schema...), OutputSchema: append(json.RawMessage(nil), schema...)}
			}
		}
	}
	for _, operation := range []string{"review.list_proposals", "review.create_proposal", "review.comment", "review.approve_apply", "review.withdraw"} {
		operations[operation] = mcphost.OperationCapability{Enabled: true, InputSchema: append(json.RawMessage(nil), schema...), OutputSchema: append(json.RawMessage(nil), schema...)}
	}
	digest := protocolcommon.Digest(desktopDigest)
	return mcphost.CapabilitySnapshot{
		ManifestETag: protocolcommon.ManifestETag(digest), Operations: operations, Resources: []mcphost.ResourceCapability{},
		GrantSummary: accessprotocol.AuthoringGrantSummary{AccessFingerprint: digest, PolicyEtag: digest, GrantedCapabilities: accesscore.FullAuthoringCapabilities(), ConstrainedCapabilities: []semantic.AuthoringCapability{}},
	}, nil
}

func (o *sharedOwner) BindLocalHost(localHost *localdocument.Host) error {
	if localHost == nil {
		return errors.New("desktop local host is unavailable")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.endpoint != nil {
		return errors.New("desktop shared owner is already started")
	}
	o.local = localHost
	return nil
}

func (o *sharedOwner) Start(context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.endpoint != nil {
		return nil
	}
	if o.local == nil {
		return errors.New("desktop local host is not bound")
	}
	if o.root == "" {
		o.root = o.local.DataRoot()
	}
	validator, err := registryengine.New(o.objects, o.local)
	if err != nil {
		return err
	}
	registryOwner, err := registry.New(validator, registrysource.AccessPort{Evaluator: accesscore.Evaluator{}}, o.local, o.local, registrysource.CredentialBroker{Resolver: registryCredentialResolver{o.credentials}}, o.transactions)
	if err != nil {
		return err
	}
	localSource := registrysource.LocalDirectory{}
	remoteSource, err := registrysource.NewHTTPS(&http.Client{})
	if err != nil {
		return err
	}
	for _, kind := range []registry.SourceKind{registry.SourceLocalDirectory, registry.SourceGit} {
		registryOwner.RegisterClient(kind, localSource)
		registryOwner.RegisterConnector(kind, localSource)
	}
	for _, kind := range []registry.SourceKind{registry.SourceOfficial, registry.SourceOrganizationPrivate, registry.SourceSelfHosted} {
		registryOwner.RegisterClient(kind, remoteSource)
		registryOwner.RegisterConnector(kind, remoteSource)
	}
	if err := registryOwner.PutTrustPolicy(registry.TrustPolicy{PolicyID: "desktop-local", AllowUnsignedLocal: true, TrustedPublishers: map[string]bool{}, PublicKeys: map[string]ed25519.PublicKey{}, RevokedKeys: map[string]bool{}}); err != nil {
		return err
	}
	if o.sources != nil {
		if err := registryOwner.AttachSourceStateStore(context.Background(), o.sources); err != nil {
			return err
		}
	}
	registryWire, err := registry.NewHostBinding(registryOwner)
	if err != nil {
		return err
	}
	var reviewStore reviewapp.Store = reviewapp.NewMemoryStore()
	if o.root != "" {
		reviewStore, err = reviewapp.NewFileStore(filepath.Join(o.root, "review"))
		if err != nil {
			return err
		}
	}
	reviewOwner, err := reviewapp.New(context.Background(), reviewStore, desktopReviewRuntime{owner: o}, o.local, nil)
	if err != nil {
		return err
	}
	engine, err := engineendpoint.NewHostEngineFacade(desktopRelease, "unknown", desktopDigest, desktopEndpoint, engineendpoint.TransportInProcess)
	if err != nil {
		return err
	}
	search, lifecycle, closeSearch, err := openPackagedNativeSearch(filepath.Join(o.root, "native-search"), o.local, engine)
	if err != nil {
		return err
	}
	endpoint, err := host.New(host.Config{LocalHost: o.local, Engine: engine, Search: search, SearchLifecycle: lifecycle})
	if err != nil {
		closeSearch()
		return err
	}
	o.endpoint, o.engine, o.nativeSearch, o.searchLife, o.closeSearch = endpoint, engine, search != nil, lifecycle, closeSearch
	o.endpoint, o.engine, o.registry, o.registryWire, o.review = endpoint, engine, registryOwner, registryWire, reviewOwner
	return nil
}

func (o *sharedOwner) Shutdown(context.Context) error {
	o.mu.Lock()
	closeSearch := o.closeSearch
	o.endpoint, o.engine, o.registry, o.registryWire, o.review, o.local, o.application = nil, nil, nil, nil, nil, nil, nil
	o.nativeSearch, o.searchLife, o.closeSearch = false, nil, nil
	o.mu.Unlock()
	if closeSearch != nil {
		closeSearch()
	}
	return nil
}

func (o *sharedOwner) DispatchRegistry(ctx context.Context, wire []byte) []byte {
	o.mu.RLock()
	binding := o.registryWire
	o.mu.RUnlock()
	if binding == nil {
		return registryWireFailure(registry.WireOperation("registry.invalid"), "", registry.FailureUnavailable, "desktop_registry")
	}
	return binding.Dispatch(ctx, wire)
}

func (o *sharedOwner) PreviewEditor(ctx context.Context, input runtimeprotocol.PreviewOperationsInput) (localdocument.EditorPreviewResult, error) {
	o.mu.RLock()
	local := o.local
	o.mu.RUnlock()
	if local == nil {
		return localdocument.EditorPreviewResult{}, errors.New("desktop editor preview is unavailable")
	}
	return local.PreviewEditor(ctx, input)
}

func (o *sharedOwner) MaterializeProjectView(ctx context.Context, session runtimeprotocol.RuntimeSessionRef, address string) (semantic.ViewData, error) {
	o.mu.RLock()
	local := o.local
	o.mu.RUnlock()
	if local == nil {
		return semantic.ViewData{}, errors.New("desktop view materialization is unavailable")
	}
	return local.MaterializeProjectView(ctx, session, address)
}

func (o *sharedOwner) Invoke(ctx context.Context, exchange desktopcontract.Exchange) (desktopcontract.ExchangeResult, error) {
	o.mu.RLock()
	endpoint, engine, registryWire, application := o.endpoint, o.engine, o.registryWire, o.application
	o.mu.RUnlock()
	if endpoint == nil || engine == nil {
		return desktopcontract.ExchangeResult{}, errors.New("desktop shared owner is not started")
	}
	if result, err, handled := invokePackagedNativeSearch(ctx, endpoint, exchange); handled {
		return result, err
	}
	if strings.HasPrefix(exchange.Operation, "registry.") {
		if registryWire == nil || len(exchange.Blobs) != 0 {
			return desktopcontract.ExchangeResult{}, errors.New("desktop Registry owner is unavailable")
		}
		return desktopcontract.ExchangeResult{Operation: exchange.Operation, Control: registryWire.Dispatch(ctx, exchange.Control)}, nil
	}
	if exchange.Operation == string(engineprotocol.HandshakeRequestEnvelopeOperationValue) {
		request, err := engineprotocol.DecodeHandshakeRequestEnvelope(exchange.Control)
		if err != nil {
			return desktopcontract.ExchangeResult{}, err
		}
		response, _, err := engine.Descriptor().Negotiate(ctx, request)
		if err != nil {
			return desktopcontract.ExchangeResult{}, err
		}
		control, err := engineprotocol.EncodeHandshakeResponseEnvelope(response)
		return desktopcontract.ExchangeResult{Operation: exchange.Operation, Control: control}, err
	}
	if exchange.Operation == string(runtimeHandshakeOperation()) {
		response, _, err := endpoint.Handshake(ctx, exchange.Control)
		if err != nil {
			return desktopcontract.ExchangeResult{}, err
		}
		return desktopcontract.ExchangeResult{Operation: response.Operation, Control: response.Control}, nil
	}
	if exchange.Operation == string(runtimeprotocol.CommitOperationsRequestEnvelopeOperationValue) {
		return invokeDurableRuntimeCommit(ctx, application, exchange)
	}
	plan, terminal, err := endpoint.Prepare(ctx, exchange.Operation, exchange.Control)
	if err != nil {
		return desktopcontract.ExchangeResult{}, err
	}
	if terminal != nil {
		return desktopcontract.ExchangeResult{Operation: terminal.Operation, Control: terminal.Control}, nil
	}
	sink := &exchangeBlobSink{}
	response, err := plan.ExecuteDispatch(ctx, exchangeBlobSource(exchange.Blobs), sink)
	if err != nil {
		return desktopcontract.ExchangeResult{}, err
	}
	return desktopcontract.ExchangeResult{Operation: response.Operation, Control: response.Control, Blobs: sink.blobs}, nil
}

func invokeDurableRuntimeCommit(ctx context.Context, application *desktopapp.Application, exchange desktopcontract.Exchange) (desktopcontract.ExchangeResult, error) {
	if len(exchange.Blobs) != 0 {
		return desktopcontract.ExchangeResult{}, errors.New("runtime commit does not accept blobs")
	}
	if application == nil {
		return desktopcontract.ExchangeResult{}, errors.New("desktop application is unavailable")
	}
	request, err := runtimeprotocol.DecodeCommitOperationsRequestEnvelope(exchange.Control)
	if err != nil {
		return desktopcontract.ExchangeResult{}, err
	}
	committed := application.Commit(ctx, request.Payload)
	if !committed.Validate() || committed.Outcome != protocolcommon.OutcomeSuccess {
		return desktopcontract.ExchangeResult{}, errors.New("desktop runtime commit failed closed")
	}
	payload := committed.Value
	control, err := runtimeprotocol.EncodeCommitOperationsResponseEnvelope(runtimeprotocol.CommitOperationsResponseEnvelope{
		Diagnostics: []protocolcommon.ProtocolDiagnostic{},
		HostRelease: desktopRelease,
		Outcome:     protocolcommon.OutcomeSuccess,
		Payload:     &payload,
		Protocol:    request.Protocol,
		RequestID:   request.RequestID,
	})
	if err != nil {
		return desktopcontract.ExchangeResult{}, err
	}
	return desktopcontract.ExchangeResult{Operation: exchange.Operation, Control: control}, nil
}

func runtimeHandshakeOperation() protocolcommon.CapabilityID { return "runtime.handshake" }

type exchangeBlobSource []desktopcontract.Blob

func (source exchangeBlobSource) Definitions(context.Context) ([]engineendpoint.BlobDefinition, error) {
	result := make([]engineendpoint.BlobDefinition, len(source))
	for index, blob := range source {
		bytes := append([]byte(nil), blob.Bytes...)
		result[index] = engineendpoint.BlobDefinition{BlobID: blob.ID, Owned: &engineendpoint.OwnedBlob{Bytes: bytes, Release: func() {}}}
	}
	return result, nil
}

type exchangeBlobSink struct{ blobs []desktopcontract.Blob }

func (sink *exchangeBlobSink) Publish(_ context.Context, blobs []engineendpoint.OutputBlob) error {
	sink.blobs = make([]desktopcontract.Blob, len(blobs))
	for index, blob := range blobs {
		sink.blobs[index] = desktopcontract.Blob{ID: blob.Ref.BlobID, Bytes: append([]byte(nil), blob.Bytes...)}
	}
	return nil
}

type platformActor struct{}

func (platformActor) ResolveLocalActor(context.Context) desktopcontract.Result[accessprotocol.ActorRef] {
	current, err := user.Current()
	if err != nil || current.Uid == "" {
		return closedFailure[accessprotocol.ActorRef](desktopcontract.FailureLocalActor)
	}
	return desktopcontract.Result[accessprotocol.ActorRef]{Outcome: protocolcommon.OutcomeSuccess, Value: accessprotocol.ActorRef{ActorID: "local-user-" + current.Uid, Kind: "user"}}
}

type unavailableCredentials struct{}

func (unavailableCredentials) Resolve(context.Context, desktopcontract.CredentialRef) desktopcontract.Result[[]byte] {
	return closedFailure[[]byte](desktopcontract.FailureCredential)
}

type unavailableOwner struct{}

func (unavailableOwner) IssueLocalOwnerGrant(context.Context, desktopcontract.LocalOwnerGrantRequest) desktopcontract.Result[accessprotocol.AuthoringGrantSnapshot] {
	return closedFailure[accessprotocol.AuthoringGrantSnapshot](desktopcontract.FailureAgentDelegation)
}

type unavailableDelegations struct{}

func (unavailableDelegations) Delegate(context.Context, accessprotocol.AuthoringGrantSnapshot, accesscore.Delegation) desktopcontract.Result[accesscore.Delegation] {
	return closedFailure[accesscore.Delegation](desktopcontract.FailureAgentDelegation)
}
func (unavailableDelegations) Resolve(context.Context, desktopcontract.DelegationFence) desktopcontract.Result[accesscore.Delegation] {
	return closedFailure[accesscore.Delegation](desktopcontract.FailureAgentDelegation)
}
func (unavailableDelegations) Revoke(context.Context, desktopcontract.DelegationFence) desktopcontract.Result[accesscore.DelegationSnapshot] {
	return closedFailure[accesscore.DelegationSnapshot](desktopcontract.FailureAgentDelegation)
}

// disabledMCP is a lifecycle-compatible closed port. It opens no listener and
// its tool/resource capabilities remain disabled in nativeCapabilities.
type disabledMCP struct{}

func (disabledMCP) Start(context.Context) desktopcontract.Result[struct{}] {
	return desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeSuccess}
}
func (disabledMCP) Shutdown(context.Context) desktopcontract.Result[struct{}] {
	return desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeSuccess}
}

func closedFailure[T any](code desktopcontract.FailureCode) desktopcontract.Result[T] {
	return desktopcontract.Result[T]{Outcome: protocolcommon.OutcomeFailed, Failure: &desktopcontract.Failure{Code: code, Component: desktopcontract.ComponentAccess, Recovery: desktopcontract.RecoveryConfigureAdapter}}
}

type nativeCapabilities struct{ externalStorage bool }

func (capabilities nativeCapabilities) Negotiate(ctx context.Context, manifest desktopcontract.Manifest) (protocolcommon.HandshakeResult, error) {
	descriptor, err := engineendpoint.NewDescriptor(engineendpoint.DescriptorConfig{
		EngineRelease: desktopRelease, SourceRevision: "unknown",
		ReleaseManifestDigest: desktopDigest, EndpointInstanceID: desktopEndpoint,
		Transports: []string{engineendpoint.TransportInProcess}, Limits: engineendpoint.DefaultLimitPolicy(),
	})
	if err != nil {
		return protocolcommon.HandshakeResult{}, err
	}
	response, _, err := descriptor.Negotiate(ctx, engineprotocol.HandshakeRequestEnvelope{
		Operation: engineprotocol.HandshakeRequestEnvelopeOperationValue,
		Payload: protocolcommon.HandshakeRequest{ClientRelease: desktopRelease,
			Protocols:            []protocolcommon.ProtocolOffer{{Name: engineendpoint.ProtocolName, SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: engineendpoint.ProtocolVersion, SchemaDigest: protocolcommon.Digest(engineprotocol.SchemaDigest)}}}},
			RequiredCapabilities: []protocolcommon.CapabilityID{engineendpoint.OperationCompile}, OptionalCapabilities: []protocolcommon.CapabilityID{}},
		Protocol: engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue}, RequestID: "desktop-startup",
	})
	if err != nil || response.Payload == nil {
		return protocolcommon.HandshakeResult{}, errors.New("desktop capability negotiation failed")
	}
	value := *response.Payload
	capabilitiesList := packagedCapabilities()
	enabled := make(map[protocolcommon.CapabilityID]bool, len(capabilitiesList))
	for _, id := range capabilitiesList {
		enabled[id] = true
	}
	if capabilities.externalStorage {
		enabled[desktopcontract.CapabilityExternalStorage] = true
	}
	ids := append(append([]protocolcommon.CapabilityID(nil), manifest.RequiredCapabilities...), manifest.OptionalCapabilities...)
	value.CapabilityStatuses = make([]protocolcommon.RequestedCapabilityStatus, 0, len(ids))
	for _, id := range ids {
		status := protocolcommon.RequestedCapabilityStatus{CapabilityID: id, Enabled: enabled[id], ProtocolVersion: desktopcontract.DesktopProtocolVersion}
		if !status.Enabled {
			reason := protocolcommon.UnavailableReasonNotConfigured
			status.UnavailableReason = &reason
		}
		value.CapabilityStatuses = append(value.CapabilityStatuses, status)
	}
	return value, nil
}

type closedOwnerDecoder struct{}

func (closedOwnerDecoder) DecodeRequest(expected string, control []byte) (desktopcontract.OwnerEnvelopeIdentity, error) {
	var value struct {
		Operation string `json:"operation"`
		RequestID string `json:"request_id"`
	}
	if json.Unmarshal(control, &value) != nil || value.Operation != expected || value.RequestID == "" {
		return desktopcontract.OwnerEnvelopeIdentity{}, errors.New("invalid request")
	}
	return desktopcontract.OwnerEnvelopeIdentity{Operation: value.Operation, RequestID: value.RequestID}, nil
}
func (closedOwnerDecoder) DecodeResponse(string, []byte) (desktopcontract.OwnerResponseIdentity, error) {
	return desktopcontract.OwnerResponseIdentity{}, errors.New("owner unavailable")
}

func packagedClients(owner *sharedOwner) (desktopcontract.ClientSet, error) {
	clients := desktopcontract.ClientSet{}
	root := reflect.ValueOf(&clients).Elem()
	methodType := reflect.TypeOf(desktopcontract.ClientMethod(nil))
	available := reflect.ValueOf(desktopcontract.ClientMethod(owner.Invoke))
	unavailable := reflect.MakeFunc(methodType, func([]reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(desktopcontract.ExchangeResult{}), reflect.ValueOf(errors.New("desktop owner is not packaged"))}
	})
	decoder := reflect.ValueOf(closedOwnerDecoder{})
	for index := 0; index < root.NumField(); index++ {
		ownerField := root.Field(index)
		ownerName := root.Type().Field(index).Name
		actual := ownerName == "Engine" || ownerName == "Runtime" || ownerName == "Registry" || (ownerName == "NativeQuery" && packagedNativeSearchEnabled())
		for fieldIndex := 0; fieldIndex < ownerField.NumField(); fieldIndex++ {
			field := ownerField.Field(fieldIndex)
			if field.Kind() == reflect.Interface {
				if ownerName == "NativeQuery" && packagedNativeSearchEnabled() {
					nativeDecoder, ok := packagedNativeSearchDecoder()
					if !ok {
						return desktopcontract.ClientSet{}, errors.New("desktop native decoder is unavailable")
					}
					field.Set(reflect.ValueOf(nativeDecoder))
				} else {
					field.Set(decoder)
				}
			} else if actual {
				field.Set(available)
			} else {
				field.Set(unavailable)
			}
		}
	}
	return clients, clients.Validate()
}
