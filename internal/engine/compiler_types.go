// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"errors"
	"fmt"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/exportrecipe"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

// CompileMode selects the only two Language 1 compiler roots.
type CompileMode string

const (
	CompileProject CompileMode = "project"
	CompilePack    CompileMode = "pack"
)

// CompileInput is a closed, in-memory compiler input. The compiler never
// supplements it from a filesystem, network, credential, state, or clock.
type CompileInput struct {
	Mode                 CompileMode
	EntryPath            string
	RootPackID           string
	ProjectSourceTree    map[string][]byte
	InstalledPackTree    map[string][]byte
	ResolvedDependencies ResolvedDependencies
	ReferencedAssets     []AssetInput
	ResourceLimits       ResourceLimits
}

// ResolvedDependencies is the exact dependency metadata paired with an
// InstalledPackTree. Installs is a slice so duplicate metadata is rejected
// rather than being silently overwritten by a map assignment.
type ResolvedDependencies struct {
	Format        string
	FormatVersion int
	Language      int
	Installs      []ResolvedPack
}

// ResolvedPack binds one install name to immutable installed bytes.
type ResolvedPack struct {
	InstallName string
	CanonicalID string
	Version     string
	Digest      string
	Path        string
	Entry       string
	// RegistrySource is portable, non-secret source identity metadata. The
	// compiler excludes it from semantic identity; container I/O preserves it.
	RegistrySource string
	Files          []ResolvedPackFile
	Dependencies   []ResolvedPackDependency
	ManifestPath   string
	Manifest       []byte
}

// ResolvedPackFile binds a pack-relative path to its raw digest.
type ResolvedPackFile struct {
	Path   string
	Digest string
}

// ResolvedPackDependency maps a dependency-local source name to an install.
type ResolvedPackDependency struct {
	LocalName   string
	InstallName string
}

// SourceOriginKind identifies the portable origin of source or asset bytes.
type SourceOriginKind string

const (
	SourceOriginProject SourceOriginKind = "project"
	SourceOriginPack    SourceOriginKind = "pack"
)

// AssetInput binds referenced bytes to the metadata already fixed by the
// closed project or installed Pack tree.
type AssetInput struct {
	Origin     SourceOriginKind
	PackID     string
	Locator    string
	Bytes      []byte
	Digest     string
	MediaType  string
	ByteLength int64
}

// ResourceLimits contains every limit enforced by the facade. Zero selects
// the corresponding safe default; negative values are invalid.
type ResourceLimits struct {
	MaxProjectSourceFiles int64
	MaxProjectSourceBytes int64
	MaxPackFiles          int64
	MaxPackBytes          int64
	MaxAssets             int64
	MaxAssetBytes         int64
	MaxRasterDimension    int64
	MaxRasterPixels       int64
	MaxDeclarations       int64
}

// DefaultResourceLimits returns the deterministic zero-value policy.
func DefaultResourceLimits() ResourceLimits {
	return ResourceLimits{
		MaxProjectSourceFiles: 4_096,
		MaxProjectSourceBytes: 64 << 20,
		MaxPackFiles:          16_384,
		MaxPackBytes:          256 << 20,
		MaxAssets:             4_096,
		MaxAssetBytes:         256 << 20,
		MaxRasterDimension:    32_768,
		MaxRasterPixels:       64 << 20,
		MaxDeclarations:       1_000_000,
	}
}

// Effective returns the explicit limits used by Compile. The bool is false
// when any field is negative.
func (l ResourceLimits) Effective() (ResourceLimits, bool) {
	defaults := DefaultResourceLimits()
	values := []*int64{
		&l.MaxProjectSourceFiles,
		&l.MaxProjectSourceBytes,
		&l.MaxPackFiles,
		&l.MaxPackBytes,
		&l.MaxAssets,
		&l.MaxAssetBytes,
		&l.MaxRasterDimension,
		&l.MaxRasterPixels,
		&l.MaxDeclarations,
	}
	fallbacks := []int64{
		defaults.MaxProjectSourceFiles,
		defaults.MaxProjectSourceBytes,
		defaults.MaxPackFiles,
		defaults.MaxPackBytes,
		defaults.MaxAssets,
		defaults.MaxAssetBytes,
		defaults.MaxRasterDimension,
		defaults.MaxRasterPixels,
		defaults.MaxDeclarations,
	}
	for i, value := range values {
		if *value < 0 {
			return ResourceLimits{}, false
		}
		if *value == 0 {
			*value = fallbacks[i]
		}
	}
	return l, true
}

