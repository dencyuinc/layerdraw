// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build !js || !wasm

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/dencyuinc/layerdraw/internal/engine"
)

func main() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, processSignals()...)
	defer signal.Stop(signals)
	os.Exit(runIO(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, signals))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runIO(args, nil, stdout, stderr, nil)
}

func runIO(args []string, stdin io.Reader, stdout, stderr io.Writer, signals <-chan os.Signal) int {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
		descriptor := engine.New(engine.BuildInfo{
			ReleaseVersion: releaseVersion,
			SourceRevision: sourceRevision,
		}).Describe()
		fmt.Fprintf(stdout, "layerdraw-engine %s (%s)\n", descriptor.ReleaseVersion, descriptor.SourceRevision)
		return 0
	}
	if len(args) == 1 && args[0] == "stdio" {
		if stdin == nil {
			fmt.Fprintln(stderr, "layerdraw-engine: stdio_configuration")
			return 1
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		received := make(chan os.Signal, 1)
		if signals != nil {
			go func() {
				select {
				case current := <-signals:
					received <- current
					cancel()
				case <-ctx.Done():
				}
			}()
		}
		err := serveStdio(ctx, stdin, stdout)
		select {
		case current := <-received:
			return signalExitCode(current)
		default:
		}
		if err != nil {
			if ctx.Err() != nil {
				return 1
			}
			fmt.Fprintln(stderr, safeStdioError(err))
			return 1
		}
		return 0
	}

	fmt.Fprintln(stderr, "usage: layerdraw-engine --version|version|stdio")
	return 2
}
