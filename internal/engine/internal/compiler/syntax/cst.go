// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package syntax

// NodeKind identifies a concrete syntax tree node.
type NodeKind string

const (
	NodeFile           NodeKind = "file"
	NodeImportDecl     NodeKind = "import_decl"
	NodeImportItems    NodeKind = "import_items"
	NodeImportItem     NodeKind = "import_item"
	NodeDeclaration    NodeKind = "declaration"
	NodeBlock          NodeKind = "block"
	NodeItemBlock      NodeKind = "item_block"
	NodeLayerItem      NodeKind = "layer_item"
	NodeEntityItem     NodeKind = "entity_item"
	NodeRelationItem   NodeKind = "relation_item"
	NodeRowItem        NodeKind = "row_item"
	NodeMoveItem       NodeKind = "move_item"
	NodeExportDecl     NodeKind = "export_decl"
	NodeExportItems    NodeKind = "export_items"
	NodeExportItem     NodeKind = "export_item"
	NodeColumnHeader   NodeKind = "column_header"
	NodeCells          NodeKind = "cells"
	NodeSymbolRef      NodeKind = "symbol_ref"
	NodeQualifiedToken NodeKind = "qualified_token"
	NodeValue          NodeKind = "value"
	NodeList           NodeKind = "list"
	NodeObject         NodeKind = "object"
	NodeObjectItem     NodeKind = "object_item"
	NodeRange          NodeKind = "range"
	NodeStatement      NodeKind = "statement"
	NodeNestedBlock    NodeKind = "nested_block"
	NodeParameterRef   NodeKind = "parameter_ref"
	NodeComment        NodeKind = "comment"
	NodeError          NodeKind = "error"
)

// Element is either a *Node or *TokenElement.
type Element interface {
	elementSpan() Span
}

// TokenElement references a concrete token by index.
type TokenElement struct {
	Index int
	Token Token
}

func (e TokenElement) elementSpan() Span {
	return e.Token.Span
}

// Node is a lossless CST node over the token stream.
type Node struct {
	Kind     NodeKind
	Span     Span
	Children []Element
}

func (n *Node) elementSpan() Span {
	if n == nil {
		return Span{}
	}
	return n.Span
}

func newNode(kind NodeKind, children ...Element) *Node {
	n := &Node{Kind: kind, Children: children}
	n.refreshSpan()
	return n
}

func (n *Node) append(children ...Element) {
	n.Children = append(n.Children, children...)
	n.refreshSpan()
}

func (n *Node) refreshSpan() {
	for _, child := range n.Children {
		span := child.elementSpan()
		if n.Span.Empty() && n.Span.Start == 0 && n.Span.End == 0 {
			n.Span = span
			continue
		}
		if span.Start < n.Span.Start {
			n.Span.Start = span.Start
		}
		if span.End > n.Span.End {
			n.Span.End = span.End
		}
	}
}

// Walk visits n and all descendants depth-first.
func Walk(n *Node, visit func(*Node)) {
	if n == nil {
		return
	}
	visit(n)
	for _, child := range n.Children {
		if node, ok := child.(*Node); ok {
			Walk(node, visit)
		}
	}
}
