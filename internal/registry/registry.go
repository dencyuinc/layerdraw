// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package registry owns registry source selection, trust decisions, dependency
// resolution, and digest-bound transaction planning. It deliberately depends
// on ports for artifact retrieval, Engine package validation, Access, and
// Runtime publication so none of those semantics leak into a UI or shell.
package registry

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

const (
	FailureUnavailable            = "registry.unavailable"
	FailurePolicyDenied           = "registry.policy_denied"
	FailureSignatureMissing       = "registry.signature_missing"
	FailureSignatureInvalid       = "registry.signature_invalid"
	FailureSignatureRevoked       = "registry.signature_revoked"
	FailureDependencyConflict     = "registry.dependency_conflict"
	FailureDependencyCycle        = "registry.dependency_cycle"
	FailureArtifactCorrupt        = "registry.artifact_corrupt"
	FailureUnsupportedFormat      = "registry.unsupported_format"
	FailureIncompatibleCapability = "registry.incompatible_capability"
	FailurePlanStale              = "registry.plan_stale"
	FailureRepairRequired         = "registry.repair_required"
	FailureRollbackFailed         = "registry.rollback_failed"
)

type Failure struct {
	Code       string
	Subject    string
	Actionable bool
	Cause      error
}

func (f *Failure) Error() string { return f.Code + ": " + f.Subject }
func (f *Failure) Unwrap() error { return f.Cause }
func IsFailure(err error, code string) bool {
	var f *Failure
	return errors.As(err, &f) && f.Code == code
}
func fail(code, subject string, actionable bool, err error) error {
	return &Failure{Code: code, Subject: subject, Actionable: actionable, Cause: err}
}

type SourceKind string

const (
	SourceOfficial            SourceKind = "official"
	SourceOrganizationPrivate SourceKind = "organization_private"
	SourceSelfHosted          SourceKind = "self_hosted"
	SourceLocalDirectory      SourceKind = "local_directory"
	SourceGit                 SourceKind = "git"
)

type RegistrySource struct {
	SourceID          string     `json:"source_id"`
	Kind              SourceKind `json:"kind"`
	EndpointRef       string     `json:"endpoint_ref"`
	TrustPolicyID     string     `json:"trust_policy_id"`
	AuthConnectionRef string     `json:"auth_connection_ref,omitempty"`
	CachePolicy       string     `json:"cache_policy"`
	Priority          int        `json:"priority"`
	Connected         bool       `json:"connected"`
}

type ArtifactKind string

const (
	ArtifactPack     ArtifactKind = "pack"
	ArtifactTemplate ArtifactKind = "template"
)

type ArtifactIdentity struct {
	Kind        ArtifactKind `json:"kind"`
	CanonicalID string       `json:"canonical_id"`
	Version     string       `json:"version"`
}

type Dependency struct {
	CanonicalID      string `json:"canonical_id"`
	VersionRange     string `json:"version_range"`
	DigestConstraint string `json:"digest_constraint,omitempty"`
}

type SignatureEnvelope struct {
	Profile   string `json:"profile"`
	KeyID     string `json:"key_id"`
	Statement []byte `json:"statement"`
	Signature []byte `json:"signature"`
}

type ArtifactSignatureStatement struct {
	ArtifactIdentity         ArtifactIdentity `json:"artifact_identity"`
	ArtifactDigest           string           `json:"artifact_digest"`
	ManifestDigest           string           `json:"manifest_digest"`
	DependencyMetadataDigest string           `json:"dependency_metadata_digest"`
	PublisherID              string           `json:"publisher_id"`
	IssuedAt                 time.Time        `json:"issued_at"`
	ExpiresAt                *time.Time       `json:"expires_at,omitempty"`
}

type TrustStatus string

const (
	TrustVerified        TrustStatus = "verified"
	TrustUnsignedAllowed TrustStatus = "unsigned_allowed"
)

type TrustDecision struct {
	Status         TrustStatus `json:"status"`
	PolicyDigest   string      `json:"policy_digest"`
	EvidenceDigest string      `json:"evidence_digest"`
}

type ArtifactRelease struct {
	Identity                 ArtifactIdentity        `json:"identity"`
	SourceID                 string                  `json:"source_id"`
	PublisherID              string                  `json:"publisher_id"`
	Digest                   string                  `json:"digest"`
	ManifestDigest           string                  `json:"manifest_digest"`
	DependencyMetadataDigest string                  `json:"dependency_metadata_digest"`
	Size                     int64                   `json:"size"`
	Dependencies             []Dependency            `json:"dependencies"`
	Compatibility            []CompatibilityDecision `json:"compatibility"`
	Signature                *SignatureEnvelope      `json:"signature,omitempty"`
	License                  string                  `json:"license"`
	ProvenanceDigest         string                  `json:"provenance_digest"`
	Trust                    *TrustDecision          `json:"trust,omitempty"`
}

type CompatibilityDecision struct {
	Subject     string   `json:"subject"`
	Required    string   `json:"required"`
	Available   string   `json:"available"`
	Status      string   `json:"status"`
	Diagnostics []string `json:"diagnostics"`
}

type SearchInput struct {
	Query             string        `json:"query"`
	Kind              *ArtifactKind `json:"kind,omitempty"`
	IncludePrerelease bool          `json:"include_prerelease,omitempty"`
}
type SourceClient interface {
	Search(context.Context, RegistrySource, SearchInput) ([]ArtifactRelease, error)
	Download(context.Context, RegistrySource, ArtifactRelease) ([]byte, error)
}

type ValidatedArtifact struct {
	Identity                   ArtifactIdentity          `json:"identity"`
	CanonicalDigest            string                    `json:"canonical_digest"`
	StagedTreeManifest         string                    `json:"staged_tree_manifest"`
	ResolvedLockDigest         string                    `json:"resolved_lock_digest"`
	MutationDigest             string                    `json:"mutation_digest"`
	AuthoringImpactDigest      string                    `json:"authoring_impact_digest"`
	AuthoringImpact            *semantic.AuthoringImpact `json:"authoring_impact,omitempty"`
	AddressMigrationPlanDigest string                    `json:"address_migration_plan_digest"`
	Diagnostics                []string                  `json:"diagnostics"`
}

// PackageValidator is implemented by the Go Engine package facade. Registry
// never parses ZIP entries, LDL, resolved trees, or package manifests itself.
type PackageValidator interface {
	ValidateRegistryArtifact(context.Context, ArtifactRelease, []byte) (ValidatedArtifact, error)
}

type AuthorArtifactRequest struct {
	Kind        ArtifactKind `json:"kind"`
	ProjectID   string       `json:"project_id"`
	OutputName  string       `json:"output_name"`
	PublisherID string       `json:"publisher_id"`
	Version     string       `json:"version"`
}
type AuthoredArtifact struct {
	Release ArtifactRelease
	Bytes   []byte
}

// PackageAuthor is implemented by the Engine package facade. Registry does not
// construct container manifests, serialize LDL, or create archive entries.
type PackageAuthor interface {
	AuthorRegistryArtifact(context.Context, AuthorArtifactRequest) (AuthoredArtifact, error)
}

