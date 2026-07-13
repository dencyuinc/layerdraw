// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package syntax

import "testing"

const validStructuralFixture = `//! LayerDraw module docs
import aws from "aws_complete"
import { subnet as private_subnet, vpc } from "aws_complete.network"

project order_platform "Order Platform" {
  description "Production order platform"
  tags [prod, stg]
  annotations { owner: "platform", critical: true }
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
	if len(got.Diagnostics) != 1 {
		t.Fatalf("Diagnostics = %+v, want one", got.Diagnostics)
	}
	diag := got.Diagnostics[0]
	if diag.Code != "LDL1101" || diag.MessageKey != "invalid_structure_syntax" || diag.Span.Start != 17 {
		t.Fatalf("Diagnostic = %+v, want late module-doc structure diagnostic", diag)
	}
}

func TestParseObjectValueVersusNestedBlock(t *testing.T) {
	t.Parallel()

	src := "query q \"Q\" {\n  metadata { owner: \"platform\" }\n  shape diagram {\n    layout layered\n  }\n}\n"
	got := Parse([]byte(src))
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

func TestCSTReferencesConcreteSeparatorTokens(t *testing.T) {
	t.Parallel()

	src := "import { subnet as private_subnet, vpc } from \"aws.network\"\nrows order_api [aws.environment, critical,] {\n  order_api production: prod, true,\n}\n"
	got := Parse([]byte(src))
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

	src := "query q \"Q\" {\n  empty_list []\n  empty_object {}\n  bad_range 0..\n}\n"
	got := Parse([]byte(src))
	if len(got.Diagnostics) != 1 {
		t.Fatalf("Diagnostics = %+v, want one missing range bound diagnostic", got.Diagnostics)
	}
	counts := countNodes(got.Root)
	if counts[NodeList] != 1 || counts[NodeObject] != 1 || counts[NodeRange] != 1 {
		t.Fatalf("counts = %v, want list/object/range", counts)
	}
}

func TestParseUnknownDeclarationAndEOFExpectation(t *testing.T) {
	t.Parallel()

	src := "nonsense\nproject"
	got := Parse([]byte(src))
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
	if len(got.Diagnostics) == 0 {
		t.Fatal("Diagnostics empty, want invalid object key/statement argument")
	}
	counts := countNodes(got.Root)
	if counts[NodeError] == 0 {
		t.Fatalf("counts = %v, want error node", counts)
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
		if ReconstructTokens(got.Tokens) != src {
			t.Fatal("parse did not preserve source")
		}
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
