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
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
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
	Revision          uint64     `json:"revision"`
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
	Kind             ArtifactKind `json:"kind"`
	CanonicalID      string       `json:"canonical_id"`
	VersionRange     string       `json:"version_range"`
	DigestConstraint string       `json:"digest_constraint,omitempty"`
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
}
type StreamingSourceClient interface {
	OpenArtifact(context.Context, RegistrySource, ArtifactRelease) (io.ReadCloser, error)
}
type ByteSourceClient interface {
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
	StagedObjects              []StagedObjectRef         `json:"staged_objects"`
}

type StagedObjectRef struct {
	ObjectID  string `json:"object_id"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"media_type"`
}

// PackageValidator is implemented by the Go Engine package facade. Registry
// never parses ZIP entries, LDL, resolved trees, or package manifests itself.
type PackageValidator interface {
	ValidateRegistryArtifact(context.Context, ArtifactRelease, []byte) (ValidatedArtifact, error)
	BuildRegistryMutationPlan(context.Context, RegistryMutationBuildInput) (ProjectMutationPlan, error)
}

type SourceEdit struct {
	Path         string `json:"path"`
	BeforeDigest string `json:"before_digest,omitempty"`
	AfterDigest  string `json:"after_digest"`
}
type StateMigrationProposal struct {
	ProposalDigest   string   `json:"proposal_digest"`
	AffectedSubjects []string `json:"affected_subjects"`
}
type ProjectMutationPlan struct {
	RegistryTransactionID      string                   `json:"registry_transaction_id"`
	PlanDigest                 string                   `json:"plan_digest"`
	BaseProjectRevision        string                   `json:"base_project_revision,omitempty"`
	ExpectedDefinitionHash     string                   `json:"expected_definition_hash,omitempty"`
	ExpectedResolvedLockDigest string                   `json:"expected_resolved_lock_digest,omitempty"`
	StagedTreeManifest         string                   `json:"staged_tree_manifest"`
	ResolvedLockDelta          ResolvedLockDelta        `json:"resolved_lock_delta"`
	SourceEdits                []SourceEdit             `json:"source_edits"`
	AddressMigrationPlanDigest string                   `json:"address_migration_plan_digest,omitempty"`
	StateMigrationProposal     *StateMigrationProposal  `json:"state_migration_proposal,omitempty"`
	TrustPolicyDigest          string                   `json:"trust_policy_digest"`
	MutationDigest             string                   `json:"mutation_digest"`
	AuthoringImpact            semantic.AuthoringImpact `json:"authoring_impact"`
	AuthoringImpactDigest      string                   `json:"authoring_impact_digest"`
	HostOperationImpactDigest  string                   `json:"host_operation_impact_digest"`
	EvaluationDigest           string                   `json:"evaluation_digest"`
	StagedObjects              []StagedObjectRef        `json:"staged_objects"`
}
type RegistryMutationBuildInput struct {
	Action             Action                    `json:"action"`
	Project            ProjectState              `json:"project"`
	Artifacts          []PlanArtifact            `json:"artifacts"`
	DependencySnapshot ProjectDependencySnapshot `json:"dependency_snapshot"`
	ResolvedLockDelta  ResolvedLockDelta         `json:"resolved_lock_delta"`
	Requested          ArtifactIdentity          `json:"requested"`
	NewDocumentID      string                    `json:"new_document_id,omitempty"`
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
	Identity                 ArtifactIdentity   `json:"identity"`
	SourceID                 string             `json:"source_id"`
	PublisherID              string             `json:"publisher_id"`
	Digest                   string             `json:"digest"`
	ProvenanceDigest         string             `json:"provenance_digest"`
	DependencyMetadataDigest string             `json:"dependency_metadata_digest"`
	Dependencies             []ArtifactIdentity `json:"dependencies"`
	Pinned                   bool               `json:"pinned"`
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
	ProjectMutationPlan        ProjectMutationPlan                  `json:"project_mutation_plan"`
	NewDocumentID              string                               `json:"new_document_id,omitempty"`
	RuntimeSessionID           string                               `json:"runtime_session_id"`
	LeaseToken                 string                               `json:"lease_token,omitempty"`
	RequestedRoot              ArtifactIdentity                     `json:"requested_root"`
}

type RuntimeCommitInput struct {
	Plan                 InstallPlan                          `json:"plan"`
	OperationID          string                               `json:"operation_id"`
	IdempotencyKey       string                               `json:"idempotency_key"`
	AuthoringImpact      *semantic.AuthoringImpact            `json:"authoring_impact,omitempty"`
	HostOperationImpacts []accessprotocol.HostOperationImpact `json:"host_operation_impacts"`
	AccessDecision       accessprotocol.AuthoringDecision     `json:"access_decision"`
	MutationPlan         ProjectMutationPlan                  `json:"mutation_plan"`
	RuntimeSessionID     string                               `json:"runtime_session_id"`
	LeaseToken           string                               `json:"lease_token,omitempty"`
}
type RuntimeCommitResult struct {
	CommittedRevision        string `json:"committed_revision"`
	OperationResultID        string `json:"operation_result_id"`
	DocumentID               string `json:"document_id"`
	InitialCommittedRevision bool   `json:"initial_committed_revision"`
}
type RuntimePort interface {
	CommitRegistryPlan(context.Context, RuntimeCommitInput) (RuntimeCommitResult, error)
}

// TemplateInitialPublicationPort is intentionally separate from RuntimePort:
// creating a Document has no committed base/head and must never be disguised
// as an update against an empty revision string.
type TemplateInitialPublicationPort interface {
	CommitInitialRegistryTemplate(context.Context, RuntimeCommitInput) (RuntimeCommitResult, error)
}
type RuntimeRegistryStatus string

const (
	RuntimeRegistryUnknown    RuntimeRegistryStatus = "unknown"
	RuntimeRegistryCommitted  RuntimeRegistryStatus = "committed"
	RuntimeRegistrySuperseded RuntimeRegistryStatus = "superseded"
)

type RuntimeRegistryOutcome struct {
	Status              RuntimeRegistryStatus
	Result              RuntimeCommitResult
	SupersedingRevision string
}
type RuntimeRecoveryPort interface {
	LookupRegistryCommit(context.Context, string, string, string) (RuntimeRegistryOutcome, error)
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
	RuntimeSessionID    string
	LeaseToken          string
	EngineSnapshot      RegistryProjectSnapshot
}

type RegistryProjectSnapshotKind string

const (
	RegistryProjectSnapshotWorking       RegistryProjectSnapshotKind = "runtime_working_document"
	RegistryProjectSnapshotEmptyTemplate RegistryProjectSnapshotKind = "empty_template_baseline"
)

// RegistryProjectSnapshot is an opaque Engine-owned input binding. Registry
// can compare its portable identity but cannot decode the handle, source tree,
// or LDL. PackageValidator is the sole consumer of Handle.
type RegistryProjectSnapshot struct {
	Kind                RegistryProjectSnapshotKind `json:"kind"`
	Handle              string                      `json:"handle"`
	DocumentID          string                      `json:"document_id"`
	Revision            string                      `json:"revision,omitempty"`
	DefinitionHash      string                      `json:"definition_hash,omitempty"`
	SourceClosureDigest string                      `json:"source_closure_digest"`
}

type ProjectStatePort interface {
	CurrentRegistryProjectState(context.Context, string) (ProjectState, error)
}
type TemplateDocumentPort interface {
	NewRegistryDocumentState(context.Context, ArtifactIdentity) (ProjectState, error)
}

func validRegistryProjectSnapshot(state ProjectState, template bool) bool {
	snapshot := state.EngineSnapshot
	if snapshot.Handle == "" || snapshot.DocumentID != state.DocumentID {
		return false
	}
	if _, err := protocolcommon.EncodeDigest(protocolcommon.Digest(snapshot.SourceClosureDigest)); err != nil {
		return false
	}
	if template {
		return snapshot.Kind == RegistryProjectSnapshotEmptyTemplate && snapshot.Revision == "" && snapshot.DefinitionHash == "" && state.Revision == "" && state.DefinitionHash == ""
	}
	return snapshot.Kind == RegistryProjectSnapshotWorking && snapshot.Revision == state.Revision && snapshot.DefinitionHash == state.DefinitionHash
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
	StateDownloading          TransactionState = "downloading"
	StateVerified             TransactionState = "verified"
	StateExpandedStaged       TransactionState = "expanded_staged"
	StateCompiled             TransactionState = "compiled"
	StateAwaitingConfirmation TransactionState = "awaiting_confirmation"
	StateApplying             TransactionState = "applying_project_change"
	StateCommitted            TransactionState = "committed"
	StateRolledBack           TransactionState = "rolled_back"
	StateRepairRequired       TransactionState = "repair_required"
	StateRepairing            TransactionState = "repairing"
	StateSuperseded           TransactionState = "superseded"
	StateNeedsReview          TransactionState = "needs_review"
)

