// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package materialize

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

// These structs are the complete byte-level projections used by Language 1
// hashing. They intentionally do not embed or serialize compiler-stage types.
type projectDefinitionHashPayload struct {
	Project           Project            `json:"project"`
	DependencyOrigins []dependencyOrigin `json:"dependency_origins"`
	EntityTypes       []EntityType       `json:"entity_types"`
	RelationTypes     []RelationType     `json:"relation_types"`
	Layers            []Layer            `json:"layers"`
	Entities          []Entity           `json:"entities"`
	Relations         []Relation         `json:"relations"`
	Queries           []Query            `json:"queries"`
	Views             []View             `json:"views"`
	References        []Reference        `json:"references"`
	Assets            []AssetBlobSummary `json:"assets"`
	Identity          IdentityHistory    `json:"identity"`
}

type packDefinitionHashPayload struct {
	Pack              PackRoot           `json:"pack"`
	DependencyOrigins []dependencyOrigin `json:"dependency_origins"`
	EntityTypes       []EntityType       `json:"entity_types"`
	RelationTypes     []RelationType     `json:"relation_types"`
	Queries           []Query            `json:"queries"`
	Views             []View             `json:"views"`
	References        []Reference        `json:"references"`
	Assets            []AssetBlobSummary `json:"assets"`
	Identity          IdentityHistory    `json:"identity"`
}

type dependencyOrigin struct {
	Address     string `json:"address"`
	CanonicalID string `json:"canonical_id"`
}

type graphHashPayload struct {
	Project       graphProject        `json:"project"`
	EntityTypes   []graphEntityType   `json:"entity_types"`
	RelationTypes []graphRelationType `json:"relation_types"`
	Layers        []Layer             `json:"layers"`
	Entities      []graphEntity       `json:"entities"`
	Relations     []graphRelation     `json:"relations"`
	Assets        []AssetBlobSummary  `json:"assets"`
}

type graphProject struct {
	Common
	ID          string `json:"id"`
	Address     string `json:"address"`
	DisplayName string `json:"display_name"`
}

type graphEntityType struct {
	Common
	ID                string             `json:"id"`
	Address           string             `json:"address"`
	DisplayName       string             `json:"display_name"`
	Icon              *string            `json:"icon,omitempty"`
	Image             *AssetRef          `json:"image,omitempty"`
	Color             *string            `json:"color,omitempty"`
	Representation    Representation     `json:"representation"`
	Columns           []graphColumn      `json:"columns"`
	UniqueConstraints []UniqueConstraint `json:"unique_constraints"`
}

type graphRelationType struct {
	Common
	ID                string                          `json:"id"`
	Address           string                          `json:"address"`
	DisplayName       string                          `json:"display_name"`
	SemanticKind      definition.RelationSemanticKind `json:"semantic_kind"`
	AllowSelf         bool                            `json:"allow_self"`
	DuplicatePolicy   definition.DuplicatePolicy      `json:"duplicate_policy"`
	From              EndpointRule                    `json:"from"`
	To                EndpointRule                    `json:"to"`
	Cardinality       Cardinality                     `json:"cardinality"`
	ForwardLabel      string                          `json:"forward_label"`
	ReverseLabel      *string                         `json:"reverse_label,omitempty"`
	Columns           []graphColumn                   `json:"columns"`
	UniqueConstraints []UniqueConstraint              `json:"unique_constraints"`
	Traversal         TraversalPolicy                 `json:"traversal"`
	Projections       ProjectionSet                   `json:"projections"`
	Render            RenderSet                       `json:"render"`
	Export            RelationExport                  `json:"export"`
}

type graphColumn struct {
	ID          string                   `json:"id"`
	Address     string                   `json:"address"`
	DisplayName string                   `json:"display_name"`
	ValueType   definition.ScalarType    `json:"value_type"`
	EnumValues  []string                 `json:"enum_values,omitempty"`
	Required    bool                     `json:"required"`
	Default     *Scalar                  `json:"default,omitempty"`
	Format      *definition.StringFormat `json:"format,omitempty"`
	Min         *float64                 `json:"min,omitempty"`
	Max         *float64                 `json:"max,omitempty"`
	MinLength   *int64                   `json:"min_length,omitempty"`
	MaxLength   *int64                   `json:"max_length,omitempty"`
}

type graphEntity struct {
	Common
	ID           string         `json:"id"`
	Address      string         `json:"address"`
	DisplayName  string         `json:"display_name"`
	TypeAddress  string         `json:"type_address"`
	LayerAddress string         `json:"layer_address"`
	Rows         []AttributeRow `json:"rows"`
}

