// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

func TestCompileSemanticStageRejections(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		source string
	}{
		{name: "definition", source: "project p \"Project\" {}\nentity_type service \"Service\" {}\n"},
		{name: "graph", source: `project p "Project" {}
layers {
  app "App" @1
}
entity_type service "Service" {
  representation shape rect
}
relation_type link "Link" dependency {
  duplicate_policy allow
  from source types [service] layers [app]
  to target types [service] layers [app]
  label "links"
}
entities service @app {
  one "One"
}
relations link {
  self: one -> one
}
`},
		{name: "query", source: `project p "Project" {}
query broken "Broken" {
}
`},
		{name: "view", source: `project p "Project" {}
query all "All" {
  select {}
}
view broken "Broken" inventory {
  source query all {}
}
`},
		{name: "materialize asset", source: assetFixture},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := New(BuildInfo{}).Compile(context.Background(), projectCompileInput(test.source))
			if err != nil || len(result.Diagnostics) == 0 {
				t.Fatalf("semantic stage rejection err=%v diagnostics=%+v", err, result.Diagnostics)
			}
			assertRejectedOutput(t, result)
		})
	}
}

func TestCompileNilContextAndManifestResourceLimit(t *testing.T) {
	t.Parallel()
	result, err := New(BuildInfo{}).Compile(nil, projectCompileInput(`project p "Project" {}`))
	if !reflect.DeepEqual(result, CompileResult{}) || !IsCompileError(err, ErrorCategoryInvariant) {
		t.Fatalf("nil context result=%+v err=%v", result, err)
	}

	input := projectCompileInput(`project p "Project" {}`)
	input.ResolvedDependencies.Installs = []ResolvedPack{{Manifest: []byte("{}")}}
	input.ResourceLimits.MaxPackBytes = 1
	result, err = New(BuildInfo{}).Compile(context.Background(), input)
	var compileError *CompileError
	if !reflect.DeepEqual(result, CompileResult{}) || !errors.As(err, &compileError) || compileError.Code != ErrorCodePackBytesExceeded {
		t.Fatalf("manifest limit result=%+v err=%v", result, err)
	}
}

func TestPrepareClosedInputValidationBranches(t *testing.T) {
	t.Parallel()
	packModeProject := rootPackInput()
	packModeProject.ProjectSourceTree["document.ldl"] = []byte(`project p "P" {}`)
	projectRootPack := projectCompileInput(`project p "P" {}`)
	projectRootPack.RootPackID = "pub/root"
	invalidManifest := installedPackProjectInput()
	invalidManifest.ResolvedDependencies.Installs[0].Manifest = []byte("{")
	emptyManifestPath := installedPackProjectInput()
	emptyManifestPath.ResolvedDependencies.Installs[0].ManifestPath = ""
	missingFile := installedPackProjectInput()
	delete(missingFile.InstalledPackTree, "pack/schema/pack.ldl")
	manifestMismatch := installedPackProjectInput()
	manifestMismatch.InstalledPackTree["pack/schema/manifest.json"] = []byte("different")
	manifestMatch := installedPackProjectInput()
	manifestMatch.InstalledPackTree["pack/schema/manifest.json"] = append([]byte{}, manifestMatch.ResolvedDependencies.Installs[0].Manifest...)
	projectAssetPackID := projectCompileInput(`project p "P" {}`)
	projectAssetPackID.ReferencedAssets = []AssetInput{{Origin: SourceOriginProject, PackID: "pub/x", Locator: "a"}}
	unknownPackAsset := projectCompileInput(`project p "P" {}`)
	unknownPackAsset.ReferencedAssets = []AssetInput{{Origin: SourceOriginPack, PackID: "pub/x", Locator: "a"}}
	invalidOriginAsset := projectCompileInput(`project p "P" {}`)
	invalidOriginAsset.ReferencedAssets = []AssetInput{{Origin: "other", Locator: "a"}}
	packAssetMismatch := installedPackProjectInput()
	packAssetMismatch.ReferencedAssets = []AssetInput{{Origin: SourceOriginPack, PackID: "pub/schema", Locator: "missing.svg", Bytes: []byte("x")}}

	for _, test := range []struct {
		name       string
		input      CompileInput
		wantErrors bool
	}{
		{name: "Pack with Project", input: packModeProject, wantErrors: true},
		{name: "Project root Pack", input: projectRootPack, wantErrors: true},
		{name: "invalid manifest", input: invalidManifest, wantErrors: true},
		{name: "default manifest path", input: emptyManifestPath},
		{name: "missing file", input: missingFile, wantErrors: true},
		{name: "manifest mismatch", input: manifestMismatch, wantErrors: true},
		{name: "manifest match", input: manifestMatch},
		{name: "Project asset Pack ID", input: projectAssetPackID, wantErrors: true},
		{name: "unknown Pack asset", input: unknownPackAsset, wantErrors: true},
		{name: "invalid asset origin", input: invalidOriginAsset, wantErrors: true},
		{name: "Pack asset mismatch", input: packAssetMismatch, wantErrors: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			closed, err := cloneClosedInput(context.Background(), test.input, DefaultResourceLimits())
			if err != nil {
				t.Fatal(err)
			}
			_, diagnostics, err := prepareClosedInput(context.Background(), closed)
			if err != nil {
				t.Fatal(err)
			}
			if (len(diagnostics) != 0) != test.wantErrors {
				t.Fatalf("diagnostics=%+v wantErrors=%v", diagnostics, test.wantErrors)
			}
		})
	}
}

