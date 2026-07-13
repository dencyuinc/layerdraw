// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package syntax

import (
	"reflect"
	"strings"
	"testing"
)

const validStructuralFixture = `//! LayerDraw module docs
import aws from "aws_complete"
import { subnet as private_subnet, vpc } from "aws_complete.network"

project order_platform "Order Platform" {
  description "Production order platform"
  tags [prod, stg]
  annotations { owner: "platform", critical: true, environments: [prod, stg] }
}

layers {
  application "Application" @0 {}
}

entity_type application_service "Application Service" {
  representation shape rect
  columns {
    environment "Environment" enum [prod, stg]
    critical "Critical" boolean
  }
}

relation_type writes_to "Writes To" dependency {
  from role source
  to role target
  label "writes to"
  cardinality 0..*
}

entities application_service @application {
  order_api "Order API" {
    description "Handles orders"
  }
}

rows order_api [environment, critical] {
  order_api production: prod, true
}

relations writes_to {
  order_api_writes_db: order_api -> order_db "writes" {}
}

relation_rows order_api_writes_db [environment] {
  order_api_writes_db production: prod
}

query production_scope "Production Scope" {
  parameters {
    environment enum [prod, stg]
  }
  select roots [order_api]
  where id == $environment
}

view production_topology "Production Topology" topology {
  source query production_scope
  shape diagram {
    layout layered
  }
}

reference operating_rules <<-TEXT
  No executable directives.
TEXT

reserved {
  entities [legacy_api]
}

moves {
  entity old_api -> order_api
  entity_row order_api old_snapshot -> production
}

export { order_api as public_order_api }
export * from "./schema/entity_types/application.ldl"
`

func TestParseValidStructuralFixture(t *testing.T) {
	t.Parallel()

	got := Parse([]byte(validStructuralFixture))
	assertParseInvariants(t, []byte(validStructuralFixture), got)
	if len(got.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
	}
	if ReconstructTokens(got.Tokens) != validStructuralFixture {
		t.Fatal("valid fixture did not round trip")
	}
	counts := countNodes(got.Root)
	for _, want := range []NodeKind{
		NodeFile, NodeImportDecl, NodeDeclaration, NodeBlock, NodeItemBlock,
		NodeLayerItem, NodeEntityItem, NodeRelationItem, NodeRowItem,
		NodeMoveItem, NodeExportDecl, NodeColumnHeader, NodeList, NodeObject,
		NodeRange, NodeParameterRef, NodeNestedBlock, NodeStatement,
	} {
		if counts[want] == 0 {
			t.Fatalf("node %s not present; counts=%v", want, counts)
		}
	}
	if got.Root.Span.Start != 0 || got.Root.Span.End != len(validStructuralFixture) {
		t.Fatalf("root span = %+v, want [0,%d)", got.Root.Span, len(validStructuralFixture))
	}
}

func TestParseInvalidFixtureRecoversToLaterDeclarations(t *testing.T) {
	t.Parallel()

	src := "project p \"P\" {\n  description\n  @\n}\nquery q \"Q\" {}\n"
	got := Parse([]byte(src))
	assertParseInvariants(t, []byte(src), got)
	if len(got.Diagnostics) == 0 {
		t.Fatal("Diagnostics empty, want structure errors")
	}
	if got.Diagnostics[0].Code != "LDL1101" || got.Diagnostics[0].MessageKey != "invalid_structure_syntax" {
		t.Fatalf("first diagnostic = %+v", got.Diagnostics[0])
	}
	if ReconstructTokens(got.Tokens) != src {
		t.Fatal("invalid fixture did not round trip")
	}
	if decls := countNodes(got.Root)[NodeDeclaration]; decls != 2 {
		t.Fatalf("declarations parsed after recovery = %d, want 2", decls)
	}
}

