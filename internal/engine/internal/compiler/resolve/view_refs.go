// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

// resolveViewRefs resolves every semantic name in a View body. Downstream
// View and Export recipe compilation deliberately has no raw-name fallback.
// Binding is performed for all loaded modules so it can drive effective
// document closure; diagnostics are emitted only after selection.
func (r *resolver) resolveViewRefs(st *moduleState, raw rawDecl, sourceAddress string, bind, diagnose bool) {
	view, ok := st.findTop(raw.id, KindView)
	if !ok {
		return
	}
	body := firstNode(raw.node, syntax.NodeBlock)
	var sourceQuery *DeclarationSymbol
	// Source-dependent owner inference must not depend on where the source is
	// authored. Formatting can move a valid source into canonical position only
	// after semantic validation.
	for _, member := range nodeChildren(body) {
		if queryHead(member) != "source" {
			continue
		}
		query := r.resolveViewSource(st, member, sourceAddress, bind, diagnose)
		if query.Address != "" && sourceQuery == nil {
			sourceQuery = &query
		}
	}
	for _, member := range nodeChildren(body) {
		switch queryHead(member) {
		case "source":
			continue
		case "relation_projection":
			args := queryArgumentValues(member)
			if len(args) > 0 {
				if relationType, ok := r.resolveViewTop(st, KindRelationType, args[0], "view:relation_projection", sourceAddress, bind, diagnose); ok {
					r.resolveProjectionColumnRefs(st, member, relationType, sourceAddress, bind, diagnose)
				}
			}
		case "diagram":
			r.resolveDiagramRefs(st, member, sourceAddress, bind, diagnose)
		case "table":
			r.resolveTableRefs(st, view, member, sourceAddress, sourceQuery, bind, diagnose)
		case "matrix":
			r.resolveMatrixRefs(st, member, sourceAddress, sourceQuery, bind, diagnose)
		case "tree":
			r.resolveRelationTypeStatements(st, member, sourceAddress, "view:tree.relation_types", bind, diagnose)
		case "flow":
			r.resolveFlowRefs(st, member, sourceAddress, sourceQuery, bind, diagnose)
		}
	}
}

func (r *resolver) resolveProjectionColumnRefs(st *moduleState, node *syntax.Node, relationType DeclarationSymbol, sourceAddress string, bind, diagnose bool) {
	for _, primitive := range nodeChildren(firstNode(node, syntax.NodeBlock)) {
		if queryHead(primitive) != "flow" {
			continue
		}
		for _, field := range nodeChildren(firstNode(primitive, syntax.NodeBlock)) {
			if queryHead(field) != "branch_value_column" {
				continue
			}
			args := queryArgumentValues(field)
			if len(args) != 1 {
				continue
			}
			r.resolveOwnerColumns(st, []DeclarationSymbol{relationType}, args[0], "view:projection.flow.branch_value_column", sourceAddress, bind, diagnose)
		}
	}
}

func (r *resolver) resolveViewSource(st *moduleState, node *syntax.Node, sourceAddress string, bind, diagnose bool) DeclarationSymbol {
	args := queryArgumentValues(node)
	if len(args) == 0 {
		return DeclarationSymbol{}
	}
	var query DeclarationSymbol
	switch args[0].text {
	case "query":
		if len(args) >= 2 {
			query, _ = r.resolveViewTop(st, KindQuery, args[1], "view:source.query", sourceAddress, bind, diagnose)
		}
		if query.Address != "" && len(args) >= 3 {
			r.resolveViewArguments(st, query, args[2].node, sourceAddress, bind, diagnose)
		}
	case "diff":
		children := nodeChildren(firstNode(node, syntax.NodeBlock))
		for _, child := range children {
			if queryHead(child) == "query" {
				values := queryArgumentValues(child)
				if len(values) == 1 {
					query, _ = r.resolveViewTop(st, KindQuery, values[0], "view:source.diff.query", sourceAddress, bind, diagnose)
				}
			}
		}
		for _, child := range children {
			if queryHead(child) == "arguments" {
				values := queryArgumentValues(child)
				if query.Address != "" && len(values) == 1 {
					r.resolveViewArguments(st, query, values[0].node, sourceAddress, bind, diagnose)
				}
			}
		}
	}
	return query
}