type TrustPolicy struct {
	PolicyID           string
	RequiredSignature  bool
	AllowUnsignedLocal bool
	TrustedPublishers  map[string]bool
	PublicKeys         map[string]ed25519.PublicKey
	RevokedKeys        map[string]bool
	ExpiresAt          *time.Time
}

type Action string

const (
	ActionInstall            Action = "install"
	ActionUpdate             Action = "update"
	ActionPin                Action = "pin"
	ActionRemove             Action = "remove"
	ActionRepair             Action = "repair"
	ActionCreateFromTemplate Action = "create_from_template"
)

type PlanRequest struct {
	Action                     Action                    `json:"action"`
	ProjectID                  string                    `json:"project_id"`
	BaseRevision               string                    `json:"base_revision"`
	ExpectedDefinitionHash     string                    `json:"expected_definition_hash"`
	ExpectedResolvedLockDigest string                    `json:"expected_resolved_lock_digest"`
	Requested                  ArtifactIdentity          `json:"requested"`
	IncludePrerelease          bool                      `json:"include_prerelease,omitempty"`
	DependencySnapshot         ProjectDependencySnapshot `json:"dependency_snapshot"`
	RequestedPin               bool                      `json:"requested_pin,omitempty"`
}

type LockedArtifact struct {
	Identity ArtifactIdentity `json:"identity"`
	SourceID string           `json:"source_id"`
	Digest   string           `json:"digest"`
	Pinned   bool             `json:"pinned"`
}
type ProjectDependencySnapshot struct {
	ResolvedLockDigest string           `json:"resolved_lock_digest"`
	Installs           []LockedArtifact `json:"installs"`
}
type ResolvedLockDelta struct {
	Added   []LockedArtifact `json:"added"`
	Updated []LockedArtifact `json:"updated"`
	Removed []LockedArtifact `json:"removed"`
	Pinned  []LockedArtifact `json:"pinned"`
}
type RollbackCheckpoint struct {
	BaseProjectRevision     string `json:"base_project_revision"`
	BaseDefinitionHash      string `json:"base_definition_hash"`
	BaseResolvedLockDigest  string `json:"base_resolved_lock_digest"`
	CurrentPackTreeManifest string `json:"current_pack_tree_manifest"`
}
type SourcePlanBinding struct {
	SourceID          string `json:"source_id"`
	SourceDigest      string `json:"source_digest"`
	TrustPolicyDigest string `json:"trust_policy_digest"`
}

type PlanArtifact struct {
	Release    ArtifactRelease   `json:"release"`
	Validation ValidatedArtifact `json:"validation"`
}

type InstallPlan struct {
	TransactionID              string                               `json:"transaction_id"`
	PlanDigest                 string                               `json:"plan_digest"`
	Action                     Action                               `json:"action"`
	ProjectID                  string                               `json:"project_id"`
	BaseRevision               string                               `json:"base_revision"`
	ExpectedDefinitionHash     string                               `json:"expected_definition_hash"`
	ExpectedResolvedLockDigest string                               `json:"expected_resolved_lock_digest"`
	Artifacts                  []PlanArtifact                       `json:"artifacts"`
	RequiredCapabilities       []string                             `json:"required_capabilities"`
	TrustPolicyDigests         []string                             `json:"trust_policy_digests"`
	SourceBindings             []SourcePlanBinding                  `json:"source_bindings"`
	DependencySnapshot         ProjectDependencySnapshot            `json:"dependency_snapshot"`
	ResolvedLockDelta          ResolvedLockDelta                    `json:"resolved_lock_delta"`
	RollbackCheckpoint         RollbackCheckpoint                   `json:"rollback_checkpoint"`
	ExpiresAt                  time.Time                            `json:"expires_at"`
	MigrationRequired          bool                                 `json:"migration_required"`
	CreatesNewDocument         bool                                 `json:"creates_new_document"`
	MutationDigest             string                               `json:"mutation_digest"`
	AuthoringImpactDigests     []string                             `json:"authoring_impact_digests"`
	HostOperationImpactDigest  string                               `json:"host_operation_impact_digest"`
	EvaluationDigest           string                               `json:"evaluation_digest"`
	AuthoringImpact            *semantic.AuthoringImpact            `json:"authoring_impact,omitempty"`
	HostOperationImpacts       []accessprotocol.HostOperationImpact `json:"host_operation_impacts"`
	AccessDecision             accessprotocol.AuthoringDecision     `json:"access_decision"`
	HostCapabilitiesDigest     string                               `json:"host_capabilities_digest"`
}

type RuntimeCommitInput struct {
	Plan                 InstallPlan                          `json:"plan"`
	OperationID          string                               `json:"operation_id"`
	IdempotencyKey       string                               `json:"idempotency_key"`
	AuthoringImpact      *semantic.AuthoringImpact            `json:"authoring_impact,omitempty"`
	HostOperationImpacts []accessprotocol.HostOperationImpact `json:"host_operation_impacts"`
	AccessDecision       accessprotocol.AuthoringDecision     `json:"access_decision"`
}
type RuntimeCommitResult struct {
	CommittedRevision string `json:"committed_revision"`
	OperationResultID string `json:"operation_result_id"`
}
type RuntimePort interface {
	CommitRegistryPlan(context.Context, RuntimeCommitInput) (RuntimeCommitResult, error)
}
type AccessPort interface {
	EvaluateRegistryPlan(context.Context, accessprotocol.EvaluateAuthoringInput) (accessprotocol.AuthoringDecision, error)
}

type ProjectState struct {
	ProjectID           string
	DocumentID          string
	LocalScopeID        string
	OrganizationScopeID *string
	Revision            string
	DefinitionHash      string
	DependencySnapshot  ProjectDependencySnapshot
	PackTreeManifest    string
	HostCapabilities    []string
	GrantSnapshot       accessprotocol.AuthoringGrantSnapshot
}
type ProjectStatePort interface {
	CurrentRegistryProjectState(context.Context, string) (ProjectState, error)
}

type CredentialLease struct {
	ConnectionRef string
	Credential    []byte
	ExpiresAt     time.Time
}
type CredentialBroker interface {
	ResolveRegistryConnection(context.Context, string) (CredentialLease, error)
}
type SourceConnector interface {
	ProbeRegistrySource(context.Context, RegistrySource, CredentialLease) error
}

type TransactionState string

const (
	StatePlanned              TransactionState = "planned"
	StateVerified             TransactionState = "verified"
	StateAwaitingConfirmation TransactionState = "awaiting_confirmation"
	StateApplying             TransactionState = "applying_project_change"
	StateCommitted            TransactionState = "committed"
	StateRolledBack           TransactionState = "rolled_back"
	StateRepairRequired       TransactionState = "repair_required"
	StateNeedsReview          TransactionState = "needs_review"
)

type TransactionEvent struct {
	State          TransactionState `json:"state"`
	EvidenceDigest string           `json:"evidence_digest"`
	Sequence       uint64           `json:"sequence"`
	IdempotencyKey string           `json:"idempotency_key,omitempty"`
}
type Transaction struct {
	Plan              InstallPlan        `json:"plan"`
	Events            []TransactionEvent `json:"events"`
	CommittedRevision string             `json:"committed_revision,omitempty"`
	OperationResultID string             `json:"operation_result_id,omitempty"`
}

