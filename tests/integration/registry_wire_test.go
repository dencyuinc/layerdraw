// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package integration_test

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	registry "github.com/dencyuinc/layerdraw/internal/registry"
)

func TestGoRegistryWireIsConsumedByTypeScriptClient(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	value, err := json.Marshal([]registry.RegistrySource{{SourceID: "official", Kind: registry.SourceOfficial, EndpointRef: "registry:official", TrustPolicyID: "official", CachePolicy: "verified", Priority: 100, Connected: true}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := json.Marshal(registry.WireResponse{WireVersion: registry.RegistryWireVersion, Operation: registry.WireListSources, RequestID: "registry-1", OK: true, Value: value})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", filepath.Join(root, "tests", "integration", "testdata", "registry_wire_node.mjs"))
	command.Stdin = bytes.NewReader(response)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("TypeScript wire consumer: %v\n%s", err, output)
	}
	var request registry.WireRequest
	if err := json.Unmarshal(output, &request); err != nil {
		t.Fatalf("decode emitted Go request: %v\n%s", err, output)
	}
	if request.WireVersion != registry.RegistryWireVersion || request.Operation != registry.WireListSources || request.RequestID != "registry-1" || string(request.Input) != "{}" {
		t.Fatalf("wire mismatch: %#v", request)
	}
}
