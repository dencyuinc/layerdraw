// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
)

func TestGeneratedParityCorpusIsCurrentAndDeterministic(t *testing.T) {
	first, err := buildCorpus()
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildCorpus()
	if err != nil {
		t.Fatal(err)
	}
	equal, err := corpusEqual(first, second)
	if err != nil || !equal {
		t.Fatalf("two in-process corpus generations differ: equal=%v err=%v", equal, err)
	}
	wantNames := []string{
		"single_module_project", "multi_module_project", "installed_pack_project", "root_pack", "asset_project",
		"all_declarations_project", "deterministic_rejection", "resource_limit_rejection", "representative_large_graph", "cancellation",
	}
	wantNames = slices.Insert(wantNames, len(wantNames)-1,
		"query_where", "query_relation_where", "query_traverse", "query_where_relation_where_traverse",
	)
	gotNames := make([]string, len(first.Cases))
	for index, test := range first.Cases {
		gotNames[index] = test.Name
	}
	if first.SchemaVersion != 1 || first.EngineReleaseVariable != engineReleaseVariable ||
		!reflect.DeepEqual(gotNames, wantNames) || len(first.RequiredFeatures) != 13 || len(first.Normalization) != 3 {
		t.Fatalf("incomplete parity corpus: names=%v features=%v normalization=%v", gotNames, first.RequiredFeatures, first.Normalization)
	}
	for _, test := range first.Cases {
		if len(test.Features) == 0 || len(test.Request.ControlBase64) == 0 || len(test.Request.Blobs) == 0 || len(test.Expected.Response) == 0 || test.Expected.Outcome == "" {
			t.Fatalf("incomplete %s vector", test.Name)
		}
		if test.Expected.Outcome == "success" && len(test.Expected.Blobs) == 0 {
			t.Fatalf("successful %s vector has no canonical bytes", test.Name)
		}
		if test.Expected.Outcome != "success" && len(test.Expected.Blobs) != 0 {
			t.Fatalf("terminal %s vector published blobs", test.Name)
		}
	}
	want, err := canonicalCorpus(first)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("..", "..", "tests", "conformance", "testdata", "engine_compile_parity_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("committed parity corpus is stale; run make generate")
	}
}

func TestGeneratedViewDataCorpusIsCurrentAndDeterministic(t *testing.T) {
	first, err := buildViewDataCorpus()
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildViewDataCorpus()
	if err != nil {
		t.Fatal(err)
	}
	left, err := canonicalViewDataCorpus(first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := canonicalViewDataCorpus(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, right) {
		t.Fatal("two in-process ViewData corpus generations differ")
	}
	if first.SchemaVersion != 1 || len(first.Documents) != 6 || len(first.Cases) != 20 ||
		len(first.RequiredShapes) != 7 || len(first.RequiredProjectionModes) != 14 ||
		len(first.RequiredStatePolicies) != 3 || len(first.RequiredFailureClasses) != 4 {
		t.Fatalf("incomplete ViewData corpus: documents=%d cases=%d", len(first.Documents), len(first.Cases))
	}
	for _, testCase := range first.Cases {
		failureCase := slices.Contains(testCase.Features, "invalid_input") || slices.Contains(testCase.Features, "limit_exceeded") ||
			testCase.Execution == "cancel" || testCase.Execution == "malformed_wire"
		if failureCase {
			if testCase.Expected.PublishesViewData || testCase.Expected.FailureClass == "" {
				t.Fatalf("%s is not a closed failure", testCase.Name)
			}
		} else if testCase.Expected.Outcome != "success" || !testCase.Expected.PublishesViewData {
			t.Fatalf("%s did not produce ViewData", testCase.Name)
		}
	}
	got, err := os.ReadFile(filepath.Join("..", "..", viewDataCorpusPath))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, left) {
		t.Fatal("committed ViewData corpus is stale; run make generate")
	}
}