type TransactionEvent struct {
	State          TransactionState `json:"state"`
	EvidenceDigest string           `json:"evidence_digest"`
	Sequence       uint64           `json:"sequence"`
	IdempotencyKey string           `json:"idempotency_key,omitempty"`
}
type Transaction struct {
	Plan                InstallPlan          `json:"plan"`
	Events              []TransactionEvent   `json:"events"`
	PlanningRequest     *PlanRequest         `json:"planning_request,omitempty"`
	CommittedRevision   string               `json:"committed_revision,omitempty"`
	OperationResultID   string               `json:"operation_result_id,omitempty"`
	RuntimeInput        *RuntimeCommitInput  `json:"runtime_input,omitempty"`
	SupersedingRevision string               `json:"superseding_revision,omitempty"`
	RuntimeResult       *RuntimeCommitResult `json:"runtime_result,omitempty"`
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
	if err := validateStoredTransaction(tx); err != nil {
		return err
	}
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
	if err := validateStoredTransaction(current); err != nil {
		return false, err
	}
	if err := validateStoredTransaction(next); err != nil {
		return false, err
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
	if !ok {
		return Transaction{}, false, nil
	}
	if err := validateStoredTransaction(tx); err != nil {
		return Transaction{}, false, err
	}
	return cloneTransaction(tx), true, nil
}

type Registry struct {
	mu               sync.RWMutex
	sources          map[string]RegistrySource
	policies         map[string]TrustPolicy
	clients          map[SourceKind]SourceClient
	validator        PackageValidator
	author           PackageAuthor
	access           AccessPort
	runtime          RuntimePort
	projectState     ProjectStatePort
	credentials      CredentialBroker
	connectors       map[SourceKind]SourceConnector
	transactions     TransactionStore
	now              func() time.Time
	verifiedCache    map[string]verifiedCacheEntry
	maxArtifactBytes int64
}

type verifiedCacheEntry struct {
	Release    ArtifactRelease
	Bytes      []byte
	Validation ValidatedArtifact
	Trust      TrustDecision
}

const defaultMaxRegistryArtifactBytes int64 = 64 << 20

func New(validator PackageValidator, access AccessPort, runtime RuntimePort, projectState ProjectStatePort, credentials CredentialBroker, transactions TransactionStore) (*Registry, error) {
	if validator == nil || access == nil || runtime == nil || projectState == nil || credentials == nil || transactions == nil {
		return nil, errors.New("registry requires Engine validator, Access, Runtime, project state, credential broker, and transaction store ports")
	}
	return &Registry{sources: map[string]RegistrySource{}, policies: map[string]TrustPolicy{}, clients: map[SourceKind]SourceClient{}, connectors: map[SourceKind]SourceConnector{}, validator: validator, access: access, runtime: runtime, projectState: projectState, credentials: credentials, transactions: transactions, verifiedCache: map[string]verifiedCacheEntry{}, maxArtifactBytes: defaultMaxRegistryArtifactBytes, now: func() time.Time { return time.Now().UTC() }}, nil
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
	current, exists := r.sources[source.SourceID]
	source.Connected = false
	source.AuthConnectionRef = ""
	if exists {
		source.Revision = current.Revision + 1
	} else {
		source.Revision = 1
	}
	r.sources[source.SourceID] = source
	return nil
}
func (r *Registry) ConnectSource(ctx context.Context, sourceID, connectionRef string) error {
	r.mu.RLock()
	source, ok := r.sources[sourceID]
	sourceDigest := digestJSON(source)
	sourceRevision := source.Revision
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
	current, stillExists := r.sources[sourceID]
	if !stillExists || current.Revision != sourceRevision || digestJSON(current) != sourceDigest {
		return fail(FailurePlanStale, sourceID, true, errors.New("source changed during connection probe"))
	}
	source = current
	source.AuthConnectionRef = connectionRef
	source.Connected = true
	source.Revision++
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
	source.Revision++
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
			found = r.cachedReleases(source.SourceID, input)
		}
		for _, release := range found {
			if release.SourceID != source.SourceID || !validReleaseIdentity(release.Identity) || (input.Kind != nil && release.Identity.Kind != *input.Kind) {
				continue
			}
			_, policy, _, ok := r.sourceContext(source.SourceID)
			if !ok {
				continue
			}
			validatedRelease, _, _, err := r.fetchValidated(ctx, source, policy, client, release)
			if err != nil {
				continue
			}
			releases = append(releases, cloneRelease(validatedRelease))
		}
	}
	if len(releases) == 0 {
		return nil, fail(FailureUnavailable, input.Query, true, nil)
	}
	sortReleases(releases, sources)
	return releases, nil
}
func (r *Registry) cachedReleases(sourceID string, input SearchInput) []ArtifactRelease {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []ArtifactRelease{}
	for _, entry := range r.verifiedCache {
		release := entry.Release
		if release.SourceID == sourceID && (input.Query == "" || release.Identity.CanonicalID == input.Query) && (input.Kind == nil || release.Identity.Kind == *input.Kind) {
			out = append(out, cloneRelease(release))
		}
	}
	return out
}

