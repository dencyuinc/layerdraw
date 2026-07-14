// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package index

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

// Build transactionally derives every internal v1 index. Rejection publishes
// diagnostics only; no partially useful SourceMap or SemanticIndex is exposed.
func Build(input Input) Result {
	diagnostics := resolve.CloneDiagnostics(input.Materialized.Diagnostics)
	stages, available := input.Materialized.ValidatedInputSnapshot()
	if input.Materialized.HasErrors || !available || !stagesCoherent(stages) {
		if !input.Materialized.HasErrors {
			diagnostics = append(diagnostics, indexDiagnostic("materialized result has no coherent validated stage snapshot", stages.Resolve.RootAddress))
		}
		resolve.SortDiagnostics(diagnostics)
		return Result{Diagnostics: diagnostics, HasErrors: true}
	}
	materialized := input.Materialized.Snapshot()
	sourceMap, semantic, err := buildIndexes(stages, materialized)
	if err != nil {
		diagnostics = append(diagnostics, indexDiagnostic(err.Error(), stages.Resolve.RootAddress))
		resolve.SortDiagnostics(diagnostics)
		return Result{Diagnostics: diagnostics, HasErrors: true}
	}
	search, err := buildSearchDocuments(materialized, sourceMap, stages.Resolve)
	if err != nil {
		diagnostics = append(diagnostics, indexDiagnostic(err.Error(), stages.Resolve.RootAddress))
		resolve.SortDiagnostics(diagnostics)
		return Result{Diagnostics: diagnostics, HasErrors: true}
	}
	snapshot := Snapshot{SourceMap: sourceMap, SemanticIndex: semantic, SearchDocuments: search}
	return Result{state: &resultState{snapshot: deepClone(snapshot)}, Diagnostics: diagnostics}
}

func stagesCoherent(input materialize.Input) bool {
	return input.Definition.MatchesResolve(input.Resolve) && input.Graph.MatchesResolve(input.Resolve) && input.Query.MatchesResolve(input.Resolve) &&
		input.View.MatchesResolve(input.Resolve) && input.View.ExportRecipes.MatchesResolve(input.Resolve) &&
		!input.Resolve.HasErrors && !input.Definition.HasErrors && !input.Graph.HasErrors && !input.Query.HasErrors && !input.View.HasErrors && !input.View.ExportRecipes.HasErrors
}

