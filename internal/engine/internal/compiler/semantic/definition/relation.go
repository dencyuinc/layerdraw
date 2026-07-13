// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func (c *compiler) endpoint(it *item, entityKind resolve.SubjectKind, owner resolve.DeclarationSymbol, src resolve.DeclarationSource) EndpointRule {
	var ep EndpointRule
	if it == nil || len(it.args) == 0 {
		c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", src, src.Range, "missing endpoint", owner.Address, "")
		return ep
	}
	if it.args[0].kind != syntax.TokenIdentifier {
		c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", src, it.args[0].span, "invalid endpoint role", owner.Address, "")
		return ep
	}
	ep.Role = it.args[0].raw
	seenSelectors := map[string]syntax.Span{}
	seenAddresses := map[string]syntax.Span{}
	lastSelectorRank := -1
	for i := 1; i < len(it.args); i++ {
		selector := it.args[i]
		if selector.kind != syntax.TokenIdentifier || (selector.raw != "types" && selector.raw != "layers") {
			c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", src, selector.span, "invalid endpoint selector", owner.Address, "")
			continue
		}
		if prev, duplicate := seenSelectors[selector.raw]; duplicate {
			c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", src, selector.span, "duplicate endpoint selector", owner.Address, "", prev)
		}
		rank := 0
		if selector.raw == "layers" {
			rank = 1
		}
		if rank < lastSelectorRank {
			c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", src, selector.span, "endpoint selectors out of order", owner.Address, "")
		}
		lastSelectorRank = rank
		seenSelectors[selector.raw] = selector.span
		if i+1 >= len(it.args) || firstNode(it.args[i+1].node, syntax.NodeList) == nil {
			c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", src, selector.span, "endpoint selector requires a list", owner.Address, "")
			continue
		}
		i++
		arg := it.args[i]
		values := listValues(arg.node)
		if len(values) == 0 {
			c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", src, arg.span, "empty endpoint selector", owner.Address, "")
			continue
		}
		for _, v := range values {
			if v.kind != syntax.TokenIdentifier {
				c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", src, v.span, "invalid endpoint reference", owner.Address, "")
				continue
			}
			kind := entityKind
			if selector.raw == "layers" {
				kind = resolve.KindLayer
			}
			addr, ok := c.binding(src.Module, kind, v.raw, v.span)
			if !ok {
				c.diag("LDL1301", "unknown_or_ambiguous_symbol", src, v.span, "unresolved endpoint selector", owner.Address, "")
				continue
			}
			if prev, duplicate := seenAddresses[string(kind)+"|"+addr]; duplicate {
				c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", src, v.span, "duplicate endpoint reference", owner.Address, "", prev)
				continue
			}
			seenAddresses[string(kind)+"|"+addr] = v.span
			if selector.raw == "types" {
				ep.EntityTypeAddresses = append(ep.EntityTypeAddresses, addr)
			} else {
				ep.LayerAddresses = append(ep.LayerAddresses, addr)
			}
		}
	}
	ep.EntityTypeAddresses = canonicalSet(ep.EntityTypeAddresses)
	ep.LayerAddresses = canonicalSet(ep.LayerAddresses)
	return ep
}

func (c *compiler) binding(module resolve.ModuleKey, kind resolve.SubjectKind, text string, span syntax.Span) (string, bool) {
	addr, ok := c.bindings[bindingKey{module: module, kind: kind, text: text, start: span.Start, end: span.End}]
	return addr, ok
}

func (c *compiler) cardinality(it *item, def Cardinality, src resolve.DeclarationSource, subject string) Cardinality {
	if it == nil {
		return def
	}
	spec := specs("to_per_from", "from_per_to")
	c.rejectUnknown(it.nested, src, spec)
	for _, stmt := range it.nested.items {
		if len(stmt.args) != 1 {
			c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", src, stmt.span, "invalid cardinality", subject, "")
			continue
		}
		b, ok := cardinalityBound(stmt.args[0])
		if !ok {
			c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", src, stmt.span, "invalid cardinality", subject, "")
			continue
		}
		if stmt.name == "to_per_from" {
			def.ToPerFrom = b
		} else if stmt.name == "from_per_to" {
			def.FromPerTo = b
		}
	}
	return def
}