func (r *resolver) resolveViewArguments(st *moduleState, query DeclarationSymbol, value *syntax.Node, sourceAddress string, bind, diagnose bool) {
	object := firstNode(value, syntax.NodeObject)
	if object == nil {
		return
	}
	for _, item := range nodeChildren(object) {
		if item.Kind != syntax.NodeObjectItem {
			continue
		}
		tokens := directTokens(item)
		if len(tokens) == 0 || tokens[0].Kind != syntax.TokenIdentifier {
			continue
		}
		id := tokens[0].Raw
		address := reservationAddress(query.Symbol, KindParameter, id)
		parameter, exists := r.symbols[address]
		if !exists || parameter.Kind != KindParameter {
			if diagnose {
				r.diag("LDL1301", "unknown_or_ambiguous_symbol", "View argument parameter is unknown", st.key, tokens[0].Span)
			}
			continue
		}
		if bind {
			st.addBinding(KindParameter, id, tokens[0].Span, "view:source.argument", parameter, sourceAddress)
		}
	}
}

func (r *resolver) resolveDiagramRefs(st *moduleState, node *syntax.Node, sourceAddress string, bind, diagnose bool) {
	for _, member := range nodeChildren(firstNode(node, syntax.NodeBlock)) {
		if queryHead(member) != "place" {
			continue
		}
		args := queryArgumentValues(member)
		if len(args) > 0 {
			r.resolveViewTop(st, KindEntity, args[0], "view:diagram.place", sourceAddress, bind, diagnose)
		}
	}
}

func (r *resolver) resolveTableRefs(st *moduleState, view DeclarationSymbol, node *syntax.Node, sourceAddress string, sourceQuery *DeclarationSymbol, bind, diagnose bool) {
	block := firstNode(node, syntax.NodeBlock)
	rowSource := "entity"
	var entityOwners []DeclarationSymbol
	for _, member := range nodeChildren(block) {
		switch queryHead(member) {
		case "rows":
			args := queryArgumentValues(member)
			if len(args) == 1 {
				rowSource = args[0].text
			}
		case "entity_types":
			entityOwners = r.resolveViewList(st, member, KindEntityType, "view:table.entity_types", sourceAddress, bind, diagnose)
		}
	}
	for _, member := range nodeChildren(block) {
		if queryHead(member) != "column" {
			continue
		}
		args := queryArgumentValues(member)
		if len(args) == 0 {
			continue
		}
		childAddress := reservationAddress(view.Symbol, KindTableColumn, args[0].text)
		r.resolveTableColumnRefs(st, member, childAddress, rowSource, entityOwners, sourceQuery, bind, diagnose)
	}
}

func (r *resolver) resolveTableColumnRefs(st *moduleState, node *syntax.Node, sourceAddress, rowSource string, tableEntityOwners []DeclarationSymbol, sourceQuery *DeclarationSymbol, bind, diagnose bool) {
	for _, member := range nodeChildren(firstNode(node, syntax.NodeBlock)) {
		if queryHead(member) != "source" {
			continue
		}
		args := queryArgumentValues(member)
		if len(args) < 2 {
			continue
		}
		switch args[0].text {
		case "attribute":
			owners := r.viewAttributeOwners(st, args[2:], rowSource, tableEntityOwners, nil, sourceQuery, sourceAddress, bind, diagnose)
			r.resolveOwnerColumns(st, owners, args[1], "view:table.column.attribute", sourceAddress, bind, diagnose)
		case "derived_count":
			for index := 2; index+1 < len(args); index++ {
				if args[index].text == "relations" {
					r.resolveViewValueList(st, args[index+1], KindRelationType, "view:table.column.derived_count", sourceAddress, bind, diagnose)
				}
			}
		}
	}
}

func (r *resolver) viewAttributeOwners(st *moduleState, args []queryValue, rowSource string, tableEntityOwners, defaultRelationOwners []DeclarationSymbol, sourceQuery *DeclarationSymbol, sourceAddress string, bind, diagnose bool) []DeclarationSymbol {
	var owners []DeclarationSymbol
	for index := 0; index+1 < len(args); index++ {
		var kind SubjectKind
		switch args[index].text {
		case "entity_types":
			kind = KindEntityType
		case "relation_types":
			kind = KindRelationType
		default:
			continue
		}
		resolved := r.resolveViewValueList(st, args[index+1], kind, "view:column.owner_types", sourceAddress, bind, diagnose)
		owners = append(owners, resolved...)
		index++
	}
	if len(owners) != 0 {
		return uniqueDeclarations(owners)
	}
	if rowSource == "entity_rows" && len(tableEntityOwners) != 0 {
		return tableEntityOwners
	}
	if rowSource == "relation_rows" || rowSource == "automatic_relations" {
		if len(defaultRelationOwners) != 0 {
			return defaultRelationOwners
		}
		return r.visibleOwners(st, KindRelationType, sourceQuery)
	}
	return r.visibleOwners(st, KindEntityType, sourceQuery)
}

