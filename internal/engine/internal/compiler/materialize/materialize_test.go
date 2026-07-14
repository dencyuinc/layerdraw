// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package materialize

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

func TestCompileProjectEnvelopeCompletenessAndDefensiveSnapshots(t *testing.T) {
	input := projectStages(t, projectFixture)
	result := Compile(input)
	if result.HasErrors {
		t.Fatalf("Compile() diagnostics=%+v", result.Diagnostics)
	}
	snapshot := result.Snapshot()
	if snapshot.Document == nil || snapshot.Pack != nil || snapshot.Hashes.Graph == nil {
		t.Fatalf("Project snapshot=%+v", snapshot)
	}
	if snapshot.Document.Format != NormalizedFormat || snapshot.Document.SchemaVersion != 1 || snapshot.Document.Language != 1 || !bytes.HasSuffix(snapshot.ArtifactJSON, []byte("\n")) || bytes.HasSuffix(snapshot.CanonicalJSON, []byte("\n")) {
		t.Fatal("invalid Project envelope or canonical newline contract")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(snapshot.CanonicalJSON, &envelope); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"assets", "dependencies", "entities", "entity_types", "format", "identity", "language", "layers", "project", "queries", "references", "relation_types", "relations", "schema_version", "views"}
	gotKeys := make([]string, 0, len(envelope))
	for key := range envelope {
		gotKeys = append(gotKeys, key)
	}
	if !sameStringSet(gotKeys, wantKeys) {
		t.Fatalf("Project keys=%v", gotKeys)
	}
	if len(snapshot.Hashes.OwnSubjects) == 0 || len(snapshot.Hashes.Subtrees) == 0 || len(snapshot.Hashes.ChildSets) == 0 {
		t.Fatalf("incomplete hashes=%+v", snapshot.Hashes)
	}
	if len(snapshot.Hashes.OwnSubjects) != 19 {
		t.Fatalf("own subject count=%d, want 19", len(snapshot.Hashes.OwnSubjects))
	}

	snapshot.Document.Project.DisplayName = "mutated"
	snapshot.CanonicalJSON[0] = '['
	snapshot.Hashes.OwnSubjects[0].Hash = "mutated"
	again := result.Snapshot()
	if again.Document.Project.DisplayName == "mutated" || again.CanonicalJSON[0] != '{' || again.Hashes.OwnSubjects[0].Hash == "mutated" {
		t.Fatal("Snapshot leaked mutable storage")
	}
	input.Resolved.SourceFiles[0].Bytes[0] = 'X'
	validated, ok := result.ValidatedInputSnapshot()
	if !ok || validated.Resolved.SourceFiles[0].Bytes[0] == 'X' {
		t.Fatal("validated input snapshot aliased caller storage")
	}
	validated.Resolved.SourceFiles[0].Bytes[0] = 'Y'
	validatedAgain, _ := result.ValidatedInputSnapshot()
	if validatedAgain.Resolved.SourceFiles[0].Bytes[0] == 'Y' {
		t.Fatal("validated input snapshots aliased each other")
	}
	var wait sync.WaitGroup
	for i := 0; i < 32; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			copy := result.Snapshot()
			if copy.Document == nil {
				t.Error("concurrent Snapshot returned empty")
			}
		}()
	}
	wait.Wait()
}

func TestCompilePackExactEnvelopeAndNoGraphHash(t *testing.T) {
	input := packStages(t, packFixture)
	result := Compile(input)
	if result.HasErrors {
		t.Fatalf("Compile() diagnostics=%+v", result.Diagnostics)
	}
	snapshot := result.Snapshot()
	if snapshot.Pack == nil || snapshot.Document != nil || snapshot.Hashes.Graph != nil {
		t.Fatalf("Pack snapshot=%+v", snapshot)
	}
	if len(snapshot.Pack.Dependencies) != 0 || duplicateSubjectHash(snapshot.Hashes.OwnSubjects) {
		t.Fatalf("Pack root leaked into dependencies or hashes=%+v", snapshot.Hashes.OwnSubjects)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(snapshot.CanonicalJSON, &envelope); err != nil {
		t.Fatal(err)
	}
	want := []string{"assets", "dependencies", "entity_types", "format", "identity", "language", "pack", "queries", "references", "relation_types", "schema_version", "views"}
	keys := []string{}
	for key := range envelope {
		keys = append(keys, key)
	}
	if !sameStringSet(keys, want) {
		t.Fatalf("Pack keys=%v", keys)
	}
	for _, forbidden := range []string{"project", "layers", "entities", "relations"} {
		if _, exists := envelope[forbidden]; exists {
			t.Fatalf("Pack fabricated %q", forbidden)
		}
	}
}

func duplicateSubjectHash(values []SubjectHash) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value.Address] {
			return true
		}
		seen[value.Address] = true
	}
	return false
}

