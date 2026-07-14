// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package materialize

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

func TestHashIsolationOwnSubtreeAndChildSets(t *testing.T) {
	base := Compile(projectStages(t, projectFixture)).Snapshot()
	referenceChanged := Compile(projectStages(t, stringsReplace(projectFixture, "Use the graph as the source of truth.", "Use only committed graph facts."))).Snapshot()
	if base.Hashes.Definition == referenceChanged.Hashes.Definition || *base.Hashes.Graph != *referenceChanged.Hashes.Graph {
		t.Fatal("Reference change did not isolate definition from graph")
	}
	if ownHash(base.Hashes, "ldl:project:p:reference:guide") == ownHash(referenceChanged.Hashes, "ldl:project:p:reference:guide") {
		t.Fatal("Reference own hash unchanged")
	}

	rowChanged := Compile(projectStages(t, stringsReplace(projectFixture, `prod, "api"`, `dev, "api"`))).Snapshot()
	entity := "ldl:project:p:entity:alpha"
	row := entity + ":row:primary"
	if ownHash(base.Hashes, entity) != ownHash(rowChanged.Hashes, entity) || ownHash(base.Hashes, row) == ownHash(rowChanged.Hashes, row) || subtreeHashValue(base.Hashes, entity) == subtreeHashValue(rowChanged.Hashes, entity) || *base.Hashes.Graph == *rowChanged.Hashes.Graph {
		t.Fatal("row hash isolation contract failed")
	}
	if childSetHash(base.Hashes, entity, SubjectEntityRow) != childSetHash(rowChanged.Hashes, entity, SubjectEntityRow) {
		t.Fatal("row value changed child membership hash")
	}

	reorderedSource := stringsReplace(projectFixture, "    environment \"Environment\" enum [prod, dev] required default prod\n    note \"Note\" string", "    note \"Note\" string\n    environment \"Environment\" enum [prod, dev] required default prod")
	reordered := Compile(projectStages(t, reorderedSource)).Snapshot()
	schema := "ldl:project:p:entity-type:service"
	if ownHash(base.Hashes, schema) == ownHash(reordered.Hashes, schema) || childSetHash(base.Hashes, schema, SubjectEntityTypeColumn) != childSetHash(reordered.Hashes, schema, SubjectEntityTypeColumn) {
		t.Fatal("authored column order was not isolated from sorted membership")
	}

	addedSource := stringsReplace(projectFixture, "    note \"Note\" string", "    note \"Note\" string\n    owner \"Owner\" string")
	added := Compile(projectStages(t, addedSource)).Snapshot()
	if childSetHash(base.Hashes, schema, SubjectEntityTypeColumn) == childSetHash(added.Hashes, schema, SubjectEntityTypeColumn) {
		t.Fatal("child create did not change child-set hash")
	}
}

