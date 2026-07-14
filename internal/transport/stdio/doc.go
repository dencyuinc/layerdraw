// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package stdio implements the LDSP framing 1.0 codec and bounded native
// Engine Protocol connection state machine.
//
// The package owns fixed-width frame parsing, structural validation, canonical
// blob chunk planning, exact I/O, handshake binding, request correlation,
// admission, cancellation, and shutdown. It calls only generated codecs and
// the transport-neutral endpoint/CompilePlan facade. It deliberately owns no
// LDL/compiler interpretation, filesystem/network acquisition, Runtime state,
// framework integration, or persistent/session blob storage.
package stdio
