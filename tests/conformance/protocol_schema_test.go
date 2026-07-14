// SPDX-License-Identifier: Apache-2.0

package conformance_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

func TestProtocolFixturesRoundTripInGeneratedGoTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		fixture string
		decode  func([]byte) (any, error)
		encode  func(any) ([]byte, error)
		check   func(*testing.T, any)
	}{
		{
			name: "compile request", fixture: "compile-request.json",
			decode: func(data []byte) (any, error) { return engineprotocol.DecodeCompileRequestEnvelope(data) },
			encode: func(raw any) ([]byte, error) {
				return engineprotocol.EncodeCompileRequestEnvelope(raw.(engineprotocol.CompileRequestEnvelope))
			},
			check: func(t *testing.T, raw any) {
				value := raw.(engineprotocol.CompileRequestEnvelope)
				if value.Operation != "engine.compile" || value.Payload.Mode != engineprotocol.CompileModeProject || len(value.Payload.ProjectSourceTree) != 1 {
					t.Fatalf("invalid compile request: %+v", value)
				}
			},
		},
		{
			name: "compile success", fixture: "compile-success.json",
			decode: func(data []byte) (any, error) { return engineprotocol.DecodeCompileResponseEnvelope(data) },
			encode: func(raw any) ([]byte, error) {
				return engineprotocol.EncodeCompileResponseEnvelope(raw.(engineprotocol.CompileResponseEnvelope))
			},
			check: func(t *testing.T, raw any) {
				value := raw.(engineprotocol.CompileResponseEnvelope)
				if value.Outcome != protocolcommon.OutcomeSuccess || value.Payload == nil || len(value.Diagnostics) != 0 {
					t.Fatalf("invalid success outcome: %+v", value)
				}
			},
		},
		{
			name: "compile Pack success", fixture: "compile-success-pack.json",
			decode: func(data []byte) (any, error) { return engineprotocol.DecodeCompileResponseEnvelope(data) },
			encode: func(raw any) ([]byte, error) {
				return engineprotocol.EncodeCompileResponseEnvelope(raw.(engineprotocol.CompileResponseEnvelope))
			},
			check: func(t *testing.T, raw any) {
				value := raw.(engineprotocol.CompileResponseEnvelope)
				if value.Payload == nil || value.Payload.NormalizedArtifact.Kind != engineprotocol.CompileModePack || value.Payload.NormalizedArtifact.Pack == nil || value.Payload.NormalizedArtifact.Project != nil {
					t.Fatalf("invalid Pack success outcome: %+v", value)
				}
			},
		},
		{
			name: "compile rejected", fixture: "compile-rejected.json",
			decode: func(data []byte) (any, error) { return engineprotocol.DecodeCompileResponseEnvelope(data) },
			encode: func(raw any) ([]byte, error) {
				return engineprotocol.EncodeCompileResponseEnvelope(raw.(engineprotocol.CompileResponseEnvelope))
			},
			check: func(t *testing.T, raw any) {
				value := raw.(engineprotocol.CompileResponseEnvelope)
				if value.Outcome != protocolcommon.OutcomeRejected || value.Payload != nil || len(value.Diagnostics) == 0 {
					t.Fatalf("invalid rejected outcome: %+v", value)
				}
			},
		},
		{
			name: "handshake success", fixture: "handshake-success.json",
			decode: func(data []byte) (any, error) { return engineprotocol.DecodeHandshakeResponseEnvelope(data) },
			encode: func(raw any) ([]byte, error) {
				return engineprotocol.EncodeHandshakeResponseEnvelope(raw.(engineprotocol.HandshakeResponseEnvelope))
			},
			check: func(t *testing.T, raw any) {
				value := raw.(engineprotocol.HandshakeResponseEnvelope)
				if value.Payload == nil || len(value.Payload.NegotiatedProtocols) != 1 ||
					string(value.Payload.NegotiatedProtocols[0].SchemaDigest) != engineprotocol.SchemaDigest {
					t.Fatalf("handshake did not bind the generated Engine schema digest: %+v", value.Payload)
				}
			},
		},
		{
			name: "handshake rejected", fixture: "handshake-rejected.json",
			decode: func(data []byte) (any, error) { return engineprotocol.DecodeHandshakeResponseEnvelope(data) },
			encode: func(raw any) ([]byte, error) {
				return engineprotocol.EncodeHandshakeResponseEnvelope(raw.(engineprotocol.HandshakeResponseEnvelope))
			},
			check: func(t *testing.T, raw any) {
				value := raw.(engineprotocol.HandshakeResponseEnvelope)
				if value.Outcome != protocolcommon.OutcomeRejected || value.Payload != nil || len(value.Diagnostics) != 1 {
					t.Fatalf("invalid handshake rejection: %+v", value)
				}
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			data := readProtocolFixture(t, test.fixture)
			value, err := test.decode(data)
			if err != nil {
				t.Fatalf("decode fixture: %v", err)
			}
			test.check(t, value)
			encoded, err := test.encode(value)
			if err != nil {
				t.Fatal(err)
			}
			var before, after any
			if err := json.Unmarshal(data, &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("round-trip changed canonical fixture\nbefore=%s\nafter=%s", data, encoded)
			}
			again, err := test.decode(encoded)
			if err != nil {
				t.Fatal(err)
			}
			reencoded, err := test.encode(again)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encoded, reencoded) {
				t.Fatalf("canonical encoding is not stable\nfirst=%s\nsecond=%s", encoded, reencoded)
			}
		})
	}
}

func TestGeneratedCompileInputBlobRefCollector(t *testing.T) {
	t.Parallel()
	root := protocolRepositoryRoot(t)
	envelope, err := engineprotocol.DecodeCompileRequestEnvelope(readRepositoryFile(t, root, "schemas/fixtures/engine/compile-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	input := envelope.Payload
	shared := input.ProjectSourceTree[0].Blob
	input.InstalledPackTree = []engineprotocol.SourceFileInput{{Path: "pack.ldl", Blob: shared}}
	input.ProjectSourceTree = append(input.ProjectSourceTree, engineprotocol.SourceFileInput{Path: "z.ldl", Blob: shared})
	input.ReferencedAssets = []engineprotocol.AssetInput{{
		Origin: engineprotocol.SourceOriginKindProject, Locator: "asset.txt", Blob: shared,
		Digest: shared.Digest, MediaType: shared.MediaType,
	}}
	input.ResolvedDependencies.Installs = []engineprotocol.ResolvedPack{{
		InstallName: "dep", CanonicalID: "publisher/pack", Version: "1.0.0", Digest: shared.Digest,
		Path: "packs/dep", Entry: "main.ldl", Files: []engineprotocol.ResolvedPackFile{},
		Dependencies: []engineprotocol.ResolvedPackDependency{}, ManifestPath: "manifest.json", Manifest: shared,
	}}

	refs, err := engineprotocol.CollectCompileInputBlobRefs(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 5 {
		t.Fatalf("collector returned %d refs, want 5", len(refs))
	}
	for index, ref := range refs {
		if !reflect.DeepEqual(ref, shared) {
			t.Fatalf("ref %d changed duplicate occurrence: got=%+v want=%+v", index, ref, shared)
		}
	}
	refs[0].BlobID = "mutated"
	if input.ProjectSourceTree[0].Blob.BlobID != shared.BlobID || refs[1].BlobID != shared.BlobID {
		t.Fatal("collector returned aliased BlobRefs")
	}

	hostile := input
	hostile.ProjectSourceTree = append([]engineprotocol.SourceFileInput(nil), input.ProjectSourceTree...)
	hostile.ProjectSourceTree[0].Blob = protocolcommon.BlobRef{}
	if _, err := engineprotocol.CollectCompileInputBlobRefs(hostile); err == nil {
		t.Fatal("collector accepted missing/invalid nested BlobRef")
	}
}

func TestGeneratedCompileResultBlobRefCollector(t *testing.T) {
	t.Parallel()
	root := protocolRepositoryRoot(t)
	envelope, err := engineprotocol.DecodeCompileResponseEnvelope(readRepositoryFile(t, root, "schemas/fixtures/engine/compile-success.json"))
	if err != nil {
		t.Fatal(err)
	}
	result := *envelope.Payload
	shared := result.CompiledRecipes.Queries[0].CanonicalJSON
	result.CompiledRecipes.Queries = append(result.CompiledRecipes.Queries, engineprotocol.CompiledQueryRecipeArtifact{
		Address: "ldl:project:fixture:query:other", CanonicalJSON: shared,
	})

	refs, err := engineprotocol.CollectCompileResultBlobRefs(result)
	if err != nil {
		t.Fatal(err)
	}
	project := result.NormalizedArtifact.Project
	if project == nil {
		t.Fatal("compile-success fixture did not contain a project artifact")
	}
	want := []string{shared.BlobID, shared.BlobID, project.ArtifactJSON.BlobID, project.CanonicalJSON.BlobID}
	if len(refs) != len(want) {
		t.Fatalf("collector returned %d refs, want %d", len(refs), len(want))
	}
	for index := range want {
		if refs[index].BlobID != want[index] {
			t.Fatalf("ref %d traversal mismatch: got=%q want=%q", index, refs[index].BlobID, want[index])
		}
	}
	refs[0].BlobID = "mutated"
	if result.CompiledRecipes.Queries[0].CanonicalJSON.BlobID != shared.BlobID || refs[1].BlobID != shared.BlobID {
		t.Fatal("collector returned aliased BlobRefs")
	}

	hostile := result
	hostile.CompiledRecipes.Queries = append([]engineprotocol.CompiledQueryRecipeArtifact(nil), result.CompiledRecipes.Queries...)
	hostile.CompiledRecipes.Queries[0].CanonicalJSON = engineprotocol.QueryRecipeBlobRef{}
	if _, err := engineprotocol.CollectCompileResultBlobRefs(hostile); err == nil {
		t.Fatal("collector accepted missing/invalid role-specific BlobRef")
	}
}

func TestGeneratedGoStrictDecodeRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	if _, err := engineprotocol.DecodeCompileRequestEnvelope(readProtocolFixture(t, "compile-invalid-request.json")); err == nil {
		t.Fatal("invalid fixture with actor_context_ref and negative unsigned limit was accepted")
	}
}

func TestGeneratedCodecsShareCanonicalCommonFixtures(t *testing.T) {
	t.Parallel()
	root := protocolRepositoryRoot(t)
	blobData, err := os.ReadFile(filepath.Join(root, "schemas", "fixtures", "common", "blob-ref-canonical.json"))
	if err != nil {
		t.Fatal(err)
	}
	blob, err := protocolcommon.DecodeBlobRef(blobData)
	if err != nil {
		t.Fatal(err)
	}
	encodedBlob, err := protocolcommon.EncodeBlobRef(blob)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(blobData), encodedBlob) {
		t.Fatalf("BlobRef canonical bytes differ\nwant=%s\ngot=%s", blobData, encodedBlob)
	}

	jsonData, err := os.ReadFile(filepath.Join(root, "schemas", "fixtures", "common", "json-value-canonical.json"))
	if err != nil {
		t.Fatal(err)
	}
	jsonValue, err := protocolcommon.DecodeJsonValue(jsonData)
	if err != nil {
		t.Fatal(err)
	}
	encodedJSON, err := protocolcommon.EncodeJsonValue(jsonValue)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(jsonData), encodedJSON) {
		t.Fatalf("recursive JSON canonical bytes differ\nwant=%s\ngot=%s", jsonData, encodedJSON)
	}
}

