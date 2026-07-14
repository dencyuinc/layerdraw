// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package view

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func (c *compiler) compileProjectionOverrides(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, members []authoredMember) []RelationProjection {
	seen := map[string]syntax.Span{}
	out := []RelationProjection{}
	for _, member := range members {
		if member.head != "relation_projection" {
			continue
		}
		if member.block == nil || len(member.args) != 1 {
			c.diag("LDL1504", "invalid_projection_contract", source, member.span, "relation_projection requires RelationType and body", declaration.Address, "")
			continue
		}
		address, ok := c.singleBindingAt(declaration.Address, resolve.KindRelationType, member.args[0].span, source)
		if !ok {
			continue
		}
		if previous, duplicate := seen[address]; duplicate {
			c.diagRelated("LDL1504", "invalid_projection_contract", source, member.args[0].span, "duplicate RelationType projection override", declaration.Address, "", previous)
			continue
		}
		seen[address] = member.args[0].span
		relationType, exists := c.relationTypes[address]
		if !exists {
			c.diag("LDL1801", "stale_revision_or_semantic_hash", source, member.args[0].span, "RelationType binding is absent from typed definitions", declaration.Address, "")
			continue
		}
		projection := RelationProjection{RelationTypeAddress: address, Projections: cloneProjectionSet(relationType.Projections), Render: cloneRenderSet(relationType.Render)}
		c.mergeProjectionOverride(source, declaration, relationType, member, &projection)
		out = append(out, projection)
	}
	c.sortProjectionOverrides(out)
	return out
}

func (c *compiler) sortProjectionOverrides(values []RelationProjection) {
	for index := 1; index < len(values); index++ {
		for current := index; current > 0 && c.compareAddresses(values[current].RelationTypeAddress, values[current-1].RelationTypeAddress) < 0; current-- {
			values[current], values[current-1] = values[current-1], values[current]
		}
	}
}

func cloneProjectionSet(source definition.ProjectionSet) definition.ProjectionSet {
	out := source
	out.Composed.ParentEndpoint = cloneEndpoint(source.Composed.ParentEndpoint)
	out.Composed.ChildEndpoint = cloneEndpoint(source.Composed.ChildEndpoint)
	out.Composed.OverlayEndpoint = cloneEndpoint(source.Composed.OverlayEndpoint)
	out.Composed.TargetEndpoint = cloneEndpoint(source.Composed.TargetEndpoint)
	out.Composed.BadgeEndpoint = cloneEndpoint(source.Composed.BadgeEndpoint)
	if source.Matrix != nil {
		copy := *source.Matrix
		out.Matrix = &copy
	}
	if source.Tree != nil {
		copy := *source.Tree
		out.Tree = &copy
	}
	if source.Flow != nil {
		copy := *source.Flow
		copy.BranchValueColumnAddress = cloneString(source.Flow.BranchValueColumnAddress)
		out.Flow = &copy
	}
	out.Context.ReverseFactTemplate = cloneString(source.Context.ReverseFactTemplate)
	return out
}

func cloneRenderSet(source definition.RenderSet) definition.RenderSet {
	out := source
	out.Edge.Color = cloneString(source.Edge.Color)
	out.Badge.Icon = cloneString(source.Badge.Icon)
	return out
}

