// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

func TestPlanExportDeterministicAndTraceable(t *testing.T) {
	input := exportPlanFixture()
	first, err := New(BuildInfo{}).PlanExport(context.Background(), input)
	if err != nil {
		t.Fatalf("%v: %v", err, errors.Unwrap(err))
	}
	second, err := New(BuildInfo{}).PlanExport(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("PlanExport is not deterministic")
	}
	if first.ViewDataHash == "" || first.RecipeHash == "" || first.ProfileRefHash == "" || first.InvocationHash == "" {
		t.Fatalf("hash closure is incomplete: %+v", first)
	}
	if first.RequiresRenderer || first.LayoutRequirement != "none" || first.Pagination.Kind != "none" {
		t.Fatalf("JSON plan inferred visual behavior: %+v", first)
	}
	if len(first.Artifacts) != 1 || !first.Artifacts[0].Primary {
		t.Fatalf("artifact plan is not pre-serialization: %+v", first.Artifacts)
	}
	if len(first.Representations) != 2 || first.Representations[0].ViewdataKey != "viewdata-root" || first.Representations[1].Source.EntityAddresses[0] != "ldl:project:p:entity:e" {
		t.Fatalf("representations lost traceability: %+v", first.Representations)
	}
	if len(first.Units) != 1 || len(first.Units[0].ViewdataKeys) != 2 {
		t.Fatalf("ordering units are incomplete: %+v", first.Units)
	}
	if len(first.RequiredFontDigests) != 1 || len(first.RequiredAssetDigests) != 2 || first.ProfileRequirementsHash == "" {
		t.Fatalf("resolved requirements were not closed into the plan: %+v", first)
	}
	if !bytes.Equal(first.SerializerOptions, input.Recipe.SerializerOptions) {
		t.Fatalf("complete serializer options were not preserved: got=%s want=%s", first.SerializerOptions, input.Recipe.SerializerOptions)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := semantic.DecodeExportPlan(encoded); err != nil {
		t.Fatalf("generated codec rejected plan: %v", err)
	}
}

func TestPlanExportRejectsProfileAndInputMismatches(t *testing.T) {
	input := exportPlanFixture()
	input.Recipe.ViewAddress = "ldl:project:p:view:other"
	if _, err := New(BuildInfo{}).PlanExport(context.Background(), input); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("cross-view recipe error = %v", err)
	}

	input = exportPlanFixture()
	input.Recipe.ExporterProfile.SpecificationDigest = "sha256:" + strings.Repeat("b", 64)
	input.Recipe.CanonicalJSON = bytes.ReplaceAll(input.Recipe.CanonicalJSON, []byte(`"specification_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`), []byte(`"specification_digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"`))
	input.Requirements.ExporterProfile.SpecificationDigest = input.Recipe.ExporterProfile.SpecificationDigest
	input.Requirements.SerializerProfile.SpecificationDigest = input.Recipe.ExporterProfile.SpecificationDigest
	input.Requirements.CanonicalJSON = bytes.ReplaceAll(input.Requirements.CanonicalJSON, []byte(`"specification_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`), []byte(`"specification_digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"`))
	changed, err := New(BuildInfo{}).PlanExport(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	original, err := New(BuildInfo{}).PlanExport(context.Background(), exportPlanFixture())
	if err != nil {
		t.Fatal(err)
	}
	if changed.ProfileRefHash == original.ProfileRefHash || changed.InvocationHash == original.InvocationHash {
		t.Fatal("profile digest was not bound into deterministic hashes")
	}

	input = exportPlanFixture()
	input.Recipe.ExporterProfile.Format = "yaml"
	if _, err := New(BuildInfo{}).PlanExport(context.Background(), input); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("profile mismatch error = %v", err)
	}
	input = exportPlanFixture()
	input.Requirements.ExporterProfile.ID = "layerdraw/other@1"
	if _, err := New(BuildInfo{}).PlanExport(context.Background(), input); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("resolved profile mismatch error = %v", err)
	}
	input = exportPlanFixture()
	input.Requirements.SerializerProfile.SpecificationDigest = "sha256:" + strings.Repeat("b", 64)
	if _, err := New(BuildInfo{}).PlanExport(context.Background(), input); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("serializer specification mismatch error = %v", err)
	}
	input = exportPlanFixture()
	input.Requirements.RequiredFontDigests = append(input.Requirements.RequiredFontDigests, "sha256:"+strings.Repeat("d", 64))
	if _, err := New(BuildInfo{}).PlanExport(context.Background(), input); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("canonical requirements divergence error = %v", err)
	}
	input = exportPlanFixture()
	input.Requirements.RequiredAssetDigests = []string{"sha256:" + strings.Repeat("d", 64), "sha256:" + strings.Repeat("c", 64)}
	input.Requirements.CanonicalJSON = []byte(strings.Replace(string(input.Requirements.CanonicalJSON), `"required_asset_digests":["sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"]`, `"required_asset_digests":["sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"]`, 1))
	if _, err := New(BuildInfo{}).PlanExport(context.Background(), input); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("unsorted requirements error = %v", err)
	}
}