// ErrorCategory is a stable non-semantic compiler failure category.
type ErrorCategory string

const (
	ErrorCategoryCancelled ErrorCategory = "cancelled"
	ErrorCategoryResource  ErrorCategory = "resource"
	ErrorCategoryInvariant ErrorCategory = "invariant"
)

const (
	ErrorCodeCancelled                  = "engine.compile.cancelled"
	ErrorCodeInvalidResourceLimits      = "engine.compile.invalid_resource_limits"
	ErrorCodeProjectSourceFilesExceeded = "engine.compile.project_source_files_exceeded"
	ErrorCodeProjectSourceBytesExceeded = "engine.compile.project_source_bytes_exceeded"
	ErrorCodePackFilesExceeded          = "engine.compile.pack_files_exceeded"
	ErrorCodePackBytesExceeded          = "engine.compile.pack_bytes_exceeded"
	ErrorCodeAssetsExceeded             = "engine.compile.assets_exceeded"
	ErrorCodeAssetBytesExceeded         = "engine.compile.asset_bytes_exceeded"
	ErrorCodeRasterDimensionExceeded    = "engine.compile.raster_dimension_exceeded"
	ErrorCodeRasterPixelsExceeded       = "engine.compile.raster_pixels_exceeded"
	ErrorCodeDeclarationsExceeded       = "engine.compile.declarations_exceeded"
	ErrorCodeInvariantFailure           = "engine.compile.invariant_failure"
)

// CompileError reports cancellation, resource exhaustion, or an internal
// invariant failure without publishing any part of a CompileResult.
type CompileError struct {
	Code     string
	Category ErrorCategory
	Resource string
	Limit    int64
	Observed int64
	Stage    string
	cause    error
}

func (e *CompileError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Resource != "" {
		return fmt.Sprintf("%s: %s observed %d exceeds limit %d", e.Code, e.Resource, e.Observed, e.Limit)
	}
	if e.Stage != "" {
		return fmt.Sprintf("%s at %s", e.Code, e.Stage)
	}
	return e.Code
}

