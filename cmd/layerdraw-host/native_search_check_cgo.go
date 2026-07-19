// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package main

import (
	"fmt"
	"io"
	"path/filepath"

	searchadapter "github.com/dencyuinc/layerdraw/internal/adapter/search"
)

func runNativeSearchCheck(args []string, stdout, stderr io.Writer) (bool, int) {
	if len(args) == 0 || args[0] != "native-search-check" {
		return false, 0
	}
	if len(args) != 5 || args[1] != "--database" || args[3] != "--fts-extension" || !filepath.IsAbs(args[2]) || !filepath.IsAbs(args[4]) {
		fmt.Fprintln(stderr, "usage: layerdraw-host native-search-check --database ABSOLUTE_PATH --fts-extension ABSOLUTE_PATH")
		return true, 2
	}
	session, err := searchadapter.OpenGoLadybugSessionWithFTS(args[2], args[4])
	if err != nil {
		fmt.Fprintln(stderr, "layerdraw-host: native_search_unavailable")
		return true, 1
	}
	defer session.Close()
	version, err := session.BackendVersion()
	if err != nil || version != searchadapter.GoLadybugBackendVersion {
		fmt.Fprintln(stderr, "layerdraw-host: native_search_version_mismatch")
		return true, 1
	}
	fmt.Fprintf(stdout, "layerdraw-host native-search ladybug %s fts loaded\n", version)
	return true, 0
}
