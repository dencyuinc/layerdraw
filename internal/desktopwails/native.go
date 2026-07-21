// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package desktopwails is the production Wails composition boundary. It owns
// native UI calls and opaque filesystem selections; document semantics remain
// in desktopapp and the Engine/Runtime packages.
package desktopwails

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// NativeRuntime is the narrow, probeable native surface shared with the
// packaged Desktop work. WailsRuntime is its production implementation.
type NativeRuntime interface {
	OpenDirectory(context.Context, string) (string, error)
	OpenFile(context.Context, string, []string) (string, error)
	SaveFile(context.Context, string, []string) (string, error)
	ShowWindow(context.Context)
	Quit(context.Context)
	Emit(context.Context, string, ...any)
}

type WailsRuntime struct{}

func (WailsRuntime) OpenDirectory(ctx context.Context, title string) (string, error) {
	return wailsruntime.OpenDirectoryDialog(ctx, wailsruntime.OpenDialogOptions{Title: title})
}
func (WailsRuntime) OpenFile(ctx context.Context, title string, extensions []string) (string, error) {
	return wailsruntime.OpenFileDialog(ctx, wailsruntime.OpenDialogOptions{Title: title, Filters: []wailsruntime.FileFilter{{DisplayName: title, Pattern: patterns(extensions)}}})
}
func (WailsRuntime) SaveFile(ctx context.Context, title string, extensions []string) (string, error) {
	return wailsruntime.SaveFileDialog(ctx, wailsruntime.SaveDialogOptions{Title: title, Filters: []wailsruntime.FileFilter{{DisplayName: title, Pattern: patterns(extensions)}}})
}
func (WailsRuntime) ShowWindow(ctx context.Context) { wailsruntime.WindowShow(ctx) }
func (WailsRuntime) Quit(ctx context.Context)       { wailsruntime.Quit(ctx) }
func (WailsRuntime) Emit(ctx context.Context, name string, data ...any) {
	wailsruntime.EventsEmit(ctx, name, data...)
}

func patterns(extensions []string) string {
	values := make([]string, 0, len(extensions))
	for _, extension := range extensions {
		extension = strings.TrimPrefix(strings.TrimSpace(extension), ".")
		if extension != "" {
			values = append(values, "*."+extension)
		}
	}
	return strings.Join(values, ";")
}

type selectionVault struct {
	mu    sync.Mutex
	paths map[string]selectionPath
}

type selectionPath struct {
	path      string
	identity  os.FileInfo
	digest    [sha256.Size]byte
	digestSet bool
}

const maxPinnedAssociationBytes int64 = 64 << 20

func newSelectionVault() *selectionVault { return &selectionVault{paths: map[string]selectionPath{}} }

func (v *selectionVault) issue(path string) (string, error) {
	return v.issuePinned(path, nil)
}

func (v *selectionVault) issuePinned(path string, identity os.FileInfo) (string, error) {
	canonical, err := filepath.Abs(path)
	if err != nil || canonical == "" || filepath.Clean(canonical) != canonical {
		return "", errors.New("native selection is invalid")
	}
	selection := selectionPath{path: canonical, identity: identity}
	if identity != nil {
		content, err := pinnedContent(selection)
		if err != nil {
			return "", err
		}
		selection.digest = sha256.Sum256(content)
		selection.digestSet = true
	}
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", errors.New("native selection token unavailable")
	}
	token := hex.EncodeToString(bytes)
	v.mu.Lock()
	v.paths[token] = selection
	v.mu.Unlock()
	return token, nil
}

func (v *selectionVault) consume(token string) (selectionPath, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	path, ok := v.paths[token]
	delete(v.paths, token)
	if !ok {
		return selectionPath{}, os.ErrNotExist
	}
	return path, nil
}

type DialogAdapter struct {
	runtime NativeRuntime
	vault   *selectionVault
}

func NewDialogAdapter(runtime NativeRuntime, vault *selectionVault) *DialogAdapter {
	return &DialogAdapter{runtime: runtime, vault: vault}
}

