// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

type resolver struct {
	input       Input
	project     Origin
	packs       map[string]packInfo
	aliases     map[string]string
	modules     map[ModuleKey]*moduleState
	visiting    map[ModuleKey]bool
	visited     map[ModuleKey]bool
	diagnostics []Diagnostic
}

type packInfo struct {
	install string
	origin  Origin
	pack    ResolvedPack
}

type moduleState struct {
	key      ModuleKey
	kind     ModuleKind
	file     SourceFile
	ast      moduleAST
	exports  map[string]DeclarationSymbol
	local    map[SubjectKind]map[string]DeclarationSymbol
	imported map[SubjectKind]map[string]DeclarationSymbol
	bindings []SourceBinding
}

func Resolve(input Input) Result {
	r := &resolver{
		input:    input,
		packs:    map[string]packInfo{},
		aliases:  map[string]string{},
		modules:  map[ModuleKey]*moduleState{},
		visiting: map[ModuleKey]bool{},
		visited:  map[ModuleKey]bool{},
	}
	r.resolve()
	return r.result()
}

func (r *resolver) resolve() {
	r.validateProjectFiles()
	r.validatePacks()
	entry, ok := normalizePath(r.input.EntryPath)
	if !ok || !strings.HasSuffix(entry, ".ldl") {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid entry module path", ModuleKey{Origin: Origin{Kind: OriginProject}, Path: r.input.EntryPath}, zeroSpan())
		return
	}
	if r.input.Mode == "" {
		r.input.Mode = CompileProject
	}
	if r.input.Mode == CompilePack {
		var first *packInfo
		for _, p := range r.packs {
			cp := p
			first = &cp
			break
		}
		if first == nil {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "pack compile requires one installed pack", ModuleKey{Origin: Origin{Kind: OriginProject}, Path: entry}, zeroSpan())
			return
		}
		r.loadModule(ModuleKey{Origin: first.origin, Path: entry})
		return
	}
	r.project = r.peekProjectOrigin(entry)
	r.loadModule(ModuleKey{Origin: r.project, Path: entry})
}

func (r *resolver) validateProjectFiles() {
	var paths []string
	for raw := range r.input.Project.Files {
		norm, ok := normalizePath(raw)
		if !ok || norm != raw || !strings.HasSuffix(raw, ".ldl") {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid project source path", ModuleKey{Origin: Origin{Kind: OriginProject}, Path: raw}, zeroSpan())
		}
		paths = append(paths, raw)
	}
	for _, pair := range caseFoldCollisions(paths) {
		r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "case-folding source path collision", ModuleKey{Origin: Origin{Kind: OriginProject}, Path: pair[1]}, zeroSpan())
	}
}

