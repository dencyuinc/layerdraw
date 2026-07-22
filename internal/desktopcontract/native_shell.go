// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopcontract

import (
	"context"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
)

type DesktopPlatform string

const (
	PlatformMacOS   DesktopPlatform = "macos"
	PlatformWindows DesktopPlatform = "windows"
	PlatformLinux   DesktopPlatform = "linux"
)

func (p DesktopPlatform) Validate() bool {
	return p == PlatformMacOS || p == PlatformWindows || p == PlatformLinux
}

type Theme string

const (
	ThemeSystem Theme = "system"
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark"
)

func (t Theme) Validate() bool { return t == ThemeSystem || t == ThemeLight || t == ThemeDark }

const SettingsSchemaVersion = 1

type DesktopSettings struct {
	SchemaVersion uint32 `json:"schema_version"`
	Theme         Theme  `json:"theme"`
	ZoomPercent   uint16 `json:"zoom_percent"`
	// Locale overrides the OS UI language. Empty or "system" follows the OS;
	// concrete values are catalog locales ("en", "ja").
	Locale string `json:"locale,omitempty"`
	// MCPEnabled restores the AI-connection switch across launches so external
	// MCP clients survive Desktop restarts.
	MCPEnabled bool `json:"mcp_enabled,omitempty"`
}

func (s DesktopSettings) Validate() bool {
	localeValid := s.Locale == "" || s.Locale == "system" || s.Locale == "en" || s.Locale == "ja"
	return s.SchemaVersion == SettingsSchemaVersion && s.Theme.Validate() && s.ZoomPercent >= 50 && s.ZoomPercent <= 300 && localeValid
}

type Rectangle struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type Display struct {
	ID      string    `json:"id"`
	Primary bool      `json:"primary"`
	Work    Rectangle `json:"work_area"`
}

func (d Display) Validate() bool { return d.ID != "" && d.Work.Width >= 640 && d.Work.Height >= 480 }

type WindowState struct {
	SchemaVersion uint32    `json:"schema_version"`
	Bounds        Rectangle `json:"bounds"`
	Maximized     bool      `json:"maximized"`
}

func (s WindowState) Validate() bool {
	return s.SchemaVersion == SettingsSchemaVersion && s.Bounds.Width >= 640 && s.Bounds.Height >= 480
}

type PersistedShellState struct {
	Settings DesktopSettings `json:"settings"`
	Window   WindowState     `json:"window"`
}

func (s PersistedShellState) Validate() bool { return s.Settings.Validate() && s.Window.Validate() }

// SettingsStore is implemented by an OS-backed, atomic settings adapter.
// Corrupt or incompatible bytes are reported as an error, never partially decoded.
type SettingsStore interface {
	Load(context.Context) (PersistedShellState, error)
	Save(context.Context, PersistedShellState) error
}

// NativeWindowPort is the Wails-owned window/settings boundary. It receives a
// normalized state and never owns document or command semantics.
type NativeWindowPort interface {
	Displays(context.Context) ([]Display, error)
	Snapshot(context.Context) (WindowState, DesktopSettings, error)
	ApplyWindow(context.Context, WindowState) error
	ApplySettings(context.Context, DesktopSettings) error
}

type CommandID string

const (
	CommandNewProject   CommandID = "project.new"
	CommandOpenProject  CommandID = "project.open"
	CommandSaveProject  CommandID = "project.save"
	CommandCloseProject CommandID = "project.close"
	CommandUndo         CommandID = "edit.undo"
	CommandRedo         CommandID = "edit.redo"
	CommandSearch       CommandID = "project.search"
	CommandSettings     CommandID = "desktop.settings"
)

func (id CommandID) Validate() bool {
	switch id {
	case CommandNewProject, CommandOpenProject, CommandSaveProject, CommandCloseProject,
		CommandUndo, CommandRedo, CommandSearch, CommandSettings:
		return true
	default:
		return false
	}
}

type CommandState string

const (
	CommandAvailable   CommandState = "available"
	CommandUnavailable CommandState = "unavailable"
	CommandPending     CommandState = "pending"
	CommandDenied      CommandState = "denied"
)

func (s CommandState) Validate() bool {
	return s == CommandAvailable || s == CommandUnavailable || s == CommandPending || s == CommandDenied
}

type CommandSource string

