// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

type resolver struct {
	input           Input
	project         Origin
	rootCanonicalID string
	packs           map[string]packInfo
	aliases         map[string]string
	modules         map[ModuleKey]*moduleState
	visiting        map[ModuleKey]bool
	diagnostics     []Diagnostic
	syntaxInvalid   bool
	invalidInput    bool
	symbols         map[string]DeclarationSymbol
	selected        map[string]bool
}

type packInfo struct {
	install string
	origin  Origin
	pack    ResolvedPack
}

type moduleState struct {
	key              ModuleKey
	kind             ModuleKind
	file             SourceFile
	ast              moduleAST
	exports          map[string]DeclarationSymbol
	exportBindings   []ExportBinding
	localTop         map[SubjectKind]map[string]DeclarationSymbol
	localByAddress   map[string]DeclarationSymbol
	imported         map[SubjectKind]map[string]DeclarationSymbol
	bindings         []SourceBinding
	namespaceAliases map[string]syntax.Span
	namedAliases     map[SubjectKind]map[string]syntax.Span
	reservations     []Reservation
	moves            []Move
	moveClosure      []MoveClosure
	finalState       evalState
	exportState      evalState
	exportCycleDiag  bool
}

type evalState uint8

const (
	evalUnvisited evalState = iota
	evalVisiting
	evalDone
)

type moveEdge struct {
	move Move
	from string
	to   string
}

func Resolve(input Input) Result {
	r := &resolver{
		input:    input,
		packs:    map[string]packInfo{},
		aliases:  map[string]string{},
		modules:  map[ModuleKey]*moduleState{},
		visiting: map[ModuleKey]bool{},
		symbols:  map[string]DeclarationSymbol{},
		selected: map[string]bool{},
	}
	r.resolve()
	result := r.result()
	result.stageGeneration = newStageGeneration()
	return result
}

func (r *resolver) resolve() {
	r.validateProjectFiles()
	r.validatePacks()
	if r.input.Mode == "" {
		r.input.Mode = CompileProject
	}
	entry, ok := normalizeModulePath(r.input.EntryPath)
	if !ok || entry != r.input.EntryPath || !strings.HasSuffix(entry, ".ldl") {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid entry module path", ModuleKey{Origin: Origin{Kind: OriginProject}, Path: r.input.EntryPath}, zeroSpan())
		r.invalidInput = true
		return
	}
	if r.input.Mode != CompileProject && r.input.Mode != CompilePack {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "unsupported compile mode", ModuleKey{Origin: Origin{Kind: OriginProject}, Path: r.input.EntryPath}, zeroSpan())
		r.invalidInput = true
		return
	}
	if r.input.Mode == CompilePack {
		r.resolvePackMode(entry)
		return
	}
	if r.input.RootPackID != "" {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "root pack id is only valid in pack mode", ModuleKey{Origin: Origin{Kind: OriginProject}, Path: r.input.EntryPath}, zeroSpan())
		r.invalidInput = true
		return
	}
	origin, ok := r.projectOrigin(entry)
	if !ok {
		r.invalidInput = true
		return
	}
	r.project = origin
	entryKey := ModuleKey{Origin: origin, Path: entry}
	entryState := r.loadModule(entryKey)
	if entryState == nil || r.invalidInput {
		return
	}
	if r.syntaxInvalid {
		r.validateLoadedReservationSchemas()
		return
	}
	r.finalizeAllModules()
	identityDiagnosticStart := len(r.diagnostics)
	r.validateAllIdentity()
	if r.hasBlockingIdentityErrors(identityDiagnosticStart) {
		return
	}
	r.selectProject(entryState)
	r.validateSelectedDeclarationRefs()
	r.validateSelectedFactGroupRefs()
}

func (r *resolver) resolvePackMode(entry string) {
	if r.input.RootPackID == "" {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "pack compile requires root pack id", ModuleKey{Origin: Origin{Kind: OriginPack}, Path: entry}, zeroSpan())
		r.invalidInput = true
		return
	}
	var matches []packInfo
	for _, install := range sortedKeysPack(r.packs) {
		info := r.packs[install]
		if info.pack.CanonicalID == r.input.RootPackID {
			matches = append(matches, info)
		}
	}
	if len(matches) == 0 {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "root pack id is not installed", ModuleKey{Origin: Origin{Kind: OriginPack}, Path: entry}, zeroSpan())
		r.invalidInput = true
		return
	}
	info := matches[0]
	r.project = info.origin
	r.rootCanonicalID = info.pack.CanonicalID
	if entry != info.pack.Entry {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "pack compile entry must equal pack manifest entry", ModuleKey{Origin: info.origin, Path: entry}, zeroSpan())
		r.invalidInput = true
		return
	}
	entryState := r.loadModule(ModuleKey{Origin: info.origin, Path: entry})
	if entryState == nil || r.invalidInput {
		return
	}
	if r.syntaxInvalid {
		r.validateLoadedReservationSchemas()
		return
	}
	r.finalizeAllModules()
	identityDiagnosticStart := len(r.diagnostics)
	r.validateAllIdentity()
	if r.hasBlockingIdentityErrors(identityDiagnosticStart) {
		return
	}
	r.selectPack(entryState)
	r.validateSelectedDeclarationRefs()
	r.validateSelectedFactGroupRefs()
}

func (r *resolver) hasBlockingIdentityErrors(identityDiagnosticStart int) bool {
	for i, diagnostic := range r.diagnostics {
		if i < identityDiagnosticStart || diagnostic.Code != "LDL1102" {
			return true
		}
	}
	return false
}

func (r *resolver) validateProjectFiles() {
	paths := make([]string, 0, len(r.input.Project.Files))
	for raw := range r.input.Project.Files {
		norm, ok := normalizeModulePath(raw)
		if !ok || norm != raw || !strings.HasSuffix(raw, ".ldl") {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid project source path", ModuleKey{Origin: Origin{Kind: OriginProject}, Path: raw}, zeroSpan())
			r.invalidInput = true
		}
		paths = append(paths, raw)
	}
	sort.Strings(paths)
	for _, pair := range caseFoldCollisions(paths) {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "case-folding source path collision", ModuleKey{Origin: Origin{Kind: OriginProject}, Path: pair[1]}, zeroSpan())
		r.invalidInput = true
	}
}

func (r *resolver) validatePacks() {
	if len(r.input.Packs.Installs) == 0 {
		return
	}
	if r.input.Packs.Format != "layerdraw-resolved" || r.input.Packs.FormatVersion != 1 || r.input.Packs.Language != 1 {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid resolved dependency envelope", ModuleKey{Origin: Origin{Kind: OriginProject}}, zeroSpan())
		r.invalidInput = true
	}
	seenCanonical := map[string]string{}
	seenPath := map[string]string{}
	installs := sortedKeysResolved(r.input.Packs.Installs)
	for _, install := range installs {
		pack := r.input.Packs.Installs[install]
		origin := Origin{Kind: OriginPack}
		if !isIdent(install) {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid installed pack alias", ModuleKey{Origin: Origin{Kind: OriginProject}, Path: pack.Entry}, zeroSpan())
			r.invalidInput = true
			continue
		}
		pub, name, ok := parseCanonicalID(pack.CanonicalID)
		if !ok {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid canonical pack id", ModuleKey{Origin: Origin{Kind: OriginProject}, Path: pack.Entry}, zeroSpan())
			r.invalidInput = true
			continue
		}
		origin = Origin{Kind: OriginPack, Publisher: pub, PackName: name}
		entry, entryOK := normalizeModulePath(pack.Entry)
		root, rootOK := normalizePortablePath(pack.Path)
		if !entryOK || entry != pack.Entry || !strings.HasSuffix(entry, ".ldl") {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid pack entry path", ModuleKey{Origin: origin, Path: pack.Entry}, zeroSpan())
			r.invalidInput = true
		}
		if !rootOK || root != pack.Path {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid installed pack path", ModuleKey{Origin: origin, Path: pack.Entry}, zeroSpan())
			r.invalidInput = true
		}
		if !isExactSemver(pack.Version) || !isDigest(pack.Digest) {
			r.diag("LDL1203", "dependency_digest_mismatch", "invalid pack version or digest", ModuleKey{Origin: origin, Path: pack.Entry}, zeroSpan())
			r.invalidInput = true
		}
		if prev, exists := seenPath[root]; exists && prev != pack.CanonicalID {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "installed pack path collision", ModuleKey{Origin: origin, Path: entry}, zeroSpan())
			r.invalidInput = true
		}
		seenPath[root] = pack.CanonicalID
		identity := pack.Version + "|" + pack.Digest + "|" + pack.Entry + "|" + digestFileMap(pack.Files)
		if prev, exists := seenCanonical[pack.CanonicalID]; exists && prev != identity {
			r.diag("LDL1203", "dependency_digest_mismatch", "canonical pack alias has different resolved content", ModuleKey{Origin: origin, Path: entry}, zeroSpan())
			r.invalidInput = true
		}
		seenCanonical[pack.CanonicalID] = identity
		r.validateManifest(install, origin, pack)
		r.validatePackFiles(origin, pack)
		r.packs[install] = packInfo{install: install, origin: origin, pack: pack}
		r.aliases[pack.CanonicalID] = install
	}
	r.validateDependencyGraph()
}

