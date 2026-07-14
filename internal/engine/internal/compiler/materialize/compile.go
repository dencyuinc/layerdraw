// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package materialize

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/exportrecipe"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

var supportedAssetMediaTypes = map[string]bool{
	"image/jpeg":    true,
	"image/png":     true,
	"image/svg+xml": true,
	"image/webp":    true,
}

// Compile transactionally materializes a complete Project or Pack artifact.
// Every input is already in memory; this package has no I/O or clock surface.
func Compile(input Input) Result {
	diagnostics := upstreamDiagnostics(input)
	gate := compileGate{input: input}
	gate.validateStages()
	assets := gate.validateAssets()
	gate.validateClosedResolution()
	diagnostics = append(diagnostics, gate.diagnostics...)
	resolve.SortDiagnostics(diagnostics)
	if hasErrors(diagnostics) || input.Resolve.HasErrors || input.Definition.HasErrors || input.Graph.HasErrors || input.Query.HasErrors || input.View.HasErrors || input.View.ExportRecipes.HasErrors {
		return Result{Diagnostics: diagnostics, HasErrors: true}
	}

	n := newNormalizer(input, assets)
	var snapshot Snapshot
	var envelope any
	switch input.Resolve.Mode {
	case resolve.CompileProject:
		document, err := n.document()
		if err != nil {
			return rejected(diagnostics, input.Resolve.RootAddress, err)
		}
		snapshot.Document = &document
		envelope = document
	case resolve.CompilePack:
		pack, err := n.pack()
		if err != nil {
			return rejected(diagnostics, input.Resolve.RootAddress, err)
		}
		snapshot.Pack = &pack
		envelope = pack
	default:
		return rejected(diagnostics, input.Resolve.RootAddress, fmt.Errorf("unsupported compile mode %q", input.Resolve.Mode))
	}

	canonical, err := Canonicalize(envelope)
	if err != nil {
		return rejected(diagnostics, input.Resolve.RootAddress, err)
	}
	hashes, err := computeHashes(input, snapshot.Document, snapshot.Pack)
	if err != nil {
		return rejected(diagnostics, input.Resolve.RootAddress, err)
	}
	snapshot.CanonicalJSON = append([]byte{}, canonical...)
	snapshot.ArtifactJSON = append(append([]byte{}, canonical...), '\n')
	snapshot.Hashes = hashes
	return Result{state: &resultState{snapshot: deepClone(snapshot), validatedInput: deepClone(input)}, Diagnostics: diagnostics}
}

type compileGate struct {
	input       Input
	diagnostics []resolve.Diagnostic
}

func (g *compileGate) validateStages() {
	checks := []struct {
		valid   bool
		message string
	}{
		{g.input.Definition.MatchesResolve(g.input.Resolve), "definition result does not match Resolve generation"},
		{g.input.Graph.MatchesResolve(g.input.Resolve), "graph result does not match Resolve generation"},
		{g.input.Query.MatchesResolve(g.input.Resolve), "Query result does not match Resolve generation"},
		{g.input.View.MatchesResolve(g.input.Resolve), "View result does not match Resolve generation"},
		{g.input.View.ExportRecipes.MatchesResolve(g.input.Resolve), "Export recipe result does not match Resolve generation"},
		{g.input.Definition.Root.Mode == g.input.Resolve.Mode && g.input.Definition.Root.Address == g.input.Resolve.RootAddress, "definition root does not match Resolve root"},
		{g.input.Graph.Graph != nil, "typed graph result is unavailable"},
	}
	for _, check := range checks {
		if !check.valid {
			g.diag("LDL1801", "stale_revision_or_semantic_hash", check.message, g.input.Resolve.RootAddress)
		}
	}
	if !sameRecipeAddresses(g.input.Resolve.Declarations, resolve.KindQuery, queryAddresses(g.input.Query.Recipes)) {
		g.diag("LDL1801", "stale_revision_or_semantic_hash", "Query recipes do not exactly cover the selected declarations", g.input.Resolve.RootAddress)
	}
	if !sameRecipeAddresses(g.input.Resolve.Declarations, resolve.KindView, viewAddresses(g.input.View.Recipes)) {
		g.diag("LDL1801", "stale_revision_or_semantic_hash", "View recipes do not exactly cover the selected declarations", g.input.Resolve.RootAddress)
	}
	if !sameExports(g.input.View.Recipes, g.input.View.ExportRecipes.Recipes) {
		g.diag("LDL1801", "stale_revision_or_semantic_hash", "View and Export recipe stages disagree", g.input.Resolve.RootAddress)
	}
}

