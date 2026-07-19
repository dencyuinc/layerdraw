// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type StateInputPolicy string

const (
	StateInputPolicyNone     StateInputPolicy = "none"
	StateInputPolicyOptional StateInputPolicy = "optional"
	StateInputPolicyRequired StateInputPolicy = "required"
)

type StateSnapshotErrorCode string

const (
	StateSnapshotBindingInvalid       StateSnapshotErrorCode = "runtime.state_binding_invalid"
	StateSnapshotBackendUnavailable   StateSnapshotErrorCode = "runtime.state_backend_unavailable"
	StateSnapshotAuthorizationInvalid StateSnapshotErrorCode = "runtime.state_authorization_invalid"
	StateSnapshotInvalid              StateSnapshotErrorCode = "runtime.state_snapshot_invalid"
)

type StateSnapshotError struct {
	Code StateSnapshotErrorCode
}

func (e *StateSnapshotError) Error() string { return string(e.Code) }

func stateSnapshotError(code StateSnapshotErrorCode) *StateSnapshotError {
	return &StateSnapshotError{Code: code}
}

type StateAddressMove struct {
	SourceAddress semantic.StableAddress
	TargetAddress semantic.StableAddress
}

// StateQueryDefinition is trusted Engine output for one fixed definition.
// AddressMoves is the Engine-computed move closure, never a Runtime parse of LDL.
type StateQueryDefinition struct {
	ProjectAddress semantic.ProjectRootAddress
	DefinitionHash protocolcommon.Digest
	GraphHash      protocolcommon.Digest
	SubjectHashes  []semantic.SubjectHash
	AddressMoves   []StateAddressMove
}

type BuildStateQueryInput struct {
	Scope      runtimeprotocol.RuntimeScope
	Binding    port.BackendBinding
	Policy     StateInputPolicy
	Definition StateQueryDefinition
}

type StateSubjectClassification string

const (
	StateSubjectMatching     StateSubjectClassification = "matching"
	StateSubjectMissing      StateSubjectClassification = "missing"
	StateSubjectStale        StateSubjectClassification = "stale"
	StateSubjectOrphaned     StateSubjectClassification = "orphaned"
	StateSubjectInaccessible StateSubjectClassification = "inaccessible"
	StateSubjectRedacted     StateSubjectClassification = "redacted"
)

type StateRecordFreshness string

const (
	StateRecordFreshnessCurrent StateRecordFreshness = "current"
	StateRecordFreshnessStale   StateRecordFreshness = "stale"
	StateRecordFreshnessUnknown StateRecordFreshness = "unknown"
)

// CanonicalStateRecord is the deterministic Runtime assessment of one durable
// state subject. ProviderFields are deliberately absent from this projection.
type CanonicalStateRecord struct {
	SourceAddress          semantic.StableAddress
	SubjectAddress         semantic.StableAddress
	SubjectKind            semantic.StateSubjectKind
	StateVersion           string
	DefinitionHash         protocolcommon.Digest
	GraphHash              protocolcommon.Digest
	OwnSubjectHash         protocolcommon.Digest
	Fields                 map[string]semantic.RecipeScalar
	Freshness              StateRecordFreshness
	Classifications        []StateSubjectClassification
	InaccessibleFieldPaths []semantic.StateFieldPath
	RedactedFieldPaths     []semantic.StateFieldPath
	Tombstoned             bool
}

type ReconcileActionKind string

const (
	ReconcileRemapState     ReconcileActionKind = "remap_state"
	ReconcileRefreshState   ReconcileActionKind = "refresh_state"
	ReconcileTombstoneState ReconcileActionKind = "tombstone_state"
	ReconcileArchiveOrphan  ReconcileActionKind = "archive_orphan"
	ReconcileManualReview   ReconcileActionKind = "manual_review"
)

type StateReconcileAction struct {
	Kind           ReconcileActionKind     `json:"kind"`
	SourceAddress  *semantic.StableAddress `json:"source_address,omitempty"`
	SubjectAddress semantic.StableAddress  `json:"subject_address"`
}