func (r *resolver) validateManifest(install string, origin Origin, pack ResolvedPack) {
	m := pack.Manifest
	if m.Format != "layerdraw-pack" || m.FormatVersion != 1 || m.Language != 1 || m.ID != pack.CanonicalID || m.Version != pack.Version || m.Entry != pack.Entry || !isIdent(m.Name) {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "pack manifest metadata does not match resolved metadata", ModuleKey{Origin: origin, Path: pack.Entry}, zeroSpan())
		r.invalidInput = true
	}
	if m.Name == install {
		// This is allowed; install aliases are project-local and often match manifest names.
	}
	if _, exists := m.Dependencies[m.Name]; exists {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "pack dependency-local name collides with manifest name", ModuleKey{Origin: origin, Path: pack.Entry}, zeroSpan())
		r.invalidInput = true
	}
	seenIDs := map[string]string{}
	for local, dep := range m.Dependencies {
		if !isIdent(local) || dep.ID == pack.CanonicalID || !isExactSemver(dep.Version) {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid pack manifest dependency", ModuleKey{Origin: origin, Path: pack.Entry}, zeroSpan())
			r.invalidInput = true
		}
		if _, _, ok := parseCanonicalID(dep.ID); !ok {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid pack manifest dependency id", ModuleKey{Origin: origin, Path: pack.Entry}, zeroSpan())
			r.invalidInput = true
		}
		if prev, ok := seenIDs[dep.ID]; ok && prev != local {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "duplicate canonical pack dependency", ModuleKey{Origin: origin, Path: pack.Entry}, zeroSpan())
			r.invalidInput = true
		}
		seenIDs[dep.ID] = local
		targetInstall, ok := pack.Dependencies[local]
		if !ok {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "manifest dependency missing from resolved metadata", ModuleKey{Origin: origin, Path: pack.Entry}, zeroSpan())
			r.invalidInput = true
			continue
		}
		target, ok := r.input.Packs.Installs[targetInstall]
		if !ok || target.CanonicalID != dep.ID || target.Version != dep.Version {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "resolved dependency target does not match manifest", ModuleKey{Origin: origin, Path: pack.Entry}, zeroSpan())
			r.invalidInput = true
		}
	}
	for local, target := range pack.Dependencies {
		if !isIdent(local) || target == "" {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid resolved dependency mapping", ModuleKey{Origin: origin, Path: pack.Entry}, zeroSpan())
			r.invalidInput = true
		}
		if _, ok := m.Dependencies[local]; !ok {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "resolved dependency missing from manifest", ModuleKey{Origin: origin, Path: pack.Entry}, zeroSpan())
			r.invalidInput = true
		}
		if _, ok := r.input.Packs.Installs[target]; !ok {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "resolved dependency target install is missing", ModuleKey{Origin: origin, Path: pack.Entry}, zeroSpan())
			r.invalidInput = true
		}
	}
}

func (r *resolver) validatePackFiles(origin Origin, pack ResolvedPack) {
	filePaths := make([]string, 0, len(pack.Files))
	for p, digest := range pack.Files {
		norm, ok := normalizePortablePath(p)
		if !ok || norm != p || !isDigest(digest) {
			r.diag("LDL1203", "dependency_digest_mismatch", "invalid pack file digest entry", ModuleKey{Origin: origin, Path: p}, zeroSpan())
			r.invalidInput = true
		}
		if strings.HasSuffix(p, ".ldl") {
			if norm, ok := normalizeModulePath(p); !ok || norm != p {
				r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid LDL source path in pack file map", ModuleKey{Origin: origin, Path: p}, zeroSpan())
				r.invalidInput = true
			}
			if _, ok := pack.SourceFiles[p]; !ok {
				r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "resolved LDL file is missing source", ModuleKey{Origin: origin, Path: p}, zeroSpan())
				r.invalidInput = true
			}
		}
		filePaths = append(filePaths, p)
	}
	sort.Strings(filePaths)
	for _, pair := range caseFoldCollisions(filePaths) {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "case-folding pack file path collision", ModuleKey{Origin: origin, Path: pair[1]}, zeroSpan())
		r.invalidInput = true
	}
	sourcePaths := make([]string, 0, len(pack.SourceFiles))
	for p := range pack.SourceFiles {
		norm, ok := normalizeModulePath(p)
		if !ok || norm != p || !strings.HasSuffix(p, ".ldl") {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid pack source path", ModuleKey{Origin: origin, Path: p}, zeroSpan())
			r.invalidInput = true
			continue
		}
		if _, ok := pack.Files[p]; !ok {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "pack source missing resolved digest", ModuleKey{Origin: origin, Path: p}, zeroSpan())
			r.invalidInput = true
		}
		sourcePaths = append(sourcePaths, p)
	}
	sort.Strings(sourcePaths)
	for _, pair := range caseFoldCollisions(sourcePaths) {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "case-folding pack source path collision", ModuleKey{Origin: origin, Path: pair[1]}, zeroSpan())
		r.invalidInput = true
	}
}

func (r *resolver) validateDependencyGraph() {
	state := map[string]int{}
	var visit func(string)
	visit = func(install string) {
		if state[install] == 1 {
			r.diag("LDL1202", "import_cycle", "resolved dependency cycle", ModuleKey{Origin: Origin{Kind: OriginProject}}, zeroSpan())
			r.invalidInput = true
			return
		}
		if state[install] == 2 {
			return
		}
		state[install] = 1
		pack := r.input.Packs.Installs[install]
		keys := sortedStringMapValues(pack.Dependencies)
		for _, target := range keys {
			if _, ok := r.input.Packs.Installs[target]; ok {
				visit(target)
			}
		}
		state[install] = 2
	}
	for _, install := range sortedKeysResolved(r.input.Packs.Installs) {
		visit(install)
	}
}

func digestFileMap(files map[string]string) string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(files[k])
		b.WriteByte('\n')
	}
	return b.String()
}

func (r *resolver) projectOrigin(entry string) (Origin, bool) {
	file, ok := r.input.Project.Files[entry]
	if !ok {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "project entry module not found", ModuleKey{Origin: Origin{Kind: OriginProject}, Path: entry}, zeroSpan())
		return Origin{}, false
	}
	ast := extractModule(file)
	var projects []rawDecl
	for _, decl := range ast.declarations {
		if decl.kind == KindProject {
			projects = append(projects, decl)
		}
	}
	if len(projects) != 1 || projects[0].id == "" {
		r.diag("LDL1302", "duplicate_or_reserved_identity", "project entry must declare exactly one project", ModuleKey{Origin: Origin{Kind: OriginProject}, Path: entry}, zeroSpan())
		return Origin{}, false
	}
	return Origin{Kind: OriginProject, ProjectID: projects[0].id}, true
}

func (r *resolver) loadModule(key ModuleKey) *moduleState {
	if r.visiting[key] {
		r.diag("LDL1202", "import_cycle", "import cycle", key, zeroSpan())
		return nil
	}
	if st := r.modules[key]; st != nil {
		return st
	}
	file, ok := r.sourceFile(key)
	if !ok {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "module not found in closed source tree", key, zeroSpan())
		return nil
	}
	r.mergeSyntaxDiagnostics(key, file)
	ast := extractModule(file)
	st := &moduleState{
		key:              key,
		kind:             r.moduleKind(key),
		file:             file,
		ast:              ast,
		localTop:         map[SubjectKind]map[string]DeclarationSymbol{},
		localByAddress:   map[string]DeclarationSymbol{},
		imported:         map[SubjectKind]map[string]DeclarationSymbol{},
		namespaceAliases: map[string]syntax.Span{},
		namedAliases:     map[SubjectKind]map[string]syntax.Span{},
	}
	r.modules[key] = st
	r.visiting[key] = true
	r.collectDeclarations(st)
	for i := range st.ast.imports {
		target, ok := r.resolveSpecifier(key, st.ast.imports[i].Specifier, st.ast.imports[i].Range)
		if ok {
			st.ast.imports[i].Module = target
			r.loadModule(target)
		}
	}
	for i := range st.ast.exports {
		if st.ast.exports[i].Specifier == "" {
			continue
		}
		target, ok := r.resolveSpecifier(key, st.ast.exports[i].Specifier, st.ast.exports[i].Range)
		if ok {
			st.ast.exports[i].Module = target
			r.loadModule(target)
		}
	}
	delete(r.visiting, key)
	return st
}

func (r *resolver) mergeSyntaxDiagnostics(key ModuleKey, file SourceFile) {
	for _, sd := range file.Diagnostics {
		d := Diagnostic{
			Code:       sd.Code,
			Severity:   sd.Severity,
			MessageKey: sd.MessageKey,
			Arguments:  map[string]string{},
			Message:    sd.Message,
			Range: &SourceRange{
				Origin:     sourceOrigin(key.Origin),
				ModulePath: key.Path,
				StartByte:  sd.Span.Start,
				EndByte:    sd.Span.End,
			},
		}
		r.diagnostics = append(r.diagnostics, d)
		if sd.Severity == "error" {
			r.syntaxInvalid = true
		}
	}
}

func (r *resolver) sourceFile(key ModuleKey) (SourceFile, bool) {
	if key.Origin.Kind == OriginProject {
		file, ok := r.input.Project.Files[key.Path]
		return file, ok
	}
	info, ok := r.packByOrigin(key.Origin)
	if !ok {
		return SourceFile{}, false
	}
	file, ok := info.pack.SourceFiles[key.Path]
	return file, ok
}

func (r *resolver) packByOrigin(origin Origin) (packInfo, bool) {
	canonical := origin.Publisher + "/" + origin.PackName
	install, ok := r.aliases[canonical]
	if !ok {
		return packInfo{}, false
	}
	info, ok := r.packs[install]
	return info, ok
}

func (r *resolver) moduleKind(key ModuleKey) ModuleKind {
	if key.Origin.Kind == OriginPack {
		info, _ := r.packByOrigin(key.Origin)
		if key.Path == info.pack.Entry {
			return ModulePackEntry
		}
		return ModulePack
	}
	if key.Path == r.input.EntryPath {
		return ModuleProjectEntry
	}
	return ModuleProject
}