func cardinalityBound(v value) (CardinalityBound, bool) {
	toks := nodeTokens(v.node)
	if len(toks) != 3 || toks[1].Kind != syntax.TokenDotDot {
		return CardinalityBound{}, false
	}
	min, err := strconv.Atoi(toks[0].Raw)
	if err != nil || (min != 0 && min != 1) {
		return CardinalityBound{}, false
	}
	max := toks[2].Raw
	if max == "*" {
		max = "many"
	} else if max != "1" {
		return CardinalityBound{}, false
	}
	return CardinalityBound{Min: min, Max: max}, true
}

func (c *compiler) traversal(it *item, def TraversalPolicy, src resolve.DeclarationSource, subject string) TraversalPolicy {
	if it == nil {
		return def
	}
	spec := specs("default_direction", "participates_in_impact", "participates_in_flow", "participates_in_hierarchy", "participates_in_dependency_matrix")
	c.rejectUnknown(it.nested, src, spec)
	def.DefaultDirection = c.optionalEnumDefault(it.nested, "default_direction", def.DefaultDirection, set("outgoing", "incoming", "both"), src, subject, "", "LDL1501", "invalid_relation_endpoint_or_self_rule")
	def.ParticipatesInImpact = c.optionalBoolDefault(it.nested, "participates_in_impact", def.ParticipatesInImpact, src, subject, "", "LDL1501", "invalid_relation_endpoint_or_self_rule")
	def.ParticipatesInFlow = c.optionalBoolDefault(it.nested, "participates_in_flow", def.ParticipatesInFlow, src, subject, "", "LDL1501", "invalid_relation_endpoint_or_self_rule")
	def.ParticipatesInHierarchy = c.optionalBoolDefault(it.nested, "participates_in_hierarchy", def.ParticipatesInHierarchy, src, subject, "", "LDL1501", "invalid_relation_endpoint_or_self_rule")
	def.ParticipatesInDependencyMatrix = c.optionalBoolDefault(it.nested, "participates_in_dependency_matrix", def.ParticipatesInDependencyMatrix, src, subject, "", "LDL1501", "invalid_relation_endpoint_or_self_rule")
	return def
}

