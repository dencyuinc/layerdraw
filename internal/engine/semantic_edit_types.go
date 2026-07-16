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
	SemanticValueToken   SemanticValueKind = "token"
)

type SemanticBlobRef struct {
	BlobID    string
	Digest    string
	Lifetime  string
	MediaType string
	Size      uint64
}

// SemanticValue is a closed recursive value. Blob values retain their complete
// verified reference metadata; the pure planner never fetches or stages bytes.
type SemanticValue struct {
	Kind    SemanticValueKind
	Address string
	Array   []SemanticValue
	Blob    string
	BlobRef *SemanticBlobRef
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
	Module PlannedModuleRef
	Digest string
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
	PlannedSourceMove    PlannedSourceEditKind = "move"
	PlannedSourceReplace PlannedSourceEditKind = "replace"
)

// PlannedModuleRef and PlannedBlobRef mirror the frozen portable source-edit
// identities without importing transport/generated packages into Engine.
type PlannedModuleRef struct {
	OriginKind  SourceOriginKind
	PackAddress string
	ModulePath  string
}

type PlannedBlobRef struct {
	BlobID    string
	Digest    string
	Lifetime  string
	MediaType string
	Size      uint64
	Bytes     []byte
}

// PlannedSourceEdit is the complete in-process SourceEdit union. Replacement
// bytes are owned by the plan and mapped to a request-lifetime BlobRef only at
// the endpoint boundary.
type PlannedSourceEdit struct {
	Kind            PlannedSourceEditKind
	BeforeModule    *PlannedModuleRef
	AfterModule     *PlannedModuleRef
	SourceRange     *SourceRange
	BeforeDigest    string
	AfterDigest     string
	ReplacementBlob *PlannedBlobRef
	// Byte-edit coordinates are retained for private overlay rebasing; they are
	// never serialized as independent wire fields.
	ModulePath  string
	StartByte   int
	EndByte     int
	Replacement []byte
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

// IdentityLineage is authored rename/migration authority. Semantic diff never
// infers identity from structural similarity; only these exact edges may pair
// a before and after StableAddress.
type IdentityLineage struct {
	ChangeKind    SemanticChangeKind
	Kind          SemanticSubjectKind
	BeforeAddress string
	AfterAddress  string
}

// SemanticPlanLimits are the generated Workbench limits after checked decimal
// conversion. Zero values are accepted only for direct in-process callers and
// select conservative compiler-derived defaults.
type SemanticPlanLimits struct {
	MaxItems       int64
	MaxOutputBytes int64
}

type SemanticDocumentGeneration struct {
	EndpointInstanceID string
	DocumentHandle     string
	Value              string
}

// SemanticRebaseAuthority is supplied only by a facade that retained both
// immutable generations for the same endpoint-bound document. The pure
// planner validates this closed provenance before attempting a three-way
// semantic rebase; an arbitrary snapshot pair is never treated as ancestry.
type SemanticRebaseAuthority struct {
	AncestorGeneration     SemanticDocumentGeneration
	CurrentGeneration      SemanticDocumentGeneration
	AncestorDefinitionHash string
	CurrentDefinitionHash  string
}

// SemanticEditPlanInput binds operations to the exact immutable source and
// compile generation from which every precondition and diff is derived.
type SemanticEditPlanInput struct {
	BaseInput CompileInput
	// BaseSnapshot is the ancestor generation named by Preconditions. BaseInput
	// is the current head to which the batch is rebased.
	BaseSnapshot    Snapshot
	Batch           SemanticOperationBatch
	Preconditions   SemanticEditPreconditions
	Generation      SemanticDocumentGeneration
	RebaseAuthority *SemanticRebaseAuthority
	Limits          SemanticPlanLimits
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
