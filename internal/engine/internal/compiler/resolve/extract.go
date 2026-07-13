// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

type moduleAST struct {
	imports            []ImportDecl
	exports            []ExportDecl
	declarations       []rawDecl
	factGroups         []rawFactGroup
	reservations       []rawReservation
	reservationBlocks  []rawReservationBlock
	moves              []rawMove
	rootReservedBlocks []syntax.Span
	rootMoveBlocks     []syntax.Span
	ownerReserveBlocks map[string][]syntax.Span
	rowReserveBlocks   map[string][]syntax.Span
}

type rawDecl struct {
	kind      SubjectKind
	id        string
	owner     string
	ownerKind SubjectKind
	childOf   *rawDecl
	span      syntax.Span
	node      *syntax.Node
	refs      []rawRef
}

type rawRef struct {
	kind SubjectKind
	text string
	span syntax.Span
}

type rawFactGroup struct {
	kind    string
	span    syntax.Span
	refs    []rawRef
	members []rawDecl
}

type rawReservation struct {
	ownerKind SubjectKind
	ownerID   string
	kind      SubjectKind
	id        string
	span      syntax.Span
}

type rawReservationBlock struct {
	ownerKind SubjectKind
	ownerID   string
	row       bool
	span      syntax.Span
	node      *syntax.Node
}

type rawMove struct {
	kind    SubjectKind
	variant string
	ownerID string
	from    string
	to      string
	span    syntax.Span
}

func extractModule(file SourceFile) moduleAST {
	ast := moduleAST{ownerReserveBlocks: map[string][]syntax.Span{}, rowReserveBlocks: map[string][]syntax.Span{}}
	for _, child := range nodeChildren(file.Root) {
		if child.Kind != syntax.NodeImportDecl && child.Kind != syntax.NodeDeclaration {
			continue
		}
		switch {
		case child.Kind == syntax.NodeImportDecl:
			ast.imports = append(ast.imports, extractImport(child))
		case firstRaw(child) == "export":
			ast.exports = append(ast.exports, extractExport(firstNode(child, syntax.NodeExportDecl)))
		default:
			extractDeclaration(child, &ast)
		}
	}
	return ast
}

func extractImport(n *syntax.Node) ImportDecl {
	toks := nodeTokens(n)
	decl := ImportDecl{Range: n.Span}
	if len(toks) < 4 {
		return decl
	}
	decl.Specifier = stringToken(toks[len(toks)-1])
	if toks[1].Kind == syntax.TokenIdentifier {
		decl.Kind = ImportNamespace
		decl.Alias = toks[1].Raw
		return decl
	}
	decl.Kind = ImportNamed
	for _, item := range nodeChildren(n) {
		if item.Kind != syntax.NodeImportItems {
			continue
		}
		for _, child := range nodeChildren(item) {
			itoks := nodeTokens(child)
			if len(itoks) == 0 {
				continue
			}
			imp := ImportItem{Remote: itoks[0].Raw, Local: itoks[0].Raw, Range: child.Span}
			if len(itoks) >= 3 && itoks[1].Raw == "as" {
				imp.Local = itoks[2].Raw
			}
			decl.Items = append(decl.Items, imp)
		}
	}
	return decl
}

func extractExport(n *syntax.Node) ExportDecl {
	if n == nil {
		return ExportDecl{}
	}
	toks := nodeTokens(n)
	decl := ExportDecl{Range: n.Span}
	for i, tok := range toks {
		if tok.Raw == "from" && i+1 < len(toks) {
			decl.Specifier = stringToken(toks[i+1])
		}
	}
	if len(toks) > 1 && toks[1].Kind == syntax.TokenStar {
		decl.Kind = ExportStar
		return decl
	}
	if decl.Specifier != "" {
		decl.Kind = ExportFrom
	} else {
		decl.Kind = ExportLocal
	}
	for _, item := range nodeChildren(n) {
		if item.Kind != syntax.NodeExportItems {
			continue
		}
		for _, child := range nodeChildren(item) {
			itoks := nodeTokens(child)
			if len(itoks) == 0 {
				continue
			}
			exp := ExportItem{Local: itoks[0].Raw, Public: itoks[0].Raw, Range: child.Span}
			if len(itoks) >= 3 && itoks[1].Raw == "as" {
				exp.Public = itoks[2].Raw
			}
			decl.Items = append(decl.Items, exp)
		}
	}
	return decl
}

