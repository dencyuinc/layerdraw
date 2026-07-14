// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package index

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

// Search v1 is deliberately self-contained. These fixed weights are not read
// from a host profile and therefore cannot vary across compilation surfaces.
const (
	SearchContentHashPrefix  = "layerdraw-search-document-v1\x00"
	SearchWeightIdentity     = 100
	SearchWeightTaxonomy     = 80
	SearchWeightDescription  = 60
	SearchWeightAttribute    = 40
	SearchWeightGuidance     = 20
	SearchIncludeInEmbedding = true

	SearchFieldID           = "id"
	SearchFieldDisplayName  = "display_name"
	SearchFieldDescription  = "description"
	SearchFieldTypeAddress  = "type_address"
	SearchFieldLayerAddress = "layer_address"
	SearchFieldForwardLabel = "forward_label"
	SearchFieldReverseLabel = "reverse_label"
	SearchFieldIntent       = "intent"
	SearchFieldText         = "text"
)

type searchContentPayload struct {
	SchemaVersion       int                     `json:"schema_version"`
	SubjectAddress      string                  `json:"subject_address"`
	SubjectKind         materialize.SubjectKind `json:"subject_kind"`
	OwnerAddress        *string                 `json:"owner_address,omitempty"`
	GraphEntryAddresses []string                `json:"graph_entry_addresses"`
	TypeAddresses       []string                `json:"type_addresses"`
	LayerAddresses      []string                `json:"layer_addresses"`
	Fields              []searchHashField       `json:"fields"`
}

// SourceRef is navigational metadata, not searchable content. Excluding it
// keeps content_hash stable across formatting and comment-only byte shifts.
type searchHashField struct {
	FieldPath          string `json:"field_path"`
	Text               string `json:"text"`
	LexicalWeight      int    `json:"lexical_weight"`
	IncludeInEmbedding bool   `json:"include_in_embedding"`
}