func buildIndexes(input materialize.Input, snapshot materialize.Snapshot) (SourceMapV1, SemanticIndexV1, error) {
	sourceMap := SourceMapV1{SchemaVersion: SourceMapSchemaVersion, Files: []SourceFileRecord{}, Subjects: []SourceSubjectRecord{}, Bindings: []SourceBindingRecord{}, Exports: []ExportBindingRecord{}, Assets: []SourceAssetRecord{}}
	semantic := SemanticIndexV1{SchemaVersion: SemanticIndexSchemaVersion, Subjects: []SemanticSubject{}, References: []SemanticReference{}, Children: []OwnerMembers{}, Rows: []OwnerMembers{}, Columns: []OwnerMembers{}, TypeMembership: []OwnerMembers{}, LayerMembership: []OwnerMembers{}, ReferenceIDs: []ReferenceIDRecord{}, Adjacency: []AdjacencyRecord{}, Dependencies: []DependencyRecord{}}

	for _, source := range input.Resolved.SourceFiles {
		digest := sha256.Sum256(source.Bytes)
		sourceMap.Files = append(sourceMap.Files, SourceFileRecord{Origin: source.Origin, ModulePath: source.ModulePath, Digest: "sha256:" + hex.EncodeToString(digest[:]), ByteLength: len(source.Bytes)})
	}
	manifestModules := map[string]ModuleRef{}
	for _, pack := range input.Resolved.SelectedClosure {
		origin := resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: pack.Address}
		digest := sha256.Sum256(pack.Manifest.Bytes)
		sourceMap.Files = append(sourceMap.Files, SourceFileRecord{Origin: origin, ModulePath: pack.Manifest.Path, Digest: "sha256:" + hex.EncodeToString(digest[:]), ByteLength: len(pack.Manifest.Bytes)})
		manifestModules[pack.Address] = ModuleRef{Origin: origin, ModulePath: pack.Manifest.Path}
	}
	sort.Slice(sourceMap.Files, func(i, j int) bool {
		return lessModule(sourceMap.Files[i].Origin, sourceMap.Files[i].ModulePath, sourceMap.Files[j].Origin, sourceMap.Files[j].ModulePath)
	})

	owners := ownersFromHashes(snapshot.Hashes.ChildSets)
	declarations := map[string]resolve.DeclarationSource{}
	for _, declaration := range input.Resolve.DeclarationSources {
		declarations[declaration.Address] = declaration
	}
	modules := map[resolve.ModuleKey]resolve.ResolvedModule{}
	for _, module := range input.Resolve.Modules {
		modules[resolve.ModuleKey{Origin: module.Origin, Path: module.Path}] = module
	}
	subtrees := map[string]string{}
	for _, hash := range snapshot.Hashes.Subtrees {
		subtrees[hash.OwnerAddress] = hash.Hash
	}
	published := map[string]bool{}
	publishedKinds := map[string]materialize.SubjectKind{}
	for _, hash := range snapshot.Hashes.OwnSubjects {
		published[hash.Address] = true
		publishedKinds[hash.Address] = hash.Kind
	}

	for _, hash := range snapshot.Hashes.OwnSubjects {
		owner := optionalString(owners[hash.Address])
		record := SourceSubjectRecord{Address: hash.Address, Kind: hash.Kind, OwnerAddress: owner, CommentRanges: []resolve.SourceRange{}}
		if hash.Kind == materialize.SubjectPack {
			manifest, exists := manifestModules[hash.Address]
			if !exists {
				return SourceMapV1{}, SemanticIndexV1{}, fmt.Errorf("published Pack root %q has no validated manifest source", hash.Address)
			}
			record.Module = &manifest
			rangeValue := resolve.SourceRange{Origin: manifest.Origin, ModulePath: manifest.ModulePath, StartByte: 0, EndByte: sourceFileLength(sourceMap.Files, manifest)}
			record.DeclarationRange = &rangeValue
			record.ManifestRoot = true
		} else if declaration, exists := declarations[hash.Address]; exists {
			moduleRef := moduleRef(input, declaration.Module)
			record.Module = &moduleRef
			rangeValue := sourceRange(moduleRef, declaration.Range)
			record.DeclarationRange = &rangeValue
			record.CommentRanges = commentRanges(modules[declaration.Module], declaration.Range, moduleRef)
		} else {
			return SourceMapV1{}, SemanticIndexV1{}, fmt.Errorf("published subject %q has no declaration source", hash.Address)
		}
		sourceMap.Subjects = append(sourceMap.Subjects, record)
		semanticSubject := SemanticSubject{Address: hash.Address, Kind: hash.Kind, OwnerAddress: owner, Module: cloneModuleRef(record.Module), OwnHash: hash.Hash}
		if value := subtrees[hash.Address]; value != "" {
			semanticSubject.SubtreeHash = &value
		}
		semantic.Subjects = append(semantic.Subjects, semanticSubject)
	}

	for _, binding := range input.Resolve.Bindings {
		if !published[binding.SourceAddress] || !published[binding.TargetAddress] {
			continue
		}
		module := moduleRef(input, binding.Module)
		rangeValue := sourceRange(module, binding.Range)
		targetKind := publishedKinds[binding.TargetAddress]
		sourceMap.Bindings = append(sourceMap.Bindings, SourceBindingRecord{SourceAddress: binding.SourceAddress, TargetAddress: binding.TargetAddress, TargetKind: targetKind, TargetOwnerAddress: binding.TargetOwnerAddress, Via: binding.Via, Module: module, Range: rangeValue})
		semantic.References = append(semantic.References, SemanticReference{SourceAddress: binding.SourceAddress, TargetAddress: binding.TargetAddress, TargetKind: targetKind, Via: binding.Via, Range: rangeValue})
	}
	for _, binding := range input.Resolve.Exports {
		if !published[binding.TargetAddress] {
			continue
		}
		module := moduleRef(input, binding.Module)
		sourceMap.Exports = append(sourceMap.Exports, ExportBindingRecord{PublicName: materialize.NormalizeString(binding.PublicName), TargetAddress: binding.TargetAddress, Module: module, Range: sourceRange(module, binding.Range), ReExport: binding.ReExport})
	}
	assets := map[indexAssetKey]materialize.ResolvedAsset{}
	for _, asset := range input.Resolved.Assets {
		assets[indexAssetKey{Origin: asset.Origin, Locator: asset.Locator}] = asset
	}
	for _, entityType := range input.Definition.EntityTypes {
		if entityType.Image == nil {
			continue
		}
		module := moduleRef(input, resolve.ModuleKey{Origin: entityType.Image.Origin, Path: entityType.Image.ModulePath})
		key := indexAssetKey{Origin: module.Origin, Locator: entityType.Image.Locator}
		asset, exists := assets[key]
		if !exists || entityType.Image.SourceRange == nil {
			return SourceMapV1{}, SemanticIndexV1{}, fmt.Errorf("published asset reference for %q is incomplete", entityType.Address)
		}
		rangeValue := *entityType.Image.SourceRange
		sourceMap.Assets = append(sourceMap.Assets, SourceAssetRecord{SubjectAddress: entityType.Address, AuthoredPath: materialize.NormalizeString(entityType.Image.AuthoredPath), Locator: entityType.Image.Locator, Origin: module.Origin, ModulePath: module.ModulePath, Range: rangeValue, Digest: asset.ExpectedDigest, MediaType: asset.ExpectedMediaType, ByteLength: asset.ExpectedByteLength})
	}

	semantic.Children = ownerMembers(owners, publishedKinds, nil, input.Resolve)
	semantic.Rows = ownerMembers(owners, publishedKinds, map[materialize.SubjectKind]bool{materialize.SubjectEntityRow: true, materialize.SubjectRelationRow: true}, input.Resolve)
	semantic.Columns = ownerMembers(owners, publishedKinds, map[materialize.SubjectKind]bool{materialize.SubjectEntityTypeColumn: true, materialize.SubjectRelationTypeColumn: true}, input.Resolve)
	semantic.TypeMembership, semantic.LayerMembership = memberships(snapshot, input.Resolve)
	semantic.ReferenceIDs = referenceIDs(snapshot, input.Resolve)
	semantic.Adjacency = adjacency(input)
	semantic.Dependencies = dependencies(input)
	sortSourceMap(&sourceMap, input.Resolve)
	sortSemantic(&semantic, input.Resolve)
	semantic.ScopedReads = scopedReads(semantic, input.Resolve)
	return sourceMap, semantic, nil
}