func (g *compileGate) validateAssets() map[assetKey]AssetBlobSummary {
	result := make(map[assetKey]AssetBlobSummary, len(g.input.Resolved.Assets))
	digests := map[string]AssetBlobSummary{}
	for _, asset := range g.input.Resolved.Assets {
		key := assetKey{Origin: asset.Origin, Locator: asset.Locator}
		if asset.Locator == "" || !validSourceOrigin(g.input, asset.Origin) || result[key].Digest != "" {
			g.diag("LDL1201", "invalid_closed_asset", "asset origin and locator are invalid or duplicated", g.input.Resolve.RootAddress)
			continue
		}
		sum := sha256.Sum256(asset.Bytes)
		digest := "sha256:" + hex.EncodeToString(sum[:])
		length := int64(len(asset.Bytes))
		packDigest, packAsset := selectedPackFileDigest(g.input, asset.Origin, asset.Locator)
		if digest != asset.ExpectedDigest || length != asset.ExpectedByteLength || !supportedAssetMediaTypes[asset.ExpectedMediaType] || (asset.Origin.Kind == resolve.OriginPack && (!packAsset || packDigest != digest)) {
			g.diag("LDL1201", "invalid_closed_asset", "asset bytes do not match the expected digest, length, or media type", g.input.Resolve.RootAddress)
			continue
		}
		if err := validateAssetContent(asset.ExpectedMediaType, asset.Bytes); err != nil {
			if errors.Is(err, errUnsafeAsset) {
				g.diag("LDL1901", "unsafe_asset", err.Error(), g.input.Resolve.RootAddress)
			} else {
				g.diag("LDL1201", "invalid_closed_asset", err.Error(), g.input.Resolve.RootAddress)
			}
			continue
		}
		summary := AssetBlobSummary{Digest: digest, MediaType: asset.ExpectedMediaType, ByteLength: length}
		if previous, exists := digests[digest]; exists && previous != summary {
			g.diag("LDL1201", "invalid_closed_asset", "one asset digest has conflicting metadata", g.input.Resolve.RootAddress)
			continue
		}
		digests[digest] = summary
		result[key] = summary
	}
	return result
}

func selectedPackFileDigest(input Input, origin resolve.SourceOrigin, locator string) (string, bool) {
	if origin.Kind != resolve.OriginPack {
		return "", false
	}
	for _, pack := range input.Resolved.SelectedClosure {
		if pack.Address != origin.PackAddress {
			continue
		}
		for _, file := range pack.Files {
			if file.Path == locator {
				return file.Digest, true
			}
		}
	}
	return "", false
}

func (g *compileGate) validateClosedResolution() {
	expected := make([]ResolvedPackSummary, len(g.input.Resolve.Dependencies))
	for i, dependency := range g.input.Resolve.Dependencies {
		expected[i] = ResolvedPackSummary{Address: dependency.Address, CanonicalID: dependency.CanonicalID, Version: dependency.Version, Digest: dependency.Digest}
	}
	actual := closureSummaries(g.input.Resolved.SelectedClosure)
	sort.Slice(expected, func(i, j int) bool { return expected[i].Address < expected[j].Address })
	sort.Slice(actual, func(i, j int) bool { return actual[i].Address < actual[j].Address })
	if !equalClosure(expected, actual) {
		g.diag("LDL1201", "invalid_closed_resolution", "selected resolved closure does not match Resolve", g.input.Resolve.RootAddress)
	}
	packSources := g.validateClosureMetadata()

	expectedSources := map[sourceKey][]byte{}
	for _, module := range g.input.Resolve.Modules {
		key, ok := g.moduleSourceKey(module)
		if !ok {
			g.diag("LDL1201", "invalid_closed_resolution", "resolved module origin is absent from the selected closure", g.input.Resolve.RootAddress)
			continue
		}
		var raw strings.Builder
		for _, token := range module.File.Tokens {
			raw.WriteString(token.FullRaw())
		}
		expectedSources[key] = []byte(raw.String())
	}
	seen := map[sourceKey]bool{}
	for _, source := range g.input.Resolved.SourceFiles {
		key := sourceKey{Origin: source.Origin, ModulePath: source.ModulePath}
		expectedBytes, exists := expectedSources[key]
		if seen[key] {
			g.diag("LDL1201", "invalid_closed_resolution", "source bytes are missing, duplicated, unexpected, or differ from Resolve", g.input.Resolve.RootAddress)
			continue
		}
		if exists {
			if !bytes.Equal(expectedBytes, source.Bytes) {
				g.diag("LDL1201", "invalid_closed_resolution", "source bytes are missing, duplicated, unexpected, or differ from Resolve", g.input.Resolve.RootAddress)
				continue
			}
		}
		packDigest, declaredPackSource := packSources[key]
		if (!exists && !declaredPackSource) || (declaredPackSource && rawDigestBytes(source.Bytes) != packDigest) {
			g.diag("LDL1201", "invalid_closed_resolution", "source bytes are missing, duplicated, unexpected, or differ from the selected pack tree", g.input.Resolve.RootAddress)
			continue
		}
		seen[key] = true
	}
	for key := range expectedSources {
		if !seen[key] {
			g.diag("LDL1201", "invalid_closed_resolution", "closed source tree is incomplete", g.input.Resolve.RootAddress)
		}
	}
	for key := range packSources {
		if !seen[key] {
			g.diag("LDL1201", "invalid_closed_resolution", "selected pack source tree is incomplete", g.input.Resolve.RootAddress)
		}
	}
}

