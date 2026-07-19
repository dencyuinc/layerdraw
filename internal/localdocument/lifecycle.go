// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package localdocument composes the Engine, Runtime coordinator, and local
// persistence adapters into one framework-neutral single-user lifecycle. It
// contains no picker, editor, desktop-shell, or remote-provider behavior.
package localdocument

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	"github.com/dencyuinc/layerdraw/internal/adapter/local"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	runtimehost "github.com/dencyuinc/layerdraw/internal/runtime"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

const (
	metadataVersion    = 1
	defaultMaxSessions = 32
	defaultMaxRecovery = 256
	metadataFileMode   = 0o600
)

type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// Scheduler is host-injected. Implementations must run fn at most once unless
// cancelled; lifecycle correctness does not depend on when it runs.
type Scheduler interface {
	Schedule(time.Duration, func()) func()
}

type timerScheduler struct{}

func (timerScheduler) Schedule(delay time.Duration, fn func()) func() {
	timer := time.AfterFunc(delay, fn)
	return func() { timer.Stop() }
}

type Config struct {
	Root                  string
	ReleaseVersion        protocolcommon.ReleaseVersion
	EndpointInstanceID    protocolcommon.EndpointInstanceID
	ReleaseManifestDigest protocolcommon.Digest
	Clock                 Clock
	Scheduler             Scheduler
	Random                io.Reader
	AutosaveDelay         time.Duration
	MaxSessions           int
	MaxRecoveryItems      int
	MaxProjectFiles       int
	MaxProjectBytes       int64
	AdapterOptions        local.Options
	// LocalActor resolves the stable OS/host identity used for local-owner
	// grants. It must not assert organization membership. The default preserves
	// the host-local owner identity for headless and embedded callers.
	LocalActor accesscore.LocalActorResolver
}

type Host struct {
	config    Config
	engine    *engineendpoint.LocalDocumentEngine
	runtime   *runtimehost.Runtime
	documents *local.Document
	state     *local.State
	assets    *local.Assets
	history   *local.History
	recovery  *local.Recovery
	external  *local.ExternalFileStore
	authority *localAuthority
	workbench *runtimeWorkbench
	mu        sync.Mutex
	// delegationMu serializes durable delegation snapshots independently from
	// session lifecycle locking.
	delegationMu sync.Mutex
	metadata     lifecycleMetadata
	sessions     map[runtimeprotocol.RuntimeSessionID]*Session
	autosaves    map[runtimeprotocol.RuntimeSessionID]func()
	closed       bool
}

type lifecycleMetadata struct {
	Version  int                        `json:"version"`
	Bindings map[string]documentBinding `json:"bindings"`
}

type documentBinding struct {
	DocumentID   runtimeprotocol.DocumentID `json:"document_id"`
	Kind         string                     `json:"kind"`
	Locator      string                     `json:"locator"`
	PortableID   string                     `json:"portable_project_id"`
	SourceDigest protocolcommon.Digest      `json:"source_digest"`
}

type Session struct {
	Open          runtimeprotocol.OpenRuntimeDocumentResult
	PortableID    string
	SourceKind    string
	SourceLocator string
	SourceDigest  protocolcommon.Digest
	working       port.WorkingDocument
	sourceInput   engineendpoint.LocalSource
	closed        bool
	delegationID  string
}

func (h *Host) accessContext(ctx context.Context, session *Session) context.Context {
	if session != nil && session.delegationID != "" {
		return withDelegation(ctx, session.delegationID)
	}
	return ctx
}

type ExternalChange struct {
	DocumentID      runtimeprotocol.DocumentID
	CommittedDigest protocolcommon.Digest
	ExternalDigest  protocolcommon.Digest
	Kind            string
	RequiresReview  bool
}

type OpenResult struct {
	Session        *Session
	History        runtimeprotocol.RevisionPage
	ExternalChange *ExternalChange
}

type OpenProjectInput struct {
	Root                 string
	EntryPath            string
	InstalledPackTree    map[string][]byte
	ResolvedDependencies engineendpoint.LocalResolvedDependencies
	ReferencedAssets     []engineendpoint.LocalAssetInput
	ResourceLimits       engineendpoint.LocalResourceLimits
}

type SaveInput struct {
	Session        *Session
	Operations     engineprotocol.SemanticOperationBatch
	Preconditions  engineprotocol.EngineEditPreconditions
	OperationID    runtimeprotocol.OperationID
	IdempotencyKey runtimeprotocol.IdempotencyKey
	Trigger        runtimeprotocol.CommitTrigger
	Cancellation   *runtimeprotocol.CancellationToken
}

