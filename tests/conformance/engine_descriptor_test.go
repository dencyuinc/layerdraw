// SPDX-License-Identifier: Apache-2.0

package conformance_test

import (
	"slices"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine"
)

func TestBootstrapDescriptorIsDeterministic(t *testing.T) {
	t.Parallel()

	instance := engine.New(engine.BuildInfo{})
	first := instance.Describe()
	second := instance.Describe()

	if first.Component != second.Component ||
		first.ReleaseVersion != second.ReleaseVersion ||
		first.SourceRevision != second.SourceRevision ||
		!slices.Equal(first.Capabilities, second.Capabilities) {
		t.Fatalf("Describe is not deterministic: first=%+v second=%+v", first, second)
	}
	if !slices.IsSorted(first.Capabilities) {
		t.Fatalf("Capabilities must be sorted: %v", first.Capabilities)
	}
}
