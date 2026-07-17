// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

// validateStateQuerySnapshotForDefinition reuses the Query evaluator's closed
// snapshot validation for direct View state reads. It performs no Query
// traversal and returns the canonical StateInputRef only after full validation.
func validateStateQuerySnapshotForDefinition(ctx context.Context, identity QueryDefinitionIdentity, graphValue TypedMasterGraph, snapshot StateQuerySnapshot) (QueryStateInputRef, []Diagnostic, error) {
	limits := DefaultQueryExecutionLimits()
	validator := &queryExecutor{
		ctx: ctx, limits: limits, graph: graphValue, definition: identity,
		stateReads: map[StateReadRef]bool{}, deniedStateReads: map[StateReadRef]bool{},
		stateSubjects: map[string]validatedStateSubject{}, stateInaccessible: map[query.StateFieldPath]bool{},
		currentStateHashes: map[string]string{}, staleStateSubjects: map[string]bool{},
	}
	if !validator.validateStateSnapshot(snapshot) {
		return QueryStateInputRef{}, sortedDiagnostics(validator.diagnostics), validator.err
	}
	return validator.stateInput, nil, validator.err
}

const (
	StateQuerySnapshotFormat        = "layerdraw-query-state"
	StateQuerySnapshotSchemaVersion = 1
)

// QueryDefinitionIdentity is the immutable subset of a compiled definition
// needed to validate state records against the graph being evaluated.
type QueryDefinitionIdentity struct {
	ProjectAddress string
	DefinitionHash string
	GraphHash      string
	SubjectHashes  []SubjectHash
}

// QueryDefinitionIdentity returns an independently owned identity projection.
func (s Snapshot) QueryDefinitionIdentity() QueryDefinitionIdentity {
	identity := QueryDefinitionIdentity{
		DefinitionHash: s.DefinitionHash,
		SubjectHashes:  append([]SubjectHash{}, s.SubjectSemanticHashes...),
	}
	if s.TypedAST.Project != nil {
		identity.ProjectAddress = s.TypedAST.Project.Address
	}
	if s.GraphHash != nil {
		identity.GraphHash = *s.GraphHash
	}
	return identity
}

// StateQuerySnapshot is the complete, Access-projected current-state input
// fixed by a Runtime before one Query or View evaluation.
type StateQuerySnapshot struct {
	Format                 string
	SchemaVersion          int
	DefinitionProject      string
	DefinitionHash         string
	GraphHash              string
	StateVersion           string
	CapturedAt             string
	InaccessibleFieldPaths []string
	Subjects               []StateQuerySubject
}

// StateQuerySubject contains only fields from the closed state registry.
// Fields is a map because its canonical JSON representation is an object;
// field iteration order is supplied by the registry, never by the Go map.
type StateQuerySubject struct {
	SubjectAddress     string
	OwnSubjectHash     string
	Fields             map[string]TypedScalar
	RedactedFieldPaths []string
}

type validatedStateSubject struct {
	ownSubjectHash string
	fields         map[query.StateFieldPath]definition.Scalar
	redacted       map[query.StateFieldPath]bool
}

type stateQuerySnapshotHashPayload struct {
	Format                 string                         `json:"format"`
	SchemaVersion          int                            `json:"schema_version"`
	DefinitionProject      string                         `json:"definition_project_address"`
	DefinitionHash         string                         `json:"definition_hash"`
	GraphHash              string                         `json:"graph_hash"`
	StateVersion           string                         `json:"state_version"`
	CapturedAt             string                         `json:"captured_at"`
	InaccessibleFieldPaths []query.StateFieldPath         `json:"inaccessible_field_paths"`
	Subjects               []stateQuerySubjectHashPayload `json:"subjects"`
}

type stateQuerySubjectHashPayload struct {
	SubjectAddress     string                                      `json:"subject_address"`
	OwnSubjectHash     string                                      `json:"own_subject_hash"`
	Fields             map[query.StateFieldPath]materialize.Scalar `json:"fields"`
	RedactedFieldPaths []query.StateFieldPath                      `json:"redacted_field_paths"`
}

