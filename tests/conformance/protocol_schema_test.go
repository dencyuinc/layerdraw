// SPDX-License-Identifier: Apache-2.0

package conformance_test

import (
	"bytes"
	"encoding/json"
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

func TestGeneratedCodecsRejectScalarOutcomeAndTaggedUnionViolations(t *testing.T) {
	t.Parallel()
	for _, invalid := range []string{`"-0"`, `"01"`, `"18446744073709551616"`} {
		if _, err := protocolcommon.DecodeCanonicalUint64([]byte(invalid)); err == nil {
			t.Errorf("accepted invalid uint64 %s", invalid)
		}
	}
	for _, invalid := range []string{`"sha256:ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789"`, `"md5:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`} {
		if _, err := protocolcommon.DecodeDigest([]byte(invalid)); err == nil {
			t.Errorf("accepted invalid digest %s", invalid)
		}
	}
	success := readProtocolFixture(t, "compile-success.json")
	var raw map[string]any
	if err := json.Unmarshal(success, &raw); err != nil {
		t.Fatal(err)
	}
	raw["failure"] = map[string]any{"category": "invariant", "code": "bad", "message": "bad", "retryable": false}
	invalidOutcome, _ := json.Marshal(raw)
	if _, err := engineprotocol.DecodeCompileResponseEnvelope(invalidOutcome); err == nil {
		t.Fatal("success plus failure was accepted")
	}
	delete(raw, "failure")
	payload := raw["payload"].(map[string]any)
	artifact := payload["normalized_artifact"].(map[string]any)
	artifact["pack"] = artifact["project"]
	invalidUnion, _ := json.Marshal(raw)
	if _, err := engineprotocol.DecodeCompileResponseEnvelope(invalidUnion); err == nil {
		t.Fatal("mixed Project/Pack normalized artifact was accepted")
	}
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