type graphRelation struct {
	Common
	ID          string         `json:"id"`
	Address     string         `json:"address"`
	DisplayName *string        `json:"display_name,omitempty"`
	TypeAddress string         `json:"type_address"`
	FromAddress string         `json:"from_address"`
	ToAddress   string         `json:"to_address"`
	Rows        []AttributeRow `json:"rows"`
}

type ownSubjectPayload struct {
	Kind    SubjectKind   `json:"kind"`
	Address string        `json:"address"`
	Fields  subjectFields `json:"fields"`
}

// subjectFields is sealed to the dedicated canonical payload structs below;
// compiler-stage structs cannot be passed to own-subject hashing.
type subjectFields interface{ isSubjectFields() }

type subjectFieldSeal struct{}

func (subjectFieldSeal) isSubjectFields() {}

type rootProjectFields struct {
	subjectFieldSeal `json:"-"`
	Common
	ID           string                   `json:"id"`
	DisplayName  string                   `json:"display_name"`
	Reservations map[SubjectKind][]string `json:"reservations"`
	Moves        []Move                   `json:"moves"`
	MoveClosure  []MoveResolution         `json:"move_closure"`
}

type rootPackFields struct {
	subjectFieldSeal `json:"-"`
	CanonicalID      string                   `json:"canonical_id"`
	Reservations     map[SubjectKind][]string `json:"reservations"`
	Moves            []Move                   `json:"moves"`
	MoveClosure      []MoveResolution         `json:"move_closure"`
}

type layerFields struct {
	subjectFieldSeal `json:"-"`
	Common
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Order       int64  `json:"order"`
}

type entityTypeFields struct {
	subjectFieldSeal `json:"-"`
	Common
	ID                    string         `json:"id"`
	DisplayName           string         `json:"display_name"`
	Icon                  *string        `json:"icon,omitempty"`
	Image                 *AssetRef      `json:"image,omitempty"`
	Color                 *string        `json:"color,omitempty"`
	Representation        Representation `json:"representation"`
	ColumnOrder           []string       `json:"column_order"`
	ReservedColumnIDs     []string       `json:"reserved_column_ids"`
	ReservedConstraintIDs []string       `json:"reserved_constraint_ids"`
}

type relationTypeFields struct {
	subjectFieldSeal `json:"-"`
	Common
	ID                    string                          `json:"id"`
	DisplayName           string                          `json:"display_name"`
	SemanticKind          definition.RelationSemanticKind `json:"semantic_kind"`
	AllowSelf             bool                            `json:"allow_self"`
	DuplicatePolicy       definition.DuplicatePolicy      `json:"duplicate_policy"`
	From                  EndpointRule                    `json:"from"`
	To                    EndpointRule                    `json:"to"`
	Cardinality           Cardinality                     `json:"cardinality"`
	ForwardLabel          string                          `json:"forward_label"`
	ReverseLabel          *string                         `json:"reverse_label,omitempty"`
	Traversal             TraversalPolicy                 `json:"traversal"`
	Projections           ProjectionSet                   `json:"projections"`
	Render                RenderSet                       `json:"render"`
	Export                RelationExport                  `json:"export"`
	ColumnOrder           []string                        `json:"column_order"`
	ReservedColumnIDs     []string                        `json:"reserved_column_ids"`
	ReservedConstraintIDs []string                        `json:"reserved_constraint_ids"`
}

