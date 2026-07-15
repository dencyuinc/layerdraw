// SPDX-License-Identifier: Apache-2.0

package conformance_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

func TestSemanticAuthorityReviewRegressions(t *testing.T) {
	parameter := func(format, value string) []byte {
		return mustJSON(t, map[string]any{
			"id": "x", "address": "ldl:project:p:query:q:parameter:x", "value_type": "string",
			"reserved_enum_values": []any{}, "required": false, "format": format,
			"default": map[string]any{"kind": "string", "string_value": value},
		})
	}
	for _, test := range []struct {
		name, format, value string
		valid               bool
	}{
		{"dotted email", "email", "first.last@example.com", true},
		{"uppercase email domain", "email", "first.last@EXAMPLE.COM", true},
		{"URI space", "uri", "http://exa mple.com", false},
		{"IPv6 nine fields", "ipv6", "1:2:3:4:5:6:7:8:9", false},
		{"IPv6 multiple elisions", "ipv6", "1::2::3", false},
		{"mapped IPv6 canonical", "ipv6", "::ffff:192.0.2.1", true},
		{"high-bit IPv4 network", "cidr", "192.0.2.0/24", true},
		{"CIDR host bits", "cidr", "192.0.2.1/24", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := semantic.DecodeQueryRecipeParameter(parameter(test.format, test.value))
			if test.valid && err != nil {
				t.Fatalf("compiler-valid format rejected: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("compiler-invalid format accepted")
			}
		})
	}
	if _, err := semantic.DecodeRecipeScalar([]byte(`{"kind":"datetime","string_value":"2026-07-15T12:34:56.120Z"}`)); err == nil {
		t.Fatal("non-canonical datetime fraction accepted")
	}

	reversedMembership := []byte(`{"kind":"field","field":"id","operand_type":{"kind":"scalar","scalar_type":"string"},"operator":"in","value":{"kind":"scalar_set","scalar_values":[{"kind":"string","string_value":"z"},{"kind":"string","string_value":"a"}]}}`)
	if _, err := semantic.DecodeRecipePredicate(reversedMembership); err != nil {
		t.Fatalf("authored scalar membership order rejected: %v", err)
	}

	query := corpusValue(t, "query-authority-v1.json", "query_recipe_minimal")
	query["where"] = map[string]any{"kind": "field", "field": "from", "operator": "exists", "operand_type": map[string]any{"kind": "address", "address_kind": "entity"}}
	if _, err := semantic.DecodeQueryRecipe(mustJSON(t, query)); err == nil {
		t.Fatal("Entity predicate accepted relation-only from field")
	}
	query = corpusValue(t, "query-authority-v1.json", "query_recipe_minimal")
	query["relation_where"] = map[string]any{"kind": "field", "field": "layer", "operator": "exists", "operand_type": map[string]any{"kind": "address", "address_kind": "layer"}}
	if _, err := semantic.DecodeQueryRecipe(mustJSON(t, query)); err == nil {
		t.Fatal("Relation predicate accepted entity-only layer field")
	}

	view := corpusValue(t, "view-export-semantics-v1.json", "complete_owned_view_graph")
	view["dependencies"].(map[string]any)["query_addresses"] = []any{}
	if _, err := semantic.DecodeViewRecipe(mustJSON(t, view)); err == nil {
		t.Fatal("View accepted an omitted source Query dependency")
	}
	view = corpusValue(t, "view-export-semantics-v1.json", "complete_owned_view_graph")
	view["dependencies"].(map[string]any)["export_addresses"] = []any{}
	if _, err := semantic.DecodeViewRecipe(mustJSON(t, view)); err == nil {
		t.Fatal("View accepted an omitted nested Export dependency")
	}

	view = corpusValue(t, "view-export-semantics-v1.json", "complete_owned_view_graph")
	baseExport := view["exports"].([]any)[0].(map[string]any)
	renameExport := func(id string) map[string]any {
		clone := cloneObject(t, baseExport)
		clone["id"] = id
		clone["address"] = "ldl:project:p:view:v:export:" + id
		clone["filename"] = id + ".json"
		return clone
	}
	zebra, alpha := renameExport("zebra"), renameExport("alpha")
	view["exports"] = []any{zebra, alpha}
	view["dependencies"].(map[string]any)["export_addresses"] = []any{zebra["address"], alpha["address"]}
	if _, err := semantic.DecodeViewRecipe(mustJSON(t, view)); err != nil {
		t.Fatalf("compiler-preserved authored Export order rejected: %v", err)
	}

	view = corpusValue(t, "view-export-semantics-v1.json", "complete_owned_view_graph")
	parameterAddress := "ldl:project:p:query:all:parameter:x"
	view["source"].(map[string]any)["arguments"] = map[string]any{
		parameterAddress: map[string]any{"kind": "string", "string_value": "ldl:project:p:entity:not-a-dependency"},
	}
	view["dependencies"].(map[string]any)["parameter_addresses"] = []any{parameterAddress}
	if _, err := semantic.DecodeViewRecipe(mustJSON(t, view)); err != nil {
		t.Fatalf("address-looking scalar argument was treated as a dependency: %v", err)
	}

	diff := corpusValue(t, "view-export-semantics-v1.json", "complete_owned_view_graph")
	diff["category"] = "diff"
	diff["source"] = map[string]any{"kind": "diff", "before": "before", "after": "after", "arguments": map[string]any{}}
	diff["shape"] = map[string]any{"kind": "diff", "diff": map[string]any{"include": []any{}, "detect_moves": false}}
	diff["exports"] = []any{}
	diffDependencies := diff["dependencies"].(map[string]any)
	diffDependencies["query_addresses"] = []any{}
	diffDependencies["export_addresses"] = []any{}
	if _, err := semantic.DecodeViewRecipe(mustJSON(t, diff)); err != nil {
		t.Fatalf("complete no-query Diff rejected: %v", err)
	}
	diffDependencies["entity_addresses"] = []any{"ldl:project:p:entity:extra"}
	if _, err := semantic.DecodeViewRecipe(mustJSON(t, diff)); err == nil {
		t.Fatal("no-query Diff accepted a non-derivable Entity dependency")
	}
	diffDependencies["entity_addresses"] = []any{}
	diffDependencies["state_reads"] = []any{map[string]any{"subject_kind": "entity", "field_path": "system.created_at", "value_type": "datetime"}}
	if _, err := semantic.DecodeViewRecipe(mustJSON(t, diff)); err == nil {
		t.Fatal("no-query Diff accepted a non-derivable state read")
	}

	view = corpusValue(t, "view-export-semantics-v1.json", "complete_owned_view_graph")
	relationTypeAddress := "ldl:project:p:relation-type:r"
	branchColumnAddress := relationTypeAddress + ":column:branch"
	view["relation_projection_overrides"] = map[string]any{relationTypeAddress: map[string]any{"flow": map[string]any{
		"source_endpoint": "from", "target_endpoint": "to", "connector_kind": "control", "branch_value_column_address": branchColumnAddress,
	}}}
	viewDependencies := view["dependencies"].(map[string]any)
	viewDependencies["relation_type_addresses"] = []any{relationTypeAddress}
	viewDependencies["column_addresses"] = []any{branchColumnAddress}
	if _, err := semantic.DecodeViewRecipe(mustJSON(t, view)); err != nil {
		t.Fatalf("Flow branch Column dependency rejected: %v", err)
	}
	viewDependencies["column_addresses"] = []any{}
	if _, err := semantic.DecodeViewRecipe(mustJSON(t, view)); err == nil {
		t.Fatal("View accepted an omitted Flow branch Column dependency")
	}

	automatic := corpusValue(t, "semantic-root-authority-v1.json", "owned_table_columns_disjoint_from_reservations")
	automatic["relation_projection_overrides"] = map[string]any{"ldl:project:p:relation-type:r": map[string]any{"table": map[string]any{"row_mode": "automatic", "include_from": true, "include_to": true, "include_relation_type": true}}}
	automatic["dependencies"].(map[string]any)["relation_type_addresses"] = []any{"ldl:project:p:relation-type:r"}
	table := automatic["shape"].(map[string]any)["table"].(map[string]any)
	table["columns"] = []any{}
	table["include_entity_id"] = false
	table["include_type"] = false
	table["include_layer"] = false
	table["row_source"] = "automatic_relations"
	table["automatic_relation_columns"] = []any{"from", "relation_type", "to"}
	table["sorts"] = []any{map[string]any{"column_id": "from", "direction": "ascending", "absent": "last"}}
	if _, err := semantic.DecodeViewRecipe(mustJSON(t, automatic)); err != nil {
		t.Fatalf("automatic Relation fixed-column sort rejected: %v", err)
	}
	withoutFrom := cloneObject(t, automatic)
	withoutFrom["relation_projection_overrides"] = map[string]any{"ldl:project:p:relation-type:r": map[string]any{"table": map[string]any{"row_mode": "automatic", "include_from": false, "include_to": false, "include_relation_type": false}}}
	withoutFrom["shape"].(map[string]any)["table"].(map[string]any)["automatic_relation_columns"] = []any{}
	if _, err := semantic.DecodeViewRecipe(mustJSON(t, withoutFrom)); err == nil {
		t.Fatal("sort accepted an automatic Relation column omitted by the effective projection")
	}

	export := corpusValue(t, "view-export-semantics-v1.json", "contract_export_svg")
	export["source_refs"] = true
	export["requires_source_manifest"] = false
	if _, err := semantic.DecodeExportRecipe(mustJSON(t, export)); err == nil {
		t.Fatal("non-embedded source refs accepted without a required manifest")
	}

	childSet := map[string]any{"owner_address": "ldl:project:p:entity:e", "child_kind": "query_parameter", "child_addresses": []any{}, "hash": digest("a")}
	if _, err := semantic.DecodeChildSetHash(mustJSON(t, childSet)); err == nil {
		t.Fatal("impossible empty ChildSet owner-kind pair accepted")
	}
	binding := map[string]any{
		"source_address": "ldl:project:p:view:v", "target_address": "ldl:project:p:query:q:parameter:x", "target_kind": "query_parameter", "via": "argument",
		"module": module("document.ldl"), "range": sourceRange("document.ldl", "0", "1"),
	}
	if _, err := semantic.DecodeSourceBindingRecord(mustJSON(t, binding)); err == nil {
		t.Fatal("child SourceBinding accepted without exact target owner")
	}
}

