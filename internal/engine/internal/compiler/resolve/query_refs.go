// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

// resolveQueryRefs resolves every semantic name in a Query body. Downstream
// Query compilation deliberately has no raw-text name lookup fallback.
func (r *resolver) resolveQueryRefs(st *moduleState, raw rawDecl, sourceAddress string) {
	body := firstNode(raw.node, syntax.NodeBlock)
	for _, member := range nodeChildren(body) {
		head := queryHead(member)
		switch head {
		case "select":
			r.resolveQuerySelect(st, member, sourceAddress)
		case "where":
			r.resolveQueryPredicate(st, member, KindEntity, sourceAddress)
		case "relation_where":
			r.resolveQueryPredicate(st, member, KindRelation, sourceAddress)
		case "traverse":
			r.resolveQueryRelationList(st, member, sourceAddress, "query:traverse.relation_types")
		}
	}
	r.resolveQueryParameterRefs(st, raw, sourceAddress)
}

func (r *resolver) resolveQuerySelect(st *moduleState, selectNode *syntax.Node, sourceAddress string) {
	block := firstNode(selectNode, syntax.NodeBlock)
	for _, member := range nodeChildren(block) {
		var kind SubjectKind
		var via string
		switch queryHead(member) {
		case "layers":
			kind, via = KindLayer, "query:select.layers"
		case "entity_types":
			kind, via = KindEntityType, "query:select.entity_types"
		case "relation_types":
			kind, via = KindRelationType, "query:select.relation_types"
		case "roots":
			kind, via = KindEntity, "query:select.roots"
		default:
			continue
		}
		args := queryArgumentValues(member)
		if len(args) != 1 || !args[0].list {
			continue
		}
		for _, value := range queryListValues(args[0].node) {
			r.resolveQueryTop(st, kind, value.text, value.span, via, sourceAddress)
		}
	}
}

func (r *resolver) resolveQueryPredicate(st *moduleState, node *syntax.Node, ownerKind SubjectKind, sourceAddress string) {
	block := firstNode(node, syntax.NodeBlock)
	for _, child := range nodeChildren(block) {
		switch queryHead(child) {
		case "all", "any", "not":
			r.resolveQueryPredicate(st, child, ownerKind, sourceAddress)
		case "field":
			r.resolveQueryField(st, child, ownerKind, sourceAddress)
		case "rows":
			r.resolveQueryRows(st, child, ownerKind, sourceAddress)
		}
	}
}

func (r *resolver) resolveQueryField(st *moduleState, node *syntax.Node, ownerKind SubjectKind, sourceAddress string) {
	args := queryArgumentValues(node)
	if len(args) < 2 {
		return
	}
	field := args[0].text
	var kind SubjectKind
	switch ownerKind {
	case KindEntity:
		switch field {
		case "address":
			kind = KindEntity
		case "type":
			kind = KindEntityType
		case "layer":
			kind = KindLayer
		}
	case KindRelation:
		switch field {
		case "address":
			kind = KindRelation
		case "type":
			kind = KindRelationType
		case "from", "to":
			kind = KindEntity
		}
	}
	if kind == "" {
		return
	}
	for _, value := range queryPredicateLiteralValues(node) {
		if value.parameter {
			continue
		}
		r.resolveQueryTop(st, kind, value.text, value.span, "query:predicate.field."+field, sourceAddress)
	}
}

func (r *resolver) resolveQueryRows(st *moduleState, node *syntax.Node, ownerKind SubjectKind, sourceAddress string) {
	typeKind := KindEntityType
	if ownerKind == KindRelation {
		typeKind = KindRelationType
	}
	var owners []DeclarationSymbol
	args := queryArgumentValues(node)
	for i := 0; i+1 < len(args); i++ {
		if args[i].text != "types" || !args[i+1].list {
			continue
		}
		for _, value := range queryListValues(args[i+1].node) {
			if target, ok := r.resolveQueryTop(st, typeKind, value.text, value.span, "query:rows.types", sourceAddress); ok {
				owners = append(owners, target)
			}
		}
		break
	}
	r.resolveQueryRowPredicate(st, firstNode(node, syntax.NodeBlock), owners, sourceAddress)
}

func (r *resolver) resolveQueryRowPredicate(st *moduleState, block *syntax.Node, owners []DeclarationSymbol, sourceAddress string) {
	for _, child := range nodeChildren(block) {
		switch queryHead(child) {
		case "all", "any", "not":
			r.resolveQueryRowPredicate(st, firstNode(child, syntax.NodeBlock), owners, sourceAddress)
		case "cell":
			args := queryArgumentValues(child)
			if len(args) == 0 || args[0].text == "" {
				continue
			}
			columnID := args[0].text
			found := 0
			for _, owner := range owners {
				address := reservationAddress(owner.Symbol, KindColumn, columnID)
				column, ok := r.symbols[address]
				if !ok || column.Kind != KindColumn {
					continue
				}
				st.addBinding(KindColumn, columnID, args[0].span, "query:row.cell", column, sourceAddress)
				found++
			}
			if found == 0 {
				r.diag("LDL1301", "unknown_or_ambiguous_symbol", "query row column is unknown for its owner types", st.key, args[0].span)
			}
		}
	}
}

