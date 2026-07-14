// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"sort"
	"strings"

	_ "golang.org/x/image/webp"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

type compileStage string

const (
	stageStart       compileStage = "start"
	stagePreprocess  compileStage = "preprocess"
	stageParse       compileStage = "parse"
	stageResolve     compileStage = "resolve"
	stageDefinition  compileStage = "definition"
	stageGraph       compileStage = "graph"
	stageQuery       compileStage = "query"
	stageViewExport  compileStage = "view_export"
	stageMaterialize compileStage = "materialize"
	stageIndex       compileStage = "index"
	stageComplete    compileStage = "complete"
)

// Compile runs the one canonical parse-to-index pipeline over a closed input.
// Semantic rejection is returned as deterministic Diagnostics with no
// published output. Cancellation, resource exhaustion, and invariant failure
// return a typed error and an entirely empty result.
func (e Engine) Compile(ctx context.Context, input CompileInput) (result CompileResult, err error) {
	stage := stageStart
	defer func() {
		if recover() != nil {
			result = CompileResult{}
			err = invariantFailure(stage)
		}
	}()
	if ctx == nil {
		return CompileResult{}, invariantFailure(stage)
	}
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}

	limits, ok := input.ResourceLimits.Effective()
	if !ok {
		return CompileResult{}, &CompileError{Code: ErrorCodeInvalidResourceLimits, Category: ErrorCategoryResource, Resource: "resource_limits", Stage: string(stageStart)}
	}
	stage = stagePreprocess
	closed, err := cloneClosedInput(ctx, input, limits)
	if err != nil {
		return CompileResult{}, err
	}
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	prepared, diagnostics, err := prepareClosedInput(ctx, closed)
	if err != nil {
		return CompileResult{}, err
	}
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	if len(diagnostics) != 0 {
		return e.rejected(ctx, stage, diagnostics)
	}

	stage = stageParse
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	resolvedInput, err := prepared.parse(ctx, limits)
	if err != nil {
		return CompileResult{}, err
	}
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}

	stage = stageResolve
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	resolved := resolve.Resolve(resolvedInput)
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	if resolved.HasErrors {
		return e.rejected(ctx, stage, resolved.Diagnostics)
	}

	stage = stageDefinition
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	defined := definition.Compile(definition.Input{Resolve: resolved})
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	if defined.HasErrors {
		return e.rejected(ctx, stage, defined.Diagnostics)
	}
	if !defined.MatchesResolve(resolved) {
		return CompileResult{}, invariantFailure(stage)
	}

	stage = stageGraph
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	graphed := graph.Compile(graph.Input{Resolve: resolved, Definition: defined})
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	if graphed.HasErrors {
		return e.rejected(ctx, stage, graphed.Diagnostics)
	}
	if !graphed.MatchesResolve(resolved) || graphed.Graph == nil {
		return CompileResult{}, invariantFailure(stage)
	}

	stage = stageQuery
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	queried := query.Compile(query.Input{Resolve: resolved, Definition: defined, Graph: graphed})
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	if queried.HasErrors {
		return e.rejected(ctx, stage, queried.Diagnostics)
	}
	if !queried.MatchesResolve(resolved) {
		return CompileResult{}, invariantFailure(stage)
	}

	stage = stageViewExport
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	viewed := view.Compile(view.Input{Resolve: resolved, Definition: defined, Graph: graphed, Query: queried})
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	if viewed.HasErrors || viewed.ExportRecipes.HasErrors {
		return e.rejected(ctx, stage, viewed.Diagnostics)
	}
	if !viewed.MatchesResolve(resolved) || !viewed.ExportRecipes.MatchesResolve(resolved) {
		return CompileResult{}, invariantFailure(stage)
	}

	stage = stageMaterialize
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	metadata, err := prepared.materializeMetadata(ctx, resolved)
	if err != nil {
		if IsCompileError(err, ErrorCategoryCancelled) {
			return CompileResult{}, err
		}
		return CompileResult{}, invariantFailure(stage)
	}
	materialized := materialize.Compile(materialize.Input{
		Resolve: resolved, Definition: defined, Graph: graphed, Query: queried, View: viewed, Resolved: metadata,
	})
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	if materialized.HasErrors {
		return e.rejected(ctx, stage, materialized.Diagnostics)
	}
	if !materialized.MatchesResolve(resolved) {
		return CompileResult{}, invariantFailure(stage)
	}

	stage = stageIndex
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	indexed := index.Build(index.Input{Materialized: materialized})
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	if indexed.HasErrors {
		return CompileResult{}, invariantFailure(stage)
	}
	indexSnapshot := indexed.Snapshot()
	if indexSnapshot.SourceMap.SchemaVersion != index.SourceMapSchemaVersion || indexSnapshot.SemanticIndex.SchemaVersion != index.SemanticIndexSchemaVersion {
		return CompileResult{}, invariantFailure(stage)
	}

	stage = stageComplete
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	materializedSnapshot := materialized.Snapshot()
	lossless, err := prepared.losslessSyntaxTree(ctx, resolved, metadata.SelectedClosure)
	if err != nil {
		if IsCompileError(err, ErrorCategoryCancelled) {
			return CompileResult{}, err
		}
		return CompileResult{}, invariantFailure(stage)
	}
	output := completeOutput(limits, resolved, defined, graphed, queried, viewed, lossless, materializedSnapshot, indexSnapshot, indexed.Diagnostics)
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	completed := compileResult(output)
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	return completed, nil
}

