// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package query

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestCompileValidQueryGolden(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "valid_query.ldl"))
	if err != nil {
		t.Fatal(err)
	}
	input := projectInput(t, map[string]string{"document.ldl": string(source)})
	got := Compile(input)
	if got.HasErrors || len(got.Recipes) != 1 {
		t.Fatalf("Compile() recipes=%+v diagnostics=%+v\nbindings=%+v", got.Recipes, got.Diagnostics, input.Resolve.Bindings)
	}
	recipe := got.Recipes[0]
	if recipe.ID != "production_scope" || recipe.Address != "ldl:project:p:query:production_scope" || recipe.DisplayName != "Production scope" || recipe.StateInput != StateRequired {
		t.Fatalf("recipe identity = %+v", recipe)
	}
	if recipe.Description == nil || *recipe.Description != "Typed recipe" || !reflect.DeepEqual(recipe.Tags, []string{"saved", "topology"}) || recipe.Annotations["owner"] != "platform" {
		t.Fatalf("common = %+v", recipe.Common)
	}
	if len(recipe.Parameters) != 4 || recipe.Parameters[0].ID != "environment" || recipe.Parameters[0].Default == nil || recipe.Parameters[0].Default.String != "prod" || !recipe.Parameters[0].Required {
		t.Fatalf("parameters = %+v", recipe.Parameters)
	}
	if recipe.Select.LayerAddresses == nil || !reflect.DeepEqual(*recipe.Select.LayerAddresses, []string{"ldl:project:p:layer:app", "ldl:project:p:layer:data"}) || recipe.Select.RootAddresses == nil || !reflect.DeepEqual(*recipe.Select.RootAddresses, []string{"ldl:project:p:entity:alpha"}) {
		t.Fatalf("select = %+v", recipe.Select)
	}
	if recipe.Traversal == nil || recipe.Traversal.Direction != definition.TraversalBoth || recipe.Traversal.MinDepth != 0 || recipe.Traversal.MaxDepth != 3 || recipe.Traversal.CyclePolicy != CycleIncludeCycleRef {
		t.Fatalf("traversal = %+v", recipe.Traversal)
	}
	if !reflect.DeepEqual(recipe.Result, []ResultMember{ResultSeedEntities, ResultTraversedEntities, ResultPathRelations, ResultInducedRelations}) {
		t.Fatalf("result = %+v", recipe.Result)
	}
	if !reflect.DeepEqual(recipe.ReservedParameterIDs, []string{"legacy_environment"}) {
		t.Fatalf("reservations = %+v", recipe.ReservedParameterIDs)
	}
	if len(recipe.Dependencies.StateReads) != 2 || recipe.Dependencies.StateReads[0].SubjectKind != StateSubjectEntity || recipe.Dependencies.StateReads[1].SubjectKind != StateSubjectEntityRow {
		t.Fatalf("state dependencies = %+v", recipe.Dependencies.StateReads)
	}
	if len(recipe.Dependencies.ColumnAddresses) != 5 || len(recipe.Dependencies.ParameterAddresses) != 4 {
		t.Fatalf("typed dependencies = %+v", recipe.Dependencies)
	}
	if !reflect.DeepEqual(recipe.Dependencies.RelationAddresses, []string{"ldl:project:p:relation:alpha_beta"}) {
		t.Fatalf("relation dependencies = %+v", recipe.Dependencies.RelationAddresses)
	}
}

func compileProject(t *testing.T, files map[string]string) Result {
	t.Helper()
	return Compile(projectInput(t, files))
}

func projectInput(t *testing.T, files map[string]string) Input {
	t.Helper()
	parsed := map[string]resolve.SourceFile{}
	for path, source := range files {
		parsed[path] = resolve.SourceFromParse(syntax.Parse([]byte(source)))
	}
	resolved := resolve.Resolve(resolve.Input{Mode: resolve.CompileProject, EntryPath: "document.ldl", Project: resolve.ProjectInput{Files: parsed}})
	defined := definition.Compile(definition.Input{Resolve: resolved})
	graphed := graph.Compile(graph.Input{Resolve: resolved, Definition: defined})
	return Input{Resolve: resolved, Definition: defined, Graph: graphed}
}