func TestPlanExportRejectsSplitBrainCanonicalInputs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ExportPlanInput)
	}{
		{name: "view fields", mutate: func(input *ExportPlanInput) { input.ViewData.Kind = "diagram" }},
		{name: "view source", mutate: func(input *ExportPlanInput) { input.ViewData.Source.EntityAddresses = nil }},
		{name: "recipe fields", mutate: func(input *ExportPlanInput) { input.Recipe.Filename = "other.json" }},
		{name: "serializer options", mutate: func(input *ExportPlanInput) {
			input.Recipe.SerializerOptions = append(input.Recipe.SerializerOptions, '\n')
		}},
		{name: "requirements fields", mutate: func(input *ExportPlanInput) { input.Requirements.RequiredFontDigests = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := exportPlanFixture()
			test.mutate(&input)
			if _, err := New(BuildInfo{}).PlanExport(context.Background(), input); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
				t.Fatalf("split-brain input error = %v", err)
			}
		})
	}
}

func TestPlanExportRequiresGeneratedCanonicalSemanticAuthority(t *testing.T) {
	appendUnknownField := func(encoded []byte) []byte {
		result := append([]byte(nil), encoded[:len(encoded)-1]...)
		return append(result, []byte(`,"unexpected":true}`)...)
	}
	tests := []struct {
		name   string
		mutate func(*ExportPlanInput)
	}{
		{name: "noncanonical ViewData", mutate: func(input *ExportPlanInput) {
			input.ViewData.CanonicalJSON = append(input.ViewData.CanonicalJSON, '\n')
		}},
		{name: "noncanonical ExportRecipe", mutate: func(input *ExportPlanInput) { input.Recipe.CanonicalJSON = append(input.Recipe.CanonicalJSON, '\n') }},
		{name: "noncanonical requirements", mutate: func(input *ExportPlanInput) {
			input.Requirements.CanonicalJSON = append(input.Requirements.CanonicalJSON, '\n')
		}},
		{name: "noncanonical state summary", mutate: func(input *ExportPlanInput) {
			*input = exportPlanFixtureWithStateSummary()
			input.StateSummary.CanonicalJSON = append(input.StateSummary.CanonicalJSON, '\n')
		}},
		{name: "noncanonical state payload", mutate: func(input *ExportPlanInput) {
			*input = exportPlanFixtureWithStateSummary()
			input.StateSummary.PayloadCanonicalJSON = append(input.StateSummary.PayloadCanonicalJSON, '\n')
		}},
		{name: "unknown canonical field", mutate: func(input *ExportPlanInput) {
			input.ViewData.CanonicalJSON = appendUnknownField(input.ViewData.CanonicalJSON)
		}},
		{name: "malformed nested canonical value", mutate: func(input *ExportPlanInput) {
			input.ViewData.CanonicalJSON = bytes.Replace(input.ViewData.CanonicalJSON, []byte(`"incoming":true`), []byte(`"incoming":"true"`), 1)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := exportPlanFixture()
			test.mutate(&input)
			if _, err := New(BuildInfo{}).PlanExport(context.Background(), input); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
				t.Fatalf("generated semantic authority error = %v", err)
			}
		})
	}
}

func TestPlanExportCancellationAndDefensiveStateSummary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(BuildInfo{}).PlanExport(ctx, exportPlanFixture()); err != context.Canceled {
		t.Fatalf("cancel error = %v", err)
	}
	input := exportPlanFixture()
	yes := true
	input.Recipe.Options.StateSummary = &yes
	if _, err := New(BuildInfo{}).PlanExport(context.Background(), input); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("missing summary error = %v", err)
	}
}

func TestPlanExportStateSummaryHashClosure(t *testing.T) {
	input := exportPlanFixtureWithStateSummary()
	plan, err := New(BuildInfo{}).PlanExport(context.Background(), input)
	if err != nil || plan.StateSummaryHash == nil {
		t.Fatalf("state summary plan=%+v err=%v cause=%v", plan, err, errors.Unwrap(err))
	}
	splitBrain := input
	summaryCopy := *input.StateSummary
	splitBrain.StateSummary = &summaryCopy
	splitBrain.StateSummary.StateVersion = "other"
	if _, err := New(BuildInfo{}).PlanExport(context.Background(), splitBrain); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("state summary metadata mismatch error=%v", err)
	}
	input.StateSummary.PayloadHash = "sha256:" + strings.Repeat("0", 64)
	if _, err := New(BuildInfo{}).PlanExport(context.Background(), input); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("payload mismatch error=%v", err)
	}
}

