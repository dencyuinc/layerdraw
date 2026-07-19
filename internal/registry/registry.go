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

type ArtifactRelease struct {
	Identity         ArtifactIdentity        `json:"identity"`
	SourceID         string                  `json:"source_id"`
	PublisherID      string                  `json:"publisher_id"`
	Digest           string                  `json:"digest"`
	Size             int64                   `json:"size"`
	Dependencies     []Dependency            `json:"dependencies"`
	Compatibility    []CompatibilityDecision `json:"compatibility"`
	Signature        *SignatureEnvelope      `json:"signature,omitempty"`
	License          string                  `json:"license"`
	ProvenanceDigest string                  `json:"provenance_digest"`
}

type CompatibilityDecision struct {
	Subject     string   `json:"subject"`
	Required    string   `json:"required"`
	Available   string   `json:"available"`
	Status      string   `json:"status"`
	Diagnostics []string `json:"diagnostics"`
}

type SearchInput struct {
	Query             string
	Kind              *ArtifactKind
	IncludePrerelease bool
}
type SourceClient interface {
	Search(context.Context, RegistrySource, SearchInput) ([]ArtifactRelease, error)
	Download(context.Context, RegistrySource, ArtifactRelease) ([]byte, error)
}

type ValidatedArtifact struct {
	Identity              ArtifactIdentity
	CanonicalDigest       string
	StagedTreeManifest    string
	ResolvedLockDigest    string
	MutationDigest        string
	AuthoringImpactDigest string
	Diagnostics           []string
}

// PackageValidator is implemented by the Go Engine package facade. Registry
// never parses ZIP entries, LDL, resolved trees, or package manifests itself.
type PackageValidator interface {
	ValidateRegistryArtifact(context.Context, ArtifactRelease, []byte) (ValidatedArtifact, error)
}

type AuthorArtifactRequest struct {
	Kind        ArtifactKind
	ProjectID   string
	OutputName  string
	PublisherID string
	Version     string
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
	Action                     Action
	ProjectID                  string
	BaseRevision               string
	ExpectedDefinitionHash     string
	ExpectedResolvedLockDigest string
	Requested                  ArtifactIdentity
	IncludePrerelease          bool
	HostCapabilities           []string
}

type PlanArtifact struct {
	Release    ArtifactRelease   `json:"release"`
	Validation ValidatedArtifact `json:"validation"`
}

type InstallPlan struct {
	TransactionID              string         `json:"transaction_id"`
	PlanDigest                 string         `json:"plan_digest"`
	Action                     Action         `json:"action"`
	ProjectID                  string         `json:"project_id"`
	BaseRevision               string         `json:"base_revision"`
	ExpectedDefinitionHash     string         `json:"expected_definition_hash"`
	ExpectedResolvedLockDigest string         `json:"expected_resolved_lock_digest"`
	Artifacts                  []PlanArtifact `json:"artifacts"`
	RequiredCapabilities       []string       `json:"required_capabilities"`
	TrustPolicyDigests         []string       `json:"trust_policy_digests"`
	MutationDigest             string         `json:"mutation_digest"`
	AuthoringImpactDigests     []string       `json:"authoring_impact_digests"`
	HostOperationImpactDigest  string         `json:"host_operation_impact_digest"`
	EvaluationDigest           string         `json:"evaluation_digest"`
}

type RuntimeCommitInput struct {
	Plan                                                                                           InstallPlan
	OperationID, IdempotencyKey, CurrentRevision, CurrentDefinitionHash, CurrentResolvedLockDigest string
}
type RuntimeCommitResult struct {
	CommittedRevision string
	OperationResultID string
}
type RuntimePort interface {
	CommitRegistryPlan(context.Context, RuntimeCommitInput) (RuntimeCommitResult, error)
}
type AccessPort interface {
	PreviewRegistryPlan(context.Context, InstallPlan) error
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
}
type Transaction struct {
	Plan              InstallPlan        `json:"plan"`
	Events            []TransactionEvent `json:"events"`
	CommittedRevision string             `json:"committed_revision,omitempty"`
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
	transactions map[string]*Transaction
}

func New(validator PackageValidator, access AccessPort, runtime RuntimePort) (*Registry, error) {
	if validator == nil || access == nil || runtime == nil {
		return nil, errors.New("registry requires Engine validator, Access, and Runtime ports")
	}
	return &Registry{sources: map[string]RegistrySource{}, policies: map[string]TrustPolicy{}, clients: map[SourceKind]SourceClient{}, validator: validator, access: access, runtime: runtime, transactions: map[string]*Transaction{}}, nil
}

