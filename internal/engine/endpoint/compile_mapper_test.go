// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"errors"
	"io"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/exportrecipe"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

func pointer[T any](value T) *T { return &value }

func TestMapDiagnosticFidelityAndInvalidRanges(t *testing.T) {
	input := []engine.Diagnostic{{
		Code: "LDL1201", Severity: "warning", MessageKey: "diagnostic_key", Arguments: map[string]string{"name": "value"}, Message: "message",
		Range:          &resolve.SourceRange{Origin: resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: "ldl:pack:pub:p"}, ModulePath: "pack.ldl", StartByte: 1, EndByte: 2},
		SubjectAddress: "ldl:pack:pub:p:entity_type:e", OwnerAddress: "ldl:pack:pub:p",
		Related: []resolve.DiagnosticRelated{{Relation: "cause", Message: "related", Range: &resolve.SourceRange{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: "document.ldl", StartByte: 3, EndByte: 4}, SubjectAddress: "ldl:project:p", OwnerAddress: "ldl:project:p"}},
	}}
	mapped, err := mapDiagnostics(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(mapped) != 1 || mapped[0].ProtocolVersion != 1 || mapped[0].Range == nil || mapped[0].Range.Origin.PackAddress == nil || mapped[0].Message == nil || len(mapped[0].Related) != 1 || mapped[0].Related[0].Range == nil || mapped[0].Arguments["name"].StringValue == nil {
		t.Fatalf("lossy diagnostic: %+v", mapped)
	}
	input[0].Range.StartByte = 5
	input[0].Range.EndByte = 4
	if _, err := mapDiagnostics(input); err == nil {
		t.Fatal("invalid range accepted")
	}
	input[0].Range = &resolve.SourceRange{Origin: resolve.SourceOrigin{Kind: "unknown"}}
	if _, err := mapDiagnostics(input); err == nil {
		t.Fatal("invalid origin accepted")
	}
}

func TestMapSourceAndSemanticIndexesCoverOptionalFamilies(t *testing.T) {
	projectOrigin := resolve.SourceOrigin{Kind: resolve.OriginProject}
	packOrigin := resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: "ldl:pack:pub:p"}
	projectRange := resolve.SourceRange{Origin: projectOrigin, ModulePath: "document.ldl", StartByte: 0, EndByte: 1}
	packRange := resolve.SourceRange{Origin: packOrigin, ModulePath: "pack.ldl", StartByte: 2, EndByte: 3}
	owner := "ldl:project:p"
	module := index.ModuleRef{Origin: projectOrigin, ModulePath: "document.ldl"}
	source := index.SourceMapV1{SchemaVersion: 1,
		Files:    []index.SourceFileRecord{{Origin: projectOrigin, ModulePath: "document.ldl", Digest: "sha256:" + strings.Repeat("a", 64), ByteLength: 4}, {Origin: packOrigin, ModulePath: "pack.ldl", Digest: "sha256:" + strings.Repeat("b", 64), ByteLength: 5}},
		Subjects: []index.SourceSubjectRecord{{Address: "ldl:project:p", Kind: materialize.SubjectProject, OwnerAddress: &owner, Module: &module, DeclarationRange: &projectRange, CommentRanges: []resolve.SourceRange{packRange}, ManifestRoot: true}},
		Bindings: []index.SourceBindingRecord{{SourceAddress: "ldl:project:p", TargetAddress: "ldl:project:p", TargetKind: materialize.SubjectProject, TargetOwnerAddress: owner, Via: "test", Module: module, Range: projectRange}},
		Exports:  []index.ExportBindingRecord{{PublicName: "p", TargetAddress: "ldl:project:p", Module: module, Range: projectRange, ReExport: true}},
		Assets:   []index.SourceAssetRecord{{SubjectAddress: "ldl:project:p", AuthoredPath: "a.png", Locator: "a.png", Origin: projectOrigin, ModulePath: "document.ldl", Range: projectRange, Digest: "sha256:" + strings.Repeat("c", 64), MediaType: "image/png", ByteLength: 1}},
	}
	mappedSource, err := mapSourceMap(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(mappedSource.Files) != 2 || mappedSource.Files[1].Origin.PackAddress == nil || mappedSource.Subjects[0].DeclarationRange == nil || mappedSource.Subjects[0].Module == nil || mappedSource.Bindings[0].TargetOwnerAddress == nil || len(mappedSource.Assets) != 1 {
		t.Fatalf("incomplete source map: %+v", mappedSource)
	}
	hash := "sha256:" + strings.Repeat("d", 64)
	semanticInput := index.SemanticIndexV1{SchemaVersion: 1,
		Subjects:   []index.SemanticSubject{{Address: "ldl:project:p", Kind: materialize.SubjectProject, OwnerAddress: &owner, Module: &module, OwnHash: hash, SubtreeHash: &hash}},
		References: []index.SemanticReference{{SourceAddress: "ldl:project:p", TargetAddress: "ldl:project:p", TargetKind: materialize.SubjectProject, Via: "self", Range: projectRange}},
		Children:   []index.OwnerMembers{}, Rows: []index.OwnerMembers{}, Columns: []index.OwnerMembers{}, TypeMembership: []index.OwnerMembers{}, LayerMembership: []index.OwnerMembers{}, ReferenceIDs: []index.ReferenceIDRecord{}, Adjacency: []index.AdjacencyRecord{},
		Dependencies: []index.DependencyRecord{{Kind: index.DependencyQuery, SubjectAddress: "ldl:project:p:query:q", QueryAddresses: []string{}, ParameterAddresses: []string{}, LayerAddresses: []string{}, EntityTypeAddresses: []string{}, RelationTypeAddresses: []string{}, EntityAddresses: []string{}, RelationAddresses: []string{}, ColumnAddresses: []string{}, ExportAddresses: []string{}, StateReads: []query.StateReadDependency{{SubjectKind: query.StateSubjectEntity, FieldPath: query.StateSystemCreatedAt, ValueType: definition.ScalarDatetime}}}},
		ScopedReads:  index.ScopedReadIndexes{ByModule: []index.ScopeAddresses{{Module: module, Addresses: []string{"ldl:project:p"}}}, ByKind: []index.KindAddresses{{Kind: materialize.SubjectProject, Addresses: []string{"ldl:project:p"}}}, ChildrenByOwner: []index.OwnerMembers{}, RowsByOwner: []index.OwnerMembers{}, ColumnsByOwner: []index.OwnerMembers{}, MembersByType: []index.OwnerMembers{}, MembersByLayer: []index.OwnerMembers{}, ReferencesByID: []index.ReferenceIDRecord{{ID: "p", Addresses: []string{"ldl:project:p"}}}, OutgoingByEntity: []index.OwnerMembers{}, IncomingByEntity: []index.OwnerMembers{}, UsagesByTarget: []index.OwnerMembers{}, QueriesByDependency: []index.OwnerMembers{}, ViewsByDependency: []index.OwnerMembers{}},
	}
	mappedIndex, err := mapSemanticIndex(semanticInput)
	if err != nil {
		t.Fatal(err)
	}
	if mappedIndex.Subjects[0].Module == nil || mappedIndex.Subjects[0].SubtreeHash == nil || len(mappedIndex.Dependencies) != 1 || len(mappedIndex.Dependencies[0].StateReads) != 1 || len(mappedIndex.ScopedReads.ByModule) != 1 {
		t.Fatalf("incomplete semantic index: %+v", mappedIndex)
	}
	if _, err := canonicalUint64FromInt64(-1); err == nil {
		t.Fatal("negative uint accepted")
	}
}