func ownersFromHashes(values []materialize.ChildSetHash) map[string]string {
	out := map[string]string{}
	for _, set := range values {
		for _, address := range set.Addresses {
			out[address] = set.OwnerAddress
		}
	}
	return out
}

type indexAssetKey struct {
	Origin  resolve.SourceOrigin
	Locator string
}

func ownerMembers(owners map[string]string, kinds map[string]materialize.SubjectKind, included map[materialize.SubjectKind]bool, resolved resolve.Result) []OwnerMembers {
	grouped := map[string][]string{}
	for address, owner := range owners {
		if included != nil && !included[kinds[address]] {
			continue
		}
		grouped[owner] = append(grouped[owner], address)
	}
	out := make([]OwnerMembers, 0, len(grouped))
	for owner, addresses := range grouped {
		sortAddresses(resolved, addresses)
		out = append(out, OwnerMembers{OwnerAddress: owner, Addresses: addresses})
	}
	sort.Slice(out, func(i, j int) bool { return lessAddress(resolved, out[i].OwnerAddress, out[j].OwnerAddress) })
	return out
}

func sourceFileLength(files []SourceFileRecord, module ModuleRef) int {
	for _, file := range files {
		if file.Origin == module.Origin && file.ModulePath == module.ModulePath {
			return file.ByteLength
		}
	}
	return 0
}

