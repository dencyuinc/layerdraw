// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package integration_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestEngineCommandReportsVersion(t *testing.T) {
	t.Parallel()

	command := exec.Command("go", "run", "../../cmd/layerdraw-engine", "--version")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go run layerdraw-engine --version: %v\n%s", err, output)
	}

	if got := string(output); !strings.HasPrefix(got, "layerdraw-engine 0.0.0-dev (") {
		t.Fatalf("version output = %q", got)
	}
}
