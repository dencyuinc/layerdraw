// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package materialize projects the validated LDL compiler stages into the
// closed Language 1 normalized model and its semantic hashes.
package materialize

import (
	"encoding/json"
	"fmt"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/exportrecipe"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

const (
	NormalizedFormat     = "layerdraw-normalized"
	NormalizedPackFormat = "layerdraw-normalized-pack"
	SchemaVersion        = 1
	LanguageVersion      = 1
)

// SubjectKind is the normative Language 1 generated-output vocabulary. The
// resolver deliberately uses a smaller compiler-internal vocabulary for
// owner-scoped declarations; generated artifacts must never expose it.
type SubjectKind string

const (
	SubjectProject                SubjectKind = "project"
	SubjectPack                   SubjectKind = "pack"
	SubjectEntityType             SubjectKind = "entity_type"
	SubjectRelationType           SubjectKind = "relation_type"
	SubjectLayer                  SubjectKind = "layer"
	SubjectEntity                 SubjectKind = "entity"
	SubjectRelation               SubjectKind = "relation"
	SubjectQuery                  SubjectKind = "query"
	SubjectView                   SubjectKind = "view"
	SubjectReference              SubjectKind = "reference"
	SubjectEntityTypeColumn       SubjectKind = "entity_type_column"
	SubjectEntityTypeConstraint   SubjectKind = "entity_type_constraint"
	SubjectRelationTypeColumn     SubjectKind = "relation_type_column"
	SubjectRelationTypeConstraint SubjectKind = "relation_type_constraint"
	SubjectEntityRow              SubjectKind = "entity_row"
	SubjectRelationRow            SubjectKind = "relation_row"
	SubjectQueryParameter         SubjectKind = "query_parameter"
	SubjectViewTableColumn        SubjectKind = "view_table_column"
	SubjectViewExport             SubjectKind = "view_export"
)

// GeneratedSubjectKind converts the resolver's compiler-only kind vocabulary
// into the closed Language 1 vocabulary. ownerKind is required only for the
// resolver's generic owner-scoped kinds.
func GeneratedSubjectKind(kind, ownerKind resolve.SubjectKind) (SubjectKind, bool) {
	switch kind {
	case resolve.KindProject:
		return SubjectProject, true
	case resolve.KindPack:
		return SubjectPack, true
	case resolve.KindEntityType:
		return SubjectEntityType, true
	case resolve.KindRelationType:
		return SubjectRelationType, true
	case resolve.KindLayer:
		return SubjectLayer, true
	case resolve.KindEntity:
		return SubjectEntity, true
	case resolve.KindRelation:
		return SubjectRelation, true
	case resolve.KindQuery:
		return SubjectQuery, true
	case resolve.KindView:
		return SubjectView, true
	case resolve.KindReference:
		return SubjectReference, true
	case resolve.KindColumn:
		if ownerKind == resolve.KindEntityType {
			return SubjectEntityTypeColumn, true
		}
		if ownerKind == resolve.KindRelationType {
			return SubjectRelationTypeColumn, true
		}
	case resolve.KindConstraint:
		if ownerKind == resolve.KindEntityType {
			return SubjectEntityTypeConstraint, true
		}
		if ownerKind == resolve.KindRelationType {
			return SubjectRelationTypeConstraint, true
		}
	case resolve.KindRow:
		if ownerKind == resolve.KindEntity {
			return SubjectEntityRow, true
		}
		if ownerKind == resolve.KindRelation {
			return SubjectRelationRow, true
		}
	case resolve.KindParameter:
		if ownerKind == resolve.KindQuery {
			return SubjectQueryParameter, true
		}
	case resolve.KindTableColumn:
		if ownerKind == resolve.KindView {
			return SubjectViewTableColumn, true
		}
	case resolve.KindExport:
		if ownerKind == resolve.KindView {
			return SubjectViewExport, true
		}
	}
	return "", false
}

// ResolvedAsset is a closed asset input. Materialization computes the digest
// and byte length from Bytes, then checks the resolver-supplied expectations.
type ResolvedAsset struct {
	Origin             resolve.SourceOrigin
	Locator            string
	Bytes              []byte
	ExpectedDigest     string
	ExpectedMediaType  string
	ExpectedByteLength int64
}

// ResolvedSourceFile is exact source-byte metadata supplied by the future
// compiler facade (#18). Generated indexes never reopen ModulePath.
type ResolvedSourceFile struct {
	Origin     resolve.SourceOrigin
	ModulePath string
	Bytes      []byte
}

// ResolvedPackClosure carries every selected pack field that is otherwise
// lost after Resolve. Source bytes are kept separately in SourceFiles.
type ResolvedPackClosure struct {
	ResolvedPackSummary
	Path         string
	Entry        string
	Files        []ResolvedPackFile
	Dependencies []ResolvedPackDependency
	Manifest     ResolvedPackManifest
}

type ResolvedPackFile struct {
	Path   string
	Digest string
}

type ResolvedPackDependency struct {
	LocalName   string
	InstallName string
	CanonicalID string
	Version     string
	Digest      string
}

type ResolvedPackManifest struct {
	Format        string
	FormatVersion int
	ID            string
	Name          string
	Version       string
	Language      int
	Entry         string
	Path          string
	Bytes         []byte
}

// ResolvedMetadata is the closed, already-resolved non-stage input. It is the
// in-memory handoff that the public compiler facade in #18 only needs to wire.
type ResolvedMetadata struct {
	SelectedClosure []ResolvedPackClosure
	Assets          []ResolvedAsset
	SourceFiles     []ResolvedSourceFile
}

// Input binds every typed stage from one Resolve invocation plus the closed
// metadata that cannot be recovered after resolution without I/O.
type Input struct {
	Resolve    resolve.Result
	Definition definition.Result
	Graph      graph.Result
	Query      query.Result
	View       view.Result
	Resolved   ResolvedMetadata
}

// Result publishes either a complete immutable Project snapshot or a complete
// immutable Pack snapshot. A rejected result contains no artifact or hashes.
type Result struct {
	state       *resultState
	Diagnostics []resolve.Diagnostic
	HasErrors   bool
}

type resultState struct {
	snapshot       Snapshot
	validatedInput Input
}

// Snapshot is a defensive copy of a successful result. Exactly one of
// Document and Pack is present.
type Snapshot struct {
	Document      *NormalizedDocument
	Pack          *NormalizedPackArtifact
	CanonicalJSON []byte
	ArtifactJSON  []byte
	Hashes        Hashes
}

// Snapshot returns storage independent from the compiler result and from all
// previously returned snapshots.
func (r Result) Snapshot() Snapshot {
	if r.state == nil {
		return Snapshot{}
	}
	return deepClone(r.state.snapshot)
}

// MatchesResolve reports whether this successful result belongs to resolved.
func (r Result) MatchesResolve(resolved resolve.Result) bool {
	return r.state != nil && r.state.snapshot.Hashes.generation.Matches(resolved.Generation())
}

// ValidatedInputSnapshot returns the exact closed compiler input accepted by
// materialization. Downstream internal stages consume this defensive copy so
// caller mutations cannot mix generations or resolved trees.
func (r Result) ValidatedInputSnapshot() (Input, bool) {
	if r.state == nil {
		return Input{}, false
	}
	return deepClone(r.state.validatedInput), true
}

// NormalizedDocument is the exact Project envelope. Pack compilation never
// fabricates this value.
type NormalizedDocument struct {
	Format        string                `json:"format"`
	SchemaVersion int                   `json:"schema_version"`
	Language      int                   `json:"language"`
	Project       Project               `json:"project"`
	Dependencies  []ResolvedPackSummary `json:"dependencies"`
	EntityTypes   []EntityType          `json:"entity_types"`
	RelationTypes []RelationType        `json:"relation_types"`
	Layers        []Layer               `json:"layers"`
	Entities      []Entity              `json:"entities"`
	Relations     []Relation            `json:"relations"`
	Queries       []Query               `json:"queries"`
	Views         []View                `json:"views"`
	References    []Reference           `json:"references"`
	Assets        []AssetBlobSummary    `json:"assets"`
	Identity      IdentityHistory       `json:"identity"`
}

// NormalizedPackArtifact is the exact Pack envelope. It intentionally has no
// Project, Layer, Entity, Relation, or row-bearing graph collection.
type NormalizedPackArtifact struct {
	Format        string                `json:"format"`
	SchemaVersion int                   `json:"schema_version"`
	Language      int                   `json:"language"`
	Pack          PackRoot              `json:"pack"`
	Dependencies  []ResolvedPackSummary `json:"dependencies"`
	EntityTypes   []EntityType          `json:"entity_types"`
	RelationTypes []RelationType        `json:"relation_types"`
	Queries       []Query               `json:"queries"`
	Views         []View                `json:"views"`
	References    []Reference           `json:"references"`
	Assets        []AssetBlobSummary    `json:"assets"`
	Identity      IdentityHistory       `json:"identity"`
}

type PackRoot struct {
	Address     string `json:"address"`
	CanonicalID string `json:"canonical_id"`
}

type ResolvedPackSummary struct {
	Address     string `json:"address"`
	CanonicalID string `json:"canonical_id"`
	Version     string `json:"version"`
	Digest      string `json:"digest"`
}

type Common struct {
	Description *string           `json:"description,omitempty"`
	Tags        []string          `json:"tags"`
	Annotations map[string]string `json:"annotations"`
}

type Project struct {
	Common
	ID          string `json:"id"`
	Address     string `json:"address"`
	DisplayName string `json:"display_name"`
}

type Layer struct {
	Common
	ID          string `json:"id"`
	Address     string `json:"address"`
	DisplayName string `json:"display_name"`
	Order       int64  `json:"order"`
}

type AssetRef struct {
	Digest    string `json:"digest"`
	MediaType string `json:"media_type"`
}

type AssetBlobSummary struct {
	Digest     string `json:"digest"`
	MediaType  string `json:"media_type"`
	ByteLength int64  `json:"byte_length"`
}

type Representation struct {
	Kind  definition.RepresentationKind   `json:"kind"`
	Shape *definition.RepresentationShape `json:"shape,omitempty"`
}

type EntityType struct {
	Common
	ID                    string             `json:"id"`
	Address               string             `json:"address"`
	DisplayName           string             `json:"display_name"`
	Icon                  *string            `json:"icon,omitempty"`
	Image                 *AssetRef          `json:"image,omitempty"`
	Color                 *string            `json:"color,omitempty"`
	Representation        Representation     `json:"representation"`
	Columns               []Column           `json:"columns"`
	UniqueConstraints     []UniqueConstraint `json:"unique_constraints"`
	ReservedColumnIDs     []string           `json:"reserved_column_ids"`
	ReservedConstraintIDs []string           `json:"reserved_constraint_ids"`
}

type Column struct {
	ID                 string                   `json:"id"`
	Address            string                   `json:"address"`
	DisplayName        string                   `json:"display_name"`
	ValueType          definition.ScalarType    `json:"value_type"`
	EnumValues         []string                 `json:"enum_values,omitempty"`
	ReservedEnumValues []string                 `json:"reserved_enum_values"`
	Required           bool                     `json:"required"`
	Default            *Scalar                  `json:"default,omitempty"`
	Format             *definition.StringFormat `json:"format,omitempty"`
	Min                *float64                 `json:"min,omitempty"`
	Max                *float64                 `json:"max,omitempty"`
	MinLength          *int64                   `json:"min_length,omitempty"`
	MaxLength          *int64                   `json:"max_length,omitempty"`
}

type UniqueConstraint struct {
	ID              string   `json:"id"`
	Address         string   `json:"address"`
	ColumnAddresses []string `json:"column_addresses"`
}

// Scalar retains the stage type for validation/indexing while serializing as
// the normalized primitive required by Language 1.
type Scalar struct {
	Type   definition.ScalarType `json:"-"`
	String string                `json:"-"`
	Int    int64                 `json:"-"`
	Float  float64               `json:"-"`
	Bool   bool                  `json:"-"`
}

func (s Scalar) MarshalJSON() ([]byte, error) {
	switch s.Type {
	case definition.ScalarString, definition.ScalarEnum, definition.ScalarDate, definition.ScalarDatetime:
		return json.Marshal(s.String)
	case definition.ScalarInteger:
		return json.Marshal(s.Int)
	case definition.ScalarNumber:
		return json.Marshal(s.Float)
	case definition.ScalarBoolean:
		return json.Marshal(s.Bool)
	default:
		return nil, fmt.Errorf("unsupported scalar type %q", s.Type)
	}
}

type AttributeRow struct {
	ID      string            `json:"id"`
	Address string            `json:"address"`
	Values  map[string]Scalar `json:"values"`
}

type Entity struct {
	Common
	ID             string         `json:"id"`
	Address        string         `json:"address"`
	DisplayName    string         `json:"display_name"`
	TypeAddress    string         `json:"type_address"`
	LayerAddress   string         `json:"layer_address"`
	Rows           []AttributeRow `json:"rows"`
	ReservedRowIDs []string       `json:"reserved_row_ids"`
}

type EndpointRule struct {
	Role                string   `json:"role"`
	EntityTypeAddresses []string `json:"entity_type_addresses,omitempty"`
	LayerAddresses      []string `json:"layer_addresses,omitempty"`
}

type CardinalityBound struct {
	Min int                `json:"min"`
	Max CardinalityMaximum `json:"max"`
}

type CardinalityMaximum struct {
	Many  bool
	Value int
}

func (m CardinalityMaximum) MarshalJSON() ([]byte, error) {
	if m.Many {
		return json.Marshal("many")
	}
	return json.Marshal(m.Value)
}

type Cardinality struct {
	ToPerFrom CardinalityBound `json:"to_per_from"`
	FromPerTo CardinalityBound `json:"from_per_to"`
}

type TraversalPolicy struct {
	DefaultDirection               definition.TraversalDirection `json:"default_direction"`
	ParticipatesInImpact           bool                          `json:"participates_in_impact"`
	ParticipatesInFlow             bool                          `json:"participates_in_flow"`
	ParticipatesInHierarchy        bool                          `json:"participates_in_hierarchy"`
	ParticipatesInDependencyMatrix bool                          `json:"participates_in_dependency_matrix"`
}

type ComposedProjection struct {
	Mode            definition.ComposedProjectionMode `json:"mode"`
	Priority        int64                             `json:"priority"`
	Conflict        definition.ProjectionConflict     `json:"conflict"`
	KeepEdge        bool                              `json:"keep_edge"`
	ParentEndpoint  *definition.ProjectionEndpoint    `json:"parent_endpoint,omitempty"`
	ChildEndpoint   *definition.ProjectionEndpoint    `json:"child_endpoint,omitempty"`
	OverlayEndpoint *definition.ProjectionEndpoint    `json:"overlay_endpoint,omitempty"`
	TargetEndpoint  *definition.ProjectionEndpoint    `json:"target_endpoint,omitempty"`
	BadgeEndpoint   *definition.ProjectionEndpoint    `json:"badge_endpoint,omitempty"`
}

type DiagramProjection struct {
	Mode                definition.DiagramProjectionMode `json:"mode"`
	SourceEndpoint      definition.ProjectionEndpoint    `json:"source_endpoint"`
	TargetEndpoint      definition.ProjectionEndpoint    `json:"target_endpoint"`
	EdgeLabel           definition.ProjectionLabel       `json:"edge_label"`
	IncludeRelationType bool                             `json:"include_relation_type"`
}

type TableProjection struct {
	RowMode             definition.TableRowMode `json:"row_mode"`
	IncludeFrom         bool                    `json:"include_from"`
	IncludeTo           bool                    `json:"include_to"`
	IncludeRelationType bool                    `json:"include_relation_type"`
}

type MatrixProjection struct {
	RowEndpoint         definition.ProjectionEndpoint `json:"row_endpoint"`
	ColumnEndpoint      definition.ProjectionEndpoint `json:"column_endpoint"`
	IncludeRelationRows bool                          `json:"include_relation_rows"`
}

type TreeProjection struct {
	ParentEndpoint definition.ProjectionEndpoint `json:"parent_endpoint"`
	ChildEndpoint  definition.ProjectionEndpoint `json:"child_endpoint"`
}

type FlowProjection struct {
	SourceEndpoint           definition.ProjectionEndpoint `json:"source_endpoint"`
	TargetEndpoint           definition.ProjectionEndpoint `json:"target_endpoint"`
	ConnectorKind            definition.FlowConnectorKind  `json:"connector_kind"`
	BranchValueColumnAddress *string                       `json:"branch_value_column_address,omitempty"`
}

type ContextProjection struct {
	FactTemplate         string  `json:"fact_template"`
	ReverseFactTemplate  *string `json:"reverse_fact_template,omitempty"`
	IncludeAttributeRows bool    `json:"include_attribute_rows"`
}

type ProjectionSet struct {
	Composed ComposedProjection `json:"composed"`
	Diagram  DiagramProjection  `json:"diagram"`
	Table    TableProjection    `json:"table"`
	Matrix   *MatrixProjection  `json:"matrix,omitempty"`
	Tree     *TreeProjection    `json:"tree,omitempty"`
	Flow     *FlowProjection    `json:"flow,omitempty"`
	Context  ContextProjection  `json:"context"`
}

type EdgeRender struct {
	Arrow definition.RenderArrow     `json:"arrow"`
	Line  definition.RenderLine      `json:"line"`
	Color *string                    `json:"color,omitempty"`
	Label definition.ProjectionLabel `json:"label"`
}

type NestedRender struct {
	FrameLabel definition.RenderFrameLabel `json:"frame_label"`
	FrameStyle definition.RenderFrameStyle `json:"frame_style"`
}

type OverlayRender struct {
	Kind     string                    `json:"kind"`
	Position definition.RenderPosition `json:"position"`
	MaxItems int64                     `json:"max_items"`
}

type BadgeRender struct {
	Icon     *string                     `json:"icon,omitempty"`
	Label    definition.RenderBadgeLabel `json:"label"`
	Position definition.RenderPosition   `json:"position"`
}

type RenderSet struct {
	Edge    EdgeRender    `json:"edge"`
	Nested  NestedRender  `json:"nested"`
	Overlay OverlayRender `json:"overlay"`
	Badge   BadgeRender   `json:"badge"`
}

type RelationExport struct {
	IncludeEndpoints    bool    `json:"include_endpoints"`
	IncludeRelationRows bool    `json:"include_relation_rows"`
	SheetName           *string `json:"sheet_name,omitempty"`
}

type RelationType struct {
	Common
	ID                    string                          `json:"id"`
	Address               string                          `json:"address"`
	DisplayName           string                          `json:"display_name"`
	SemanticKind          definition.RelationSemanticKind `json:"semantic_kind"`
	AllowSelf             bool                            `json:"allow_self"`
	DuplicatePolicy       definition.DuplicatePolicy      `json:"duplicate_policy"`
	From                  EndpointRule                    `json:"from"`
	To                    EndpointRule                    `json:"to"`
	Cardinality           Cardinality                     `json:"cardinality"`
	ForwardLabel          string                          `json:"forward_label"`
	ReverseLabel          *string                         `json:"reverse_label,omitempty"`
	Columns               []Column                        `json:"columns"`
	UniqueConstraints     []UniqueConstraint              `json:"unique_constraints"`
	Traversal             TraversalPolicy                 `json:"traversal"`
	Projections           ProjectionSet                   `json:"projections"`
	Render                RenderSet                       `json:"render"`
	Export                RelationExport                  `json:"export"`
	ReservedColumnIDs     []string                        `json:"reserved_column_ids"`
	ReservedConstraintIDs []string                        `json:"reserved_constraint_ids"`
}

type Relation struct {
	Common
	ID             string         `json:"id"`
	Address        string         `json:"address"`
	DisplayName    *string        `json:"display_name,omitempty"`
	TypeAddress    string         `json:"type_address"`
	FromAddress    string         `json:"from_address"`
	ToAddress      string         `json:"to_address"`
	Rows           []AttributeRow `json:"rows"`
	ReservedRowIDs []string       `json:"reserved_row_ids"`
}

type Query struct {
	Common
	ID                   string               `json:"id"`
	Address              string               `json:"address"`
	DisplayName          string               `json:"display_name"`
	StateInput           query.StatePolicy    `json:"state_input"`
	Parameters           []QueryParameter     `json:"parameters"`
	Select               QuerySelect          `json:"select"`
	Where                Predicate            `json:"where"`
	RelationWhere        Predicate            `json:"relation_where"`
	Traverse             *QueryTraversal      `json:"traverse,omitempty"`
	Result               []query.ResultMember `json:"result"`
	ReservedParameterIDs []string             `json:"reserved_parameter_ids"`
}

type QueryParameter struct {
	ID                 string                   `json:"id"`
	Address            string                   `json:"address"`
	ValueType          definition.ScalarType    `json:"value_type"`
	EnumValues         []string                 `json:"enum_values,omitempty"`
	ReservedEnumValues []string                 `json:"reserved_enum_values"`
	Required           bool                     `json:"required"`
	Default            *Scalar                  `json:"default,omitempty"`
	Format             *definition.StringFormat `json:"format,omitempty"`
	Min                *float64                 `json:"min,omitempty"`
	Max                *float64                 `json:"max,omitempty"`
	MinLength          *int64                   `json:"min_length,omitempty"`
	MaxLength          *int64                   `json:"max_length,omitempty"`
}

type QuerySelect struct {
	LayerAddresses        *[]string `json:"layer_addresses,omitempty"`
	EntityTypeAddresses   *[]string `json:"entity_type_addresses,omitempty"`
	RelationTypeAddresses *[]string `json:"relation_type_addresses,omitempty"`
	RootAddresses         *[]string `json:"root_addresses,omitempty"`
}

type Predicate struct {
	Kind          query.PredicateKind  `json:"kind"`
	Children      []Predicate          `json:"children,omitempty"`
	Child         *Predicate           `json:"child,omitempty"`
	Field         string               `json:"field,omitempty"`
	FieldPath     query.StateFieldPath `json:"field_path,omitempty"`
	Operator      query.Operator       `json:"operator,omitempty"`
	Value         *PredicateValue      `json:"value,omitempty"`
	Quantifier    query.RowQuantifier  `json:"quantifier,omitempty"`
	TypeAddresses []string             `json:"type_addresses,omitempty"`
	RowPredicate  *RowPredicate        `json:"predicate,omitempty"`
}

// MarshalJSON preserves the discriminated-union shape while retaining the
// required empty children array for all/any predicates.
func (p Predicate) MarshalJSON() ([]byte, error) {
	type wire struct {
		Kind          query.PredicateKind  `json:"kind"`
		Children      *[]Predicate         `json:"children,omitempty"`
		Child         *Predicate           `json:"child,omitempty"`
		Field         string               `json:"field,omitempty"`
		FieldPath     query.StateFieldPath `json:"field_path,omitempty"`
		Operator      query.Operator       `json:"operator,omitempty"`
		Value         *PredicateValue      `json:"value,omitempty"`
		Quantifier    query.RowQuantifier  `json:"quantifier,omitempty"`
		TypeAddresses []string             `json:"type_addresses,omitempty"`
		RowPredicate  *RowPredicate        `json:"predicate,omitempty"`
	}
	var children *[]Predicate
	if p.Kind == query.PredicateAll || p.Kind == query.PredicateAny {
		values := p.Children
		if values == nil {
			values = []Predicate{}
		}
		children = &values
	}
	return json.Marshal(wire{Kind: p.Kind, Children: children, Child: p.Child, Field: p.Field, FieldPath: p.FieldPath, Operator: p.Operator, Value: p.Value, Quantifier: p.Quantifier, TypeAddresses: p.TypeAddresses, RowPredicate: p.RowPredicate})
}

type RowPredicate struct {
	Kind            query.PredicateKind  `json:"kind"`
	Children        []RowPredicate       `json:"children,omitempty"`
	Child           *RowPredicate        `json:"child,omitempty"`
	ColumnAddresses []string             `json:"column_addresses,omitempty"`
	FieldPath       query.StateFieldPath `json:"field_path,omitempty"`
	Operator        query.Operator       `json:"operator,omitempty"`
	Value           *PredicateValue      `json:"value,omitempty"`
}

// MarshalJSON applies the same required-array rule to nested row predicates.
func (p RowPredicate) MarshalJSON() ([]byte, error) {
	type wire struct {
		Kind            query.PredicateKind  `json:"kind"`
		Children        *[]RowPredicate      `json:"children,omitempty"`
		Child           *RowPredicate        `json:"child,omitempty"`
		ColumnAddresses []string             `json:"column_addresses,omitempty"`
		FieldPath       query.StateFieldPath `json:"field_path,omitempty"`
		Operator        query.Operator       `json:"operator,omitempty"`
		Value           *PredicateValue      `json:"value,omitempty"`
	}
	var children *[]RowPredicate
	if p.Kind == query.PredicateAll || p.Kind == query.PredicateAny {
		values := p.Children
		if values == nil {
			values = []RowPredicate{}
		}
		children = &values
	}
	return json.Marshal(wire{Kind: p.Kind, Children: children, Child: p.Child, ColumnAddresses: p.ColumnAddresses, FieldPath: p.FieldPath, Operator: p.Operator, Value: p.Value})
}

type PredicateValue struct {
	Kind             query.PredicateValueKind `json:"kind"`
	Scalar           *Scalar                  `json:"-"`
	Address          *string                  `json:"-"`
	Scalars          []Scalar                 `json:"-"`
	Addresses        []string                 `json:"-"`
	ParameterAddress string                   `json:"parameter_address,omitempty"`
}

func (v PredicateValue) MarshalJSON() ([]byte, error) {
	if v.Kind == query.ValueParameter {
		return json.Marshal(struct {
			Kind             query.PredicateValueKind `json:"kind"`
			ParameterAddress string                   `json:"parameter_address"`
		}{Kind: v.Kind, ParameterAddress: v.ParameterAddress})
	}
	if v.Kind != query.ValueLiteral {
		return nil, fmt.Errorf("unsupported predicate value kind %q", v.Kind)
	}
	switch {
	case v.Scalar != nil:
		return marshalLiteralPredicateValue(v.Kind, v.Scalar)
	case v.Address != nil:
		return marshalLiteralPredicateValue(v.Kind, v.Address)
	case v.Scalars != nil:
		return marshalLiteralPredicateValue(v.Kind, v.Scalars)
	case v.Addresses != nil:
		return marshalLiteralPredicateValue(v.Kind, v.Addresses)
	default:
		return nil, fmt.Errorf("literal predicate value has no closed value")
	}
}

type literalPredicateValue interface {
	*Scalar | *string | []Scalar | []string
}

func marshalLiteralPredicateValue[T literalPredicateValue](kind query.PredicateValueKind, value T) ([]byte, error) {
	return json.Marshal(struct {
		Kind  query.PredicateValueKind `json:"kind"`
		Value T                        `json:"value"`
	}{Kind: kind, Value: value})
}

type QueryTraversal struct {
	Direction             definition.TraversalDirection `json:"direction"`
	MinDepth              int64                         `json:"min_depth"`
	MaxDepth              int64                         `json:"max_depth"`
	CyclePolicy           query.CyclePolicy             `json:"cycle_policy"`
	RelationTypeAddresses *[]string                     `json:"relation_type_addresses,omitempty"`
}

type View struct {
	Common
	ID                     string                        `json:"id"`
	Address                string                        `json:"address"`
	DisplayName            string                        `json:"display_name"`
	StateInput             query.StatePolicy             `json:"state_input"`
	Category               view.Category                 `json:"category"`
	Intent                 *string                       `json:"intent,omitempty"`
	Source                 ViewSource                    `json:"source"`
	RelationProjections    map[string]ProjectionOverride `json:"relation_projection_overrides"`
	Shape                  ViewShape                     `json:"shape"`
	Exports                []ExportRecipe                `json:"exports"`
	ReservedTableColumnIDs []string                      `json:"reserved_table_column_ids"`
	ReservedExportIDs      []string                      `json:"reserved_export_ids"`
}

type ViewSource struct {
	Kind         view.SourceKind   `json:"kind"`
	QueryAddress *string           `json:"query_address,omitempty"`
	Arguments    map[string]Scalar `json:"arguments"`
	Before       *string           `json:"before,omitempty"`
	After        *string           `json:"after,omitempty"`
}

// ProjectionOverride emits the validated effective override explicitly. A
// complete value is valid for the Language schema's optional fields and avoids
// reinterpreting source presence after the View stage has merged fallbacks.
type ProjectionOverride struct {
	Composed *ComposedProjection `json:"composed,omitempty"`
	Diagram  *DiagramProjection  `json:"diagram,omitempty"`
	Table    *TableProjection    `json:"table,omitempty"`
	Matrix   *MatrixProjection   `json:"matrix,omitempty"`
	Tree     *TreeProjection     `json:"tree,omitempty"`
	Flow     *FlowProjection     `json:"flow,omitempty"`
	Context  *ContextProjection  `json:"context,omitempty"`
	Render   *RenderSet          `json:"render,omitempty"`
}

type ViewShape struct {
	Kind    view.ShapeKind `json:"kind"`
	Diagram *DiagramShape  `json:"diagram,omitempty"`
	Table   *TableShape    `json:"table,omitempty"`
	Matrix  *MatrixShape   `json:"matrix,omitempty"`
	Tree    *TreeShape     `json:"tree,omitempty"`
	Flow    *FlowShape     `json:"flow,omitempty"`
	Context *ContextShape  `json:"context,omitempty"`
	Diff    *DiffShape     `json:"diff,omitempty"`
}

// MarshalJSON flattens the in-memory discriminated union into the exact
// Language 1 tagged-union object.
func (s ViewShape) MarshalJSON() ([]byte, error) {
	switch s.Kind {
	case view.ShapeDiagram:
		return json.Marshal(struct {
			Kind view.ShapeKind `json:"kind"`
			*DiagramShape
		}{s.Kind, s.Diagram})
	case view.ShapeTable:
		return json.Marshal(struct {
			Kind view.ShapeKind `json:"kind"`
			*TableShape
		}{s.Kind, s.Table})
	case view.ShapeMatrix:
		return json.Marshal(struct {
			Kind view.ShapeKind `json:"kind"`
			*MatrixShape
		}{s.Kind, s.Matrix})
	case view.ShapeTree:
		return json.Marshal(struct {
			Kind view.ShapeKind `json:"kind"`
			*TreeShape
		}{s.Kind, s.Tree})
	case view.ShapeFlow:
		return json.Marshal(struct {
			Kind view.ShapeKind `json:"kind"`
			*FlowShape
		}{s.Kind, s.Flow})
	case view.ShapeContext:
		return json.Marshal(struct {
			Kind view.ShapeKind `json:"kind"`
			*ContextShape
		}{s.Kind, s.Context})
	case view.ShapeDiff:
		return json.Marshal(struct {
			Kind view.ShapeKind `json:"kind"`
			*DiffShape
		}{s.Kind, s.Diff})
	default:
		return nil, fmt.Errorf("unsupported View shape %q", s.Kind)
	}
}

type DiagramShape struct {
	Layout      view.DiagramLayout      `json:"layout"`
	Direction   view.DiagramDirection   `json:"direction"`
	Abstraction view.DiagramAbstraction `json:"abstraction"`
	Composed    bool                    `json:"composed"`
	Placements  []Placement             `json:"placements"`
}

type Placement struct {
	EntityAddress string  `json:"entity_address"`
	X             float64 `json:"x"`
	Y             float64 `json:"y"`
	Width         float64 `json:"width"`
	Height        float64 `json:"height"`
}

type TableShape struct {
	RowSource           view.TableRowSource `json:"row_source"`
	EntityTypeAddresses *[]string           `json:"entity_type_addresses,omitempty"`
	IncludeEntityID     bool                `json:"include_entity_id"`
	IncludeType         bool                `json:"include_type"`
	IncludeLayer        bool                `json:"include_layer"`
	Columns             []TableColumn       `json:"columns"`
	Sorts               []TableSort         `json:"sorts"`
}

type TableColumn struct {
	ID        string            `json:"id"`
	Address   string            `json:"address"`
	Label     *string           `json:"label,omitempty"`
	Source    TableColumnSource `json:"source"`
	Aggregate view.Aggregate    `json:"aggregate"`
}

type TableColumnSource struct {
	Kind                  view.TableColumnSourceKind    `json:"kind"`
	Field                 string                        `json:"field,omitempty"`
	ColumnAddresses       []string                      `json:"column_addresses,omitempty"`
	Endpoint              definition.ProjectionEndpoint `json:"endpoint,omitempty"`
	Direction             definition.TraversalDirection `json:"direction,omitempty"`
	RelationTypeAddresses *[]string                     `json:"relation_type_addresses,omitempty"`
	StateFieldPath        query.StateFieldPath          `json:"field_path,omitempty"`
}

type TableSort struct {
	ColumnID  string             `json:"column_id"`
	Direction view.SortDirection `json:"direction"`
	Absent    view.AbsentOrder   `json:"absent"`
}

type MatrixShape struct {
	RowAxis    MatrixAxis `json:"row_axis"`
	ColumnAxis MatrixAxis `json:"column_axis"`
	Cell       MatrixCell `json:"cell"`
}

type MatrixAxis struct {
	EntityTypeAddresses *[]string           `json:"entity_type_addresses,omitempty"`
	LabelField          view.AxisLabelField `json:"label_field"`
}

type MatrixCell struct {
	RelationTypeAddresses    *[]string                     `json:"relation_type_addresses,omitempty"`
	Direction                definition.TraversalDirection `json:"direction"`
	Semantic                 view.MatrixSemantic           `json:"semantic"`
	Display                  view.MatrixDisplay            `json:"display"`
	AttributeColumnAddresses *[]string                     `json:"attribute_column_addresses,omitempty"`
}

type TreeShape struct {
	RelationTypeAddresses []string               `json:"relation_type_addresses"`
	CyclePolicy           view.TreeCyclePolicy   `json:"cycle_policy"`
	SharedChildPolicy     view.SharedChildPolicy `json:"shared_child_policy"`
}

type FlowShape struct {
	RelationTypeAddresses []string             `json:"relation_type_addresses"`
	LaneBy                view.LaneBy          `json:"lane_by"`
	LaneColumnAddresses   *[]string            `json:"lane_column_addresses,omitempty"`
	CyclePolicy           view.FlowCyclePolicy `json:"cycle_policy"`
	PreserveParallel      bool                 `json:"preserve_parallel"`
}

type ContextShape struct {
	GroupBy             view.ContextGroupBy `json:"group_by"`
	IncludeEntityRows   bool                `json:"include_entity_rows"`
	IncludeRelationRows bool                `json:"include_relation_rows"`
	Incoming            bool                `json:"incoming"`
	Outgoing            bool                `json:"outgoing"`
}

type DiffShape struct {
	Include     []view.DiffSubjectKind `json:"include"`
	DetectMoves bool                   `json:"detect_moves"`
}

type ExportRecipe struct {
	ID              string                `json:"id"`
	Address         string                `json:"address"`
	Format          exportrecipe.Format   `json:"format"`
	Filename        string                `json:"filename"`
	Fidelity        exportrecipe.Fidelity `json:"fidelity"`
	SourceRefs      bool                  `json:"source_refs"`
	ExporterProfile ExporterProfileRef    `json:"exporter_profile"`
	Options         ExportOptions         `json:"options"`
}

type ExporterProfileRef struct {
	ID                    string              `json:"id"`
	Format                exportrecipe.Format `json:"format"`
	RegistrySchemaVersion int                 `json:"registry_schema_version"`
	RegistryDigest        string              `json:"registry_digest"`
	SpecificationDigest   string              `json:"specification_digest"`
}

type ExportOptions struct {
	Kind           exportrecipe.Format       `json:"kind"`
	Diagnostics    *bool                     `json:"diagnostics,omitempty"`
	StateSummary   *bool                     `json:"state_summary,omitempty"`
	Width          *Dimension                `json:"width,omitempty"`
	Height         *Dimension                `json:"height,omitempty"`
	Scale          *float64                  `json:"scale,omitempty"`
	Background     *string                   `json:"background,omitempty"`
	PageSize       *exportrecipe.PageSize    `json:"page_size,omitempty"`
	Orientation    *exportrecipe.Orientation `json:"orientation,omitempty"`
	Fit            *exportrecipe.Fit         `json:"fit,omitempty"`
	Legend         *bool                     `json:"legend,omitempty"`
	Interactive    *bool                     `json:"interactive,omitempty"`
	EmbedAssets    *bool                     `json:"embed_assets,omitempty"`
	Bundle         *bool                     `json:"bundle,omitempty"`
	Header         *bool                     `json:"header,omitempty"`
	SourceManifest *bool                     `json:"source_manifest,omitempty"`
	Profile        *exportrecipe.XLSXProfile `json:"profile,omitempty"`
	LookupSheets   *bool                     `json:"lookup_sheets,omitempty"`
	HiddenIDs      *bool                     `json:"hidden_ids,omitempty"`
	Formulas       *bool                     `json:"formulas,omitempty"`
	ViewDataJSON   *bool                     `json:"view_data_json,omitempty"`
}

type Dimension struct {
	Auto  bool
	Value int64
}

func (d Dimension) MarshalJSON() ([]byte, error) {
	if d.Auto {
		return json.Marshal("auto")
	}
	return json.Marshal(d.Value)
}

type Reference struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Text    string `json:"text"`
}

