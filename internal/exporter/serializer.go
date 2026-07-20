// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package exporter implements the deterministic, host-side interchange
// boundary used by Desktop. It consumes Engine-owned plans and generated wire
// values; it never interprets LDL or changes the plan's semantic mapping.
package exporter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

type FailureCode string

const (
	FailureUnsupported         FailureCode = "export.unsupported_shape_format"
	FailureProfile             FailureCode = "export.profile_incompatible"
	FailureInputMismatch       FailureCode = "export.render_input_mismatch"
	FailureAssetMissing        FailureCode = "export.asset_missing"
	FailureFontMissing         FailureCode = "export.font_missing"
	FailureSourceManifest      FailureCode = "export.source_manifest_invalid"
	FailureSerializer          FailureCode = "export.serializer_failed"
	FailureCancelled           FailureCode = "export.cancelled"
	FailureDestination         FailureCode = "export.destination_failed"
	FailureUnsafeAsset         FailureCode = "asset.unsafe_content"
	FailureDigestMismatch      FailureCode = "asset.digest_mismatch"
	FailurePreviewStale        FailureCode = "artifact.preview_stale"
	FailurePreviewIncompatible FailureCode = "artifact.preview_incompatible"
	FailureImportInvalid       FailureCode = "import.invalid_document"
)

type Failure struct {
	Code FailureCode
	err  error
}

func (f *Failure) Error() string                { return string(f.Code) }
func (f *Failure) Unwrap() error                { return f.err }
func failure(code FailureCode, err error) error { return &Failure{Code: code, err: err} }

func IsFailure(err error, code FailureCode) bool {
	var target *Failure
	return errors.As(err, &target) && target.Code == code
}

type Resource struct {
	Digest protocolcommon.Digest `json:"digest"`
	Bytes  []byte                `json:"bytes"`
}

type SerializeInput struct {
	Plan           semantic.ExportPlan `json:"plan"`
	ViewData       semantic.ViewData   `json:"view_data"`
	Assets         []Resource          `json:"assets"`
	Fonts          []Resource          `json:"fonts"`
	MaxInputBytes  int64               `json:"max_input_bytes,omitempty"`
	MaxOutputBytes int64               `json:"max_output_bytes,omitempty"`
}

type Artifact struct {
	Role          string
	LogicalPath   string
	MediaType     string
	Primary       bool
	Bytes         []byte
	ContentDigest protocolcommon.Digest
}

type Result struct {
	Artifacts          []Artifact
	SourceManifest     semantic.ExportSourceManifest
	SourceManifestJSON []byte
}

type Profile struct {
	Format        semantic.ExportFormat `json:"format"`
	SchemaVersion int64                 `json:"schema_version"`
	RequiresShape []string              `json:"requires_shape"`
}

var profiles = map[semantic.ExportFormat]Profile{
	semantic.ExportFormatJSON: {Format: semantic.ExportFormatJSON, SchemaVersion: 1},
	semantic.ExportFormatCsv:  {Format: semantic.ExportFormatCsv, SchemaVersion: 1, RequiresShape: []string{"table", "matrix"}},
	semantic.ExportFormatTsv:  {Format: semantic.ExportFormatTsv, SchemaVersion: 1, RequiresShape: []string{"table", "matrix"}},
}

func Profiles() []Profile {
	result := make([]Profile, 0, len(profiles))
	for _, value := range profiles {
		value.RequiresShape = append([]string(nil), value.RequiresShape...)
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Format < result[j].Format })
	return result
}

