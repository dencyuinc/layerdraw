// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	transport "github.com/dencyuinc/layerdraw/internal/transport/stdio"
)

func TestRunIOVersionUsageAndMalformedFrame(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runIO([]string{"--version"}, bytes.NewReader(nil), &stdout, &stderr, nil); code != 0 || !strings.Contains(stdout.String(), "layerdraw-host") {
		t.Fatalf("version code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runIO([]string{"stdio", "--root", "relative"}, bytes.NewReader(nil), &stdout, &stderr, nil); code != 1 || !strings.Contains(stderr.String(), "stdio_configuration") {
		t.Fatalf("relative code=%d stderr=%q", code, stderr.String())
	}

	previous := releaseManifestDigest
	releaseManifestDigest = "sha256:" + strings.Repeat("a", 64)
	t.Cleanup(func() { releaseManifestDigest = previous })
	stdout.Reset()
	stderr.Reset()
	if code := runIO([]string{"stdio", "--root", filepath.Join(t.TempDir(), "storage")}, bytes.NewReader([]byte("bad")), &stdout, &stderr, nil); code != 1 || !strings.Contains(stderr.String(), "stdio_framing") {
		t.Fatalf("malformed code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunIOCleanProcessExitAndFreshEndpointGeneration(t *testing.T) {
	previous := releaseManifestDigest
	releaseManifestDigest = "sha256:" + strings.Repeat("b", 64)
	t.Cleanup(func() { releaseManifestDigest = previous })
	var stdout, stderr bytes.Buffer
	root := filepath.Join(t.TempDir(), "storage")
	if code := runIO([]string{"stdio", "--root", root}, bytes.NewReader(nil), &stdout, &stderr, nil); code != 0 {
		t.Fatalf("clean exit code=%d stderr=%q", code, stderr.String())
	}
	first, err := endpointInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := endpointInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasPrefix(first, "host-") || !strings.HasPrefix(second, "host-") {
		t.Fatalf("endpoint ids %q %q", first, second)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runIO([]string{"engine-stdio", "--root", root}, bytes.NewReader(nil), &stdout, &stderr, nil); code != 0 {
		t.Fatalf("clean Engine exit code=%d stderr=%q", code, stderr.String())
	}
	if digest, err := manifestDigest(); err != nil || digest != releaseManifestDigest {
		t.Fatalf("digest=%q err=%v", digest, err)
	}
}

func TestRunIOSignalAndPlatformExitCodes(t *testing.T) {
	previous := releaseManifestDigest
	releaseManifestDigest = "sha256:" + strings.Repeat("d", 64)
	t.Cleanup(func() { releaseManifestDigest = previous })
	signals := make(chan os.Signal, 1)
	signals <- syscall.SIGTERM
	var stdout, stderr bytes.Buffer
	if code := runIO([]string{"stdio", "--root", filepath.Join(t.TempDir(), "storage")}, bytes.NewReader(nil), &stdout, &stderr, signals); code != 143 {
		t.Fatalf("signal exit code=%d stderr=%q", code, stderr.String())
	}
	if signalExitCode(os.Interrupt) != 130 || signalExitCode(syscall.SIGTERM) != 143 {
		t.Fatal("platform signal exit codes changed")
	}
	if len(processSignals()) != 2 {
		t.Fatalf("signals=%v", processSignals())
	}
}

func TestSafeErrorRedactsUnderlyingProcessFailure(t *testing.T) {
	if got := safeError(errors.New("/secret/path: crashed")); got != "layerdraw-host: stdio_invariant" {
		t.Fatalf("safe error=%q", got)
	}
	if got := safeError(&transport.SessionError{Code: transport.SessionErrorFraming}); strings.Contains(got, string(os.PathSeparator)+"secret") || got != "layerdraw-host: stdio_framing" {
		t.Fatalf("session error=%q", got)
	}
}

func TestConfigurationFailuresRemainRedacted(t *testing.T) {
	previous := releaseManifestDigest
	releaseManifestDigest = ""
	t.Cleanup(func() { releaseManifestDigest = previous })
	if _, err := manifestDigest(); err == nil {
		t.Fatal("missing adjacent release manifest was accepted")
	}
	var stdout, stderr bytes.Buffer
	if code := runIO([]string{"engine-stdio", "--root", t.TempDir()}, bytes.NewReader(nil), &stdout, &stderr, nil); code != 1 || !strings.Contains(stderr.String(), "stdio_configuration") {
		t.Fatalf("Engine configuration code=%d stderr=%q", code, stderr.String())
	}
	releaseManifestDigest = "not-a-digest"
	stdout.Reset()
	stderr.Reset()
	if code := runIO([]string{"stdio", "--root", t.TempDir()}, bytes.NewReader(nil), &stdout, &stderr, nil); code != 1 || !strings.Contains(stderr.String(), "stdio_configuration") {
		t.Fatalf("Runtime configuration code=%d stderr=%q", code, stderr.String())
	}
	releaseManifestDigest = "sha256:" + strings.Repeat("e", 64)
	quietSignals := make(chan os.Signal)
	stdout.Reset()
	stderr.Reset()
	if code := runIO([]string{"stdio", "--root", filepath.Join(t.TempDir(), "storage")}, bytes.NewReader(nil), &stdout, &stderr, quietSignals); code != 0 {
		t.Fatalf("clean monitored exit code=%d stderr=%q", code, stderr.String())
	}
}