type TransactionStore interface {
	CreateRegistryTransaction(context.Context, Transaction) error
	GetRegistryTransaction(context.Context, string) (Transaction, bool, error)
	CompareAndSwapRegistryTransaction(context.Context, string, uint64, Transaction) (bool, error)
}

type MemoryTransactionStore struct {
	mu           sync.RWMutex
	transactions map[string]Transaction
}

func NewMemoryTransactionStore() *MemoryTransactionStore {
	return &MemoryTransactionStore{transactions: map[string]Transaction{}}
}
func (s *MemoryTransactionStore) CreateRegistryTransaction(_ context.Context, tx Transaction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.transactions[tx.Plan.TransactionID]; exists {
		return errors.New("registry transaction already exists")
	}
	s.transactions[tx.Plan.TransactionID] = cloneTransaction(tx)
	return nil
}
func (s *MemoryTransactionStore) CompareAndSwapRegistryTransaction(_ context.Context, id string, expected uint64, next Transaction) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.transactions[id]
	if !ok {
		return false, nil
	}
	if transactionVersion(current) != expected {
		return false, nil
	}
	if err := validateTransactionAppend(current, next); err != nil {
		return false, err
	}
	s.transactions[id] = cloneTransaction(next)
	return true, nil
}
func (s *MemoryTransactionStore) GetRegistryTransaction(_ context.Context, id string) (Transaction, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tx, ok := s.transactions[id]
	return cloneTransaction(tx), ok, nil
}

type Registry struct {
	mu           sync.RWMutex
	sources      map[string]RegistrySource
	policies     map[string]TrustPolicy
	clients      map[SourceKind]SourceClient
	validator    PackageValidator
	author       PackageAuthor
	access       AccessPort
	runtime      RuntimePort
	projectState ProjectStatePort
	credentials  CredentialBroker
	connectors   map[SourceKind]SourceConnector
	transactions TransactionStore
	now          func() time.Time
}

