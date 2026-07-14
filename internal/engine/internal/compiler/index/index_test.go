// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package index

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

func TestBuildCompleteSourceSemanticAndSearchIndexes(t *testing.T) {
	input := indexProject(t, indexFixture)
	result := Build(input)
	if result.HasErrors {
		t.Fatalf("Build() diagnostics=%+v", result.Diagnostics)
	}
	snapshot := result.Snapshot()
	materialized := input.Materialized.Snapshot()
	if snapshot.SourceMap.SchemaVersion != 1 || snapshot.SemanticIndex.SchemaVersion != 1 || len(snapshot.SourceMap.Files) != 1 {
		t.Fatalf("index versions/files=%+v", snapshot)
	}
	if len(snapshot.SourceMap.Subjects) != len(materialized.Hashes.OwnSubjects) || len(snapshot.SemanticIndex.Subjects) != len(materialized.Hashes.OwnSubjects) {
		t.Fatalf("subject completeness source=%d semantic=%d hashes=%d", len(snapshot.SourceMap.Subjects), len(snapshot.SemanticIndex.Subjects), len(materialized.Hashes.OwnSubjects))
	}
	sourceAddresses, semanticAddresses := map[string]bool{}, map[string]bool{}
	for _, subject := range snapshot.SourceMap.Subjects {
		if sourceAddresses[subject.Address] {
			t.Fatalf("duplicate SourceMap subject=%s", subject.Address)
		}
		sourceAddresses[subject.Address] = true
		if subject.Kind != materialize.SubjectPack && (subject.Module == nil || subject.DeclarationRange == nil) {
			t.Fatalf("subject lacks exact source=%+v", subject)
		}
	}
	for _, subject := range snapshot.SemanticIndex.Subjects {
		if semanticAddresses[subject.Address] {
			t.Fatalf("duplicate SemanticIndex subject=%s", subject.Address)
		}
		semanticAddresses[subject.Address] = true
	}
	for _, hash := range materialized.Hashes.OwnSubjects {
		if !sourceAddresses[hash.Address] || !semanticAddresses[hash.Address] {
			t.Fatalf("published subject missing from indexes=%s", hash.Address)
		}
	}
	service := sourceSubject(t, snapshot.SourceMap, "ldl:project:p:entity-type:service")
	if len(service.CommentRanges) != 1 || service.CommentRanges[0].EndByte <= service.CommentRanges[0].StartByte {
		t.Fatalf("doc comment ranges=%+v", service.CommentRanges)
	}
	if len(snapshot.SourceMap.Bindings) == 0 || len(snapshot.SemanticIndex.References) == 0 || len(snapshot.SemanticIndex.Rows) != 2 || len(snapshot.SemanticIndex.Columns) != 2 {
		t.Fatalf("bindings/owner indexes incomplete=%+v", snapshot.SemanticIndex)
	}
	if len(snapshot.SemanticIndex.Adjacency) != 2 || len(snapshot.SemanticIndex.Dependencies) != 2 || len(snapshot.SemanticIndex.ScopedReads.ByModule) != 1 || len(snapshot.SemanticIndex.ScopedReads.ColumnsByOwner) != 2 || len(snapshot.SemanticIndex.ScopedReads.UsagesByTarget) == 0 {
		t.Fatalf("semantic indexes incomplete=%+v", snapshot.SemanticIndex)
	}
	if len(snapshot.SearchDocuments) != 11 {
		t.Fatalf("SearchDocuments=%d, want 11", len(snapshot.SearchDocuments))
	}
	relation := searchDocument(t, snapshot.SearchDocuments, "ldl:project:p:relation:alpha_beta")
	wantEntries := []string{"ldl:project:p:entity:alpha", "ldl:project:p:entity:beta"}
	if !reflect.DeepEqual(relation.GraphEntryAddresses, wantEntries) || relation.ContentHash == "" {
		t.Fatalf("relation SearchDocument=%+v", relation)
	}
	row := searchDocument(t, snapshot.SearchDocuments, "ldl:project:p:entity:alpha:row:primary")
	if row.OwnerAddress == nil || *row.OwnerAddress != "ldl:project:p:entity:alpha" || !reflect.DeepEqual(row.GraphEntryAddresses, []string{"ldl:project:p:entity:alpha"}) {
		t.Fatalf("row SearchDocument=%+v", row)
	}
	for _, document := range snapshot.SearchDocuments {
		for _, field := range document.Fields {
			if field.SourceRef == nil || !field.IncludeInEmbedding {
				t.Fatalf("deterministic field contract=%+v", field)
			}
		}
	}

	snapshot.SourceMap.Files[0].Digest = "mutated"
	snapshot.SearchDocuments[0].Fields[0].Text = "mutated"
	snapshot.SemanticIndex.Subjects[0].OwnHash = "mutated"
	again := result.Snapshot()
	if again.SourceMap.Files[0].Digest == "mutated" || again.SearchDocuments[0].Fields[0].Text == "mutated" || again.SemanticIndex.Subjects[0].OwnHash == "mutated" {
		t.Fatal("Snapshot leaked mutable index storage")
	}
	var wait sync.WaitGroup
	for i := 0; i < 32; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if len(result.Snapshot().SourceMap.Subjects) == 0 {
				t.Error("concurrent Snapshot empty")
			}
		}()
	}
	wait.Wait()
}