type columnFields struct {
	subjectFieldSeal   `json:"-"`
	ID                 string                   `json:"id"`
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

type constraintFields struct {
	subjectFieldSeal `json:"-"`
	ID               string   `json:"id"`
	ColumnAddresses  []string `json:"column_addresses"`
}

type entityFields struct {
	subjectFieldSeal `json:"-"`
	Common
	ID             string   `json:"id"`
	DisplayName    string   `json:"display_name"`
	TypeAddress    string   `json:"type_address"`
	LayerAddress   string   `json:"layer_address"`
	ReservedRowIDs []string `json:"reserved_row_ids"`
}

type relationFields struct {
	subjectFieldSeal `json:"-"`
	Common
	ID             string   `json:"id"`
	DisplayName    *string  `json:"display_name,omitempty"`
	TypeAddress    string   `json:"type_address"`
	FromAddress    string   `json:"from_address"`
	ToAddress      string   `json:"to_address"`
	ReservedRowIDs []string `json:"reserved_row_ids"`
}

type rowFields struct {
	subjectFieldSeal `json:"-"`
	ID               string            `json:"id"`
	Values           map[string]Scalar `json:"values"`
}

type queryFields struct {
	subjectFieldSeal `json:"-"`
	Common
	ID                   string               `json:"id"`
	DisplayName          string               `json:"display_name"`
	StateInput           query.StatePolicy    `json:"state_input"`
	Select               QuerySelect          `json:"select"`
	Where                Predicate            `json:"where"`
	RelationWhere        Predicate            `json:"relation_where"`
	Traverse             *QueryTraversal      `json:"traverse,omitempty"`
	Result               []query.ResultMember `json:"result"`
	ReservedParameterIDs []string             `json:"reserved_parameter_ids"`
}

type parameterFields struct {
	subjectFieldSeal   `json:"-"`
	ID                 string                   `json:"id"`
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

type viewFields struct {
	subjectFieldSeal `json:"-"`
	Common
	ID                     string                        `json:"id"`
	DisplayName            string                        `json:"display_name"`
	StateInput             query.StatePolicy             `json:"state_input"`
	Category               view.Category                 `json:"category"`
	Intent                 *string                       `json:"intent,omitempty"`
	Source                 ViewSource                    `json:"source"`
	RelationProjections    map[string]ProjectionOverride `json:"relation_projection_overrides"`
	Shape                  ViewShape                     `json:"shape"`
	TableColumnOrder       []string                      `json:"table_column_order"`
	ReservedTableColumnIDs []string                      `json:"reserved_table_column_ids"`
	ReservedExportIDs      []string                      `json:"reserved_export_ids"`
}

type tableColumnFields struct {
	subjectFieldSeal `json:"-"`
	ID               string            `json:"id"`
	Label            *string           `json:"label,omitempty"`
	Source           TableColumnSource `json:"source"`
	Aggregate        view.Aggregate    `json:"aggregate"`
}

type exportFields struct {
	subjectFieldSeal `json:"-"`
	ID               string             `json:"id"`
	Format           string             `json:"format"`
	Filename         string             `json:"filename"`
	Fidelity         string             `json:"fidelity"`
	SourceRefs       bool               `json:"source_refs"`
	ExporterProfile  ExporterProfileRef `json:"exporter_profile"`
	Options          ExportOptions      `json:"options"`
}

type referenceFields struct {
	subjectFieldSeal `json:"-"`
	ID               string `json:"id"`
	Text             string `json:"text"`
}

type subtreePayload struct {
	OwnerAddress string      `json:"owner_address"`
	OwnerHash    string      `json:"owner_hash"`
	Children     []childHash `json:"children"`
}
type childHash struct {
	Address string `json:"address"`
	Hash    string `json:"hash"`
}
type childSetPayload struct {
	OwnerAddress   string      `json:"owner_address"`
	ChildKind      SubjectKind `json:"child_kind"`
	ChildAddresses []string    `json:"child_addresses"`
}

type hashNode struct {
	address  string
	kind     SubjectKind
	owner    string
	fields   subjectFields
	children []string
	ownHash  string
	subtree  string
}

func computeHashes(input Input, document *NormalizedDocument, pack *NormalizedPackArtifact) (Hashes, error) {
	out := Hashes{generation: input.Resolve.Generation(), OwnSubjects: []SubjectHash{}, Subtrees: []SubtreeHash{}, ChildSets: []ChildSetHash{}}
	definitionPayload, graphPayload, nodes, err := buildHashPayloads(input, document, pack)
	if err != nil {
		return Hashes{}, err
	}
	out.Definition, err = SemanticHash(DomainDefinition, definitionPayload)
	if err != nil {
		return Hashes{}, err
	}
	if graphPayload != nil {
		value, hashErr := SemanticHash(DomainGraph, *graphPayload)
		if hashErr != nil {
			return Hashes{}, hashErr
		}
		out.Graph = &value
	}
	for _, node := range nodes {
		node.ownHash, err = SemanticHash(DomainSubject, ownSubjectPayload{Kind: node.kind, Address: node.address, Fields: node.fields})
		if err != nil {
			return Hashes{}, err
		}
		out.OwnSubjects = append(out.OwnSubjects, SubjectHash{Address: node.address, Kind: node.kind, Hash: node.ownHash})
	}
	byAddress := map[string]*hashNode{}
	for _, node := range nodes {
		byAddress[node.address] = node
	}
	for _, node := range nodes {
		if len(node.children) != 0 || ownerKinds[node.kind] {
			if _, err = subtreeHash(node, byAddress); err != nil {
				return Hashes{}, err
			}
		}
	}
	for _, node := range nodes {
		if node.subtree != "" {
			out.Subtrees = append(out.Subtrees, SubtreeHash{OwnerAddress: node.address, Hash: node.subtree})
		}
	}
	for _, node := range nodes {
		for _, kind := range childKinds(node.kind) {
			addresses := []string{}
			for _, childAddress := range node.children {
				if byAddress[childAddress].kind == kind {
					addresses = append(addresses, childAddress)
				}
			}
			sortAddresses(input.Resolve, addresses)
			payload := childSetPayload{OwnerAddress: node.address, ChildKind: kind, ChildAddresses: addresses}
			hash, hashErr := SemanticHash(DomainChildSet, payload)
			if hashErr != nil {
				return Hashes{}, hashErr
			}
			out.ChildSets = append(out.ChildSets, ChildSetHash{OwnerAddress: node.address, ChildKind: kind, Addresses: addresses, Hash: hash})
		}
	}
	sort.Slice(out.OwnSubjects, func(i, j int) bool {
		return lessAddress(input.Resolve, out.OwnSubjects[i].Address, out.OwnSubjects[j].Address)
	})
	sort.Slice(out.Subtrees, func(i, j int) bool {
		return lessAddress(input.Resolve, out.Subtrees[i].OwnerAddress, out.Subtrees[j].OwnerAddress)
	})
	sort.Slice(out.ChildSets, func(i, j int) bool {
		if out.ChildSets[i].OwnerAddress != out.ChildSets[j].OwnerAddress {
			return lessAddress(input.Resolve, out.ChildSets[i].OwnerAddress, out.ChildSets[j].OwnerAddress)
		}
		return kindRank(out.ChildSets[i].ChildKind) < kindRank(out.ChildSets[j].ChildKind)
	})
	return out, nil
}

func buildHashPayloads(input Input, document *NormalizedDocument, pack *NormalizedPackArtifact) (any, *graphHashPayload, []*hashNode, error) {
	if (document == nil) == (pack == nil) {
		return nil, nil, nil, fmt.Errorf("hashing requires exactly one normalized envelope")
	}
	dependencies := []ResolvedPackSummary{}
	if document != nil {
		dependencies = document.Dependencies
	} else {
		dependencies = pack.Dependencies
	}
	origins := make([]dependencyOrigin, 0, len(dependencies))
	for _, dependency := range dependencies {
		origins = append(origins, dependencyOrigin{Address: dependency.Address, CanonicalID: dependency.CanonicalID})
	}
	sort.Slice(origins, func(i, j int) bool { return lessAddress(input.Resolve, origins[i].Address, origins[j].Address) })
	var graphPayload *graphHashPayload
	var definitionPayload any
	if document != nil {
		definitionPayload = projectDefinitionHashPayload{Project: document.Project, DependencyOrigins: origins, EntityTypes: document.EntityTypes, RelationTypes: document.RelationTypes, Layers: document.Layers, Entities: document.Entities, Relations: document.Relations, Queries: document.Queries, Views: document.Views, References: document.References, Assets: document.Assets, Identity: document.Identity}
		graph := makeGraphPayload(*document)
		graphPayload = &graph
	} else {
		definitionPayload = packDefinitionHashPayload{Pack: pack.Pack, DependencyOrigins: origins, EntityTypes: pack.EntityTypes, RelationTypes: pack.RelationTypes, Queries: pack.Queries, Views: pack.Views, References: pack.References, Assets: pack.Assets, Identity: pack.Identity}
	}
	nodes := buildHashNodes(input, document, pack)
	return definitionPayload, graphPayload, nodes, nil
}

func makeGraphPayload(value NormalizedDocument) graphHashPayload {
	out := graphHashPayload{Project: graphProject{Common: value.Project.Common, ID: value.Project.ID, Address: value.Project.Address, DisplayName: value.Project.DisplayName}, Layers: value.Layers, Assets: value.Assets}
	for _, item := range value.EntityTypes {
		out.EntityTypes = append(out.EntityTypes, graphEntityType{Common: item.Common, ID: item.ID, Address: item.Address, DisplayName: item.DisplayName, Icon: item.Icon, Image: item.Image, Color: item.Color, Representation: item.Representation, Columns: graphColumns(item.Columns), UniqueConstraints: item.UniqueConstraints})
	}
	for _, item := range value.RelationTypes {
		out.RelationTypes = append(out.RelationTypes, graphRelationType{Common: item.Common, ID: item.ID, Address: item.Address, DisplayName: item.DisplayName, SemanticKind: item.SemanticKind, AllowSelf: item.AllowSelf, DuplicatePolicy: item.DuplicatePolicy, From: item.From, To: item.To, Cardinality: item.Cardinality, ForwardLabel: item.ForwardLabel, ReverseLabel: item.ReverseLabel, Columns: graphColumns(item.Columns), UniqueConstraints: item.UniqueConstraints, Traversal: item.Traversal, Projections: item.Projections, Render: item.Render, Export: item.Export})
	}
	for _, item := range value.Entities {
		out.Entities = append(out.Entities, graphEntity{Common: item.Common, ID: item.ID, Address: item.Address, DisplayName: item.DisplayName, TypeAddress: item.TypeAddress, LayerAddress: item.LayerAddress, Rows: item.Rows})
	}
	for _, item := range value.Relations {
		out.Relations = append(out.Relations, graphRelation{Common: item.Common, ID: item.ID, Address: item.Address, DisplayName: item.DisplayName, TypeAddress: item.TypeAddress, FromAddress: item.FromAddress, ToAddress: item.ToAddress, Rows: item.Rows})
	}
	return out
}

func graphColumns(values []Column) []graphColumn {
	out := make([]graphColumn, len(values))
	for i, item := range values {
		out[i] = graphColumn{ID: item.ID, Address: item.Address, DisplayName: item.DisplayName, ValueType: item.ValueType, EnumValues: item.EnumValues, Required: item.Required, Default: item.Default, Format: item.Format, Min: item.Min, Max: item.Max, MinLength: item.MinLength, MaxLength: item.MaxLength}
	}
	return out
}

func buildHashNodes(input Input, document *NormalizedDocument, pack *NormalizedPackArtifact) []*hashNode {
	nodes := []*hashNode{}
	byAddress := map[string]*hashNode{}
	add := func(node *hashNode) { nodes = append(nodes, node); byAddress[node.address] = node }
	identity := IdentityHistory{}
	if document != nil {
		identity = document.Identity
		add(&hashNode{address: document.Project.Address, kind: SubjectProject, fields: rootProjectFields{Common: document.Project.Common, ID: document.Project.ID, DisplayName: document.Project.DisplayName, Reservations: identity.RootReservations[document.Project.Address], Moves: rootMoves(identity.Moves, document.Project.Address), MoveClosure: rootMoveClosure(identity.MoveClosure, document.Project.Address)}})
		for _, dependency := range document.Dependencies {
			add(&hashNode{address: dependency.Address, kind: SubjectPack, fields: rootPackFields{CanonicalID: dependency.CanonicalID, Reservations: identity.RootReservations[dependency.Address], Moves: rootMoves(identity.Moves, dependency.Address), MoveClosure: rootMoveClosure(identity.MoveClosure, dependency.Address)}})
		}
		addDocumentNodes(add, *document)
	} else {
		identity = pack.Identity
		add(&hashNode{address: pack.Pack.Address, kind: SubjectPack, fields: rootPackFields{CanonicalID: pack.Pack.CanonicalID, Reservations: identity.RootReservations[pack.Pack.Address], Moves: rootMoves(identity.Moves, pack.Pack.Address), MoveClosure: rootMoveClosure(identity.MoveClosure, pack.Pack.Address)}})
		for _, dependency := range pack.Dependencies {
			add(&hashNode{address: dependency.Address, kind: SubjectPack, fields: rootPackFields{CanonicalID: dependency.CanonicalID, Reservations: identity.RootReservations[dependency.Address], Moves: rootMoves(identity.Moves, dependency.Address), MoveClosure: rootMoveClosure(identity.MoveClosure, dependency.Address)}})
		}
		addPackNodes(add, *pack)
	}
	ownerByAddress := ownerAddresses(input.Resolve, byAddress)
	for _, node := range nodes {
		if node.kind != SubjectProject && node.kind != SubjectPack {
			node.owner = ownerByAddress[node.address]
			if owner := byAddress[node.owner]; owner != nil {
				owner.children = append(owner.children, node.address)
			}
		}
	}
	for _, node := range nodes {
		sortAddresses(input.Resolve, node.children)
	}
	return nodes
}

func addDocumentNodes(add func(*hashNode), value NormalizedDocument) {
	addSchemaNodes(add, value.EntityTypes, value.RelationTypes)
	for _, item := range value.Layers {
		add(&hashNode{address: item.Address, kind: SubjectLayer, fields: layerFields{Common: item.Common, ID: item.ID, DisplayName: item.DisplayName, Order: item.Order}})
	}
	for _, item := range value.Entities {
		add(&hashNode{address: item.Address, kind: SubjectEntity, fields: entityFields{Common: item.Common, ID: item.ID, DisplayName: item.DisplayName, TypeAddress: item.TypeAddress, LayerAddress: item.LayerAddress, ReservedRowIDs: item.ReservedRowIDs}})
		for _, row := range item.Rows {
			add(&hashNode{address: row.Address, kind: SubjectEntityRow, owner: item.Address, fields: rowFields{ID: row.ID, Values: row.Values}})
		}
	}
	for _, item := range value.Relations {
		add(&hashNode{address: item.Address, kind: SubjectRelation, fields: relationFields{Common: item.Common, ID: item.ID, DisplayName: item.DisplayName, TypeAddress: item.TypeAddress, FromAddress: item.FromAddress, ToAddress: item.ToAddress, ReservedRowIDs: item.ReservedRowIDs}})
		for _, row := range item.Rows {
			add(&hashNode{address: row.Address, kind: SubjectRelationRow, owner: item.Address, fields: rowFields{ID: row.ID, Values: row.Values}})
		}
	}
	addRecipeNodes(add, value.Queries, value.Views, value.References)
}

func addPackNodes(add func(*hashNode), value NormalizedPackArtifact) {
	addSchemaNodes(add, value.EntityTypes, value.RelationTypes)
	addRecipeNodes(add, value.Queries, value.Views, value.References)
}

func addSchemaNodes(add func(*hashNode), entityTypes []EntityType, relationTypes []RelationType) {
	for _, item := range entityTypes {
		order := columnAddresses(item.Columns)
		add(&hashNode{address: item.Address, kind: SubjectEntityType, fields: entityTypeFields{Common: item.Common, ID: item.ID, DisplayName: item.DisplayName, Icon: item.Icon, Image: item.Image, Color: item.Color, Representation: item.Representation, ColumnOrder: order, ReservedColumnIDs: item.ReservedColumnIDs, ReservedConstraintIDs: item.ReservedConstraintIDs}})
		for _, child := range item.Columns {
			add(&hashNode{address: child.Address, kind: SubjectEntityTypeColumn, owner: item.Address, fields: columnOwnFields(child)})
		}
		for _, child := range item.UniqueConstraints {
			add(&hashNode{address: child.Address, kind: SubjectEntityTypeConstraint, owner: item.Address, fields: constraintFields{ID: child.ID, ColumnAddresses: child.ColumnAddresses}})
		}
	}
	for _, item := range relationTypes {
		order := columnAddresses(item.Columns)
		add(&hashNode{address: item.Address, kind: SubjectRelationType, fields: relationTypeFields{Common: item.Common, ID: item.ID, DisplayName: item.DisplayName, SemanticKind: item.SemanticKind, AllowSelf: item.AllowSelf, DuplicatePolicy: item.DuplicatePolicy, From: item.From, To: item.To, Cardinality: item.Cardinality, ForwardLabel: item.ForwardLabel, ReverseLabel: item.ReverseLabel, Traversal: item.Traversal, Projections: item.Projections, Render: item.Render, Export: item.Export, ColumnOrder: order, ReservedColumnIDs: item.ReservedColumnIDs, ReservedConstraintIDs: item.ReservedConstraintIDs}})
		for _, child := range item.Columns {
			add(&hashNode{address: child.Address, kind: SubjectRelationTypeColumn, owner: item.Address, fields: columnOwnFields(child)})
		}
		for _, child := range item.UniqueConstraints {
			add(&hashNode{address: child.Address, kind: SubjectRelationTypeConstraint, owner: item.Address, fields: constraintFields{ID: child.ID, ColumnAddresses: child.ColumnAddresses}})
		}
	}
}

func addRecipeNodes(add func(*hashNode), queries []Query, views []View, references []Reference) {
	for _, item := range queries {
		add(&hashNode{address: item.Address, kind: SubjectQuery, fields: queryFields{Common: item.Common, ID: item.ID, DisplayName: item.DisplayName, StateInput: item.StateInput, Select: item.Select, Where: item.Where, RelationWhere: item.RelationWhere, Traverse: item.Traverse, Result: item.Result, ReservedParameterIDs: item.ReservedParameterIDs}})
		for _, child := range item.Parameters {
			add(&hashNode{address: child.Address, kind: SubjectQueryParameter, owner: item.Address, fields: parameterOwnFields(child)})
		}
	}
	for _, item := range views {
		shape := item.Shape
		order := []string{}
		if shape.Table != nil {
			for _, child := range shape.Table.Columns {
				order = append(order, child.Address)
			}
			table := *shape.Table
			table.Columns = []TableColumn{}
			shape.Table = &table
		}
		add(&hashNode{address: item.Address, kind: SubjectView, fields: viewFields{Common: item.Common, ID: item.ID, DisplayName: item.DisplayName, StateInput: item.StateInput, Category: item.Category, Intent: item.Intent, Source: item.Source, RelationProjections: item.RelationProjections, Shape: shape, TableColumnOrder: order, ReservedTableColumnIDs: item.ReservedTableColumnIDs, ReservedExportIDs: item.ReservedExportIDs}})
		if item.Shape.Table != nil {
			for _, child := range item.Shape.Table.Columns {
				add(&hashNode{address: child.Address, kind: SubjectViewTableColumn, owner: item.Address, fields: tableColumnFields{ID: child.ID, Label: child.Label, Source: child.Source, Aggregate: child.Aggregate}})
			}
		}
		for _, child := range item.Exports {
			add(&hashNode{address: child.Address, kind: SubjectViewExport, owner: item.Address, fields: exportFields{ID: child.ID, Format: string(child.Format), Filename: child.Filename, Fidelity: string(child.Fidelity), SourceRefs: child.SourceRefs, ExporterProfile: child.ExporterProfile, Options: child.Options}})
		}
	}
	for _, item := range references {
		add(&hashNode{address: item.Address, kind: SubjectReference, fields: referenceFields{ID: item.ID, Text: item.Text}})
	}
}

func columnOwnFields(item Column) columnFields {
	return columnFields{ID: item.ID, DisplayName: item.DisplayName, ValueType: item.ValueType, EnumValues: item.EnumValues, ReservedEnumValues: item.ReservedEnumValues, Required: item.Required, Default: item.Default, Format: item.Format, Min: item.Min, Max: item.Max, MinLength: item.MinLength, MaxLength: item.MaxLength}
}
func parameterOwnFields(item QueryParameter) parameterFields {
	return parameterFields{ID: item.ID, ValueType: item.ValueType, EnumValues: item.EnumValues, ReservedEnumValues: item.ReservedEnumValues, Required: item.Required, Default: item.Default, Format: item.Format, Min: item.Min, Max: item.Max, MinLength: item.MinLength, MaxLength: item.MaxLength}
}
func columnAddresses(values []Column) []string {
	out := make([]string, len(values))
	for i := range values {
		out[i] = values[i].Address
	}
	return out
}

func ownerAddresses(result resolve.Result, nodes map[string]*hashNode) map[string]string {
	out := map[string]string{}
	symbols := map[string]resolve.DeclarationSymbol{}
	for _, declaration := range result.Candidates {
		symbols[declaration.Address] = declaration
	}
	for _, declaration := range result.Declarations {
		symbols[declaration.Address] = declaration
	}
	rootByOrigin := map[resolve.Origin]string{}
	for address, node := range nodes {
		if node.kind == SubjectProject || node.kind == SubjectPack {
			for _, declaration := range symbols {
				if len(declaration.Symbol.Path) == 0 && declaration.Address == address {
					rootByOrigin[declaration.Symbol.Origin] = address
				}
			}
			if address == result.RootAddress {
				rootByOrigin[resolve.Origin{Kind: originKind(result.Mode), ProjectID: projectID(result)}] = address
			}
		}
	}
	for address, declaration := range symbols {
		if declaration.Owner != nil {
			out[address] = resolve.StableAddress(*declaration.Owner)
			continue
		}
		if root := rootByOrigin[declaration.Symbol.Origin]; root != "" {
			out[address] = root
			continue
		}
		for root := range nodes {
			if strings.HasPrefix(address, root+":") {
				out[address] = root
				break
			}
		}
	}
	for address, node := range nodes {
		if node.owner != "" {
			out[address] = node.owner
		}
	}
	return out
}

func originKind(mode resolve.CompileMode) resolve.OriginKind {
	if mode == resolve.CompilePack {
		return resolve.OriginPack
	}
	return resolve.OriginProject
}
func projectID(result resolve.Result) string {
	for _, declaration := range result.Declarations {
		if declaration.Kind == resolve.KindProject {
			return declaration.Symbol.Origin.ProjectID
		}
	}
	return ""
}

func subtreeHash(node *hashNode, nodes map[string]*hashNode) (string, error) {
	if node.subtree != "" {
		return node.subtree, nil
	}
	children := make([]childHash, 0, len(node.children))
	for _, address := range node.children {
		child := nodes[address]
		hash := child.ownHash
		if ownerKinds[child.kind] {
			var err error
			hash, err = subtreeHash(child, nodes)
			if err != nil {
				return "", err
			}
		}
		children = append(children, childHash{Address: address, Hash: hash})
	}
	value, err := SemanticHash(DomainSubtree, subtreePayload{OwnerAddress: node.address, OwnerHash: node.ownHash, Children: children})
	if err != nil {
		return "", err
	}
	node.subtree = value
	return value, nil
}

var ownerKinds = map[SubjectKind]bool{SubjectProject: true, SubjectPack: true, SubjectEntityType: true, SubjectRelationType: true, SubjectEntity: true, SubjectRelation: true, SubjectQuery: true, SubjectView: true}

func childKinds(kind SubjectKind) []SubjectKind {
	switch kind {
	case SubjectProject:
		return []SubjectKind{SubjectEntityType, SubjectRelationType, SubjectLayer, SubjectEntity, SubjectRelation, SubjectQuery, SubjectView, SubjectReference}
	case SubjectPack:
		return []SubjectKind{SubjectEntityType, SubjectRelationType, SubjectQuery, SubjectView, SubjectReference}
	case SubjectEntityType:
		return []SubjectKind{SubjectEntityTypeColumn, SubjectEntityTypeConstraint}
	case SubjectRelationType:
		return []SubjectKind{SubjectRelationTypeColumn, SubjectRelationTypeConstraint}
	case SubjectEntity:
		return []SubjectKind{SubjectEntityRow}
	case SubjectRelation:
		return []SubjectKind{SubjectRelationRow}
	case SubjectQuery:
		return []SubjectKind{SubjectQueryParameter}
	case SubjectView:
		return []SubjectKind{SubjectViewTableColumn, SubjectViewExport}
	default:
		return nil
	}
}

func rootMoves(values []Move, root string) []Move {
	out := []Move{}
	for _, value := range values {
		if value.OldAddress == root || strings.HasPrefix(value.OldAddress, root+":") {
			out = append(out, value)
		}
	}
	return out
}
func rootMoveClosure(values []MoveResolution, root string) []MoveResolution {
	out := []MoveResolution{}
	for _, value := range values {
		if value.SourceAddress == root || strings.HasPrefix(value.SourceAddress, root+":") {
			out = append(out, value)
		}
	}
	return out
}

func sortAddresses(result resolve.Result, values []string) {
	sort.SliceStable(values, func(i, j int) bool { return lessAddress(result, values[i], values[j]) })
}
func lessAddress(result resolve.Result, left, right string) bool {
	return LessStableAddress(result, left, right)
}

// LessStableAddress compares generated StableAddresses using the normative
// structured StableSymbol order, including roots that have no declaration.
func LessStableAddress(result resolve.Result, left, right string) bool {
	symbols := map[string]resolve.StableSymbol{}
	for _, declaration := range result.Candidates {
		symbols[declaration.Address] = declaration.Symbol
	}
	for _, declaration := range result.Declarations {
		symbols[declaration.Address] = declaration.Symbol
	}
	a, aOK := symbols[left]
	if !aOK {
		a, aOK = stableSymbolFromAddress(left)
	}
	b, bOK := symbols[right]
	if !bOK {
		b, bOK = stableSymbolFromAddress(right)
	}
	if aOK && bOK {
		return resolve.CompareStableSymbols(a, b) < 0
	}
	return left < right
}

func stableSymbolFromAddress(address string) (resolve.StableSymbol, bool) {
	parts := strings.Split(address, ":")
	if len(parts) < 3 || parts[0] != "ldl" {
		return resolve.StableSymbol{}, false
	}
	var symbol resolve.StableSymbol
	var pathStart int
	switch parts[1] {
	case "project":
		symbol.Origin = resolve.Origin{Kind: resolve.OriginProject, ProjectID: parts[2]}
		pathStart = 3
	case "pack":
		if len(parts) < 4 {
			return resolve.StableSymbol{}, false
		}
		symbol.Origin = resolve.Origin{Kind: resolve.OriginPack, Publisher: parts[2], PackName: parts[3]}
		pathStart = 4
	default:
		return resolve.StableSymbol{}, false
	}
	if (len(parts)-pathStart)%2 != 0 {
		return resolve.StableSymbol{}, false
	}
	for index := pathStart; index < len(parts); index += 2 {
		kind, ok := resolvedAddressKind(parts[index])
		if !ok || parts[index+1] == "" {
			return resolve.StableSymbol{}, false
		}
		symbol.Path = append(symbol.Path, resolve.SymbolSegment{Kind: kind, ID: parts[index+1]})
	}
	return symbol, true
}

func resolvedAddressKind(value string) (resolve.SubjectKind, bool) {
	for _, kind := range []resolve.SubjectKind{resolve.KindEntityType, resolve.KindRelationType, resolve.KindLayer, resolve.KindEntity, resolve.KindRelation, resolve.KindQuery, resolve.KindView, resolve.KindReference, resolve.KindColumn, resolve.KindConstraint, resolve.KindRow, resolve.KindParameter, resolve.KindTableColumn, resolve.KindExport} {
		if string(kind) == value {
			return kind, true
		}
	}
	return "", false
}
func kindRank(kind SubjectKind) int {
	for index, value := range []SubjectKind{SubjectProject, SubjectPack, SubjectEntityType, SubjectRelationType, SubjectLayer, SubjectEntity, SubjectRelation, SubjectQuery, SubjectView, SubjectReference, SubjectEntityTypeColumn, SubjectEntityTypeConstraint, SubjectRelationTypeColumn, SubjectRelationTypeConstraint, SubjectEntityRow, SubjectRelationRow, SubjectQueryParameter, SubjectViewTableColumn, SubjectViewExport} {
		if kind == value {
			return index
		}
	}
	return 99
}
