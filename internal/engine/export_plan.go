// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/canonicaljson"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
)

// ExportPlanInput is the complete closed semantic input to the Go-owned export
// planner. It deliberately contains neither RenderData nor serializer state.
type ExportPlanInput struct {
	ViewData     ExportPlanViewData
	Recipe       ExportPlanRecipe
	Requirements ExportPlanRequirements
	StateSummary *ExportPlanStateSummary
}

type ExportPlanRequirements struct {
	ExporterProfile, SerializerProfile        ExportPlanProfileRef
	RequiredAssetDigests, RequiredFontDigests []string
	CanonicalJSON                             []byte
}

type ExportPlanViewData struct {
	Kind, ViewAddress, RevisionKind, StatePolicy string
	DefinitionHash                               *string
	StateInput                                   ExportPlanStateInput
	Source                                       ExportPlanSourceRefs
	CanonicalJSON                                []byte
}

type ExportPlanRecipe struct {
	Address, ViewAddress, Format, Filename, Extension, Fidelity    string
	NativeMaximumFidelity, EffectiveMaximumFidelity, FidelityBasis string
	ExporterProfile                                                ExportPlanProfileRef
	Options                                                        ExportPlanOptions
	RequiresSourceManifest                                         bool
	CanonicalJSON, SerializerOptions                               []byte
}

type ExportPlanOptions struct {
	Bundle       *bool   `json:"bundle,omitempty"`
	StateSummary *bool   `json:"state_summary,omitempty"`
	Orientation  *string `json:"orientation,omitempty"`
	PageSize     *string `json:"page_size,omitempty"`
}
type ExportPlanProfileRef struct {
	ID                    string `json:"id"`
	Format                string `json:"format"`
	RegistrySchemaVersion int64  `json:"registry_schema_version"`
	RegistryDigest        string `json:"registry_digest"`
	SpecificationDigest   string `json:"specification_digest"`
}
type ExportPlanStateSummary struct {
	Format, DefinitionHash, StateVersion, PayloadHash string
	SchemaVersion                                     int64
	PayloadCanonicalJSON, CanonicalJSON               []byte
}
type ExportPlanStateInput struct {
	Kind           string  `json:"kind"`
	CapturedAt     *string `json:"captured_at,omitempty"`
	DefinitionHash *string `json:"definition_hash,omitempty"`
	SnapshotHash   *string `json:"snapshot_hash,omitempty"`
	StateVersion   *string `json:"state_version,omitempty"`
}
type ExportPlanCellRef struct {
	RowAddress    string `json:"row_address"`
	ColumnAddress string `json:"column_address"`
}
type ExportPlanStateReadRef struct {
	SubjectAddress string `json:"subject_address"`
	FieldPath      string `json:"field_path"`
}
type ExportPlanStateRefs struct {
	Reads []ExportPlanStateReadRef `json:"reads"`
}
type ExportPlanSourceRefs struct {
	SubjectAddresses  []string            `json:"subject_addresses"`
	EntityAddresses   []string            `json:"entity_addresses"`
	RelationAddresses []string            `json:"relation_addresses"`
	LayerAddresses    []string            `json:"layer_addresses"`
	RowAddresses      []string            `json:"row_addresses"`
	CellRefs          []ExportPlanCellRef `json:"cell_refs"`
	AssetDigests      []string            `json:"asset_digests"`
	State             ExportPlanStateRefs `json:"state"`
}