// Unwrap preserves context cancellation identity without exposing internal
// failure details.
func (e *CompileError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// IsCompileError reports whether err has the requested stable category.
func IsCompileError(err error, category ErrorCategory) bool {
	var compileError *CompileError
	return errors.As(err, &compileError) && compileError.Category == category
}

// The facade aliases stable in-process semantic values without exposing any
// internal stage Result. These aliases are not new public wire schemas.
type (
	SyntaxNode             = syntax.Node
	SyntaxToken            = syntax.Token
	Diagnostic             = resolve.Diagnostic
	DiagnosticRelated      = resolve.DiagnosticRelated
	SourceRange            = resolve.SourceRange
	SourceOrigin           = resolve.SourceOrigin
	TypedRoot              = definition.Root
	TypedProject           = definition.Project
	TypedPack              = definition.Pack
	TypedEntityType        = definition.EntityType
	TypedRelationType      = definition.RelationType
	TypedLayer             = definition.Layer
	TypedReference         = definition.Reference
	TypedScalar            = definition.Scalar
	TypedMasterGraph       = graph.MasterGraph
	CompiledQueryRecipe    = query.Recipe
	CompiledViewRecipe     = view.Recipe
	CompiledExportRecipe   = exportrecipe.Recipe
	NormalizedDocument     = materialize.NormalizedDocument
	NormalizedPackArtifact = materialize.NormalizedPackArtifact
	SubjectHash            = materialize.SubjectHash
	SubtreeHash            = materialize.SubtreeHash
	ChildSetHash           = materialize.ChildSetHash
	SemanticSubjectKind    = materialize.SubjectKind
	SourceMap              = index.SourceMapV1
	SemanticIndex          = index.SemanticIndexV1
	SearchDocument         = index.SearchDocument
	SearchField            = index.SearchField
)

// LosslessSyntaxTree retains every loaded module's source, CST, and tokens.
type LosslessSyntaxTree struct {
	Files []LosslessSyntaxFile
}

type LosslessSyntaxFile struct {
	Origin     SourceOrigin
	ModulePath string
	Source     []byte
	Root       *SyntaxNode
	Tokens     []SyntaxToken
}

// TypedAST is the complete typed, renderer-independent projection assembled
// by the definition, graph, Query, View, and Export recipe stages.
type TypedAST struct {
	Root          TypedRoot
	Project       *TypedProject
	Pack          *TypedPack
	EntityTypes   []TypedEntityType
	RelationTypes []TypedRelationType
	Layers        []TypedLayer
	References    []TypedReference
	Graph         *TypedMasterGraph
	Queries       []CompiledQueryRecipe
	Views         []CompiledViewRecipe
	Exports       []CompiledExportRecipe
}

// AuthoringCapability is the closed subject classification vocabulary used by
// later Workbench diff classification.
type AuthoringCapability string

const (
	CapabilitySchemaWrite      AuthoringCapability = "schema:write"
	CapabilityGraphWrite       AuthoringCapability = "graph:write"
	CapabilityQueryWrite       AuthoringCapability = "query:write"
	CapabilityViewWrite        AuthoringCapability = "view:write"
	CapabilityReferenceWrite   AuthoringCapability = "reference:write"
	CapabilitySourceMaintain   AuthoringCapability = "source:maintain"
	CapabilityProjectConfigure AuthoringCapability = "project:configure"
)

type AuthoringSubjectClassification struct {
	Address    string
	Kind       SemanticSubjectKind
	Capability AuthoringCapability
}

// CompileOutput is one complete semantic generation. On semantic rejection,
// only Diagnostics is populated.
type CompileOutput struct {
	Mode                           CompileMode
	EffectiveLimits                ResourceLimits
	LosslessSyntaxTree             LosslessSyntaxTree
	TypedAST                       TypedAST
	NormalizedDocument             *NormalizedDocument
	NormalizedPackArtifact         *NormalizedPackArtifact
	CanonicalJSON                  []byte
	ArtifactJSON                   []byte
	SourceMap                      SourceMap
	SemanticIndex                  SemanticIndex
	StableAddresses                []string
	DefinitionHash                 string
	GraphHash                      *string
	SubjectSemanticHashes          []SubjectHash
	SubtreeHashes                  []SubtreeHash
	ChildSetHashes                 []ChildSetHash
	AuthoringSubjectClassification []AuthoringSubjectClassification
	CompiledQueryRecipes           []CompiledQueryRecipe
	CompiledViewRecipes            []CompiledViewRecipe
	CompiledExportRecipes          []CompiledExportRecipe
	SearchDocuments                []SearchDocument
	Diagnostics                    []Diagnostic
}

// CompileResult is the return value of Engine.Compile. Its visible fields and
// all snapshots own independent mutable storage.
type CompileResult struct {
	CompileOutput
	state *compileResultState
}

type compileResultState struct {
	output CompileOutput
}

// Snapshot is a defensive in-process copy, not an Engine Protocol wire type.
type Snapshot struct {
	CompileOutput
}

// Snapshot returns storage independent from the CompileResult and from every
// previously returned Snapshot.
func (r CompileResult) Snapshot() Snapshot {
	if r.state == nil {
		return Snapshot{}
	}
	return Snapshot{CompileOutput: deepClone(r.state.output)}
}
