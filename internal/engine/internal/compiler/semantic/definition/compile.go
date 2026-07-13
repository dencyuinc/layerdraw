// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
	"slices"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func Compile(input Input) Result {
	declarations := append([]resolve.DeclarationSymbol{}, input.Resolve.Declarations...)
	resolve.SortDeclarations(declarations)
	c := compiler{
		input:      input,
		decls:      map[string]resolve.DeclarationSymbol{},
		sources:    map[string]resolve.DeclarationSource{},
		bindings:   map[bindingKey]string{},
		columnDecl: map[string]resolve.DeclarationSymbol{},
	}
	for _, decl := range declarations {
		c.decls[decl.Address] = decl
		if decl.Kind == resolve.KindColumn || decl.Kind == resolve.KindConstraint {
			c.columnDecl[childKey(decl.Owner, decl.Kind, decl.ID)] = decl
		}
	}
	for _, src := range input.Resolve.DeclarationSources {
		c.sources[src.Address] = src
	}
	for _, b := range input.Resolve.Bindings {
		c.bindings[bindingKey{module: b.Module, kind: b.ExpectedKind, text: b.SourceText, start: b.Range.Start, end: b.Range.End}] = b.TargetAddress
	}

	out := Result{
		Root:          Root{Mode: input.Resolve.Mode, Address: input.Resolve.RootAddress},
		Dependencies:  append([]resolve.ResolvedPackSummary{}, input.Resolve.Dependencies...),
		Identity:      input.Resolve.Identity,
		Diagnostics:   append([]resolve.Diagnostic{}, input.Resolve.Diagnostics...),
		HasErrors:     input.Resolve.HasErrors,
		EntityTypes:   []EntityType{},
		RelationTypes: []RelationType{},
		Layers:        []Layer{},
		References:    []Reference{},
	}
	if input.Resolve.Mode == resolve.CompilePack && input.Resolve.RootAddress != "" {
		out.Pack = &Pack{Address: input.Resolve.RootAddress, CanonicalID: input.Resolve.RootCanonicalID}
	}
	for _, d := range declarations {
		src := c.sources[d.Address]
		switch d.Kind {
		case resolve.KindProject:
			project := c.compileProject(d, src)
			out.Project = &project
		case resolve.KindLayer:
			out.Layers = append(out.Layers, c.compileLayer(d, src))
		case resolve.KindEntityType:
			out.EntityTypes = append(out.EntityTypes, c.compileEntityType(d, src))
		case resolve.KindRelationType:
			out.RelationTypes = append(out.RelationTypes, c.compileRelationType(d, src))
		case resolve.KindReference:
			out.References = append(out.References, c.compileReference(d, src))
		}
	}
	out.Diagnostics = append(out.Diagnostics, c.diagnostics...)
	resolve.SortDiagnostics(out.Diagnostics)
	out.HasErrors = out.HasErrors || len(c.diagnostics) > 0
	return out
}

func LayersByDisplayOrder(layers []Layer) []Layer {
	out := append([]Layer{}, layers...)
	slices.SortStableFunc(out, func(a, b Layer) int {
		if a.Order < b.Order {
			return -1
		}
		if a.Order > b.Order {
			return 1
		}
		if a.symbol.Origin.Kind != "" && b.symbol.Origin.Kind != "" {
			if compared := resolve.CompareStableSymbols(a.symbol, b.symbol); compared != 0 {
				return compared
			}
		}
		return strings.Compare(a.Address, b.Address)
	})
	return out
}

type compiler struct {
	input       Input
	decls       map[string]resolve.DeclarationSymbol
	sources     map[string]resolve.DeclarationSource
	bindings    map[bindingKey]string
	columnDecl  map[string]resolve.DeclarationSymbol
	diagnostics []resolve.Diagnostic
}

type bindingKey struct {
	module resolve.ModuleKey
	kind   resolve.SubjectKind
	text   string
	start  int
	end    int
}

func (c *compiler) compileProject(d resolve.DeclarationSymbol, src resolve.DeclarationSource) Project {
	toks := directTokens(src.Node)
	p := Project{ID: d.ID, Address: d.Address, Common: Common{Annotations: map[string]string{}}}
	if len(toks) > 2 {
		p.DisplayName = normalizeString(tokenString(toks[2]))
	}
	body := c.body(src)
	c.rejectUnknown(body, src, commonSpec())
	p.Common = c.common(body, src, d.Address, "")
	return p
}