func (e Engine) checkCompileBoundary(ctx context.Context, stage compileStage) error {
	if hook, ok := ctx.(interface{ onCompileBoundary(compileStage) }); ok {
		hook.onCompileBoundary(stage)
	}
	if err := ctx.Err(); err != nil {
		return &CompileError{Code: ErrorCodeCancelled, Category: ErrorCategoryCancelled, Stage: string(stage), cause: err}
	}
	return nil
}

func pollContext(ctx context.Context, stage compileStage) error {
	if err := ctx.Err(); err != nil {
		return &CompileError{Code: ErrorCodeCancelled, Category: ErrorCategoryCancelled, Stage: string(stage), cause: err}
	}
	return nil
}

func invariantFailure(stage compileStage) *CompileError {
	return &CompileError{Code: ErrorCodeInvariantFailure, Category: ErrorCategoryInvariant, Stage: string(stage)}
}

func resourceFailure(stage compileStage, code, resource string, limit, observed int64) *CompileError {
	return &CompileError{Code: code, Category: ErrorCategoryResource, Resource: resource, Limit: limit, Observed: observed, Stage: string(stage)}
}

func rejectedCompileResult(diagnostics []resolve.Diagnostic) CompileResult {
	cloned := resolve.CloneDiagnostics(diagnostics)
	resolve.SortDiagnostics(cloned)
	return compileResult(CompileOutput{Diagnostics: cloned})
}

func (e Engine) rejected(ctx context.Context, stage compileStage, diagnostics []resolve.Diagnostic) (CompileResult, error) {
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	result := rejectedCompileResult(diagnostics)
	if err := e.checkCompileBoundary(ctx, stage); err != nil {
		return CompileResult{}, err
	}
	return result, nil
}

