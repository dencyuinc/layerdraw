// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/privatefs"
)

const referenceExternalStateVersion = 1

// ReferenceExternalStorageConfig constructs the deterministic provider used by
// Desktop contract tests and offline/local deployments. Credentials are
// resolved from the host credential port for each authorization/refresh and
// are zeroed immediately; neither their bytes nor their reference are written
// to the adapter state file.
type ReferenceExternalStorageConfig struct {
	Root        string
	Credentials desktopcontract.CredentialPort
	Now         func() time.Time
}

type referenceConnection struct {
	ConnectionID string                     `json:"connection_id"`
	ProviderID   string                     `json:"provider_id"`
	AccountLabel string                     `json:"account_label"`
	ScopeLabel   string                     `json:"scope_label"`
	Status       ExternalConnectionStatus   `json:"status"`
	Capabilities ExternalProviderCapability `json:"capabilities"`
}

type referenceBinding struct {
	Binding       ExternalBackendBinding               `json:"binding"`
	LocalRevision runtimeprotocol.CommittedRevisionRef `json:"local_revision"`
	PayloadDigest string                               `json:"payload_digest,omitempty"`
}

type referenceExternalState struct {
	Version     int                            `json:"version"`
	Connections map[string]referenceConnection `json:"connections"`
	Bindings    map[string]referenceBinding    `json:"bindings"`
}

// ReferenceExternalStorage is a restart-safe, deterministic StorageAdapter.
// It intentionally knows nothing about LDL, Runtime revisions, or Access
// policy; the Desktop owner supplies those prevalidated values.
type ReferenceExternalStorage struct {
	mu             sync.Mutex
	root           string
	statePath      string
	credentials    desktopcontract.CredentialPort
	now            func() time.Time
	state          referenceExternalState
	leases         map[string]ExternalLease
	credentialRefs map[string]desktopcontract.CredentialRef
}

