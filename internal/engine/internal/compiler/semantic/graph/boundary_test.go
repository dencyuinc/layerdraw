// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package graph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestMalformedResolveDefinitionEntityBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Input, string)
		code   string
	}{
		{name: "missing source", code: "LDL1101", mutate: func(input *Input, address string) { removeSource(input, address) }},
		{name: "invalid identity", code: "LDL1101", mutate: func(input *Input, address string) {
			replaceSourceNode(input, address, &syntax.Node{Kind: syntax.NodeEntityItem, Children: []syntax.Element{
				syntax.TokenElement{Token: syntax.Token{Kind: syntax.TokenIdentifier, Raw: "wrong"}},
			}})
		}},
		{name: "missing type binding", code: "LDL1301", mutate: func(input *Input, address string) { removeBindings(input, address, resolve.KindEntityType) }},
		{name: "ambiguous layer binding", code: "LDL1301", mutate: func(input *Input, address string) {
			for _, binding := range input.Resolve.Bindings {
				if binding.SourceAddress == address && binding.ExpectedKind == resolve.KindLayer {
					input.Resolve.Bindings = append(input.Resolve.Bindings, binding)
					return
				}
			}
		}},
		{name: "missing typed entity type", code: "LDL1301", mutate: func(input *Input, _ string) { input.Definition.EntityTypes = nil }},
		{name: "missing typed layer", code: "LDL1301", mutate: func(input *Input, _ string) { input.Definition.Layers = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := inputFiles(t, map[string]string{"document.ldl": deterministicDocument(false)})
			address := firstAddress(input.Resolve.Declarations, resolve.KindEntity)
			tt.mutate(&input, address)
			requireFailureCode(t, Compile(input), tt.code)
		})
	}
}

func TestMalformedResolveDefinitionRelationBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Input, string)
		code   string
	}{
		{name: "missing source", code: "LDL1101", mutate: func(input *Input, address string) { removeSource(input, address) }},
		{name: "invalid identity", code: "LDL1101", mutate: func(input *Input, address string) {
			replaceSourceNode(input, address, &syntax.Node{Kind: syntax.NodeRelationItem, Children: []syntax.Element{
				syntax.TokenElement{Token: syntax.Token{Kind: syntax.TokenIdentifier, Raw: "wrong"}},
			}})
		}},
		{name: "missing type binding", code: "LDL1301", mutate: func(input *Input, address string) { removeBindings(input, address, resolve.KindRelationType) }},
		{name: "missing typed relation type", code: "LDL1301", mutate: func(input *Input, _ string) { input.Definition.RelationTypes = nil }},
		{name: "missing ordered endpoint", code: "LDL1501", mutate: func(input *Input, address string) {
			replaceSourceNode(input, address, &syntax.Node{Kind: syntax.NodeRelationItem, Children: []syntax.Element{
				syntax.TokenElement{Token: syntax.Token{Kind: syntax.TokenIdentifier, Raw: "first"}},
				syntax.TokenElement{Token: syntax.Token{Kind: syntax.TokenColon, Raw: ":"}},
				syntax.TokenElement{Token: syntax.Token{Kind: syntax.TokenArrow, Raw: "->"}},
			}})
		}},
		{name: "missing endpoint binding", code: "LDL1301", mutate: func(input *Input, address string) {
			removed := false
			bindings := input.Resolve.Bindings[:0]
			for _, binding := range input.Resolve.Bindings {
				if !removed && binding.SourceAddress == address && binding.ExpectedKind == resolve.KindEntity {
					removed = true
					continue
				}
				bindings = append(bindings, binding)
			}
			input.Resolve.Bindings = bindings
		}},
		{name: "ambiguous endpoint binding", code: "LDL1301", mutate: func(input *Input, address string) {
			for _, binding := range input.Resolve.Bindings {
				if binding.SourceAddress == address && binding.ExpectedKind == resolve.KindEntity {
					input.Resolve.Bindings = append(input.Resolve.Bindings, binding)
					return
				}
			}
		}},
		{name: "endpoint entity not compiled", code: "LDL1501", mutate: func(input *Input, _ string) {
			declarations := input.Resolve.Declarations[:0]
			for _, decl := range input.Resolve.Declarations {
				if decl.Kind != resolve.KindEntity {
					declarations = append(declarations, decl)
				}
			}
			input.Resolve.Declarations = declarations
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := inputFiles(t, map[string]string{"document.ldl": deterministicDocument(false)})
			address := firstAddress(input.Resolve.Declarations, resolve.KindRelation)
			tt.mutate(&input, address)
			requireFailureCode(t, Compile(input), tt.code)
		})
	}
}

