// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
package local

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type assetDisk struct {
	Metadata port.AssetMetadata `json:"metadata"`
}

func validateAssetRef(ref port.AssetRef) error {
	if !validDigest(ref.Digest) {
		return port.ErrConflict
	}
	return nil
}

func (s *Assets) assetPaths(ref port.AssetRef) (string, string, error) {
	if err := validateAssetRef(ref); err != nil {
		return "", "", err
	}
	dir, err := s.scopeDir(ref.Scope)
	if err != nil {
		return "", "", err
	}
	id, _ := safeID(string(ref.Digest))
	base := filepath.Join(dir, "assets", id)
	return base + ".data", base + ".json", nil
}

func (s *Assets) Stat(ctx context.Context, ref port.AssetRef) (port.AssetMetadata, error) {
	if err := ctx.Err(); err != nil {
		return port.AssetMetadata{}, err
	}
	_, metaPath, err := s.assetPaths(ref)
	if err != nil {
		return port.AssetMetadata{}, err
	}
	var disk assetDisk
	if err := s.readJSON(metaPath, &disk); err != nil {
		return port.AssetMetadata{}, err
	}
	if disk.Metadata.Digest != ref.Digest {
		return port.AssetMetadata{}, fmt.Errorf("asset metadata mismatch: %w", port.ErrIndeterminate)
	}
	if err := s.validateAssetData(dataPathFor(metaPath), disk.Metadata); err != nil {
		return port.AssetMetadata{}, err
	}
	return disk.Metadata, nil
}

func dataPathFor(meta string) string { return strings.TrimSuffix(meta, ".json") + ".data" }
func (s *Assets) validateAssetData(path string, meta port.AssetMetadata) error {
	f, err := s.openVerifiedAsset(path, meta)
	if err != nil {
		return err
	}
	return f.Close()
}
func (s *Assets) openVerifiedAsset(path string, meta port.AssetMetadata) (*os.File, error) {
	size, err := parseUint(meta.Size)
	if err != nil || size >= math.MaxInt64 || size > s.maxAsset || !validDigest(meta.Digest) || meta.MediaType == "" {
		return nil, fmt.Errorf("invalid asset metadata: %w", port.ErrIndeterminate)
	}
	size64 := int64(size)
	info, err := s.validateFile(path)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return nil, fmt.Errorf("asset data missing: %w", port.ErrIndeterminate)
		}
		return nil, err
	}
	if uint64(info.Size()) != size {
		return nil, fmt.Errorf("asset size mismatch: %w", port.ErrIndeterminate)
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
	opened, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, classify(err)
	}
	if !os.SameFile(info, opened) {
		_ = f.Close()
		return nil, fmt.Errorf("asset changed during open: %w", port.ErrConflict)
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, size64+1))
	if err != nil {
		_ = f.Close()
		return nil, classify(err)
	}
	if uint64(n) != size || protocolcommon.Digest("sha256:"+fmt.Sprintf("%x", h.Sum(nil))) != meta.Digest {
		_ = f.Close()
		return nil, fmt.Errorf("asset digest mismatch: %w", port.ErrIndeterminate)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, classify(err)
	}
	return f, nil
}

func (s *Assets) Get(ctx context.Context, ref port.AssetRef) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dataPath, _, err := s.assetPaths(ref)
	if err != nil {
		return nil, err
	}
	meta, err := s.Stat(ctx, ref)
	if err != nil {
		return nil, err
	}
	return s.openVerifiedAsset(dataPath, meta)
}

