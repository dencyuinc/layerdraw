// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package syntax

import (
	"bytes"
	"unicode/utf8"
)

// LexResult is the complete output of lexical analysis.
type LexResult struct {
	Tokens      []Token
	Diagnostics []Diagnostic
}

// Lex tokenizes LDL source bytes without requiring a language header.
func Lex(src []byte) LexResult {
	l := lexer{src: src}
	l.lex()
	return LexResult{Tokens: l.tokens, Diagnostics: l.diagnostics}
}

// ReconstructTokens returns the exact source represented by a complete token
// stream, including trivia attached to EOF.
func ReconstructTokens(tokens []Token) string {
	var out []byte
	for _, tok := range tokens {
		out = append(out, tok.FullRaw()...)
	}
	return string(out)
}

type lexer struct {
	src         []byte
	pos         int
	leading     []Trivia
	tokens      []Token
	diagnostics []Diagnostic
}

func (l *lexer) lex() {
	for l.pos < len(l.src) {
		if l.consumeTrivia() {
			continue
		}
		start := l.pos
		switch ch := l.src[l.pos]; {
		case ch == '\n':
			l.emit(TokenNewline, start, start+1)
		case ch == '\r':
			if l.match("\r\n") {
				l.emit(TokenNewline, start, start+2)
			} else {
				l.emit(TokenNewline, start, start+1)
			}
		case l.match("//!"):
			l.scanLineComment(TokenModuleDoc)
		case l.match("///"):
			l.scanLineComment(TokenDocComment)
		case l.match("//"):
			l.scanLineComment(TokenLineComment)
		case l.match("<<-"):
			l.scanHeredoc()
		case ch == '"':
			l.scanString()
		case ch == '-' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1]):
			l.scanNumber()
		case isDigit(ch):
			l.scanNumber()
		case isIdentStart(ch):
			l.scanIdentifier()
		default:
			l.scanPunctuation()
		}
	}
	l.emit(TokenEOF, len(l.src), len(l.src))
}

func (l *lexer) consumeTrivia() bool {
	start := l.pos
	if l.pos == 0 && bytes.HasPrefix(l.src, []byte{0xEF, 0xBB, 0xBF}) {
		l.pos += 3
		l.addTrivia(TriviaBOM, start, l.pos)
		return true
	}
	for l.pos < len(l.src) {
		switch l.src[l.pos] {
		case ' ', '\t', '\f', '\v':
			l.pos++
		default:
			if l.pos > start {
				l.addTrivia(TriviaWhitespace, start, l.pos)
				return true
			}
			return false
		}
	}
	if l.pos > start {
		l.addTrivia(TriviaWhitespace, start, l.pos)
		return true
	}
	return false
}

func (l *lexer) addTrivia(kind TriviaKind, start, end int) {
	l.leading = append(l.leading, Trivia{Kind: kind, Span: Span{Start: start, End: end}, Raw: string(l.src[start:end])})
}

func (l *lexer) emit(kind TokenKind, start, end int) {
	tok := Token{Kind: kind, Span: Span{Start: start, End: end}, Raw: string(l.src[start:end]), Leading: l.leading}
	l.leading = nil
	l.tokens = append(l.tokens, tok)
	l.pos = end
}

func (l *lexer) match(s string) bool {
	return bytes.HasPrefix(l.src[l.pos:], []byte(s))
}

func (l *lexer) scanLineComment(kind TokenKind) {
	start := l.pos
	for l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
		l.pos++
	}
	l.emit(kind, start, l.pos)
}

func (l *lexer) scanIdentifier() {
	start := l.pos
	for l.pos < len(l.src) && isIdentContinue(l.src[l.pos]) {
		l.pos++
	}
	l.emit(TokenIdentifier, start, l.pos)
}

func (l *lexer) scanNumber() {
	start := l.pos
	if l.src[l.pos] == '-' {
		l.pos++
	}
	for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
		l.pos++
	}
	kind := TokenInteger
	if l.pos < len(l.src) && l.src[l.pos] == '.' && !(l.pos+1 < len(l.src) && l.src[l.pos+1] == '.') {
		kind = TokenNumber
		l.pos++
		if l.pos >= len(l.src) || !isDigit(l.src[l.pos]) {
			l.diagnostics = append(l.diagnostics, invalidStructure(Span{Start: start, End: l.pos}, "malformed number literal"))
		}
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			l.pos++
		}
	}
	if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
		for l.pos < len(l.src) && !isDelimiter(l.src[l.pos]) {
			l.pos++
		}
		l.diagnostics = append(l.diagnostics, invalidStructure(Span{Start: start, End: l.pos}, "exponent notation is not valid LDL syntax"))
	}
	l.emit(kind, start, l.pos)
}