func extractDeclaration(n *syntax.Node, ast *moduleAST) {
	toks := directTokens(n)
	if len(toks) == 0 {
		return
	}
	switch toks[0].Raw {
	case "project":
		if len(toks) > 1 {
			ast.declarations = append(ast.declarations, rawDecl{kind: KindProject, id: toks[1].Raw, span: n.Span})
		}
	case "entity_type":
		d := rawDecl{kind: KindEntityType, span: n.Span}
		if len(toks) > 1 {
			d.id = toks[1].Raw
		}
		ast.declarations = append(ast.declarations, d)
		extractOwnerChildren(n, d, ast)
		extractOwnerReservations(n, d, ast)
	case "relation_type":
		d := rawDecl{kind: KindRelationType, span: n.Span}
		if len(toks) > 1 {
			d.id = toks[1].Raw
		}
		d.refs = append(d.refs, extractRelationTypeEndpointRefs(n)...)
		ast.declarations = append(ast.declarations, d)
		extractOwnerChildren(n, d, ast)
		extractOwnerReservations(n, d, ast)
	case "layers":
		for _, item := range descendants(n, syntax.NodeLayerItem) {
			itoks := nodeTokens(item)
			if len(itoks) > 0 {
				ast.declarations = append(ast.declarations, rawDecl{kind: KindLayer, id: itoks[0].Raw, span: item.Span})
			}
		}
	case "entities":
		refs := directSymbolRefs(n)
		var ownerType, layer qualifiedValue
		if len(refs) > 0 {
			ownerType = refs[0]
		}
		if len(refs) > 1 {
			layer = refs[1]
		}
		group := rawFactGroup{kind: "entities", span: n.Span, refs: []rawRef{{kind: KindEntityType, text: ownerType.text, span: ownerType.span}, {kind: KindLayer, text: layer.text, span: layer.span}}}
		for _, item := range descendants(n, syntax.NodeEntityItem) {
			itoks := nodeTokens(item)
			if len(itoks) > 0 {
				d := rawDecl{kind: KindEntity, id: itoks[0].Raw, span: item.Span}
				ast.declarations = append(ast.declarations, d)
				group.members = append(group.members, d)
				extractRowReservations(item, KindEntity, itoks[0].Raw, ast)
			}
		}
		ast.factGroups = append(ast.factGroups, group)
	case "rows":
		groupType := firstDirectSymbolRef(n)
		group := rawFactGroup{kind: "rows", span: n.Span, refs: []rawRef{{kind: KindEntityType, text: groupType.text, span: groupType.span}}}
		for _, item := range descendants(n, syntax.NodeRowItem) {
			itoks := nodeTokens(item)
			if len(itoks) >= 2 {
				d := rawDecl{kind: KindRow, id: itoks[1].Raw, owner: itoks[0].Raw, ownerKind: KindEntity, span: item.Span}
				ast.declarations = append(ast.declarations, d)
				group.members = append(group.members, d)
			}
		}
		ast.factGroups = append(ast.factGroups, group)
	case "relations":
		relType := firstDirectSymbolRef(n)
		group := rawFactGroup{kind: "relations", span: n.Span, refs: []rawRef{{kind: KindRelationType, text: relType.text, span: relType.span}}}
		for _, item := range descendants(n, syntax.NodeRelationItem) {
			itoks := nodeTokens(item)
			if len(itoks) > 0 {
				var refs []rawRef
				srefs := directSymbolRefs(item)
				for _, ref := range srefs {
					refs = append(refs, rawRef{kind: KindEntity, text: ref.text, span: ref.span})
				}
				d := rawDecl{kind: KindRelation, id: itoks[0].Raw, span: item.Span, refs: refs}
				ast.declarations = append(ast.declarations, d)
				group.members = append(group.members, d)
				extractRowReservations(item, KindRelation, itoks[0].Raw, ast)
			}
		}
		ast.factGroups = append(ast.factGroups, group)
	case "relation_rows":
		groupType := firstDirectSymbolRef(n)
		group := rawFactGroup{kind: "relation_rows", span: n.Span, refs: []rawRef{{kind: KindRelationType, text: groupType.text, span: groupType.span}}}
		for _, item := range descendants(n, syntax.NodeRowItem) {
			itoks := nodeTokens(item)
			if len(itoks) >= 2 {
				d := rawDecl{kind: KindRow, id: itoks[1].Raw, owner: itoks[0].Raw, ownerKind: KindRelation, span: item.Span}
				ast.declarations = append(ast.declarations, d)
				group.members = append(group.members, d)
			}
		}
		ast.factGroups = append(ast.factGroups, group)
	case "query":
		d := rawDecl{kind: KindQuery, span: n.Span, node: n}
		if len(toks) > 1 {
			d.id = toks[1].Raw
		}
		ast.declarations = append(ast.declarations, d)
		extractNamedBlockChildren(n, d, "parameters", KindParameter, ast)
		extractOwnerReservations(n, d, ast)
	case "view":
		d := rawDecl{kind: KindView, span: n.Span}
		if len(toks) > 1 {
			d.id = toks[1].Raw
		}
		ast.declarations = append(ast.declarations, d)
		extractViewChildren(n, d, ast)
		extractOwnerReservations(n, d, ast)
	case "reference":
		if len(toks) > 1 {
			ast.declarations = append(ast.declarations, rawDecl{kind: KindReference, id: toks[1].Raw, span: n.Span})
		}
	case "reserved":
		ast.rootReservedBlocks = append(ast.rootReservedBlocks, n.Span)
		ast.reservationBlocks = append(ast.reservationBlocks, rawReservationBlock{span: n.Span, node: n})
	case "moves":
		ast.rootMoveBlocks = append(ast.rootMoveBlocks, n.Span)
		ast.moves = append(ast.moves, extractMoves(n)...)
	}
}

