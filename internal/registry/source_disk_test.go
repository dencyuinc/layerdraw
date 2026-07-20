// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDiskSourceStateStoreRestoresLocalAndFencesRemoteLeases(t *testing.T) {
	ctx := context.Background()
	store, err := NewDiskSourceStateStore(filepath.Join(t.TempDir(), "registry", "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	sources := []RegistrySource{
		{SourceID: "local", Kind: SourceLocalDirectory, EndpointRef: "/tmp/catalog", TrustPolicyID: "desktop-local", AuthConnectionRef: "local", Connected: true, Revision: 2},
		{SourceID: "remote", Kind: SourceOfficial, EndpointRef: "https://registry.example/", TrustPolicyID: "official", AuthConnectionRef: "keychain:remote", Connected: true, Revision: 3},
	}
	if err := store.SaveRegistrySources(ctx, sources); err != nil {
		t.Fatal(err)
	}
	registryValue := &Registry{sources: map[string]RegistrySource{}}
	if err := registryValue.AttachSourceStateStore(ctx, store); err != nil {
		t.Fatal(err)
	}
	loaded := registryValue.Sources()
	if len(loaded) != 2 {
		t.Fatalf("sources=%+v", loaded)
	}
	for _, source := range loaded {
		if source.SourceID == "local" && !source.Connected {
			t.Fatal("local source did not survive restart")
		}
		if source.SourceID == "remote" && (source.Connected || source.AuthConnectionRef != "") {
			t.Fatal("remote credential lease survived restart")
		}
	}
}
