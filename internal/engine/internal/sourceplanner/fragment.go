// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package sourceplanner

import (
	"bytes"
	"context"
	"unicode/utf8"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func applyFragment(ctx context.Context, compiler Compiler, input CompileInput, before Snapshot, fragment FragmentInput, blobs PlannerBlobs) (CompileInput, []Diagnostic, []SemanticConflict, error) {
	fragmentBytes, blobDiagnostic := resolveBlob(fragment.FragmentBlob, blobs, textMediaType)
	if blobDiagnostic != nil {
		return CompileInput{}, []Diagnostic{*blobDiagnostic}, nil, nil
	}
	if !utf8.Valid(fragmentBytes) {
		return CompileInput{}, diagnostics("LDL1001", "invalid_utf8", "fragment is not valid UTF-8", nil), nil, nil
	}
	parsed := syntax.Parse(fragmentBytes)
	if len(parsed.Diagnostics) != 0 {
		return CompileInput{}, mapSyntaxDiagnostics(parsed.Diagnostics), nil, nil
	}
	declarations := declarationNodes(parsed.Root)
	if len(declarations) == 0 {
		return CompileInput{}, diagnostics("LDL1101", "invalid_structure_syntax", "fragment contains no complete LDL declaration", nil), nil, nil
	}
	if fragment.Intent == "replace" && len(declarations) != 1 {
		return CompileInput{}, diagnostics("LDL1802", "semantic_operation_conflict", "replacement fragment must contain exactly one complete declaration", nil), nil, nil
	}
	allowed := map[SubjectKind]bool{}
	for _, kind := range fragment.AllowedKinds {
		allowed[kind] = true
	}
	for _, declaration := range declarations {
		families := syntacticKinds(fragmentBytes[declaration.Span.Start:declaration.Span.End])
		matched := false
		for _, kind := range families {
			matched = matched || allowed[kind]
		}
		if !matched {
			return CompileInput{}, diagnostics("LDL1802", "semantic_operation_conflict", "fragment declaration kind is outside allowed_kinds", nil), nil, nil
		}
	}
	formatted, ok := canonicalFormat(fragmentBytes)
	if !ok {
		return CompileInput{}, diagnostics("LDL1101", "invalid_structure_syntax", "fragment cannot be canonically formatted", nil), nil, nil
	}
	formatted = bytes.Trim(formatted, "\n")

	candidate := cloneCompileInput(input)
	targets := sourceSubjects(before)
	modulePath := input.EntryPath
	start, end := -1, -1
	if fragment.Intent == "replace" {
		if fragment.ReplacementTarget == nil {
			return CompileInput{}, diagnostics("LDL1802", "semantic_operation_conflict", "replacement fragment has no target", nil), nil, nil
		}
		target, exists := targets[string(*fragment.ReplacementTarget)]
		if !exists || target.DeclarationRange == nil || target.Module == nil || target.Module.Origin.Kind != OriginKindProject {
			return CompileInput{}, diagnostics("LDL1802", "semantic_operation_conflict", "replacement target is not a project-local declaration", nil), nil, nil
		}
		if target.OwnerAddress == nil || *target.OwnerAddress != fragment.InsertionOwner {
			return CompileInput{}, diagnostics("LDL1802", "semantic_operation_conflict", "replacement target is not owned by insertion_owner", target.DeclarationRange), nil, nil
		}
		modulePath, start, end = target.Module.ModulePath, intValue(target.DeclarationRange.StartByte), intValue(target.DeclarationRange.EndByte)
	} else if fragment.Intent == "insert" {
		var placementDiagnostic *Diagnostic
		modulePath, start, placementDiagnostic = fragmentInsertionPoint(input, before, fragment)
		if placementDiagnostic != nil {
			return CompileInput{}, []Diagnostic{*placementDiagnostic}, nil, nil
		}
		end = start
	} else {
		return CompileInput{}, diagnostics("LDL1802", "semantic_operation_conflict", "fragment intent is neither insert nor replace", nil), nil, nil
	}
	source, exists := candidate.ProjectSourceTree[modulePath]
	if !exists || start < 0 || end < start || end > len(source) {
		return CompileInput{}, diagnostics("LDL1801", "stale_revision_or_semantic_hash", "fragment placement range is stale", nil), nil, nil
	}
	replacement := formatted
	if fragment.Intent == "insert" {
		var framed bytes.Buffer
		if start > 0 && source[start-1] != '\n' {
			framed.WriteByte('\n')
		}
		framed.Write(formatted)
		if start < len(source) && (framed.Len() == 0 || framed.Bytes()[framed.Len()-1] != '\n') {
			framed.WriteByte('\n')
		}
		if start == len(source) && framed.Len() != 0 && framed.Bytes()[framed.Len()-1] != '\n' {
			framed.WriteByte('\n')
		}
		replacement = framed.Bytes()
	}
	var edited bytes.Buffer
	edited.Write(source[:start])
	edited.Write(replacement)
	edited.Write(source[end:])
	candidate.ProjectSourceTree[modulePath] = edited.Bytes()

	// A standalone parse proves syntax; this full compile resolves the fragment
	// in the existing symbol environment before its local edit is accepted.
	afterResult, err := compiler.Compile(ctx, cloneCompileInput(candidate))
	if err != nil {
		return CompileInput{}, nil, nil, err
	}
	after := afterResult.Snapshot()
	if len(after.Diagnostics) != 0 {
		return CompileInput{}, mapCompilerDiagnostics(after.Diagnostics), nil, nil
	}
	if diagnostic := verifyFragmentScope(before, after, fragment, allowed); diagnostic != nil {
		return CompileInput{}, []Diagnostic{*diagnostic}, nil, nil
	}
	return candidate, nil, nil, nil
}

func declarationNodes(root *syntax.Node) []*syntax.Node {
	if root == nil {
		return nil
	}
	out := []*syntax.Node{}
	for _, child := range root.Children {
		if node, ok := child.(*syntax.Node); ok && node.Kind == syntax.NodeDeclaration {
			out = append(out, node)
		}
	}
	return out
}

func syntacticKinds(source []byte) []SubjectKind {
	lexed := syntax.Lex(source)
	if len(lexed.Tokens) == 0 {
		return nil
	}
	keyword := lexed.Tokens[0].Raw
	switch keyword {
	case "project":
		return []SubjectKind{SubjectKindProject}
	case "entity_type":
		return []SubjectKind{SubjectKindEntityType}
	case "relation_type":
		return []SubjectKind{SubjectKindRelationType}
	case "layers":
		return []SubjectKind{SubjectKindLayer}
	case "entities":
		return []SubjectKind{SubjectKindEntity}
	case "relations":
		return []SubjectKind{SubjectKindRelation}
	case "rows":
		return []SubjectKind{SubjectKindEntityRow, SubjectKindRelationRow}
	case "relation_rows":
		return []SubjectKind{SubjectKindRelationRow}
	case "query":
		return []SubjectKind{SubjectKindQuery}
	case "view":
		return []SubjectKind{SubjectKindView}
	case "reference":
		return []SubjectKind{SubjectKindReference}
	default:
		return nil
	}
}

func fragmentInsertionPoint(input CompileInput, before Snapshot, fragment FragmentInput) (string, int, *Diagnostic) {
	modulePath := input.EntryPath
	position := "end"
	var anchor *SourceSubjectRecord
	if fragment.Placement != nil {
		position = fragment.Placement.Position
		if fragment.Placement.ModulePath != nil {
			modulePath = string(*fragment.Placement.ModulePath)
		}
		if fragment.Placement.GroupAnchorAddress != nil {
			for _, subject := range before.SourceMap.Subjects {
				if subject.Address == string(*fragment.Placement.GroupAnchorAddress) {
					copyValue := mapSourceSubject(subject)
					anchor = &copyValue
					break
				}
			}
			if anchor == nil || anchor.Module == nil || anchor.DeclarationRange == nil || anchor.Module.Origin.Kind != OriginKindProject {
				d := diagnostic("LDL1802", "semantic_operation_conflict", "fragment placement anchor is unavailable", nil)
				return "", 0, &d
			}
			if fragment.Placement.ModulePath != nil && anchor.Module.ModulePath != modulePath {
				d := diagnostic("LDL1802", "semantic_operation_conflict", "fragment anchor and module_path disagree", nil)
				return "", 0, &d
			}
			modulePath = anchor.Module.ModulePath
		}
	}
	source, exists := input.ProjectSourceTree[modulePath]
	if !exists {
		d := diagnostic("LDL1802", "semantic_operation_conflict", "fragment placement module is unavailable", nil)
		return "", 0, &d
	}
	switch position {
	case "before":
		if anchor == nil {
			d := diagnostic("LDL1802", "semantic_operation_conflict", "before placement requires an anchor", nil)
			return "", 0, &d
		}
		return modulePath, intValue(anchor.DeclarationRange.StartByte), nil
	case "after", "end":
		if anchor != nil {
			return modulePath, intValue(anchor.DeclarationRange.EndByte), nil
		}
		return modulePath, len(source), nil
	default:
		d := diagnostic("LDL1802", "semantic_operation_conflict", "fragment placement position is invalid", nil)
		return "", 0, &d
	}
}

func verifyFragmentScope(before, after Snapshot, fragment FragmentInput, allowed map[SubjectKind]bool) *Diagnostic {
	old := map[string]index.SourceSubjectRecord{}
	oldHashes := map[string]string{}
	for _, item := range before.SourceMap.Subjects {
		old[item.Address] = item
	}
	for _, item := range before.SubjectSemanticHashes {
		oldHashes[item.Address] = item.Hash
	}
	afterHashes := map[string]string{}
	for _, item := range after.SubjectSemanticHashes {
		afterHashes[item.Address] = item.Hash
	}
	touched := []index.SourceSubjectRecord{}
	for _, item := range after.SourceMap.Subjects {
		if previous, exists := old[item.Address]; !exists || oldHashes[item.Address] != afterHashes[item.Address] {
			_ = previous
			if item.Kind == "project" || item.Kind == "pack" {
				continue
			}
			touched = append(touched, item)
		}
	}
	if len(touched) == 0 {
		d := diagnostic("LDL1802", "semantic_operation_conflict", "fragment produces no scoped semantic declaration", nil)
		return &d
	}
	for _, item := range touched {
		if !allowed[SubjectKind(item.Kind)] {
			d := diagnostic("LDL1802", "semantic_operation_conflict", "resolved fragment kind is outside allowed_kinds", nil)
			return &d
		}
		if item.OwnerAddress == nil || string(*item.OwnerAddress) != string(fragment.InsertionOwner) {
			d := diagnostic("LDL1802", "semantic_operation_conflict", "resolved fragment declaration is outside insertion_owner", nil)
			return &d
		}
	}
	return nil
}

func sourceSubjects(snapshot Snapshot) map[string]SourceSubjectRecord {
	out := make(map[string]SourceSubjectRecord, len(snapshot.SourceMap.Subjects))
	for _, subject := range snapshot.SourceMap.Subjects {
		out[subject.Address] = mapSourceSubject(subject)
	}
	return out
}

func mapSourceSubject(subject index.SourceSubjectRecord) SourceSubjectRecord {
	out := SourceSubjectRecord{Address: StableAddress(subject.Address), Kind: SubjectKind(subject.Kind), ManifestRoot: subject.ManifestRoot, CommentRanges: []SourceRange{}}
	if subject.OwnerAddress != nil {
		value := StableAddress(*subject.OwnerAddress)
		out.OwnerAddress = &value
	}
	if subject.Module != nil {
		value := ModuleRef{Origin: mapOrigin(subject.Module.Origin), ModulePath: subject.Module.ModulePath}
		out.Module = &value
	}
	if subject.DeclarationRange != nil {
		value := mapRange(*subject.DeclarationRange)
		out.DeclarationRange = &value
	}
	for _, comment := range subject.CommentRanges {
		out.CommentRanges = append(out.CommentRanges, mapRange(comment))
	}
	return out
}

func mapSyntaxDiagnostics(values []syntax.Diagnostic) []Diagnostic {
	out := make([]Diagnostic, 0, len(values))
	for _, value := range values {
		r := SourceRange{Origin: SourceOrigin{Kind: OriginKindProject}, ModulePath: "fragment.ldl", StartByte: canonicalUint(value.Span.Start), EndByte: canonicalUint(value.Span.End)}
		out = append(out, diagnostic(value.Code, value.MessageKey, value.Message, &r))
	}
	return out
}

func mapCompilerDiagnostics(values []CompileDiagnostic) []Diagnostic {
	out := make([]Diagnostic, 0, len(values))
	for _, value := range values {
		var r *SourceRange
		if value.Range != nil {
			mapped := mapRange(*value.Range)
			r = &mapped
		}
		out = append(out, diagnostic(value.Code, value.MessageKey, value.Message, r))
	}
	return out
}

func mapRange(value resolve.SourceRange) SourceRange {
	return SourceRange{Origin: mapOrigin(value.Origin), ModulePath: value.ModulePath, StartByte: canonicalUint(value.StartByte), EndByte: canonicalUint(value.EndByte)}
}

func mapOrigin(value resolve.SourceOrigin) SourceOrigin {
	origin := SourceOrigin{Kind: OriginKind(value.Kind)}
	if value.PackAddress != "" {
		address := PackRootAddress(value.PackAddress)
		origin.PackAddress = &address
	}
	return origin
}

func canonicalUint(value int) int { return value }