type AutosaveResult struct {
	Result runtimeprotocol.RuntimeCommitResult
	Err    error
}

func New(config Config) (*Host, error) {
	if config.Root == "" || !filepath.IsAbs(config.Root) {
		return nil, errors.New("local document root must be absolute")
	}
	if config.Clock == nil {
		config.Clock = systemClock{}
	}
	if config.ReleaseVersion == "" {
		config.ReleaseVersion = "0.0.0-dev"
	}
	if config.EndpointInstanceID == "" {
		config.EndpointInstanceID = "local-document-runtime"
	}
	if config.ReleaseManifestDigest == "" {
		config.ReleaseManifestDigest = digestJSON(struct {
			Component string `json:"component"`
		}{"local-document"})
	}
	if config.Scheduler == nil {
		config.Scheduler = timerScheduler{}
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.AutosaveDelay <= 0 {
		config.AutosaveDelay = 750 * time.Millisecond
	}
	if config.MaxSessions <= 0 {
		config.MaxSessions = defaultMaxSessions
	}
	if config.MaxRecoveryItems <= 0 {
		config.MaxRecoveryItems = defaultMaxRecovery
	}
	if config.MaxProjectFiles <= 0 {
		config.MaxProjectFiles = 4096
	}
	if config.MaxProjectBytes <= 0 {
		config.MaxProjectBytes = 64 << 20
	}
	config.AdapterOptions.Now = config.Clock.Now
	config.AdapterOptions.Random = config.Random
	documents, err := local.NewDocumentStore(config.Root, config.AdapterOptions)
	if err != nil {
		return nil, err
	}
	state, err := local.NewStateBackend(config.Root, config.AdapterOptions)
	if err != nil {
		return nil, err
	}
	assets, err := local.NewAssetStore(config.Root, config.AdapterOptions)
	if err != nil {
		return nil, err
	}
	history, err := local.NewHistoryStore(config.Root, config.AdapterOptions)
	if err != nil {
		return nil, err
	}
	recovery, err := local.NewRecoveryJournal(config.Root, config.AdapterOptions)
	if err != nil {
		return nil, err
	}
	external, err := local.NewExternalFileStore(config.Root, local.ExternalFileOptions{MaxFiles: config.MaxProjectFiles, MaxBytes: config.MaxProjectBytes})
	if err != nil {
		return nil, err
	}
	instance := engineendpoint.NewLocalDocumentEngine()
	endpointID := config.EndpointInstanceID
	workbench := &runtimeWorkbench{bridge: instance.NewRuntimeEngineBridge(endpointID), engine: instance, kinds: map[runtimeprotocol.DocumentID]port.ExternalFileKind{}}
	resolver := config.LocalActor
	if resolver == nil {
		resolver = accesscore.StaticLocalActorResolver{ActorID: "local-owner"}
	}
	actor, err := resolver.ResolveLocalActor(context.Background())
	if err != nil {
		return nil, err
	}
	delegations, err := loadDelegations(config.Root)
	if err != nil {
		return nil, err
	}
	authority := newLocalAuthorityWithDelegations(config.Clock, config.Random, actor, delegations)
	host := &Host{config: config, engine: instance, documents: documents, state: state, assets: assets, history: history, recovery: recovery, external: external, authority: authority, workbench: workbench, sessions: map[runtimeprotocol.RuntimeSessionID]*Session{}, autosaves: map[runtimeprotocol.RuntimeSessionID]func(){}}
	metadata, err := host.loadMetadata()
	if err != nil {
		return nil, err
	}
	host.metadata = metadata
	for _, binding := range metadata.Bindings {
		authority.add(binding.DocumentID)
	}
	runtimeValue, err := runtimehost.New(runtimehost.Config{ReleaseVersion: config.ReleaseVersion, EndpointInstanceID: endpointID, ReleaseManifestDigest: config.ReleaseManifestDigest, Limits: defaultRuntimeLimits(), Ports: runtimehost.Ports{Workbench: workbench, Grants: authority, Scopes: authority, Documents: documents, State: state, StateBindings: localStateBinding{backend: state}, StateAccess: authority, Assets: assets, History: history, Recovery: recovery, External: external, Authoring: authority, Clock: authority, Identities: authority}})
	if err != nil {
		return nil, err
	}
	host.runtime = runtimeValue
	return host, nil
}

func defaultRuntimeLimits() runtimeprotocol.RuntimeLimits {
	items := runtimeprotocol.RuntimeItemLimitValue{HardMaximum: "4096", Unit: runtimeprotocol.RuntimeItemLimitValueUnitValue}
	bytes := runtimeprotocol.RuntimeByteLimitValue{HardMaximum: "268435456", Unit: runtimeprotocol.RuntimeByteLimitValueUnitValue}
	return runtimeprotocol.RuntimeLimits{MaxBlobBytes: bytes, MaxBlobTotalBytes: bytes, MaxCommitOperations: items, MaxHistoryItems: items, MaxOutputBytes: bytes, MaxStateMutations: items}
}

func (h *Host) OpenProject(ctx context.Context, input OpenProjectInput) (OpenResult, error) {
	root, err := canonicalLocalPath(input.Root, true)
	if err != nil {
		return OpenResult{}, err
	}
	if input.EntryPath == "" {
		input.EntryPath = "document.ldl"
	}
	tree, err := readProjectTree(ctx, root, h.config.MaxProjectFiles, h.config.MaxProjectBytes)
	if err != nil {
		return OpenResult{}, err
	}
	if _, ok := tree[input.EntryPath]; !ok {
		return OpenResult{}, fmt.Errorf("entry module is unavailable: %w", port.ErrNotFound)
	}
	resolved := input.ResolvedDependencies
	if resolved.Format == "" {
		resolved = engineendpoint.LocalResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}
	}
	source, err := h.engine.CompileProject(ctx, engineendpoint.LocalProjectInput{EntryPath: input.EntryPath, ProjectSourceTree: tree, InstalledPackTree: cloneByteMap(input.InstalledPackTree), ResolvedDependencies: resolved, ReferencedAssets: append([]engineendpoint.LocalAssetInput(nil), input.ReferencedAssets...), ResourceLimits: input.ResourceLimits})
	if err != nil {
		return OpenResult{}, err
	}
	return h.openSource(ctx, "project", root, source, false)
}

