// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type documentHeadDisk struct {
	Head port.DocumentHead `json:"head"`
}

type stagedRevisionDisk struct {
	Scope runtimeprotocol.RuntimeScope `json:"scope"`
	Stage port.StagedRevision          `json:"stage"`
	Input port.StageRevisionInput      `json:"input"`
}

type revisionDisk struct {
	Snapshot port.RevisionSnapshot `json:"snapshot"`
}

func (s *Document) GetHead(ctx context.Context, in port.GetDocumentHeadInput) (port.DocumentHead, error) {
	if err := ctx.Err(); err != nil {
		return port.DocumentHead{}, err
	}
	dir, err := s.scopeDir(in.Scope)
	if err != nil {
		return port.DocumentHead{}, err
	}
	var disk documentHeadDisk
	if err := s.readJSON(filepath.Join(dir, "documents", "head.json"), &disk); err != nil {
		return port.DocumentHead{}, err
	}
	if !validDocumentHeadDisk(in.Scope, disk.Head) {
		return port.DocumentHead{}, invalidPersisted("document head")
	}
	return clone(disk.Head)
}

func (s *Document) ReadRevision(ctx context.Context, in port.ReadRevisionInput) (port.RevisionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return port.RevisionSnapshot{}, err
	}
	id, err := safeID(string(in.RevisionID))
	if err != nil {
		return port.RevisionSnapshot{}, err
	}
	dir, err := s.scopeDir(in.Scope)
	if err != nil {
		return port.RevisionSnapshot{}, err
	}
	var disk revisionDisk
	if err := s.readJSON(filepath.Join(dir, "documents", "revisions", id+".json"), &disk); err != nil {
		return port.RevisionSnapshot{}, err
	}
	if !validRevisionSnapshotDisk(in.Scope, disk.Snapshot) {
		return port.RevisionSnapshot{}, invalidPersisted("document revision")
	}
	if disk.Snapshot.Revision.RevisionID != in.RevisionID || disk.Snapshot.Revision.DocumentID != in.Scope.DocumentID {
		return port.RevisionSnapshot{}, fmt.Errorf("revision identity mismatch: %w", port.ErrIndeterminate)
	}
	return clone(disk.Snapshot)
}

func (s *Document) ReadSourceBlobs(ctx context.Context, in port.ReadSourceBlobsInput) (port.SourceBlobSet, error) {
	if err := ctx.Err(); err != nil {
		return port.SourceBlobSet{}, err
	}
	snapshot, err := s.ReadRevision(ctx, port.ReadRevisionInput{Scope: in.Scope, RevisionID: in.Revision.RevisionID})
	if err != nil {
		return port.SourceBlobSet{}, err
	}
	if !reflect.DeepEqual(snapshot.Revision, in.Revision) {
		return port.SourceBlobSet{}, port.ErrConflict
	}
	allowed := make(map[string]protocolcommon.BlobRef, len(snapshot.SourceBlobs))
	for _, ref := range snapshot.SourceBlobs {
		allowed[ref.BlobID] = ref
	}
	dir, _ := s.scopeDir(in.Scope)
	out := port.SourceBlobSet{Revision: in.Revision, Blobs: make([]port.SourceBlob, 0, len(in.Blobs))}
	seen := map[string]bool{}
	for _, ref := range in.Blobs {
		if seen[ref.BlobID] || !reflect.DeepEqual(allowed[ref.BlobID], ref) {
			return port.SourceBlobSet{}, port.ErrConflict
		}
		seen[ref.BlobID] = true
		name, err := safeID(string(ref.Digest))
		if err != nil {
			return port.SourceBlobSet{}, err
		}
		blobPath := filepath.Join(dir, "documents", "blobs", name)
		expectedSize, sizeErr := parseUint(ref.Size)
		if sizeErr != nil || expectedSize >= math.MaxInt64 {
			return port.SourceBlobSet{}, port.ErrIndeterminate
		}
		expectedSize64 := int64(expectedSize)
		data, err := s.readValidatedFile(blobPath, expectedSize64)
		if err != nil {
			return port.SourceBlobSet{}, classify(err)
		}
		size, err := parseUint(ref.Size)
		if err != nil || uint64(len(data)) != size || digestBytes(data) != ref.Digest {
			return port.SourceBlobSet{}, fmt.Errorf("corrupt source blob: %w", port.ErrIndeterminate)
		}
		out.Blobs = append(out.Blobs, port.SourceBlob{Ref: ref, Contents: bytes.Clone(data)})
	}
	return out, nil
}

