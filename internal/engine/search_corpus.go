// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"encoding/json"
	"strings"
)

type SearchCorpusDocument struct {
	SubjectAddress      string
	SubjectKind         string
	OwnerAddress        string
	GraphEntryAddresses []string
	TypeAddresses       []string
	LayerAddresses      []string
	ContentHash         string
	LexicalText         string
	Text                string
	Fields              []SearchCorpusField
}

type SearchCorpusField struct {
	FieldPath     string
	SourceRef     string
	Text          string
	LexicalWeight int
}

// SearchCorpusProjection is implemented only by the trusted Access boundary.
// Engine remains responsible for canonical field ordering and text assembly.
type SearchCorpusProjection interface {
	AllowSearchDocument(SearchDocument) bool
	AllowSearchField(SearchDocument, SearchField) bool
}

// ReadSearchCorpus returns the canonical SearchDocuments owned by one retained
// immutable Engine generation. It never accepts caller-authored document text.
func (e Engine) ReadSearchCorpus(ctx context.Context, generation DocumentGeneration, projection SearchCorpusProjection) ([]SearchCorpusDocument, error) {
	if projection == nil {
		return nil, &WorkbenchError{Code: "engine.workbench.search_projection_invalid", Category: WorkbenchErrorInputInvalid}
	}
	_, snapshot, err := e.acquireSnapshot(ctx, generation)
	if err != nil {
		return nil, err
	}
	documents := make([]SearchCorpusDocument, 0, len(snapshot.compiled.SearchDocuments))
	for _, document := range snapshot.compiled.SearchDocuments {
		if !projection.AllowSearchDocument(document) {
			continue
		}
		lexicalFields := make([]string, 0, len(document.Fields))
		embeddingFields := make([]string, 0, len(document.Fields))
		fields := make([]SearchCorpusField, 0, len(document.Fields))
		for _, field := range document.Fields {
			if projection.AllowSearchField(document, field) && field.Text != "" {
				repetitions := field.LexicalWeight / 20
				if repetitions < 1 {
					repetitions = 1
				}
				for index := 0; index < repetitions; index++ {
					lexicalFields = append(lexicalFields, field.Text)
				}
				sourceRef := ""
				if field.SourceRef != nil {
					if encoded, encodeErr := json.Marshal(field.SourceRef); encodeErr == nil {
						sourceRef = string(encoded)
					}
				}
				fields = append(fields, SearchCorpusField{FieldPath: field.FieldPath, SourceRef: sourceRef, Text: field.Text, LexicalWeight: field.LexicalWeight})
				if field.IncludeInEmbedding {
					embeddingFields = append(embeddingFields, field.Text)
				}
			}
		}
		owner := ""
		if document.OwnerAddress != nil {
			owner = *document.OwnerAddress
		}
		documents = append(documents, SearchCorpusDocument{SubjectAddress: document.SubjectAddress, SubjectKind: string(document.SubjectKind), OwnerAddress: owner, GraphEntryAddresses: append([]string(nil), document.GraphEntryAddresses...), TypeAddresses: append([]string(nil), document.TypeAddresses...), LayerAddresses: append([]string(nil), document.LayerAddresses...), ContentHash: document.ContentHash, LexicalText: strings.Join(lexicalFields, "\n"), Text: strings.Join(embeddingFields, "\n"), Fields: fields})
	}
	return documents, nil
}