func TestGeneratedGoMatchesSharedCanonicalEngineEnvelopes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		decode func([]byte) ([]byte, error)
	}{
		{"compile-request.json", func(data []byte) ([]byte, error) {
			value, err := engineprotocol.DecodeCompileRequestEnvelope(data)
			if err != nil {
				return nil, err
			}
			return engineprotocol.EncodeCompileRequestEnvelope(value)
		}},
		{"compile-rejected.json", func(data []byte) ([]byte, error) {
			value, err := engineprotocol.DecodeCompileResponseEnvelope(data)
			if err != nil {
				return nil, err
			}
			return engineprotocol.EncodeCompileResponseEnvelope(value)
		}},
		{"compile-success-pack.json", func(data []byte) ([]byte, error) {
			value, err := engineprotocol.DecodeCompileResponseEnvelope(data)
			if err != nil {
				return nil, err
			}
			return engineprotocol.EncodeCompileResponseEnvelope(value)
		}},
		{"handshake-success.json", func(data []byte) ([]byte, error) {
			value, err := engineprotocol.DecodeHandshakeResponseEnvelope(data)
			if err != nil {
				return nil, err
			}
			return engineprotocol.EncodeHandshakeResponseEnvelope(value)
		}},
		{"handshake-rejected.json", func(data []byte) ([]byte, error) {
			value, err := engineprotocol.DecodeHandshakeResponseEnvelope(data)
			if err != nil {
				return nil, err
			}
			return engineprotocol.EncodeHandshakeResponseEnvelope(value)
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			canonical, err := os.ReadFile(filepath.Join(protocolRepositoryRoot(t), "schemas", "fixtures", "conformance", "engine", test.name))
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := test.decode(readProtocolFixture(t, test.name))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encoded, bytes.TrimSpace(canonical)) {
				t.Fatalf("Go engine bytes differ from shared cross-language golden\nwant=%s\ngot=%s", canonical, encoded)
			}
			if reencoded, err := test.decode(bytes.TrimSpace(canonical)); err != nil || !bytes.Equal(reencoded, encoded) {
				t.Fatalf("shared golden did not round-trip: %v", err)
			}
		})
	}
}

func TestGeneratedGoJsonValueIsTyped(t *testing.T) {
	t.Parallel()
	value, err := protocolcommon.DecodeJsonValue([]byte(`{"array":[true,"text",null]}`))
	if err != nil {
		t.Fatal(err)
	}
	if value.Kind != protocolcommon.JsonValueKindObject || value.Object["array"].Kind != protocolcommon.JsonValueKindArray || value.Object["array"].Array[0].Kind != protocolcommon.JsonValueKindBoolean {
		t.Fatalf("recursive JsonValue did not retain typed kinds: %+v", value)
	}
	if _, err := protocolcommon.EncodeJsonValue(protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKind("number")}); err == nil {
		t.Fatal("typed JsonValue admitted an unsupported host-number kind")
	}
}

func TestGeneratedGoJsonValueRejectsContradictionsCyclesAndExcessDepth(t *testing.T) {
	t.Parallel()
	invalid := []protocolcommon.JsonValue{
		{Kind: protocolcommon.JsonValueKindNull, Boolean: true},
		{Kind: protocolcommon.JsonValueKindNull, String: "inactive"},
		{Kind: protocolcommon.JsonValueKindNull, Array: []protocolcommon.JsonValue{}},
		{Kind: protocolcommon.JsonValueKindNull, Object: map[string]protocolcommon.JsonValue{}},
		{Kind: protocolcommon.JsonValueKindBoolean, String: "inactive"},
		{Kind: protocolcommon.JsonValueKindString, Boolean: true},
		{Kind: protocolcommon.JsonValueKindArray, Object: map[string]protocolcommon.JsonValue{}},
		{Kind: protocolcommon.JsonValueKindObject, Array: []protocolcommon.JsonValue{}},
	}
	for index, value := range invalid {
		if _, err := protocolcommon.EncodeJsonValue(value); err == nil {
			t.Errorf("contradictory JsonValue %d was accepted", index)
		}
	}

	self := map[string]protocolcommon.JsonValue{}
	selfValue := protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindObject, Object: self}
	self["self"] = selfValue
	if _, err := protocolcommon.EncodeJsonValue(selfValue); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("self cycle did not return a stable validation error: %v", err)
	}
	a := map[string]protocolcommon.JsonValue{}
	b := map[string]protocolcommon.JsonValue{}
	aValue := protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindObject, Object: a}
	bValue := protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindObject, Object: b}
	a["b"] = bValue
	b["a"] = aValue
	if _, err := protocolcommon.EncodeJsonValue(aValue); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("mutual cycle did not return a stable validation error: %v", err)
	}

	value := protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindString, String: "leaf"}
	for range protocolcommon.MaxWireJSONDepth {
		value = protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindArray, Array: []protocolcommon.JsonValue{value}}
	}
	encoded, err := protocolcommon.EncodeJsonValue(value)
	if err != nil {
		t.Fatalf("programmatic depth 128 was rejected: %v", err)
	}
	decoded, err := protocolcommon.DecodeJsonValue(encoded)
	if err != nil || decoded.Kind != protocolcommon.JsonValueKindArray {
		t.Fatalf("valid recursive value was not preserved: %v", err)
	}
	tooDeep := protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindArray, Array: []protocolcommon.JsonValue{value}}
	if _, err := protocolcommon.EncodeJsonValue(tooDeep); err == nil || !strings.Contains(err.Error(), "depth 128") {
		t.Fatalf("programmatic depth 129 did not return a stable validation error: %v", err)
	}
}

