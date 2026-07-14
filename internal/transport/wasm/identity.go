// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package wasm

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// newEndpointInstanceID mints a non-secret identity inside each Go/WASM
// runtime. Runtime randomness never enters artifact bytes or semantic output.
func newEndpointInstanceID() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("mint endpoint instance ID: %w", err)
	}
	return "wasm-" + hex.EncodeToString(nonce[:]), nil
}