func TestExportPlanningFormatBranches(t *testing.T) {
	for _, format := range []string{"json", "xlsx", "pdf", "docx", "pptx", "svg", "png", "drawio", "csv", "tsv", "markdown"} {
		recipe := exportPlanFixture().Recipe
		recipe.Format = format
		pagination := exportPagination(recipe)
		if pagination.Kind == "" {
			t.Fatalf("pagination missing for %s", format)
		}
		artifacts := []ExportArtifactEntry{{Role: "primary", Primary: true}}
		_ = exportPlanUnits("diagram", recipe, artifacts)
		_ = exportRequiresRenderer(format)
	}
	yes := true
	recipe := exportPlanFixture().Recipe
	recipe.Format, recipe.Options.Bundle = "csv", &yes
	if got := exportArtifacts("diagram", recipe); len(got) != 6 {
		t.Fatalf("bundled diagram artifacts=%d", len(got))
	}
	for _, shape := range []string{"diagram", "table", "matrix", "tree", "flow", "context", "diff", "unknown"} {
		_ = exportShapeRoles(shape)
		_ = primaryArtifactRole(shape, "png")
	}
	for _, role := range []string{"row_axis", "column_axis", "support_items", "cycle_refs", "link_refs", "facts"} {
		_ = canonicalArtifactRole(role)
	}
	if exportLayoutRequirement(true) != "presentation_geometry" || exportLayoutRequirement(false) != "none" {
		t.Fatal("layout requirement mapping")
	}
}

func TestExportTopologyAcrossArtifactAndUnitFormats(t *testing.T) {
	roles := exportShapeRoles("diagram")
	items := make([]exportItem, len(roles))
	for index, role := range roles {
		items[index] = exportItem{key: fmt.Sprintf("vdi:%s:%s", strings.TrimSuffix(role, "s"), strings.Repeat("A", 43)), role: role, source: ExportPlanSourceRefs{}}
	}
	yes := true
	tests := []struct {
		name, format, kind string
		bundle             *bool
		artifacts, units   int
		omitted            int
		lossless           bool
	}{
		{name: "diagram xlsx", format: "xlsx", kind: "sheet", artifacts: 1, units: 6},
		{name: "bundled csv", format: "csv", kind: "section", bundle: &yes, artifacts: 6, units: 6},
		{name: "single csv", format: "csv", kind: "section", artifacts: 1, units: 1, omitted: 5},
		{name: "pdf pages", format: "pdf", kind: "page", artifacts: 1, units: 6},
		{name: "pptx slides", format: "pptx", kind: "slide", artifacts: 1, units: 6},
		{name: "json lossless", format: "json", kind: "section", artifacts: 1, units: 1, lossless: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recipe := exportPlanFixture().Recipe
			recipe.Format, recipe.Options.Bundle = test.format, test.bundle
			if test.lossless {
				recipe.Fidelity = "lossless"
			} else {
				recipe.Fidelity = "lossy"
			}
			artifacts := exportArtifacts("diagram", recipe)
			units := exportPlanUnits("diagram", recipe, artifacts)
			representations := exportRepresentations(items, ExportPlanSourceRefs{}, recipe, units)
			if err := validateExportTopology(artifacts, units, representations); err != nil {
				t.Fatal(err)
			}
			if len(artifacts) != test.artifacts || len(units) != test.units {
				t.Fatalf("artifacts=%d units=%d", len(artifacts), len(units))
			}
			omitted := 0
			for _, representation := range representations {
				if representation.Disposition == "omitted" {
					omitted++
				}
			}
			if omitted != test.omitted || units[0].Kind != test.kind {
				t.Fatalf("omitted=%d first unit=%+v", omitted, units[0])
			}
			if test.lossless && representations[0].ViewdataKey != "viewdata-root" {
				t.Fatalf("lossless root representation=%+v", representations[0])
			}
		})
	}
	recipe := exportPlanFixture().Recipe
	recipe.Format, recipe.Fidelity = "xlsx", "lossy"
	artifacts := exportArtifacts("diagram", recipe)
	units := exportPlanUnits("diagram", recipe, artifacts)
	representations := exportRepresentations(items, ExportPlanSourceRefs{}, recipe, units)
	badRole := "missing-artifact"
	representations[0].ArtifactRole = &badRole
	if err := validateExportTopology(artifacts, units, representations); err == nil {
		t.Fatal("cross-artifact representation topology was accepted")
	}
}

