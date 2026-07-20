// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/privatefs"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

const (
	defaultExternalMaxFiles   = 4096
	defaultExternalMaxBytes   = int64(64 << 20)
	defaultExternalMaxEntries = 100000
	externalOwnerMarker       = ".layerdraw-external-owner"
)

type ExternalFileOptions struct {
	MaxFiles   int
	MaxBytes   int64
	MaxEntries int
	Fault      func(string) error
}

type ExternalFileBinding struct {
	Scope   runtimeprotocol.RuntimeScope
	Kind    port.ExternalFileKind
	Locator string
}

type ExternalFileStore struct {
	*Store
	maxFiles, maxEntries int
	maxBytes             int64
	fault                func(string) error
}

type externalBindingDisk struct {
	Kind    port.ExternalFileKind `json:"kind"`
	Locator string                `json:"locator"`
}

type externalStageDisk struct {
	Scope                   runtimeprotocol.RuntimeScope         `json:"scope"`
	OperationID             runtimeprotocol.OperationID          `json:"operation_id"`
	IdempotencyKey          runtimeprotocol.IdempotencyKey       `json:"idempotency_key"`
	RevisionID              runtimeprotocol.RevisionID           `json:"revision_id"`
	ExpectedProviderVersion runtimeprotocol.ProviderVersionToken `json:"expected_provider_version"`
	Stage                   port.ExternalFileStage               `json:"stage"`
	Kind                    port.ExternalFileKind                `json:"kind"`
	DesiredPaths            []string                             `json:"desired_paths"`
	DesiredDigests          map[string]protocolcommon.Digest     `json:"desired_digests"`
	PriorPaths              []string                             `json:"prior_paths"`
	StagedPath              string                               `json:"staged_path"`
	BackupPath              string                               `json:"backup_path"`
}

type externalReceiptDisk struct {
	Scope   runtimeprotocol.RuntimeScope `json:"scope"`
	Receipt port.ExternalFileReceipt     `json:"receipt"`
}

func NewExternalFileStore(root string, options ExternalFileOptions) (*ExternalFileStore, error) {
	store, err := New(root, Options{})
	if err != nil {
		return nil, err
	}
	if options.MaxFiles <= 0 {
		options.MaxFiles = defaultExternalMaxFiles
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = defaultExternalMaxBytes
	}
	if options.MaxEntries <= 0 {
		options.MaxEntries = defaultExternalMaxEntries
	}
	return &ExternalFileStore{Store: store, maxFiles: options.MaxFiles, maxBytes: options.MaxBytes, maxEntries: options.MaxEntries, fault: options.Fault}, nil
}

func (s *ExternalFileStore) Bind(ctx context.Context, input ExternalFileBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	locator, err := canonicalExternalLocator(input.Kind, input.Locator)
	if err != nil {
		return err
	}
	return s.withLock(input.Scope, func(dir string) error {
		path := filepath.Join(dir, "external", "binding.json")
		var existing externalBindingDisk
		if err := s.readJSON(path, &existing); err == nil {
			if existing.Kind != input.Kind || existing.Locator != locator {
				return port.ErrConflict
			}
			return nil
		} else if !externalMissing(err) {
			return err
		}
		return s.writeJSON(path, externalBindingDisk{Kind: input.Kind, Locator: locator})
	})
}

// Relocate replaces a binding only when the caller proves the exact prior
// canonical locator. This is used by the host's explicit moved-project flow;
// ordinary Bind calls remain immutable and fail on locator drift.
func (s *ExternalFileStore) Relocate(ctx context.Context, scope runtimeprotocol.RuntimeScope, kind port.ExternalFileKind, prior, replacement string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if prior == "" || !filepath.IsAbs(prior) || filepath.Clean(prior) != prior {
		return port.ErrConflict
	}
	oldLocator := prior
	newLocator, err := canonicalExternalLocator(kind, replacement)
	if err != nil {
		return err
	}
	return s.withLock(scope, func(dir string) error {
		path := filepath.Join(dir, "external", "binding.json")
		var existing externalBindingDisk
		if err := s.readJSON(path, &existing); err != nil {
			return err
		}
		if existing.Kind != kind || existing.Locator != oldLocator {
			return port.ErrConflict
		}
		return s.writeJSON(path, externalBindingDisk{Kind: kind, Locator: newLocator})
	})
}

