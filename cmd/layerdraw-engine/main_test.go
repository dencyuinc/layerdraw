// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

func TestRunVersion(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if exitCode := run([]string{"--version"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("run exit code = %d, want 0; stderr = %q", exitCode, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "layerdraw-engine ") {
		t.Fatalf("stdout = %q, want version output", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunStdioCleanEOFAndFatalRedaction(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if exitCode := runIO([]string{"stdio"}, bytes.NewReader(nil), &stdout, &stderr, nil); exitCode != 0 {
		t.Fatalf("clean EOF exit = %d, stderr=%q", exitCode, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("clean EOF stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := runIO([]string{"stdio"}, strings.NewReader("broken secret-source"), &stdout, &stderr, nil); exitCode != 1 {
		t.Fatalf("fatal exit = %d", exitCode)
	}
	if stdout.Len() != 0 || stderr.String() != "layerdraw-engine: stdio_framing\n" || strings.Contains(stderr.String(), "secret-source") {
		t.Fatalf("fatal stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunStdioConfigurationAndSafeOperationalErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := runIO([]string{"stdio"}, nil, &stdout, &stderr, nil); got != 1 {
		t.Fatalf("nil stdin exit = %d", got)
	}
	if stdout.Len() != 0 || stderr.String() != "layerdraw-engine: stdio_configuration\n" {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if got := safeStdioError(errors.New("secret underlying failure")); got != "layerdraw-engine: stdio_invariant" {
		t.Fatalf("fallback error = %q", got)
	}
	supported := processSignals()
	if len(supported) == 0 {
		t.Fatal("no process signal configured")
	}
	for _, current := range supported {
		if code := signalExitCode(current); code != 130 && code != 143 {
			t.Fatalf("signal %v exit = %d", current, code)
		}
	}
}

func TestStdioCompositionMintsPerInstanceIdentity(t *testing.T) {
	t.Parallel()
	first, err := newEndpointInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newEndpointInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasPrefix(first, "stdio-") || len(first) != len("stdio-")+32 {
		t.Fatalf("instance IDs = %q, %q", first, second)
	}
	if releaseManifestDigest == "sha256:"+strings.Repeat("0", 64) {
		t.Fatal("zero release-manifest identity")
	}
}

func TestDevelopmentReleaseManifestMatchesLinkedAuthority(t *testing.T) {
	t.Parallel()
	encoded, err := os.ReadFile("../../deploy/development-release-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		EngineProtocolSchemaDigest string `json:"engine_protocol_schema_digest"`
		Release                    string `json:"release"`
	}
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	wantDigest := "sha256:" + hex.EncodeToString(digest[:])
	if releaseManifestDigest != wantDigest || manifest.EngineProtocolSchemaDigest != engineprotocol.SchemaDigest || manifest.Release != engine.DevelopmentVersion {
		t.Fatalf("linked=%q file=%q release=%q", releaseManifestDigest, manifest.EngineProtocolSchemaDigest, manifest.Release)
	}
}

func TestRunStdioSignalExitCodes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		signal os.Signal
		want   int
	}{{os.Interrupt, 130}} {
		t.Run(test.signal.String(), func(t *testing.T) {
			signals := make(chan os.Signal, 1)
			signals <- test.signal
			reader, writer := io.Pipe()
			defer writer.Close()
			var stdout, stderr bytes.Buffer
			if got := runIO([]string{"stdio"}, reader, &stdout, &stderr, signals); got != test.want {
				t.Fatalf("exit = %d, want %d", got, test.want)
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunRejectsUnknownArguments(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if exitCode := run([]string{"serve"}, &stdout, &stderr); exitCode != 2 {
		t.Fatalf("run exit code = %d, want 2", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}
