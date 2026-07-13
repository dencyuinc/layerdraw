// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package query compiles resolved LDL Query declarations into typed,
// backend-independent recipes. It does not evaluate Queries or produce
// physical execution plans.
package query

import (
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
)

type Input struct {
	Resolve    resolve.Result
	Definition definition.Result
	Graph      graph.Result
}

type Result struct {
	stageGeneration resolve.StageGeneration
	Recipes         []Recipe
	Diagnostics     []resolve.Diagnostic
	HasErrors       bool
}

// MatchesResolve reports whether this result was compiled from the supplied
// Resolve invocation.
func (r Result) MatchesResolve(resolved resolve.Result) bool {
	return r.stageGeneration.Matches(resolved.Generation())
}

// Generation returns the opaque token for propagation to the next internal
// compiler stage.
func (r Result) Generation() resolve.StageGeneration {
	return r.stageGeneration
}

type Recipe struct {
	definition.Common
	ID                   string
	Address              string
	DisplayName          string
	StateInput           StatePolicy
	Parameters           []Parameter
	Select               Select
	Where                Predicate
	RelationWhere        Predicate
	Traversal            *Traversal
	Result               []ResultMember
	ReservedParameterIDs []string
	Dependencies         Dependencies
}

type Parameter struct {
	ID                 string
	Address            string
	ValueType          definition.ScalarType
	EnumValues         []string
	ReservedEnumValues []string
	Required           bool
	Default            *definition.Scalar
	Format             *definition.StringFormat
	Min                *float64
	Max                *float64
	MinLength          *int64
	MaxLength          *int64
}

type StatePolicy string

const (
	StateNone     StatePolicy = "none"
	StateOptional StatePolicy = "optional"
	StateRequired StatePolicy = "required"
)

// Pointer-valued selector fields preserve absent (unrestricted) versus an
// explicitly authored empty set (no candidates).
type Select struct {
	LayerAddresses        *[]string
	EntityTypeAddresses   *[]string
	RelationTypeAddresses *[]string
	RootAddresses         *[]string
}

type PredicateKind string

const (
	PredicateAll   PredicateKind = "all"
	PredicateAny   PredicateKind = "any"
	PredicateNot   PredicateKind = "not"
	PredicateField PredicateKind = "field"
	PredicateCell  PredicateKind = "cell"
	PredicateState PredicateKind = "state"
	PredicateRows  PredicateKind = "rows"
)

type Predicate struct {
	Kind          PredicateKind
	Children      []Predicate
	Child         *Predicate
	Field         string
	FieldPath     StateFieldPath
	OperandType   OperandType
	Operator      Operator
	Value         *PredicateValue
	Quantifier    RowQuantifier
	TypeAddresses []string
	Row           *RowPredicate
}

type RowPredicate struct {
	Kind            PredicateKind
	Children        []RowPredicate
	Child           *RowPredicate
	ColumnAddresses []string
	FieldPath       StateFieldPath
	OperandType     OperandType
	Operator        Operator
	Value           *PredicateValue
}

type RowQuantifier string

const (
	RowsAny  RowQuantifier = "any"
	RowsAll  RowQuantifier = "all"
	RowsNone RowQuantifier = "none"
)

type Operator string

const (
	OperatorEqual      Operator = "eq"
	OperatorNotEqual   Operator = "ne"
	OperatorLess       Operator = "lt"
	OperatorLessEqual  Operator = "lte"
	OperatorGreater    Operator = "gt"
	OperatorGreaterEq  Operator = "gte"
	OperatorIn         Operator = "in"
	OperatorNotIn      Operator = "not_in"
	OperatorContains   Operator = "contains"
	OperatorStartsWith Operator = "starts_with"
	OperatorEndsWith   Operator = "ends_with"
	OperatorExists     Operator = "exists"
	OperatorMissing    Operator = "missing"
)

type OperandKind string

