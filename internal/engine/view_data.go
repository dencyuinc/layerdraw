// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

// ViewMaterializationInput is a closed source union. Exactly one of Query or
// Diff must be present and must match Recipe.Source.
type ViewMaterializationInput struct {
	Recipe CompiledViewRecipe
	Query  *QueryViewMaterializationInput
	Diff   *DiffViewMaterializationInput
}

// QueryViewMaterializationInput binds a QueryResult to one immutable
// definition revision. StateSnapshot is supplied only when the effective View
// state policy resolves to a snapshot.
type QueryViewMaterializationInput struct {
	RevisionID    string
	Snapshot      Snapshot
	QueryResult   QueryResult
	StateSnapshot *StateQuerySnapshot
}

// DiffViewMaterializationInput binds the recipe and both compared definitions.
// Query results are present on both sides only when the Diff source names a
// Query. Runtime revision lookup is intentionally outside this value.
type DiffViewMaterializationInput struct {
	RecipeRevisionID  string
	RecipeSnapshot    Snapshot
	BeforeRevisionID  string
	BeforeSnapshot    Snapshot
	AfterRevisionID   string
	AfterSnapshot     Snapshot
	BeforeQueryResult *QueryResult
	AfterQueryResult  *QueryResult
}

type ViewMaterializationResponse struct {
	Status      string
	Result      *ViewData
	Diagnostics []Diagnostic
}

type ViewDataKind string

const (
	ViewDataDiagram ViewDataKind = "diagram"
	ViewDataTable   ViewDataKind = "table"
	ViewDataMatrix  ViewDataKind = "matrix"
	ViewDataTree    ViewDataKind = "tree"
	ViewDataFlow    ViewDataKind = "flow"
	ViewDataContext ViewDataKind = "context"
	ViewDataDiff    ViewDataKind = "diff"
)

// ViewData is a closed discriminated union. Exactly one shape pointer is
// present and its embedded base Kind must match that pointer.
type ViewData struct {
	Diagram *DiagramViewData
	Table   *TableViewData
	Matrix  *MatrixViewData
	Tree    *TreeViewData
	Flow    *FlowViewData
	Context *ContextViewData
	Diff    *DiffViewData
}

type ViewDataBase struct {
	Kind           ViewDataKind
	Category       string
	Shape          view.Shape
	ProjectAddress string
	ViewAddress    string
	QueryAddress   *string
	Revision       ViewRevision
	StatePolicy    string
	StateInput     QueryStateInputRef
	Source         ViewDataSourceRefs
	Diagnostics    []Diagnostic
}

type ViewRevision struct {
	Single *SingleRevision
	Diff   *DiffRevision
}

type SingleRevision struct {
	Kind           string
	RevisionID     string
	DefinitionHash string
}

type DiffRevision struct {
	Kind                 string
	RecipeRevisionID     string
	RecipeDefinitionHash string
	BeforeRevisionID     string
	BeforeDefinitionHash string
	AfterRevisionID      string
	AfterDefinitionHash  string
}

type ViewDataSourceRefs struct {
	SubjectAddresses  []string
	EntityAddresses   []string
	RelationAddresses []string
	LayerAddresses    []string
	RowAddresses      []string
	CellRefs          []ViewDataCellRef
	AssetDigests      []string
	State             ViewDataStateRefs
}

type ViewDataStateRefs struct {
	Reads []StateReadRef
}

type ViewDataCellRef struct {
	RowAddress    string
	ColumnAddress string
}

type DiagramViewData struct {
	ViewDataBase
	Occurrences  []DiagramOccurrence
	Edges        []DiagramEdge
	Containers   []DiagramContainer
	Overlays     []DiagramOverlay
	Badges       []DiagramBadge
	SupportItems []DiagramSupportItem
}

type DiagramOccurrenceRole string

const (
	DiagramRoleNode      DiagramOccurrenceRole = "node"
	DiagramRoleContainer DiagramOccurrenceRole = "container"
	DiagramRoleSupport   DiagramOccurrenceRole = "support"
)

type DiagramOccurrence struct {
	Key                string
	EntityAddress      string
	LayerAddress       string
	ParentKey          *string
	ViaRelationAddress *string
	Role               DiagramOccurrenceRole
	Source             ViewDataSourceRefs
}