func TestGenerateViewDataCorpusWritesCanonicalOutput(t *testing.T) {
	output := filepath.Join(t.TempDir(), "nested", "viewdata.json")
	if err := generateViewDataCorpus(output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil || len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("generated ViewData corpus bytes=%d err=%v", len(data), err)
	}
}

func TestViewDataCorpusValidationRejectsIncompleteCoverage(t *testing.T) {
	valid := func() viewDataCorpus {
		return viewDataCorpus{
			Documents: []viewDataCorpusDocument{{
				ID: "document",
				Input: engineprotocol.CompileInput{
					ProjectSourceTree: []engineprotocol.SourceFileInput{{}},
				},
				Blobs: []viewDataCorpusBlob{{BlobID: "source"}},
			}},
			Cases: []viewDataCorpusCase{{
				Name: "case", Execution: "materialize", Features: []string{"covered"},
				Source: viewDataCorpusSource{Kind: "query"}, Repeat: 1,
				Expected: viewDataCorpusExpected{
					Outcome: "success", PublishesViewData: true, NormalizedResponse: json.RawMessage(`{}`),
				},
			}},
		}
	}
	if err := validateViewDataCoverage(valid()); err != nil {
		t.Fatalf("minimal valid corpus: %v", err)
	}

	tests := map[string]func(*viewDataCorpus){
		"invalid document": func(corpus *viewDataCorpus) { corpus.Documents[0].ID = "" },
		"duplicate document": func(corpus *viewDataCorpus) {
			corpus.Documents = append(corpus.Documents, corpus.Documents[0])
		},
		"incomplete case":         func(corpus *viewDataCorpus) { corpus.Cases[0].Repeat = 0 },
		"unsupported execution":   func(corpus *viewDataCorpus) { corpus.Cases[0].Execution = "unknown" },
		"unsupported source":      func(corpus *viewDataCorpus) { corpus.Cases[0].Source.Kind = "unknown" },
		"missing oracle":          func(corpus *viewDataCorpus) { corpus.Cases[0].Expected.NormalizedResponse = nil },
		"partial failure":         func(corpus *viewDataCorpus) { corpus.Cases[0].Expected.Outcome = "failed" },
		"missing success payload": func(corpus *viewDataCorpus) { corpus.Cases[0].Expected.PublishesViewData = false },
		"unclassified failure": func(corpus *viewDataCorpus) {
			corpus.Cases[0].Features = []string{"invalid_input"}
			corpus.Cases[0].Expected = viewDataCorpusExpected{Outcome: "rejected", NormalizedResponse: json.RawMessage(`{}`)}
		},
		"missing feature": func(corpus *viewDataCorpus) { corpus.RequiredShapes = []string{"missing"} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			corpus := valid()
			mutate(&corpus)
			if err := validateViewDataCoverage(corpus); err == nil {
				t.Fatal("invalid ViewData corpus was accepted")
			}
		})
	}

	invalid := valid()
	invalid.Cases[0].Expected.NormalizedResponse = json.RawMessage("{")
	if _, err := canonicalViewDataCorpus(invalid); err == nil {
		t.Fatal("invalid raw ViewData response was canonically encoded")
	}
}

func TestViewDataNormalizationClassifiesClosedOutcomes(t *testing.T) {
	if _, err := normalizeViewDataResponse([]byte("{"), nil); err == nil {
		t.Fatal("invalid response was normalized")
	}
	if got := normalizedViewDataOutcome(json.RawMessage("{")); got != "" {
		t.Fatalf("invalid outcome = %q", got)
	}
	if viewDataResponsePublishes(json.RawMessage("{")) {
		t.Fatal("invalid response published ViewData")
	}
	for name, test := range map[string]struct {
		response json.RawMessage
		outcome  string
		want     string
	}{
		"rejected":  {json.RawMessage(`{}`), "rejected", "invalid_input"},
		"limit":     {json.RawMessage(`{"failure":{"code":"engine.workbench.limit_exceeded"}}`), "failed", "limit_exceeded"},
		"cancelled": {json.RawMessage(`{"failure":{"code":"engine.workbench.cancelled"}}`), "cancelled", "cancelled"},
		"unknown":   {json.RawMessage(`{"failure":{"code":"unknown"}}`), "failed", ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := viewDataFailureClass(test.response, test.outcome); got != test.want {
				t.Fatalf("failure class = %q want %q", got, test.want)
			}
		})
	}
}

