// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"errors"
	"path"
	"sort"
	"strings"
)

const LayerdrawPackFormat = "layerdraw-pack"

// RegistryPackManifest is the Language 1 semantic minimum. Registry metadata,
// URLs, credentials, ranges, and tags are intentionally not representable.
type RegistryPackManifest struct {
	Format        string                            `json:"format"`
	FormatVersion int                               `json:"format_version"`
	ID            string                            `json:"id"`
	Name          string                            `json:"name"`
	Version       string                            `json:"version"`
	Language      int                               `json:"language"`
	Entry         string                            `json:"entry"`
	Dependencies  map[string]RegistryPackDependency `json:"dependencies"`
}

type RegistryPackDependency struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type RegistryPackArtifact struct {
	Manifest RegistryPackManifest
	Files    map[string][]byte
	Digests  map[string]string
}

// ReadRegistryPack validates the common archive safety policy, the exact
// canonical manifest, closed file classes, and entry presence. Dependency
// closure compilation is performed only after Registry has resolved all exact
// artifacts; this method never performs filesystem or network fallback.
func (e Engine) ReadRegistryPack(ctx context.Context, data []byte, limits LayerdrawLimits) (RegistryPackArtifact, error) {
	scanned, err := scanLayerdraw(ctx, data, limits)
	if err != nil {
		return RegistryPackArtifact{}, err
	}
	files := make(map[string][]byte, len(scanned.files))
	for _, file := range scanned.files {
		if file.FileInfo().IsDir() {
			continue
		}
		value, err := readZipFile(ctx, file, scanned.limits)
		if err != nil {
			return RegistryPackArtifact{}, err
		}
		files[file.Name] = value
	}
	manifestBytes, ok := files["manifest.json"]
	if !ok || int64(len(manifestBytes)) > scanned.limits.MaxManifestBytes {
		return RegistryPackArtifact{}, layerdrawFailure(LayerdrawErrorManifest, "manifest.json", nil)
	}
	var manifest RegistryPackManifest
	if err := decodeJSON(manifestBytes, &manifest, true); err != nil {
		return RegistryPackArtifact{}, layerdrawFailure(LayerdrawErrorManifest, "manifest.json", err)
	}
	canonical, err := canonicalJSONBytes(manifestBytes)
	if err != nil || !bytes.Equal(canonical, manifestBytes) {
		return RegistryPackArtifact{}, layerdrawFailure(LayerdrawErrorManifest, "manifest.json", err)
	}
	if manifest.Format != LayerdrawPackFormat || manifest.FormatVersion != 1 || manifest.Language != LayerdrawLanguage || manifest.ID == "" || manifest.Name == "" || manifest.Version == "" || manifest.Dependencies == nil || !validPackEntry(manifest.Entry) {
		return RegistryPackArtifact{}, layerdrawFailure(LayerdrawErrorManifest, "manifest.json", nil)
	}
	if _, ok := files[manifest.Entry]; !ok {
		return RegistryPackArtifact{}, layerdrawFailure(LayerdrawErrorManifest, manifest.Entry, nil)
	}
	seenDependencies := map[string]bool{}
	for localName, dependency := range manifest.Dependencies {
		if localName == "" || localName == manifest.Name || dependency.ID == "" || dependency.Version == "" || seenDependencies[dependency.ID] {
			return RegistryPackArtifact{}, layerdrawFailure(LayerdrawErrorManifest, "manifest.json", nil)
		}
		seenDependencies[dependency.ID] = true
	}
	digests := make(map[string]string, len(files)-1)
	for name, value := range files {
		if name == "manifest.json" {
			continue
		}
		if !allowedPackEntry(name) {
			return RegistryPackArtifact{}, layerdrawFailure(LayerdrawErrorForbiddenPortable, name, nil)
		}
		digests[name] = rawDigest(value)
	}
	return RegistryPackArtifact{Manifest: manifest, Files: cloneByteMap(files), Digests: digests}, nil
}

func validPackEntry(value string) bool {
	return value != "" && path.Clean(value) == value && !strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "../") && path.Ext(value) == ".ldl"
}

func allowedPackEntry(value string) bool {
	if validPackEntry(value) {
		return true
	}
	return value == "checksums.json" || value == "signature.json" || strings.HasPrefix(value, "assets/") || strings.HasPrefix(value, "previews/")
}

// CompileRegistryPackClosure compiles the exact Registry-resolved closure.
// The root and dependency metadata must bind every staged manifest and file.
func (e Engine) CompileRegistryPackClosure(ctx context.Context, rootID string, packs []ResolvedPack, tree map[string][]byte, limits ResourceLimits) (Snapshot, error) {
	if rootID == "" || len(packs) == 0 || len(tree) == 0 {
		return Snapshot{}, errors.New("Registry Pack closure is incomplete")
	}
	copyPacks := append([]ResolvedPack(nil), packs...)
	sort.Slice(copyPacks, func(i, j int) bool { return copyPacks[i].InstallName < copyPacks[j].InstallName })
	var entry string
	for _, pack := range copyPacks {
		if pack.CanonicalID == rootID {
			entry = pack.Entry
			break
		}
	}
	if entry == "" {
		return Snapshot{}, errors.New("Registry Pack root is absent from closure")
	}
	result, err := e.Compile(ctx, CompileInput{Mode: CompilePack, EntryPath: entry, RootPackID: rootID, InstalledPackTree: cloneByteMap(tree), ResolvedDependencies: ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: copyPacks}, ResourceLimits: limits})
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := result.Snapshot()
	if hasErrorDiagnostics(snapshot.Diagnostics) || snapshot.NormalizedPackArtifact == nil || snapshot.TypedAST.Pack == nil {
		return Snapshot{}, errors.New("Registry Pack closure failed semantic validation")
	}
	return snapshot, nil
}
