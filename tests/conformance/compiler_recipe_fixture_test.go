// SPDX-License-Identifier: Apache-2.0

package conformance_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine"
)

// TestCanonicalRecipeFixturesMatchCompilerOutput keeps the portable recipe
// corpus subordinate to the Go compiler. A schema-valid value is not
// canonical unless this test can obtain the same value from a successful
// compiler generation.
func TestCanonicalRecipeFixturesMatchCompilerOutput(t *testing.T) {
	t.Parallel()
	result, err := engine.New(engine.BuildInfo{}).Compile(context.Background(), engine.CompileInput{
		Mode: engine.CompileProject, EntryPath: "document.ldl",
		ProjectSourceTree:    map[string][]byte{"document.ldl": []byte(canonicalRecipeAuthoritySource)},
		ResolvedDependencies: conformanceEmptyResolved(),
	})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("authority compiler generation failed: err=%v diagnostics=%+v", err, result.Diagnostics)
	}
	snapshot := result.Snapshot()

	query := compilerQueryRecipeValue(snapshot.CompiledQueryRecipes[0])
	overview := compilerViewRecipeValue(findCompilerView(t, snapshot.CompiledViewRecipes, "v"))
	exports := make(map[string]any, len(snapshot.CompiledExportRecipes))
	for _, recipe := range snapshot.CompiledExportRecipes {
		exports[fmt.Sprint(recipe.Format)] = compilerExportRecipeValue(recipe)
	}
	queryCorpus := readValueCorpus(t, "query-authority-v1.json")
	assertCompilerFixtureValue(t, queryCorpus, "query_recipe_minimal", query)
	viewCorpus := readValueCorpus(t, "view-export-semantics-v1.json")
	assertCompilerFixtureValue(t, viewCorpus, "owned_export_recipe", exports["json"])
	assertCompilerFixtureValue(t, viewCorpus, "complete_owned_view_graph", overview)
	for format, value := range exports {
		if format == "json" {
			continue
		}
		assertCompilerFixtureValue(t, viewCorpus, "contract_export_"+format, value)
	}

	shared := readValueCorpus(t, "v1.json")
	assertCompilerFixtureValue(t, shared, "typed_query_recipe_document", map[string]any{"format": "layerdraw-query-recipe", "recipe": query, "schema_version": 1})
	assertCompilerFixtureValue(t, shared, "typed_view_recipe_document", map[string]any{"format": "layerdraw-view-recipe", "recipe": overview, "schema_version": 1})
	assertCompilerFixtureValue(t, shared, "typed_export_recipe_document", map[string]any{"format": "layerdraw-export-recipe", "recipe": exports["json"], "schema_version": 1})
}

type valueCorpus struct {
	Canonical []struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
		Input string          `json:"input"`
	} `json:"canonical_cases"`
}

func readValueCorpus(t *testing.T, name string) valueCorpus {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(protocolRepositoryRoot(t), "schemas", "fixtures", "conformance", name))
	if err != nil {
		t.Fatal(err)
	}
	var corpus valueCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	return corpus
}

func assertCompilerFixtureValue(t *testing.T, corpus valueCorpus, name string, actual any) {
	t.Helper()
	for _, vector := range corpus.Canonical {
		if vector.Name != name {
			continue
		}
		raw := vector.Value
		if len(raw) == 0 && vector.Input != "" {
			raw = json.RawMessage(vector.Input)
		}
		var want, got any
		encoded, err := json.Marshal(actual)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encoded, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s is not actual compiler output\nwant=%s\ngot=%s", name, raw, encoded)
		}
		return
	}
	t.Fatalf("missing compiler-authority canonical case %q", name)
}

func findCompilerView(t *testing.T, recipes []engine.CompiledViewRecipe, id string) engine.CompiledViewRecipe {
	t.Helper()
	for _, recipe := range recipes {
		if recipe.ID == id {
			return recipe
		}
	}
	t.Fatalf("compiler did not produce View %q", id)
	return engine.CompiledViewRecipe{}
}

