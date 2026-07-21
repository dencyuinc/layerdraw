// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package registrysource contains production transports for Registry sources.
// It transports typed Registry catalog entries and artifact bytes only; trust,
// dependency resolution, archive validation, and installation remain owned by
// the Registry and Engine components.
package registrysource

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/internal/registry"
)

const (
	CatalogPath     = ".layerdraw-registry/catalog-v1.json"
	CatalogVersion  = 1
	maxCatalogBytes = 4 << 20
)

type Catalog struct {
	SchemaVersion int            `json:"schema_version"`
	Artifacts     []CatalogEntry `json:"artifacts"`
}

type CatalogEntry struct {
	Release      registry.ArtifactRelease `json:"release"`
	ArtifactPath string                   `json:"artifact_path"`
}

// LocalDirectory implements local_directory and checked-out git sources.
// Every resolved file must remain below the configured source root, including
// after symlink evaluation.
type LocalDirectory struct{}

func (LocalDirectory) ProbeRegistrySource(ctx context.Context, source registry.RegistrySource, lease registry.CredentialLease) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if source.Kind != registry.SourceLocalDirectory && source.Kind != registry.SourceGit {
		return errors.New("unsupported local Registry source kind")
	}
	if len(lease.Credential) != 0 {
		return errors.New("local Registry source does not accept credentials")
	}
	_, err := readLocalCatalog(source)
	return err
}

func (LocalDirectory) Search(ctx context.Context, source registry.RegistrySource, input registry.SearchInput) ([]registry.ArtifactRelease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	catalog, err := readLocalCatalog(source)
	if err != nil {
		return nil, err
	}
	return searchCatalog(source, catalog, input)
}

func (LocalDirectory) OpenArtifact(ctx context.Context, source registry.RegistrySource, release registry.ArtifactRelease) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	catalog, err := readLocalCatalog(source)
	if err != nil {
		return nil, err
	}
	entry, err := findCatalogEntry(source, catalog, release)
	if err != nil {
		return nil, err
	}
	root, err := localRoot(source)
	if err != nil {
		return nil, err
	}
	path, err := confinedPath(root, entry.ArtifactPath)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func readLocalCatalog(source registry.RegistrySource) (Catalog, error) {
	root, err := localRoot(source)
	if err != nil {
		return Catalog{}, err
	}
	path, err := confinedPath(root, CatalogPath)
	if err != nil {
		return Catalog{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return Catalog{}, err
	}
	defer file.Close()
	return decodeCatalog(io.LimitReader(file, maxCatalogBytes+1))
}

func localRoot(source registry.RegistrySource) (string, error) {
	if source.Kind != registry.SourceLocalDirectory && source.Kind != registry.SourceGit {
		return "", errors.New("unsupported local Registry source kind")
	}
	if source.EndpointRef == "" || strings.ContainsRune(source.EndpointRef, '\x00') {
		return "", errors.New("invalid local Registry endpoint")
	}
	value := source.EndpointRef
	// A native Windows absolute path begins with a drive letter and colon.
	// net/url interprets that drive letter as a URL scheme, so recognize native
	// absolute paths before accepting the explicit file: URL form.
	if !filepath.IsAbs(value) {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme == "" {
			return "", errors.New("invalid local Registry endpoint")
		}
		if parsed.Scheme != "file" || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", errors.New("invalid local Registry endpoint")
		}
		value, err = url.PathUnescape(parsed.Path)
		if err != nil {
			return "", errors.New("invalid local Registry endpoint")
		}
	}
	if !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return "", errors.New("local Registry endpoint must be a clean absolute path")
	}
	root, err := filepath.EvalSymlinks(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", errors.New("local Registry endpoint is not a directory")
	}
	return root, nil
}

func confinedPath(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != filepath.FromSlash(relative) {
		return "", errors.New("Registry artifact path is not canonical")
	}
	joined := filepath.Join(root, filepath.FromSlash(relative))
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("Registry artifact escapes source root")
	}
	return resolved, nil
}