func TestExportPlanCollectsEveryViewDataShapeWithClosedRoleAndSourceTopology(t *testing.T) {
	rootSource := ExportPlanSourceRefs{SubjectAddresses: []string{"ldl:project:p:entity:root"}, EntityAddresses: []string{"ldl:project:p:entity:root"}, RelationAddresses: []string{}, LayerAddresses: []string{}, RowAddresses: []string{}, CellRefs: []ExportPlanCellRef{}, AssetDigests: []string{}, State: ExportPlanStateRefs{Reads: []ExportPlanStateReadRef{}}}
	itemSource := ExportPlanSourceRefs{SubjectAddresses: []string{"ldl:project:p:entity:item"}, EntityAddresses: []string{"ldl:project:p:entity:item"}, RelationAddresses: []string{}, LayerAddresses: []string{}, RowAddresses: []string{}, CellRefs: []ExportPlanCellRef{}, AssetDigests: []string{}, State: ExportPlanStateRefs{Reads: []ExportPlanStateReadRef{}}}
	nestedSource := ExportPlanSourceRefs{SubjectAddresses: []string{"ldl:project:p:entity:nested"}, EntityAddresses: []string{"ldl:project:p:entity:nested"}, RelationAddresses: []string{}, LayerAddresses: []string{}, RowAddresses: []string{}, CellRefs: []ExportPlanCellRef{}, AssetDigests: []string{}, State: ExportPlanStateRefs{Reads: []ExportPlanStateReadRef{}}}
	projectionSource := func(source ExportPlanSourceRefs) any {
		encoded, err := json.Marshal(source)
		if err != nil {
			t.Fatal(err)
		}
		var result any
		if err := json.Unmarshal(encoded, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	key := func(kind, suffix string) string { return "vdi:" + kind + ":" + strings.Repeat(suffix, 43) }
	item := func(kind, suffix string, source ExportPlanSourceRefs) map[string]any {
		return map[string]any{"key": key(kind, suffix), "source": projectionSource(source)}
	}
	type expectedItem struct {
		key, role, sourceEntity string
	}
	tests := []struct {
		kind     string
		payload  map[string]any
		expected []expectedItem
	}{
		{kind: "diagram", payload: map[string]any{
			"occurrences": []any{item("diagram-occurrence", "A", itemSource)}, "edges": []any{item("diagram-edge", "B", itemSource)},
			"containers": []any{item("diagram-container", "C", itemSource)}, "overlays": []any{item("diagram-overlay", "D", itemSource)},
			"badges": []any{item("diagram-badge", "E", itemSource)}, "support_items": []any{item("diagram-support", "F", itemSource)},
		}, expected: []expectedItem{
			{key("diagram-occurrence", "A"), "occurrences", "ldl:project:p:entity:item"}, {key("diagram-edge", "B"), "edges", "ldl:project:p:entity:item"},
			{key("diagram-container", "C"), "containers", "ldl:project:p:entity:item"}, {key("diagram-overlay", "D"), "overlays", "ldl:project:p:entity:item"},
			{key("diagram-badge", "E"), "badges", "ldl:project:p:entity:item"}, {key("diagram-support", "F"), "support_items", "ldl:project:p:entity:item"},
		}},
		{kind: "table", payload: map[string]any{
			"columns": []any{map[string]any{"key": key("table-column", "G")}}, "rows": []any{item("table-row", "H", itemSource)},
		}, expected: []expectedItem{{key("table-column", "G"), "columns", "ldl:project:p:entity:root"}, {key("table-row", "H"), "rows", "ldl:project:p:entity:item"}}},
		{kind: "matrix", payload: map[string]any{
			"row_axis": []any{item("matrix-row", "I", itemSource)}, "column_axis": []any{item("matrix-column", "J", itemSource)}, "cells": []any{item("matrix-cell", "K", itemSource)},
		}, expected: []expectedItem{{key("matrix-row", "I"), "row_axis", "ldl:project:p:entity:item"}, {key("matrix-column", "J"), "column_axis", "ldl:project:p:entity:item"}, {key("matrix-cell", "K"), "cells", "ldl:project:p:entity:item"}}},
		{kind: "tree", payload: map[string]any{
			"roots":      []any{map[string]any{"key": key("tree-occurrence", "L"), "source": projectionSource(itemSource), "children": []any{map[string]any{"key": key("tree-occurrence", "M"), "source": projectionSource(nestedSource), "children": []any{}}}}},
			"cycle_refs": []any{item("tree-cycle", "N", itemSource)}, "link_refs": []any{item("tree-link", "O", itemSource)},
		}, expected: []expectedItem{{key("tree-occurrence", "L"), "occurrences", "ldl:project:p:entity:item"}, {key("tree-occurrence", "M"), "occurrences", "ldl:project:p:entity:nested"}, {key("tree-cycle", "N"), "cycle_refs", "ldl:project:p:entity:item"}, {key("tree-link", "O"), "link_refs", "ldl:project:p:entity:item"}}},
		{kind: "flow", payload: map[string]any{
			"steps": []any{item("flow-step", "P", itemSource)}, "connectors": []any{item("flow-connector", "Q", itemSource)}, "lanes": []any{item("flow-lane", "R", itemSource)}, "cycle_refs": []any{item("flow-cycle", "S", itemSource)},
		}, expected: []expectedItem{{key("flow-step", "P"), "steps", "ldl:project:p:entity:item"}, {key("flow-connector", "Q"), "connectors", "ldl:project:p:entity:item"}, {key("flow-lane", "R"), "lanes", "ldl:project:p:entity:item"}, {key("flow-cycle", "S"), "cycle_refs", "ldl:project:p:entity:item"}}},
		{kind: "context", payload: map[string]any{
			"groups": []any{map[string]any{"key": key("context-group", "T"), "source": projectionSource(itemSource), "facts": []any{item("context-fact", "U", nestedSource)}, "attributes": []any{item("context-attribute", "V", nestedSource)}}},
		}, expected: []expectedItem{{key("context-group", "T"), "groups", "ldl:project:p:entity:item"}, {key("context-fact", "U"), "facts", "ldl:project:p:entity:nested"}, {key("context-attribute", "V"), "attributes", "ldl:project:p:entity:nested"}}},
		{kind: "diff", payload: map[string]any{
			"changes": []any{map[string]any{"key": key("diff-change", "W"), "source": projectionSource(itemSource), "fields": []any{map[string]any{"key": key("diff-field", "X")}}}},
		}, expected: []expectedItem{{key("diff-change", "W"), "changes", "ldl:project:p:entity:item"}, {key("diff-field", "X"), "field_diffs", "ldl:project:p:entity:item"}}},
	}

	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			projection := map[string]any{"kind": test.kind, "source": projectionSource(rootSource), test.kind: test.payload}
			items, err := collectExportItems(projection)
			if err != nil {
				t.Fatal(err)
			}
			recipe := exportPlanFixture().Recipe
			recipe.Format, recipe.Fidelity = "xlsx", "lossless"
			artifacts := exportArtifacts(test.kind, recipe)
			units := exportPlanUnits(test.kind, recipe, artifacts)
			representations := exportRepresentations(items, rootSource, recipe, units)
			if err := validateExportTopology(artifacts, units, representations); err != nil {
				t.Fatal(err)
			}
			if len(representations) != len(test.expected)+1 || representations[0].ViewdataKey != "viewdata-root" {
				t.Fatalf("reserved root topology is not canonical: %+v", representations)
			}
			expectedOrder := make([]string, 0, len(test.expected))
			unitsByID := map[string]ExportPlanUnit{}
			for _, unit := range units {
				unitsByID[unit.UnitID] = unit
			}
			for _, expected := range test.expected {
				expectedOrder = append(expectedOrder, expected.key)
				matches := []ExportRepresentation{}
				for _, representation := range representations {
					if representation.ViewdataKey == expected.key {
						matches = append(matches, representation)
					}
				}
				if len(matches) != 1 || matches[0].Disposition == "omitted" || matches[0].UnitID == nil {
					t.Fatalf("%s representation topology=%+v", expected.key, matches)
				}
				if unit := unitsByID[*matches[0].UnitID]; unit.Role != expected.role {
					t.Fatalf("%s role=%q want=%q", expected.key, unit.Role, expected.role)
				}
				if len(matches[0].Source.EntityAddresses) != 1 || matches[0].Source.EntityAddresses[0] != expected.sourceEntity {
					t.Fatalf("%s source=%+v", expected.key, matches[0].Source)
				}
			}
			sort.Strings(expectedOrder)
			actualOrder := make([]string, len(representations)-1)
			for index := 1; index < len(representations); index++ {
				actualOrder[index-1] = representations[index].ViewdataKey
			}
			if !reflect.DeepEqual(actualOrder, expectedOrder) {
				t.Fatalf("representation order=%v want=%v", actualOrder, expectedOrder)
			}
		})
	}
}

