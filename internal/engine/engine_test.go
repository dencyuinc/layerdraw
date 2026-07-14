// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"reflect"
	"testing"
)

func TestNewUsesDevelopmentDefaults(t *testing.T) {
	t.Parallel()

	descriptor := New(BuildInfo{}).Describe()

	if descriptor.Component != "engine" {
		t.Fatalf("Component = %q, want engine", descriptor.Component)
	}
	if descriptor.ReleaseVersion != DevelopmentVersion {
		t.Fatalf("ReleaseVersion = %q, want %q", descriptor.ReleaseVersion, DevelopmentVersion)
	}
	if descriptor.SourceRevision != UnknownSourceRevision {
		t.Fatalf("SourceRevision = %q, want %q", descriptor.SourceRevision, UnknownSourceRevision)
	}
	if want := []string{CapabilityCompile, CapabilityDescribe}; !reflect.DeepEqual(descriptor.Capabilities, want) {
		t.Fatalf("Capabilities = %v, want %v", descriptor.Capabilities, want)
	}
}

func TestDescribeReturnsCapabilitySnapshot(t *testing.T) {
	t.Parallel()

	instance := New(BuildInfo{ReleaseVersion: "1.2.3", SourceRevision: "abc123"})
	first := instance.Describe()
	first.Capabilities[0] = "modified"
	second := instance.Describe()

	if second.Capabilities[0] != CapabilityCompile {
		t.Fatalf("Describe returned mutable shared state: %v", second.Capabilities)
	}
	if second.ReleaseVersion != "1.2.3" || second.SourceRevision != "abc123" {
		t.Fatalf("Describe lost build information: %+v", second)
	}
}