func (s *Assets) PutIfAbsent(ctx context.Context, in port.PutAssetInput) (port.AssetMetadata, error) {
	if err := ctx.Err(); err != nil {
		return port.AssetMetadata{}, err
	}
	if !validDigest(in.ExpectedDigest) || in.MediaType == "" || len(in.MediaType) > 255 || strings.ContainsAny(in.MediaType, "\r\n\x00") {
		return port.AssetMetadata{}, port.ErrConflict
	}
	if in.Contents == nil {
		return port.AssetMetadata{}, port.ErrConflict
	}
	size, err := parseUint(in.Size)
	if err != nil || size >= math.MaxInt64 || size > s.maxAsset {
		return port.AssetMetadata{}, port.ErrConflict
	}
	size64 := int64(size)
	ref := port.AssetRef{Scope: in.Scope, Digest: in.ExpectedDigest}
	dataPath, metaPath, err := s.assetPaths(ref)
	if err != nil {
		return port.AssetMetadata{}, err
	}
	meta := port.AssetMetadata{Digest: in.ExpectedDigest, MediaType: in.MediaType, Size: in.Size}
	err = s.withLock(in.Scope, func(_ string) error {
		var existing assetDisk
		if err := s.readJSON(metaPath, &existing); err == nil {
			if existing.Metadata == meta {
				if err := s.validateAssetData(dataPath, meta); err != nil {
					return err
				}
				return nil
			}
			return port.ErrConflict
		} else if !errors.Is(err, port.ErrNotFound) {
			return err
		}
		limited := &io.LimitedReader{R: in.Contents, N: size64 + 1}
		dir := filepath.Dir(dataPath)
		if err := s.ensureDir(dir); err != nil {
			return err
		}
		if s.fault != nil {
			if err := s.fault("open", dataPath); err != nil {
				return classify(err)
			}
		}
		f, err := os.CreateTemp(dir, ".asset-")
		if err != nil {
			return classify(err)
		}
		tmp := f.Name()
		defer func() { _ = f.Close(); _ = os.Remove(tmp) }()
		if err := f.Chmod(fileMode); err != nil {
			return classify(err)
		}
		if s.fault != nil {
			if err := s.fault("write", dataPath); err != nil {
				return classify(err)
			}
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(f, h), limited)
		if err != nil {
			return classify(err)
		}
		actual := protocolcommon.Digest("sha256:" + fmt.Sprintf("%x", h.Sum(nil)))
		if n != size64 || limited.N == 0 || actual != in.ExpectedDigest {
			return port.ErrConflict
		}
		if s.fault != nil {
			if err := s.fault("sync", dataPath); err != nil {
				return classify(err)
			}
		}
		if err := f.Sync(); err != nil {
			return classify(err)
		}
		if err := f.Close(); err != nil {
			return classify(err)
		}
		if s.fault != nil {
			if err := s.fault("rename", dataPath); err != nil {
				return classify(err)
			}
		}
		if err := os.Rename(tmp, dataPath); err != nil {
			return classify(err)
		}
		if err := s.syncDirAfterMutation(dir); err != nil {
			return err
		}
		if err := s.writeJSON(metaPath, assetDisk{Metadata: meta}); err != nil {
			if errors.Is(err, port.ErrIndeterminate) {
				return err
			}
			if removeErr := os.Remove(dataPath); removeErr != nil {
				return fmt.Errorf("asset rollback: %w: %w", port.ErrIndeterminate, removeErr)
			}
			if syncErr := s.syncDirAfterMutation(dir); syncErr != nil {
				return syncErr
			}
			return err
		}
		return nil
	})
	return meta, err
}

func (s *Assets) DeleteIfUnreferenced(ctx context.Context, in port.DeleteAssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !in.ExpectedUnreferenced {
		return port.ErrConflict
	}
	data, meta, err := s.assetPaths(in.AssetRef)
	if err != nil {
		return err
	}
	return s.withLock(in.Scope, func(_ string) error {
		if _, err := s.Stat(ctx, in.AssetRef); err != nil {
			return err
		}
		if _, err := s.validateFile(data); err != nil {
			return err
		}
		if _, err := s.validateFile(meta); err != nil {
			return err
		}
		if err := os.Remove(data); err != nil {
			return classify(err)
		}
		if err := s.syncDirAfterMutation(filepath.Dir(data)); err != nil {
			return err
		}
		if err := os.Remove(meta); err != nil {
			return fmt.Errorf("asset metadata removal after data deletion: %w: %w", port.ErrIndeterminate, err)
		}
		return s.syncDirAfterMutation(filepath.Dir(meta))
	})
}