func (g *compileGate) validateClosureMetadata() map[sourceKey]string {
	packSources := map[sourceKey]string{}
	targets := map[string]ResolvedPackSummary{}
	for _, pack := range g.input.Resolved.SelectedClosure {
		rawManifest, manifestErr := decodeManifestBasics(pack.Manifest.Bytes)
		if _, duplicate := targets[pack.CanonicalID]; duplicate || pack.Path == "" || pack.Entry == "" ||
			pack.Manifest.Format != "layerdraw-pack" || pack.Manifest.FormatVersion != 1 || pack.Manifest.Language != LanguageVersion ||
			pack.Manifest.ID != pack.CanonicalID || pack.Manifest.Version != pack.Version || pack.Manifest.Entry != pack.Entry || pack.Manifest.Name == "" ||
			pack.Manifest.Path != "manifest.json" || manifestErr != nil || !sameManifestBasics(pack.Manifest, rawManifest) {
			g.diag("LDL1201", "invalid_closed_resolution", "selected pack metadata is incomplete, duplicated, or internally inconsistent", g.input.Resolve.RootAddress)
		}
		targets[pack.CanonicalID] = pack.ResolvedPackSummary
		origin := resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: pack.Address}
		seenFiles := map[string]bool{}
		entryFound := false
		for _, file := range pack.Files {
			if file.Path == "" || seenFiles[file.Path] || !validSHA256(file.Digest) {
				g.diag("LDL1201", "invalid_closed_resolution", "selected pack file metadata is invalid or duplicated", pack.Address)
				continue
			}
			seenFiles[file.Path] = true
			entryFound = entryFound || file.Path == pack.Entry
			if strings.HasSuffix(file.Path, ".ldl") {
				packSources[sourceKey{Origin: origin, ModulePath: file.Path}] = file.Digest
			}
		}
		if !entryFound {
			g.diag("LDL1201", "invalid_closed_resolution", "selected pack entry is absent from its file metadata", pack.Address)
		}
	}
	for _, pack := range g.input.Resolved.SelectedClosure {
		seenDependencies := map[string]bool{}
		for _, dependency := range pack.Dependencies {
			target, exists := targets[dependency.CanonicalID]
			if dependency.LocalName == "" || dependency.InstallName == "" || seenDependencies[dependency.LocalName] || !exists || target.Version != dependency.Version || target.Digest != dependency.Digest {
				g.diag("LDL1201", "invalid_closed_resolution", "selected pack dependency metadata is incomplete or does not target the selected closure", pack.Address)
			}
			seenDependencies[dependency.LocalName] = true
		}
	}
	return packSources
}