func TestPlanExportRejectsGloballyDuplicateViewDataItemKeys(t *testing.T) {
	input := exportPlanFixture()
	view, err := semantic.DecodeViewData(input.ViewData.CanonicalJSON)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := view.Context.Groups[0].Key
	rowAddress := semantic.StableAddress("ldl:project:p:entity:e:row:primary")
	view.Source.RowAddresses = []semantic.StableAddress{rowAddress}
	view.Source.SubjectAddresses = append(view.Source.SubjectAddresses, rowAddress)
	view.Context.Groups[0].Source = view.Source
	view.Context.Groups[0].Attributes = []semantic.ContextAttribute{{
		GroupKey: duplicate, Key: duplicate, OwnerAddress: "ldl:project:p:entity:e", RowAddress: rowAddress, Source: view.Source, Values: map[string]semantic.RecipeScalar{},
	}}
	input.ViewData.CanonicalJSON, err = semantic.EncodeViewData(view)
	if err != nil {
		t.Fatalf("duplicate must remain wire-valid to exercise the planner boundary: %v", err)
	}
	encodedSource, _ := json.Marshal(view.Source)
	if err := json.Unmarshal(encodedSource, &input.ViewData.Source); err != nil {
		t.Fatal(err)
	}
	if _, err := New(BuildInfo{}).PlanExport(context.Background(), input); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) || !strings.Contains(fmt.Sprint(errors.Unwrap(err)), "duplicate ViewData item key") {
		t.Fatalf("globally duplicated ViewData key error=%v cause=%v", err, errors.Unwrap(err))
	}
}

