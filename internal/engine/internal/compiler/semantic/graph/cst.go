// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package graph

import (
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

type sourceKey struct {
	module resolve.ModuleKey
	span   syntax.Span
}

type factGroup struct {
	kind        string
	module      resolve.ModuleKey
	span        syntax.Span
	refs        []groupRef
	header      []headerColumn
	typeAddress string
}

type groupRef struct {
	kind resolve.SubjectKind
	span syntax.Span
}

type headerColumn struct {
	id   string
	span syntax.Span
}

type authoredCell struct {
	absent bool
	raw    string
	kind   syntax.TokenKind
	span   syntax.Span
}

func inspectFactGroups(modules []resolve.ResolvedModule) ([]*factGroup, map[sourceKey]*factGroup) {
	var groups []*factGroup
	rows := map[sourceKey]*factGroup{}
	for _, module := range modules {
		for _, decl := range nodeChildren(module.File.Root) {
			if decl.Kind != syntax.NodeDeclaration {
				continue
			}
			toks := directTokens(decl)
			if len(toks) == 0 || toks[0].Raw != "entities" && toks[0].Raw != "relations" && toks[0].Raw != "rows" && toks[0].Raw != "relation_rows" {
				continue
			}
			group := &factGroup{
				kind:   toks[0].Raw,
				module: resolve.ModuleKey{Origin: module.Origin, Path: module.Path},
				span:   decl.Span,
				header: readHeader(firstNode(decl, syntax.NodeColumnHeader)),
			}
			refs := directSymbolRefs(decl)
			switch group.kind {
			case "entities":
				if len(refs) > 0 {
					group.refs = append(group.refs, groupRef{kind: resolve.KindEntityType, span: refs[0].Span})
				}
				if len(refs) > 1 {
					group.refs = append(group.refs, groupRef{kind: resolve.KindLayer, span: refs[1].Span})
				}
			case "relations":
				if len(refs) > 0 {
					group.refs = append(group.refs, groupRef{kind: resolve.KindRelationType, span: refs[0].Span})
				}
			case "rows":
				if len(refs) > 0 {
					group.refs = append(group.refs, groupRef{kind: resolve.KindEntityType, span: refs[0].Span})
				}
			case "relation_rows":
				if len(refs) > 0 {
					group.refs = append(group.refs, groupRef{kind: resolve.KindRelationType, span: refs[0].Span})
				}
			}
			groups = append(groups, group)
			block := firstNode(decl, syntax.NodeItemBlock)
			for _, item := range nodeChildren(block) {
				if item.Kind == syntax.NodeRowItem {
					rows[sourceKey{module: group.module, span: item.Span}] = group
				}
			}
		}
	}
	return groups, rows
}

func readHeader(n *syntax.Node) []headerColumn {
	var out []headerColumn
	for _, ref := range nodeChildren(n) {
		if ref.Kind != syntax.NodeSymbolRef {
			continue
		}
		out = append(out, headerColumn{id: qualifiedText(ref), span: ref.Span})
	}
	return out
}

func relationEndpointRefs(n *syntax.Node) []*syntax.Node {
	var out []*syntax.Node
	for _, child := range nodeChildren(n) {
		if child.Kind == syntax.NodeSymbolRef {
			out = append(out, child)
		}
	}
	return out
}

func directSymbolRefs(n *syntax.Node) []*syntax.Node {
	var out []*syntax.Node
	for _, child := range nodeChildren(n) {
		if child.Kind == syntax.NodeSymbolRef {
			out = append(out, child)
		}
	}
	return out
}

func rowCells(n *syntax.Node) []authoredCell {
	cells := firstNode(n, syntax.NodeCells)
	if cells == nil {
		return nil
	}
	var out []authoredCell
	for _, child := range cells.Children {
		switch value := child.(type) {
		case syntax.TokenElement:
			if value.Token.Kind == syntax.TokenUnderscore {
				out = append(out, authoredCell{absent: true, span: value.Token.Span})
			}
		case *syntax.Node:
			if value.Kind != syntax.NodeValue {
				continue
			}
			toks := nodeTokens(value)
			if len(toks) == 0 {
				continue
			}
			var raw strings.Builder
			for _, tok := range toks {
				raw.WriteString(tok.Raw)
			}
			out = append(out, authoredCell{raw: raw.String(), kind: toks[0].Kind, span: value.Span})
		}
	}
	return out
}

func normalizedStringToken(tok syntax.Token) string {
	if tok.Kind != syntax.TokenString {
		return definition.NormalizeText(tok.Raw)
	}
	value, err := strconv.Unquote(tok.Raw)
	if err != nil {
		return definition.NormalizeText(tok.Raw)
	}
	return definition.NormalizeText(value)
}

func qualifiedText(n *syntax.Node) string {
	var parts []string
	for _, tok := range nodeTokens(n) {
		if tok.Kind == syntax.TokenIdentifier {
			parts = append(parts, tok.Raw)
		}
	}
	return strings.Join(parts, ".")
}

func firstNode(n *syntax.Node, kind syntax.NodeKind) *syntax.Node {
	for _, child := range nodeChildren(n) {
		if child.Kind == kind {
			return child
		}
	}
	return nil
}

func nodeChildren(n *syntax.Node) []*syntax.Node {
	if n == nil {
		return nil
	}
	var out []*syntax.Node
	for _, child := range n.Children {
		if node, ok := child.(*syntax.Node); ok {
			out = append(out, node)
		}
	}
	return out
}

func descendants(n *syntax.Node, kind syntax.NodeKind) []*syntax.Node {
	var out []*syntax.Node
	syntax.Walk(n, func(node *syntax.Node) {
		if node.Kind == kind {
			out = append(out, node)
		}
	})
	return out
}

func directTokens(n *syntax.Node) []syntax.Token {
	if n == nil {
		return nil
	}
	var out []syntax.Token
	for _, child := range n.Children {
		if tok, ok := child.(syntax.TokenElement); ok {
			out = append(out, tok.Token)
		}
	}
	return out
}

func nodeTokens(n *syntax.Node) []syntax.Token {
	if n == nil {
		return nil
	}
	var out []syntax.Token
	syntax.Walk(n, func(node *syntax.Node) {
		for _, child := range node.Children {
			if tok, ok := child.(syntax.TokenElement); ok {
				switch tok.Token.Kind {
				case syntax.TokenNewline, syntax.TokenLineComment, syntax.TokenDocComment, syntax.TokenModuleDoc, syntax.TokenEOF:
				default:
					out = append(out, tok.Token)
				}
			}
		}
	})
	return out
}