func (r *Registry) fetchValidated(ctx context.Context, source RegistrySource, policy TrustPolicy, client SourceClient, release ArtifactRelease) (ArtifactRelease, []byte, ValidatedArtifact, error) {
	decision, err := verifyTrust(r.now(), source, policy, release)
	if err != nil {
		return ArtifactRelease{}, nil, ValidatedArtifact{}, err
	}
	if release.Size < 0 || release.Size > r.maxArtifactBytes {
		return ArtifactRelease{}, nil, ValidatedArtifact{}, fail(FailureArtifactCorrupt, release.Identity.CanonicalID, true, errors.New("artifact exceeds host byte limit"))
	}
	r.mu.RLock()
	cached, cachedOK := r.verifiedCache[release.Digest]
	r.mu.RUnlock()
	if cachedOK && cached.Release.Identity == release.Identity && cached.Release.SourceID == release.SourceID && cached.Release.PublisherID == release.PublisherID && cached.Release.ProvenanceDigest == release.ProvenanceDigest && cached.Release.DependencyMetadataDigest == release.DependencyMetadataDigest && int64(len(cached.Bytes)) == release.Size && digestBytes(cached.Bytes) == release.Digest {
		out := cloneRelease(release)
		out.Trust = &decision
		return out, append([]byte{}, cached.Bytes...), cached.Validation, nil
	}
	var body []byte
	if streaming, ok := client.(StreamingSourceClient); ok {
		reader, openErr := streaming.OpenArtifact(ctx, source, release)
		if openErr != nil {
			return ArtifactRelease{}, nil, ValidatedArtifact{}, fail(FailureUnavailable, source.SourceID, true, openErr)
		}
		defer reader.Close()
		body, err = io.ReadAll(io.LimitReader(reader, r.maxArtifactBytes+1))
	} else if byteClient, ok := client.(ByteSourceClient); ok {
		body, err = byteClient.Download(ctx, source, release)
	} else {
		err = errors.New("source client has no streaming artifact transport")
	}
	if err != nil {
		return ArtifactRelease{}, nil, ValidatedArtifact{}, fail(FailureUnavailable, source.SourceID, true, err)
	}
	if int64(len(body)) != release.Size || int64(len(body)) > r.maxArtifactBytes || digestBytes(body) != release.Digest {
		return ArtifactRelease{}, nil, ValidatedArtifact{}, fail(FailureArtifactCorrupt, release.Identity.CanonicalID, true, nil)
	}
	validation, err := r.validator.ValidateRegistryArtifact(ctx, release, body)
	if err != nil || validation.Identity != release.Identity || validation.CanonicalDigest != release.Digest {
		return ArtifactRelease{}, nil, ValidatedArtifact{}, fail(FailureArtifactCorrupt, release.Identity.CanonicalID, true, err)
	}
	out := cloneRelease(release)
	out.Trust = &decision
	r.mu.Lock()
	r.verifiedCache[release.Digest] = verifiedCacheEntry{Release: cloneRelease(out), Bytes: append([]byte{}, body...), Validation: validation, Trust: decision}
	r.mu.Unlock()
	return out, body, validation, nil
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

func (r *Registry) Plan(ctx context.Context, request PlanRequest) (result InstallPlan, resultErr error) {
	if !validAction(request.Action) || !validIdentity(request.Requested) || (request.Action != ActionCreateFromTemplate && (request.ProjectID == "" || request.BaseRevision == "")) {
		return InstallPlan{}, fail(FailureDependencyConflict, request.Requested.CanonicalID, true, nil)
	}
	if request.Action == ActionCreateFromTemplate && request.Requested.Kind != ArtifactTemplate {
		return InstallPlan{}, fail(FailureUnsupportedFormat, request.Requested.CanonicalID, true, nil)
	}
	var state ProjectState
	var err error
	if request.Action == ActionCreateFromTemplate {
		allocator, ok := r.projectState.(TemplateDocumentPort)
		if !ok {
			return InstallPlan{}, fail(FailureUnavailable, "template_document_allocator", true, nil)
		}
		state, err = allocator.NewRegistryDocumentState(ctx, request.Requested)
		if err != nil || state.DocumentID == "" || state.RuntimeSessionID == "" || !validRegistryProjectSnapshot(state, true) {
			return InstallPlan{}, fail(FailureUnavailable, "template_document_allocator", true, err)
		}
		request.ProjectID = state.ProjectID
		request.BaseRevision = ""
		request.ExpectedDefinitionHash = ""
		request.ExpectedResolvedLockDigest = ""
	} else {
		state, err = r.projectState.CurrentRegistryProjectState(ctx, request.ProjectID)
		if err != nil {
			return InstallPlan{}, fail(FailureUnavailable, request.ProjectID, true, err)
		}
		if state.ProjectID != request.ProjectID || state.DocumentID == "" || state.Revision != request.BaseRevision || state.DefinitionHash != request.ExpectedDefinitionHash || state.DependencySnapshot.ResolvedLockDigest != request.ExpectedResolvedLockDigest || state.RuntimeSessionID == "" || !validRegistryProjectSnapshot(state, false) {
			return InstallPlan{}, fail(FailurePlanStale, request.ProjectID, true, nil)
		}
	}
	installed, hasInstalled := findLocked(state.DependencySnapshot, request.Requested)
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
	if request.Action == ActionRemove {
		for _, candidate := range state.DependencySnapshot.Installs {
			for _, dependency := range candidate.Dependencies {
				if dependency.Kind == request.Requested.Kind && dependency.CanonicalID == request.Requested.CanonicalID {
					return InstallPlan{}, fail(FailureDependencyConflict, candidate.Identity.CanonicalID, true, errors.New("installed dependent requires removed artifact"))
				}
			}
		}
	}
	if request.Action == ActionPin && request.Requested.Version == "latest" {
		return InstallPlan{}, fail(FailureDependencyConflict, request.Requested.CanonicalID, true, nil)
	}
	if request.Action == ActionRepair {
		request.Requested = installed.Identity
	}
	transactionID := strings.TrimPrefix(digestJSON(struct {
		Request    PlanRequest
		DocumentID string
	}{request, state.DocumentID}), "sha256:")[:32]
	planningDigest := digestJSON(struct {
		Request    PlanRequest
		DocumentID string
	}{request, state.DocumentID})
	planningRequest := request
	tx, exists, loadErr := r.transactions.GetRegistryTransaction(ctx, transactionID)
	if loadErr != nil {
		return InstallPlan{}, fail(FailureUnavailable, transactionID, true, loadErr)
	}
	if exists {
		if transactionState(tx) == StateAwaitingConfirmation {
			if err := validateFinalizedPlan(tx.Plan); err != nil {
				return InstallPlan{}, fail(FailurePlanStale, transactionID, true, err)
			}
			return clonePlan(tx.Plan), nil
		}
		if tx.PlanningRequest == nil || digestPlanningRequest(*tx.PlanningRequest) != digestPlanningRequest(request) || (transactionState(tx) != StatePlanned && transactionState(tx) != StateDownloading) {
			return InstallPlan{}, fail(FailurePlanStale, transactionID, true, nil)
		}
	} else {
		tx = Transaction{Plan: InstallPlan{TransactionID: transactionID, PlanDigest: planningDigest, Action: request.Action, ProjectID: request.ProjectID, RequestedRoot: request.Requested}, PlanningRequest: &planningRequest, Events: []TransactionEvent{{State: StatePlanned, EvidenceDigest: digestJSON(request), Sequence: 1}}}
		if err := r.transactions.CreateRegistryTransaction(ctx, tx); err != nil {
			return InstallPlan{}, fail(FailureUnavailable, transactionID, true, err)
		}
	}
	if transactionState(tx) == StatePlanned {
		if err := r.appendEvent(ctx, &tx, TransactionEvent{State: StateDownloading, EvidenceDigest: digestJSON(request)}); err != nil {
			return InstallPlan{}, err
		}
	}
	defer func() {
		if resultErr != nil && transactionState(tx) == StateDownloading {
			_ = r.appendEvent(ctx, &tx, TransactionEvent{State: StateRolledBack, EvidenceDigest: digestJSON(resultErr)})
		}
	}()
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
	if hasInstalled && request.Action != ActionRemove && request.Action != ActionUpdate {
		root := resolved[string(request.Requested.Kind)+":"+request.Requested.CanonicalID]
		if (installed.SourceID != "" && root.Release.SourceID != installed.SourceID) || (installed.PublisherID != "" && root.Release.PublisherID != installed.PublisherID) || (installed.ProvenanceDigest != "" && root.Release.ProvenanceDigest != installed.ProvenanceDigest) {
			return InstallPlan{}, fail(FailureDependencyConflict, request.Requested.CanonicalID, true, errors.New("locked source, publisher, or provenance changed"))
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
	sort.Strings(impacts)
	sort.Strings(trust)
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].SourceID < bindings[j].SourceID })
	hostCapabilities := append([]string{}, state.HostCapabilities...)
	sort.Strings(hostCapabilities)
	for _, prior := range state.DependencySnapshot.Installs {
		if artifactKey(prior.Identity) == artifactKey(request.Requested) {
			continue
		}
		if candidate, ok := resolved[artifactKey(prior.Identity)]; ok && !lockedReleaseBindingMatches(prior, candidate.Release, resolved) {
			return InstallPlan{}, fail(FailureDependencyConflict, prior.Identity.CanonicalID, true, errors.New("installed dependency requires an explicit update transaction"))
		}
	}
	delta, err := buildLockDelta(request.Action, state.DependencySnapshot, resolved, request.Requested, request.RequestedPin)
	if err != nil {
		return InstallPlan{}, fail(FailureArtifactCorrupt, request.Requested.CanonicalID, true, err)
	}
	mutationPlan, err := r.validator.BuildRegistryMutationPlan(ctx, RegistryMutationBuildInput{Action: request.Action, Project: state, Artifacts: artifacts, DependencySnapshot: cloneDependencySnapshot(state.DependencySnapshot), ResolvedLockDelta: delta, Requested: request.Requested, NewDocumentID: func() string {
		if request.Action == ActionCreateFromTemplate {
			return state.DocumentID
		}
		return ""
	}()})
	if err != nil || mutationPlan.AuthoringImpactDigest == "" || mutationPlan.MutationDigest == "" || string(mutationPlan.AuthoringImpact.ImpactDigest) != mutationPlan.AuthoringImpactDigest {
		return InstallPlan{}, fail(FailureArtifactCorrupt, request.Requested.CanonicalID, true, err)
	}
	if mutationPlan.BaseProjectRevision != state.Revision || mutationPlan.ExpectedDefinitionHash != state.DefinitionHash || mutationPlan.ExpectedResolvedLockDigest != state.DependencySnapshot.ResolvedLockDigest || digestJSON(mutationPlan.ResolvedLockDelta) != digestJSON(delta) {
		return InstallPlan{}, fail(FailureArtifactCorrupt, request.Requested.CanonicalID, true, errors.New("Engine mutation plan preconditions or lock delta mismatch"))
	}
	authoringImpact := &mutationPlan.AuthoringImpact
	hostImpact, err := accesscore.HostOperationImpact(accessprotocol.HostOperationKindPackageTransaction, hostImpactAction(request.Action), accessprotocol.HostResourceScope{DocumentID: state.DocumentID, LocalScopeID: state.LocalScopeID, OrganizationScopeID: state.OrganizationScopeID}, []string{request.Requested.CanonicalID})
	if err != nil {
		return InstallPlan{}, fail(FailureArtifactCorrupt, request.Requested.CanonicalID, true, err)
	}
	mutation := mutationPlan.MutationDigest
	evaluate := accessprotocol.EvaluateAuthoringInput{AuthoringImpact: authoringImpact, GrantSnapshot: state.GrantSnapshot, HostOperationImpacts: []accessprotocol.HostOperationImpact{hostImpact}, RequestIntent: "apply"}
	decision, err := r.access.EvaluateRegistryPlan(ctx, evaluate)
	if err != nil || decision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow || !decisionBindsMutation(decision, *authoringImpact, hostImpact) {
		return InstallPlan{}, fail(FailurePolicyDenied, request.ProjectID, true, err)
	}
	plan := InstallPlan{Action: request.Action, ProjectID: request.ProjectID, BaseRevision: state.Revision, ExpectedDefinitionHash: state.DefinitionHash, ExpectedResolvedLockDigest: state.DependencySnapshot.ResolvedLockDigest, Artifacts: artifacts, RequiredCapabilities: capabilitiesToStrings(decision.RequiredCapabilities), TrustPolicyDigests: trust, SourceBindings: bindings, DependencySnapshot: cloneDependencySnapshot(state.DependencySnapshot), ResolvedLockDelta: delta, RollbackCheckpoint: RollbackCheckpoint{BaseProjectRevision: state.Revision, BaseDefinitionHash: state.DefinitionHash, BaseResolvedLockDigest: state.DependencySnapshot.ResolvedLockDigest, CurrentPackTreeManifest: state.PackTreeManifest}, ExpiresAt: r.now().Add(15 * time.Minute), MigrationRequired: migrationRequired, CreatesNewDocument: request.Action == ActionCreateFromTemplate, MutationDigest: mutation, AuthoringImpactDigests: []string{mutationPlan.AuthoringImpactDigest}, HostOperationImpactDigest: string(hostImpact.ImpactDigest), EvaluationDigest: string(decision.EvaluationDigest), AuthoringImpact: authoringImpact, HostOperationImpacts: []accessprotocol.HostOperationImpact{hostImpact}, AccessDecision: decision, HostCapabilitiesDigest: digestJSON(hostCapabilities), ProjectMutationPlan: mutationPlan, RuntimeSessionID: state.RuntimeSessionID, LeaseToken: state.LeaseToken, RequestedRoot: request.Requested}
	if request.Action == ActionCreateFromTemplate {
		plan.NewDocumentID = state.DocumentID
	}
	plan.TransactionID = transactionID
	plan.ProjectMutationPlan.RegistryTransactionID = plan.TransactionID
	plan.ProjectMutationPlan.HostOperationImpactDigest = plan.HostOperationImpactDigest
	plan.ProjectMutationPlan.EvaluationDigest = plan.EvaluationDigest
	plan.PlanDigest = digestPlan(plan)
	plan.ProjectMutationPlan.PlanDigest = plan.PlanDigest
	if err := validateFinalizedPlan(plan); err != nil {
		return InstallPlan{}, fail(FailureArtifactCorrupt, plan.TransactionID, true, err)
	}
	tx.Plan = plan
	for _, step := range []TransactionEvent{{State: StateVerified, EvidenceDigest: digestJSON(plan.TrustPolicyDigests)}, {State: StateExpandedStaged, EvidenceDigest: plan.ProjectMutationPlan.StagedTreeManifest}, {State: StateCompiled, EvidenceDigest: plan.ProjectMutationPlan.AuthoringImpactDigest}, {State: StateAwaitingConfirmation, EvidenceDigest: plan.EvaluationDigest}} {
		version := transactionVersion(tx)
		step.Sequence = version + 1
		tx.Events = append(tx.Events, step)
		swapped, appendErr := r.transactions.CompareAndSwapRegistryTransaction(ctx, plan.TransactionID, version, tx)
		if appendErr != nil || !swapped {
			return InstallPlan{}, fail(FailureUnavailable, plan.TransactionID, true, appendErr)
		}
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
	for _, candidate := range releases {
		if candidate.Identity == selected.Identity && candidate.Digest != selected.Digest {
			return fail(FailureDependencyConflict, identity.CanonicalID, true, errors.New("same artifact identity resolved to divergent digests"))
		}
	}
	source, policy, client, ok := r.sourceContext(selected.SourceID)
	if !ok || !source.Connected || client == nil {
		return fail(FailureUnavailable, selected.SourceID, true, nil)
	}
	if selected.Identity.Kind != identity.Kind {
		return fail(FailureUnsupportedFormat, identity.CanonicalID, true, nil)
	}
	verified, _, validated, err := r.fetchValidated(ctx, source, policy, client, *selected)
	if err != nil {
		return err
	}
	for _, decision := range selected.Compatibility {
		if decision.Status == "incompatible" || decision.Status == "disabled" {
			return fail(FailureIncompatibleCapability, decision.Subject, true, nil)
		}
	}
	resolved[key] = PlanArtifact{Release: cloneRelease(verified), Validation: validated}
	dependencies := append([]Dependency{}, selected.Dependencies...)
	sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].CanonicalID < dependencies[j].CanonicalID })
	for _, dependency := range dependencies {
		version, err := r.resolveVersion(ctx, dependency, prerelease)
		if err != nil {
			return err
		}
		dependencyKind := dependency.Kind
		if dependencyKind == "" {
			dependencyKind = ArtifactPack
		}
		if err := r.resolve(ctx, ArtifactIdentity{Kind: dependencyKind, CanonicalID: dependency.CanonicalID, Version: version}, prerelease, resolved, visiting); err != nil {
			return err
		}
		child := resolved[string(dependencyKind)+":"+dependency.CanonicalID]
		if dependency.DigestConstraint != "" && child.Release.Digest != dependency.DigestConstraint {
			return fail(FailureDependencyConflict, dependency.CanonicalID, true, nil)
		}
	}
	return nil
}

