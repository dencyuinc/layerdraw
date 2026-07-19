// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package local implements the Runtime storage ports using a private local
// filesystem tree. It deliberately contains no LDL or container semantics.
package local

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

const (
	dirMode        = 0o700
	fileMode       = 0o600
	defaultMaxJSON = 16 << 20
	defaultMaxBlob = 1 << 30
)

// Options supplies deterministic test boundaries without weakening on-disk
// validation. Zero values select secure production defaults.
type Options struct {
	Now           func() time.Time
	Random        io.Reader
	MaxJSONBytes  int64
	MaxAssetBytes uint64
	// Fault is an optional deterministic test hook called immediately before
	// filesystem operations. Production hosts leave it nil.
	Fault func(operation, path string) error
}

// Store is the shared filesystem core embedded by the five port adapters.
type Store struct {
	root     string
	now      func() time.Time
	random   io.Reader
	maxJSON  int64
	maxAsset uint64
	fault    func(string, string) error
}

func New(root string, options Options) (*Store, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, fmt.Errorf("local adapter root must be absolute")
	}
	clean := filepath.Clean(root)
	if err := secureRoot(clean); err != nil {
		return nil, err
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	if options.MaxJSONBytes <= 0 {
		options.MaxJSONBytes = defaultMaxJSON
	}
	if options.MaxAssetBytes == 0 {
		options.MaxAssetBytes = defaultMaxBlob
	}
	return &Store{root: clean, now: options.Now, random: options.Random, maxJSON: options.MaxJSONBytes, maxAsset: options.MaxAssetBytes, fault: options.Fault}, nil
}

type Document struct{ *Store }
type State struct{ *Store }
type Assets struct{ *Store }
type History struct{ *Store }
type Recovery struct{ *Store }

func NewDocumentStore(root string, o Options) (*Document, error) {
	v, e := New(root, o)
	if e != nil {
		return nil, e
	}
	return &Document{v}, nil
}
func NewStateBackend(root string, o Options) (*State, error) {
	v, e := New(root, o)
	if e != nil {
		return nil, e
	}
	return &State{v}, nil
}
func NewAssetStore(root string, o Options) (*Assets, error) {
	v, e := New(root, o)
	if e != nil {
		return nil, e
	}
	return &Assets{v}, nil
}
func NewHistoryStore(root string, o Options) (*History, error) {
	v, e := New(root, o)
	if e != nil {
		return nil, e
	}
	return &History{v}, nil
}
func NewRecoveryJournal(root string, o Options) (*Recovery, error) {
	v, e := New(root, o)
	if e != nil {
		return nil, e
	}
	return &Recovery{v}, nil
}

func secureRoot(root string) error {
	existed := false
	for p := root; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("local adapter root crosses non-directory or symlink: %w", port.ErrConflict)
			}
			if p == root {
				existed = true
				if info.Mode().Perm() != dirMode {
					entries, readErr := os.ReadDir(root)
					if readErr != nil {
						return classify(readErr)
					}
					if len(entries) != 0 {
						return fmt.Errorf("local adapter root permissions are not private: %w", port.ErrConflict)
					}
					if chmodErr := os.Chmod(root, dirMode); chmodErr != nil {
						return classify(chmodErr)
					}
				}
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return classify(err)
		}
		next := filepath.Dir(p)
		if next == p {
			break
		}
	}
	if err := os.MkdirAll(root, dirMode); err != nil {
		return classify(err)
	}
	if !existed {
		if err := os.Chmod(root, dirMode); err != nil {
			return classify(err)
		}
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unsafe local adapter root: %w", port.ErrConflict)
	}
	if info.Mode().Perm() != dirMode {
		return fmt.Errorf("local adapter root permissions are not private: %w", port.ErrConflict)
	}
	return nil
}

func (s *Store) scopeDir(scope runtimeprotocol.RuntimeScope) (string, error) {
	b, err := runtimeprotocol.EncodeRuntimeScope(scope)
	if err != nil {
		return "", fmt.Errorf("invalid scope: %w", port.ErrConflict)
	}
	h := sha256.Sum256(b)
	p := filepath.Join(s.root, "scopes", hex.EncodeToString(h[:]))
	if err := s.ensureDir(p); err != nil {
		return "", err
	}
	return p, nil
}

