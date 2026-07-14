// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package exportrecipe

import (
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

type authoredMember struct {
	head     string
	args     []authoredValue
	block    *syntax.Node
	span     syntax.Span
	headSpan syntax.Span
}

type authoredValue struct {
	raw  string
	kind syntax.TokenKind
	span syntax.Span
	node *syntax.Node
}

func exportArguments(node *syntax.Node) []authoredValue {
	return authoredArguments(node)
}

func exportBody(node *syntax.Node) []authoredMember {
	return readMembers(firstNode(node, syntax.NodeBlock))
}

func readMembers(block *syntax.Node) []authoredMember {
	var out []authoredMember
	for _, child := range nodeChildren(block) {
		if child.Kind != syntax.NodeStatement && child.Kind != syntax.NodeNestedBlock {
			continue
		}
		tokens := directTokens(child)
		if len(tokens) == 0 {
			continue
		}
		member := authoredMember{head: tokens[0].Raw, args: authoredArguments(child), span: child.Span, headSpan: tokens[0].Span}
		if child.Kind == syntax.NodeNestedBlock {
			member.block = firstNode(child, syntax.NodeBlock)
		}
		out = append(out, member)
	}
	return out
}

func authoredArguments(node *syntax.Node) []authoredValue {
	var out []authoredValue
	for _, child := range nodeChildren(node) {
		if child.Kind != syntax.NodeValue {
			continue
		}
		tokens := nodeTokens(child)
		var raw strings.Builder
		for _, token := range tokens {
			raw.WriteString(token.Raw)
		}
		value := authoredValue{raw: raw.String(), span: child.Span, node: child}
		if len(tokens) != 0 {
			value.kind = tokens[0].Kind
		}
		out = append(out, value)
	}
	return out
}

func authoredString(value authoredValue) (string, bool) {
	if value.kind != syntax.TokenString {
		return "", false
	}
	unquoted, err := strconv.Unquote(value.raw)
	if err != nil {
		return "", false
	}
	return definition.NormalizeText(unquoted), true
}

func nodeChildren(node *syntax.Node) []*syntax.Node {
	if node == nil {
		return nil
	}
	var out []*syntax.Node
	for _, child := range node.Children {
		if nested, ok := child.(*syntax.Node); ok {
			out = append(out, nested)
		}
	}
	return out
}

func firstNode(node *syntax.Node, kind syntax.NodeKind) *syntax.Node {
	for _, child := range nodeChildren(node) {
		if child.Kind == kind {
			return child
		}
	}
	return nil
}

func directTokens(node *syntax.Node) []syntax.Token {
	if node == nil {
		return nil
	}
	var out []syntax.Token
	for _, child := range node.Children {
		if token, ok := child.(syntax.TokenElement); ok {
			out = append(out, token.Token)
		}
	}
	return out
}

func nodeTokens(node *syntax.Node) []syntax.Token {
	if node == nil {
		return nil
	}
	var out []syntax.Token
	syntax.Walk(node, func(current *syntax.Node) {
		for _, child := range current.Children {
			token, ok := child.(syntax.TokenElement)
			if !ok {
				continue
			}
			switch token.Token.Kind {
			case syntax.TokenNewline, syntax.TokenLineComment, syntax.TokenDocComment, syntax.TokenModuleDoc, syntax.TokenEOF:
			default:
				out = append(out, token.Token)
			}
		}
	})
	return out
}