func compilerQueryRecipeValue(recipe engine.CompiledQueryRecipe) map[string]any {
	value := map[string]any{
		"address": recipe.Address, "annotations": recipe.Annotations,
		"dependencies": map[string]any{
			"column_addresses": recipe.Dependencies.ColumnAddresses, "entity_addresses": recipe.Dependencies.EntityAddresses,
			"entity_type_addresses": recipe.Dependencies.EntityTypeAddresses, "layer_addresses": recipe.Dependencies.LayerAddresses,
			"parameter_addresses": recipe.Dependencies.ParameterAddresses, "relation_addresses": recipe.Dependencies.RelationAddresses,
			"relation_type_addresses": recipe.Dependencies.RelationTypeAddresses, "state_reads": []any{},
		},
		"display_name": recipe.DisplayName, "id": recipe.ID, "parameters": []any{},
		"relation_where":         map[string]any{"kind": "all", "children": []any{}},
		"reserved_parameter_ids": append([]string{}, recipe.ReservedParameterIDs...), "result": []any{}, "select": map[string]any{},
		"state_input": fmt.Sprint(recipe.StateInput), "tags": recipe.Tags,
		"where": map[string]any{"kind": "all", "children": []any{}},
	}
	if recipe.Description != nil {
		value["description"] = *recipe.Description
	}
	return value
}

func compilerViewRecipeValue(recipe engine.CompiledViewRecipe) map[string]any {
	exports := make([]any, len(recipe.Exports))
	for index, item := range recipe.Exports {
		exports[index] = compilerExportRecipeValue(item)
	}
	arguments := map[string]any{}
	for _, argument := range recipe.Source.Query.Arguments {
		arguments[argument.ParameterAddress] = nil
	}
	contextShape := recipe.Shape.Context
	value := map[string]any{
		"address": recipe.Address, "annotations": recipe.Annotations, "category": fmt.Sprint(recipe.Category),
		"dependencies": map[string]any{
			"column_addresses": recipe.Dependencies.ColumnAddresses, "entity_addresses": recipe.Dependencies.EntityAddresses,
			"entity_type_addresses": recipe.Dependencies.EntityTypeAddresses, "export_addresses": recipe.Dependencies.ExportAddresses,
			"layer_addresses": recipe.Dependencies.LayerAddresses, "parameter_addresses": recipe.Dependencies.ParameterAddresses,
			"query_addresses": recipe.Dependencies.QueryAddresses, "relation_addresses": recipe.Dependencies.RelationAddresses,
			"relation_type_addresses": recipe.Dependencies.RelationTypeAddresses, "state_reads": []any{},
		},
		"display_name": recipe.DisplayName, "exports": exports, "id": recipe.ID,
		"relation_projection_overrides": map[string]any{}, "reserved_export_ids": append([]string{}, recipe.ReservedExportIDs...),
		"reserved_table_column_ids": append([]string{}, recipe.ReservedTableColumnIDs...),
		"shape": map[string]any{"kind": "context", "context": map[string]any{
			"group_by": fmt.Sprint(contextShape.GroupBy), "include_entity_rows": contextShape.IncludeEntityRows,
			"include_relation_rows": contextShape.IncludeRelationRows, "incoming": contextShape.Incoming, "outgoing": contextShape.Outgoing,
		}},
		"source":      map[string]any{"arguments": arguments, "kind": "query", "query_address": recipe.Source.Query.QueryAddress},
		"state_input": fmt.Sprint(recipe.StateInput), "state_requirement": fmt.Sprint(recipe.StateRequirement), "tags": recipe.Tags,
	}
	if recipe.Description != nil {
		value["description"] = *recipe.Description
	}
	if recipe.Intent != nil {
		value["intent"] = *recipe.Intent
	}
	return value
}

func compilerExportRecipeValue(recipe engine.CompiledExportRecipe) map[string]any {
	value := map[string]any{
		"address": recipe.Address, "effective_maximum_fidelity": fmt.Sprint(recipe.EffectiveMaximumFidelity),
		"exporter_profile": map[string]any{
			"format": fmt.Sprint(recipe.ExporterProfile.Format), "id": recipe.ExporterProfile.ID,
			"registry_digest": recipe.ExporterProfile.RegistryDigest, "registry_schema_version": recipe.ExporterProfile.RegistrySchemaVersion,
			"specification_digest": recipe.ExporterProfile.SpecificationDigest,
		},
		"extension": recipe.Extension, "fidelity": fmt.Sprint(recipe.Fidelity), "fidelity_basis": fmt.Sprint(recipe.FidelityBasis),
		"filename": recipe.Filename, "format": fmt.Sprint(recipe.Format), "id": recipe.ID,
		"native_maximum_fidelity": fmt.Sprint(recipe.NativeMaximumFidelity), "options": compilerExportOptionsValue(recipe),
		"requires_source_manifest": recipe.RequiresSourceManifest, "source_refs": recipe.SourceRefs, "view_address": recipe.ViewAddress,
	}
	return value
}