func (r *Registry) RegisterClient(kind SourceKind, client SourceClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[kind] = client
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
func (r *Registry) SetConnected(sourceID string, connected bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	source, ok := r.sources[sourceID]
	if !ok {
		return fail(FailureUnavailable, sourceID, true, nil)
	}
	source.Connected = connected
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
			if release.SourceID != source.SourceID || !validIdentity(release.Identity) {
				continue
			}
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
	if !validIdentity(request.Requested) || request.ProjectID == "" || request.BaseRevision == "" {
		return InstallPlan{}, fail(FailureDependencyConflict, request.Requested.CanonicalID, true, nil)
	}
	if request.Action == ActionCreateFromTemplate && request.Requested.Kind != ArtifactTemplate {
		return InstallPlan{}, fail(FailureUnsupportedFormat, request.Requested.CanonicalID, true, nil)
	}
	resolved := map[string]PlanArtifact{}
	visiting := map[string]bool{}
	if err := r.resolve(ctx, request.Requested, request.IncludePrerelease, resolved, visiting); err != nil {
		return InstallPlan{}, err
	}
	keys := make([]string, 0, len(resolved))
	for key := range resolved {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	artifacts := make([]PlanArtifact, 0, len(keys))
	impacts := []string{}
	trust := []string{}
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
	}
	sort.Strings(impacts)
	sort.Strings(trust)
	hostImpact := digestJSON(struct {
		Action    Action
		ProjectID string
	}{request.Action, request.ProjectID})
	mutation := digestJSON(artifacts)
	evaluation := digestJSON(struct {
		Mutation  string
		Authoring []string
		Host      string
		Base      string
	}{mutation, impacts, hostImpact, request.BaseRevision})
	plan := InstallPlan{Action: request.Action, ProjectID: request.ProjectID, BaseRevision: request.BaseRevision, ExpectedDefinitionHash: request.ExpectedDefinitionHash, ExpectedResolvedLockDigest: request.ExpectedResolvedLockDigest, Artifacts: artifacts, RequiredCapabilities: []string{"package:manage"}, TrustPolicyDigests: trust, MutationDigest: mutation, AuthoringImpactDigests: impacts, HostOperationImpactDigest: hostImpact, EvaluationDigest: evaluation}
	if err := r.access.PreviewRegistryPlan(ctx, plan); err != nil {
		return InstallPlan{}, fail(FailurePolicyDenied, request.ProjectID, true, err)
	}
	plan.TransactionID = strings.TrimPrefix(digestJSON(struct{ Project, Evaluation string }{request.ProjectID, evaluation}), "sha256:")[:32]
	plan.PlanDigest = digestPlan(plan)
	r.mu.Lock()
	r.transactions[plan.TransactionID] = &Transaction{Plan: plan, Events: []TransactionEvent{{State: StatePlanned, EvidenceDigest: plan.PlanDigest}, {State: StateVerified, EvidenceDigest: mutation}, {State: StateAwaitingConfirmation, EvidenceDigest: evaluation}}}
	r.mu.Unlock()
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
	if err := verifyTrust(source, policy, *selected); err != nil {
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
	r.mu.Lock()
	tx, ok := r.transactions[input.Plan.TransactionID]
	if !ok || tx.Plan.PlanDigest != input.Plan.PlanDigest || digestPlan(input.Plan) != input.Plan.PlanDigest {
		r.mu.Unlock()
		return RuntimeCommitResult{}, fail(FailurePlanStale, input.Plan.TransactionID, true, nil)
	}
	if input.CurrentRevision != tx.Plan.BaseRevision || input.CurrentDefinitionHash != tx.Plan.ExpectedDefinitionHash || input.CurrentResolvedLockDigest != tx.Plan.ExpectedResolvedLockDigest {
		r.mu.Unlock()
		return RuntimeCommitResult{}, fail(FailurePlanStale, input.Plan.TransactionID, true, nil)
	}
	tx.Events = append(tx.Events, TransactionEvent{State: StateApplying, EvidenceDigest: tx.Plan.EvaluationDigest})
	r.mu.Unlock()
	result, err := r.runtime.CommitRegistryPlan(ctx, input)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		tx.Events = append(tx.Events, TransactionEvent{State: StateRepairRequired, EvidenceDigest: tx.Plan.MutationDigest})
		return RuntimeCommitResult{}, fail(FailureRepairRequired, input.Plan.TransactionID, true, err)
	}
	tx.CommittedRevision = result.CommittedRevision
	tx.Events = append(tx.Events, TransactionEvent{State: StateCommitted, EvidenceDigest: digestJSON(result)})
	return result, nil
}

func (r *Registry) Transaction(id string) (Transaction, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tx, ok := r.transactions[id]
	if !ok {
		return Transaction{}, false
	}
	out := *tx
	out.Plan = clonePlan(tx.Plan)
	out.Events = append([]TransactionEvent{}, tx.Events...)
	return out, true
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

func verifyTrust(source RegistrySource, policy TrustPolicy, release ArtifactRelease) error {
	if policy.TrustedPublishers != nil && !policy.TrustedPublishers[release.PublisherID] {
		return fail(FailurePolicyDenied, release.PublisherID, true, nil)
	}
	if release.Signature == nil {
		if source.Kind == SourceLocalDirectory && policy.AllowUnsignedLocal {
			return nil
		}
		if policy.RequiredSignature {
			return fail(FailureSignatureMissing, release.Identity.CanonicalID, true, nil)
		}
		return nil
	}
	envelope := release.Signature
	if policy.RevokedKeys[envelope.KeyID] {
		return fail(FailureSignatureRevoked, envelope.KeyID, true, nil)
	}
	key := policy.PublicKeys[envelope.KeyID]
	if len(key) != ed25519.PublicKeySize || envelope.Profile != "ed25519" || !ed25519.Verify(key, envelope.Statement, envelope.Signature) {
		return fail(FailureSignatureInvalid, release.Identity.CanonicalID, true, nil)
	}
	var statement struct {
		Identity                    ArtifactIdentity `json:"artifact_identity"`
		ArtifactDigest, PublisherID string
	}
	if json.Unmarshal(envelope.Statement, &statement) != nil || statement.Identity != release.Identity || statement.ArtifactDigest != release.Digest || statement.PublisherID != release.PublisherID {
		return fail(FailureSignatureInvalid, release.Identity.CanonicalID, true, nil)
	}
	return nil
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
		base, ok := parseVersion(strings.TrimPrefix(expr, "^"))
		return ok && compareVersions(version, strings.TrimPrefix(expr, "^")) >= 0 && v.major == base.major
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
	for key := range policy.PublicKeys {
		keys = append(keys, key)
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
	return digestJSON(struct {
		ID                        string
		Required, Unsigned        bool
		Keys, Publishers, Revoked []string
	}{policy.PolicyID, policy.RequiredSignature, policy.AllowUnsignedLocal, keys, publishers, revoked})
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