func New(validator PackageValidator, access AccessPort, runtime RuntimePort, projectState ProjectStatePort, credentials CredentialBroker, transactions TransactionStore) (*Registry, error) {
	if validator == nil || access == nil || runtime == nil || projectState == nil || credentials == nil || transactions == nil {
		return nil, errors.New("registry requires Engine validator, Access, Runtime, project state, credential broker, and transaction store ports")
	}
	return &Registry{sources: map[string]RegistrySource{}, policies: map[string]TrustPolicy{}, clients: map[SourceKind]SourceClient{}, connectors: map[SourceKind]SourceConnector{}, validator: validator, access: access, runtime: runtime, projectState: projectState, credentials: credentials, transactions: transactions, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (r *Registry) RegisterClient(kind SourceKind, client SourceClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[kind] = client
}
func (r *Registry) RegisterConnector(kind SourceKind, connector SourceConnector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.connectors[kind] = connector
}
func (r *Registry) RegisterPackageAuthor(author PackageAuthor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.author = author
}
func (r *Registry) PutTrustPolicy(policy TrustPolicy) error {
	if policy.PolicyID == "" {
		return errors.New("trust policy id is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policies[policy.PolicyID] = clonePolicy(policy)
	return nil
}
func (r *Registry) ConfigureSource(source RegistrySource) error {
	if source.SourceID == "" || source.EndpointRef == "" || source.TrustPolicyID == "" {
		return errors.New("source identity, endpoint ref, and trust policy are required")
	}
	if strings.Contains(strings.ToLower(source.EndpointRef), "token=") || strings.Contains(strings.ToLower(source.EndpointRef), "password=") {
		return fail(FailurePolicyDenied, source.SourceID, true, errors.New("credential material is forbidden in source configuration"))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.policies[source.TrustPolicyID]; !ok {
		return fail(FailurePolicyDenied, source.TrustPolicyID, true, errors.New("unknown trust policy"))
	}
	r.sources[source.SourceID] = source
	return nil
}
func (r *Registry) ConnectSource(ctx context.Context, sourceID, connectionRef string) error {
	r.mu.RLock()
	source, ok := r.sources[sourceID]
	connector := r.connectors[source.Kind]
	r.mu.RUnlock()
	if !ok || connector == nil || connectionRef == "" {
		return fail(FailureUnavailable, sourceID, true, nil)
	}
	lease, err := r.credentials.ResolveRegistryConnection(ctx, connectionRef)
	if err != nil || lease.ConnectionRef != connectionRef || len(lease.Credential) == 0 || !lease.ExpiresAt.After(r.now()) {
		return fail(FailurePolicyDenied, sourceID, true, err)
	}
	if err := connector.ProbeRegistrySource(ctx, source, lease); err != nil {
		return fail(FailureUnavailable, sourceID, true, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	source = r.sources[sourceID]
	source.AuthConnectionRef = connectionRef
	source.Connected = true
	r.sources[sourceID] = source
	return nil
}
func (r *Registry) DisconnectSource(sourceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	source, ok := r.sources[sourceID]
	if !ok {
		return fail(FailureUnavailable, sourceID, true, nil)
	}
	source.Connected = false
	source.AuthConnectionRef = ""
	r.sources[sourceID] = source
	return nil
}
func (r *Registry) Sources() []RegistrySource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RegistrySource, 0, len(r.sources))
	for _, s := range r.sources {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].SourceID < out[j].SourceID
	})
	return out
}
func (r *Registry) getSource(id string) (RegistrySource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	source, ok := r.sources[id]
	return source, ok
}

func (r *Registry) Search(ctx context.Context, input SearchInput) ([]ArtifactRelease, error) {
	sources, clients := r.snapshotSources()
	var releases []ArtifactRelease
	for _, source := range sources {
		if !source.Connected {
			continue
		}
		client := clients[source.Kind]
		if client == nil {
			continue
		}
		found, err := client.Search(ctx, source, input)
		if err != nil {
			continue
		}
		for _, release := range found {
			if release.SourceID != source.SourceID || !validIdentity(release.Identity) || (input.Kind != nil && release.Identity.Kind != *input.Kind) {
				continue
			}
			_, policy, _, ok := r.sourceContext(source.SourceID)
			if !ok {
				continue
			}
			decision, err := verifyTrust(r.now(), source, policy, release)
			if err != nil {
				continue
			}
			release.Trust = &decision
			releases = append(releases, cloneRelease(release))
		}
	}
	if len(releases) == 0 {
		return nil, fail(FailureUnavailable, input.Query, true, nil)
	}
	sortReleases(releases, sources)
	return releases, nil
}

func (r *Registry) AuthorArtifact(ctx context.Context, request AuthorArtifactRequest) (AuthoredArtifact, error) {
	if (request.Kind != ArtifactPack && request.Kind != ArtifactTemplate) || request.ProjectID == "" || request.OutputName == "" || request.PublisherID == "" || request.Version == "" {
		return AuthoredArtifact{}, fail(FailureUnsupportedFormat, request.OutputName, true, nil)
	}
	wantSuffix := ".ldpack"
	if request.Kind == ArtifactTemplate {
		wantSuffix = ".layerdraw"
	}
	if !strings.HasSuffix(request.OutputName, wantSuffix) {
		return AuthoredArtifact{}, fail(FailureUnsupportedFormat, request.OutputName, true, nil)
	}
	r.mu.RLock()
	author := r.author
	r.mu.RUnlock()
	if author == nil {
		return AuthoredArtifact{}, fail(FailureUnavailable, "artifact_author", true, nil)
	}
	result, err := author.AuthorRegistryArtifact(ctx, request)
	if err != nil {
		return AuthoredArtifact{}, fail(FailureArtifactCorrupt, request.OutputName, true, err)
	}
	if result.Release.Identity.Kind != request.Kind || result.Release.Identity.Version != request.Version || result.Release.PublisherID != request.PublisherID || result.Release.Digest != digestBytes(result.Bytes) || result.Release.Size != int64(len(result.Bytes)) {
		return AuthoredArtifact{}, fail(FailureArtifactCorrupt, request.OutputName, true, nil)
	}
	validated, err := r.validator.ValidateRegistryArtifact(ctx, result.Release, result.Bytes)
	if err != nil || validated.Identity != result.Release.Identity || validated.CanonicalDigest != result.Release.Digest {
		return AuthoredArtifact{}, fail(FailureArtifactCorrupt, request.OutputName, true, err)
	}
	result.Bytes = append([]byte{}, result.Bytes...)
	result.Release = cloneRelease(result.Release)
	return result, nil
}

func (r *Registry) Plan(ctx context.Context, request PlanRequest) (InstallPlan, error) {
	if !validAction(request.Action) || !validIdentity(request.Requested) || request.ProjectID == "" || request.BaseRevision == "" {
		return InstallPlan{}, fail(FailureDependencyConflict, request.Requested.CanonicalID, true, nil)
	}
	if request.Action == ActionCreateFromTemplate && request.Requested.Kind != ArtifactTemplate {
		return InstallPlan{}, fail(FailureUnsupportedFormat, request.Requested.CanonicalID, true, nil)
	}
	state, err := r.projectState.CurrentRegistryProjectState(ctx, request.ProjectID)
	if err != nil {
		return InstallPlan{}, fail(FailureUnavailable, request.ProjectID, true, err)
	}
	if state.ProjectID != request.ProjectID || state.Revision != request.BaseRevision || state.DefinitionHash != request.ExpectedDefinitionHash || state.DependencySnapshot.ResolvedLockDigest != request.ExpectedResolvedLockDigest {
		return InstallPlan{}, fail(FailurePlanStale, request.ProjectID, true, nil)
	}
	installed, hasInstalled := findLocked(state.DependencySnapshot, request.Requested.CanonicalID)
	switch request.Action {
	case ActionInstall:
		if hasInstalled {
			return InstallPlan{}, fail(FailureDependencyConflict, request.Requested.CanonicalID, true, nil)
		}
	case ActionUpdate, ActionPin, ActionRemove, ActionRepair:
		if !hasInstalled {
			return InstallPlan{}, fail(FailureDependencyConflict, request.Requested.CanonicalID, true, nil)
		}
	}
	if request.Action == ActionPin && request.Requested.Version == "latest" {
		return InstallPlan{}, fail(FailureDependencyConflict, request.Requested.CanonicalID, true, nil)
	}
	if request.Action == ActionRepair {
		request.Requested = installed.Identity
	}
	resolved := map[string]PlanArtifact{}
	if request.Action != ActionRemove {
		visiting := map[string]bool{}
		if err := r.resolve(ctx, request.Requested, request.IncludePrerelease, resolved, visiting); err != nil {
			return InstallPlan{}, err
		}
		if request.Action == ActionRepair {
			root := resolved[string(request.Requested.Kind)+":"+request.Requested.CanonicalID]
			if root.Release.Digest != installed.Digest || root.Release.SourceID != installed.SourceID {
				return InstallPlan{}, fail(FailureRepairRequired, request.Requested.CanonicalID, true, errors.New("repair requires the exact locked source and digest"))
			}
		}
	}
	keys := make([]string, 0, len(resolved))
	for key := range resolved {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	artifacts := make([]PlanArtifact, 0, len(keys))
	impacts := []string{}
	trust := []string{}
	bindings := []SourcePlanBinding{}
	var authoringImpact *semantic.AuthoringImpact
	migrationRequired := false
	for _, key := range keys {
		item := resolved[key]
		artifacts = append(artifacts, item)
		impacts = append(impacts, item.Validation.AuthoringImpactDigest)
		source, policy, _, ok := r.sourceContext(item.Release.SourceID)
		if !ok {
			return InstallPlan{}, fail(FailureUnavailable, item.Release.SourceID, true, nil)
		}
		trust = append(trust, digestJSON(struct {
			Source RegistrySource
			Policy string
		}{source, digestTrust(policy)}))
		bindings = append(bindings, SourcePlanBinding{SourceID: source.SourceID, SourceDigest: digestJSON(source), TrustPolicyDigest: digestTrust(policy)})
		if item.Release.Identity == request.Requested && item.Validation.AuthoringImpact != nil {
			impact := *item.Validation.AuthoringImpact
			authoringImpact = &impact
		}
		if item.Validation.AddressMigrationPlanDigest != "" {
			migrationRequired = true
		}
		for _, compatibility := range item.Release.Compatibility {
			if compatibility.Status == "migration_required" {
				migrationRequired = true
			}
			if strings.HasPrefix(compatibility.Subject, "capability:") && !containsString(state.HostCapabilities, strings.TrimPrefix(compatibility.Subject, "capability:")) {
				return InstallPlan{}, fail(FailureIncompatibleCapability, compatibility.Subject, true, nil)
			}
		}
	}
	if request.Action != ActionRemove && authoringImpact == nil {
		return InstallPlan{}, fail(FailureArtifactCorrupt, request.Requested.CanonicalID, true, errors.New("Engine validation omitted resulting AuthoringImpact"))
	}
	sort.Strings(impacts)
	sort.Strings(trust)
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].SourceID < bindings[j].SourceID })
	hostCapabilities := append([]string{}, state.HostCapabilities...)
	sort.Strings(hostCapabilities)
	delta := buildLockDelta(request.Action, state.DependencySnapshot, resolved, request.Requested)
	hostImpactDigest := protocolcommon.Digest(digestJSON(struct {
		Action    Action
		ProjectID string
		Delta     ResolvedLockDelta
	}{request.Action, request.ProjectID, delta}))
	hostImpact := accessprotocol.HostOperationImpact{Action: hostImpactAction(request.Action), ImpactDigest: hostImpactDigest, OperationKind: accessprotocol.HostOperationKindPackageTransaction, RequiredAuthoringCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityPackageManage}, ResourceRefs: []string{request.Requested.CanonicalID}, ResourceScope: accessprotocol.HostResourceScope{DocumentID: state.DocumentID, LocalScopeID: state.LocalScopeID, OrganizationScopeID: state.OrganizationScopeID}}
	mutation := digestJSON(artifacts)
	evaluate := accessprotocol.EvaluateAuthoringInput{AuthoringImpact: authoringImpact, GrantSnapshot: state.GrantSnapshot, HostOperationImpacts: []accessprotocol.HostOperationImpact{hostImpact}, RequestIntent: "registry." + string(request.Action)}
	decision, err := r.access.EvaluateRegistryPlan(ctx, evaluate)
	if err != nil || decision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow {
		return InstallPlan{}, fail(FailurePolicyDenied, request.ProjectID, true, err)
	}
	plan := InstallPlan{Action: request.Action, ProjectID: request.ProjectID, BaseRevision: state.Revision, ExpectedDefinitionHash: state.DefinitionHash, ExpectedResolvedLockDigest: state.DependencySnapshot.ResolvedLockDigest, Artifacts: artifacts, RequiredCapabilities: capabilitiesToStrings(decision.RequiredCapabilities), TrustPolicyDigests: trust, SourceBindings: bindings, DependencySnapshot: cloneDependencySnapshot(state.DependencySnapshot), ResolvedLockDelta: delta, RollbackCheckpoint: RollbackCheckpoint{BaseProjectRevision: state.Revision, BaseDefinitionHash: state.DefinitionHash, BaseResolvedLockDigest: state.DependencySnapshot.ResolvedLockDigest, CurrentPackTreeManifest: state.PackTreeManifest}, ExpiresAt: r.now().Add(15 * time.Minute), MigrationRequired: migrationRequired, CreatesNewDocument: request.Action == ActionCreateFromTemplate, MutationDigest: mutation, AuthoringImpactDigests: impacts, HostOperationImpactDigest: string(hostImpactDigest), EvaluationDigest: string(decision.EvaluationDigest), AuthoringImpact: authoringImpact, HostOperationImpacts: []accessprotocol.HostOperationImpact{hostImpact}, AccessDecision: decision, HostCapabilitiesDigest: digestJSON(hostCapabilities)}
	plan.TransactionID = strings.TrimPrefix(digestJSON(struct{ Project, Evaluation, Mutation string }{request.ProjectID, plan.EvaluationDigest, plan.MutationDigest}), "sha256:")[:32]
	plan.PlanDigest = digestPlan(plan)
	tx := Transaction{Plan: plan, Events: []TransactionEvent{{State: StatePlanned, EvidenceDigest: plan.PlanDigest, Sequence: 1}, {State: StateVerified, EvidenceDigest: mutation, Sequence: 2}, {State: StateAwaitingConfirmation, EvidenceDigest: plan.EvaluationDigest, Sequence: 3}}}
	if err := r.transactions.CreateRegistryTransaction(ctx, tx); err != nil {
		return InstallPlan{}, fail(FailureUnavailable, plan.TransactionID, true, err)
	}
	return clonePlan(plan), nil
}

