// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
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