func (c *compiler) projections(items []item, r *RelationType, src resolve.DeclarationSource) {
	for _, it := range items {
		if len(it.args) != 1 || !it.block {
			continue
		}
		switch it.args[0].raw {
		case "composed":
			c.rejectUnknown(it.nested, src, specs("mode", "parent_endpoint", "child_endpoint", "overlay_endpoint", "target_endpoint", "badge_endpoint", "priority", "conflict", "keep_edge"))
			r.Projections.Composed.Mode = c.optionalEnumDefault(it.nested, "mode", r.Projections.Composed.Mode, set("edge", "nest", "overlay", "badge", "hide"), src, r.Address, "", "LDL1504", "invalid_projection_contract")
			r.Projections.Composed.Priority = c.optionalIntDefault(it.nested, "priority", r.Projections.Composed.Priority, src, r.Address)
			r.Projections.Composed.Conflict = c.optionalEnumDefault(it.nested, "conflict", r.Projections.Composed.Conflict, set("keep_edge", "prefer_first", "diagnostic"), src, r.Address, "", "LDL1504", "invalid_projection_contract")
			r.Projections.Composed.KeepEdge = c.optionalBoolDefault(it.nested, "keep_edge", r.Projections.Composed.KeepEdge, src, r.Address, "", "LDL1504", "invalid_projection_contract")
			r.Projections.Composed.ParentEndpoint = c.endpointField(it.nested, "parent_endpoint", src, r.Address)
			r.Projections.Composed.ChildEndpoint = c.endpointField(it.nested, "child_endpoint", src, r.Address)
			r.Projections.Composed.OverlayEndpoint = c.endpointField(it.nested, "overlay_endpoint", src, r.Address)
			r.Projections.Composed.TargetEndpoint = c.endpointField(it.nested, "target_endpoint", src, r.Address)
			r.Projections.Composed.BadgeEndpoint = c.endpointField(it.nested, "badge_endpoint", src, r.Address)
			c.validateComposed(r.Projections.Composed, src, it.span, r.Address)
		case "diagram":
			c.rejectUnknown(it.nested, src, specs("mode", "source_endpoint", "target_endpoint", "edge_label", "include_relation_type"))
			r.Projections.Diagram.Mode = c.optionalEnumDefault(it.nested, "mode", r.Projections.Diagram.Mode, set("edge", "hide"), src, r.Address, "", "LDL1504", "invalid_projection_contract")
			r.Projections.Diagram.SourceEndpoint = c.optionalEnumDefault(it.nested, "source_endpoint", r.Projections.Diagram.SourceEndpoint, endpointSet(), src, r.Address, "", "LDL1504", "invalid_projection_contract")
			r.Projections.Diagram.TargetEndpoint = c.optionalEnumDefault(it.nested, "target_endpoint", r.Projections.Diagram.TargetEndpoint, endpointSet(), src, r.Address, "", "LDL1504", "invalid_projection_contract")
			r.Projections.Diagram.EdgeLabel = c.optionalLabelDefault(it.nested, "edge_label", r.Projections.Diagram.EdgeLabel, r, src)
			r.Projections.Diagram.IncludeRelationType = c.optionalBoolDefault(it.nested, "include_relation_type", r.Projections.Diagram.IncludeRelationType, src, r.Address, "", "LDL1504", "invalid_projection_contract")
			c.distinctEndpoints(r.Projections.Diagram.SourceEndpoint, r.Projections.Diagram.TargetEndpoint, src, it.span, r.Address)
		case "table":
			c.rejectUnknown(it.nested, src, specs("row_mode", "include_from", "include_to", "include_relation_type"))
			r.Projections.Table.RowMode = c.optionalEnumDefault(it.nested, "row_mode", r.Projections.Table.RowMode, set("relation", "relation_rows", "automatic"), src, r.Address, "", "LDL1504", "invalid_projection_contract")
			r.Projections.Table.IncludeFrom = c.optionalBoolDefault(it.nested, "include_from", r.Projections.Table.IncludeFrom, src, r.Address, "", "LDL1504", "invalid_projection_contract")
			r.Projections.Table.IncludeTo = c.optionalBoolDefault(it.nested, "include_to", r.Projections.Table.IncludeTo, src, r.Address, "", "LDL1504", "invalid_projection_contract")
			r.Projections.Table.IncludeRelationType = c.optionalBoolDefault(it.nested, "include_relation_type", r.Projections.Table.IncludeRelationType, src, r.Address, "", "LDL1504", "invalid_projection_contract")
		case "matrix":
			c.rejectUnknown(it.nested, src, specs("row_endpoint", "column_endpoint", "include_relation_rows"))
			m := MatrixProjection{
				RowEndpoint:         c.requiredEndpoint(it.nested, "row_endpoint", src, r.Address),
				ColumnEndpoint:      c.requiredEndpoint(it.nested, "column_endpoint", src, r.Address),
				IncludeRelationRows: c.requiredBool(it.nested, "include_relation_rows", src, r.Address),
			}
			c.distinctEndpoints(m.RowEndpoint, m.ColumnEndpoint, src, it.span, r.Address)
			r.Projections.Matrix = &m
		case "tree":
			c.rejectUnknown(it.nested, src, specs("parent_endpoint", "child_endpoint"))
			t := TreeProjection{ParentEndpoint: c.requiredEndpoint(it.nested, "parent_endpoint", src, r.Address), ChildEndpoint: c.requiredEndpoint(it.nested, "child_endpoint", src, r.Address)}
			c.distinctEndpoints(t.ParentEndpoint, t.ChildEndpoint, src, it.span, r.Address)
			r.Projections.Tree = &t
		case "flow":
			c.rejectUnknown(it.nested, src, specs("source_endpoint", "target_endpoint", "connector_kind", "branch_value_column"))
			f := FlowProjection{SourceEndpoint: c.requiredEndpoint(it.nested, "source_endpoint", src, r.Address), TargetEndpoint: c.requiredEndpoint(it.nested, "target_endpoint", src, r.Address), ConnectorKind: c.optionalEnumDefault(it.nested, "connector_kind", "", set("sequence", "control", "data", "message", "error"), src, r.Address, "", "LDL1504", "invalid_projection_contract")}
			if f.ConnectorKind == "" {
				c.diag("LDL1504", "invalid_projection_contract", src, it.span, "missing connector kind", r.Address, "")
			}
			if bv := it.nested.stmt("branch_value_column"); bv != nil {
				if len(bv.args) != 1 || bv.args[0].kind != syntax.TokenIdentifier {
					c.diag("LDL1504", "invalid_projection_contract", src, bv.span, "invalid branch column reference", r.Address, "")
				} else if addr := columnAddress(r.Columns, bv.args[0].raw); addr != "" {
					f.BranchValueColumnAddress = &addr
				} else {
					c.diag("LDL1301", "unknown_or_ambiguous_symbol", src, bv.span, "unknown branch column", r.Address, "")
				}
			}
			c.distinctEndpoints(f.SourceEndpoint, f.TargetEndpoint, src, it.span, r.Address)
			r.Projections.Flow = &f
		case "context":
			c.rejectUnknown(it.nested, src, specs("fact_template", "reverse_fact_template", "include_attribute_rows"))
			if value := c.optionalString(it.nested, "fact_template", src, r.Address, ""); value != nil {
				r.Projections.Context.FactTemplate = *value
			}
			if value := c.optionalString(it.nested, "reverse_fact_template", src, r.Address, ""); value != nil {
				r.Projections.Context.ReverseFactTemplate = value
			}
			r.Projections.Context.IncludeAttributeRows = c.optionalBoolDefault(it.nested, "include_attribute_rows", r.Projections.Context.IncludeAttributeRows, src, r.Address, "", "LDL1504", "invalid_projection_contract")
			c.contextPlaceholders(r.Projections.Context.FactTemplate, src, it.span, r.Address)
			if r.Projections.Context.ReverseFactTemplate != nil {
				c.contextPlaceholders(*r.Projections.Context.ReverseFactTemplate, src, it.span, r.Address)
			}
		default:
			c.diag("LDL1504", "invalid_projection_contract", src, it.span, "unknown projection primitive", r.Address, "")
		}
	}
}