func (h *Host) OpenContainer(ctx context.Context, path string) (OpenResult, error) {
	return h.openContainer(ctx, path, false)
}

// OpenDocument reopens an already host-bound document by its host identity.
// This is the durable route for import-as-new documents whose source locator
// may also be bound to another host document.
func (h *Host) OpenDocument(ctx context.Context, documentID runtimeprotocol.DocumentID) (OpenResult, error) {
	h.mu.Lock()
	var binding documentBinding
	found := false
	for _, candidate := range h.metadata.Bindings {
		if candidate.DocumentID == documentID {
			binding, found = candidate, true
			break
		}
	}
	h.mu.Unlock()
	if !found {
		return OpenResult{}, port.ErrNotFound
	}
	scope := h.authority.add(documentID)
	head, err := h.documents.GetHead(ctx, port.GetDocumentHeadInput{Scope: scope})
	if err != nil {
		return OpenResult{}, err
	}
	revision, err := h.documents.ReadRevision(ctx, port.ReadRevisionInput{Scope: scope, RevisionID: head.Revision.RevisionID})
	if err != nil {
		return OpenResult{}, err
	}
	sources, err := h.documents.ReadSourceBlobs(ctx, port.ReadSourceBlobsInput{Scope: scope, Revision: revision.Revision, Blobs: revision.SourceBlobs})
	if err != nil {
		return OpenResult{}, err
	}
	var encoded []byte
	for _, blob := range sources.Blobs {
		if blob.Ref == revision.Manifest {
			encoded = blob.Contents
			break
		}
	}
	if encoded == nil {
		return OpenResult{}, port.ErrIndeterminate
	}
	source, err := h.engine.ReadEncodedInput(ctx, encoded)
	if err != nil {
		return OpenResult{}, err
	}
	return h.openBound(ctx, binding, binding.SourceDigest, source)
}

// ImportContainer always assigns a new host Document ID. Portable project
// identity, dependency source metadata, StableAddresses, and references remain
// unchanged because the validated closed CompileInput is persisted verbatim.
func (h *Host) ImportContainer(ctx context.Context, path string) (OpenResult, error) {
	return h.openContainer(ctx, path, true)
}

func (h *Host) openContainer(ctx context.Context, path string, importAsNew bool) (OpenResult, error) {
	locator, err := canonicalLocalPath(path, false)
	if err != nil {
		return OpenResult{}, err
	}
	data, err := os.ReadFile(locator)
	if err != nil {
		return OpenResult{}, err
	}
	document, err := h.engine.ReadContainer(ctx, data)
	if err != nil {
		return OpenResult{}, err
	}
	return h.openSource(ctx, "container", locator, document, importAsNew)
}