// StateReconciliationPlan is preview-only. There is intentionally no apply
// method on this surface and construction never changes backend or LDL state.
type StateReconciliationPlan struct {
	Actions    []StateReconcileAction
	PlanDigest protocolcommon.Digest
}

type BuiltStateQueryInput struct {
	StateInput                  runtimeprotocol.StateInput
	StateInputRef               StateInputRef
	Snapshot                    *ImmutableStateQuerySnapshot
	Records                     []CanonicalStateRecord
	ReconciliationPlan          StateReconciliationPlan
	AuthorizationDecisionDigest *protocolcommon.Digest
}

type StateInputRef struct {
	CapturedAt     *protocolcommon.Rfc3339Time
	DefinitionHash *protocolcommon.Digest
	Kind           string
	SnapshotHash   *protocolcommon.Digest
	StateVersion   *string
}

// ImmutableStateQuerySnapshot retains canonical bytes and a fixed hash while
// returning defensive copies of every mutable Go value.
type ImmutableStateQuerySnapshot struct {
	snapshot      semantic.StateQuerySnapshot
	canonicalJSON []byte
	hash          protocolcommon.Digest
}

func (s *ImmutableStateQuerySnapshot) Snapshot() semantic.StateQuerySnapshot {
	if s == nil {
		return semantic.StateQuerySnapshot{}
	}
	return cloneSemanticSnapshot(s.snapshot)
}

func (s *ImmutableStateQuerySnapshot) CanonicalJSON() []byte {
	if s == nil {
		return nil
	}
	return append([]byte(nil), s.canonicalJSON...)
}

func (s *ImmutableStateQuerySnapshot) Hash() protocolcommon.Digest {
	if s == nil {
		return ""
	}
	return s.hash
}

func StateFieldRegistry() []semantic.StateFieldPath {
	return engineendpoint.StateFieldRegistry()
}

func (r *Runtime) BuildStateQueryInput(ctx context.Context, input BuildStateQueryInput) (BuiltStateQueryInput, error) {
	if err := validateStateQueryBuildInput(input); err != nil {
		return BuiltStateQueryInput{}, err
	}
	if input.Policy == StateInputPolicyNone || input.Binding.Kind == port.BackendBindingNone {
		return noStateQueryInput()
	}
	if r.config.Ports.StateBindings == nil || r.config.Ports.StateAccess == nil {
		return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotBackendUnavailable)
	}
	backend, err := r.config.Ports.StateBindings.ResolveStateBackend(ctx, port.ResolveStateBackendInput{Scope: input.Scope, Binding: input.Binding})
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return noStateQueryInput()
		}
		return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotBackendUnavailable)
	}
	if backend == nil {
		return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotBackendUnavailable)
	}
	head, err := backend.GetHead(ctx, port.GetStateHeadInput{Scope: input.Scope})
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return noStateQueryInput()
		}
		return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotBackendUnavailable)
	}
	if !validStateHead(head) {
		return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotInvalid)
	}
	state, err := backend.ReadState(ctx, port.ReadStateInput{Scope: input.Scope, ExpectedStateVersion: &head.StateVersion})
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return noStateQueryInput()
		}
		return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotBackendUnavailable)
	}
	if !reflect.DeepEqual(state.Head, head) {
		return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotInvalid)
	}
	return r.projectStateQuerySnapshot(ctx, input, state)
}

func noStateQueryInput() (BuiltStateQueryInput, error) {
	plan, err := reconciliationPlan(nil)
	if err != nil {
		return BuiltStateQueryInput{}, err
	}
	return BuiltStateQueryInput{
		StateInput:    runtimeprotocol.StateInput{Kind: "none"},
		StateInputRef: StateInputRef{Kind: "none"},
		Records:       []CanonicalStateRecord{}, ReconciliationPlan: plan,
	}, nil
}

