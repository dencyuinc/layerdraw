// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build js && wasm

package main

import wasmtransport "github.com/dencyuinc/layerdraw/internal/transport/wasm"

func main() {
	wasmtransport.Run(wasmtransport.StaticConfig{
		EngineRelease:       releaseVersion,
		SourceRevision:      sourceRevision,
		SBOMAuthorityDigest: sbomAuthorityDigest,
	})
}