func cloneClosedInput(ctx context.Context, input CompileInput, limits ResourceLimits) (CompileInput, error) {
	out := CompileInput{
		Mode: input.Mode, EntryPath: input.EntryPath, RootPackID: input.RootPackID,
		ResolvedDependencies: input.ResolvedDependencies, ResourceLimits: limits,
		ProjectSourceTree: map[string][]byte{}, InstalledPackTree: map[string][]byte{},
	}
	if int64(len(input.ProjectSourceTree)) > limits.MaxProjectSourceFiles {
		return CompileInput{}, resourceFailure(stagePreprocess, ErrorCodeProjectSourceFilesExceeded, "project_source_files", limits.MaxProjectSourceFiles, int64(len(input.ProjectSourceTree)))
	}
	projectBytes := int64(0)
	for _, key := range sortedByteMapKeys(input.ProjectSourceTree) {
		if err := pollContext(ctx, stagePreprocess); err != nil {
			return CompileInput{}, err
		}
		value := input.ProjectSourceTree[key]
		if observed, exceeded := addExceeds(projectBytes, int64(len(value)), limits.MaxProjectSourceBytes); exceeded {
			return CompileInput{}, resourceFailure(stagePreprocess, ErrorCodeProjectSourceBytesExceeded, "project_source_bytes", limits.MaxProjectSourceBytes, observed)
		}
		projectBytes += int64(len(value))
		out.ProjectSourceTree[key] = bytes.Clone(value)
	}
	if int64(len(input.InstalledPackTree)) > limits.MaxPackFiles {
		return CompileInput{}, resourceFailure(stagePreprocess, ErrorCodePackFilesExceeded, "pack_files", limits.MaxPackFiles, int64(len(input.InstalledPackTree)))
	}
	// Resolved file metadata is a second representation of the installed tree,
	// so it is checked independently rather than double-counted. Every raw
	// manifest is one closed Pack file even when it is not repeated in the tree.
	metadataPackFiles := int64(len(input.ResolvedDependencies.Installs))
	if metadataPackFiles > limits.MaxPackFiles {
		return CompileInput{}, resourceFailure(stagePreprocess, ErrorCodePackFilesExceeded, "pack_files", limits.MaxPackFiles, metadataPackFiles)
	}
	for i := range input.ResolvedDependencies.Installs {
		observed, exceeded := addExceeds(metadataPackFiles, int64(len(input.ResolvedDependencies.Installs[i].Files)), limits.MaxPackFiles)
		if exceeded {
			return CompileInput{}, resourceFailure(stagePreprocess, ErrorCodePackFilesExceeded, "pack_files", limits.MaxPackFiles, observed)
		}
		metadataPackFiles = observed
	}
	packBytes := int64(0)
	for _, key := range sortedByteMapKeys(input.InstalledPackTree) {
		if err := pollContext(ctx, stagePreprocess); err != nil {
			return CompileInput{}, err
		}
		value := input.InstalledPackTree[key]
		if observed, exceeded := addExceeds(packBytes, int64(len(value)), limits.MaxPackBytes); exceeded {
			return CompileInput{}, resourceFailure(stagePreprocess, ErrorCodePackBytesExceeded, "pack_bytes", limits.MaxPackBytes, observed)
		}
		packBytes += int64(len(value))
		out.InstalledPackTree[key] = bytes.Clone(value)
	}
	out.ResolvedDependencies.Installs = make([]ResolvedPack, len(input.ResolvedDependencies.Installs))
	for i := range input.ResolvedDependencies.Installs {
		if err := pollContext(ctx, stagePreprocess); err != nil {
			return CompileInput{}, err
		}
		pack := input.ResolvedDependencies.Installs[i]
		if observed, exceeded := addExceeds(packBytes, int64(len(pack.Manifest)), limits.MaxPackBytes); exceeded {
			return CompileInput{}, resourceFailure(stagePreprocess, ErrorCodePackBytesExceeded, "pack_bytes", limits.MaxPackBytes, observed)
		}
		packBytes += int64(len(pack.Manifest))
		pack.Manifest = bytes.Clone(pack.Manifest)
		pack.Files = append([]ResolvedPackFile{}, pack.Files...)
		pack.Dependencies = append([]ResolvedPackDependency{}, pack.Dependencies...)
		out.ResolvedDependencies.Installs[i] = pack
	}
	if int64(len(input.ReferencedAssets)) > limits.MaxAssets {
		return CompileInput{}, resourceFailure(stagePreprocess, ErrorCodeAssetsExceeded, "assets", limits.MaxAssets, int64(len(input.ReferencedAssets)))
	}
	out.ReferencedAssets = make([]AssetInput, len(input.ReferencedAssets))
	assetBytes := int64(0)
	for i := range input.ReferencedAssets {
		if err := pollContext(ctx, stagePreprocess); err != nil {
			return CompileInput{}, err
		}
		asset := input.ReferencedAssets[i]
		if observed, exceeded := addExceeds(assetBytes, int64(len(asset.Bytes)), limits.MaxAssetBytes); exceeded {
			return CompileInput{}, resourceFailure(stagePreprocess, ErrorCodeAssetBytesExceeded, "asset_bytes", limits.MaxAssetBytes, observed)
		}
		assetBytes += int64(len(asset.Bytes))
		if err := validateRasterResources(asset, limits); err != nil {
			return CompileInput{}, err
		}
		asset.Bytes = bytes.Clone(asset.Bytes)
		out.ReferencedAssets[i] = asset
	}
	return out, pollContext(ctx, stagePreprocess)
}

func validateRasterResources(asset AssetInput, limits ResourceLimits) error {
	expectedFormat, supported := map[string]string{
		"image/jpeg": "jpeg",
		"image/png":  "png",
		"image/webp": "webp",
	}[asset.MediaType]
	if !supported {
		return nil
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(asset.Bytes))
	if err != nil || format != expectedFormat {
		return nil
	}
	width, height := int64(config.Width), int64(config.Height)
	observedDimension := max(width, height)
	if observedDimension > limits.MaxRasterDimension {
		return resourceFailure(stagePreprocess, ErrorCodeRasterDimensionExceeded, "raster_dimension", limits.MaxRasterDimension, observedDimension)
	}
	if width > 0 && height > math.MaxInt64/width {
		return resourceFailure(stagePreprocess, ErrorCodeRasterPixelsExceeded, "raster_pixels", limits.MaxRasterPixels, math.MaxInt64)
	}
	pixels := width * height
	if pixels > limits.MaxRasterPixels {
		return resourceFailure(stagePreprocess, ErrorCodeRasterPixelsExceeded, "raster_pixels", limits.MaxRasterPixels, pixels)
	}
	return nil
}

func addExceeds(total, amount, limit int64) (int64, bool) {
	if amount <= limit-total {
		return total + amount, false
	}
	if amount > math.MaxInt64-total {
		return math.MaxInt64, true
	}
	return total + amount, true
}

func sortedByteMapKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type preparedInput struct {
	input        CompileInput
	projectPaths []string
	packs        []preparedPack
	byInstall    map[string]*preparedPack
	byCanonical  map[string][]*preparedPack
}

type preparedPack struct {
	input       ResolvedPack
	manifest    resolve.PackManifest
	files       []ResolvedPackFile
	fileBytes   map[string][]byte
	fileDigests map[string]string
}

type rawPackManifest struct {
	Format        string                            `json:"format"`
	FormatVersion int                               `json:"format_version"`
	ID            string                            `json:"id"`
	Name          string                            `json:"name"`
	Version       string                            `json:"version"`
	Language      int                               `json:"language"`
	Entry         string                            `json:"entry"`
	Dependencies  map[string]resolve.PackDependency `json:"dependencies"`
}

func prepareClosedInput(ctx context.Context, input CompileInput) (preparedInput, []resolve.Diagnostic, error) {
	prepared := preparedInput{input: input, projectPaths: sortedByteMapKeys(input.ProjectSourceTree), byInstall: map[string]*preparedPack{}, byCanonical: map[string][]*preparedPack{}}
	diagnostics := []resolve.Diagnostic{}
	if input.Mode != CompileProject && input.Mode != CompilePack {
		diagnostics = append(diagnostics, closedInputDiagnostic("compile_mode", "compile mode must be project or pack"))
	}
	if input.Mode == CompilePack && len(input.ProjectSourceTree) != 0 {
		diagnostics = append(diagnostics, closedInputDiagnostic("pack_mode_project_tree", "Pack compilation cannot contain a Project source tree"))
	}
	if input.Mode == CompileProject && input.RootPackID != "" {
		diagnostics = append(diagnostics, closedInputDiagnostic("project_mode_root_pack", "Project compilation cannot select a root Pack"))
	}
	if input.ResolvedDependencies.Format != "layerdraw-resolved" || input.ResolvedDependencies.FormatVersion != 1 || input.ResolvedDependencies.Language != 1 {
		diagnostics = append(diagnostics, closedInputDiagnostic("resolved_envelope", "resolved dependency metadata must use the canonical Language 1 envelope"))
	}
	installs := append([]ResolvedPack{}, prepared.input.ResolvedDependencies.Installs...)
	sort.SliceStable(installs, func(i, j int) bool {
		if installs[i].InstallName != installs[j].InstallName {
			return installs[i].InstallName < installs[j].InstallName
		}
		return installs[i].CanonicalID < installs[j].CanonicalID
	})
	expectedTree := map[string]string{}
	for _, packInput := range installs {
		if err := pollContext(ctx, stagePreprocess); err != nil {
			return preparedInput{}, nil, err
		}
		if _, exists := prepared.byInstall[packInput.InstallName]; exists || packInput.InstallName == "" {
			diagnostics = append(diagnostics, closedInputDiagnostic("duplicate_install", "resolved Pack install names must be non-empty and unique"))
			continue
		}
		var raw rawPackManifest
		if err := json.Unmarshal(packInput.Manifest, &raw); err != nil {
			diagnostics = append(diagnostics, closedInputDiagnostic("invalid_pack_manifest", "Pack manifest bytes are not valid closed metadata"))
			continue
		}
		manifestPath := packInput.ManifestPath
		if manifestPath == "" {
			manifestPath = "manifest.json"
		}
		packInput.ManifestPath = manifestPath
		pack := &preparedPack{
			input: packInput,
			manifest: resolve.PackManifest{
				Format: raw.Format, FormatVersion: raw.FormatVersion, ID: raw.ID, Name: raw.Name,
				Version: raw.Version, Language: raw.Language, Entry: raw.Entry, Dependencies: raw.Dependencies,
			},
			files: append([]ResolvedPackFile{}, packInput.Files...), fileBytes: map[string][]byte{}, fileDigests: map[string]string{},
		}
		dependencyNames := map[string]bool{}
		for _, dependency := range pack.input.Dependencies {
			if dependencyNames[dependency.LocalName] {
				diagnostics = append(diagnostics, closedInputDiagnostic("duplicate_pack_dependency", "resolved Pack dependency names must be unique"))
			}
			dependencyNames[dependency.LocalName] = true
		}
		sort.SliceStable(pack.files, func(i, j int) bool { return pack.files[i].Path < pack.files[j].Path })
		for _, file := range pack.files {
			if err := pollContext(ctx, stagePreprocess); err != nil {
				return preparedInput{}, nil, err
			}
			if _, duplicate := pack.fileDigests[file.Path]; duplicate || file.Path == "" {
				diagnostics = append(diagnostics, closedInputDiagnostic("duplicate_pack_file", "resolved Pack file paths must be non-empty and unique"))
				continue
			}
			pack.fileDigests[file.Path] = file.Digest
			fullPath := pack.input.Path + "/" + file.Path
			value, exists := input.InstalledPackTree[fullPath]
			if !exists {
				diagnostics = append(diagnostics, closedInputDiagnostic("missing_pack_file", "installed Pack tree is missing resolved file bytes"))
				continue
			}
			if digestBytes(value) != file.Digest {
				diagnostics = append(diagnostics, closedInputDiagnostic("pack_file_digest_mismatch", "installed Pack bytes do not match resolved file metadata"))
			}
			if previous, duplicate := expectedTree[fullPath]; duplicate && previous != pack.input.CanonicalID {
				diagnostics = append(diagnostics, closedInputDiagnostic("duplicate_pack_path", "installed Pack paths cannot be shared by different canonical Packs"))
			}
			expectedTree[fullPath] = pack.input.CanonicalID
			pack.fileBytes[file.Path] = value
		}
		manifestFullPath := pack.input.Path + "/" + pack.input.ManifestPath
		if value, exists := input.InstalledPackTree[manifestFullPath]; exists {
			if !bytes.Equal(value, pack.input.Manifest) {
				diagnostics = append(diagnostics, closedInputDiagnostic("pack_manifest_mismatch", "manifest bytes disagree with the installed Pack tree"))
			}
			expectedTree[manifestFullPath] = pack.input.CanonicalID
		}
		prepared.packs = append(prepared.packs, *pack)
		stored := &prepared.packs[len(prepared.packs)-1]
		prepared.byInstall[packInput.InstallName] = stored
		prepared.byCanonical[packInput.CanonicalID] = append(prepared.byCanonical[packInput.CanonicalID], stored)
	}
	// Rebuild pointers after append growth so maps never retain stale slice addresses.
	prepared.byInstall = map[string]*preparedPack{}
	prepared.byCanonical = map[string][]*preparedPack{}
	for i := range prepared.packs {
		pack := &prepared.packs[i]
		prepared.byInstall[pack.input.InstallName] = pack
		prepared.byCanonical[pack.input.CanonicalID] = append(prepared.byCanonical[pack.input.CanonicalID], pack)
	}
	for _, treePath := range sortedByteMapKeys(input.InstalledPackTree) {
		if err := pollContext(ctx, stagePreprocess); err != nil {
			return preparedInput{}, nil, err
		}
		if _, expected := expectedTree[treePath]; !expected {
			diagnostics = append(diagnostics, closedInputDiagnostic("unexpected_pack_file", "installed Pack tree contains bytes absent from resolved metadata"))
		}
	}
	assetKeys := map[string]bool{}
	for _, asset := range input.ReferencedAssets {
		if err := pollContext(ctx, stagePreprocess); err != nil {
			return preparedInput{}, nil, err
		}
		key := string(asset.Origin) + "\x00" + asset.PackID + "\x00" + asset.Locator
		if assetKeys[key] {
			diagnostics = append(diagnostics, closedInputDiagnostic("duplicate_asset", "referenced assets must have unique origin and locator"))
		}
		assetKeys[key] = true
		switch asset.Origin {
		case SourceOriginProject:
			if asset.PackID != "" {
				diagnostics = append(diagnostics, closedInputDiagnostic("invalid_asset_origin", "Project assets cannot identify a Pack"))
			}
		case SourceOriginPack:
			packs := prepared.byCanonical[asset.PackID]
			if asset.PackID == "" || len(packs) == 0 {
				diagnostics = append(diagnostics, closedInputDiagnostic("invalid_asset_origin", "Pack assets must identify an installed canonical Pack"))
				continue
			}
			found := false
			for _, pack := range packs {
				if value, exists := pack.fileBytes[asset.Locator]; exists && bytes.Equal(value, asset.Bytes) {
					found = true
				}
			}
			if !found {
				diagnostics = append(diagnostics, closedInputDiagnostic("pack_asset_mismatch", "Pack asset bytes are not present in the installed Pack tree"))
			}
		default:
			diagnostics = append(diagnostics, closedInputDiagnostic("invalid_asset_origin", "asset origin is not part of the closed vocabulary"))
		}
	}
	resolve.SortDiagnostics(diagnostics)
	return prepared, diagnostics, nil
}

