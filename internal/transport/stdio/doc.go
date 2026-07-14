// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package stdio implements the dependency-light LDSP framing 1.0 wire codec.
//
// The package owns fixed-width frame parsing, structural validation, canonical
// blob chunk planning, and exact I/O. It deliberately does not own Engine
// Protocol decoding, handshake state, request dispatch, process lifecycle, or
// any LDL/compiler behavior.
package stdio
