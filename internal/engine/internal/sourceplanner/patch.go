// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package sourceplanner

import (
	"bytes"
	"context"
	"sort"
	"unicode/utf8"
)

type resolvedPatch struct {
	start, end  int
	replacement []byte
	rangeValue  SourceRange
}

func applySourcePatches(ctx context.Context, input CompileInput, before Snapshot, batch SourcePatchBatch, blobs PlannerBlobs) (CompileInput, []Diagnostic, []SemanticConflict, error) {
	candidate := cloneCompileInput(input)
	patches := append([]SourcePatchInput(nil), batch.Patches...)
	sort.SliceStable(patches, func(i, j int) bool {
		a, b := patches[i].SourceRange, patches[j].SourceRange
		if a.Origin.Kind != b.Origin.Kind {
			return a.Origin.Kind < b.Origin.Kind
		}
		if a.ModulePath != b.ModulePath {
			return a.ModulePath < b.ModulePath
		}
		return a.StartByte < b.StartByte
	})
	grouped := map[string][]resolvedPatch{}
	for _, patch := range patches {
		if err := ctx.Err(); err != nil {
			return CompileInput{}, nil, nil, err
		}
		r := patch.SourceRange
		if r.Origin.Kind != OriginKindProject || r.Origin.PackAddress != nil {
			return CompileInput{}, diagnostics("LDL1802", "semantic_operation_conflict", "Pack-origin source is immutable", &r), nil, nil
		}
		source, ok := candidate.ProjectSourceTree[r.ModulePath]
		if !ok {
			return CompileInput{}, diagnostics("LDL1801", "stale_revision_or_semantic_hash", "source patch module is not in the closed project tree", &r), nil, nil
		}
		if patch.ExpectedSourceDigest != digest(source) {
			return CompileInput{}, diagnostics("LDL1801", "stale_revision_or_semantic_hash", "source patch expected digest is stale", &r), nil, nil
		}
		start, end := intValue(r.StartByte), intValue(r.EndByte)
		if start < 0 || end < start || end > len(source) || !utf8.Valid(source) || !utf8Boundary(source, start) || !utf8Boundary(source, end) {
			// Generated diagnostics require a structurally valid range. Preserve
			// exact valid coordinates, but do not echo an inverted or host-sized
			// offset which was itself the reason for rejection.
			var diagnosticRange *SourceRange
			if start >= 0 && end >= start {
				diagnosticRange = &r
			}
			return CompileInput{}, diagnostics("LDL1001", "invalid_utf8", "source patch range is stale or splits a UTF-8 scalar", diagnosticRange), nil, nil
		}
		replacement, blobDiagnostic := resolveBlob(patch.ReplacementBlob, blobs, textMediaType)
		if blobDiagnostic != nil {
			return CompileInput{}, []Diagnostic{*blobDiagnostic}, nil, nil
		}
		if !utf8.Valid(replacement) || bytes.HasPrefix(replacement, []byte{0xff, 0xfe}) || bytes.HasPrefix(replacement, []byte{0xfe, 0xff}) {
			return CompileInput{}, diagnostics("LDL1001", "invalid_utf8", "source patch replacement has invalid or ambiguous encoding", &r), nil, nil
		}
		items := grouped[r.ModulePath]
		if len(items) != 0 && start < items[len(items)-1].end {
			return CompileInput{}, diagnostics("LDL1802", "semantic_operation_conflict", "source patch ranges overlap", &r), nil, nil
		}
		grouped[r.ModulePath] = append(items, resolvedPatch{start: start, end: end, replacement: replacement, rangeValue: r})
	}
	for path, items := range grouped {
		source := candidate.ProjectSourceTree[path]
		var out bytes.Buffer
		cursor := 0
		for _, item := range items {
			out.Write(source[cursor:item.start])
			out.Write(item.replacement)
			cursor = item.end
		}
		out.Write(source[cursor:])
		candidate.ProjectSourceTree[path] = out.Bytes()
	}
	_ = before
	return candidate, nil, nil, nil
}

func utf8Boundary(source []byte, offset int) bool {
	return offset == 0 || offset == len(source) || offset > 0 && offset < len(source) && utf8.RuneStart(source[offset])
}

func intValue(value int) int { return value }
