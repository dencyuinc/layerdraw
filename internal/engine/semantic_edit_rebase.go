// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

// rebaseSnapshotSourceRanges carries only source-location authority across a
// temporarily invalid private overlay. Semantic identities and hashes remain
// those of the last successful compile and can never escape as a result.
func rebaseSnapshotSourceRanges(snapshot Snapshot, before, after map[string][]byte) Snapshot {
	out := Snapshot{CompileOutput: deepClone(snapshot.CompileOutput)}
	byPath := map[string][]PlannedSourceEdit{}
	for path, left := range before {
		right, ok := after[path]
		if !ok || bytes.Equal(left, right) {
			continue
		}
		byPath[path] = minimalModuleEdits(path, left, right)
	}
	for i := range out.SourceMap.Files {
		file := &out.SourceMap.Files[i]
		if file.Origin.Kind != resolve.OriginProject {
			continue
		}
		if source, ok := after[file.ModulePath]; ok {
			file.Digest = semanticDigest(source)
			file.ByteLength = len(source)
		}
	}
	for i := range out.SourceMap.Subjects {
		subject := &out.SourceMap.Subjects[i]
		if subject.Module == nil || subject.Module.Origin.Kind != resolve.OriginProject {
			continue
		}
		edits := byPath[subject.Module.ModulePath]
		if subject.DeclarationRange != nil {
			value := rebaseSourceRange(*subject.DeclarationRange, edits)
			subject.DeclarationRange = &value
		}
		for j := range subject.CommentRanges {
			subject.CommentRanges[j] = rebaseSourceRange(subject.CommentRanges[j], edits)
		}
	}
	for i := range out.SourceMap.Bindings {
		binding := &out.SourceMap.Bindings[i]
		if binding.Module.Origin.Kind == resolve.OriginProject {
			binding.Range = rebaseSourceRange(binding.Range, byPath[binding.Module.ModulePath])
		}
	}
	for i := range out.SourceMap.Exports {
		binding := &out.SourceMap.Exports[i]
		if binding.Module.Origin.Kind == resolve.OriginProject {
			binding.Range = rebaseSourceRange(binding.Range, byPath[binding.Module.ModulePath])
		}
	}
	for i := range out.SourceMap.Assets {
		asset := &out.SourceMap.Assets[i]
		if asset.Origin.Kind == resolve.OriginProject {
			asset.Range = rebaseSourceRange(asset.Range, byPath[asset.ModulePath])
		}
	}
	for i := range out.SemanticIndex.References {
		ref := &out.SemanticIndex.References[i]
		if ref.Range.Origin.Kind == resolve.OriginProject {
			ref.Range = rebaseSourceRange(ref.Range, byPath[ref.Range.ModulePath])
		}
	}
	for i := range out.LosslessSyntaxTree.Files {
		file := &out.LosslessSyntaxTree.Files[i]
		if file.Origin.Kind != resolve.OriginProject {
			continue
		}
		edits := byPath[file.ModulePath]
		if source, ok := after[file.ModulePath]; ok {
			file.Source = bytes.Clone(source)
		}
		for j := range file.Tokens {
			file.Tokens[j].Span = rebaseSyntaxSpan(file.Tokens[j].Span, edits)
			for k := range file.Tokens[j].Leading {
				file.Tokens[j].Leading[k].Span = rebaseSyntaxSpan(file.Tokens[j].Leading[k].Span, edits)
			}
		}
		syntax.Walk(file.Root, func(node *syntax.Node) { node.Span = rebaseSyntaxSpan(node.Span, edits) })
	}
	return out
}

func rebaseSourceRange(value SourceRange, edits []PlannedSourceEdit) SourceRange {
	value.StartByte = rebaseOffset(value.StartByte, edits)
	value.EndByte = rebaseOffset(value.EndByte, edits)
	if value.EndByte < value.StartByte {
		value.EndByte = value.StartByte
	}
	return value
}
func rebaseSyntaxSpan(value syntax.Span, edits []PlannedSourceEdit) syntax.Span {
	value.Start = rebaseOffset(value.Start, edits)
	value.End = rebaseOffset(value.End, edits)
	if value.End < value.Start {
		value.End = value.Start
	}
	return value
}
func rebaseOffset(offset int, edits []PlannedSourceEdit) int {
	delta := 0
	for _, edit := range edits {
		if offset <= edit.StartByte {
			return offset + delta
		}
		if offset >= edit.EndByte {
			delta += len(edit.Replacement) - (edit.EndByte - edit.StartByte)
			continue
		}
		relative := offset - edit.StartByte
		if relative > len(edit.Replacement) {
			relative = len(edit.Replacement)
		}
		return edit.StartByte + delta + relative
	}
	return offset + delta
}