func TestCollectExportItemsRejectsMalformedPlanningProjections(t *testing.T) {
	source := map[string]any{
		"subject_addresses": []any{}, "entity_addresses": []any{}, "relation_addresses": []any{}, "layer_addresses": []any{},
		"row_addresses": []any{}, "cell_refs": []any{}, "asset_digests": []any{}, "state": map[string]any{"reads": []any{}},
	}
	validKey := "vdi:test-item:" + strings.Repeat("A", 43)
	tests := []struct {
		name string
		root any
	}{
		{name: "root is not object", root: []any{}},
		{name: "root source is not encodable", root: map[string]any{"source": func() {}, "kind": "diagram", "diagram": map[string]any{}}},
		{name: "kind missing", root: map[string]any{"source": source}},
		{name: "shape missing", root: map[string]any{"source": source, "kind": "diagram"}},
		{name: "unsupported shape", root: map[string]any{"source": source, "kind": "future", "future": map[string]any{}}},
		{name: "required collection missing", root: map[string]any{"source": source, "kind": "diagram", "diagram": map[string]any{}}},
		{name: "item is not object", root: map[string]any{"source": source, "kind": "diagram", "diagram": map[string]any{"occurrences": []any{"bad"}, "edges": []any{}, "containers": []any{}, "overlays": []any{}, "badges": []any{}, "support_items": []any{}}}},
		{name: "item key invalid", root: map[string]any{"source": source, "kind": "diagram", "diagram": map[string]any{"occurrences": []any{map[string]any{"key": "bad", "source": source}}, "edges": []any{}, "containers": []any{}, "overlays": []any{}, "badges": []any{}, "support_items": []any{}}}},
		{name: "item source is not encodable", root: map[string]any{"source": source, "kind": "diagram", "diagram": map[string]any{"occurrences": []any{map[string]any{"key": validKey, "source": func() {}}}, "edges": []any{}, "containers": []any{}, "overlays": []any{}, "badges": []any{}, "support_items": []any{}}}},
		{name: "cross collection duplicate", root: map[string]any{"source": source, "kind": "diagram", "diagram": map[string]any{"occurrences": []any{map[string]any{"key": validKey, "source": source}}, "edges": []any{map[string]any{"key": validKey, "source": source}}, "containers": []any{}, "overlays": []any{}, "badges": []any{}, "support_items": []any{}}}},
		{name: "nested collection missing", root: map[string]any{"source": source, "kind": "tree", "tree": map[string]any{"roots": []any{map[string]any{"key": validKey, "source": source}}, "cycle_refs": []any{}, "link_refs": []any{}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := collectExportItems(test.root); err == nil {
				t.Fatal("malformed planning projection was accepted")
			}
		})
	}
}

func TestPlanExportEnforcesViewDataStatePolicyInputMatrix(t *testing.T) {
	tests := []struct {
		name, policy, inputKind string
		valid                   bool
	}{
		{name: "none rejects snapshot", policy: "none", inputKind: "snapshot"},
		{name: "required rejects none", policy: "required", inputKind: "none"},
		{name: "optional accepts none", policy: "optional", inputKind: "none", valid: true},
		{name: "optional accepts snapshot", policy: "optional", inputKind: "snapshot", valid: true},
		{name: "required accepts snapshot", policy: "required", inputKind: "snapshot", valid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := exportPlanFixtureWithViewState(t, test.policy, test.inputKind)
			_, err := New(BuildInfo{}).PlanExport(context.Background(), input)
			if test.valid && err != nil {
				t.Fatalf("valid state matrix rejected: %v cause=%v", err, errors.Unwrap(err))
			}
			if !test.valid && !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
				t.Fatalf("invalid state matrix error=%v", err)
			}
		})
	}
	if err := validateExportViewStateInput("optional", "future"); err == nil {
		t.Fatal("optional accepted an unknown state input kind")
	}
	if err := validateExportViewStateInput("future", "none"); err == nil {
		t.Fatal("unknown state policy was accepted")
	}
}

func TestExportProjectionRejectsInvalidJSONAndStripsMessages(t *testing.T) {
	if _, err := encodedProjection([]byte("{")); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if _, err := exportHashProjection([]byte("[]")); err == nil {
		t.Fatal("non-object ViewData accepted")
	}
	projection := map[string]any{"message": "localized", "related": []any{map[string]any{"message": "nested"}}, "ignored": "ok"}
	stripLocalizedDiagnosticMessages([]any{projection, "ignored"})
	if _, exists := projection["message"]; exists {
		t.Fatal("localized message retained")
	}
}

func pointerString(value string) *string { return &value }