func TestParseModuleDocAfterDeclarationDiagnostic(t *testing.T) {
	t.Parallel()

	src := "project p \"P\" {}\n//! late\n"
	got := Parse([]byte(src))
	assertParseInvariants(t, []byte(src), got)
	if len(got.Diagnostics) != 1 {
		t.Fatalf("Diagnostics = %+v, want one", got.Diagnostics)
	}
	diag := got.Diagnostics[0]
	if diag.Code != "LDL1101" || diag.MessageKey != "invalid_structure_syntax" || diag.Span.Start != 17 {
		t.Fatalf("Diagnostic = %+v, want late module-doc structure diagnostic", diag)
	}
}

func TestParseDocCommentItemBlockRestrictions(t *testing.T) {
	t.Parallel()

	positive := []string{
		"layers {\n  /// layer docs\n  application \"Application\" @0\n}\n",
		"entities application_service @application {\n  /// entity docs\n  order_api \"Order API\"\n}\n",
		"relations writes_to {\n  /// relation docs\n  r: a -> b\n}\n",
	}
	for _, src := range positive {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got := Parse([]byte(src))
			assertParseInvariants(t, []byte(src), got)
			if len(got.Diagnostics) != 0 {
				t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
			}
		})
	}

	negative := []struct {
		src string
		raw string
	}{
		{src: "rows owner [col] {\n  /// row docs\n  owner row: value\n}\n", raw: "/// row docs"},
		{src: "relation_rows owner [col] {\n  /// row docs\n  owner row: value\n}\n", raw: "/// row docs"},
		{src: "moves {\n  /// move docs\n  project old -> new\n}\n", raw: "/// move docs"},
	}
	for _, tt := range negative {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			got := Parse([]byte(tt.src))
			assertParseInvariants(t, []byte(tt.src), got)
			assertOneDiagnosticAtRaw(t, got.Diagnostics, tt.src, tt.raw)
		})
	}
}

func TestParseModuleDocInvalidInDelimitedContexts(t *testing.T) {
	t.Parallel()

	tests := []string{
		"query q \"Q\" {\n  values [\n    //! bad\n    prod,\n  ]\n}\n",
		"query q \"Q\" {\n  metadata { owner: \"platform\",\n    //! bad\n    env: prod,\n  }\n}\n",
		"rows owner [\n  //! bad\n  col,\n] {}\n",
		"import {\n  //! bad\n  subnet,\n} from \"aws.network\"\n",
		"export {\n  //! bad\n  subnet,\n} from \"./network.ldl\"\n",
		"query q \"Q\" {\n  //! bad\n  field value\n}\n",
	}
	for _, src := range tests {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got := Parse([]byte(src))
			assertParseInvariants(t, []byte(src), got)
			assertOneDiagnosticAtRaw(t, got.Diagnostics, src, "//! bad")
		})
	}
}

func TestParseModuleDocFirstObjectItemRecovery(t *testing.T) {
	t.Parallel()

	src := "query q \"Q\" {\n  metadata {\n    //! bad\n    owner: \"x\"\n  }\n}\n"
	got := Parse([]byte(src))
	assertParseInvariants(t, []byte(src), got)
	assertOneDiagnosticAtRaw(t, got.Diagnostics, src, "//! bad")
	counts := countNodes(got.Root)
	if counts[NodeObject] != 1 || counts[NodeNestedBlock] != 0 {
		t.Fatalf("counts = %v, want object value recovery without nested block misclassification", counts)
	}
}

func TestParseDocCommentStartsDeclarationPhase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		span Span
	}{
		{
			name: "module doc after doc comment",
			src:  "/// declaration docs\n//! module docs\nproject p \"P\" {}\n",
			span: Span{Start: 21, End: 36},
		},
		{
			name: "import after doc comment",
			src:  "/// declaration docs\nimport aws from \"aws\"\nproject p \"P\" {}\n",
			span: Span{Start: 21, End: 27},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Parse([]byte(tt.src))
			assertParseInvariants(t, []byte(tt.src), got)
			if len(got.Diagnostics) == 0 {
				t.Fatal("Diagnostics empty, want phase diagnostic")
			}
			if got.Diagnostics[0].Span != tt.span {
				t.Fatalf("first diagnostic = %+v, want span %+v", got.Diagnostics[0], tt.span)
			}
		})
	}
}

