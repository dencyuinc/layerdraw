// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package syntax

import "testing"

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
	if got.Diagnostics[1].Code != "LDL1001" || got.Diagnostics[1].Span != (Span{Start: 5, End: 6}) {
		t.Fatalf("UTF-8 diagnostic = %+v, want LDL1001 at 5..6", got.Diagnostics[1])
	}

	backslash := Lex([]byte("\"abc\\"))
	if len(backslash.Diagnostics) == 0 || backslash.Diagnostics[len(backslash.Diagnostics)-1].Message != "unclosed string literal" {
		t.Fatalf("backslash EOF diagnostics = %+v", backslash.Diagnostics)
	}
}

func TestLexInvalidUTF8EveryContextWithoutDuplicates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  []byte
		span Span
	}{
		{name: "line comment", src: []byte{'/', '/', ' ', 0xff, '\n'}, span: Span{Start: 3, End: 4}},
		{name: "doc comment", src: []byte{'/', '/', '/', ' ', 0xff, '\n'}, span: Span{Start: 4, End: 5}},
		{name: "module doc", src: []byte{'/', '/', '!', ' ', 0xff, '\n'}, span: Span{Start: 4, End: 5}},
		{name: "heredoc body", src: []byte("reference r <<-TEXT\nbad \xff\nTEXT\n"), span: Span{Start: 24, End: 25}},
		{name: "heredoc close line", src: []byte("reference r <<-TEXT\nbody\nTE\xffXT\n"), span: Span{Start: 27, End: 28}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Lex(tt.src)
			matches := 0
			for _, diag := range got.Diagnostics {
				if diag.Code == "LDL1001" {
					matches++
					if diag.Span != tt.span {
						t.Fatalf("UTF-8 span = %+v, want %+v; diagnostics=%+v", diag.Span, tt.span, got.Diagnostics)
					}
				}
			}
			if matches != 1 {
				t.Fatalf("LDL1001 count = %d, want 1; diagnostics=%+v", matches, got.Diagnostics)
			}
			if ReconstructTokens(got.Tokens) != string(tt.src) {
				t.Fatal("invalid UTF-8 source did not round trip")
			}
		})
	}
}

func TestLexJSONCompatibleStringEscapes(t *testing.T) {
	t.Parallel()

	valid := `"\"\\\/\b\f\n\r\t\u0000\u12Af"`
	if got := Lex([]byte(valid)); len(got.Diagnostics) != 0 {
		t.Fatalf("valid JSON escapes diagnostics = %+v", got.Diagnostics)
	}

	tests := []string{`"\u"`, `"\u1"`, `"\u12xz"`, "\"raw\tcontrol\"", "\"raw\x00control\""}
	for _, src := range tests {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got := Lex([]byte(src))
			found := false
			for _, diag := range got.Diagnostics {
				if diag.Code == "LDL1101" {
					found = true
				}
			}
			if !found {
				t.Fatalf("Diagnostics = %+v, want LDL1101", got.Diagnostics)
			}
			if ReconstructTokens(got.Tokens) != src {
				t.Fatal("malformed string source did not round trip")
			}
		})
	}
}

func TestLexHeredocPinnedRules(t *testing.T) {
	t.Parallel()

	valid := "reference r <<-TEXT\t \nTEXT\t \nnext\n"
	got := Lex([]byte(valid))
	if len(got.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
	}
	assertTokenKinds(t, got.Tokens, TokenIdentifier, TokenIdentifier, TokenHeredoc, TokenNewline, TokenIdentifier)

	notClosedByPrefix := "reference r <<-TEXT\nTEXT_suffix\n"
	if got := Lex([]byte(notClosedByPrefix)); len(got.Diagnostics) == 0 {
		t.Fatal("prefix/suffix close marker diagnosed as closed; want unclosed heredoc")
	}
	badOpen := "reference r <<-TEXT // nope\nTEXT\n"
	if got := Lex([]byte(badOpen)); len(got.Diagnostics) == 0 {
		t.Fatal("opening marker trailing text accepted; want diagnostic")
	}
}