func (c *compiler) render(items []item, r *RelationType, src resolve.DeclarationSource) {
	for _, it := range items {
		if len(it.args) != 1 || !it.block {
			continue
		}
		switch it.args[0].raw {
		case "edge":
			c.rejectUnknown(it.nested, src, specs("arrow", "line", "color", "label"))
			r.Render.Edge.Arrow = c.optionalEnumDefault(it.nested, "arrow", r.Render.Edge.Arrow, set("forward", "backward", "both", "none"), src, r.Address, "", "LDL1504", "invalid_projection_contract")
			r.Render.Edge.Line = c.optionalEnumDefault(it.nested, "line", r.Render.Edge.Line, set("solid", "dashed", "dotted"), src, r.Address, "", "LDL1504", "invalid_projection_contract")
			r.Render.Edge.Label = c.optionalLabelDefault(it.nested, "label", r.Render.Edge.Label, r, src)
			r.Render.Edge.Color = c.optionalColor(it.nested, "color", src, r.Address, "")
		case "nested":
			c.rejectUnknown(it.nested, src, specs("frame_label", "frame_style"))
			r.Render.Nested.FrameLabel = c.optionalEnumDefault(it.nested, "frame_label", r.Render.Nested.FrameLabel, set("parent", "type", "display_name", "none"), src, r.Address, "", "LDL1504", "invalid_projection_contract")
			r.Render.Nested.FrameStyle = c.optionalEnumDefault(it.nested, "frame_style", r.Render.Nested.FrameStyle, set("subtle", "strong", "none"), src, r.Address, "", "LDL1504", "invalid_projection_contract")
		case "overlay":
			c.rejectUnknown(it.nested, src, specs("kind", "position", "max_items"))
			r.Render.Overlay.Kind = c.optionalAtomDefault(it.nested, "kind", r.Render.Overlay.Kind, src, r.Address)
			r.Render.Overlay.Position = c.optionalEnumDefault(it.nested, "position", r.Render.Overlay.Position, set("top_left", "top_right", "bottom_left", "bottom_right", "center"), src, r.Address, "", "LDL1504", "invalid_projection_contract")
			r.Render.Overlay.MaxItems = c.optionalPositiveIntDefault(it.nested, "max_items", r.Render.Overlay.MaxItems, src, r.Address)
		case "badge":
			c.rejectUnknown(it.nested, src, specs("icon", "label", "position"))
			r.Render.Badge.Icon = c.optionalString(it.nested, "icon", src, r.Address, "")
			r.Render.Badge.Label = c.optionalEnumDefault(it.nested, "label", r.Render.Badge.Label, set("type", "display_name", "count", "none"), src, r.Address, "", "LDL1504", "invalid_projection_contract")
			r.Render.Badge.Position = c.optionalEnumDefault(it.nested, "position", r.Render.Badge.Position, set("top_left", "top_right", "bottom_left", "bottom_right"), src, r.Address, "", "LDL1504", "invalid_projection_contract")
		default:
			c.diag("LDL1504", "invalid_projection_contract", src, it.span, "unknown render primitive", r.Address, "")
		}
	}
}