func cloneEndpoint(value *definition.ProjectionEndpoint) *definition.ProjectionEndpoint {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (c *compiler) mergeProjectionOverride(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, relationType definition.RelationType, member authoredMember, projection *RelationProjection) {
	members := readMembers(member.block)
	seen := map[string]authoredMember{}
	for _, primitive := range members {
		key := primitive.head
		if primitive.head == "render" && len(primitive.args) == 1 {
			key += ":" + primitive.args[0].raw
		}
		if previous, duplicate := seen[key]; duplicate {
			c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", source, primitive.headSpan, "duplicate projection primitive override", declaration.Address, "", previous.headSpan)
			continue
		}
		seen[key] = primitive
		if primitive.block == nil {
			c.diag("LDL1504", "invalid_projection_contract", source, primitive.headSpan, "projection primitive override requires a body", declaration.Address, "")
			continue
		}
		if primitive.head != "render" && len(primitive.args) != 0 {
			c.diag("LDL1504", "invalid_projection_contract", source, primitive.span, primitive.head+" override takes no arguments", declaration.Address, "")
			continue
		}
		switch primitive.head {
		case "composed":
			c.mergeComposed(source, declaration, primitive, &projection.Projections.Composed)
		case "diagram":
			c.mergeDiagram(source, declaration, relationType, primitive, &projection.Projections.Diagram)
		case "table":
			c.mergeTableProjection(source, declaration, primitive, &projection.Projections.Table)
		case "matrix":
			c.mergeMatrixProjection(source, declaration, primitive, &projection.Projections.Matrix)
		case "tree":
			c.mergeTreeProjection(source, declaration, primitive, &projection.Projections.Tree)
		case "flow":
			c.mergeFlowProjection(source, declaration, relationType, primitive, &projection.Projections.Flow)
		case "context":
			c.mergeContextProjection(source, declaration, primitive, &projection.Projections.Context)
		case "render":
			c.mergeRender(source, declaration, relationType, primitive, &projection.Render)
		default:
			c.diag("LDL1504", "invalid_projection_contract", source, primitive.headSpan, "unknown projection override primitive", declaration.Address, "")
		}
	}
}

func (c *compiler) mergeComposed(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, primitive authoredMember, target *definition.ComposedProjection) {
	members := readMembers(primitive.block)
	c.validateClosedMembers(source, declaration.Address, "composed projection", members, map[string]memberRule{"mode": {}, "priority": {}, "conflict": {}, "keep_edge": {}, "parent_endpoint": {}, "child_endpoint": {}, "overlay_endpoint": {}, "target_endpoint": {}, "badge_endpoint": {}}, true)
	if member := oneMember(members, "mode"); member != nil {
		target.Mode = definition.ComposedProjectionMode(c.enumMember(source, declaration.Address, *member, string(target.Mode), set("edge", "nest", "overlay", "badge", "hide")))
	}
	if member := oneMember(members, "priority"); member != nil {
		target.Priority = c.integerMember(source, declaration.Address, *member, target.Priority)
	}
	if member := oneMember(members, "conflict"); member != nil {
		target.Conflict = definition.ProjectionConflict(c.enumMember(source, declaration.Address, *member, string(target.Conflict), set("keep_edge", "prefer_first", "diagnostic")))
	}
	if member := oneMember(members, "keep_edge"); member != nil {
		target.KeepEdge = c.booleanMember(source, declaration.Address, *member, target.KeepEdge)
	}
	for name, pointer := range map[string]**definition.ProjectionEndpoint{
		"parent_endpoint": &target.ParentEndpoint, "child_endpoint": &target.ChildEndpoint, "overlay_endpoint": &target.OverlayEndpoint,
		"target_endpoint": &target.TargetEndpoint, "badge_endpoint": &target.BadgeEndpoint,
	} {
		if member := oneMember(members, name); member != nil {
			value := definition.ProjectionEndpoint(c.enumMember(source, declaration.Address, *member, "", set("from", "to")))
			*pointer = &value
		}
	}
	c.validateComposed(source, declaration.Address, primitive.headSpan, *target)
}

func (c *compiler) validateComposed(source resolve.DeclarationSource, subject string, span syntax.Span, value definition.ComposedProjection) {
	distinct := func(a, b *definition.ProjectionEndpoint) bool { return a != nil && b != nil && *a != *b }
	switch value.Mode {
	case definition.ComposedNest:
		if !distinct(value.ParentEndpoint, value.ChildEndpoint) || value.OverlayEndpoint != nil || value.TargetEndpoint != nil || value.BadgeEndpoint != nil {
			c.diag("LDL1504", "invalid_projection_contract", source, span, "invalid effective nest projection endpoints", subject, "")
		}
	case definition.ComposedOverlay:
		if !distinct(value.OverlayEndpoint, value.TargetEndpoint) || value.ParentEndpoint != nil || value.ChildEndpoint != nil || value.BadgeEndpoint != nil {
			c.diag("LDL1504", "invalid_projection_contract", source, span, "invalid effective overlay projection endpoints", subject, "")
		}
	case definition.ComposedBadge:
		if !distinct(value.BadgeEndpoint, value.TargetEndpoint) || value.ParentEndpoint != nil || value.ChildEndpoint != nil || value.OverlayEndpoint != nil {
			c.diag("LDL1504", "invalid_projection_contract", source, span, "invalid effective badge projection endpoints", subject, "")
		}
	case definition.ComposedEdge, definition.ComposedHide:
		if value.ParentEndpoint != nil || value.ChildEndpoint != nil || value.OverlayEndpoint != nil || value.TargetEndpoint != nil || value.BadgeEndpoint != nil {
			c.diag("LDL1504", "invalid_projection_contract", source, span, "endpoint fields are forbidden for effective edge/hide projection", subject, "")
		}
	}
}

func (c *compiler) mergeDiagram(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, relationType definition.RelationType, primitive authoredMember, target *definition.DiagramProjection) {
	members := readMembers(primitive.block)
	c.validateClosedMembers(source, declaration.Address, "diagram projection", members, map[string]memberRule{"mode": {}, "source_endpoint": {}, "target_endpoint": {}, "edge_label": {}, "include_relation_type": {}}, true)
	if member := oneMember(members, "mode"); member != nil {
		target.Mode = definition.DiagramProjectionMode(c.enumMember(source, declaration.Address, *member, string(target.Mode), set("edge", "hide")))
	}
	if member := oneMember(members, "source_endpoint"); member != nil {
		target.SourceEndpoint = definition.ProjectionEndpoint(c.enumMember(source, declaration.Address, *member, string(target.SourceEndpoint), set("from", "to")))
	}
	if member := oneMember(members, "target_endpoint"); member != nil {
		target.TargetEndpoint = definition.ProjectionEndpoint(c.enumMember(source, declaration.Address, *member, string(target.TargetEndpoint), set("from", "to")))
	}
	if member := oneMember(members, "edge_label"); member != nil {
		target.EdgeLabel = definition.ProjectionLabel(c.projectionLabel(source, declaration.Address, relationType, *member, target.EdgeLabel))
	}
	if member := oneMember(members, "include_relation_type"); member != nil {
		target.IncludeRelationType = c.booleanMember(source, declaration.Address, *member, target.IncludeRelationType)
	}
	if target.SourceEndpoint == target.TargetEndpoint {
		c.diag("LDL1504", "invalid_projection_contract", source, primitive.headSpan, "effective Diagram endpoints must differ", declaration.Address, "")
	}
}

func (c *compiler) mergeTableProjection(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, primitive authoredMember, target *definition.TableProjection) {
	members := readMembers(primitive.block)
	c.validateClosedMembers(source, declaration.Address, "table projection", members, map[string]memberRule{"row_mode": {}, "include_from": {}, "include_to": {}, "include_relation_type": {}}, true)
	if member := oneMember(members, "row_mode"); member != nil {
		target.RowMode = definition.TableRowMode(c.enumMember(source, declaration.Address, *member, string(target.RowMode), set("relation", "relation_rows", "automatic")))
	}
	if member := oneMember(members, "include_from"); member != nil {
		target.IncludeFrom = c.booleanMember(source, declaration.Address, *member, target.IncludeFrom)
	}
	if member := oneMember(members, "include_to"); member != nil {
		target.IncludeTo = c.booleanMember(source, declaration.Address, *member, target.IncludeTo)
	}
	if member := oneMember(members, "include_relation_type"); member != nil {
		target.IncludeRelationType = c.booleanMember(source, declaration.Address, *member, target.IncludeRelationType)
	}
}

func (c *compiler) mergeMatrixProjection(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, primitive authoredMember, target **definition.MatrixProjection) {
	value := definition.MatrixProjection{}
	if *target != nil {
		value = **target
	}
	members := readMembers(primitive.block)
	c.validateClosedMembers(source, declaration.Address, "matrix projection", members, map[string]memberRule{"row_endpoint": {}, "column_endpoint": {}, "include_relation_rows": {}}, true)
	if member := oneMember(members, "row_endpoint"); member != nil {
		value.RowEndpoint = definition.ProjectionEndpoint(c.enumMember(source, declaration.Address, *member, string(value.RowEndpoint), set("from", "to")))
	}
	if member := oneMember(members, "column_endpoint"); member != nil {
		value.ColumnEndpoint = definition.ProjectionEndpoint(c.enumMember(source, declaration.Address, *member, string(value.ColumnEndpoint), set("from", "to")))
	}
	if member := oneMember(members, "include_relation_rows"); member != nil {
		value.IncludeRelationRows = c.booleanMember(source, declaration.Address, *member, value.IncludeRelationRows)
	}
	if value.RowEndpoint == "" || value.ColumnEndpoint == "" || value.RowEndpoint == value.ColumnEndpoint {
		c.diag("LDL1504", "invalid_projection_contract", source, primitive.headSpan, "effective Matrix endpoints must be present and distinct", declaration.Address, "")
	}
	*target = &value
}

func (c *compiler) mergeTreeProjection(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, primitive authoredMember, target **definition.TreeProjection) {
	value := definition.TreeProjection{}
	if *target != nil {
		value = **target
	}
	members := readMembers(primitive.block)
	c.validateClosedMembers(source, declaration.Address, "tree projection", members, map[string]memberRule{"parent_endpoint": {}, "child_endpoint": {}}, true)
	if member := oneMember(members, "parent_endpoint"); member != nil {
		value.ParentEndpoint = definition.ProjectionEndpoint(c.enumMember(source, declaration.Address, *member, string(value.ParentEndpoint), set("from", "to")))
	}
	if member := oneMember(members, "child_endpoint"); member != nil {
		value.ChildEndpoint = definition.ProjectionEndpoint(c.enumMember(source, declaration.Address, *member, string(value.ChildEndpoint), set("from", "to")))
	}
	if value.ParentEndpoint == "" || value.ChildEndpoint == "" || value.ParentEndpoint == value.ChildEndpoint {
		c.diag("LDL1504", "invalid_projection_contract", source, primitive.headSpan, "effective Tree endpoints must be present and distinct", declaration.Address, "")
	}
	*target = &value
}

func (c *compiler) mergeFlowProjection(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, relationType definition.RelationType, primitive authoredMember, target **definition.FlowProjection) {
	value := definition.FlowProjection{}
	if *target != nil {
		value = **target
		value.BranchValueColumnAddress = cloneString((*target).BranchValueColumnAddress)
	}
	members := readMembers(primitive.block)
	c.validateClosedMembers(source, declaration.Address, "flow projection", members, map[string]memberRule{"source_endpoint": {}, "target_endpoint": {}, "connector_kind": {}, "branch_value_column": {}}, true)
	if member := oneMember(members, "source_endpoint"); member != nil {
		value.SourceEndpoint = definition.ProjectionEndpoint(c.enumMember(source, declaration.Address, *member, string(value.SourceEndpoint), set("from", "to")))
	}
	if member := oneMember(members, "target_endpoint"); member != nil {
		value.TargetEndpoint = definition.ProjectionEndpoint(c.enumMember(source, declaration.Address, *member, string(value.TargetEndpoint), set("from", "to")))
	}
	if member := oneMember(members, "connector_kind"); member != nil {
		value.ConnectorKind = definition.FlowConnectorKind(c.enumMember(source, declaration.Address, *member, string(value.ConnectorKind), set("sequence", "control", "data", "message", "error")))
	}
	if member := oneMember(members, "branch_value_column"); member != nil {
		if len(member.args) != 1 {
			c.diag("LDL1504", "invalid_projection_contract", source, member.span, "branch_value_column requires one Column", declaration.Address, "")
		} else {
			bindings := c.bindingsAt(declaration.Address, resolve.KindColumn, member.args[0].span)
			for _, binding := range bindings {
				if binding.TargetOwnerAddress == relationType.Address {
					address := binding.TargetAddress
					value.BranchValueColumnAddress = &address
					break
				}
			}
			if value.BranchValueColumnAddress == nil {
				c.diag("LDL1301", "unknown_or_ambiguous_symbol", source, member.args[0].span, "flow branch Column lacks an owner-compatible resolver binding", declaration.Address, "")
			}
		}
	}
	if value.SourceEndpoint == "" || value.TargetEndpoint == "" || value.SourceEndpoint == value.TargetEndpoint || value.ConnectorKind == "" {
		c.diag("LDL1504", "invalid_projection_contract", source, primitive.headSpan, "effective Flow projection requires distinct endpoints and connector kind", declaration.Address, "")
	}
	*target = &value
}

func (c *compiler) mergeContextProjection(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, primitive authoredMember, target *definition.ContextProjection) {
	members := readMembers(primitive.block)
	c.validateClosedMembers(source, declaration.Address, "context projection", members, map[string]memberRule{"fact_template": {}, "reverse_fact_template": {}, "include_attribute_rows": {}}, true)
	if member := oneMember(members, "fact_template"); member != nil {
		value := c.optionalString(source, declaration, *member)
		if value != nil {
			target.FactTemplate = *value
			c.validateContextTemplate(source, declaration.Address, member.args[0].span, *value)
		}
	}
	if member := oneMember(members, "reverse_fact_template"); member != nil {
		value := c.optionalString(source, declaration, *member)
		if value != nil {
			target.ReverseFactTemplate = value
			c.validateContextTemplate(source, declaration.Address, member.args[0].span, *value)
		}
	}
	if member := oneMember(members, "include_attribute_rows"); member != nil {
		target.IncludeAttributeRows = c.booleanMember(source, declaration.Address, *member, target.IncludeAttributeRows)
	}
}

func (c *compiler) mergeRender(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, relationType definition.RelationType, primitive authoredMember, target *definition.RenderSet) {
	if len(primitive.args) != 1 {
		c.diag("LDL1504", "invalid_projection_contract", source, primitive.span, "render override requires one primitive", declaration.Address, "")
		return
	}
	members := readMembers(primitive.block)
	switch primitive.args[0].raw {
	case "edge":
		c.validateClosedMembers(source, declaration.Address, "edge render", members, map[string]memberRule{"arrow": {}, "line": {}, "color": {}, "label": {}}, true)
		if member := oneMember(members, "arrow"); member != nil {
			target.Edge.Arrow = definition.RenderArrow(c.enumMember(source, declaration.Address, *member, string(target.Edge.Arrow), set("forward", "backward", "both", "none")))
		}
		if member := oneMember(members, "line"); member != nil {
			target.Edge.Line = definition.RenderLine(c.enumMember(source, declaration.Address, *member, string(target.Edge.Line), set("solid", "dashed", "dotted")))
		}
		if member := oneMember(members, "color"); member != nil {
			target.Edge.Color = c.colorMember(source, declaration.Address, *member)
		}
		if member := oneMember(members, "label"); member != nil {
			target.Edge.Label = c.projectionLabel(source, declaration.Address, relationType, *member, target.Edge.Label)
		}
	case "nested":
		c.validateClosedMembers(source, declaration.Address, "nested render", members, map[string]memberRule{"frame_label": {}, "frame_style": {}}, true)
		if member := oneMember(members, "frame_label"); member != nil {
			target.Nested.FrameLabel = definition.RenderFrameLabel(c.enumMember(source, declaration.Address, *member, string(target.Nested.FrameLabel), set("parent", "type", "display_name", "none")))
		}
		if member := oneMember(members, "frame_style"); member != nil {
			target.Nested.FrameStyle = definition.RenderFrameStyle(c.enumMember(source, declaration.Address, *member, string(target.Nested.FrameStyle), set("subtle", "strong", "none")))
		}
	case "overlay":
		c.validateClosedMembers(source, declaration.Address, "overlay render", members, map[string]memberRule{"kind": {}, "position": {}, "max_items": {}}, true)
		if member := oneMember(members, "kind"); member != nil {
			if len(member.args) != 1 || member.args[0].kind != syntax.TokenIdentifier && member.args[0].kind != syntax.TokenString {
				c.diag("LDL1504", "invalid_projection_contract", source, member.span, "overlay kind requires one atom", declaration.Address, "")
			} else if member.args[0].kind == syntax.TokenString {
				value, _ := authoredString(member.args[0])
				target.Overlay.Kind = value
			} else {
				target.Overlay.Kind = member.args[0].raw
			}
		}
		if member := oneMember(members, "position"); member != nil {
			target.Overlay.Position = definition.RenderPosition(c.enumMember(source, declaration.Address, *member, string(target.Overlay.Position), set("top_left", "top_right", "bottom_left", "bottom_right", "center")))
		}
		if member := oneMember(members, "max_items"); member != nil {
			target.Overlay.MaxItems = c.positiveIntegerMember(source, declaration.Address, *member, target.Overlay.MaxItems)
		}
	case "badge":
		c.validateClosedMembers(source, declaration.Address, "badge render", members, map[string]memberRule{"icon": {}, "label": {}, "position": {}}, true)
		if member := oneMember(members, "icon"); member != nil {
			target.Badge.Icon = c.optionalString(source, declaration, *member)
		}
		if member := oneMember(members, "label"); member != nil {
			target.Badge.Label = definition.RenderBadgeLabel(c.enumMember(source, declaration.Address, *member, string(target.Badge.Label), set("type", "display_name", "count", "none")))
		}
		if member := oneMember(members, "position"); member != nil {
			target.Badge.Position = definition.RenderPosition(c.enumMember(source, declaration.Address, *member, string(target.Badge.Position), set("top_left", "top_right", "bottom_left", "bottom_right")))
		}
	default:
		c.diag("LDL1504", "invalid_projection_contract", source, primitive.args[0].span, "unknown render override primitive", declaration.Address, "")
	}
}

func (c *compiler) projectionLabel(source resolve.DeclarationSource, subject string, relationType definition.RelationType, member authoredMember, fallback definition.ProjectionLabel) definition.ProjectionLabel {
	label := definition.ProjectionLabel(c.enumMember(source, subject, member, string(fallback), set("type", "display_name", "forward_label", "reverse_label", "none")))
	if label == definition.ProjectionLabelReverseLabel && relationType.ReverseLabel == nil {
		c.diag("LDL1504", "invalid_projection_contract", source, member.span, "reverse_label requires an authored RelationType reverse label", subject, "")
		return fallback
	}
	return label
}

func (c *compiler) booleanMember(source resolve.DeclarationSource, subject string, member authoredMember, fallback bool) bool {
	if member.block != nil || len(member.args) != 1 || member.args[0].kind != syntax.TokenIdentifier || member.args[0].raw != "true" && member.args[0].raw != "false" {
		c.diag("LDL1504", "invalid_projection_contract", source, member.span, member.head+" requires true or false", subject, "")
		return fallback
	}
	return member.args[0].raw == "true"
}

func (c *compiler) integerMember(source resolve.DeclarationSource, subject string, member authoredMember, fallback int64) int64 {
	if member.block != nil || len(member.args) != 1 || member.args[0].kind != syntax.TokenInteger {
		c.diag("LDL1504", "invalid_projection_contract", source, member.span, member.head+" requires an integer", subject, "")
		return fallback
	}
	value, err := strconv.ParseInt(member.args[0].raw, 10, 64)
	if err != nil || value < -(1<<53-1) || value > 1<<53-1 {
		c.diag("LDL1504", "invalid_projection_contract", source, member.args[0].span, member.head+" requires a JSON-safe integer", subject, "")
		return fallback
	}
	return value
}

func (c *compiler) positiveIntegerMember(source resolve.DeclarationSource, subject string, member authoredMember, fallback int64) int64 {
	value := c.integerMember(source, subject, member, fallback)
	if value <= 0 {
		c.diag("LDL1504", "invalid_projection_contract", source, member.span, member.head+" requires a positive integer", subject, "")
		return fallback
	}
	return value
}

func (c *compiler) colorMember(source resolve.DeclarationSource, subject string, member authoredMember) *string {
	if member.block != nil || len(member.args) != 1 {
		c.diag("LDL1504", "invalid_projection_contract", source, member.span, "color requires one string", subject, "")
		return nil
	}
	value, ok := authoredString(member.args[0])
	value = strings.ToUpper(value)
	if !ok || !colorPattern.MatchString(value) {
		c.diag("LDL1504", "invalid_projection_contract", source, member.args[0].span, "invalid canonical color", subject, "")
		return nil
	}
	return &value
}

var colorPattern = regexp.MustCompile(`^#[0-9A-F]{6}([0-9A-F]{2})?$`)

func (c *compiler) validateContextTemplate(source resolve.DeclarationSource, subject string, span syntax.Span, value string) {
	allowed := set("{from.id}", "{from.display_name}", "{from.type}", "{from.layer}", "{to.id}", "{to.display_name}", "{to.type}", "{to.layer}", "{relation.id}", "{relation.display_name}", "{relation.type}")
	pattern := regexp.MustCompile(`\{[^}]+\}`)
	for _, placeholder := range pattern.FindAllString(value, -1) {
		if !allowed[placeholder] {
			c.diag("LDL1504", "invalid_projection_contract", source, span, "unknown Context template placeholder", subject, "")
			return
		}
	}
	if strings.ContainsAny(pattern.ReplaceAllString(value, ""), "{}") {
		c.diag("LDL1504", "invalid_projection_contract", source, span, "malformed Context template placeholder", subject, "")
	}
}