func extractRelationTypeEndpointRefs(n *syntax.Node) []rawRef {
	var refs []rawRef
	body := firstNode(n, syntax.NodeBlock)
	for _, stmt := range nodeChildren(body) {
		if stmt.Kind != syntax.NodeStatement {
			continue
		}
		toks := nodeTokens(stmt)
		if len(toks) == 0 || (toks[0].Raw != "from" && toks[0].Raw != "to") {
			continue
		}
		mode := ""
		for _, child := range stmt.Children {
			node, ok := child.(*syntax.Node)
			if !ok || node.Kind != syntax.NodeValue {
				continue
			}
			vtoks := nodeTokens(node)
			if len(vtoks) == 0 {
				continue
			}
			switch vtoks[0].Raw {
			case "types", "layers":
				mode = vtoks[0].Raw
				continue
			}
			switch mode {
			case "types":
				for _, q := range qualifiedValues(node) {
					refs = append(refs, rawRef{kind: KindEntityType, text: q.text, span: q.span})
				}
			case "layers":
				for _, q := range qualifiedValues(node) {
					refs = append(refs, rawRef{kind: KindLayer, text: q.text, span: q.span})
				}
			}
		}
	}
	return refs
}

type qualifiedValue struct {
	text string
	span syntax.Span
}

func qualifiedValues(n *syntax.Node) []qualifiedValue {
	var out []qualifiedValue
	syntax.Walk(n, func(node *syntax.Node) {
		if node.Kind != syntax.NodeQualifiedToken && node.Kind != syntax.NodeSymbolRef {
			return
		}
		toks := nodeTokens(node)
		var parts []string
		for _, tok := range toks {
			if tok.Kind == syntax.TokenIdentifier {
				parts = append(parts, tok.Raw)
			}
		}
		if len(parts) > 0 {
			out = append(out, qualifiedValue{text: strings.Join(parts, "."), span: node.Span})
		}
	})
	return out
}

func extractOwnerChildren(n *syntax.Node, owner rawDecl, ast *moduleAST) {
	extractNamedBlockChildren(n, owner, "columns", KindColumn, ast)
	extractUniqueConstraints(n, owner, ast)
}