func TestCompilerCompoundCollectionOrderRegressions(t *testing.T) {
	payload := compileResultPayload(t)
	childSets := []any{
		map[string]any{"owner_address": "ldl:project:p", "child_kind": "entity_type", "child_addresses": []any{}, "hash": digest("a")},
		map[string]any{"owner_address": "ldl:project:p", "child_kind": "relation_type", "child_addresses": []any{}, "hash": digest("b")},
	}
	payload["child_set_hashes"] = childSets
	if _, err := engineprotocol.DecodeCompileResult(mustJSON(t, payload)); err != nil {
		t.Fatalf("canonical ChildSet order rejected: %v", err)
	}
	payload["child_set_hashes"] = []any{childSets[1], childSets[0]}
	if _, err := engineprotocol.DecodeCompileResult(mustJSON(t, payload)); err == nil {
		t.Fatal("reversed ChildSet compound order accepted")
	}

	semanticIndex := compileResultPayload(t)["semantic_index"].(map[string]any)
	references := []any{
		semanticReference("ldl:project:p:entity:a", "ldl:project:p:entity:b", "0"),
		semanticReference("ldl:project:p:entity:b", "ldl:project:p:entity:a", "0"),
	}
	semanticIndex["references"] = references
	if _, err := semantic.DecodeSemanticIndex(mustJSON(t, semanticIndex)); err != nil {
		t.Fatalf("canonical SemanticReference order rejected: %v", err)
	}
	semanticIndex["references"] = []any{references[1], references[0]}
	if _, err := semantic.DecodeSemanticIndex(mustJSON(t, semanticIndex)); err == nil {
		t.Fatal("reversed SemanticReference order accepted")
	}

	semanticIndex = compileResultPayload(t)["semantic_index"].(map[string]any)
	semanticIndex["reference_ids"] = []any{map[string]any{"id": "a", "addresses": []any{}}, map[string]any{"id": "z", "addresses": []any{}}}
	if _, err := semantic.DecodeSemanticIndex(mustJSON(t, semanticIndex)); err != nil {
		t.Fatalf("canonical Reference ID order rejected: %v", err)
	}
	semanticIndex["reference_ids"] = []any{map[string]any{"id": "z", "addresses": []any{}}, map[string]any{"id": "a", "addresses": []any{}}}
	if _, err := semantic.DecodeSemanticIndex(mustJSON(t, semanticIndex)); err == nil {
		t.Fatal("reversed Reference ID order accepted")
	}

	semanticIndex = compileResultPayload(t)["semantic_index"].(map[string]any)
	scoped := semanticIndex["scoped_reads"].(map[string]any)
	scoped["by_kind"] = []any{map[string]any{"kind": "entity", "addresses": []any{}}, map[string]any{"kind": "relation", "addresses": []any{}}}
	scoped["by_module"] = []any{map[string]any{"module": module("a.ldl"), "addresses": []any{}}, map[string]any{"module": module("z.ldl"), "addresses": []any{}}}
	if _, err := semantic.DecodeSemanticIndex(mustJSON(t, semanticIndex)); err != nil {
		t.Fatalf("canonical scoped-read order rejected: %v", err)
	}
	scoped["by_kind"] = []any{map[string]any{"kind": "relation", "addresses": []any{}}, map[string]any{"kind": "entity", "addresses": []any{}}}
	if _, err := semantic.DecodeSemanticIndex(mustJSON(t, semanticIndex)); err == nil {
		t.Fatal("reversed subject-kind scope order accepted")
	}
	scoped["by_kind"] = []any{}
	scoped["by_module"] = []any{map[string]any{"module": module("z.ldl"), "addresses": []any{}}, map[string]any{"module": module("a.ldl"), "addresses": []any{}}}
	if _, err := semantic.DecodeSemanticIndex(mustJSON(t, semanticIndex)); err == nil {
		t.Fatal("reversed module scope order accepted")
	}

	testSourceMapCollectionOrders(t)
}