func (r *Registry) resolve(ctx context.Context, identity ArtifactIdentity, prerelease bool, resolved map[string]PlanArtifact, visiting map[string]bool) error {
	key := string(identity.Kind) + ":" + identity.CanonicalID
	if visiting[key] {
		return fail(FailureDependencyCycle, identity.CanonicalID, true, nil)
	}
	if existing, ok := resolved[key]; ok {
		if existing.Release.Identity.Version != identity.Version && identity.Version != "latest" {
			return fail(FailureDependencyConflict, identity.CanonicalID, true, nil)
		}
		return nil
	}
	visiting[key] = true
	defer delete(visiting, key)
	kind := identity.Kind
	releases, err := r.Search(ctx, SearchInput{Query: identity.CanonicalID, Kind: &kind, IncludePrerelease: prerelease})
	if err != nil {
		return err
	}
	var selected *ArtifactRelease
	for i := range releases {
		candidate := releases[i]
		if candidate.Identity.CanonicalID != identity.CanonicalID {
			continue
		}
		if identity.Version != "latest" && candidate.Identity.Version != identity.Version {
			continue
		}
		if !prerelease && strings.Contains(candidate.Identity.Version, "-") {
			continue
		}
		selected = &candidate
		break
	}
	if selected == nil {
		return fail(FailureDependencyConflict, identity.CanonicalID, true, nil)
	}
	source, policy, client, ok := r.sourceContext(selected.SourceID)
	if !ok || !source.Connected || client == nil {
		return fail(FailureUnavailable, selected.SourceID, true, nil)
	}
	bytes, err := client.Download(ctx, source, *selected)
	if err != nil {
		return fail(FailureUnavailable, selected.SourceID, true, err)
	}
	if int64(len(bytes)) != selected.Size || digestBytes(bytes) != selected.Digest {
		return fail(FailureArtifactCorrupt, identity.CanonicalID, true, nil)
	}
	if selected.Identity.Kind != identity.Kind {
		return fail(FailureUnsupportedFormat, identity.CanonicalID, true, nil)
	}
	if _, err := verifyTrust(r.now(), source, policy, *selected); err != nil {
		return err
	}
	validated, err := r.validator.ValidateRegistryArtifact(ctx, *selected, bytes)
	if err != nil {
		return fail(FailureArtifactCorrupt, identity.CanonicalID, true, err)
	}
	if validated.Identity != selected.Identity || validated.CanonicalDigest != selected.Digest {
		return fail(FailureArtifactCorrupt, identity.CanonicalID, true, nil)
	}
	for _, decision := range selected.Compatibility {
		if decision.Status == "incompatible" || decision.Status == "disabled" {
			return fail(FailureIncompatibleCapability, decision.Subject, true, nil)
		}
	}
	resolved[key] = PlanArtifact{Release: cloneRelease(*selected), Validation: validated}
	dependencies := append([]Dependency{}, selected.Dependencies...)
	sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].CanonicalID < dependencies[j].CanonicalID })
	for _, dependency := range dependencies {
		version, err := r.resolveVersion(ctx, dependency, prerelease)
		if err != nil {
			return err
		}
		if err := r.resolve(ctx, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: dependency.CanonicalID, Version: version}, prerelease, resolved, visiting); err != nil {
			return err
		}
		child := resolved[string(ArtifactPack)+":"+dependency.CanonicalID]
		if dependency.DigestConstraint != "" && child.Release.Digest != dependency.DigestConstraint {
			return fail(FailureDependencyConflict, dependency.CanonicalID, true, nil)
		}
	}
	return nil
}

func (r *Registry) resolveVersion(ctx context.Context, dependency Dependency, prerelease bool) (string, error) {
	kind := ArtifactPack
	releases, err := r.Search(ctx, SearchInput{Query: dependency.CanonicalID, Kind: &kind, IncludePrerelease: prerelease})
	if err != nil {
		return "", err
	}
	for _, release := range releases {
		if release.Identity.CanonicalID == dependency.CanonicalID && matchesRange(release.Identity.Version, dependency.VersionRange, prerelease) {
			return release.Identity.Version, nil
		}
	}
	return "", fail(FailureDependencyConflict, dependency.CanonicalID, true, nil)
}