func extractOwnerReservations(n *syntax.Node, owner rawDecl, ast *moduleAST) {
	for _, nb := range nodeChildren(firstNode(n, syntax.NodeBlock)) {
		if nb.Kind != syntax.NodeNestedBlock {
			continue
		}
		toks := directTokens(nb)
		if len(toks) == 0 || toks[0].Raw != "reserve" {
			continue
		}
		key := string(owner.kind) + ":" + owner.id
		ast.ownerReserveBlocks[key] = append(ast.ownerReserveBlocks[key], nb.Span)
		ast.reservationBlocks = append(ast.reservationBlocks, rawReservationBlock{ownerKind: owner.kind, ownerID: owner.id, span: nb.Span, node: nb})
	}
}

func extractRowReservations(n *syntax.Node, ownerKind SubjectKind, ownerID string, ast *moduleAST) {
	for _, stmt := range nodeChildren(firstNode(n, syntax.NodeBlock)) {
		if stmt.Kind != syntax.NodeStatement {
			continue
		}
		toks := nodeTokens(stmt)
		if len(toks) == 0 || toks[0].Raw != "reserve_rows" {
			continue
		}
		key := string(ownerKind) + ":" + ownerID
		ast.rowReserveBlocks[key] = append(ast.rowReserveBlocks[key], stmt.Span)
		ast.reservationBlocks = append(ast.reservationBlocks, rawReservationBlock{ownerKind: ownerKind, ownerID: ownerID, row: true, span: stmt.Span, node: stmt})
	}
}

func extractNamedBlockChildren(n *syntax.Node, owner rawDecl, block string, kind SubjectKind, ast *moduleAST) {
	for _, nb := range nodeChildren(firstNode(n, syntax.NodeBlock)) {
		if nb.Kind != syntax.NodeNestedBlock {
			continue
		}
		toks := directTokens(nb)
		if len(toks) == 0 || toks[0].Raw != block {
			continue
		}
		for _, stmt := range nodeChildren(firstNode(nb, syntax.NodeBlock)) {
			if stmt.Kind != syntax.NodeStatement {
				continue
			}
			stoks := nodeTokens(stmt)
			if len(stoks) > 0 {
				od := owner
				ast.declarations = append(ast.declarations, rawDecl{kind: kind, id: stoks[0].Raw, childOf: &od, span: stmt.Span})
			}
		}
	}
}

func extractUniqueConstraints(n *syntax.Node, owner rawDecl, ast *moduleAST) {
	for _, stmt := range nodeChildren(firstNode(n, syntax.NodeBlock)) {
		if stmt.Kind != syntax.NodeStatement {
			continue
		}
		toks := nodeTokens(stmt)
		if len(toks) >= 2 && toks[0].Raw == "unique" {
			od := owner
			ast.declarations = append(ast.declarations, rawDecl{kind: KindConstraint, id: toks[1].Raw, childOf: &od, span: stmt.Span})
		}
	}
}

func extractViewChildren(n *syntax.Node, owner rawDecl, ast *moduleAST) {
	for _, nb := range descendants(n, syntax.NodeNestedBlock) {
		toks := nodeTokens(nb)
		if len(toks) < 2 {
			continue
		}
		switch toks[0].Raw {
		case "column":
			od := owner
			ast.declarations = append(ast.declarations, rawDecl{kind: KindTableColumn, id: toks[1].Raw, childOf: &od, span: nb.Span})
		case "export":
			od := owner
			ast.declarations = append(ast.declarations, rawDecl{kind: KindExport, id: toks[1].Raw, childOf: &od, span: nb.Span})
		}
	}
}

func extractMoves(n *syntax.Node) []rawMove {
	var out []rawMove
	for _, item := range descendants(n, syntax.NodeMoveItem) {
		toks := nodeTokens(item)
		if len(toks) < 4 {
			continue
		}
		kind, child := moveKind(toks[0].Raw)
		if child {
			if len(toks) >= 5 {
				out = append(out, rawMove{kind: kind, variant: toks[0].Raw, ownerID: toks[1].Raw, from: toks[2].Raw, to: toks[4].Raw, span: item.Span})
			}
		} else {
			out = append(out, rawMove{kind: kind, variant: toks[0].Raw, from: toks[1].Raw, to: toks[3].Raw, span: item.Span})
		}
	}
	return out
}