func closedInputDiagnostic(key, message string) resolve.Diagnostic {
	return resolve.Diagnostic{Code: "LDL1201", Severity: "error", MessageKey: "invalid_closed_input_" + key, Arguments: map[string]string{}, Message: message, Related: []resolve.DiagnosticRelated{}}
}

func (p preparedInput) parse(ctx context.Context, limits ResourceLimits) (resolve.Input, error) {
	declarationCount := int64(0)
	countDeclarations := func(file resolve.SourceFile) error {
		observed, exceeded := addExceeds(declarationCount, resolve.AuthoredDeclarationCount(file), limits.MaxDeclarations)
		if exceeded {
			return resourceFailure(stageParse, ErrorCodeDeclarationsExceeded, "declarations", limits.MaxDeclarations, observed)
		}
		declarationCount = observed
		return nil
	}
	projectFiles := make(map[string]resolve.SourceFile, len(p.projectPaths))
	for _, sourcePath := range p.projectPaths {
		if err := pollContext(ctx, stageParse); err != nil {
			return resolve.Input{}, err
		}
		file := resolve.SourceFromParse(syntax.Parse(p.input.ProjectSourceTree[sourcePath]))
		if err := countDeclarations(file); err != nil {
			return resolve.Input{}, err
		}
		projectFiles[sourcePath] = file
	}
	installs := make(map[string]resolve.ResolvedPack, len(p.packs))
	for i := range p.packs {
		if err := pollContext(ctx, stageParse); err != nil {
			return resolve.Input{}, err
		}
		pack := p.packs[i]
		files := make(map[string]string, len(pack.files))
		sourceFiles := map[string]resolve.SourceFile{}
		for _, file := range pack.files {
			files[file.Path] = file.Digest
			if strings.HasSuffix(file.Path, ".ldl") {
				if err := pollContext(ctx, stageParse); err != nil {
					return resolve.Input{}, err
				}
				parsed := resolve.SourceFromParse(syntax.Parse(pack.fileBytes[file.Path]))
				if err := countDeclarations(parsed); err != nil {
					return resolve.Input{}, err
				}
				sourceFiles[file.Path] = parsed
			}
		}
		dependencies := make(map[string]string, len(pack.input.Dependencies))
		for _, dependency := range pack.input.Dependencies {
			dependencies[dependency.LocalName] = dependency.InstallName
		}
		installs[pack.input.InstallName] = resolve.ResolvedPack{
			CanonicalID: pack.input.CanonicalID, Version: pack.input.Version, Digest: pack.input.Digest,
			Path: pack.input.Path, Entry: pack.input.Entry, Files: files, Dependencies: dependencies,
			Manifest: pack.manifest, SourceFiles: sourceFiles,
		}
	}
	return resolve.Input{
		Mode: resolve.CompileMode(p.input.Mode), EntryPath: p.input.EntryPath, RootPackID: p.input.RootPackID,
		Project: resolve.ProjectInput{Files: projectFiles},
		Packs: resolve.ResolvedDependencies{
			Format: p.input.ResolvedDependencies.Format, FormatVersion: p.input.ResolvedDependencies.FormatVersion,
			Language: p.input.ResolvedDependencies.Language, Installs: installs,
		},
	}, nil
}

