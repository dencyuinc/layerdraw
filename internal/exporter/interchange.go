// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package exporter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
)

const OperationsJSONProfile = "layerdraw.operations-json@1"

type ImportPreview struct {
	Profile    string                                `json:"profile"`
	MediaType  string                                `json:"media_type"`
	Batch      engineprotocol.SemanticOperationBatch `json:"batch"`
	Canonical  []byte                                `json:"-"`
	SourceHash string                                `json:"source_hash"`
}

type OperationsDocument struct {
	Format        string                             `json:"format"`
	SchemaVersion int64                              `json:"schema_version"`
	Operations    []engineprotocol.SemanticOperation `json:"operations"`
}

// ImportOperationsJSON returns only a generated Engine Workbench operation
// set. Callers must submit it to engine.preview_operations; this adapter never
// writes or synthesizes LDL source.
func ImportOperationsJSON(ctx context.Context, value []byte, maxBytes int64, maxOperations int) (ImportPreview, error) {
	if maxBytes == 0 {
		maxBytes = 16 << 20
	}
	if maxOperations == 0 {
		maxOperations = 10_000
	}
	if err := ctx.Err(); err != nil {
		return ImportPreview{}, failure(FailureCancelled, err)
	}
	if maxBytes < 1 || maxOperations < 1 || int64(len(value)) > maxBytes {
		return ImportPreview{}, failure(FailureImportInvalid, nil)
	}
	var raw struct {
		Format        string          `json:"format"`
		SchemaVersion int64           `json:"schema_version"`
		Operations    json.RawMessage `json:"operations"`
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil || raw.Format != "layerdraw-semantic-operations" || raw.SchemaVersion != 1 {
		return ImportPreview{}, failure(FailureImportInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ImportPreview{}, failure(FailureImportInvalid, nil)
	}
	batch, err := engineprotocol.DecodeSemanticOperationBatch([]byte(`{"operations":` + string(raw.Operations) + `}`))
	if err != nil || len(batch.Operations) == 0 || len(batch.Operations) > maxOperations {
		return ImportPreview{}, failure(FailureImportInvalid, err)
	}
	canonicalBatch, err := engineprotocol.EncodeSemanticOperationBatch(batch)
	if err != nil {
		return ImportPreview{}, failure(FailureImportInvalid, err)
	}
	canonicalDocument, err := canonical(struct {
		Format        string          `json:"format"`
		SchemaVersion int64           `json:"schema_version"`
		Batch         json.RawMessage `json:"operations"`
	}{Format: raw.Format, SchemaVersion: 1, Batch: extractOperations(canonicalBatch)})
	if err != nil {
		return ImportPreview{}, failure(FailureImportInvalid, err)
	}
	return ImportPreview{Profile: OperationsJSONProfile, MediaType: "application/vnd.layerdraw.operations+json", Batch: batch, Canonical: canonicalDocument, SourceHash: string(digest(value))}, nil
}

func extractOperations(batch []byte) json.RawMessage {
	var value struct {
		Operations json.RawMessage `json:"operations"`
	}
	_ = json.Unmarshal(batch, &value)
	return value.Operations
}