func TestParseImportExportRequirePhysicalLineEnd(t *testing.T) {
	t.Parallel()

	negative := []string{
		"import aws from \"aws\" import gcp from \"gcp\"\n",
		"import aws from \"aws\" project p \"P\" {}\n",
		"export { p } project p \"P\" {}\n",
		"export { p } from \"./p.ldl\" project p \"P\" {}\n",
	}
	for _, src := range negative {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got := Parse([]byte(src))
			assertParseInvariants(t, []byte(src), got)
			if len(got.Diagnostics) == 0 {
				t.Fatalf("Diagnostics empty for %q", src)
			}
		})
	}

	positive := []string{
		"import aws from \"aws\" // trailing\nproject p \"P\" {}\n",
		"export { p } // trailing\nproject p \"P\" {}\n",
		"export { p } from \"./p.ldl\" // trailing\nproject p \"P\" {}\n",
		"import aws from \"aws\"",
	}
	for _, src := range positive {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got := Parse([]byte(src))
			assertParseInvariants(t, []byte(src), got)
			if len(got.Diagnostics) != 0 {
				t.Fatalf("Diagnostics = %+v, want none for %q", got.Diagnostics, src)
			}
		})
	}
}

func TestParseObjectValueVersusNestedBlock(t *testing.T) {
	t.Parallel()

	src := "query q \"Q\" {\n  metadata { owner: \"platform\" }\n  shape diagram {\n    layout layered\n  }\n}\n"
	got := Parse([]byte(src))
	assertParseInvariants(t, []byte(src), got)
	if len(got.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
	}
	counts := countNodes(got.Root)
	if counts[NodeObject] != 1 {
		t.Fatalf("object nodes = %d, want 1", counts[NodeObject])
	}
	if counts[NodeNestedBlock] != 1 {
		t.Fatalf("nested block nodes = %d, want 1", counts[NodeNestedBlock])
	}
}

func TestParseGenericStatementListValues(t *testing.T) {
	t.Parallel()

	src := "query q \"Q\" {\n  empty []\n  strings [\"a\", \"b\"]\n  numbers [1, 2]\n  objects [{ owner: \"platform\" }]\n}\n"
	got := Parse([]byte(src))
	assertParseInvariants(t, []byte(src), got)
	if len(got.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
	}
	counts := countNodes(got.Root)
	if counts[NodeList] != 4 || counts[NodeColumnHeader] != 0 {
		t.Fatalf("counts = %v, want four NodeList values and no column headers", counts)
	}
}

func TestParseMultilineObjectValueVersusNestedBlock(t *testing.T) {
	t.Parallel()

	src := "query q \"Q\" {\n  metadata {\n    owner: \"platform\"\n  }\n  shape diagram {\n    layout layered\n  }\n}\n"
	got := Parse([]byte(src))
	assertParseInvariants(t, []byte(src), got)
	if len(got.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
	}
	counts := countNodes(got.Root)
	if counts[NodeObject] != 1 || counts[NodeNestedBlock] != 1 {
		t.Fatalf("counts = %v, want one object value and one nested block", counts)
	}
}

func TestParseEmptyBlockPrecedenceOverObjectAtStatementTail(t *testing.T) {
	t.Parallel()

	nested := "query q \"Q\" {\n  select {}\n  source query q {}\n}\n"
	got := Parse([]byte(nested))
	assertParseInvariants(t, []byte(nested), got)
	if len(got.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
	}
	counts := countNodes(got.Root)
	if counts[NodeNestedBlock] != 2 || counts[NodeObject] != 0 {
		t.Fatalf("counts = %v, want two nested empty blocks and no object values", counts)
	}

	unambiguousObjects := "query q \"Q\" {\n  list [{}]\n  metadata { nested: {} }\n  nested_block {\n    field value\n  }\n}\n"
	got = Parse([]byte(unambiguousObjects))
	assertParseInvariants(t, []byte(unambiguousObjects), got)
	if len(got.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
	}
	counts = countNodes(got.Root)
	if counts[NodeObject] != 3 || counts[NodeNestedBlock] != 1 {
		t.Fatalf("counts = %v, want three unambiguous objects and one true nested block", counts)
	}
}