func (r *Registry) Commit(ctx context.Context, input RuntimeCommitInput) (RuntimeCommitResult, error) {
	tx, ok, loadErr := r.transactions.GetRegistryTransaction(ctx, input.Plan.TransactionID)
	if loadErr != nil || !ok || tx.Plan.PlanDigest != input.Plan.PlanDigest || digestPlan(input.Plan) != input.Plan.PlanDigest {
		return RuntimeCommitResult{}, fail(FailurePlanStale, input.Plan.TransactionID, true, nil)
	}
	if !tx.Plan.ExpiresAt.After(r.now()) {
		return RuntimeCommitResult{}, fail(FailurePlanStale, input.Plan.TransactionID, true, nil)
	}
	state, err := r.projectState.CurrentRegistryProjectState(ctx, tx.Plan.ProjectID)
	if err != nil || state.Revision != tx.Plan.BaseRevision || state.DefinitionHash != tx.Plan.ExpectedDefinitionHash || state.DependencySnapshot.ResolvedLockDigest != tx.Plan.ExpectedResolvedLockDigest {
		return RuntimeCommitResult{}, fail(FailurePlanStale, input.Plan.TransactionID, true, err)
	}
	hostCapabilities := append([]string{}, state.HostCapabilities...)
	sort.Strings(hostCapabilities)
	if digestJSON(hostCapabilities) != tx.Plan.HostCapabilitiesDigest {
		return RuntimeCommitResult{}, fail(FailurePlanStale, "host_capabilities", true, nil)
	}
	for _, binding := range tx.Plan.SourceBindings {
		source, policy, _, ok := r.sourceContext(binding.SourceID)
		if !ok || digestJSON(source) != binding.SourceDigest || digestTrust(policy) != binding.TrustPolicyDigest {
			return RuntimeCommitResult{}, fail(FailurePlanStale, binding.SourceID, true, nil)
		}
		for _, artifact := range tx.Plan.Artifacts {
			if artifact.Release.SourceID == binding.SourceID {
				if _, err := verifyTrust(r.now(), source, policy, artifact.Release); err != nil {
					return RuntimeCommitResult{}, fail(FailurePlanStale, binding.SourceID, true, err)
				}
			}
		}
	}
	evaluate := accessprotocol.EvaluateAuthoringInput{AuthoringImpact: tx.Plan.AuthoringImpact, GrantSnapshot: state.GrantSnapshot, HostOperationImpacts: append([]accessprotocol.HostOperationImpact{}, tx.Plan.HostOperationImpacts...), RequestIntent: "registry." + string(tx.Plan.Action)}
	decision, err := r.access.EvaluateRegistryPlan(ctx, evaluate)
	if err != nil || decision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow || string(decision.EvaluationDigest) != tx.Plan.EvaluationDigest {
		return RuntimeCommitResult{}, fail(FailurePlanStale, "authoring_policy", true, err)
	}
	stateName := transactionState(tx)
	if stateName == StateCommitted {
		if lastIdempotencyKey(tx) == input.IdempotencyKey {
			return RuntimeCommitResult{CommittedRevision: tx.CommittedRevision, OperationResultID: tx.OperationResultID}, nil
		}
		return RuntimeCommitResult{}, fail(FailurePlanStale, input.Plan.TransactionID, true, nil)
	}
	if stateName == StateApplying {
		return RuntimeCommitResult{}, fail(FailureRepairRequired, input.Plan.TransactionID, true, errors.New("publication outcome requires recovery; refusing duplicate publish"))
	}
	if stateName != StateAwaitingConfirmation {
		return RuntimeCommitResult{}, fail(FailurePlanStale, input.Plan.TransactionID, true, nil)
	}
	expectedVersion := transactionVersion(tx)
	tx.Events = append(tx.Events, TransactionEvent{State: StateApplying, EvidenceDigest: tx.Plan.EvaluationDigest, Sequence: expectedVersion + 1, IdempotencyKey: input.IdempotencyKey})
	swapped, storeErr := r.transactions.CompareAndSwapRegistryTransaction(ctx, tx.Plan.TransactionID, expectedVersion, tx)
	if storeErr != nil {
		return RuntimeCommitResult{}, fail(FailureUnavailable, input.Plan.TransactionID, true, storeErr)
	}
	if !swapped {
		latest, ok, err := r.transactions.GetRegistryTransaction(ctx, tx.Plan.TransactionID)
		if err != nil || !ok {
			return RuntimeCommitResult{}, fail(FailureUnavailable, input.Plan.TransactionID, true, err)
		}
		if transactionState(latest) == StateCommitted && lastIdempotencyKey(latest) == input.IdempotencyKey {
			return RuntimeCommitResult{CommittedRevision: latest.CommittedRevision, OperationResultID: latest.OperationResultID}, nil
		}
		return RuntimeCommitResult{}, fail(FailureRepairRequired, input.Plan.TransactionID, true, errors.New("concurrent publication already started"))
	}
	input.AuthoringImpact = tx.Plan.AuthoringImpact
	input.HostOperationImpacts = append([]accessprotocol.HostOperationImpact{}, tx.Plan.HostOperationImpacts...)
	input.AccessDecision = decision
	result, err := r.runtime.CommitRegistryPlan(ctx, input)
	if err != nil {
		nextState := StateRolledBack
		code := FailureUnavailable
		var publication *RuntimePublicationError
		if errors.As(err, &publication) && publication.Published {
			nextState = StateRepairRequired
			code = FailureRepairRequired
		}
		version := transactionVersion(tx)
		tx.Events = append(tx.Events, TransactionEvent{State: nextState, EvidenceDigest: tx.Plan.MutationDigest, Sequence: version + 1, IdempotencyKey: input.IdempotencyKey})
		if ok, storeErr := r.transactions.CompareAndSwapRegistryTransaction(ctx, tx.Plan.TransactionID, version, tx); storeErr != nil || !ok {
			return RuntimeCommitResult{}, fail(FailureRepairRequired, input.Plan.TransactionID, true, storeErr)
		}
		return RuntimeCommitResult{}, fail(code, input.Plan.TransactionID, true, err)
	}
	tx.CommittedRevision = result.CommittedRevision
	tx.OperationResultID = result.OperationResultID
	version := transactionVersion(tx)
	tx.Events = append(tx.Events, TransactionEvent{State: StateCommitted, EvidenceDigest: digestJSON(result), Sequence: version + 1, IdempotencyKey: input.IdempotencyKey})
	if ok, storeErr := r.transactions.CompareAndSwapRegistryTransaction(ctx, tx.Plan.TransactionID, version, tx); storeErr != nil || !ok {
		return RuntimeCommitResult{}, fail(FailureRepairRequired, input.Plan.TransactionID, true, storeErr)
	}
	return result, nil
}

type RuntimePublicationError struct {
	Published bool
	Cause     error
}

func (e *RuntimePublicationError) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "registry runtime publication failed"
}
func (e *RuntimePublicationError) Unwrap() error { return e.Cause }

