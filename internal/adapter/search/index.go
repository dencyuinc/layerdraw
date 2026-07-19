// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type DurableIndexStore struct {
	root      string
	executor  port.QueryExecutionPort
	now       func() time.Time
	inspector port.PhysicalIndexInspector
	mu        sync.Mutex
}

type persistedIndex struct {
	Status        port.SearchIndexStatus `json:"status"`
	PayloadDigest string                 `json:"payload_digest"`
}

func NewDurableIndexStore(root string, executor port.QueryExecutionPort, now func() time.Time) (*DurableIndexStore, error) {
	inspector, ok := executor.(port.PhysicalIndexInspector)
	if root == "" || !filepath.IsAbs(root) || executor == nil || !ok {
		return nil, fmt.Errorf("invalid search index store configuration")
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("unsafe search index root")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, err
	}
	return &DurableIndexStore{root: filepath.Clean(root), executor: executor, inspector: inspector, now: now}, nil
}

func (s *DurableIndexStore) Describe(ctx context.Context, identity port.SearchIndexIdentity) (port.SearchIndexStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, err := identityKey(identity)
	if err != nil {
		return port.SearchIndexStatus{}, port.ErrConflict
	}
	for _, state := range []string{"active", "building"} {
		stored, readErr := s.read(filepath.Join(s.root, key+"."+state+".json"))
		if readErr == nil {
			if !reflect.DeepEqual(stored.Status.Identity, identity) {
				return port.SearchIndexStatus{}, port.ErrConflict
			}
			if state == "active" && (stored.Status.PhysicalIndex == nil || s.inspector.InspectPhysicalIndex(ctx, *stored.Status.PhysicalIndex) != nil) {
				return port.SearchIndexStatus{}, port.ErrNotFound
			}
			return stored.Status, nil
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			return port.SearchIndexStatus{}, readErr
		}
	}
	return port.SearchIndexStatus{}, port.ErrNotFound
}

func (s *DurableIndexStore) ApplyPlan(ctx context.Context, identity port.SearchIndexIdentity, plan port.ExecutionPlan) (port.SearchIndexApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if plan.Kind != port.PlanSearchIndex {
		return port.SearchIndexApplyResult{}, port.ErrConflict
	}
	key, err := identityKey(identity)
	if err != nil {
		return port.SearchIndexApplyResult{}, port.ErrConflict
	}
	digest := sha256.Sum256(plan.Payload)
	status := port.SearchIndexStatus{Identity: identity, State: "building", PlanID: plan.PlanID, UpdatedAt: s.now().UTC()}
	path := filepath.Join(s.root, key+".building.json")
	if err := s.write(path, persistedIndex{Status: status, PayloadDigest: hex.EncodeToString(digest[:])}); err != nil {
		return port.SearchIndexApplyResult{}, err
	}
	execution, err := s.executor.Execute(ctx, plan)
	if err != nil {
		return port.SearchIndexApplyResult{}, err
	}
	if execution.Truncated || !execution.Complete || execution.PhysicalIndex == nil || execution.PhysicalIndex.IdentityDigest != key || execution.PhysicalIndex.BackendVersion != identity.LadybugBackendVersion || execution.PhysicalIndex.ContentDigest == "" {
		return port.SearchIndexApplyResult{}, port.ErrConflict
	}
	if err := s.inspector.InspectPhysicalIndex(ctx, *execution.PhysicalIndex); err != nil {
		return port.SearchIndexApplyResult{}, port.ErrConflict
	}
	status.PhysicalIndex = execution.PhysicalIndex
	if err := s.write(path, persistedIndex{Status: status, PayloadDigest: hex.EncodeToString(digest[:])}); err != nil {
		return port.SearchIndexApplyResult{}, err
	}
	return port.SearchIndexApplyResult{Identity: identity, PlanID: plan.PlanID, PhysicalIndex: *execution.PhysicalIndex}, nil
}

func (s *DurableIndexStore) Activate(ctx context.Context, input port.SearchIndexApplyResult) (port.SearchIndexStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, err := identityKey(input.Identity)
	if err != nil {
		return port.SearchIndexStatus{}, port.ErrConflict
	}
	buildingPath := filepath.Join(s.root, key+".building.json")
	stored, err := s.read(buildingPath)
	if err != nil {
		return port.SearchIndexStatus{}, err
	}
	if stored.Status.PlanID != input.PlanID || !reflect.DeepEqual(stored.Status.Identity, input.Identity) || stored.Status.PhysicalIndex == nil || *stored.Status.PhysicalIndex != input.PhysicalIndex || s.inspector.InspectPhysicalIndex(ctx, input.PhysicalIndex) != nil {
		return port.SearchIndexStatus{}, port.ErrConflict
	}
	stored.Status.State = "active"
	stored.Status.UpdatedAt = s.now().UTC()
	activePath := filepath.Join(s.root, key+".active.json")
	if err := s.write(activePath, stored); err != nil {
		return port.SearchIndexStatus{}, err
	}
	if err := os.Remove(buildingPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return port.SearchIndexStatus{}, err
	}
	return stored.Status, nil
}

func (s *DurableIndexStore) Invalidate(_ context.Context, identity port.SearchIndexIdentity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, err := identityKey(identity)
	if err != nil {
		return port.ErrConflict
	}
	for _, state := range []string{"active", "building"} {
		if err := os.Remove(filepath.Join(s.root, key+"."+state+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func identityKey(identity port.SearchIndexIdentity) (string, error) {
	snapshot := identity.DocumentSnapshotRef
	validHost := snapshot.Kind == port.SnapshotHostRevision && snapshot.HostDocumentID != "" && snapshot.CommittedRevision != "" && snapshot.SourceTreeDigest == "" && snapshot.DocumentGeneration == 0
	validPortable := snapshot.Kind == port.SnapshotPortableGeneration && snapshot.HostDocumentID == "" && snapshot.CommittedRevision == "" && snapshot.SourceTreeDigest != ""
	if (!validHost && !validPortable) || snapshot.DefinitionHash == "" || identity.SearchProfileID == "" || identity.SearchProfileDigest == "" || identity.AccessProjectionDigest == "" || identity.LadybugBackendVersion == "" || identity.IndexSchemaVersion == "" {
		return "", fmt.Errorf("incomplete identity")
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (s *DurableIndexStore) read(path string) (persistedIndex, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return persistedIndex{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		return persistedIndex{}, port.ErrConflict
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return persistedIndex{}, err
	}
	var result persistedIndex
	if err := json.Unmarshal(data, &result); err != nil {
		return persistedIndex{}, port.ErrConflict
	}
	return result, nil
}

func (s *DurableIndexStore) write(path string, value persistedIndex) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(s.root, ".index-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
