// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

// SemanticValueKind is the closed authored-value vocabulary used by semantic
// operations. It mirrors, but does not alias, the generated Engine Protocol
// type; generated values cross the handwritten endpoint mapper only.
type SemanticValueKind string

const (
	SemanticValueAbsent  SemanticValueKind = "absent"
	SemanticValueAddress SemanticValueKind = "address"
	SemanticValueArray   SemanticValueKind = "array"
	SemanticValueBlob    SemanticValueKind = "blob"
	SemanticValueBoolean SemanticValueKind = "boolean"
	SemanticValueDecimal SemanticValueKind = "decimal"
	SemanticValueInteger SemanticValueKind = "integer"
	SemanticValueMap     SemanticValueKind = "map"
	SemanticValueString  SemanticValueKind = "string"
)

// SemanticValue is a closed recursive value. Blob values retain only their
// already-verified digest; the pure planner never fetches or stages bytes.
type SemanticValue struct {
	Kind    SemanticValueKind
	Address string
	Array   []SemanticValue
	Blob    string
	Boolean bool
	Decimal string
	Integer int64
	Map     []SemanticMapEntry
	String  string
}

type SemanticMapEntry struct {
	Key   string
	Value SemanticValue
}

type SemanticRowCell struct {
	ColumnAddress string
	Value         SemanticValue
}

type SemanticStringMapEntry struct {
	Key   string
	Value string
}

type SemanticOperationKind string

const (
	OperationCreateRelation         SemanticOperationKind = "create_relation"
	OperationCreateSubject          SemanticOperationKind = "create_subject"
	OperationDeleteRow              SemanticOperationKind = "delete_row"
	OperationDeleteSubject          SemanticOperationKind = "delete_subject"
	OperationMigrateProjectIdentity SemanticOperationKind = "migrate_project_identity"
	OperationMoveEntityToLayer      SemanticOperationKind = "move_entity_to_layer"
	OperationRenameSubject          SemanticOperationKind = "rename_subject"
	OperationUpdateRelationEndpoint SemanticOperationKind = "update_relation_endpoint"
	OperationUpdateSubjectField     SemanticOperationKind = "update_subject_field"
	OperationUpsertRow              SemanticOperationKind = "upsert_row"
)

// SemanticOperation contains the complete generated operation union in a
// planner-oriented closed representation. Fields is the generated
// subject-specific field object represented as a canonical ordered map.
type SemanticOperation struct {
	Kind                          SemanticOperationKind
	SubjectKind                   SemanticSubjectKind
	ParentAddress                 string
	TargetAddress                 string
	OwnerAddress                  string
	ProjectAddress                string
	RelationAddress               string
	RowAddress                    string
	EntityAddress                 string
	LayerAddress                  string
	TypeAddress                   string
	FromAddress                   string
	ToAddress                     string
	ID                            string
	NewID                         string
	NewProjectID                  string
	Endpoint                      string
	Path                          []string
	Action                        string
	Value                         *SemanticValue
	Fields                        []SemanticMapEntry
	Values                        []SemanticRowCell
	ExplicitAbsentColumnAddresses []string
	Placement                     *SemanticPlacementHint
}

type SemanticPlacementHint struct {
	ModulePath         string
	GroupAnchorAddress string
	Position           string
}

type SemanticOperationBatch struct {
	Operations []SemanticOperation
}

type ExpectedSemanticHash struct {
	Address string
	Hash    string
}

type ExpectedSemanticChildSet struct {
	OwnerAddress string
	ChildKind    SemanticSubjectKind
	Hash         string
}

type ExpectedSemanticSourceDigest struct {
	ModulePath string
	Digest     string
}

// SemanticEditPreconditions protect one immutable compile generation. The
// endpoint-bound document handle remains outside this pure primitive.
type SemanticEditPreconditions struct {
	ExpectedSubjectHashes []ExpectedSemanticHash
	ExpectedSubtreeHashes []ExpectedSemanticHash
	ExpectedChildSets     []ExpectedSemanticChildSet
	ExpectedSourceDigests []ExpectedSemanticSourceDigest
}

type SemanticConflictKind string

