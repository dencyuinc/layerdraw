// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package syntax implements the lossless LDL lexical and structural frontend.
package syntax

import (
	"fmt"
	"strings"
)

// Span is a zero-based UTF-8 byte half-open source range.
type Span struct {
	Start int
	End   int
}

// Empty reports whether the span contains no source bytes.
func (s Span) Empty() bool {
	return s.Start == s.End
}

// TokenKind identifies a concrete lexical token.
type TokenKind uint8

const (
	TokenInvalid TokenKind = iota
	TokenEOF
	TokenIdentifier
	TokenString
	TokenInteger
	TokenNumber
	TokenHeredoc
	TokenLineComment
	TokenDocComment
	TokenModuleDoc
	TokenNewline
	TokenLBrace
	TokenRBrace
	TokenLBracket
	TokenRBracket
	TokenLParen
	TokenRParen
	TokenComma
	TokenColon
	TokenAt
	TokenDollar
	TokenDot
	TokenDotDot
	TokenArrow
	TokenStar
	TokenUnderscore
	TokenEqualEqual
	TokenBangEqual
	TokenLess
	TokenLessEqual
	TokenGreater
	TokenGreaterEqual
)

var tokenNames = map[TokenKind]string{
	TokenInvalid:      "invalid",
	TokenEOF:          "eof",
	TokenIdentifier:   "identifier",
	TokenString:       "string",
	TokenInteger:      "integer",
	TokenNumber:       "number",
	TokenHeredoc:      "heredoc",
	TokenLineComment:  "line_comment",
	TokenDocComment:   "doc_comment",
	TokenModuleDoc:    "module_doc",
	TokenNewline:      "newline",
	TokenLBrace:       "{",
	TokenRBrace:       "}",
	TokenLBracket:     "[",
	TokenRBracket:     "]",
	TokenLParen:       "(",
	TokenRParen:       ")",
	TokenComma:        ",",
	TokenColon:        ":",
	TokenAt:           "@",
	TokenDollar:       "$",
	TokenDot:          ".",
	TokenDotDot:       "..",
	TokenArrow:        "->",
	TokenStar:         "*",
	TokenUnderscore:   "_",
	TokenEqualEqual:   "==",
	TokenBangEqual:    "!=",
	TokenLess:         "<",
	TokenLessEqual:    "<=",
	TokenGreater:      ">",
	TokenGreaterEqual: ">=",
}

func (k TokenKind) String() string {
	if name, ok := tokenNames[k]; ok {
		return name
	}
	return fmt.Sprintf("token(%d)", k)
}

// TriviaKind identifies source bytes attached to a following token.
type TriviaKind uint8

const (
	TriviaWhitespace TriviaKind = iota
	TriviaBOM
)

// Trivia preserves non-token source bytes.
type Trivia struct {
	Kind TriviaKind
	Span Span
	Raw  string
}

// Token is a lossless lexical item. Leading trivia belongs to this token.
type Token struct {
	Kind    TokenKind
	Span    Span
	Raw     string
	Leading []Trivia
}

// FullRaw returns leading trivia followed by the token lexeme.
func (t Token) FullRaw() string {
	var b strings.Builder
	for _, tr := range t.Leading {
		b.WriteString(tr.Raw)
	}
	b.WriteString(t.Raw)
	return b.String()
}

func isKeyword(t Token, keyword string) bool {
	return t.Kind == TokenIdentifier && t.Raw == keyword
}

func isValueStart(kind TokenKind) bool {
	switch kind {
	case TokenString, TokenHeredoc, TokenInteger, TokenNumber, TokenIdentifier,
		TokenDollar, TokenLBracket, TokenLBrace, TokenUnderscore:
		return true
	default:
		return false
	}
}
