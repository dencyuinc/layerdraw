// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"testing"
	"time"
)

func TestPackagedProjectScenarioInjectsInstalledFaultRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := conformanceProjectFaultRecovery(ctx); err != nil {
		t.Fatal(err)
	}
}