type IdentityHistory struct {
	RootReservations map[string]map[SubjectKind][]string `json:"root_reservations"`
	Moves            []Move                              `json:"moves"`
	MoveClosure      []MoveResolution                    `json:"move_closure"`
}

type Move struct {
	Kind         SubjectKind `json:"kind"`
	OwnerAddress *string     `json:"owner_address,omitempty"`
	OldAddress   string      `json:"old_address"`
	NewAddress   string      `json:"new_address"`
}

type MoveResolution struct {
	Kind            SubjectKind `json:"kind"`
	OwnerAddress    *string     `json:"owner_address,omitempty"`
	SourceAddress   string      `json:"source_address"`
	TerminalAddress string      `json:"terminal_address"`
}

type Hashes struct {
	generation  resolve.StageGeneration
	Definition  string         `json:"definition"`
	Graph       *string        `json:"graph,omitempty"`
	OwnSubjects []SubjectHash  `json:"own_subjects"`
	Subtrees    []SubtreeHash  `json:"subtrees"`
	ChildSets   []ChildSetHash `json:"child_sets"`
}

type SubjectHash struct {
	Address string      `json:"address"`
	Kind    SubjectKind `json:"kind"`
	Hash    string      `json:"hash"`
}

type SubtreeHash struct {
	OwnerAddress string `json:"owner_address"`
	Hash         string `json:"hash"`
}

type ChildSetHash struct {
	OwnerAddress string      `json:"owner_address"`
	ChildKind    SubjectKind `json:"child_kind"`
	Addresses    []string    `json:"child_addresses"`
	Hash         string      `json:"hash"`
}