func compilerExportOptionsValue(recipe engine.CompiledExportRecipe) map[string]any {
	options := map[string]any{"kind": fmt.Sprint(recipe.Options.Kind)}
	if value := recipe.Options.Structured; value != nil {
		options["diagnostics"], options["state_summary"] = value.Diagnostics, value.StateSummary
	}
	if value := recipe.Options.Image; value != nil {
		options["width"], options["height"] = compilerDimensionValue(value.Width), compilerDimensionValue(value.Height)
		options["scale"], options["background"] = canonicalFloat(value.Scale), value.Background
	}
	if value := recipe.Options.Page; value != nil {
		options["page_size"], options["orientation"], options["fit"], options["legend"] = fmt.Sprint(value.PageSize), fmt.Sprint(value.Orientation), fmt.Sprint(value.Fit), value.Legend
	}
	if value := recipe.Options.HTML; value != nil {
		options["interactive"], options["embed_assets"] = value.Interactive, value.EmbedAssets
	}
	if value := recipe.Options.Delimited; value != nil {
		options["bundle"], options["header"], options["source_manifest"] = value.Bundle, value.Header, value.SourceManifest
	}
	if value := recipe.Options.XLSX; value != nil {
		options["profile"], options["lookup_sheets"], options["hidden_ids"] = fmt.Sprint(value.Profile), value.LookupSheets, value.HiddenIDs
		options["formulas"], options["view_data_json"] = value.Formulas, value.ViewDataJSON
	}
	if value := recipe.Options.Manifest; value != nil {
		options["source_manifest"] = value.SourceManifest
	}
	return options
}

func compilerDimensionValue(value struct {
	Auto  bool
	Value int64
}) map[string]any {
	if value.Auto {
		return map[string]any{"kind": "auto"}
	}
	return map[string]any{"kind": "value", "value": strconv.FormatInt(value.Value, 10)}
}

func canonicalFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

const canonicalRecipeAuthoritySource = `project p "Project" {}

entity_type service "Service" {
  representation shape rect
}

relation_type link "Link" dependency {
  from source types [service]
  to target types [service]
  label "links"
  reverse "is linked by"
  projection tree {
    parent_endpoint from
    child_endpoint to
  }
  projection flow {
    source_endpoint from
    target_endpoint to
    connector_kind data
  }
}

query all "All" {
  select {}
}

view v "V" context {
  source query all {}
  context {}
  export json json "v.json" {
    fidelity lossless
    source_refs
  }
}

view diagram "Diagram" topology {
  source query all {}
  diagram {
    composed
  }
  export yaml yaml "diagram.yaml" {
    fidelity lossless
    source_refs
  }
  export svg svg "diagram.svg" {
    fidelity visual_only
  }
  export png png "diagram.png" {
    fidelity visual_only
  }
  export pdf pdf "diagram.pdf" {
    fidelity visual_only
  }
  export html html "diagram.html" {
    fidelity traceable_summary
    source_refs
  }
  export csv csv "diagram.csv" {
    fidelity traceable_summary
    source_refs
    bundle
    header
    source_manifest
  }
  export tsv tsv "diagram.tsv" {
    fidelity lossy
  }
  export xlsx xlsx "diagram.xlsx" {
    fidelity lossless
    source_refs
    hidden_ids
    view_data_json
  }
  export pptx pptx "diagram.pptx" {
    fidelity visual_only
  }
  export docx docx "diagram.docx" {
    fidelity visual_only
  }
  export drawio drawio "diagram.drawio" {
    fidelity visual_only
    source_refs
    source_manifest
  }
}

view table "Table" inventory {
  source query all {}
  table {}
  export markdown markdown "table.md" {
    fidelity lossy
  }
}

view tree "Tree" hierarchy {
  source query all {}
  tree {
    relation_types [link]
  }
  export mermaid mermaid "tree.mmd" {
    fidelity traceable_summary
    source_refs
    source_manifest
  }
}

view flow "Flow" flow {
  source query all {}
  flow {
    relation_types [link]
  }
  export bpmn bpmn "flow.bpmn" {
    fidelity lossy
    source_manifest
  }
}
`