func (e *queryExecutor) validateStateSnapshot(snapshot StateQuerySnapshot) bool {
	if snapshot.Format != StateQuerySnapshotFormat || snapshot.SchemaVersion != StateQuerySnapshotSchemaVersion {
		return e.invalidStateInput("invalid StateQuerySnapshot schema identity", snapshot.DefinitionProject)
	}
	if snapshot.InaccessibleFieldPaths == nil || snapshot.Subjects == nil {
		return e.invalidStateInput("StateQuerySnapshot collections must be present", snapshot.DefinitionProject)
	}
	if !e.validateDefinitionIdentity() {
		return false
	}
	if snapshot.DefinitionProject != e.definition.ProjectAddress {
		return e.invalidStateInput("StateQuerySnapshot belongs to a different Project", snapshot.DefinitionProject)
	}
	if !validSemanticHash(snapshot.DefinitionHash) || !validSemanticHash(snapshot.GraphHash) {
		return e.invalidStateInput("StateQuerySnapshot definition identity is invalid", snapshot.DefinitionProject)
	}
	if snapshot.StateVersion == "" || definition.NormalizeText(snapshot.StateVersion) != snapshot.StateVersion {
		return e.invalidStateInput("StateQuerySnapshot state version is not a canonical NFC string", snapshot.DefinitionProject)
	}
	captured, valid := definition.NormalizeScalarValue(definition.Scalar{Type: definition.ScalarDatetime, String: snapshot.CapturedAt}, definition.Column{ValueType: definition.ScalarDatetime}, e.charge)
	if !valid {
		if e.err != nil {
			return false
		}
		return e.invalidStateInput("StateQuerySnapshot captured_at is invalid", snapshot.DefinitionProject)
	}
	if captured.String != snapshot.CapturedAt {
		return e.invalidStateInput("StateQuerySnapshot captured_at is not canonical UTC", snapshot.DefinitionProject)
	}
	if !e.chargeString(snapshot.StateVersion) || !e.chargeString(snapshot.DefinitionProject) {
		return false
	}

	inaccessible, ok := e.validateStateFieldSet(snapshot.InaccessibleFieldPaths, "inaccessible_field_paths", snapshot.DefinitionProject)
	if !ok {
		return false
	}
	e.stateInaccessible = inaccessible

	active, currentHashes, ok := e.activeStateSubjects()
	if !ok {
		return false
	}
	e.currentStateHashes = currentHashes
	if !e.charge(int64(len(snapshot.Subjects))) {
		return false
	}
	e.stateSubjects = make(map[string]validatedStateSubject, len(snapshot.Subjects))

	previousAddress := ""
	for index, subject := range snapshot.Subjects {
		if !e.step() || !e.chargeString(subject.SubjectAddress) || !e.chargeString(subject.OwnSubjectHash) {
			return false
		}
		if subject.Fields == nil || subject.RedactedFieldPaths == nil {
			return e.invalidStateInput("StateQuerySubject collections must be present", subject.SubjectAddress)
		}
		if index != 0 && compareStableAddressText(previousAddress, subject.SubjectAddress) >= 0 {
			return e.invalidStateInput("StateQuerySnapshot subjects are not in canonical unique order", subject.SubjectAddress)
		}
		previousAddress = subject.SubjectAddress
		if _, exists := active[subject.SubjectAddress]; !exists {
			return e.invalidStateInput("StateQuerySubject is not active in the evaluated graph", subject.SubjectAddress)
		}
		if !validSemanticHash(subject.OwnSubjectHash) {
			return e.invalidStateInput("StateQuerySubject own-subject hash is invalid", subject.SubjectAddress)
		}
		redacted, valid := e.validateStateFieldSet(subject.RedactedFieldPaths, "redacted_field_paths", subject.SubjectAddress)
		if !valid {
			return false
		}
		if !e.charge(int64(len(subject.Fields))) {
			return false
		}
		fields := make(map[query.StateFieldPath]definition.Scalar, len(subject.Fields))
		paths := make([]query.StateFieldPath, 0, len(subject.Fields))
		for rawPath := range subject.Fields {
			paths = append(paths, query.StateFieldPath(rawPath))
		}
		sort.Slice(paths, func(i, j int) bool { return query.CompareStateFieldPaths(paths[i], paths[j]) < 0 })
		for _, path := range paths {
			if !e.step() || !e.chargeString(string(path)) {
				return false
			}
			if e.stateInaccessible[path] {
				return e.invalidStateInput("inaccessible state field contains a value", subject.SubjectAddress)
			}
			if redacted[path] {
				return e.invalidStateInput("state field is both present and redacted", subject.SubjectAddress)
			}
			value, valid := query.NormalizeStateFieldValue(path, subject.Fields[string(path)], e.charge)
			if !valid {
				if e.err != nil {
					return false
				}
				return e.invalidStateInput("state field is unknown, non-canonical, or has an invalid typed value", subject.SubjectAddress)
			}
			if value != subject.Fields[string(path)] {
				return e.invalidStateInput("state field value is not canonical", subject.SubjectAddress)
			}
			fields[path] = value
		}
		if len(fields) == 0 && len(redacted) == 0 {
			return e.invalidStateInput("empty StateQuerySubject records must be omitted", subject.SubjectAddress)
		}
		e.stateSubjects[subject.SubjectAddress] = validatedStateSubject{ownSubjectHash: subject.OwnSubjectHash, fields: fields, redacted: redacted}
	}

	hash, err := stateQuerySnapshotHash(snapshot)
	if err != nil {
		return e.invalidStateInput("StateQuerySnapshot cannot be canonically hashed", snapshot.DefinitionProject)
	}
	e.stateInput = QueryStateInputRef{
		Kind:           "snapshot",
		SnapshotHash:   hash,
		StateVersion:   snapshot.StateVersion,
		CapturedAt:     snapshot.CapturedAt,
		DefinitionHash: snapshot.DefinitionHash,
	}
	return true
}