func TestNormalizedArtifactGoldenDigests(t *testing.T) {
	project := Compile(projectStages(t, projectFixture)).Snapshot()
	pack := Compile(packStages(t, packFixture)).Snapshot()
	if got, want := rawDigest(project.CanonicalJSON), goldenDigest(t, "project.golden.sha256"); got != want {
		t.Fatalf("project golden digest=%s", got)
	}
	if got, want := rawDigest(pack.CanonicalJSON), goldenDigest(t, "pack.golden.sha256"); got != want {
		t.Fatalf("pack golden digest=%s", got)
	}
}

func TestCompilePermutationAndCommentFormattingHashIsolation(t *testing.T) {
	baselineInput := projectStages(t, projectFixture)
	baseline := Compile(baselineInput)
	if baseline.HasErrors {
		t.Fatal(baseline.Diagnostics)
	}
	permuted := baselineInput
	reverseDefinitions(&permuted)
	permutation := Compile(permuted)
	if permutation.HasErrors {
		t.Fatal(permutation.Diagnostics)
	}
	if !bytes.Equal(baseline.Snapshot().CanonicalJSON, permutation.Snapshot().CanonicalJSON) || !reflect.DeepEqual(publicHashes(baseline.Snapshot().Hashes), publicHashes(permutation.Snapshot().Hashes)) {
		t.Fatal("stage slice permutation changed output")
	}

	changed := strings.Replace(projectFixture, "/// Service docs.\nentity_type", "// formatting-only\r\n/// Service docs.\r\nentity_type", 1)
	isolated := Compile(projectStages(t, changed))
	if isolated.HasErrors {
		t.Fatal(isolated.Diagnostics)
	}
	if !bytes.Equal(baseline.Snapshot().CanonicalJSON, isolated.Snapshot().CanonicalJSON) || !reflect.DeepEqual(publicHashes(baseline.Snapshot().Hashes), publicHashes(isolated.Snapshot().Hashes)) {
		t.Fatal("comment/formatting changed semantic output")
	}
}

func TestCompileClosedInputAndTransactionalFailures(t *testing.T) {
	input := projectStages(t, projectFixture)
	input.Resolved.SourceFiles[0].Bytes = []byte("different")
	rejected := Compile(input)
	if !rejected.HasErrors || rejected.Snapshot().Document != nil || rejected.Snapshot().CanonicalJSON != nil {
		t.Fatalf("source mismatch published partial result=%+v", rejected.Snapshot())
	}

	input = projectStages(t, assetFixture)
	assetBytes := encodedPNG(t)
	input.Resolved.Assets = []ResolvedAsset{{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, Locator: "icon.png", Bytes: assetBytes, ExpectedDigest: rawDigest(assetBytes), ExpectedMediaType: "image/png", ExpectedByteLength: int64(len(assetBytes))}}
	accepted := Compile(input)
	if accepted.HasErrors || len(accepted.Snapshot().Document.Assets) != 1 {
		t.Fatalf("closed asset diagnostics=%+v snapshot=%+v", accepted.Diagnostics, accepted.Snapshot())
	}
	input.Resolved.Assets[0].ExpectedDigest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if got := Compile(input); !got.HasErrors || got.Snapshot().Document != nil {
		t.Fatalf("asset mismatch published=%+v", got.Snapshot())
	}

	stale := projectStages(t, projectFixture)
	other := projectStages(t, strings.Replace(projectFixture, "Project", "Other", 1))
	stale.Query = other.Query
	if got := Compile(stale); !got.HasErrors || got.MatchesResolve(stale.Resolve) || got.Snapshot().Document != nil {
		t.Fatalf("stale generation accepted=%+v", got)
	}
}

