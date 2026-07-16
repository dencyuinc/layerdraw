// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package sourceplanner

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
)

// CompileMode selects the closed compiler root used by a planning base.
type CompileMode string

const (
	CompileProject CompileMode = "project"
	CompilePack    CompileMode = "pack"
)

// CompileInput is the planner-owned, transport-neutral closed compiler input.
type CompileInput struct {
	Mode                 CompileMode
	EntryPath            string
	RootPackID           string
	ProjectSourceTree    map[string][]byte
	InstalledPackTree    map[string][]byte
	ResolvedDependencies ResolvedDependencies
	ReferencedAssets     []AssetInput
	ResourceLimits       ResourceLimits
}

type ResolvedDependencies struct {
	Format        string
	FormatVersion int
	Language      int
	Installs      []ResolvedPack
}

type ResolvedPack struct {
	InstallName  string
	CanonicalID  string
	Version      string
	Digest       string
	Path         string
	Entry        string
	Files        []ResolvedPackFile
	Dependencies []ResolvedPackDependency
	ManifestPath string
	Manifest     []byte
}

type ResolvedPackFile struct{ Path, Digest string }
type ResolvedPackDependency struct{ LocalName, InstallName string }

type AssetInput struct {
	Origin     string
	PackID     string
	Locator    string
	Bytes      []byte
	Digest     string
	MediaType  string
	ByteLength int64
}

type ResourceLimits struct {
	MaxProjectSourceFiles int64
	MaxProjectSourceBytes int64
	MaxPackFiles          int64
	MaxPackBytes          int64
	MaxAssets             int64
	MaxAssetBytes         int64
	MaxRasterDimension    int64
	MaxRasterPixels       int64
	MaxDeclarations       int64
}

// Compiler is the only execution dependency of the pure planner. Engine owns
// the adapter; source planning owns neither handles nor endpoint protocol.
type Compiler interface {
	Compile(context.Context, CompileInput) (CompileResult, error)
}

type CompileResult struct {
	Output         Snapshot
	Diagnostics    []CompileDiagnostic
	DefinitionHash string
}

func (r CompileResult) Snapshot() Snapshot { return r.Output }

type TypedAST struct{ Graph *graph.MasterGraph }

type AuthoringSubjectClassification struct {
	Address    string
	Kind       materialize.SubjectKind
	Capability AuthoringCapability
}

// Snapshot is the immutable compiler projection required by planning.
type Snapshot struct {
	Mode                           CompileMode
	TypedAST                       TypedAST
	NormalizedDocument             *materialize.NormalizedDocument
	NormalizedPackArtifact         *materialize.NormalizedPackArtifact
	CanonicalJSON                  []byte
	SourceMap                      index.SourceMapV1
	StableAddresses                []string
	DefinitionHash                 string
	GraphHash                      *string
	SubjectSemanticHashes          []materialize.SubjectHash
	SubtreeHashes                  []materialize.SubtreeHash
	ChildSetHashes                 []materialize.ChildSetHash
	AuthoringSubjectClassification []AuthoringSubjectClassification
	Diagnostics                    []CompileDiagnostic
}

type CompileDiagnostic struct {
	Code, Severity, MessageKey, Message string
	Range                               *resolve.SourceRange
	SubjectAddress, OwnerAddress        string
	Related                             []resolve.DiagnosticRelated
}

type Digest string
type StableAddress string
type SubjectKind string
type OriginKind string
type PackRootAddress string
type ProjectRootAddress string

const (
	OriginKindProject OriginKind = "project"
	OriginKindPack    OriginKind = "pack"

	SubjectKindProject      SubjectKind = "project"
	SubjectKindEntityType   SubjectKind = "entity_type"
	SubjectKindRelationType SubjectKind = "relation_type"
	SubjectKindLayer        SubjectKind = "layer"
	SubjectKindEntity       SubjectKind = "entity"
	SubjectKindRelation     SubjectKind = "relation"
	SubjectKindEntityRow    SubjectKind = "entity_row"
	SubjectKindRelationRow  SubjectKind = "relation_row"
	SubjectKindQuery        SubjectKind = "query"
	SubjectKindView         SubjectKind = "view"
	SubjectKindReference    SubjectKind = "reference"
)

type SourceOrigin struct {
	Kind        OriginKind       `json:"kind"`
	PackAddress *PackRootAddress `json:"pack_address,omitempty"`
}