type DiagramEdge struct {
	Key                 string
	FromOccurrenceKey   string
	ToOccurrenceKey     string
	RelationAddress     string
	RelationTypeAddress string
	Source              ViewDataSourceRefs
}

type DiagramContainer struct {
	Key           string
	OccurrenceKey string
	ChildKeys     []string
	Source        ViewDataSourceRefs
}

type DiagramOverlay struct {
	Key                  string
	TargetOccurrenceKey  string
	OverlayEntityAddress string
	RelationAddress      string
	RelationTypeAddress  string
	Source               ViewDataSourceRefs
}

type DiagramBadge struct {
	Key                 string
	TargetOccurrenceKey string
	RelationAddress     string
	RelationTypeAddress string
	Label               *string
	Source              ViewDataSourceRefs
}

type DiagramSupportKind string

const (
	DiagramSupportHiddenRelation DiagramSupportKind = "hidden_relation"
	DiagramSupportHiddenEntity   DiagramSupportKind = "hidden_entity"
	DiagramSupportSourceOnly     DiagramSupportKind = "source_only"
)

type DiagramSupportItem struct {
	Key             string
	SupportKind     DiagramSupportKind
	EntityAddress   *string
	RelationAddress *string
	Source          ViewDataSourceRefs
}

type TableViewData struct {
	ViewDataBase
	Columns []TableColumn
	Rows    []TableRow
}

type TableColumn struct {
	Key                   string
	ID                    string
	Address               *string
	Label                 string
	ValueType             string
	EnumValues            []string
	SourceColumnAddresses []string
	StateFieldPath        *string
}

type TableRow struct {
	Key    string
	Cells  map[string]TableCell
	Source ViewDataSourceRefs
}

type TableCell struct {
	Present bool
	Value   *ViewDataValue
	Source  ViewDataSourceRefs
}

type ViewDataValue struct {
	Kind      string
	Scalar    *TypedScalar
	Address   *string
	StringSet []string
}

type MatrixViewData struct {
	ViewDataBase
	RowAxis    []MatrixAxisItem
	ColumnAxis []MatrixAxisItem
	Cells      []MatrixCell
}

type MatrixAxisItem struct {
	Key           string
	EntityAddress string
	Label         string
	Source        ViewDataSourceRefs
}

type MatrixSemanticRef struct {
	RelationAddress *string
	Path            *QueryPath
}

type MatrixDisplayValue struct {
	Kind       string
	Boolean    bool
	Integer    int64
	StringSet  []string
	Attributes []MatrixAttributeItem
}

type MatrixAttributeItem struct {
	RelationAddress string
	RowAddress      string
	ColumnAddress   string
	Value           TypedScalar
}

type MatrixCell struct {
	Key          string
	RowKey       string
	ColumnKey    string
	SemanticRefs []MatrixSemanticRef
	DisplayValue MatrixDisplayValue
	Source       ViewDataSourceRefs
}

type TreeViewData struct {
	ViewDataBase
	Roots     []TreeOccurrence
	CycleRefs []TreeRef
	LinkRefs  []TreeRef
}

type TreeOccurrence struct {
	Key                string
	EntityAddress      string
	ViaRelationAddress *string
	Children           []TreeOccurrence
	Source             ViewDataSourceRefs
}

type TreeRef struct {
	Key               string
	FromOccurrenceKey string
	ToEntityAddress   string
	RelationAddress   string
	Source            ViewDataSourceRefs
}

type FlowViewData struct {
	ViewDataBase
	Lanes      []FlowLane
	Steps      []FlowStep
	Connectors []FlowConnector
	CycleRefs  []FlowCycleRef
}

type FlowLane struct {
	Key      string
	Label    string
	StepKeys []string
	Source   ViewDataSourceRefs
}

type FlowStep struct {
	Key           string
	EntityAddress string
	LaneKey       string
	Branch        bool
	Join          bool
	Source        ViewDataSourceRefs
}

type FlowConnectorKind string

const (
	FlowConnectorSequence FlowConnectorKind = "sequence"
	FlowConnectorControl  FlowConnectorKind = "control"
	FlowConnectorData     FlowConnectorKind = "data"
	FlowConnectorMessage  FlowConnectorKind = "message"
	FlowConnectorError    FlowConnectorKind = "error"
)

