// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package resolve implements the pure LDL module and identity resolution boundary.
package resolve

import "github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"

type CompileMode string

const (
	CompileProject CompileMode = "project"
	CompilePack    CompileMode = "pack"
)

type SourceFile struct {
	Root        *syntax.Node
	Tokens      []syntax.Token
	Diagnostics []syntax.Diagnostic
}

func SourceFromParse(result syntax.ParseResult) SourceFile {
	return SourceFile{Root: result.Root, Tokens: result.Tokens, Diagnostics: result.Diagnostics}
}

type Input struct {
	Mode      CompileMode
	EntryPath string
	Project   ProjectInput
	Packs     ResolvedDependencies
}

type ProjectInput struct {
	Files map[string]SourceFile
}

type ResolvedDependencies struct {
	Format        string
	FormatVersion int
	Language      int
	Installs      map[string]ResolvedPack
}

type ResolvedPack struct {
	CanonicalID  string
	Version      string
	Digest       string
	Path         string
	Entry        string
	Files        map[string]string
	Dependencies map[string]string
	Manifest     PackManifest
	SourceFiles  map[string]SourceFile
}

type PackManifest struct {
	Format        string
	FormatVersion int
	ID            string
	Name          string
	Version       string
	Language      int
	Entry         string
	Dependencies  map[string]PackDependency
}

type PackDependency struct {
	ID      string
	Version string
}

type Result struct {
	Modules      []ResolvedModule
	Exports      []ExportBinding
	Bindings     []SourceBinding
	Declarations []DeclarationSymbol
	Identity     IdentityHistory
	Dependencies []ResolvedPackSummary
	Diagnostics  []Diagnostic
	HasErrors    bool
}

type OriginKind string

const (
	OriginProject OriginKind = "project"
	OriginPack    OriginKind = "pack"
)

type Origin struct {
	Kind      OriginKind
	ProjectID string
	Publisher string
	PackName  string
}

type ModuleKey struct {
	Origin Origin
	Path   string
}

type ModuleKind string

const (
	ModuleProjectEntry ModuleKind = "project_entry"
	ModuleProject      ModuleKind = "project_module"
	ModulePackEntry    ModuleKind = "pack_entry"
	ModulePack         ModuleKind = "pack_module"
)

type ResolvedModule struct {
	Origin  Origin
	Path    string
	Kind    ModuleKind
	File    SourceFile
	Imports []ImportDecl
	Exports []ExportDecl
}

type ImportKind string

const (
	ImportNamespace ImportKind = "namespace"
	ImportNamed     ImportKind = "named"
)

type ImportDecl struct {
	Kind      ImportKind
	Alias     string
	Items     []ImportItem
	Specifier string
	Range     syntax.Span
	Module    ModuleKey
}

type ImportItem struct {
	Remote string
	Local  string
	Range  syntax.Span
}

type ExportKind string

const (
	ExportLocal ExportKind = "local"
	ExportFrom  ExportKind = "from"
	ExportStar  ExportKind = "star"
)

type ExportDecl struct {
	Kind      ExportKind
	Items     []ExportItem
	Specifier string
	Range     syntax.Span
	Module    ModuleKey
}

type ExportItem struct {
	Local  string
	Public string
	Range  syntax.Span
}

type SubjectKind string

const (
	KindProject      SubjectKind = "project"
	KindPack         SubjectKind = "pack"
	KindEntityType   SubjectKind = "entity-type"
	KindRelationType SubjectKind = "relation-type"
	KindLayer        SubjectKind = "layer"
	KindEntity       SubjectKind = "entity"
	KindRelation     SubjectKind = "relation"
	KindQuery        SubjectKind = "query"
	KindView         SubjectKind = "view"
	KindReference    SubjectKind = "reference"
	KindColumn       SubjectKind = "column"
	KindConstraint   SubjectKind = "constraint"
	KindRow          SubjectKind = "row"
	KindParameter    SubjectKind = "parameter"
	KindTableColumn  SubjectKind = "table-column"
	KindExport       SubjectKind = "export"
)

type StableSymbol struct {
	Origin Origin
	Path   []SymbolSegment
}

type SymbolSegment struct {
	Kind SubjectKind
	ID   string
}

type DeclarationSymbol struct {
	Symbol        StableSymbol
	Address       string
	Kind          SubjectKind
	ID            string
	Owner         *StableSymbol
	Module        ModuleKey
	Range         syntax.Span
	ExportedNames []string
}

type SourceBinding struct {
	Module        ModuleKey
	ExpectedKind  SubjectKind
	SourceText    string
	Range         syntax.Span
	Target        StableSymbol
	TargetAddress string
	Via           string
}

type ExportBinding struct {
	Module        ModuleKey
	PublicName    string
	Target        StableSymbol
	TargetAddress string
	Range         syntax.Span
	ReExport      bool
}

type IdentityHistory struct {
	Reservations []Reservation
	Moves        []Move
	MoveClosure  []MoveClosure
}

type Reservation struct {
	Owner   StableSymbol
	Kind    SubjectKind
	ID      string
	Address string
	Range   syntax.Span
}

type Move struct {
	Origin      Origin
	Kind        SubjectKind
	OwnerID     string
	From        string
	To          string
	FromAddress string
	ToAddress   string
	Range       syntax.Span
}

type MoveClosure struct {
	From string
	To   string
}

type ResolvedPackSummary struct {
	Address     string
	CanonicalID string
	Version     string
	Digest      string
}

type SourceOrigin struct {
	Kind        OriginKind
	PackAddress string
}

type SourceRange struct {
	Origin     SourceOrigin
	ModulePath string
	StartByte  int
	EndByte    int
}

type Diagnostic struct {
	Code           string
	Severity       string
	MessageKey     string
	Arguments      map[string]string
	Message        string
	Range          *SourceRange
	SubjectAddress string
	OwnerAddress   string
	Related        []DiagnosticRelated
}

type DiagnosticRelated struct {
	Relation       string
	Message        string
	Range          *SourceRange
	SubjectAddress string
	OwnerAddress   string
}