type SourceRange struct {
	EndByte    int          `json:"end_byte"`
	ModulePath string       `json:"module_path"`
	Origin     SourceOrigin `json:"origin"`
	StartByte  int          `json:"start_byte"`
}

func (r SourceRange) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		EndByte    string       `json:"end_byte"`
		ModulePath string       `json:"module_path"`
		Origin     SourceOrigin `json:"origin"`
		StartByte  string       `json:"start_byte"`
	}{strconv.Itoa(r.EndByte), r.ModulePath, r.Origin, strconv.Itoa(r.StartByte)})
}

type ModuleRef struct {
	ModulePath string       `json:"module_path"`
	Origin     SourceOrigin `json:"origin"`
}

type SourceSubjectRecord struct {
	Address          StableAddress  `json:"address"`
	Kind             SubjectKind    `json:"kind"`
	OwnerAddress     *StableAddress `json:"owner_address,omitempty"`
	Module           *ModuleRef     `json:"module,omitempty"`
	DeclarationRange *SourceRange   `json:"declaration_range,omitempty"`
	CommentRanges    []SourceRange  `json:"comment_ranges"`
	ManifestRoot     bool           `json:"manifest_root"`
}

type BlobLifetime string

const BlobLifetimeRequest BlobLifetime = "request"

type BlobRef struct {
	BlobID    string       `json:"blob_id"`
	Digest    Digest       `json:"digest"`
	Lifetime  BlobLifetime `json:"lifetime"`
	MediaType string       `json:"media_type"`
	Size      uint64       `json:"size"`
}

func (r BlobRef) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		BlobID    string       `json:"blob_id"`
		Digest    Digest       `json:"digest"`
		Lifetime  BlobLifetime `json:"lifetime"`
		MediaType string       `json:"media_type"`
		Size      string       `json:"size"`
	}{r.BlobID, r.Digest, r.Lifetime, r.MediaType, strconv.FormatUint(r.Size, 10)})
}

type Generation struct {
	Namespace  string `json:"namespace"`
	DocumentID string `json:"document_id"`
	Value      uint64 `json:"value"`
}

func (g Generation) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Document struct {
			Endpoint string `json:"endpoint_instance_id"`
			Value    string `json:"value"`
		} `json:"document_handle"`
		Value string `json:"value"`
	}{Document: struct {
		Endpoint string `json:"endpoint_instance_id"`
		Value    string `json:"value"`
	}{g.Namespace, g.DocumentID}, Value: strconv.FormatUint(g.Value, 10)})
}

type PreviewID struct {
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

func (id PreviewID) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Endpoint string `json:"endpoint_instance_id"`
		Value    string `json:"value"`
	}{id.Namespace, id.Value})
}

type WorkbenchLimits struct{ MaxItems, MaxOutputBytes uint64 }

type ExpectedHash struct {
	Address StableAddress
	Hash    Digest
}

type ExpectedChildSet struct {
	OwnerAddress StableAddress
	ChildKind    SubjectKind
	Hash         Digest
}

type ExpectedSourceDigest struct {
	Module ModuleRef
	Digest Digest
}

type EngineEditPreconditions struct {
	Generation            Generation
	ExpectedChildSets     []ExpectedChildSet
	ExpectedSubjectHashes []ExpectedHash
	ExpectedSubtreeHashes []ExpectedHash
	ExpectedSourceDigests *[]ExpectedSourceDigest
}

type SourcePatchInput struct {
	SourceRange          SourceRange
	ExpectedSourceDigest Digest
	ReplacementBlob      BlobRef
}

type SourcePatchBatch struct{ Patches []SourcePatchInput }

type PlacementHint struct {
	GroupAnchorAddress *StableAddress
	ModulePath         *string
	Position           string
}

type FragmentInput struct {
	Intent            string
	InsertionOwner    StableAddress
	AllowedKinds      []SubjectKind
	FragmentBlob      BlobRef
	ReplacementTarget *StableAddress
	Placement         *PlacementHint
}

type PreviewSourcePatchInput struct {
	Limits        WorkbenchLimits
	Preconditions EngineEditPreconditions
	Patch         SourcePatchBatch
}

type PreviewFragmentInput struct {
	Limits        WorkbenchLimits
	Preconditions EngineEditPreconditions
	Fragment      FragmentInput
}

type FormatScopeInput struct {
	Limits         WorkbenchLimits
	Preconditions  EngineEditPreconditions
	ScopeAddresses []StableAddress
}

type OrganizeWorkspaceInput struct {
	Limits        WorkbenchLimits
	Preconditions EngineEditPreconditions
	Strategy      string
}