func (c *compiler) compileLayer(d resolve.DeclarationSymbol, src resolve.DeclarationSource) Layer {
	toks := directTokens(src.Node)
	l := Layer{ID: d.ID, Address: d.Address, symbol: d.Symbol, Common: Common{Annotations: map[string]string{}}}
	if len(toks) > 1 {
		l.DisplayName = normalizeString(tokenString(toks[1]))
	}
	if len(toks) > 3 {
		order, err := strconv.ParseInt(toks[3].Raw, 10, 64)
		if err != nil {
			c.diag("LDL1401", "scalar_or_column_type_mismatch", src, toks[3].Span, "layer order out of range", d.Address, "")
		} else {
			l.Order = order
		}
	}
	body := c.body(src)
	c.rejectUnknown(body, src, commonSpec())
	l.Common = c.common(body, src, d.Address, "")
	return l
}

func (c *compiler) compileEntityType(d resolve.DeclarationSymbol, src resolve.DeclarationSource) EntityType {
	toks := directTokens(src.Node)
	e := EntityType{ID: d.ID, Address: d.Address, Common: Common{Annotations: map[string]string{}}}
	if len(toks) > 2 {
		e.DisplayName = normalizeString(tokenString(toks[2]))
	}
	body := c.body(src)
	c.rejectUnknown(body, src, entitySpec())
	e.Common = c.common(body, src, d.Address, "")
	e.Icon = c.optionalString(body, "icon", src, d.Address, "")
	e.Image = c.optionalAsset(body, "image", src, d.Address)
	e.Color = c.optionalColor(body, "color", src, d.Address, "")
	e.Representation = c.representation(body, src, d.Address, "")
	e.Columns = c.columns(body.block("columns"), d, src)
	e.UniqueConstraints = c.uniques(body, d, src, e.Columns)
	e.ReservedColumnIDs, e.ReservedConstraintIDs = c.childReservations(d.Symbol)
	return e
}

func (c *compiler) compileRelationType(d resolve.DeclarationSymbol, src resolve.DeclarationSource) RelationType {
	toks := directTokens(src.Node)
	r := RelationType{
		ID:              d.ID,
		Address:         d.Address,
		Common:          Common{Annotations: map[string]string{}},
		AllowSelf:       false,
		DuplicatePolicy: DuplicateDenySameTypeBetweenSameEndpoints,
		Cardinality: Cardinality{
			ToPerFrom: CardinalityBound{Min: 0, Max: CardinalityMaximumMany},
			FromPerTo: CardinalityBound{Min: 0, Max: CardinalityMaximumMany},
		},
		Traversal:   TraversalPolicy{DefaultDirection: TraversalOutgoing},
		Projections: defaultProjections(),
		Render:      defaultRender(),
		Export:      RelationExport{IncludeEndpoints: true, IncludeRelationRows: true},
	}
	if len(toks) > 2 {
		r.DisplayName = normalizeString(tokenString(toks[2]))
	}
	if len(toks) > 3 {
		if !semanticKinds[toks[3].Raw] {
			c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", src, toks[3].Span, "invalid semantic kind", d.Address, "")
		} else {
			r.SemanticKind = RelationSemanticKind(toks[3].Raw)
		}
	}
	body := c.body(src)
	c.rejectUnknown(body, src, relationSpec())
	r.Common = c.common(body, src, d.Address, "")
	r.AllowSelf = c.optionalBoolDefault(body, "allow_self", false, src, d.Address, "", "LDL1501", "invalid_relation_endpoint_or_self_rule")
	r.DuplicatePolicy = DuplicatePolicy(c.optionalEnumDefault(body, "duplicate_policy", string(r.DuplicatePolicy), duplicatePolicies, src, d.Address, "", "LDL1501", "invalid_relation_endpoint_or_self_rule"))
	r.From = c.endpoint(body.stmt("from"), resolve.KindEntityType, d, src)
	r.To = c.endpoint(body.stmt("to"), resolve.KindEntityType, d, src)
	r.Cardinality = c.cardinality(body.block("cardinality"), r.Cardinality, src, d.Address)
	r.ForwardLabel = c.requiredString(body, "label", src, d.Address, "", "LDL1501", "invalid_relation_endpoint_or_self_rule")
	r.ReverseLabel = c.optionalString(body, "reverse", src, d.Address, "")
	r.Projections.Context = defaultContext(r.ForwardLabel, r.ReverseLabel)
	contextRanges := defaultContextRanges(body, src)
	r.Columns = c.columns(body.block("columns"), d, src)
	r.UniqueConstraints = c.uniques(body, d, src, r.Columns)
	r.Traversal = c.traversal(body.block("traversal"), r.Traversal, src, d.Address)
	if !c.projections(body.blocksByHead("projection"), &r, src, &contextRanges) {
		c.validateContext(r.Projections.Context, contextRanges, src, r.Address)
	}
	c.render(body.blocksByHead("render"), &r, src)
	r.Export = c.export(body.block("export"), r.Export, src, d.Address)
	r.ReservedColumnIDs, r.ReservedConstraintIDs = c.childReservations(d.Symbol)
	return r
}