type manifestBasics struct {
	Format        string `json:"format"`
	FormatVersion int    `json:"format_version"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	Language      int    `json:"language"`
	Entry         string `json:"entry"`
}

func decodeManifestBasics(data []byte) (manifestBasics, error) {
	var value manifestBasics
	if len(data) == 0 {
		return value, errors.New("manifest bytes are empty")
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return manifestBasics{}, err
	}
	return value, nil
}

func sameManifestBasics(typed ResolvedPackManifest, raw manifestBasics) bool {
	return typed.Format == raw.Format && typed.FormatVersion == raw.FormatVersion && typed.ID == raw.ID && typed.Name == raw.Name && typed.Version == raw.Version && typed.Language == raw.Language && typed.Entry == raw.Entry
}

type sourceKey struct {
	Origin     resolve.SourceOrigin
	ModulePath string
}

func (g *compileGate) moduleSourceKey(module resolve.ResolvedModule) (sourceKey, bool) {
	origin, ok := sourceOrigin(g.input, module.Origin)
	if !ok {
		return sourceKey{}, false
	}
	return sourceKey{Origin: origin, ModulePath: module.Path}, true
}

func sourceOrigin(input Input, origin resolve.Origin) (resolve.SourceOrigin, bool) {
	resolved := resolve.SourceOrigin{Kind: origin.Kind}
	if origin.Kind == resolve.OriginProject {
		return resolved, true
	}
	canonicalID := origin.Publisher + "/" + origin.PackName
	for _, pack := range input.Resolved.SelectedClosure {
		if pack.CanonicalID == canonicalID {
			resolved.PackAddress = pack.Address
			return resolved, true
		}
	}
	return resolve.SourceOrigin{}, false
}

func validSourceOrigin(input Input, origin resolve.SourceOrigin) bool {
	if origin.Kind == resolve.OriginProject {
		return input.Resolve.Mode == resolve.CompileProject && origin.PackAddress == ""
	}
	if origin.Kind != resolve.OriginPack || origin.PackAddress == "" {
		return false
	}
	for _, pack := range input.Resolved.SelectedClosure {
		if pack.Address == origin.PackAddress {
			return true
		}
	}
	return false
}

func closureSummaries(values []ResolvedPackClosure) []ResolvedPackSummary {
	out := make([]ResolvedPackSummary, len(values))
	for i := range values {
		out[i] = values[i].ResolvedPackSummary
	}
	return out
}

func rawDigestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func validSHA256(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && value == strings.ToLower(value)
}

func (g *compileGate) diag(code, key, message, subject string) {
	g.diagnostics = append(g.diagnostics, resolve.Diagnostic{Code: code, Severity: "error", MessageKey: key, Arguments: map[string]string{}, Message: message, SubjectAddress: subject})
}

func upstreamDiagnostics(input Input) []resolve.Diagnostic {
	for _, diagnostics := range [][]resolve.Diagnostic{input.View.Diagnostics, input.Query.Diagnostics, input.Graph.Diagnostics, input.Definition.Diagnostics, input.Resolve.Diagnostics} {
		if len(diagnostics) != 0 {
			return resolve.CloneDiagnostics(diagnostics)
		}
	}
	return []resolve.Diagnostic{}
}

func rejected(diagnostics []resolve.Diagnostic, subject string, err error) Result {
	diagnostics = append(resolve.CloneDiagnostics(diagnostics), resolve.Diagnostic{Code: "LDL1801", Severity: "error", MessageKey: "materialization_failed", Arguments: map[string]string{}, Message: err.Error(), SubjectAddress: subject})
	resolve.SortDiagnostics(diagnostics)
	return Result{Diagnostics: diagnostics, HasErrors: true}
}

func hasErrors(diagnostics []resolve.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "error" {
			return true
		}
	}
	return false
}

func equalClosure(left, right []ResolvedPackSummary) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func sameRecipeAddresses(declarations []resolve.DeclarationSymbol, kind resolve.SubjectKind, actual []string) bool {
	expected := []string{}
	for _, declaration := range declarations {
		if declaration.Kind == kind {
			expected = append(expected, declaration.Address)
		}
	}
	sort.Strings(expected)
	sort.Strings(actual)
	if len(expected) != len(actual) {
		return false
	}
	for i := range expected {
		if expected[i] != actual[i] {
			return false
		}
	}
	for i := 1; i < len(actual); i++ {
		if actual[i-1] == actual[i] {
			return false
		}
	}
	return true
}

func queryAddresses(values []query.Recipe) []string {
	out := make([]string, len(values))
	for i := range values {
		out[i] = values[i].Address
	}
	return out
}
func viewAddresses(values []view.Recipe) []string {
	out := make([]string, len(values))
	for i := range values {
		out[i] = values[i].Address
	}
	return out
}

func sameExports(views []view.Recipe, values []exportrecipe.Recipe) bool {
	expected := []string{}
	for _, recipe := range views {
		for _, item := range recipe.Exports {
			expected = append(expected, item.Address)
		}
	}
	actual := make([]string, len(values))
	for i := range values {
		actual[i] = values[i].Address
	}
	sort.Strings(expected)
	sort.Strings(actual)
	if len(expected) != len(actual) {
		return false
	}
	for i := range expected {
		if expected[i] != actual[i] {
			return false
		}
	}
	return true
}