func TestCompileImportedPackUsesCompleteOriginQualifiedClosure(t *testing.T) {
	projectSource := "import { service } from \"schema\"\nproject p \"Project\" {}\nentity_type local \"Local\" {\n  image \"icon.svg\"\n  representation shape rect\n}\n"
	packSource := "entity_type service \"Service\" {\n  image \"icon.svg\"\n  representation shape rect\n}\nexport { service }\n"
	assetBytes := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="1" height="1"/></svg>`)
	projectAssetBytes := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><circle r="1"/></svg>`)
	pack := resolve.ResolvedPack{CanonicalID: "pub/schema", Version: "1.0.0", Digest: "sha256:" + strings.Repeat("a", 64), Path: "pack/schema", Entry: "pack.ldl", Files: map[string]string{"pack.ldl": rawDigest([]byte(packSource)), "icon.svg": rawDigest(assetBytes)}, Manifest: resolve.PackManifest{Format: "layerdraw-pack", FormatVersion: 1, ID: "pub/schema", Name: "schema", Version: "1.0.0", Language: 1, Entry: "pack.ldl"}, SourceFiles: map[string]resolve.SourceFile{"pack.ldl": resolve.SourceFromParse(syntax.Parse([]byte(packSource)))}}
	resolved := resolve.Resolve(resolve.Input{Mode: resolve.CompileProject, EntryPath: "document.ldl", Project: resolve.ProjectInput{Files: map[string]resolve.SourceFile{"document.ldl": resolve.SourceFromParse(syntax.Parse([]byte(projectSource)))}}, Packs: resolve.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]resolve.ResolvedPack{"schema": pack}}})
	closure := testPackClosure(t, resolved, pack)
	metadata := ResolvedMetadata{SelectedClosure: []ResolvedPackClosure{closure}, SourceFiles: []ResolvedSourceFile{{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: "document.ldl", Bytes: []byte(projectSource)}, {Origin: resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: closure.Address}, ModulePath: "pack.ldl", Bytes: []byte(packSource)}}, Assets: []ResolvedAsset{{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, Locator: "icon.svg", Bytes: projectAssetBytes, ExpectedDigest: rawDigest(projectAssetBytes), ExpectedMediaType: "image/svg+xml", ExpectedByteLength: int64(len(projectAssetBytes))}, {Origin: resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: closure.Address}, Locator: "icon.svg", Bytes: assetBytes, ExpectedDigest: rawDigest(assetBytes), ExpectedMediaType: "image/svg+xml", ExpectedByteLength: int64(len(assetBytes))}}}
	input := stages(t, resolved, metadata)
	result := Compile(input)
	if result.HasErrors || len(result.Snapshot().Document.Dependencies) != 1 || len(result.Snapshot().Document.Assets) != 2 {
		t.Fatalf("closed Pack materialization=%+v diagnostics=%+v", result.Snapshot(), result.Diagnostics)
	}
	baseSnapshot := result.Snapshot()
	resolutionChanged := deepClone(input)
	resolutionChanged.Resolve.Dependencies[0].Version = "1.0.1"
	resolutionChanged.Resolve.Dependencies[0].Digest = "sha256:" + strings.Repeat("b", 64)
	resolutionChanged.Resolved.SelectedClosure[0].Version = "1.0.1"
	resolutionChanged.Resolved.SelectedClosure[0].Digest = "sha256:" + strings.Repeat("b", 64)
	resolutionChanged.Resolved.SelectedClosure[0].Manifest.Version = "1.0.1"
	resolutionChanged.Resolved.SelectedClosure[0].Manifest.Bytes = testManifestBytes(resolutionChanged.Resolved.SelectedClosure[0].Manifest)
	changed := Compile(resolutionChanged)
	changedSnapshot := changed.Snapshot()
	if changed.HasErrors || bytes.Equal(baseSnapshot.CanonicalJSON, changedSnapshot.CanonicalJSON) || baseSnapshot.Hashes.Definition != changedSnapshot.Hashes.Definition || *baseSnapshot.Hashes.Graph != *changedSnapshot.Hashes.Graph || ownHash(baseSnapshot.Hashes, closure.Address) != ownHash(changedSnapshot.Hashes, closure.Address) {
		t.Fatalf("resolution metadata leaked into semantic hashes: base=%+v changed=%+v diagnostics=%+v", baseSnapshot.Hashes, changedSnapshot.Hashes, changed.Diagnostics)
	}
	pathChanged := deepClone(input)
	pathChanged.Resolved.SelectedClosure[0].Path = "vendor/relocated-schema"
	if relocated := Compile(pathChanged); relocated.HasErrors || !bytes.Equal(baseSnapshot.CanonicalJSON, relocated.Snapshot().CanonicalJSON) || !reflect.DeepEqual(publicHashes(baseSnapshot.Hashes), publicHashes(relocated.Snapshot().Hashes)) {
		t.Fatal("installed Pack path changed normalized semantics")
	}
	input.Resolved.SelectedClosure[0].Files[1].Digest = "sha256:" + strings.Repeat("f", 64)
	if rejected := Compile(input); !rejected.HasErrors || rejected.Snapshot().Document != nil {
		t.Fatalf("mismatched resolved asset tree published=%+v", rejected.Snapshot())
	}
}