func TestMapAllRecipeScalarAndPredicateVariants(t *testing.T) {
	scalars := []materialize.Scalar{{Type: definition.ScalarString, String: "s"}, {Type: definition.ScalarEnum, String: "e"}, {Type: definition.ScalarDate, String: "2026-01-01"}, {Type: definition.ScalarDatetime, String: "2026-01-01T00:00:00Z"}, {Type: definition.ScalarInteger, Int: 1}, {Type: definition.ScalarNumber, Float: 1.25}, {Type: definition.ScalarBoolean, Bool: true}}
	for _, input := range scalars {
		if _, err := mapRecipeScalar(input); err != nil {
			t.Fatalf("scalar %s: %v", input.Type, err)
		}
	}
	if _, err := mapRecipeScalar(materialize.Scalar{Type: "bad"}); err == nil {
		t.Fatal("bad scalar accepted")
	}
	if _, err := mapRecipeScalar(materialize.Scalar{Type: definition.ScalarInteger, Int: maxSafeInteger + 1}); err == nil {
		t.Fatal("unsafe integer accepted")
	}
	if _, err := mapRecipeScalar(materialize.Scalar{Type: definition.ScalarNumber, Float: math.NaN()}); err == nil {
		t.Fatal("NaN accepted")
	}

	scalar := materialize.Scalar{Type: definition.ScalarString, String: "x"}
	address := "ldl:project:p:entity:e"
	values := []*materialize.PredicateValue{
		nil,
		{Kind: query.ValueParameter, ParameterAddress: "ldl:project:p:query:q:parameter:p"},
		{Kind: query.ValueLiteral, Scalar: &scalar},
		{Kind: query.ValueLiteral, Address: &address},
		{Kind: query.ValueLiteral, Scalars: []materialize.Scalar{}},
		{Kind: query.ValueLiteral, Addresses: []string{}},
	}
	for _, value := range values {
		if _, err := mapOptionalPredicateValue(value); err != nil {
			t.Fatalf("value: %v", err)
		}
	}
	for _, bad := range []*materialize.PredicateValue{{Kind: "bad"}, {Kind: query.ValueLiteral}} {
		if _, err := mapOptionalPredicateValue(bad); err == nil {
			t.Fatal("bad value accepted")
		}
	}

	rowCell := materialize.RowPredicate{Kind: query.PredicateCell, ColumnAddresses: []string{}, Operator: query.OperatorExists}
	rowState := materialize.RowPredicate{Kind: query.PredicateState, FieldPath: query.StateSystemCreatedAt, Operator: query.OperatorExists}
	rowNot := materialize.RowPredicate{Kind: query.PredicateNot, Child: &rowCell}
	rowAll := materialize.RowPredicate{Kind: query.PredicateAll, Children: []materialize.RowPredicate{rowCell}}
	rowAny := materialize.RowPredicate{Kind: query.PredicateAny, Children: []materialize.RowPredicate{rowState}}
	for _, predicate := range []materialize.RowPredicate{rowCell, rowState, rowNot, rowAll, rowAny} {
		if _, err := mapRowPredicate(predicate); err != nil {
			t.Fatalf("row predicate: %v", err)
		}
	}
	for _, bad := range []materialize.RowPredicate{{Kind: query.PredicateNot}, {Kind: "bad"}} {
		if _, err := mapRowPredicate(bad); err == nil {
			t.Fatal("bad row predicate accepted")
		}
	}

	field := materialize.Predicate{Kind: query.PredicateField, Field: "id", Operator: query.OperatorExists}
	state := materialize.Predicate{Kind: query.PredicateState, FieldPath: query.StateSystemCreatedAt, Operator: query.OperatorExists}
	not := materialize.Predicate{Kind: query.PredicateNot, Child: &field}
	all := materialize.Predicate{Kind: query.PredicateAll, Children: []materialize.Predicate{field}}
	any := materialize.Predicate{Kind: query.PredicateAny, Children: []materialize.Predicate{state}}
	rows := materialize.Predicate{Kind: query.PredicateRows, RowPredicate: &rowCell, Quantifier: query.RowsAny, TypeAddresses: []string{}}
	for _, predicate := range []materialize.Predicate{field, state, not, all, any, rows} {
		if _, err := mapRecipePredicate(predicate); err != nil {
			t.Fatalf("predicate: %v", err)
		}
	}
	for _, bad := range []materialize.Predicate{{Kind: query.PredicateNot}, {Kind: query.PredicateRows}, {Kind: "bad"}} {
		if _, err := mapRecipePredicate(bad); err == nil {
			t.Fatal("bad predicate accepted")
		}
	}
}

func TestMapQueryParameterAndNumericBoundaries(t *testing.T) {
	parameter := materialize.QueryParameter{ID: "p", Address: "ldl:project:p:query:q:parameter:p", ValueType: definition.ScalarNumber, EnumValues: []string{}, ReservedEnumValues: []string{}, Required: true, Default: &materialize.Scalar{Type: definition.ScalarNumber, Float: 1.5}, Format: pointer(definition.StringFormatURI), Min: pointer(1.0), Max: pointer(2.0), MinLength: pointer(int64(0)), MaxLength: pointer(int64(2))}
	if _, err := mapQueryParameter(parameter); err != nil {
		t.Fatal(err)
	}
	if _, err := finiteDecimal(math.Inf(1), false); err == nil {
		t.Fatal("infinity accepted")
	}
	for _, value := range []float64{0, 1e-7, 1e-6, 1e20, 1e21, -1.25} {
		if _, err := finiteDecimal(value, false); err != nil {
			t.Fatalf("finite %g: %v", value, err)
		}
	}
	if _, err := finiteDecimal(0, true); err == nil {
		t.Fatal("non-positive accepted")
	}
	if _, err := safeInteger(maxSafeInteger + 1); err == nil {
		t.Fatal("unsafe accepted")
	}
	if _, err := nonNegativeSafe(-1); err == nil {
		t.Fatal("negative accepted")
	}
	if _, err := positiveSafe(0); err == nil {
		t.Fatal("zero accepted")
	}
}