func validateSourceSet(in port.StageRevisionInput) error {
	if _, err := runtimeprotocol.EncodeRuntimeScope(in.Scope); err != nil {
		return port.ErrConflict
	}
	if _, err := safeID(string(in.OperationID)); err != nil {
		return port.ErrConflict
	}
	if _, err := safeID(string(in.IdempotencyKey)); err != nil {
		return port.ErrConflict
	}
	if !validRevision(in.Scope, in.BaseRevision) {
		return port.ErrConflict
	}
	if !reflect.DeepEqual(in.SourceBlobs.Revision, in.BaseRevision) {
		return port.ErrConflict
	}
	seen := map[string]bool{}
	for _, blob := range in.SourceBlobs.Blobs {
		if seen[blob.Ref.BlobID] || !validBlobRef(blob.Ref) {
			return port.ErrConflict
		}
		seen[blob.Ref.BlobID] = true
		size, err := parseUint(blob.Ref.Size)
		if err != nil || uint64(len(blob.Contents)) != size || digestBytes(blob.Contents) != blob.Ref.Digest {
			return port.ErrConflict
		}
	}
	if !validDigest(in.DefinitionHash) || !validDigest(in.GraphHash) || !validBlobRef(in.Manifest) {
		return port.ErrConflict
	}
	return nil
}

func expectedStagedRevision(in port.StageRevisionInput) (port.StagedRevision, error) {
	encoded, err := json.Marshal(in)
	if err != nil {
		return port.StagedRevision{}, err
	}
	d := digestBytes(encoded)
	revisionID := runtimeprotocol.RevisionID("rev_" + string(d)[7:39])
	return port.StagedRevision{StageID: "stage_" + string(d)[7:55], Revision: runtimeprotocol.CommittedRevisionRef{DocumentID: in.Scope.DocumentID, RevisionID: revisionID, DefinitionHash: in.DefinitionHash, GraphHash: in.GraphHash}, StagedDigest: d}, nil
}

func (s *Document) StageRevision(ctx context.Context, in port.StageRevisionInput) (port.StagedRevision, error) {
	if err := ctx.Err(); err != nil {
		return port.StagedRevision{}, err
	}
	cloned, cloneErr := clone(in)
	if cloneErr != nil {
		return port.StagedRevision{}, cloneErr
	}
	in = cloned
	if err := validateSourceSet(in); err != nil {
		return port.StagedRevision{}, err
	}
	var result port.StagedRevision
	err := s.withLock(in.Scope, func(dir string) error {
		stage, stageErr := expectedStagedRevision(in)
		if stageErr != nil {
			return stageErr
		}
		result = stage
		stageID := result.StageID
		id, _ := safeID(stageID)
		stageDir := filepath.Join(dir, "documents", "staged", id)
		var existing stagedRevisionDisk
		if err := s.readJSON(filepath.Join(stageDir, "record.json"), &existing); err == nil {
			expected, expectedErr := expectedStagedRevision(existing.Input)
			if !reflect.DeepEqual(existing.Scope, in.Scope) || expectedErr != nil || !reflect.DeepEqual(existing.Stage, expected) || validateSourceSet(existing.Input) != nil {
				return invalidPersisted("staged revision")
			}
			if reflect.DeepEqual(existing.Input, in) {
				result = existing.Stage
				return nil
			}
			return port.ErrConflict
		} else if !errors.Is(err, port.ErrNotFound) {
			return err
		}
		for _, blob := range in.SourceBlobs.Blobs {
			name, _ := safeID(string(blob.Ref.Digest))
			if err := s.atomicWrite(filepath.Join(stageDir, "blobs", name), bytes.NewReader(blob.Contents), int64(len(blob.Contents))); err != nil {
				return err
			}
		}
		return s.writeJSON(filepath.Join(stageDir, "record.json"), stagedRevisionDisk{Scope: in.Scope, Stage: result, Input: in})
	})
	return result, err
}