func (h *Host) openSource(ctx context.Context, kind, locator string, source engineendpoint.LocalSource, importAsNew bool) (OpenResult, error) {
	sourceDigest := source.Digest()
	portableID := source.PortableID
	key := bindingKey(kind, locator)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return OpenResult{}, errors.New("local document host is closed")
	}
	if len(h.sessions) >= h.config.MaxSessions {
		h.mu.Unlock()
		return OpenResult{}, errors.New("local document session limit reached")
	}
	binding, exists := h.metadata.Bindings[key]
	h.mu.Unlock()
	if importAsNew {
		exists = false
	}
	if !exists {
		documentIDValue, err := h.authority.NewID(ctx, port.IdentityRevision)
		if err != nil {
			return OpenResult{}, err
		}
		documentID := runtimeprotocol.DocumentID("doc_" + strings.TrimPrefix(documentIDValue, "revision_"))
		scope := h.authority.add(documentID)
		binding = documentBinding{DocumentID: documentID, Kind: kind, Locator: locator, PortableID: portableID, SourceDigest: sourceDigest}
		if importAsNew {
			key = key + "\x00" + string(documentID)
		}
		if err := h.initializeDocument(ctx, scope, source); err != nil {
			return OpenResult{}, err
		}
		h.mu.Lock()
		h.metadata.Bindings[key] = binding
		err = h.saveMetadataLocked()
		h.mu.Unlock()
		if err != nil {
			return OpenResult{}, err
		}
	} else if binding.PortableID != portableID {
		return OpenResult{}, errors.New("portable project identity changed at bound source")
	}
	return h.openBound(ctx, binding, sourceDigest, source)
}

func (h *Host) initializeDocument(ctx context.Context, scope runtimeprotocol.RuntimeScope, source engineendpoint.LocalSource) error {
	revisionIDValue, err := h.authority.NewID(ctx, port.IdentityRevision)
	if err != nil {
		return err
	}
	provider := runtimeprotocol.ProviderVersionToken("1")
	revision := runtimeprotocol.CommittedRevisionRef{DocumentID: scope.DocumentID, RevisionID: runtimeprotocol.RevisionID(revisionIDValue), DefinitionHash: source.DefinitionHash, GraphHash: source.GraphHash, ProviderVersion: &provider}
	encoded, manifest, err := source.EncodedInput()
	if err != nil {
		return err
	}
	sources := port.SourceBlobSet{Revision: revision, Blobs: []port.SourceBlob{{Ref: manifest, Contents: encoded}}}
	refs := make([]protocolcommon.BlobRef, len(sources.Blobs))
	for i := range sources.Blobs {
		refs[i] = sources.Blobs[i].Ref
	}
	if err := h.documents.InitializeDocument(ctx, scope, port.RevisionSnapshot{Revision: revision, SourceBlobs: refs, Manifest: manifest}, provider, "0", sources.Blobs); err != nil {
		return err
	}
	stateRef := protocolcommon.BlobRef{BlobID: "local-empty-state", Digest: digestJSON(struct{}{}), Lifetime: protocolcommon.BlobLifetimePersistent, MediaType: "application/json", Size: "2"}
	stateHead := port.StateHead{StateVersion: "0", BackendVersion: "1", DefinitionHash: revision.DefinitionHash, GraphHash: revision.GraphHash, CapturedAt: protocolcommon.Rfc3339Time(h.config.Clock.Now().UTC().Format(time.RFC3339Nano)), SubjectHashes: map[semantic.StableAddress]protocolcommon.Digest{}}
	if err := h.state.InitializeState(ctx, scope, port.StateSnapshot{Head: stateHead, Contents: stateRef, InaccessibleFieldPaths: []semantic.StateFieldPath{}, Records: []port.StateRecord{}}); err != nil {
		return err
	}
	operation := runtimeprotocol.OperationID("bootstrap_" + string(revision.RevisionID))
	_, err = h.history.AppendRevision(ctx, port.AppendRevisionInput{Scope: scope, Metadata: runtimeprotocol.RevisionMetadata{Revision: revision, OperationID: operation, CommittedAt: protocolcommon.Rfc3339Time(h.config.Clock.Now().UTC().Format(time.RFC3339Nano)), Trigger: runtimeprotocol.CommitTriggerRestore, AuthoringDecisionDigest: digestJSON(struct {
		Operation runtimeprotocol.OperationID `json:"operation"`
	}{operation})}})
	return err
}

