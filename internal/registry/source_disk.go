// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/dencyuinc/layerdraw/internal/privatefs"
)

type diskSourceState struct {
	Version uint32           `json:"version"`
	Sources []RegistrySource `json:"sources"`
}

// DiskSourceStateStore durably preserves non-secret source configuration.
// Remote credential leases are deliberately discarded by Registry on attach.
type DiskSourceStateStore struct {
	path string
	mu   sync.Mutex
}

func NewDiskSourceStateStore(path string) (*DiskSourceStateStore, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("Registry source state path must be a clean absolute path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return &DiskSourceStateStore{path: path}, nil
}

func (s *DiskSourceStateStore) LoadRegistrySources(ctx context.Context) ([]RegistrySource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return []RegistrySource{}, nil
	}
	if err != nil {
		return nil, err
	}
	var state diskSourceState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF || state.Version != 1 || state.Sources == nil {
		return nil, errors.New("Registry source state is invalid")
	}
	return append([]RegistrySource(nil), state.Sources...), nil
}

func (s *DiskSourceStateStore) SaveRegistrySources(ctx context.Context, sources []RegistrySource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(diskSourceState{Version: 1, Sources: append([]RegistrySource(nil), sources...)})
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".registry-sources-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(name)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, s.path); err != nil {
		return err
	}
	committed = true
	directory, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	syncErr := privatefs.SyncDirectory(directory)
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

var _ SourceStateStore = (*DiskSourceStateStore)(nil)