func (s *Store) ensureDir(path string) error {
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escape: %w", port.ErrConflict)
	}
	cur := s.root
	rootInfo, err := os.Lstat(cur)
	if err != nil {
		return classify(err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 || rootInfo.Mode().Perm() != dirMode {
		return fmt.Errorf("unsafe root boundary: %w", port.ErrConflict)
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		info, statErr := os.Lstat(cur)
		if statErr == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != dirMode {
				return fmt.Errorf("unsafe directory boundary: %w", port.ErrConflict)
			}
			continue
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return classify(statErr)
		}
		if err := os.Mkdir(cur, dirMode); err != nil && !errors.Is(err, fs.ErrExist) {
			return classify(err)
		}
		if err := os.Chmod(cur, dirMode); err != nil {
			return classify(err)
		}
	}
	return nil
}

func (s *Store) validateParents(path string) error {
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escape: %w", port.ErrConflict)
	}
	parent := filepath.Dir(rel)
	cur := s.root
	parts := []string{}
	if parent != "." {
		parts = strings.Split(parent, string(filepath.Separator))
	}
	for _, part := range append([]string{""}, parts...) {
		if part != "" {
			cur = filepath.Join(cur, part)
		}
		info, e := os.Lstat(cur)
		if e != nil {
			return classify(e)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != dirMode {
			return fmt.Errorf("unsafe directory boundary: %w", port.ErrConflict)
		}
	}
	return nil
}

func (s *Store) validateFile(path string) (fs.FileInfo, error) {
	if err := s.validateParents(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, classify(err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("unsafe file boundary: %w", port.ErrConflict)
	}
	if info.Mode().Perm() != fileMode {
		return nil, fmt.Errorf("file permissions are not private: %w", port.ErrConflict)
	}
	return info, nil
}

func safeID(value string) (string, error) {
	if value == "" || len(value) > 1024 || !utf8.ValidString(value) {
		return "", fmt.Errorf("invalid opaque identity: %w", port.ErrConflict)
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x21 || value[i] > 0x7e || value[i] == '/' || value[i] == '\\' {
			return "", fmt.Errorf("ambiguous opaque identity: %w", port.ErrConflict)
		}
	}
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:]), nil
}

func digestBytes(data []byte) protocolcommon.Digest {
	h := sha256.Sum256(data)
	return protocolcommon.Digest("sha256:" + hex.EncodeToString(h[:]))
}

func validDigest(d protocolcommon.Digest) bool {
	v := string(d)
	if len(v) != 71 || !strings.HasPrefix(v, "sha256:") {
		return false
	}
	for _, c := range v[7:] {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	_, err := hex.DecodeString(v[7:])
	return err == nil
}

func parseUint(v protocolcommon.CanonicalUint64) (uint64, error) {
	n, e := strconv.ParseUint(string(v), 10, 64)
	if e == nil && strconv.FormatUint(n, 10) != string(v) {
		e = fmt.Errorf("non-canonical uint")
	}
	return n, e
}
func parseNN(v protocolcommon.CanonicalNonNegativeInt64) (uint64, error) {
	n, e := strconv.ParseUint(string(v), 10, 63)
	if e == nil && strconv.FormatUint(n, 10) != string(v) {
		e = fmt.Errorf("non-canonical non-negative int")
	}
	return n, e
}

func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %v", port.ErrNotFound, err)
	}
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("local storage permission: %w", err)
	}
	return err
}