func (l *lexer) scanString() {
	start := l.pos
	l.pos++
	for l.pos < len(l.src) {
		r, width := utf8.DecodeRune(l.src[l.pos:])
		if r == utf8.RuneError && width == 1 {
			l.diagnostics = append(l.diagnostics, invalidUTF8(Span{Start: l.pos, End: l.pos + 1}))
			l.pos++
			continue
		}
		if l.src[l.pos] == '"' {
			l.pos++
			l.emit(TokenString, start, l.pos)
			return
		}
		if l.src[l.pos] == '\n' || l.src[l.pos] == '\r' {
			l.diagnostics = append(l.diagnostics, invalidStructure(Span{Start: start, End: l.pos}, "unclosed string literal"))
			l.emit(TokenString, start, l.pos)
			return
		}
		if l.src[l.pos] == '\\' {
			l.pos++
			if l.pos >= len(l.src) {
				break
			}
			if !isJSONEscape(l.src[l.pos]) {
				l.diagnostics = append(l.diagnostics, invalidStructure(Span{Start: l.pos - 1, End: l.pos + 1}, "malformed string escape"))
			}
		}
		l.pos += width
	}
	l.diagnostics = append(l.diagnostics, invalidStructure(Span{Start: start, End: len(l.src)}, "unclosed string literal"))
	l.emit(TokenString, start, len(l.src))
}

func (l *lexer) scanHeredoc() {
	start := l.pos
	l.pos += len("<<-")
	markerStart := l.pos
	if l.pos >= len(l.src) || !isHeredocMarkerStart(l.src[l.pos]) {
		l.diagnostics = append(l.diagnostics, invalidStructure(Span{Start: start, End: l.pos}, "missing heredoc marker"))
		l.emit(TokenHeredoc, start, l.pos)
		return
	}
	for l.pos < len(l.src) && isHeredocMarkerContinue(l.src[l.pos]) {
		l.pos++
	}
	marker := string(l.src[markerStart:l.pos])
	for l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
		l.pos++
	}
	if l.pos < len(l.src) {
		if l.match("\r\n") {
			l.pos += 2
		} else {
			l.pos++
		}
	}
	closed := false
	for l.pos < len(l.src) {
		lineStart := l.pos
		lineEnd := lineStart
		for lineEnd < len(l.src) && l.src[lineEnd] != '\n' && l.src[lineEnd] != '\r' {
			lineEnd++
		}
		if string(bytes.TrimSpace(l.src[lineStart:lineEnd])) == marker {
			closed = true
			l.pos = lineEnd
			if l.pos < len(l.src) {
				if l.match("\r\n") {
					l.pos += 2
				} else {
					l.pos++
				}
			}
			break
		}
		l.pos = lineEnd
		if l.pos < len(l.src) {
			if l.match("\r\n") {
				l.pos += 2
			} else {
				l.pos++
			}
		}
	}
	if !closed {
		l.diagnostics = append(l.diagnostics, invalidStructure(Span{Start: start, End: len(l.src)}, "unclosed heredoc"))
	}
	l.emit(TokenHeredoc, start, l.pos)
}

func (l *lexer) scanPunctuation() {
	start := l.pos
	pairs := []struct {
		text string
		kind TokenKind
	}{
		{"..", TokenDotDot}, {"->", TokenArrow}, {"==", TokenEqualEqual},
		{"!=", TokenBangEqual}, {"<=", TokenLessEqual}, {">=", TokenGreaterEqual},
	}
	for _, pair := range pairs {
		if l.match(pair.text) {
			l.emit(pair.kind, start, start+len(pair.text))
			return
		}
	}
	switch l.src[l.pos] {
	case '{':
		l.emit(TokenLBrace, start, start+1)
	case '}':
		l.emit(TokenRBrace, start, start+1)
	case '[':
		l.emit(TokenLBracket, start, start+1)
	case ']':
		l.emit(TokenRBracket, start, start+1)
	case '(':
		l.emit(TokenLParen, start, start+1)
	case ')':
		l.emit(TokenRParen, start, start+1)
	case ',':
		l.emit(TokenComma, start, start+1)
	case ':':
		l.emit(TokenColon, start, start+1)
	case '@':
		l.emit(TokenAt, start, start+1)
	case '$':
		l.emit(TokenDollar, start, start+1)
	case '.':
		l.emit(TokenDot, start, start+1)
	case '*':
		l.emit(TokenStar, start, start+1)
	case '_':
		l.emit(TokenUnderscore, start, start+1)
	case '<':
		l.emit(TokenLess, start, start+1)
	case '>':
		l.emit(TokenGreater, start, start+1)
	default:
		r, width := utf8.DecodeRune(l.src[l.pos:])
		if r == utf8.RuneError && width == 1 {
			l.diagnostics = append(l.diagnostics, invalidUTF8(Span{Start: start, End: start + 1}))
		} else {
			l.diagnostics = append(l.diagnostics, invalidStructure(Span{Start: start, End: start + width}, "unexpected character"))
		}
		l.emit(TokenInvalid, start, start+width)
	}
}

func isIdentStart(b byte) bool {
	return b >= 'a' && b <= 'z'
}

func isIdentContinue(b byte) bool {
	return isIdentStart(b) || isDigit(b) || b == '_'
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func isDelimiter(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '{', '}', '[', ']', '(', ')', ',', ':', '@', '$':
		return true
	default:
		return false
	}
}

func isJSONEscape(b byte) bool {
	switch b {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't', 'u':
		return true
	default:
		return false
	}
}

func isHeredocMarkerStart(b byte) bool {
	return (b >= 'A' && b <= 'Z') || isIdentStart(b)
}

func isHeredocMarkerContinue(b byte) bool {
	return isHeredocMarkerStart(b) || isDigit(b) || b == '_'
}
