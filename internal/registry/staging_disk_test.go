// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

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