// HTTPS implements official, organization_private, and self_hosted sources.
// Credential bytes live only in memory and are attached as a Bearer token.
// Redirects are rejected so credentials cannot cross an origin boundary.
type HTTPS struct {
	client *http.Client
	mu     sync.RWMutex
	leases map[string]registry.CredentialLease
}

func NewHTTPS(client *http.Client) (*HTTPS, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if copy.Timeout <= 0 {
		copy.Timeout = 30 * time.Second
	}
	return &HTTPS{client: &copy, leases: map[string]registry.CredentialLease{}}, nil
}

func (h *HTTPS) ProbeRegistrySource(ctx context.Context, source registry.RegistrySource, lease registry.CredentialLease) error {
	if _, err := remoteBase(source); err != nil {
		return err
	}
	if lease.ConnectionRef == "" || len(lease.Credential) == 0 || !lease.ExpiresAt.After(time.Now()) {
		return errors.New("Registry credential lease is invalid")
	}
	h.mu.Lock()
	h.leases[source.SourceID] = cloneLease(lease)
	h.mu.Unlock()
	if _, err := h.readCatalog(ctx, source); err != nil {
		h.mu.Lock()
		delete(h.leases, source.SourceID)
		h.mu.Unlock()
		return err
	}
	return nil
}

func (h *HTTPS) Search(ctx context.Context, source registry.RegistrySource, input registry.SearchInput) ([]registry.ArtifactRelease, error) {
	catalog, err := h.readCatalog(ctx, source)
	if err != nil {
		return nil, err
	}
	return searchCatalog(source, catalog, input)
}

func (h *HTTPS) OpenArtifact(ctx context.Context, source registry.RegistrySource, release registry.ArtifactRelease) (io.ReadCloser, error) {
	catalog, err := h.readCatalog(ctx, source)
	if err != nil {
		return nil, err
	}
	entry, err := findCatalogEntry(source, catalog, release)
	if err != nil {
		return nil, err
	}
	base, err := remoteBase(source)
	if err != nil {
		return nil, err
	}
	artifactURL, err := resolveRemotePath(base, entry.ArtifactPath)
	if err != nil {
		return nil, err
	}
	response, err := h.get(ctx, source, artifactURL)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("Registry artifact response status %d", response.StatusCode)
	}
	if response.ContentLength >= 0 && release.Size >= 0 && response.ContentLength != release.Size {
		response.Body.Close()
		return nil, errors.New("Registry artifact size does not match catalog")
	}
	return response.Body, nil
}

func (h *HTTPS) readCatalog(ctx context.Context, source registry.RegistrySource) (Catalog, error) {
	base, err := remoteBase(source)
	if err != nil {
		return Catalog{}, err
	}
	catalogURL, err := resolveRemotePath(base, CatalogPath)
	if err != nil {
		return Catalog{}, err
	}
	response, err := h.get(ctx, source, catalogURL)
	if err != nil {
		return Catalog{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Catalog{}, fmt.Errorf("Registry catalog response status %d", response.StatusCode)
	}
	return decodeCatalog(io.LimitReader(response.Body, maxCatalogBytes+1))
}

func (h *HTTPS) get(ctx context.Context, source registry.RegistrySource, target *url.URL) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	if source.AuthConnectionRef != "" {
		h.mu.RLock()
		lease, ok := h.leases[source.SourceID]
		h.mu.RUnlock()
		if !ok || lease.ConnectionRef != source.AuthConnectionRef || !lease.ExpiresAt.After(time.Now()) || len(lease.Credential) == 0 {
			return nil, errors.New("Registry credential lease is unavailable")
		}
		request.Header.Set("Authorization", "Bearer "+string(lease.Credential))
	}
	request.Header.Set("Accept", "application/json, application/octet-stream")
	return h.client.Do(request)
}

func remoteBase(source registry.RegistrySource) (*url.URL, error) {
	if source.Kind != registry.SourceOfficial && source.Kind != registry.SourceOrganizationPrivate && source.Kind != registry.SourceSelfHosted {
		return nil, errors.New("unsupported remote Registry source kind")
	}
	parsed, err := url.Parse(source.EndpointRef)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("remote Registry endpoint must be an HTTPS origin path")
	}
	parsed.Path = strings.TrimSuffix(parsed.EscapedPath(), "/") + "/"
	parsed.RawPath = ""
	return parsed, nil
}