func (r *resolver) resolveSpecifier(from ModuleKey, spec string, span syntax.Span) (ModuleKey, bool) {
	if strings.HasPrefix(spec, ".") {
		target, ok := resolveRelative(from.Path, spec)
		if !ok {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "relative module escapes origin or is not .ldl", from, span)
			return ModuleKey{}, false
		}
		return ModuleKey{Origin: from.Origin, Path: target}, true
	}
	segments := strings.Split(spec, ".")
	for _, seg := range segments {
		if !isIdent(seg) {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid pack module specifier", from, span)
			return ModuleKey{}, false
		}
	}
	if from.Origin.Kind == OriginProject {
		info, ok := r.packs[segments[0]]
		if !ok {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "unknown installed pack alias", from, span)
			return ModuleKey{}, false
		}
		return ModuleKey{Origin: info.origin, Path: packModulePath(segments, info.pack.Entry)}, true
	}
	info, ok := r.packByOrigin(from.Origin)
	if !ok {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "unknown current pack origin", from, span)
		return ModuleKey{}, false
	}
	var target packInfo
	if segments[0] == info.pack.Manifest.Name {
		target = info
	} else {
		install, ok := info.pack.Dependencies[segments[0]]
		if !ok {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "unknown pack dependency-local name", from, span)
			return ModuleKey{}, false
		}
		target, ok = r.packs[install]
		if !ok {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "pack dependency target is not installed", from, span)
			return ModuleKey{}, false
		}
	}
	return ModuleKey{Origin: target.origin, Path: packModulePath(segments, target.pack.Entry)}, true
}

func (r *resolver) collectDeclarations(st *moduleState) {
	if st.key.Origin.Kind == OriginPack && st.kind == ModulePackEntry {
		root := StableSymbol{Origin: st.key.Origin}
		r.addDecl(st, DeclarationSymbol{Symbol: root, Address: addressOf(root), Kind: KindPack, Module: st.key})
	}
	for _, d := range st.ast.declarations {
		if d.kind == KindProject {
			if st.key.Origin.Kind == OriginPack {
				r.diag("LDL1102", "unknown_or_duplicate_schema_member", "project declaration is not allowed in pack source", st.key, d.span)
				continue
			}
			if st.kind != ModuleProjectEntry {
				r.diag("LDL1302", "duplicate_or_reserved_identity", "project declaration must be in entry module", st.key, d.span)
				continue
			}
			r.addDecl(st, DeclarationSymbol{Symbol: StableSymbol{Origin: st.key.Origin}, Address: addressOf(StableSymbol{Origin: st.key.Origin}), Kind: KindProject, ID: d.id, Module: st.key, Range: d.span})
			continue
		}
		if isChildKind(d.kind) || d.kind == KindRow {
			continue
		}
		if st.key.Origin.Kind == OriginPack && !packKindAllowed(d.kind) {
			r.diag("LDL1102", "unknown_or_duplicate_schema_member", "declaration kind is not allowed in pack source", st.key, d.span)
			continue
		}
		r.addDecl(st, topDecl(st.key, d.kind, d.id, d.span))
	}
	for _, d := range st.ast.declarations {
		if d.childOf != nil {
			owner, ok := st.findTop(d.childOf.id, d.childOf.kind)
			if !ok {
				r.diag("LDL1301", "unknown_or_ambiguous_symbol", "owner declaration is missing for child", st.key, d.span)
				continue
			}
			r.addDecl(st, childDecl(st.key, owner, d.kind, d.id, d.span))
			continue
		}
		if d.kind == KindRow {
			owner, ok := st.findTop(d.owner, d.ownerKind)
			if !ok {
				continue
			}
			r.addDecl(st, childDecl(st.key, owner, KindRow, d.id, d.span))
		}
	}
}

func topDecl(module ModuleKey, kind SubjectKind, id string, span syntax.Span) DeclarationSymbol {
	sym := StableSymbol{Origin: module.Origin, Path: []SymbolSegment{{Kind: kind, ID: id}}}
	return DeclarationSymbol{Symbol: sym, Address: addressOf(sym), Kind: kind, ID: id, Module: module, Range: span}
}

func childDecl(module ModuleKey, owner DeclarationSymbol, kind SubjectKind, id string, span syntax.Span) DeclarationSymbol {
	sym := StableSymbol{Origin: owner.Symbol.Origin, Path: append(append([]SymbolSegment{}, owner.Symbol.Path...), SymbolSegment{Kind: kind, ID: id})}
	ownerSym := owner.Symbol
	return DeclarationSymbol{Symbol: sym, Address: addressOf(sym), Kind: kind, ID: id, Owner: &ownerSym, Module: module, Range: span}
}

func (r *resolver) addDecl(st *moduleState, decl DeclarationSymbol) {
	if decl.Address == "" || !validSymbol(decl.Symbol) {
		r.diag("LDL1302", "duplicate_or_reserved_identity", "invalid stable symbol", st.key, decl.Range)
		return
	}
	if prev, ok := r.symbols[decl.Address]; ok {
		r.diag("LDL1302", "duplicate_or_reserved_identity", "duplicate declaration identity", st.key, decl.Range)
		diagnostic := &r.diagnostics[len(r.diagnostics)-1]
		if decl.Owner != nil {
			diagnostic.SubjectAddress = decl.Address
			diagnostic.OwnerAddress = addressOf(*decl.Owner)
		}
		r.addRelatedConflict(diagnostic, prev)
		return
	}
	r.symbols[decl.Address] = decl
	st.localByAddress[decl.Address] = decl
	if len(decl.Symbol.Path) == 1 || decl.Kind == KindProject || decl.Kind == KindPack {
		if st.localTop[decl.Kind] == nil {
			st.localTop[decl.Kind] = map[string]DeclarationSymbol{}
		}
		if _, exists := st.localTop[decl.Kind][decl.ID]; !exists {
			st.localTop[decl.Kind][decl.ID] = decl
		}
	}
}

func (r *resolver) addRelatedConflict(d *Diagnostic, prev DeclarationSymbol) {
	if d == nil {
		return
	}
	related := DiagnosticRelated{Relation: "previous", Range: &SourceRange{Origin: sourceOrigin(prev.Module.Origin), ModulePath: prev.Module.Path, StartByte: prev.Range.Start, EndByte: prev.Range.End}, SubjectAddress: prev.Address}
	if prev.Owner != nil {
		related.OwnerAddress = addressOf(*prev.Owner)
	}
	d.Related = append(d.Related, related)
}

func (st *moduleState) findTop(id string, kinds ...SubjectKind) (DeclarationSymbol, bool) {
	for _, kind := range kinds {
		if decl, ok := st.localTop[kind][id]; ok {
			return decl, true
		}
	}
	return DeclarationSymbol{}, false
}

func (r *resolver) finalizeAllModules() {
	for _, key := range sortedModuleKeys(r.modules) {
		r.finalizeModule(r.modules[key])
	}
}

func (r *resolver) finalizeModule(st *moduleState) {
	if st == nil || st.finalState == evalDone {
		return
	}
	if st.finalState == evalVisiting {
		r.diag("LDL1202", "import_cycle", "module import/export cycle", st.key, zeroSpan())
		return
	}
	st.finalState = evalVisiting
	for i := range st.ast.imports {
		target := r.modules[st.ast.imports[i].Module]
		if target != nil {
			r.finalizeModule(target)
			r.bindImport(st, &st.ast.imports[i], target)
		}
	}
	r.bindDeclarationRefs(st)
	r.bindFactGroupRefs(st)
	st.exports = r.computeExports(st)
	st.finalState = evalDone
}

func (r *resolver) bindImport(st *moduleState, imp *ImportDecl, target *moduleState) {
	if st.imported == nil {
		st.imported = map[SubjectKind]map[string]DeclarationSymbol{}
	}
	if st.namespaceAliases == nil {
		st.namespaceAliases = map[string]syntax.Span{}
	}
	if st.namedAliases == nil {
		st.namedAliases = map[SubjectKind]map[string]syntax.Span{}
	}
	if target.exports == nil {
		target.exports = r.computeExports(target)
	}
	if imp.Kind == ImportNamespace {
		if prev, exists := st.namespaceAliases[imp.Alias]; exists {
			r.diag("LDL1302", "duplicate_or_reserved_identity", "duplicate namespace import alias", st.key, imp.Range)
			r.diagnostics[len(r.diagnostics)-1].Related = append(r.diagnostics[len(r.diagnostics)-1].Related, DiagnosticRelated{Relation: "previous", Range: &SourceRange{Origin: sourceOrigin(st.key.Origin), ModulePath: st.key.Path, StartByte: prev.Start, EndByte: prev.End}})
			return
		}
		st.namespaceAliases[imp.Alias] = imp.Range
		names := sortedExportNames(target.exports)
		for _, name := range names {
			decl := target.exports[name]
			if !isTopKind(decl.Kind) {
				continue
			}
			text := imp.Alias + "." + name
			r.addImportedBinding(st, decl.Kind, text, imp.Range, "import:"+imp.Alias, decl)
		}
		return
	}
	for i := range imp.Items {
		item := &imp.Items[i]
		decl, ok := target.exports[item.Remote]
		if !ok {
			r.diag("LDL1301", "unknown_or_ambiguous_symbol", "named import target is not exported", st.key, item.Range)
			continue
		}
		if st.namedAliases[decl.Kind] == nil {
			st.namedAliases[decl.Kind] = map[string]syntax.Span{}
		}
		if _, exists := st.localTop[decl.Kind][item.Local]; exists {
			r.diag("LDL1302", "duplicate_or_reserved_identity", "named import alias collides with local declaration", st.key, item.Range)
			continue
		}
		if prev, exists := st.namedAliases[decl.Kind][item.Local]; exists {
			r.diag("LDL1302", "duplicate_or_reserved_identity", "named import alias collides with another import", st.key, item.Range)
			r.diagnostics[len(r.diagnostics)-1].Related = append(r.diagnostics[len(r.diagnostics)-1].Related, DiagnosticRelated{Relation: "previous", Range: &SourceRange{Origin: sourceOrigin(st.key.Origin), ModulePath: st.key.Path, StartByte: prev.Start, EndByte: prev.End}})
			continue
		}
		st.namedAliases[decl.Kind][item.Local] = item.Range
		item.Target = decl.Symbol
		item.TargetAddress = decl.Address
		item.TargetKind = decl.Kind
		r.addImportedBinding(st, decl.Kind, item.Local, item.Range, "import:"+item.Local, decl)
	}
}