const (
	CommandSourceControl  CommandSource = "control"
	CommandSourceMenu     CommandSource = "menu"
	CommandSourceShortcut CommandSource = "shortcut"
)

func (s CommandSource) Validate() bool {
	return s == CommandSourceControl || s == CommandSourceMenu || s == CommandSourceShortcut
}

type CommandStatus struct {
	ID         CommandID                      `json:"id"`
	State      CommandState                   `json:"state"`
	Generation protocolcommon.CanonicalUint64 `json:"generation"`
}

func (s CommandStatus) Validate() bool {
	if !s.ID.Validate() || !s.State.Validate() {
		return false
	}
	_, err := protocolcommon.EncodeCanonicalUint64(s.Generation)
	return err == nil
}

type CommandInvocation struct {
	ID               CommandID                      `json:"id"`
	Source           CommandSource                  `json:"source"`
	StatusGeneration protocolcommon.CanonicalUint64 `json:"status_generation"`
}

func (i CommandInvocation) Validate() bool {
	if !i.ID.Validate() || !i.Source.Validate() {
		return false
	}
	_, err := protocolcommon.EncodeCanonicalUint64(i.StatusGeneration)
	return err == nil
}

// CommandRouter is injected by the Desktop UI composition (#124). Menus,
// shortcuts and visible controls all call this exact owner route. Route must
// atomically re-evaluate the generation and state before invoking semantics.
type CommandRouter interface {
	Status(context.Context) ([]CommandStatus, error)
	Route(context.Context, CommandInvocation) (CommandStatus, error)
}

type ExternalTargetKind string

const (
	ExternalWebLink ExternalTargetKind = "web_link"
	ExternalEmail   ExternalTargetKind = "email"
)

type ExternalTarget struct {
	Kind  ExternalTargetKind `json:"kind"`
	Value string             `json:"value"`
}

func (t ExternalTarget) Validate() bool {
	if t.Value == "" || strings.ContainsAny(t.Value, "\r\n\x00") {
		return false
	}
	parsed, err := url.ParseRequestURI(t.Value)
	if err != nil || parsed.User != nil {
		return false
	}
	switch t.Kind {
	case ExternalWebLink:
		return parsed.Scheme == "https" && parsed.Host != ""
	case ExternalEmail:
		if parsed.Scheme != "mailto" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return false
		}
		_, err := mail.ParseAddress(strings.TrimPrefix(t.Value, "mailto:"))
		return err == nil
	default:
		return false
	}
}

// ExternalOpenPort receives only a target accepted by the shell allowlist.
// It must use the OS URL opener directly and must never invoke a command shell.
type ExternalOpenPort interface {
	OpenExternal(context.Context, ExternalTarget) error
}

type FileAssociationKind string

const (
	FileAssociationLDL       FileAssociationKind = "ldl"
	FileAssociationLayerDraw FileAssociationKind = "layerdraw"
)

type FileAssociationHandoff struct {
	Kind  FileAssociationKind `json:"kind"`
	Token string              `json:"token"`
}

// FileAssociationPort converts an OS-owned path to an opaque, single-purpose
// host token. Native paths do not enter a frontend request, response, or log.
type FileAssociationPort interface {
	Next(context.Context) (FileAssociationHandoff, error)
}

type ShellEvent string

const (
	EventSettingsRestored  ShellEvent = "settings.restored"
	EventSettingsRecovered ShellEvent = "settings.recovered"
	EventSettingsSaved     ShellEvent = "settings.saved"
	EventCommandInvoked    ShellEvent = "command.invoked"
	EventCommandRejected   ShellEvent = "command.rejected"
	EventExternalOpened    ShellEvent = "external.opened"
	EventExternalDenied    ShellEvent = "external.denied"
	EventFailurePresented  ShellEvent = "failure.presented"
	EventOperationFailed   ShellEvent = "operation.failed"
	EventOperationRejected ShellEvent = "operation.rejected"
)

type LogLevel string

const (
	LogInfo  LogLevel = "info"
	LogWarn  LogLevel = "warn"
	LogError LogLevel = "error"
)

