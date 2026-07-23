// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"os"
	"strings"
	"testing"
)

func TestDisplayLocationLabelShortensHomeAndPassesThrough(t *testing.T) {
	if displayLocationLabel("") != "" {
		t.Fatal("empty root must stay empty")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory in this environment")
	}
	if got := displayLocationLabel(home + "/projects/demo"); !strings.HasPrefix(got, "~") {
		t.Fatalf("home-relative label=%q", got)
	}
	if got := displayLocationLabel("/opt/shared/demo"); got != "/opt/shared/demo" {
		t.Fatalf("passthrough label=%q", got)
	}
}