func TestCompileGateAndClosedInputFailureBranches(t *testing.T) {
	base := projectStages(t, projectFixture)
	other := projectStages(t, stringsReplace(projectFixture, "Project", "Other"))
	cases := []struct {
		name   string
		mutate func(*Input)
	}{
		{"definition generation", func(value *Input) { value.Definition = other.Definition }},
		{"graph generation", func(value *Input) { value.Graph = other.Graph }},
		{"query generation", func(value *Input) { value.Query = other.Query }},
		{"view generation", func(value *Input) { value.View = other.View }},
		{"export generation", func(value *Input) { value.View.ExportRecipes = other.View.ExportRecipes }},
		{"definition root", func(value *Input) { value.Definition.Root.Address = "wrong" }},
		{"missing graph", func(value *Input) { value.Graph.Graph = nil }},
		{"query completeness", func(value *Input) { value.Query.Recipes = nil }},
		{"view completeness", func(value *Input) { value.View.Recipes = nil }},
		{"export completeness", func(value *Input) { value.View.ExportRecipes.Recipes = nil }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			if got := Compile(input); !got.HasErrors || got.Snapshot().Document != nil {
				t.Fatalf("invalid input accepted=%+v", got)
			}
		})
	}

	invalidMode := base
	invalidMode.Resolve.Mode = "invalid"
	invalidMode.Definition.Root.Mode = "invalid"
	if got := Compile(invalidMode); !got.HasErrors || !diagnosticMessage(got.Diagnostics, "unsupported compile mode") {
		t.Fatalf("invalid mode=%+v", got.Diagnostics)
	}
	badRoot := base
	badRoot.Definition.Project = nil
	if got := Compile(badRoot); !got.HasErrors || !diagnosticMessage(got.Diagnostics, "Project stages") {
		t.Fatalf("bad root=%+v", got.Diagnostics)
	}

	missingClosure := base
	missingClosure.Resolved.SelectedClosure = []ResolvedPackClosure{{ResolvedPackSummary: ResolvedPackSummary{Address: "extra"}}}
	if got := Compile(missingClosure); !got.HasErrors {
		t.Fatal("mismatched closure accepted")
	}
	missingSource := base
	missingSource.Resolved.SourceFiles = nil
	if got := Compile(missingSource); !got.HasErrors {
		t.Fatal("missing source accepted")
	}
	duplicateSource := base
	duplicateSource.Resolved.SourceFiles = append(duplicateSource.Resolved.SourceFiles, duplicateSource.Resolved.SourceFiles[0])
	if got := Compile(duplicateSource); !got.HasErrors {
		t.Fatal("duplicate source accepted")
	}

	bytesValue := []byte("asset")
	assetBase := projectStages(t, assetFixture)
	if got := Compile(assetBase); !got.HasErrors || !diagnosticMessage(got.Diagnostics, "no closed resolved bytes") {
		t.Fatalf("missing referenced asset=%+v", got.Diagnostics)
	}
	projectOrigin := resolve.SourceOrigin{Kind: resolve.OriginProject}
	assetCases := [][]ResolvedAsset{
		{{Origin: projectOrigin, Locator: "", Bytes: bytesValue, ExpectedDigest: rawDigest(bytesValue), ExpectedMediaType: "image/png", ExpectedByteLength: 5}},
		{{Origin: projectOrigin, Locator: "icon.png", Bytes: bytesValue, ExpectedDigest: rawDigest(bytesValue), ExpectedMediaType: "text/plain", ExpectedByteLength: 5}},
		{{Origin: projectOrigin, Locator: "icon.png", Bytes: bytesValue, ExpectedDigest: rawDigest(bytesValue), ExpectedMediaType: "image/png", ExpectedByteLength: 4}},
		{{Origin: projectOrigin, Locator: "icon.png", Bytes: bytesValue, ExpectedDigest: rawDigest(bytesValue), ExpectedMediaType: "image/png", ExpectedByteLength: 5}, {Origin: projectOrigin, Locator: "copy.png", Bytes: bytesValue, ExpectedDigest: rawDigest(bytesValue), ExpectedMediaType: "image/jpeg", ExpectedByteLength: 5}},
	}
	for _, assets := range assetCases {
		input := assetBase
		input.Resolved.Assets = assets
		if got := Compile(input); !got.HasErrors {
			t.Fatalf("bad asset accepted=%+v", assets)
		}
	}
}

