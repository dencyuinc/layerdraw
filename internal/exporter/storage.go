// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package exporter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
)

type AtomicFileStore struct{}

// Publish atomically replaces one explicitly selected destination. A failed
// write, sync, chmod, close, or rename never publishes a partial artifact.
func (AtomicFileStore) Publish(ctx context.Context, destination string, value []byte) error {
	if destination == "" || !filepath.IsAbs(destination) || filepath.Clean(destination) != destination {
		return failure(FailureDestination, nil)
	}
	if err := ctx.Err(); err != nil {
		return failure(FailureCancelled, err)
	}
	directory := filepath.Dir(destination)
	temporary, err := os.CreateTemp(directory, ".layerdraw-export-*")
	if err != nil {
		return failure(FailureDestination, err)
	}
	name := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return failure(FailureDestination, err)
	}
	if _, err := io.Copy(temporary, bytes.NewReader(value)); err != nil {
		return failure(FailureDestination, err)
	}
	if err := ctx.Err(); err != nil {
		return failure(FailureCancelled, err)
	}
	if err := temporary.Sync(); err != nil {
		return failure(FailureDestination, err)
	}
	if err := temporary.Close(); err != nil {
		return failure(FailureDestination, err)
	}
	if err := os.Rename(name, destination); err != nil {
		return failure(FailureDestination, err)
	}
	committed = true
	if directoryHandle, err := os.Open(directory); err == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	return nil
}

// PublishSet stages all files before publication and rolls back every rename
// if a later destination fails. All destinations must share one directory so
// the rename operations retain filesystem atomicity.
func (AtomicFileStore) PublishSet(ctx context.Context, files map[string][]byte) error {
	if len(files) == 0 {
		return failure(FailureDestination, nil)
	}
	directory := ""
	for destination := range files {
		if destination == "" || !filepath.IsAbs(destination) || filepath.Clean(destination) != destination {
			return failure(FailureDestination, nil)
		}
		if directory == "" {
			directory = filepath.Dir(destination)
		} else if filepath.Dir(destination) != directory {
			return failure(FailureDestination, nil)
		}
	}
	type stagedFile struct {
		destination, staged, backup string
		existed, committed          bool
	}
	staged := make([]stagedFile, 0, len(files))
	destinations := make([]string, 0, len(files))
	for destination := range files {
		destinations = append(destinations, destination)
	}
	sort.Strings(destinations)
	rollback := func() {
		for i := len(staged) - 1; i >= 0; i-- {
			item := &staged[i]
			if item.committed {
				_ = os.Remove(item.destination)
			}
			if item.existed {
				_ = os.Rename(item.backup, item.destination)
			}
			_ = os.Remove(item.staged)
			_ = os.Remove(item.backup)
		}
	}
	for _, destination := range destinations {
		if err := ctx.Err(); err != nil {
			rollback()
			return failure(FailureCancelled, err)
		}
		temporary, err := os.CreateTemp(directory, ".layerdraw-export-set-*")
		if err != nil {
			rollback()
			return failure(FailureDestination, err)
		}
		name := temporary.Name()
		if err = temporary.Chmod(0o600); err == nil {
			_, err = temporary.Write(files[destination])
		}
		if err == nil {
			err = temporary.Sync()
		}
		closeErr := temporary.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(name)
			rollback()
			return failure(FailureDestination, err)
		}
		item := stagedFile{destination: destination, staged: name}
		if _, err := os.Lstat(destination); err == nil {
			backup, createErr := os.CreateTemp(directory, ".layerdraw-export-backup-*")
			if createErr != nil {
				rollback()
				return failure(FailureDestination, createErr)
			}
			item.backup = backup.Name()
			_ = backup.Close()
			_ = os.Remove(item.backup)
			if err := os.Rename(destination, item.backup); err != nil {
				rollback()
				return failure(FailureDestination, err)
			}
			item.existed = true
		}
		staged = append(staged, item)
	}
	for i := range staged {
		if err := os.Rename(staged[i].staged, staged[i].destination); err != nil {
			rollback()
			return failure(FailureDestination, err)
		}
		staged[i].committed = true
	}
	for i := range staged {
		if staged[i].existed {
			_ = os.Remove(staged[i].backup)
		}
	}
	if handle, err := os.Open(directory); err == nil {
		_ = handle.Sync()
		_ = handle.Close()
	}
	return nil
}

type AssetStore struct{ root string }

func NewAssetStore(root string) (*AssetStore, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("asset store root must be absolute")
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, err
	}
	return &AssetStore{root: root}, nil
}

func (s *AssetStore) Import(ctx context.Context, mediaType string, value []byte, expected *protocolcommon.Digest) (protocolcommon.Digest, error) {
	if err := ctx.Err(); err != nil {
		return "", failure(FailureCancelled, err)
	}
	if unsafeAsset(mediaType, value) {
		return "", failure(FailureUnsafeAsset, nil)
	}
	actual := digest(value)
	if expected != nil && *expected != actual {
		return "", failure(FailureDigestMismatch, nil)
	}
	path := filepath.Join(s.root, strings.TrimPrefix(string(actual), "sha256:"))
	if existing, err := os.ReadFile(path); err == nil {
		if digest(existing) != actual {
			return "", failure(FailureDigestMismatch, nil)
		}
		return actual, nil
	}
	if err := (AtomicFileStore{}).Publish(ctx, path, value); err != nil {
		return "", err
	}
	return actual, nil
}