func (h *Host) openBound(ctx context.Context, binding documentBinding, externalDigest protocolcommon.Digest, sourceInput engineendpoint.LocalSource) (OpenResult, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return OpenResult{}, errors.New("local document host is closed")
	}
	if len(h.sessions) >= h.config.MaxSessions {
		h.mu.Unlock()
		return OpenResult{}, errors.New("local document session limit reached")
	}
	h.mu.Unlock()
	h.authority.add(binding.DocumentID)
	kind := port.ExternalFileKind(binding.Kind)
	if err := h.external.Bind(ctx, local.ExternalFileBinding{Scope: h.authority.add(binding.DocumentID), Kind: kind, Locator: binding.Locator}); err != nil {
		return OpenResult{}, err
	}
	h.workbench.BindExternal(binding.DocumentID, kind)
	if _, err := h.Recover(ctx, binding.DocumentID); err != nil {
		return OpenResult{}, err
	}
	opened, rejection := h.runtime.OpenDocument(ctx, runtimeprotocol.OpenRuntimeDocumentInput{DocumentID: binding.DocumentID})
	if rejection != nil {
		return OpenResult{}, rejection
	}
	working, ok := h.workbench.Opened(opened.CommittedRevision)
	if !ok {
		return OpenResult{}, errors.New("working document binding unavailable")
	}
	session := &Session{Open: opened, PortableID: binding.PortableID, SourceKind: binding.Kind, SourceLocator: binding.Locator, SourceDigest: binding.SourceDigest, working: working, sourceInput: sourceInput}
	h.mu.Lock()
	h.sessions[opened.Session.RuntimeSessionID] = session
	h.mu.Unlock()
	history, rejection := h.runtime.ListRevisions(ctx, runtimeprotocol.ListRevisionsInput{Session: opened.Session, MaxItems: "128", MaxOutputBytes: "1048576"})
	if rejection != nil {
		_ = h.Close(ctx, session)
		return OpenResult{}, rejection
	}
	result := OpenResult{Session: session, History: history}
	if externalDigest != binding.SourceDigest {
		result.ExternalChange = &ExternalChange{DocumentID: binding.DocumentID, CommittedDigest: binding.SourceDigest, ExternalDigest: externalDigest, Kind: binding.Kind, RequiresReview: true}
	}
	return result, nil
}