func TestMalformedRowBoundaryBranches(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "valid_graph.ldl"))
	if err != nil {
		t.Fatal(err)
	}
	newCompilerWithFacts := func(t *testing.T) *compiler {
		t.Helper()
		c := newCompiler(inputFiles(t, map[string]string{"document.ldl": string(source)}))
		c.compileEntities()
		c.compileRelations()
		return c
	}
	rowDecl := func(c *compiler, ownerKind resolve.SubjectKind) *resolve.DeclarationSymbol {
		for i := range c.declarations {
			decl := &c.declarations[i]
			if decl.Kind == resolve.KindRow && decl.Owner != nil && decl.Owner.Path[len(decl.Owner.Path)-1].Kind == ownerKind {
				return decl
			}
		}
		return nil
	}

	t.Run("missing source or owner", func(t *testing.T) {
		c := newCompilerWithFacts(t)
		decl := rowDecl(c, resolve.KindEntity)
		delete(c.sources, decl.Address)
		c.compileRows()
		if !hasCompilerCode(c, "LDL1402") {
			t.Fatalf("diagnostics = %+v", c.diagnostics)
		}
	})
	t.Run("missing header", func(t *testing.T) {
		c := newCompilerWithFacts(t)
		c.rowGroups = map[sourceKey]*factGroup{}
		c.compileRows()
		if !hasCompilerCode(c, "LDL1402") {
			t.Fatalf("diagnostics = %+v", c.diagnostics)
		}
	})
	t.Run("identity mismatch", func(t *testing.T) {
		c := newCompilerWithFacts(t)
		decl := rowDecl(c, resolve.KindEntity)
		src := c.sources[decl.Address]
		src.Node = &syntax.Node{Kind: syntax.NodeRowItem, Children: []syntax.Element{
			syntax.TokenElement{Token: syntax.Token{Kind: syntax.TokenIdentifier, Raw: "alpha"}},
			syntax.TokenElement{Token: syntax.Token{Kind: syntax.TokenIdentifier, Raw: "wrong"}},
		}}
		c.sources[decl.Address] = src
		c.compileRows()
		if !hasCompilerCode(c, "LDL1402") {
			t.Fatalf("diagnostics = %+v", c.diagnostics)
		}
	})
	t.Run("entity owner absent", func(t *testing.T) {
		c := newCompilerWithFacts(t)
		decl := rowDecl(c, resolve.KindEntity)
		delete(c.entityIndex, resolve.StableAddress(*decl.Owner))
		c.compileRows()
		if !hasCompilerCode(c, "LDL1402") {
			t.Fatalf("diagnostics = %+v", c.diagnostics)
		}
	})
	t.Run("relation owner absent", func(t *testing.T) {
		c := newCompilerWithFacts(t)
		decl := rowDecl(c, resolve.KindRelation)
		delete(c.relationIndex, resolve.StableAddress(*decl.Owner))
		c.compileRows()
		if !hasCompilerCode(c, "LDL1402") {
			t.Fatalf("diagnostics = %+v", c.diagnostics)
		}
	})
	t.Run("cross module augmentation", func(t *testing.T) {
		c := newCompilerWithFacts(t)
		decl := rowDecl(c, resolve.KindEntity)
		src := c.sources[decl.Address]
		oldKey := sourceKey{module: src.Module, span: src.Range}
		group := c.rowGroups[oldKey]
		delete(c.rowGroups, oldKey)
		src.Module.Path = "other.ldl"
		c.sources[decl.Address] = src
		c.rowGroups[sourceKey{module: src.Module, span: src.Range}] = group
		c.compileRows()
		if !hasCompilerCode(c, "LDL1402") {
			t.Fatalf("diagnostics = %+v", c.diagnostics)
		}
	})
	t.Run("invalid owner kind", func(t *testing.T) {
		c := newCompilerWithFacts(t)
		decl := rowDecl(c, resolve.KindEntity)
		owner := *decl.Owner
		owner.Path = append([]resolve.SymbolSegment{}, owner.Path...)
		owner.Path[len(owner.Path)-1].Kind = resolve.KindLayer
		decl.Owner = &owner
		c.compileRows()
		if !hasCompilerCode(c, "LDL1402") {
			t.Fatalf("diagnostics = %+v", c.diagnostics)
		}
	})
}