func (s *Document) PublishHead(ctx context.Context, in port.PublishDocumentHeadInput) (port.PublishHeadResult, error) {
	if err := ctx.Err(); err != nil {
		return port.PublishHeadResult{}, err
	}
	var result port.PublishHeadResult
	err := s.withLock(in.Scope, func(dir string) error {
		id, err := safeID(in.StageID)
		if err != nil {
			return err
		}
		stageDir := filepath.Join(dir, "documents", "staged", id)
		var staged stagedRevisionDisk
		if err := s.readJSON(filepath.Join(stageDir, "record.json"), &staged); err != nil {
			return err
		}
		expectedStage, expectedErr := expectedStagedRevision(staged.Input)
		if !reflect.DeepEqual(staged.Scope, in.Scope) || expectedErr != nil || !reflect.DeepEqual(staged.Stage, expectedStage) || staged.Stage.StageID != in.StageID || validateSourceSet(staged.Input) != nil {
			return port.ErrConflict
		}
		var current documentHeadDisk
		if err := s.readJSON(filepath.Join(dir, "documents", "head.json"), &current); err != nil {
			return err
		}
		if !validDocumentHeadDisk(in.Scope, current.Head) {
			return invalidPersisted("document head")
		}
		if current.Head.Revision.RevisionID != in.ExpectedRevision || current.Head.Revision.DefinitionHash != in.ExpectedDefinitionHash || current.Head.ProviderVersion != in.ExpectedProviderVersion || current.Head.FencingToken != in.FencingToken {
			return port.ErrConflict
		}
		version, err := strconv.ParseUint(string(current.Head.ProviderVersion), 10, 64)
		if err != nil {
			return port.ErrIndeterminate
		}
		provider := runtimeprotocol.ProviderVersionToken(strconv.FormatUint(version+1, 10))
		revision := staged.Stage.Revision
		revision.ProviderVersion = &provider
		for _, blob := range staged.Input.SourceBlobs.Blobs {
			name, _ := safeID(string(blob.Ref.Digest))
			dest := filepath.Join(dir, "documents", "blobs", name)
			if err := s.ensureDir(filepath.Dir(dest)); err != nil {
				return err
			}
			if err := s.validateParents(dest); err != nil {
				return err
			}
			if info, err := os.Lstat(dest); err == nil {
				if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
					return port.ErrIndeterminate
				}
				existing, readErr := s.readValidatedFile(dest, int64(len(blob.Contents)))
				if readErr != nil || digestBytes(existing) != blob.Ref.Digest {
					return port.ErrIndeterminate
				}
			} else if errors.Is(err, fs.ErrNotExist) {
				if err := s.atomicWrite(dest, bytes.NewReader(blob.Contents), int64(len(blob.Contents))); err != nil {
					return err
				}
			} else {
				return classify(err)
			}
		}
		refs := make([]protocolcommon.BlobRef, len(staged.Input.SourceBlobs.Blobs))
		for i := range refs {
			refs[i] = staged.Input.SourceBlobs.Blobs[i].Ref
		}
		revID, _ := safeID(string(revision.RevisionID))
		revPath := filepath.Join(dir, "documents", "revisions", revID+".json")
		candidate := revisionDisk{Snapshot: port.RevisionSnapshot{Revision: revision, SourceBlobs: refs, Manifest: staged.Input.Manifest}}
		var existing revisionDisk
		if err := s.readJSON(revPath, &existing); err == nil {
			if !reflect.DeepEqual(existing, candidate) {
				return port.ErrConflict
			}
		} else if !errors.Is(err, port.ErrNotFound) {
			return err
		} else if err := s.writeJSON(revPath, candidate); err != nil {
			return err
		}
		newHead := port.DocumentHead{Revision: revision, ProviderVersion: provider, FencingToken: current.Head.FencingToken}
		if err := s.writeJSON(filepath.Join(dir, "documents", "head.json"), documentHeadDisk{Head: newHead}); err != nil {
			return fmt.Errorf("publish head: %w", port.ErrIndeterminate)
		}
		result = port.PublishHeadResult{Published: true, Revision: revision, ProviderVersion: provider}
		return nil
	})
	return result, err
}

