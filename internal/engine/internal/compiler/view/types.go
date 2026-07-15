// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package view compiles resolved LDL View declarations into closed, typed,
// renderer-independent static recipes. It does not execute Queries, consume or
// produce ViewData or ExportPlan, perform layout, or access runtime state.
package view

import (
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/exportrecipe"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
)

type Input struct {
	Resolve    resolve.Result
	Definition definition.Result
	Graph      graph.Result
	Query      query.Result
	Registry   *exportrecipe.ProfileRegistry
}

type Result struct {
	stageGeneration resolve.StageGeneration
	Recipes         []Recipe
	ExportRecipes   exportrecipe.Result
	Diagnostics     []resolve.Diagnostic
	HasErrors       bool
}

func (r Result) MatchesResolve(resolved resolve.Result) bool {
	return r.stageGeneration.Matches(resolved.Generation())
}

func (r Result) Generation() resolve.StageGeneration {
	return r.stageGeneration
}

type Recipe struct {
	definition.Common
	ID                     string
	Address                string
	DisplayName            string
	Category               Category
	Intent                 *string
	StateInput             query.StatePolicy
	StateRequirement       query.StatePolicy
	Source                 Source
	RelationProjections    []RelationProjection
	Shape                  Shape
	Exports                []exportrecipe.Recipe
	ReservedTableColumnIDs []string
	ReservedExportIDs      []string
	Dependencies           Dependencies
}

type Category string

const (
	CategoryTopology   Category = "topology"
	CategoryInventory  Category = "inventory"
	CategoryDependency Category = "dependency"
	CategoryHierarchy  Category = "hierarchy"
	CategoryFlow       Category = "flow"
	CategoryImpact     Category = "impact"
	CategoryDiff       Category = "diff"
	CategoryContext    Category = "context"
)

type SourceKind string

const (
	SourceQuery SourceKind = "query"
	SourceDiff  SourceKind = "diff"
)

type Source struct {
	Kind  SourceKind
	Query *QuerySource
	Diff  *DiffSource
}

type QuerySource struct {
	QueryAddress string
	Arguments    []Argument
}

type DiffSource struct {
	Before       string
	After        string
	QueryAddress *string
	Arguments    []Argument
}

type Argument struct {
	ParameterAddress string
	Value            definition.Scalar
	Defaulted        bool
}

type RelationProjection struct {
	RelationTypeAddress string
	Projections         definition.ProjectionSet
	Render              definition.RenderSet
}

type ShapeKind string

const (
	ShapeDiagram ShapeKind = "diagram"
	ShapeTable   ShapeKind = "table"
	ShapeMatrix  ShapeKind = "matrix"
	ShapeTree    ShapeKind = "tree"
	ShapeFlow    ShapeKind = "flow"
	ShapeContext ShapeKind = "context"
	ShapeDiff    ShapeKind = "diff"
)

type Shape struct {
	Kind    ShapeKind
	Diagram *DiagramShape
	Table   *TableShape
	Matrix  *MatrixShape
	Tree    *TreeShape
	Flow    *FlowShape
	Context *ContextShape
	Diff    *DiffShape
}

type DiagramShape struct {
	Layout      DiagramLayout
	Direction   DiagramDirection
	Abstraction DiagramAbstraction
	Composed    bool
	Placements  []Placement
}

type DiagramLayout string

const (
	LayoutLayered DiagramLayout = "layered"
	LayoutForce   DiagramLayout = "force"
	LayoutGrid    DiagramLayout = "grid"
	LayoutRadial  DiagramLayout = "radial"
	LayoutManual  DiagramLayout = "manual"
)

type DiagramDirection string

const (
	DirectionLeftToRight DiagramDirection = "left_to_right"
	DirectionRightToLeft DiagramDirection = "right_to_left"
	DirectionTopToBottom DiagramDirection = "top_to_bottom"
	DirectionBottomToTop DiagramDirection = "bottom_to_top"
)

