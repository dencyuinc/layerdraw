// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestDiskSourceStateStoreRejectsUnsafeAndCorruptState(t *testing.T) {
	for _, path := range []string{"", "relative/sources.json", t.TempDir() + "/registry/../sources.json"} {
		if _, err := NewDiskSourceStateStore(path); err == nil {
			t.Fatalf("unsafe state path accepted: %q", path)
		}
	}
	parentFile := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(parentFile, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDiskSourceStateStore(filepath.Join(parentFile, "sources.json")); err == nil {
		t.Fatal("state store below a file was accepted")
	}

	path := filepath.Join(t.TempDir(), "sources.json")
	store, err := NewDiskSourceStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadRegistrySources(context.Background())
	if err != nil || len(loaded) != 0 {
		t.Fatalf("missing state loaded=%+v err=%v", loaded, err)
	}
	for _, data := range []string{
		`not json`,
		`{"version":2,"sources":[]}`,
		`{"version":1,"sources":null}`,
		`{"version":1,"sources":[],"unknown":true}`,
		`{"version":1,"sources":[]} {}`,
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.LoadRegistrySources(context.Background()); err == nil {
			t.Fatalf("corrupt state accepted: %q", data)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.LoadRegistrySources(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled load err=%v", err)
	}
	if err := store.SaveRegistrySources(cancelled, []RegistrySource{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled save err=%v", err)
	}

	directoryTarget := t.TempDir()
	directoryStore, err := NewDiskSourceStateStore(directoryTarget)
	if err != nil {
		t.Fatal(err)
	}
	if err := directoryStore.SaveRegistrySources(context.Background(), []RegistrySource{}); err == nil {
		t.Fatal("directory state target was overwritten")
	}
}

func TestSourceMapValuesAreStableAndDetached(t *testing.T) {
	values := map[string]RegistrySource{
		"z": {SourceID: "z", Priority: 1},
		"a": {SourceID: "a", Priority: 2},
	}
	ordered := sourceMapValues(values)
	if len(ordered) != 2 || ordered[0].SourceID != "a" || ordered[1].SourceID != "z" {
		t.Fatalf("ordered=%+v", ordered)
	}
	ordered[0].SourceID = "changed"
	if values["a"].SourceID != "a" {
		t.Fatal("source map projection aliased map values")
	}
}

type sourceStateStoreStub struct {
	sources []RegistrySource
	err     error
	saveErr error
	saved   *[]RegistrySource
}

func (s sourceStateStoreStub) LoadRegistrySources(context.Context) ([]RegistrySource, error) {
	return s.sources, s.err
}
func (s sourceStateStoreStub) SaveRegistrySources(_ context.Context, sources []RegistrySource) error {
	if s.saved != nil {
		*s.saved = append([]RegistrySource(nil), sources...)
	}
	return s.saveErr
}

func TestAttachSourceStateStoreValidationAndLeaseFencing(t *testing.T) {
	registryValue := &Registry{sources: map[string]RegistrySource{}}
	if err := registryValue.AttachSourceStateStore(context.Background(), nil); err == nil {
		t.Fatal("nil source store accepted")
	}
	sentinel := errors.New("load failed")
	if err := registryValue.AttachSourceStateStore(context.Background(), sourceStateStoreStub{err: sentinel}); !errors.Is(err, sentinel) {
		t.Fatalf("load err=%v", err)
	}
	invalidSets := [][]RegistrySource{
		{{}},
		{{SourceID: "x", EndpointRef: "endpoint"}},
		{{SourceID: "x", TrustPolicyID: "policy"}},
		{{SourceID: "x", EndpointRef: "one", TrustPolicyID: "policy"}, {SourceID: "x", EndpointRef: "two", TrustPolicyID: "policy"}},
	}
	for _, sources := range invalidSets {
		if err := registryValue.AttachSourceStateStore(context.Background(), sourceStateStoreStub{sources: sources}); err == nil {
			t.Fatalf("invalid persisted sources accepted: %+v", sources)
		}
	}
	local := RegistrySource{SourceID: "local", Kind: SourceGit, EndpointRef: "/repo", TrustPolicyID: "local", AuthConnectionRef: "lease", Connected: true}
	remote := RegistrySource{SourceID: "remote", Kind: SourceOfficial, EndpointRef: "https://example.invalid", TrustPolicyID: "official", AuthConnectionRef: "secret", Connected: true}
	store := sourceStateStoreStub{sources: []RegistrySource{local, remote}}
	if err := registryValue.AttachSourceStateStore(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	got := registryValue.Sources()
	if len(got) != 2 || !got[0].Connected || got[0].AuthConnectionRef != "lease" || got[1].Connected || got[1].AuthConnectionRef != "" {
		t.Fatalf("restored sources=%+v", got)
	}
}

func TestConfiguredSourcesPersistAtomically(t *testing.T) {
	sentinel := errors.New("save failed")
	base := RegistrySource{SourceID: "source", Kind: SourceLocalDirectory, EndpointRef: "/catalog", TrustPolicyID: "policy"}
	registryValue := &Registry{
		sources:  map[string]RegistrySource{},
		policies: map[string]TrustPolicy{"policy": {PolicyID: "policy"}},
		now:      func() time.Time { return time.Unix(1, 0) },
	}
	registryValue.sourceStore = sourceStateStoreStub{saveErr: sentinel}
	if err := registryValue.ConfigureSource(base); !IsFailure(err, FailureUnavailable) || len(registryValue.sources) != 0 {
		t.Fatalf("failed configure mutated state: err=%v sources=%+v", err, registryValue.sources)
	}
	var saved []RegistrySource
	registryValue.sourceStore = sourceStateStoreStub{saved: &saved}
	if err := registryValue.ConfigureSource(base); err != nil || len(saved) != 1 || saved[0].Revision != 1 {
		t.Fatalf("configure save=%+v err=%v", saved, err)
	}
	if err := registryValue.ConfigureSource(base); err != nil || registryValue.sources[base.SourceID].Revision != 2 {
		t.Fatalf("reconfigure err=%v source=%+v", err, registryValue.sources[base.SourceID])
	}

	registryValue.connectors = map[SourceKind]SourceConnector{SourceLocalDirectory: sourceConnectorFunc(func(context.Context, RegistrySource, CredentialLease) error { return nil })}
	registryValue.sourceStore = sourceStateStoreStub{saveErr: sentinel}
	if err := registryValue.ConnectSource(context.Background(), base.SourceID, "local"); !IsFailure(err, FailureUnavailable) || registryValue.sources[base.SourceID].Connected {
		t.Fatalf("failed connect mutated state: err=%v source=%+v", err, registryValue.sources[base.SourceID])
	}
	registryValue.sourceStore = sourceStateStoreStub{saved: &saved}
	if err := registryValue.ConnectSource(context.Background(), base.SourceID, "local"); err != nil || !registryValue.sources[base.SourceID].Connected {
		t.Fatalf("connect err=%v source=%+v", err, registryValue.sources[base.SourceID])
	}
	registryValue.sourceStore = sourceStateStoreStub{saveErr: sentinel}
	if err := registryValue.DisconnectSource(base.SourceID); !IsFailure(err, FailureUnavailable) || !registryValue.sources[base.SourceID].Connected {
		t.Fatalf("failed disconnect mutated state: err=%v source=%+v", err, registryValue.sources[base.SourceID])
	}
	registryValue.sourceStore = sourceStateStoreStub{saved: &saved}
	if err := registryValue.DisconnectSource(base.SourceID); err != nil || registryValue.sources[base.SourceID].Connected {
		t.Fatalf("disconnect err=%v source=%+v", err, registryValue.sources[base.SourceID])
	}
}

type sourceConnectorFunc func(context.Context, RegistrySource, CredentialLease) error

func (f sourceConnectorFunc) ProbeRegistrySource(ctx context.Context, source RegistrySource, lease CredentialLease) error {
	return f(ctx, source, lease)
}

func TestDiskSourceStateStoreFilesystemErrors(t *testing.T) {
	directoryStore, err := NewDiskSourceStateStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := directoryStore.LoadRegistrySources(context.Background()); err == nil {
		t.Fatal("directory source-state file was read")
	}
	missingParent := filepath.Join(t.TempDir(), "missing", "sources.json")
	direct := &DiskSourceStateStore{path: missingParent}
	if err := direct.SaveRegistrySources(context.Background(), []RegistrySource{}); err == nil {
		t.Fatal("save below missing parent succeeded")
	}
}