func TestCSTReferencesConcreteSeparatorTokens(t *testing.T) {
	t.Parallel()

	src := "import { subnet as private_subnet, vpc } from \"aws.network\"\nrows order_api [aws.environment, critical,] {\n  order_api production: prod, true\n}\n"
	got := Parse([]byte(src))
	assertParseInvariants(t, []byte(src), got)
	if len(got.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
	}
	seen := map[TokenKind]int{}
	walkTokens(got.Root, func(tok Token) {
		seen[tok.Kind]++
	})
	for _, kind := range []TokenKind{TokenComma, TokenDot, TokenLBrace, TokenRBrace, TokenLBracket, TokenRBracket, TokenColon} {
		if seen[kind] == 0 {
			t.Fatalf("CST did not reference %s token; seen=%v", kind, seen)
		}
	}
}

func TestParseTopLevelRecoveryAndExpectedKeyword(t *testing.T) {
	t.Parallel()

	src := "@@@\nimport aws \"missing_from\"\nproject p \"P\" {}\n"
	got := Parse([]byte(src))
	assertParseInvariants(t, []byte(src), got)
	if len(got.Diagnostics) < 2 {
		t.Fatalf("Diagnostics = %+v, want recovery and expected keyword diagnostics", got.Diagnostics)
	}
	counts := countNodes(got.Root)
	if counts[NodeImportDecl] != 1 || counts[NodeDeclaration] != 1 {
		t.Fatalf("counts after top-level recovery = %v", counts)
	}
}

func TestParseEmptyCollectionsAndMissingRangeBound(t *testing.T) {
	t.Parallel()

	src := "query q \"Q\" {\n  empty_list []\n  empty_object { values: [] }\n  bad_range 0..\n}\n"
	got := Parse([]byte(src))
	assertParseInvariants(t, []byte(src), got)
	if len(got.Diagnostics) != 1 {
		t.Fatalf("Diagnostics = %+v, want missing range bound diagnostic only", got.Diagnostics)
	}
	counts := countNodes(got.Root)
	if counts[NodeList] != 2 || counts[NodeObject] != 1 || counts[NodeRange] != 1 {
		t.Fatalf("counts = %v, want list/object/range", counts)
	}
}

func TestParseUnknownDeclarationAndEOFExpectation(t *testing.T) {
	t.Parallel()

	src := "nonsense\nproject"
	got := Parse([]byte(src))
	assertParseInvariants(t, []byte(src), got)
	if len(got.Diagnostics) < 2 {
		t.Fatalf("Diagnostics = %+v, want unknown declaration and EOF expectation errors", got.Diagnostics)
	}
	Walk(nil, func(*Node) {
		t.Fatal("Walk(nil) should not visit")
	})
	p := parser{tokens: []Token{{Kind: TokenEOF}}}
	_ = p.consume()
	_ = p.consume()
}

func TestParseObjectInvalidKeyAndStatementInvalidArgument(t *testing.T) {
	t.Parallel()

	src := "query q \"Q\" {\n  metadata { 1: true }\n  compare id < value\n  broken !\n}\n"
	got := Parse([]byte(src))
	assertParseInvariants(t, []byte(src), got)
	if len(got.Diagnostics) == 0 {
		t.Fatal("Diagnostics empty, want invalid object key/statement argument")
	}
	counts := countNodes(got.Root)
	if counts[NodeError] == 0 {
		t.Fatalf("counts = %v, want error node", counts)
	}
}