// Matches verifies a trusted binding without exposing its locator.
func (s *ExternalFileStore) Matches(ctx context.Context, scope runtimeprotocol.RuntimeScope, kind port.ExternalFileKind, locator string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	canonical, err := canonicalExternalLocator(kind, locator)
	if err != nil {
		return err
	}
	return s.withLock(scope, func(dir string) error {
		var existing externalBindingDisk
		if err := s.readJSON(filepath.Join(dir, "external", "binding.json"), &existing); err != nil {
			return err
		}
		if existing.Kind != kind || existing.Locator != canonical {
			return port.ErrConflict
		}
		return nil
	})
}

func (s *ExternalFileStore) GetExternalHead(ctx context.Context, input port.GetExternalFileHeadInput) (port.ExternalFileHead, error) {
	if err := ctx.Err(); err != nil {
		return port.ExternalFileHead{}, err
	}
	var result port.ExternalFileHead
	err := s.withLock(input.Scope, func(dir string) error {
		binding, err := s.loadBinding(dir)
		if err != nil {
			return err
		}
		version, _, err := s.currentVersion(binding)
		if err != nil {
			return err
		}
		result.ProviderVersion = version
		return nil
	})
	return result, err
}

func (s *ExternalFileStore) Prepare(ctx context.Context, input port.PrepareExternalFileInput) (port.ExternalFileStage, error) {
	if err := ctx.Err(); err != nil {
		return port.ExternalFileStage{}, err
	}
	var result port.ExternalFileStage
	err := s.withLock(input.Scope, func(dir string) error {
		binding, err := s.loadBinding(dir)
		if err != nil {
			return err
		}
		if binding.Kind != input.Materialization.Kind {
			return port.ErrConflict
		}
		current, prior, err := s.currentVersion(binding)
		if err != nil || current != input.ExpectedProviderVersion {
			if err != nil {
				return err
			}
			return port.ErrConflict
		}
		materialization, desired, digest, err := s.normalize(input.Materialization)
		if err != nil {
			return err
		}
		stageID := externalIdentity(input.OperationID, input.IdempotencyKey)
		metadataPath := s.stageMetadataPath(dir, stageID)
		var existing externalStageDisk
		if err := s.readJSON(metadataPath, &existing); err == nil {
			if existing.OperationID != input.OperationID || existing.IdempotencyKey != input.IdempotencyKey || existing.RevisionID != input.RevisionID || existing.ExpectedProviderVersion != input.ExpectedProviderVersion || existing.Stage.MaterializationDigest != digest {
				return port.ErrConflict
			}
			result = existing.Stage
			return nil
		} else if !externalMissing(err) {
			return err
		}
		stagePath, backupPath := externalSiblingPaths(binding, stageID)
		if err := requireExternalAbsent(stagePath); err != nil {
			return err
		}
		if err := requireExternalAbsent(backupPath); err != nil {
			return err
		}
		result = port.ExternalFileStage{StageID: stageID, CandidateProviderVersion: runtimeprotocol.ProviderVersionToken(digest), MaterializationDigest: digest}
		if err := s.writeExternalStage(stagePath, materialization, externalOwner(stageID, digest)); err != nil {
			return err
		}
		record := externalStageDisk{Scope: input.Scope, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey, RevisionID: input.RevisionID, ExpectedProviderVersion: input.ExpectedProviderVersion, Stage: result, Kind: binding.Kind, DesiredPaths: desired, DesiredDigests: externalFileDigests(materialization.ProjectFiles), PriorPaths: prior, StagedPath: stagePath, BackupPath: backupPath}
		return s.writeJSON(metadataPath, record)
	})
	return result, err
}