func (p preparedInput) materializeMetadata(ctx context.Context, resolved resolve.Result) (materialize.ResolvedMetadata, error) {
	closures := make([]materialize.ResolvedPackClosure, 0, len(resolved.Dependencies))
	closureByCanonical := map[string]materialize.ResolvedPackClosure{}
	for _, summary := range resolved.Dependencies {
		if err := pollContext(ctx, stageMaterialize); err != nil {
			return materialize.ResolvedMetadata{}, err
		}
		candidates := p.byCanonical[summary.CanonicalID]
		if len(candidates) == 0 {
			return materialize.ResolvedMetadata{}, fmt.Errorf("selected Pack metadata is unavailable")
		}
		pack := candidates[0]
		files := make([]materialize.ResolvedPackFile, len(pack.files))
		for i, file := range pack.files {
			files[i] = materialize.ResolvedPackFile{Path: file.Path, Digest: file.Digest}
		}
		dependencies := make([]materialize.ResolvedPackDependency, 0, len(pack.input.Dependencies))
		for _, dependency := range pack.input.Dependencies {
			target := p.byInstall[dependency.InstallName]
			if target == nil {
				return materialize.ResolvedMetadata{}, fmt.Errorf("selected Pack dependency is unavailable")
			}
			dependencies = append(dependencies, materialize.ResolvedPackDependency{
				LocalName: dependency.LocalName, InstallName: dependency.InstallName,
				CanonicalID: target.input.CanonicalID, Version: target.input.Version, Digest: target.input.Digest,
			})
		}
		sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].LocalName < dependencies[j].LocalName })
		closure := materialize.ResolvedPackClosure{
			ResolvedPackSummary: materialize.ResolvedPackSummary{Address: summary.Address, CanonicalID: summary.CanonicalID, Version: summary.Version, Digest: summary.Digest},
			Path:                pack.input.Path, Entry: pack.input.Entry, Files: files, Dependencies: dependencies,
			Manifest: materialize.ResolvedPackManifest{
				Format: pack.manifest.Format, FormatVersion: pack.manifest.FormatVersion, ID: pack.manifest.ID,
				Name: pack.manifest.Name, Version: pack.manifest.Version, Language: pack.manifest.Language,
				Entry: pack.manifest.Entry, Path: pack.input.ManifestPath, Bytes: bytes.Clone(pack.input.Manifest),
			},
		}
		closures = append(closures, closure)
		closureByCanonical[summary.CanonicalID] = closure
	}
	sources := []materialize.ResolvedSourceFile{}
	seenSources := map[string]bool{}
	for _, module := range resolved.Modules {
		if err := pollContext(ctx, stageMaterialize); err != nil {
			return materialize.ResolvedMetadata{}, err
		}
		origin, sourceBytes, ok := p.moduleSource(module, closureByCanonical)
		if !ok {
			return materialize.ResolvedMetadata{}, fmt.Errorf("resolved module bytes are unavailable")
		}
		key := string(origin.Kind) + "\x00" + origin.PackAddress + "\x00" + module.Path
		if !seenSources[key] {
			sources = append(sources, materialize.ResolvedSourceFile{Origin: origin, ModulePath: module.Path, Bytes: bytes.Clone(sourceBytes)})
			seenSources[key] = true
		}
	}
	for _, closure := range closures {
		if err := pollContext(ctx, stageMaterialize); err != nil {
			return materialize.ResolvedMetadata{}, err
		}
		pack := p.byCanonical[closure.CanonicalID][0]
		origin := resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: closure.Address}
		for _, file := range pack.files {
			if !strings.HasSuffix(file.Path, ".ldl") {
				continue
			}
			key := string(origin.Kind) + "\x00" + origin.PackAddress + "\x00" + file.Path
			if !seenSources[key] {
				sources = append(sources, materialize.ResolvedSourceFile{Origin: origin, ModulePath: file.Path, Bytes: bytes.Clone(pack.fileBytes[file.Path])})
				seenSources[key] = true
			}
		}
	}
	assets := make([]materialize.ResolvedAsset, 0, len(p.input.ReferencedAssets))
	for _, asset := range p.input.ReferencedAssets {
		if err := pollContext(ctx, stageMaterialize); err != nil {
			return materialize.ResolvedMetadata{}, err
		}
		origin := resolve.SourceOrigin{Kind: resolve.OriginProject}
		if asset.Origin == SourceOriginPack {
			closure, exists := closureByCanonical[asset.PackID]
			if !exists {
				origin = resolve.SourceOrigin{Kind: resolve.OriginPack}
			} else {
				origin = resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: closure.Address}
			}
		}
		assets = append(assets, materialize.ResolvedAsset{
			Origin: origin, Locator: asset.Locator, Bytes: bytes.Clone(asset.Bytes), ExpectedDigest: asset.Digest,
			ExpectedMediaType: asset.MediaType, ExpectedByteLength: asset.ByteLength,
		})
	}
	sort.Slice(sources, func(i, j int) bool {
		return lessSource(sources[i].Origin, sources[i].ModulePath, sources[j].Origin, sources[j].ModulePath)
	})
	return materialize.ResolvedMetadata{SelectedClosure: closures, Assets: assets, SourceFiles: sources}, nil
}