func (r *resolver) validatePacks() {
	type identity struct {
		version string
		digest  string
		entry   string
		files   string
	}
	seenCanonical := map[string]identity{}
	seenPath := map[string]string{}
	for install, pack := range r.input.Packs.Installs {
		if !isIdent(install) {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid installed pack alias", ModuleKey{Origin: Origin{Kind: OriginProject}, Path: pack.Entry}, zeroSpan())
			continue
		}
		pub, name, ok := parseCanonicalID(pack.CanonicalID)
		if !ok {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid canonical pack id", ModuleKey{Origin: Origin{Kind: OriginProject}, Path: pack.Entry}, zeroSpan())
			continue
		}
		entry, ok := normalizePath(pack.Entry)
		if !ok || !strings.HasSuffix(entry, ".ldl") {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid pack entry path", ModuleKey{Origin: Origin{Kind: OriginPack}, Path: pack.Entry}, zeroSpan())
			continue
		}
		root, ok := normalizePath(pack.Path)
		if !ok {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid installed pack path", ModuleKey{Origin: Origin{Kind: OriginPack}, Path: pack.Entry}, zeroSpan())
			continue
		}
		if prev, exists := seenPath[root]; exists && prev != pack.CanonicalID {
			r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "installed pack path collision", ModuleKey{Origin: Origin{Kind: OriginPack}, Path: entry}, zeroSpan())
		}
		seenPath[root] = pack.CanonicalID
		filesKey := digestFileMap(pack.Files)
		id := identity{version: pack.Version, digest: pack.Digest, entry: entry, files: filesKey}
		if prev, exists := seenCanonical[pack.CanonicalID]; exists && prev != id {
			r.diag("LDL1203", "dependency_digest_mismatch", "canonical pack alias has different resolved content", ModuleKey{Origin: Origin{Kind: OriginPack}, Path: entry}, zeroSpan())
		}
		seenCanonical[pack.CanonicalID] = id
		origin := Origin{Kind: OriginPack, Publisher: pub, PackName: name}
		info := packInfo{install: install, origin: origin, pack: pack}
		r.packs[install] = info
		r.aliases[pack.CanonicalID] = install
		for p := range pack.SourceFiles {
			norm, ok := normalizePath(p)
			if !ok || norm != p || !strings.HasSuffix(p, ".ldl") {
				r.diag("LDL1201", "module_pack_or_asset_resolution_failed", "invalid pack source path", ModuleKey{Origin: origin, Path: p}, zeroSpan())
			}
		}
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
	r.visiting[key] = true
	ast := extractModule(file)
	st := &moduleState{key: key, kind: r.moduleKind(key), file: file, ast: ast, local: map[SubjectKind]map[string]DeclarationSymbol{}, imported: map[SubjectKind]map[string]DeclarationSymbol{}}
	r.modules[key] = st
	r.collectLocalDeclarations(st)
	for i := range st.ast.imports {
		target, ok := r.resolveSpecifier(key, st.ast.imports[i].Specifier, st.ast.imports[i].Range)
		if !ok {
			continue
		}
		st.ast.imports[i].Module = target
		tst := r.loadModule(target)
		if tst == nil {
			continue
		}
		r.bindImport(st, st.ast.imports[i], tst)
	}
	for i := range st.ast.exports {
		if st.ast.exports[i].Specifier == "" {
			continue
		}
		target, ok := r.resolveSpecifier(key, st.ast.exports[i].Specifier, st.ast.exports[i].Range)
		if !ok {
			continue
		}
		st.ast.exports[i].Module = target
		r.loadModule(target)
	}
	r.resolveDeclarationRefs(st)
	st.exports = r.computeExports(st)
	r.validateMovesAndReservations(st)
	delete(r.visiting, key)
	r.visited[key] = true
	return st
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

func (r *resolver) peekProjectOrigin(entry string) Origin {
	file, ok := r.input.Project.Files[entry]
	if !ok {
		return Origin{Kind: OriginProject, ProjectID: "unknown_project"}
	}
	for _, decl := range extractModule(file).declarations {
		if decl.kind == KindProject && decl.id != "" {
			return Origin{Kind: OriginProject, ProjectID: decl.id}
		}
	}
	return Origin{Kind: OriginProject, ProjectID: "unknown_project"}
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
	switch {
	case segments[0] == info.pack.Manifest.Name:
		target = info
	default:
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

func (r *resolver) collectLocalDeclarations(st *moduleState) {
	if st.key.Origin.Kind == OriginPack {
		root := StableSymbol{Origin: st.key.Origin}
		r.addLocal(st, DeclarationSymbol{Symbol: root, Address: addressOf(root), Kind: KindPack, Module: st.key})
	}
	for _, d := range st.ast.declarations {
		if st.key.Origin.Kind == OriginPack && !packKindAllowed(d.kind) {
			r.diag("LDL1102", "unknown_or_duplicate_schema_member", "declaration kind is not allowed in pack source", st.key, d.span)
			continue
		}
		if d.kind == KindProject {
			if st.key.Origin.Kind == OriginPack {
				r.diag("LDL1102", "unknown_or_duplicate_schema_member", "project declaration is not allowed in pack source", st.key, d.span)
				continue
			}
			origin := st.key.Origin
			sym := StableSymbol{Origin: origin}
			r.addLocal(st, DeclarationSymbol{Symbol: sym, Address: addressOf(sym), Kind: KindProject, ID: d.id, Module: st.key, Range: d.span})
			continue
		}
		if d.childOf != nil {
			ownerSym := StableSymbol{Origin: st.key.Origin, Path: []SymbolSegment{{Kind: d.childOf.kind, ID: d.childOf.id}}}
			sym := StableSymbol{Origin: st.key.Origin, Path: []SymbolSegment{{Kind: d.childOf.kind, ID: d.childOf.id}, {Kind: d.kind, ID: d.id}}}
			r.addLocal(st, DeclarationSymbol{Symbol: sym, Address: addressOf(sym), Kind: d.kind, ID: d.id, Owner: &ownerSym, Module: st.key, Range: d.span})
			continue
		}
		if d.kind == KindRow {
			owner, ok := st.findTop(d.owner, KindEntity)
			if !ok {
				owner, ok = st.findTop(d.owner, KindRelation)
			}
			if !ok {
				r.diag("LDL1301", "unknown_or_ambiguous_symbol", "row owner is not declared in the same module", st.key, d.span)
				continue
			}
			sym := StableSymbol{Origin: owner.Symbol.Origin, Path: append(append([]SymbolSegment{}, owner.Symbol.Path...), SymbolSegment{Kind: KindRow, ID: d.id})}
			r.addLocal(st, DeclarationSymbol{Symbol: sym, Address: addressOf(sym), Kind: KindRow, ID: d.id, Owner: &owner.Symbol, Module: st.key, Range: d.span})
			continue
		}
		sym := StableSymbol{Origin: st.key.Origin, Path: []SymbolSegment{{Kind: d.kind, ID: d.id}}}
		r.addLocal(st, DeclarationSymbol{Symbol: sym, Address: addressOf(sym), Kind: d.kind, ID: d.id, Module: st.key, Range: d.span})
	}
}

func (r *resolver) addLocal(st *moduleState, decl DeclarationSymbol) {
	if st.local[decl.Kind] == nil {
		st.local[decl.Kind] = map[string]DeclarationSymbol{}
	}
	if prev, exists := st.local[decl.Kind][decl.ID]; exists {
		r.diag("LDL1302", "duplicate_or_reserved_identity", "duplicate declaration identity", st.key, decl.Range)
		if compareSymbol(prev.Symbol, decl.Symbol) <= 0 {
			return
		}
	}
	st.local[decl.Kind][decl.ID] = decl
}

func (st *moduleState) findTop(id string, kinds ...SubjectKind) (DeclarationSymbol, bool) {
	for _, kind := range kinds {
		if decl, ok := st.local[kind][id]; ok {
			return decl, true
		}
	}
	return DeclarationSymbol{}, false
}

func packKindAllowed(kind SubjectKind) bool {
	switch kind {
	case KindEntityType, KindRelationType, KindQuery, KindView, KindReference, KindColumn, KindConstraint, KindParameter, KindTableColumn, KindExport:
		return true
	default:
		return false
	}
}

func (r *resolver) bindImport(st *moduleState, imp ImportDecl, target *moduleState) {
	if target.exports == nil {
		target.exports = r.computeExports(target)
	}
	if imp.Kind == ImportNamespace {
		for name, decl := range target.exports {
			for _, kind := range topImportKinds() {
				if decl.Kind != kind {
					continue
				}
				if st.imported[kind] == nil {
					st.imported[kind] = map[string]DeclarationSymbol{}
				}
				st.imported[kind][imp.Alias+"."+name] = decl
				st.addBinding(kind, imp.Alias+"."+name, decl)
			}
		}
		return
	}
	for _, item := range imp.Items {
		decl, ok := target.exports[item.Remote]
		if !ok {
			r.diag("LDL1301", "unknown_or_ambiguous_symbol", "named import target is not exported", st.key, item.Range)
			continue
		}
		if st.imported[decl.Kind] == nil {
			st.imported[decl.Kind] = map[string]DeclarationSymbol{}
		}
		if _, exists := st.local[decl.Kind][item.Local]; exists {
			r.diag("LDL1302", "duplicate_or_reserved_identity", "named import alias collides with local declaration", st.key, item.Range)
			continue
		}
		if _, exists := st.imported[decl.Kind][item.Local]; exists {
			r.diag("LDL1302", "duplicate_or_reserved_identity", "named import alias collides with another import", st.key, item.Range)
			continue
		}
		st.imported[decl.Kind][item.Local] = decl
		st.addBinding(decl.Kind, item.Local, decl)
	}
}

func topImportKinds() []SubjectKind {
	return []SubjectKind{KindEntityType, KindRelationType, KindLayer, KindEntity, KindRelation, KindQuery, KindView, KindReference}
}

func (r *resolver) computeExports(st *moduleState) map[string]DeclarationSymbol {
	out := map[string]DeclarationSymbol{}
	for _, exp := range st.ast.exports {
		switch exp.Kind {
		case ExportLocal:
			for _, item := range exp.Items {
				decl, ok := r.resolveAny(st, item.Local)
				if !ok {
					r.diag("LDL1301", "unknown_or_ambiguous_symbol", "exported local symbol is unknown or ambiguous", st.key, item.Range)
					continue
				}
				if _, exists := out[item.Public]; exists {
					r.diag("LDL1302", "duplicate_or_reserved_identity", "duplicate public export name", st.key, item.Range)
					continue
				}
				out[item.Public] = decl
			}
		case ExportFrom:
			target := r.modules[exp.Module]
			if target == nil {
				continue
			}
			for _, item := range exp.Items {
				decl, ok := target.exports[item.Local]
				if !ok {
					r.diag("LDL1301", "unknown_or_ambiguous_symbol", "re-export target is not exported", st.key, item.Range)
					continue
				}
				if _, exists := out[item.Public]; exists {
					r.diag("LDL1302", "duplicate_or_reserved_identity", "duplicate public export name", st.key, item.Range)
					continue
				}
				out[item.Public] = decl
			}
		case ExportStar:
			target := r.modules[exp.Module]
			if target == nil {
				continue
			}
			names := make([]string, 0, len(target.exports))
			for name := range target.exports {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				if _, exists := out[name]; exists {
					r.diag("LDL1302", "duplicate_or_reserved_identity", "duplicate public export name", st.key, exp.Range)
					continue
				}
				out[name] = target.exports[name]
			}
		}
	}
	return out
}

func (r *resolver) resolveAny(st *moduleState, text string) (DeclarationSymbol, bool) {
	var found []DeclarationSymbol
	for _, kind := range topImportKinds() {
		if decl, ok := st.local[kind][text]; ok {
			found = append(found, decl)
		}
		if decl, ok := st.imported[kind][text]; ok {
			found = append(found, decl)
		}
	}
	if len(found) != 1 {
		return DeclarationSymbol{}, false
	}
	return found[0], true
}

func (r *resolver) resolveDeclarationRefs(st *moduleState) {
	for _, decl := range st.ast.declarations {
		for _, ref := range decl.refs {
			if ref.text == "" {
				continue
			}
			target, ok := r.resolveText(st, ref.kind, ref.text)
			if !ok {
				r.diag("LDL1301", "unknown_or_ambiguous_symbol", "source binding is unknown or ambiguous", st.key, ref.span)
				continue
			}
			st.addBinding(ref.kind, ref.text, target)
		}
	}
}

func (r *resolver) resolveText(st *moduleState, kind SubjectKind, text string) (DeclarationSymbol, bool) {
	var found []DeclarationSymbol
	if decl, ok := st.local[kind][text]; ok {
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

func (st *moduleState) addBinding(kind SubjectKind, text string, target DeclarationSymbol) {
	st.bindings = append(st.bindings, SourceBinding{Module: st.key, ExpectedKind: kind, SourceText: text, Target: target.Symbol, TargetAddress: target.Address})
}

func (r *resolver) validateMovesAndReservations(st *moduleState) {
	active := map[SubjectKind]map[string]bool{}
	for kind, byID := range st.local {
		active[kind] = map[string]bool{}
		for id := range byID {
			active[kind][id] = true
		}
	}
	for _, res := range st.ast.reservations {
		if active[res.kind][res.id] {
			r.diag("LDL1302", "duplicate_or_reserved_identity", "reservation uses active identity", st.key, res.span)
		}
	}
	successor := map[SubjectKind]map[string]string{}
	for _, mv := range st.ast.moves {
		if successor[mv.kind] == nil {
			successor[mv.kind] = map[string]string{}
		}
		if _, exists := successor[mv.kind][mv.from]; exists {
			r.diag("LDL1303", "invalid_move_graph", "move source has multiple successors", st.key, mv.span)
		}
		successor[mv.kind][mv.from] = mv.to
		if active[mv.kind][mv.from] {
			r.diag("LDL1303", "invalid_move_graph", "move source is active", st.key, mv.span)
		}
	}
	for kind, graph := range successor {
		for from := range graph {
			seen := map[string]bool{}
			cur := from
			for graph[cur] != "" {
				if seen[cur] {
					r.diag("LDL1303", "invalid_move_graph", "move graph cycle", st.key, zeroSpan())
					break
				}
				seen[cur] = true
				cur = graph[cur]
			}
			if !active[kind][cur] {
				r.diag("LDL1303", "invalid_move_graph", "terminal move target is not active", st.key, zeroSpan())
			}
		}
	}
}

func (r *resolver) result() Result {
	var result Result
	for _, st := range r.modules {
		result.Modules = append(result.Modules, ResolvedModule{Origin: st.key.Origin, Path: st.key.Path, Kind: st.kind, File: st.file, Imports: st.ast.imports, Exports: st.ast.exports})
		for _, byID := range st.local {
			for _, decl := range byID {
				result.Declarations = append(result.Declarations, decl)
			}
		}
		for public, decl := range st.exports {
			result.Exports = append(result.Exports, ExportBinding{Module: st.key, PublicName: public, Target: decl.Symbol, TargetAddress: decl.Address})
		}
		result.Bindings = append(result.Bindings, st.bindings...)
		for _, res := range st.ast.reservations {
			root := StableSymbol{Origin: st.key.Origin}
			addr := addressOf(StableSymbol{Origin: st.key.Origin, Path: []SymbolSegment{{Kind: res.kind, ID: res.id}}})
			result.Identity.Reservations = append(result.Identity.Reservations, Reservation{Owner: root, Kind: res.kind, ID: res.id, Address: addr, Range: res.span})
		}
		for _, mv := range st.ast.moves {
			from := addressOf(StableSymbol{Origin: st.key.Origin, Path: []SymbolSegment{{Kind: mv.kind, ID: mv.from}}})
			to := addressOf(StableSymbol{Origin: st.key.Origin, Path: []SymbolSegment{{Kind: mv.kind, ID: mv.to}}})
			result.Identity.Moves = append(result.Identity.Moves, Move{Origin: st.key.Origin, Kind: mv.kind, OwnerID: mv.ownerID, From: mv.from, To: mv.to, FromAddress: from, ToAddress: to, Range: mv.span})
			result.Identity.MoveClosure = append(result.Identity.MoveClosure, MoveClosure{From: from, To: to})
		}
	}
	usedPacks := map[Origin]bool{}
	for _, mod := range result.Modules {
		if mod.Origin.Kind == OriginPack {
			usedPacks[mod.Origin] = true
		}
	}
	for _, info := range r.packs {
		if !usedPacks[info.origin] {
			continue
		}
		addr := addressOf(StableSymbol{Origin: info.origin})
		result.Dependencies = append(result.Dependencies, ResolvedPackSummary{Address: addr, CanonicalID: info.pack.CanonicalID, Version: info.pack.Version, Digest: info.pack.Digest})
	}
	sort.SliceStable(result.Modules, func(i, j int) bool {
		if cmp := compareOrigin(result.Modules[i].Origin, result.Modules[j].Origin); cmp != 0 {
			return cmp < 0
		}
		return result.Modules[i].Path < result.Modules[j].Path
	})
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
		return result.Bindings[i].SourceText < result.Bindings[j].SourceText
	})
	sort.SliceStable(result.Dependencies, func(i, j int) bool { return result.Dependencies[i].Address < result.Dependencies[j].Address })
	sortDiagnostics(r.diagnostics)
	result.Diagnostics = r.diagnostics
	for _, d := range result.Diagnostics {
		if d.Severity == "error" {
			result.HasErrors = true
			break
		}
	}
	return result
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
