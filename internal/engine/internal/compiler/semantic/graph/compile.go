// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package graph

import (
	"sort"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

type compiler struct {
	input         Input
	declarations  []resolve.DeclarationSymbol
	sources       map[string]resolve.DeclarationSource
	bindings      map[string][]resolve.SourceBinding
	entityTypes   map[string]definition.EntityType
	relationTypes map[string]definition.RelationType
	layers        map[string]definition.Layer
	rowGroups     map[sourceKey]rowGroup
	entities      []Entity
	relations     []Relation
	entityIndex   map[string]int
	relationIndex map[string]int
	diagnostics   []resolve.Diagnostic
}

// Compile validates and compiles all selected graph facts transactionally. A
// graph is returned only when resolve, definition, row, and graph constraints
// all succeed.
func Compile(input Input) Result {
	diagnostics := append([]resolve.Diagnostic{}, input.Definition.Diagnostics...)
	if len(diagnostics) == 0 {
		diagnostics = append(diagnostics, input.Resolve.Diagnostics...)
	}
	resolve.SortDiagnostics(diagnostics)
	if input.Resolve.HasErrors || input.Definition.HasErrors {
		return Result{Diagnostics: diagnostics, HasErrors: true}
	}

	c := newCompiler(input)
	if input.Definition.Root.Mode != input.Resolve.Mode || input.Definition.Root.Address != input.Resolve.RootAddress {
		c.diag("LDL1301", "unknown_or_ambiguous_symbol", resolve.DeclarationSource{}, syntax.Span{}, "definition result does not match resolve result", input.Resolve.RootAddress, "")
	}
	c.compileEntities()
	c.compileRelations()
	c.compileRows()
	c.validateRelations()

	diagnostics = append(diagnostics, c.diagnostics...)
	resolve.SortDiagnostics(diagnostics)
	if len(c.diagnostics) > 0 {
		return Result{Diagnostics: diagnostics, HasErrors: true}
	}
	graph := c.masterGraph()
	return Result{Graph: &graph, Diagnostics: diagnostics}
}

func newCompiler(input Input) *compiler {
	declarations := append([]resolve.DeclarationSymbol{}, input.Resolve.Declarations...)
	resolve.SortDeclarations(declarations)
	c := &compiler{
		input:         input,
		declarations:  declarations,
		sources:       map[string]resolve.DeclarationSource{},
		bindings:      map[string][]resolve.SourceBinding{},
		entityTypes:   map[string]definition.EntityType{},
		relationTypes: map[string]definition.RelationType{},
		layers:        map[string]definition.Layer{},
		rowGroups:     inspectRowGroups(input.Resolve.Modules),
		entityIndex:   map[string]int{},
		relationIndex: map[string]int{},
	}
	for _, src := range input.Resolve.DeclarationSources {
		c.sources[src.Address] = src
	}
	for _, binding := range input.Resolve.Bindings {
		c.bindings[binding.SourceAddress] = append(c.bindings[binding.SourceAddress], binding)
	}
	for _, entityType := range input.Definition.EntityTypes {
		c.entityTypes[entityType.Address] = entityType
	}
	for _, relationType := range input.Definition.RelationTypes {
		c.relationTypes[relationType.Address] = relationType
	}
	for _, layer := range input.Definition.Layers {
		c.layers[layer.Address] = layer
	}
	return c
}

func (c *compiler) compileEntities() {
	for _, decl := range c.declarations {
		if decl.Kind != resolve.KindEntity {
			continue
		}
		src, ok := c.sources[decl.Address]
		if !ok || src.Node == nil {
			c.diag("LDL1101", "invalid_structure_syntax", src, decl.Range, "missing entity declaration source", decl.Address, "")
			continue
		}
		toks := directTokens(src.Node)
		if len(toks) < 2 || toks[0].Raw != decl.ID || toks[1].Kind != syntax.TokenString {
			c.diag("LDL1101", "invalid_structure_syntax", src, src.Range, "invalid entity identity or display name", decl.Address, "")
			continue
		}
		typeAddress, typeOK := c.singleBinding(decl.Address, resolve.KindEntityType, src)
		layerAddress, layerOK := c.singleBinding(decl.Address, resolve.KindLayer, src)
		if typeOK {
			if _, exists := c.entityTypes[typeAddress]; !exists {
				c.diag("LDL1301", "unknown_or_ambiguous_symbol", src, src.Range, "entity type is not in the typed definition", decl.Address, "")
				typeOK = false
			}
		}
		if layerOK {
			if _, exists := c.layers[layerAddress]; !exists {
				c.diag("LDL1301", "unknown_or_ambiguous_symbol", src, src.Range, "entity layer is not in the typed definition", decl.Address, "")
				layerOK = false
			}
		}
		common, commonDiagnostics := definition.CompileFactCommon(src)
		c.diagnostics = append(c.diagnostics, commonDiagnostics...)
		entity := Entity{
			Common:         common,
			ID:             decl.ID,
			Address:        decl.Address,
			DisplayName:    normalizedStringToken(toks[1]),
			Rows:           []AttributeRow{},
			ReservedRowIDs: c.reservedRows(decl.Symbol),
		}
		if typeOK {
			entity.TypeAddress = typeAddress
		}
		if layerOK {
			entity.LayerAddress = layerAddress
		}
		c.entityIndex[entity.Address] = len(c.entities)
		c.entities = append(c.entities, entity)
	}
}

func (c *compiler) compileRelations() {
	for _, decl := range c.declarations {
		if decl.Kind != resolve.KindRelation {
			continue
		}
		src, ok := c.sources[decl.Address]
		if !ok || src.Node == nil {
			c.diag("LDL1101", "invalid_structure_syntax", src, decl.Range, "missing relation declaration source", decl.Address, "")
			continue
		}
		toks := directTokens(src.Node)
		if len(toks) < 3 || toks[0].Raw != decl.ID {
			c.diag("LDL1101", "invalid_structure_syntax", src, src.Range, "invalid relation identity", decl.Address, "")
			continue
		}
		typeAddress, typeOK := c.singleBinding(decl.Address, resolve.KindRelationType, src)
		if typeOK {
			if _, exists := c.relationTypes[typeAddress]; !exists {
				c.diag("LDL1301", "unknown_or_ambiguous_symbol", src, src.Range, "relation type is not in the typed definition", decl.Address, "")
				typeOK = false
			}
		}
		refs := relationEndpointRefs(src.Node)
		if len(refs) != 2 {
			c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", src, src.Range, "relation must have ordered from and to endpoints", decl.Address, "")
			continue
		}
		fromAddress, fromOK := c.bindingAt(decl.Address, resolve.KindEntity, refs[0].Span, src)
		toAddress, toOK := c.bindingAt(decl.Address, resolve.KindEntity, refs[1].Span, src)
		if fromOK {
			if _, exists := c.entityIndex[fromAddress]; !exists {
				c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", src, refs[0].Span, "from endpoint is not a compiled entity", decl.Address, "")
				fromOK = false
			}
		}
		if toOK {
			if _, exists := c.entityIndex[toAddress]; !exists {
				c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", src, refs[1].Span, "to endpoint is not a compiled entity", decl.Address, "")
				toOK = false
			}
		}
		common, commonDiagnostics := definition.CompileFactCommon(src)
		c.diagnostics = append(c.diagnostics, commonDiagnostics...)
		relation := Relation{
			Common:         common,
			ID:             decl.ID,
			Address:        decl.Address,
			Rows:           []AttributeRow{},
			ReservedRowIDs: c.reservedRows(decl.Symbol),
		}
		for _, tok := range toks[1:] {
			if tok.Kind == syntax.TokenString {
				displayName := normalizedStringToken(tok)
				relation.DisplayName = &displayName
				break
			}
		}
		if typeOK {
			relation.TypeAddress = typeAddress
		}
		if fromOK {
			relation.FromAddress = fromAddress
		}
		if toOK {
			relation.ToAddress = toAddress
		}
		if fromOK && toOK {
			relation.CrossLayer = c.entities[c.entityIndex[fromAddress]].LayerAddress != c.entities[c.entityIndex[toAddress]].LayerAddress
		}
		c.relationIndex[relation.Address] = len(c.relations)
		c.relations = append(c.relations, relation)
	}
}

func (c *compiler) singleBinding(sourceAddress string, kind resolve.SubjectKind, src resolve.DeclarationSource) (string, bool) {
	var matches []resolve.SourceBinding
	for _, binding := range c.bindings[sourceAddress] {
		if binding.ExpectedKind == kind {
			matches = append(matches, binding)
		}
	}
	if len(matches) != 1 {
		c.diag("LDL1301", "unknown_or_ambiguous_symbol", src, src.Range, "expected exactly one resolved source binding", sourceAddress, "")
		return "", false
	}
	return matches[0].TargetAddress, true
}

func (c *compiler) bindingAt(sourceAddress string, kind resolve.SubjectKind, span syntax.Span, src resolve.DeclarationSource) (string, bool) {
	var matches []resolve.SourceBinding
	for _, binding := range c.bindings[sourceAddress] {
		if binding.ExpectedKind == kind && binding.Range == span {
			matches = append(matches, binding)
		}
	}
	if len(matches) != 1 {
		c.diag("LDL1301", "unknown_or_ambiguous_symbol", src, span, "expected exactly one resolved source binding", sourceAddress, "")
		return "", false
	}
	return matches[0].TargetAddress, true
}

func (c *compiler) reservedRows(owner resolve.StableSymbol) []string {
	var out []string
	for _, reservation := range c.input.Resolve.Identity.Reservations {
		if reservation.Kind == resolve.KindRow && resolve.CompareStableSymbols(reservation.Owner, owner) == 0 {
			out = append(out, reservation.ID)
		}
	}
	sort.Strings(out)
	return out
}

func (c *compiler) masterGraph() MasterGraph {
	outgoing := make([]Adjacency, len(c.entities))
	incoming := make([]Adjacency, len(c.entities))
	for i, entity := range c.entities {
		outgoing[i] = Adjacency{EntityAddress: entity.Address, RelationAddresses: []string{}}
		incoming[i] = Adjacency{EntityAddress: entity.Address, RelationAddresses: []string{}}
	}
	for _, relation := range c.relations {
		from := c.entityIndex[relation.FromAddress]
		to := c.entityIndex[relation.ToAddress]
		outgoing[from].RelationAddresses = append(outgoing[from].RelationAddresses, relation.Address)
		incoming[to].RelationAddresses = append(incoming[to].RelationAddresses, relation.Address)
	}
	return MasterGraph{Entities: c.entities, Relations: c.relations, Outgoing: outgoing, Incoming: incoming}
}

func (c *compiler) diag(code, key string, src resolve.DeclarationSource, span syntax.Span, message, subject, owner string) {
	c.diagnostics = append(c.diagnostics, resolve.Diagnostic{
		Code:           code,
		Severity:       "error",
		MessageKey:     key,
		Arguments:      map[string]string{},
		Message:        message,
		Range:          sourceRange(src, span),
		SubjectAddress: subject,
		OwnerAddress:   owner,
	})
}

func (c *compiler) diagRelated(code, key string, src resolve.DeclarationSource, span syntax.Span, message, subject, owner string, previous resolve.DeclarationSource) {
	c.diag(code, key, src, span, message, subject, owner)
	c.diagnostics[len(c.diagnostics)-1].Related = []resolve.DiagnosticRelated{{
		Relation:       "previous",
		Range:          sourceRange(previous, previous.Range),
		SubjectAddress: previous.Address,
		OwnerAddress:   owner,
	}}
}

func sourceRange(src resolve.DeclarationSource, span syntax.Span) *resolve.SourceRange {
	if src.Module.Path == "" {
		return nil
	}
	origin := resolve.SourceOrigin{Kind: src.Module.Origin.Kind}
	if src.Module.Origin.Kind == resolve.OriginPack {
		origin.PackAddress = resolve.StableAddress(resolve.StableSymbol{Origin: src.Module.Origin})
	}
	return &resolve.SourceRange{Origin: origin, ModulePath: src.Module.Path, StartByte: span.Start, EndByte: span.End}
}
