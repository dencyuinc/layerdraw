// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package desktop contains production OS and Wails bridge adapters for the
// framework-neutral Desktop shell. It does not own document or UI semantics.
package desktop

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

const maximumSettingsBytes = 64 * 1024
const maximumAssociationBytes = 64 * 1024 * 1024

type AtomicSettingsStore struct{ path string }

func NewAtomicSettingsStore(path string) (*AtomicSettingsStore, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("desktop settings path must be clean and absolute")
	}
	return &AtomicSettingsStore{path: path}, nil
}

func (s *AtomicSettingsStore) Load(context.Context) (desktopcontract.PersistedShellState, error) {
	file, info, err := openRegularFile(s.path, os.O_RDONLY, 0)
	if err != nil {
		return desktopcontract.PersistedShellState{}, err
	}
	defer file.Close()
	if info.Size() > maximumSettingsBytes || info.Mode().Perm()&0o077 != 0 {
		return desktopcontract.PersistedShellState{}, errors.New("desktop settings file is unsafe")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maximumSettingsBytes+1))
	decoder.DisallowUnknownFields()
	var value desktopcontract.PersistedShellState
	if err := decoder.Decode(&value); err != nil {
		return desktopcontract.PersistedShellState{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) || !value.Validate() {
		return desktopcontract.PersistedShellState{}, errors.New("desktop settings are invalid")
	}
	return value, nil
}

func (s *AtomicSettingsStore) Save(_ context.Context, value desktopcontract.PersistedShellState) error {
	if !value.Validate() {
		return errors.New("desktop settings are invalid")
	}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > maximumSettingsBytes {
		return errors.New("desktop settings cannot be encoded")
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(s.path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return errors.New("desktop settings target is unsafe")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".layerdraw-settings-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return err
	}
	committed = true
	if directoryHandle, err := os.Open(directory); err == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	return nil
}

type commandRunner interface {
	Run(context.Context, string, ...string) error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, arguments ...string) error {
	return exec.CommandContext(ctx, name, arguments...).Run()
}

type SystemExternalOpener struct {
	platform desktopcontract.DesktopPlatform
	runner   commandRunner
}

func NewSystemExternalOpener(platform desktopcontract.DesktopPlatform) (*SystemExternalOpener, error) {
	return newSystemExternalOpener(platform, execRunner{})
}

func newSystemExternalOpener(platform desktopcontract.DesktopPlatform, runner commandRunner) (*SystemExternalOpener, error) {
	if !platform.Validate() || runner == nil {
		return nil, errors.New("desktop external opener is incomplete")
	}
	return &SystemExternalOpener{platform: platform, runner: runner}, nil
}

func (o *SystemExternalOpener) OpenExternal(ctx context.Context, target desktopcontract.ExternalTarget) error {
	if !target.Validate() {
		return errors.New("desktop external target denied")
	}
	switch o.platform {
	case desktopcontract.PlatformMacOS:
		return o.runner.Run(ctx, "/usr/bin/open", "--", target.Value)
	case desktopcontract.PlatformWindows:
		return o.runner.Run(ctx, `C:\Windows\System32\rundll32.exe`, "url.dll,FileProtocolHandler", target.Value)
	case desktopcontract.PlatformLinux:
		return o.runner.Run(ctx, "/usr/bin/xdg-open", target.Value)
	default:
		return errors.New("desktop platform unsupported")
	}
}

// WailsRuntimeBridge is implemented by the generated Wails application layer.
// The concrete adapter below is the only production bridge to native window
// and packaged accessibility operations.
type WailsRuntimeBridge interface {
	Displays(context.Context) ([]desktopcontract.Display, error)
	Snapshot(context.Context) (desktopcontract.WindowState, desktopcontract.DesktopSettings, error)
	ApplyWindow(context.Context, desktopcontract.WindowState) error
	ApplySettings(context.Context, desktopcontract.DesktopSettings) error
	VerifyPackagedAccessibility(context.Context, desktopcontract.AccessibilityProfile) (desktopcontract.AccessibilityReport, error)
}

type WailsRuntimeAdapter struct{ bridge WailsRuntimeBridge }

func NewWailsRuntimeAdapter(bridge WailsRuntimeBridge) (*WailsRuntimeAdapter, error) {
	if bridge == nil {
		return nil, errors.New("Wails runtime bridge is unavailable")
	}
	return &WailsRuntimeAdapter{bridge: bridge}, nil
}

func (a *WailsRuntimeAdapter) Displays(ctx context.Context) ([]desktopcontract.Display, error) {
	return a.bridge.Displays(ctx)
}
func (a *WailsRuntimeAdapter) Snapshot(ctx context.Context) (desktopcontract.WindowState, desktopcontract.DesktopSettings, error) {
	return a.bridge.Snapshot(ctx)
}
func (a *WailsRuntimeAdapter) ApplyWindow(ctx context.Context, value desktopcontract.WindowState) error {
	return a.bridge.ApplyWindow(ctx, value)
}
func (a *WailsRuntimeAdapter) ApplySettings(ctx context.Context, value desktopcontract.DesktopSettings) error {
	return a.bridge.ApplySettings(ctx, value)
}
func (a *WailsRuntimeAdapter) VerifyPackaged(ctx context.Context, value desktopcontract.AccessibilityProfile) (desktopcontract.AccessibilityReport, error) {
	return a.bridge.VerifyPackagedAccessibility(ctx, value)
}

type AssociationBroker struct {
	mu        sync.Mutex
	locations map[string]associationLocation
	queue     []desktopcontract.FileAssociationHandoff
}

type associationLocation struct {
	path   string
	info   os.FileInfo
	digest [sha256.Size]byte
}

func NewAssociationBroker() *AssociationBroker {
	return &AssociationBroker{locations: make(map[string]associationLocation)}
}

// AcceptOSPath is called only by the native application event. It verifies the
// file and converts the private path to a random opaque token before queuing it.
func (b *AssociationBroker) AcceptOSPath(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("file association path is invalid")
	}
	file, info, err := openRegularFile(path, os.O_RDONLY, 0)
	if err != nil {
		return errors.New("file association target is unsafe")
	}
	digest, err := boundedAssociationDigest(file)
	_ = file.Close()
	if err != nil {
		return err
	}
	extension := strings.ToLower(filepath.Ext(path))
	kind := desktopcontract.FileAssociationKind("")
	switch extension {
	case ".ldl":
		kind = desktopcontract.FileAssociationLDL
	case ".layerdraw":
		kind = desktopcontract.FileAssociationLayerDraw
	default:
		return errors.New("file association type is unsupported")
	}
	random := make([]byte, 24)
	if _, err := io.ReadFull(rand.Reader, random); err != nil {
		return errors.New("file association token unavailable")
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	b.mu.Lock()
	b.locations[token] = associationLocation{path: path, info: info, digest: digest}
	b.queue = append(b.queue, desktopcontract.FileAssociationHandoff{Kind: kind, Token: token})
	b.mu.Unlock()
	return nil
}

func (b *AssociationBroker) Next(context.Context) (desktopcontract.FileAssociationHandoff, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.queue) == 0 {
		return desktopcontract.FileAssociationHandoff{}, errors.New("no file association pending")
	}
	value := b.queue[0]
	b.queue = b.queue[1:]
	return value, nil
}

