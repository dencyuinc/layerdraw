// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package index builds generated, transport-neutral source, semantic, and
// search indexes from one successful materialization generation.
package index

import (
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

const (
	SourceMapSchemaVersion      = 1
	SemanticIndexSchemaVersion  = 1
	SearchDocumentSchemaVersion = 1
)

// Input accepts only the successful materialization boundary. Its internally
// retained validated stage snapshot prevents mixed closed inputs.
type Input struct {
	Materialized materialize.Result
}

type Result struct {
	state       *resultState
	Diagnostics []resolve.Diagnostic
	HasErrors   bool
}

type resultState struct{ snapshot Snapshot }

// Snapshot contains internal Go schemas only. It is not a public wire format
// and this package does not emit layerdraw.index.json.
type Snapshot struct {
	SourceMap       SourceMapV1
	SemanticIndex   SemanticIndexV1
	SearchDocuments []SearchDocument
}

func (r Result) Snapshot() Snapshot {
	if r.state == nil {
		return Snapshot{}
	}
	return deepClone(r.state.snapshot)
}

type SourceMapV1 struct {
	SchemaVersion int                   `json:"schema_version"`
	Files         []SourceFileRecord    `json:"files"`
	Subjects      []SourceSubjectRecord `json:"subjects"`
	Bindings      []SourceBindingRecord `json:"bindings"`
	Exports       []ExportBindingRecord `json:"exports"`
	Assets        []SourceAssetRecord   `json:"assets"`
}

type SourceFileRecord struct {
	Origin     resolve.SourceOrigin `json:"origin"`
	ModulePath string               `json:"module_path"`
	Digest     string               `json:"digest"`
	ByteLength int                  `json:"byte_length"`
}

type ModuleRef struct {
	Origin     resolve.SourceOrigin `json:"origin"`
	ModulePath string               `json:"module_path"`
}

type SourceSubjectRecord struct {
	Address          string                  `json:"address"`
	Kind             materialize.SubjectKind `json:"kind"`
	OwnerAddress     *string                 `json:"owner_address,omitempty"`
	Module           *ModuleRef              `json:"module,omitempty"`
	DeclarationRange *resolve.SourceRange    `json:"declaration_range,omitempty"`
	CommentRanges    []resolve.SourceRange   `json:"comment_ranges"`
	ManifestRoot     bool                    `json:"manifest_root"`
}

type SourceBindingRecord struct {
	SourceAddress      string                  `json:"source_address"`
	TargetAddress      string                  `json:"target_address"`
	TargetKind         materialize.SubjectKind `json:"target_kind"`
	TargetOwnerAddress string                  `json:"target_owner_address,omitempty"`
	Via                string                  `json:"via"`
	Module             ModuleRef               `json:"module"`
	Range              resolve.SourceRange     `json:"range"`
}

type ExportBindingRecord struct {
	PublicName    string              `json:"public_name"`
	TargetAddress string              `json:"target_address"`
	Module        ModuleRef           `json:"module"`
	Range         resolve.SourceRange `json:"range"`
	ReExport      bool                `json:"re_export"`
}

type SourceAssetRecord struct {
	SubjectAddress string               `json:"subject_address"`
	AuthoredPath   string               `json:"authored_path"`
	Locator        string               `json:"locator"`
	Origin         resolve.SourceOrigin `json:"origin"`
	ModulePath     string               `json:"module_path"`
	Range          resolve.SourceRange  `json:"range"`
	Digest         string               `json:"digest"`
	MediaType      string               `json:"media_type"`
	ByteLength     int64                `json:"byte_length"`
}

type SemanticIndexV1 struct {
	SchemaVersion   int                 `json:"schema_version"`
	Subjects        []SemanticSubject   `json:"subjects"`
	References      []SemanticReference `json:"references"`
	Children        []OwnerMembers      `json:"children"`
	Rows            []OwnerMembers      `json:"rows"`
	Columns         []OwnerMembers      `json:"columns"`
	TypeMembership  []OwnerMembers      `json:"type_membership"`
	LayerMembership []OwnerMembers      `json:"layer_membership"`
	ReferenceIDs    []ReferenceIDRecord `json:"reference_ids"`
	Adjacency       []AdjacencyRecord   `json:"adjacency"`
	Dependencies    []DependencyRecord  `json:"dependencies"`
	ScopedReads     ScopedReadIndexes   `json:"scoped_reads"`
}

type SemanticSubject struct {
	Address      string                  `json:"address"`
	Kind         materialize.SubjectKind `json:"kind"`
	OwnerAddress *string                 `json:"owner_address,omitempty"`
	Module       *ModuleRef              `json:"module,omitempty"`
	OwnHash      string                  `json:"own_hash"`
	SubtreeHash  *string                 `json:"subtree_hash,omitempty"`
}

type SemanticReference struct {
	SourceAddress string                  `json:"source_address"`
	TargetAddress string                  `json:"target_address"`
	TargetKind    materialize.SubjectKind `json:"target_kind"`
	Via           string                  `json:"via"`
	Range         resolve.SourceRange     `json:"range"`
}

type OwnerMembers struct {
	OwnerAddress string   `json:"owner_address"`
	Addresses    []string `json:"addresses"`
}

type ReferenceIDRecord struct {
	ID        string   `json:"id"`
	Addresses []string `json:"addresses"`
}

type AdjacencyRecord struct {
	EntityAddress string   `json:"entity_address"`
	Outgoing      []string `json:"outgoing"`
	Incoming      []string `json:"incoming"`
}

type DependencyKind string

const (
	DependencyQuery DependencyKind = "query"
	DependencyView  DependencyKind = "view"
)

type DependencyRecord struct {
	Kind                  DependencyKind              `json:"kind"`
	SubjectAddress        string                      `json:"subject_address"`
	QueryAddresses        []string                    `json:"query_addresses"`
	ParameterAddresses    []string                    `json:"parameter_addresses"`
	LayerAddresses        []string                    `json:"layer_addresses"`
	EntityTypeAddresses   []string                    `json:"entity_type_addresses"`
	RelationTypeAddresses []string                    `json:"relation_type_addresses"`
	EntityAddresses       []string                    `json:"entity_addresses"`
	RelationAddresses     []string                    `json:"relation_addresses"`
	ColumnAddresses       []string                    `json:"column_addresses"`
	ExportAddresses       []string                    `json:"export_addresses"`
	StateReads            []query.StateReadDependency `json:"state_reads"`
}

type ScopedReadIndexes struct {
	ByModule            []ScopeAddresses    `json:"by_module"`
	ByKind              []KindAddresses     `json:"by_kind"`
	ChildrenByOwner     []OwnerMembers      `json:"children_by_owner"`
	RowsByOwner         []OwnerMembers      `json:"rows_by_owner"`
	ColumnsByOwner      []OwnerMembers      `json:"columns_by_owner"`
	MembersByType       []OwnerMembers      `json:"members_by_type"`
	MembersByLayer      []OwnerMembers      `json:"members_by_layer"`
	ReferencesByID      []ReferenceIDRecord `json:"references_by_id"`
	OutgoingByEntity    []OwnerMembers      `json:"outgoing_by_entity"`
	IncomingByEntity    []OwnerMembers      `json:"incoming_by_entity"`
	UsagesByTarget      []OwnerMembers      `json:"usages_by_target"`
	QueriesByDependency []OwnerMembers      `json:"queries_by_dependency"`
	ViewsByDependency   []OwnerMembers      `json:"views_by_dependency"`
}

type ScopeAddresses struct {
	Module    ModuleRef `json:"module"`
	Addresses []string  `json:"addresses"`
}

type KindAddresses struct {
	Kind      materialize.SubjectKind `json:"kind"`
	Addresses []string                `json:"addresses"`
}

type SearchDocument struct {
	SchemaVersion       int                     `json:"schema_version"`
	SubjectAddress      string                  `json:"subject_address"`
	SubjectKind         materialize.SubjectKind `json:"subject_kind"`
	OwnerAddress        *string                 `json:"owner_address,omitempty"`
	GraphEntryAddresses []string                `json:"graph_entry_addresses"`
	TypeAddresses       []string                `json:"type_addresses"`
	LayerAddresses      []string                `json:"layer_addresses"`
	Fields              []SearchField           `json:"fields"`
	ContentHash         string                  `json:"content_hash"`
	defaultSource       *resolve.SourceRange
}

type SearchField struct {
	FieldPath          string               `json:"field_path"`
	SourceRef          *resolve.SourceRange `json:"source_ref,omitempty"`
	Text               string               `json:"text"`
	LexicalWeight      int                  `json:"lexical_weight"`
	IncludeInEmbedding bool                 `json:"include_in_embedding"`
}