func Serialize(ctx context.Context, input SerializeInput) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, failure(FailureCancelled, err)
	}
	planBytes, err := semantic.EncodeExportPlan(input.Plan)
	if err != nil {
		return Result{}, failure(FailureSourceManifest, err)
	}
	viewBytes, err := semantic.EncodeViewData(input.ViewData)
	if err != nil {
		return Result{}, failure(FailureInputMismatch, err)
	}
	maxInput := input.MaxInputBytes
	if maxInput == 0 {
		maxInput = 64 << 20
	}
	maxOutput := input.MaxOutputBytes
	if maxOutput == 0 {
		maxOutput = 256 << 20
	}
	if maxInput < 1 || maxOutput < 1 || int64(len(planBytes)+len(viewBytes)) > maxInput {
		return Result{}, failure(FailureSerializer, nil)
	}
	if err := validateBinding(input.Plan, input.ViewData, viewBytes); err != nil {
		return Result{}, err
	}
	if err := validateResources(input.Plan.RequiredAssetDigests, input.Assets, FailureAssetMissing); err != nil {
		return Result{}, err
	}
	if err := validateResources(input.Plan.RequiredFontDigests, input.Fonts, FailureFontMissing); err != nil {
		return Result{}, err
	}

	primary, err := serializePrimary(input.Plan, input.ViewData)
	if err != nil {
		return Result{}, err
	}
	if int64(len(primary)) > maxOutput {
		return Result{}, failure(FailureSerializer, nil)
	}
	artifacts := make([]Artifact, len(input.Plan.Artifacts))
	var total int64
	for i, entry := range input.Plan.Artifacts {
		if err := ctx.Err(); err != nil {
			return Result{}, failure(FailureCancelled, err)
		}
		// Language 1 native JSON/CSV/TSV profiles currently produce one closed
		// artifact. Reject multi-artifact plans instead of inventing a mapping.
		if len(input.Plan.Artifacts) != 1 {
			return Result{}, failure(FailureProfile, nil)
		}
		value := bytes.Clone(primary)
		total += int64(len(value))
		if total > maxOutput {
			return Result{}, failure(FailureSerializer, nil)
		}
		artifacts[i] = Artifact{Role: entry.Role, LogicalPath: entry.LogicalPath, MediaType: entry.MediaType, Primary: entry.Primary, Bytes: value, ContentDigest: digest(value)}
	}
	manifest, manifestJSON, err := sourceManifest(input.Plan, input.ViewData, artifacts)
	if err != nil {
		return Result{}, err
	}
	if total+int64(len(manifestJSON)) > maxOutput {
		return Result{}, failure(FailureSerializer, nil)
	}
	return Result{Artifacts: artifacts, SourceManifest: manifest, SourceManifestJSON: manifestJSON}, nil
}

func validateBinding(plan semantic.ExportPlan, view semantic.ViewData, viewBytes []byte) error {
	profile, ok := profiles[plan.Format]
	if !ok {
		return failure(FailureUnsupported, nil)
	}
	if plan.SchemaVersion != 1 || profile.SchemaVersion != 1 || plan.SerializerOptions.Kind != plan.Format ||
		plan.ExporterProfile.Format != plan.Format || plan.SerializerProfile.Format != plan.Format ||
		plan.ExporterProfile.RegistrySchemaVersion != 1 || plan.SerializerProfile.RegistrySchemaVersion != 1 ||
		plan.ExporterProfile != plan.SerializerProfile {
		return failure(FailureProfile, nil)
	}
	if len(profile.RequiresShape) != 0 && !contains(profile.RequiresShape, view.Kind) {
		return failure(FailureUnsupported, nil)
	}
	if plan.RecipeAddress == "" || !strings.HasPrefix(string(plan.RecipeAddress), string(view.ViewAddress)+":export:") ||
		plan.StatePolicy != view.StatePolicy || !equalJSON(plan.StateInput, view.StateInput) {
		return failure(FailureInputMismatch, nil)
	}
	if len(plan.Artifacts) == 0 || countPrimary(plan.Artifacts) != 1 {
		return failure(FailureSourceManifest, nil)
	}
	seen := map[string]bool{}
	for _, entry := range plan.Artifacts {
		if entry.Role == "" || entry.LogicalPath == "" || entry.MediaType == "" || strings.ContainsAny(entry.LogicalPath, "/\\\x00") || seen[entry.Role] || seen[entry.LogicalPath] {
			return failure(FailureSourceManifest, nil)
		}
		seen[entry.Role], seen[entry.LogicalPath] = true, true
	}
	computed, err := viewDataHash(viewBytes)
	if err != nil || computed != plan.ViewDataHash {
		return failure(FailureInputMismatch, err)
	}
	return nil
}