// Resolve consumes a token inside the trusted backend. The path never crosses
// the frontend binding and a token cannot be replayed.
func (b *AssociationBroker) Resolve(token string) (string, error) {
	path, _, err := b.ResolveIdentity(token)
	return path, err
}

// ResolveIdentity consumes a handoff and preserves the OS file identity so the
// storage boundary can revalidate it immediately before project resolution.
func (b *AssociationBroker) ResolveIdentity(token string) (string, os.FileInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	location, ok := b.locations[token]
	if !ok {
		return "", nil, errors.New("file association token is invalid")
	}
	delete(b.locations, token)
	file, current, err := openRegularFile(location.path, os.O_RDONLY, 0)
	if err != nil || !os.SameFile(location.info, current) {
		if file != nil {
			_ = file.Close()
		}
		return "", nil, errors.New("file association target changed")
	}
	digest, digestErr := boundedAssociationDigest(file)
	_ = file.Close()
	if digestErr != nil || digest != location.digest {
		return "", nil, errors.New("file association target changed")
	}
	return location.path, location.info, nil
}

func boundedAssociationDigest(file *os.File) ([sha256.Size]byte, error) {
	var empty [sha256.Size]byte
	if file == nil {
		return empty, errors.New("file association target is unavailable")
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, maximumAssociationBytes+1))
	if err != nil || written > maximumAssociationBytes {
		return empty, errors.New("file association target exceeds the safe size limit")
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

type JSONLogStore struct {
	mu   sync.Mutex
	path string
}

func NewJSONLogStore(path string) (*JSONLogStore, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("desktop log path must be clean and absolute")
	}
	return &JSONLogStore{path: path}, nil
}

func (s *JSONLogStore) Write(_ context.Context, record desktopcontract.StructuredLogRecord) error {
	if !record.Validate() {
		return errors.New("desktop structured log is invalid")
	}
	encoded, err := json.Marshal(record)
	if err != nil || bytes.ContainsAny(encoded, "\r\n") {
		return errors.New("desktop structured log is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	file, info, err := openRegularFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		_ = file.Close()
		return errors.New("desktop log permissions are unsafe")
	}
	defer file.Close()
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func openRegularFile(path string, flag int, perm os.FileMode) (*os.File, os.FileInfo, error) {
	file, err := os.OpenFile(path, flag, perm)
	if err != nil {
		return nil, nil, err
	}
	closeFailure := func(err error) (*os.File, os.FileInfo, error) {
		_ = file.Close()
		return nil, nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		return closeFailure(err)
	}
	linked, err := os.Lstat(path)
	if err != nil {
		return closeFailure(err)
	}
	if !opened.Mode().IsRegular() || !linked.Mode().IsRegular() || linked.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, linked) {
		return closeFailure(errors.New("desktop private file is unsafe"))
	}
	return file, opened, nil
}