func TestUtilityAndTypedUniqueIdentityBranches(t *testing.T) {
	if sourceRange(resolve.DeclarationSource{}, syntax.Span{}) != nil {
		t.Fatal("source-less range should be absent")
	}
	packRange := sourceRange(resolve.DeclarationSource{Module: resolve.ModuleKey{Origin: resolve.Origin{Kind: resolve.OriginPack, Publisher: "pub", PackName: "pack"}, Path: "facts.ldl"}}, syntax.Span{Start: 2, End: 3})
	if packRange == nil || packRange.Origin.PackAddress != "ldl:pack:pub:pack" {
		t.Fatalf("pack range = %+v", packRange)
	}
	if nodeChildren(nil) != nil || directTokens(nil) != nil || nodeTokens(nil) != nil || firstNode(&syntax.Node{}, syntax.NodeCells) != nil {
		t.Fatal("nil/empty CST helper contract changed")
	}
	if got := normalizedStringToken(syntax.Token{Kind: syntax.TokenIdentifier, Raw: "e\u0301"}); got != "é" {
		t.Fatalf("normalized atom = %q", got)
	}
	if got := normalizedStringToken(syntax.Token{Kind: syntax.TokenString, Raw: `"unterminated`}); got != `"unterminated` {
		t.Fatalf("invalid quoted fallback = %q", got)
	}
	if rowCells(&syntax.Node{}) != nil {
		t.Fatal("missing cells should return none")
	}

	identities := map[string]bool{}
	for _, scalar := range []definition.Scalar{
		{Type: definition.ScalarString, String: "x"},
		{Type: definition.ScalarInteger, Int: 1},
		{Type: definition.ScalarNumber, Float: 1},
		{Type: definition.ScalarBoolean, Bool: true},
	} {
		identities[scalarIdentity(scalar)] = true
	}
	if len(identities) != 4 {
		t.Fatalf("scalar identities collided: %+v", identities)
	}
	c := &compiler{relationTypes: map[string]definition.RelationType{}}
	if c.duplicateConflict("missing", "also-missing") {
		t.Fatal("missing relation types conflict")
	}
}

func firstAddress(declarations []resolve.DeclarationSymbol, kind resolve.SubjectKind) string {
	for _, decl := range declarations {
		if decl.Kind == kind {
			return decl.Address
		}
	}
	return ""
}

func removeSource(input *Input, address string) {
	out := input.Resolve.DeclarationSources[:0]
	for _, src := range input.Resolve.DeclarationSources {
		if src.Address != address {
			out = append(out, src)
		}
	}
	input.Resolve.DeclarationSources = out
}

func replaceSourceNode(input *Input, address string, node *syntax.Node) {
	for i := range input.Resolve.DeclarationSources {
		if input.Resolve.DeclarationSources[i].Address == address {
			node.Span = input.Resolve.DeclarationSources[i].Range
			input.Resolve.DeclarationSources[i].Node = node
			return
		}
	}
}

func removeBindings(input *Input, address string, kind resolve.SubjectKind) {
	out := input.Resolve.Bindings[:0]
	for _, binding := range input.Resolve.Bindings {
		if binding.SourceAddress != address || binding.ExpectedKind != kind {
			out = append(out, binding)
		}
	}
	input.Resolve.Bindings = out
}

func hasCompilerCode(c *compiler, code string) bool {
	for _, diagnostic := range c.diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