func (r *Registry) Transaction(id string) (Transaction, bool) {
	tx, ok, err := r.transactions.GetRegistryTransaction(context.Background(), id)
	return tx, ok && err == nil
}
func (r *Registry) GetTransaction(ctx context.Context, id string) (Transaction, error) {
	tx, ok, err := r.transactions.GetRegistryTransaction(ctx, id)
	if err != nil {
		return Transaction{}, fail(FailureUnavailable, id, true, err)
	}
	if !ok {
		return Transaction{}, fail(FailureUnavailable, id, true, nil)
	}
	return tx, nil
}
func (r *Registry) RecoverTransaction(ctx context.Context, id string) (Transaction, error) {
	tx, err := r.GetTransaction(ctx, id)
	if err != nil {
		return Transaction{}, err
	}
	if transactionState(tx) != StateApplying {
		return tx, nil
	}
	version := transactionVersion(tx)
	tx.Events = append(tx.Events, TransactionEvent{State: StateRepairRequired, EvidenceDigest: tx.Plan.MutationDigest, Sequence: version + 1, IdempotencyKey: lastIdempotencyKey(tx)})
	ok, err := r.transactions.CompareAndSwapRegistryTransaction(ctx, id, version, tx)
	if err != nil {
		return Transaction{}, fail(FailureUnavailable, id, true, err)
	}
	if !ok {
		return r.GetTransaction(ctx, id)
	}
	return tx, nil
}

func (r *Registry) sourceContext(id string) (RegistrySource, TrustPolicy, SourceClient, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	source, ok := r.sources[id]
	if !ok {
		return RegistrySource{}, TrustPolicy{}, nil, false
	}
	policy, ok := r.policies[source.TrustPolicyID]
	return source, clonePolicy(policy), r.clients[source.Kind], ok
}
func (r *Registry) snapshotSources() ([]RegistrySource, map[SourceKind]SourceClient) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sources := make([]RegistrySource, 0, len(r.sources))
	for _, s := range r.sources {
		sources = append(sources, s)
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].Priority != sources[j].Priority {
			return sources[i].Priority > sources[j].Priority
		}
		return sources[i].SourceID < sources[j].SourceID
	})
	clients := map[SourceKind]SourceClient{}
	for k, v := range r.clients {
		clients[k] = v
	}
	return sources, clients
}

func verifyTrust(now time.Time, source RegistrySource, policy TrustPolicy, release ArtifactRelease) (TrustDecision, error) {
	policyDigest := digestTrust(policy)
	if policy.ExpiresAt != nil && !policy.ExpiresAt.After(now) {
		return TrustDecision{}, fail(FailurePolicyDenied, policy.PolicyID, true, nil)
	}
	if policy.TrustedPublishers != nil && !policy.TrustedPublishers[release.PublisherID] {
		return TrustDecision{}, fail(FailurePolicyDenied, release.PublisherID, true, nil)
	}
	if release.Signature == nil {
		if source.Kind == SourceLocalDirectory && policy.AllowUnsignedLocal {
			return TrustDecision{Status: TrustUnsignedAllowed, PolicyDigest: policyDigest, EvidenceDigest: digestJSON(release.Identity)}, nil
		}
		if policy.RequiredSignature {
			return TrustDecision{}, fail(FailureSignatureMissing, release.Identity.CanonicalID, true, nil)
		}
		return TrustDecision{Status: TrustUnsignedAllowed, PolicyDigest: policyDigest, EvidenceDigest: digestJSON(release.Identity)}, nil
	}
	envelope := release.Signature
	if policy.RevokedKeys[envelope.KeyID] {
		return TrustDecision{}, fail(FailureSignatureRevoked, envelope.KeyID, true, nil)
	}
	key := policy.PublicKeys[envelope.KeyID]
	if len(key) != ed25519.PublicKeySize || envelope.Profile != "ed25519" || !ed25519.Verify(key, envelope.Statement, envelope.Signature) {
		return TrustDecision{}, fail(FailureSignatureInvalid, release.Identity.CanonicalID, true, nil)
	}
	var statement ArtifactSignatureStatement
	if json.Unmarshal(envelope.Statement, &statement) != nil || statement.ArtifactIdentity != release.Identity || statement.ArtifactDigest != release.Digest || statement.ManifestDigest != release.ManifestDigest || statement.DependencyMetadataDigest != release.DependencyMetadataDigest || statement.PublisherID != release.PublisherID || statement.IssuedAt.After(now) || (statement.ExpiresAt != nil && !statement.ExpiresAt.After(now)) {
		return TrustDecision{}, fail(FailureSignatureInvalid, release.Identity.CanonicalID, true, nil)
	}
	return TrustDecision{Status: TrustVerified, PolicyDigest: policyDigest, EvidenceDigest: digestBytes(envelope.Statement)}, nil
}

type semanticVersion struct {
	major, minor, patch int
	prerelease          string
}