func NewReferenceExternalStorage(config ReferenceExternalStorageConfig) (*ReferenceExternalStorage, error) {
	if config.Root == "" || !filepath.IsAbs(config.Root) || config.Credentials == nil {
		return nil, errors.New("external storage composition is incomplete")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	root := filepath.Join(config.Root, "external-storage-reference")
	if err := os.MkdirAll(filepath.Join(root, "objects"), 0o700); err != nil {
		return nil, err
	}
	adapter := &ReferenceExternalStorage{
		root: root, statePath: filepath.Join(root, "state.json"), credentials: config.Credentials,
		now: config.Now, leases: map[string]ExternalLease{}, credentialRefs: map[string]desktopcontract.CredentialRef{},
		state: referenceExternalState{Version: referenceExternalStateVersion, Connections: map[string]referenceConnection{}, Bindings: map[string]referenceBinding{}},
	}
	if err := adapter.load(); err != nil {
		return nil, err
	}
	return adapter, nil
}

func (a *ReferenceExternalStorage) Start(context.Context) error { return nil }
func (a *ReferenceExternalStorage) Shutdown(context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	clear(a.leases)
	return nil
}

func (a *ReferenceExternalStorage) Connect(ctx context.Context, request ExternalConnectionRequest) desktopcontract.Result[ExternalConnection] {
	if request.ProviderID == "" || request.CredentialRef.ID == "" || request.AccountLabel == "" || request.ScopeLabel == "" {
		return failed[ExternalConnection](desktopcontract.FailureCredential, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryReconnect)
	}
	credential := safeResolveCredential(ctx, a.credentials, request.CredentialRef)
	defer clear(credential.Value)
	if !credential.Validate() || credential.Outcome != protocolcommon.OutcomeSuccess || len(credential.Value) == 0 {
		return failed[ExternalConnection](desktopcontract.FailureCredential, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryReconnect)
	}
	connection := referenceConnection{
		ConnectionID: stableExternalID("connection", request.ProviderID, request.AccountLabel, request.ScopeLabel),
		ProviderID:   request.ProviderID, AccountLabel: request.AccountLabel, ScopeLabel: request.ScopeLabel,
		Status:       ExternalConnectionConnected,
		Capabilities: ExternalProviderCapability{Open: true, ConditionalWrite: true, Lease: true, MoveDetection: true, ResumableUpload: true},
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Connections[connection.ConnectionID] = connection
	a.credentialRefs[connection.ConnectionID] = request.CredentialRef
	if err := a.saveLocked(); err != nil {
		return failed[ExternalConnection](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryRetry)
	}
	return success(connection.public())
}

func (a *ReferenceExternalStorage) Inspect(_ context.Context, connectionID string) desktopcontract.Result[ExternalConnection] {
	a.mu.Lock()
	defer a.mu.Unlock()
	connection, ok := a.state.Connections[connectionID]
	if !ok {
		return failed[ExternalConnection](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryReconnect)
	}
	return success(connection.public())
}

func (a *ReferenceExternalStorage) Refresh(ctx context.Context, connectionID string) desktopcontract.Result[ExternalConnection] {
	a.mu.Lock()
	connection, exists := a.state.Connections[connectionID]
	ref, authorized := a.credentialRefs[connectionID]
	a.mu.Unlock()
	if !exists || !authorized {
		return failed[ExternalConnection](desktopcontract.FailureCredential, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryReconnect)
	}
	credential := safeResolveCredential(ctx, a.credentials, ref)
	defer clear(credential.Value)
	if !credential.Validate() || credential.Outcome != protocolcommon.OutcomeSuccess || len(credential.Value) == 0 {
		a.mu.Lock()
		connection.Status = ExternalConnectionExpired
		a.state.Connections[connectionID] = connection
		_ = a.saveLocked()
		a.mu.Unlock()
		return failed[ExternalConnection](desktopcontract.FailureCredential, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryReconnect)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	connection.Status = ExternalConnectionConnected
	a.state.Connections[connectionID] = connection
	if err := a.saveLocked(); err != nil {
		return failed[ExternalConnection](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryRetry)
	}
	return success(connection.public())
}

func (a *ReferenceExternalStorage) Disconnect(_ context.Context, connectionID string) desktopcontract.Result[ExternalConnection] {
	a.mu.Lock()
	defer a.mu.Unlock()
	connection, ok := a.state.Connections[connectionID]
	if !ok {
		return failed[ExternalConnection](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	connection.Status = ExternalConnectionDisconnected
	a.state.Connections[connectionID] = connection
	delete(a.credentialRefs, connectionID)
	for key, lease := range a.leases {
		if binding, exists := a.state.Bindings[key]; exists && binding.Binding.ConnectionID == connectionID {
			delete(a.leases, key)
		}
		_ = lease
	}
	if err := a.saveLocked(); err != nil {
		return failed[ExternalConnection](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryRetry)
	}
	return success(connection.public())
}

func (a *ReferenceExternalStorage) SelectRemote(_ context.Context, request ExternalRemoteSelectionRequest) desktopcontract.Result[ExternalBackendBinding] {
	if request.DocumentID == "" || request.SelectionToken == "" {
		return failed[ExternalBackendBinding](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryReview)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	connection, ok := a.state.Connections[request.ConnectionID]
	if !ok || connection.Status != ExternalConnectionConnected {
		return failed[ExternalBackendBinding](desktopcontract.FailureReconnect, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryReconnect)
	}
	remoteID := stableExternalID("item", request.ConnectionID, request.SelectionToken)
	bindingID := stableExternalID("binding", request.ConnectionID, string(request.DocumentID), remoteID)
	binding := ExternalBackendBinding{BindingID: bindingID, ConnectionID: request.ConnectionID, DocumentID: request.DocumentID, RemoteItemID: remoteID, ProviderVersion: "v0"}
	if existing, exists := a.state.Bindings[bindingID]; exists {
		binding = existing.Binding
	} else {
		a.state.Bindings[bindingID] = referenceBinding{Binding: binding}
		if err := a.saveLocked(); err != nil {
			return failed[ExternalBackendBinding](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryRetry)
		}
	}
	return success(binding)
}

func (a *ReferenceExternalStorage) AcquireLease(_ context.Context, binding ExternalBackendBinding) desktopcontract.Result[ExternalLease] {
	a.mu.Lock()
	defer a.mu.Unlock()
	stored, ok := a.state.Bindings[binding.BindingID]
	if !ok || stored.Binding != binding || !a.connectionLiveLocked(binding.ConnectionID) {
		return failed[ExternalLease](desktopcontract.FailureProjectConflict, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryReconnect)
	}
	expires := a.now().UTC().Add(5 * time.Minute)
	lease := ExternalLease{Token: stableExternalID("lease", binding.BindingID, expires.Format(time.RFC3339Nano)), ExpiresAt: protocolcommon.Rfc3339Time(expires.Format(time.RFC3339Nano))}
	a.leases[binding.BindingID] = lease
	return success(lease)
}

func (a *ReferenceExternalStorage) Write(ctx context.Context, request ExternalWriteRequest) desktopcontract.Result[ExternalWriteResult] {
	if err := ctx.Err(); err != nil {
		return success(ExternalWriteResult{State: ExternalWriteOffline, Retryable: true})
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	stored, ok := a.state.Bindings[request.Binding.BindingID]
	if !ok || stored.Binding.ConnectionID != request.Binding.ConnectionID || stored.Binding.DocumentID != request.Binding.DocumentID {
		return success(ExternalWriteResult{State: ExternalWriteMoved, Retryable: false})
	}
	if !a.connectionLiveLocked(request.Binding.ConnectionID) {
		return success(ExternalWriteResult{State: ExternalWriteOffline, Retryable: true})
	}
	lease, leased := a.leases[request.Binding.BindingID]
	expires, _ := time.Parse(time.RFC3339Nano, string(lease.ExpiresAt))
	if !leased || request.LeaseToken == "" || request.LeaseToken != lease.Token || !a.now().Before(expires) {
		return success(ExternalWriteResult{State: ExternalWriteConflict, ProviderVersion: stored.Binding.ProviderVersion, Retryable: true})
	}
	if request.ExpectedProviderVersion != stored.Binding.ProviderVersion {
		return success(ExternalWriteResult{State: ExternalWriteConflict, ProviderVersion: stored.Binding.ProviderVersion, Retryable: true})
	}
	version := nextProviderVersion(stored.Binding.ProviderVersion)
	payloadDigest := sha256.Sum256(request.Payload)
	objectPath := filepath.Join(a.root, "objects", hex.EncodeToString(payloadDigest[:]))
	if err := writePrivateAtomic(objectPath, request.Payload); err != nil {
		return success(ExternalWriteResult{State: ExternalWritePartial, ProviderVersion: stored.Binding.ProviderVersion, Retryable: true})
	}
	stored.Binding.ProviderVersion = version
	stored.LocalRevision = request.Revision
	stored.PayloadDigest = "sha256:" + hex.EncodeToString(payloadDigest[:])
	a.state.Bindings[request.Binding.BindingID] = stored
	delete(a.leases, request.Binding.BindingID)
	if err := a.saveLocked(); err != nil {
		return success(ExternalWriteResult{State: ExternalWriteUnknown, Retryable: true})
	}
	return success(ExternalWriteResult{State: ExternalWritePublished, ProviderVersion: version})
}

func (a *ReferenceExternalStorage) Sync(_ context.Context, request ExternalSyncRequest) desktopcontract.Result[ExternalSyncResult] {
	a.mu.Lock()
	defer a.mu.Unlock()
	binding, ok := a.bindingForLocked(request.ConnectionID, request.DocumentID)
	if !ok || !a.connectionLiveLocked(request.ConnectionID) {
		return failed[ExternalSyncResult](desktopcontract.FailureReconnect, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryReconnect)
	}
	reconcile := binding.LocalRevision.RevisionID != "" && binding.LocalRevision.RevisionID != request.Revision.RevisionID
	return success(ExternalSyncResult{ProviderVersion: binding.Binding.ProviderVersion, ReconcileNeeded: reconcile})
}

func (a *ReferenceExternalStorage) PlanReconcile(_ context.Context, request ExternalSyncRequest, restricted bool) desktopcontract.Result[ExternalReconcilePlan] {
	a.mu.Lock()
	defer a.mu.Unlock()
	binding, ok := a.bindingForLocked(request.ConnectionID, request.DocumentID)
	if !ok {
		return failed[ExternalReconcilePlan](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	kind := ExternalReconcileUpToDate
	if binding.LocalRevision.RevisionID == "" {
		kind = ExternalReconcileFastForward
	} else if binding.LocalRevision.RevisionID != request.Revision.RevisionID {
		kind = ExternalReconcileConflict
	}
	if restricted && kind != ExternalReconcileUpToDate {
		kind = ExternalReconcileQuarantined
	}
	plan := ExternalReconcilePlan{
		PlanID:  stableExternalID("plan", binding.Binding.BindingID, string(request.Revision.RevisionID), string(binding.Binding.ProviderVersion), string(kind)),
		Binding: binding.Binding, Kind: kind, LocalRevision: request.Revision, ProviderVersion: binding.Binding.ProviderVersion,
		RequiresReview: kind != ExternalReconcileUpToDate, Restricted: restricted,
	}
	return success(plan)
}

func (a *ReferenceExternalStorage) Reconcile(ctx context.Context, request ExternalReconcileRequest) desktopcontract.Result[ExternalReconcileResult] {
	// Compatibility surface remains fail-closed: an unpreviewed resolution can
	// only report the current head and can never mutate provider content.
	a.mu.Lock()
	defer a.mu.Unlock()
	binding, ok := a.bindingForLocked(request.ConnectionID, request.DocumentID)
	if !ok || ctx.Err() != nil {
		return failed[ExternalReconcileResult](desktopcontract.FailureReconnect, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryReconnect)
	}
	return success(ExternalReconcileResult{ProviderVersion: binding.Binding.ProviderVersion, Converged: request.Resolution == "keep_current"})
}

func (a *ReferenceExternalStorage) ApplyReconcile(_ context.Context, plan ExternalReconcilePlan, resolution string) desktopcontract.Result[ExternalReconcileResult] {
	a.mu.Lock()
	defer a.mu.Unlock()
	stored, ok := a.state.Bindings[plan.Binding.BindingID]
	if !ok || stored.Binding.ProviderVersion != plan.ProviderVersion || plan.PlanID != stableExternalID("plan", stored.Binding.BindingID, string(plan.LocalRevision.RevisionID), string(plan.ProviderVersion), string(plan.Kind)) {
		return failed[ExternalReconcileResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryReview)
	}
	if plan.Restricted || plan.Kind == ExternalReconcileQuarantined || resolution == "" {
		return success(ExternalReconcileResult{ProviderVersion: stored.Binding.ProviderVersion, Converged: false})
	}
	if resolution != "accept_provider" && resolution != "publish_local" && resolution != "keep_current" {
		return failed[ExternalReconcileResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryReview)
	}
	stored.LocalRevision = plan.LocalRevision
	a.state.Bindings[stored.Binding.BindingID] = stored
	if err := a.saveLocked(); err != nil {
		return failed[ExternalReconcileResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryRetry)
	}
	return success(ExternalReconcileResult{ProviderVersion: stored.Binding.ProviderVersion, Converged: true})
}

func (a *ReferenceExternalStorage) load() error {
	info, statErr := os.Lstat(a.statePath)
	if statErr == nil && (!info.Mode().IsRegular() || !privatefs.PermissionsMatch(info, 0o600) || info.Size() > 4<<20) {
		return errors.New("external storage state requires recovery")
	}
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	data, err := os.ReadFile(a.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var state referenceExternalState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil || state.Version != referenceExternalStateVersion || state.Connections == nil || state.Bindings == nil {
		return errors.New("external storage state requires recovery")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("external storage state requires recovery")
	}
	for id, connection := range state.Connections {
		if id == "" || connection.ConnectionID != id || connection.ProviderID == "" || connection.AccountLabel == "" || connection.ScopeLabel == "" || !validExternalConnectionStatus(connection.Status) {
			return errors.New("external storage state requires recovery")
		}
	}
	for id, binding := range state.Bindings {
		if id == "" || binding.Binding.BindingID != id || binding.Binding.ConnectionID == "" || binding.Binding.DocumentID == "" || binding.Binding.RemoteItemID == "" || binding.Binding.ProviderVersion == "" || state.Connections[binding.Binding.ConnectionID].ConnectionID == "" {
			return errors.New("external storage state requires recovery")
		}
	}
	a.state = state
	return nil
}

func validExternalConnectionStatus(status ExternalConnectionStatus) bool {
	switch status {
	case ExternalConnectionConnected, ExternalConnectionExpired, ExternalConnectionRateLimited, ExternalConnectionDisconnected, ExternalConnectionReconnect:
		return true
	default:
		return false
	}
}

func (a *ReferenceExternalStorage) saveLocked() error {
	data, err := json.Marshal(a.state)
	if err != nil {
		return err
	}
	return writePrivateAtomic(a.statePath, append(data, '\n'))
}

func (a *ReferenceExternalStorage) bindingForLocked(connectionID string, documentID runtimeprotocol.DocumentID) (referenceBinding, bool) {
	for _, binding := range a.state.Bindings {
		if binding.Binding.ConnectionID == connectionID && binding.Binding.DocumentID == documentID {
			return binding, true
		}
	}
	return referenceBinding{}, false
}

func (a *ReferenceExternalStorage) connectionLiveLocked(connectionID string) bool {
	connection, ok := a.state.Connections[connectionID]
	return ok && connection.Status == ExternalConnectionConnected
}

func (connection referenceConnection) public() ExternalConnection {
	return ExternalConnection{ConnectionID: connection.ConnectionID, ProviderID: connection.ProviderID, AccountLabel: connection.AccountLabel, ScopeLabel: connection.ScopeLabel, Status: connection.Status, Capabilities: connection.Capabilities}
}

func success[T any](value T) desktopcontract.Result[T] {
	return desktopcontract.Result[T]{Outcome: protocolcommon.OutcomeSuccess, Value: value}
}

func stableExternalID(kind string, values ...string) string {
	hash := sha256.New()
	for _, value := range append([]string{kind}, values...) {
		_, _ = hash.Write([]byte{byte(len(value) >> 24), byte(len(value) >> 16), byte(len(value) >> 8), byte(len(value))})
		_, _ = hash.Write([]byte(value))
	}
	return kind + "_" + hex.EncodeToString(hash.Sum(nil))[:24]
}

func nextProviderVersion(current runtimeprotocol.ProviderVersionToken) runtimeprotocol.ProviderVersionToken {
	var number int
	_, _ = fmt.Sscanf(string(current), "v%d", &number)
	return runtimeprotocol.ProviderVersionToken(fmt.Sprintf("v%d", number+1))
}

func writePrivateAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".external-storage-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	remove := true
	defer func() {
		_ = temporary.Close()
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	remove = false
	return nil
}