func exportPlanFixture() ExportPlanInput {
	digest := protocolcommon.Digest("sha256:" + strings.Repeat("a", 64))
	revisionID := "revision-1"
	source := semantic.ViewDataSourceRefs{
		AssetDigests: []protocolcommon.Digest{digest}, CellRefs: []semantic.ViewDataCellRef{},
		EntityAddresses: []semantic.EntityAddress{"ldl:project:p:entity:e"}, LayerAddresses: []semantic.LayerAddress{},
		RelationAddresses: []semantic.RelationAddress{}, RowAddresses: []semantic.StableAddress{},
		State:            semantic.ViewDataStateRefs{Reads: []semantic.ViewDataStateReadRef{}},
		SubjectAddresses: []semantic.StableAddress{"ldl:project:p:entity:e"},
	}
	contextShape := semantic.ViewContextShape{GroupBy: "none", IncludeEntityRows: false, IncludeRelationRows: false, Incoming: true, Outgoing: true}
	view := semantic.ViewData{
		Category: "context", Context: &semantic.ContextViewData{Groups: []semantic.ContextGroup{{Attributes: []semantic.ContextAttribute{}, Facts: []semantic.ContextFact{}, Key: semantic.ViewDataItemKey("vdi:context-group:" + strings.Repeat("A", 43)), Label: "All", Source: source}}},
		Diagnostics: []semantic.Diagnostic{}, Kind: "context", ProjectAddress: "ldl:project:p",
		Revision: semantic.ViewRevision{DefinitionHash: &digest, Kind: "single", RevisionID: &revisionID},
		Shape:    semantic.ViewRecipeShape{Context: &contextShape, Kind: "context"}, Source: source,
		StateInput: semantic.ViewDataStateInputRef{Kind: "none"}, StatePolicy: "none", ViewAddress: "ldl:project:p:view:v",
	}
	no := false
	recipe := semantic.ExportRecipe{
		Address: "ldl:project:p:view:v:export:json", EffectiveMaximumFidelity: "lossless",
		ExporterProfile: semantic.ExporterProfileRef{Format: "json", ID: "layerdraw/json@1", RegistryDigest: digest, RegistrySchemaVersion: 1, SpecificationDigest: digest},
		Extension:       ".json", Fidelity: "lossless", FidelityBasis: "native", Filename: "v.json", Format: "json", ID: "json",
		NativeMaximumFidelity: "lossless", Options: semantic.ExportOptions{Diagnostics: &no, Kind: "json", StateSummary: &no},
		RequiresSourceManifest: false, SourceRefs: true, ViewAddress: "ldl:project:p:view:v",
	}
	viewJSON, err := semantic.EncodeViewData(view)
	if err != nil {
		panic(err)
	}
	recipeJSON, err := semantic.EncodeExportRecipe(recipe)
	if err != nil {
		panic(err)
	}
	fontDigest := protocolcommon.Digest("sha256:" + strings.Repeat("b", 64))
	assetDigest := protocolcommon.Digest("sha256:" + strings.Repeat("c", 64))
	requirements := semantic.ResolvedExportProfileRequirements{SchemaVersion: 1, ExporterProfile: recipe.ExporterProfile, SerializerProfile: recipe.ExporterProfile, RequiredAssetDigests: []protocolcommon.Digest{assetDigest}, RequiredFontDigests: []protocolcommon.Digest{fontDigest}}
	requirementsJSON, err := semantic.EncodeResolvedExportProfileRequirements(requirements)
	if err != nil {
		panic(err)
	}
	var rootSource ExportPlanSourceRefs
	encodedSource, _ := json.Marshal(source)
	if err := json.Unmarshal(encodedSource, &rootSource); err != nil {
		panic(err)
	}
	definitionHash := string(digest)
	return ExportPlanInput{
		ViewData: ExportPlanViewData{
			Kind: "context", ViewAddress: string(view.ViewAddress), RevisionKind: "single", DefinitionHash: &definitionHash,
			StatePolicy: view.StatePolicy, StateInput: ExportPlanStateInput{Kind: "none"}, Source: rootSource, CanonicalJSON: viewJSON,
		},
		Recipe: ExportPlanRecipe{
			Address: string(recipe.Address), ViewAddress: string(recipe.ViewAddress), Format: string(recipe.Format), Filename: recipe.Filename,
			Extension: recipe.Extension, Fidelity: string(recipe.Fidelity), NativeMaximumFidelity: string(recipe.NativeMaximumFidelity),
			EffectiveMaximumFidelity: string(recipe.EffectiveMaximumFidelity), FidelityBasis: recipe.FidelityBasis,
			ExporterProfile:        ExportPlanProfileRef{ID: recipe.ExporterProfile.ID, Format: string(recipe.ExporterProfile.Format), RegistrySchemaVersion: recipe.ExporterProfile.RegistrySchemaVersion, RegistryDigest: string(recipe.ExporterProfile.RegistryDigest), SpecificationDigest: string(recipe.ExporterProfile.SpecificationDigest)},
			Options:                ExportPlanOptions{Bundle: recipe.Options.Bundle, StateSummary: recipe.Options.StateSummary, Orientation: recipe.Options.Orientation, PageSize: recipe.Options.PageSize},
			RequiresSourceManifest: recipe.RequiresSourceManifest, CanonicalJSON: recipeJSON, SerializerOptions: mustEncodeExportOptions(recipe.Options),
		},
		Requirements: ExportPlanRequirements{
			ExporterProfile:      ExportPlanProfileRef{ID: recipe.ExporterProfile.ID, Format: string(recipe.ExporterProfile.Format), RegistrySchemaVersion: recipe.ExporterProfile.RegistrySchemaVersion, RegistryDigest: string(recipe.ExporterProfile.RegistryDigest), SpecificationDigest: string(recipe.ExporterProfile.SpecificationDigest)},
			SerializerProfile:    ExportPlanProfileRef{ID: recipe.ExporterProfile.ID, Format: string(recipe.ExporterProfile.Format), RegistrySchemaVersion: recipe.ExporterProfile.RegistrySchemaVersion, RegistryDigest: string(recipe.ExporterProfile.RegistryDigest), SpecificationDigest: string(recipe.ExporterProfile.SpecificationDigest)},
			RequiredAssetDigests: []string{string(assetDigest)}, RequiredFontDigests: []string{string(fontDigest)}, CanonicalJSON: requirementsJSON,
		},
	}
}

