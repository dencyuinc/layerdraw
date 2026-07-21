// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registrysource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/internal/registry"
)

func TestLocalDirectorySearchAndArtifactAreRootConfined(t *testing.T) {
	root := t.TempDir()
	body := []byte("pack bytes")
	release := fixtureRelease("", body)
	writeCatalog(t, root, Catalog{SchemaVersion: CatalogVersion, Artifacts: []CatalogEntry{{Release: release, ArtifactPath: "artifacts/demo.ldpack"}}})
	if err := os.MkdirAll(filepath.Join(root, "artifacts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "artifacts", "demo.ldpack"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	source := registry.RegistrySource{SourceID: "local", Kind: registry.SourceLocalDirectory, EndpointRef: root}
	client := LocalDirectory{}
	if err := client.ProbeRegistrySource(context.Background(), source, registry.CredentialLease{}); err != nil {
		t.Fatal(err)
	}
	pack := registry.ArtifactPack
	found, err := client.Search(context.Background(), source, registry.SearchInput{Query: "EXAMPLE/DEMO", Kind: &pack})
	if err != nil || len(found) != 1 || found[0].SourceID != source.SourceID {
		t.Fatalf("search=%+v err=%v", found, err)
	}
	stream, err := client.OpenArtifact(context.Background(), source, found[0])
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	got, _ := io.ReadAll(stream)
	if string(got) != string(body) {
		t.Fatalf("artifact=%q", got)
	}

	outside := filepath.Join(t.TempDir(), "secret.ldpack")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Remove(filepath.Join(root, "artifacts", "demo.ldpack")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "artifacts", "demo.ldpack")); err != nil {
			t.Fatal(err)
		}
		if _, err := client.OpenArtifact(context.Background(), source, found[0]); err == nil {
			t.Fatal("symlink escape accepted")
		}
	}
}

func TestHTTPSUsesCredentialLeaseAndRejectsRedirect(t *testing.T) {
	body := []byte("remote pack")
	release := fixtureRelease("remote", body)
	catalog := Catalog{SchemaVersion: CatalogVersion, Artifacts: []CatalogEntry{{Release: release, ArtifactPath: "artifacts/demo.ldpack"}}}
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer registry-token" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/base/" + CatalogPath:
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write(encoded)
		case "/base/artifacts/demo.ldpack":
			_, _ = writer.Write(body)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewHTTPS(server.Client())
	if err != nil {
		t.Fatal(err)
	}
	source := registry.RegistrySource{SourceID: "remote", Kind: registry.SourceSelfHosted, EndpointRef: server.URL + "/base", AuthConnectionRef: "keychain:remote"}
	lease := registry.CredentialLease{ConnectionRef: source.AuthConnectionRef, Credential: []byte("registry-token"), ExpiresAt: time.Now().Add(time.Hour)}
	if err := client.ProbeRegistrySource(context.Background(), source, lease); err != nil {
		t.Fatal(err)
	}
	lease.Credential[0] = 'X'
	found, err := client.Search(context.Background(), source, registry.SearchInput{})
	if err != nil || len(found) != 1 {
		t.Fatalf("search=%+v err=%v", found, err)
	}
	stream, err := client.OpenArtifact(context.Background(), source, found[0])
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(stream)
	_ = stream.Close()
	if string(got) != string(body) {
		t.Fatalf("artifact=%q", got)
	}

	redirect := httptest.NewTLSServer(http.RedirectHandler(server.URL+"/base/"+CatalogPath, http.StatusFound))
	defer redirect.Close()
	redirectClient, _ := NewHTTPS(redirect.Client())
	redirectSource := source
	redirectSource.EndpointRef = redirect.URL
	if err := redirectClient.ProbeRegistrySource(context.Background(), redirectSource, registry.CredentialLease{ConnectionRef: source.AuthConnectionRef, Credential: []byte("registry-token"), ExpiresAt: time.Now().Add(time.Hour)}); err == nil {
		t.Fatal("redirect accepted")
	}
}

func TestCatalogAndEndpointValidationFailClosed(t *testing.T) {
	for _, value := range []string{
		`{}`,
		`{"schema_version":1,"artifacts":null}`,
		`{"schema_version":1,"artifacts":[],"extra":true}`,
		`{"schema_version":1,"artifacts":[]} {}`,
		strings.Repeat("x", maxCatalogBytes+1),
	} {
		if _, err := decodeCatalog(strings.NewReader(value)); err == nil {
			t.Fatalf("invalid catalog accepted: %.80q", value)
		}
	}
	for _, endpoint := range []string{"http://registry.example", "https://user:secret@registry.example", "https://registry.example?token=x", "https://registry.example/#fragment"} {
		if _, err := remoteBase(registry.RegistrySource{Kind: registry.SourceOfficial, EndpointRef: endpoint}); err == nil {
			t.Fatalf("invalid endpoint accepted: %s", endpoint)
		}
	}
	if _, err := resolveRemotePath(&urlFixture, "../secret"); err == nil {
		t.Fatal("remote traversal accepted")
	}
}