func (r *resolver) addImportedBinding(st *moduleState, kind SubjectKind, text string, span syntax.Span, via string, target DeclarationSymbol) {
	if st.imported[kind] == nil {
		st.imported[kind] = map[string]DeclarationSymbol{}
	}
	if prev, exists := st.imported[kind][text]; exists && prev.Address != target.Address {
		r.diag("LDL1302", "duplicate_or_reserved_identity", "binding alias collides with another target", st.key, span)
		return
	}
	st.imported[kind][text] = target
	st.addBinding(kind, text, span, via, target, target.Address)
}

func topImportKinds() []SubjectKind {
	return []SubjectKind{KindEntityType, KindRelationType, KindLayer, KindEntity, KindRelation, KindQuery, KindView, KindReference}
}

func (r *resolver) computeExports(st *moduleState) map[string]DeclarationSymbol {
	if st == nil {
		return map[string]DeclarationSymbol{}
	}
	if st.exportState == evalDone {
		return st.exports
	}
	if st.exportState == evalVisiting {
		if !st.exportCycleDiag {
			r.diag("LDL1202", "import_cycle", "export cycle", st.key, zeroSpan())
			st.exportCycleDiag = true
		}
		return map[string]DeclarationSymbol{}
	}
	st.exportState = evalVisiting
	out := map[string]DeclarationSymbol{}
	defer func() {
		st.exports = out
		st.exportState = evalDone
	}()
	for _, exp := range st.ast.exports {
		switch exp.Kind {
		case ExportLocal:
			for _, item := range exp.Items {
				decl, ok := r.resolveAny(st, item.Local)
				if !ok {
					r.diag("LDL1301", "unknown_or_ambiguous_symbol", "exported local symbol is unknown or ambiguous", st.key, item.Range)
					continue
				}
				r.addExport(out, st, item.Public, decl, item.Range, false)
			}
		case ExportFrom:
			target := r.modules[exp.Module]
			if target == nil {
				continue
			}
			target.exports = r.computeExports(target)
			for _, item := range exp.Items {
				decl, ok := target.exports[item.Local]
				if !ok {
					r.diag("LDL1301", "unknown_or_ambiguous_symbol", "re-export target is not exported", st.key, item.Range)
					continue
				}
				r.addExport(out, st, item.Public, decl, item.Range, true)
			}
		case ExportStar:
			target := r.modules[exp.Module]
			if target == nil {
				continue
			}
			target.exports = r.computeExports(target)
			for _, name := range sortedExportNames(target.exports) {
				r.addExport(out, st, name, target.exports[name], exp.Range, true)
			}
		}
	}
	return out
}

func (r *resolver) addExport(out map[string]DeclarationSymbol, st *moduleState, name string, decl DeclarationSymbol, span syntax.Span, reExport bool) {
	if _, exists := out[name]; exists {
		r.diag("LDL1302", "duplicate_or_reserved_identity", "duplicate public export name", st.key, span)
		return
	}
	out[name] = decl
	st.exportBindings = append(st.exportBindings, ExportBinding{Module: st.key, PublicName: name, Target: decl.Symbol, TargetAddress: decl.Address, Range: span, ReExport: reExport})
}

func (r *resolver) resolveAny(st *moduleState, text string) (DeclarationSymbol, bool) {
	var found []DeclarationSymbol
	for _, kind := range topImportKinds() {
		if decl, ok := st.localTop[kind][text]; ok {
			found = append(found, decl)
		}
		if decl, ok := st.imported[kind][text]; ok {
			found = append(found, decl)
		}
	}
	return single(found)
}

func (r *resolver) bindDeclarationRefs(st *moduleState) {
	for _, decl := range st.ast.declarations {
		sourceAddress := st.declarationAddress(decl)
		for _, ref := range decl.refs {
			if ref.text == "" {
				continue
			}
			target, ok := r.resolveText(st, ref.kind, ref.text)
			if !ok {
				continue
			}
			st.addBinding(ref.kind, ref.text, ref.span, "reference", target, sourceAddress)
		}
		if decl.kind == KindQuery && decl.node != nil && sourceAddress != "" {
			r.resolveQueryRefs(st, decl, sourceAddress, true, false)
		}
	}
}

func (r *resolver) validateSelectedDeclarationRefs() {
	for _, key := range sortedModuleKeys(r.modules) {
		r.validateDeclarationRefs(r.modules[key], true)
	}
}

func (r *resolver) validateDeclarationRefs(st *moduleState, selectedOnly bool) {
	for _, decl := range st.ast.declarations {
		sourceAddress := st.declarationAddress(decl)
		if selectedOnly && (sourceAddress == "" || !r.selected[sourceAddress]) {
			continue
		}
		for _, ref := range decl.refs {
			if ref.text == "" {
				continue
			}
			if _, ok := r.resolveText(st, ref.kind, ref.text); !ok {
				r.diag("LDL1301", "unknown_or_ambiguous_symbol", "source binding is unknown or ambiguous", st.key, ref.span)
			}
		}
		if decl.kind == KindQuery && decl.node != nil && sourceAddress != "" {
			r.resolveQueryRefs(st, decl, sourceAddress, false, true)
		}
	}
}

func (r *resolver) resolveFactGroupRefs(st *moduleState) {
	r.bindFactGroupRefs(st)
	r.validateFactGroupRefs(st, false)
}

func (r *resolver) bindFactGroupRefs(st *moduleState) {
	for _, group := range st.ast.factGroups {
		for _, ref := range group.refs {
			if ref.text == "" {
				continue
			}
			target, ok := r.resolveText(st, ref.kind, ref.text)
			if !ok {
				continue
			}
			if len(group.members) == 0 {
				if st.kind == ModuleProjectEntry || st.kind == ModulePackEntry {
					st.addBinding(ref.kind, ref.text, ref.span, "group-header", target, "")
				}
				continue
			}
			for _, member := range group.members {
				sourceAddress := st.declarationAddress(member)
				if sourceAddress == "" {
					continue
				}
				st.addBinding(ref.kind, ref.text, ref.span, "group-header", target, sourceAddress)
			}
		}
	}
}

func (r *resolver) validateSelectedFactGroupRefs() {
	for _, key := range sortedModuleKeys(r.modules) {
		r.validateFactGroupRefs(r.modules[key], true)
	}
}

func (r *resolver) validateFactGroupRefs(st *moduleState, selectedOnly bool) {
	for _, group := range st.ast.factGroups {
		if selectedOnly && !r.factGroupSelected(st, group) {
			continue
		}
		if selectedOnly {
			for _, member := range group.members {
				if member.kind == KindRow && st.declarationAddress(member) == "" && (st.kind == ModuleProjectEntry || st.kind == ModulePackEntry) {
					r.diag("LDL1301", "unknown_or_ambiguous_symbol", "row owner is not declared in the same module", st.key, member.span)
				}
			}
		}
		for _, ref := range group.refs {
			if ref.text == "" {
				continue
			}
			if _, ok := r.resolveText(st, ref.kind, ref.text); !ok {
				r.diag("LDL1301", "unknown_or_ambiguous_symbol", "fact group header binding is unknown or ambiguous", st.key, ref.span)
			}
		}
	}
}

func (r *resolver) factGroupSelected(st *moduleState, group rawFactGroup) bool {
	if len(group.members) > 0 {
		for _, member := range group.members {
			if r.selected[st.declarationAddress(member)] {
				return true
			}
		}
		return st.kind == ModuleProjectEntry || st.kind == ModulePackEntry
	}
	if st.kind == ModuleProjectEntry || st.kind == ModulePackEntry {
		return true
	}
	return false
}

func (r *resolver) resolveText(st *moduleState, kind SubjectKind, text string) (DeclarationSymbol, bool) {
	var found []DeclarationSymbol
	if decl, ok := st.localTop[kind][text]; ok {
		found = append(found, decl)
	}
	if decl, ok := st.imported[kind][text]; ok {
		found = append(found, decl)
	}
	return single(found)
}

func single(found []DeclarationSymbol) (DeclarationSymbol, bool) {
	if len(found) != 1 {
		return DeclarationSymbol{}, false
	}
	return found[0], true
}

func (st *moduleState) addBinding(kind SubjectKind, text string, span syntax.Span, via string, target DeclarationSymbol, sourceAddress string) {
	owner := ""
	if target.Owner != nil {
		owner = addressOf(*target.Owner)
	}
	st.bindings = append(st.bindings, SourceBinding{Module: st.key, ExpectedKind: kind, SourceText: text, Range: span, Target: target.Symbol, TargetAddress: target.Address, TargetOwnerAddress: owner, Via: via, SourceAddress: sourceAddress})
}

func (st *moduleState) declarationAddress(raw rawDecl) string {
	if raw.childOf != nil {
		owner, ok := st.findTop(raw.childOf.id, raw.childOf.kind)
		if !ok {
			return ""
		}
		return reservationAddress(owner.Symbol, raw.kind, raw.id)
	}
	if raw.kind == KindRow {
		owner, ok := st.findTop(raw.owner, raw.ownerKind)
		if !ok {
			return ""
		}
		return reservationAddress(owner.Symbol, KindRow, raw.id)
	}
	if raw.kind == KindProject {
		return addressOf(StableSymbol{Origin: st.key.Origin})
	}
	if isTopKind(raw.kind) {
		if decl, ok := st.findTop(raw.id, raw.kind); ok {
			return decl.Address
		}
	}
	return ""
}

func (r *resolver) validateAllIdentity() {
	for _, key := range sortedModuleKeys(r.modules) {
		st := r.modules[key]
		r.validateRootAndOwnerBlocks(st)
		r.validateReservationSchemas(st)
	}
	for _, key := range sortedModuleKeys(r.modules) {
		st := r.modules[key]
		r.validateReservations(st)
	}
	for _, key := range sortedModuleKeys(r.modules) {
		st := r.modules[key]
		r.validateMoves(st)
	}
	r.validateOriginIdentityDisjointness()
}

func (r *resolver) validateLoadedReservationSchemas() {
	for _, key := range sortedModuleKeys(r.modules) {
		r.validateReservationSchemas(r.modules[key])
	}
}