func (s *Document) AbortStagedRevision(ctx context.Context, in port.AbortStagedRevisionInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.withLock(in.Scope, func(dir string) error {
		id, err := safeID(in.StageID)
		if err != nil {
			return err
		}
		stageDir := filepath.Join(dir, "documents", "staged", id)
		if err := s.validateParents(stageDir); err != nil {
			return err
		}
		info, err := os.Lstat(stageDir)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return classify(err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != dirMode {
			return port.ErrConflict
		}
		if err = os.RemoveAll(stageDir); err != nil {
			return classify(err)
		}
		return s.syncDirAfterMutation(filepath.Dir(stageDir))
	})
}

// InitializeDocument installs the first immutable revision and head. It is a
// host bootstrap helper, not part of the Runtime port.
func (s *Document) InitializeDocument(ctx context.Context, scope runtimeprotocol.RuntimeScope, snapshot port.RevisionSnapshot, provider runtimeprotocol.ProviderVersionToken, fencing protocolcommon.CanonicalUint64, blobs []port.SourceBlob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	clonedSnapshot, cloneErr := clone(snapshot)
	if cloneErr != nil {
		return cloneErr
	}
	clonedBlobs, cloneErr := clone(blobs)
	if cloneErr != nil {
		return cloneErr
	}
	snapshot, blobs = clonedSnapshot, clonedBlobs
	return s.withLock(scope, func(dir string) error {
		if !validDocumentHeadDisk(scope, port.DocumentHead{Revision: snapshot.Revision, ProviderVersion: provider, FencingToken: fencing}) || !validRevisionSnapshotDisk(scope, snapshot) {
			return port.ErrConflict
		}
		headPath := filepath.Join(dir, "documents", "head.json")
		if err := s.ensureDir(filepath.Dir(headPath)); err != nil {
			return err
		}
		if err := s.validateParents(headPath); err != nil {
			return err
		}
		if _, err := os.Lstat(headPath); err == nil {
			return port.ErrConflict
		} else if !errors.Is(err, fs.ErrNotExist) {
			return classify(err)
		}
		wanted := map[string]protocolcommon.BlobRef{}
		for _, ref := range snapshot.SourceBlobs {
			if _, ok := wanted[ref.BlobID]; ok {
				return port.ErrConflict
			}
			wanted[ref.BlobID] = ref
		}
		seen := map[string]bool{}
		for _, blob := range blobs {
			if seen[blob.Ref.BlobID] || !reflect.DeepEqual(wanted[blob.Ref.BlobID], blob.Ref) {
				return port.ErrConflict
			}
			seen[blob.Ref.BlobID] = true
			size, err := parseUint(blob.Ref.Size)
			if err != nil || uint64(len(blob.Contents)) != size || digestBytes(blob.Contents) != blob.Ref.Digest {
				return port.ErrConflict
			}
			n, _ := safeID(string(blob.Ref.Digest))
			if err := s.atomicWrite(filepath.Join(dir, "documents", "blobs", n), bytes.NewReader(blob.Contents), int64(len(blob.Contents))); err != nil {
				return err
			}
		}
		if len(seen) != len(wanted) {
			return port.ErrConflict
		}
		rid, _ := safeID(string(snapshot.Revision.RevisionID))
		if err := s.writeJSON(filepath.Join(dir, "documents", "revisions", rid+".json"), revisionDisk{Snapshot: snapshot}); err != nil {
			return err
		}
		return s.writeJSON(filepath.Join(dir, "documents", "head.json"), documentHeadDisk{Head: port.DocumentHead{Revision: snapshot.Revision, ProviderVersion: provider, FencingToken: fencing}})
	})
}