func encodedPNG(t *testing.T) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 1, 1))
	picture.Set(0, 0, color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func projectStages(t *testing.T, source string) Input {
	t.Helper()
	parsed := resolve.SourceFromParse(syntax.Parse([]byte(source)))
	resolved := resolve.Resolve(resolve.Input{Mode: resolve.CompileProject, EntryPath: "document.ldl", Project: resolve.ProjectInput{Files: map[string]resolve.SourceFile{"document.ldl": parsed}}})
	return stages(t, resolved, ResolvedMetadata{SourceFiles: []ResolvedSourceFile{{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: "document.ldl", Bytes: []byte(source)}}})
}

func packStages(t *testing.T, source string) Input {
	t.Helper()
	parsed := resolve.SourceFromParse(syntax.Parse([]byte(source)))
	pack := resolve.ResolvedPack{CanonicalID: "pub/core", Version: "1.0.0", Digest: "sha256:" + strings.Repeat("a", 64), Path: "pack/pub-core", Entry: "pack.ldl", Files: map[string]string{"pack.ldl": rawDigest([]byte(source))}, Manifest: resolve.PackManifest{Format: "layerdraw-pack", FormatVersion: 1, ID: "pub/core", Name: "core", Version: "1.0.0", Language: 1, Entry: "pack.ldl"}, SourceFiles: map[string]resolve.SourceFile{"pack.ldl": parsed}}
	resolved := resolve.Resolve(resolve.Input{Mode: resolve.CompilePack, RootPackID: "pub/core", EntryPath: "pack.ldl", Packs: resolve.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]resolve.ResolvedPack{"root": pack}}})
	metadata := ResolvedMetadata{SelectedClosure: []ResolvedPackClosure{testPackClosure(t, resolved, pack)}, SourceFiles: []ResolvedSourceFile{{Origin: resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: resolved.RootAddress}, ModulePath: "pack.ldl", Bytes: []byte(source)}}}
	return stages(t, resolved, metadata)
}

func stages(t *testing.T, resolved resolve.Result, metadata ResolvedMetadata) Input {
	t.Helper()
	defined := definition.Compile(definition.Input{Resolve: resolved})
	graphed := graph.Compile(graph.Input{Resolve: resolved, Definition: defined})
	queried := query.Compile(query.Input{Resolve: resolved, Definition: defined, Graph: graphed})
	viewed := view.Compile(view.Input{Resolve: resolved, Definition: defined, Graph: graphed, Query: queried})
	if resolved.HasErrors || defined.HasErrors || graphed.HasErrors || queried.HasErrors || viewed.HasErrors {
		t.Fatalf("fixture failed: resolve=%+v definition=%+v graph=%+v query=%+v view=%+v", resolved.Diagnostics, defined.Diagnostics, graphed.Diagnostics, queried.Diagnostics, viewed.Diagnostics)
	}
	return Input{Resolve: resolved, Definition: defined, Graph: graphed, Query: queried, View: viewed, Resolved: metadata}
}