func (s *ExternalFileStore) Publish(ctx context.Context, input port.PublishExternalFileInput) (port.ExternalFileReceipt, error) {
	if err := ctx.Err(); err != nil {
		return port.ExternalFileReceipt{}, err
	}
	var result port.ExternalFileReceipt
	err := s.withLock(input.Scope, func(dir string) error {
		if receipt, err := s.loadReceipt(dir, input.OperationID, input.IdempotencyKey); err == nil {
			result = receipt
			var stage externalStageDisk
			if readErr := s.readJSON(s.stageMetadataPath(dir, input.StageID), &stage); readErr == nil {
				_ = s.cleanupExternalArtifacts(dir, stage)
			}
			return nil
		} else if !externalMissing(err) {
			return err
		}
		var stage externalStageDisk
		if err := s.readJSON(s.stageMetadataPath(dir, input.StageID), &stage); err != nil {
			return err
		}
		if stage.Scope.DocumentID != input.Scope.DocumentID || stage.OperationID != input.OperationID || stage.IdempotencyKey != input.IdempotencyKey || stage.ExpectedProviderVersion != input.ExpectedProviderVersion || stage.Stage.StageID != input.StageID {
			return port.ErrConflict
		}
		binding, err := s.loadBinding(dir)
		if err != nil {
			return err
		}
		current, _, currentErr := s.currentVersion(binding)
		candidate := stage.Stage.CandidateProviderVersion
		inProgress := externalExists(stage.BackupPath)
		if currentErr == nil && current == candidate {
			if s.fault != nil {
				if err := s.fault("before_external_receipt"); err != nil {
					return err
				}
			}
			return s.finishReceipt(dir, stage, &result)
		}
		if !inProgress && (currentErr != nil || current != input.ExpectedProviderVersion) {
			return port.ErrConflict
		}
		if binding.Kind == port.ExternalFileKindProject {
			if err := s.publishProject(binding, stage); err != nil {
				return err
			}
		} else if err := s.publishContainer(binding, stage); err != nil {
			return err
		}
		verified, _, err := s.currentVersion(binding)
		if err != nil || verified != candidate {
			return port.ErrIndeterminate
		}
		if s.fault != nil {
			if err := s.fault("before_external_receipt"); err != nil {
				return err
			}
		}
		return s.finishReceipt(dir, stage, &result)
	})
	return result, err
}

func (s *ExternalFileStore) Inspect(ctx context.Context, input port.InspectExternalFileInput) (port.ExternalFileInspection, error) {
	if err := ctx.Err(); err != nil {
		return port.ExternalFileInspection{}, err
	}
	var result port.ExternalFileInspection
	err := s.withLock(input.Scope, func(dir string) error {
		if receipt, err := s.loadReceipt(dir, input.OperationID, input.IdempotencyKey); err == nil {
			result.Receipt = &receipt
			return nil
		} else if !externalMissing(err) {
			return err
		}
		stageID := externalIdentity(input.OperationID, input.IdempotencyKey)
		var stage externalStageDisk
		if err := s.readJSON(s.stageMetadataPath(dir, stageID), &stage); err != nil {
			return err
		}
		if stage.OperationID != input.OperationID || stage.IdempotencyKey != input.IdempotencyKey {
			return port.ErrConflict
		}
		value := stage.Stage
		result.Stage = &value
		return nil
	})
	return result, err
}

func (s *ExternalFileStore) Abort(ctx context.Context, input port.AbortExternalFileInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.withLock(input.Scope, func(dir string) error {
		if s.fault != nil {
			if err := s.fault("before_external_abort"); err != nil {
				return err
			}
		}
		var stage externalStageDisk
		metadata := s.stageMetadataPath(dir, input.StageID)
		if err := s.readJSON(metadata, &stage); err != nil {
			if externalMissing(err) {
				return nil
			}
			return err
		}
		if externalExists(stage.BackupPath) {
			return port.ErrConflict
		}
		if err := s.removeOwnedExternalPath(stage, stage.StagedPath, false); err != nil {
			return err
		}
		if err := trustedPathRemove(metadata); err != nil {
			return err
		}
		return syncExternalDir(filepath.Dir(metadata))
	})
}

func (s *ExternalFileStore) loadBinding(dir string) (externalBindingDisk, error) {
	var binding externalBindingDisk
	err := s.readJSON(filepath.Join(dir, "external", "binding.json"), &binding)
	return binding, err
}

func (s *ExternalFileStore) stageMetadataPath(dir, stage string) string {
	return filepath.Join(dir, "external", "stages", stage+".json")
}

func (s *ExternalFileStore) receiptPath(dir string, operation runtimeprotocol.OperationID) string {
	return filepath.Join(dir, "external", "receipts", string(operation)+".json")
}

func (s *ExternalFileStore) loadReceipt(dir string, operation runtimeprotocol.OperationID, key runtimeprotocol.IdempotencyKey) (port.ExternalFileReceipt, error) {
	var disk externalReceiptDisk
	if err := s.readJSON(s.receiptPath(dir, operation), &disk); err != nil {
		return port.ExternalFileReceipt{}, err
	}
	if disk.Receipt.OperationID != operation || disk.Receipt.IdempotencyKey != key {
		return port.ExternalFileReceipt{}, port.ErrConflict
	}
	return disk.Receipt, nil
}