func exportPlanFixtureWithStateSummary() ExportPlanInput {
	input := exportPlanFixture()
	yes := true
	input.Recipe.Options.StateSummary = &yes
	input.Recipe.CanonicalJSON = bytes.Replace(input.Recipe.CanonicalJSON, []byte(`"state_summary":false`), []byte(`"state_summary":true`), 1)
	input.Recipe.SerializerOptions = bytes.Replace(input.Recipe.SerializerOptions, []byte(`"state_summary":false`), []byte(`"state_summary":true`), 1)
	input.ViewData.StatePolicy = "optional"
	input.ViewData.CanonicalJSON = bytes.Replace(input.ViewData.CanonicalJSON, []byte(`"state_policy":"none"`), []byte(`"state_policy":"optional"`), 1)
	payload := protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindObject, Object: map[string]protocolcommon.JsonValue{
		"enabled": {Kind: protocolcommon.JsonValueKindBoolean, Boolean: true},
	}}
	payloadJSON, err := protocolcommon.EncodeJsonValue(payload)
	if err != nil {
		panic(err)
	}
	payloadHash := sha256.Sum256(payloadJSON)
	summaryValue := semantic.ExternalStateSummary{
		Format: semantic.ExternalStateSummaryFormatValue, SchemaVersion: 1,
		DefinitionHash: protocolcommon.Digest(*input.ViewData.DefinitionHash), StateVersion: "s1",
		PayloadHash: protocolcommon.Digest(fmt.Sprintf("sha256:%x", payloadHash)), Payload: payload,
	}
	summaryJSON, err := semantic.EncodeExternalStateSummary(summaryValue)
	if err != nil {
		panic(err)
	}
	input.StateSummary = &ExportPlanStateSummary{
		Format: string(summaryValue.Format), SchemaVersion: summaryValue.SchemaVersion,
		DefinitionHash: string(summaryValue.DefinitionHash), StateVersion: summaryValue.StateVersion,
		PayloadHash: string(summaryValue.PayloadHash), PayloadCanonicalJSON: payloadJSON, CanonicalJSON: summaryJSON,
	}
	return input
}

func exportPlanFixtureWithViewState(t *testing.T, policy, inputKind string) ExportPlanInput {
	t.Helper()
	input := exportPlanFixture()
	view, err := semantic.DecodeViewData(input.ViewData.CanonicalJSON)
	if err != nil {
		t.Fatal(err)
	}
	view.StatePolicy = policy
	view.StateInput = semantic.ViewDataStateInputRef{Kind: inputKind}
	if inputKind == "snapshot" {
		capturedAt := protocolcommon.Rfc3339Time("2026-07-18T00:00:00Z")
		definitionHash := protocolcommon.Digest(*input.ViewData.DefinitionHash)
		snapshotHash := protocolcommon.Digest("sha256:" + strings.Repeat("d", 64))
		stateVersion := "state-1"
		view.StateInput.CapturedAt = &capturedAt
		view.StateInput.DefinitionHash = &definitionHash
		view.StateInput.SnapshotHash = &snapshotHash
		view.StateInput.StateVersion = &stateVersion
	}
	input.ViewData.CanonicalJSON, err = semantic.EncodeViewData(view)
	if err != nil {
		t.Fatalf("state matrix fixture must remain wire-valid: %v", err)
	}
	input.ViewData.StatePolicy = policy
	encodedStateInput, err := json.Marshal(view.StateInput)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encodedStateInput, &input.ViewData.StateInput); err != nil {
		t.Fatal(err)
	}
	return input
}

func mustEncodeExportOptions(value semantic.ExportOptions) []byte {
	encoded, err := semantic.EncodeExportOptions(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