func TestMapProjectionAndViewShapeFamilies(t *testing.T) {
	projection := materialize.ProjectionOverride{
		Composed: &materialize.ComposedProjection{Mode: definition.ComposedEdge, Priority: 1, Conflict: definition.ProjectionConflictKeepEdge, KeepEdge: true, ParentEndpoint: pointer(definition.ProjectionEndpointFrom), ChildEndpoint: pointer(definition.ProjectionEndpointTo), OverlayEndpoint: pointer(definition.ProjectionEndpointFrom), TargetEndpoint: pointer(definition.ProjectionEndpointTo), BadgeEndpoint: pointer(definition.ProjectionEndpointFrom)},
		Diagram:  &materialize.DiagramProjection{Mode: definition.DiagramEdge, SourceEndpoint: definition.ProjectionEndpointFrom, TargetEndpoint: definition.ProjectionEndpointTo, EdgeLabel: definition.ProjectionLabelType, IncludeRelationType: true},
		Table:    &materialize.TableProjection{RowMode: definition.TableRowsRelation, IncludeFrom: true, IncludeTo: true, IncludeRelationType: true},
		Matrix:   &materialize.MatrixProjection{RowEndpoint: definition.ProjectionEndpointFrom, ColumnEndpoint: definition.ProjectionEndpointTo, IncludeRelationRows: true},
		Tree:     &materialize.TreeProjection{ParentEndpoint: definition.ProjectionEndpointFrom, ChildEndpoint: definition.ProjectionEndpointTo},
		Flow:     &materialize.FlowProjection{SourceEndpoint: definition.ProjectionEndpointFrom, TargetEndpoint: definition.ProjectionEndpointTo, ConnectorKind: definition.FlowConnectorSequence, BranchValueColumnAddress: pointer("ldl:project:p:entity_type:e:column:c")},
		Context:  &materialize.ContextProjection{FactTemplate: "{from}", ReverseFactTemplate: pointer("{to}"), IncludeAttributeRows: true},
		Render:   &materialize.RenderSet{Edge: materialize.EdgeRender{Arrow: definition.RenderArrowForward, Line: definition.RenderLineSolid, Color: pointer("#fff"), Label: definition.ProjectionLabelType}, Nested: materialize.NestedRender{FrameLabel: definition.RenderFrameLabelType, FrameStyle: definition.RenderFrameSubtle}, Overlay: materialize.OverlayRender{Kind: "list", Position: definition.RenderPositionTopLeft, MaxItems: 1}, Badge: materialize.BadgeRender{Icon: pointer("i"), Label: definition.RenderBadgeLabelType, Position: definition.RenderPositionTopRight}},
	}
	mapped, err := mapProjectionOverride(projection)
	if err != nil {
		t.Fatal(err)
	}
	if mapped.Composed == nil || mapped.Diagram == nil || mapped.Table == nil || mapped.Matrix == nil || mapped.Tree == nil || mapped.Flow == nil || mapped.Context == nil || mapped.Render == nil {
		t.Fatalf("projection incomplete: %+v", mapped)
	}
	projection.Render.Overlay.MaxItems = 0
	if _, err := mapProjectionOverride(projection); err == nil {
		t.Fatal("invalid render accepted")
	}

	shapes := []materialize.ViewShape{
		{Kind: view.ShapeDiagram, Diagram: &materialize.DiagramShape{Layout: view.LayoutManual, Direction: view.DirectionLeftToRight, Abstraction: view.AbstractionNormal, Composed: true, Placements: []materialize.Placement{{EntityAddress: "ldl:project:p:entity:e", X: 0, Y: -1, Width: 1, Height: 2}}}},
		{Kind: view.ShapeTable, Table: &materialize.TableShape{RowSource: view.RowsEntity, EntityTypeAddresses: pointer([]string{}), Columns: []materialize.TableColumn{{ID: "c", Address: "ldl:project:p:view:v:table_column:c", Source: materialize.TableColumnSource{Kind: view.ColumnRelationEndpoint, Field: "id", ColumnAddresses: []string{}, Endpoint: definition.ProjectionEndpointFrom, Direction: definition.TraversalBoth, RelationTypeAddresses: pointer([]string{}), StateFieldPath: query.StateSystemCreatedAt}, Aggregate: view.AggregateNone}}, Sorts: []materialize.TableSort{{ColumnID: "c", Direction: view.SortAscending, Absent: view.AbsentLast}}}},
		{Kind: view.ShapeMatrix, Matrix: &materialize.MatrixShape{RowAxis: materialize.MatrixAxis{EntityTypeAddresses: pointer([]string{}), LabelField: view.AxisLabelID}, ColumnAxis: materialize.MatrixAxis{LabelField: view.AxisLabelType}, Cell: materialize.MatrixCell{RelationTypeAddresses: pointer([]string{}), Direction: definition.TraversalBoth, Semantic: view.MatrixRelationRefs, Display: view.MatrixExists, AttributeColumnAddresses: pointer([]string{})}}},
		{Kind: view.ShapeTree, Tree: &materialize.TreeShape{RelationTypeAddresses: []string{}, CyclePolicy: view.TreeCycleError, SharedChildPolicy: view.SharedChildLink}},
		{Kind: view.ShapeFlow, Flow: &materialize.FlowShape{RelationTypeAddresses: []string{}, LaneBy: view.LaneNone, LaneColumnAddresses: pointer([]string{}), CyclePolicy: view.FlowCycleError, PreserveParallel: true}},
		{Kind: view.ShapeContext, Context: &materialize.ContextShape{GroupBy: view.ContextGroupNone, Incoming: true, Outgoing: true}},
		{Kind: view.ShapeDiff, Diff: &materialize.DiffShape{Include: []view.DiffSubjectKind{view.DiffProject}, DetectMoves: true}},
	}
	for _, shape := range shapes {
		if _, err := mapViewShape(shape); err != nil {
			t.Fatalf("shape %s: %v", shape.Kind, err)
		}
	}
	bad := shapes[0]
	bad.Diagram.Placements[0].Width = 0
	if _, err := mapViewShape(bad); err == nil {
		t.Fatal("invalid placement accepted")
	}
}

func TestMapExportOptionFamilies(t *testing.T) {
	options := materialize.ExportOptions{Kind: exportrecipe.FormatPNG, Diagnostics: pointer(true), StateSummary: pointer(false), Width: &materialize.Dimension{Auto: true}, Height: &materialize.Dimension{Value: 2}, Scale: pointer(1.5), Background: pointer("#fff"), PageSize: pointer(exportrecipe.PageA4), Orientation: pointer(exportrecipe.OrientationPortrait), Fit: pointer(exportrecipe.FitPage), Legend: pointer(true), Interactive: pointer(true), EmbedAssets: pointer(true), Bundle: pointer(true), Header: pointer(true), SourceManifest: pointer(true), Profile: pointer(exportrecipe.XLSXTypeWorkbook), LookupSheets: pointer(true), HiddenIDs: pointer(true), Formulas: pointer(true), ViewDataJSON: pointer(true)}
	if _, err := mapExportOptions(options); err != nil {
		t.Fatal(err)
	}
	if _, err := mapDimension(materialize.Dimension{}); err == nil {
		t.Fatal("zero dimension accepted")
	}
	if _, err := mapExportOptions(materialize.ExportOptions{Kind: exportrecipe.FormatPNG, Scale: pointer(0.0)}); err == nil {
		t.Fatal("zero scale accepted")
	}
}