func TestGeneratedGoSemanticRecursiveCodecsRejectCyclesAndExcessDepth(t *testing.T) {
	t.Parallel()
	stringPointer := func(value string) *string { return &value }
	stateFieldPathPointer := func(value semantic.StateFieldPath) *semantic.StateFieldPath { return &value }
	expectCycle := func(name string, encode func() error) {
		t.Helper()
		if err := encode(); err == nil || !strings.Contains(err.Error(), "cycle") {
			t.Errorf("%s did not return a stable cycle error: %v", name, err)
		}
	}
	expectDepth := func(name string, encode func() error) {
		t.Helper()
		if err := encode(); err == nil || err.Error() != "protocol value exceeds depth 128" {
			t.Errorf("%s did not return the stable depth error: %v", name, err)
		}
	}

	selfItems := make([]semantic.DiagnosticArgumentValue, 1)
	selfDiagnostic := semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindArray, ArrayValue: &selfItems}
	selfItems[0] = selfDiagnostic
	expectCycle("DiagnosticArgumentValue self cycle", func() error {
		_, err := semantic.EncodeDiagnosticArgumentValue(selfDiagnostic)
		return err
	})
	leftItems := make([]semantic.DiagnosticArgumentValue, 1)
	rightItems := make([]semantic.DiagnosticArgumentValue, 1)
	leftDiagnostic := semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindArray, ArrayValue: &leftItems}
	rightDiagnostic := semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindArray, ArrayValue: &rightItems}
	leftItems[0], rightItems[0] = rightDiagnostic, leftDiagnostic
	expectCycle("DiagnosticArgumentValue mutual cycle", func() error {
		_, err := semantic.EncodeDiagnosticArgumentValue(leftDiagnostic)
		return err
	})
	sharedObject := map[string]semantic.DiagnosticArgumentValue{}
	sharedDiagnostic := semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindObject, ObjectValue: &sharedObject}
	aliasedItems := []semantic.DiagnosticArgumentValue{sharedDiagnostic, sharedDiagnostic}
	if _, err := semantic.EncodeDiagnosticArgumentValue(semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindArray, ArrayValue: &aliasedItems}); err != nil {
		t.Fatalf("DiagnosticArgumentValue rejected an acyclic shared alias: %v", err)
	}

	emptyItems := []semantic.DiagnosticArgumentValue{}
	diagnosticAtLimit := semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindArray, ArrayValue: &emptyItems}
	for range 63 {
		items := []semantic.DiagnosticArgumentValue{diagnosticAtLimit}
		diagnosticAtLimit = semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindArray, ArrayValue: &items}
	}
	if _, err := semantic.EncodeDiagnosticArgumentValue(diagnosticAtLimit); err != nil {
		t.Fatalf("DiagnosticArgumentValue rejected wire depth 128: %v", err)
	}
	diagnosticTooDeep := semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindString, StringValue: stringPointer("leaf")}
	for range 64 {
		items := []semantic.DiagnosticArgumentValue{diagnosticTooDeep}
		diagnosticTooDeep = semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindArray, ArrayValue: &items}
	}
	expectDepth("DiagnosticArgumentValue wire depth 129", func() error {
		_, err := semantic.EncodeDiagnosticArgumentValue(diagnosticTooDeep)
		return err
	})

	selfPredicate := &semantic.RecipePredicate{Kind: "not"}
	selfPredicate.Child = selfPredicate
	expectCycle("RecipePredicate self cycle", func() error {
		_, err := semantic.EncodeRecipePredicate(*selfPredicate)
		return err
	})
	leftPredicate := &semantic.RecipePredicate{Kind: "not"}
	rightPredicate := &semantic.RecipePredicate{Kind: "not", Child: leftPredicate}
	leftPredicate.Child = rightPredicate
	expectCycle("RecipePredicate mutual cycle", func() error {
		_, err := semantic.EncodeRecipePredicate(*leftPredicate)
		return err
	})
	sharedPredicate := &semantic.RecipePredicate{Kind: "field", Field: stringPointer("name"), Operator: stringPointer("exists")}
	predicateAliases := []semantic.RecipePredicate{{Kind: "not", Child: sharedPredicate}, {Kind: "not", Child: sharedPredicate}}
	if _, err := semantic.EncodeRecipePredicate(semantic.RecipePredicate{Kind: "all", Children: &predicateAliases}); err != nil {
		t.Fatalf("RecipePredicate rejected an acyclic shared alias: %v", err)
	}
	predicateAtLimit := sharedPredicate
	for range semantic.MaxWireJSONDepth - 1 {
		predicateAtLimit = &semantic.RecipePredicate{Kind: "not", Child: predicateAtLimit}
	}
	if _, err := semantic.EncodeRecipePredicate(*predicateAtLimit); err != nil {
		t.Fatalf("RecipePredicate rejected wire depth 128: %v", err)
	}
	expectDepth("RecipePredicate wire depth 129", func() error {
		_, err := semantic.EncodeRecipePredicate(semantic.RecipePredicate{Kind: "not", Child: predicateAtLimit})
		return err
	})

	selfRow := &semantic.RecipeRowPredicate{Kind: "not"}
	selfRow.Child = selfRow
	expectCycle("RecipeRowPredicate self cycle", func() error {
		_, err := semantic.EncodeRecipeRowPredicate(*selfRow)
		return err
	})
	leftRow := &semantic.RecipeRowPredicate{Kind: "not"}
	rightRow := &semantic.RecipeRowPredicate{Kind: "not", Child: leftRow}
	leftRow.Child = rightRow
	expectCycle("RecipeRowPredicate mutual cycle", func() error {
		_, err := semantic.EncodeRecipeRowPredicate(*leftRow)
		return err
	})
	sharedRow := &semantic.RecipeRowPredicate{Kind: "state", FieldPath: stateFieldPathPointer("system.updated_at"), Operator: stringPointer("exists")}
	rowAliases := []semantic.RecipeRowPredicate{{Kind: "not", Child: sharedRow}, {Kind: "not", Child: sharedRow}}
	if _, err := semantic.EncodeRecipeRowPredicate(semantic.RecipeRowPredicate{Kind: "all", Children: &rowAliases}); err != nil {
		t.Fatalf("RecipeRowPredicate rejected an acyclic shared alias: %v", err)
	}
	rowAtLimit := sharedRow
	for range semantic.MaxWireJSONDepth - 1 {
		rowAtLimit = &semantic.RecipeRowPredicate{Kind: "not", Child: rowAtLimit}
	}
	if _, err := semantic.EncodeRecipeRowPredicate(*rowAtLimit); err != nil {
		t.Fatalf("RecipeRowPredicate rejected wire depth 128: %v", err)
	}
	expectDepth("RecipeRowPredicate wire depth 129", func() error {
		_, err := semantic.EncodeRecipeRowPredicate(semantic.RecipeRowPredicate{Kind: "not", Child: rowAtLimit})
		return err
	})
}

func TestGeneratedGoEngineCodecAppliesSharedCycleAndDepthPreflight(t *testing.T) {
	t.Parallel()
	decodeRequest := func() engineprotocol.CompileRequestEnvelope {
		t.Helper()
		value, err := engineprotocol.DecodeCompileRequestEnvelope(readProtocolFixture(t, "compile-request.json"))
		if err != nil {
			t.Fatal(err)
		}
		return value
	}

	aliased := decodeRequest()
	sharedMap := map[string]protocolcommon.JsonValue{}
	shared := protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindObject, Object: sharedMap}
	aliasExtensions := protocolcommon.Extensions{"example.left": shared, "example.right": shared}
	aliased.Extensions = &aliasExtensions
	if _, err := engineprotocol.EncodeCompileRequestEnvelope(aliased); err != nil {
		t.Fatalf("engine codec rejected an acyclic shared alias: %v", err)
	}

	cyclic := decodeRequest()
	selfMap := map[string]protocolcommon.JsonValue{}
	self := protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindObject, Object: selfMap}
	selfMap["self"] = self
	cycleExtensions := protocolcommon.Extensions{"example.cycle": self}
	cyclic.Extensions = &cycleExtensions
	if _, err := engineprotocol.EncodeCompileRequestEnvelope(cyclic); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("engine codec did not return a stable cycle error: %v", err)
	}

	atLimit := decodeRequest()
	extension := protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindString, String: "leaf"}
	for range protocolcommon.MaxWireJSONDepth - 2 {
		extension = protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindArray, Array: []protocolcommon.JsonValue{extension}}
	}
	depthExtensions := protocolcommon.Extensions{"example.depth": extension}
	atLimit.Extensions = &depthExtensions
	if _, err := engineprotocol.EncodeCompileRequestEnvelope(atLimit); err != nil {
		t.Fatalf("engine codec rejected wire depth 128: %v", err)
	}

	tooDeep := decodeRequest()
	tooDeepExtensions := protocolcommon.Extensions{"example.depth": {Kind: protocolcommon.JsonValueKindArray, Array: []protocolcommon.JsonValue{extension}}}
	tooDeep.Extensions = &tooDeepExtensions
	if _, err := engineprotocol.EncodeCompileRequestEnvelope(tooDeep); err == nil || err.Error() != "protocol value exceeds depth 128" {
		t.Fatalf("engine codec did not return the stable depth error: %v", err)
	}
}

