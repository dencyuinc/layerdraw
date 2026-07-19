// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package local

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

// StagedInspection is bounded, read-only recovery evidence. It deliberately
// exposes provider-neutral stage input rather than local paths.
type StagedInspection struct {
	Stage port.StagedRevision
	Input port.StageRevisionInput
}

// RecoveryInspection contains the durable journal record and the published
// revision evidence retained by the journal.
type RecoveryInspection struct {
	Record            port.RecoveryRecord
	PublishedRevision *runtimeprotocol.CommittedRevisionRef
}

// ListStaged returns at most limit validated staged revisions in stable order.
func (s *Document) ListStaged(ctx context.Context, scope runtimeprotocol.RuntimeScope, limit int) ([]StagedInspection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, port.ErrConflict
	}
	dir, err := s.scopeDir(scope)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(dir, "documents", "staged")
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return []StagedInspection{}, nil
	}
	if err != nil {
		return nil, classify(err)
	}
	if len(entries) > limit {
		return nil, port.ErrConflict
	}
	result := make([]StagedInspection, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, invalidPersisted("staged revision directory")
		}
		var disk stagedRevisionDisk
		if err := s.readJSON(filepath.Join(root, entry.Name(), "record.json"), &disk); err != nil {
			return nil, err
		}
		expected, expectedErr := expectedStagedRevision(disk.Input)
		if expectedErr != nil || disk.Scope.DocumentID != scope.DocumentID || !reflect.DeepEqual(disk.Stage, expected) || validateSourceSet(disk.Input) != nil {
			return nil, invalidPersisted("staged revision")
		}
		result = append(result, StagedInspection{Stage: disk.Stage, Input: disk.Input})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Stage.StageID < result[j].Stage.StageID })
	return result, nil
}

// List returns at most limit validated records that still require automatic
// recovery or review. Final records remain durable and addressable through Get
// for idempotent result lookup, but do not consume the recovery-work bound.
func (s *Recovery) List(ctx context.Context, scope runtimeprotocol.RuntimeScope, limit int) ([]RecoveryInspection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, port.ErrConflict
	}
	disk, err := s.loadRecovery(scope)
	if err != nil {
		return nil, err
	}
	result := make([]RecoveryInspection, 0, len(disk.Records))
	for _, entry := range disk.Records {
		if !validRecoveryEntry(scope, entry) {
			return nil, invalidPersisted("recovery record")
		}
		if entry.Record.Status.Phase == runtimeprotocol.RecoveryPhaseFinal {
			continue
		}
		if len(result) == limit {
			return nil, port.ErrConflict
		}
		var published *runtimeprotocol.CommittedRevisionRef
		if entry.PublishedRevision != nil {
			value := *entry.PublishedRevision
			published = &value
		}
		result = append(result, RecoveryInspection{Record: entry.Record, PublishedRevision: published})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Record.Status.OperationID < result[j].Record.Status.OperationID })
	return result, nil
}