type FlowConnector struct {
	Key                string
	FromStepKey        string
	ToStepKey          string
	Kind               FlowConnectorKind
	BranchValue        *TypedScalar
	BranchRowAddresses []string
	RelationAddresses  []string
	Source             ViewDataSourceRefs
}

type FlowCycleRef struct {
	Key                string
	ConnectorKey       string
	FromStepKey        string
	ToStepKey          string
	Kind               FlowConnectorKind
	BranchValue        *TypedScalar
	BranchRowAddresses []string
	RelationAddresses  []string
	Source             ViewDataSourceRefs
}

type ContextViewData struct {
	ViewDataBase
	Groups []ContextGroup
}

type ContextGroup struct {
	Key        string
	Label      string
	Facts      []ContextFact
	Attributes []ContextAttribute
	Source     ViewDataSourceRefs
}

type ContextFactDirection string

const (
	ContextFactOutgoing ContextFactDirection = "outgoing"
	ContextFactIncoming ContextFactDirection = "incoming"
)

type ContextFact struct {
	Key             string
	Direction       ContextFactDirection
	Text            string
	EntityAddress   string
	RelationAddress string
	RowAddresses    []string
	Source          ViewDataSourceRefs
}

type ContextAttribute struct {
	Key          string
	GroupKey     string
	OwnerAddress string
	RowAddress   string
	Values       map[string]TypedScalar
	Source       ViewDataSourceRefs
}

type DiffViewData struct {
	ViewDataBase
	Changes []DiffChange
}

type DiffChangeKind string

const (
	DiffChangeAdded        DiffChangeKind = "added"
	DiffChangeRemoved      DiffChangeKind = "removed"
	DiffChangeUpdated      DiffChangeKind = "updated"
	DiffChangeMoved        DiffChangeKind = "moved"
	DiffChangeMovedUpdated DiffChangeKind = "moved_updated"
)

type DiffChange struct {
	Key           string
	Kind          DiffChangeKind
	SubjectKind   string
	BeforeAddress *string
	AfterAddress  *string
	Fields        []FieldDiff
	Source        ViewDataSourceRefs
	BeforeSource  *ViewDataSourceRefs
	AfterSource   *ViewDataSourceRefs
}

type FieldDiff struct {
	Key           string
	Path          []string
	BeforePresent bool
	Before        *SemanticValue
	AfterPresent  bool
	After         *SemanticValue
}

// Base returns the common base only when the union contains exactly one valid
// shape variant.
func (value ViewData) Base() (ViewDataBase, bool) {
	var base *ViewDataBase
	valid := true
	assign := func(candidate *ViewDataBase, expected ViewDataKind) {
		if candidate == nil || base != nil || candidate.Kind != expected {
			valid = false
			return
		}
		base = candidate
	}
	count := 0
	if value.Diagram != nil {
		count++
		assign(&value.Diagram.ViewDataBase, ViewDataDiagram)
	}
	if value.Table != nil {
		count++
		assign(&value.Table.ViewDataBase, ViewDataTable)
	}
	if value.Matrix != nil {
		count++
		assign(&value.Matrix.ViewDataBase, ViewDataMatrix)
	}
	if value.Tree != nil {
		count++
		assign(&value.Tree.ViewDataBase, ViewDataTree)
	}
	if value.Flow != nil {
		count++
		assign(&value.Flow.ViewDataBase, ViewDataFlow)
	}
	if value.Context != nil {
		count++
		assign(&value.Context.ViewDataBase, ViewDataContext)
	}
	if value.Diff != nil {
		count++
		assign(&value.Diff.ViewDataBase, ViewDataDiff)
	}
	if count != 1 || base == nil || !valid {
		return ViewDataBase{}, false
	}
	return deepClone(*base), true
}