func (h *Host) Save(ctx context.Context, input SaveInput) (runtimeprotocol.RuntimeCommitResult, error) {
	if input.Session == nil {
		return runtimeprotocol.RuntimeCommitResult{}, errors.New("session is required")
	}
	h.mu.Lock()
	tracked := h.sessions[input.Session.Open.Session.RuntimeSessionID]
	closed := input.Session.closed || tracked != input.Session
	h.mu.Unlock()
	if closed {
		return runtimeprotocol.RuntimeCommitResult{}, errors.New("session is closed or unknown")
	}
	ctx = h.accessContext(ctx, input.Session)
	if input.Trigger == "" {
		input.Trigger = runtimeprotocol.CommitTriggerExplicitSave
	}
	if input.Trigger != runtimeprotocol.CommitTriggerExplicitSave && input.Trigger != runtimeprotocol.CommitTriggerAutosave {
		return runtimeprotocol.RuntimeCommitResult{}, errors.New("local lifecycle supports explicit_save or autosave")
	}
	// Durable, document-scoped journal identity is the retry authority. This
	// avoids retaining an unbounded process cache and survives host restart.
	if input.OperationID != "" && input.IdempotencyKey != "" {
		record, err := h.recovery.Get(ctx, port.GetRecoveryRecordInput{Scope: input.Session.Open.Session.Scope, OperationID: &input.OperationID})
		if err == nil {
			if record.Status.IdempotencyKey != input.IdempotencyKey {
				return runtimeprotocol.RuntimeCommitResult{}, port.ErrConflict
			}
			candidate := runtimeprotocol.RuntimeCommitInput{Session: input.Session.Open.Session, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey, OperationBatch: runtimeprotocol.RuntimeOperationBatch{DocumentID: record.BaseRevision.DocumentID, BaseRevision: record.BaseRevision, ExpectedDefinitionHash: record.BaseRevision.DefinitionHash, Operations: input.Operations, Preconditions: input.Preconditions}, Trigger: input.Trigger}
			if runtimehost.RetryRequestDigest(candidate) != record.PayloadDigest {
				return runtimeprotocol.RuntimeCommitResult{}, port.ErrConflict
			}
			if record.CommitResult != nil {
				return *record.CommitResult, nil
			}
		} else if !errors.Is(err, port.ErrNotFound) {
			return runtimeprotocol.RuntimeCommitResult{}, err
		}
	}
	if change, err := h.detectExternalChange(ctx, input.Session); err != nil {
		return runtimeprotocol.RuntimeCommitResult{}, err
	} else if change != nil {
		return runtimeprotocol.RuntimeCommitResult{}, fmt.Errorf("external source change requires reconcile preview: %w", port.ErrConflict)
	}
	if input.OperationID == "" {
		value, err := h.authority.NewID(ctx, port.IdentityOperation)
		if err != nil {
			return runtimeprotocol.RuntimeCommitResult{}, err
		}
		input.OperationID = runtimeprotocol.OperationID(value)
	}
	if input.IdempotencyKey == "" {
		value, err := h.authority.NewID(ctx, port.IdentityOperation)
		if err != nil {
			return runtimeprotocol.RuntimeCommitResult{}, err
		}
		input.IdempotencyKey = runtimeprotocol.IdempotencyKey("idem_" + value)
	}
	current := input.Session.Open.CommittedRevision
	input.Preconditions.DocumentGeneration = engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: h.config.EndpointInstanceID, Value: input.Session.working.Handle}, Value: protocolcommon.CanonicalUint64(input.Session.working.Generation)}
	prepared, err := h.workbench.Preview(ctx, port.PreviewWorkingDocumentInput{Document: input.Session.working, Batch: input.Operations, Preconditions: input.Preconditions, MaxOperations: "4096"})
	if err != nil {
		return runtimeprotocol.RuntimeCommitResult{}, err
	}
	grant, _, err := h.authority.ResolveGrant(ctx, input.Session.Open.Session.Scope)
	if err != nil {
		return runtimeprotocol.RuntimeCommitResult{}, err
	}
	evaluation := accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &prepared.AuthoringImpact, GrantSnapshot: grant, HostOperationImpacts: []accessprotocol.HostOperationImpact{}, RequestIntent: "propose"}
	decision, authorizationRejection := h.runtime.Authorize(ctx, runtimehost.AuthorizationRequest{Scope: input.Session.Open.Session.Scope, CurrentRevision: current, Evaluation: evaluation})
	if authorizationRejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, authorizationRejection
	}
	proof := runtimeprotocol.AuthoringProof{AccessFingerprint: grant.AccessFingerprint, BaseRevision: current, DecisionDigest: decision.DecisionDigest, EvaluationDigest: decision.EvaluationDigest, MembershipVersion: grant.MembershipVersion, PolicyRefs: grant.PolicyRefs}
	commit := runtimeprotocol.RuntimeCommitInput{Session: input.Session.Open.Session, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey, OperationBatch: runtimeprotocol.RuntimeOperationBatch{DocumentID: current.DocumentID, BaseRevision: current, ExpectedDefinitionHash: current.DefinitionHash, Operations: input.Operations, Preconditions: input.Preconditions}, AuthoringProof: proof, Trigger: input.Trigger, CancellationToken: input.Cancellation}
	result, rejection := h.runtime.CommitOperations(ctx, commit)
	if rejection != nil {
		return runtimeprotocol.RuntimeCommitResult{}, rejection
	}
	if err := h.applyCommit(input.Session, result); err != nil {
		return result, err
	}
	return result, nil
}

func (h *Host) acceptSessionSourceBaseline(session *Session, digest protocolcommon.Digest) error {
	if _, err := protocolcommon.EncodeDigest(digest); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if session == nil || session.closed || h.sessions[session.Open.Session.RuntimeSessionID] != session {
		return errors.New("session is closed or unknown")
	}
	session.SourceDigest = digest
	return h.acceptDocumentSourceBaselineLocked(session.Open.Session.Scope.DocumentID, digest)
}

func (h *Host) acceptDocumentSourceBaseline(documentID runtimeprotocol.DocumentID, digest protocolcommon.Digest) error {
	if _, err := protocolcommon.EncodeDigest(digest); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.acceptDocumentSourceBaselineLocked(documentID, digest)
}