func (c *compiler) export(it *item, def RelationExport, src resolve.DeclarationSource, subject string) RelationExport {
	if it == nil {
		return def
	}
	c.rejectUnknown(it.nested, src, specs("include_endpoints", "include_relation_rows", "sheet_name"))
	def.IncludeEndpoints = c.optionalBoolDefault(it.nested, "include_endpoints", def.IncludeEndpoints, src, subject, "", "LDL1504", "invalid_projection_contract")
	def.IncludeRelationRows = c.optionalBoolDefault(it.nested, "include_relation_rows", def.IncludeRelationRows, src, subject, "", "LDL1504", "invalid_projection_contract")
	def.SheetName = c.optionalString(it.nested, "sheet_name", src, subject, "")
	return def
}

func (c *compiler) optionalIntDefault(b body, name string, def int64, src resolve.DeclarationSource, subject string) int64 {
	it := b.stmt(name)
	if it == nil {
		return def
	}
	if len(it.args) != 1 {
		c.diag("LDL1504", "invalid_projection_contract", src, it.span, "expected integer", subject, "")
		return def
	}
	n, ok := it.args[0].integer()
	if !ok {
		c.diag("LDL1504", "invalid_projection_contract", src, it.span, "expected integer", subject, "")
		return def
	}
	return n
}

func (c *compiler) optionalPositiveIntDefault(b body, name string, def int64, src resolve.DeclarationSource, subject string) int64 {
	n := c.optionalIntDefault(b, name, def, src, subject)
	if n < 1 {
		span := src.Range
		if it := b.stmt(name); it != nil {
			span = it.span
		}
		c.diag("LDL1504", "invalid_projection_contract", src, span, "expected positive integer", subject, "")
		return def
	}
	return n
}

func (c *compiler) optionalAtomDefault(b body, name, def string, src resolve.DeclarationSource, subject string) string {
	it := b.stmt(name)
	if it == nil {
		return def
	}
	if len(it.args) != 1 || (it.args[0].kind != syntax.TokenIdentifier && it.args[0].kind != syntax.TokenString) {
		c.diag("LDL1504", "invalid_projection_contract", src, it.span, "expected atom", subject, "")
		return def
	}
	return it.args[0].string()
}

func (c *compiler) endpointField(b body, name string, src resolve.DeclarationSource, subject string) *string {
	it := b.stmt(name)
	if it == nil {
		return nil
	}
	ep := c.optionalEnumDefault(b, name, "", endpointSet(), src, subject, "", "LDL1504", "invalid_projection_contract")
	if ep == "" {
		return nil
	}
	return &ep
}

func (c *compiler) requiredEndpoint(b body, name string, src resolve.DeclarationSource, subject string) string {
	ep := c.optionalEnumDefault(b, name, "", endpointSet(), src, subject, "", "LDL1504", "invalid_projection_contract")
	if ep == "" {
		c.diag("LDL1504", "invalid_projection_contract", src, src.Range, "missing endpoint", subject, "")
	}
	return ep
}

func (c *compiler) requiredBool(b body, name string, src resolve.DeclarationSource, subject string) bool {
	it := b.stmt(name)
	if it == nil {
		c.diag("LDL1504", "invalid_projection_contract", src, src.Range, "missing required boolean", subject, "")
		return false
	}
	return c.optionalBoolDefault(b, name, false, src, subject, "", "LDL1504", "invalid_projection_contract")
}