const (
	ConflictStaleRevision          SemanticConflictKind = "stale_revision"
	ConflictSubjectChanged         SemanticConflictKind = "subject_changed"
	ConflictSubtreeChanged         SemanticConflictKind = "subtree_changed"
	ConflictChildSetChanged        SemanticConflictKind = "child_set_changed"
	ConflictSameFieldChanged       SemanticConflictKind = "same_field_changed"
	ConflictDeleteVsUpdate         SemanticConflictKind = "delete_vs_update"
	ConflictDuplicateIdentity      SemanticConflictKind = "duplicate_identity"
	ConflictReferenceBroken        SemanticConflictKind = "reference_broken"
	ConflictSchemaRowIncompatible  SemanticConflictKind = "schema_row_incompatible"
	ConflictPlacementChanged       SemanticConflictKind = "placement_changed"
	ConflictProjectIdentityChanged SemanticConflictKind = "project_identity_changed"
)

type SemanticConflict struct {
	Kind          SemanticConflictKind
	TargetAddress string
	OwnerAddress  string
	ChildKind     SemanticSubjectKind
	Path          []string
}

type PlannedSourceEditKind string

const (
	PlannedSourceCreate  PlannedSourceEditKind = "create"
	PlannedSourceDelete  PlannedSourceEditKind = "delete"
	PlannedSourceReplace PlannedSourceEditKind = "replace"
)

// PlannedSourceEdit is an in-process source edit. Replacement owns its bytes.
type PlannedSourceEdit struct {
	Kind         PlannedSourceEditKind
	ModulePath   string
	StartByte    int
	EndByte      int
	BeforeDigest string
	AfterDigest  string
	Replacement  []byte
}

type PlannedSourceDiff struct {
	Edits  []PlannedSourceEdit
	Digest string
}

type SemanticChangeKind string

const (
	SemanticCreated          SemanticChangeKind = "created"
	SemanticUpdated          SemanticChangeKind = "updated"
	SemanticDeleted          SemanticChangeKind = "deleted"
	SemanticRenamed          SemanticChangeKind = "renamed"
	SemanticMoved            SemanticChangeKind = "moved"
	SemanticReferenceChanged SemanticChangeKind = "reference_changed"
)

type AuthoredFieldPath struct{ Tokens []string }

type SemanticDiffEntry struct {
	Kind              SemanticChangeKind
	SubjectKind       SemanticSubjectKind
	BeforeAddress     string
	AfterAddress      string
	BeforeHash        string
	AfterHash         string
	OwnerAddress      string
	ChangedFieldPaths []AuthoredFieldPath
}

type PlannedSemanticDiff struct {
	Entries []SemanticDiffEntry
	Digest  string
}

type GraphAuthoringFacts struct {
	EntityTypeAddresses     []string
	RelationTypeAddresses   []string
	LayerAddresses          []string
	ColumnAddresses         []string
	EndpointEntityAddresses []string
	ActionFlags             []string
}

type AuthoringImpactAction string

type AuthoringImpactEntry struct {
	Capability        AuthoringCapability
	Action            AuthoringImpactAction
	SubjectKind       SemanticSubjectKind
	SubjectAddress    string
	OwnerAddress      string
	ChangedFieldPaths []AuthoredFieldPath
	BeforeRefs        []string
	AfterRefs         []string
	SourceRefs        []SourceRange
	GraphFacts        *GraphAuthoringFacts
}

type PlannedAuthoringImpact struct {
	BaseDefinitionHash      string
	ResultingDefinitionHash string
	SemanticDiffHash        string
	SourceDiffHash          string
	Entries                 []AuthoringImpactEntry
	RequiredCapabilities    []AuthoringCapability
	ImpactDigest            string
}

// SemanticEditPlanInput binds operations to the exact immutable source and
// compile generation from which every precondition and diff is derived.
type SemanticEditPlanInput struct {
	BaseInput     CompileInput
	BaseSnapshot  Snapshot
	Batch         SemanticOperationBatch
	Preconditions SemanticEditPreconditions
}

type SemanticEditPlan struct {
	Status             string
	ChangedSourceFiles []string
	SourceTree         map[string][]byte
	SourceDiff         PlannedSourceDiff
	SemanticDiff       PlannedSemanticDiff
	AuthoringImpact    *PlannedAuthoringImpact
	Result             *Snapshot
	Conflicts          []SemanticConflict
	Diagnostics        []Diagnostic
}