func TestGenerateWritesCanonicalCorpusAndRejectsInvalidTargets(t *testing.T) {
	output := filepath.Join(t.TempDir(), "nested", "corpus.json")
	if err := generate(output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil || len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("generated corpus bytes=%d err=%v", len(data), err)
	}

	parentFile := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(parentFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := generate(filepath.Join(parentFile, "corpus.json")); err == nil {
		t.Fatal("generator accepted a file as the output parent")
	}
	if err := generate(t.TempDir()); err == nil {
		t.Fatal("generator accepted a directory as the output file")
	}
}

func TestCorpusValidationAndPortableLimitOverrides(t *testing.T) {
	if err := validateCoverage(parityCorpus{Cases: []parityCase{{Name: "bad", Execution: "unknown"}}}); err == nil {
		t.Fatal("unknown execution mode was accepted")
	}
	if err := validateCoverage(parityCorpus{
		RequiredFeatures: []string{"missing"},
		Cases:            []parityCase{{Name: "valid", Execution: "compile", Features: []string{"present"}}},
	}); err == nil {
		t.Fatal("missing required feature was accepted")
	}

	values := make([]protocolcommon.CanonicalNonNegativeInt64, 9)
	for index := range values {
		values[index] = protocolcommon.CanonicalNonNegativeInt64(string(rune('1' + index)))
	}
	overrides := engineprotocol.ResourceLimits{
		MaxProjectSourceFiles: &values[0],
		MaxProjectSourceBytes: &values[1],
		MaxPackFiles:          &values[2],
		MaxPackBytes:          &values[3],
		MaxAssets:             &values[4],
		MaxAssetBytes:         &values[5],
		MaxRasterDimension:    &values[6],
		MaxRasterPixels:       &values[7],
		MaxDeclarations:       &values[8],
	}
	if got := portableResourceLimits(overrides); !reflect.DeepEqual(got, overrides) {
		t.Fatalf("portable overrides=%+v want=%+v", got, overrides)
	}

	invalid := parityCorpus{Cases: []parityCase{{Expected: parityExpected{Response: json.RawMessage("{")}}}}
	if _, err := canonicalCorpus(invalid); err == nil {
		t.Fatal("invalid raw response was canonically encoded")
	}
	if _, err := corpusEqual(invalid, parityCorpus{}); err == nil {
		t.Fatal("invalid left corpus was compared")
	}
	if _, err := corpusEqual(parityCorpus{}, invalid); err == nil {
		t.Fatal("invalid right corpus was compared")
	}
}

// loadFixtureQueryRecipeDocument decodes the committed query-recipe blob for
// the fully-claused "query_where_relation_where_traverse" fixture case, so
// mismatch tests can mutate a schema-valid document instead of re-deriving
// one from a fresh compile.
func loadFixtureQueryRecipeDocument(t *testing.T) semantic.CompiledQueryRecipeDocument {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "conformance", "testdata", "engine_compile_parity_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus parityCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range corpus.Cases {
		if testCase.Name != "query_where_relation_where_traverse" {
			continue
		}
		for _, blob := range testCase.Expected.Blobs {
			if blob.MediaType != string(engineprotocol.QueryRecipeBlobRefMediaTypeValue) {
				continue
			}
			raw, err := base64.StdEncoding.DecodeString(blob.BytesBase64)
			if err != nil {
				t.Fatal(err)
			}
			document, err := semantic.DecodeCompiledQueryRecipeDocument(raw)
			if err != nil {
				t.Fatal(err)
			}
			return document
		}
	}
	t.Fatal("fixture is missing the query recipe blob")
	return semantic.CompiledQueryRecipeDocument{}
}