func (s *Store) readJSON(path string, out any) error {
	info, err := s.validateFile(path)
	if err != nil {
		return err
	}
	if info.Size() > s.maxJSON {
		return fmt.Errorf("corrupt local record: %w", port.ErrIndeterminate)
	}
	if s.fault != nil {
		if err := s.fault("open", path); err != nil {
			return classify(err)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return classify(err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return classify(err)
	}
	if !os.SameFile(info, opened) {
		return fmt.Errorf("file changed during open: %w", port.ErrConflict)
	}
	dec := json.NewDecoder(io.LimitReader(f, s.maxJSON+1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("corrupt local record: %w", port.ErrIndeterminate)
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return fmt.Errorf("trailing local record data: %w", port.ErrIndeterminate)
	}
	return nil
}

func (s *Store) readValidatedFile(path string, max int64) ([]byte, error) {
	info, err := s.validateFile(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > max {
		return nil, fmt.Errorf("file exceeds bound: %w", port.ErrIndeterminate)
	}
	if s.fault != nil {
		if err := s.fault("open", path); err != nil {
			return nil, classify(err)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, classify(err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, classify(err)
	}
	if !os.SameFile(info, opened) {
		return nil, fmt.Errorf("file changed during open: %w", port.ErrConflict)
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, classify(err)
	}
	if int64(len(data)) != info.Size() {
		return nil, fmt.Errorf("file changed during read: %w", port.ErrIndeterminate)
	}
	return data, nil
}

func (s *Store) writeJSON(path string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if int64(len(b)) > s.maxJSON {
		return fmt.Errorf("local record too large: %w", port.ErrConflict)
	}
	return s.atomicWrite(path, bytes.NewReader(b), int64(len(b)))
}

func (s *Store) atomicWrite(path string, r io.Reader, size int64) (retErr error) {
	dir := filepath.Dir(path)
	if err := s.ensureDir(dir); err != nil {
		return err
	}
	if err := s.validateParents(path); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink target rejected: %w", port.ErrConflict)
	} else if err == nil && (!info.Mode().IsRegular() || info.Mode().Perm() != fileMode) {
		return fmt.Errorf("unsafe file target: %w", port.ErrConflict)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return classify(err)
	}
	if s.fault != nil {
		if err := s.fault("open", path); err != nil {
			return classify(err)
		}
	}
	f, err := os.CreateTemp(dir, ".layerdraw-tmp-")
	if err != nil {
		return classify(err)
	}
	tmp := f.Name()
	defer func() { _ = f.Close(); _ = os.Remove(tmp) }()
	if err = f.Chmod(fileMode); err != nil {
		return classify(err)
	}
	if s.fault != nil {
		if err := s.fault("write", path); err != nil {
			return classify(err)
		}
	}
	n, err := io.Copy(f, r)
	if err != nil {
		return classify(err)
	}
	if size >= 0 && n != size {
		return fmt.Errorf("short local write: %w", port.ErrIndeterminate)
	}
	if s.fault != nil {
		if err := s.fault("sync", path); err != nil {
			return classify(err)
		}
	}
	if err = f.Sync(); err != nil {
		return classify(err)
	}
	if err = f.Close(); err != nil {
		return classify(err)
	}
	if s.fault != nil {
		if err := s.fault("rename", path); err != nil {
			return classify(err)
		}
	}
	if err = os.Rename(tmp, path); err != nil {
		return classify(err)
	}
	if s.fault != nil {
		if err := s.fault("diropen", dir); err != nil {
			return fmt.Errorf("directory open after rename: %w: %w", port.ErrIndeterminate, err)
		}
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("directory open after rename: %w: %w", port.ErrIndeterminate, err)
	}
	defer d.Close()
	if s.fault != nil {
		if err := s.fault("dirsync", dir); err != nil {
			return fmt.Errorf("directory sync after rename: %w: %w", port.ErrIndeterminate, err)
		}
	}
	if err = d.Sync(); err != nil {
		return fmt.Errorf("directory sync after rename: %w: %w", port.ErrIndeterminate, err)
	}
	return nil
}

func (s *Store) syncDirAfterMutation(dir string) error {
	if err := s.validateParents(filepath.Join(dir, "placeholder")); err != nil {
		return fmt.Errorf("post-mutation directory validation: %w: %w", port.ErrIndeterminate, err)
	}
	if s.fault != nil {
		if err := s.fault("dirsync", dir); err != nil {
			return fmt.Errorf("directory sync after mutation: %w: %w", port.ErrIndeterminate, err)
		}
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("directory open after mutation: %w: %w", port.ErrIndeterminate, err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("directory sync after mutation: %w: %w", port.ErrIndeterminate, err)
	}
	return nil
}

func (s *Store) withLock(scope runtimeprotocol.RuntimeScope, fn func(string) error) error {
	dir, err := s.scopeDir(scope)
	if err != nil {
		return err
	}
	lockPath := filepath.Join(dir, ".lock")
	if err := s.validateParents(lockPath); err != nil {
		return err
	}
	var prior fs.FileInfo
	if info, statErr := os.Lstat(lockPath); statErr == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != fileMode {
			return fmt.Errorf("unsafe lock file: %w", port.ErrConflict)
		}
		prior = info
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return classify(statErr)
	}
	f, err := openLockFile(lockPath)
	if err != nil {
		return classify(err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return classify(err)
	}
	if !opened.Mode().IsRegular() || opened.Mode().Perm() != fileMode || (prior != nil && !os.SameFile(prior, opened)) {
		return fmt.Errorf("unsafe lock file open: %w", port.ErrConflict)
	}
	if err := lockFile(f); err != nil {
		return classify(err)
	}
	defer unlockFile(f)
	return fn(dir)
}

func clone[T any](in T) (T, error) {
	var out T
	b, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(b, &out)
	return out, err
}

func randomToken(r io.Reader, prefix string) (string, error) {
	b := make([]byte, 24)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

var (
	_ port.DocumentStore   = (*Document)(nil)
	_ port.StateBackend    = (*State)(nil)
	_ port.AssetStore      = (*Assets)(nil)
	_ port.HistoryStore    = (*History)(nil)
	_ port.RecoveryJournal = (*Recovery)(nil)
	_                      = reflect.DeepEqual
)