func (s *ExternalFileStore) finishReceipt(dir string, stage externalStageDisk, result *port.ExternalFileReceipt) error {
	receipt := port.ExternalFileReceipt{OperationID: stage.OperationID, IdempotencyKey: stage.IdempotencyKey, RevisionID: stage.RevisionID, ProviderVersion: stage.Stage.CandidateProviderVersion, MaterializationDigest: stage.Stage.MaterializationDigest}
	receipt.ReceiptDigest = externalDigest(struct {
		OperationID           runtimeprotocol.OperationID          `json:"operation_id"`
		IdempotencyKey        runtimeprotocol.IdempotencyKey       `json:"idempotency_key"`
		RevisionID            runtimeprotocol.RevisionID           `json:"revision_id"`
		ProviderVersion       runtimeprotocol.ProviderVersionToken `json:"provider_version"`
		MaterializationDigest protocolcommon.Digest                `json:"materialization_digest"`
	}{receipt.OperationID, receipt.IdempotencyKey, receipt.RevisionID, receipt.ProviderVersion, receipt.MaterializationDigest})
	if err := s.writeJSON(s.receiptPath(dir, stage.OperationID), externalReceiptDisk{Scope: stage.Scope, Receipt: receipt}); err != nil {
		return err
	}
	*result = receipt
	// The receipt is authoritative and durable before cleanup begins. Cleanup
	// is opportunistic and idempotent; failure never turns publication into a
	// false failure, and a later Publish retries it from the retained metadata.
	_ = s.cleanupExternalArtifacts(dir, stage)
	return nil
}

func (s *ExternalFileStore) cleanupExternalArtifacts(dir string, stage externalStageDisk) error {
	if err := s.removeOwnedExternalPath(stage, stage.BackupPath, true); err != nil {
		return err
	}
	if err := s.removeOwnedExternalPath(stage, stage.StagedPath, false); err != nil {
		return err
	}
	metadata := s.stageMetadataPath(dir, stage.Stage.StageID)
	if err := trustedPathRemove(metadata); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return syncExternalDir(filepath.Dir(metadata))
}

func (s *ExternalFileStore) removeOwnedExternalPath(stage externalStageDisk, target string, backup bool) error {
	if !externalExists(target) {
		return nil
	}
	validate := func() error {
		if stage.Kind == port.ExternalFileKindContainer {
			if err := ensureOwnedExternalPath(target, false); err != nil {
				return err
			}
			contents, err := trustedPathReadFile(target)
			if err != nil {
				return err
			}
			expected := protocolcommon.Digest(stage.Stage.CandidateProviderVersion)
			if backup {
				expected = protocolcommon.Digest(stage.ExpectedProviderVersion)
			}
			if externalDigestBytes(contents) != expected {
				return port.ErrConflict
			}
			return nil
		}
		return s.validateOwnedProjectTree(stage, target, backup)
	}
	if err := validate(); err != nil {
		return err
	}
	// Path-based mutation cannot eliminate a hostile replacement between this
	// final validation and remove. We deliberately revalidate immediately here;
	// a future descriptor-relative adapter may close that residual local race.
	if err := validate(); err != nil {
		return err
	}
	if stage.Kind == port.ExternalFileKindContainer {
		if err := trustedPathRemove(target); err != nil {
			return err
		}
	} else if err := trustedPathRemoveAll(target); err != nil {
		return err
	}
	return syncExternalDir(filepath.Dir(target))
}

func (s *ExternalFileStore) validateOwnedProjectTree(stage externalStageDisk, root string, backup bool) error {
	if err := ensureOwnedExternalPath(root, true); err != nil {
		return err
	}
	files := make([]port.ExternalProjectFile, 0)
	entries := 0
	markerSeen := false
	var total int64
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		entries++
		if entries > s.maxEntries || entry.Type()&os.ModeSymlink != 0 {
			return port.ErrConflict
		}
		if current == root || entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == externalOwnerMarker {
			contents, err := os.ReadFile(current)
			if err != nil || string(contents) != externalOwner(stage.Stage.StageID, stage.Stage.MaterializationDigest) {
				return port.ErrConflict
			}
			markerSeen = true
			return nil
		}
		if filepath.Ext(relative) != ".ldl" || len(files) >= s.maxFiles {
			return port.ErrConflict
		}
		contents, err := os.ReadFile(current)
		if err != nil || int64(len(contents)) > s.maxBytes-total {
			return port.ErrConflict
		}
		if !backup {
			expected, ok := stage.DesiredDigests[relative]
			if !ok || externalDigestBytes(contents) != expected {
				return port.ErrConflict
			}
		}
		files = append(files, port.ExternalProjectFile{Path: relative, Contents: contents})
		total += int64(len(contents))
		return nil
	})
	if err != nil || !markerSeen {
		if err != nil {
			return err
		}
		return port.ErrConflict
	}
	if backup {
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		if externalProjectDigest(files) != protocolcommon.Digest(stage.ExpectedProviderVersion) {
			return port.ErrConflict
		}
	}
	return nil
}