func TestLexLongestMatchAndMalformedNumericRuns(t *testing.T) {
	t.Parallel()

	src := "... .... ->> --1 1..2 1...2 <== !== <<-X\nX\n1abc 3.14foo"
	got := Lex([]byte(src))
	want := []TokenKind{
		TokenDotDot, TokenDot, TokenDotDot, TokenDotDot, TokenArrow, TokenGreater,
		TokenInvalid, TokenInteger, TokenInteger, TokenDotDot, TokenInteger,
		TokenInteger, TokenDotDot, TokenDot, TokenInteger, TokenLessEqual,
		TokenInvalid, TokenBangEqual, TokenInvalid, TokenHeredoc, TokenNewline,
		TokenInteger, TokenNumber, TokenEOF,
	}
	gotKinds := make([]TokenKind, len(got.Tokens))
	for i, tok := range got.Tokens {
		gotKinds[i] = tok.Kind
	}
	if len(gotKinds) != len(want) {
		t.Fatalf("token kinds len = %d, want %d: %v", len(gotKinds), len(want), gotKinds)
	}
	for i := range want {
		if gotKinds[i] != want[i] {
			t.Fatalf("token %d = %s raw=%q, want %s; all=%v", i, got.Tokens[i].Kind, got.Tokens[i].Raw, want[i], gotKinds)
		}
	}
	numericDiagnostics := 0
	for _, diag := range got.Diagnostics {
		if diag.Message == "malformed numeric adjacency" {
			numericDiagnostics++
		}
	}
	if numericDiagnostics != 2 {
		t.Fatalf("numeric adjacency diagnostics = %d, want 2; diagnostics=%+v", numericDiagnostics, got.Diagnostics)
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
	seeds := [][]byte{
		[]byte("project p \"P\" {}\n"),
		[]byte("entities application_service @application {\n  order_api \"Order API\" {}\n}\n"),
		[]byte("reference r <<-TEXT\nbody\nTEXT\n"),
		[]byte("rows order_api [environment, critical] {\n  order_api production: prod, true\n}\n"),
		{0, 0xff, '\n', '/', '/', 0xfe},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		got := Lex(src)
		if ReconstructTokens(got.Tokens) != string(src) {
			t.Fatalf("round trip failed for %q", string(src))
		}
		lastEnd := 0
		for i, tok := range got.Tokens {
			for _, tr := range tok.Leading {
				if tr.Span.Start != lastEnd || tr.Span.End < tr.Span.Start || tr.Raw != string(src[tr.Span.Start:tr.Span.End]) {
					t.Fatalf("bad trivia at token %d: %+v lastEnd=%d", i, tr, lastEnd)
				}
				lastEnd = tr.Span.End
			}
			if tok.Span.Start != lastEnd || tok.Span.End < tok.Span.Start || tok.Span.End > len(src) || tok.Raw != string(src[tok.Span.Start:tok.Span.End]) {
				t.Fatalf("bad token %d: %+v lastEnd=%d len=%d", i, tok, lastEnd, len(src))
			}
			lastEnd = tok.Span.End
		}
		if lastEnd != len(src) {
			t.Fatalf("token coverage ended at %d, want %d", lastEnd, len(src))
		}
		seenInvalid := map[Span]int{}
		for _, diag := range got.Diagnostics {
			if diag.Span.Start < 0 || diag.Span.End < diag.Span.Start || diag.Span.End > len(src) {
				t.Fatalf("diagnostic out of bounds: %+v len=%d", diag, len(src))
			}
			if diag.Code == "LDL1001" {
				seenInvalid[diag.Span]++
			}
		}
		for span, count := range seenInvalid {
			if count != 1 {
				t.Fatalf("duplicate LDL1001 for %+v: %d", span, count)
			}
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
