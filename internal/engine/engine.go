// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package engine exposes the transport-neutral facade for LayerDraw semantics.
package engine

import "slices"

const (
	// DevelopmentVersion is used when no release version is injected at build time.
	DevelopmentVersion = "0.0.0-dev"

	// UnknownSourceRevision is used when source provenance is unavailable.
	UnknownSourceRevision = "unknown"

	// CapabilityDescribe identifies the bootstrap component-description operation.
	CapabilityDescribe = "engine.describe"
)

var bootstrapCapabilities = []string{CapabilityDescribe}

// BuildInfo identifies the source used to build an Engine instance.
type BuildInfo struct {
	ReleaseVersion string
	SourceRevision string
}

// Descriptor reports capabilities linked into this Engine instance.
// It is an in-process model, not an Engine Protocol wire type.
type Descriptor struct {
	Component      string
	ReleaseVersion string
	SourceRevision string
	Capabilities   []string
}

// Engine is the public facade for the canonical Go semantic implementation.
type Engine struct {
	build BuildInfo
}

// New creates an Engine with deterministic development defaults.
func New(build BuildInfo) Engine {
	if build.ReleaseVersion == "" {
		build.ReleaseVersion = DevelopmentVersion
	}
	if build.SourceRevision == "" {
		build.SourceRevision = UnknownSourceRevision
	}

	return Engine{build: build}
}

// Describe returns a defensive snapshot of the linked component capabilities.
func (e Engine) Describe() Descriptor {
	return Descriptor{
		Component:      "engine",
		ReleaseVersion: e.build.ReleaseVersion,
		SourceRevision: e.build.SourceRevision,
		Capabilities:   slices.Clone(bootstrapCapabilities),
	}
}