func TestLocalDirectoryRejectsInvalidBindingsAndSupportsFileURL(t *testing.T) {
	root := t.TempDir()
	body := []byte("pack bytes")
	release := fixtureRelease("", body)
	writeCatalog(t, root, Catalog{SchemaVersion: CatalogVersion, Artifacts: []CatalogEntry{{Release: release, ArtifactPath: "demo.ldpack"}}})
	if err := os.WriteFile(filepath.Join(root, "demo.ldpack"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	client := LocalDirectory{}
	source := registry.RegistrySource{SourceID: "git", Kind: registry.SourceGit, EndpointRef: (&url.URL{Scheme: "file", Path: filepath.ToSlash(root)}).String()}
	if err := client.ProbeRegistrySource(context.Background(), source, registry.CredentialLease{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.OpenArtifact(context.Background(), source, fixtureRelease("git", []byte("different"))); err == nil {
		t.Fatal("catalog artifact with a different digest was opened")
	}
	if err := client.ProbeRegistrySource(context.Background(), source, registry.CredentialLease{Credential: []byte("secret")}); err == nil {
		t.Fatal("local source accepted credentials")
	}
	badKind := source
	badKind.Kind = registry.SourceOfficial
	if err := client.ProbeRegistrySource(context.Background(), badKind, registry.CredentialLease{}); err == nil {
		t.Fatal("remote kind was accepted by local transport")
	}
	for _, endpoint := range []string{"relative", "file://host/path", "file:///path?query=x", "file:///path#fragment", string([]byte{'/', 'x', 0})} {
		bad := source
		bad.EndpointRef = endpoint
		if _, err := localRoot(bad); err == nil {
			t.Fatalf("invalid local endpoint accepted: %q", endpoint)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Search(cancelled, source, registry.SearchInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled search err=%v", err)
	}
}

func TestCatalogSearchFilteringSortingAndSourceBinding(t *testing.T) {
	body := []byte("pack")
	first := fixtureRelease("", body)
	first.Identity.Version = "1.0.0"
	second := first
	second.Identity.Version = "2.0.0"
	second.PublisherID = "Acme"
	template := first
	template.Identity = registry.ArtifactIdentity{Kind: registry.ArtifactTemplate, CanonicalID: "example/template", Version: "1.0.0"}
	catalog := Catalog{SchemaVersion: CatalogVersion, Artifacts: []CatalogEntry{{Release: first}, {Release: template}, {Release: second}}}
	source := registry.RegistrySource{SourceID: "bound"}
	pack := registry.ArtifactPack
	found, err := searchCatalog(source, catalog, registry.SearchInput{Query: "EXAMPLE", Kind: &pack})
	if err != nil || len(found) != 2 || found[0].Identity.Version != "2.0.0" || found[1].Identity.Version != "1.0.0" {
		t.Fatalf("found=%+v err=%v", found, err)
	}
	if unmatched, err := searchCatalog(source, catalog, registry.SearchInput{Query: "missing"}); err != nil || len(unmatched) != 0 {
		t.Fatalf("unmatched=%+v err=%v", unmatched, err)
	}
	bound := first
	bound.SourceID = "other"
	if _, err := searchCatalog(source, Catalog{SchemaVersion: CatalogVersion, Artifacts: []CatalogEntry{{Release: bound}}}, registry.SearchInput{}); err == nil {
		t.Fatal("catalog source mismatch was accepted")
	}
}

func TestHTTPSRejectsUnavailableLeaseStatusAndSizeMismatch(t *testing.T) {
	body := []byte("remote pack")
	release := fixtureRelease("remote", body)
	catalog := Catalog{SchemaVersion: CatalogVersion, Artifacts: []CatalogEntry{{Release: release, ArtifactPath: "artifact.ldpack"}}}
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/base/" + CatalogPath:
			_, _ = writer.Write(encoded)
		case "/base/artifact.ldpack":
			writer.Header().Set("Content-Length", "1")
			_, _ = writer.Write([]byte("x"))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewHTTPS(server.Client())
	if err != nil {
		t.Fatal(err)
	}
	source := registry.RegistrySource{SourceID: "remote", Kind: registry.SourceSelfHosted, EndpointRef: server.URL + "/base", AuthConnectionRef: "connection"}
	if _, err := client.Search(context.Background(), source, registry.SearchInput{}); err == nil {
		t.Fatal("authenticated source was read without a lease")
	}
	if err := client.ProbeRegistrySource(context.Background(), source, registry.CredentialLease{ConnectionRef: "connection", Credential: []byte("token"), ExpiresAt: time.Now().Add(-time.Minute)}); err == nil {
		t.Fatal("expired credential lease was accepted")
	}
	if err := client.ProbeRegistrySource(context.Background(), source, registry.CredentialLease{ConnectionRef: "connection", Credential: []byte("token"), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.OpenArtifact(context.Background(), source, release); err == nil {
		t.Fatal("artifact response with mismatched size was accepted")
	}
}

var urlFixture = mustURL("https://registry.example/base/")

func mustURL(value string) (result url.URL) {
	parsed, err := url.Parse(value)
	if err != nil {
		panic(err)
	}
	return *parsed
}

func fixtureRelease(sourceID string, body []byte) registry.ArtifactRelease {
	digest := sha256.Sum256(body)
	return registry.ArtifactRelease{
		Identity: registry.ArtifactIdentity{Kind: registry.ArtifactPack, CanonicalID: "example/demo", Version: "1.0.0"},
		SourceID: sourceID, PublisherID: "example", Digest: "sha256:" + hex.EncodeToString(digest[:]),
		ManifestDigest: "sha256:manifest", DependencyMetadataDigest: "sha256:dependencies", Size: int64(len(body)),
		Dependencies: []registry.Dependency{}, Compatibility: []registry.CompatibilityDecision{}, License: "MIT", ProvenanceDigest: "sha256:provenance",
	}
}

func writeCatalog(t *testing.T, root string, catalog Catalog) {
	t.Helper()
	directory := filepath.Join(root, filepath.FromSlash(filepath.Dir(CatalogPath)))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(CatalogPath)), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
