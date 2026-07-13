// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package syntax

import (
	"strings"
	"testing"
)

func TestLexRoundTripAndTokenKinds(t *testing.T) {
	t.Parallel()

	src := "\ufeff//! module\nimport { subnet as private_subnet, vpc } from \"aws.network\"\n" +
		"project order_platform \"Order Platform\" {\n" +
		"  description \"A \\\"quoted\\\" project\"\n" +
		"  tags [prod, stg, \"legacy-prod\"]\n" +
		"  annotations { owner: \"platform\", critical: true }\n" +
		"}\n"

	got := Lex([]byte(src))
	if len(got.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
	}
	if reconstructed := ReconstructTokens(got.Tokens); reconstructed != src {
		t.Fatalf("round trip mismatch\n got: %q\nwant: %q", reconstructed, src)
	}
	assertTokenKinds(t, got.Tokens,
		TokenModuleDoc,
		TokenNewline,
		TokenIdentifier, TokenLBrace, TokenIdentifier, TokenIdentifier, TokenIdentifier, TokenComma, TokenIdentifier, TokenRBrace, TokenIdentifier, TokenString,
	)
	if got.Tokens[0].Leading[0].Kind != TriviaBOM {
		t.Fatalf("first token leading trivia = %+v, want BOM", got.Tokens[0].Leading)
	}
}

func TestLexOperatorsRangesAndHeredoc(t *testing.T) {
	t.Parallel()

	src := "reference runbook <<-TEXT\n  use 0..* and a -> b\nTEXT\n" +
		"relations writes_to { r: a -> b }\n" +
		"query q \"Q\" { where id == $id\n relation_where tags != [prod]\n depth 0..*\n }\n"
	got := Lex([]byte(src))
	if len(got.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
	}
	if ReconstructTokens(got.Tokens) != src {
		t.Fatal("heredoc/operator source did not round trip")
	}
	for _, kind := range []TokenKind{TokenHeredoc, TokenArrow, TokenDotDot, TokenEqualEqual, TokenBangEqual, TokenDollar} {
		if !hasToken(got.Tokens, kind) {
			t.Fatalf("missing token kind %s in %#v", kind, got.Tokens)
		}
	}
}

func TestLexDiagnosticsAreStable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		src        []byte
		code       string
		messageKey string
		span       Span
	}{
		{
			name:       "invalid utf8",
			src:        []byte{'p', 'r', 'o', 'j', 'e', 'c', 't', ' ', 0xff},
			code:       "LDL1001",
			messageKey: "invalid_utf8",
			span:       Span{Start: 8, End: 9},
		},
		{
			name:       "unclosed string",
			src:        []byte("project p \"unterminated\n"),
			code:       "LDL1101",
			messageKey: "invalid_structure_syntax",
			span:       Span{Start: 10, End: 23},
		},
		{
			name:       "exponent",
			src:        []byte("query q \"Q\" { depth 1e9\n}\n"),
			code:       "LDL1101",
			messageKey: "invalid_structure_syntax",
			span:       Span{Start: 20, End: 23},
		},
		{
			name:       "unclosed heredoc",
			src:        []byte("reference r <<-TEXT\nbody\n"),
			code:       "LDL1101",
			messageKey: "invalid_structure_syntax",
			span:       Span{Start: 12, End: 25},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Lex(tt.src)
			if len(got.Diagnostics) == 0 {
				t.Fatal("Diagnostics empty, want one")
			}
			diag := got.Diagnostics[0]
			if diag.Code != tt.code || diag.MessageKey != tt.messageKey || diag.Span != tt.span {
				t.Fatalf("Diagnostic = %+v, want code=%s key=%s span=%+v", diag, tt.code, tt.messageKey, tt.span)
			}
			if ReconstructTokens(got.Tokens) != string(tt.src) {
				t.Fatal("invalid source did not round trip")
			}
		})
	}
}

func TestLexerAcceptsCRLFAndTabsAsLosslessTrivia(t *testing.T) {
	t.Parallel()

	src := "project\tp \"P\" {}\r\n// comment\r\n"
	got := Lex([]byte(src))
	if len(got.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
	}
	if ReconstructTokens(got.Tokens) != src {
		t.Fatal("CRLF/tab source did not round trip")
	}
	newlines := 0
	for _, tok := range got.Tokens {
		if tok.Kind == TokenNewline {
			newlines++
		}
	}
	if newlines != 2 {
		t.Fatalf("newlines = %d, want 2", newlines)
	}
}

