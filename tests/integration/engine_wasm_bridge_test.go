// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEngineWASMBridgeNodeSmoke(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	nodeVersion, err := exec.Command("node", "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("read Node version: %v\n%s", err, nodeVersion)
	}
	if strings.TrimSpace(string(nodeVersion)) != "v24.18.0" {
		t.Fatalf("Node version = %q, want pinned v24.18.0", strings.TrimSpace(string(nodeVersion)))
	}
	revisionBytes, err := exec.Command("git", "-C", repositoryRoot, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("read source revision: %v\n%s", err, revisionBytes)
	}
	revision := strings.TrimSpace(string(revisionBytes))
	artifactDirectory := filepath.Join(t.TempDir(), "artifact")
	build := exec.Command(filepath.Join(repositoryRoot, "tools", "build-engine-wasm.sh"))
	build.Dir = repositoryRoot
	build.Env = append(os.Environ(),
		"ENGINE_WASM_ALLOW_DIRTY=1",
		"ENGINE_WASM_OUTPUT_DIR="+artifactDirectory,
		"SOURCE_REVISION="+revision,
		"VERSION=0.0.0-dev",
	)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Engine WASM: %v\n%s", err, output)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	script := filepath.Join(repositoryRoot, "tests", "integration", "testdata", "wasm_bridge_node.mjs")
	command := exec.CommandContext(ctx, "node", script, artifactDirectory)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run Engine WASM bridge smoke: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "smoke passed") {
		t.Fatalf("unexpected bridge smoke output: %s", output)
	}
}