func resolveRemotePath(base *url.URL, relative string) (*url.URL, error) {
	if relative == "" || strings.HasPrefix(relative, "/") || strings.Contains(relative, "\\") {
		return nil, errors.New("Registry remote path is not canonical")
	}
	ref, err := url.Parse(relative)
	if err != nil || ref.IsAbs() || ref.Host != "" || ref.RawQuery != "" || ref.Fragment != "" || strings.Contains(ref.Path, "..") {
		return nil, errors.New("Registry remote path is not canonical")
	}
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != base.Scheme || resolved.Host != base.Host || !strings.HasPrefix(resolved.EscapedPath(), base.EscapedPath()) {
		return nil, errors.New("Registry remote path escapes endpoint")
	}
	return resolved, nil
}

func decodeCatalog(reader io.Reader) (Catalog, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return Catalog{}, err
	}
	if len(data) == 0 || len(data) > maxCatalogBytes {
		return Catalog{}, errors.New("Registry catalog size is invalid")
	}
	var catalog Catalog
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return Catalog{}, errors.New("Registry catalog is invalid")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Catalog{}, errors.New("Registry catalog has trailing JSON")
	}
	if catalog.SchemaVersion != CatalogVersion || catalog.Artifacts == nil {
		return Catalog{}, errors.New("Registry catalog version is unsupported")
	}
	seen := map[string]bool{}
	for _, entry := range catalog.Artifacts {
		key := releaseKey(entry.Release)
		if key == "" || seen[key] || entry.Release.Digest == "" || entry.Release.Size < 0 || entry.Release.Dependencies == nil || entry.Release.Compatibility == nil {
			return Catalog{}, errors.New("Registry catalog entry is invalid")
		}
		if _, err := resolveRemotePath(&url.URL{Scheme: "https", Host: "catalog.invalid", Path: "/"}, entry.ArtifactPath); err != nil {
			return Catalog{}, err
		}
		seen[key] = true
	}
	return catalog, nil
}

func searchCatalog(source registry.RegistrySource, catalog Catalog, input registry.SearchInput) ([]registry.ArtifactRelease, error) {
	query := strings.ToLower(strings.TrimSpace(input.Query))
	result := make([]registry.ArtifactRelease, 0)
	for _, entry := range catalog.Artifacts {
		release := entry.Release
		if release.SourceID != "" && release.SourceID != source.SourceID {
			return nil, errors.New("Registry catalog source binding is invalid")
		}
		if input.Kind != nil && release.Identity.Kind != *input.Kind {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(release.Identity.CanonicalID), query) && !strings.Contains(strings.ToLower(release.PublisherID), query) {
			continue
		}
		release.SourceID = source.SourceID
		result = append(result, release)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Identity.CanonicalID != result[j].Identity.CanonicalID {
			return result[i].Identity.CanonicalID < result[j].Identity.CanonicalID
		}
		return result[i].Identity.Version > result[j].Identity.Version
	})
	return result, nil
}

func findCatalogEntry(source registry.RegistrySource, catalog Catalog, release registry.ArtifactRelease) (CatalogEntry, error) {
	wanted := releaseKey(release)
	for _, entry := range catalog.Artifacts {
		candidate := entry.Release
		if candidate.SourceID == "" {
			candidate.SourceID = source.SourceID
		}
		if releaseKey(candidate) == wanted && subtle.ConstantTimeCompare([]byte(candidate.Digest), []byte(release.Digest)) == 1 {
			return entry, nil
		}
	}
	return CatalogEntry{}, errors.New("Registry artifact is absent from catalog")
}

func releaseKey(release registry.ArtifactRelease) string {
	if release.Identity.Kind == "" || release.Identity.CanonicalID == "" || release.Identity.Version == "" {
		return ""
	}
	return string(release.Identity.Kind) + "\x00" + release.Identity.CanonicalID + "\x00" + release.Identity.Version + "\x00" + release.SourceID
}

func cloneLease(value registry.CredentialLease) registry.CredentialLease {
	value.Credential = append([]byte(nil), value.Credential...)
	return value
}
