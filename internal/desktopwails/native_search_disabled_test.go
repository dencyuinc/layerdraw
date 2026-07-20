// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build !ladybug_native

package desktopwails

import "testing"

func TestNativeSearchBuildBoundaryStaysClosed(t *testing.T) {
	if packagedNativeSearchEnabled() {
		t.Fatal("native Search advertised without the ladybug_native build tag")
	}
	search, lifecycle, closeSearch, err := openPackagedNativeSearch("", nil, nil)
	if err != nil || search != nil || lifecycle != nil || closeSearch == nil {
		t.Fatalf("disabled native composition = %v, %v, close=%t, %v", search, lifecycle, closeSearch != nil, err)
	}
	closeSearch()
	if decoder, ok := packagedNativeSearchDecoder(); ok || decoder != nil {
		t.Fatalf("native decoder advertised without its build: %T, %t", decoder, ok)
	}
}