func testPackClosure(t *testing.T, resolved resolve.Result, pack resolve.ResolvedPack) ResolvedPackClosure {
	t.Helper()
	var summary ResolvedPackSummary
	for _, dependency := range resolved.Dependencies {
		if dependency.CanonicalID == pack.CanonicalID {
			summary = ResolvedPackSummary{Address: dependency.Address, CanonicalID: dependency.CanonicalID, Version: dependency.Version, Digest: dependency.Digest}
		}
	}
	if summary.Address == "" {
		t.Fatalf("missing selected pack %s", pack.CanonicalID)
	}
	files := make([]ResolvedPackFile, 0, len(pack.Files))
	for path, digest := range pack.Files {
		files = append(files, ResolvedPackFile{Path: path, Digest: digest})
	}
	manifest := ResolvedPackManifest{Format: pack.Manifest.Format, FormatVersion: pack.Manifest.FormatVersion, ID: pack.Manifest.ID, Name: pack.Manifest.Name, Version: pack.Manifest.Version, Language: pack.Manifest.Language, Entry: pack.Manifest.Entry, Path: "manifest.json"}
	manifest.Bytes = testManifestBytes(manifest)
	return ResolvedPackClosure{ResolvedPackSummary: summary, Path: pack.Path, Entry: pack.Entry, Files: files, Manifest: manifest}
}

func testManifestBytes(manifest ResolvedPackManifest) []byte {
	value, err := json.Marshal(manifestBasics{Format: manifest.Format, FormatVersion: manifest.FormatVersion, ID: manifest.ID, Name: manifest.Name, Version: manifest.Version, Language: manifest.Language, Entry: manifest.Entry})
	if err != nil {
		panic(err)
	}
	return value
}

func reverseDefinitions(input *Input) {
	reverse(input.Definition.EntityTypes)
	reverse(input.Definition.RelationTypes)
	reverse(input.Definition.Layers)
	reverse(input.Definition.References)
	reverse(input.Graph.Graph.Entities)
	reverse(input.Graph.Graph.Relations)
	reverse(input.Query.Recipes)
	reverse(input.View.Recipes)
	reverse(input.View.ExportRecipes.Recipes)
}

func reverse[T any](values []T) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
func publicHashes(value Hashes) Hashes { value.generation = resolve.StageGeneration{}; return value }
func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	values := map[string]int{}
	for _, item := range left {
		values[item]++
	}
	for _, item := range right {
		values[item]--
	}
	for _, count := range values {
		if count != 0 {
			return false
		}
	}
	return true
}

func rawDigest(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func goldenDigest(t *testing.T, name string) string {
	t.Helper()
	value, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(value))
}

const projectFixture = `project p "Project" {
  description "Root description"
  tags [root]
}

layers {
  app "Application" @10
}

/// Service docs.
entity_type service "Service" {
  representation shape rect
  columns {
    environment "Environment" enum [prod, dev] required default prod
    note "Note" string
  }
  unique env_unique [environment]
}

relation_type link "Link" dependency {
  duplicate_policy allow
  from source types [service] layers [app]
  to target types [service] layers [app]
  label "links"
  columns {
    weight "Weight" number
  }
}

entities service @app {
  alpha "Alpha" {
    tags [critical]
  }
  beta "Beta"
}

rows service [environment, note] {
  alpha primary: prod, "api"
}

relations link {
  alpha_beta: alpha -> beta "Alpha to Beta"
}

relation_rows link [weight] {
  alpha_beta primary: 1.5
}

query scope "Scope" {
  parameters {
    environment enum [prod, dev] default prod
  }
  select {
    entity_types [service]
    relation_types [link]
    roots [alpha]
  }
  result [seed_entities, induced_relations]
}

view inventory "Inventory" inventory {
  intent "Find service inventory"
  source query scope {}
  table {
    rows entity_rows
    entity_types [service]
    entity_id
    column environment {
      source attribute environment entity_types [service]
    }
    sort environment ascending nulls last
  }
  export data json "inventory.json" {
    fidelity lossless
    source_refs
    diagnostics
  }
}

reference guide <<-TEXT
Use the graph as the source of truth.
TEXT
`

const packFixture = `entity_type service "Service" {
  representation shape rect
  columns {
    name "Name" string
  }
}
query all "All" {
  select {
    entity_types [service]
  }
}
view catalog "Catalog" inventory {
  source query all {}
  table {
    rows entity
  }
}
reference guide <<-TEXT
Pack guidance.
TEXT
export { service, all, catalog, guide }
`

const assetFixture = `project p "Project" {}
entity_type service "Service" {
  image "icon.png"
  representation shape rect
}
`
