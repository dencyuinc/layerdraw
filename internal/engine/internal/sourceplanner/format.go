// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package sourceplanner

import (
	"bytes"
	"context"
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

type byteEdit struct {
	start, end  int
	replacement []byte
}

func formatScopes(ctx context.Context, input CompileInput, before Snapshot, addresses []StableAddress) (CompileInput, []Diagnostic, []SemanticConflict, error) {
	candidate := cloneCompileInput(input)
	byAddress := make(map[string]resolve.SourceRange, len(before.SourceMap.Subjects))
	comments := make(map[string][]resolve.SourceRange, len(before.SourceMap.Subjects))
	for _, subject := range before.SourceMap.Subjects {
		if subject.DeclarationRange != nil {
			byAddress[subject.Address] = *subject.DeclarationRange
			comments[subject.Address] = subject.CommentRanges
		}
	}
	grouped := map[string][]byteEdit{}
	seen := map[string]bool{}
	for _, address := range addresses {
		if err := ctx.Err(); err != nil {
			return CompileInput{}, nil, nil, err
		}
		if seen[string(address)] {
			continue
		}
		seen[string(address)] = true
		r, ok := byAddress[string(address)]
		if !ok || r.Origin.Kind != "project" {
			return CompileInput{}, diagnostics("LDL1802", "semantic_operation_conflict", "format scope is not one complete project-local syntax scope", nil), nil, nil
		}
		start, end := r.StartByte, r.EndByte
		for _, comment := range comments[string(address)] {
			if comment.ModulePath == r.ModulePath && comment.StartByte < start {
				start = comment.StartByte
			}
		}
		source := candidate.ProjectSourceTree[r.ModulePath]
		if start < 0 || end < start || end > len(source) {
			return CompileInput{}, diagnostics("LDL1801", "stale_revision_or_semantic_hash", "format scope range is stale", nil), nil, nil
		}
		formatted, ok := canonicalFormat(source[start:end])
		if !ok {
			return CompileInput{}, diagnostics("LDL1101", "invalid_structure_syntax", "format scope is not a complete parseable syntax scope", nil), nil, nil
		}
		grouped[r.ModulePath] = append(grouped[r.ModulePath], byteEdit{start: start, end: end, replacement: formatted})
	}
	for path, edits := range grouped {
		sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
		merged := edits[:0]
		for _, edit := range edits {
			if len(merged) != 0 && edit.start < merged[len(merged)-1].end {
				// A parent scope subsumes a requested child scope. Formatting the
				// complete parent once is the deterministic non-overlapping plan.
				if edit.end <= merged[len(merged)-1].end {
					continue
				}
				return CompileInput{}, diagnostics("LDL1802", "semantic_operation_conflict", "format scopes overlap without containment", nil), nil, nil
			}
			merged = append(merged, edit)
		}
		source := candidate.ProjectSourceTree[path]
		var out bytes.Buffer
		cursor := 0
		for _, edit := range merged {
			out.Write(source[cursor:edit.start])
			out.Write(edit.replacement)
			cursor = edit.end
		}
		out.Write(source[cursor:])
		candidate.ProjectSourceTree[path] = out.Bytes()
	}
	return candidate, nil, nil, nil
}

// canonicalFormat prints an already complete syntax scope from Go lexer tokens.
// Token lexemes, comment text, string quoting, and heredoc bodies are retained;
// only trivia is canonicalized. Applying it twice returns identical bytes.
func canonicalFormat(source []byte) ([]byte, bool) {
	parsed := syntax.Lex(source)
	if len(parsed.Diagnostics) != 0 {
		return nil, false
	}
	var out bytes.Buffer
	indent, lineStart := 0, true
	var previous syntax.Token
	havePrevious := false
	for _, token := range parsed.Tokens {
		if token.Kind == syntax.TokenEOF {
			break
		}
		if token.Kind == syntax.TokenNewline {
			trimBufferSpaces(&out)
			if out.Len() == 0 || out.Bytes()[out.Len()-1] != '\n' {
				out.WriteByte('\n')
			}
			lineStart, havePrevious = true, false
			continue
		}
		if token.Kind == syntax.TokenRBrace && indent > 0 {
			indent--
		}
		if lineStart {
			if hasBOM(token) {
				out.Write([]byte{0xef, 0xbb, 0xbf})
			}
			out.WriteString(strings.Repeat("  ", indent))
			lineStart = false
		} else if havePrevious && needsSpace(previous, token) {
			out.WriteByte(' ')
		}
		out.WriteString(token.Raw)
		if token.Kind == syntax.TokenLBrace {
			indent++
		}
		if token.Kind == syntax.TokenHeredoc && strings.HasSuffix(token.Raw, "\n") {
			lineStart, havePrevious = true, false
			continue
		}
		previous, havePrevious = token, true
	}
	trimBufferSpaces(&out)
	return out.Bytes(), true
}

func hasBOM(token syntax.Token) bool {
	for _, trivia := range token.Leading {
		if trivia.Kind == syntax.TriviaBOM {
			return true
		}
	}
	return false
}

func needsSpace(previous, current syntax.Token) bool {
	if previous.Kind == syntax.TokenLineComment || previous.Kind == syntax.TokenDocComment || previous.Kind == syntax.TokenModuleDoc {
		return false
	}
	if current.Kind == syntax.TokenLineComment {
		return true
	}
	switch current.Kind {
	case syntax.TokenComma, syntax.TokenColon, syntax.TokenRBrace, syntax.TokenRBracket, syntax.TokenRParen, syntax.TokenDot, syntax.TokenDotDot:
		return false
	}
	switch previous.Kind {
	case syntax.TokenLBrace, syntax.TokenLBracket, syntax.TokenLParen, syntax.TokenDot, syntax.TokenDotDot, syntax.TokenDollar:
		return false
	case syntax.TokenComma, syntax.TokenColon, syntax.TokenArrow, syntax.TokenEqualEqual, syntax.TokenBangEqual, syntax.TokenLess, syntax.TokenLessEqual, syntax.TokenGreater, syntax.TokenGreaterEqual:
		return true
	case syntax.TokenAt:
		return current.Kind != syntax.TokenInteger
	}
	if current.Kind == syntax.TokenArrow || current.Kind == syntax.TokenEqualEqual || current.Kind == syntax.TokenBangEqual || current.Kind == syntax.TokenLess || current.Kind == syntax.TokenLessEqual || current.Kind == syntax.TokenGreater || current.Kind == syntax.TokenGreaterEqual || current.Kind == syntax.TokenAt {
		return true
	}
	return true
}

func trimBufferSpaces(buffer *bytes.Buffer) {
	value := buffer.Bytes()
	end := len(value)
	for end > 0 && (value[end-1] == ' ' || value[end-1] == '\t' || value[end-1] == '\r') {
		end--
	}
	if end != len(value) {
		copyValue := bytes.Clone(value[:end])
		buffer.Reset()
		buffer.Write(copyValue)
	}
}