func buildSearchDocuments(snapshot materialize.Snapshot, sourceMap SourceMapV1, resolved resolve.Result) ([]SearchDocument, error) {
	if snapshot.Pack != nil {
		return nil, nil
	}
	if snapshot.Document == nil {
		return nil, fmt.Errorf("Project search requires a normalized document")
	}
	document := snapshot.Document
	ranges := map[string]*resolve.SourceRange{}
	for _, subject := range sourceMap.Subjects {
		if subject.DeclarationRange != nil {
			copy := *subject.DeclarationRange
			ranges[subject.Address] = &copy
		}
	}
	entities := map[string]materialize.Entity{}
	for _, entity := range document.Entities {
		entities[entity.Address] = entity
	}
	relationTypes := map[string]materialize.RelationType{}
	for _, relationType := range document.RelationTypes {
		relationTypes[relationType.Address] = relationType
	}

	documents := []SearchDocument{}
	for _, item := range document.EntityTypes {
		doc := newSearchDocument(item.Address, materialize.SubjectEntityType, "", ranges[item.Address])
		addCommonFields(&doc, item.ID, item.DisplayName, item.Common)
		for _, column := range item.Columns {
			addSearchFieldAt(&doc, columnDisplayFieldPath(column.Address), column.DisplayName, SearchWeightTaxonomy, ranges[column.Address])
		}
		documents = append(documents, doc)
	}
	for _, item := range document.RelationTypes {
		doc := newSearchDocument(item.Address, materialize.SubjectRelationType, "", ranges[item.Address])
		addCommonFields(&doc, item.ID, item.DisplayName, item.Common)
		addSearchField(&doc, SearchFieldForwardLabel, item.ForwardLabel, SearchWeightTaxonomy)
		if item.ReverseLabel != nil {
			addSearchField(&doc, SearchFieldReverseLabel, *item.ReverseLabel, SearchWeightTaxonomy)
		}
		for _, column := range item.Columns {
			addSearchFieldAt(&doc, columnDisplayFieldPath(column.Address), column.DisplayName, SearchWeightTaxonomy, ranges[column.Address])
		}
		documents = append(documents, doc)
	}
	for _, item := range document.Layers {
		doc := newSearchDocument(item.Address, materialize.SubjectLayer, "", ranges[item.Address])
		addCommonFields(&doc, item.ID, item.DisplayName, item.Common)
		documents = append(documents, doc)
	}
	for _, item := range document.Entities {
		doc := newSearchDocument(item.Address, materialize.SubjectEntity, "", ranges[item.Address])
		doc.GraphEntryAddresses = []string{item.Address}
		doc.TypeAddresses = []string{item.TypeAddress}
		doc.LayerAddresses = []string{item.LayerAddress}
		addCommonFields(&doc, item.ID, item.DisplayName, item.Common)
		addSearchField(&doc, SearchFieldTypeAddress, item.TypeAddress, SearchWeightTaxonomy)
		addSearchField(&doc, SearchFieldLayerAddress, item.LayerAddress, SearchWeightTaxonomy)
		documents = append(documents, doc)
		for _, row := range item.Rows {
			documents = append(documents, rowSearchDocument(row, materialize.SubjectEntityRow, item.Address, item.TypeAddress, []string{item.LayerAddress}, []string{item.Address}, nil, nil, ranges[row.Address], resolved))
		}
	}
	for _, item := range document.Relations {
		entries := dedupeOrdered([]string{item.FromAddress, item.ToAddress})
		layers := endpointLayers(entries, entities, resolved)
		doc := newSearchDocument(item.Address, materialize.SubjectRelation, "", ranges[item.Address])
		doc.GraphEntryAddresses, doc.TypeAddresses, doc.LayerAddresses = entries, []string{item.TypeAddress}, layers
		addSearchField(&doc, SearchFieldID, item.ID, SearchWeightIdentity)
		if item.DisplayName != nil {
			addSearchField(&doc, SearchFieldDisplayName, *item.DisplayName, SearchWeightIdentity)
		}
		addCommonOnly(&doc, item.Common)
		addSearchField(&doc, SearchFieldTypeAddress, item.TypeAddress, SearchWeightTaxonomy)
		relationType := relationTypes[item.TypeAddress]
		addRelationLabels(&doc, relationType, ranges[item.TypeAddress])
		documents = append(documents, doc)
		for _, row := range item.Rows {
			documents = append(documents, rowSearchDocument(row, materialize.SubjectRelationRow, item.Address, item.TypeAddress, layers, entries, &relationType, ranges[item.TypeAddress], ranges[row.Address], resolved))
		}
	}
	for _, item := range document.Queries {
		doc := newSearchDocument(item.Address, materialize.SubjectQuery, "", ranges[item.Address])
		addCommonFields(&doc, item.ID, item.DisplayName, item.Common)
		documents = append(documents, doc)
	}
	for _, item := range document.Views {
		doc := newSearchDocument(item.Address, materialize.SubjectView, "", ranges[item.Address])
		addCommonFields(&doc, item.ID, item.DisplayName, item.Common)
		if item.Intent != nil {
			addSearchField(&doc, SearchFieldIntent, *item.Intent, SearchWeightGuidance)
		}
		documents = append(documents, doc)
	}
	for _, item := range document.References {
		doc := newSearchDocument(item.Address, materialize.SubjectReference, "", ranges[item.Address])
		addSearchField(&doc, SearchFieldID, item.ID, SearchWeightIdentity)
		addSearchField(&doc, SearchFieldText, item.Text, SearchWeightGuidance)
		documents = append(documents, doc)
	}
	for i := range documents {
		sortAddresses(resolved, documents[i].TypeAddresses)
		sortAddresses(resolved, documents[i].LayerAddresses)
		hash, err := searchContentHash(documents[i])
		if err != nil {
			return nil, err
		}
		documents[i].ContentHash = hash
	}
	sort.Slice(documents, func(i, j int) bool {
		return lessAddress(resolved, documents[i].SubjectAddress, documents[j].SubjectAddress)
	})
	return documents, nil
}

func newSearchDocument(address string, kind materialize.SubjectKind, owner string, source *resolve.SourceRange) SearchDocument {
	return SearchDocument{SchemaVersion: SearchDocumentSchemaVersion, SubjectAddress: address, SubjectKind: kind, OwnerAddress: optionalString(owner), GraphEntryAddresses: []string{}, TypeAddresses: []string{}, LayerAddresses: []string{}, Fields: []SearchField{}, defaultSource: cloneRange(source)}
}

func addCommonFields(document *SearchDocument, id, displayName string, common materialize.Common) {
	addSearchField(document, SearchFieldID, id, SearchWeightIdentity)
	addSearchField(document, SearchFieldDisplayName, displayName, SearchWeightIdentity)
	addCommonOnly(document, common)
}