func (c *compiler) optionalLabelDefault(b body, name, def string, r *RelationType, src resolve.DeclarationSource) string {
	label := c.optionalEnumDefault(b, name, def, set("type", "display_name", "forward_label", "reverse_label", "none"), src, r.Address, "", "LDL1504", "invalid_projection_contract")
	if label == "reverse_label" && r.ReverseLabel == nil {
		span := src.Range
		if it := b.stmt(name); it != nil {
			span = it.span
		}
		c.diag("LDL1504", "invalid_projection_contract", src, span, "reverse label requires authored reverse", r.Address, "")
	}
	return label
}

func (c *compiler) validateComposed(p ComposedProjection, src resolve.DeclarationSource, span syntax.Span, subject string) {
	switch p.Mode {
	case "nest":
		if p.ParentEndpoint == nil || p.ChildEndpoint == nil || *p.ParentEndpoint == *p.ChildEndpoint {
			c.diag("LDL1504", "invalid_projection_contract", src, span, "invalid nest endpoints", subject, "")
		}
		if p.OverlayEndpoint != nil || p.TargetEndpoint != nil || p.BadgeEndpoint != nil {
			c.diag("LDL1504", "invalid_projection_contract", src, span, "endpoint fields forbidden for nest", subject, "")
		}
	case "overlay":
		if p.OverlayEndpoint == nil || p.TargetEndpoint == nil || *p.OverlayEndpoint == *p.TargetEndpoint {
			c.diag("LDL1504", "invalid_projection_contract", src, span, "invalid overlay endpoints", subject, "")
		}
		if p.ParentEndpoint != nil || p.ChildEndpoint != nil || p.BadgeEndpoint != nil {
			c.diag("LDL1504", "invalid_projection_contract", src, span, "endpoint fields forbidden for overlay", subject, "")
		}
	case "badge":
		if p.BadgeEndpoint == nil || p.TargetEndpoint == nil || *p.BadgeEndpoint == *p.TargetEndpoint {
			c.diag("LDL1504", "invalid_projection_contract", src, span, "invalid badge endpoints", subject, "")
		}
		if p.ParentEndpoint != nil || p.ChildEndpoint != nil || p.OverlayEndpoint != nil {
			c.diag("LDL1504", "invalid_projection_contract", src, span, "endpoint fields forbidden for badge", subject, "")
		}
	case "edge", "hide":
		if p.ParentEndpoint != nil || p.ChildEndpoint != nil || p.OverlayEndpoint != nil || p.TargetEndpoint != nil || p.BadgeEndpoint != nil {
			c.diag("LDL1504", "invalid_projection_contract", src, span, "endpoint fields forbidden", subject, "")
		}
	}
}

func (c *compiler) distinctEndpoints(a, b string, src resolve.DeclarationSource, span syntax.Span, subject string) {
	if a != "" && b != "" && a == b {
		c.diag("LDL1504", "invalid_projection_contract", src, span, "endpoints must be distinct", subject, "")
	}
}

func (c *compiler) contextPlaceholders(template string, src resolve.DeclarationSource, span syntax.Span, subject string) {
	allowed := set("{from.id}", "{from.display_name}", "{from.type}", "{from.layer}", "{to.id}", "{to.display_name}", "{to.type}", "{to.layer}", "{relation.id}", "{relation.display_name}", "{relation.type}")
	pattern := regexp.MustCompile(`\{[^}]+\}`)
	for _, m := range pattern.FindAllString(template, -1) {
		if !allowed[m] {
			c.diag("LDL1504", "invalid_projection_contract", src, span, "unknown context placeholder", subject, "")
		}
	}
	remaining := pattern.ReplaceAllString(template, "")
	if strings.ContainsAny(remaining, "{}") {
		c.diag("LDL1504", "invalid_projection_contract", src, span, "malformed context placeholder", subject, "")
	}
}

func columnAddress(columns []Column, id string) string {
	for _, col := range columns {
		if col.ID == id {
			return col.Address
		}
	}
	return ""
}

func endpointSet() map[string]bool {
	return set("from", "to")
}

func canonicalSet(vals []string) []string {
	sort.Strings(vals)
	out := vals[:0]
	var prev string
	for _, v := range vals {
		if v == prev {
			continue
		}
		out = append(out, v)
		prev = v
	}
	return out
}
