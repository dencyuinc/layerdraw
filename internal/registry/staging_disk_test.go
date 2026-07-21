// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestDiskStagedObjectStoreDurabilityIntegrityAndDeduplication(t *testing.T) {
	root := t.TempDir()
	store, err := NewDiskStagedObjectStore(root, 1024)
	if err != nil {
		t.Fatal(err)
	}
	data := "durable Registry bytes"
	ref, err := store.PutRegistryObject(context.Background(), "application/vnd.layerdraw.pack", strings.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := NewDiskStagedObjectStore(root, 1024)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := reopened.OpenRegistryObject(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(stream)
	_ = stream.Close()
	if string(got) != data {
		t.Fatalf("object=%q", got)
	}
	second, err := reopened.PutRegistryObject(context.Background(), ref.MediaType, strings.NewReader(data), int64(len(data)))
	if err != nil || second != ref {
		t.Fatalf("dedupe=%+v err=%v", second, err)
	}
	if err := os.WriteFile(filepath.Join(root, ref.ObjectID), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.OpenRegistryObject(context.Background(), ref); err == nil {
		t.Fatal("corrupt staged bytes accepted")
	}
}

func TestDiskStagedObjectStoreBoundsAndConcurrentPut(t *testing.T) {
	store, err := NewDiskStagedObjectStore(t.TempDir(), 32)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		media string
		body  string
		size  int64
	}{{"", "x", 1}, {"text/plain", "x", 2}, {"text/plain", strings.Repeat("x", 33), 33}} {
		if _, err := store.PutRegistryObject(context.Background(), test.media, strings.NewReader(test.body), test.size); err == nil {
			t.Fatalf("invalid input accepted: %+v", test)
		}
	}
	const workers = 16
	refs := make(chan StagedObjectRef, workers)
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ref, err := store.PutRegistryObject(context.Background(), "text/plain", strings.NewReader("same"), 4)
			refs <- ref
			errs <- err
		}()
	}
	wait.Wait()
	close(refs)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first StagedObjectRef
	for ref := range refs {
		if first.ObjectID == "" {
			first = ref
		} else if ref != first {
			t.Fatalf("non-deterministic refs: %+v %+v", first, ref)
		}
	}
	for _, ref := range []StagedObjectRef{{}, {ObjectID: "../escape", Digest: first.Digest, Size: 4, MediaType: "text/plain"}, {ObjectID: first.ObjectID, Digest: first.Digest, Size: 4, MediaType: "text/plain\nsecret"}} {
		if _, err := store.OpenRegistryObject(context.Background(), ref); err == nil {
			t.Fatalf("invalid ref accepted: %+v", ref)
		}
	}
}

func TestDiskStagedObjectStoreRejectsSymlinkAndNonRegularTargets(t *testing.T) {
	root := t.TempDir()
	store, err := NewDiskStagedObjectStore(root, 32)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.PutRegistryObject(context.Background(), "text/plain", strings.NewReader("safe"), 4)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, ref.ObjectID)
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.blob")
	if err := os.WriteFile(outside, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, target); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := store.OpenRegistryObject(context.Background(), ref); err == nil {
		t.Fatal("symlinked staged object accepted")
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OpenRegistryObject(context.Background(), ref); err == nil {
		t.Fatal("non-regular staged object accepted")
	}
}

func TestDiskStagedObjectStoreRejectsUnsafeRootsAndInvalidOperations(t *testing.T) {
	for _, root := range []string{"", "relative", t.TempDir() + "/objects/../objects"} {
		if _, err := NewDiskStagedObjectStore(root, 1); err == nil {
			t.Fatalf("unsafe root accepted: %q", root)
		}
	}
	rootFile := filepath.Join(t.TempDir(), "objects")
	if err := os.WriteFile(rootFile, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDiskStagedObjectStore(filepath.Join(rootFile, "nested"), 1); err == nil {
		t.Fatal("root below file accepted")
	}

	store, err := NewDiskStagedObjectStore(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if store.maxBytes != DefaultMaxStagedObjectBytes {
		t.Fatalf("default max=%d", store.maxBytes)
	}
	for _, test := range []struct {
		media string
		body  io.Reader
		size  int64
	}{
		{"text/plain", nil, 0},
		{"text/plain\x00bad", strings.NewReader(""), 0},
		{"text/plain\rbad", strings.NewReader(""), 0},
		{"text/plain", strings.NewReader(""), -1},
		{"text/plain", failingReader{}, 0},
	} {
		if _, err := store.PutRegistryObject(context.Background(), test.media, test.body, test.size); err == nil {
			t.Fatalf("invalid put accepted: media=%q size=%d", test.media, test.size)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.PutRegistryObject(cancelled, "text/plain", strings.NewReader("x"), 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled put err=%v", err)
	}

	valid, err := store.PutRegistryObject(context.Background(), "text/plain", strings.NewReader("x"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.OpenRegistryObject(cancelled, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled open err=%v", err)
	}
	missing := valid
	missing.Digest = "sha256:" + strings.Repeat("0", 64)
	missing.ObjectID = strings.Repeat("0", 64) + ".blob"
	if _, err := store.OpenRegistryObject(context.Background(), missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing open err=%v", err)
	}
	for _, invalid := range []StagedObjectRef{
		{ObjectID: valid.ObjectID, Digest: valid.Digest, Size: -1, MediaType: valid.MediaType},
		{ObjectID: valid.ObjectID, Digest: valid.Digest, Size: DefaultMaxStagedObjectBytes + 1, MediaType: valid.MediaType},
		{ObjectID: valid.ObjectID, Digest: valid.Digest, Size: valid.Size, MediaType: ""},
		{ObjectID: valid.ObjectID, Digest: "sha256:ABC", Size: valid.Size, MediaType: valid.MediaType},
	} {
		if _, err := store.OpenRegistryObject(context.Background(), invalid); err == nil {
			t.Fatalf("invalid ref accepted: %+v", invalid)
		}
	}
}

func TestDiskStagedObjectStoreFilesystemFailures(t *testing.T) {
	missingRoot := filepath.Join(t.TempDir(), "missing")
	store := &DiskStagedObjectStore{root: missingRoot, maxBytes: 32}
	if _, err := store.PutRegistryObject(context.Background(), "text/plain", strings.NewReader("x"), 1); err == nil {
		t.Fatal("put below missing root succeeded")
	}
	ref := StagedObjectRef{ObjectID: strings.Repeat("0", 64) + ".blob", Digest: "sha256:" + strings.Repeat("0", 64), Size: 1, MediaType: "text/plain"}
	if _, err := store.OpenRegistryObject(context.Background(), ref); err == nil {
		t.Fatal("open below missing root succeeded")
	}

	root := t.TempDir()
	store = &DiskStagedObjectStore{root: root, maxBytes: 32}
	data := "collision-directory"
	digest := digestBytes([]byte(data))
	target := filepath.Join(root, strings.TrimPrefix(digest, "sha256:")+".blob")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutRegistryObject(context.Background(), "text/plain", strings.NewReader(data), int64(len(data))); err == nil {
		t.Fatal("non-regular collision target accepted")
	}

	oversized := &DiskStagedObjectStore{root: t.TempDir(), maxBytes: 1}
	if _, err := oversized.PutRegistryObject(context.Background(), "text/plain", strings.NewReader("xx"), 1); err == nil {
		t.Fatal("reader exceeding declared maximum accepted")
	}
}