func queryRecipeBlobs(t *testing.T, document semantic.CompiledQueryRecipeDocument) []endpoint.OutputBlob {
	t.Helper()
	encoded, err := semantic.EncodeCompiledQueryRecipeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	return []endpoint.OutputBlob{{
		Ref:   protocolcommon.BlobRef{MediaType: string(engineprotocol.QueryRecipeBlobRefMediaTypeValue)},
		Bytes: encoded,
	}}
}

func TestAssertQueryClauseRecipeRejectsMismatches(t *testing.T) {
	fixture := loadFixtureQueryRecipeDocument(t)

	full := queryClauseExpectation{where: true, relationWhere: true, traverse: true}
	if err := assertQueryClauseRecipe("query_where_relation_where_traverse", queryRecipeBlobs(t, fixture), full); err != nil {
		t.Fatalf("well-formed fixture was rejected: %v", err)
	}

	if err := assertQueryClauseRecipe("missing", nil, full); err == nil || !strings.Contains(err.Error(), "did not publish a query recipe") {
		t.Fatalf("blob-less recipe error = %v", err)
	}

	malformed := []endpoint.OutputBlob{{
		Ref:   protocolcommon.BlobRef{MediaType: string(engineprotocol.QueryRecipeBlobRefMediaTypeValue)},
		Bytes: []byte("not json"),
	}}
	if err := assertQueryClauseRecipe("malformed", malformed, full); err == nil || !strings.Contains(err.Error(), "decode query recipe") {
		t.Fatalf("malformed recipe error = %v", err)
	}

	whereMismatch := fixture
	mutatedWhereChildren := slices.Clone(*fixture.Recipe.Where.Children)
	mutatedValue := "beta"
	mutatedWhereChildren[0].Value = &semantic.RecipePredicateValue{
		Kind:        "scalar",
		ScalarValue: &semantic.RecipeScalar{Kind: "string", StringValue: &mutatedValue},
	}
	whereMismatch.Recipe.Where = semantic.RecipePredicate{Kind: "all", Children: &mutatedWhereChildren}
	if err := assertQueryClauseRecipe("where_mismatch", queryRecipeBlobs(t, whereMismatch), full); err == nil || !strings.Contains(err.Error(), "where recipe") {
		t.Fatalf("where mismatch error = %v", err)
	}

	relationWhereMismatch := fixture
	mutatedRelationChildren := slices.Clone(*fixture.Recipe.RelationWhere.Children)
	mutatedOperator := "ne"
	mutatedRelationChildren[0].Operator = &mutatedOperator
	relationWhereMismatch.Recipe.RelationWhere = semantic.RecipePredicate{Kind: "all", Children: &mutatedRelationChildren}
	if err := assertQueryClauseRecipe("relation_where_mismatch", queryRecipeBlobs(t, relationWhereMismatch), full); err == nil || !strings.Contains(err.Error(), "relation_where recipe") {
		t.Fatalf("relation_where mismatch error = %v", err)
	}

	if err := assertQueryClauseRecipe("unexpected_traverse", queryRecipeBlobs(t, fixture), queryClauseExpectation{where: true, relationWhere: true}); err == nil ||
		!strings.Contains(err.Error(), "unexpected traverse recipe") {
		t.Fatalf("unexpected traverse error = %v", err)
	}

	traverseMismatch := fixture
	mutatedTraverse := *fixture.Recipe.Traverse
	mutatedTraverse.Direction = "incoming"
	traverseMismatch.Recipe.Traverse = &mutatedTraverse
	if err := assertQueryClauseRecipe("traverse_mismatch", queryRecipeBlobs(t, traverseMismatch), full); err == nil ||
		!strings.Contains(err.Error(), "traverse recipe does not preserve the authored traversal") {
		t.Fatalf("traverse mismatch error = %v", err)
	}
}

