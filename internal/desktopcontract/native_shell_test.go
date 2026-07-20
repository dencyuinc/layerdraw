// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopcontract

import (
	"testing"
	"time"
)

func TestNativeShellClosedValueValidation(t *testing.T) {
	for _, value := range []DesktopPlatform{PlatformMacOS, PlatformWindows, PlatformLinux} {
		if !value.Validate() {
			t.Fatalf("platform %q invalid", value)
		}
	}
	if DesktopPlatform("other").Validate() {
		t.Fatal("unknown platform accepted")
	}
	for _, value := range []Theme{ThemeSystem, ThemeLight, ThemeDark} {
		if !value.Validate() {
			t.Fatalf("theme %q invalid", value)
		}
	}
	if Theme("other").Validate() {
		t.Fatal("unknown theme accepted")
	}
	for _, value := range []CommandID{CommandNewProject, CommandOpenProject, CommandSaveProject, CommandCloseProject, CommandUndo, CommandRedo, CommandSearch, CommandSettings} {
		if !value.Validate() {
			t.Fatalf("command %q invalid", value)
		}
	}
	if CommandID("os.execute").Validate() {
		t.Fatal("unknown command accepted")
	}
	for _, value := range []CommandState{CommandAvailable, CommandUnavailable, CommandPending, CommandDenied} {
		if !value.Validate() {
			t.Fatalf("state %q invalid", value)
		}
	}
	if CommandState("other").Validate() {
		t.Fatal("unknown command state accepted")
	}
	for _, value := range []CommandSource{CommandSourceControl, CommandSourceMenu, CommandSourceShortcut} {
		if !value.Validate() {
			t.Fatalf("source %q invalid", value)
		}
	}
	if CommandSource("script").Validate() {
		t.Fatal("unknown command source accepted")
	}
}

func TestNativeShellStateValidation(t *testing.T) {
	settings := DesktopSettings{SchemaVersion: 1, Theme: ThemeSystem, ZoomPercent: 100}
	window := WindowState{SchemaVersion: 1, Bounds: Rectangle{Width: 800, Height: 600}}
	if !settings.Validate() || !window.Validate() || !(PersistedShellState{Settings: settings, Window: window}).Validate() {
		t.Fatal("valid shell state rejected")
	}
	for _, invalid := range []DesktopSettings{
		{SchemaVersion: 2, Theme: ThemeSystem, ZoomPercent: 100},
		{SchemaVersion: 1, Theme: "other", ZoomPercent: 100},
		{SchemaVersion: 1, Theme: ThemeSystem, ZoomPercent: 49},
		{SchemaVersion: 1, Theme: ThemeSystem, ZoomPercent: 301},
	} {
		if invalid.Validate() {
			t.Fatalf("invalid settings accepted: %+v", invalid)
		}
	}
	if (Display{ID: "primary", Work: Rectangle{Width: 1920, Height: 1080}}).Validate() == false ||
		(Display{ID: "", Work: Rectangle{Width: 1920, Height: 1080}}).Validate() ||
		(Display{ID: "small", Work: Rectangle{Width: 639, Height: 480}}).Validate() {
		t.Fatal("display validation mismatch")
	}
	if (WindowState{SchemaVersion: 2, Bounds: Rectangle{Width: 800, Height: 600}}).Validate() ||
		(WindowState{SchemaVersion: 1, Bounds: Rectangle{Width: 800, Height: 479}}).Validate() {
		t.Fatal("invalid window accepted")
	}
}

func TestNativeShellCommandGenerationValidation(t *testing.T) {
	status := CommandStatus{ID: CommandOpenProject, State: CommandAvailable, Generation: "1"}
	invocation := CommandInvocation{ID: CommandOpenProject, Source: CommandSourceShortcut, StatusGeneration: "1"}
	if !status.Validate() || !invocation.Validate() {
		t.Fatal("valid generated command contract rejected")
	}
	status.Generation = "01"
	invocation.StatusGeneration = "18446744073709551616"
	if status.Validate() || invocation.Validate() {
		t.Fatal("noncanonical command generation accepted")
	}
}

func TestExternalTargetValidation(t *testing.T) {
	for _, value := range []ExternalTarget{
		{Kind: ExternalWebLink, Value: "https://layerdraw.com/docs"},
		{Kind: ExternalEmail, Value: "mailto:help@layerdraw.com"},
	} {
		if !value.Validate() {
			t.Fatalf("safe target rejected: %+v", value)
		}
	}
	for _, value := range []ExternalTarget{
		{Kind: ExternalWebLink, Value: "http://layerdraw.com"},
		{Kind: ExternalWebLink, Value: "https://token@layerdraw.com"},
		{Kind: ExternalWebLink, Value: "file:///private/document.ldl"},
		{Kind: ExternalEmail, Value: "mailto:help@layerdraw.com?body=private"},
		{Kind: "other", Value: "https://layerdraw.com"},
	} {
		if value.Validate() {
			t.Fatalf("unsafe target accepted: %+v", value)
		}
	}
}

func TestStructuredLogRecordValidation(t *testing.T) {
	code := FailureSettings
	command := CommandSettings
	events := []ShellEvent{
		EventSettingsRestored, EventSettingsRecovered, EventSettingsSaved,
		EventCommandInvoked, EventCommandRejected, EventExternalOpened,
		EventExternalDenied, EventFailurePresented, EventOperationFailed,
		EventOperationRejected,
	}
	for _, event := range events {
		record := StructuredLogRecord{At: time.Unix(1, 0).UTC(), Level: LogInfo, Event: event, Platform: PlatformLinux, Failure: &code, Command: &command}
		if !record.Validate() || !event.Validate() {
			t.Fatalf("valid record rejected: %+v", record)
		}
	}
	invalid := []StructuredLogRecord{
		{Level: LogInfo, Event: EventSettingsSaved, Platform: PlatformLinux},
		{At: time.Unix(1, 0), Level: "debug", Event: EventSettingsSaved, Platform: PlatformLinux},
		{At: time.Unix(1, 0), Level: LogInfo, Event: "private", Platform: PlatformLinux},
		{At: time.Unix(1, 0), Level: LogInfo, Event: EventSettingsSaved, Platform: "other"},
	}
	for _, record := range invalid {
		if record.Validate() {
			t.Fatalf("invalid record accepted: %+v", record)
		}
	}
	badFailure := FailureCode("private")
	record := StructuredLogRecord{At: time.Unix(1, 0), Level: LogError, Event: EventOperationFailed, Platform: PlatformLinux, Failure: &badFailure}
	if record.Validate() || badFailure.Validate() {
		t.Fatal("unknown failure code accepted")
	}
}