type DiagramAbstraction string

const (
	AbstractionSummary DiagramAbstraction = "summary"
	AbstractionNormal  DiagramAbstraction = "normal"
	AbstractionDetail  DiagramAbstraction = "detail"
)

type Placement struct {
	EntityAddress string
	X             float64
	Y             float64
	Width         float64
	Height        float64
}

type TableShape struct {
	RowSource                TableRowSource
	AutomaticRelationColumns []string
	EntityTypeAddresses      *[]string
	IncludeEntityID          bool
	IncludeType              bool
	IncludeLayer             bool
	Columns                  []TableColumn
	Sorts                    []TableSort
}

type TableRowSource string

const (
	RowsEntity             TableRowSource = "entity"
	RowsEntityRows         TableRowSource = "entity_rows"
	RowsRelation           TableRowSource = "relation"
	RowsRelationRows       TableRowSource = "relation_rows"
	RowsAutomaticRelations TableRowSource = "automatic_relations"
)

type TableColumn struct {
	ID        string
	Address   string
	Label     *string
	Source    TableColumnSource
	Aggregate Aggregate
	ValueType TableValueType
}

type TableValueKind string

const (
	TableValueScalar        TableValueKind = "scalar"
	TableValueStableAddress TableValueKind = "stable_address"
	TableValueStringSet     TableValueKind = "string_set"
)

type TableValueType struct {
	Kind       TableValueKind
	ScalarType definition.ScalarType
	EnumValues []string
	Format     *definition.StringFormat
}

type TableColumnSourceKind string

const (
	ColumnField            TableColumnSourceKind = "field"
	ColumnAttribute        TableColumnSourceKind = "attribute"
	ColumnRelationEndpoint TableColumnSourceKind = "relation_endpoint"
	ColumnDerivedCount     TableColumnSourceKind = "derived_count"
	ColumnState            TableColumnSourceKind = "state"
)

type TableColumnSource struct {
	Kind                  TableColumnSourceKind
	Field                 string
	ColumnAddresses       []string
	Endpoint              definition.ProjectionEndpoint
	Direction             definition.TraversalDirection
	RelationTypeAddresses *[]string
	StateFieldPath        query.StateFieldPath
}

type Aggregate string

const (
	AggregateNone          Aggregate = "none"
	AggregateCount         Aggregate = "count"
	AggregateCountDistinct Aggregate = "count_distinct"
	AggregateMin           Aggregate = "min"
	AggregateMax           Aggregate = "max"
	AggregateJoinUnique    Aggregate = "join_unique"
)

type TableSort struct {
	ColumnID  string
	Direction SortDirection
	Absent    AbsentOrder
}

type SortDirection string

const (
	SortAscending  SortDirection = "ascending"
	SortDescending SortDirection = "descending"
)

type AbsentOrder string

const (
	AbsentFirst AbsentOrder = "first"
	AbsentLast  AbsentOrder = "last"
)

type MatrixShape struct {
	RowAxis    MatrixAxis
	ColumnAxis MatrixAxis
	Cell       MatrixCell
}

type MatrixAxis struct {
	EntityTypeAddresses *[]string
	LabelField          AxisLabelField
}

type AxisLabelField string

const (
	AxisLabelID          AxisLabelField = "id"
	AxisLabelDisplayName AxisLabelField = "display_name"
	AxisLabelType        AxisLabelField = "type"
	AxisLabelLayer       AxisLabelField = "layer"
)

type MatrixCell struct {
	RelationTypeAddresses    *[]string
	Direction                definition.TraversalDirection
	Semantic                 MatrixSemantic
	Display                  MatrixDisplay
	AttributeColumnAddresses *[]string
}

type MatrixSemantic string

const (
	MatrixRelationRefs MatrixSemantic = "relation_refs"
	MatrixPathRefs     MatrixSemantic = "path_refs"
)

type MatrixDisplay string