func (e *queryExecutor) validateDefinitionIdentity() bool {
	if e.definition.ProjectAddress == "" || !validSemanticHash(e.definition.DefinitionHash) || !validSemanticHash(e.definition.GraphHash) {
		return e.invalidStateInput("evaluated definition identity is incomplete", e.definition.ProjectAddress)
	}
	seen := map[string]bool{}
	for _, subject := range e.definition.SubjectHashes {
		if !e.step() || !e.chargeString(subject.Address) || !e.chargeString(subject.Hash) {
			return false
		}
		if subject.Address == "" || seen[subject.Address] || !validSemanticHash(subject.Hash) {
			return e.invalidStateInput("evaluated definition subject identity is invalid", subject.Address)
		}
		seen[subject.Address] = true
	}
	return true
}

func (e *queryExecutor) activeStateSubjects() (map[string]SemanticSubjectKind, map[string]string, bool) {
	active := map[string]SemanticSubjectKind{}
	add := func(address string, kind SemanticSubjectKind) bool {
		if address == "" || active[address] != "" {
			return false
		}
		active[address] = kind
		return true
	}
	for _, entity := range e.graph.Entities {
		if !e.step() {
			return nil, nil, false
		}
		if !add(entity.Address, materialize.SubjectEntity) {
			return nil, nil, e.invalidStateInput("evaluated graph contains an invalid state subject identity", entity.Address)
		}
		for _, row := range entity.Rows {
			if !e.step() {
				return nil, nil, false
			}
			if !add(row.Address, materialize.SubjectEntityRow) {
				return nil, nil, e.invalidStateInput("evaluated graph contains an invalid row identity", row.Address)
			}
		}
	}
	for _, relation := range e.graph.Relations {
		if !e.step() {
			return nil, nil, false
		}
		if !add(relation.Address, materialize.SubjectRelation) {
			return nil, nil, e.invalidStateInput("evaluated graph contains an invalid state subject identity", relation.Address)
		}
		for _, row := range relation.Rows {
			if !e.step() {
				return nil, nil, false
			}
			if !add(row.Address, materialize.SubjectRelationRow) {
				return nil, nil, e.invalidStateInput("evaluated graph contains an invalid row identity", row.Address)
			}
		}
	}
	hashes := map[string]string{}
	for _, subject := range e.definition.SubjectHashes {
		kind, relevant := active[subject.Address]
		if !relevant {
			continue
		}
		if subject.Kind != kind {
			return nil, nil, e.invalidStateInput("evaluated definition subject kind does not match the graph", subject.Address)
		}
		hashes[subject.Address] = subject.Hash
	}
	addresses := make([]string, 0, len(active))
	for address := range active {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(i, j int) bool { return compareStableAddressText(addresses[i], addresses[j]) < 0 })
	for _, address := range addresses {
		if hashes[address] == "" {
			return nil, nil, e.invalidStateInput("evaluated definition lacks an active subject hash", address)
		}
	}
	return active, hashes, true
}