func TestInputDuplicateEnumerationAndEveryResourcePreflight(t *testing.T) {
	value := []byte("x")
	ref := testBlobRef("blob", "text/plain", value)
	packID := engineprotocol.CanonicalPackSelector("pub/p")
	input := engineprotocol.CompileInput{
		EntryPath: "document.ldl", Mode: engineprotocol.CompileModeProject,
		ProjectSourceTree: []engineprotocol.SourceFileInput{{Path: "document.ldl", Blob: ref}, {Path: "document.ldl", Blob: ref}},
		InstalledPackTree: []engineprotocol.SourceFileInput{{Path: "pack/p/a.ldl", Blob: ref}, {Path: "pack/p/a.ldl", Blob: ref}},
		ResolvedDependencies: engineprotocol.ResolvedDependencies{Format: engineprotocol.ResolvedDependenciesFormatValue, FormatVersion: 1, Language: 1, Installs: []engineprotocol.ResolvedPack{
			{InstallName: "p", CanonicalID: packID, Path: "pack/p", Manifest: ref, Files: []engineprotocol.ResolvedPackFile{{Path: "a.ldl"}, {Path: "a.ldl"}}, Dependencies: []engineprotocol.ResolvedPackDependency{{LocalName: "d"}, {LocalName: "d"}}},
			{InstallName: "p", CanonicalID: packID, Path: "pack/p", Manifest: ref, Files: []engineprotocol.ResolvedPackFile{{Path: "a.ldl"}}, Dependencies: []engineprotocol.ResolvedPackDependency{}},
		}},
		ReferencedAssets: []engineprotocol.AssetInput{{Origin: engineprotocol.SourceOriginKindPack, PackID: &packID, Locator: "a", Blob: ref, Digest: ref.Digest, MediaType: ref.MediaType}, {Origin: engineprotocol.SourceOriginKindPack, PackID: &packID, Locator: "a", Blob: ref, Digest: ref.Digest, MediaType: ref.MediaType}},
		ResourceLimits:   engineprotocol.ResourceLimits{},
	}
	if diagnostics := validateLogicalDuplicates(input); len(diagnostics) < 7 {
		t.Fatalf("missing duplicate classes: %+v", diagnostics)
	}
	uses := enumerateBlobUses(input)
	if len(uses) != len(input.ProjectSourceTree)+len(input.InstalledPackTree)+len(input.ResolvedDependencies.Installs)+len(input.ReferencedAssets) {
		t.Fatalf("blob use enumeration incomplete: %d", len(uses))
	}
	conflict := uses[0]
	conflict.ref.MediaType = "other"
	if failure := validateBlobAliases([]blobUse{uses[0], conflict}); failure == nil || failure.Code != FailureCompileConflictingBlobRef {
		t.Fatalf("conflict not rejected: %+v", failure)
	}

	limits := engine.ResourceLimits{MaxProjectSourceFiles: 1, MaxProjectSourceBytes: 1, MaxPackFiles: 1, MaxPackBytes: 1, MaxAssets: 1, MaxAssetBytes: 1, MaxRasterDimension: 1, MaxRasterPixels: 1, MaxDeclarations: 1}
	base := engineprotocol.CompileInput{ProjectSourceTree: []engineprotocol.SourceFileInput{{Path: "a", Blob: ref}}, InstalledPackTree: []engineprotocol.SourceFileInput{}, ResolvedDependencies: engineprotocol.ResolvedDependencies{Installs: []engineprotocol.ResolvedPack{}}, ReferencedAssets: []engineprotocol.AssetInput{}}
	cases := []struct {
		name, code string
		mutate     func(*engineprotocol.CompileInput)
	}{
		{"project files", engine.ErrorCodeProjectSourceFilesExceeded, func(v *engineprotocol.CompileInput) {
			v.ProjectSourceTree = append(v.ProjectSourceTree, v.ProjectSourceTree[0])
		}},
		{"project bytes", engine.ErrorCodeProjectSourceBytesExceeded, func(v *engineprotocol.CompileInput) { v.ProjectSourceTree[0].Blob.Size = "2" }},
		{"pack files", engine.ErrorCodePackFilesExceeded, func(v *engineprotocol.CompileInput) {
			v.InstalledPackTree = []engineprotocol.SourceFileInput{{Blob: ref}, {Blob: ref}}
		}},
		{"metadata files", engine.ErrorCodePackFilesExceeded, func(v *engineprotocol.CompileInput) {
			v.ResolvedDependencies.Installs = []engineprotocol.ResolvedPack{{Files: []engineprotocol.ResolvedPackFile{{}}}}
		}},
		{"assets", engine.ErrorCodeAssetsExceeded, func(v *engineprotocol.CompileInput) { v.ReferencedAssets = []engineprotocol.AssetInput{{}, {}} }},
		{"pack bytes", engine.ErrorCodePackBytesExceeded, func(v *engineprotocol.CompileInput) {
			v.InstalledPackTree = []engineprotocol.SourceFileInput{{Blob: protocolcommon.BlobRef{Size: "2"}}}
		}},
		{"manifest bytes", engine.ErrorCodePackBytesExceeded, func(v *engineprotocol.CompileInput) {
			v.ResolvedDependencies.Installs = []engineprotocol.ResolvedPack{{Manifest: protocolcommon.BlobRef{Size: "2"}}}
		}},
		{"asset bytes", engine.ErrorCodeAssetBytesExceeded, func(v *engineprotocol.CompileInput) {
			assetRef := ref
			assetRef.Size = "2"
			v.ReferencedAssets = []engineprotocol.AssetInput{{Blob: assetRef, Digest: assetRef.Digest, MediaType: assetRef.MediaType}}
		}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			candidate := base
			candidate.ProjectSourceTree = slices.Clone(base.ProjectSourceTree)
			item.mutate(&candidate)
			failure := preflightBlobResources(candidate, limits)
			if failure == nil || failure.Code != item.code {
				t.Fatalf("failure=%+v", failure)
			}
		})
	}
	conflicting := base
	conflicting.ReferencedAssets = []engineprotocol.AssetInput{{Blob: ref, Digest: protocolcommon.Digest("sha256:" + strings.Repeat("f", 64)), MediaType: ref.MediaType}}
	if failure := preflightBlobResources(conflicting, limits); failure == nil || failure.Code != FailureCompileConflictingBlobRef {
		t.Fatalf("asset metadata=%+v", failure)
	}
	invalidRef := ref
	invalidRef.Size = "not-a-size"
	if _, failure := blobSize(invalidRef); failure == nil {
		t.Fatal("invalid size accepted")
	}
	invalidRef.Size = protocolcommon.CanonicalUint64("18446744073709551615")
	if _, failure := blobSize(invalidRef); failure == nil || failure.Code != FailureCompileBlobOversized {
		t.Fatalf("oversized=%+v", failure)
	}
	if saturatedAdd(math.MaxInt64, 1) != math.MaxInt64 || saturatedAdd(1, 1) != 2 {
		t.Fatal("saturated addition")
	}
	if resourceFailure("code", "resource", 1, 2).SafeDetails == nil {
		t.Fatal("resource details absent")
	}
}

func TestCompileFailureMappingAndDispatcherMisuse(t *testing.T) {
	release := protocolcommon.ReleaseVersion(engine.DevelopmentVersion)
	for _, err := range []error{
		&engine.CompileError{Code: engine.ErrorCodeCancelled, Category: engine.ErrorCategoryCancelled},
		&engine.CompileError{Code: engine.ErrorCodeAssetBytesExceeded, Category: engine.ErrorCategoryResource, Resource: "asset_bytes", Limit: 1, Observed: 2},
		&engine.CompileError{Code: engine.ErrorCodeInvariantFailure, Category: engine.ErrorCategoryInvariant},
		context.Canceled, errors.New("unknown"),
	} {
		response, mapErr := mapCompileError("id", release, err)
		if mapErr != nil {
			t.Fatal(mapErr)
		}
		if response.Failure == nil {
			t.Fatalf("failure absent for %v", err)
		}
	}
	request := compileRequest([]byte("project p \"P\" {}"))
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	if _, err := (*CompileDispatcher)(nil).DispatchCompile(context.Background(), nil, request, nil, nil); err == nil {
		t.Fatal("nil dispatcher accepted")
	}
	if _, err := dispatcher.DispatchCompile(nil, nil, request, nil, nil); err == nil {
		t.Fatal("nil context accepted")
	}
	request.RequestID = ""
	if _, err := dispatcher.DispatchCompile(context.Background(), nil, request, nil, nil); err == nil {
		t.Fatal("empty ID accepted")
	}
	request = compileRequest([]byte("project p \"P\" {}"))
	response, err := dispatcher.DispatchCompile(context.Background(), nil, request, &memoryBlobSource{}, &memoryBlobSink{})
	if err != nil || response.Failure == nil || response.Failure.Code != FailureCompileUnnegotiated {
		t.Fatalf("nil negotiation: %+v %v", response, err)
	}
	negotiated := compileContext(t)
	request.Payload.ProjectSourceTree = nil
	response, err = dispatcher.DispatchCompile(context.Background(), negotiated, request, &memoryBlobSource{}, &memoryBlobSink{})
	if err != nil || response.Failure == nil || response.Failure.Code != FailureCompileInvalidRequest {
		t.Fatalf("invalid envelope: %+v %v", response, err)
	}
}