func memberships(snapshot materialize.Snapshot, resolved resolve.Result) ([]OwnerMembers, []OwnerMembers) {
	byType, byLayer := map[string][]string{}, map[string][]string{}
	if snapshot.Document != nil {
		for _, entity := range snapshot.Document.Entities {
			byType[entity.TypeAddress] = append(byType[entity.TypeAddress], entity.Address)
			byLayer[entity.LayerAddress] = append(byLayer[entity.LayerAddress], entity.Address)
		}
		for _, relation := range snapshot.Document.Relations {
			byType[relation.TypeAddress] = append(byType[relation.TypeAddress], relation.Address)
		}
	}
	return groupedMembers(byType, resolved), groupedMembers(byLayer, resolved)
}

func referenceIDs(snapshot materialize.Snapshot, resolved resolve.Result) []ReferenceIDRecord {
	byID := map[string][]string{}
	if snapshot.Document != nil {
		for _, reference := range snapshot.Document.References {
			byID[reference.ID] = append(byID[reference.ID], reference.Address)
		}
	}
	if snapshot.Pack != nil {
		for _, reference := range snapshot.Pack.References {
			byID[reference.ID] = append(byID[reference.ID], reference.Address)
		}
	}
	out := make([]ReferenceIDRecord, 0, len(byID))
	for id, addresses := range byID {
		sortAddresses(resolved, addresses)
		out = append(out, ReferenceIDRecord{ID: id, Addresses: addresses})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func adjacency(input materialize.Input) []AdjacencyRecord {
	if input.Graph.Graph == nil {
		return []AdjacencyRecord{}
	}
	outgoing, incoming := map[string][]string{}, map[string][]string{}
	for _, item := range input.Graph.Graph.Outgoing {
		outgoing[item.EntityAddress] = append([]string{}, item.RelationAddresses...)
	}
	for _, item := range input.Graph.Graph.Incoming {
		incoming[item.EntityAddress] = append([]string{}, item.RelationAddresses...)
	}
	out := make([]AdjacencyRecord, 0, len(input.Graph.Graph.Entities))
	for _, entity := range input.Graph.Graph.Entities {
		outgoingAddresses := emptySlice(outgoing[entity.Address])
		incomingAddresses := emptySlice(incoming[entity.Address])
		sortAddresses(input.Resolve, outgoingAddresses)
		sortAddresses(input.Resolve, incomingAddresses)
		out = append(out, AdjacencyRecord{EntityAddress: entity.Address, Outgoing: outgoingAddresses, Incoming: incomingAddresses})
	}
	return out
}

func dependencies(input materialize.Input) []DependencyRecord {
	out := []DependencyRecord{}
	for _, recipe := range input.Query.Recipes {
		d := recipe.Dependencies
		out = append(out, DependencyRecord{Kind: DependencyQuery, SubjectAddress: recipe.Address, ParameterAddresses: cloneStrings(d.ParameterAddresses), LayerAddresses: cloneStrings(d.LayerAddresses), EntityTypeAddresses: cloneStrings(d.EntityTypeAddresses), RelationTypeAddresses: cloneStrings(d.RelationTypeAddresses), EntityAddresses: cloneStrings(d.EntityAddresses), RelationAddresses: cloneStrings(d.RelationAddresses), ColumnAddresses: cloneStrings(d.ColumnAddresses), QueryAddresses: []string{}, ExportAddresses: []string{}, StateReads: append([]query.StateReadDependency{}, d.StateReads...)})
	}
	for _, recipe := range input.View.Recipes {
		d := recipe.Dependencies
		out = append(out, DependencyRecord{Kind: DependencyView, SubjectAddress: recipe.Address, QueryAddresses: cloneStrings(d.QueryAddresses), ParameterAddresses: cloneStrings(d.ParameterAddresses), LayerAddresses: cloneStrings(d.LayerAddresses), EntityTypeAddresses: cloneStrings(d.EntityTypeAddresses), RelationTypeAddresses: cloneStrings(d.RelationTypeAddresses), EntityAddresses: cloneStrings(d.EntityAddresses), RelationAddresses: cloneStrings(d.RelationAddresses), ColumnAddresses: cloneStrings(d.ColumnAddresses), ExportAddresses: cloneStrings(d.ExportAddresses), StateReads: append([]query.StateReadDependency{}, d.StateReads...)})
	}
	for index := range out {
		for _, addresses := range [][]string{out[index].QueryAddresses, out[index].ParameterAddresses, out[index].LayerAddresses, out[index].EntityTypeAddresses, out[index].RelationTypeAddresses, out[index].EntityAddresses, out[index].RelationAddresses, out[index].ColumnAddresses, out[index].ExportAddresses} {
			sortAddresses(input.Resolve, addresses)
		}
		out[index].StateReads = query.CanonicalStateReads(out[index].StateReads)
	}
	return out
}

func scopedReads(semantic SemanticIndexV1, resolved resolve.Result) ScopedReadIndexes {
	byModule := map[ModuleRef][]string{}
	byKind := map[materialize.SubjectKind][]string{}
	for _, subject := range semantic.Subjects {
		if subject.Module != nil {
			byModule[*subject.Module] = append(byModule[*subject.Module], subject.Address)
		}
		byKind[subject.Kind] = append(byKind[subject.Kind], subject.Address)
	}
	out := ScopedReadIndexes{ChildrenByOwner: deepClone(semantic.Children), RowsByOwner: deepClone(semantic.Rows), ColumnsByOwner: deepClone(semantic.Columns), MembersByType: deepClone(semantic.TypeMembership), MembersByLayer: deepClone(semantic.LayerMembership), ReferencesByID: deepClone(semantic.ReferenceIDs), OutgoingByEntity: []OwnerMembers{}, IncomingByEntity: []OwnerMembers{}, UsagesByTarget: []OwnerMembers{}, QueriesByDependency: []OwnerMembers{}, ViewsByDependency: []OwnerMembers{}}
	for module, addresses := range byModule {
		sortAddresses(resolved, addresses)
		out.ByModule = append(out.ByModule, ScopeAddresses{Module: module, Addresses: addresses})
	}
	for kind, addresses := range byKind {
		sortAddresses(resolved, addresses)
		out.ByKind = append(out.ByKind, KindAddresses{Kind: kind, Addresses: addresses})
	}
	for _, item := range semantic.Adjacency {
		out.OutgoingByEntity = append(out.OutgoingByEntity, OwnerMembers{OwnerAddress: item.EntityAddress, Addresses: cloneStrings(item.Outgoing)})
		out.IncomingByEntity = append(out.IncomingByEntity, OwnerMembers{OwnerAddress: item.EntityAddress, Addresses: cloneStrings(item.Incoming)})
	}
	usage := map[string][]string{}
	for _, reference := range semantic.References {
		usage[reference.TargetAddress] = append(usage[reference.TargetAddress], reference.SourceAddress)
	}
	queries, views := map[string][]string{}, map[string][]string{}
	for _, dependency := range semantic.Dependencies {
		for _, address := range allDependencies(dependency) {
			if dependency.Kind == DependencyQuery {
				queries[address] = append(queries[address], dependency.SubjectAddress)
			} else {
				views[address] = append(views[address], dependency.SubjectAddress)
			}
		}
	}
	out.UsagesByTarget = groupedMembers(usage, resolved)
	out.QueriesByDependency = groupedMembers(queries, resolved)
	out.ViewsByDependency = groupedMembers(views, resolved)
	sort.Slice(out.ByModule, func(i, j int) bool {
		return lessModule(out.ByModule[i].Module.Origin, out.ByModule[i].Module.ModulePath, out.ByModule[j].Module.Origin, out.ByModule[j].Module.ModulePath)
	})
	sort.Slice(out.ByKind, func(i, j int) bool { return subjectKindRank(out.ByKind[i].Kind) < subjectKindRank(out.ByKind[j].Kind) })
	return out
}

func allDependencies(value DependencyRecord) []string {
	out := []string{}
	for _, values := range [][]string{value.QueryAddresses, value.ParameterAddresses, value.LayerAddresses, value.EntityTypeAddresses, value.RelationTypeAddresses, value.EntityAddresses, value.RelationAddresses, value.ColumnAddresses, value.ExportAddresses} {
		out = append(out, values...)
	}
	return out
}

func groupedMembers(values map[string][]string, resolved resolve.Result) []OwnerMembers {
	out := make([]OwnerMembers, 0, len(values))
	for owner, addresses := range values {
		addresses = dedupe(addresses)
		sortAddresses(resolved, addresses)
		out = append(out, OwnerMembers{OwnerAddress: owner, Addresses: addresses})
	}
	sort.Slice(out, func(i, j int) bool { return lessAddress(resolved, out[i].OwnerAddress, out[j].OwnerAddress) })
	return out
}

func sortSourceMap(value *SourceMapV1, resolved resolve.Result) {
	sort.Slice(value.Subjects, func(i, j int) bool {
		return lessAddress(resolved, value.Subjects[i].Address, value.Subjects[j].Address)
	})
	sort.Slice(value.Bindings, func(i, j int) bool {
		a, b := value.Bindings[i], value.Bindings[j]
		if a.SourceAddress != b.SourceAddress {
			return lessAddress(resolved, a.SourceAddress, b.SourceAddress)
		}
		if a.Range.StartByte != b.Range.StartByte {
			return a.Range.StartByte < b.Range.StartByte
		}
		if a.Range.EndByte != b.Range.EndByte {
			return a.Range.EndByte < b.Range.EndByte
		}
		if a.TargetAddress != b.TargetAddress {
			return lessAddress(resolved, a.TargetAddress, b.TargetAddress)
		}
		if a.TargetKind != b.TargetKind {
			return subjectKindRank(a.TargetKind) < subjectKindRank(b.TargetKind)
		}
		if a.TargetOwnerAddress != b.TargetOwnerAddress {
			return lessAddress(resolved, a.TargetOwnerAddress, b.TargetOwnerAddress)
		}
		return a.Via < b.Via
	})
	sort.Slice(value.Exports, func(i, j int) bool {
		a, b := value.Exports[i], value.Exports[j]
		if lessModule(a.Module.Origin, a.Module.ModulePath, b.Module.Origin, b.Module.ModulePath) {
			return true
		}
		if a.Module != b.Module {
			return false
		}
		if a.Range.StartByte != b.Range.StartByte {
			return a.Range.StartByte < b.Range.StartByte
		}
		if a.Range.EndByte != b.Range.EndByte {
			return a.Range.EndByte < b.Range.EndByte
		}
		if a.PublicName != b.PublicName {
			return a.PublicName < b.PublicName
		}
		if a.TargetAddress != b.TargetAddress {
			return lessAddress(resolved, a.TargetAddress, b.TargetAddress)
		}
		return !a.ReExport && b.ReExport
	})
	sort.Slice(value.Assets, func(i, j int) bool {
		if value.Assets[i].SubjectAddress != value.Assets[j].SubjectAddress {
			return lessAddress(resolved, value.Assets[i].SubjectAddress, value.Assets[j].SubjectAddress)
		}
		return value.Assets[i].Locator < value.Assets[j].Locator
	})
}

func sortSemantic(value *SemanticIndexV1, resolved resolve.Result) {
	sort.Slice(value.Subjects, func(i, j int) bool {
		return lessAddress(resolved, value.Subjects[i].Address, value.Subjects[j].Address)
	})
	sort.Slice(value.References, func(i, j int) bool {
		a, b := value.References[i], value.References[j]
		if a.SourceAddress != b.SourceAddress {
			return lessAddress(resolved, a.SourceAddress, b.SourceAddress)
		}
		if a.Range.StartByte != b.Range.StartByte {
			return a.Range.StartByte < b.Range.StartByte
		}
		if a.Range.EndByte != b.Range.EndByte {
			return a.Range.EndByte < b.Range.EndByte
		}
		if a.TargetAddress != b.TargetAddress {
			return lessAddress(resolved, a.TargetAddress, b.TargetAddress)
		}
		if a.TargetKind != b.TargetKind {
			return subjectKindRank(a.TargetKind) < subjectKindRank(b.TargetKind)
		}
		return a.Via < b.Via
	})
	sort.Slice(value.Adjacency, func(i, j int) bool {
		return lessAddress(resolved, value.Adjacency[i].EntityAddress, value.Adjacency[j].EntityAddress)
	})
	sort.Slice(value.Dependencies, func(i, j int) bool {
		if value.Dependencies[i].SubjectAddress != value.Dependencies[j].SubjectAddress {
			return lessAddress(resolved, value.Dependencies[i].SubjectAddress, value.Dependencies[j].SubjectAddress)
		}
		return value.Dependencies[i].Kind < value.Dependencies[j].Kind
	})
}

func commentRanges(module resolve.ResolvedModule, declaration syntax.Span, ref ModuleRef) []resolve.SourceRange {
	if module.Path == "" {
		return []resolve.SourceRange{}
	}
	out := []resolve.SourceRange{}
	index := len(module.File.Tokens) - 1
	for index >= 0 && module.File.Tokens[index].Span.Start >= declaration.Start {
		index--
	}
	for index >= 0 {
		token := module.File.Tokens[index]
		if token.Kind == syntax.TokenNewline {
			index--
			continue
		}
		if token.Kind != syntax.TokenDocComment {
			break
		}
		out = append(out, sourceRange(ref, token.Span))
		index--
	}
	for left, right := 0, len(out)-1; left < right; left, right = left+1, right-1 {
		out[left], out[right] = out[right], out[left]
	}
	return out
}

func moduleRef(input materialize.Input, module resolve.ModuleKey) ModuleRef {
	origin := resolve.SourceOrigin{Kind: module.Origin.Kind}
	if module.Origin.Kind == resolve.OriginPack {
		canonical := module.Origin.Publisher + "/" + module.Origin.PackName
		if input.Resolve.Mode == resolve.CompilePack && canonical == input.Resolve.RootCanonicalID {
			origin.PackAddress = input.Resolve.RootAddress
		} else {
			for _, dependency := range input.Resolved.SelectedClosure {
				if dependency.CanonicalID == canonical {
					origin.PackAddress = dependency.Address
					break
				}
			}
		}
	}
	return ModuleRef{Origin: origin, ModulePath: module.Path}
}

func sourceRange(module ModuleRef, span syntax.Span) resolve.SourceRange {
	return resolve.SourceRange{Origin: module.Origin, ModulePath: module.ModulePath, StartByte: span.Start, EndByte: span.End}
}
func lessModule(a resolve.SourceOrigin, aPath string, b resolve.SourceOrigin, bPath string) bool {
	if sourceOriginRank(a) != sourceOriginRank(b) {
		return sourceOriginRank(a) < sourceOriginRank(b)
	}
	if a.PackAddress != b.PackAddress {
		return a.PackAddress < b.PackAddress
	}
	return aPath < bPath
}

func sourceOriginRank(origin resolve.SourceOrigin) int {
	if origin.Kind == resolve.OriginPack {
		return 1
	}
	return 0
}
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func cloneModuleRef(value *ModuleRef) *ModuleRef {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}
func emptySlice(value []string) []string {
	if value == nil {
		return []string{}
	}
	return append([]string{}, value...)
}
func cloneStrings(value []string) []string {
	if value == nil {
		return []string{}
	}
	return append([]string{}, value...)
}
func dedupe(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func sortAddresses(result resolve.Result, values []string) {
	sort.SliceStable(values, func(i, j int) bool { return lessAddress(result, values[i], values[j]) })
}
func lessAddress(result resolve.Result, left, right string) bool {
	return materialize.LessStableAddress(result, left, right)
}

func subjectKindRank(kind materialize.SubjectKind) int {
	for index, value := range []materialize.SubjectKind{materialize.SubjectProject, materialize.SubjectPack, materialize.SubjectEntityType, materialize.SubjectRelationType, materialize.SubjectLayer, materialize.SubjectEntity, materialize.SubjectRelation, materialize.SubjectQuery, materialize.SubjectView, materialize.SubjectReference, materialize.SubjectEntityTypeColumn, materialize.SubjectEntityTypeConstraint, materialize.SubjectRelationTypeColumn, materialize.SubjectRelationTypeConstraint, materialize.SubjectEntityRow, materialize.SubjectRelationRow, materialize.SubjectQueryParameter, materialize.SubjectViewTableColumn, materialize.SubjectViewExport} {
		if kind == value {
			return index
		}
	}
	return 99
}
func indexDiagnostic(message, subject string) resolve.Diagnostic {
	return resolve.Diagnostic{Code: "LDL1801", Severity: "error", MessageKey: "generated_index_failed", Arguments: map[string]string{}, Message: message, SubjectAddress: subject}
}