func (s *AssetStore) Resolve(ctx context.Context, value protocolcommon.Digest) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, failure(FailureCancelled, err)
	}
	path := filepath.Join(s.root, strings.TrimPrefix(string(value), "sha256:"))
	loaded, err := os.ReadFile(path)
	if err != nil {
		return nil, failure(FailureAssetMissing, err)
	}
	if digest(loaded) != value {
		return nil, failure(FailureDigestMismatch, nil)
	}
	return loaded, nil
}

func unsafeAsset(mediaType string, value []byte) bool {
	lower := strings.ToLower(mediaType)
	if strings.Contains(lower, "javascript") || strings.Contains(lower, "executable") || strings.Contains(lower, "x-sh") {
		return true
	}
	if lower == "image/svg+xml" {
		body := strings.ToLower(string(value))
		return strings.Contains(body, "<script") || strings.Contains(body, "javascript:") || strings.Contains(body, "href=\"http") || strings.Contains(body, "href='http")
	}
	return false
}

type PreviewMetadata struct {
	SchemaVersion  int64                   `json:"schema_version"`
	ArtifactDigest protocolcommon.Digest   `json:"artifact_digest"`
	InvocationHash protocolcommon.Digest   `json:"invocation_hash"`
	RevisionID     string                  `json:"revision_id"`
	ProfileDigest  protocolcommon.Digest   `json:"profile_digest"`
	MediaType      string                  `json:"media_type"`
	AssetDigests   []protocolcommon.Digest `json:"asset_digests"`
}

type PreviewExpectation struct {
	InvocationHash protocolcommon.Digest
	RevisionID     string
	ProfileDigest  protocolcommon.Digest
	MediaType      string
}

type Preview struct {
	Bytes         []byte
	SourceOfTruth bool
	MissingAssets []protocolcommon.Digest
}

type PreviewStore struct {
	root   string
	assets *AssetStore
}

func NewPreviewStore(root string, assets *AssetStore) (*PreviewStore, error) {
	if root == "" || !filepath.IsAbs(root) || assets == nil {
		return nil, errors.New("preview store composition is incomplete")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &PreviewStore{root: filepath.Clean(root), assets: assets}, nil
}

func (s *PreviewStore) Put(ctx context.Context, id string, metadata PreviewMetadata, value []byte) error {
	if !safeID(id) || metadata.SchemaVersion != 1 || metadata.ArtifactDigest != digest(value) {
		return failure(FailurePreviewIncompatible, nil)
	}
	encoded, err := canonical(metadata)
	if err != nil {
		return failure(FailurePreviewIncompatible, err)
	}
	directory := filepath.Join(s.root, id)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return failure(FailureDestination, err)
	}
	if err := (AtomicFileStore{}).Publish(ctx, filepath.Join(directory, "artifact"), value); err != nil {
		return err
	}
	if err := (AtomicFileStore{}).Publish(ctx, filepath.Join(directory, "metadata.json"), encoded); err != nil {
		_ = os.Remove(filepath.Join(directory, "artifact"))
		return err
	}
	return nil
}

func (s *PreviewStore) Load(ctx context.Context, id string, expected PreviewExpectation) (Preview, error) {
	if !safeID(id) {
		return Preview{}, failure(FailurePreviewIncompatible, nil)
	}
	directory := filepath.Join(s.root, id)
	metadataBytes, err := os.ReadFile(filepath.Join(directory, "metadata.json"))
	if err != nil {
		return Preview{}, failure(FailurePreviewStale, err)
	}
	var metadata PreviewMetadata
	decoder := json.NewDecoder(bytes.NewReader(metadataBytes))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&metadata) != nil || metadata.SchemaVersion != 1 {
		return Preview{}, failure(FailurePreviewIncompatible, nil)
	}
	if metadata.InvocationHash != expected.InvocationHash || metadata.RevisionID != expected.RevisionID {
		return Preview{}, failure(FailurePreviewStale, nil)
	}
	if metadata.ProfileDigest != expected.ProfileDigest || metadata.MediaType != expected.MediaType {
		return Preview{}, failure(FailurePreviewIncompatible, nil)
	}
	value, err := os.ReadFile(filepath.Join(directory, "artifact"))
	if err != nil || digest(value) != metadata.ArtifactDigest {
		return Preview{}, failure(FailurePreviewStale, err)
	}
	missing := []protocolcommon.Digest{}
	for _, asset := range metadata.AssetDigests {
		if _, err := s.assets.Resolve(ctx, asset); err != nil {
			missing = append(missing, asset)
		}
	}
	return Preview{Bytes: value, SourceOfTruth: false, MissingAssets: missing}, nil
}

func safeID(value string) bool {
	if value == "" || len(value) > 128 || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
			return false
		}
	}
	return true
}