func reservationKind(raw string) (SubjectKind, bool) {
	switch raw {
	case "entity_types":
		return KindEntityType, true
	case "relation_types":
		return KindRelationType, true
	case "layers":
		return KindLayer, true
	case "entities":
		return KindEntity, true
	case "relations":
		return KindRelation, true
	case "queries":
		return KindQuery, true
	case "views":
		return KindView, true
	case "references":
		return KindReference, true
	case "columns":
		return KindColumn, true
	case "constraints":
		return KindConstraint, true
	case "parameters":
		return KindParameter, true
	case "table_columns":
		return KindTableColumn, true
	case "exports":
		return KindExport, true
	default:
		return "", false
	}
}

func moveKind(raw string) (SubjectKind, bool) {
	switch raw {
	case "project":
		return KindProject, false
	case "entity_type":
		return KindEntityType, false
	case "relation_type":
		return KindRelationType, false
	case "layer":
		return KindLayer, false
	case "entity":
		return KindEntity, false
	case "relation":
		return KindRelation, false
	case "query":
		return KindQuery, false
	case "view":
		return KindView, false
	case "reference":
		return KindReference, false
	case "entity_type_column", "relation_type_column":
		return KindColumn, true
	case "entity_type_constraint", "relation_type_constraint":
		return KindConstraint, true
	case "entity_row", "relation_row":
		return KindRow, true
	case "query_parameter":
		return KindParameter, true
	case "view_table_column":
		return KindTableColumn, true
	case "view_export":
		return KindExport, true
	default:
		return "", false
	}
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

func firstNode(n *syntax.Node, kind syntax.NodeKind) *syntax.Node {
	for _, child := range nodeChildren(n) {
		if child.Kind == kind {
			return child
		}
	}
	return nil
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

func nodesBySpan(n *syntax.Node) map[syntax.Span]*syntax.Node {
	out := map[syntax.Span]*syntax.Node{}
	syntax.Walk(n, func(node *syntax.Node) {
		if _, exists := out[node.Span]; !exists {
			out[node.Span] = node
		}
	})
	return out
}

func firstRaw(n *syntax.Node) string {
	toks := nodeTokens(n)
	if len(toks) == 0 {
		return ""
	}
	return toks[0].Raw
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
	var walk func(*syntax.Node)
	walk = func(node *syntax.Node) {
		for _, child := range node.Children {
			switch c := child.(type) {
			case syntax.TokenElement:
				if c.Token.Kind != syntax.TokenNewline && c.Token.Kind != syntax.TokenLineComment && c.Token.Kind != syntax.TokenDocComment && c.Token.Kind != syntax.TokenModuleDoc && c.Token.Kind != syntax.TokenEOF {
					out = append(out, c.Token)
				}
			case *syntax.Node:
				walk(c)
			}
		}
	}
	walk(n)
	return out
}

func stringToken(tok syntax.Token) string {
	if tok.Kind != syntax.TokenString {
		return tok.Raw
	}
	s, err := strconv.Unquote(tok.Raw)
	if err != nil {
		return tok.Raw
	}
	return s
}

func firstSymbolRef(n *syntax.Node) string {
	refs := symbolRefs(n)
	if len(refs) == 0 {
		return ""
	}
	return refs[0]
}

func firstDirectSymbolRef(n *syntax.Node) qualifiedValue {
	refs := directSymbolRefs(n)
	if len(refs) == 0 {
		return qualifiedValue{}
	}
	return refs[0]
}

func directSymbolRefs(n *syntax.Node) []qualifiedValue {
	var refs []qualifiedValue
	for _, ref := range nodeChildren(n) {
		if ref.Kind != syntax.NodeSymbolRef {
			continue
		}
		toks := nodeTokens(ref)
		var parts []string
		for _, tok := range toks {
			if tok.Kind == syntax.TokenIdentifier {
				parts = append(parts, tok.Raw)
			}
		}
		if len(parts) > 0 {
			refs = append(refs, qualifiedValue{text: strings.Join(parts, "."), span: ref.Span})
		}
	}
	return refs
}

func symbolRefs(n *syntax.Node) []string {
	var refs []string
	for _, ref := range descendants(n, syntax.NodeSymbolRef) {
		toks := nodeTokens(ref)
		if len(toks) == 1 {
			refs = append(refs, toks[0].Raw)
		} else if len(toks) >= 3 {
			refs = append(refs, toks[0].Raw+"."+toks[2].Raw)
		}
	}
	return refs
}