func TestAssertFieldPredicateRejectsMismatches(t *testing.T) {
	field := "id"
	value := "alpha"
	otherField := "other"

	stringType := semantic.ValueType("string")
	relationType := semantic.SubjectKind("relation_type")
	address := semantic.StableAddress("ldl:project:p:relation-type:link")
	otherAddress := semantic.StableAddress("ldl:project:p:relation-type:other")

	validScalarChild := semantic.RecipePredicate{
		Kind: "field", Field: &field, Operator: ptr("eq"),
		OperandType: &semantic.RecipeOperandType{Kind: "scalar", ScalarType: &stringType},
		Value:       &semantic.RecipePredicateValue{Kind: "scalar", ScalarValue: &semantic.RecipeScalar{Kind: "string", StringValue: &value}},
	}
	validAddressChild := semantic.RecipePredicate{
		Kind: "field", Field: &field, Operator: ptr("eq"),
		OperandType: &semantic.RecipeOperandType{Kind: "address", AddressKind: &relationType},
		Value:       &semantic.RecipePredicateValue{Kind: "address", AddressValue: &address},
	}

	if err := assertFieldPredicate(semantic.RecipePredicate{Kind: "field"}, false, field, "scalar", "string", value); err == nil {
		t.Fatal("non-empty-shaped omitted predicate was accepted")
	}
	if err := assertFieldPredicate(semantic.RecipePredicate{Kind: "all", Children: &[]semantic.RecipePredicate{}}, false, field, "scalar", "string", value); err != nil {
		t.Fatalf("well-formed omitted predicate was rejected: %v", err)
	}

	if err := assertFieldPredicate(semantic.RecipePredicate{Kind: "field"}, true, field, "scalar", "string", value); err == nil {
		t.Fatal("non-all-shaped predicate tree was accepted")
	}
	if err := assertFieldPredicate(semantic.RecipePredicate{Kind: "all", Children: &[]semantic.RecipePredicate{validScalarChild, validScalarChild}}, true, field, "scalar", "string", value); err == nil {
		t.Fatal("multi-child predicate tree was accepted")
	}

	wrongKindChild := validScalarChild
	wrongKindChild.Kind = "row"
	if err := assertFieldPredicate(semantic.RecipePredicate{Kind: "all", Children: &[]semantic.RecipePredicate{wrongKindChild}}, true, field, "scalar", "string", value); err == nil {
		t.Fatal("non-field child kind was accepted")
	}
	wrongFieldChild := validScalarChild
	wrongFieldChild.Field = &otherField
	if err := assertFieldPredicate(semantic.RecipePredicate{Kind: "all", Children: &[]semantic.RecipePredicate{wrongFieldChild}}, true, field, "scalar", "string", value); err == nil {
		t.Fatal("mismatched field name was accepted")
	}

	if err := assertFieldPredicate(semantic.RecipePredicate{Kind: "all", Children: &[]semantic.RecipePredicate{validScalarChild}}, true, field, "scalar", "string", "beta"); err == nil {
		t.Fatal("mismatched scalar value was accepted")
	}
	if err := assertFieldPredicate(semantic.RecipePredicate{Kind: "all", Children: &[]semantic.RecipePredicate{validAddressChild}}, true, field, "address", "relation_type", string(otherAddress)); err == nil {
		t.Fatal("mismatched address value was accepted")
	}

	bogusKind := "bogus"
	bogusChild := semantic.RecipePredicate{
		Kind: "field", Field: &field, Operator: ptr("eq"),
		OperandType: &semantic.RecipeOperandType{Kind: bogusKind},
		Value:       &semantic.RecipePredicateValue{Kind: bogusKind},
	}
	if err := assertFieldPredicate(semantic.RecipePredicate{Kind: "all", Children: &[]semantic.RecipePredicate{bogusChild}}, true, field, bogusKind, "irrelevant", "irrelevant"); err == nil ||
		!strings.Contains(err.Error(), "unsupported expected operand kind") {
		t.Fatalf("unsupported operand kind error = %v", err)
	}
}

func ptr[T any](value T) *T {
	return &value
}