func TestInternalIndexGoldenDigest(t *testing.T) {
	snapshot := Build(indexProject(t, indexFixture)).Snapshot()
	canonical, err := materialize.Canonicalize(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	golden, readErr := os.ReadFile(filepath.Join("testdata", "index.golden.sha256"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got, want := sourceDigest(canonical), strings.TrimSpace(string(golden)); got != want {
		t.Fatalf("index golden digest=%s", got)
	}
}

func TestSourceMapRetainsValidatedAssetOriginAndSource(t *testing.T) {
	source := "project p \"Project\" {}\nentity_type service \"Service\" {\n  image \"icon.svg\"\n  representation shape rect\n}\n"
	parsed := resolve.SourceFromParse(syntax.Parse([]byte(source)))
	resolved := resolve.Resolve(resolve.Input{Mode: resolve.CompileProject, EntryPath: "document.ldl", Project: resolve.ProjectInput{Files: map[string]resolve.SourceFile{"document.ldl": parsed}}})
	asset := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`)
	metadata := materialize.ResolvedMetadata{SourceFiles: []materialize.ResolvedSourceFile{{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: "document.ldl", Bytes: []byte(source)}}, Assets: []materialize.ResolvedAsset{{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, Locator: "icon.svg", Bytes: asset, ExpectedDigest: sourceDigest(asset), ExpectedMediaType: "image/svg+xml", ExpectedByteLength: int64(len(asset))}}}
	snapshot := Build(indexStages(t, resolved, metadata)).Snapshot()
	if len(snapshot.SourceMap.Assets) != 1 {
		t.Fatalf("asset source records=%+v", snapshot.SourceMap.Assets)
	}
	record := snapshot.SourceMap.Assets[0]
	if record.SubjectAddress != "ldl:project:p:entity-type:service" || record.Origin.Kind != resolve.OriginProject || record.AuthoredPath != "icon.svg" || record.Range.EndByte <= record.Range.StartByte || record.Digest != sourceDigest(asset) {
		t.Fatalf("asset source record=%+v", record)
	}
}

func TestSearchDocumentV1GoldenFieldAndContentRules(t *testing.T) {
	snapshot := Build(indexProject(t, indexFixture)).Snapshot()
	reference := searchDocument(t, snapshot.SearchDocuments, "ldl:project:p:reference:guide")
	if SearchWeightIdentity != 100 || SearchWeightTaxonomy != 80 || SearchWeightDescription != 60 || SearchWeightAttribute != 40 || SearchWeightGuidance != 20 {
		t.Fatal("Search v1 weights changed without a schema version change")
	}
	wantFields := []SearchField{{FieldPath: "id", Text: "guide", LexicalWeight: SearchWeightIdentity, IncludeInEmbedding: true}, {FieldPath: "text", Text: "Use the graph as the source of truth.\n", LexicalWeight: SearchWeightGuidance, IncludeInEmbedding: true}}
	if len(reference.Fields) != len(wantFields) {
		t.Fatalf("Reference fields=%+v", reference.Fields)
	}
	for index, want := range wantFields {
		got := reference.Fields[index]
		got.SourceRef = nil
		if !reflect.DeepEqual(got, want) || reference.Fields[index].SourceRef == nil {
			t.Fatalf("Reference field %d=%+v", index, reference.Fields[index])
		}
	}
	const wantContentHash = "sha256:aaad1d31adbfe049244371551e3f66bcca9bb53481c2bfcb068177043659c6f8"
	if reference.ContentHash != wantContentHash {
		t.Fatalf("Reference content hash=%s", reference.ContentHash)
	}

	entityType := searchDocument(t, snapshot.SearchDocuments, "ldl:project:p:entity-type:service")
	if len(entityType.Fields) < 3 || entityType.Fields[2].SourceRef == nil || entityType.Fields[0].SourceRef == nil || entityType.Fields[2].SourceRef.StartByte == entityType.Fields[0].SourceRef.StartByte {
		t.Fatalf("column field source was not narrowed to its owned child=%+v", entityType.Fields)
	}
}

func TestBuildDeterministicAndSourceDigestIsolated(t *testing.T) {
	baseInput := indexProject(t, indexFixture)
	base := Build(baseInput)
	changedSource := strings.Replace(indexFixture, "/// Service docs.", "/// Different documentation comment.", 1)
	changed := Build(indexProject(t, changedSource))
	if base.HasErrors || changed.HasErrors {
		t.Fatalf("base=%+v changed=%+v", base.Diagnostics, changed.Diagnostics)
	}
	baseSnapshot, changedSnapshot := base.Snapshot(), changed.Snapshot()
	if baseSnapshot.SourceMap.Files[0].Digest == changedSnapshot.SourceMap.Files[0].Digest {
		t.Fatal("comment change did not change exact source digest")
	}
	if !reflect.DeepEqual(searchHashes(baseSnapshot.SearchDocuments), searchHashes(changedSnapshot.SearchDocuments)) {
		t.Fatal("comment change changed SearchDocument content hashes")
	}
	if !reflect.DeepEqual(semanticHashes(baseSnapshot.SemanticIndex.Subjects), semanticHashes(changedSnapshot.SemanticIndex.Subjects)) {
		t.Fatal("comment change changed semantic hashes")
	}

	permuted := validatedStages(t, baseInput)
	reverse(permuted.Resolve.Bindings)
	reverse(permuted.Query.Recipes)
	reverse(permuted.View.Recipes)
	reverse(permuted.Graph.Graph.Outgoing)
	reverse(permuted.Graph.Graph.Incoming)
	got := Build(Input{Materialized: materialize.Compile(permuted)})
	if got.HasErrors || !reflect.DeepEqual(baseSnapshot, got.Snapshot()) {
		t.Fatalf("permutation changed index\nbase=%+v\ngot=%+v", baseSnapshot, got.Snapshot())
	}
}

func TestBuildPackHasNoSearchDocumentsAndFailuresAreTransactional(t *testing.T) {
	pack := indexPack(t, packIndexFixture)
	result := Build(pack)
	if result.HasErrors || result.Snapshot().SearchDocuments != nil {
		t.Fatalf("Pack search contract result=%+v diagnostics=%+v", result.Snapshot(), result.Diagnostics)
	}
	if len(result.Snapshot().SourceMap.Subjects) == 0 || !result.Snapshot().SourceMap.Subjects[0].ManifestRoot || duplicateSourceSubject(result.Snapshot().SourceMap.Subjects) {
		t.Fatalf("Pack root source sentinel=%+v", result.Snapshot().SourceMap.Subjects)
	}

	rejected := Build(Input{})
	if !rejected.HasErrors || len(rejected.Snapshot().SourceMap.Subjects) != 0 || len(rejected.Snapshot().SemanticIndex.Subjects) != 0 {
		t.Fatalf("stale result published partial index=%+v", rejected.Snapshot())
	}

	failedStages := validatedStages(t, indexProject(t, indexFixture))
	failedStages.Query.HasErrors = true
	if got := Build(Input{Materialized: materialize.Compile(failedStages)}); !got.HasErrors || len(got.Snapshot().SearchDocuments) != 0 {
		t.Fatalf("failed parent published=%+v", got.Snapshot())
	}
}

func duplicateSourceSubject(values []SourceSubjectRecord) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value.Address] {
			return true
		}
		seen[value.Address] = true
	}
	return false
}

func indexProject(t *testing.T, source string) Input {
	t.Helper()
	parsed := resolve.SourceFromParse(syntax.Parse([]byte(source)))
	resolved := resolve.Resolve(resolve.Input{Mode: resolve.CompileProject, EntryPath: "document.ldl", Project: resolve.ProjectInput{Files: map[string]resolve.SourceFile{"document.ldl": parsed}}})
	metadata := materialize.ResolvedMetadata{SourceFiles: []materialize.ResolvedSourceFile{{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: "document.ldl", Bytes: []byte(source)}}}
	return indexStages(t, resolved, metadata)
}

func indexPack(t *testing.T, source string) Input {
	t.Helper()
	parsed := resolve.SourceFromParse(syntax.Parse([]byte(source)))
	pack := resolve.ResolvedPack{CanonicalID: "pub/core", Version: "1.0.0", Digest: "sha256:" + strings.Repeat("a", 64), Path: "pack/pub-core", Entry: "pack.ldl", Files: map[string]string{"pack.ldl": sourceDigest([]byte(source))}, Manifest: resolve.PackManifest{Format: "layerdraw-pack", FormatVersion: 1, ID: "pub/core", Name: "core", Version: "1.0.0", Language: 1, Entry: "pack.ldl"}, SourceFiles: map[string]resolve.SourceFile{"pack.ldl": parsed}}
	resolved := resolve.Resolve(resolve.Input{Mode: resolve.CompilePack, RootPackID: "pub/core", EntryPath: "pack.ldl", Packs: resolve.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]resolve.ResolvedPack{"root": pack}}})
	metadata := materialize.ResolvedMetadata{SelectedClosure: []materialize.ResolvedPackClosure{indexPackClosure(t, resolved, pack)}, SourceFiles: []materialize.ResolvedSourceFile{{Origin: resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: resolved.RootAddress}, ModulePath: "pack.ldl", Bytes: []byte(source)}}}
	return indexStages(t, resolved, metadata)
}

func indexStages(t *testing.T, resolved resolve.Result, metadata materialize.ResolvedMetadata) Input {
	t.Helper()
	defined := definition.Compile(definition.Input{Resolve: resolved})
	graphed := graph.Compile(graph.Input{Resolve: resolved, Definition: defined})
	queried := query.Compile(query.Input{Resolve: resolved, Definition: defined, Graph: graphed})
	viewed := view.Compile(view.Input{Resolve: resolved, Definition: defined, Graph: graphed, Query: queried})
	if resolved.HasErrors || defined.HasErrors || graphed.HasErrors || queried.HasErrors || viewed.HasErrors {
		t.Fatalf("fixture failed resolve=%+v definition=%+v graph=%+v query=%+v view=%+v", resolved.Diagnostics, defined.Diagnostics, graphed.Diagnostics, queried.Diagnostics, viewed.Diagnostics)
	}
	stages := materialize.Input{Resolve: resolved, Definition: defined, Graph: graphed, Query: queried, View: viewed, Resolved: metadata}
	output := materialize.Compile(stages)
	if output.HasErrors {
		t.Fatalf("materialize failed=%+v", output.Diagnostics)
	}
	return Input{Materialized: output}
}

func validatedStages(t *testing.T, input Input) materialize.Input {
	t.Helper()
	stages, ok := input.Materialized.ValidatedInputSnapshot()
	if !ok {
		t.Fatal("materialized input snapshot unavailable")
	}
	return stages
}

func indexPackClosure(t *testing.T, resolved resolve.Result, pack resolve.ResolvedPack) materialize.ResolvedPackClosure {
	t.Helper()
	var summary materialize.ResolvedPackSummary
	for _, dependency := range resolved.Dependencies {
		if dependency.CanonicalID == pack.CanonicalID {
			summary = materialize.ResolvedPackSummary{Address: dependency.Address, CanonicalID: dependency.CanonicalID, Version: dependency.Version, Digest: dependency.Digest}
		}
	}
	if summary.Address == "" {
		t.Fatalf("missing selected pack %s", pack.CanonicalID)
	}
	files := make([]materialize.ResolvedPackFile, 0, len(pack.Files))
	for path, digest := range pack.Files {
		files = append(files, materialize.ResolvedPackFile{Path: path, Digest: digest})
	}
	manifest := materialize.ResolvedPackManifest{Format: pack.Manifest.Format, FormatVersion: pack.Manifest.FormatVersion, ID: pack.Manifest.ID, Name: pack.Manifest.Name, Version: pack.Manifest.Version, Language: pack.Manifest.Language, Entry: pack.Manifest.Entry, Path: "manifest.json"}
	manifest.Bytes = indexManifestBytes(manifest)
	return materialize.ResolvedPackClosure{ResolvedPackSummary: summary, Path: pack.Path, Entry: pack.Entry, Files: files, Manifest: manifest}
}

func indexManifestBytes(manifest materialize.ResolvedPackManifest) []byte {
	value, err := json.Marshal(struct {
		Format        string `json:"format"`
		FormatVersion int    `json:"format_version"`
		ID            string `json:"id"`
		Name          string `json:"name"`
		Version       string `json:"version"`
		Language      int    `json:"language"`
		Entry         string `json:"entry"`
	}{manifest.Format, manifest.FormatVersion, manifest.ID, manifest.Name, manifest.Version, manifest.Language, manifest.Entry})
	if err != nil {
		panic(err)
	}
	return value
}

func sourceSubject(t *testing.T, source SourceMapV1, address string) SourceSubjectRecord {
	t.Helper()
	for _, item := range source.Subjects {
		if item.Address == address {
			return item
		}
	}
	t.Fatalf("missing source subject %s", address)
	return SourceSubjectRecord{}
}
func searchDocument(t *testing.T, values []SearchDocument, address string) SearchDocument {
	t.Helper()
	for _, item := range values {
		if item.SubjectAddress == address {
			return item
		}
	}
	t.Fatalf("missing SearchDocument %s", address)
	return SearchDocument{}
}
func searchHashes(values []SearchDocument) []string {
	out := make([]string, len(values))
	for i := range values {
		out[i] = values[i].ContentHash
	}
	return out
}
func semanticHashes(values []SemanticSubject) []string {
	out := make([]string, len(values))
	for i := range values {
		out[i] = values[i].OwnHash
	}
	return out
}
func reverse[T any](values []T) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
func sourceDigest(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}
func sameBytes(left, right []byte) bool { return bytes.Equal(left, right) }

const indexFixture = `project p "Project" {
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
  reverse "linked by"
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

const packIndexFixture = `entity_type service "Service" {
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