func validateStateQueryBuildInput(input BuildStateQueryInput) error {
	if _, err := runtimeprotocol.EncodeRuntimeScope(input.Scope); err != nil {
		return stateSnapshotError(StateSnapshotBindingInvalid)
	}
	switch input.Policy {
	case StateInputPolicyNone, StateInputPolicyOptional, StateInputPolicyRequired:
	default:
		return stateSnapshotError(StateSnapshotBindingInvalid)
	}
	switch input.Binding.Kind {
	case port.BackendBindingNone:
		if input.Binding.BindingID != "" {
			return stateSnapshotError(StateSnapshotBindingInvalid)
		}
	case port.BackendBindingLocal, port.BackendBindingPackaged:
		if input.Binding.BindingID == "" {
			return stateSnapshotError(StateSnapshotBindingInvalid)
		}
	default:
		return stateSnapshotError(StateSnapshotBindingInvalid)
	}
	if _, err := semantic.EncodeProjectRootAddress(input.Definition.ProjectAddress); err != nil {
		return stateSnapshotError(StateSnapshotInvalid)
	}
	if _, err := protocolcommon.EncodeDigest(input.Definition.DefinitionHash); err != nil {
		return stateSnapshotError(StateSnapshotInvalid)
	}
	if _, err := protocolcommon.EncodeDigest(input.Definition.GraphHash); err != nil {
		return stateSnapshotError(StateSnapshotInvalid)
	}
	seen := map[semantic.StableAddress]bool{}
	for _, subject := range input.Definition.SubjectHashes {
		_, ok := stateSubjectKind(subject.Kind)
		if !ok {
			continue
		}
		if seen[subject.Address] {
			return stateSnapshotError(StateSnapshotInvalid)
		}
		seen[subject.Address] = true
		if _, err := semantic.EncodeStableAddress(subject.Address); err != nil {
			return stateSnapshotError(StateSnapshotInvalid)
		}
		if _, err := protocolcommon.EncodeDigest(subject.Hash); err != nil {
			return stateSnapshotError(StateSnapshotInvalid)
		}
	}
	moveSources := map[semantic.StableAddress]bool{}
	for _, move := range input.Definition.AddressMoves {
		if moveSources[move.SourceAddress] || move.SourceAddress == move.TargetAddress {
			return stateSnapshotError(StateSnapshotInvalid)
		}
		moveSources[move.SourceAddress] = true
		if _, err := semantic.EncodeStableAddress(move.SourceAddress); err != nil {
			return stateSnapshotError(StateSnapshotInvalid)
		}
		if _, err := semantic.EncodeStableAddress(move.TargetAddress); err != nil || !seen[move.TargetAddress] {
			return stateSnapshotError(StateSnapshotInvalid)
		}
	}
	return nil
}

func stateSubjectKind(kind semantic.SubjectKind) (semantic.StateSubjectKind, bool) {
	switch kind {
	case semantic.SubjectKindEntity:
		return semantic.StateSubjectKindEntity, true
	case semantic.SubjectKindRelation:
		return semantic.StateSubjectKindRelation, true
	case semantic.SubjectKindEntityRow:
		return semantic.StateSubjectKindEntityRow, true
	case semantic.SubjectKindRelationRow:
		return semantic.StateSubjectKindRelationRow, true
	default:
		return "", false
	}
}

type activeStateSubject struct {
	kind semantic.StateSubjectKind
	hash protocolcommon.Digest
}