func (s *ExternalFileStore) currentVersion(binding externalBindingDisk) (runtimeprotocol.ProviderVersionToken, []string, error) {
	if binding.Kind == port.ExternalFileKindContainer {
		contents, err := trustedPathReadFile(binding.Locator)
		if err != nil {
			return "", nil, err
		}
		digest := externalDigestBytes(contents)
		return runtimeprotocol.ProviderVersionToken(digest), nil, nil
	}
	files, paths, err := s.readManagedProject(binding.Locator)
	if err != nil {
		return "", nil, err
	}
	digest := externalProjectDigest(files)
	return runtimeprotocol.ProviderVersionToken(digest), paths, nil
}

func (s *ExternalFileStore) normalize(value port.ExternalMaterialization) (port.ExternalMaterialization, []string, protocolcommon.Digest, error) {
	if value.Kind == port.ExternalFileKindContainer {
		if len(value.Container) == 0 || int64(len(value.Container)) > s.maxBytes || len(value.ProjectFiles) != 0 {
			return port.ExternalMaterialization{}, nil, "", port.ErrConflict
		}
		copy := append([]byte(nil), value.Container...)
		return port.ExternalMaterialization{Kind: value.Kind, Container: copy}, nil, externalDigestBytes(copy), nil
	}
	if value.Kind != port.ExternalFileKindProject || len(value.ProjectFiles) == 0 || len(value.ProjectFiles) > s.maxFiles || len(value.Container) != 0 {
		return port.ExternalMaterialization{}, nil, "", port.ErrConflict
	}
	files := append([]port.ExternalProjectFile(nil), value.ProjectFiles...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	paths := make([]string, len(files))
	var total int64
	for index := range files {
		path, err := cleanManagedPath(files[index].Path)
		if err != nil || (index > 0 && path == paths[index-1]) || int64(len(files[index].Contents)) > s.maxBytes-total {
			return port.ExternalMaterialization{}, nil, "", port.ErrConflict
		}
		files[index].Path = path
		files[index].Contents = append([]byte(nil), files[index].Contents...)
		paths[index] = path
		total += int64(len(files[index].Contents))
	}
	return port.ExternalMaterialization{Kind: value.Kind, ProjectFiles: files}, paths, externalProjectDigest(files), nil
}

func (s *ExternalFileStore) readManagedProject(root string) ([]port.ExternalProjectFile, []string, error) {
	files := []port.ExternalProjectFile{}
	entries := 0
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		entries++
		if entries > s.maxEntries {
			return port.ErrConflict
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return port.ErrConflict
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" || strings.HasPrefix(name, ".layerdraw-external-") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(entry.Name()) != ".ldl" {
			return nil
		}
		if len(files) >= s.maxFiles {
			return port.ErrConflict
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > s.maxBytes-total {
			return port.ErrConflict
		}
		contents, err := os.ReadFile(path)
		if err != nil || int64(len(contents)) != info.Size() {
			return port.ErrIndeterminate
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, port.ExternalProjectFile{Path: filepath.ToSlash(relative), Contents: contents})
		total += int64(len(contents))
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	paths := make([]string, len(files))
	for index := range files {
		paths[index] = files[index].Path
	}
	return files, paths, nil
}

func (s *ExternalFileStore) writeExternalStage(path string, value port.ExternalMaterialization, owner string) error {
	if err := requireExternalAbsent(path); err != nil {
		return err
	}
	if value.Kind == port.ExternalFileKindContainer {
		file, err := trustedPathOpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if _, err = file.Write(value.Container); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		return syncExternalDir(filepath.Dir(path))
	}
	if err := trustedPathMkdir(path, 0o700); err != nil {
		return err
	}
	directories := map[string]struct{}{path: {}}
	for _, file := range value.ProjectFiles {
		target := filepath.Join(path, filepath.FromSlash(file.Path))
		parent := filepath.Dir(target)
		if err := ensureExternalManagedParent(path, target, true); err != nil {
			return err
		}
		if err := mkdirExternalManagedParents(path, target); err != nil {
			return err
		}
		if err := ensureExternalManagedParent(path, target, false); err != nil {
			return err
		}
		for current := parent; ; current = filepath.Dir(current) {
			directories[current] = struct{}{}
			if current == path {
				break
			}
		}
		f, err := trustedPathOpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(file.Contents)
		if writeErr == nil {
			writeErr = f.Sync()
		}
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		if s.fault != nil {
			if err := s.fault("project_stage_file_synced"); err != nil {
				return err
			}
		}
	}
	marker := filepath.Join(path, externalOwnerMarker)
	markerFile, err := trustedPathOpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, markerErr := markerFile.WriteString(owner)
	if markerErr == nil {
		markerErr = markerFile.Sync()
	}
	closeErr := markerFile.Close()
	if markerErr != nil {
		return markerErr
	}
	if closeErr != nil {
		return closeErr
	}
	ordered := make([]string, 0, len(directories))
	for directory := range directories {
		ordered = append(ordered, directory)
	}
	sort.Slice(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	for _, directory := range ordered {
		if err := syncExternalDir(directory); err != nil {
			return err
		}
	}
	if err := syncExternalDir(filepath.Dir(path)); err != nil {
		return err
	}
	if s.fault != nil {
		if err := s.fault("project_stage_durable"); err != nil {
			return err
		}
	}
	return nil
}

func (s *ExternalFileStore) publishProject(binding externalBindingDisk, stage externalStageDisk) error {
	if err := ensureExternalRoot(binding.Locator, true); err != nil {
		return err
	}
	if err := ensureOwnedExternalPath(stage.StagedPath, true); err != nil {
		return err
	}
	if externalExists(stage.BackupPath) {
		if err := ensureOwnedExternalPath(stage.BackupPath, true); err != nil {
			return err
		}
		if err := validateExternalOwnerMarker(stage.BackupPath, externalOwner(stage.Stage.StageID, stage.Stage.MaterializationDigest)); err != nil {
			return err
		}
	} else {
		if err := requireExternalAbsent(stage.BackupPath); err != nil {
			return err
		}
		if err := ensureExternalRoot(filepath.Dir(stage.BackupPath), true); err != nil {
			return err
		}
		if err := trustedPathMkdir(stage.BackupPath, 0o700); err != nil {
			return err
		}
		if err := writeExternalOwnerMarker(stage.BackupPath, externalOwner(stage.Stage.StageID, stage.Stage.MaterializationDigest)); err != nil {
			return err
		}
	}
	if err := ensureOwnedExternalPath(stage.BackupPath, true); err != nil {
		return err
	}
	if err := syncExternalDir(filepath.Dir(stage.BackupPath)); err != nil {
		return err
	}
	desired := make(map[string]bool, len(stage.DesiredPaths))
	for _, path := range stage.DesiredPaths {
		desired[path] = true
	}
	for _, path := range stage.PriorPaths {
		target := filepath.Join(binding.Locator, filepath.FromSlash(path))
		backup := filepath.Join(stage.BackupPath, filepath.FromSlash(path))
		if externalExists(target) && !externalExists(backup) {
			if err := ensureExternalManagedFile(binding.Locator, target); err != nil {
				return err
			}
			if err := mkdirExternalManagedParents(stage.BackupPath, backup); err != nil {
				return err
			}
			if err := ensureExternalManagedParent(stage.BackupPath, backup, false); err != nil {
				return err
			}
			if err := ensureExternalManagedFile(binding.Locator, target); err != nil {
				return err
			}
			if err := requireExternalAbsent(backup); err != nil {
				return err
			}
			if err := trustedPathRename(target, backup); err != nil {
				return err
			}
			if err := syncExternalRename(target, backup); err != nil {
				return err
			}
		}
	}
	if s.fault != nil {
		if err := s.fault("project_after_backup"); err != nil {
			return err
		}
	}
	for path := range desired {
		target := filepath.Join(binding.Locator, filepath.FromSlash(path))
		staged := filepath.Join(stage.StagedPath, filepath.FromSlash(path))
		if externalExists(staged) {
			if err := ensureExternalManagedFile(stage.StagedPath, staged); err != nil {
				return err
			}
			if err := ensureExternalManagedParent(binding.Locator, target, true); err != nil {
				return err
			}
			if err := mkdirExternalManagedParents(binding.Locator, target); err != nil {
				return err
			}
			if err := ensureExternalManagedParent(binding.Locator, target, false); err != nil {
				return err
			}
			if externalExists(target) {
				return port.ErrConflict
			}
			if err := ensureExternalManagedFile(stage.StagedPath, staged); err != nil {
				return err
			}
			if err := requireExternalAbsent(target); err != nil {
				return err
			}
			if err := trustedPathRename(staged, target); err != nil {
				return err
			}
			if err := syncExternalRename(staged, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *ExternalFileStore) publishContainer(binding externalBindingDisk, stage externalStageDisk) error {
	if err := ensureExternalRoot(filepath.Dir(binding.Locator), true); err != nil {
		return err
	}
	if !externalExists(stage.BackupPath) && externalExists(binding.Locator) {
		if err := ensureOwnedExternalPath(binding.Locator, false); err != nil {
			return err
		}
		if err := requireExternalAbsent(stage.BackupPath); err != nil {
			return err
		}
		if err := trustedPathRename(binding.Locator, stage.BackupPath); err != nil {
			return err
		}
		if err := syncExternalRename(binding.Locator, stage.BackupPath); err != nil {
			return err
		}
	}
	if s.fault != nil {
		if err := s.fault("container_after_backup"); err != nil {
			return err
		}
	}
	if externalExists(stage.StagedPath) {
		if err := ensureOwnedExternalPath(stage.StagedPath, false); err != nil {
			return err
		}
		if err := requireExternalAbsent(binding.Locator); err != nil {
			return err
		}
		if err := trustedPathRename(stage.StagedPath, binding.Locator); err != nil {
			return err
		}
		return syncExternalRename(stage.StagedPath, binding.Locator)
	}
	return nil
}

func canonicalExternalLocator(kind port.ExternalFileKind, path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	// The locator is an explicit native-picker capability. Keep all filesystem
	// access behind the audited trusted-path boundary after canonicalization.
	info, err := trustedPathLstat(real)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return "", port.ErrConflict
	}
	if (kind == port.ExternalFileKindProject && !info.IsDir()) || (kind == port.ExternalFileKindContainer && !info.Mode().IsRegular()) {
		return "", port.ErrConflict
	}
	if kind != port.ExternalFileKindProject && kind != port.ExternalFileKindContainer {
		return "", port.ErrConflict
	}
	return filepath.Clean(real), nil
}

func externalSiblingPaths(binding externalBindingDisk, stageID string) (string, string) {
	parent := filepath.Dir(binding.Locator)
	base := ".layerdraw-external-" + stageID
	if binding.Kind == port.ExternalFileKindProject {
		return filepath.Join(parent, base+".stage"), filepath.Join(parent, base+".backup")
	}
	return filepath.Join(parent, base+".stage"), filepath.Join(parent, base+".backup")
}

func cleanManagedPath(value string) (string, error) {
	if !strings.HasSuffix(value, ".ldl") {
		return "", port.ErrConflict
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == "." || !filepath.IsLocal(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", port.ErrConflict
	}
	return filepath.ToSlash(clean), nil
}

func externalIdentity(operation runtimeprotocol.OperationID, key runtimeprotocol.IdempotencyKey) string {
	sum := sha256.Sum256([]byte(string(operation) + "\x00" + string(key)))
	return hex.EncodeToString(sum[:16])
}

func externalOwner(stageID string, digest protocolcommon.Digest) string {
	// This marker is an ownership/collision guard, not an authentication token.
	// Cleanup also verifies the recorded type, bounded tree shape, and exact
	// content digests; a missing or merely copied marker is never sufficient.
	return stageID + "\n" + string(digest) + "\n"
}

func externalFileDigests(files []port.ExternalProjectFile) map[string]protocolcommon.Digest {
	if len(files) == 0 {
		return nil
	}
	result := make(map[string]protocolcommon.Digest, len(files))
	for _, file := range files {
		result[file.Path] = externalDigestBytes(file.Contents)
	}
	return result
}

func writeExternalOwnerMarker(root, owner string) error {
	marker := filepath.Join(root, externalOwnerMarker)
	file, err := trustedPathOpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(owner)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return syncExternalDir(root)
}

func validateExternalOwnerMarker(root, owner string) error {
	marker := filepath.Join(root, externalOwnerMarker)
	if err := ensureExternalManagedFile(root, marker); err != nil {
		return err
	}
	contents, err := trustedPathReadFile(marker)
	if err != nil || string(contents) != owner {
		return port.ErrConflict
	}
	return nil
}

func externalProjectDigest(files []port.ExternalProjectFile) protocolcommon.Digest {
	projection := make([]struct {
		Path     string `json:"path"`
		Contents []byte `json:"contents"`
	}, len(files))
	for index, file := range files {
		projection[index] = struct {
			Path     string `json:"path"`
			Contents []byte `json:"contents"`
		}{file.Path, file.Contents}
	}
	return externalDigest(projection)
}

func externalDigest(value any) protocolcommon.Digest {
	encoded, _ := json.Marshal(value)
	return externalDigestBytes(encoded)
}

func externalDigestBytes(value []byte) protocolcommon.Digest {
	digest := sha256.Sum256(value)
	return protocolcommon.Digest("sha256:" + hex.EncodeToString(digest[:]))
}

func externalExists(path string) bool {
	_, err := trustedPathLstat(path)
	return err == nil
}

func externalMissing(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, port.ErrNotFound)
}

func requireExternalAbsent(path string) error {
	_, err := trustedPathLstat(path)
	if err == nil {
		return port.ErrConflict
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func ensureExternalRoot(root string, wantDirectory bool) error {
	clean := filepath.Clean(root)
	real, err := filepath.EvalSymlinks(clean)
	if err != nil || real != clean {
		return port.ErrConflict
	}
	info, err := trustedPathLstat(clean)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return port.ErrConflict
	}
	if wantDirectory && !info.IsDir() {
		return port.ErrConflict
	}
	if !wantDirectory && !info.Mode().IsRegular() {
		return port.ErrConflict
	}
	return nil
}

func ensureExternalManagedParent(root, target string, allowMissing bool) error {
	if err := ensureExternalRoot(root, true); err != nil {
		return err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return port.ErrConflict
	}
	current := root
	parent := filepath.Dir(relative)
	if parent == "." {
		return nil
	}
	for _, part := range strings.Split(parent, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := trustedPathLstat(current)
		if errors.Is(statErr, fs.ErrNotExist) && allowMissing {
			continue
		}
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return port.ErrConflict
		}
	}
	return nil
}

func ensureExternalManagedFile(root, target string) error {
	if err := ensureExternalManagedParent(root, target, false); err != nil {
		return err
	}
	info, err := trustedPathLstat(target)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return port.ErrConflict
	}
	return nil
}

func mkdirExternalManagedParents(root, target string) error {
	if err := ensureExternalRoot(root, true); err != nil {
		return err
	}
	relative, err := filepath.Rel(root, filepath.Dir(target))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return port.ErrConflict
	}
	if relative == "." {
		return nil
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		parent := current
		current = filepath.Join(current, part)
		info, statErr := trustedPathLstat(current)
		if errors.Is(statErr, fs.ErrNotExist) {
			if err := trustedPathMkdir(current, 0o700); err != nil {
				return err
			}
			if err := syncExternalDir(parent); err != nil {
				return err
			}
			info, statErr = trustedPathLstat(current)
		}
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return port.ErrConflict
		}
	}
	return nil
}

func ensureOwnedExternalPath(path string, wantDirectory bool) error {
	parent := filepath.Dir(path)
	if err := ensureExternalRoot(parent, true); err != nil {
		return err
	}
	info, err := trustedPathLstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return port.ErrConflict
	}
	if wantDirectory && !info.IsDir() {
		return port.ErrConflict
	}
	if !wantDirectory && !info.Mode().IsRegular() {
		return port.ErrConflict
	}
	return nil
}

func syncExternalRename(source, destination string) error {
	if err := syncExternalDir(filepath.Dir(source)); err != nil {
		return err
	}
	if filepath.Dir(destination) != filepath.Dir(source) {
		return syncExternalDir(filepath.Dir(destination))
	}
	return nil
}

func syncExternalDir(path string) error {
	directory, err := trustedPathOpen(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return privatefs.SyncDirectory(directory)
}

var _ port.ExternalFileStore = (*ExternalFileStore)(nil)