func (e *queryExecutor) validateStateFieldSet(paths []string, label, subject string) (map[query.StateFieldPath]bool, bool) {
	out := make(map[query.StateFieldPath]bool, len(paths))
	var previous query.StateFieldPath
	for index, rawPath := range paths {
		path := query.StateFieldPath(rawPath)
		if !e.step() || !e.chargeString(rawPath) {
			return nil, false
		}
		if _, exists := query.LookupStateFieldSchema(path); !exists {
			return nil, e.invalidStateInput(label+" contains an unknown state field", subject)
		}
		if index != 0 && query.CompareStateFieldPaths(previous, path) >= 0 {
			return nil, e.invalidStateInput(label+" is not in canonical unique registry order", subject)
		}
		previous = path
		out[path] = true
	}
	return out, true
}

func (e *queryExecutor) invalidStateInput(message, subject string) bool {
	e.addDiag("LDL1601", "invalid_query_or_arguments", message, subject, e.recipe.Address)
	return false
}

func (e *queryExecutor) chargeString(value string) bool {
	return e.charge(int64(len(value)))
}

func validSemanticHash(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		decimal := character >= '0' && character <= '9'
		hexadecimal := character >= 'a' && character <= 'f'
		if !decimal && !hexadecimal {
			return false
		}
	}
	return true
}

func stateQuerySnapshotHash(snapshot StateQuerySnapshot) (string, error) {
	payload := stateQuerySnapshotHashPayload{
		Format:                 snapshot.Format,
		SchemaVersion:          snapshot.SchemaVersion,
		DefinitionProject:      snapshot.DefinitionProject,
		DefinitionHash:         snapshot.DefinitionHash,
		GraphHash:              snapshot.GraphHash,
		StateVersion:           snapshot.StateVersion,
		CapturedAt:             snapshot.CapturedAt,
		InaccessibleFieldPaths: stateFieldPaths(snapshot.InaccessibleFieldPaths),
		Subjects:               make([]stateQuerySubjectHashPayload, len(snapshot.Subjects)),
	}
	for index, subject := range snapshot.Subjects {
		fields := make(map[query.StateFieldPath]materialize.Scalar, len(subject.Fields))
		for path, value := range subject.Fields {
			fields[query.StateFieldPath(path)] = materialize.Scalar{Type: value.Type, String: value.String, Int: value.Int, Float: value.Float, Bool: value.Bool}
		}
		payload.Subjects[index] = stateQuerySubjectHashPayload{
			SubjectAddress:     subject.SubjectAddress,
			OwnSubjectHash:     subject.OwnSubjectHash,
			Fields:             fields,
			RedactedFieldPaths: stateFieldPaths(subject.RedactedFieldPaths),
		}
	}
	hash, err := materialize.SemanticHash(materialize.DomainStateQuery, payload)
	if err != nil {
		return "", fmt.Errorf("hash StateQuerySnapshot: %w", err)
	}
	return hash, nil
}

func stateFieldPaths(values []string) []query.StateFieldPath {
	out := make([]query.StateFieldPath, len(values))
	for index, value := range values {
		out[index] = query.StateFieldPath(value)
	}
	return out
}