func testSourceMapCollectionOrders(t *testing.T) {
	base := compileResultPayload(t)["source_map"].(map[string]any)
	ordered := func(property string, values []any) {
		t.Helper()
		value := cloneObject(t, base)
		value[property] = values
		if _, err := semantic.DecodeSourceMap(mustJSON(t, value)); err != nil {
			t.Fatalf("canonical %s order rejected: %v", property, err)
		}
		value[property] = []any{values[1], values[0]}
		if _, err := semantic.DecodeSourceMap(mustJSON(t, value)); err == nil {
			t.Fatalf("reversed %s compound order accepted", property)
		}
	}
	ordered("files", []any{sourceFile("a.ldl"), sourceFile("z.ldl")})
	ordered("assets", []any{sourceAsset("a"), sourceAsset("z")})
	ordered("bindings", []any{sourceBinding("a"), sourceBinding("z")})
	ordered("exports", []any{exportBinding("a"), exportBinding("z")})
}

func corpusValue(t *testing.T, filename, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(protocolRepositoryRoot(t), "schemas", "fixtures", "conformance", filename))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Canonical []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		} `json:"canonical_cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, item := range corpus.Canonical {
		if item.Name == name {
			var value map[string]any
			if err := json.Unmarshal(item.Value, &value); err != nil {
				t.Fatal(err)
			}
			return value
		}
	}
	t.Fatalf("missing canonical case %s in %s", name, filename)
	return nil
}

func compileResultPayload(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(protocolRepositoryRoot(t), "schemas", "fixtures", "conformance", "engine", "compile-success.json"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	return cloneObject(t, envelope["payload"].(map[string]any))
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func cloneObject(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	var clone map[string]any
	if err := json.Unmarshal(mustJSON(t, value), &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}
func digest(character string) string {
	result := "sha256:"
	for range 64 {
		result += character
	}
	return result
}
func module(path string) map[string]any {
	return map[string]any{"origin": map[string]any{"kind": "project"}, "module_path": path}
}
func sourceRange(path, start, end string) map[string]any {
	return map[string]any{"origin": map[string]any{"kind": "project"}, "module_path": path, "start_byte": start, "end_byte": end}
}
func sourceFile(path string) map[string]any {
	return map[string]any{"origin": map[string]any{"kind": "project"}, "module_path": path, "digest": digest("a"), "byte_length": "0"}
}
func sourceAsset(locator string) map[string]any {
	return map[string]any{"subject_address": "ldl:project:p:entity:e", "authored_path": locator, "locator": locator, "origin": map[string]any{"kind": "project"}, "module_path": "document.ldl", "range": sourceRange("document.ldl", "0", "1"), "digest": digest("a"), "media_type": "image/png", "byte_length": "0"}
}
func sourceBinding(id string) map[string]any {
	return map[string]any{"source_address": "ldl:project:p:view:v", "target_address": "ldl:project:p:entity:" + id, "target_kind": "entity", "target_owner_address": "ldl:project:p", "via": "test", "module": module("document.ldl"), "range": sourceRange("document.ldl", "0", "1")}
}
func exportBinding(name string) map[string]any {
	return map[string]any{"public_name": name, "target_address": "ldl:project:p:entity:e", "module": module("document.ldl"), "range": sourceRange("document.ldl", "0", "1"), "re_export": false}
}
func semanticReference(source, target, start string) map[string]any {
	return map[string]any{"source_address": source, "target_address": target, "target_kind": "entity", "via": "test", "range": sourceRange("document.ldl", start, "1")}
}