func (r *Registry) resolveVersion(ctx context.Context, dependency Dependency, prerelease bool) (string, error) {
	kind := dependency.Kind
	if kind == "" {
		kind = ArtifactPack
	}
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
	if !validRuntimeOperationID(input.OperationID) || input.IdempotencyKey == "" {
		return RuntimeCommitResult{}, fail(FailurePlanStale, input.Plan.TransactionID, true, errors.New("operation_id and idempotency_key are required"))
	}
	tx, ok, loadErr := r.transactions.GetRegistryTransaction(ctx, input.Plan.TransactionID)
	if loadErr != nil || !ok || validateFinalizedPlan(tx.Plan) != nil || validateFinalizedPlan(input.Plan) != nil || tx.Plan.PlanDigest != input.Plan.PlanDigest {
		return RuntimeCommitResult{}, fail(FailurePlanStale, input.Plan.TransactionID, true, nil)
	}
	if !tx.Plan.ExpiresAt.After(r.now()) {
		return RuntimeCommitResult{}, fail(FailurePlanStale, input.Plan.TransactionID, true, nil)
	}
	boundDocumentID, documentErr := planBoundDocumentID(tx.Plan)
	if documentErr != nil {
		return RuntimeCommitResult{}, fail(FailurePlanStale, input.Plan.TransactionID, true, documentErr)
	}
	var state ProjectState
	var err error
	if tx.Plan.CreatesNewDocument {
		allocator, ok := r.projectState.(TemplateDocumentPort)
		if !ok {
			return RuntimeCommitResult{}, fail(FailurePlanStale, "template_document_allocator", true, nil)
		}
		requested := tx.Plan.RequestedRoot
		state, err = allocator.NewRegistryDocumentState(ctx, requested)
		if err != nil || state.DocumentID != boundDocumentID || !validRegistryProjectSnapshot(state, true) {
			return RuntimeCommitResult{}, fail(FailurePlanStale, tx.Plan.NewDocumentID, true, err)
		}
	} else {
		state, err = r.projectState.CurrentRegistryProjectState(ctx, tx.Plan.ProjectID)
		if err != nil || state.DocumentID != boundDocumentID || state.Revision != tx.Plan.BaseRevision || state.DefinitionHash != tx.Plan.ExpectedDefinitionHash || state.DependencySnapshot.ResolvedLockDigest != tx.Plan.ExpectedResolvedLockDigest || !validRegistryProjectSnapshot(state, false) {
			return RuntimeCommitResult{}, fail(FailurePlanStale, input.Plan.TransactionID, true, err)
		}
	}
	hostCapabilities := append([]string{}, state.HostCapabilities...)
	sort.Strings(hostCapabilities)
	if digestJSON(hostCapabilities) != tx.Plan.HostCapabilitiesDigest {
		return RuntimeCommitResult{}, fail(FailurePlanStale, "host_capabilities", true, nil)
	}
	for _, binding := range tx.Plan.SourceBindings {
		source, policy, client, ok := r.sourceContext(binding.SourceID)
		if !ok || digestJSON(source) != binding.SourceDigest || digestTrust(policy) != binding.TrustPolicyDigest {
			return RuntimeCommitResult{}, fail(FailurePlanStale, binding.SourceID, true, nil)
		}
		for _, artifact := range tx.Plan.Artifacts {
			if artifact.Release.SourceID == binding.SourceID {
				if _, err := verifyTrust(r.now(), source, policy, artifact.Release); err != nil {
					return RuntimeCommitResult{}, fail(FailurePlanStale, binding.SourceID, true, err)
				}
				kind := artifact.Release.Identity.Kind
				fresh, searchErr := client.Search(ctx, source, SearchInput{Query: artifact.Release.Identity.CanonicalID, Kind: &kind, IncludePrerelease: true})
				if searchErr != nil {
					return RuntimeCommitResult{}, fail(FailurePlanStale, binding.SourceID, true, searchErr)
				}
				matched := false
				for _, candidate := range fresh {
					if immutableReleaseBinding(candidate, artifact.Release) {
						matched = true
						break
					}
				}
				if !matched {
					return RuntimeCommitResult{}, fail(FailurePlanStale, artifact.Release.Identity.CanonicalID, true, errors.New("artifact metadata changed"))
				}
			}
		}
	}
	freshMutation, mutationErr := r.validator.BuildRegistryMutationPlan(ctx, RegistryMutationBuildInput{Action: tx.Plan.Action, Project: state, Artifacts: tx.Plan.Artifacts, DependencySnapshot: tx.Plan.DependencySnapshot, ResolvedLockDelta: tx.Plan.ResolvedLockDelta, Requested: tx.Plan.RequestedRoot, NewDocumentID: tx.Plan.NewDocumentID})
	if mutationErr != nil || mutationSemanticDigest(freshMutation) != mutationSemanticDigest(tx.Plan.ProjectMutationPlan) {
		return RuntimeCommitResult{}, fail(FailurePlanStale, "project_mutation_plan", true, mutationErr)
	}
	evaluate := accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &freshMutation.AuthoringImpact, GrantSnapshot: state.GrantSnapshot, HostOperationImpacts: append([]accessprotocol.HostOperationImpact{}, tx.Plan.HostOperationImpacts...), RequestIntent: "apply"}
	decision, err := r.access.EvaluateRegistryPlan(ctx, evaluate)
	if err != nil || decision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow || string(decision.EvaluationDigest) != tx.Plan.EvaluationDigest || !decisionBindsMutation(decision, freshMutation.AuthoringImpact, tx.Plan.HostOperationImpacts[0]) {
		return RuntimeCommitResult{}, fail(FailurePlanStale, "authoring_policy", true, err)
	}
	stateName := transactionState(tx)
	if stateName == StateCommitted {
		if lastIdempotencyKey(tx) == input.IdempotencyKey {
			result, resultErr := persistedRuntimeCommitResult(tx, input, boundDocumentID)
			if resultErr != nil {
				return RuntimeCommitResult{}, fail(FailureRepairRequired, input.Plan.TransactionID, true, resultErr)
			}
			return result, nil
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
	input.MutationPlan = cloneJSONValue(tx.Plan.ProjectMutationPlan)
	input.RuntimeSessionID = state.RuntimeSessionID
	input.LeaseToken = state.LeaseToken
	input.AuthoringImpact = &freshMutation.AuthoringImpact
	input.HostOperationImpacts = append([]accessprotocol.HostOperationImpact{}, tx.Plan.HostOperationImpacts...)
	input.AccessDecision = decision
	tx.RuntimeInput = &input
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
			result, resultErr := persistedRuntimeCommitResult(latest, input, boundDocumentID)
			if resultErr != nil {
				return RuntimeCommitResult{}, fail(FailureRepairRequired, input.Plan.TransactionID, true, resultErr)
			}
			return result, nil
		}
		return RuntimeCommitResult{}, fail(FailureRepairRequired, input.Plan.TransactionID, true, errors.New("concurrent publication already started"))
	}
	var result RuntimeCommitResult
	if tx.Plan.CreatesNewDocument {
		initial, ok := r.runtime.(TemplateInitialPublicationPort)
		if !ok {
			err = errors.New("Runtime initial Registry publication facade is unavailable")
		} else {
			result, err = initial.CommitInitialRegistryTemplate(ctx, input)
		}
	} else {
		result, err = r.runtime.CommitRegistryPlan(ctx, input)
	}
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
	if resultErr := validateRuntimeCommitResult(tx.Plan, input, result, boundDocumentID); resultErr != nil {
		version := transactionVersion(tx)
		tx.Events = append(tx.Events, TransactionEvent{State: StateRepairRequired, EvidenceDigest: tx.Plan.MutationDigest, Sequence: version + 1, IdempotencyKey: input.IdempotencyKey})
		_, _ = r.transactions.CompareAndSwapRegistryTransaction(ctx, tx.Plan.TransactionID, version, tx)
		return RuntimeCommitResult{}, fail(FailureRepairRequired, tx.Plan.TransactionID, true, resultErr)
	}
	tx.CommittedRevision = result.CommittedRevision
	tx.OperationResultID = result.OperationResultID
	tx.RuntimeResult = &result
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
	if err != nil || !ok || validateStoredTransaction(tx) != nil {
		return Transaction{}, false
	}
	return tx, true
}
func (r *Registry) GetTransaction(ctx context.Context, id string) (Transaction, error) {
	tx, ok, err := r.transactions.GetRegistryTransaction(ctx, id)
	if err != nil {
		return Transaction{}, fail(FailureUnavailable, id, true, err)
	}
	if !ok {
		return Transaction{}, fail(FailureUnavailable, id, true, nil)
	}
	if err := validateStoredTransaction(tx); err != nil {
		return Transaction{}, fail(FailureUnavailable, id, true, err)
	}
	return tx, nil
}
func (r *Registry) RecoverTransaction(ctx context.Context, id string) (Transaction, error) {
	tx, err := r.GetTransaction(ctx, id)
	if err != nil {
		return Transaction{}, err
	}
	if (transactionState(tx) == StatePlanned || transactionState(tx) == StateDownloading) && tx.PlanningRequest != nil {
		if _, err := r.Plan(ctx, *tx.PlanningRequest); err != nil {
			latest, _, _ := r.transactions.GetRegistryTransaction(ctx, id)
			return latest, err
		}
		return r.GetTransaction(ctx, id)
	}
	for {
		var next *TransactionEvent
		switch transactionState(tx) {
		case StatePlanned:
			next = &TransactionEvent{State: StateDownloading, EvidenceDigest: digestJSON(tx.Plan.SourceBindings)}
		case StateDownloading:
			next = &TransactionEvent{State: StateVerified, EvidenceDigest: digestJSON(tx.Plan.TrustPolicyDigests)}
		case StateVerified:
			next = &TransactionEvent{State: StateExpandedStaged, EvidenceDigest: tx.Plan.ProjectMutationPlan.StagedTreeManifest}
		case StateExpandedStaged:
			next = &TransactionEvent{State: StateCompiled, EvidenceDigest: tx.Plan.ProjectMutationPlan.AuthoringImpactDigest}
		case StateCompiled:
			next = &TransactionEvent{State: StateAwaitingConfirmation, EvidenceDigest: tx.Plan.EvaluationDigest}
		}
		if next == nil {
			break
		}
		if err := r.appendEvent(ctx, &tx, *next); err != nil {
			return Transaction{}, err
		}
	}
	if transactionState(tx) != StateApplying && transactionState(tx) != StateRepairRequired && transactionState(tx) != StateRepairing {
		return tx, nil
	}
	if planErr := validateFinalizedPlan(tx.Plan); planErr != nil {
		return tx, fail(FailureRepairRequired, id, true, planErr)
	}
	if transactionState(tx) == StateApplying {
		if err := r.appendEvent(ctx, &tx, TransactionEvent{State: StateRepairRequired, EvidenceDigest: tx.Plan.MutationDigest, IdempotencyKey: lastIdempotencyKey(tx)}); err != nil {
			return Transaction{}, err
		}
	}
	if transactionState(tx) == StateRepairRequired {
		if err := r.appendEvent(ctx, &tx, TransactionEvent{State: StateRepairing, EvidenceDigest: tx.Plan.MutationDigest, IdempotencyKey: lastIdempotencyKey(tx)}); err != nil {
			return Transaction{}, err
		}
	}
	recovery, ok := r.runtime.(RuntimeRecoveryPort)
	if !ok {
		return r.recoveryNeedsReview(ctx, tx, errors.New("Runtime recovery lookup unavailable"))
	}
	outcome, lookupErr := recovery.LookupRegistryCommit(ctx, id, tx.Plan.PlanDigest, func() string {
		if tx.RuntimeInput != nil {
			return tx.RuntimeInput.OperationID
		}
		return ""
	}())
	if lookupErr != nil {
		return r.recoveryNeedsReview(ctx, tx, lookupErr)
	}
	if outcome.Status == RuntimeRegistryUnknown && tx.RuntimeInput != nil {
		freshInput, refreshErr := r.refreshRecoveryRuntimeInput(ctx, tx, *tx.RuntimeInput)
		if refreshErr != nil {
			return r.recoveryNeedsReviewWithCode(ctx, tx, FailurePlanStale, refreshErr)
		}
		tx.RuntimeInput = &freshInput
		result, replayErr := r.runtime.CommitRegistryPlan(ctx, freshInput)
		if replayErr != nil {
			return r.recoveryNeedsReview(ctx, tx, replayErr)
		}
		outcome = RuntimeRegistryOutcome{Status: RuntimeRegistryCommitted, Result: result}
	}
	switch outcome.Status {
	case RuntimeRegistryCommitted:
		if tx.RuntimeInput == nil {
			return r.recoveryNeedsReview(ctx, tx, errors.New("Runtime committed outcome is missing its durable operation binding"))
		}
		boundDocumentID, documentErr := planBoundDocumentID(tx.Plan)
		if documentErr != nil {
			return r.recoveryNeedsReview(ctx, tx, documentErr)
		}
		if resultErr := validateRuntimeCommitResult(tx.Plan, *tx.RuntimeInput, outcome.Result, boundDocumentID); resultErr != nil {
			return r.recoveryNeedsReview(ctx, tx, resultErr)
		}
		tx.RuntimeResult = &outcome.Result
		tx.CommittedRevision = outcome.Result.CommittedRevision
		tx.OperationResultID = outcome.Result.OperationResultID
		if err := r.appendEvent(ctx, &tx, TransactionEvent{State: StateCommitted, EvidenceDigest: digestJSON(outcome.Result), IdempotencyKey: lastIdempotencyKey(tx)}); err != nil {
			return Transaction{}, err
		}
	case RuntimeRegistrySuperseded:
		tx.SupersedingRevision = outcome.SupersedingRevision
		if err := r.appendEvent(ctx, &tx, TransactionEvent{State: StateSuperseded, EvidenceDigest: digestJSON(outcome), IdempotencyKey: lastIdempotencyKey(tx)}); err != nil {
			return Transaction{}, err
		}
	default:
		return r.recoveryNeedsReview(ctx, tx, errors.New("Runtime could not prove publication outcome"))
	}
	return tx, nil
}

