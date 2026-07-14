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

func TestSharedOutcomeAndUnionMutationCorpus(t *testing.T) {
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
	case "OperationCapability":
		value, err := protocolcommon.DecodeOperationCapability(input)
		if err != nil {
			return nil, err
		}
		return protocolcommon.EncodeOperationCapability(value)
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
			if !isStandardLibrary && !strings.HasPrefix(importPath, "github.com/dencyuinc/layerdraw/gen/go/") {
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