func (a *DialogAdapter) Select(ctx context.Context, request desktopcontract.DialogRequest) desktopcontract.Result[desktopcontract.DialogSelection] {
	var path string
	var err error
	switch request.Kind {
	case desktopcontract.DialogCreateProject:
		path, err = a.runtime.SaveFile(ctx, "Create LayerDraw Project", []string{"ldl"})
	case desktopcontract.DialogOpenProject:
		path, err = a.runtime.OpenFile(ctx, "Open LayerDraw Project", request.Extensions)
	case desktopcontract.DialogImport:
		extensions := request.Extensions
		if len(extensions) == 0 {
			extensions = []string{"layerdraw"}
		}
		path, err = a.runtime.OpenFile(ctx, "Import LayerDraw Project", extensions)
	case desktopcontract.DialogExport:
		path, err = a.runtime.SaveFile(ctx, "Export LayerDraw Project", request.Extensions)
	default:
		err = errors.New("unsupported native dialog")
	}
	if err != nil {
		return failed[desktopcontract.DialogSelection](desktopcontract.FailureAdapterUnavailable, desktopcontract.RecoveryRetry)
	}
	if path == "" {
		return desktopcontract.Result[desktopcontract.DialogSelection]{Outcome: protocolcommon.OutcomeCancelled, Failure: &desktopcontract.Failure{Code: desktopcontract.FailureDialogCancelled, Component: desktopcontract.ComponentBindingShell, Recovery: desktopcontract.RecoveryRetry}}
	}
	token, err := a.vault.issue(path)
	if err != nil {
		return failed[desktopcontract.DialogSelection](desktopcontract.FailureAdapterUnavailable, desktopcontract.RecoveryRetry)
	}
	return desktopcontract.Result[desktopcontract.DialogSelection]{Outcome: protocolcommon.OutcomeSuccess, Value: desktopcontract.DialogSelection{Token: token}}
}

type WindowAdapter struct{ runtime NativeRuntime }

func (a WindowAdapter) Show(ctx context.Context) error         { a.runtime.ShowWindow(ctx); return nil }
func (a WindowAdapter) RequestClose(ctx context.Context) error { a.runtime.Quit(ctx); return nil }

type ProjectStorageAdapter struct{ vault *selectionVault }

func NewProjectStorageAdapter(vault *selectionVault) *ProjectStorageAdapter {
	return &ProjectStorageAdapter{vault: vault}
}

func (a *ProjectStorageAdapter) Create(_ context.Context, token string) (desktopapp.ProjectLocation, error) {
	selection, err := a.vault.consume(token)
	if err != nil {
		return desktopapp.ProjectLocation{}, err
	}
	path := selection.path
	if filepath.Ext(path) == "" {
		path += ".ldl"
	}
	if strings.ToLower(filepath.Ext(path)) != ".ldl" {
		return desktopapp.ProjectLocation{}, errors.New("project entry must be LDL")
	}
	// A Desktop-created project owns a dedicated source-tree root. Treating the
	// save panel's parent directory as the root makes unrelated sibling LDL
	// files part of the project (for example every LDL in Downloads), and also
	// violates the canonical GUI project layout.
	root := strings.TrimSuffix(path, filepath.Ext(path))
	if root == "" || root == filepath.Dir(root) {
		return desktopapp.ProjectLocation{}, errors.New("project root is invalid")
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		return desktopapp.ProjectLocation{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(root)
		}
	}()
	for _, directory := range []string{"schema/entity_types", "schema/relation_types", "layers", "views", "references", "pack", "assets"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(directory)), 0o700); err != nil {
			return desktopapp.ProjectLocation{}, err
		}
	}
	entry := filepath.Join(root, "document.ldl")
	file, err := os.OpenFile(entry, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return desktopapp.ProjectLocation{}, err
	}
	if _, err = file.WriteString("project project \"Untitled\" {}\n"); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return desktopapp.ProjectLocation{}, err
	}
	committed = true
	return desktopapp.ProjectLocation{Root: root, EntryPath: "document.ldl", Kind: "project"}, nil
}