func TestClosedResolvedTreeValidationBranches(t *testing.T) {
	base := packStages(t, packFixture)
	privateSource := []byte("reference private <<-TEXT\nPrivate.\nTEXT\n")
	complete := base
	complete.Resolved.SelectedClosure[0].Files = append(complete.Resolved.SelectedClosure[0].Files, ResolvedPackFile{Path: "private.ldl", Digest: rawDigestBytes(privateSource)})
	complete.Resolved.SourceFiles = append(complete.Resolved.SourceFiles, ResolvedSourceFile{Origin: resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: complete.Resolve.RootAddress}, ModulePath: "private.ldl", Bytes: privateSource})
	if got := Compile(complete); got.HasErrors {
		t.Fatalf("complete unused Pack source rejected=%+v", got.Diagnostics)
	}

	mutations := []func(*Input){
		func(value *Input) { value.Resolved.SelectedClosure[0].Manifest.Name = "" },
		func(value *Input) { value.Resolved.SelectedClosure[0].Files = nil },
		func(value *Input) {
			value.Resolved.SelectedClosure[0].Files = append(value.Resolved.SelectedClosure[0].Files, ResolvedPackFile{})
		},
		func(value *Input) {
			value.Resolved.SelectedClosure[0].Dependencies = []ResolvedPackDependency{{LocalName: "missing", InstallName: "missing", CanonicalID: "missing/pack"}}
		},
		func(value *Input) {
			value.Resolved.SourceFiles = append(value.Resolved.SourceFiles, ResolvedSourceFile{Origin: resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: value.Resolve.RootAddress}, ModulePath: "unexpected.ldl", Bytes: []byte("x")})
		},
	}
	for index, mutate := range mutations {
		input := base
		mutate(&input)
		if got := Compile(input); !got.HasErrors || got.Snapshot().Pack != nil {
			t.Fatalf("invalid closed tree case %d published=%+v", index, got.Snapshot())
		}
	}
	if _, ok := (Result{}).ValidatedInputSnapshot(); ok {
		t.Fatal("rejected result exposed validated input")
	}
	if validSHA256("SHA256:"+strings.Repeat("A", 64)) || validSHA256("sha256:"+strings.Repeat("A", 64)) || validSourceOrigin(base, resolve.SourceOrigin{Kind: "unknown"}) {
		t.Fatal("invalid closed metadata helper accepted input")
	}
	if _, ok := selectedPackFileDigest(base, resolve.SourceOrigin{Kind: resolve.OriginProject}, "pack.ldl"); ok {
		t.Fatal("Project origin resolved as a Pack file")
	}
	if _, ok := selectedPackFileDigest(base, resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: base.Resolve.RootAddress}, "missing.ldl"); ok {
		t.Fatal("missing Pack file resolved")
	}
	if validSourceOrigin(base, resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: "missing"}) {
		t.Fatal("unknown Pack origin accepted")
	}
	if lessAddress(base.Resolve, "unknown-z", base.Resolve.RootAddress) || !lessAddress(base.Resolve, base.Resolve.RootAddress, "unknown-z") {
		t.Fatal("mixed fallback address ordering is inconsistent")
	}
	subjectFieldSeal{}.isSubjectFields()
}