func TestParseEBNFNegativeMatrix(t *testing.T) {
	t.Parallel()

	tests := []string{
		"project p \"P\" {}\nimport late from \"x\"\n",
		"import {} from \"x\"\n",
		"export {} from \"x\"\n",
		"rows owner [] {}\n",
		"rows owner [col] {\n  owner row:\n}\n",
		"rows owner [col] {\n  owner row: value,\n}\n",
		"query q \"Q\" {\n  bad _\n}\n",
		"query q \"Q\" { statement \"compact\" }\n",
		"query q \"Q\" {\n  list [a,]\n  object { owner: \"platform\", }\n}\n",
		"query q \"Q\" {\n  //! nested module doc\n}\n",
	}
	for _, src := range tests {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got := Parse([]byte(src))
			assertParseInvariants(t, []byte(src), got)
			if len(got.Diagnostics) == 0 {
				t.Fatalf("Diagnostics empty for malformed source %q", src)
			}
		})
	}
}

func TestParseEBNFPositiveMatrix(t *testing.T) {
	t.Parallel()

	tests := []string{
		"import aws from \"aws_complete\"\n",
		"import {\n  subnet as private_subnet,\n  vpc,\n} from \"aws_complete.network\"\n",
		"import {\n  // ordinary comment\n  subnet,\n  vpc,\n} from \"aws_complete.network\"\n",
		"export { subnet as network_segment } from \"./network.ldl\"\n",
		"export {\n  // ordinary comment\n  subnet,\n} from \"./network.ldl\"\n",
		"layers {}\n",
		"moves {\n  project old_project -> new_project\n  entity_row order_api old_snapshot -> production\n}\n",
		"moves {\n  project old_project -> new_project\n  entity_type old_entity_type -> new_entity_type\n  relation_type old_relation_type -> new_relation_type\n  layer old_layer -> new_layer\n  entity old_entity -> new_entity\n  relation old_relation -> new_relation\n  query old_query -> new_query\n  view old_view -> new_view\n  reference old_reference -> new_reference\n  entity_type_column owner old_column -> new_column\n  entity_type_constraint owner old_constraint -> new_constraint\n  relation_type_column owner old_column -> new_column\n  relation_type_constraint owner old_constraint -> new_constraint\n  entity_row owner old_row -> new_row\n  relation_row owner old_row -> new_row\n  query_parameter owner old_parameter -> new_parameter\n  view_table_column owner old_column -> new_column\n  view_export owner old_export -> new_export\n}\n",
		"rows owner [\n  aws.environment,\n  critical,\n] {\n  owner production: prod, true\n}\n",
		"query q \"Q\" {\n  values [\n    prod,\n  ]\n  metadata { owner: \"platform\",\n  }\n}\n",
		"reference r <<-TEXT\nbody\nTEXT\nquery q \"Q\" {}\n",
	}
	for _, src := range tests {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got := Parse([]byte(src))
			assertParseInvariants(t, []byte(src), got)
			if len(got.Diagnostics) != 0 {
				t.Fatalf("Diagnostics = %+v, want none for %q", got.Diagnostics, src)
			}
		})
	}
}

func TestParseMoveItemKindAndArityMatrix(t *testing.T) {
	t.Parallel()

	valid := "moves {\n  project old_project -> new_project\n  entity_type old_entity_type -> new_entity_type\n  relation_type old_relation_type -> new_relation_type\n  layer old_layer -> new_layer\n  entity old_entity -> new_entity\n  relation old_relation -> new_relation\n  query old_query -> new_query\n  view old_view -> new_view\n  reference old_reference -> new_reference\n  entity_type_column owner old_column -> new_column\n  entity_type_constraint owner old_constraint -> new_constraint\n  relation_type_column owner old_column -> new_column\n  relation_type_constraint owner old_constraint -> new_constraint\n  entity_row owner old_row -> new_row\n  relation_row owner old_row -> new_row\n  query_parameter owner old_parameter -> new_parameter\n  view_table_column owner old_column -> new_column\n  view_export owner old_export -> new_export\n}\n"
	got := Parse([]byte(valid))
	assertParseInvariants(t, []byte(valid), got)
	if len(got.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
	}
	if moves := countNodes(got.Root)[NodeMoveItem]; moves != 18 {
		t.Fatalf("move items = %d, want 18", moves)
	}

	tests := []struct {
		name string
		src  string
		raw  string
	}{
		{name: "unknown kind", src: "moves {\n  unknown_kind old -> new\n}\n", raw: "unknown_kind"},
		{name: "top kind child arity", src: "moves {\n  project owner old -> new\n}\n", raw: "old"},
		{name: "child kind top arity", src: "moves {\n  entity_row old -> new\n}\n", raw: "->"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Parse([]byte(tt.src))
			assertParseInvariants(t, []byte(tt.src), got)
			assertOneDiagnosticAtRaw(t, got.Diagnostics, tt.src, tt.raw)
		})
	}
}