const (
	MatrixExists           MatrixDisplay = "exists"
	MatrixCount            MatrixDisplay = "count"
	MatrixRelationTypes    MatrixDisplay = "relation_types"
	MatrixAttributeSummary MatrixDisplay = "attribute_summary"
)

type TreeShape struct {
	RelationTypeAddresses []string
	CyclePolicy           TreeCyclePolicy
	SharedChildPolicy     SharedChildPolicy
}

type TreeCyclePolicy string

const (
	TreeCycleError               TreeCyclePolicy = "error"
	TreeCycleTruncate            TreeCyclePolicy = "truncate"
	TreeCycleDuplicateOccurrence TreeCyclePolicy = "duplicate_occurrence"
)

type SharedChildPolicy string

const (
	SharedChildError               SharedChildPolicy = "error"
	SharedChildDuplicateOccurrence SharedChildPolicy = "duplicate_occurrence"
	SharedChildLink                SharedChildPolicy = "link"
)

type FlowShape struct {
	RelationTypeAddresses []string
	LaneBy                LaneBy
	LaneColumnAddresses   *[]string
	CyclePolicy           FlowCyclePolicy
	PreserveParallel      bool
}

type LaneBy string

const (
	LaneNone       LaneBy = "none"
	LaneLayer      LaneBy = "layer"
	LaneEntityType LaneBy = "entity_type"
	LaneAttribute  LaneBy = "attribute"
)

type FlowCyclePolicy string

const (
	FlowCycleError           FlowCyclePolicy = "error"
	FlowCycleTruncate        FlowCyclePolicy = "truncate"
	FlowCycleIncludeCycleRef FlowCyclePolicy = "include_cycle_ref"
)

type ContextShape struct {
	GroupBy             ContextGroupBy
	IncludeEntityRows   bool
	IncludeRelationRows bool
	Incoming            bool
	Outgoing            bool
}

type ContextGroupBy string

const (
	ContextGroupNone       ContextGroupBy = "none"
	ContextGroupLayer      ContextGroupBy = "layer"
	ContextGroupEntityType ContextGroupBy = "entity_type"
)

type DiffShape struct {
	Include     []DiffSubjectKind
	DetectMoves bool
}

type DiffSubjectKind string

const (
	DiffProject                DiffSubjectKind = "project"
	DiffPack                   DiffSubjectKind = "pack"
	DiffEntityType             DiffSubjectKind = "entity_type"
	DiffRelationType           DiffSubjectKind = "relation_type"
	DiffLayer                  DiffSubjectKind = "layer"
	DiffEntity                 DiffSubjectKind = "entity"
	DiffRelation               DiffSubjectKind = "relation"
	DiffQuery                  DiffSubjectKind = "query"
	DiffView                   DiffSubjectKind = "view"
	DiffReference              DiffSubjectKind = "reference"
	DiffEntityTypeColumn       DiffSubjectKind = "entity_type_column"
	DiffEntityTypeConstraint   DiffSubjectKind = "entity_type_constraint"
	DiffRelationTypeColumn     DiffSubjectKind = "relation_type_column"
	DiffRelationTypeConstraint DiffSubjectKind = "relation_type_constraint"
	DiffEntityRow              DiffSubjectKind = "entity_row"
	DiffRelationRow            DiffSubjectKind = "relation_row"
	DiffQueryParameter         DiffSubjectKind = "query_parameter"
	DiffViewTableColumn        DiffSubjectKind = "view_table_column"
	DiffViewExport             DiffSubjectKind = "view_export"
)

type Dependencies struct {
	QueryAddresses        []string
	ParameterAddresses    []string
	LayerAddresses        []string
	EntityTypeAddresses   []string
	RelationTypeAddresses []string
	EntityAddresses       []string
	RelationAddresses     []string
	ColumnAddresses       []string
	ExportAddresses       []string
	StateReads            []query.StateReadDependency
}