func (r *Runtime) projectStateQuerySnapshot(ctx context.Context, input BuildStateQueryInput, state port.StateSnapshot) (BuiltStateQueryInput, error) {
	active := map[semantic.StableAddress]activeStateSubject{}
	for _, subject := range input.Definition.SubjectHashes {
		kind, ok := stateSubjectKind(subject.Kind)
		if ok {
			active[subject.Address] = activeStateSubject{kind: kind, hash: subject.Hash}
		}
	}
	moves := map[semantic.StableAddress]semantic.StableAddress{}
	for _, move := range input.Definition.AddressMoves {
		moves[move.SourceAddress] = move.TargetAddress
	}
	validatedSources := map[semantic.StableAddress]bool{}
	for _, record := range state.Records {
		if validatedSources[record.SubjectAddress] || !validStateRecord(record, state.Head) {
			return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotInvalid)
		}
		validatedSources[record.SubjectAddress] = true
	}
	authorizationSubjects := make(map[semantic.StableAddress]semantic.StateSubjectKind, len(active)+len(state.Records))
	for address, subject := range active {
		authorizationSubjects[address] = subject.kind
	}
	for _, record := range state.Records {
		address := record.SubjectAddress
		if target, ok := moves[address]; ok {
			address = target
		}
		if _, isActive := active[address]; !isActive {
			authorizationSubjects[address] = record.SubjectKind
		}
	}
	authSubjects := make([]port.StateQueryAuthorizationSubject, 0, len(authorizationSubjects))
	for address, kind := range authorizationSubjects {
		authSubjects = append(authSubjects, port.StateQueryAuthorizationSubject{SubjectAddress: address, SubjectKind: kind})
	}
	sort.Slice(authSubjects, func(i, j int) bool {
		return engineendpoint.CompareStableAddresses(authSubjects[i].SubjectAddress, authSubjects[j].SubjectAddress) < 0
	})
	decision, err := r.config.Ports.StateAccess.EvaluateStateQuery(ctx, port.StateQueryAuthorizationInput{
		Scope: input.Scope, DefinitionProjectAddress: input.Definition.ProjectAddress,
		DefinitionHash: input.Definition.DefinitionHash, GraphHash: input.Definition.GraphHash,
		FieldPaths: StateFieldRegistry(), Subjects: authSubjects,
	})
	if err != nil || decision.AccessFingerprint != input.Scope.AccessFingerprint {
		return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotAuthorizationInvalid)
	}
	if _, err := protocolcommon.EncodeDigest(decision.DecisionDigest); err != nil {
		return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotAuthorizationInvalid)
	}
	inaccessible, err := canonicalStatePaths(append(append([]semantic.StateFieldPath(nil), state.InaccessibleFieldPaths...), decision.InaccessibleFieldPaths...))
	if err != nil {
		return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotAuthorizationInvalid)
	}
	inaccessibleSet := pathSet(inaccessible)
	allowedSubjects := map[semantic.StableAddress]bool{}
	for _, subject := range authSubjects {
		allowedSubjects[subject.SubjectAddress] = true
	}
	for address, paths := range decision.RedactedFieldPaths {
		if !allowedSubjects[address] {
			return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotAuthorizationInvalid)
		}
		if _, err := canonicalStatePaths(paths); err != nil {
			return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotAuthorizationInvalid)
		}
	}

	seenActive := map[semantic.StableAddress]bool{}
	seenSnapshot := map[semantic.StableAddress]bool{}
	records := make([]CanonicalStateRecord, 0, len(state.Records)+len(active))
	subjects := make([]semantic.StateQuerySubject, 0, len(state.Records))
	for _, raw := range state.Records {
		address := raw.SubjectAddress
		if target, ok := moves[address]; ok {
			address = target
		}
		if _, err := semantic.EncodeStableAddress(address); err != nil {
			return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotInvalid)
		}
		redacted, err := canonicalStatePaths(append(append([]semantic.StateFieldPath(nil), raw.RedactedFieldPaths...), decision.RedactedFieldPaths[address]...))
		if err != nil {
			return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotInvalid)
		}
		redacted = excludeStatePaths(redacted, inaccessibleSet)
		redactedSet := pathSet(redacted)
		fields := make(map[string]semantic.RecipeScalar)
		recordInaccessible := make([]semantic.StateFieldPath, 0)
		for _, path := range StateFieldRegistry() {
			value, present := raw.Fields[string(path)]
			if !present {
				continue
			}
			if inaccessibleSet[path] {
				recordInaccessible = append(recordInaccessible, path)
				continue
			}
			if redactedSet[path] {
				continue
			}
			fields[string(path)] = cloneRecipeScalar(value)
		}
		classifications := make([]StateSubjectClassification, 0, 3)
		freshness := StateRecordFreshnessUnknown
		current, activeRecord := active[address]
		if raw.Tombstoned || !activeRecord || current.kind != raw.SubjectKind {
			classifications = append(classifications, StateSubjectOrphaned)
		} else if current.hash == raw.OwnSubjectHash {
			classifications = append(classifications, StateSubjectMatching)
			freshness = StateRecordFreshnessCurrent
			seenActive[address] = true
		} else {
			classifications = append(classifications, StateSubjectStale)
			freshness = StateRecordFreshnessStale
			seenActive[address] = true
		}
		if len(recordInaccessible) != 0 {
			classifications = append(classifications, StateSubjectInaccessible)
		}
		if len(redacted) != 0 {
			classifications = append(classifications, StateSubjectRedacted)
		}
		records = append(records, CanonicalStateRecord{
			SourceAddress: raw.SubjectAddress, SubjectAddress: address, SubjectKind: raw.SubjectKind,
			StateVersion: string(state.Head.StateVersion), DefinitionHash: state.Head.DefinitionHash, GraphHash: state.Head.GraphHash,
			OwnSubjectHash: raw.OwnSubjectHash, Fields: fields, Freshness: freshness,
			Classifications: classifications, InaccessibleFieldPaths: recordInaccessible, RedactedFieldPaths: redacted, Tombstoned: raw.Tombstoned,
		})
		if activeRecord && !raw.Tombstoned && current.kind == raw.SubjectKind && (len(fields) != 0 || len(redacted) != 0) {
			if seenSnapshot[address] {
				return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotInvalid)
			}
			seenSnapshot[address] = true
			subjects = append(subjects, semantic.StateQuerySubject{SubjectAddress: address, OwnSubjectHash: raw.OwnSubjectHash, Fields: fields, RedactedFieldPaths: redacted})
		}
	}
	for address, subject := range active {
		if seenActive[address] {
			continue
		}
		redacted, err := canonicalStatePaths(decision.RedactedFieldPaths[address])
		if err != nil {
			return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotAuthorizationInvalid)
		}
		redacted = excludeStatePaths(redacted, inaccessibleSet)
		classifications := []StateSubjectClassification{StateSubjectMissing}
		if len(redacted) != 0 {
			classifications = append(classifications, StateSubjectRedacted)
			subjects = append(subjects, semantic.StateQuerySubject{
				SubjectAddress: address, OwnSubjectHash: subject.hash,
				Fields: map[string]semantic.RecipeScalar{}, RedactedFieldPaths: redacted,
			})
		}
		records = append(records, CanonicalStateRecord{
			SubjectAddress: address, SubjectKind: subject.kind, StateVersion: string(state.Head.StateVersion),
			DefinitionHash: state.Head.DefinitionHash, GraphHash: state.Head.GraphHash, Fields: map[string]semantic.RecipeScalar{},
			Freshness: StateRecordFreshnessUnknown, Classifications: classifications,
			InaccessibleFieldPaths: []semantic.StateFieldPath{}, RedactedFieldPaths: redacted,
		})
	}
	sort.Slice(subjects, func(i, j int) bool {
		return engineendpoint.CompareStableAddresses(subjects[i].SubjectAddress, subjects[j].SubjectAddress) < 0
	})
	sortCanonicalStateRecords(records)
	captured, err := time.Parse(time.RFC3339Nano, string(state.Head.CapturedAt))
	if err != nil {
		return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotInvalid)
	}
	snapshot := semantic.StateQuerySnapshot{
		Format: semantic.StateQuerySnapshotFormatValue, SchemaVersion: 1,
		DefinitionProjectAddress: input.Definition.ProjectAddress, DefinitionHash: state.Head.DefinitionHash, GraphHash: state.Head.GraphHash,
		StateVersion: string(state.Head.StateVersion), CapturedAt: protocolcommon.Rfc3339Time(captured.UTC().Format(time.RFC3339Nano)),
		InaccessibleFieldPaths: inaccessible, Subjects: subjects,
	}
	canonical, hash, err := engineendpoint.CanonicalizeStateQuerySnapshot(snapshot)
	if err != nil {
		return BuiltStateQueryInput{}, fmt.Errorf("%w: %v", stateSnapshotError(StateSnapshotInvalid), err)
	}
	immutable := &ImmutableStateQuerySnapshot{snapshot: cloneSemanticSnapshot(snapshot), canonicalJSON: append(append([]byte(nil), canonical...), '\n'), hash: hash}
	version := snapshot.StateVersion
	capturedAt, definitionHash := snapshot.CapturedAt, snapshot.DefinitionHash
	inputSnapshot := immutable.Snapshot()
	expectedVersion := state.Head.StateVersion
	plan, err := reconciliationPlan(records)
	if err != nil {
		return BuiltStateQueryInput{}, stateSnapshotError(StateSnapshotInvalid)
	}
	decisionDigest := decision.DecisionDigest
	return BuiltStateQueryInput{
		StateInput:    runtimeprotocol.StateInput{Kind: "snapshot", ExpectedStateVersion: &expectedVersion, Snapshot: &inputSnapshot, SnapshotHash: &hash},
		StateInputRef: StateInputRef{Kind: "snapshot", SnapshotHash: &hash, StateVersion: &version, CapturedAt: &capturedAt, DefinitionHash: &definitionHash},
		Snapshot:      immutable, Records: cloneCanonicalRecords(records), ReconciliationPlan: plan, AuthorizationDecisionDigest: &decisionDigest,
	}, nil
}