func validateResources(required []protocolcommon.Digest, supplied []Resource, code FailureCode) error {
	if len(required) != len(supplied) {
		return failure(code, nil)
	}
	seen := map[protocolcommon.Digest]bool{}
	for _, resource := range supplied {
		if seen[resource.Digest] || digest(resource.Bytes) != resource.Digest {
			return failure(code, nil)
		}
		seen[resource.Digest] = true
	}
	for _, value := range required {
		if !seen[value] {
			return failure(code, nil)
		}
	}
	return nil
}

func serializePrimary(plan semantic.ExportPlan, view semantic.ViewData) ([]byte, error) {
	switch plan.Format {
	case semantic.ExportFormatJSON:
		value := struct {
			Format        string            `json:"format"`
			SchemaVersion int64             `json:"schema_version"`
			ViewData      semantic.ViewData `json:"view_data"`
		}{Format: "layerdraw-viewdata", SchemaVersion: 1, ViewData: view}
		return canonical(value)
	case semantic.ExportFormatCsv, semantic.ExportFormatTsv:
		return serializeDelimited(plan.Format, view)
	default:
		return nil, failure(FailureUnsupported, nil)
	}
}

func serializeDelimited(format semantic.ExportFormat, view semantic.ViewData) ([]byte, error) {
	var rows [][]string
	switch view.Kind {
	case "table":
		if view.Table == nil {
			return nil, failure(FailureInputMismatch, nil)
		}
		header := make([]string, len(view.Table.Columns))
		for i, column := range view.Table.Columns {
			header[i] = column.Label
		}
		rows = append(rows, header)
		for _, row := range view.Table.Rows {
			values := make([]string, len(view.Table.Columns))
			for i, column := range view.Table.Columns {
				cell, ok := row.Cells[string(column.Key)]
				if ok && cell.Present && cell.Value != nil {
					values[i] = viewValueString(*cell.Value)
				}
			}
			rows = append(rows, values)
		}
	case "matrix":
		if view.Matrix == nil {
			return nil, failure(FailureInputMismatch, nil)
		}
		header := []string{""}
		for _, column := range view.Matrix.ColumnAxis {
			header = append(header, column.Label)
		}
		rows = append(rows, header)
		cells := map[string]string{}
		for _, cell := range view.Matrix.Cells {
			cells[string(cell.RowKey)+"\x00"+string(cell.ColumnKey)] = matrixValueString(cell.DisplayValue)
		}
		for _, row := range view.Matrix.RowAxis {
			values := []string{row.Label}
			for _, column := range view.Matrix.ColumnAxis {
				values = append(values, cells[string(row.Key)+"\x00"+string(column.Key)])
			}
			rows = append(rows, values)
		}
	default:
		return nil, failure(FailureUnsupported, nil)
	}
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	if format == semantic.ExportFormatTsv {
		writer.Comma = '\t'
	}
	writer.UseCRLF = false
	if err := writer.WriteAll(rows); err != nil {
		return nil, failure(FailureSerializer, err)
	}
	return buffer.Bytes(), nil
}

func viewValueString(value semantic.ViewDataValue) string {
	if value.Scalar != nil {
		return scalarString(*value.Scalar)
	}
	if value.StableAddress != nil {
		return string(*value.StableAddress)
	}
	if value.StringSet != nil {
		encoded, _ := canonical(*value.StringSet)
		return string(encoded)
	}
	return ""
}

func scalarString(value semantic.RecipeScalar) string {
	switch value.Kind {
	case "boolean":
		if value.BooleanValue != nil {
			return fmt.Sprint(*value.BooleanValue)
		}
	case "integer":
		if value.IntegerValue != nil {
			return fmt.Sprint(*value.IntegerValue)
		}
	case "number":
		if value.NumberValue != nil {
			return string(*value.NumberValue)
		}
	case "string", "date", "datetime", "enum":
		if value.StringValue != nil {
			return *value.StringValue
		}
	}
	return ""
}

