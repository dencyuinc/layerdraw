// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package sourceplanner

import (
	"bytes"
	"context"
	pathpkg "path"
	"sort"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func organizeStandardLayout(ctx context.Context, input CompileInput, before Snapshot) (CompileInput, []Diagnostic, []SemanticConflict, error) {
	if input.Mode != CompileProject {
		return CompileInput{}, diagnostics("LDL1802", "semantic_operation_conflict", "workspace organization applies only to project-local source", nil), nil, nil
	}
	if split, ok, err := splitStandardLayout(ctx, input, before); err != nil {
		return CompileInput{}, nil, nil, err
	} else if ok {
		return split, nil, nil, nil
	}
	byModule := map[string][]index.SourceSubjectRecord{}
	for _, subject := range before.SourceMap.Subjects {
		if subject.Module == nil || subject.Module.Origin.Kind != "project" || subject.ManifestRoot || subject.Kind == "project" {
			continue
		}
		byModule[subject.Module.ModulePath] = append(byModule[subject.Module.ModulePath], subject)
	}
	moves := map[string]string{}
	claimed := map[string]string{input.EntryPath: input.EntryPath}
	paths := sortedPaths(input.ProjectSourceTree)
	for _, oldPath := range paths {
		if err := ctx.Err(); err != nil {
			return CompileInput{}, nil, nil, err
		}
		if oldPath == input.EntryPath {
			moves[oldPath] = oldPath
			continue
		}
		target := standardModulePath(byModule[oldPath], before)
		if target == "" {
			target = oldPath
		}
		if previous, exists := claimed[target]; exists && previous != oldPath {
			base, extension := strings.TrimSuffix(target, pathpkg.Ext(target)), pathpkg.Ext(target)
			target = base + "-" + shortDigest(oldPath) + extension
		}
		claimed[target], moves[oldPath] = oldPath, target
	}
	candidate := cloneCompileInput(input)
	candidate.ProjectSourceTree = make(map[string][]byte, len(input.ProjectSourceTree))
	for _, oldPath := range paths {
		newPath := moves[oldPath]
		updated, ok := rewriteModuleReferences(input.ProjectSourceTree[oldPath], oldPath, newPath, moves)
		if !ok {
			return CompileInput{}, diagnostics("LDL1201", "module_pack_or_asset_resolution_failed", "workspace organization found an invalid relative module reference", nil), nil, nil
		}
		candidate.ProjectSourceTree[newPath] = updated
	}
	if movedEntry := moves[input.EntryPath]; movedEntry != "" {
		candidate.EntryPath = movedEntry
	}
	return candidate, nil, nil, nil
}

type organizationDeclaration struct {
	oldPath  string
	start    int
	end      int
	source   []byte
	subjects []index.SourceSubjectRecord
	target   string
	imports  map[string]map[string]bool
	exports  map[string]bool
	rewrites []byteEdit
}

// splitStandardLayout turns arbitrary project modules, including a legal
// single document.ldl, into explicit standard shards. It operates on complete
// parsed declaration nodes, carries owner row groups with their owners, and
// regenerates only the import/export closure needed by those shards.
func splitStandardLayout(ctx context.Context, input CompileInput, snapshot Snapshot) (CompileInput, bool, error) {
	declarations := []*organizationDeclaration{}
	for _, modulePath := range sortedPaths(input.ProjectSourceTree) {
		if err := ctx.Err(); err != nil {
			return CompileInput{}, false, err
		}
		source := input.ProjectSourceTree[modulePath]
		parsed := syntax.Parse(source)
		if len(parsed.Diagnostics) != 0 {
			return CompileInput{}, false, nil
		}
		for _, node := range declarationNodes(parsed.Root) {
			keyword := ""
			lexed := syntax.Lex(source[node.Span.Start:node.Span.End])
			if len(lexed.Tokens) != 0 {
				keyword = lexed.Tokens[0].Raw
			}
			if keyword == "export" {
				continue
			}
			start := node.Span.Start
			subjects := []index.SourceSubjectRecord{}
			for _, subject := range snapshot.SourceMap.Subjects {
				if subject.Module == nil || subject.Module.Origin.Kind != "project" || subject.Module.ModulePath != modulePath || subject.DeclarationRange == nil {
					continue
				}
				if subject.DeclarationRange.StartByte >= node.Span.Start && subject.DeclarationRange.EndByte <= node.Span.End {
					subjects = append(subjects, subject)
					for _, comment := range subject.CommentRanges {
						if comment.ModulePath == modulePath && comment.StartByte < start {
							start = comment.StartByte
						}
					}
				}
			}
			declarations = append(declarations, &organizationDeclaration{oldPath: modulePath, start: start, end: node.Span.End, source: bytes.Clone(source[start:node.Span.End]), subjects: subjects, imports: map[string]map[string]bool{}, exports: map[string]bool{}})
		}
	}
	if len(declarations) == 0 {
		return cloneCompileInput(input), true, nil
	}
	subjectDeclaration := map[string]*organizationDeclaration{}
	for _, declaration := range declarations {
		for _, subject := range declaration.subjects {
			subjectDeclaration[subject.Address] = declaration
		}
	}
	for _, declaration := range declarations {
		declaration.target = standardModulePath(declaration.subjects, snapshot)
		if declaration.target == "" {
			for _, subject := range declaration.subjects {
				if subject.OwnerAddress != nil && subjectDeclaration[*subject.OwnerAddress] != nil {
					declaration.target = subjectDeclaration[*subject.OwnerAddress].target
					break
				}
			}
		}
		if declaration.target == "" {
			declaration.target = input.EntryPath
		}
	}
	// A row declaration can be visited before its owner received a target.
	for _, declaration := range declarations {
		if declaration.target != input.EntryPath || len(declaration.subjects) == 0 {
			continue
		}
		for _, subject := range declaration.subjects {
			if subject.OwnerAddress != nil && subjectDeclaration[*subject.OwnerAddress] != nil && subjectDeclaration[*subject.OwnerAddress].target != "" {
				declaration.target = subjectDeclaration[*subject.OwnerAddress].target
				break
			}
		}
	}
	for _, declaration := range declarations {
		for _, subject := range declaration.subjects {
			if exportableOrganizationKind(subject.Kind) {
				declaration.exports[addressID(subject.Address)] = true
			}
		}
	}
	for _, binding := range snapshot.SourceMap.Bindings {
		sourceDeclaration, targetDeclaration := subjectDeclaration[binding.SourceAddress], subjectDeclaration[binding.TargetAddress]
		if sourceDeclaration != nil && targetDeclaration == nil && strings.HasPrefix(binding.TargetAddress, "ldl:pack:") {
			return CompileInput{}, false, nil
		}
		if sourceDeclaration == nil || targetDeclaration == nil {
			continue
		}
		if sourceDeclaration.target != targetDeclaration.target {
			if sourceDeclaration.imports[targetDeclaration.target] == nil {
				sourceDeclaration.imports[targetDeclaration.target] = map[string]bool{}
			}
			sourceDeclaration.imports[targetDeclaration.target][addressID(binding.TargetAddress)] = true
		}
		if binding.Module.Origin.Kind == "project" && binding.Module.ModulePath == sourceDeclaration.oldPath && binding.Range.StartByte >= sourceDeclaration.start && binding.Range.EndByte <= sourceDeclaration.end {
			start, end := binding.Range.StartByte-sourceDeclaration.start, binding.Range.EndByte-sourceDeclaration.start
			if start >= 0 && end >= start && end <= len(sourceDeclaration.source) {
				sourceDeclaration.rewrites = append(sourceDeclaration.rewrites, byteEdit{start: start, end: end, replacement: []byte(addressID(binding.TargetAddress))})
			}
		}
	}
	for _, declaration := range declarations {
		declaration.source = applyLocalEdits(declaration.source, declaration.rewrites)
	}
	grouped := map[string][]*organizationDeclaration{}
	for _, declaration := range declarations {
		grouped[declaration.target] = append(grouped[declaration.target], declaration)
	}
	// The entry imports every exported standard shard, making directory
	// placement explicit without filesystem discovery.
	entryDeclarations := grouped[input.EntryPath]
	if len(entryDeclarations) == 0 {
		return CompileInput{}, false, nil
	}
	entry := entryDeclarations[0]
	for target, group := range grouped {
		if target == input.EntryPath {
			continue
		}
		for _, declaration := range group {
			for name := range declaration.exports {
				if entry.imports[target] == nil {
					entry.imports[target] = map[string]bool{}
				}
				entry.imports[target][name] = true
			}
		}
	}
	candidate := cloneCompileInput(input)
	candidate.ProjectSourceTree = map[string][]byte{}
	candidate.EntryPath = input.EntryPath
	targets := make([]string, 0, len(grouped))
	for target := range grouped {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		group := grouped[target]
		sort.SliceStable(group, func(i, j int) bool {
			if group[i].oldPath != group[j].oldPath {
				return group[i].oldPath < group[j].oldPath
			}
			return group[i].start < group[j].start
		})
		imports, exports := map[string]map[string]bool{}, map[string]bool{}
		for _, declaration := range group {
			for module, names := range declaration.imports {
				if imports[module] == nil {
					imports[module] = map[string]bool{}
				}
				for name := range names {
					imports[module][name] = true
				}
			}
			for name := range declaration.exports {
				exports[name] = true
			}
		}
		var out bytes.Buffer
		for _, module := range sortedMapKeys(imports) {
			if module == target || len(imports[module]) == 0 {
				continue
			}
			relative := relativeModulePath(pathpkg.Dir(target), module)
			if !strings.HasPrefix(relative, ".") {
				relative = "./" + relative
			}
			out.WriteString("import { ")
			out.WriteString(strings.Join(sortedSet(imports[module]), ", "))
			out.WriteString(" } from ")
			out.WriteString(strconv.Quote(relative))
			out.WriteByte('\n')
		}
		if out.Len() != 0 {
			out.WriteByte('\n')
		}
		for _, declaration := range group {
			out.Write(bytes.Trim(declaration.source, "\n"))
			out.WriteString("\n\n")
		}
		if target != input.EntryPath && len(exports) != 0 {
			out.WriteString("export { ")
			out.WriteString(strings.Join(sortedSet(exports), ", "))
			out.WriteString(" }\n")
		}
		candidate.ProjectSourceTree[target] = bytes.TrimRight(out.Bytes(), "\n")
		candidate.ProjectSourceTree[target] = append(candidate.ProjectSourceTree[target], '\n')
	}
	return candidate, true, nil
}

func standardModulePath(subjects []index.SourceSubjectRecord, snapshot Snapshot) string {
	if len(subjects) == 0 {
		return ""
	}
	// Nested children and row groups travel with their owner module; select the
	// first top-level source subject in canonical SourceMap order as shard owner.
	var subject index.SourceSubjectRecord
	found := false
	for _, candidate := range subjects {
		switch candidate.Kind {
		case "entity_type", "relation_type", "layer", "entity", "relation", "query", "view", "reference":
			subject, found = candidate, true
		}
		if found {
			break
		}
	}
	if !found {
		return ""
	}
	id := addressID(subject.Address)
	switch subject.Kind {
	case "entity_type":
		return "schema/entity_types/" + id + ".ldl"
	case "relation_type":
		return "schema/relation_types/" + id + ".ldl"
	case "layer":
		return "layers/layers.ldl"
	case "entity":
		for _, entity := range snapshot.TypedAST.Graph.Entities {
			if entity.Address == subject.Address {
				return "layers/" + addressID(entity.LayerAddress) + "/" + id + ".ldl"
			}
		}
	case "relation":
		for _, relation := range snapshot.TypedAST.Graph.Relations {
			if relation.Address != subject.Address {
				continue
			}
			for _, entity := range snapshot.TypedAST.Graph.Entities {
				if entity.Address == relation.FromAddress {
					return "layers/" + addressID(entity.LayerAddress) + "/" + id + ".ldl"
				}
			}
		}
	case "query", "view":
		return "views/" + id + ".ldl"
	case "reference":
		return "references/" + id + ".ldl"
	}
	return ""
}

func rewriteModuleReferences(source []byte, oldPath, newPath string, moves map[string]string) ([]byte, bool) {
	lexed := syntax.Lex(source)
	if len(lexed.Diagnostics) != 0 {
		return nil, false
	}
	edits := []byteEdit{}
	from := false
	for _, token := range lexed.Tokens {
		if token.Kind == syntax.TokenIdentifier && token.Raw == "from" {
			from = true
			continue
		}
		if !from {
			continue
		}
		if token.Kind != syntax.TokenString {
			if token.Kind != syntax.TokenNewline {
				from = false
			}
			continue
		}
		from = false
		reference, err := strconv.Unquote(token.Raw)
		if err != nil || (!strings.HasPrefix(reference, "./") && !strings.HasPrefix(reference, "../")) {
			continue
		}
		oldTarget := pathpkg.Clean(pathpkg.Join(pathpkg.Dir(oldPath), reference))
		newTarget, moved := moves[oldTarget]
		if !moved {
			continue
		}
		updated := relativeModulePath(pathpkg.Dir(newPath), newTarget)
		if !strings.HasPrefix(updated, ".") {
			updated = "./" + updated
		}
		edits = append(edits, byteEdit{start: token.Span.Start, end: token.Span.End, replacement: []byte(strconv.Quote(updated))})
	}
	if len(edits) == 0 {
		return bytes.Clone(source), true
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var out bytes.Buffer
	cursor := 0
	for _, edit := range edits {
		out.Write(source[cursor:edit.start])
		out.Write(edit.replacement)
		cursor = edit.end
	}
	out.Write(source[cursor:])
	return out.Bytes(), true
}

func addressID(address string) string {
	parts := strings.Split(address, ":")
	if len(parts) == 0 {
		return "module"
	}
	return parts[len(parts)-1]
}
func shortDigest(value string) string {
	return strings.TrimPrefix(string(digest([]byte(value))), "sha256:")[:12]
}

func exportableOrganizationKind(kind materialize.SubjectKind) bool {
	switch kind {
	case "entity_type", "relation_type", "layer", "entity", "relation", "query", "view", "reference":
		return true
	default:
		return false
	}
}

func applyLocalEdits(source []byte, edits []byteEdit) []byte {
	if len(edits) == 0 {
		return bytes.Clone(source)
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := bytes.Clone(source)
	for _, edit := range edits {
		if edit.start < 0 || edit.end < edit.start || edit.end > len(out) {
			continue
		}
		next := make([]byte, 0, len(out)-(edit.end-edit.start)+len(edit.replacement))
		next = append(next, out[:edit.start]...)
		next = append(next, edit.replacement...)
		next = append(next, out[edit.end:]...)
		out = next
	}
	return out
}

func sortedMapKeys(values map[string]map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
func sortedSet(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func relativeModulePath(fromDir, target string) string {
	fromDir, target = pathpkg.Clean(fromDir), pathpkg.Clean(target)
	if fromDir == "." {
		fromDir = ""
	}
	fromParts, targetParts := []string{}, strings.Split(target, "/")
	if fromDir != "" {
		fromParts = strings.Split(fromDir, "/")
	}
	common := 0
	for common < len(fromParts) && common < len(targetParts) && fromParts[common] == targetParts[common] {
		common++
	}
	parts := make([]string, 0, len(fromParts)-common+len(targetParts)-common)
	for index := common; index < len(fromParts); index++ {
		parts = append(parts, "..")
	}
	parts = append(parts, targetParts[common:]...)
	if len(parts) == 0 {
		return "."
	}
	return strings.Join(parts, "/")
}