func (r *resolver) resolveMatrixRefs(st *moduleState, node *syntax.Node, sourceAddress string, sourceQuery *DeclarationSymbol, bind, diagnose bool) {
	var relationOwners []DeclarationSymbol
	block := firstNode(node, syntax.NodeBlock)
	for _, member := range nodeChildren(block) {
		switch queryHead(member) {
		case "row_axis", "column_axis":
			for _, axisMember := range nodeChildren(firstNode(member, syntax.NodeBlock)) {
				if queryHead(axisMember) == "entity_types" {
					r.resolveViewList(st, axisMember, KindEntityType, "view:matrix.axis.entity_types", sourceAddress, bind, diagnose)
				}
			}
		case "cell":
			for _, cellMember := range nodeChildren(firstNode(member, syntax.NodeBlock)) {
				if queryHead(cellMember) == "relation_types" {
					relationOwners = r.resolveViewList(st, cellMember, KindRelationType, "view:matrix.cell.relation_types", sourceAddress, bind, diagnose)
				}
			}
		}
	}
	for _, member := range nodeChildren(block) {
		if queryHead(member) != "cell" {
			continue
		}
		for _, cellMember := range nodeChildren(firstNode(member, syntax.NodeBlock)) {
			if queryHead(cellMember) != "attributes" {
				continue
			}
			args := queryArgumentValues(cellMember)
			if len(args) != 1 || !args[0].list {
				continue
			}
			owners := relationOwners
			if len(owners) == 0 {
				owners = r.visibleOwners(st, KindRelationType, sourceQuery)
			}
			for _, column := range queryListValues(args[0].node) {
				r.resolveOwnerColumns(st, owners, queryValue{text: column.text, span: column.span}, "view:matrix.cell.attributes", sourceAddress, bind, diagnose)
			}
		}
	}
}

func (r *resolver) resolveFlowRefs(st *moduleState, node *syntax.Node, sourceAddress string, sourceQuery *DeclarationSymbol, bind, diagnose bool) {
	r.resolveRelationTypeStatements(st, node, sourceAddress, "view:flow.relation_types", bind, diagnose)
	for _, member := range nodeChildren(firstNode(node, syntax.NodeBlock)) {
		if queryHead(member) != "lane_by" {
			continue
		}
		args := queryArgumentValues(member)
		if len(args) != 1 || !strings.HasPrefix(args[0].text, "attribute.") {
			continue
		}
		column := strings.TrimPrefix(args[0].text, "attribute.")
		span := args[0].span
		owners := r.visibleOwners(st, KindEntityType, sourceQuery)
		r.resolveOwnerColumns(st, owners, queryValue{text: column, span: span}, "view:flow.lane_by", sourceAddress, bind, diagnose)
	}
}

func (r *resolver) resolveRelationTypeStatements(st *moduleState, node *syntax.Node, sourceAddress, via string, bind, diagnose bool) {
	for _, member := range nodeChildren(firstNode(node, syntax.NodeBlock)) {
		if queryHead(member) == "relation_types" {
			r.resolveViewList(st, member, KindRelationType, via, sourceAddress, bind, diagnose)
		}
	}
}

func (r *resolver) resolveViewList(st *moduleState, node *syntax.Node, kind SubjectKind, via, sourceAddress string, bind, diagnose bool) []DeclarationSymbol {
	args := queryArgumentValues(node)
	if len(args) != 1 {
		return nil
	}
	return r.resolveViewValueList(st, args[0], kind, via, sourceAddress, bind, diagnose)
}

func (r *resolver) resolveViewValueList(st *moduleState, value queryValue, kind SubjectKind, via, sourceAddress string, bind, diagnose bool) []DeclarationSymbol {
	if !value.list {
		return nil
	}
	var out []DeclarationSymbol
	for _, item := range queryListValues(value.node) {
		decl, ok := r.resolveViewTop(st, kind, queryValue{text: item.text, span: item.span}, via, sourceAddress, bind, diagnose)
		if ok {
			out = append(out, decl)
		}
	}
	return uniqueDeclarations(out)
}