func newViewDataItemKey(kind string, tuple any) (string, error) {
	hash, err := materialize.SemanticHash(materialize.DomainViewDataItem, tuple)
	if err != nil {
		return "", err
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(hash, "sha256:"))
	if err != nil || len(raw) != 32 {
		return "", fmt.Errorf("invalid ViewData item hash")
	}
	return "vdi:" + kind + ":" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func emptyViewDataSourceRefs() ViewDataSourceRefs {
	return ViewDataSourceRefs{
		SubjectAddresses: []string{}, EntityAddresses: []string{}, RelationAddresses: []string{},
		LayerAddresses: []string{}, RowAddresses: []string{}, CellRefs: []ViewDataCellRef{},
		AssetDigests: []string{}, State: ViewDataStateRefs{Reads: []StateReadRef{}},
	}
}

func canonicalViewDataSourceRefs(value ViewDataSourceRefs) ViewDataSourceRefs {
	value.EntityAddresses = sortedUniqueStableAddresses(value.EntityAddresses)
	value.RelationAddresses = sortedUniqueStableAddresses(value.RelationAddresses)
	value.LayerAddresses = sortedUniqueStableAddresses(value.LayerAddresses)
	value.RowAddresses = sortedUniqueStableAddresses(value.RowAddresses)
	value.AssetDigests = sortedUniqueStrings(value.AssetDigests)
	value.CellRefs = canonicalViewDataCellRefs(value.CellRefs)
	value.State.Reads = canonicalStateReads(value.State.Reads)
	all := append([]string{}, value.SubjectAddresses...)
	all = append(all, value.EntityAddresses...)
	all = append(all, value.RelationAddresses...)
	all = append(all, value.LayerAddresses...)
	all = append(all, value.RowAddresses...)
	for _, cell := range value.CellRefs {
		all = append(all, cell.RowAddress, cell.ColumnAddress)
	}
	for _, read := range value.State.Reads {
		all = append(all, read.SubjectAddress)
	}
	value.SubjectAddresses = sortedUniqueStableAddresses(all)
	return value
}

func mergeViewDataSourceRefs(values ...ViewDataSourceRefs) ViewDataSourceRefs {
	out := emptyViewDataSourceRefs()
	for _, value := range values {
		out.SubjectAddresses = append(out.SubjectAddresses, value.SubjectAddresses...)
		out.EntityAddresses = append(out.EntityAddresses, value.EntityAddresses...)
		out.RelationAddresses = append(out.RelationAddresses, value.RelationAddresses...)
		out.LayerAddresses = append(out.LayerAddresses, value.LayerAddresses...)
		out.RowAddresses = append(out.RowAddresses, value.RowAddresses...)
		out.CellRefs = append(out.CellRefs, value.CellRefs...)
		out.AssetDigests = append(out.AssetDigests, value.AssetDigests...)
		out.State.Reads = append(out.State.Reads, value.State.Reads...)
	}
	return canonicalViewDataSourceRefs(out)
}

func canonicalViewDataCellRefs(values []ViewDataCellRef) []ViewDataCellRef {
	out := append([]ViewDataCellRef{}, values...)
	sort.Slice(out, func(i, j int) bool {
		if compared := compareStableAddressText(out[i].RowAddress, out[j].RowAddress); compared != 0 {
			return compared < 0
		}
		return compareStableAddressText(out[i].ColumnAddress, out[j].ColumnAddress) < 0
	})
	result := make([]ViewDataCellRef, 0, len(out))
	for _, value := range out {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func canonicalStateReads(values []StateReadRef) []StateReadRef {
	out := append([]StateReadRef{}, values...)
	sort.Slice(out, func(i, j int) bool {
		if compared := compareStableAddressText(out[i].SubjectAddress, out[j].SubjectAddress); compared != 0 {
			return compared < 0
		}
		return query.CompareStateFieldPaths(query.StateFieldPath(out[i].FieldPath), query.StateFieldPath(out[j].FieldPath)) < 0
	})
	result := make([]StateReadRef, 0, len(out))
	for _, value := range out {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func sortedUniqueStableAddresses(values []string) []string {
	out := append([]string{}, values...)
	sort.Slice(out, func(i, j int) bool { return compareStableAddressText(out[i], out[j]) < 0 })
	result := make([]string, 0, len(out))
	for _, value := range out {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func sortedUniqueStrings(values []string) []string {
	out := append([]string{}, values...)
	sort.Strings(out)
	result := make([]string, 0, len(out))
	for _, value := range out {
		if value != "" && (len(result) == 0 || result[len(result)-1] != value) {
			result = append(result, value)
		}
	}
	return result
}