func (r *Registry) refreshRecoveryRuntimeInput(ctx context.Context, tx Transaction, input RuntimeCommitInput) (RuntimeCommitInput, error) {
	if !validRuntimeOperationID(input.OperationID) || input.IdempotencyKey == "" {
		return RuntimeCommitInput{}, errors.New("recovery replay is missing operation identity")
	}
	boundDocumentID, err := planBoundDocumentID(tx.Plan)
	if err != nil {
		return RuntimeCommitInput{}, err
	}
	var state ProjectState
	if tx.Plan.CreatesNewDocument {
		allocator, ok := r.projectState.(TemplateDocumentPort)
		if !ok {
			return RuntimeCommitInput{}, errors.New("template document allocator unavailable")
		}
		state, err = allocator.NewRegistryDocumentState(ctx, tx.Plan.RequestedRoot)
		if err != nil || state.DocumentID != boundDocumentID || !validRegistryProjectSnapshot(state, true) {
			return RuntimeCommitInput{}, errors.New("template document allocation changed during recovery")
		}
	} else {
		state, err = r.projectState.CurrentRegistryProjectState(ctx, tx.Plan.ProjectID)
		if err != nil || state.DocumentID != boundDocumentID || state.Revision != tx.Plan.BaseRevision || state.DefinitionHash != tx.Plan.ExpectedDefinitionHash || state.DependencySnapshot.ResolvedLockDigest != tx.Plan.ExpectedResolvedLockDigest || !validRegistryProjectSnapshot(state, false) {
			return RuntimeCommitInput{}, errors.New("project preconditions changed during recovery")
		}
	}
	hostCapabilities := append([]string{}, state.HostCapabilities...)
	sort.Strings(hostCapabilities)
	if state.RuntimeSessionID == "" || digestJSON(hostCapabilities) != tx.Plan.HostCapabilitiesDigest {
		return RuntimeCommitInput{}, errors.New("Runtime session or host capabilities changed during recovery")
	}
	for _, binding := range tx.Plan.SourceBindings {
		source, policy, client, ok := r.sourceContext(binding.SourceID)
		if !ok || digestJSON(source) != binding.SourceDigest || digestTrust(policy) != binding.TrustPolicyDigest {
			return RuntimeCommitInput{}, errors.New("Registry source or trust policy changed during recovery")
		}
		for _, artifact := range tx.Plan.Artifacts {
			if artifact.Release.SourceID != binding.SourceID {
				continue
			}
			if _, err := verifyTrust(r.now(), source, policy, artifact.Release); err != nil {
				return RuntimeCommitInput{}, err
			}
			kind := artifact.Release.Identity.Kind
			fresh, err := client.Search(ctx, source, SearchInput{Query: artifact.Release.Identity.CanonicalID, Kind: &kind, IncludePrerelease: true})
			if err != nil {
				return RuntimeCommitInput{}, err
			}
			matched := false
			for _, candidate := range fresh {
				if immutableReleaseBinding(candidate, artifact.Release) {
					matched = true
					break
				}
			}
			if !matched {
				return RuntimeCommitInput{}, errors.New("artifact metadata changed during recovery")
			}
		}
	}
	freshMutation, err := r.validator.BuildRegistryMutationPlan(ctx, RegistryMutationBuildInput{Action: tx.Plan.Action, Project: state, Artifacts: tx.Plan.Artifacts, DependencySnapshot: tx.Plan.DependencySnapshot, ResolvedLockDelta: tx.Plan.ResolvedLockDelta, Requested: tx.Plan.RequestedRoot, NewDocumentID: tx.Plan.NewDocumentID})
	if err != nil || mutationSemanticDigest(freshMutation) != mutationSemanticDigest(tx.Plan.ProjectMutationPlan) {
		return RuntimeCommitInput{}, errors.New("Engine mutation changed during recovery")
	}
	if len(tx.Plan.HostOperationImpacts) != 1 {
		return RuntimeCommitInput{}, errors.New("recovery requires one bound host impact")
	}
	decision, err := r.access.EvaluateRegistryPlan(ctx, accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &freshMutation.AuthoringImpact, GrantSnapshot: state.GrantSnapshot, HostOperationImpacts: append([]accessprotocol.HostOperationImpact{}, tx.Plan.HostOperationImpacts...), RequestIntent: "apply"})
	if err != nil || decision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow || string(decision.EvaluationDigest) != tx.Plan.EvaluationDigest || !decisionBindsMutation(decision, freshMutation.AuthoringImpact, tx.Plan.HostOperationImpacts[0]) {
		return RuntimeCommitInput{}, errors.New("fresh Access evaluation denied recovery replay")
	}
	input.Plan = clonePlan(tx.Plan)
	input.MutationPlan = tx.Plan.ProjectMutationPlan
	input.RuntimeSessionID = state.RuntimeSessionID
	input.LeaseToken = state.LeaseToken
	input.AuthoringImpact = cloneAuthoringImpact(&freshMutation.AuthoringImpact)
	input.HostOperationImpacts = cloneHostImpacts(tx.Plan.HostOperationImpacts)
	input.AccessDecision = cloneAccessDecision(decision)
	return input, nil
}