func TestDispatcherMapsSemanticResourceAndNegotiationTerminalPaths(t *testing.T) {
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	negotiated := compileContext(t)
	value := []byte("this is not LDL")
	request := compileRequest(value)
	sink := &memoryBlobSink{}
	response, err := dispatcher.DispatchCompile(context.Background(), negotiated, request, sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), sink)
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeRejected || len(response.Diagnostics) == 0 || response.Payload != nil || response.Failure != nil || sink.calls != 0 {
		t.Fatalf("semantic rejection: %+v sink=%d", response, sink.calls)
	}

	value = []byte("project p \"P\" {}\nentity_type e \"E\" {}\n")
	request = compileRequest(value)
	one := protocolcommon.CanonicalNonNegativeInt64("1")
	request.Payload.ResourceLimits.MaxDeclarations = &one
	response, err = dispatcher.DispatchCompile(context.Background(), negotiated, request, sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), &memoryBlobSink{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeFailed || response.Failure == nil || response.Failure.Code != engine.ErrorCodeDeclarationsExceeded || response.Failure.SafeDetails == nil {
		t.Fatalf("resource failure: %+v", response)
	}

	request = compileRequest([]byte("project p \"P\" {}"))
	wrongRelease := NewCompileDispatcher(engine.New(engine.BuildInfo{ReleaseVersion: "1.2.3"}))
	source := &memoryBlobSource{}
	response, err = wrongRelease.DispatchCompile(context.Background(), negotiated, request, source, &memoryBlobSink{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Failure == nil || response.Failure.Code != FailureCompileInvariant || source.calls != 0 {
		t.Fatalf("release mismatch: %+v calls=%d", response, source.calls)
	}
	response, err = dispatcher.DispatchCompile(context.Background(), negotiated, request, nil, &memoryBlobSink{})
	if err != nil || response.Failure == nil || response.Failure.Code != FailureCompileInvariant {
		t.Fatalf("nil source: %+v %v", response, err)
	}
	response, err = dispatcher.DispatchCompile(context.Background(), negotiated, request, &memoryBlobSource{}, nil)
	if err != nil || response.Failure == nil || response.Failure.Code != FailureCompileInvariant {
		t.Fatalf("nil sink: %+v %v", response, err)
	}
	sourceRequest := compileRequest([]byte("project p \"P\" {}"))
	response, err = dispatcher.DispatchCompile(context.Background(), negotiated, sourceRequest, errorBlobSource{context.Canceled}, &memoryBlobSink{})
	if err != nil || response.Outcome != protocolcommon.OutcomeCancelled {
		t.Fatalf("source cancellation: %+v %v", response, err)
	}
	validSource := sourceFor(sourceRequest.Payload.ProjectSourceTree[0].Blob, []byte("project p \"P\" {}"))
	response, err = dispatcher.DispatchCompile(context.Background(), negotiated, sourceRequest, validSource, &memoryBlobSink{err: context.Canceled})
	if err != nil || response.Outcome != protocolcommon.OutcomeCancelled {
		t.Fatalf("sink cancellation: %+v %v", response, err)
	}
}

func TestDirectCompleteRecipeMappingWithOptionalValues(t *testing.T) {
	description := "description"
	normalizedQuery := materialize.Query{Common: materialize.Common{Description: &description, Tags: []string{"tag"}, Annotations: map[string]string{"a": "b"}}, ID: "q", Address: "ldl:project:p:query:q", DisplayName: "Q", StateInput: query.StateOptional, Parameters: []materialize.QueryParameter{}, Select: materialize.QuerySelect{}, Where: materialize.Predicate{Kind: query.PredicateAll, Children: []materialize.Predicate{}}, RelationWhere: materialize.Predicate{Kind: query.PredicateAny, Children: []materialize.Predicate{}}, Traverse: &materialize.QueryTraversal{Direction: definition.TraversalBoth, MinDepth: 0, MaxDepth: 1, CyclePolicy: query.CycleVisitOnce, RelationTypeAddresses: pointer([]string{})}, Result: []query.ResultMember{}, ReservedParameterIDs: []string{}}
	compiledQuery := engine.CompiledQueryRecipe{ID: "q", Address: normalizedQuery.Address, Dependencies: query.Dependencies{LayerAddresses: []string{}, EntityTypeAddresses: []string{}, RelationTypeAddresses: []string{}, EntityAddresses: []string{}, RelationAddresses: []string{}, ColumnAddresses: []string{}, ParameterAddresses: []string{}, StateReads: []query.StateReadDependency{}}}
	mappedQuery, err := mapQueryRecipe(normalizedQuery, compiledQuery)
	if err != nil {
		t.Fatal(err)
	}
	if mappedQuery.Description == nil || mappedQuery.Traverse == nil {
		t.Fatalf("optional Query values absent: %+v", mappedQuery)
	}
	compiledQuery.Address = "other"
	if _, err := mapQueryRecipe(normalizedQuery, compiledQuery); err == nil {
		t.Fatal("mixed Query generation accepted")
	}

	exportInput := materialize.ExportRecipe{ID: "e", Address: "ldl:project:p:view:v:export:e", Format: exportrecipe.FormatJSON, Filename: "e.json", Fidelity: exportrecipe.FidelityLossless, SourceRefs: true, ExporterProfile: materialize.ExporterProfileRef{ID: "builtin", Format: exportrecipe.FormatJSON, RegistrySchemaVersion: 1, RegistryDigest: "sha256:" + strings.Repeat("a", 64), SpecificationDigest: "sha256:" + strings.Repeat("b", 64)}, Options: materialize.ExportOptions{Kind: exportrecipe.FormatJSON, Diagnostics: pointer(true), StateSummary: pointer(false)}}
	compiledExport := exportrecipe.Recipe{ID: "e", Address: exportInput.Address, ViewAddress: "ldl:project:p:view:v", Format: exportrecipe.FormatJSON, Extension: ".json", Fidelity: exportrecipe.FidelityLossless, NativeMaximumFidelity: exportrecipe.FidelityLossless, EffectiveMaximumFidelity: exportrecipe.FidelityLossless, FidelityBasis: exportrecipe.FidelityBasisNative, RequiresSourceManifest: false}
	viewSourceAddress := "ldl:project:p:query:q"
	normalizedView := materialize.View{Common: materialize.Common{Description: &description, Tags: []string{}, Annotations: map[string]string{}}, ID: "v", Address: "ldl:project:p:view:v", DisplayName: "V", StateInput: query.StateOptional, Category: view.CategoryContext, Intent: &description, Source: materialize.ViewSource{Kind: view.SourceQuery, QueryAddress: &viewSourceAddress, Arguments: map[string]materialize.Scalar{}}, RelationProjections: map[string]materialize.ProjectionOverride{"ldl:project:p:relation_type:r": {}}, Shape: materialize.ViewShape{Kind: view.ShapeContext, Context: &materialize.ContextShape{GroupBy: view.ContextGroupNone}}, Exports: []materialize.ExportRecipe{exportInput}, ReservedTableColumnIDs: []string{}, ReservedExportIDs: []string{}}
	compiledView := engine.CompiledViewRecipe{ID: "v", Address: normalizedView.Address, StateRequirement: query.StateOptional, Dependencies: view.Dependencies{QueryAddresses: []string{}, ParameterAddresses: []string{}, LayerAddresses: []string{}, EntityTypeAddresses: []string{}, RelationTypeAddresses: []string{}, EntityAddresses: []string{}, RelationAddresses: []string{}, ColumnAddresses: []string{}, ExportAddresses: []string{}, StateReads: []query.StateReadDependency{}}}
	mappedView, err := mapViewRecipe(normalizedView, compiledView, map[string]exportrecipe.Recipe{compiledExport.Address: compiledExport})
	if err != nil {
		t.Fatal(err)
	}
	if mappedView.Description == nil || mappedView.Intent == nil || len(mappedView.Exports) != 1 || len(mappedView.RelationProjectionOverrides) != 1 {
		t.Fatalf("optional View values absent: %+v", mappedView)
	}
	if _, err := mapViewRecipe(normalizedView, compiledView, map[string]exportrecipe.Recipe{}); err == nil {
		t.Fatal("missing Export accepted")
	}
	badView := normalizedView
	badView.Source.Arguments = map[string]materialize.Scalar{"bad": {Type: "bad"}}
	if _, err := mapViewRecipe(badView, compiledView, map[string]exportrecipe.Recipe{compiledExport.Address: compiledExport}); err == nil {
		t.Fatal("invalid View scalar accepted")
	}
	badExport := compiledExport
	badExport.Address = "other"
	if _, err := mapExportRecipe(exportInput, badExport); err == nil {
		t.Fatal("mixed Export generation accepted")
	}
}

type errorBlobSource struct{ err error }

func (source errorBlobSource) Definitions(context.Context) ([]BlobDefinition, error) {
	return nil, source.err
}

type panicBlobSource struct{}

func (panicBlobSource) Definitions(context.Context) ([]BlobDefinition, error) { panic("private panic") }

type errorReadCloser struct{ readErr, closeErr error }

func (reader *errorReadCloser) Read([]byte) (int, error) { return 0, reader.readErr }
func (reader *errorReadCloser) Close() error             { return reader.closeErr }

func TestBlobSourceErrorsCancellationAndPanicAreContained(t *testing.T) {
	value := []byte("project p \"P\" {}")
	request := compileRequest(value)
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	negotiated := compileContext(t)
	for _, source := range []BlobSource{errorBlobSource{errors.New("path /private")}, panicBlobSource{}, &memoryBlobSource{definitions: []BlobDefinition{{BlobID: "source", Reader: &errorReadCloser{readErr: errors.New("read")}}}}, &memoryBlobSource{definitions: []BlobDefinition{{BlobID: "source", Reader: &errorReadCloser{readErr: io.EOF, closeErr: errors.New("close")}}}}} {
		response, err := dispatcher.DispatchCompile(context.Background(), negotiated, request, source, &memoryBlobSink{})
		if err != nil {
			t.Fatal(err)
		}
		if response.Outcome != protocolcommon.OutcomeFailed || response.Failure == nil || strings.Contains(response.Failure.Message, "private") {
			t.Fatalf("unsafe failure: %+v", response)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	failure := blobSourceFailure(ctx, context.Canceled)
	if failure.Category != protocolcommon.ProtocolFailureCategoryCancelled {
		t.Fatalf("not cancelled: %+v", failure)
	}
}

func TestCompileSnapshotInvariantBranchesAndOutputBlobValidation(t *testing.T) {
	result, err := engine.New(engine.BuildInfo{}).Compile(context.Background(), engine.CompileInput{Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte("project p \"P\" {}")}, ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}})
	if err != nil {
		t.Fatal(err)
	}
	base := result.Snapshot()
	rejected := engine.Snapshot{CompileOutput: engine.CompileOutput{Diagnostics: []engine.Diagnostic{{Code: "LDL1001"}}}}
	if !isRejectedCompileSnapshot(rejected) {
		t.Fatal("complete semantic rejection was not recognized")
	}
	rejected.StableAddresses = []string{}
	if isRejectedCompileSnapshot(rejected) {
		t.Fatal("partial semantic rejection was accepted")
	}
	mutations := []struct {
		name   string
		mutate func(*engine.Snapshot)
	}{
		{"mode", func(v *engine.Snapshot) { v.Mode = "bad" }},
		{"limits", func(v *engine.Snapshot) { v.EffectiveLimits.MaxAssets = 0 }},
		{"source map", func(v *engine.Snapshot) { v.SourceMap.Files[0].ByteLength = -1 }},
		{"semantic index", func(v *engine.Snapshot) {
			v.SemanticIndex.References = append(v.SemanticIndex.References, index.SemanticReference{Range: resolve.SourceRange{Origin: resolve.SourceOrigin{Kind: "bad"}}})
		}},
		{"search documents", func(v *engine.Snapshot) {
			v.SearchDocuments = append(v.SearchDocuments, index.SearchDocument{Fields: []index.SearchField{{SourceRef: &resolve.SourceRange{Origin: resolve.SourceOrigin{Kind: "bad"}}}}})
		}},
		{"normalized union", func(v *engine.Snapshot) { v.NormalizedDocument = nil }},
		{"recipe generation", func(v *engine.Snapshot) {
			v.CompiledQueryRecipes = append(v.CompiledQueryRecipes, engine.CompiledQueryRecipe{Address: "ldl:project:p:query:missing"})
		}},
	}
	for _, item := range mutations {
		t.Run(item.name, func(t *testing.T) {
			snapshot := base
			item.mutate(&snapshot)
			if _, _, err := mapCompileSnapshot(snapshot); err == nil {
				t.Fatal("invalid snapshot accepted")
			}
		})
	}
	if _, err := mapEffectiveLimits(engine.ResourceLimits{}); err == nil {
		t.Fatal("zero limits accepted")
	}
	packSnapshot := engine.Snapshot{CompileOutput: engine.CompileOutput{Mode: engine.CompilePack, NormalizedPackArtifact: &materialize.NormalizedPackArtifact{Pack: materialize.PackRoot{Address: "ldl:pack:pub:p"}}, CanonicalJSON: []byte("{}"), ArtifactJSON: []byte("{}\n")}}
	if _, _, err := mapNormalizedArtifact(packSnapshot, []semantic.SearchDocument{{}}); err == nil {
		t.Fatal("Pack search document accepted")
	}
	packSnapshot.NormalizedDocument = &materialize.NormalizedDocument{}
	if _, _, err := mapNormalizedArtifact(packSnapshot, []semantic.SearchDocument{}); err == nil {
		t.Fatal("mixed Pack union accepted")
	}

	one := newOutputBlob("same", []byte("a"))
	one.Ref.MediaType = "a"
	two := newOutputBlob("same", []byte("b"))
	two.Ref.MediaType = "b"
	if err := validateUniqueOutputBlobs([]OutputBlob{one, two}); err == nil {
		t.Fatal("duplicate output accepted")
	}
	one.Ref.Digest = protocolcommon.Digest("sha256:" + strings.Repeat("f", 64))
	if err := validateUniqueOutputBlobs([]OutputBlob{one}); err == nil {
		t.Fatal("corrupt output accepted")
	}
}

func TestMapperInvalidSourceFamiliesAndRecipeSetInvariants(t *testing.T) {
	for _, origin := range []resolve.SourceOrigin{{Kind: resolve.OriginProject, PackAddress: "ldl:pack:x:y"}, {Kind: resolve.OriginPack}, {Kind: "bad"}} {
		if _, err := mapSourceOrigin(origin); err == nil {
			t.Fatalf("origin accepted: %+v", origin)
		}
	}
	if _, err := mapModuleRef(index.ModuleRef{Origin: resolve.SourceOrigin{Kind: "bad"}}); err == nil {
		t.Fatal("bad module accepted")
	}
	badRange := resolve.SourceRange{Origin: resolve.SourceOrigin{Kind: "bad"}}
	sourceCases := []index.SourceMapV1{
		{Files: []index.SourceFileRecord{{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ByteLength: -1}}, Subjects: []index.SourceSubjectRecord{}, Bindings: []index.SourceBindingRecord{}, Exports: []index.ExportBindingRecord{}, Assets: []index.SourceAssetRecord{}},
		{Files: []index.SourceFileRecord{}, Subjects: []index.SourceSubjectRecord{{Module: &index.ModuleRef{Origin: resolve.SourceOrigin{Kind: "bad"}}, CommentRanges: []resolve.SourceRange{}}}, Bindings: []index.SourceBindingRecord{}, Exports: []index.ExportBindingRecord{}, Assets: []index.SourceAssetRecord{}},
		{Files: []index.SourceFileRecord{}, Subjects: []index.SourceSubjectRecord{{DeclarationRange: &badRange, CommentRanges: []resolve.SourceRange{}}}, Bindings: []index.SourceBindingRecord{}, Exports: []index.ExportBindingRecord{}, Assets: []index.SourceAssetRecord{}},
		{Files: []index.SourceFileRecord{}, Subjects: []index.SourceSubjectRecord{{CommentRanges: []resolve.SourceRange{badRange}}}, Bindings: []index.SourceBindingRecord{}, Exports: []index.ExportBindingRecord{}, Assets: []index.SourceAssetRecord{}},
		{Files: []index.SourceFileRecord{}, Subjects: []index.SourceSubjectRecord{}, Bindings: []index.SourceBindingRecord{{Module: index.ModuleRef{Origin: resolve.SourceOrigin{Kind: "bad"}}}}, Exports: []index.ExportBindingRecord{}, Assets: []index.SourceAssetRecord{}},
		{Files: []index.SourceFileRecord{}, Subjects: []index.SourceSubjectRecord{}, Bindings: []index.SourceBindingRecord{}, Exports: []index.ExportBindingRecord{{Module: index.ModuleRef{Origin: resolve.SourceOrigin{Kind: "bad"}}}}, Assets: []index.SourceAssetRecord{}},
		{Files: []index.SourceFileRecord{}, Subjects: []index.SourceSubjectRecord{}, Bindings: []index.SourceBindingRecord{}, Exports: []index.ExportBindingRecord{}, Assets: []index.SourceAssetRecord{{Origin: resolve.SourceOrigin{Kind: "bad"}}}},
	}
	for i, input := range sourceCases {
		if _, err := mapSourceMap(input); err == nil {
			t.Fatalf("source case %d accepted", i)
		}
	}
	if _, _, err := normalizedRecipeSets(engine.Snapshot{}); err == nil {
		t.Fatal("invalid recipe mode accepted")
	}
	if _, _, err := normalizedRecipeSets(engine.Snapshot{CompileOutput: engine.CompileOutput{Mode: engine.CompileProject}}); err == nil {
		t.Fatal("missing Project accepted")
	}
	if _, _, err := normalizedRecipeSets(engine.Snapshot{CompileOutput: engine.CompileOutput{Mode: engine.CompilePack}}); err == nil {
		t.Fatal("missing Pack accepted")
	}
	if err := uniqueRecipeAddresses([]materialize.Query{{Address: "x"}, {Address: "x"}}, nil, nil, nil, nil); err == nil {
		t.Fatal("duplicate recipe accepted")
	}
}

type noProgressReader struct{}

func (noProgressReader) Read([]byte) (int, error) { return 0, nil }

func TestBlobReadAndLimitParsingEdgeBranches(t *testing.T) {
	if _, err := readBounded(context.Background(), strings.NewReader(""), math.MaxInt64); err == nil {
		t.Fatal("max read accepted")
	}
	if _, err := readBounded(context.Background(), noProgressReader{}, 1); !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("no progress=%v", err)
	}
	if _, err := readBounded(context.Background(), &errorReadCloser{readErr: errors.New("read")}, 1); err == nil {
		t.Fatal("read error lost")
	}
	limits := engine.DefaultResourceLimits()
	wire := engineprotocol.ResourceLimits{}
	zero := protocolcommon.CanonicalNonNegativeInt64("0")
	one := protocolcommon.CanonicalNonNegativeInt64("1")
	wire.MaxProjectSourceFiles = &zero
	wire.MaxProjectSourceBytes = &one
	wire.MaxPackFiles = &one
	wire.MaxPackBytes = &one
	wire.MaxAssets = &one
	wire.MaxAssetBytes = &one
	wire.MaxRasterDimension = &one
	wire.MaxRasterPixels = &one
	wire.MaxDeclarations = &one
	if _, diagnostics, failure := mapRequestLimits(wire, limits, limits); failure != nil || len(diagnostics) != 0 {
		t.Fatalf("valid limits: %+v %+v", diagnostics, failure)
	}
	bad := protocolcommon.CanonicalNonNegativeInt64("bad")
	wire.MaxAssets = &bad
	if _, _, failure := mapRequestLimits(wire, limits, limits); failure == nil {
		t.Fatal("bad typed limit accepted")
	}
	ref := testBlobRef("x", "text/plain", []byte("x"))
	uses := []blobUse{{ref: ref}}
	for _, source := range []BlobSource{&memoryBlobSource{definitions: []BlobDefinition{{BlobID: "", Reader: io.NopCloser(strings.NewReader("x"))}}}, &memoryBlobSource{definitions: []BlobDefinition{{BlobID: "x", Reader: nil}}}} {
		if _, failure := resolveBlobUses(context.Background(), uses, source); failure == nil {
			t.Fatal("invalid definition accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, failure := resolveBlobUses(ctx, uses, sourceFor(ref, []byte("x"))); failure == nil || failure.Category != protocolcommon.ProtocolFailureCategoryCancelled {
		t.Fatalf("cancel=%+v", failure)
	}
}

func TestMapCompileInputCopiesPacksDependenciesAssetsAndRoot(t *testing.T) {
	fileBytes := []byte("file")
	manifestBytes := []byte("manifest")
	assetBytes := []byte("asset")
	fileRef := testBlobRef("file", "text/plain", fileBytes)
	manifestRef := testBlobRef("manifest", "application/json", manifestBytes)
	assetRef := testBlobRef("asset", "image/svg+xml", assetBytes)
	packID := engineprotocol.CanonicalPackSelector("pub/p")
	input := engineprotocol.CompileInput{Mode: engineprotocol.CompileModePack, EntryPath: "pack.ldl", RootPackID: &packID, ProjectSourceTree: []engineprotocol.SourceFileInput{}, InstalledPackTree: []engineprotocol.SourceFileInput{{Path: "pack/p/pack.ldl", Blob: fileRef}}, ResolvedDependencies: engineprotocol.ResolvedDependencies{Format: engineprotocol.ResolvedDependenciesFormatValue, FormatVersion: 1, Language: 1, Installs: []engineprotocol.ResolvedPack{{InstallName: "p", CanonicalID: packID, Version: "1.0.0", Digest: protocolcommon.Digest("sha256:" + strings.Repeat("a", 64)), Path: "pack/p", Entry: "pack.ldl", Files: []engineprotocol.ResolvedPackFile{{Path: "pack.ldl", Digest: fileRef.Digest}}, Dependencies: []engineprotocol.ResolvedPackDependency{{LocalName: "d", InstallName: "dep"}}, ManifestPath: "manifest.json", Manifest: manifestRef}}}, ReferencedAssets: []engineprotocol.AssetInput{{Origin: engineprotocol.SourceOriginKindPack, PackID: &packID, Locator: "asset.svg", Blob: assetRef, Digest: assetRef.Digest, MediaType: assetRef.MediaType}}, ResourceLimits: engineprotocol.ResourceLimits{}}
	source := &memoryBlobSource{definitions: []BlobDefinition{{BlobID: fileRef.BlobID, Reader: io.NopCloser(strings.NewReader(string(fileBytes)))}, {BlobID: manifestRef.BlobID, Reader: io.NopCloser(strings.NewReader(string(manifestBytes)))}, {BlobID: assetRef.BlobID, Reader: io.NopCloser(strings.NewReader(string(assetBytes)))}}}
	negotiated := compileContext(t)
	mapped, diagnostics, failure := mapCompileInput(context.Background(), negotiated, input, source)
	if failure != nil || len(diagnostics) != 0 {
		t.Fatalf("map failed diagnostics=%+v failure=%+v", diagnostics, failure)
	}
	if mapped.RootPackID != "pub/p" || len(mapped.InstalledPackTree) != 1 || len(mapped.ResolvedDependencies.Installs) != 1 || len(mapped.ResolvedDependencies.Installs[0].Files) != 1 || len(mapped.ResolvedDependencies.Installs[0].Dependencies) != 1 || len(mapped.ReferencedAssets) != 1 || mapped.ReferencedAssets[0].PackID != "pub/p" || mapped.ReferencedAssets[0].ByteLength != int64(len(assetBytes)) {
		t.Fatalf("incomplete map: %+v", mapped)
	}
}

func TestGeneratedRecipeMappingInvariantFailures(t *testing.T) {
	result, err := engine.New(engine.BuildInfo{}).Compile(context.Background(), engine.CompileInput{Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte(allRecipeDeclarationsFixture)}, ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*engine.Snapshot)
	}{
		{"missing Query", func(v *engine.Snapshot) { v.NormalizedDocument.Queries = nil }},
		{"missing View", func(v *engine.Snapshot) { v.NormalizedDocument.Views = nil }},
		{"missing Export", func(v *engine.Snapshot) { v.NormalizedDocument.Views[0].Exports = nil }},
		{"duplicate Query", func(v *engine.Snapshot) {
			v.CompiledQueryRecipes = append(v.CompiledQueryRecipes, v.CompiledQueryRecipes[0])
		}},
		{"invalid Query document", func(v *engine.Snapshot) { v.CompiledQueryRecipes[0].Dependencies.EntityAddresses = []string{"invalid"} }},
		{"invalid View document", func(v *engine.Snapshot) { v.CompiledViewRecipes[0].StateRequirement = "bad" }},
		{"invalid Export document", func(v *engine.Snapshot) { v.CompiledExportRecipes[0].FidelityBasis = "bad" }},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			snapshot := result.Snapshot()
			item.mutate(&snapshot)
			if _, _, err := mapCompiledRecipes(snapshot); err == nil {
				t.Fatal("invalid recipe generation accepted")
			}
		})
	}
}