func (h *Host) acceptDocumentSourceBaselineLocked(documentID runtimeprotocol.DocumentID, digest protocolcommon.Digest) error {
	for key, binding := range h.metadata.Bindings {
		if binding.DocumentID != documentID {
			continue
		}
		if binding.SourceDigest == digest {
			return nil
		}
		prior := binding
		binding.SourceDigest = digest
		h.metadata.Bindings[key] = binding
		if err := h.saveMetadataLocked(); err != nil {
			h.metadata.Bindings[key] = prior
			return err
		}
		return nil
	}
	return port.ErrNotFound
}

func (h *Host) GetOperationStatus(ctx context.Context, session *Session, operation runtimeprotocol.OperationID) (runtimeprotocol.RuntimeOperationStatus, error) {
	if session == nil {
		return runtimeprotocol.RuntimeOperationStatus{}, errors.New("session is required")
	}
	resolved, err := h.SessionFor(session.Open.Session)
	if err != nil || resolved != session {
		return runtimeprotocol.RuntimeOperationStatus{}, errors.New("session is closed or unknown")
	}
	ctx = h.accessContext(ctx, session)
	if err := h.authority.AuthorizeRead(ctx, session.Open.Session.Scope, accesscore.SurfaceReview); err != nil {
		return runtimeprotocol.RuntimeOperationStatus{}, err
	}
	status, rejection := h.runtime.GetOperationResult(ctx, runtimeprotocol.GetOperationResultInput{Session: session.Open.Session, LookupBy: "operation_id", OperationID: &operation})
	if rejection != nil {
		return runtimeprotocol.RuntimeOperationStatus{}, rejection
	}
	return status, nil
}

func (h *Host) ScheduleAutosave(ctx context.Context, input SaveInput, result chan<- AutosaveResult) error {
	if input.Session == nil {
		return errors.New("session is required")
	}
	input.Trigger = runtimeprotocol.CommitTriggerAutosave
	id := input.Session.Open.Session.RuntimeSessionID
	h.mu.Lock()
	if prior := h.autosaves[id]; prior != nil {
		prior()
	}
	if input.Session.closed || h.sessions[id] != input.Session {
		h.mu.Unlock()
		return errors.New("session is closed or unknown")
	}
	cancel := h.config.Scheduler.Schedule(h.config.AutosaveDelay, func() {
		value, err := h.Save(context.WithoutCancel(ctx), input)
		if result != nil {
			result <- AutosaveResult{Result: value, Err: err}
		}
		h.mu.Lock()
		delete(h.autosaves, id)
		h.mu.Unlock()
	})
	h.autosaves[id] = cancel
	h.mu.Unlock()
	return nil
}

func (h *Host) CancelAutosave(ref runtimeprotocol.RuntimeSessionRef) error {
	session, err := h.SessionFor(ref)
	if err != nil {
		return err
	}
	id := session.Open.Session.RuntimeSessionID
	h.mu.Lock()
	if cancel := h.autosaves[id]; cancel != nil {
		cancel()
		delete(h.autosaves, id)
	}
	h.mu.Unlock()
	return nil
}

func (h *Host) CancelOperation(ctx context.Context, session *Session, operation runtimeprotocol.OperationID, token runtimeprotocol.CancellationToken) (runtimeprotocol.CancelOperationResult, error) {
	if session == nil {
		return runtimeprotocol.CancelOperationResult{}, errors.New("session is required")
	}
	result, rejection := h.runtime.CancelOperation(ctx, runtimeprotocol.CancelOperationInput{Session: session.Open.Session, OperationID: operation, CancellationToken: token})
	if rejection != nil {
		return runtimeprotocol.CancelOperationResult{}, rejection
	}
	return result, nil
}

