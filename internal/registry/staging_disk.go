// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/dencyuinc/layerdraw/internal/privatefs"
)

const DefaultMaxStagedObjectBytes int64 = 256 << 20

var stagedDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type StagedObjectStore interface {
	PutRegistryObject(context.Context, string, io.Reader, int64) (StagedObjectRef, error)
	OpenRegistryObject(context.Context, StagedObjectRef) (io.ReadCloser, error)
}

// DiskStagedObjectStore is a content-addressed, process-local synchronized
// durable store shared by Registry planning and the Runtime publication owner.
// A digest is verified both before publication and on every open.
type DiskStagedObjectStore struct {
	root     string
	maxBytes int64
	mu       sync.Mutex
}

func NewDiskStagedObjectStore(root string, maxBytes int64) (*DiskStagedObjectStore, error) {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, errors.New("Registry staged-object root must be a clean absolute path")
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxStagedObjectBytes
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &DiskStagedObjectStore{root: root, maxBytes: maxBytes}, nil
}

func (s *DiskStagedObjectStore) PutRegistryObject(ctx context.Context, mediaType string, source io.Reader, expectedSize int64) (StagedObjectRef, error) {
	if source == nil || mediaType == "" || strings.ContainsAny(mediaType, "\x00\r\n") || expectedSize < 0 || expectedSize > s.maxBytes {
		return StagedObjectRef{}, errors.New("Registry staged object input is invalid")
	}
	if err := ctx.Err(); err != nil {
		return StagedObjectRef{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	temp, err := os.CreateTemp(s.root, ".object-*")
	if err != nil {
		return StagedObjectRef{}, err
	}
	tempName := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return StagedObjectRef{}, err
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(source, s.maxBytes+1))
	if err != nil || written != expectedSize || written > s.maxBytes {
		return StagedObjectRef{}, errors.New("Registry staged object size mismatch")
	}
	digest := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	name := strings.TrimPrefix(digest, "sha256:") + ".blob"
	target := filepath.Join(s.root, name)
	if existing, err := os.ReadFile(target); err == nil {
		if int64(len(existing)) != written || digestBytes(existing) != digest {
			return StagedObjectRef{}, errors.New("Registry staged object collision")
		}
		return StagedObjectRef{ObjectID: name, Digest: digest, Size: written, MediaType: mediaType}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return StagedObjectRef{}, err
	}
	if err := temp.Sync(); err != nil {
		return StagedObjectRef{}, err
	}
	if err := temp.Close(); err != nil {
		return StagedObjectRef{}, err
	}
	if err := os.Rename(tempName, target); err != nil {
		return StagedObjectRef{}, err
	}
	committed = true
	directory, err := os.Open(s.root)
	if err != nil {
		return StagedObjectRef{}, err
	}
	syncErr := privatefs.SyncDirectory(directory)
	closeErr := directory.Close()
	if syncErr != nil {
		return StagedObjectRef{}, syncErr
	}
	if closeErr != nil {
		return StagedObjectRef{}, closeErr
	}
	return StagedObjectRef{ObjectID: name, Digest: digest, Size: written, MediaType: mediaType}, nil
}

func (s *DiskStagedObjectStore) OpenRegistryObject(ctx context.Context, ref StagedObjectRef) (io.ReadCloser, error) {
	if err := validateStagedRef(ref, s.maxBytes); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	objectID := filepath.Base(ref.ObjectID)
	if objectID != ref.ObjectID {
		return nil, errors.New("Registry staged object reference is invalid")
	}
	data, err := os.ReadFile(filepath.Join(s.root, objectID))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != ref.Size || digestBytes(data) != ref.Digest {
		return nil, errors.New("Registry staged object failed integrity verification")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func validateStagedRef(ref StagedObjectRef, maxBytes int64) error {
	if !stagedDigestPattern.MatchString(ref.Digest) || ref.ObjectID != strings.TrimPrefix(ref.Digest, "sha256:")+".blob" || ref.Size < 0 || ref.Size > maxBytes || ref.MediaType == "" || strings.ContainsAny(ref.MediaType, "\x00\r\n") {
		return errors.New("Registry staged object reference is invalid")
	}
	return nil
}