func (r *resolver) validateReservationSchemas(st *moduleState) {
	st.ast.reservations = nil
	seenBlocks := map[string]bool{}
	for _, block := range st.ast.reservationBlocks {
		scope := string(block.ownerKind) + ":" + block.ownerID
		if block.ownerID == "" {
			scope = "root"
		}
		if block.row {
			scope += ":rows"
		}
		if seenBlocks[scope] {
			// The duplicate block/statement already has its placement diagnostic.
			continue
		}
		seenBlocks[scope] = true
		if block.row {
			reservations, _ := r.validateReservationStatement(st, block, block.node, KindRow)
			st.ast.reservations = append(st.ast.reservations, reservations...)
			continue
		}
		seenCategories := map[SubjectKind]syntax.Span{}
		for _, member := range nodeChildren(firstNode(block.node, syntax.NodeBlock)) {
			head := directTokens(member)
			if len(head) == 0 {
				continue
			}
			headSpan := head[0].Span
			kind, known := reservationKind(head[0].Raw)
			if !known {
				r.diag("LDL1102", "unknown_or_duplicate_schema_member", "unknown reservation category", st.key, headSpan)
				continue
			}
			if member.Kind != syntax.NodeStatement {
				r.diag("LDL1102", "unknown_or_duplicate_schema_member", "reservation category must be a statement", st.key, headSpan)
				continue
			}
			if previous, duplicate := seenCategories[kind]; duplicate {
				r.diag("LDL1102", "unknown_or_duplicate_schema_member", "duplicate reservation category", st.key, headSpan)
				diagnostic := &r.diagnostics[len(r.diagnostics)-1]
				diagnostic.Related = append(diagnostic.Related, DiagnosticRelated{
					Relation: "previous",
					Range: &SourceRange{Origin: sourceOrigin(st.key.Origin), ModulePath: st.key.Path,
						StartByte: previous.Start, EndByte: previous.End},
				})
				continue
			}
			seenCategories[kind] = headSpan
			reservations, schemaValid := r.validateReservationStatement(st, block, member, kind)
			if !reservationCategoryAllowed(st, block, kind) {
				if schemaValid {
					r.diag("LDL1302", "duplicate_or_reserved_identity", reservationCategoryMessage(st, block, kind), st.key, headSpan)
				}
				continue
			}
			st.ast.reservations = append(st.ast.reservations, reservations...)
		}
	}
}

func (r *resolver) validateReservationStatement(st *moduleState, block rawReservationBlock, stmt *syntax.Node, kind SubjectKind) ([]rawReservation, bool) {
	var reservations []rawReservation
	valid := true
	var args []*syntax.Node
	for _, child := range nodeChildren(stmt) {
		if child.Kind == syntax.NodeValue {
			args = append(args, child)
		}
	}
	head := directTokens(stmt)
	headSpan := stmt.Span
	if len(head) > 0 {
		headSpan = head[0].Span
	}
	if len(args) == 0 {
		r.diag("LDL1102", "unknown_or_duplicate_schema_member", "reservation requires one list", st.key, headSpan)
		return nil, false
	}
	if len(args) != 1 {
		r.diag("LDL1102", "unknown_or_duplicate_schema_member", "reservation requires one list", st.key, args[1].Span)
		return nil, false
	}
	list := firstNode(args[0], syntax.NodeList)
	if list == nil {
		r.diag("LDL1102", "unknown_or_duplicate_schema_member", "reservation requires one list", st.key, args[0].Span)
		return nil, false
	}
	for _, member := range nodeChildren(list) {
		if member.Kind != syntax.NodeValue {
			if member.Kind == syntax.NodeError {
				r.diag("LDL1102", "unknown_or_duplicate_schema_member", "malformed reservation identifier", st.key, member.Span)
				valid = false
			}
			continue
		}
		tokens := nodeTokens(member)
		if len(descendants(member, syntax.NodeError)) != 0 || len(tokens) != 1 || tokens[0].Kind != syntax.TokenIdentifier || !isIdent(tokens[0].Raw) {
			r.diag("LDL1102", "unknown_or_duplicate_schema_member", "invalid reservation identifier", st.key, member.Span)
			valid = false
			continue
		}
		reservations = append(reservations, rawReservation{
			ownerKind: block.ownerKind,
			ownerID:   block.ownerID,
			kind:      kind,
			id:        tokens[0].Raw,
			span:      tokens[0].Span,
		})
	}
	return reservations, valid
}

func reservationCategoryAllowed(st *moduleState, block rawReservationBlock, kind SubjectKind) bool {
	if block.ownerID == "" {
		return !isChildKind(kind) && rootReservationAllowed(st.key.Origin.Kind, kind)
	}
	return ownerReservationAllowed(block.ownerKind, kind)
}

func reservationCategoryMessage(st *moduleState, block rawReservationBlock, kind SubjectKind) string {
	if block.ownerID == "" && isChildKind(kind) {
		return "root reservation uses owner-scoped kind"
	}
	if block.ownerID == "" && !rootReservationAllowed(st.key.Origin.Kind, kind) {
		return "reservation kind is not allowed for origin root"
	}
	return "reservation kind is not allowed for owner"
}

func (r *resolver) validateRootAndOwnerBlocks(st *moduleState) {
	entry := st.kind == ModuleProjectEntry || st.kind == ModulePackEntry
	if len(st.ast.rootReservedBlocks) > 0 && !entry {
		r.diag("LDL1302", "duplicate_or_reserved_identity", "root reserved block must be in origin entry", st.key, st.ast.rootReservedBlocks[0])
	}
	if len(st.ast.rootReservedBlocks) > 1 {
		r.diag("LDL1302", "duplicate_or_reserved_identity", "duplicate root reserved block", st.key, st.ast.rootReservedBlocks[1])
		r.addPreviousRange(st.key, st.ast.rootReservedBlocks[0])
	}
	if len(st.ast.rootMoveBlocks) > 0 && !entry {
		r.diag("LDL1303", "invalid_move_graph", "root moves block must be in origin entry", st.key, st.ast.rootMoveBlocks[0])
	}
	if len(st.ast.rootMoveBlocks) > 1 {
		r.diag("LDL1303", "invalid_move_graph", "duplicate root moves block", st.key, st.ast.rootMoveBlocks[1])
		r.addPreviousRange(st.key, st.ast.rootMoveBlocks[0])
	}
	for _, spans := range st.ast.ownerReserveBlocks {
		if len(spans) > 1 {
			r.diag("LDL1302", "duplicate_or_reserved_identity", "duplicate owner reserve block", st.key, spans[1])
			r.addPreviousRange(st.key, spans[0])
		}
	}
	for _, spans := range st.ast.rowReserveBlocks {
		if len(spans) > 1 {
			r.diag("LDL1302", "duplicate_or_reserved_identity", "duplicate reserve_rows statement", st.key, spans[1])
			r.addPreviousRange(st.key, spans[0])
		}
	}
}

func (r *resolver) addPreviousRange(module ModuleKey, span syntax.Span) {
	if len(r.diagnostics) == 0 {
		return
	}
	diagnostic := &r.diagnostics[len(r.diagnostics)-1]
	diagnostic.Related = append(diagnostic.Related, DiagnosticRelated{Relation: "previous", Range: &SourceRange{
		Origin: sourceOrigin(module.Origin), ModulePath: module.Path, StartByte: span.Start, EndByte: span.End,
	}})
}

func (r *resolver) validateReservations(st *moduleState) {
	seen := map[string]Reservation{}
	for _, raw := range st.ast.reservations {
		if raw.ownerID == "" && isChildKind(raw.kind) {
			r.diag("LDL1302", "duplicate_or_reserved_identity", "root reservation uses owner-scoped kind", st.key, raw.span)
			continue
		}
		if raw.ownerID == "" && !rootReservationAllowed(st.key.Origin.Kind, raw.kind) {
			r.diag("LDL1302", "duplicate_or_reserved_identity", "reservation kind is not allowed for origin root", st.key, raw.span)
			continue
		}
		if raw.ownerID != "" && !ownerReservationAllowed(raw.ownerKind, raw.kind) {
			r.diag("LDL1302", "duplicate_or_reserved_identity", "reservation kind is not allowed for owner", st.key, raw.span)
			continue
		}
		owner, ok := r.reservationOwner(st, raw)
		if !ok {
			r.diag("LDL1302", "duplicate_or_reserved_identity", "reservation owner is invalid", st.key, raw.span)
			continue
		}
		addr := reservationAddress(owner, raw.kind, raw.id)
		if active, exists := r.symbols[addr]; exists {
			r.diag("LDL1302", "duplicate_or_reserved_identity", "reservation uses active identity", st.key, raw.span)
			diagnostic := &r.diagnostics[len(r.diagnostics)-1]
			setReservationDiagnosticContext(diagnostic, addr, owner, raw.kind)
			r.addRelatedConflict(diagnostic, active)
			continue
		}
		if previous, exists := seen[addr]; exists {
			r.diag("LDL1302", "duplicate_or_reserved_identity", "duplicate reservation", st.key, raw.span)
			diagnostic := &r.diagnostics[len(r.diagnostics)-1]
			setReservationDiagnosticContext(diagnostic, addr, owner, raw.kind)
			addReservationRelated(diagnostic, st.key, previous, "previous")
			continue
		}
		res := Reservation{Owner: owner, Kind: raw.kind, ID: raw.id, Address: addr, Range: raw.span}
		seen[addr] = res
		st.reservations = append(st.reservations, res)
	}
}

func setReservationDiagnosticContext(diagnostic *Diagnostic, address string, owner StableSymbol, kind SubjectKind) {
	if diagnostic == nil {
		return
	}
	diagnostic.SubjectAddress = address
	if isChildKind(kind) {
		diagnostic.OwnerAddress = addressOf(owner)
	}
}

func addReservationRelated(diagnostic *Diagnostic, module ModuleKey, reservation Reservation, relation string) {
	if diagnostic == nil {
		return
	}
	related := DiagnosticRelated{
		Relation:       relation,
		Range:          &SourceRange{Origin: sourceOrigin(module.Origin), ModulePath: module.Path, StartByte: reservation.Range.Start, EndByte: reservation.Range.End},
		SubjectAddress: reservation.Address,
	}
	if isChildKind(reservation.Kind) {
		related.OwnerAddress = addressOf(reservation.Owner)
	}
	diagnostic.Related = append(diagnostic.Related, related)
}