func validStateRecord(record port.StateRecord, head port.StateHead) bool {
	if _, err := semantic.EncodeStableAddress(record.SubjectAddress); err != nil {
		return false
	}
	switch record.SubjectKind {
	case semantic.StateSubjectKindEntity, semantic.StateSubjectKindRelation, semantic.StateSubjectKindEntityRow, semantic.StateSubjectKindRelationRow:
	default:
		return false
	}
	if _, err := protocolcommon.EncodeDigest(record.OwnSubjectHash); err != nil || record.Fields == nil {
		return false
	}
	headHash, ok := head.SubjectHashes[record.SubjectAddress]
	return ok && headHash == record.OwnSubjectHash
}

func canonicalStatePaths(values []semantic.StateFieldPath) ([]semantic.StateFieldPath, error) {
	registry := StateFieldRegistry()
	allowed := make(map[semantic.StateFieldPath]bool, len(registry))
	for _, path := range registry {
		allowed[path] = true
	}
	seen := map[semantic.StateFieldPath]bool{}
	for _, value := range values {
		if !allowed[value] {
			return nil, fmt.Errorf("unknown state field path")
		}
		seen[value] = true
	}
	result := make([]semantic.StateFieldPath, 0, len(seen))
	for _, value := range registry {
		if seen[value] {
			result = append(result, value)
		}
	}
	return result, nil
}

