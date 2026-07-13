// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package definition compiles resolved LDL declarations into typed definition semantics.
package definition

import "github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"

type Input struct {
	Resolve resolve.Result
}

type Result struct {
	stageGeneration resolve.StageGeneration
	Root            Root
	Project         *Project
	Pack            *Pack
	EntityTypes     []EntityType
	RelationTypes   []RelationType
	Layers          []Layer
	References      []Reference
	Dependencies    []resolve.ResolvedPackSummary
	Identity        IdentityHistory
	Diagnostics     []resolve.Diagnostic
	HasErrors       bool
}

// MatchesResolve reports whether this result was compiled from exactly the
// supplied resolve generation.
func (r Result) MatchesResolve(resolved resolve.Result) bool {
	return r.stageGeneration.Matches(resolved.Generation())
}

// Generation returns the opaque token for propagation to the next internal
// compiler stage.
func (r Result) Generation() resolve.StageGeneration {
	return r.stageGeneration
}

type Root struct {
	Mode    resolve.CompileMode
	Address string
}

type IdentityHistory struct {
	RootReservations map[string]map[resolve.SubjectKind][]string
	Moves            []Move
	MoveClosure      []MoveResolution
}

type Move struct {
	Kind         resolve.SubjectKind
	OwnerAddress *string
	OldAddress   string
	NewAddress   string
}

type MoveResolution struct {
	Kind            resolve.SubjectKind
	OwnerAddress    *string
	SourceAddress   string
	TerminalAddress string
}

type Pack struct {
	Address     string
	CanonicalID string
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
	symbol      resolve.StableSymbol
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
	Kind  RepresentationKind
	Shape RepresentationShape
}

type RepresentationKind string

const (
	RepresentationContainer RepresentationKind = "container"
	RepresentationTable     RepresentationKind = "table"
	RepresentationShapeKind RepresentationKind = "shape"
)

type RepresentationShape string

const (
	ShapeRect     RepresentationShape = "rect"
	ShapeRounded  RepresentationShape = "rounded"
	ShapeEllipse  RepresentationShape = "ellipse"
	ShapeDiamond  RepresentationShape = "diamond"
	ShapeCylinder RepresentationShape = "cylinder"
	ShapeCloud    RepresentationShape = "cloud"
	ShapeHexagon  RepresentationShape = "hexagon"
	ShapePerson   RepresentationShape = "person"
	ShapeDevice   RepresentationShape = "device"
)

type Column struct {
	ID                 string
	Address            string
	DisplayName        string
	ValueType          ScalarType
	EnumValues         []string
	ReservedEnumValues []string
	Required           bool
	Default            *Scalar
	Format             *StringFormat
	Min                *float64
	Max                *float64
	MinLength          *int64
	MaxLength          *int64
}

type StringFormat string

const (
	StringFormatURI      StringFormat = "uri"
	StringFormatEmail    StringFormat = "email"
	StringFormatHostname StringFormat = "hostname"
	StringFormatIPv4     StringFormat = "ipv4"
	StringFormatIPv6     StringFormat = "ipv6"
	StringFormatCIDR     StringFormat = "cidr"
)

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
	SemanticKind          RelationSemanticKind
	AllowSelf             bool
	DuplicatePolicy       DuplicatePolicy
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

type RelationSemanticKind string

const (
	RelationDependency  RelationSemanticKind = "dependency"
	RelationDataFlow    RelationSemanticKind = "data_flow"
	RelationControlFlow RelationSemanticKind = "control_flow"
	RelationDeployment  RelationSemanticKind = "deployment"
	RelationNetwork     RelationSemanticKind = "network"
	RelationSecurity    RelationSemanticKind = "security"
	RelationContainment RelationSemanticKind = "containment"
	RelationOwnership   RelationSemanticKind = "ownership"
	RelationSequence    RelationSemanticKind = "sequence"
	RelationImpact      RelationSemanticKind = "impact"
	RelationReference   RelationSemanticKind = "reference"
	RelationGovernance  RelationSemanticKind = "governance"
)

type DuplicatePolicy string

const (
	DuplicateAllow                            DuplicatePolicy = "allow"
	DuplicateDenySameTypeBetweenSameEndpoints DuplicatePolicy = "deny_same_type_between_same_endpoints"
	DuplicateDenyAnyBetweenSameEndpoints      DuplicatePolicy = "deny_any_between_same_endpoints"
)

type EndpointRule struct {
	Role                string
	EntityTypeAddresses []string
	LayerAddresses      []string
}

type CardinalityBound struct {
	Min int
	Max CardinalityMaximum
}

type CardinalityMaximum uint8

const (
	CardinalityMaximumOne CardinalityMaximum = iota + 1
	CardinalityMaximumMany
)

type Cardinality struct {
	ToPerFrom CardinalityBound
	FromPerTo CardinalityBound
}

type TraversalPolicy struct {
	DefaultDirection               TraversalDirection
	ParticipatesInImpact           bool
	ParticipatesInFlow             bool
	ParticipatesInHierarchy        bool
	ParticipatesInDependencyMatrix bool
}

type TraversalDirection string

const (
	TraversalOutgoing TraversalDirection = "outgoing"
	TraversalIncoming TraversalDirection = "incoming"
	TraversalBoth     TraversalDirection = "both"
)

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
	Mode            ComposedProjectionMode
	Priority        int64
	Conflict        ProjectionConflict
	KeepEdge        bool
	ParentEndpoint  *ProjectionEndpoint
	ChildEndpoint   *ProjectionEndpoint
	OverlayEndpoint *ProjectionEndpoint
	TargetEndpoint  *ProjectionEndpoint
	BadgeEndpoint   *ProjectionEndpoint
}