func (c *compiler) compileReference(d resolve.DeclarationSymbol, src resolve.DeclarationSource) Reference {
	ref := Reference{ID: d.ID, Address: d.Address}
	for _, tok := range nodeTokens(src.Node) {
		if tok.Kind == syntax.TokenHeredoc {
			ref.Text = heredocText(tok.Raw)
			break
		}
	}
	return ref
}

type fieldCardinality uint8

const (
	singleton fieldCardinality = iota
	repeated
)

type fieldSpec struct {
	card   fieldCardinality
	nested bool
	either bool
}

var (
	semanticKinds     = set("dependency", "data_flow", "control_flow", "deployment", "network", "security", "containment", "ownership", "sequence", "impact", "reference", "governance")
	duplicatePolicies = set("allow", "deny_same_type_between_same_endpoints", "deny_any_between_same_endpoints")
)

func entitySpec() map[string]fieldSpec {
	return map[string]fieldSpec{
		"description": {card: singleton}, "tags": {card: singleton}, "annotations": {card: singleton, either: true},
		"icon": {card: singleton}, "image": {card: singleton}, "color": {card: singleton}, "representation": {card: singleton},
		"columns": {card: singleton, nested: true}, "unique": {card: repeated}, "reserve": {card: singleton, nested: true},
	}
}

func commonSpec() map[string]fieldSpec {
	return map[string]fieldSpec{"description": {card: singleton}, "tags": {card: singleton}, "annotations": {card: singleton, either: true}}
}

func specs(names ...string) map[string]fieldSpec {
	out := map[string]fieldSpec{}
	for _, name := range names {
		out[name] = fieldSpec{card: singleton}
	}
	return out
}

func relationSpec() map[string]fieldSpec {
	m := commonSpec()
	m["columns"] = fieldSpec{card: singleton, nested: true}
	m["unique"] = fieldSpec{card: repeated}
	m["reserve"] = fieldSpec{card: singleton, nested: true}
	for _, k := range []string{"allow_self", "duplicate_policy", "from", "to", "label", "reverse"} {
		m[k] = fieldSpec{card: singleton}
	}
	for _, k := range []string{"cardinality", "traversal", "export"} {
		m[k] = fieldSpec{card: singleton, nested: true}
	}
	m["projection"] = fieldSpec{card: repeated, nested: true}
	m["render"] = fieldSpec{card: repeated, nested: true}
	return m
}

func defaultProjections() ProjectionSet {
	return ProjectionSet{
		Composed: ComposedProjection{Mode: ComposedEdge, Priority: 0, Conflict: ProjectionConflictDiagnostic, KeepEdge: true},
		Diagram:  DiagramProjection{Mode: DiagramEdge, SourceEndpoint: ProjectionEndpointFrom, TargetEndpoint: ProjectionEndpointTo, EdgeLabel: ProjectionLabelForwardLabel, IncludeRelationType: false},
		Table:    TableProjection{RowMode: TableRowsAutomatic, IncludeFrom: true, IncludeTo: true, IncludeRelationType: true},
		Context:  ContextProjection{FactTemplate: "{from.display_name} {to.display_name}", IncludeAttributeRows: false},
	}
}

func defaultContext(forward string, reverse *string) ContextProjection {
	context := ContextProjection{FactTemplate: "{from.display_name} " + forward + " {to.display_name}"}
	if reverse != nil {
		value := "{to.display_name} " + *reverse + " {from.display_name}"
		context.ReverseFactTemplate = &value
	}
	return context
}

func defaultRender() RenderSet {
	return RenderSet{
		Edge:    EdgeRender{Arrow: RenderArrowForward, Line: RenderLineSolid, Label: ProjectionLabelForwardLabel},
		Nested:  NestedRender{FrameLabel: RenderFrameLabelParent, FrameStyle: RenderFrameSubtle},
		Overlay: OverlayRender{Kind: "badge", Position: RenderPositionTopRight, MaxItems: 4},
		Badge:   BadgeRender{Label: RenderBadgeLabelCount, Position: RenderPositionTopRight},
	}
}