func TestInternalProjectionOrderingAndErrorHelpers(t *testing.T) {
	if _, _, _, err := buildHashPayloads(Input{}, nil, nil); err == nil {
		t.Fatal("open hash envelope accepted")
	}
	if _, err := appendCanonicalValue(struct{}{}); err == nil {
		t.Fatal("unsupported canonical value accepted")
	}
	if err := appendCanonicalString(&bytes.Buffer{}, string([]byte{0xff})); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	if _, err := Canonicalize(map[string]string{"é": "one", "e\u0301": "two"}); err == nil {
		t.Fatal("NFC duplicate key accepted")
	}

	set := definition.ProjectionSet{Matrix: &definition.MatrixProjection{}, Tree: &definition.TreeProjection{}, Flow: &definition.FlowProjection{}}
	converted := projectionSet(set)
	if converted.Matrix == nil || converted.Tree == nil || converted.Flow == nil || matrixProjection(nil) != nil || treeProjection(nil) != nil || flowProjection(nil) != nil {
		t.Fatal("projection pointer conversion failed")
	}
	traversal := query.Traversal{Direction: definition.TraversalBoth}
	if queryTraversal(&traversal) == nil || queryTraversal(nil) != nil {
		t.Fatal("traversal conversion failed")
	}
	diff := view.Source{Kind: view.SourceDiff, Diff: &view.DiffSource{Before: "a", After: "b", Arguments: []view.Argument{{ParameterAddress: "p", Value: definition.Scalar{Type: definition.ScalarBoolean, Bool: true}}}}}
	if got := viewSource(diff); got.Before == nil || got.QueryAddress != nil || !got.Arguments["p"].Bool {
		t.Fatalf("Diff source=%+v", got)
	}
	projectionViews := views([]view.Recipe{{ID: "v", Address: "v", Source: view.Source{Kind: view.SourceQuery, Query: &view.QuerySource{QueryAddress: "q"}}, Shape: view.Shape{Kind: view.ShapeDiagram, Diagram: &view.DiagramShape{Placements: []view.Placement{}}}, RelationProjections: []view.RelationProjection{{RelationTypeAddress: "r", Projections: set}}}})
	if len(projectionViews) != 1 || len(projectionViews[0].RelationProjections) != 1 {
		t.Fatal("View projection conversion failed")
	}

	history := definition.IdentityHistory{RootReservations: map[string]map[resolve.SubjectKind][]string{"root": {resolve.KindEntity: {"z", "a"}}}, Moves: []definition.Move{{Kind: resolve.KindEntity, OldAddress: "root:entity:a", NewAddress: "root:entity:b"}}, MoveClosure: []definition.MoveResolution{{Kind: resolve.KindEntity, SourceAddress: "root:entity:a", TerminalAddress: "root:entity:b"}}}
	got, identityErr := (normalizer{}).identity(history)
	if identityErr != nil || len(got.Moves) != 1 || len(rootMoves(got.Moves, "root")) != 1 || len(rootMoveClosure(got.MoveClosure, "root")) != 1 || len(rootMoves(got.Moves, "other")) != 0 {
		t.Fatalf("identity projection=%+v", got)
	}

	n := normalizer{symbols: map[string]resolve.StableSymbol{}}
	document := NormalizedDocument{EntityTypes: []EntityType{{Address: "z", UniqueConstraints: []UniqueConstraint{{Address: "z"}, {Address: "a"}}}, {Address: "a"}}, RelationTypes: []RelationType{{Address: "z", UniqueConstraints: []UniqueConstraint{{Address: "z"}, {Address: "a"}}}, {Address: "a"}}, Layers: []Layer{{Address: "z"}, {Address: "a"}}, Entities: []Entity{{Address: "z"}, {Address: "a"}}, Relations: []Relation{{Address: "z"}, {Address: "a"}}, Queries: []Query{{Address: "z", Parameters: []QueryParameter{{Address: "z"}, {Address: "a"}}}, {Address: "a"}}, Views: []View{{Address: "z", Exports: []ExportRecipe{{Address: "z"}, {Address: "a"}}}, {Address: "a"}}, References: []Reference{{Address: "z"}, {Address: "a"}}}
	n.sortDocument(&document)
	if document.EntityTypes[0].Address != "a" || document.Queries[0].Address != "a" {
		t.Fatal("fallback ordering failed")
	}
	pack := NormalizedPackArtifact{EntityTypes: append([]EntityType{}, document.EntityTypes...), RelationTypes: append([]RelationType{}, document.RelationTypes...), Queries: append([]Query{}, document.Queries...), Views: append([]View{}, document.Views...), References: append([]Reference{}, document.References...)}
	n.sortPack(&pack)
	if kindRank("unknown") != 99 || childKinds(SubjectEntityRow) != nil {
		t.Fatal("closed kind helpers accepted unknown")
	}

	node := &hashNode{address: "owner", ownHash: "sha256:x"}
	nodes := map[string]*hashNode{"owner": node}
	first, err := subtreeHash(node, nodes)
	second, err2 := subtreeHash(node, nodes)
	if err != nil || err2 != nil || first != second {
		t.Fatal("subtree cache failed")
	}

	if !equalClosure([]ResolvedPackSummary{{Address: "a"}}, []ResolvedPackSummary{{Address: "a"}}) || equalClosure(nil, []ResolvedPackSummary{{}}) || equalClosure([]ResolvedPackSummary{{Address: "a"}}, []ResolvedPackSummary{{Address: "b"}}) {
		t.Fatal("closure equality failed")
	}
	duplicateDeclarations := []resolve.DeclarationSymbol{{Address: "a", Kind: resolve.KindQuery}, {Address: "b", Kind: resolve.KindQuery}}
	if sameRecipeAddresses(nil, resolve.KindQuery, []string{"duplicate", "duplicate"}) || sameRecipeAddresses(duplicateDeclarations, resolve.KindQuery, []string{"a", "a"}) {
		t.Fatal("duplicate recipes accepted")
	}
	if got := n.dependencies([]ResolvedPackClosure{{ResolvedPackSummary: ResolvedPackSummary{Address: "z"}}, {ResolvedPackSummary: ResolvedPackSummary{Address: "a"}}}, ""); got[0].Address != "a" {
		t.Fatal("dependency ordering failed")
	}
	if normalizedMap(map[string]string{"e\u0301": "x\r\ny"})["é"] != "x\ny" {
		t.Fatal("annotation normalization failed")
	}

	projectInput := projectStages(t, projectFixture)
	packInput := packStages(t, packFixture)
	projectGate, packGate := compileGate{input: projectInput}, compileGate{input: packInput}
	badPack := newNormalizer(packInput, map[assetKey]AssetBlobSummary{})
	badPack.input.Definition.Project = &definition.Project{}
	if _, err := badPack.pack(); err == nil {
		t.Fatal("Pack accepted a Project root")
	}
	if _, ok := projectGate.moduleSourceKey(projectInput.Resolve.Modules[0]); !ok {
		t.Fatal("Project source key failed")
	}
	if _, ok := packGate.moduleSourceKey(packInput.Resolve.Modules[0]); !ok {
		t.Fatal("Pack root source key failed")
	}
	foreign := packInput.Resolve.Modules[0]
	foreign.Origin = resolve.Origin{Kind: resolve.OriginPack, Publisher: "other", PackName: "dependency"}
	packGate.input.Resolved.SelectedClosure = []ResolvedPackClosure{{ResolvedPackSummary: ResolvedPackSummary{Address: "ldl:pack:other:dependency", CanonicalID: "other/dependency"}}}
	if _, ok := packGate.moduleSourceKey(foreign); !ok {
		t.Fatal("Pack dependency source key failed")
	}
	packGate.input.Resolved.SelectedClosure = nil
	if _, ok := packGate.moduleSourceKey(foreign); ok {
		t.Fatal("unknown Pack source key accepted")
	}

	for index := 0; index < 5; index++ {
		input := Input{}
		diagnostic := []resolve.Diagnostic{{Code: "X"}}
		switch index {
		case 0:
			input.View.Diagnostics = diagnostic
		case 1:
			input.Query.Diagnostics = diagnostic
		case 2:
			input.Graph.Diagnostics = diagnostic
		case 3:
			input.Definition.Diagnostics = diagnostic
		case 4:
			input.Resolve.Diagnostics = diagnostic
		}
		if len(upstreamDiagnostics(input)) != 1 {
			t.Fatal("diagnostic priority failed")
		}
	}
}

func appendCanonicalValue(value any) ([]byte, error) {
	var out bytes.Buffer
	err := appendCanonical(&out, value)
	return out.Bytes(), err
}
func diagnosticMessage(values []resolve.Diagnostic, fragment string) bool {
	for _, value := range values {
		if stringsContains(value.Message, fragment) {
			return true
		}
	}
	return false
}
func stringsReplace(value, old, replacement string) string {
	return strings.Replace(value, old, replacement, 1)
}
func stringsContains(value, fragment string) bool { return strings.Contains(value, fragment) }
func ownHash(value Hashes, address string) string {
	for _, item := range value.OwnSubjects {
		if item.Address == address {
			return item.Hash
		}
	}
	return ""
}
func subtreeHashValue(value Hashes, address string) string {
	for _, item := range value.Subtrees {
		if item.OwnerAddress == address {
			return item.Hash
		}
	}
	return ""
}
func childSetHash(value Hashes, owner string, kind SubjectKind) string {
	for _, item := range value.ChildSets {
		if item.OwnerAddress == owner && item.ChildKind == kind {
			return item.Hash
		}
	}
	return ""
}
