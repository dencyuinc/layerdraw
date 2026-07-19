// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
package local

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func TestAssetSizeBeyondInt64FailsBeforeReaderUse(t *testing.T) {
	scope := testScope()
	store, err := NewAssetStore(t.TempDir(), Options{MaxAssetBytes: math.MaxUint64})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []uint64{uint64(math.MaxInt64), uint64(math.MaxInt64) + 1} {
		size := protocolcommon.CanonicalUint64(strconv.FormatUint(raw, 10))
		reader := &panicReader{}
		if _, err := store.PutIfAbsent(context.Background(), port.PutAssetInput{
			Scope: scope, ExpectedDigest: testDigest('1'), MediaType: "application/octet-stream", Size: size, Contents: reader,
		}); !errors.Is(err, port.ErrConflict) {
			t.Fatalf("put size %s=%v", size, err)
		}
		if _, err := store.openVerifiedAsset("unused", port.AssetMetadata{
			Digest: testDigest('1'), MediaType: "application/octet-stream", Size: size,
		}); !errors.Is(err, port.ErrIndeterminate) {
			t.Fatalf("open size %s=%v", size, err)
		}
	}
}

type panicReader struct{}

func (*panicReader) Read([]byte) (int, error) {
	panic("oversize input must be rejected before reading")
}

func TestDocumentSourceSizeBeyondInt64FailsBeforeFileRead(t *testing.T) {
	ctx := context.Background()
	store, scope, input, _ := stagedDocumentFixture(t, Options{})
	revision, err := store.ReadRevision(ctx, port.ReadRevisionInput{Scope: scope, RevisionID: input.BaseRevision.RevisionID})
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := store.scopeDir(scope)
	id, _ := safeID(string(revision.Revision.RevisionID))
	for _, raw := range []uint64{uint64(math.MaxInt64), uint64(math.MaxInt64) + 1} {
		revision.SourceBlobs[0].Size = protocolcommon.CanonicalUint64(strconv.FormatUint(raw, 10))
		if err := store.writeJSON(filepath.Join(dir, "documents", "revisions", id+".json"), revisionDisk{Snapshot: revision}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ReadSourceBlobs(ctx, port.ReadSourceBlobsInput{
			Scope: scope, Revision: revision.Revision, Blobs: revision.SourceBlobs,
		}); !errors.Is(err, port.ErrIndeterminate) {
			t.Fatalf("read size %s=%v", revision.SourceBlobs[0].Size, err)
		}
	}
}

func TestParseAuditMaxItemsUsesPlatformIntWidth(t *testing.T) {
	if got, err := parseAuditMaxItems("1"); err != nil || got != 1 {
		t.Fatalf("one=%d err=%v", got, err)
	}
	if _, err := parseAuditMaxItems("9007199254740992"); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("beyond protocol safe integer=%v", err)
	}
	platformOverflow := protocolcommon.CanonicalPositiveSafeInteger(strconv.FormatUint(uint64(math.MaxInt)+1, 10))
	if _, err := parseAuditMaxItems(platformOverflow); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("beyond platform int=%v", err)
	}
	if strconv.IntSize == 32 {
		if _, err := parseAuditMaxItems("2147483648"); !errors.Is(err, port.ErrConflict) {
			t.Fatalf("platform overflow=%v", err)
		}
	} else {
		if got, err := parseAuditMaxItems("9007199254740991"); err != nil || uint64(got) != 9007199254740991 {
			t.Fatalf("safe maximum=%d err=%v", got, err)
		}
	}
}