func (p preparedInput) moduleSource(module resolve.ResolvedModule, closures map[string]materialize.ResolvedPackClosure) (resolve.SourceOrigin, []byte, bool) {
	if module.Origin.Kind == resolve.OriginProject {
		value, ok := p.input.ProjectSourceTree[module.Path]
		return resolve.SourceOrigin{Kind: resolve.OriginProject}, value, ok
	}
	canonicalID := module.Origin.Publisher + "/" + module.Origin.PackName
	closure, ok := closures[canonicalID]
	if !ok || len(p.byCanonical[canonicalID]) == 0 {
		return resolve.SourceOrigin{}, nil, false
	}
	value, exists := p.byCanonical[canonicalID][0].fileBytes[module.Path]
	return resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: closure.Address}, value, exists
}

func (p preparedInput) losslessSyntaxTree(ctx context.Context, resolved resolve.Result, closures []materialize.ResolvedPackClosure) (LosslessSyntaxTree, error) {
	closureByCanonical := map[string]materialize.ResolvedPackClosure{}
	for _, closure := range closures {
		closureByCanonical[closure.CanonicalID] = closure
	}
	files := make([]LosslessSyntaxFile, 0, len(resolved.Modules))
	for _, module := range resolved.Modules {
		if err := pollContext(ctx, stageComplete); err != nil {
			return LosslessSyntaxTree{}, err
		}
		origin, sourceBytes, ok := p.moduleSource(module, closureByCanonical)
		if !ok {
			return LosslessSyntaxTree{}, fmt.Errorf("lossless source bytes are unavailable")
		}
		files = append(files, LosslessSyntaxFile{
			Origin: origin, ModulePath: module.Path, Source: bytes.Clone(sourceBytes),
			Root: module.File.Root, Tokens: append([]syntax.Token{}, module.File.Tokens...),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return lessSource(files[i].Origin, files[i].ModulePath, files[j].Origin, files[j].ModulePath)
	})
	return deepClone(LosslessSyntaxTree{Files: files}), nil
}

func lessSource(left resolve.SourceOrigin, leftPath string, right resolve.SourceOrigin, rightPath string) bool {
	leftRank, rightRank := 1, 1
	if left.Kind == resolve.OriginProject {
		leftRank = 0
	}
	if right.Kind == resolve.OriginProject {
		rightRank = 0
	}
	if leftRank != rightRank {
		return leftRank < rightRank
	}
	if left.PackAddress != right.PackAddress {
		return left.PackAddress < right.PackAddress
	}
	return leftPath < rightPath
}

func completeOutput(
	limits ResourceLimits,
	resolved resolve.Result,
	defined definition.Result,
	graphed graph.Result,
	queried query.Result,
	viewed view.Result,
	lossless LosslessSyntaxTree,
	materialized materialize.Snapshot,
	indexed index.Snapshot,
	diagnostics []resolve.Diagnostic,
) CompileOutput {
	addresses := make([]string, len(indexed.SemanticIndex.Subjects))
	classifications := make([]AuthoringSubjectClassification, 0, len(indexed.SemanticIndex.Subjects))
	for i, subject := range indexed.SemanticIndex.Subjects {
		addresses[i] = subject.Address
		if capability, ok := subjectCapability(subject.Kind); ok {
			classifications = append(classifications, AuthoringSubjectClassification{Address: subject.Address, Kind: subject.Kind, Capability: capability})
		}
	}
	clonedDiagnostics := resolve.CloneDiagnostics(diagnostics)
	resolve.SortDiagnostics(clonedDiagnostics)
	return CompileOutput{
		Mode: CompileMode(resolved.Mode), EffectiveLimits: limits, LosslessSyntaxTree: lossless,
		TypedAST: TypedAST{
			Root: defined.Root, Project: defined.Project, Pack: defined.Pack,
			EntityTypes: defined.EntityTypes, RelationTypes: defined.RelationTypes, Layers: defined.Layers,
			References: defined.References, Graph: graphed.Graph, Queries: queried.Recipes,
			Views: viewed.Recipes, Exports: viewed.ExportRecipes.Recipes,
		},
		NormalizedDocument: materialized.Document, NormalizedPackArtifact: materialized.Pack,
		CanonicalJSON: materialized.CanonicalJSON, ArtifactJSON: materialized.ArtifactJSON,
		SourceMap: indexed.SourceMap, SemanticIndex: indexed.SemanticIndex, StableAddresses: addresses,
		DefinitionHash: materialized.Hashes.Definition, GraphHash: materialized.Hashes.Graph,
		SubjectSemanticHashes: materialized.Hashes.OwnSubjects, SubtreeHashes: materialized.Hashes.Subtrees,
		ChildSetHashes: materialized.Hashes.ChildSets, AuthoringSubjectClassification: classifications,
		CompiledQueryRecipes: queried.Recipes, CompiledViewRecipes: viewed.Recipes,
		CompiledExportRecipes: viewed.ExportRecipes.Recipes, SearchDocuments: indexed.SearchDocuments,
		Diagnostics: clonedDiagnostics,
	}
}

func subjectCapability(kind materialize.SubjectKind) (AuthoringCapability, bool) {
	switch kind {
	case materialize.SubjectProject:
		return CapabilityProjectConfigure, true
	case materialize.SubjectEntityType, materialize.SubjectRelationType, materialize.SubjectLayer,
		materialize.SubjectEntityTypeColumn, materialize.SubjectEntityTypeConstraint,
		materialize.SubjectRelationTypeColumn, materialize.SubjectRelationTypeConstraint:
		return CapabilitySchemaWrite, true
	case materialize.SubjectEntity, materialize.SubjectRelation, materialize.SubjectEntityRow, materialize.SubjectRelationRow:
		return CapabilityGraphWrite, true
	case materialize.SubjectQuery, materialize.SubjectQueryParameter:
		return CapabilityQueryWrite, true
	case materialize.SubjectView, materialize.SubjectViewTableColumn, materialize.SubjectViewExport:
		return CapabilityViewWrite, true
	case materialize.SubjectReference:
		return CapabilityReferenceWrite, true
	default:
		return "", false
	}
}

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}