func addCommonOnly(document *SearchDocument, common materialize.Common) {
	for index, tag := range common.Tags {
		addSearchField(document, tagFieldPath(index), tag, SearchWeightTaxonomy)
	}
	if common.Description != nil {
		addSearchField(document, SearchFieldDescription, *common.Description, SearchWeightDescription)
	}
}

func addSearchField(document *SearchDocument, path, text string, weight int) {
	addSearchFieldAt(document, path, text, weight, document.defaultSource)
}

func addSearchFieldAt(document *SearchDocument, path, text string, weight int, source *resolve.SourceRange) {
	document.Fields = append(document.Fields, SearchField{FieldPath: path, SourceRef: cloneRange(source), Text: materialize.NormalizeString(text), LexicalWeight: weight, IncludeInEmbedding: SearchIncludeInEmbedding})
}

func rowSearchDocument(row materialize.AttributeRow, kind materialize.SubjectKind, owner, typeAddress string, layers, entries []string, relationType *materialize.RelationType, relationTypeSource, source *resolve.SourceRange, resolved resolve.Result) SearchDocument {
	doc := newSearchDocument(row.Address, kind, owner, source)
	doc.GraphEntryAddresses, doc.TypeAddresses, doc.LayerAddresses = cloneStrings(entries), []string{typeAddress}, cloneStrings(layers)
	sortAddresses(resolved, doc.TypeAddresses)
	sortAddresses(resolved, doc.LayerAddresses)
	addSearchField(&doc, SearchFieldID, row.ID, SearchWeightIdentity)
	keys := make([]string, 0, len(row.Values))
	for key := range row.Values {
		keys = append(keys, key)
	}
	sortAddresses(resolved, keys)
	for _, key := range keys {
		addSearchField(&doc, valueFieldPath(key), scalarText(row.Values[key]), SearchWeightAttribute)
	}
	if relationType != nil {
		addRelationLabels(&doc, *relationType, relationTypeSource)
	}
	return doc
}

func addRelationLabels(document *SearchDocument, relationType materialize.RelationType, source *resolve.SourceRange) {
	addSearchFieldAt(document, SearchFieldForwardLabel, relationType.ForwardLabel, SearchWeightTaxonomy, source)
	if relationType.ReverseLabel != nil {
		addSearchFieldAt(document, SearchFieldReverseLabel, *relationType.ReverseLabel, SearchWeightTaxonomy, source)
	}
}

func columnDisplayFieldPath(address string) string { return "columns." + address + ".display_name" }
func tagFieldPath(index int) string                { return "tags." + strconv.Itoa(index) }
func valueFieldPath(columnAddress string) string   { return "values." + columnAddress }

func scalarText(value materialize.Scalar) string {
	switch value.Type {
	case definition.ScalarString, definition.ScalarEnum, definition.ScalarDate, definition.ScalarDatetime:
		return value.String
	case definition.ScalarInteger:
		return strconv.FormatInt(value.Int, 10)
	case definition.ScalarNumber:
		encoded, err := materialize.Canonicalize(value)
		if err == nil {
			return string(encoded)
		}
		return strconv.FormatFloat(value.Float, 'g', -1, 64)
	case definition.ScalarBoolean:
		return strconv.FormatBool(value.Bool)
	default:
		return ""
	}
}

func endpointLayers(addresses []string, entities map[string]materialize.Entity, resolved resolve.Result) []string {
	values := []string{}
	for _, address := range addresses {
		if entity, exists := entities[address]; exists {
			values = append(values, entity.LayerAddress)
		}
	}
	values = dedupeOrdered(values)
	sortAddresses(resolved, values)
	return values
}

func dedupeOrdered(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func searchContentHash(document SearchDocument) (string, error) {
	fields := make([]searchHashField, len(document.Fields))
	for index, field := range document.Fields {
		fields[index] = searchHashField{FieldPath: field.FieldPath, Text: field.Text, LexicalWeight: field.LexicalWeight, IncludeInEmbedding: field.IncludeInEmbedding}
	}
	payload := searchContentPayload{SchemaVersion: document.SchemaVersion, SubjectAddress: document.SubjectAddress, SubjectKind: document.SubjectKind, OwnerAddress: document.OwnerAddress, GraphEntryAddresses: document.GraphEntryAddresses, TypeAddresses: document.TypeAddresses, LayerAddresses: document.LayerAddresses, Fields: fields}
	canonical, err := materialize.Canonicalize(payload)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(SearchContentHashPrefix))
	_, _ = hash.Write(canonical)
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func cloneRange(value *resolve.SourceRange) *resolve.SourceRange {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}