func rootReservationAllowed(origin OriginKind, kind SubjectKind) bool {
	if origin == OriginPack {
		switch kind {
		case KindEntityType, KindRelationType, KindQuery, KindView, KindReference:
			return true
		default:
			return false
		}
	}
	return isTopKind(kind)
}

func rootMoveAllowed(origin OriginKind, kind SubjectKind) bool {
	if kind == KindProject {
		return origin == OriginProject
	}
	return rootReservationAllowed(origin, kind)
}

func ownerReservationAllowed(ownerKind, childKind SubjectKind) bool {
	switch ownerKind {
	case KindEntityType, KindRelationType:
		return childKind == KindColumn || childKind == KindConstraint
	case KindEntity, KindRelation:
		return childKind == KindRow
	case KindQuery:
		return childKind == KindParameter
	case KindView:
		return childKind == KindTableColumn || childKind == KindExport
	default:
		return false
	}
}

func (r *resolver) reservationOwner(st *moduleState, raw rawReservation) (StableSymbol, bool) {
	if raw.ownerID == "" {
		return StableSymbol{Origin: st.key.Origin}, true
	}
	owner, ok := st.findTop(raw.ownerID, raw.ownerKind)
	if !ok {
		return StableSymbol{}, false
	}
	return owner.Symbol, true
}

func reservationAddress(owner StableSymbol, kind SubjectKind, id string) string {
	sym := StableSymbol{Origin: owner.Origin, Path: append(append([]SymbolSegment{}, owner.Path...), SymbolSegment{Kind: kind, ID: id})}
	return addressOf(sym)
}

func (r *resolver) validateMoves(st *moduleState) {
	byScope := map[string][]moveEdge{}
	for _, raw := range st.ast.moves {
		if raw.ownerID == "" && !rootMoveAllowed(st.key.Origin.Kind, raw.kind) {
			r.diag("LDL1303", "invalid_move_graph", "move kind is not allowed for origin root", st.key, raw.span)
			continue
		}
		mv, ok := r.materializeMove(st, raw)
		if !ok {
			continue
		}
		st.moves = append(st.moves, mv)
		scope := moveScope(mv)
		byScope[scope] = append(byScope[scope], moveEdge{move: mv, from: mv.FromAddress, to: mv.ToAddress})
	}
	for _, scope := range sortedEdgeScopes(byScope) {
		edges := byScope[scope]
		successor := map[string]moveEdge{}
		predecessor := map[string]moveEdge{}
		for _, e := range edges {
			if _, exists := successor[e.from]; exists {
				r.diag("LDL1303", "invalid_move_graph", "move source has multiple successors", st.key, e.move.Range)
			}
			if _, exists := predecessor[e.to]; exists {
				r.diag("LDL1303", "invalid_move_graph", "move target has multiple predecessors", st.key, e.move.Range)
			}
			successor[e.from] = e
			predecessor[e.to] = e
			if _, active := r.symbols[e.from]; active {
				r.diag("LDL1303", "invalid_move_graph", "move source is active", st.key, e.move.Range)
			}
		}
		for _, e := range edges {
			seen := map[string]bool{}
			cur := e.from
			curSymbol := e.move.fromSymbol
			for {
				next, ok := successor[cur]
				if !ok {
					if _, active := r.symbols[cur]; !active {
						r.diag("LDL1303", "invalid_move_graph", "terminal move target is not active", st.key, e.move.Range)
					}
					if e.from != cur {
						appendMoveClosureUnique(&st.moveClosure, MoveClosure{From: e.from, To: cur, fromSymbol: e.move.fromSymbol, toSymbol: curSymbol})
					}
					break
				}
				if seen[cur] {
					r.diag("LDL1303", "invalid_move_graph", "move graph cycle", st.key, e.move.Range)
					break
				}
				seen[cur] = true
				cur = next.to
				curSymbol = next.move.toSymbol
			}
		}
	}
	r.materializeComposedMoveClosure(st)
}

func (r *resolver) materializeComposedMoveClosure(st *moduleState) {
	for _, decl := range sortedDeclMap(r.symbols) {
		if decl.Symbol.Origin != st.key.Origin {
			continue
		}
		for _, fromSymbol := range st.historicalSymbolsFor(decl.Symbol) {
			from := addressOf(fromSymbol)
			to := decl.Address
			if from != to {
				appendMoveClosureUnique(&st.moveClosure, MoveClosure{From: from, To: to, fromSymbol: fromSymbol, toSymbol: decl.Symbol})
			}
		}
	}
}

func (st *moduleState) historicalSymbolsFor(current StableSymbol) []StableSymbol {
	roots := st.historicalOrigins(current.Origin)
	if len(current.Path) == 0 {
		out := make([]StableSymbol, 0, len(roots))
		for _, root := range roots {
			out = append(out, StableSymbol{Origin: root})
		}
		return out
	}
	top := current.Path[0]
	topIDs := st.historicalTopIDs(top.Kind, top.ID)
	if len(current.Path) == 1 {
		var out []StableSymbol
		for _, root := range roots {
			for _, topID := range topIDs {
				out = append(out, StableSymbol{Origin: root, Path: []SymbolSegment{{Kind: top.Kind, ID: topID}}})
			}
		}
		return out
	}
	child := current.Path[1]
	childIDs := st.historicalChildIDs(current, child.ID)
	var out []StableSymbol
	for _, root := range roots {
		for _, topID := range topIDs {
			for _, childID := range childIDs {
				out = append(out, StableSymbol{Origin: root, Path: []SymbolSegment{{Kind: top.Kind, ID: topID}, {Kind: child.Kind, ID: childID}}})
			}
		}
	}
	return out
}

func (st *moduleState) historicalOrigins(current Origin) []Origin {
	out := []Origin{current}
	if current.Kind != OriginProject {
		return out
	}
	seen := map[string]bool{current.ProjectID: true}
	for changed := true; changed; {
		changed = false
		for _, mv := range st.moves {
			if mv.Kind == KindProject && seen[mv.To] && !seen[mv.From] {
				seen[mv.From] = true
				out = append(out, Origin{Kind: OriginProject, ProjectID: mv.From})
				changed = true
			}
		}
	}
	sort.SliceStable(out[1:], func(i, j int) bool { return out[i+1].ProjectID < out[j+1].ProjectID })
	return out
}

func (st *moduleState) historicalTopIDs(kind SubjectKind, currentID string) []string {
	out := []string{currentID}
	seen := map[string]bool{currentID: true}
	for changed := true; changed; {
		changed = false
		for _, mv := range st.moves {
			if !isChildKind(mv.Kind) && mv.Kind == kind && seen[mv.To] && !seen[mv.From] {
				seen[mv.From] = true
				out = append(out, mv.From)
				changed = true
			}
		}
	}
	sort.Strings(out[1:])
	return out
}

func (st *moduleState) historicalChildIDs(current StableSymbol, currentID string) []string {
	out := []string{currentID}
	owner := StableSymbol{Origin: current.Origin, Path: append([]SymbolSegment{}, current.Path[:1]...)}
	ownerAddress := addressOf(owner)
	childKind := current.Path[1].Kind
	seen := map[string]bool{currentID: true}
	for changed := true; changed; {
		changed = false
		for _, mv := range st.moves {
			if mv.Kind == childKind && mv.Owner != nil && addressOf(*mv.Owner) == ownerAddress && seen[mv.To] && !seen[mv.From] {
				seen[mv.From] = true
				out = append(out, mv.From)
				changed = true
			}
		}
	}
	sort.Strings(out[1:])
	return out
}

func appendMoveClosureUnique(out *[]MoveClosure, item MoveClosure) {
	for _, existing := range *out {
		if existing.From == item.From && existing.To == item.To {
			return
		}
	}
	*out = append(*out, item)
}

type identityReservationRef struct {
	module ModuleKey
	item   Reservation
}

func (r *resolver) validateOriginIdentityDisjointness() {
	reserved := map[string]identityReservationRef{}
	for _, key := range sortedModuleKeys(r.modules) {
		st := r.modules[key]
		for _, res := range st.reservations {
			if prev, exists := reserved[res.Address]; exists {
				r.diag("LDL1302", "duplicate_or_reserved_identity", "duplicate reservation", key, res.Range)
				diagnostic := &r.diagnostics[len(r.diagnostics)-1]
				setReservationDiagnosticContext(diagnostic, res.Address, res.Owner, res.Kind)
				addReservationRelated(diagnostic, prev.module, prev.item, "previous")
				continue
			}
			reserved[res.Address] = identityReservationRef{module: key, item: res}
		}
	}
	for _, key := range sortedModuleKeys(r.modules) {
		st := r.modules[key]
		for _, mv := range st.moves {
			if ref, ok := reserved[mv.FromAddress]; ok {
				r.diag("LDL1302", "duplicate_or_reserved_identity", "move source is also explicitly reserved", key, mv.Range)
				diagnostic := &r.diagnostics[len(r.diagnostics)-1]
				setReservationDiagnosticContext(diagnostic, ref.item.Address, ref.item.Owner, ref.item.Kind)
				addReservationRelated(diagnostic, ref.module, ref.item, "conflict")
			}
		}
	}
}