func (r *resolver) resolveQueryRelationList(st *moduleState, node *syntax.Node, sourceAddress, via string) {
	args := queryArgumentValues(node)
	for i := 0; i+1 < len(args); i++ {
		if args[i].text != "relations" || !args[i+1].list {
			continue
		}
		for _, value := range queryListValues(args[i+1].node) {
			r.resolveQueryTop(st, KindRelationType, value.text, value.span, via, sourceAddress)
		}
		return
	}
}

func (r *resolver) resolveQueryParameterRefs(st *moduleState, raw rawDecl, sourceAddress string) {
	owner, ok := st.findTop(raw.id, KindQuery)
	if !ok {
		return
	}
	syntax.Walk(raw.node, func(node *syntax.Node) {
		if node.Kind != syntax.NodeParameterRef {
			return
		}
		tokens := nodeTokens(node)
		if len(tokens) != 2 || tokens[1].Kind != syntax.TokenIdentifier {
			return
		}
		id := tokens[1].Raw
		address := reservationAddress(owner.Symbol, KindParameter, id)
		parameter, exists := r.symbols[address]
		if !exists || parameter.Kind != KindParameter {
			r.diag("LDL1301", "unknown_or_ambiguous_symbol", "query parameter reference is unknown", st.key, node.Span)
			return
		}
		st.addBinding(KindParameter, "$"+id, node.Span, "query:parameter", parameter, sourceAddress)
	})
}

func (r *resolver) resolveQueryTop(st *moduleState, kind SubjectKind, text string, span syntax.Span, via, sourceAddress string) (DeclarationSymbol, bool) {
	target, ok := r.resolveText(st, kind, text)
	if !ok {
		r.diag("LDL1301", "unknown_or_ambiguous_symbol", "query source binding is unknown or ambiguous", st.key, span)
		return DeclarationSymbol{}, false
	}
	st.addBinding(kind, text, span, via, target, sourceAddress)
	return target, true
}

type queryValue struct {
	text      string
	span      syntax.Span
	node      *syntax.Node
	list      bool
	parameter bool
}

func queryHead(node *syntax.Node) string {
	tokens := directTokens(node)
	if len(tokens) == 0 {
		return ""
	}
	return tokens[0].Raw
}

func queryArgumentValues(node *syntax.Node) []queryValue {
	var out []queryValue
	for _, child := range nodeChildren(node) {
		if child.Kind != syntax.NodeValue {
			continue
		}
		out = append(out, queryValueFromNode(child))
	}
	return out
}

func queryValueFromNode(node *syntax.Node) queryValue {
	tokens := nodeTokens(node)
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind == syntax.TokenDollar {
			parts = append(parts, "$")
			continue
		}
		parts = append(parts, token.Raw)
	}
	return queryValue{
		text:      strings.Join(parts, ""),
		span:      node.Span,
		node:      node,
		list:      firstNode(node, syntax.NodeList) != nil,
		parameter: firstNode(node, syntax.NodeParameterRef) != nil,
	}
}

func queryListValues(node *syntax.Node) []qualifiedValue {
	list := firstNode(node, syntax.NodeList)
	if list == nil {
		return nil
	}
	var out []qualifiedValue
	for _, child := range nodeChildren(list) {
		if child.Kind != syntax.NodeValue {
			continue
		}
		value := queryValueFromNode(child)
		out = append(out, qualifiedValue{text: value.text, span: value.span})
	}
	return out
}

func queryPredicateLiteralValues(node *syntax.Node) []queryValue {
	args := queryArgumentValues(node)
	if len(args) < 2 {
		return nil
	}
	hasSymbolOperator := false
	for _, token := range directTokens(node) {
		switch token.Kind {
		case syntax.TokenEqualEqual, syntax.TokenBangEqual, syntax.TokenLess, syntax.TokenLessEqual, syntax.TokenGreater, syntax.TokenGreaterEqual:
			hasSymbolOperator = true
		}
	}
	if !hasSymbolOperator {
		switch args[1].text {
		case "exists", "missing":
			return nil
		case "in", "not_in", "contains", "starts_with", "ends_with":
			if len(args) < 3 {
				return nil
			}
		default:
			return nil
		}
	}
	value := args[len(args)-1]
	if !value.list {
		return []queryValue{value}
	}
	var out []queryValue
	for _, child := range nodeChildren(firstNode(value.node, syntax.NodeList)) {
		if child.Kind == syntax.NodeValue {
			out = append(out, queryValueFromNode(child))
		}
	}
	return out
}