type SemanticConflict struct {
	Kind          string         `json:"kind"`
	TargetAddress *StableAddress `json:"target_address,omitempty"`
	OwnerAddress  *StableAddress `json:"owner_address,omitempty"`
	ChildKind     *SubjectKind   `json:"child_kind,omitempty"`
}

type DiagnosticSeverity string

const DiagnosticSeverityError DiagnosticSeverity = "error"

type DiagnosticArgumentValue any
type DiagnosticRelated struct{}

type Diagnostic struct {
	Arguments       map[string]DiagnosticArgumentValue `json:"arguments"`
	Code            string                             `json:"code"`
	Message         *string                            `json:"message,omitempty"`
	MessageKey      string                             `json:"message_key"`
	ProtocolVersion int                                `json:"protocol_version"`
	Range           *SourceRange                       `json:"range,omitempty"`
	Related         []DiagnosticRelated                `json:"related"`
	Severity        DiagnosticSeverity                 `json:"severity"`
}

type SourceEditKind string

const (
	SourceEditKindCreate  SourceEditKind = "create"
	SourceEditKindDelete  SourceEditKind = "delete"
	SourceEditKindMove    SourceEditKind = "move"
	SourceEditKindReplace SourceEditKind = "replace"
)

type SourceEdit struct {
	AfterDigest     *Digest        `json:"after_digest,omitempty"`
	AfterModule     *ModuleRef     `json:"after_module,omitempty"`
	BeforeDigest    *Digest        `json:"before_digest,omitempty"`
	BeforeModule    *ModuleRef     `json:"before_module,omitempty"`
	Kind            SourceEditKind `json:"kind"`
	ReplacementBlob *BlobRef       `json:"replacement_blob,omitempty"`
	SourceRange     *SourceRange   `json:"source_range,omitempty"`
}

type SourceDiff struct {
	Digest Digest       `json:"digest"`
	Edits  []SourceEdit `json:"edits"`
}

type SemanticChangeKind string

const (
	SemanticChangeKindCreated          SemanticChangeKind = "created"
	SemanticChangeKindDeleted          SemanticChangeKind = "deleted"
	SemanticChangeKindUpdated          SemanticChangeKind = "updated"
	SemanticChangeKindRenamed          SemanticChangeKind = "renamed"
	SemanticChangeKindMoved            SemanticChangeKind = "moved"
	SemanticChangeKindReferenceChanged SemanticChangeKind = "reference_changed"
)

type AuthoredFieldPath struct {
	Tokens []string `json:"tokens"`
}

type SemanticDiffEntry struct {
	AfterAddress      *StableAddress      `json:"after_address,omitempty"`
	AfterHash         *Digest             `json:"after_hash,omitempty"`
	BeforeAddress     *StableAddress      `json:"before_address,omitempty"`
	BeforeHash        *Digest             `json:"before_hash,omitempty"`
	ChangedFieldPaths []AuthoredFieldPath `json:"changed_field_paths"`
	Kind              SemanticChangeKind  `json:"kind"`
	OwnerAddress      *StableAddress      `json:"owner_address,omitempty"`
	SubjectKind       SubjectKind         `json:"subject_kind"`
}

type SemanticDiff struct {
	Digest  Digest              `json:"digest"`
	Entries []SemanticDiffEntry `json:"entries"`
}

type AuthoringCapability string

const (
	AuthoringCapabilityAssetWrite       AuthoringCapability = "asset:write"
	AuthoringCapabilityGraphWrite       AuthoringCapability = "graph:write"
	AuthoringCapabilityPackageManage    AuthoringCapability = "package:manage"
	AuthoringCapabilityProjectConfigure AuthoringCapability = "project:configure"
	AuthoringCapabilityQueryWrite       AuthoringCapability = "query:write"
	AuthoringCapabilityReferenceWrite   AuthoringCapability = "reference:write"
	AuthoringCapabilitySchemaWrite      AuthoringCapability = "schema:write"
	AuthoringCapabilitySourceMaintain   AuthoringCapability = "source:maintain"
	AuthoringCapabilityViewWrite        AuthoringCapability = "view:write"
)

type AuthoringAction string

const (
	AuthoringActionCreate   AuthoringAction = "create"
	AuthoringActionDelete   AuthoringAction = "delete"
	AuthoringActionRename   AuthoringAction = "rename"
	AuthoringActionMove     AuthoringAction = "move"
	AuthoringActionBind     AuthoringAction = "bind"
	AuthoringActionUpdate   AuthoringAction = "update"
	AuthoringActionMaintain AuthoringAction = "maintain"
)