func pathSet(values []semantic.StateFieldPath) map[semantic.StateFieldPath]bool {
	result := make(map[semantic.StateFieldPath]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func excludeStatePaths(values []semantic.StateFieldPath, excluded map[semantic.StateFieldPath]bool) []semantic.StateFieldPath {
	result := make([]semantic.StateFieldPath, 0, len(values))
	for _, value := range values {
		if !excluded[value] {
			result = append(result, value)
		}
	}
	return result
}

func reconciliationPlan(records []CanonicalStateRecord) (StateReconciliationPlan, error) {
	actions := make([]StateReconcileAction, 0)
	for _, record := range records {
		classes := classificationSet(record.Classifications)
		if record.SourceAddress != "" && record.SourceAddress != record.SubjectAddress {
			source := record.SourceAddress
			actions = append(actions, StateReconcileAction{Kind: ReconcileRemapState, SourceAddress: &source, SubjectAddress: record.SubjectAddress})
		}
		switch {
		case classes[StateSubjectMissing]:
			actions = append(actions, StateReconcileAction{Kind: ReconcileRefreshState, SubjectAddress: record.SubjectAddress})
		case classes[StateSubjectStale] && (classes[StateSubjectInaccessible] || classes[StateSubjectRedacted]):
			actions = append(actions, StateReconcileAction{Kind: ReconcileManualReview, SubjectAddress: record.SubjectAddress})
		case classes[StateSubjectStale]:
			actions = append(actions, StateReconcileAction{Kind: ReconcileRefreshState, SubjectAddress: record.SubjectAddress})
		case classes[StateSubjectOrphaned] && record.Tombstoned:
			actions = append(actions, StateReconcileAction{Kind: ReconcileArchiveOrphan, SubjectAddress: record.SubjectAddress})
		case classes[StateSubjectOrphaned]:
			actions = append(actions, StateReconcileAction{Kind: ReconcileTombstoneState, SubjectAddress: record.SubjectAddress})
		}
	}
	sort.Slice(actions, func(i, j int) bool {
		if compared := engineendpoint.CompareStableAddresses(actions[i].SubjectAddress, actions[j].SubjectAddress); compared != 0 {
			return compared < 0
		}
		if actions[i].Kind != actions[j].Kind {
			return actions[i].Kind < actions[j].Kind
		}
		return engineendpoint.CompareStableAddresses(optionalAddress(actions[i].SourceAddress), optionalAddress(actions[j].SourceAddress)) < 0
	})
	payload, err := json.Marshal(struct {
		Actions []StateReconcileAction `json:"actions"`
	}{Actions: actions})
	if err != nil {
		return StateReconciliationPlan{}, err
	}
	digest := sha256.Sum256(payload)
	return StateReconciliationPlan{Actions: cloneReconcileActions(actions), PlanDigest: protocolcommon.Digest("sha256:" + hex.EncodeToString(digest[:]))}, nil
}

func sortCanonicalStateRecords(records []CanonicalStateRecord) {
	sort.Slice(records, func(i, j int) bool {
		if compared := engineendpoint.CompareStableAddresses(records[i].SubjectAddress, records[j].SubjectAddress); compared != 0 {
			return compared < 0
		}
		return engineendpoint.CompareStableAddresses(records[i].SourceAddress, records[j].SourceAddress) < 0
	})
}

func optionalAddress(value *semantic.StableAddress) semantic.StableAddress {
	if value == nil {
		return ""
	}
	return *value
}

func classificationSet(values []StateSubjectClassification) map[StateSubjectClassification]bool {
	result := make(map[StateSubjectClassification]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func cloneSemanticSnapshot(input semantic.StateQuerySnapshot) semantic.StateQuerySnapshot {
	input.InaccessibleFieldPaths = append(make([]semantic.StateFieldPath, 0, len(input.InaccessibleFieldPaths)), input.InaccessibleFieldPaths...)
	input.Subjects = append(make([]semantic.StateQuerySubject, 0, len(input.Subjects)), input.Subjects...)
	for index := range input.Subjects {
		subject := &input.Subjects[index]
		subject.RedactedFieldPaths = append(make([]semantic.StateFieldPath, 0, len(subject.RedactedFieldPaths)), subject.RedactedFieldPaths...)
		fields := make(map[string]semantic.RecipeScalar, len(subject.Fields))
		for path, value := range subject.Fields {
			fields[path] = cloneRecipeScalar(value)
		}
		subject.Fields = fields
	}
	return input
}

func cloneRecipeScalar(input semantic.RecipeScalar) semantic.RecipeScalar {
	if input.BooleanValue != nil {
		value := *input.BooleanValue
		input.BooleanValue = &value
	}
	if input.IntegerValue != nil {
		value := *input.IntegerValue
		input.IntegerValue = &value
	}
	if input.NumberValue != nil {
		value := *input.NumberValue
		input.NumberValue = &value
	}
	if input.StringValue != nil {
		value := *input.StringValue
		input.StringValue = &value
	}
	return input
}

func cloneCanonicalRecords(input []CanonicalStateRecord) []CanonicalStateRecord {
	result := append([]CanonicalStateRecord(nil), input...)
	for index := range result {
		record := &result[index]
		record.Classifications = append([]StateSubjectClassification(nil), record.Classifications...)
		record.InaccessibleFieldPaths = append([]semantic.StateFieldPath(nil), record.InaccessibleFieldPaths...)
		record.RedactedFieldPaths = append([]semantic.StateFieldPath(nil), record.RedactedFieldPaths...)
		fields := make(map[string]semantic.RecipeScalar, len(record.Fields))
		for path, value := range record.Fields {
			fields[path] = cloneRecipeScalar(value)
		}
		record.Fields = fields
	}
	return result
}

func cloneReconcileActions(input []StateReconcileAction) []StateReconcileAction {
	result := append([]StateReconcileAction(nil), input...)
	for index := range result {
		if result[index].SourceAddress != nil {
			value := *result[index].SourceAddress
			result[index].SourceAddress = &value
		}
	}
	return result
}