// StructuredLogRecord deliberately has no message, path, content, token,
// credential, URL or arbitrary fields surface.
type StructuredLogRecord struct {
	At       time.Time       `json:"at"`
	Level    LogLevel        `json:"level"`
	Event    ShellEvent      `json:"event"`
	Platform DesktopPlatform `json:"platform"`
	Failure  *FailureCode    `json:"failure,omitempty"`
	Command  *CommandID      `json:"command,omitempty"`
}

func (r StructuredLogRecord) Validate() bool {
	if r.At.IsZero() || !r.Platform.Validate() ||
		(r.Level != LogInfo && r.Level != LogWarn && r.Level != LogError) || !r.Event.Validate() {
		return false
	}
	if r.Failure != nil && !r.Failure.Validate() {
		return false
	}
	return r.Command == nil || r.Command.Validate()
}

func (e ShellEvent) Validate() bool {
	switch e {
	case EventSettingsRestored, EventSettingsRecovered, EventSettingsSaved,
		EventCommandInvoked, EventCommandRejected, EventExternalOpened,
		EventExternalDenied, EventFailurePresented, EventOperationFailed,
		EventOperationRejected:
		return true
	default:
		return false
	}
}

type StructuredLogPort interface {
	Write(context.Context, StructuredLogRecord) error
}

type UnexpectedFailureOrigin string

const (
	FailureOriginBackend  UnexpectedFailureOrigin = "backend"
	FailureOriginFrontend UnexpectedFailureOrigin = "frontend"
)

type RecoveryRef struct {
	ID string `json:"id"`
}

type CrashContext struct {
	Origin    UnexpectedFailureOrigin `json:"origin"`
	Lifecycle LifecycleState          `json:"lifecycle"`
	At        time.Time               `json:"at"`
}

// CrashRecoveryPort is owned by project lifecycle (#123). The shell supplies
// only closed context; the port decides which sessions have durable recovery data.
type CrashRecoveryPort interface {
	Preserve(context.Context, CrashContext) (RecoveryRef, error)
}

type ErrorSurface struct {
	Failure  FailureCode    `json:"failure"`
	Recovery RecoveryAction `json:"recovery"`
	Ref      *RecoveryRef   `json:"recovery_ref,omitempty"`
}

type ErrorSurfacePort interface {
	Present(context.Context, ErrorSurface) error
}

type AccessibilityProfile struct {
	Platform      DesktopPlatform `json:"platform"`
	ScreenReader  bool            `json:"screen_reader"`
	KeyboardOnly  bool            `json:"keyboard_only"`
	ReducedMotion bool            `json:"reduced_motion"`
	ZoomPercent   uint16          `json:"zoom_percent"`
	ProbeID       string          `json:"probe_id,omitempty"`
	ViewerMode    string          `json:"viewer_mode,omitempty"`
	WindowWidth   uint16          `json:"window_width,omitempty"`
	WindowHeight  uint16          `json:"window_height,omitempty"`
}

type AccessibilityReport struct {
	LabelsComplete        bool    `json:"labels_complete"`
	ScreenReaderSemantics bool    `json:"screen_reader_semantics"`
	FocusOrderValid       bool    `json:"focus_order_valid"`
	FocusOrderFailures    string  `json:"focus_order_failures,omitempty"`
	KeyboardWorkflowValid bool    `json:"keyboard_workflow_valid"`
	ReducedMotionHonored  bool    `json:"reduced_motion_honored"`
	MinimumContrast       float64 `json:"minimum_contrast"`
	MinimumContrastTarget string  `json:"minimum_contrast_target,omitempty"`
	ZoomLayoutValid       bool    `json:"zoom_layout_valid"`
	ViewportWidth         uint16  `json:"viewport_width,omitempty"`
	ViewportHeight        uint16  `json:"viewport_height,omitempty"`
	ViewerMode            string  `json:"viewer_mode,omitempty"`
	RendererBackend       string  `json:"renderer_backend,omitempty"`
	ViewerItemCount       uint32  `json:"viewer_item_count,omitempty"`
	ViewerRelationCount   uint32  `json:"viewer_relation_count,omitempty"`
	ViewerCrossLayerCount uint32  `json:"viewer_cross_layer_relation_count,omitempty"`
	ViewerKeyboardSelect  bool    `json:"viewer_keyboard_selection"`
	WebGLVerified         bool    `json:"webgl_verified"`
}

type AccessibilityProbe interface {
	VerifyPackaged(context.Context, AccessibilityProfile) (AccessibilityReport, error)
}
