// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"testing"
)

func TestRuntimeBridgeProjectsMasterStructure(t *testing.T) {
	ctx := context.Background()
	const sourceText = `project p "Project" {}
layers {
  app "Application" @10
  data "Data" @20
}
entity_type service "Service" {
  representation shape rect
  columns {
    environment "Environment" enum [prod, dev] required default prod
    capacity "Capacity" integer
    load "Load" number
    active "Active" boolean
  }
}
relation_type calls "Calls" data_flow {
  from caller types [service] layers [app, data]
  to callee types [service] layers [app, data]
  label "calls"
}
entities service @app {
  api "API"
  gateway "Gateway"
}
relations calls {
  api_gateway: api -> gateway "API to Gateway"
}
rows service [environment, capacity, load, active] {
  api primary: prod, 75, 1.5, true
}
`
	local := NewLocalDocumentEngine()
	source, err := local.CompileProject(ctx, LocalProjectInput{
		EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte(sourceText)},
		ResolvedDependencies: LocalResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := source.EncodedInput()
	if err != nil {
		t.Fatal(err)
	}
	bridge := local.NewRuntimeEngineBridge("structure-test-endpoint")
	working, err := bridge.Open(ctx, "document_structure", "revision_1", source.DefinitionHash, source.GraphHash, encoded)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := bridge.Structure(working)
	if err != nil {
		t.Fatal(err)
	}
	if structure.ProjectAddress != "ldl:project:p" {
		t.Fatalf("project address=%q", structure.ProjectAddress)
	}
	if string(structure.DocumentGeneration.DocumentHandle.EndpointInstanceID) != "structure-test-endpoint" ||
		structure.DocumentGeneration.DocumentHandle.Value != working.Handle || string(structure.DocumentGeneration.Value) != working.Generation {
		t.Fatalf("generation=%+v", structure.DocumentGeneration)
	}
	if len(structure.Layers) != 2 || structure.Layers[0].ID != "app" || structure.Layers[0].DisplayName != "Application" {
		t.Fatalf("layers=%+v", structure.Layers)
	}
	if len(structure.EntityTypes) != 1 || len(structure.EntityTypes[0].Columns) != 4 || structure.EntityTypes[0].Columns[0].ValueType != "enum" {
		t.Fatalf("entity types=%+v", structure.EntityTypes)
	}
	if len(structure.RelationTypes) != 1 || structure.RelationTypes[0].ForwardLabel != "calls" || len(structure.RelationTypes[0].FromEntityTypes) != 1 {
		t.Fatalf("relation types=%+v", structure.RelationTypes)
	}
	if len(structure.Entities) != 2 || structure.Entities[0].LayerAddress != "ldl:project:p:layer:app" {
		t.Fatalf("entities=%+v", structure.Entities)
	}
	var api *BridgeEntity
	for index := range structure.Entities {
		if structure.Entities[index].ID == "api" {
			api = &structure.Entities[index]
		}
	}
	if api == nil || len(api.Rows) != 1 || len(api.Rows[0].Values) != 4 {
		t.Fatalf("api rows=%+v", api)
	}
	values := map[string]string{}
	for _, cell := range api.Rows[0].Values {
		values[cell.Kind] = cell.Value
	}
	if values["enum"] != "prod" || values["integer"] != "75" || values["number"] != "1.5" || values["boolean"] != "true" {
		t.Fatalf("cells=%+v", api.Rows[0].Values)
	}
	if len(structure.Relations) != 1 || structure.Relations[0].FromAddress != "ldl:project:p:entity:api" || structure.Relations[0].CrossLayer {
		t.Fatalf("relations=%+v", structure.Relations)
	}
	subjects, err := bridge.Subjects(working)
	if err != nil || len(subjects) == 0 {
		t.Fatalf("subjects=%d err=%v", len(subjects), err)
	}
	preconditions, err := bridge.Preconditions(working)
	if err != nil || len(preconditions.ExpectedSubjectHashes) == 0 || len(preconditions.ExpectedSubtreeHashes) == 0 || len(preconditions.ExpectedChildSets) == 0 {
		t.Fatalf("preconditions=%+v err=%v", preconditions, err)
	}
	if structure.Relations[0].DisplayName == nil || *structure.Relations[0].DisplayName != "API to Gateway" {
		t.Fatalf("relation display name=%+v", structure.Relations[0].DisplayName)
	}
	stale := working
	stale.Generation = "2"
	if _, err := bridge.Structure(stale); err == nil {
		t.Fatal("stale working document exposed structure")
	}
	if _, err := bridge.Subjects(stale); err == nil {
		t.Fatal("stale working document exposed subjects")
	}
	if _, err := bridge.Preconditions(stale); err == nil {
		t.Fatal("stale working document exposed preconditions")
	}
}