func matrixValueString(value semantic.MatrixDisplayValue) string {
	if value.Boolean != nil {
		return fmt.Sprint(*value.Boolean)
	}
	if value.Integer != nil {
		return fmt.Sprint(*value.Integer)
	}
	if value.StringSet != nil {
		encoded, _ := canonical(*value.StringSet)
		return string(encoded)
	}
	if value.Attributes != nil {
		encoded, _ := canonical(*value.Attributes)
		return string(encoded)
	}
	return ""
}

func sourceManifest(plan semantic.ExportPlan, view semantic.ViewData, artifacts []Artifact) (semantic.ExportSourceManifest, []byte, error) {
	entries := make([]semantic.CompletedExportArtifactEntry, len(artifacts))
	primary := ""
	for i, artifact := range artifacts {
		entries[i] = semantic.CompletedExportArtifactEntry{ContentDigest: artifact.ContentDigest, LogicalPath: artifact.LogicalPath, MediaType: artifact.MediaType, Primary: artifact.Primary, Role: artifact.Role}
		if artifact.Primary {
			primary = artifact.LogicalPath
		}
	}
	assetDigests := append(make([]protocolcommon.Digest, 0, len(plan.RequiredAssetDigests)), plan.RequiredAssetDigests...)
	fontDigests := append(make([]protocolcommon.Digest, 0, len(plan.RequiredFontDigests)), plan.RequiredFontDigests...)
	representations := append(make([]semantic.ExportRepresentation, 0, len(plan.Representations)), plan.Representations...)
	manifest := semantic.ExportSourceManifest{
		Artifacts: entries, AssetDigests: assetDigests,
		EffectiveMaximumFidelity: plan.EffectiveMaximumFidelity, ExporterProfile: plan.ExporterProfile,
		FidelityBasis: plan.FidelityBasis, FontDigests: fontDigests,
		Format: semantic.ExportSourceManifestFormatValue, InvocationHash: plan.InvocationHash,
		NativeMaximumFidelity: plan.NativeMaximumFidelity, PrimaryArtifact: primary, ProfileRefHash: plan.ProfileRefHash,
		ProfileRequirementsHash: plan.ProfileRequirementsHash, RecipeAddress: plan.RecipeAddress, RecipeHash: plan.RecipeHash,
		Representations: representations, RequestedFidelity: plan.RequestedFidelity,
		Revision: view.Revision, SchemaVersion: 1, SerializerProfile: plan.SerializerProfile,
		StateInput: plan.StateInput, StatePolicy: plan.StatePolicy, StateSummaryHash: plan.StateSummaryHash, ViewDataHash: plan.ViewDataHash,
	}
	encoded, err := semantic.EncodeExportSourceManifest(manifest)
	if err != nil {
		return semantic.ExportSourceManifest{}, nil, failure(FailureSourceManifest, err)
	}
	return manifest, encoded, nil
}

func viewDataHash(encoded []byte) (protocolcommon.Digest, error) {
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return "", err
	}
	stripDiagnosticMessages(value)
	canonicalValue, err := canonical(value)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, _ = h.Write([]byte("layerdraw-language-1\x00export-viewdata\x00"))
	_, _ = h.Write(canonicalValue)
	return protocolcommon.Digest("sha256:" + hex.EncodeToString(h.Sum(nil))), nil
}

func stripDiagnosticMessages(value any) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			stripDiagnosticMessages(item)
		}
	case map[string]any:
		delete(typed, "message")
		for _, item := range typed {
			stripDiagnosticMessages(item)
		}
	}
}

func canonical(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if !json.Valid(encoded) {
		return nil, errors.New("invalid json")
	}
	return encoded, nil
}

func digest(value []byte) protocolcommon.Digest {
	sum := sha256.Sum256(value)
	return protocolcommon.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func equalJSON(left, right any) bool {
	l, le := canonical(left)
	r, re := canonical(right)
	return le == nil && re == nil && bytes.Equal(l, r)
}
func countPrimary(entries []semantic.ExportArtifactEntry) int {
	count := 0
	for _, value := range entries {
		if value.Primary {
			count++
		}
	}
	return count
}
func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