func (r *Registry) appendEvent(ctx context.Context, tx *Transaction, event TransactionEvent) error {
	version := transactionVersion(*tx)
	event.Sequence = version + 1
	tx.Events = append(tx.Events, event)
	ok, err := r.transactions.CompareAndSwapRegistryTransaction(ctx, tx.Plan.TransactionID, version, *tx)
	if err != nil || !ok {
		return fail(FailureUnavailable, tx.Plan.TransactionID, true, err)
	}
	return nil
}
func (r *Registry) recoveryNeedsReview(ctx context.Context, tx Transaction, cause error) (Transaction, error) {
	return r.recoveryNeedsReviewWithCode(ctx, tx, FailureRepairRequired, cause)
}
func (r *Registry) recoveryNeedsReviewWithCode(ctx context.Context, tx Transaction, code string, cause error) (Transaction, error) {
	if err := r.appendEvent(ctx, &tx, TransactionEvent{State: StateNeedsReview, EvidenceDigest: tx.Plan.MutationDigest, IdempotencyKey: lastIdempotencyKey(tx)}); err != nil {
		return Transaction{}, err
	}
	return tx, fail(code, tx.Plan.TransactionID, true, cause)
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
		return TrustDecision{}, fail(FailureSignatureMissing, release.Identity.CanonicalID, true, nil)
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
	prerelease          []string
}