func (r *resolver) materializeMove(st *moduleState, raw rawMove) (Move, bool) {
	if raw.kind == KindProject {
		if st.key.Origin.Kind != OriginProject || raw.to != st.key.Origin.ProjectID {
			r.diag("LDL1303", "invalid_move_graph", "project move target must be current project id", st.key, raw.span)
			return Move{}, false
		}
		fromSymbol := StableSymbol{Origin: Origin{Kind: OriginProject, ProjectID: raw.from}}
		toSymbol := StableSymbol{Origin: st.key.Origin}
		return Move{Origin: st.key.Origin, Kind: raw.kind, From: raw.from, To: raw.to, FromAddress: addressOf(fromSymbol), ToAddress: addressOf(toSymbol), fromSymbol: fromSymbol, toSymbol: toSymbol, Range: raw.span}, true
	}
	if isChildKind(raw.kind) {
		owner, ok := r.moveOwner(st, raw)
		if !ok {
			r.diag("LDL1303", "invalid_move_graph", "child move owner is invalid", st.key, raw.span)
			return Move{}, false
		}
		fromSymbol := childSymbol(owner.Symbol, raw.kind, raw.from)
		toSymbol := childSymbol(owner.Symbol, raw.kind, raw.to)
		return Move{Origin: st.key.Origin, Kind: raw.kind, OwnerID: raw.ownerID, Owner: &owner.Symbol, From: raw.from, To: raw.to, FromAddress: addressOf(fromSymbol), ToAddress: addressOf(toSymbol), fromSymbol: fromSymbol, toSymbol: toSymbol, Range: raw.span}, true
	}
	fromSymbol := StableSymbol{Origin: st.key.Origin, Path: []SymbolSegment{{Kind: raw.kind, ID: raw.from}}}
	toSymbol := StableSymbol{Origin: st.key.Origin, Path: []SymbolSegment{{Kind: raw.kind, ID: raw.to}}}
	return Move{Origin: st.key.Origin, Kind: raw.kind, OwnerID: raw.ownerID, From: raw.from, To: raw.to, FromAddress: addressOf(fromSymbol), ToAddress: addressOf(toSymbol), fromSymbol: fromSymbol, toSymbol: toSymbol, Range: raw.span}, true
}

func childSymbol(owner StableSymbol, kind SubjectKind, id string) StableSymbol {
	return StableSymbol{Origin: owner.Origin, Path: append(append([]SymbolSegment{}, owner.Path...), SymbolSegment{Kind: kind, ID: id})}
}

func (r *resolver) moveOwner(st *moduleState, raw rawMove) (DeclarationSymbol, bool) {
	switch raw.variant {
	case "entity_type_column", "entity_type_constraint":
		return r.findOriginTop(st.key.Origin, raw.ownerID, KindEntityType)
	case "relation_type_column", "relation_type_constraint":
		return r.findOriginTop(st.key.Origin, raw.ownerID, KindRelationType)
	case "entity_row":
		return r.findOriginTop(st.key.Origin, raw.ownerID, KindEntity)
	case "relation_row":
		return r.findOriginTop(st.key.Origin, raw.ownerID, KindRelation)
	case "query_parameter":
		return r.findOriginTop(st.key.Origin, raw.ownerID, KindQuery)
	case "view_table_column", "view_export":
		return r.findOriginTop(st.key.Origin, raw.ownerID, KindView)
	default:
		return DeclarationSymbol{}, false
	}
}

func (r *resolver) findOriginTop(origin Origin, id string, kind SubjectKind) (DeclarationSymbol, bool) {
	addr := addressOf(StableSymbol{Origin: origin, Path: []SymbolSegment{{Kind: kind, ID: id}}})
	decl, ok := r.symbols[addr]
	return decl, ok
}

func moveScope(m Move) string {
	owner := ""
	if m.Owner != nil {
		owner = addressOf(*m.Owner)
	}
	return addressOf(StableSymbol{Origin: m.Origin}) + "|" + owner + "|" + string(m.Kind)
}

func (r *resolver) selectProject(entry *moduleState) {
	for _, decl := range entry.localByAddress {
		if isTopKind(decl.Kind) || decl.Kind == KindProject {
			r.selectDecl(decl)
		}
	}
	for _, imp := range entry.ast.imports {
		if imp.Kind == ImportNamed {
			for _, item := range imp.Items {
				target, ok := r.symbols[item.TargetAddress]
				if ok {
					r.selectDecl(target)
				}
			}
			continue
		}
		prefix := imp.Alias + "."
		for _, binding := range entry.bindings {
			if strings.HasPrefix(binding.SourceText, prefix) {
				if target, ok := r.symbols[binding.TargetAddress]; ok {
					r.selectDecl(target)
				}
			}
		}
	}
	for _, decl := range entry.exports {
		r.selectDecl(decl)
	}
}

func (r *resolver) selectPack(entry *moduleState) {
	if root, ok := entry.localByAddress[addressOf(StableSymbol{Origin: entry.key.Origin})]; ok {
		r.selectDecl(root)
	}
	for _, name := range sortedExportNames(entry.exports) {
		r.selectDecl(entry.exports[name])
	}
	for _, binding := range entry.bindings {
		if binding.SourceAddress == "" && binding.Via == "group-header" {
			if target, ok := r.symbols[binding.TargetAddress]; ok {
				r.selectDecl(target)
			}
		}
	}
}

func (r *resolver) selectDecl(decl DeclarationSymbol) {
	if decl.Address == "" || r.selected[decl.Address] {
		return
	}
	r.selected[decl.Address] = true
	if decl.Symbol.Origin.Kind == OriginPack {
		r.selected[addressOf(StableSymbol{Origin: decl.Symbol.Origin})] = true
	}
	for _, child := range r.childrenOf(decl.Symbol) {
		r.selectDecl(child)
	}
	st := r.modules[decl.Module]
	if st != nil {
		for _, b := range st.bindings {
			if b.Module == decl.Module && b.SourceAddress == decl.Address {
				if target, ok := r.symbols[b.TargetAddress]; ok {
					r.selectDecl(target)
					if decl.Kind == KindQuery && target.Owner != nil {
						if owner, exists := r.symbols[addressOf(*target.Owner)]; exists {
							r.selectDecl(owner)
						}
					}
				}
			}
		}
	}
}

func (r *resolver) childrenOf(owner StableSymbol) []DeclarationSymbol {
	if len(owner.Path) == 0 {
		return nil
	}
	var out []DeclarationSymbol
	prefix := addressOf(owner) + ":"
	for addr, decl := range r.symbols {
		if strings.HasPrefix(addr, prefix) && len(decl.Symbol.Path) == len(owner.Path)+1 {
			out = append(out, decl)
		}
	}
	sortDeclarations(out)
	return out
}

func (r *resolver) result() Result {
	result := Result{Mode: r.input.Mode, RootCanonicalID: r.rootCanonicalID}
	if r.project.Kind != "" {
		result.RootAddress = addressOf(StableSymbol{Origin: r.project})
	}
	for _, key := range sortedModuleKeys(r.modules) {
		st := r.modules[key]
		sourceNodes := nodesBySpan(st.file.Root)
		result.Modules = append(result.Modules, ResolvedModule{Origin: st.key.Origin, Path: st.key.Path, Kind: st.kind, File: st.file, Imports: st.ast.imports, Exports: st.ast.exports})
		if !r.syntaxInvalid && len(r.selected) > 0 {
			for _, decl := range sortedDeclMap(st.localByAddress) {
				if r.selected[decl.Address] {
					decl.Selected = true
					result.Declarations = append(result.Declarations, decl)
				}
			}
			for _, binding := range st.exportBindings {
				if r.selected[binding.TargetAddress] {
					result.Exports = append(result.Exports, binding)
				}
			}
			for _, binding := range st.bindings {
				if r.bindingSelected(binding) {
					result.Bindings = append(result.Bindings, binding)
				}
			}
			for _, decl := range st.ast.declarations {
				address := st.declarationAddress(decl)
				if !r.selected[address] {
					continue
				}
				symbol := StableSymbol{Origin: st.key.Origin}
				if selected, ok := r.symbols[address]; ok {
					symbol = selected.Symbol
				}
				result.DeclarationSources = append(result.DeclarationSources, DeclarationSource{
					Symbol:  symbol,
					Address: address,
					Kind:    decl.kind,
					Module:  st.key,
					Range:   decl.span,
					Node:    sourceNodes[decl.span],
				})
			}
			result.CandidateIdentity.Reservations = append(result.CandidateIdentity.Reservations, st.reservations...)
			result.CandidateIdentity.Moves = append(result.CandidateIdentity.Moves, st.moves...)
			result.CandidateIdentity.MoveClosure = append(result.CandidateIdentity.MoveClosure, st.moveClosure...)
			for _, res := range st.reservations {
				if r.reservationSelected(res) {
					result.Identity.Reservations = append(result.Identity.Reservations, res)
				}
			}
			for _, mv := range st.moves {
				if r.moveSelected(mv) {
					result.Identity.Moves = append(result.Identity.Moves, mv)
				}
			}
			for _, closure := range st.moveClosure {
				if r.selected[closure.To] {
					result.Identity.MoveClosure = append(result.Identity.MoveClosure, closure)
				}
			}
		}
	}
	if !r.syntaxInvalid && len(r.selected) > 0 {
		result.Candidates = sortedDeclMap(r.symbols)
		usedPacks := map[Origin]bool{}
		for address := range r.selected {
			decl, ok := r.symbols[address]
			if ok && decl.Symbol.Origin.Kind == OriginPack {
				usedPacks[decl.Symbol.Origin] = true
			}
		}
		r.addTransitivePackDependencies(usedPacks)
		emitted := map[Origin]bool{}
		for _, install := range sortedKeysPack(r.packs) {
			info := r.packs[install]
			if !usedPacks[info.origin] || emitted[info.origin] {
				continue
			}
			emitted[info.origin] = true
			addr := addressOf(StableSymbol{Origin: info.origin})
			result.Dependencies = append(result.Dependencies, ResolvedPackSummary{Address: addr, CanonicalID: info.pack.CanonicalID, Version: info.pack.Version, Digest: info.pack.Digest})
		}
	}
	sortDeclarations(result.Declarations)
	sort.SliceStable(result.Exports, func(i, j int) bool {
		if result.Exports[i].PublicName != result.Exports[j].PublicName {
			return result.Exports[i].PublicName < result.Exports[j].PublicName
		}
		return result.Exports[i].TargetAddress < result.Exports[j].TargetAddress
	})
	sort.SliceStable(result.Bindings, func(i, j int) bool {
		if result.Bindings[i].Module != result.Bindings[j].Module {
			return fmt.Sprint(result.Bindings[i].Module) < fmt.Sprint(result.Bindings[j].Module)
		}
		if result.Bindings[i].ExpectedKind != result.Bindings[j].ExpectedKind {
			return kindRank(result.Bindings[i].ExpectedKind) < kindRank(result.Bindings[j].ExpectedKind)
		}
		if result.Bindings[i].SourceText != result.Bindings[j].SourceText {
			return result.Bindings[i].SourceText < result.Bindings[j].SourceText
		}
		return result.Bindings[i].TargetAddress < result.Bindings[j].TargetAddress
	})
	sort.SliceStable(result.DeclarationSources, func(i, j int) bool {
		return compareSymbol(result.DeclarationSources[i].Symbol, result.DeclarationSources[j].Symbol) < 0
	})
	sort.SliceStable(result.Dependencies, func(i, j int) bool { return result.Dependencies[i].Address < result.Dependencies[j].Address })
	sort.SliceStable(result.Identity.Reservations, func(i, j int) bool {
		a := childSymbol(result.Identity.Reservations[i].Owner, result.Identity.Reservations[i].Kind, result.Identity.Reservations[i].ID)
		b := childSymbol(result.Identity.Reservations[j].Owner, result.Identity.Reservations[j].Kind, result.Identity.Reservations[j].ID)
		if cmp := compareSymbol(a, b); cmp != 0 {
			return cmp < 0
		}
		return result.Identity.Reservations[i].Address < result.Identity.Reservations[j].Address
	})
	sort.SliceStable(result.CandidateIdentity.Reservations, func(i, j int) bool {
		a := childSymbol(result.CandidateIdentity.Reservations[i].Owner, result.CandidateIdentity.Reservations[i].Kind, result.CandidateIdentity.Reservations[i].ID)
		b := childSymbol(result.CandidateIdentity.Reservations[j].Owner, result.CandidateIdentity.Reservations[j].Kind, result.CandidateIdentity.Reservations[j].ID)
		if cmp := compareSymbol(a, b); cmp != 0 {
			return cmp < 0
		}
		return result.CandidateIdentity.Reservations[i].Address < result.CandidateIdentity.Reservations[j].Address
	})
	sort.SliceStable(result.Identity.Moves, func(i, j int) bool {
		if cmp := compareSymbol(result.Identity.Moves[i].fromSymbol, result.Identity.Moves[j].fromSymbol); cmp != 0 {
			return cmp < 0
		}
		return compareSymbol(result.Identity.Moves[i].toSymbol, result.Identity.Moves[j].toSymbol) < 0
	})
	sort.SliceStable(result.CandidateIdentity.Moves, func(i, j int) bool {
		if cmp := compareSymbol(result.CandidateIdentity.Moves[i].fromSymbol, result.CandidateIdentity.Moves[j].fromSymbol); cmp != 0 {
			return cmp < 0
		}
		return compareSymbol(result.CandidateIdentity.Moves[i].toSymbol, result.CandidateIdentity.Moves[j].toSymbol) < 0
	})
	sort.SliceStable(result.Identity.MoveClosure, func(i, j int) bool {
		if cmp := compareSymbol(result.Identity.MoveClosure[i].fromSymbol, result.Identity.MoveClosure[j].fromSymbol); cmp != 0 {
			return cmp < 0
		}
		return compareSymbol(result.Identity.MoveClosure[i].toSymbol, result.Identity.MoveClosure[j].toSymbol) < 0
	})
	sort.SliceStable(result.CandidateIdentity.MoveClosure, func(i, j int) bool {
		if cmp := compareSymbol(result.CandidateIdentity.MoveClosure[i].fromSymbol, result.CandidateIdentity.MoveClosure[j].fromSymbol); cmp != 0 {
			return cmp < 0
		}
		return compareSymbol(result.CandidateIdentity.MoveClosure[i].toSymbol, result.CandidateIdentity.MoveClosure[j].toSymbol) < 0
	})
	sortDiagnostics(r.diagnostics)
	result.Diagnostics = r.diagnostics
	result.HasErrors = r.hasErrors()
	return result
}