const (
	OperandScalar    OperandKind = "scalar"
	OperandAddress   OperandKind = "address"
	OperandStringSet OperandKind = "string_set"
)

type OperandType struct {
	Kind        OperandKind
	ScalarType  definition.ScalarType
	AddressKind resolve.SubjectKind
}

type PredicateValueKind string

const (
	ValueLiteral   PredicateValueKind = "literal"
	ValueParameter PredicateValueKind = "parameter"
)

type PredicateValue struct {
	Kind             PredicateValueKind
	Scalar           *definition.Scalar
	Address          *string
	Scalars          []definition.Scalar
	Addresses        []string
	ParameterAddress string
}

type StateFieldPath string

const (
	StateSystemCreatedAt                 StateFieldPath = "system.created_at"
	StateSystemUpdatedAt                 StateFieldPath = "system.updated_at"
	StateSystemCreatedByKind             StateFieldPath = "system.created_by.kind"
	StateSystemCreatedByID               StateFieldPath = "system.created_by.id"
	StateSystemCreatedByDisplayName      StateFieldPath = "system.created_by.display_name"
	StateSystemUpdatedByKind             StateFieldPath = "system.updated_by.kind"
	StateSystemUpdatedByID               StateFieldPath = "system.updated_by.id"
	StateSystemUpdatedByDisplayName      StateFieldPath = "system.updated_by.display_name"
	StateSystemCreatedRevision           StateFieldPath = "system.created_revision"
	StateSystemUpdatedRevision           StateFieldPath = "system.updated_revision"
	StateProvenanceSourceKind            StateFieldPath = "provenance.source.kind"
	StateProvenanceSourceLabel           StateFieldPath = "provenance.source.label"
	StateProvenanceSourceURI             StateFieldPath = "provenance.source.uri"
	StateProvenanceSourceExternalID      StateFieldPath = "provenance.source.external_id"
	StateProvenanceObservedAt            StateFieldPath = "provenance.observed_at"
	StateProvenanceVerifiedAt            StateFieldPath = "provenance.verified_at"
	StateProvenanceStaleAfter            StateFieldPath = "provenance.stale_after"
	StateProvenanceVerifiedByKind        StateFieldPath = "provenance.verified_by.kind"
	StateProvenanceVerifiedByID          StateFieldPath = "provenance.verified_by.id"
	StateProvenanceVerifiedByDisplayName StateFieldPath = "provenance.verified_by.display_name"
	StateProvenanceConfidence            StateFieldPath = "provenance.confidence"
)

type Traversal struct {
	Direction             definition.TraversalDirection
	MinDepth              int64
	MaxDepth              int64
	CyclePolicy           CyclePolicy
	RelationTypeAddresses *[]string
}

type CyclePolicy string

const (
	CycleError           CyclePolicy = "error"
	CycleVisitOnce       CyclePolicy = "visit_once"
	CycleIncludeCycleRef CyclePolicy = "include_cycle_ref"
)

type ResultMember string

const (
	ResultSeedEntities      ResultMember = "seed_entities"
	ResultTraversedEntities ResultMember = "traversed_entities"
	ResultPathRelations     ResultMember = "path_relations"
	ResultInducedRelations  ResultMember = "induced_relations"
)

type Dependencies struct {
	LayerAddresses        []string
	EntityTypeAddresses   []string
	RelationTypeAddresses []string
	EntityAddresses       []string
	RelationAddresses     []string
	ColumnAddresses       []string
	ParameterAddresses    []string
	StateReads            []StateReadDependency
}

type StateSubjectKind string

const (
	StateSubjectEntity      StateSubjectKind = "entity"
	StateSubjectRelation    StateSubjectKind = "relation"
	StateSubjectEntityRow   StateSubjectKind = "entity_row"
	StateSubjectRelationRow StateSubjectKind = "relation_row"
)

type StateReadDependency struct {
	SubjectKind StateSubjectKind
	FieldPath   StateFieldPath
	ValueType   definition.ScalarType
}