func FuzzGeneratedGoRecursiveCodecBoundaries(f *testing.F) {
	for _, seed := range []struct {
		codec, depth uint8
		cycle        bool
	}{{0, 0, false}, {0, 64, false}, {1, 127, false}, {1, 128, false}, {2, 4, true}} {
		f.Add(seed.codec, seed.depth, seed.cycle)
	}
	f.Fuzz(func(t *testing.T, codec, rawDepth uint8, cycle bool) {
		codec %= 3
		depth := int(rawDepth % 140)
		var wireDepth int
		var err error
		switch codec {
		case 0:
			if cycle {
				items := make([]semantic.DiagnosticArgumentValue, 1)
				value := semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindArray, ArrayValue: &items}
				items[0] = value
				_, err = semantic.EncodeDiagnosticArgumentValue(value)
				break
			}
			text := "leaf"
			value := semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindString, StringValue: &text}
			for range depth {
				items := []semantic.DiagnosticArgumentValue{value}
				value = semantic.DiagnosticArgumentValue{Kind: semantic.DiagnosticArgumentKindArray, ArrayValue: &items}
			}
			wireDepth = 1 + 2*depth
			_, err = semantic.EncodeDiagnosticArgumentValue(value)
		case 1:
			field, operator := "name", "exists"
			value := &semantic.RecipePredicate{Kind: "field", Field: &field, Operator: &operator}
			if cycle {
				value = &semantic.RecipePredicate{Kind: "not"}
				value.Child = value
			} else {
				for range depth {
					value = &semantic.RecipePredicate{Kind: "not", Child: value}
				}
				wireDepth = 1 + depth
			}
			_, err = semantic.EncodeRecipePredicate(*value)
		case 2:
			fieldPath, operator := semantic.StateFieldPath("system.updated_at"), "exists"
			value := &semantic.RecipeRowPredicate{Kind: "state", FieldPath: &fieldPath, Operator: &operator}
			if cycle {
				value = &semantic.RecipeRowPredicate{Kind: "not"}
				value.Child = value
			} else {
				for range depth {
					value = &semantic.RecipeRowPredicate{Kind: "not", Child: value}
				}
				wireDepth = 1 + depth
			}
			_, err = semantic.EncodeRecipeRowPredicate(*value)
		}
		if cycle {
			if err == nil || !strings.Contains(err.Error(), "cycle") {
				t.Fatalf("codec %d did not reject a cycle with a stable error: %v", codec, err)
			}
			return
		}
		if wireDepth <= semantic.MaxWireJSONDepth {
			if err != nil {
				t.Fatalf("codec %d rejected wire depth %d: %v", codec, wireDepth, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "depth 128") {
			t.Fatalf("codec %d did not reject wire depth %d with a stable error: %v", codec, wireDepth, err)
		}
	})
}

func TestGeneratedGoCanonicalByteLimitUsesEmittedBytes(t *testing.T) {
	for _, fill := range []string{"<", ">", "&", "\u2028", "\u2029"} {
		fill := fill
		t.Run(fmt.Sprintf("text_%U", []rune(fill)[0]), func(t *testing.T) {
			base := semantic.SearchField{FieldPath: "p", IncludeInEmbedding: false, LexicalWeight: 1, Text: ""}
			empty, err := semantic.EncodeSearchField(base)
			if err != nil {
				t.Fatal(err)
			}
			base.Text = fill
			one, err := semantic.EncodeSearchField(base)
			if err != nil {
				t.Fatal(err)
			}
			unitBytes := len(one) - len(empty)
			available := semantic.MaxWireJSONBytes - len(empty)
			base.Text = strings.Repeat(fill, available/unitBytes) + strings.Repeat("a", available%unitBytes)
			maximum, err := semantic.EncodeSearchField(base)
			if err != nil || len(maximum) != semantic.MaxWireJSONBytes {
				t.Fatalf("exact emitted-byte maximum failed: bytes=%d err=%v", len(maximum), err)
			}
			base.Text += "a"
			if _, err := semantic.EncodeSearchField(base); err == nil {
				t.Fatal("value one emitted byte beyond the maximum was accepted")
			}
		})
	}
	for _, key := range []string{"界", "😀"} {
		key := key
		t.Run(fmt.Sprintf("key_%U", []rune(key)[0]), func(t *testing.T) {
			value := protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindObject, Object: map[string]protocolcommon.JsonValue{
				key: {Kind: protocolcommon.JsonValueKindString},
			}}
			empty, err := protocolcommon.EncodeJsonValue(value)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Repeat("a", protocolcommon.MaxWireJSONBytes-len(empty))
			value.Object[key] = protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindString, String: text}
			maximum, err := protocolcommon.EncodeJsonValue(value)
			if err != nil || len(maximum) != protocolcommon.MaxWireJSONBytes {
				t.Fatalf("multibyte-key maximum failed: bytes=%d err=%v", len(maximum), err)
			}
			value.Object[key] = protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindString, String: text + "a"}
			if _, err := protocolcommon.EncodeJsonValue(value); err == nil {
				t.Fatal("multibyte-key value one byte beyond the maximum was accepted")
			}
		})
	}
}

type sharedConformanceCorpus struct {
	SchemaVersion        int                                            `json:"schema_version"`
	MaxJSONBytes         int                                            `json:"max_json_bytes"`
	MaxJSONDepth         int                                            `json:"max_json_depth"`
	CanonicalCases       []struct{ Name, Type, Input, Expected string } `json:"canonical_cases"`
	RejectionCases       []struct{ Name, Type, Input string }           `json:"rejection_cases"`
	MutationCases        []struct{ Name, Fixture, Mutation string }     `json:"mutation_cases"`
	RequestMutationCases []struct{ Name, Fixture, Mutation string }     `json:"request_mutation_cases"`
}

func TestSharedCanonicalAndRejectionCorpus(t *testing.T) {
	t.Parallel()
	corpus := readSharedConformanceCorpus(t)
	if corpus.SchemaVersion != 1 || corpus.MaxJSONBytes != protocolcommon.MaxWireJSONBytes || corpus.MaxJSONDepth != protocolcommon.MaxWireJSONDepth {
		t.Fatalf("shared corpus limits differ from generated metadata: %+v", corpus)
	}
	for _, test := range corpus.CanonicalCases {
		test := test
		t.Run(test.Name, func(t *testing.T) {
			encoded, err := roundTripSharedWire(test.Type, []byte(test.Input))
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != test.Expected {
				t.Fatalf("canonical bytes differ\nwant=%s\ngot=%s", test.Expected, encoded)
			}
			reencoded, err := roundTripSharedWire(test.Type, encoded)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encoded, reencoded) {
				t.Fatalf("other-runtime golden bytes were not stable: %s != %s", encoded, reencoded)
			}
		})
	}
	for _, test := range corpus.RejectionCases {
		test := test
		t.Run(test.Name, func(t *testing.T) {
			if _, err := roundTripSharedWire(test.Type, []byte(test.Input)); err == nil {
				t.Fatalf("accepted rejected shared vector %s", test.Input)
			}
		})
	}
	if _, err := protocolcommon.DecodeJsonValue([]byte{'"', 0xff, '"'}); err == nil {
		t.Fatal("malformed UTF-8 was accepted")
	}
}