func (r *resolver) bindingSelected(binding SourceBinding) bool {
	if binding.SourceAddress == "" {
		return r.selected[binding.TargetAddress]
	}
	if !r.selected[binding.SourceAddress] {
		return false
	}
	return r.selected[binding.TargetAddress]
}

func (r *resolver) reservationSelected(res Reservation) bool {
	if len(res.Owner.Path) == 0 {
		return r.selected[addressOf(StableSymbol{Origin: res.Owner.Origin})]
	}
	return r.selected[addressOf(res.Owner)]
}

func (r *resolver) moveSelected(mv Move) bool {
	if mv.Owner != nil {
		return r.selected[addressOf(*mv.Owner)]
	}
	if r.selected[mv.ToAddress] {
		return true
	}
	for _, st := range r.modules {
		for _, closure := range st.moveClosure {
			if closure.From == mv.FromAddress && r.selected[closure.To] {
				return true
			}
		}
	}
	return false
}

func (r *resolver) addTransitivePackDependencies(used map[Origin]bool) {
	var visit func(Origin)
	visit = func(origin Origin) {
		info, ok := r.packByOrigin(origin)
		if !ok {
			return
		}
		for _, local := range sortedManifestDependencyNames(info.pack.Manifest.Dependencies) {
			install := info.pack.Dependencies[local]
			target, ok := r.packs[install]
			if !ok {
				continue
			}
			if !used[target.origin] {
				used[target.origin] = true
				visit(target.origin)
			}
		}
	}
	for origin := range used {
		visit(origin)
	}
}

func (r *resolver) hasErrors() bool {
	for _, d := range r.diagnostics {
		if d.Severity == "error" {
			return true
		}
	}
	return false
}

func compareOrigin(a, b Origin) int {
	if originRank(a) != originRank(b) {
		return originRank(a) - originRank(b)
	}
	if a.ProjectID != b.ProjectID {
		return strings.Compare(a.ProjectID, b.ProjectID)
	}
	if a.Publisher != b.Publisher {
		return strings.Compare(a.Publisher, b.Publisher)
	}
	return strings.Compare(a.PackName, b.PackName)
}

func zeroSpan() syntax.Span { return syntax.Span{} }

func packKindAllowed(kind SubjectKind) bool {
	switch kind {
	case KindEntityType, KindRelationType, KindQuery, KindView, KindReference, KindColumn, KindConstraint, KindParameter, KindTableColumn, KindExport:
		return true
	default:
		return false
	}
}

func isTopKind(kind SubjectKind) bool {
	switch kind {
	case KindEntityType, KindRelationType, KindLayer, KindEntity, KindRelation, KindQuery, KindView, KindReference:
		return true
	default:
		return false
	}
}

func isChildKind(kind SubjectKind) bool {
	switch kind {
	case KindColumn, KindConstraint, KindRow, KindParameter, KindTableColumn, KindExport:
		return true
	default:
		return false
	}
}

func validSymbol(sym StableSymbol) bool {
	if sym.Origin.Kind == OriginProject && sym.Origin.ProjectID == "" {
		return false
	}
	if sym.Origin.Kind == OriginPack && (sym.Origin.Publisher == "" || sym.Origin.PackName == "") {
		return false
	}
	if len(sym.Path) > 2 {
		return false
	}
	if len(sym.Path) == 0 {
		return true
	}
	if !isTopKind(sym.Path[0].Kind) || !isIdent(sym.Path[0].ID) {
		return false
	}
	if len(sym.Path) == 2 && (!isChildKind(sym.Path[1].Kind) || !isIdent(sym.Path[1].ID)) {
		return false
	}
	return true
}

var digestRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func isExactSemver(s string) bool {
	mainAndBuild := strings.Split(s, "+")
	if len(mainAndBuild) > 2 || (len(mainAndBuild) == 2 && !validDotIdentifiers(mainAndBuild[1], false)) {
		return false
	}
	corePart := mainAndBuild[0]
	prePart := ""
	if idx := strings.IndexByte(corePart, '-'); idx >= 0 {
		prePart = corePart[idx+1:]
		corePart = corePart[:idx]
	}
	if prePart != "" && !validDotIdentifiers(prePart, true) {
		return false
	}
	if strings.Contains(mainAndBuild[0], "-") && prePart == "" {
		return false
	}
	core := strings.Split(corePart, ".")
	if len(core) != 3 {
		return false
	}
	for _, part := range core {
		if !validNumericIdentifier(part) {
			return false
		}
	}
	return true
}

func validDotIdentifiers(s string, checkNumericLeadingZero bool) bool {
	if s == "" {
		return false
	}
	for _, part := range strings.Split(s, ".") {
		if part == "" {
			return false
		}
		allDigits := true
		for _, c := range part {
			if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '-' {
				if c < '0' || c > '9' {
					allDigits = false
				}
				continue
			}
			return false
		}
		if checkNumericLeadingZero && allDigits && !validNumericIdentifier(part) {
			return false
		}
	}
	return true
}

func validNumericIdentifier(s string) bool {
	if s == "" {
		return false
	}
	if len(s) > 1 && s[0] == '0' {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func isDigest(s string) bool { return digestRe.MatchString(s) }

func sortedKeysResolved(m map[string]ResolvedPack) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedKeysPack(m map[string]packInfo) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringMapValues(m map[string]string) []string {
	values := make([]string, 0, len(m))
	for _, v := range m {
		values = append(values, v)
	}
	sort.Strings(values)
	return values
}

func sortedModuleKeys(m map[ModuleKey]*moduleState) []ModuleKey {
	keys := make([]ModuleKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if cmp := compareOrigin(keys[i].Origin, keys[j].Origin); cmp != 0 {
			return cmp < 0
		}
		return keys[i].Path < keys[j].Path
	})
	return keys
}

func sortedExportNames(m map[string]DeclarationSymbol) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedManifestDependencyNames(m map[string]PackDependency) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedDeclMap(m map[string]DeclarationSymbol) []DeclarationSymbol {
	out := make([]DeclarationSymbol, 0, len(m))
	for _, decl := range m {
		out = append(out, decl)
	}
	sortDeclarations(out)
	return out
}

func sortedEdgeScopes(m map[string][]moveEdge) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