type EntityTypeAddress string
type RelationTypeAddress string
type LayerAddress string
type ColumnAddress string
type EntityAddress string

type GraphAuthoringFacts struct {
	ActionFlags             []string              `json:"action_flags"`
	ColumnAddresses         []ColumnAddress       `json:"column_addresses"`
	EndpointEntityAddresses []EntityAddress       `json:"endpoint_entity_addresses"`
	EntityTypeAddresses     []EntityTypeAddress   `json:"entity_type_addresses"`
	LayerAddresses          []LayerAddress        `json:"layer_addresses"`
	RelationTypeAddresses   []RelationTypeAddress `json:"relation_type_addresses"`
}

type AuthoringImpactEntry struct {
	Action            AuthoringAction      `json:"action"`
	AfterRefs         []StableAddress      `json:"after_refs"`
	BeforeRefs        []StableAddress      `json:"before_refs"`
	Capability        AuthoringCapability  `json:"capability"`
	ChangedFieldPaths []AuthoredFieldPath  `json:"changed_field_paths"`
	GraphFacts        *GraphAuthoringFacts `json:"graph_facts,omitempty"`
	OwnerAddress      *StableAddress       `json:"owner_address,omitempty"`
	SourceRefs        []SourceRange        `json:"source_refs"`
	SubjectAddress    *StableAddress       `json:"subject_address,omitempty"`
	SubjectKind       SubjectKind          `json:"subject_kind"`
}

type AuthoringImpact struct {
	BaseDefinitionHash      Digest                 `json:"base_definition_hash"`
	Entries                 []AuthoringImpactEntry `json:"entries"`
	ImpactDigest            Digest                 `json:"impact_digest"`
	RequiredCapabilities    []AuthoringCapability  `json:"required_capabilities"`
	ResultingDefinitionHash Digest                 `json:"resulting_definition_hash"`
	SemanticDiffHash        Digest                 `json:"semantic_diff_hash"`
	SourceDiffHash          Digest                 `json:"source_diff_hash"`
}

type SubjectHash struct {
	Address StableAddress `json:"address"`
	Hash    Digest        `json:"hash"`
	Kind    SubjectKind   `json:"kind"`
}
type SubtreeHash struct {
	Hash         Digest        `json:"hash"`
	OwnerAddress StableAddress `json:"owner_address"`
}
type ChildSetHash struct {
	ChildAddresses []StableAddress `json:"child_addresses"`
	ChildKind      SubjectKind     `json:"child_kind"`
	Hash           Digest          `json:"hash"`
	OwnerAddress   StableAddress   `json:"owner_address"`
}

type ResultingHashes struct {
	ChildSetHashes []ChildSetHash      `json:"child_set_hashes"`
	DefinitionHash Digest              `json:"definition_hash"`
	GraphHash      *Digest             `json:"graph_hash,omitempty"`
	Mode           CompileMode         `json:"mode"`
	PackAddress    *PackRootAddress    `json:"pack_address,omitempty"`
	ProjectAddress *ProjectRootAddress `json:"project_address,omitempty"`
	SubjectHashes  []SubjectHash       `json:"subject_hashes"`
	SubtreeHashes  []SubtreeHash       `json:"subtree_hashes"`
}

type WorkbenchPreviewResult struct {
	AuthoringImpact               *AuthoringImpact       `json:"authoring_impact,omitempty"`
	AuthoringImpactDigest         *Digest                `json:"authoring_impact_digest,omitempty"`
	BaseGeneration                Generation             `json:"base_generation"`
	ChangedSourceFiles            []ModuleRef            `json:"changed_source_files"`
	Conflicts                     []SemanticConflict     `json:"conflicts"`
	Diagnostics                   []Diagnostic           `json:"diagnostics"`
	PreviewDigest                 *Digest                `json:"preview_digest,omitempty"`
	PreviewID                     *PreviewID             `json:"preview_id,omitempty"`
	ProposedGeneration            *Generation            `json:"proposed_generation,omitempty"`
	RequiredAuthoringCapabilities *[]AuthoringCapability `json:"required_authoring_capabilities,omitempty"`
	ResultingHashes               *ResultingHashes       `json:"resulting_hashes,omitempty"`
	SemanticDiff                  SemanticDiff           `json:"semantic_diff"`
	SourceDiff                    SourceDiff             `json:"source_diff"`
	Status                        string                 `json:"status"`
}