type ExportArtifactEntry struct {
	LogicalPath string `json:"logical_path"`
	Role        string `json:"role"`
	MediaType   string `json:"media_type"`
	Primary     bool   `json:"primary"`
}
type ExportRepresentation struct {
	ArtifactRole   *string              `json:"artifact_role,omitempty"`
	Disposition    string               `json:"disposition"`
	Locator        *string              `json:"locator,omitempty"`
	OmissionReason *string              `json:"omission_reason,omitempty"`
	Source         ExportPlanSourceRefs `json:"source"`
	UnitID         *string              `json:"unit_id,omitempty"`
	ViewdataKey    string               `json:"viewdata_key"`
}
type ExportPlanUnit struct {
	ArtifactRole string   `json:"artifact_role"`
	Kind         string   `json:"kind"`
	Order        string   `json:"order"`
	Role         string   `json:"role"`
	UnitID       string   `json:"unit_id"`
	ViewdataKeys []string `json:"viewdata_keys"`
}
type ExportPagination struct {
	Kind        string  `json:"kind"`
	Orientation *string `json:"orientation,omitempty"`
	PageSize    *string `json:"page_size,omitempty"`
}
type ExportPlan struct {
	Artifacts                []ExportArtifactEntry  `json:"artifacts"`
	EffectiveMaximumFidelity string                 `json:"effective_maximum_fidelity"`
	ExporterProfile          ExportPlanProfileRef   `json:"exporter_profile"`
	FidelityBasis            string                 `json:"fidelity_basis"`
	Format                   string                 `json:"format"`
	InvocationHash           string                 `json:"invocation_hash"`
	LayoutRequirement        string                 `json:"layout_requirement"`
	NativeMaximumFidelity    string                 `json:"native_maximum_fidelity"`
	Pagination               ExportPagination       `json:"pagination"`
	ProfileRefHash           string                 `json:"profile_ref_hash"`
	ProfileRequirementsHash  string                 `json:"profile_requirements_hash"`
	RecipeAddress            string                 `json:"recipe_address"`
	RecipeHash               string                 `json:"recipe_hash"`
	Representations          []ExportRepresentation `json:"representations"`
	RequestedFidelity        string                 `json:"requested_fidelity"`
	RequiredAssetDigests     []string               `json:"required_asset_digests"`
	RequiredFontDigests      []string               `json:"required_font_digests"`
	RequiresRenderer         bool                   `json:"requires_renderer"`
	SchemaVersion            int64                  `json:"schema_version"`
	SerializerOptions        json.RawMessage        `json:"serializer_options"`
	SerializerProfile        ExportPlanProfileRef   `json:"serializer_profile"`
	SourceManifestPath       *string                `json:"source_manifest_path,omitempty"`
	SourceManifestRequired   bool                   `json:"source_manifest_required"`
	StateInput               ExportPlanStateInput   `json:"state_input"`
	StatePolicy              string                 `json:"state_policy"`
	StateSummaryHash         *string                `json:"state_summary_hash,omitempty"`
	Units                    []ExportPlanUnit       `json:"units"`
	ViewDataHash             string                 `json:"view_data_hash"`
}

