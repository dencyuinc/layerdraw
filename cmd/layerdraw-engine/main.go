// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"fmt"
	"io"
	"os"

	"github.com/dencyuinc/layerdraw/internal/engine"
)

var (
	releaseVersion = engine.DevelopmentVersion
	sourceRevision = engine.UnknownSourceRevision
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
		descriptor := engine.New(engine.BuildInfo{
			ReleaseVersion: releaseVersion,
			SourceRevision: sourceRevision,
		}).Describe()
		fmt.Fprintf(stdout, "layerdraw-engine %s (%s)\n", descriptor.ReleaseVersion, descriptor.SourceRevision)
		return 0
	}

	fmt.Fprintln(stderr, "usage: layerdraw-engine --version")
	return 2
}