func TestParseBOMLineEndingAndByteOffsetMatrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		src          []byte
		wantDiag     bool
		wantDiagSpan Span
	}{
		{name: "bom only", src: []byte{0xEF, 0xBB, 0xBF}},
		{name: "mid file bom", src: []byte("project p \"P\" {}\n\xef\xbb\xbf\n"), wantDiag: true, wantDiagSpan: Span{Start: 17, End: 20}},
		{name: "bare cr", src: []byte("project p \"P\" {}\rquery q \"Q\" {}\r")},
		{name: "crlf", src: []byte("project p \"P\" {}\r\nquery q \"Q\" {}\r\n")},
		{name: "multibyte before invalid string", src: []byte("project p \"日本語"), wantDiag: true, wantDiagSpan: Span{Start: 10, End: 20}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Parse(tt.src)
			assertParseInvariants(t, tt.src, got)
			if tt.wantDiag {
				found := false
				for _, diag := range got.Diagnostics {
					if diag.Span == tt.wantDiagSpan {
						found = true
					}
				}
				if !found {
					t.Fatalf("Diagnostics = %+v, want span %+v", got.Diagnostics, tt.wantDiagSpan)
				}
			}
		})
	}
}

func TestParseDiagnosticsDeterministicAcrossRuns(t *testing.T) {
	t.Parallel()

	src := []byte("project p \"P\" {\n  @\n  bad _\n}\n//! late\n")
	first := Parse(src)
	assertParseInvariants(t, src, first)
	for range 10 {
		next := Parse(src)
		assertParseInvariants(t, src, next)
		if len(next.Diagnostics) != len(first.Diagnostics) {
			t.Fatalf("diagnostic count changed: first=%+v next=%+v", first.Diagnostics, next.Diagnostics)
		}
		for i := range first.Diagnostics {
			if next.Diagnostics[i] != first.Diagnostics[i] {
				t.Fatalf("diagnostic %d changed:\nfirst=%+v\n next=%+v", i, first.Diagnostics[i], next.Diagnostics[i])
			}
		}
	}
}

func TestParseLineTerminatorBranches(t *testing.T) {
	t.Parallel()

	tests := []string{
		"query q \"Q\" {\n  description \"ok\" // trailing\n}\n",
		"query q \"Q\" {\n  description \"ok\"}",
		"query q \"Q\" {\n  description \"ok\"",
		"query q \"Q\" {\n  compact \"no\" extra\n}\n",
	}
	for _, src := range tests {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got := Parse([]byte(src))
			assertParseInvariants(t, []byte(src), got)
		})
	}
}

func TestParseListAndObjectBranchMatrix(t *testing.T) {
	t.Parallel()

	tests := []string{
		"query q \"Q\" {\n  metadata { owner: [], }\n}\n",
		"query q \"Q\" {\n  metadata { owner: [\n  ], }\n}\n",
		"query q \"Q\" {\n  metadata { owner: { nested: [] } }\n}\n",
	}
	for _, src := range tests {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got := Parse([]byte(src))
			assertParseInvariants(t, []byte(src), got)
		})
	}
}

func FuzzParseDoesNotPanic(f *testing.F) {
	for _, seed := range []string{
		validStructuralFixture,
		"project p \"P\" {\n  broken [\n}\n",
		"relations writes_to {\n  r: a -> b {}\n}\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		got := Parse([]byte(src))
		if got.Root == nil {
			t.Fatal("nil root")
		}
		assertParseInvariants(t, []byte(src), got)
	})
}