func (a *ProjectStorageAdapter) Open(_ context.Context, token string) (desktopapp.ProjectLocation, error) {
	selection, err := a.vault.consume(token)
	if err != nil {
		return desktopapp.ProjectLocation{}, err
	}
	location, err := projectLocationPinned(selection)
	if err != nil {
		return desktopapp.ProjectLocation{}, err
	}
	location.PinnedContent, err = pinnedContent(selection)
	return location, err
}

func (a *ProjectStorageAdapter) Import(_ context.Context, token string) (desktopapp.ProjectLocation, error) {
	selection, err := a.vault.consume(token)
	if err != nil {
		return desktopapp.ProjectLocation{}, err
	}
	path := selection.path
	if err := validateIdentity(selection); err != nil {
		return desktopapp.ProjectLocation{}, err
	}
	if strings.ToLower(filepath.Ext(path)) != ".layerdraw" {
		return desktopapp.ProjectLocation{}, errors.New("import must be a LayerDraw container")
	}
	if err := regularFile(path); err != nil {
		return desktopapp.ProjectLocation{}, err
	}
	content, err := pinnedContent(selection)
	if err != nil {
		return desktopapp.ProjectLocation{}, err
	}
	return desktopapp.ProjectLocation{Root: path, Kind: "container", PinnedContent: content}, nil
}

func (a *ProjectStorageAdapter) Relocate(_ context.Context, _ runtimeprotocol.DocumentID, token string) (desktopapp.ProjectLocation, error) {
	selection, err := a.vault.consume(token)
	if err != nil {
		return desktopapp.ProjectLocation{}, err
	}
	return projectLocationPinned(selection)
}

func projectLocation(path string) (desktopapp.ProjectLocation, error) {
	return projectLocationPinned(selectionPath{path: path})
}

func projectLocationPinned(selection selectionPath) (desktopapp.ProjectLocation, error) {
	path := selection.path
	if err := validateIdentity(selection); err != nil {
		return desktopapp.ProjectLocation{}, err
	}
	if err := regularFile(path); err != nil {
		return desktopapp.ProjectLocation{}, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ldl":
		return desktopapp.ProjectLocation{Root: filepath.Dir(path), EntryPath: filepath.Base(path), Kind: "project"}, nil
	case ".layerdraw":
		return desktopapp.ProjectLocation{Root: path, Kind: "container"}, nil
	default:
		return desktopapp.ProjectLocation{}, errors.New("unsupported project selection")
	}
}

func validateIdentity(selection selectionPath) error {
	if selection.identity == nil {
		return nil
	}
	current, err := os.Lstat(selection.path)
	if err != nil || !current.Mode().IsRegular() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(selection.identity, current) {
		return errors.New("native selection target changed")
	}
	if selection.digestSet {
		if _, err := pinnedContent(selection); err != nil {
			return err
		}
	}
	return nil
}

func pinnedContent(selection selectionPath) ([]byte, error) {
	if selection.identity == nil {
		return nil, nil
	}
	file, err := os.Open(selection.path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	current, err := file.Stat()
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(selection.identity, current) {
		return nil, errors.New("native selection target changed")
	}
	if current.Size() > maxPinnedAssociationBytes {
		return nil, errors.New("native selection target is too large")
	}
	content, err := io.ReadAll(io.LimitReader(file, maxPinnedAssociationBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maxPinnedAssociationBytes {
		return nil, errors.New("native selection target is too large")
	}
	if selection.digestSet && sha256.Sum256(content) != selection.digest {
		return nil, errors.New("native selection target changed")
	}
	return content, nil
}

func regularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("project selection is not a regular file")
	}
	return nil
}

func failed[T any](code desktopcontract.FailureCode, recovery desktopcontract.RecoveryAction) desktopcontract.Result[T] {
	return desktopcontract.Result[T]{Outcome: protocolcommon.OutcomeFailed, Failure: &desktopcontract.Failure{Code: code, Component: desktopcontract.ComponentBindingShell, Retryable: true, Recovery: recovery}}
}