func TestLexPunctuationAndScalarEdges(t *testing.T) {
	t.Parallel()

	src := "( ) < <= > >= != == -> . .. * _ $ @ : , { } [ ] -2 3.14 1. \"bad\\q\" \"bad\xff\""
	got := Lex([]byte(src))
	if ReconstructTokens(got.Tokens) != src {
		t.Fatal("punctuation/scalar source did not round trip")
	}
	for _, kind := range []TokenKind{
		TokenLParen, TokenRParen, TokenLess, TokenLessEqual, TokenGreater,
		TokenGreaterEqual, TokenBangEqual, TokenEqualEqual, TokenArrow,
		TokenDot, TokenDotDot, TokenStar, TokenUnderscore, TokenDollar,
		TokenAt, TokenColon, TokenComma, TokenLBrace, TokenRBrace,
		TokenLBracket, TokenRBracket, TokenInteger, TokenNumber,
	} {
		if !hasToken(got.Tokens, kind) {
			t.Fatalf("missing %s token", kind)
		}
	}
	if len(got.Diagnostics) < 2 {
		t.Fatalf("Diagnostics = %+v, want malformed number/string diagnostics", got.Diagnostics)
	}
}

func TestLexInvalidUTF8InsideStringAndEOFBackslash(t *testing.T) {
	t.Parallel()

	src := append([]byte("\"bad "), 0xff)
	got := Lex(src)
	if len(got.Diagnostics) != 2 {
		t.Fatalf("Diagnostics = %+v, want invalid UTF-8 and unclosed string", got.Diagnostics)
	}
	if got.Diagnostics[0].Code != "LDL1001" {
		t.Fatalf("first diagnostic = %+v, want LDL1001", got.Diagnostics[0])
	}

	backslash := Lex([]byte("\"abc\\"))
	if len(backslash.Diagnostics) == 0 || backslash.Diagnostics[len(backslash.Diagnostics)-1].Message != "unclosed string literal" {
		t.Fatalf("backslash EOF diagnostics = %+v", backslash.Diagnostics)
	}
}

func TestLexHeredocMissingMarkerAndCRLFClosure(t *testing.T) {
	t.Parallel()

	missing := Lex([]byte("reference r <<-\n"))
	if len(missing.Diagnostics) == 0 || missing.Diagnostics[0].Message != "missing heredoc marker" {
		t.Fatalf("missing marker diagnostics = %+v", missing.Diagnostics)
	}

	src := "reference r <<-EOF\r\nbody\r\nEOF\r\n"
	got := Lex([]byte(src))
	if len(got.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
	}
	if ReconstructTokens(got.Tokens) != src {
		t.Fatal("CRLF heredoc did not round trip")
	}
}

func TestTokenKindStringFallback(t *testing.T) {
	t.Parallel()

	if got := TokenKind(255).String(); got != "token(255)" {
		t.Fatalf("String fallback = %q", got)
	}
}

func FuzzLexRoundTrip(f *testing.F) {
	seeds := []string{
		"project p \"P\" {}\n",
		"entities application_service @application {\n  order_api \"Order API\" {}\n}\n",
		"reference r <<-TEXT\nbody\nTEXT\n",
		"rows order_api [environment, critical] {\n  order_api production: prod, true\n}\n",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if strings.ContainsRune(src, 0) {
			t.Skip("NUL is outside LDL source fixtures")
		}
		got := Lex([]byte(src))
		if ReconstructTokens(got.Tokens) != src {
			t.Fatalf("round trip failed for %q", src)
		}
	})
}

func assertTokenKinds(t *testing.T, tokens []Token, want ...TokenKind) {
	t.Helper()
	for i, kind := range want {
		if i >= len(tokens) {
			t.Fatalf("token %d missing, want %s", i, kind)
		}
		if tokens[i].Kind != kind {
			t.Fatalf("token %d = %s (%q), want %s", i, tokens[i].Kind, tokens[i].Raw, kind)
		}
	}
}

func hasToken(tokens []Token, kind TokenKind) bool {
	for _, tok := range tokens {
		if tok.Kind == kind {
			return true
		}
	}
	return false
}
