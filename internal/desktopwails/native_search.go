// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package desktopwails

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/host"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
)

func packagedNativeSearchEnabled() bool { return true }

func packagedNativeSearchLifecycle(owner *sharedOwner) desktopapp.NativeSearchLifecycle { return owner }

func (o *sharedOwner) RefreshSearchIndex(ctx context.Context, session *localdocument.Session) error {
	o.mu.RLock()
	lifecycle := o.searchLife
	o.mu.RUnlock()
	if lifecycle == nil {
		return errors.New("Desktop native Search lifecycle is unavailable")
	}
	return lifecycle.RefreshSearchIndex(ctx, session)
}

func openPackagedNativeSearch(root string, local *localdocument.Host, engine *endpoint.HostEngineFacade) (host.ConsumerSearchSurface, host.SearchDocumentLifecycle, func(), error) {
	if !filepath.IsAbs(root) {
		return nil, nil, nil, errors.New("Desktop native Search root must be absolute")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, nil, nil, err
	}
	nativeRoot, err := packagedLadybugRoot()
	if err != nil {
		return nil, nil, nil, err
	}
	keys := make([]byte, 64)
	if _, err := rand.Read(keys); err != nil {
		return nil, nil, nil, err
	}
	composition, err := host.OpenDesktopNativeSearch(host.DesktopNativeSearchConfig{
		Root: root, DatabasePath: filepath.Join(root, "desktop-search.lbug"),
		FTSExtensionPath:    filepath.Join(nativeRoot, "libfts.lbug_extension"),
		VectorExtensionPath: filepath.Join(nativeRoot, "libvector.lbug_extension"),
		AlgoExtensionPath:   filepath.Join(nativeRoot, "libalgo.lbug_extension"),
		PlanKey:             keys[:32], SearchDocumentKey: keys[32:64], LocalModelSeed: packagedEmbeddingSeed(),
		EmbeddingProfile: packagedEmbeddingProfile(),
		MaxRows:          1000, MaxBytes: 8 << 20, LocalHost: local, Engine: engine,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return composition.Surface, composition, composition.Close, nil
}

func packagedLadybugRoot() (string, error) {
	if explicit := os.Getenv("LAYERDRAW_LADYBUG_NATIVE_DIR"); explicit != "" {
		return validateLadybugRoot(explicit)
	}
	if fts := os.Getenv("LAYERDRAW_LADYBUG_FTS_EXTENSION"); fts != "" {
		return validateLadybugRoot(filepath.Dir(fts))
	}
	executable, err := os.Executable()
	if err != nil {
		return "", errors.New("Desktop native component root is unavailable")
	}
	var candidate string
	switch CurrentPlatform() {
	case "macos":
		candidate = filepath.Join(filepath.Dir(executable), "..", "Resources", "layerdraw", "native")
	case "windows":
		candidate = filepath.Join(filepath.Dir(executable), "native")
	case "linux":
		candidate = "/usr/lib/layerdraw/native"
	default:
		return "", errors.New("Desktop native platform is unsupported")
	}
	return validateLadybugRoot(candidate)
}

func validateLadybugRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil || filepath.Clean(root) != filepath.Clean(abs) {
		return "", errors.New("Desktop native component root must be absolute")
	}
	manifestPath := filepath.Join(abs, "ladybug-native.json")
	manifestInfo, statErr := os.Lstat(manifestPath)
	if statErr != nil || !manifestInfo.Mode().IsRegular() || manifestInfo.Size() > 4096 {
		return "", errors.New("Desktop native component manifest is invalid")
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", errors.New("Desktop native component manifest is unavailable")
	}
	var manifest struct {
		SchemaVersion  int               `json:"schema_version"`
		LadybugVersion string            `json:"ladybug_version"`
		Platform       string            `json:"platform"`
		Files          map[string]string `json:"files"`
	}
	if decodeExactBytes(manifestBytes, &manifest) != nil || manifest.SchemaVersion != 1 || manifest.LadybugVersion != "0.17.0" || manifest.Platform != packagedNativePlatform() || len(manifest.Files) != 3 {
		return "", errors.New("Desktop native component manifest is invalid")
	}
	for _, name := range []string{"libfts.lbug_extension", "libvector.lbug_extension", "libalgo.lbug_extension"} {
		info, statErr := os.Lstat(filepath.Join(abs, name))
		if statErr != nil || !info.Mode().IsRegular() {
			return "", errors.New("Desktop native component bundle is incomplete")
		}
		digest, digestErr := nativeFileDigest(filepath.Join(abs, name))
		if digestErr != nil || manifest.Files[name] != digest {
			return "", errors.New("Desktop native component digest mismatch")
		}
	}
	return abs, nil
}

func packagedNativePlatform() string {
	return runtime.GOOS + "-" + map[string]string{"amd64": "amd64", "arm64": "arm64"}[runtime.GOARCH]
}

func nativeFileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
