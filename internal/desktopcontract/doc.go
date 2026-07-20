// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package desktopcontract defines the framework-neutral boundary consumed by
// the Wails Desktop composition root and generated frontend bindings.
//
// It deliberately owns no LDL, Runtime, Access, Registry, Review, exporter, or
// MCP semantics. Those components remain authoritative; a Desktop shell only
// negotiates them and forwards generated protocol envelopes mechanically.
package desktopcontract