func TestRemainingRecipeMapperErrorBranches(t *testing.T) {
	badScalar := materialize.Scalar{Type: "bad"}
	nan := math.NaN()
	negative := int64(-1)
	parameters := []materialize.QueryParameter{{Default: &badScalar}, {Min: &nan}, {Max: &nan}, {MinLength: &negative}, {MaxLength: &negative}}
	for i, input := range parameters {
		if _, err := mapQueryParameter(input); err == nil {
			t.Fatalf("parameter %d accepted", i)
		}
	}
	badValue := &materialize.PredicateValue{Kind: "bad"}
	predicates := []materialize.Predicate{
		{Kind: query.PredicateAll, Children: []materialize.Predicate{{Kind: "bad"}}},
		{Kind: query.PredicateNot, Child: &materialize.Predicate{Kind: "bad"}},
		{Kind: query.PredicateField, Operator: query.OperatorEqual, Value: badValue},
		{Kind: query.PredicateState, Operator: query.OperatorEqual, Value: badValue},
		{Kind: query.PredicateRows, Quantifier: query.RowsAny, RowPredicate: &materialize.RowPredicate{Kind: "bad"}},
	}
	for i, input := range predicates {
		if _, err := mapRecipePredicate(input); err == nil {
			t.Fatalf("predicate %d accepted", i)
		}
	}
	rows := []materialize.RowPredicate{{Kind: query.PredicateAll, Children: []materialize.RowPredicate{{Kind: "bad"}}}, {Kind: query.PredicateNot, Child: &materialize.RowPredicate{Kind: "bad"}}, {Kind: query.PredicateCell, Operator: query.OperatorEqual, Value: badValue}, {Kind: query.PredicateState, Operator: query.OperatorEqual, Value: badValue}}
	for i, input := range rows {
		if _, err := mapRowPredicate(input); err == nil {
			t.Fatalf("row %d accepted", i)
		}
	}
	projection := materialize.ProjectionOverride{Composed: &materialize.ComposedProjection{Priority: maxSafeInteger + 1}}
	if _, err := mapProjectionOverride(projection); err == nil {
		t.Fatal("unsafe priority accepted")
	}
	viewInput := materialize.View{Common: materialize.Common{Tags: []string{}, Annotations: map[string]string{}}, Address: "a", Source: materialize.ViewSource{Arguments: map[string]materialize.Scalar{}}, RelationProjections: map[string]materialize.ProjectionOverride{"x": projection}, Shape: materialize.ViewShape{Kind: view.ShapeContext, Context: &materialize.ContextShape{}}, Exports: []materialize.ExportRecipe{}, ReservedTableColumnIDs: []string{}, ReservedExportIDs: []string{}}
	compiledView := engine.CompiledViewRecipe{Address: "a", Dependencies: view.Dependencies{QueryAddresses: []string{}, ParameterAddresses: []string{}, LayerAddresses: []string{}, EntityTypeAddresses: []string{}, RelationTypeAddresses: []string{}, EntityAddresses: []string{}, RelationAddresses: []string{}, ColumnAddresses: []string{}, ExportAddresses: []string{}, StateReads: []query.StateReadDependency{}}}
	if _, err := mapViewRecipe(viewInput, compiledView, map[string]exportrecipe.Recipe{}); err == nil {
		t.Fatal("bad projection accepted")
	}
	viewInput.RelationProjections = map[string]materialize.ProjectionOverride{}
	viewInput.Shape = materialize.ViewShape{Kind: view.ShapeDiagram, Diagram: &materialize.DiagramShape{Placements: []materialize.Placement{{Width: 0, Height: 1}}}}
	if _, err := mapViewRecipe(viewInput, compiledView, map[string]exportrecipe.Recipe{}); err == nil {
		t.Fatal("bad shape accepted")
	}
	semanticInput := index.SemanticIndexV1{Subjects: []index.SemanticSubject{}, References: []index.SemanticReference{}, Children: []index.OwnerMembers{}, Rows: []index.OwnerMembers{}, Columns: []index.OwnerMembers{}, TypeMembership: []index.OwnerMembers{}, LayerMembership: []index.OwnerMembers{}, ReferenceIDs: []index.ReferenceIDRecord{}, Adjacency: []index.AdjacencyRecord{}, Dependencies: []index.DependencyRecord{}, ScopedReads: index.ScopedReadIndexes{ByModule: []index.ScopeAddresses{{Module: index.ModuleRef{Origin: resolve.SourceOrigin{Kind: "bad"}}}}}}
	if _, err := mapSemanticIndex(semanticInput); err == nil {
		t.Fatal("bad scoped module accepted")
	}
}