func TestCompilePackAssetAndDependencyMetadata(t *testing.T) {
	t.Parallel()
	baseSource := []byte("entity_type base \"Base\" {\n  representation shape rect\n}\nexport { base }\n")
	childSource := []byte("import { base } from \"core\"\nentity_type service \"Service\" {\n  image \"icon.svg\"\n  representation shape rect\n}\nexport { service }\n")
	icon := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="1" height="1"/></svg>`)
	baseManifest := packManifestBytes("pub/base", "base", "1.0.0", "pack.ldl", nil)
	childManifest := packManifestBytes("pub/child", "child", "1.0.0", "pack.ldl", map[string]map[string]string{"core": {"id": "pub/base", "version": "1.0.0"}})
	input := CompileInput{
		Mode: CompileProject, EntryPath: "document.ldl",
		ProjectSourceTree: map[string][]byte{"document.ldl": []byte("import { service } from \"child\"\nproject p \"Project\" {}\n")},
		InstalledPackTree: map[string][]byte{
			"pack/base/pack.ldl": baseSource, "pack/base/manifest.json": baseManifest,
			"pack/child/pack.ldl": childSource, "pack/child/icon.svg": icon,
		},
		ResolvedDependencies: ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: []ResolvedPack{
			{
				InstallName: "base", CanonicalID: "pub/base", Version: "1.0.0", Digest: "sha256:" + strings.Repeat("b", 64), Path: "pack/base", Entry: "pack.ldl",
				Files: []ResolvedPackFile{{Path: "pack.ldl", Digest: digestBytes(baseSource)}}, ManifestPath: "manifest.json", Manifest: baseManifest,
			},
			{
				InstallName: "child", CanonicalID: "pub/child", Version: "1.0.0", Digest: "sha256:" + strings.Repeat("c", 64), Path: "pack/child", Entry: "pack.ldl",
				Files:        []ResolvedPackFile{{Path: "icon.svg", Digest: digestBytes(icon)}, {Path: "pack.ldl", Digest: digestBytes(childSource)}},
				Dependencies: []ResolvedPackDependency{{LocalName: "core", InstallName: "base"}}, ManifestPath: "manifest.json", Manifest: childManifest,
			},
		}},
		ReferencedAssets: []AssetInput{{Origin: SourceOriginPack, PackID: "pub/child", Locator: "icon.svg", Bytes: icon, Digest: digestBytes(icon), MediaType: "image/svg+xml", ByteLength: int64(len(icon))}},
	}
	result, err := New(BuildInfo{}).Compile(context.Background(), input)
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("dependency compile err=%v diagnostics=%+v", err, result.Diagnostics)
	}
	snapshot := result.Snapshot()
	if len(snapshot.NormalizedDocument.Dependencies) != 2 || len(snapshot.NormalizedDocument.Assets) != 1 {
		t.Fatalf("dependency/asset metadata missing: %+v", snapshot.NormalizedDocument)
	}
}

func TestInternalCancellationAndInvariantHelpers(t *testing.T) {
	t.Parallel()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, input := range []CompileInput{
		{ProjectSourceTree: map[string][]byte{"a": []byte("a")}},
		{InstalledPackTree: map[string][]byte{"a": []byte("a")}},
		{ResolvedDependencies: ResolvedDependencies{Installs: []ResolvedPack{{Manifest: []byte("a")}}}},
		{ReferencedAssets: []AssetInput{{Bytes: []byte("a")}}},
	} {
		if _, err := cloneClosedInput(cancelled, input, DefaultResourceLimits()); !IsCompileError(err, ErrorCategoryCancelled) {
			t.Fatalf("clone cancellation err=%v", err)
		}
	}
	if _, err := (preparedInput{projectPaths: []string{"a"}, input: CompileInput{ProjectSourceTree: map[string][]byte{"a": []byte("a")}}}).parse(cancelled, DefaultResourceLimits()); !IsCompileError(err, ErrorCategoryCancelled) {
		t.Fatalf("parse cancellation err=%v", err)
	}
	if observed, exceeded := addExceeds(math.MaxInt64-1, 2, math.MaxInt64); !exceeded || observed != math.MaxInt64 {
		t.Fatalf("overflow observation=%d exceeded=%v", observed, exceeded)
	}

	missing := preparedInput{byCanonical: map[string][]*preparedPack{}}
	if _, err := missing.materializeMetadata(context.Background(), resolve.Result{Dependencies: []resolve.ResolvedPackSummary{{CanonicalID: "pub/missing"}}}); err == nil {
		t.Fatal("missing selected metadata accepted")
	}
	if _, err := missing.losslessSyntaxTree(context.Background(), resolve.Result{Modules: []resolve.ResolvedModule{{Origin: resolve.Origin{Kind: resolve.OriginProject}, Path: "missing.ldl"}}}, nil); err == nil {
		t.Fatal("missing lossless source accepted")
	}
	if _, ok := subjectCapability(materialize.SubjectKind("unknown")); ok {
		t.Fatal("unknown generated subject classified")
	}
	left := resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: "a"}
	right := resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: "b"}
	if !lessSource(left, "z", right, "a") || lessSource(right, "a", left, "z") {
		t.Fatal("Pack source ordering does not use Pack address")
	}
	if snapshot := (CompileResult{}).Snapshot(); !reflect.DeepEqual(snapshot, Snapshot{}) {
		t.Fatalf("empty Snapshot=%+v", snapshot)
	}
	bare := (&CompileError{Code: "bare"}).Error()
	resource := (&CompileError{Code: "resource", Resource: "bytes", Limit: 1, Observed: 2}).Error()
	if bare != "bare" || !strings.Contains(resource, "observed 2 exceeds limit 1") {
		t.Fatalf("error strings bare=%q resource=%q", bare, resource)
	}
	if got := cloneReflectValue(reflect.Value{}); got.IsValid() {
		t.Fatal("invalid reflect value became valid")
	}
}

type stagedCancelContext struct {
	remaining int
}

func (c *stagedCancelContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *stagedCancelContext) Done() <-chan struct{}       { return nil }
func (c *stagedCancelContext) Value(any) any               { return nil }
func (c *stagedCancelContext) Err() error {
	if c.remaining == 0 {
		return context.Canceled
	}
	c.remaining--
	return nil
}

func TestParseCancellationInsidePackLoops(t *testing.T) {
	t.Parallel()
	closed, err := cloneClosedInput(context.Background(), installedPackProjectInput(), DefaultResourceLimits())
	if err != nil {
		t.Fatal(err)
	}
	prepared, diagnostics, err := prepareClosedInput(context.Background(), closed)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	for _, remaining := range []int{1, 2} {
		if _, err := prepared.parse(&stagedCancelContext{remaining: remaining}, DefaultResourceLimits()); !IsCompileError(err, ErrorCategoryCancelled) {
			t.Fatalf("staged parse cancellation remaining=%d err=%v", remaining, err)
		}
	}
}

func TestPreparationAndMaterializationPollEveryCollection(t *testing.T) {
	t.Parallel()
	input := installedPackProjectInput()
	input.ReferencedAssets = []AssetInput{{Origin: SourceOriginProject, Locator: "unused.png", Bytes: testPNG}}
	closed, err := cloneClosedInput(context.Background(), input, DefaultResourceLimits())
	if err != nil {
		t.Fatal(err)
	}
	// Pack, Pack file, installed tree, and asset each have an independent poll.
	for _, remaining := range []int{0, 1, 2, 3} {
		if _, _, err := prepareClosedInput(&stagedCancelContext{remaining: remaining}, closed); !IsCompileError(err, ErrorCategoryCancelled) {
			t.Fatalf("preparation cancellation remaining=%d err=%v", remaining, err)
		}
	}

	prepared, diagnostics, err := prepareClosedInput(context.Background(), closed)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("prepare err=%v diagnostics=%+v", err, diagnostics)
	}
	resolverInput, err := prepared.parse(context.Background(), DefaultResourceLimits())
	if err != nil {
		t.Fatal(err)
	}
	resolved := resolve.Resolve(resolverInput)
	if resolved.HasErrors {
		t.Fatal(resolved.Diagnostics)
	}
	// Selected closures, resolved modules, remaining Pack sources, and assets
	// are all bounded collections with independent cancellation polling.
	for _, remaining := range []int{0, 1, 3, 4} {
		if _, err := prepared.materializeMetadata(&stagedCancelContext{remaining: remaining}, resolved); !IsCompileError(err, ErrorCategoryCancelled) {
			t.Fatalf("metadata cancellation remaining=%d err=%v", remaining, err)
		}
	}
	if _, err := prepared.losslessSyntaxTree(&stagedCancelContext{}, resolved, nil); !IsCompileError(err, ErrorCategoryCancelled) {
		t.Fatalf("lossless cancellation err=%v", err)
	}
}