func parseVersion(value string) (semanticVersion, bool) {
	main, pre, _ := strings.Cut(value, "-")
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return semanticVersion{}, false
	}
	nums := make([]int, 3)
	for i, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return semanticVersion{}, false
		}
		n, e := strconv.Atoi(p)
		if e != nil || n < 0 {
			return semanticVersion{}, false
		}
		nums[i] = n
	}
	return semanticVersion{nums[0], nums[1], nums[2], pre}, true
}
func compareVersions(a, b string) int {
	av, aok := parseVersion(a)
	bv, bok := parseVersion(b)
	if !aok || !bok {
		return strings.Compare(a, b)
	}
	for _, pair := range [][2]int{{av.major, bv.major}, {av.minor, bv.minor}, {av.patch, bv.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if av.prerelease == bv.prerelease {
		return 0
	}
	if av.prerelease == "" {
		return 1
	}
	if bv.prerelease == "" {
		return -1
	}
	return strings.Compare(av.prerelease, bv.prerelease)
}
func matchesRange(version, expr string, prerelease bool) bool {
	v, ok := parseVersion(version)
	if !ok || (!prerelease && v.prerelease != "") {
		return false
	}
	expr = strings.TrimSpace(expr)
	if expr == "" || expr == "*" {
		return true
	}
	if strings.HasPrefix(expr, "^") {
		baseText := strings.TrimPrefix(expr, "^")
		base, ok := parseVersion(baseText)
		if !ok || compareVersions(version, baseText) < 0 {
			return false
		}
		if base.major > 0 {
			return v.major == base.major
		}
		if base.minor > 0 {
			return v.major == 0 && v.minor == base.minor
		}
		return v.major == 0 && v.minor == 0 && v.patch == base.patch
	}
	if strings.HasPrefix(expr, "~") {
		base, ok := parseVersion(strings.TrimPrefix(expr, "~"))
		return ok && compareVersions(version, strings.TrimPrefix(expr, "~")) >= 0 && v.major == base.major && v.minor == base.minor
	}
	if strings.HasPrefix(expr, ">=") {
		return compareVersions(version, strings.TrimSpace(strings.TrimPrefix(expr, ">="))) >= 0
	}
	return version == expr
}
func validIdentity(value ArtifactIdentity) bool {
	return (value.Kind == ArtifactPack || value.Kind == ArtifactTemplate) && value.CanonicalID != "" && value.Version != ""
}
func validAction(action Action) bool {
	switch action {
	case ActionInstall, ActionUpdate, ActionPin, ActionRemove, ActionRepair, ActionCreateFromTemplate:
		return true
	default:
		return false
	}
}
func findLocked(snapshot ProjectDependencySnapshot, canonicalID string) (LockedArtifact, bool) {
	for _, item := range snapshot.Installs {
		if item.Identity.CanonicalID == canonicalID {
			return item, true
		}
	}
	return LockedArtifact{}, false
}
func lockedFromPlan(item PlanArtifact, pinned bool) LockedArtifact {
	return LockedArtifact{Identity: item.Release.Identity, SourceID: item.Release.SourceID, Digest: item.Release.Digest, Pinned: pinned}
}
func buildLockDelta(action Action, snapshot ProjectDependencySnapshot, resolved map[string]PlanArtifact, requested ArtifactIdentity) ResolvedLockDelta {
	delta := ResolvedLockDelta{Added: []LockedArtifact{}, Updated: []LockedArtifact{}, Removed: []LockedArtifact{}, Pinned: []LockedArtifact{}}
	if action == ActionRemove {
		if installed, ok := findLocked(snapshot, requested.CanonicalID); ok {
			delta.Removed = append(delta.Removed, installed)
		}
		return delta
	}
	keys := make([]string, 0, len(resolved))
	for key := range resolved {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		item := resolved[key]
		lock := lockedFromPlan(item, action == ActionPin && item.Release.Identity.CanonicalID == requested.CanonicalID)
		if prior, ok := findLocked(snapshot, item.Release.Identity.CanonicalID); ok {
			if action == ActionPin {
				delta.Pinned = append(delta.Pinned, lock)
			} else if prior.Digest != lock.Digest || prior.Identity.Version != lock.Identity.Version {
				delta.Updated = append(delta.Updated, lock)
			}
		} else {
			delta.Added = append(delta.Added, lock)
		}
	}
	return delta
}
func hostImpactAction(action Action) string {
	switch action {
	case ActionRemove:
		return "delete"
	case ActionInstall, ActionCreateFromTemplate:
		return "create"
	default:
		return "update"
	}
}
func capabilitiesToStrings(values []semantic.AuthoringCapability) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = string(value)
	}
	return out
}
func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func cloneDependencySnapshot(value ProjectDependencySnapshot) ProjectDependencySnapshot {
	value.Installs = append([]LockedArtifact{}, value.Installs...)
	return value
}
func sortReleases(releases []ArtifactRelease, sources []RegistrySource) {
	priority := map[string]int{}
	for _, source := range sources {
		priority[source.SourceID] = source.Priority
	}
	sort.SliceStable(releases, func(i, j int) bool {
		if releases[i].Identity.CanonicalID != releases[j].Identity.CanonicalID {
			return releases[i].Identity.CanonicalID < releases[j].Identity.CanonicalID
		}
		if c := compareVersions(releases[i].Identity.Version, releases[j].Identity.Version); c != 0 {
			return c > 0
		}
		if priority[releases[i].SourceID] != priority[releases[j].SourceID] {
			return priority[releases[i].SourceID] > priority[releases[j].SourceID]
		}
		return releases[i].SourceID < releases[j].SourceID
	})
}
func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func digestJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("registry canonical value: %v", err))
	}
	return digestBytes(data)
}
func digestPlan(plan InstallPlan) string { plan.PlanDigest = ""; return digestJSON(plan) }
func digestTrust(policy TrustPolicy) string {
	keys := make([]string, 0, len(policy.PublicKeys))
	for key, value := range policy.PublicKeys {
		keys = append(keys, key+":"+hex.EncodeToString(value))
	}
	sort.Strings(keys)
	publishers := make([]string, 0, len(policy.TrustedPublishers))
	for publisher, allowed := range policy.TrustedPublishers {
		if allowed {
			publishers = append(publishers, publisher)
		}
	}
	sort.Strings(publishers)
	revoked := make([]string, 0, len(policy.RevokedKeys))
	for key, value := range policy.RevokedKeys {
		if value {
			revoked = append(revoked, key)
		}
	}
	sort.Strings(revoked)
	var expires string
	if policy.ExpiresAt != nil {
		expires = policy.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	return digestJSON(struct {
		ID                        string
		Required, Unsigned        bool
		Keys, Publishers, Revoked []string
		Expires                   string
	}{policy.PolicyID, policy.RequiredSignature, policy.AllowUnsignedLocal, keys, publishers, revoked, expires})
}
func clonePolicy(p TrustPolicy) TrustPolicy {
	out := p
	if p.TrustedPublishers != nil {
		out.TrustedPublishers = map[string]bool{}
		for k, v := range p.TrustedPublishers {
			out.TrustedPublishers[k] = v
		}
	}
	if p.RevokedKeys != nil {
		out.RevokedKeys = map[string]bool{}
		for k, v := range p.RevokedKeys {
			out.RevokedKeys[k] = v
		}
	}
	if p.PublicKeys != nil {
		out.PublicKeys = map[string]ed25519.PublicKey{}
		for k, v := range p.PublicKeys {
			out.PublicKeys[k] = append(ed25519.PublicKey{}, v...)
		}
	}
	return out
}
func cloneRelease(value ArtifactRelease) ArtifactRelease {
	out := value
	out.Dependencies = append([]Dependency{}, value.Dependencies...)
	out.Compatibility = append([]CompatibilityDecision{}, value.Compatibility...)
	if value.Signature != nil {
		s := *value.Signature
		s.Statement = append([]byte{}, s.Statement...)
		s.Signature = append([]byte{}, s.Signature...)
		out.Signature = &s
	}
	return out
}
func clonePlan(value InstallPlan) InstallPlan {
	out := value
	out.Artifacts = append([]PlanArtifact{}, value.Artifacts...)
	out.RequiredCapabilities = append([]string{}, value.RequiredCapabilities...)
	out.TrustPolicyDigests = append([]string{}, value.TrustPolicyDigests...)
	out.AuthoringImpactDigests = append([]string{}, value.AuthoringImpactDigests...)
	return out
}
func cloneTransaction(value Transaction) Transaction {
	value.Plan = clonePlan(value.Plan)
	value.Events = append([]TransactionEvent{}, value.Events...)
	return value
}
func transactionVersion(value Transaction) uint64 {
	if len(value.Events) == 0 {
		return 0
	}
	return value.Events[len(value.Events)-1].Sequence
}
func transactionState(value Transaction) TransactionState {
	if len(value.Events) == 0 {
		return ""
	}
	return value.Events[len(value.Events)-1].State
}
func lastIdempotencyKey(value Transaction) string {
	for i := len(value.Events) - 1; i >= 0; i-- {
		if value.Events[i].IdempotencyKey != "" {
			return value.Events[i].IdempotencyKey
		}
	}
	return ""
}
func validateTransactionAppend(current, next Transaction) error {
	if current.Plan.TransactionID == "" || next.Plan.TransactionID != current.Plan.TransactionID || next.Plan.PlanDigest != current.Plan.PlanDigest || len(next.Events) != len(current.Events)+1 {
		return errors.New("registry transaction CAS must append exactly one event")
	}
	if transactionVersion(next) != transactionVersion(current)+1 {
		return errors.New("registry transaction sequence mismatch")
	}
	from, to := transactionState(current), transactionState(next)
	allowed := false
	switch from {
	case StateAwaitingConfirmation:
		allowed = to == StateApplying
	case StateApplying:
		allowed = to == StateCommitted || to == StateRolledBack || to == StateRepairRequired
	case StateRepairRequired:
		allowed = to == StateCommitted || to == StateNeedsReview
	}
	if !allowed {
		return fmt.Errorf("invalid registry transaction transition %s -> %s", from, to)
	}
	return nil
}