func (h *Host) Close(ctx context.Context, session *Session) error {
	if session == nil {
		return nil
	}
	id := session.Open.Session.RuntimeSessionID
	h.mu.Lock()
	if cancel := h.autosaves[id]; cancel != nil {
		cancel()
		delete(h.autosaves, id)
	}
	if session.closed {
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()
	if rejection := h.runtime.CloseDocument(ctx, session.Open.Session); rejection != nil {
		return rejection
	}
	h.mu.Lock()
	if h.sessions[id] == session {
		session.closed = true
		delete(h.sessions, id)
	}
	h.mu.Unlock()
	return nil
}

func (h *Host) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	sessions := make([]*Session, 0, len(h.sessions))
	for _, session := range h.sessions {
		sessions = append(sessions, session)
	}
	h.closed = true
	h.mu.Unlock()
	var first error
	for _, session := range sessions {
		if err := h.Close(ctx, session); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (h *Host) detectExternalChange(ctx context.Context, session *Session) (*ExternalChange, error) {
	var digest protocolcommon.Digest
	switch session.SourceKind {
	case "container":
		data, err := os.ReadFile(session.SourceLocator)
		if err != nil {
			return nil, err
		}
		document, err := h.engine.ReadContainer(ctx, data)
		if err != nil {
			return nil, err
		}
		digest = document.Digest()
	case "project":
		tree, err := readProjectTree(ctx, session.SourceLocator, h.config.MaxProjectFiles, h.config.MaxProjectBytes)
		if err != nil {
			return nil, err
		}
		candidate, err := h.engine.WithProjectTree(ctx, session.sourceInput, tree)
		if err != nil {
			return nil, err
		}
		digest = candidate.Digest()
	default:
		return nil, errors.New("unknown local source kind")
	}
	if digest == session.SourceDigest {
		return nil, nil
	}
	return &ExternalChange{DocumentID: session.Open.Session.Scope.DocumentID, CommittedDigest: session.SourceDigest, ExternalDigest: digest, Kind: session.SourceKind, RequiresReview: true}, nil
}

func bindingKey(kind, locator string) string { return kind + ":" + locator }

func canonicalLocalPath(path string, directory bool) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(real)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return "", errors.New("unsafe local source path")
	}
	return filepath.Clean(real), nil
}

func readProjectTree(ctx context.Context, root string, maxFiles int, maxBytes int64) (map[string][]byte, error) {
	result := map[string][]byte{}
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("project tree contains a symlink")
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("project tree contains a special file")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasSuffix(rel, ".ldl") {
			return nil
		}
		if len(result) >= maxFiles || info.Size() > maxBytes-total {
			return errors.New("project source tree exceeds configured bounds")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if int64(len(data)) != info.Size() {
			return errors.New("project source changed during read")
		}
		result[rel] = data
		total += int64(len(data))
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, errors.New("project contains no LDL source")
	}
	return result, nil
}

func cloneByteMap(input map[string][]byte) map[string][]byte {
	result := make(map[string][]byte, len(input))
	for key, value := range input {
		result[key] = append([]byte(nil), value...)
	}
	return result
}

func (h *Host) metadataPath() string {
	return filepath.Join(h.config.Root, "local-document-bindings.json")
}

func (h *Host) readMetadataFile() ([]byte, error) {
	// The private stdio caller explicitly grants this absolute storage root;
	// New validates and owns the root before metadata access.
	return os.ReadFile(h.metadataPath()) // lgtm[go/path-injection]
}

func (h *Host) loadMetadata() (lifecycleMetadata, error) {
	data, err := h.readMetadataFile()
	if errors.Is(err, fs.ErrNotExist) {
		return lifecycleMetadata{Version: metadataVersion, Bindings: map[string]documentBinding{}}, nil
	}
	if err != nil {
		return lifecycleMetadata{}, err
	}
	var value lifecycleMetadata
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || value.Version != metadataVersion || value.Bindings == nil {
		return lifecycleMetadata{}, errors.New("corrupt local document bindings")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return lifecycleMetadata{}, errors.New("corrupt local document bindings")
	}
	seenDocuments := map[runtimeprotocol.DocumentID]bool{}
	for key, binding := range value.Bindings {
		baseKey := bindingKey(binding.Kind, binding.Locator)
		importKey := baseKey + "\x00" + string(binding.DocumentID)
		_, documentErr := runtimeprotocol.EncodeOpenRuntimeDocumentInput(runtimeprotocol.OpenRuntimeDocumentInput{DocumentID: binding.DocumentID})
		_, portableErr := semantic.EncodeProjectRootAddress(semantic.ProjectRootAddress(binding.PortableID))
		_, digestErr := protocolcommon.EncodeDigest(binding.SourceDigest)
		if documentErr != nil || portableErr != nil || digestErr != nil || seenDocuments[binding.DocumentID] || (binding.Kind != "project" && binding.Kind != "container") || !filepath.IsAbs(binding.Locator) || filepath.Clean(binding.Locator) != binding.Locator || (key != baseKey && key != importKey) {
			return lifecycleMetadata{}, errors.New("corrupt local document bindings")
		}
		seenDocuments[binding.DocumentID] = true
	}
	return value, nil
}

func (h *Host) saveMetadataLocked() error {
	data, err := json.Marshal(h.metadata)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(h.config.Root, ".local-document-bindings-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(metadataFileMode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, h.metadataPath()); err != nil {
		return err
	}
	dir, err := os.Open(h.config.Root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
