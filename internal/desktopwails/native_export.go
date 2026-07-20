// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	nativeexport "github.com/dencyuinc/layerdraw/internal/exporter"
)

type NativeInterchangeAdapter struct {
	vault  *selectionVault
	root   string
	mu     sync.Mutex
	staged map[string]nativeexport.Result
}

func NewNativeInterchangeAdapter(vault *selectionVault, root string) (*NativeInterchangeAdapter, error) {
	if vault == nil || root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("native interchange composition is incomplete")
	}
	root = filepath.Join(root, "native-interchange")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &NativeInterchangeAdapter{vault: vault, root: root, staged: map[string]nativeexport.Result{}}, nil
}

func (a *NativeInterchangeAdapter) Start(context.Context) error { return nil }
func (a *NativeInterchangeAdapter) Shutdown(context.Context) error {
	a.mu.Lock()
	clear(a.staged)
	a.mu.Unlock()
	return nil
}
func (a *NativeInterchangeAdapter) Profiles() []nativeexport.Profile { return nativeexport.Profiles() }

func (a *NativeInterchangeAdapter) Serialize(ctx context.Context, input nativeexport.SerializeInput) (desktopapp.NativeSerializeResult, error) {
	result, err := nativeexport.Serialize(ctx, input)
	if err != nil {
		return desktopapp.NativeSerializeResult{}, err
	}
	var primary nativeexport.Artifact
	for _, artifact := range result.Artifacts {
		if artifact.Primary {
			primary = artifact
			break
		}
	}
	if primary.LogicalPath == "" {
		return desktopapp.NativeSerializeResult{}, errors.New("native export primary artifact is unavailable")
	}
	id, err := randomID()
	if err != nil {
		return desktopapp.NativeSerializeResult{}, err
	}
	a.mu.Lock()
	a.staged[id] = result
	a.mu.Unlock()
	return desktopapp.NativeSerializeResult{Artifact: desktopapp.NativeArtifactRef{ArtifactID: id, LogicalPath: primary.LogicalPath, MediaType: primary.MediaType, ContentDigest: primary.ContentDigest}, SourceManifest: result, Manifest: append([]byte(nil), result.SourceManifestJSON...)}, nil
}

func (a *NativeInterchangeAdapter) Publish(ctx context.Context, token, artifactID string) error {
	destination, err := a.vault.consume(token)
	if err != nil {
		return err
	}
	a.mu.Lock()
	result, ok := a.staged[artifactID]
	if ok {
		delete(a.staged, artifactID)
	}
	a.mu.Unlock()
	if !ok {
		return os.ErrNotExist
	}
	var primary nativeexport.Artifact
	for _, artifact := range result.Artifacts {
		if artifact.Primary {
			primary = artifact
			break
		}
	}
	if primary.LogicalPath == "" {
		return errors.New("native export primary artifact is unavailable")
	}
	if filepath.Ext(destination) == "" {
		destination += filepath.Ext(primary.LogicalPath)
	}
	files := map[string][]byte{destination: primary.Bytes}
	manifestPath := strings.TrimSuffix(destination, filepath.Ext(destination)) + ".sources.json"
	files[manifestPath] = result.SourceManifestJSON
	return (nativeexport.AtomicFileStore{}).PublishSet(ctx, files)
}

func (a *NativeInterchangeAdapter) Import(ctx context.Context, token, profile string) (nativeexport.ImportPreview, error) {
	path, err := a.vault.consume(token)
	if err != nil {
		return nativeexport.ImportPreview{}, err
	}
	if profile != nativeexport.OperationsJSONProfile {
		return nativeexport.ImportPreview{}, errors.New("external import profile unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return nativeexport.ImportPreview{}, errors.New("external import selection is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return nativeexport.ImportPreview{}, err
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
	if err != nil || len(value) > 16<<20 {
		return nativeexport.ImportPreview{}, errors.New("external import selection is invalid")
	}
	return nativeexport.ImportOperationsJSON(ctx, value, 16<<20, 10_000)
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

var _ desktopapp.NativeInterchangePort = (*NativeInterchangeAdapter)(nil)
var _ desktopapp.Adapter = (*NativeInterchangeAdapter)(nil)
