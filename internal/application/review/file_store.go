// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

const maxSnapshotBytes = 16 << 20

type FileStore struct {
	mu         sync.Mutex
	root, name string
}

func NewFileStore(root string) (*FileStore, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, ErrInvalid
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &FileStore{root: root, name: "review-proposals.json"}, nil
}

func (s *FileStore) Load(context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	directory, err := os.OpenRoot(s.root)
	if err != nil {
		return Snapshot{}, err
	}
	defer directory.Close()
	info, err := directory.Lstat(s.name)
	if errors.Is(err, fs.ErrNotExist) {
		return Snapshot{Version: 1, Proposals: []Proposal{}}, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > maxSnapshotBytes {
		return Snapshot{}, ErrInvalid
	}
	data, err := directory.ReadFile(s.name)
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, ErrInvalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Snapshot{}, ErrInvalid
	}
	if err := validateSnapshot(snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func (s *FileStore) Save(ctx context.Context, snapshot Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateSnapshot(snapshot); err != nil {
		return err
	}
	data, err := json.Marshal(snapshot)
	if err != nil || len(data) > maxSnapshotBytes {
		return ErrInvalid
	}
	tmp, err := os.CreateTemp(s.root, ".review-proposals-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(s.root, s.name)); err != nil {
		return err
	}
	directory, err := os.Open(s.root)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
