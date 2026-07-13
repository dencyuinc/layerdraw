// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package definition compiles resolved LDL declarations into typed definition semantics.
package definition

import "github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"

type Input struct {
	Resolve resolve.Result
}

type Result struct {
	Root          Root
	Project       *Project
	Pack          *Pack
	EntityTypes   []EntityType
	RelationTypes []RelationType
	Layers        []Layer
	References    []Reference
	Dependencies  []resolve.ResolvedPackSummary
	Identity      resolve.IdentityHistory
	Diagnostics   []resolve.Diagnostic
	HasErrors     bool
}

type Root struct {
	Mode    resolve.CompileMode
	Address string
}

type Pack struct {
	Address string
}

type Common struct {
	Description *string
	Tags        []string
	Annotations map[string]string
}

type Project struct {
	Common
	ID          string
	Address     string
	DisplayName string
}

type Layer struct {
	Common
	ID          string
	Address     string
	DisplayName string
	Order       int64
}

type EntityType struct {
	Common
	ID                    string
	Address               string
	DisplayName           string
	Icon                  *string
	Image                 *AuthoredAsset
	Color                 *string
	Representation        Representation
	Columns               []Column
	UniqueConstraints     []UniqueConstraint
	ReservedColumnIDs     []string
	ReservedConstraintIDs []string
}

type AuthoredAsset struct {
	AuthoredPath string
	Locator      string
	Origin       resolve.Origin
	ModulePath   string
	SourceRange  *resolve.SourceRange
}

type Representation struct {
	Kind  string
	Shape string
}

type Column struct {
	ID                 string
	Address            string
	DisplayName        string
	ValueType          ScalarType
	EnumValues         []string
	ReservedEnumValues []string
	Required           bool
	Default            *Scalar
	Format             *string
	Min                *float64
	Max                *float64
	MinLength          *int64
	MaxLength          *int64
}

type ScalarType string

const (
	ScalarString   ScalarType = "string"
	ScalarInteger  ScalarType = "integer"
	ScalarNumber   ScalarType = "number"
	ScalarBoolean  ScalarType = "boolean"
	ScalarEnum     ScalarType = "enum"
	ScalarDate     ScalarType = "date"
	ScalarDatetime ScalarType = "datetime"
)

type Scalar struct {
	Type   ScalarType
	String string
	Int    int64
	Float  float64
	Bool   bool
}

type UniqueConstraint struct {
	ID              string
	Address         string
	ColumnAddresses []string
}

type Reference struct {
	ID      string
	Address string
	Text    string
}

type RelationType struct {
	Common
	ID                    string
	Address               string
	DisplayName           string
	SemanticKind          string
	AllowSelf             bool
	DuplicatePolicy       string
	From                  EndpointRule
	To                    EndpointRule
	Cardinality           Cardinality
	ForwardLabel          string
	ReverseLabel          *string
	Columns               []Column
	UniqueConstraints     []UniqueConstraint
	Traversal             TraversalPolicy
	Projections           ProjectionSet
	Render                RenderSet
	Export                RelationExport
	ReservedColumnIDs     []string
	ReservedConstraintIDs []string
}

type EndpointRule struct {
	Role                string
	EntityTypeAddresses []string
	LayerAddresses      []string
}

type CardinalityBound struct {
	Min int
	Max string
}

type Cardinality struct {
	ToPerFrom CardinalityBound
	FromPerTo CardinalityBound
}

type TraversalPolicy struct {
	DefaultDirection               string
	ParticipatesInImpact           bool
	ParticipatesInFlow             bool
	ParticipatesInHierarchy        bool
	ParticipatesInDependencyMatrix bool
}

type ProjectionSet struct {
	Composed ComposedProjection
	Diagram  DiagramProjection
	Table    TableProjection
	Matrix   *MatrixProjection
	Tree     *TreeProjection
	Flow     *FlowProjection
	Context  ContextProjection
}

type ComposedProjection struct {
	Mode            string
	Priority        int64
	Conflict        string
	KeepEdge        bool
	ParentEndpoint  *string
	ChildEndpoint   *string
	OverlayEndpoint *string
	TargetEndpoint  *string
	BadgeEndpoint   *string
}

type DiagramProjection struct {
	Mode                string
	SourceEndpoint      string
	TargetEndpoint      string
	EdgeLabel           string
	IncludeRelationType bool
}

type TableProjection struct {
	RowMode             string
	IncludeFrom         bool
	IncludeTo           bool
	IncludeRelationType bool
}

type MatrixProjection struct {
	RowEndpoint         string
	ColumnEndpoint      string
	IncludeRelationRows bool
}

type TreeProjection struct {
	ParentEndpoint string
	ChildEndpoint  string
}

type FlowProjection struct {
	SourceEndpoint           string
	TargetEndpoint           string
	ConnectorKind            string
	BranchValueColumnAddress *string
}

type ContextProjection struct {
	FactTemplate         string
	ReverseFactTemplate  *string
	IncludeAttributeRows bool
}

type RenderSet struct {
	Edge    EdgeRender
	Nested  NestedRender
	Overlay OverlayRender
	Badge   BadgeRender
}

type EdgeRender struct {
	Arrow string
	Line  string
	Color *string
	Label string
}

type NestedRender struct {
	FrameLabel string
	FrameStyle string
}

type OverlayRender struct {
	Kind     string
	Position string
	MaxItems int64
}

type BadgeRender struct {
	Icon     *string
	Label    string
	Position string
}

type RelationExport struct {
	IncludeEndpoints    bool
	IncludeRelationRows bool
	SheetName           *string
}