// PlanExport deterministically maps complete ViewData, one compiled recipe, and
// host-resolved profile requirements to serializer instructions. It validates
// their exact recipe binding but does not perform registry resolution, layout,
// serialization, or artifact I/O.
func (e Engine) PlanExport(ctx context.Context, input ExportPlanInput) (ExportPlan, error) {
	if err := ctx.Err(); err != nil {
		return ExportPlan{}, err
	}
	if len(input.ViewData.CanonicalJSON) == 0 || len(input.Recipe.CanonicalJSON) == 0 || len(input.Requirements.CanonicalJSON) == 0 {
		return ExportPlan{}, invalidExportPlan("export.invocation_invalid", nil)
	}
	if err := validateCanonicalExportInput(input); err != nil {
		return ExportPlan{}, invalidExportPlan("export.invocation_invalid", err)
	}
	if err := validateExportViewStateInput(input.ViewData.StatePolicy, input.ViewData.StateInput.Kind); err != nil {
		return ExportPlan{}, invalidExportPlan("export.view_data_invalid", err)
	}
	if input.Recipe.ViewAddress != input.ViewData.ViewAddress {
		return ExportPlan{}, invalidExportPlan("export.render_input_mismatch", nil)
	}
	if input.Recipe.ExporterProfile.Format != input.Recipe.Format {
		return ExportPlan{}, invalidExportPlan("export.profile_incompatible", nil)
	}
	if input.Requirements.ExporterProfile != input.Recipe.ExporterProfile || input.Requirements.SerializerProfile != input.Recipe.ExporterProfile {
		return ExportPlan{}, invalidExportPlan("export.profile_requirements_mismatch", nil)
	}
	if err := validateExportRequirements(input.Requirements); err != nil {
		return ExportPlan{}, invalidExportPlan("export.profile_requirements_invalid", err)
	}

	viewProjection, err := exportHashProjection(input.ViewData.CanonicalJSON)
	if err != nil {
		return ExportPlan{}, invalidExportPlan("export.view_data_invalid", err)
	}
	viewHash, err := materialize.SemanticHash(materialize.DomainExportViewData, viewProjection)
	if err != nil {
		return ExportPlan{}, invalidExportPlan("export.view_data_invalid", err)
	}
	recipeProjection, err := encodedProjection(input.Recipe.CanonicalJSON)
	if err != nil {
		return ExportPlan{}, invalidExportPlan("export.recipe_invalid", err)
	}
	recipeHash, err := materialize.SemanticHash(materialize.DomainExportRecipe, recipeProjection)
	if err != nil {
		return ExportPlan{}, invalidExportPlan("export.recipe_invalid", err)
	}
	profileJSON, err := json.Marshal(input.Recipe.ExporterProfile)
	if err != nil {
		return ExportPlan{}, invalidExportPlan("export.profile_incompatible", err)
	}
	profileProjection, err := encodedProjection(profileJSON)
	if err != nil {
		return ExportPlan{}, invalidExportPlan("export.profile_incompatible", err)
	}
	profileHash, err := materialize.SemanticHash(materialize.DomainExportProfileRef, profileProjection)
	if err != nil {
		return ExportPlan{}, invalidExportPlan("export.profile_incompatible", err)
	}
	requirementsProjection, err := encodedProjection(input.Requirements.CanonicalJSON)
	if err != nil {
		return ExportPlan{}, invalidExportPlan("export.profile_requirements_invalid", err)
	}
	requirementsHash, err := materialize.SemanticHash(materialize.DomainExportRequirements, requirementsProjection)
	if err != nil {
		return ExportPlan{}, invalidExportPlan("export.profile_requirements_invalid", err)
	}
	stateSummaryHash, err := validateExportStateSummary(input)
	if err != nil {
		return ExportPlan{}, err
	}
	invocationHash, err := materialize.SemanticHash(materialize.DomainExportInvocation, map[string]any{
		"profile_ref_hash":          profileHash,
		"profile_requirements_hash": requirementsHash,
		"recipe_hash":               recipeHash,
		"state_summary_hash":        optionalDigestValue(stateSummaryHash),
		"view_data_hash":            viewHash,
	})
	if err != nil {
		return ExportPlan{}, invalidExportPlan("export.invocation_invalid", err)
	}

	items, err := collectExportItems(viewProjection)
	if err != nil {
		return ExportPlan{}, invalidExportPlan("export.view_data_invalid", err)
	}
	artifacts := exportArtifacts(input.ViewData.Kind, input.Recipe)
	units := exportPlanUnits(input.ViewData.Kind, input.Recipe, artifacts)
	representations := exportRepresentations(items, input.ViewData.Source, input.Recipe, units)
	if err := validateExportTopology(artifacts, units, representations); err != nil {
		return ExportPlan{}, invalidExportPlan("export.plan_invariant", err)
	}
	assets := mergeExportDigests(collectExportAssetDigests(viewProjection), input.Requirements.RequiredAssetDigests)
	fonts := mergeExportDigests(nil, input.Requirements.RequiredFontDigests)
	requiresRenderer := exportRequiresRenderer(input.Recipe.Format)
	pagination := exportPagination(input.Recipe)
	sourceManifestRequired := input.Recipe.RequiresSourceManifest || input.ViewData.StatePolicy != "none"
	var sourceManifestPath *string
	if sourceManifestRequired {
		path := strings.TrimSuffix(input.Recipe.Filename, input.Recipe.Extension) + ".sources.json"
		sourceManifestPath = &path
	}
	plan := ExportPlan{
		Artifacts: artifacts, EffectiveMaximumFidelity: input.Recipe.EffectiveMaximumFidelity,
		ExporterProfile: input.Recipe.ExporterProfile, FidelityBasis: input.Recipe.FidelityBasis,
		Format: input.Recipe.Format, InvocationHash: string(invocationHash),
		LayoutRequirement: exportLayoutRequirement(requiresRenderer), NativeMaximumFidelity: input.Recipe.NativeMaximumFidelity,
		Pagination: pagination, ProfileRefHash: string(profileHash), ProfileRequirementsHash: string(requirementsHash), RecipeAddress: input.Recipe.Address,
		RecipeHash: string(recipeHash), Representations: representations, RequestedFidelity: input.Recipe.Fidelity,
		RequiredAssetDigests: assets, RequiredFontDigests: fonts, RequiresRenderer: requiresRenderer,
		SchemaVersion: 1, SerializerOptions: bytes.Clone(input.Recipe.SerializerOptions), SerializerProfile: input.Requirements.SerializerProfile, SourceManifestPath: sourceManifestPath,
		SourceManifestRequired: sourceManifestRequired, StateInput: input.ViewData.StateInput, StatePolicy: input.ViewData.StatePolicy,
		StateSummaryHash: stateSummaryHash, Units: units, ViewDataHash: string(viewHash),
	}
	return plan, nil
}

func validateExportViewStateInput(policy, inputKind string) error {
	switch policy {
	case "none":
		if inputKind != "none" {
			return fmt.Errorf("state policy none requires state input none")
		}
	case "required":
		if inputKind != "snapshot" {
			return fmt.Errorf("state policy required requires a snapshot state input")
		}
	case "optional":
		if inputKind != "none" && inputKind != "snapshot" {
			return fmt.Errorf("state policy optional requires state input none or snapshot")
		}
	default:
		return fmt.Errorf("unsupported state policy %q", policy)
	}
	return nil
}