func TestSharedCustomFormatAuthorityVectorsMatchGoCodecs(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(protocolRepositoryRoot(t), "schemas", "fixtures", "conformance", "formats-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		SchemaVersion int `json:"schema_version"`
		Vectors       []struct {
			Name  string `json:"name"`
			Type  string `json:"type"`
			Value string `json:"value"`
			Valid bool   `json:"valid"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != 1 {
		t.Fatalf("unsupported format corpus version %d", corpus.SchemaVersion)
	}
	for _, vector := range corpus.Vectors {
		vector := vector
		t.Run(vector.Name, func(t *testing.T) {
			input, err := json.Marshal(vector.Value)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := roundTripSharedWire(vector.Type, input)
			if vector.Valid {
				if err != nil || !bytes.Equal(encoded, input) {
					t.Fatalf("valid format vector rejected or changed: %s, %v", encoded, err)
				}
			} else if err == nil {
				t.Fatalf("invalid format vector accepted: %s", input)
			}
		})
	}
}

func TestSharedExportOptionVariantCorpus(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(protocolRepositoryRoot(t), "schemas", "fixtures", "conformance", "export-options-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		SchemaVersion int                                      `json:"schema_version"`
		Canonical     []struct{ Name, Input, Expected string } `json:"canonical_cases"`
		Rejections    []struct{ Name, Input string }           `json:"rejection_cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != 1 || len(corpus.Canonical) != 15 || len(corpus.Rejections) != 15 {
		t.Fatalf("incomplete export option corpus: %+v", corpus)
	}
	for _, vector := range corpus.Canonical {
		vector := vector
		t.Run(vector.Name+" canonical", func(t *testing.T) {
			encoded, err := roundTripSharedWire("ExportOptions", []byte(vector.Input))
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != vector.Expected {
				t.Fatalf("canonical export bytes differ\nwant=%s\ngot=%s", vector.Expected, encoded)
			}
		})
	}
	for _, vector := range corpus.Rejections {
		vector := vector
		t.Run(vector.Name+" rejection", func(t *testing.T) {
			if _, err := roundTripSharedWire("ExportOptions", []byte(vector.Input)); err == nil {
				t.Fatal("invalid export option variant accepted")
			}
		})
	}
}

func TestSharedPredicateVariantCorpus(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(protocolRepositoryRoot(t), "schemas", "fixtures", "conformance", "predicates-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		SchemaVersion int                                            `json:"schema_version"`
		Canonical     []struct{ Name, Type, Input, Expected string } `json:"canonical_cases"`
		Rejections    []struct{ Name, Type, Input string }           `json:"rejection_cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != 1 || len(corpus.Canonical) != 11 || len(corpus.Rejections) != 11 {
		t.Fatalf("incomplete predicate corpus: %+v", corpus)
	}
	for _, vector := range corpus.Canonical {
		vector := vector
		t.Run(vector.Name+" canonical", func(t *testing.T) {
			encoded, err := roundTripSharedWire(vector.Type, []byte(vector.Input))
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != vector.Expected {
				t.Fatalf("canonical predicate bytes differ\nwant=%s\ngot=%s", vector.Expected, encoded)
			}
		})
	}
	for _, vector := range corpus.Rejections {
		vector := vector
		t.Run(vector.Name+" rejection", func(t *testing.T) {
			if _, err := roundTripSharedWire(vector.Type, []byte(vector.Input)); err == nil {
				t.Fatal("invalid predicate variant accepted")
			}
		})
	}
}

func TestSharedViewSourceVariantCorpus(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(protocolRepositoryRoot(t), "schemas", "fixtures", "conformance", "view-sources-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		SchemaVersion int                                            `json:"schema_version"`
		Canonical     []struct{ Name, Type, Input, Expected string } `json:"canonical_cases"`
		Rejections    []struct{ Name, Type, Input string }           `json:"rejection_cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != 1 || len(corpus.Canonical) != 30 || len(corpus.Rejections) != 59 {
		t.Fatalf("incomplete View source corpus: %+v", corpus)
	}
	for _, vector := range corpus.Canonical {
		vector := vector
		t.Run(vector.Name+" canonical", func(t *testing.T) {
			encoded, err := roundTripSharedWire(vector.Type, []byte(vector.Input))
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != vector.Expected {
				t.Fatalf("canonical View source bytes differ\nwant=%s\ngot=%s", vector.Expected, encoded)
			}
		})
	}
	for _, vector := range corpus.Rejections {
		vector := vector
		t.Run(vector.Name+" rejection", func(t *testing.T) {
			if _, err := roundTripSharedWire(vector.Type, []byte(vector.Input)); err == nil {
				t.Fatal("invalid View source variant accepted")
			}
		})
	}
}

func TestSharedViewExportSemanticCorpus(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(protocolRepositoryRoot(t), "schemas", "fixtures", "conformance", "view-export-semantics-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		SchemaVersion int `json:"schema_version"`
		Canonical     []struct {
			Name  string          `json:"name"`
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"canonical_cases"`
		Rejections []struct {
			Name  string          `json:"name"`
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"rejection_cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != 1 || len(corpus.Canonical) != 19 || len(corpus.Rejections) != 50 {
		t.Fatalf("incomplete View/Export semantic corpus: version=%d canonical=%d rejection=%d", corpus.SchemaVersion, len(corpus.Canonical), len(corpus.Rejections))
	}
	for _, vector := range corpus.Canonical {
		vector := vector
		t.Run(vector.Name+" canonical", func(t *testing.T) {
			encoded, err := roundTripSharedWire(vector.Type, vector.Value)
			if err != nil {
				t.Fatal(err)
			}
			var want, got any
			if err := json.Unmarshal(vector.Value, &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("canonical View/Export value changed\nwant=%s\ngot=%s", vector.Value, encoded)
			}
			if _, err := roundTripSharedWire(vector.Type, encoded); err != nil {
				t.Fatalf("canonical View/Export output was not stable: %v", err)
			}
		})
	}
	for _, vector := range corpus.Rejections {
		vector := vector
		t.Run(vector.Name+" rejection", func(t *testing.T) {
			if _, err := roundTripSharedWire(vector.Type, vector.Value); err == nil {
				t.Fatalf("invalid View/Export semantic vector accepted: %s", vector.Value)
			}
		})
	}
}

func TestSharedScalarUnicodeCorpus(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(protocolRepositoryRoot(t), "schemas", "fixtures", "conformance", "unicode-scalars-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		SchemaVersion int                                            `json:"schema_version"`
		Canonical     []struct{ Name, Type, Input, Expected string } `json:"canonical_cases"`
		Rejections    []struct{ Name, Type, Input string }           `json:"rejection_cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != 1 || len(corpus.Canonical) != 2 || len(corpus.Rejections) != 9 {
		t.Fatalf("incomplete scalar Unicode corpus: %+v", corpus)
	}
	for _, vector := range corpus.Canonical {
		vector := vector
		t.Run(vector.Name+" canonical", func(t *testing.T) {
			encoded, err := roundTripSharedWire(vector.Type, []byte(vector.Input))
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != vector.Expected {
				t.Fatalf("canonical scalar Unicode bytes differ\nwant=%s\ngot=%s", vector.Expected, encoded)
			}
		})
	}
	for _, vector := range corpus.Rejections {
		vector := vector
		t.Run(vector.Name+" rejection", func(t *testing.T) {
			if _, err := roundTripSharedWire(vector.Type, []byte(vector.Input)); err == nil {
				t.Fatal("non-scalar Unicode accepted")
			}
		})
	}
}

func TestGeneratedTypedRootAndViewSourceInputsAreClosed(t *testing.T) {
	t.Parallel()
	root := protocolRepositoryRoot(t)
	projectEnvelope, err := engineprotocol.DecodeCompileResponseEnvelope(readRepositoryFile(t, root, "schemas/fixtures/engine/compile-success.json"))
	if err != nil {
		t.Fatal(err)
	}
	packEnvelope, err := engineprotocol.DecodeCompileResponseEnvelope(readRepositoryFile(t, root, "schemas/fixtures/engine/compile-success-pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	project := *projectEnvelope.Payload.NormalizedArtifact.Project
	pack := *packEnvelope.Payload.NormalizedArtifact.Pack
	for name, address := range map[string]semantic.ProjectRootAddress{
		"cross_origin": "ldl:pack:publisher:pack",
		"non_root":     "ldl:project:p:view:v",
	} {
		t.Run("project_"+name, func(t *testing.T) {
			candidate := project
			candidate.ProjectAddress = address
			if _, err := engineprotocol.EncodeNormalizedProjectArtifact(candidate); err == nil {
				t.Fatal("invalid typed Project normalized root accepted")
			}
		})
	}
	for name, address := range map[string]semantic.PackRootAddress{
		"cross_origin": "ldl:project:p",
		"non_root":     "ldl:pack:publisher:pack:view:v",
	} {
		t.Run("pack_"+name, func(t *testing.T) {
			candidate := pack
			candidate.PackAddress = address
			if _, err := engineprotocol.EncodeNormalizedPackArtifact(candidate); err == nil {
				t.Fatal("invalid typed Pack normalized root accepted")
			}
		})
	}

	column := []semantic.ColumnAddress{"ldl:project:p:entity-type:t:column:c"}
	relationType := []semantic.RelationTypeAddress{"ldl:project:p:relation-type:r"}
	validColumns := []semantic.ViewTableColumnSource{
		{Kind: "field", Field: protocolTestString("tags")},
		{Kind: "attribute", ColumnAddresses: &column},
		{Kind: "relation_endpoint", Endpoint: protocolTestString("from"), Field: protocolTestString("display_name")},
		{Kind: "derived_count", Direction: protocolTestString("both"), RelationTypeAddresses: &relationType},
		{Kind: "state", FieldPath: protocolTestStateFieldPath("system.updated_at")},
	}
	for index, value := range validColumns {
		if _, err := semantic.EncodeViewTableColumnSource(value); err != nil {
			t.Fatalf("valid typed column branch %d rejected: %v", index, err)
		}
	}
	invalidColumns := []semantic.ViewTableColumnSource{
		{Kind: "field"},
		{Kind: "attribute", ColumnAddresses: &column, Field: protocolTestString("id")},
		{Kind: "attribute", ColumnAddresses: protocolTestColumnAddresses("ldl:project:p")},
		{Kind: "attribute", ColumnAddresses: protocolTestColumnAddresses("ldl:project:p:entity-type:t:column:c", "ldl:project:p:entity-type:t:column:c")},
		{Kind: "attribute", ColumnAddresses: protocolTestColumnAddresses("ldl:pack:publisher:shared-pack:entity-type:t:column:c", "ldl:project:p:entity-type:t:column:c")},
		{Kind: "relation_endpoint", Endpoint: protocolTestString("from"), Field: protocolTestString("description")},
		{Kind: "derived_count"},
		{Kind: "derived_count", Direction: protocolTestString("both"), RelationTypeAddresses: protocolTestRelationTypeAddresses("ldl:project:p:entity:e")},
		{Kind: "derived_count", Direction: protocolTestString("both"), RelationTypeAddresses: protocolTestRelationTypeAddresses("ldl:project:p:relation-type:r", "ldl:project:p:relation-type:r")},
		{Kind: "state"},
		{Kind: "state", FieldPath: protocolTestStateFieldPath("review.status")},
	}
	for index, value := range invalidColumns {
		if _, err := semantic.EncodeViewTableColumnSource(value); err == nil {
			t.Fatalf("invalid typed column branch %d accepted", index)
		}
	}

	queryAddress := semantic.QueryAddress("ldl:project:p:query:q")
	arguments := map[string]semantic.RecipeScalar{
		"ldl:project:p:query:q:parameter:x": {Kind: "string", StringValue: protocolTestString("x")},
	}
	validDiffs := []semantic.ViewRecipeSource{
		{Kind: "query", QueryAddress: &queryAddress, Arguments: map[string]semantic.RecipeScalar{}},
		{Kind: "diff", Before: protocolTestString("base"), After: protocolTestString("head"), Arguments: map[string]semantic.RecipeScalar{}},
		{Kind: "diff", Before: protocolTestString("base"), After: protocolTestString("head"), QueryAddress: &queryAddress, Arguments: map[string]semantic.RecipeScalar{}},
		{Kind: "diff", Before: protocolTestString("base"), After: protocolTestString("head"), QueryAddress: &queryAddress, Arguments: arguments},
	}
	for index, value := range validDiffs {
		if _, err := semantic.EncodeViewRecipeSource(value); err != nil {
			t.Fatalf("valid typed Diff source %d rejected: %v", index, err)
		}
	}
	invalidDiffs := []semantic.ViewRecipeSource{
		{Kind: "query", QueryAddress: protocolTestQueryAddress("ldl:project:p"), Arguments: map[string]semantic.RecipeScalar{}},
		{Kind: "query", QueryAddress: &queryAddress, Arguments: map[string]semantic.RecipeScalar{"not-an-address": {Kind: "string", StringValue: protocolTestString("x")}}},
		{Kind: "diff", After: protocolTestString("head"), Arguments: map[string]semantic.RecipeScalar{}},
		{Kind: "diff", Before: protocolTestString("base"), Arguments: map[string]semantic.RecipeScalar{}},
		{Kind: "diff", Before: protocolTestString(""), After: protocolTestString("head"), Arguments: map[string]semantic.RecipeScalar{}},
		{Kind: "diff", Before: protocolTestString("base"), After: protocolTestString(""), Arguments: map[string]semantic.RecipeScalar{}},
		{Kind: "diff", Before: protocolTestString("same"), After: protocolTestString("same"), Arguments: map[string]semantic.RecipeScalar{}},
		{Kind: "diff", Before: protocolTestString("base"), After: protocolTestString("head"), Arguments: arguments},
	}
	for index, value := range invalidDiffs {
		if _, err := semantic.EncodeViewRecipeSource(value); err == nil {
			t.Fatalf("invalid typed Diff source %d accepted", index)
		}
	}
}

func protocolTestString(value string) *string { return &value }

func protocolTestColumnAddresses(values ...semantic.ColumnAddress) *[]semantic.ColumnAddress {
	return &values
}

func protocolTestRelationTypeAddresses(values ...semantic.RelationTypeAddress) *[]semantic.RelationTypeAddress {
	return &values
}

func protocolTestQueryAddress(value semantic.QueryAddress) *semantic.QueryAddress { return &value }

func protocolTestStateFieldPath(value semantic.StateFieldPath) *semantic.StateFieldPath {
	return &value
}

func TestSharedRecursiveValueLimits(t *testing.T) {
	t.Parallel()
	corpus := readSharedConformanceCorpus(t)
	deep := strings.Repeat("[", corpus.MaxJSONDepth) + `"x"` + strings.Repeat("]", corpus.MaxJSONDepth)
	value, err := protocolcommon.DecodeJsonValue([]byte(deep))
	if err != nil {
		t.Fatalf("maximum documented depth rejected: %v", err)
	}
	encoded, err := protocolcommon.EncodeJsonValue(value)
	if err != nil || string(encoded) != deep {
		t.Fatalf("maximum-depth canonical round trip failed: %v", err)
	}
	tooDeep := "[" + deep + "]"
	if _, err := protocolcommon.DecodeJsonValue([]byte(tooDeep)); err == nil {
		t.Fatal("value beyond documented depth was accepted")
	}

	maximumString := `"` + strings.Repeat("a", corpus.MaxJSONBytes-2) + `"`
	if _, err := protocolcommon.DecodeJsonValue([]byte(maximumString)); err != nil {
		t.Fatalf("maximum documented byte size rejected: %v", err)
	}
	tooLarge := `"` + strings.Repeat("a", corpus.MaxJSONBytes-1) + `"`
	if _, err := protocolcommon.DecodeJsonValue([]byte(tooLarge)); err == nil {
		t.Fatal("value beyond documented byte size was accepted")
	}
}

func TestSharedResponseEnvelopeMutationsRejectBeforeBlobResolution(t *testing.T) {
	t.Parallel()
	corpus := readSharedConformanceCorpus(t)
	for _, test := range corpus.MutationCases {
		test := test
		t.Run(test.Name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(readProtocolFixture(t, test.Fixture), &value); err != nil {
				t.Fatal(err)
			}
			switch test.Mutation {
			case "add_valid_failure":
				value["failure"] = map[string]any{"category": "invariant", "code": "engine.invariant", "message": "safe", "retryable": false}
			case "add_valid_pack_variant":
				var packResponse map[string]any
				if err := json.Unmarshal(readProtocolFixture(t, "compile-success-pack.json"), &packResponse); err != nil {
					t.Fatal(err)
				}
				artifact := value["payload"].(map[string]any)["normalized_artifact"].(map[string]any)
				artifact["pack"] = packResponse["payload"].(map[string]any)["normalized_artifact"].(map[string]any)["pack"]
			case "remove_pack_variant":
				delete(value["payload"].(map[string]any)["normalized_artifact"].(map[string]any), "pack")
			case "set_failed":
				value["outcome"] = "failed"
			case "set_cancelled":
				value["outcome"] = "cancelled"
			case "add_valid_success_payload":
				var success map[string]any
				if err := json.Unmarshal(readProtocolFixture(t, "compile-success.json"), &success); err != nil {
					t.Fatal(err)
				}
				value["payload"] = success["payload"]
			case "remove_project_graph":
				delete(value["payload"].(map[string]any)["normalized_artifact"].(map[string]any), "graph_hash")
			case "add_pack_graph":
				value["payload"].(map[string]any)["normalized_artifact"].(map[string]any)["graph_hash"] = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
			case "add_pack_search_document":
				var success map[string]any
				if err := json.Unmarshal(readProtocolFixture(t, "compile-success.json"), &success); err != nil {
					t.Fatal(err)
				}
				value["payload"].(map[string]any)["normalized_artifact"].(map[string]any)["search_documents"] = success["payload"].(map[string]any)["normalized_artifact"].(map[string]any)["search_documents"]
			case "corrupt_project_media_type":
				value["payload"].(map[string]any)["normalized_artifact"].(map[string]any)["project"].(map[string]any)["artifact_json"].(map[string]any)["media_type"] = "application/json"
			case "set_project_artifact_session_lifetime":
				value["payload"].(map[string]any)["normalized_artifact"].(map[string]any)["project"].(map[string]any)["artifact_json"].(map[string]any)["lifetime"] = "session"
			case "set_project_artifact_persistent_lifetime":
				value["payload"].(map[string]any)["normalized_artifact"].(map[string]any)["project"].(map[string]any)["artifact_json"].(map[string]any)["lifetime"] = "persistent"
			case "set_project_canonical_session_lifetime":
				value["payload"].(map[string]any)["normalized_artifact"].(map[string]any)["project"].(map[string]any)["canonical_json"].(map[string]any)["lifetime"] = "session"
			case "set_project_canonical_persistent_lifetime":
				value["payload"].(map[string]any)["normalized_artifact"].(map[string]any)["project"].(map[string]any)["canonical_json"].(map[string]any)["lifetime"] = "persistent"
			case "set_pack_artifact_session_lifetime":
				value["payload"].(map[string]any)["normalized_artifact"].(map[string]any)["pack"].(map[string]any)["artifact_json"].(map[string]any)["lifetime"] = "session"
			case "set_pack_artifact_persistent_lifetime":
				value["payload"].(map[string]any)["normalized_artifact"].(map[string]any)["pack"].(map[string]any)["artifact_json"].(map[string]any)["lifetime"] = "persistent"
			case "set_pack_canonical_session_lifetime":
				value["payload"].(map[string]any)["normalized_artifact"].(map[string]any)["pack"].(map[string]any)["canonical_json"].(map[string]any)["lifetime"] = "session"
			case "set_pack_canonical_persistent_lifetime":
				value["payload"].(map[string]any)["normalized_artifact"].(map[string]any)["pack"].(map[string]any)["canonical_json"].(map[string]any)["lifetime"] = "persistent"
			default:
				t.Fatalf("unknown mutation %q", test.Mutation)
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := engineprotocol.DecodeCompileResponseEnvelope(encoded); err == nil {
				t.Fatal("isolated invalid outcome/tagged-union mutation was accepted")
			}
		})
	}
	for _, test := range corpus.RequestMutationCases {
		test := test
		t.Run(test.Name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(readProtocolFixture(t, test.Fixture), &value); err != nil {
				t.Fatal(err)
			}
			payload := value["payload"].(map[string]any)
			switch test.Mutation {
			case "add_project_root":
				payload["root_pack_id"] = "publisher/root"
			case "set_pack_without_root":
				payload["mode"] = "pack"
			case "set_pack_empty_root":
				payload["mode"] = "pack"
				payload["root_pack_id"] = ""
			case "set_pack_with_root":
				payload["mode"] = "pack"
				payload["root_pack_id"] = "publisher/root"
			case "set_pack_bad_root":
				payload["mode"] = "pack"
				payload["root_pack_id"] = "Bad"
				payload["installed_pack_tree"] = payload["project_source_tree"]
				payload["project_source_tree"] = []any{}
			case "project_asset_pack_id":
				source := payload["project_source_tree"].([]any)[0].(map[string]any)
				blob := source["blob"].(map[string]any)
				payload["referenced_assets"] = []any{map[string]any{"origin": "project", "pack_id": "publisher/pack", "locator": "asset.svg", "blob": blob, "digest": blob["digest"], "media_type": "image/svg+xml"}}
			case "pack_asset_without_id":
				source := payload["project_source_tree"].([]any)[0].(map[string]any)
				blob := source["blob"].(map[string]any)
				payload["referenced_assets"] = []any{map[string]any{"origin": "pack", "locator": "asset.svg", "blob": blob, "digest": blob["digest"], "media_type": "image/svg+xml"}}
			case "bad_source_path":
				payload["project_source_tree"].([]any)[0].(map[string]any)["path"] = "../document.ldl"
			default:
				t.Fatalf("unknown request mutation %q", test.Mutation)
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := engineprotocol.DecodeCompileRequestEnvelope(encoded); err == nil {
				t.Fatal("invalid request mode/root invariant was accepted")
			}
		})
	}
}

func roundTripSharedWire(typeName string, input []byte) ([]byte, error) {
	switch typeName {
	case "ByteResourceLimitCapability":
		value, err := protocolcommon.DecodeByteResourceLimitCapability(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeByteResourceLimitCapability(value)
	case "CapabilityID":
		value, err := protocolcommon.DecodeCapabilityID(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeCapabilityID(value)
	case "CanonicalFiniteDecimal":
		value, err := semantic.DecodeCanonicalFiniteDecimal(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeCanonicalFiniteDecimal(value)
	case "CanonicalInt64":
		value, err := protocolcommon.DecodeCanonicalInt64(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeCanonicalInt64(value)
	case "CanonicalNonNegativeInt64":
		value, err := protocolcommon.DecodeCanonicalNonNegativeInt64(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeCanonicalNonNegativeInt64(value)
	case "CanonicalNonNegativeSafeInteger":
		value, err := protocolcommon.DecodeCanonicalNonNegativeSafeInteger(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeCanonicalNonNegativeSafeInteger(value)
	case "CanonicalPositiveFiniteDecimal":
		value, err := semantic.DecodeCanonicalPositiveFiniteDecimal(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeCanonicalPositiveFiniteDecimal(value)
	case "CanonicalPositiveInt64":
		value, err := protocolcommon.DecodeCanonicalPositiveInt64(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeCanonicalPositiveInt64(value)
	case "CanonicalPositiveSafeInteger":
		value, err := protocolcommon.DecodeCanonicalPositiveSafeInteger(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeCanonicalPositiveSafeInteger(value)
	case "CanonicalSafeInteger":
		value, err := protocolcommon.DecodeCanonicalSafeInteger(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeCanonicalSafeInteger(value)
	case "CanonicalSourcePath":
		value, err := engineprotocol.DecodeCanonicalSourcePath(input)
		if err != nil {
			return nil, err
		}
		return engineprotocol.EncodeCanonicalSourcePath(value)
	case "CanonicalUint64":
		value, err := protocolcommon.DecodeCanonicalUint64(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeCanonicalUint64(value)
	case "Color":
		value, err := semantic.DecodeColor(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeColor(value)
	case "HandshakeRequest":
		value, err := protocolcommon.DecodeHandshakeRequest(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeHandshakeRequest(value)
	case "OperationCapability":
		value, err := protocolcommon.DecodeOperationCapability(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeOperationCapability(value)
	case "ProtocolOffer":
		value, err := protocolcommon.DecodeProtocolOffer(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeProtocolOffer(value)
	case "ProtocolVersion":
		value, err := protocolcommon.DecodeProtocolVersion(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeProtocolVersion(value)
	case "ProtocolVersionOrRange":
		value, err := protocolcommon.DecodeProtocolVersionOrRange(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeProtocolVersionOrRange(value)
	case "ProtocolVersionRange":
		value, err := protocolcommon.DecodeProtocolVersionRange(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeProtocolVersionRange(value)
	case "RequestedCapabilityStatus":
		value, err := protocolcommon.DecodeRequestedCapabilityStatus(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeRequestedCapabilityStatus(value)
	case "Rfc3339Time":
		value, err := protocolcommon.DecodeRfc3339Time(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeRfc3339Time(value)
	case "JsonValue":
		value, err := protocolcommon.DecodeJsonValue(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeJsonValue(value)
	case "SearchField":
		value, err := semantic.DecodeSearchField(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeSearchField(value)
	case "StableAddress":
		value, err := semantic.DecodeStableAddress(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeStableAddress(value)
	case "PackRootAddress":
		value, err := semantic.DecodePackRootAddress(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodePackRootAddress(value)
	case "ProjectRootAddress":
		value, err := semantic.DecodeProjectRootAddress(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeProjectRootAddress(value)
	case "EffectiveResourceLimits":
		value, err := engineprotocol.DecodeEffectiveResourceLimits(input)
		if err != nil {
			return nil, err
		}
		return engineprotocol.EncodeEffectiveResourceLimits(value)
	case "ExportRecipeBlobRef":
		value, err := engineprotocol.DecodeExportRecipeBlobRef(input)
		if err != nil {
			return nil, err
		}
		return engineprotocol.EncodeExportRecipeBlobRef(value)
	case "NormalizedPackArtifactBlobRef":
		value, err := engineprotocol.DecodeNormalizedPackArtifactBlobRef(input)
		if err != nil {
			return nil, err
		}
		return engineprotocol.EncodeNormalizedPackArtifactBlobRef(value)
	case "NormalizedPackCanonicalBlobRef":
		value, err := engineprotocol.DecodeNormalizedPackCanonicalBlobRef(input)
		if err != nil {
			return nil, err
		}
		return engineprotocol.EncodeNormalizedPackCanonicalBlobRef(value)
	case "NormalizedProjectArtifactBlobRef":
		value, err := engineprotocol.DecodeNormalizedProjectArtifactBlobRef(input)
		if err != nil {
			return nil, err
		}
		return engineprotocol.EncodeNormalizedProjectArtifactBlobRef(value)
	case "NormalizedProjectCanonicalBlobRef":
		value, err := engineprotocol.DecodeNormalizedProjectCanonicalBlobRef(input)
		if err != nil {
			return nil, err
		}
		return engineprotocol.EncodeNormalizedProjectCanonicalBlobRef(value)
	case "QueryRecipeBlobRef":
		value, err := engineprotocol.DecodeQueryRecipeBlobRef(input)
		if err != nil {
			return nil, err
		}
		return engineprotocol.EncodeQueryRecipeBlobRef(value)
	case "ViewRecipeBlobRef":
		value, err := engineprotocol.DecodeViewRecipeBlobRef(input)
		if err != nil {
			return nil, err
		}
		return engineprotocol.EncodeViewRecipeBlobRef(value)
	case "ExportDimension":
		value, err := semantic.DecodeExportDimension(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeExportDimension(value)
	case "ExportOptions":
		value, err := semantic.DecodeExportOptions(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeExportOptions(value)
	case "ExportRecipe":
		value, err := semantic.DecodeExportRecipe(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeExportRecipe(value)
	case "EntityTypeColumnAddress":
		value, err := semantic.DecodeEntityTypeColumnAddress(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeEntityTypeColumnAddress(value)
	case "RecipePredicate":
		value, err := semantic.DecodeRecipePredicate(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeRecipePredicate(value)
	case "RecipeRowPredicate":
		value, err := semantic.DecodeRecipeRowPredicate(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeRecipeRowPredicate(value)
	case "RasterBackground":
		value, err := semantic.DecodeRasterBackground(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeRasterBackground(value)
	case "RelationTypeColumnAddress":
		value, err := semantic.DecodeRelationTypeColumnAddress(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeRelationTypeColumnAddress(value)
	case "ViewRecipeSource":
		value, err := semantic.DecodeViewRecipeSource(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewRecipeSource(value)
	case "ViewAddress":
		value, err := semantic.DecodeViewAddress(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewAddress(value)
	case "ViewDiagramProjection":
		value, err := semantic.DecodeViewDiagramProjection(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewDiagramProjection(value)
	case "ViewDiagramShape":
		value, err := semantic.DecodeViewDiagramShape(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewDiagramShape(value)
	case "ViewFlowShape":
		value, err := semantic.DecodeViewFlowShape(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewFlowShape(value)
	case "ViewExportAddress":
		value, err := semantic.DecodeViewExportAddress(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewExportAddress(value)
	case "ViewFlowProjection":
		value, err := semantic.DecodeViewFlowProjection(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewFlowProjection(value)
	case "ViewMatrixAxis":
		value, err := semantic.DecodeViewMatrixAxis(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewMatrixAxis(value)
	case "ViewMatrixCell":
		value, err := semantic.DecodeViewMatrixCell(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewMatrixCell(value)
	case "ViewTableColumnSource":
		value, err := semantic.DecodeViewTableColumnSource(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewTableColumnSource(value)
	case "ViewTableProjection":
		value, err := semantic.DecodeViewTableProjection(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewTableProjection(value)
	case "ViewTableShape":
		value, err := semantic.DecodeViewTableShape(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewTableShape(value)
	case "ViewTreeShape":
		value, err := semantic.DecodeViewTreeShape(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewTreeShape(value)
	case "Diagnostic":
		value, err := semantic.DecodeDiagnostic(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeDiagnostic(value)
	case "CompiledQueryRecipeDocument":
		value, err := semantic.DecodeCompiledQueryRecipeDocument(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeCompiledQueryRecipeDocument(value)
	case "CompiledViewRecipeDocument":
		value, err := semantic.DecodeCompiledViewRecipeDocument(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeCompiledViewRecipeDocument(value)
	case "CompiledExportRecipeDocument":
		value, err := semantic.DecodeCompiledExportRecipeDocument(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeCompiledExportRecipeDocument(value)
	case "Digest":
		value, err := protocolcommon.DecodeDigest(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeDigest(value)
	case "EndpointInstanceID":
		value, err := protocolcommon.DecodeEndpointInstanceID(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeEndpointInstanceID(value)
	case "ManifestETag":
		value, err := protocolcommon.DecodeManifestETag(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeManifestETag(value)
	case "ReleaseVersion":
		value, err := protocolcommon.DecodeReleaseVersion(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeReleaseVersion(value)
	case "TotalItems":
		value, err := protocolcommon.DecodeTotalItems(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeTotalItems(value)
	case "UpgradeDiagnosticData":
		value, err := protocolcommon.DecodeUpgradeDiagnosticData(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeUpgradeDiagnosticData(value)
	case "ViewPlacement":
		value, err := semantic.DecodeViewPlacement(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewPlacement(value)
	case "ViewRenderSet":
		value, err := semantic.DecodeViewRenderSet(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewRenderSet(value)
	case "ViewRecipe":
		value, err := semantic.DecodeViewRecipe(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewRecipe(value)
	case "ViewRecipeDependencies":
		value, err := semantic.DecodeViewRecipeDependencies(input)
		if err != nil {
			return nil, err
		}
		return semantic.EncodeViewRecipeDependencies(value)
	default:
		return nil, fmt.Errorf("unknown shared conformance type %q", typeName)
	}
}

func readSharedConformanceCorpus(t *testing.T) sharedConformanceCorpus {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(protocolRepositoryRoot(t), "schemas", "fixtures", "conformance", "v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus sharedConformanceCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	return corpus
}

func TestGeneratedProtocolPackagesHaveNoHandwrittenOrForbiddenDependencies(t *testing.T) {
	t.Parallel()
	root := protocolRepositoryRoot(t)
	err := filepath.WalkDir(filepath.Join(root, "gen", "go"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if !strings.HasSuffix(path, ".gen.go") {
			t.Errorf("handwritten Go source in generated wire package: %s", path)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range parsed.Imports {
			importPath := strings.Trim(spec.Path.Value, `"`)
			isStandardLibrary := !strings.Contains(strings.Split(importPath, "/")[0], ".")
			isGeneratedRuntimeDependency := importPath == "golang.org/x/text/unicode/norm"
			if !isStandardLibrary && !isGeneratedRuntimeDependency && !strings.HasPrefix(importPath, "github.com/dencyuinc/layerdraw/gen/go/") {
				t.Errorf("generated Go package imports non-generated dependency %q in %s", importPath, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(root, "packages", "protocol", "src"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".ts") {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".gen.ts") {
			t.Errorf("handwritten TypeScript source in generated wire package: %s", entry.Name())
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, "packages", "protocol", "src", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		source := string(data)
		for _, forbidden := range []string{`"node:`, "Buffer", "process.", "document.", "window.", "Worker", "@layerdraw/runtime", "@layerdraw/sdk"} {
			if strings.Contains(source, forbidden) {
				t.Errorf("browser-neutral generated module %s contains forbidden runtime token %q", entry.Name(), forbidden)
			}
		}
	}
	manifestData, err := os.ReadFile(filepath.Join(root, "packages", "protocol", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Dependencies map[string]string `json:"dependencies"`
		Exports      map[string]any    `json:"exports"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Dependencies) != 0 {
		t.Fatalf("generated protocol package has runtime dependencies: %v", manifest.Dependencies)
	}
	if _, exists := manifest.Exports["."]; exists {
		t.Fatal("protocol package must not expose a root barrel entrypoint")
	}
	wantedExports := map[string]bool{"./common": true, "./semantic": true, "./engine": true}
	if len(manifest.Exports) != len(wantedExports) {
		t.Fatalf("unexpected protocol exports: %v", manifest.Exports)
	}
	for export := range manifest.Exports {
		if !wantedExports[export] {
			t.Errorf("undeclared protocol deep export %q", export)
		}
	}
	packagedLicense, err := os.ReadFile(filepath.Join(root, "packages", "protocol", "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	canonicalLicense, err := os.ReadFile(filepath.Join(root, "docs", "legal", "licenses", "Apache-2.0.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(packagedLicense, canonicalLicense) {
		t.Fatal("protocol package LICENSE differs from canonical Apache-2.0 text")
	}
}

func readProtocolFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(protocolRepositoryRoot(t), "schemas", "fixtures", "engine", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func protocolRepositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
