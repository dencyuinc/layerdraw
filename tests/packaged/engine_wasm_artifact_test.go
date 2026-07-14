// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package packaged_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackagedEngineWASMManifestHashesLegalAndSBOM(t *testing.T) {
	bundle := os.Getenv("LAYERDRAW_ENGINE_WASM_DIR")
	if bundle == "" {
		t.Skip("LAYERDRAW_ENGINE_WASM_DIR is not set")
	}
	manifestBytes, err := os.ReadFile(filepath.Join(bundle, "engine-wasm.manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, manifestBytes); err != nil {
		t.Fatal(err)
	}
	compact.WriteByte('\n')
	if !bytes.Equal(manifestBytes, compact.Bytes()) {
		t.Fatal("artifact manifest is not canonical compact JSON")
	}
	var manifest struct {
		ArtifactID string `json:"artifact_id"`
		Build      struct {
			GoVersion      string `json:"go_version"`
			SourceRevision string `json:"source_revision"`
		} `json:"build"`
		Protocol struct {
			SchemaDigest string `json:"schema_digest"`
		} `json:"protocol"`
		RuntimeSupport struct {
			Digest string `json:"digest"`
		} `json:"runtime_support"`
		Files []struct {
			Path   string `json:"path"`
			Size   int64  `json:"size"`
			Digest string `json:"digest"`
		} `json:"files"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ArtifactID != "@layerdraw/engine-wasm" || manifest.Build.GoVersion != "go1.26.5" || len(manifest.Build.SourceRevision) != 40 || !strings.HasPrefix(manifest.Protocol.SchemaDigest, "sha256:") {
		t.Fatalf("unexpected artifact authority: %+v", manifest)
	}
	const supportDigest = "sha256:0c949f4996f9a89698e4b5c586de32249c3b69b7baadb64d220073cc04acba14"
	if manifest.RuntimeSupport.Digest != supportDigest {
		t.Fatalf("runtime support digest=%s", manifest.RuntimeSupport.Digest)
	}
	seen := map[string]bool{}
	for _, file := range manifest.Files {
		data, err := os.ReadFile(filepath.Join(bundle, filepath.FromSlash(file.Path)))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		if int64(len(data)) != file.Size || "sha256:"+hex.EncodeToString(digest[:]) != file.Digest {
			t.Fatalf("manifest hash mismatch for %s", file.Path)
		}
		seen[file.Path] = true
	}
	for _, required := range []string{"layerdraw-engine.wasm", "wasm_exec.js", "engine-wasm-worker-v1.json", "LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.txt", "engine-wasm.cdx.json"} {
		if !seen[required] {
			t.Fatalf("manifest omits %s", required)
		}
	}
	notices, err := os.ReadFile(filepath.Join(bundle, "THIRD_PARTY_NOTICES.txt"))
	if err != nil || !bytes.Contains(notices, []byte("Go WebAssembly runtime support go1.26.5")) || !bytes.Contains(notices, []byte("BSD-3-Clause")) {
		t.Fatalf("runtime support notice is incomplete: err=%v", err)
	}
	sbomBytes, err := os.ReadFile(filepath.Join(bundle, "engine-wasm.cdx.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(sbomBytes, []byte(`"bomFormat":"CycloneDX"`)) || !bytes.Contains(sbomBytes, []byte(`"specVersion":"1.6"`)) || !bytes.Contains(sbomBytes, []byte("Go WebAssembly runtime support")) {
		t.Fatal("artifact SBOM does not identify CycloneDX 1.6 and Go runtime support")
	}
}