func validateCanonicalExportInput(input ExportPlanInput) error {
	// The generated semantic codecs are the sole authority for accepting these
	// bytes. Manual projection below occurs only after full schema validation
	// and byte-for-byte equality with the generated canonical encoding.
	if err := canonicaljson.ValidateViewData(input.ViewData.CanonicalJSON); err != nil {
		return err
	}
	var view struct {
		Kind        string `json:"kind"`
		ViewAddress string `json:"view_address"`
		StatePolicy string `json:"state_policy"`
		Revision    struct {
			Kind           string  `json:"kind"`
			DefinitionHash *string `json:"definition_hash,omitempty"`
		} `json:"revision"`
		StateInput ExportPlanStateInput `json:"state_input"`
		Source     ExportPlanSourceRefs `json:"source"`
	}
	if err := json.Unmarshal(input.ViewData.CanonicalJSON, &view); err != nil {
		return err
	}
	viewFields := ExportPlanViewData{Kind: view.Kind, ViewAddress: view.ViewAddress, RevisionKind: view.Revision.Kind, DefinitionHash: view.Revision.DefinitionHash, StatePolicy: view.StatePolicy, StateInput: view.StateInput, Source: view.Source, CanonicalJSON: input.ViewData.CanonicalJSON}
	if !reflect.DeepEqual(input.ViewData, viewFields) {
		return fmt.Errorf("ViewData fields do not match canonical bytes")
	}
	if err := canonicaljson.ValidateExportRecipe(input.Recipe.CanonicalJSON); err != nil {
		return err
	}
	var recipe struct {
		Address                  string               `json:"address"`
		ViewAddress              string               `json:"view_address"`
		Format                   string               `json:"format"`
		Filename                 string               `json:"filename"`
		Extension                string               `json:"extension"`
		Fidelity                 string               `json:"fidelity"`
		NativeMaximumFidelity    string               `json:"native_maximum_fidelity"`
		EffectiveMaximumFidelity string               `json:"effective_maximum_fidelity"`
		FidelityBasis            string               `json:"fidelity_basis"`
		ExporterProfile          ExportPlanProfileRef `json:"exporter_profile"`
		Options                  json.RawMessage      `json:"options"`
		RequiresSourceManifest   bool                 `json:"requires_source_manifest"`
	}
	if err := json.Unmarshal(input.Recipe.CanonicalJSON, &recipe); err != nil {
		return err
	}
	var plannerOptions ExportPlanOptions
	if err := json.Unmarshal(recipe.Options, &plannerOptions); err != nil {
		return err
	}
	recipeFields := ExportPlanRecipe{Address: recipe.Address, ViewAddress: recipe.ViewAddress, Format: recipe.Format, Filename: recipe.Filename, Extension: recipe.Extension, Fidelity: recipe.Fidelity, NativeMaximumFidelity: recipe.NativeMaximumFidelity, EffectiveMaximumFidelity: recipe.EffectiveMaximumFidelity, FidelityBasis: recipe.FidelityBasis, ExporterProfile: recipe.ExporterProfile, Options: plannerOptions, RequiresSourceManifest: recipe.RequiresSourceManifest, CanonicalJSON: input.Recipe.CanonicalJSON, SerializerOptions: recipe.Options}
	if !reflect.DeepEqual(input.Recipe, recipeFields) {
		return fmt.Errorf("ExportRecipe fields do not match canonical bytes")
	}
	if err := validateExportRequirements(input.Requirements); err != nil {
		return err
	}
	if input.StateSummary != nil {
		if err := canonicaljson.ValidateExternalStateSummary(input.StateSummary.CanonicalJSON); err != nil {
			return err
		}
		var summary struct {
			Format         string          `json:"format"`
			DefinitionHash string          `json:"definition_hash"`
			StateVersion   string          `json:"state_version"`
			PayloadHash    string          `json:"payload_hash"`
			SchemaVersion  int64           `json:"schema_version"`
			Payload        json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(input.StateSummary.CanonicalJSON, &summary); err != nil {
			return err
		}
		if summary.Format != input.StateSummary.Format || summary.SchemaVersion != input.StateSummary.SchemaVersion || summary.DefinitionHash != input.StateSummary.DefinitionHash || summary.StateVersion != input.StateSummary.StateVersion || summary.PayloadHash != input.StateSummary.PayloadHash || !bytes.Equal(summary.Payload, input.StateSummary.PayloadCanonicalJSON) {
			return fmt.Errorf("ExternalStateSummary fields do not match canonical bytes")
		}
	}
	return nil
}

func invalidExportPlan(code string, cause error) error {
	if cause == nil {
		cause = fmt.Errorf("invalid export plan input")
	}
	return &WorkbenchError{Code: code, Category: WorkbenchErrorInputInvalid, cause: cause}
}

func encodedProjection(encoded []byte) (any, error) {
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func validateExportRequirements(requirements ExportPlanRequirements) error {
	if err := canonicaljson.ValidateResolvedExportProfileRequirements(requirements.CanonicalJSON); err != nil {
		return err
	}
	var canonical struct {
		SchemaVersion        int64                `json:"schema_version"`
		ExporterProfile      ExportPlanProfileRef `json:"exporter_profile"`
		SerializerProfile    ExportPlanProfileRef `json:"serializer_profile"`
		RequiredAssetDigests []string             `json:"required_asset_digests"`
		RequiredFontDigests  []string             `json:"required_font_digests"`
	}
	if err := json.Unmarshal(requirements.CanonicalJSON, &canonical); err != nil {
		return err
	}
	if canonical.SchemaVersion != 1 || canonical.ExporterProfile != requirements.ExporterProfile || canonical.SerializerProfile != requirements.SerializerProfile ||
		!equalExportStrings(canonical.RequiredAssetDigests, requirements.RequiredAssetDigests) || !equalExportStrings(canonical.RequiredFontDigests, requirements.RequiredFontDigests) {
		return fmt.Errorf("resolved requirements do not match their canonical bytes")
	}
	if !canonicalExportDigests(requirements.RequiredAssetDigests) || !canonicalExportDigests(requirements.RequiredFontDigests) {
		return fmt.Errorf("resolved requirement digests must be sorted, unique SHA-256 digests")
	}
	return nil
}

func canonicalExportDigests(values []string) bool {
	if !sort.StringsAreSorted(values) {
		return false
	}
	for index, value := range values {
		if !validSemanticHash(value) || index > 0 && values[index-1] == value {
			return false
		}
	}
	return true
}

func equalExportStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func exportHashProjection(value []byte) (any, error) {
	projection, err := encodedProjection(value)
	if err != nil {
		return nil, err
	}
	object, ok := projection.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("ViewData must be an object")
	}
	if diagnostics, ok := object["diagnostics"].([]any); ok {
		stripLocalizedDiagnosticMessages(diagnostics)
	}
	return projection, nil
}

func stripLocalizedDiagnosticMessages(values []any) {
	for _, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			continue
		}
		delete(object, "message")
		if related, ok := object["related"].([]any); ok {
			stripLocalizedDiagnosticMessages(related)
		}
	}
}