func FuzzParseBytesCSTInvariants(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(validStructuralFixture),
		[]byte{0xff, '@', '\n'},
		[]byte("\xef\xbb\xbf"),
		[]byte("project p \"P\" {\r  bad _\r}\r"),
		[]byte("reference r <<-TEXT\n\xff\nTEXT\n"),
		[]byte("query q \"Q\" {\n  list [a,\n  ]\n}\n"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		got := Parse(src)
		assertParseInvariants(t, src, got)
	})
}

func countNodes(root *Node) map[NodeKind]int {
	counts := map[NodeKind]int{}
	Walk(root, func(n *Node) {
		counts[n.Kind]++
	})
	return counts
}

func walkTokens(root *Node, visit func(Token)) {
	for _, child := range root.Children {
		switch v := child.(type) {
		case TokenElement:
			visit(v.Token)
		case *Node:
			walkTokens(v, visit)
		}
	}
}

func assertParseInvariants(t *testing.T, src []byte, got ParseResult) {
	t.Helper()
	if got.Root == nil {
		t.Fatal("nil root")
	}
	if got.Root.Span != (Span{Start: 0, End: len(src)}) {
		t.Fatalf("root span = %+v, want [0,%d)", got.Root.Span, len(src))
	}
	if reconstructed := ReconstructTokens(got.Tokens); reconstructed != string(src) {
		t.Fatalf("token reconstruction mismatch\n got: %q\nwant: %q", reconstructed, string(src))
	}
	owners := make([]int, len(got.Tokens))
	var ordered []TokenElement
	var walk func(*Node)
	walk = func(n *Node) {
		for _, child := range n.Children {
			switch v := child.(type) {
			case TokenElement:
				if v.Index < 0 || v.Index >= len(got.Tokens) {
					t.Fatalf("token index out of bounds: %d len=%d", v.Index, len(got.Tokens))
				}
				owners[v.Index]++
				ordered = append(ordered, v)
				if !reflect.DeepEqual(v.Token, got.Tokens[v.Index]) {
					t.Fatalf("token element mismatch at %d: %+v != %+v", v.Index, v.Token, got.Tokens[v.Index])
				}
			case *Node:
				if v.Span.Start < 0 || v.Span.End < v.Span.Start || v.Span.End > len(src) {
					t.Fatalf("node %s has invalid span %+v for source len %d", v.Kind, v.Span, len(src))
				}
				walk(v)
			default:
				t.Fatalf("unknown CST child %T", child)
			}
		}
	}
	walk(got.Root)
	for i, count := range owners {
		if count != 1 {
			t.Fatalf("token %d %s %q owned %d times; owners=%v", i, got.Tokens[i].Kind, got.Tokens[i].Raw, count, owners)
		}
	}
	for i := 1; i < len(ordered); i++ {
		if ordered[i-1].Index >= ordered[i].Index {
			t.Fatalf("CST token order is not increasing around %d then %d", ordered[i-1].Index, ordered[i].Index)
		}
	}
	for _, diag := range got.Diagnostics {
		if diag.Span.Start < 0 || diag.Span.End < diag.Span.Start || diag.Span.End > len(src) {
			t.Fatalf("diagnostic out of bounds: %+v for len %d", diag, len(src))
		}
		if diag.Code != "LDL1001" && diag.Code != "LDL1101" {
			t.Fatalf("unexpected diagnostic identity: %+v", diag)
		}
	}
}

func assertOneDiagnosticAtRaw(t *testing.T, diagnostics []Diagnostic, src string, raw string) {
	t.Helper()
	start := strings.Index(src, raw)
	if start < 0 {
		t.Fatalf("raw marker %q not found in source", raw)
	}
	want := Span{Start: start, End: start + len(raw)}
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics at %q = %+v, want exactly one", raw, diagnostics)
	}
	diag := diagnostics[0]
	if diag.Span != want || diag.Code != "LDL1101" || diag.MessageKey != "invalid_structure_syntax" {
		t.Fatalf("diagnostic at %q = %+v, want LDL1101 invalid_structure_syntax at %+v", raw, diag, want)
	}
}
