// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package query

import (
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestCorruptStageArtifactsFailTransactionally(t *testing.T) {
	base := projectInput(t, map[string]string{"document.ldl": minimalSchema + "query q \"Q\" {\n  select {}\n}\n"})

	t.Run("definition diagnostic", func(t *testing.T) {
		input := base
		input.Definition.Diagnostics = []resolve.Diagnostic{{Code: "LDL1401", Severity: "error", MessageKey: "invalid_scalar_or_constraint", Arguments: map[string]string{}}}
		input.Definition.HasErrors = true
		got := Compile(input)
		if !got.HasErrors || got.Recipes != nil || !diagnosticCode(got, "LDL1401") {
			t.Fatalf("definition failure leaked output: %+v", got)
		}
	})

	t.Run("missing declaration source", func(t *testing.T) {
		input := base
		input.Resolve.DeclarationSources = withoutSourceKind(input.Resolve.DeclarationSources, resolve.KindQuery)
		got := Compile(input)
		if !got.HasErrors || got.Recipes != nil || !diagnosticCode(got, "LDL1101") {
			t.Fatalf("missing Query CST was accepted: %+v", got)
		}
	})

	t.Run("invalid identity token", func(t *testing.T) {
		input := base
		for index := range input.Resolve.DeclarationSources {
			if input.Resolve.DeclarationSources[index].Kind != resolve.KindQuery {
				continue
			}
			source := input.Resolve.DeclarationSources[index]
			node := *source.Node
			node.Children = append([]syntax.Element{}, source.Node.Children...)
			token := node.Children[2].(syntax.TokenElement)
			token.Token.Raw = `"`
			node.Children[2] = token
			source.Node = &node
			input.Resolve.DeclarationSources[index] = source
		}
		got := Compile(input)
		if !got.HasErrors || got.Recipes != nil || !diagnosticCode(got, "LDL1601") {
			t.Fatalf("invalid Query display name was accepted: %+v", got)
		}
	})
}

func TestResolverBindingAndCSTDefenses(t *testing.T) {
	base := projectInput(t, map[string]string{"document.ldl": minimalSchema + "query q \"Q\" {\n  select {}\n}\n"})
	c := newCompiler(base)
	queryAddress := "ldl:project:p:query:q"
	span := syntax.Span{Start: 10, End: 11}
	c.bindings[queryAddress] = []resolve.SourceBinding{
		{SourceAddress: queryAddress, ExpectedKind: resolve.KindLayer, Range: span, TargetAddress: "z"},
		{SourceAddress: queryAddress, ExpectedKind: resolve.KindLayer, Range: span, TargetAddress: "a"},
	}
	if _, ok := c.singleBindingAt(queryAddress, resolve.KindLayer, span, resolve.DeclarationSource{}); ok {
		t.Fatal("ambiguous resolver binding was accepted")
	}
	addresses := []string{"z", "a"}
	c.sortAddresses(addresses)
	if addresses[0] != "a" || addresses[1] != "z" {
		t.Fatalf("fallback address order = %v", addresses)
	}

	packRange := sourceRange(resolve.DeclarationSource{Module: resolve.ModuleKey{
		Origin: resolve.Origin{Kind: resolve.OriginPack, Publisher: "acme", PackName: "base"},
		Path:   "schema.ldl",
	}}, span)
	if packRange == nil || packRange.Origin.PackAddress == "" {
		t.Fatalf("pack source range = %+v", packRange)
	}

	if nodeChildren(nil) != nil || directTokens(nil) != nil || nodeTokens(nil) != nil || listItems(authoredValue{}) != nil {
		t.Fatal("nil CST helpers did not preserve absence")
	}
	if _, ok := authoredString(authoredValue{kind: syntax.TokenIdentifier, raw: "x"}); ok {
		t.Fatal("identifier accepted as authored string")
	}
	if _, ok := authoredString(authoredValue{kind: syntax.TokenString, raw: `"`}); ok {
		t.Fatal("malformed authored string accepted")
	}
	emptyStatement := &syntax.Node{Kind: syntax.NodeStatement}
	block := &syntax.Node{Kind: syntax.NodeBlock, Children: []syntax.Element{emptyStatement}}
	if members := readMembers(block); len(members) != 0 {
		t.Fatalf("tokenless statement became a member: %+v", members)
	}

	before := len(c.diagnostics)
	c.compilePredicateMember(resolve.DeclarationSource{}, authoredMember{head: "future_predicate"}, resolve.KindEntity, queryAddress)
	c.compileStatePredicate(resolve.DeclarationSource{}, authoredMember{head: "state"}, StateSubjectEntity, queryAddress)
	if len(c.diagnostics) != before+2 {
		t.Fatalf("corrupt predicate defenses emitted %d diagnostics, want 2", len(c.diagnostics)-before)
	}
}

func withoutSourceKind(sources []resolve.DeclarationSource, kind resolve.SubjectKind) []resolve.DeclarationSource {
	out := make([]resolve.DeclarationSource, 0, len(sources))
	for _, source := range sources {
		if source.Kind != kind {
			out = append(out, source)
		}
	}
	return out
}