func (r *resolver) resolveViewTop(st *moduleState, kind SubjectKind, value queryValue, via, sourceAddress string, bind, diagnose bool) (DeclarationSymbol, bool) {
	target, ok := r.resolveText(st, kind, value.text)
	if !ok {
		if diagnose {
			r.diag("LDL1301", "unknown_or_ambiguous_symbol", "View source binding is unknown or ambiguous", st.key, value.span)
		}
		return DeclarationSymbol{}, false
	}
	if bind {
		st.addBinding(kind, value.text, value.span, via, target, sourceAddress)
	}
	return target, true
}

func (r *resolver) resolveOwnerColumns(st *moduleState, owners []DeclarationSymbol, value queryValue, via, sourceAddress string, bind, diagnose bool) []DeclarationSymbol {
	var found []DeclarationSymbol
	for _, owner := range owners {
		address := reservationAddress(owner.Symbol, KindColumn, value.text)
		column, ok := r.symbols[address]
		if !ok || column.Kind != KindColumn {
			continue
		}
		found = append(found, column)
		if bind {
			st.addBinding(KindColumn, value.text, value.span, via, column, sourceAddress)
		}
	}
	if len(found) == 0 && diagnose {
		r.diag("LDL1301", "unknown_or_ambiguous_symbol", "View Column is unknown for its owner types", st.key, value.span)
	}
	return uniqueDeclarations(found)
}

func (r *resolver) visibleOwners(st *moduleState, kind SubjectKind, sourceQuery *DeclarationSymbol) []DeclarationSymbol {
	seen := map[string]DeclarationSymbol{}
	selectorAuthored := false
	if sourceQuery != nil {
		queryModule := r.modules[sourceQuery.Module]
		if queryModule != nil {
			selectorVia := querySelectorVia(kind)
			selectorAuthored = querySelectorAuthored(queryModule, sourceQuery.Address, kind)
			for _, binding := range queryModule.bindings {
				if binding.SourceAddress == sourceQuery.Address && binding.ExpectedKind == kind && binding.Via == selectorVia {
					if target, ok := r.symbols[binding.TargetAddress]; ok {
						seen[target.Address] = target
					}
				}
			}
		}
	}
	// An explicitly empty Query selector means no candidate owners. Only an
	// omitted selector is unrestricted and falls back to the View module's
	// visible types. Predicate and traversal bindings never widen this set.
	if len(seen) == 0 && !selectorAuthored {
		for _, decl := range st.localTop[kind] {
			seen[decl.Address] = decl
		}
		for _, decl := range st.imported[kind] {
			seen[decl.Address] = decl
		}
	}
	out := make([]DeclarationSymbol, 0, len(seen))
	for _, decl := range seen {
		out = append(out, decl)
	}
	sortDeclarations(out)
	return out
}

func querySelectorVia(kind SubjectKind) string {
	switch kind {
	case KindEntityType:
		return "query:select.entity_types"
	case KindRelationType:
		return "query:select.relation_types"
	default:
		return ""
	}
}

func querySelectorAuthored(st *moduleState, sourceAddress string, kind SubjectKind) bool {
	var memberName string
	switch kind {
	case KindEntityType:
		memberName = "entity_types"
	case KindRelationType:
		memberName = "relation_types"
	default:
		return false
	}
	for _, declaration := range st.ast.declarations {
		if declaration.kind != KindQuery || declaration.node == nil || st.declarationAddress(declaration) != sourceAddress {
			continue
		}
		body := firstNode(declaration.node, syntax.NodeBlock)
		for _, member := range nodeChildren(body) {
			if queryHead(member) != "select" {
				continue
			}
			for _, selector := range nodeChildren(firstNode(member, syntax.NodeBlock)) {
				if queryHead(selector) == memberName {
					return true
				}
			}
		}
	}
	return false
}

func uniqueDeclarations(values []DeclarationSymbol) []DeclarationSymbol {
	seen := map[string]DeclarationSymbol{}
	for _, value := range values {
		seen[value.Address] = value
	}
	out := make([]DeclarationSymbol, 0, len(seen))
	for _, value := range seen {
		out = append(out, value)
	}
	sort.SliceStable(out, func(i, j int) bool { return compareSymbol(out[i].Symbol, out[j].Symbol) < 0 })
	return out
}