func parseVersion(value string) (semanticVersion, bool) {
	if value == "" || strings.Count(value, "+") > 1 {
		return semanticVersion{}, false
	}
	withoutBuild, build, hasBuild := strings.Cut(value, "+")
	if hasBuild && !validSemverIdentifiers(build, false) {
		return semanticVersion{}, false
	}
	main, pre, hasPre := strings.Cut(withoutBuild, "-")
	if hasPre && !validSemverIdentifiers(pre, true) {
		return semanticVersion{}, false
	}
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
	prerelease := []string(nil)
	if hasPre {
		prerelease = strings.Split(pre, ".")
	}
	return semanticVersion{nums[0], nums[1], nums[2], prerelease}, true
}
func validSemverIdentifiers(value string, rejectNumericLeadingZero bool) bool {
	if value == "" {
		return false
	}
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" || (rejectNumericLeadingZero && len(identifier) > 1 && identifier[0] == '0' && isASCIIDigits(identifier)) {
			return false
		}
		for _, ch := range identifier {
			if !((ch >= '0' && ch <= '9') || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || ch == '-') {
				return false
			}
		}
	}
	return true
}
func isASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
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
	if len(av.prerelease) == 0 && len(bv.prerelease) == 0 {
		return 0
	}
	if len(av.prerelease) == 0 {
		return 1
	}
	if len(bv.prerelease) == 0 {
		return -1
	}
	for i := 0; i < len(av.prerelease) && i < len(bv.prerelease); i++ {
		aPart, bPart := av.prerelease[i], bv.prerelease[i]
		if aPart == bPart {
			continue
		}
		aNumeric, bNumeric := isASCIIDigits(aPart), isASCIIDigits(bPart)
		if aNumeric && bNumeric {
			if len(aPart) != len(bPart) {
				if len(aPart) < len(bPart) {
					return -1
				}
				return 1
			}
			return strings.Compare(aPart, bPart)
		}
		if aNumeric {
			return -1
		}
		if bNumeric {
			return 1
		}
		return strings.Compare(aPart, bPart)
	}
	if len(av.prerelease) < len(bv.prerelease) {
		return -1
	}
	if len(av.prerelease) > len(bv.prerelease) {
		return 1
	}
	return 0
}
func matchesRange(version, expr string, prerelease bool) bool {
	v, ok := parseVersion(version)
	if !ok || (!prerelease && len(v.prerelease) != 0) {
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
	_, expressionValid := parseVersion(expr)
	return expressionValid && compareVersions(version, expr) == 0
}
func validIdentity(value ArtifactIdentity) bool {
	_, versionOK := parseVersion(value.Version)
	return (value.Kind == ArtifactPack || value.Kind == ArtifactTemplate) && value.CanonicalID != "" && (value.Version == "latest" || versionOK)
}
func validReleaseIdentity(value ArtifactIdentity) bool {
	_, versionOK := parseVersion(value.Version)
	return (value.Kind == ArtifactPack || value.Kind == ArtifactTemplate) && value.CanonicalID != "" && versionOK
}
func immutableReleaseBinding(a, b ArtifactRelease) bool {
	return a.Identity == b.Identity && a.SourceID == b.SourceID && a.PublisherID == b.PublisherID && a.Digest == b.Digest && a.ManifestDigest == b.ManifestDigest && a.DependencyMetadataDigest == b.DependencyMetadataDigest && a.ProvenanceDigest == b.ProvenanceDigest && digestJSON(a.Dependencies) == digestJSON(b.Dependencies)
}
func mutationSemanticDigest(value ProjectMutationPlan) string {
	value.RegistryTransactionID = ""
	value.PlanDigest = ""
	value.HostOperationImpactDigest = ""
	value.EvaluationDigest = ""
	return digestJSON(value)
}
func decisionBindsMutation(decision accessprotocol.AuthoringDecision, impact semantic.AuthoringImpact, host accessprotocol.HostOperationImpact) bool {
	if decision.AuthoringImpactDigest == nil || string(*decision.AuthoringImpactDigest) != string(impact.ImpactDigest) || len(decision.HostOperationImpactDigests) != 1 || string(decision.HostOperationImpactDigests[0]) != string(host.ImpactDigest) {
		return false
	}
	required := map[string]bool{string(semantic.AuthoringCapabilityPackageManage): true}
	for _, capability := range impact.RequiredCapabilities {
		required[string(capability)] = true
	}
	for _, capability := range decision.RequiredCapabilities {
		delete(required, string(capability))
	}
	return len(required) == 0
}
func validAction(action Action) bool {
	switch action {
	case ActionInstall, ActionUpdate, ActionPin, ActionRemove, ActionRepair, ActionCreateFromTemplate:
		return true
	default:
		return false
	}
}
func findLocked(snapshot ProjectDependencySnapshot, identity ArtifactIdentity) (LockedArtifact, bool) {
	for _, item := range snapshot.Installs {
		if item.Identity.Kind == identity.Kind && item.Identity.CanonicalID == identity.CanonicalID {
			return item, true
		}
	}
	return LockedArtifact{}, false
}
func lockedFromPlan(item PlanArtifact, pinned bool, resolved map[string]PlanArtifact) (LockedArtifact, error) {
	dependencies := make([]ArtifactIdentity, 0, len(item.Release.Dependencies))
	for _, dependency := range item.Release.Dependencies {
		kind := dependency.Kind
		if kind == "" {
			kind = ArtifactPack
		}
		child, ok := resolved[artifactKey(ArtifactIdentity{Kind: kind, CanonicalID: dependency.CanonicalID})]
		if !ok {
			return LockedArtifact{}, errors.New("resolved dependency graph is incomplete")
		}
		dependencies = append(dependencies, child.Release.Identity)
	}
	return LockedArtifact{Identity: item.Release.Identity, SourceID: item.Release.SourceID, PublisherID: item.Release.PublisherID, Digest: item.Release.Digest, ProvenanceDigest: item.Release.ProvenanceDigest, DependencyMetadataDigest: item.Release.DependencyMetadataDigest, Dependencies: dependencies, Pinned: pinned}, nil
}
func lockedReleaseBindingMatches(locked LockedArtifact, release ArtifactRelease, resolved map[string]PlanArtifact) bool {
	expected, err := lockedFromPlan(PlanArtifact{Release: release}, locked.Pinned, resolved)
	return err == nil && lockedArtifactBindingMatches(locked, expected)
}
func validateRuntimeCommitResult(plan InstallPlan, input RuntimeCommitInput, result RuntimeCommitResult, expectedDocumentID string) error {
	if strings.TrimSpace(result.CommittedRevision) == "" || !validRuntimeOperationID(input.OperationID) || !validRuntimeOperationID(result.OperationResultID) || result.OperationResultID != input.OperationID {
		return errors.New("Runtime commit result is not bound to the requested operation and committed revision")
	}
	if expectedDocumentID == "" || result.DocumentID != expectedDocumentID {
		return errors.New("Runtime commit result is not bound to the expected Document")
	}
	if plan.CreatesNewDocument {
		if plan.NewDocumentID != expectedDocumentID || !result.InitialCommittedRevision {
			return errors.New("Runtime did not create the bound initial Document revision")
		}
	} else if result.InitialCommittedRevision {
		return errors.New("Runtime marked an existing Document update as its initial revision")
	}
	return nil
}
func persistedRuntimeCommitResult(tx Transaction, input RuntimeCommitInput, expectedDocumentID string) (RuntimeCommitResult, error) {
	result := RuntimeCommitResult{CommittedRevision: tx.CommittedRevision, OperationResultID: tx.OperationResultID, DocumentID: expectedDocumentID, InitialCommittedRevision: tx.Plan.CreatesNewDocument}
	if tx.RuntimeResult != nil {
		result = *tx.RuntimeResult
		if tx.CommittedRevision != result.CommittedRevision || tx.OperationResultID != result.OperationResultID {
			return RuntimeCommitResult{}, errors.New("persisted Runtime result does not match transaction convergence fields")
		}
	}
	if err := validateRuntimeCommitResult(tx.Plan, input, result, expectedDocumentID); err != nil {
		return RuntimeCommitResult{}, err
	}
	return result, nil
}
func planBoundDocumentID(plan InstallPlan) (string, error) {
	if len(plan.HostOperationImpacts) != 1 {
		return "", errors.New("plan requires exactly one bound host impact")
	}
	impact := plan.HostOperationImpacts[0]
	documentID := impact.ResourceScope.DocumentID
	if strings.TrimSpace(documentID) == "" || accesscore.ValidateHostOperationImpact(impact) != nil || string(impact.ImpactDigest) != plan.HostOperationImpactDigest {
		return "", errors.New("plan host impact is missing its bound Document identity")
	}
	if plan.CreatesNewDocument && plan.NewDocumentID != documentID {
		return "", errors.New("template plan Document identity does not match its host impact")
	}
	return documentID, nil
}
func validRuntimeOperationID(value string) bool {
	if len(value) == 0 || len(value) > 256 || !isASCIIAlphaNumeric(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		ch := value[index]
		if !isASCIIAlphaNumeric(ch) && ch != '.' && ch != '_' && ch != ':' && ch != '-' {
			return false
		}
	}
	return true
}
func isASCIIAlphaNumeric(value byte) bool {
	return (value >= '0' && value <= '9') || (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z')
}
func buildLockDelta(action Action, snapshot ProjectDependencySnapshot, resolved map[string]PlanArtifact, requested ArtifactIdentity, requestedPin bool) (ResolvedLockDelta, error) {
	delta := ResolvedLockDelta{Added: []LockedArtifact{}, Updated: []LockedArtifact{}, Removed: []LockedArtifact{}, Pinned: []LockedArtifact{}}
	if action == ActionRemove {
		if installed, ok := findLocked(snapshot, requested); ok {
			delta.Removed = append(delta.Removed, installed)
		}
		return delta, nil
	}
	keys := make([]string, 0, len(resolved))
	for key := range resolved {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		item := resolved[key]
		prior, installed := findLocked(snapshot, item.Release.Identity)
		isRequested := artifactKey(item.Release.Identity) == artifactKey(requested)
		pinned := installed && prior.Pinned
		if action == ActionPin && isRequested {
			pinned = true
		} else if isRequested && (action == ActionInstall || action == ActionUpdate) {
			pinned = requestedPin
		}
		lock, err := lockedFromPlan(item, pinned, resolved)
		if err != nil {
			return ResolvedLockDelta{}, err
		}
		if installed {
			if action == ActionPin && isRequested {
				delta.Pinned = append(delta.Pinned, lock)
			} else if !lockedArtifactBindingMatches(prior, lock) {
				delta.Updated = append(delta.Updated, lock)
			}
		} else {
			if action == ActionPin && isRequested {
				delta.Pinned = append(delta.Pinned, lock)
			} else {
				delta.Added = append(delta.Added, lock)
			}
		}
	}
	if action == ActionUpdate || action == ActionRepair {
		old := lockedDependencyClosure(snapshot, requested)
		for key, item := range old {
			if _, stillResolved := resolved[key]; !stillResolved && key != artifactKey(requested) {
				delta.Removed = append(delta.Removed, item)
			}
		}
		sort.Slice(delta.Removed, func(i, j int) bool {
			return artifactKey(delta.Removed[i].Identity) < artifactKey(delta.Removed[j].Identity)
		})
	}
	return delta, nil
}
func lockedArtifactBindingMatches(left LockedArtifact, right LockedArtifact) bool {
	if left.Identity != right.Identity || left.SourceID != right.SourceID || left.PublisherID != right.PublisherID || left.Digest != right.Digest || left.ProvenanceDigest != right.ProvenanceDigest || left.DependencyMetadataDigest != right.DependencyMetadataDigest || left.Pinned != right.Pinned || len(left.Dependencies) != len(right.Dependencies) {
		return false
	}
	for index := range left.Dependencies {
		if left.Dependencies[index] != right.Dependencies[index] {
			return false
		}
	}
	return true
}
func artifactKey(identity ArtifactIdentity) string {
	return string(identity.Kind) + ":" + identity.CanonicalID
}
func lockedDependencyClosure(snapshot ProjectDependencySnapshot, root ArtifactIdentity) map[string]LockedArtifact {
	byKey := map[string]LockedArtifact{}
	for _, item := range snapshot.Installs {
		byKey[artifactKey(item.Identity)] = item
	}
	out := map[string]LockedArtifact{}
	var visit func(ArtifactIdentity)
	visit = func(identity ArtifactIdentity) {
		key := artifactKey(identity)
		if _, seen := out[key]; seen {
			return
		}
		item, ok := byKey[key]
		if !ok {
			return
		}
		out[key] = item
		for _, dependency := range item.Dependencies {
			visit(dependency)
		}
	}
	visit(root)
	return out
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
	sort.Strings(out)
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
	return cloneJSONValue(value)
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
func digestPlan(plan InstallPlan) string {
	plan.PlanDigest = ""
	plan.ProjectMutationPlan.PlanDigest = ""
	return digestJSON(plan)
}
func validateFinalizedPlan(plan InstallPlan) error {
	if plan.PlanDigest == "" || plan.ProjectMutationPlan.PlanDigest != plan.PlanDigest || digestPlan(plan) != plan.PlanDigest {
		return errors.New("registry finalized plan digest does not bind its complete body")
	}
	mutation := plan.ProjectMutationPlan
	if mutation.RegistryTransactionID != plan.TransactionID || mutation.HostOperationImpactDigest != plan.HostOperationImpactDigest || mutation.EvaluationDigest != plan.EvaluationDigest || mutation.MutationDigest != plan.MutationDigest {
		return errors.New("registry finalized plan contains inconsistent owner bindings")
	}
	if mutation.BaseProjectRevision != plan.BaseRevision || plan.RollbackCheckpoint.BaseProjectRevision != plan.BaseRevision || mutation.ExpectedDefinitionHash != plan.ExpectedDefinitionHash || plan.RollbackCheckpoint.BaseDefinitionHash != plan.ExpectedDefinitionHash || mutation.ExpectedResolvedLockDigest != plan.ExpectedResolvedLockDigest || plan.DependencySnapshot.ResolvedLockDigest != plan.ExpectedResolvedLockDigest || plan.RollbackCheckpoint.BaseResolvedLockDigest != plan.ExpectedResolvedLockDigest || !reflect.DeepEqual(mutation.ResolvedLockDelta, plan.ResolvedLockDelta) {
		return errors.New("registry finalized plan contains inconsistent project preconditions")
	}
	if plan.AuthoringImpact == nil || !reflect.DeepEqual(*plan.AuthoringImpact, mutation.AuthoringImpact) || len(plan.AuthoringImpactDigests) != 1 || plan.AuthoringImpactDigests[0] != mutation.AuthoringImpactDigest || mutation.AuthoringImpactDigest != string(mutation.AuthoringImpact.ImpactDigest) || string(plan.AuthoringImpact.ImpactDigest) != mutation.AuthoringImpactDigest || string(plan.AuthoringImpact.BaseDefinitionHash) != plan.ExpectedDefinitionHash {
		return errors.New("registry finalized plan contains inconsistent authoring impact bindings")
	}
	if err := accesscore.ValidateAuthoringDecisionBindings(plan.AccessDecision, plan.AuthoringImpact, plan.HostOperationImpacts); err != nil {
		return err
	}
	if plan.AccessDecision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow || string(plan.AccessDecision.EvaluationDigest) != plan.EvaluationDigest || len(plan.AccessDecision.MissingCapabilities) != 0 || len(plan.AccessDecision.ConstraintViolations) != 0 || len(plan.AccessDecision.ApprovalRuleRefs) != 0 || !reflect.DeepEqual(plan.RequiredCapabilities, capabilitiesToStrings(plan.AccessDecision.RequiredCapabilities)) {
		return errors.New("registry finalized plan contains an inconsistent Access decision")
	}
	if _, err := planBoundDocumentID(plan); err != nil {
		return err
	}
	impact := plan.HostOperationImpacts[0]
	if impact.OperationKind != accessprotocol.HostOperationKindPackageTransaction || impact.Action != hostImpactAction(plan.Action) || !reflect.DeepEqual(impact.ResourceRefs, []string{plan.RequestedRoot.CanonicalID}) {
		return errors.New("registry finalized plan host impact does not bind its action and requested root")
	}
	return nil
}
func transactionRequiresFinalizedPlan(tx Transaction) bool {
	for _, event := range tx.Events {
		switch event.State {
		case StateVerified, StateExpandedStaged, StateCompiled, StateAwaitingConfirmation, StateApplying, StateRepairRequired, StateRepairing, StateCommitted, StateSuperseded, StateNeedsReview:
			return true
		}
	}
	return transactionState(tx) == StateRolledBack && planHasFinalizedBody(tx.Plan)
}
func planHasFinalizedBody(plan InstallPlan) bool {
	return plan.MutationDigest != "" || plan.HostOperationImpactDigest != "" || plan.EvaluationDigest != "" || plan.AuthoringImpact != nil || len(plan.HostOperationImpacts) > 0 || !reflect.DeepEqual(plan.ProjectMutationPlan, ProjectMutationPlan{})
}
func validateStoredTransaction(tx Transaction) error {
	if transactionRequiresFinalizedPlan(tx) || planHasFinalizedBody(tx.Plan) {
		return validateFinalizedPlan(tx.Plan)
	}
	return nil
}
func digestPlanningRequest(request PlanRequest) string {
	request.DependencySnapshot = cloneDependencySnapshot(request.DependencySnapshot)
	return digestJSON(request)
}
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
	return cloneJSONValue(value)
}
func clonePlan(value InstallPlan) InstallPlan {
	return cloneJSONValue(value)
}
func cloneTransaction(value Transaction) Transaction {
	return cloneJSONValue(value)
}
func cloneLockDelta(value ResolvedLockDelta) ResolvedLockDelta {
	return cloneJSONValue(value)
}
func cloneAuthoringImpact(value *semantic.AuthoringImpact) *semantic.AuthoringImpact {
	if value == nil {
		return nil
	}
	cloned := cloneJSONValue(*value)
	return &cloned
}
func cloneHostImpacts(value []accessprotocol.HostOperationImpact) []accessprotocol.HostOperationImpact {
	return cloneJSONValue(value)
}
func cloneAccessDecision(value accessprotocol.AuthoringDecision) accessprotocol.AuthoringDecision {
	return cloneJSONValue(value)
}
func cloneJSONValue[T any](value T) T {
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("registry clone marshal: %v", err))
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		panic(fmt.Sprintf("registry clone unmarshal: %v", err))
	}
	return out
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
	if current.Plan.TransactionID == "" || next.Plan.TransactionID != current.Plan.TransactionID || len(next.Events) != len(current.Events)+1 {
		return errors.New("registry transaction CAS must append exactly one event")
	}
	if transactionVersion(next) != transactionVersion(current)+1 {
		return errors.New("registry transaction sequence mismatch")
	}
	if err := validateStoredTransaction(current); err != nil {
		return fmt.Errorf("registry transaction current record is corrupt: %w", err)
	}
	if err := validateStoredTransaction(next); err != nil {
		return fmt.Errorf("registry transaction next record is corrupt: %w", err)
	}
	from, to := transactionState(current), transactionState(next)
	planFinalized := from == StateDownloading && to == StateVerified && current.PlanningRequest != nil && next.PlanningRequest != nil && digestPlanningRequest(*current.PlanningRequest) == digestPlanningRequest(*next.PlanningRequest) && current.Plan.PlanDigest != next.Plan.PlanDigest && next.Plan.PlanDigest == digestPlan(next.Plan)
	if current.Plan.PlanDigest != next.Plan.PlanDigest && !planFinalized {
		return fmt.Errorf("registry transaction plan changed outside finalization (%s -> %s, request_bound=%t, digest_bound=%t)", from, to, current.PlanningRequest != nil && next.PlanningRequest != nil && digestPlanningRequest(*current.PlanningRequest) == digestPlanningRequest(*next.PlanningRequest), next.Plan.PlanDigest == digestPlan(next.Plan))
	}
	allowed := false
	switch from {
	case StatePlanned:
		allowed = to == StateDownloading || to == StateRolledBack
	case StateDownloading:
		allowed = to == StateVerified || to == StateRolledBack
	case StateVerified:
		allowed = to == StateExpandedStaged || to == StateRolledBack
	case StateExpandedStaged:
		allowed = to == StateCompiled || to == StateRolledBack
	case StateCompiled:
		allowed = to == StateAwaitingConfirmation || to == StateRolledBack
	case StateAwaitingConfirmation:
		allowed = to == StateApplying || to == StateRolledBack
	case StateApplying:
		allowed = to == StateCommitted || to == StateRolledBack || to == StateRepairRequired
	case StateRepairRequired:
		allowed = to == StateRepairing || to == StateNeedsReview
	case StateRepairing:
		allowed = to == StateCommitted || to == StateSuperseded || to == StateNeedsReview
	}
	if !allowed {
		return fmt.Errorf("invalid registry transaction transition %s -> %s", from, to)
	}
	return nil
}