type ComposedProjectionMode string

const (
	ComposedEdge    ComposedProjectionMode = "edge"
	ComposedNest    ComposedProjectionMode = "nest"
	ComposedOverlay ComposedProjectionMode = "overlay"
	ComposedBadge   ComposedProjectionMode = "badge"
	ComposedHide    ComposedProjectionMode = "hide"
)

type ProjectionConflict string

const (
	ProjectionConflictKeepEdge    ProjectionConflict = "keep_edge"
	ProjectionConflictPreferFirst ProjectionConflict = "prefer_first"
	ProjectionConflictDiagnostic  ProjectionConflict = "diagnostic"
)

type ProjectionEndpoint string

const (
	ProjectionEndpointFrom ProjectionEndpoint = "from"
	ProjectionEndpointTo   ProjectionEndpoint = "to"
)

type DiagramProjection struct {
	Mode                DiagramProjectionMode
	SourceEndpoint      ProjectionEndpoint
	TargetEndpoint      ProjectionEndpoint
	EdgeLabel           ProjectionLabel
	IncludeRelationType bool
}

type DiagramProjectionMode string

const (
	DiagramEdge DiagramProjectionMode = "edge"
	DiagramHide DiagramProjectionMode = "hide"
)

type ProjectionLabel string

const (
	ProjectionLabelType         ProjectionLabel = "type"
	ProjectionLabelDisplayName  ProjectionLabel = "display_name"
	ProjectionLabelForwardLabel ProjectionLabel = "forward_label"
	ProjectionLabelReverseLabel ProjectionLabel = "reverse_label"
	ProjectionLabelNone         ProjectionLabel = "none"
)

type TableProjection struct {
	RowMode             TableRowMode
	IncludeFrom         bool
	IncludeTo           bool
	IncludeRelationType bool
}

type TableRowMode string

const (
	TableRowsRelation     TableRowMode = "relation"
	TableRowsRelationRows TableRowMode = "relation_rows"
	TableRowsAutomatic    TableRowMode = "automatic"
)

type MatrixProjection struct {
	RowEndpoint         ProjectionEndpoint
	ColumnEndpoint      ProjectionEndpoint
	IncludeRelationRows bool
}

type TreeProjection struct {
	ParentEndpoint ProjectionEndpoint
	ChildEndpoint  ProjectionEndpoint
}

type FlowProjection struct {
	SourceEndpoint           ProjectionEndpoint
	TargetEndpoint           ProjectionEndpoint
	ConnectorKind            FlowConnectorKind
	BranchValueColumnAddress *string
}

type FlowConnectorKind string

const (
	FlowConnectorSequence FlowConnectorKind = "sequence"
	FlowConnectorControl  FlowConnectorKind = "control"
	FlowConnectorData     FlowConnectorKind = "data"
	FlowConnectorMessage  FlowConnectorKind = "message"
	FlowConnectorError    FlowConnectorKind = "error"
)

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
	Arrow RenderArrow
	Line  RenderLine
	Color *string
	Label ProjectionLabel
}

type RenderArrow string

const (
	RenderArrowForward  RenderArrow = "forward"
	RenderArrowBackward RenderArrow = "backward"
	RenderArrowBoth     RenderArrow = "both"
	RenderArrowNone     RenderArrow = "none"
)

type RenderLine string

const (
	RenderLineSolid  RenderLine = "solid"
	RenderLineDashed RenderLine = "dashed"
	RenderLineDotted RenderLine = "dotted"
)

type NestedRender struct {
	FrameLabel RenderFrameLabel
	FrameStyle RenderFrameStyle
}

type RenderFrameLabel string

const (
	RenderFrameLabelParent      RenderFrameLabel = "parent"
	RenderFrameLabelType        RenderFrameLabel = "type"
	RenderFrameLabelDisplayName RenderFrameLabel = "display_name"
	RenderFrameLabelNone        RenderFrameLabel = "none"
)

type RenderFrameStyle string

const (
	RenderFrameSubtle RenderFrameStyle = "subtle"
	RenderFrameStrong RenderFrameStyle = "strong"
	RenderFrameNone   RenderFrameStyle = "none"
)

type OverlayRender struct {
	Kind     string
	Position RenderPosition
	MaxItems int64
}

type BadgeRender struct {
	Icon     *string
	Label    RenderBadgeLabel
	Position RenderPosition
}

type RenderPosition string

const (
	RenderPositionTopLeft     RenderPosition = "top_left"
	RenderPositionTopRight    RenderPosition = "top_right"
	RenderPositionBottomLeft  RenderPosition = "bottom_left"
	RenderPositionBottomRight RenderPosition = "bottom_right"
	RenderPositionCenter      RenderPosition = "center"
)

type RenderBadgeLabel string

const (
	RenderBadgeLabelType        RenderBadgeLabel = "type"
	RenderBadgeLabelDisplayName RenderBadgeLabel = "display_name"
	RenderBadgeLabelCount       RenderBadgeLabel = "count"
	RenderBadgeLabelNone        RenderBadgeLabel = "none"
)

type RelationExport struct {
	IncludeEndpoints    bool
	IncludeRelationRows bool
	SheetName           *string
}