func validateExportStateSummary(input ExportPlanInput) (*string, error) {
	wanted := input.Recipe.Options.StateSummary != nil && *input.Recipe.Options.StateSummary
	if wanted != (input.StateSummary != nil) {
		return nil, invalidExportPlan("export.state_summary_invalid", nil)
	}
	if input.StateSummary == nil {
		return nil, nil
	}
	if input.ViewData.RevisionKind != "single" || input.ViewData.DefinitionHash == nil ||
		*input.ViewData.DefinitionHash != input.StateSummary.DefinitionHash {
		return nil, invalidExportPlan("export.state_summary_invalid", nil)
	}
	payloadDigest := sha256.Sum256(input.StateSummary.PayloadCanonicalJSON)
	if string("sha256:"+hex.EncodeToString(payloadDigest[:])) != input.StateSummary.PayloadHash {
		return nil, invalidExportPlan("export.state_summary_invalid", nil)
	}
	projection, err := encodedProjection(input.StateSummary.CanonicalJSON)
	if err != nil {
		return nil, invalidExportPlan("export.state_summary_invalid", err)
	}
	hash, err := materialize.SemanticHash(materialize.DomainExportStateSummary, projection)
	if err != nil {
		return nil, invalidExportPlan("export.state_summary_invalid", err)
	}
	digest := string(hash)
	return &digest, nil
}

func optionalDigestValue(value *string) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

type exportItem struct {
	key    string
	role   string
	source ExportPlanSourceRefs
}

func collectExportItems(root any) ([]exportItem, error) {
	view, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("ViewData projection must be an object")
	}
	rootSource, err := inheritedExportSource(view, ExportPlanSourceRefs{})
	if err != nil {
		return nil, fmt.Errorf("ViewData source: %w", err)
	}
	kind, ok := view["kind"].(string)
	if !ok {
		return nil, fmt.Errorf("ViewData kind is missing")
	}
	shape, ok := view[kind].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("ViewData %s payload is missing", kind)
	}

	collector := exportItemCollector{seen: map[string]bool{}}
	simple := func(field, role string) error {
		return collector.collectArray(shape, field, role, rootSource, nil)
	}
	switch kind {
	case "diagram":
		for _, collection := range [][2]string{{"occurrences", "occurrences"}, {"edges", "edges"}, {"containers", "containers"}, {"overlays", "overlays"}, {"badges", "badges"}, {"support_items", "support_items"}} {
			if err := simple(collection[0], collection[1]); err != nil {
				return nil, err
			}
		}
	case "table":
		for _, collection := range [][2]string{{"columns", "columns"}, {"rows", "rows"}} {
			if err := simple(collection[0], collection[1]); err != nil {
				return nil, err
			}
		}
	case "matrix":
		for _, collection := range [][2]string{{"row_axis", "row_axis"}, {"column_axis", "column_axis"}, {"cells", "cells"}} {
			if err := simple(collection[0], collection[1]); err != nil {
				return nil, err
			}
		}
	case "tree":
		var collectOccurrences exportItemNestedCollector
		collectOccurrences = func(item map[string]any, source ExportPlanSourceRefs) error {
			return collector.collectArray(item, "children", "occurrences", source, collectOccurrences)
		}
		if err := collector.collectArray(shape, "roots", "occurrences", rootSource, collectOccurrences); err != nil {
			return nil, err
		}
		for _, collection := range [][2]string{{"cycle_refs", "cycle_refs"}, {"link_refs", "link_refs"}} {
			if err := simple(collection[0], collection[1]); err != nil {
				return nil, err
			}
		}
	case "flow":
		for _, collection := range [][2]string{{"steps", "steps"}, {"connectors", "connectors"}, {"lanes", "lanes"}, {"cycle_refs", "cycle_refs"}} {
			if err := simple(collection[0], collection[1]); err != nil {
				return nil, err
			}
		}
	case "context":
		if err := collector.collectArray(shape, "groups", "groups", rootSource, func(group map[string]any, source ExportPlanSourceRefs) error {
			if err := collector.collectArray(group, "facts", "facts", source, nil); err != nil {
				return err
			}
			return collector.collectArray(group, "attributes", "attributes", source, nil)
		}); err != nil {
			return nil, err
		}
	case "diff":
		if err := collector.collectArray(shape, "changes", "changes", rootSource, func(change map[string]any, source ExportPlanSourceRefs) error {
			return collector.collectArray(change, "fields", "field_diffs", source, nil)
		}); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported ViewData kind %q", kind)
	}
	sort.Slice(collector.items, func(i, j int) bool { return collector.items[i].key < collector.items[j].key })
	return collector.items, nil
}

type exportItemNestedCollector func(map[string]any, ExportPlanSourceRefs) error

type exportItemCollector struct {
	items []exportItem
	seen  map[string]bool
}

func (c *exportItemCollector) collectArray(owner map[string]any, field, role string, inherited ExportPlanSourceRefs, nested exportItemNestedCollector) error {
	values, ok := owner[field].([]any)
	if !ok {
		return fmt.Errorf("ViewData collection %q is missing", field)
	}
	for index, value := range values {
		item, source, err := c.collectItem(value, role, inherited)
		if err != nil {
			return fmt.Errorf("ViewData collection %q item %d: %w", field, index, err)
		}
		if nested != nil {
			if err := nested(item, source); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *exportItemCollector) collectItem(value any, role string, inherited ExportPlanSourceRefs) (map[string]any, ExportPlanSourceRefs, error) {
	item, ok := value.(map[string]any)
	if !ok {
		return nil, ExportPlanSourceRefs{}, fmt.Errorf("item must be an object")
	}
	key, ok := item["key"].(string)
	if !ok || !strings.HasPrefix(key, "vdi:") {
		return nil, ExportPlanSourceRefs{}, fmt.Errorf("item key is invalid")
	}
	if c.seen[key] {
		return nil, ExportPlanSourceRefs{}, fmt.Errorf("duplicate ViewData item key %q", key)
	}
	source, err := inheritedExportSource(item, inherited)
	if err != nil {
		return nil, ExportPlanSourceRefs{}, fmt.Errorf("item %q source: %w", key, err)
	}
	c.seen[key] = true
	c.items = append(c.items, exportItem{key: key, role: role, source: source})
	return item, source, nil
}

func inheritedExportSource(item map[string]any, inherited ExportPlanSourceRefs) (ExportPlanSourceRefs, error) {
	raw, ok := item["source"]
	if !ok {
		return inherited, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return ExportPlanSourceRefs{}, err
	}
	var source ExportPlanSourceRefs
	if err := json.Unmarshal(encoded, &source); err != nil {
		return ExportPlanSourceRefs{}, err
	}
	return source, nil
}

func collectExportAssetDigests(root any) []string {
	set := map[string]bool{}
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if values, ok := typed["asset_digests"].([]any); ok {
				for _, value := range values {
					if digest, ok := value.(string); ok {
						set[digest] = true
					}
				}
			}
			for _, item := range typed {
				walk(item)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(root)
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	sort.Strings(values)
	out := make([]string, len(values))
	for index, value := range values {
		out[index] = string(value)
	}
	return out
}

func mergeExportDigests(groups ...[]string) []string {
	set := map[string]bool{}
	for _, group := range groups {
		for _, digest := range group {
			set[digest] = true
		}
	}
	result := make([]string, 0, len(set))
	for digest := range set {
		result = append(result, digest)
	}
	sort.Strings(result)
	return result
}

func exportRepresentations(items []exportItem, root ExportPlanSourceRefs, recipe ExportPlanRecipe, units []ExportPlanUnit) []ExportRepresentation {
	result := make([]ExportRepresentation, 0, len(items)+1)
	unitsByRole := map[string]int{}
	for index := range units {
		unitsByRole[units[index].Role] = index
	}
	bind := func(index int, key string) (artifactRole, unitID, locator string) {
		units[index].ViewdataKeys = append(units[index].ViewdataKeys, key)
		return units[index].ArtifactRole, units[index].UnitID, units[index].UnitID + ":" + key
	}
	if recipe.Fidelity == "lossless" {
		role, unitID, locator := bind(0, "viewdata-root")
		result = append(result, ExportRepresentation{ArtifactRole: &role, Disposition: "embedded", Locator: &locator, Source: root, UnitID: &unitID, ViewdataKey: "viewdata-root"})
	}
	roleAware := recipe.Format == "xlsx" || recipe.Format == "pdf" || recipe.Format == "docx" || recipe.Format == "pptx" ||
		((recipe.Format == "csv" || recipe.Format == "tsv") && recipe.Options.Bundle != nil && *recipe.Options.Bundle)
	singleTable := (recipe.Format == "csv" || recipe.Format == "tsv") && !roleAware
	for _, item := range items {
		unitIndex := 0
		if roleAware || singleTable {
			matched, ok := unitsByRole[item.role]
			if !ok {
				reason := "lossy_format"
				result = append(result, ExportRepresentation{Disposition: "omitted", OmissionReason: &reason, Source: item.source, ViewdataKey: item.key})
				continue
			}
			unitIndex = matched
		}
		role, unitID, locator := bind(unitIndex, item.key)
		disposition := "rendered"
		if recipe.Format == "json" || recipe.Format == "yaml" {
			disposition = "embedded"
		} else if recipe.Format == "csv" || recipe.Format == "tsv" || recipe.Format == "xlsx" || recipe.Format == "markdown" {
			disposition = "tabular"
		}
		result = append(result, ExportRepresentation{ArtifactRole: &role, Disposition: disposition, Locator: &locator, Source: item.source, UnitID: &unitID, ViewdataKey: item.key})
	}
	if len(result) == 0 {
		role, unitID, locator := bind(0, "viewdata-root")
		result = append(result, ExportRepresentation{ArtifactRole: &role, Disposition: "rendered", Locator: &locator, Source: root, UnitID: &unitID, ViewdataKey: "viewdata-root"})
	}
	for index := range units {
		sort.Strings(units[index].ViewdataKeys)
		units[index].ViewdataKeys = uniqueExportStrings(units[index].ViewdataKeys)
	}
	return result
}

func exportArtifacts(shape string, recipe ExportPlanRecipe) []ExportArtifactEntry {
	roles := []string{primaryArtifactRole(shape, string(recipe.Format))}
	if (recipe.Format == "csv" || recipe.Format == "tsv") && recipe.Options.Bundle != nil && *recipe.Options.Bundle {
		roles = exportShapeRoles(shape)
	} else {
		switch recipe.Format {
		case "xlsx":
			roles = []string{"workbook"}
		case "pdf", "docx":
			roles = []string{"document"}
		case "pptx":
			roles = []string{"presentation"}
		}
	}
	artifacts := make([]ExportArtifactEntry, 0, len(roles))
	stem := strings.TrimSuffix(recipe.Filename, recipe.Extension)
	for index, role := range roles {
		path := recipe.Filename
		if index != 0 {
			path = stem + "." + role + recipe.Extension
		}
		artifacts = append(artifacts, ExportArtifactEntry{LogicalPath: path, MediaType: exportMediaType(string(recipe.Format)), Primary: index == 0, Role: role})
	}
	sort.SliceStable(artifacts, func(i, j int) bool {
		if artifacts[i].Primary != artifacts[j].Primary {
			return artifacts[i].Primary
		}
		if artifacts[i].Role != artifacts[j].Role {
			return artifacts[i].Role < artifacts[j].Role
		}
		return artifacts[i].LogicalPath < artifacts[j].LogicalPath
	})
	return artifacts
}

func exportPlanUnits(shape string, recipe ExportPlanRecipe, artifacts []ExportArtifactEntry) []ExportPlanUnit {
	kind := "section"
	switch recipe.Format {
	case "xlsx":
		kind = "sheet"
	case "pdf", "docx":
		kind = "page"
	case "pptx":
		kind = "slide"
	}
	type unitRole struct{ artifact, role string }
	planned := []unitRole{}
	shapeRoles := exportShapeRoles(shape)
	bundledTable := (recipe.Format == "csv" || recipe.Format == "tsv") && recipe.Options.Bundle != nil && *recipe.Options.Bundle
	switch {
	case bundledTable:
		for _, artifact := range artifacts {
			planned = append(planned, unitRole{artifact: artifact.Role, role: artifact.Role})
		}
	case recipe.Format == "xlsx" || recipe.Format == "pdf" || recipe.Format == "docx" || recipe.Format == "pptx":
		for _, role := range shapeRoles {
			planned = append(planned, unitRole{artifact: artifacts[0].Role, role: role})
		}
	case recipe.Format == "csv" || recipe.Format == "tsv":
		planned = append(planned, unitRole{artifact: artifacts[0].Role, role: artifacts[0].Role})
	default:
		planned = append(planned, unitRole{artifact: artifacts[0].Role, role: artifacts[0].Role})
	}
	if len(planned) == 0 {
		planned = append(planned, unitRole{artifact: artifacts[0].Role, role: artifacts[0].Role})
	}
	units := make([]ExportPlanUnit, 0, len(planned))
	for index, plannedUnit := range planned {
		units = append(units, ExportPlanUnit{ArtifactRole: plannedUnit.artifact, Kind: kind, Order: fmt.Sprintf("%d", index), Role: plannedUnit.role, UnitID: "unit:" + plannedUnit.role, ViewdataKeys: []string{}})
	}
	return units
}

func validateExportTopology(artifacts []ExportArtifactEntry, units []ExportPlanUnit, representations []ExportRepresentation) error {
	artifactRoles := map[string]bool{}
	primary := 0
	for _, artifact := range artifacts {
		if artifactRoles[artifact.Role] {
			return fmt.Errorf("duplicate artifact role %q", artifact.Role)
		}
		artifactRoles[artifact.Role] = true
		if artifact.Primary {
			primary++
		}
	}
	if primary != 1 {
		return fmt.Errorf("export plan must contain exactly one primary artifact")
	}
	unitsByID := map[string]ExportPlanUnit{}
	for _, unit := range units {
		if !artifactRoles[unit.ArtifactRole] || unit.UnitID == "" || unitsByID[unit.UnitID].UnitID != "" {
			return fmt.Errorf("unit %q has invalid artifact topology", unit.UnitID)
		}
		unitsByID[unit.UnitID] = unit
	}
	for _, representation := range representations {
		if representation.Disposition == "omitted" {
			if representation.ArtifactRole != nil || representation.UnitID != nil || representation.Locator != nil || representation.OmissionReason == nil {
				return fmt.Errorf("omitted representation %q is not closed", representation.ViewdataKey)
			}
			continue
		}
		if representation.ArtifactRole == nil || representation.UnitID == nil || representation.Locator == nil || representation.OmissionReason != nil {
			return fmt.Errorf("representation %q has incomplete topology", representation.ViewdataKey)
		}
		unit, ok := unitsByID[*representation.UnitID]
		if !ok || unit.ArtifactRole != *representation.ArtifactRole || !containsExportString(unit.ViewdataKeys, representation.ViewdataKey) {
			return fmt.Errorf("representation %q points outside its artifact unit", representation.ViewdataKey)
		}
	}
	return nil
}

func uniqueExportStrings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func containsExportString(values []string, wanted string) bool {
	index := sort.SearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}

func exportPagination(recipe ExportPlanRecipe) ExportPagination {
	result := ExportPagination{Kind: "none"}
	switch recipe.Format {
	case "xlsx":
		result.Kind = "sheets"
	case "pdf", "docx":
		result.Kind, result.Orientation, result.PageSize = "pages", recipe.Options.Orientation, recipe.Options.PageSize
	case "pptx":
		result.Kind, result.Orientation, result.PageSize = "slides", recipe.Options.Orientation, recipe.Options.PageSize
	}
	return result
}

func exportRequiresRenderer(format string) bool {
	switch format {
	case "svg", "png", "pdf", "pptx", "docx", "drawio":
		return true
	}
	return false
}

func exportLayoutRequirement(required bool) string {
	if required {
		return "presentation_geometry"
	}
	return "none"
}

func primaryArtifactRole(shape, format string) string {
	if format == "json" || format == "yaml" {
		return "viewdata"
	}
	roles := exportShapeRoles(shape)
	if len(roles) != 0 {
		return roles[0]
	}
	return "primary"
}

func exportShapeRoles(shape string) []string {
	switch shape {
	case "diagram":
		return []string{"occurrences", "edges", "containers", "overlays", "badges", "support_items"}
	case "table":
		return []string{"rows", "columns"}
	case "matrix":
		return []string{"cells", "row_axis", "column_axis"}
	case "tree":
		return []string{"occurrences", "cycle_refs", "link_refs"}
	case "flow":
		return []string{"steps", "connectors", "lanes", "cycle_refs"}
	case "context":
		return []string{"facts", "attributes", "groups"}
	case "diff":
		return []string{"changes", "field_diffs"}
	default:
		return nil
	}
}

func canonicalArtifactRole(role string) string {
	switch role {
	case "row_axis":
		return "row_axis"
	case "column_axis":
		return "column_axis"
	case "support_items":
		return "support_items"
	case "cycle_refs":
		return "cycle_refs"
	case "link_refs":
		return "link_refs"
	}
	return strings.TrimSuffix(role, "_items")
}

func exportMediaType(format string) string {
	values := map[string]string{"json": "application/json", "yaml": "application/yaml", "svg": "image/svg+xml", "png": "image/png", "pdf": "application/pdf", "html": "text/html", "csv": "text/csv", "tsv": "text/tab-separated-values", "xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "markdown": "text/markdown", "pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation", "docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "mermaid": "text/vnd.mermaid", "bpmn": "application/xml", "drawio": "application/vnd.jgraph.mxfile"}
	return values[format]
}
