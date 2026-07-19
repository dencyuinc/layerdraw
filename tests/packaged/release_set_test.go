// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package packaged_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	transport "github.com/dencyuinc/layerdraw/internal/transport/stdio"
)

func TestFixedReleaseSetNativeUsesVerifiedSidecarIdentity(t *testing.T) {
	root := os.Getenv("LAYERDRAW_RELEASE_SET_DIR")
	if root == "" {
		t.Skip("LAYERDRAW_RELEASE_SET_DIR is not set")
	}
	manifestBytes, err := os.ReadFile(filepath.Join(root, "layerdraw-release-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		ReleaseVersion string `json:"release_version"`
		Artifacts      []struct {
			ArtifactID string `json:"artifact_id"`
			Path       string `json:"path"`
			Legal      struct {
				SPDX struct {
					Path string `json:"path"`
				} `json:"spdx"`
				CycloneDX struct {
					Path string `json:"path"`
				} `json:"cyclonedx"`
				Notices struct {
					Path string `json:"path"`
				} `json:"third_party_notices"`
			} `json:"legal"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ReleaseVersion == "" || len(manifest.Artifacts) != 5 {
		t.Fatalf("incomplete fixed release manifest: %+v", manifest)
	}
	for _, artifact := range manifest.Artifacts {
		for _, relative := range []string{artifact.Path, artifact.Legal.SPDX.Path, artifact.Legal.CycloneDX.Path, artifact.Legal.Notices.Path} {
			if data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative))); err != nil || len(data) == 0 {
				t.Fatalf("%s release file %s: bytes=%d err=%v", artifact.ArtifactID, relative, len(data), err)
			}
		}
	}
	var desktopArchive string
	for _, artifact := range manifest.Artifacts {
		if artifact.ArtifactID == "layerdraw-host-native" {
			desktopArchive = filepath.Join(root, filepath.FromSlash(artifact.Path))
		}
	}
	if desktopArchive == "" {
		t.Fatal("Desktop native artifact is absent")
	}
	desktopRoot := t.TempDir()
	extractDesktopNative(t, desktopArchive, desktopRoot)
	desktopCommand := exec.Command(
		filepath.Join(desktopRoot, "layerdraw-host-native"),
		"native-search-check",
		"--database", filepath.Join(t.TempDir(), "offline-search.lbug"),
		"--fts-extension", filepath.Join(desktopRoot, "libfts.lbug_extension"),
	)
	desktopCommand.Env = []string{"HOME=" + t.TempDir(), "HTTP_PROXY=http://127.0.0.1:1", "HTTPS_PROXY=http://127.0.0.1:1", "NO_PROXY=*"}
	if output, err := desktopCommand.CombinedOutput(); err != nil || !bytes.Contains(output, []byte("ladybug 0.17.0 fts loaded")) {
		t.Fatalf("offline Desktop native check: output=%q err=%v", output, err)
	}

	binary := filepath.Join(root, "layerdraw-engine")
	versionOutput, err := exec.Command(binary, "--version").CombinedOutput()
	if err != nil || !bytes.Contains(versionOutput, []byte("layerdraw-engine "+manifest.ReleaseVersion+" ")) {
		t.Fatalf("release binary version: output=%q err=%v", versionOutput, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "stdio")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	encoder, decoder := transport.NewEncoder(stdin), transport.NewDecoder(stdout)
	request, err := engineprotocol.EncodeHandshakeRequestEnvelope(packagedHandshake())
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindRequestControl, StreamID: 1, Payload: request}); err != nil {
		t.Fatal(err)
	}
	frame := packagedReadFrame(t, decoder)
	response, err := engineprotocol.DecodeHandshakeResponseEnvelope(frame.Payload)
	if err != nil || response.Outcome != protocolcommon.OutcomeSuccess || response.Payload == nil {
		t.Fatalf("release handshake=%+v err=%v", response, err)
	}
	digest := sha256.Sum256(manifestBytes)
	wantDigest := protocolcommon.Digest("sha256:" + hex.EncodeToString(digest[:]))
	if response.Payload.ReleaseManifestDigest != wantDigest || string(response.Payload.HostRelease) != manifest.ReleaseVersion {
		t.Fatalf("release authority=%+v want digest=%s version=%s", response.Payload, wantDigest, manifest.ReleaseVersion)
	}
	packagedReadFrame(t, decoder)
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindClose}); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	if err := command.Wait(); err != nil || stderr.Len() != 0 {
		t.Fatalf("release binary exit=%v stderr=%q", err, stderr.String())
	}
}

func extractDesktopNative(t *testing.T, archivePath, destination string) {
	t.Helper()
	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	archive := tar.NewReader(compressed)
	allowed := map[string]bool{"layerdraw-host-native": true, "libfts.lbug_extension": true, "ladybug-native.json": true, "LICENSE": true, "NOTICE": true, "LICENSING.md": true, "THIRD_PARTY_NOTICES.txt": true}
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil || !allowed[header.Name] || header.Typeflag != tar.TypeReg {
			t.Fatalf("invalid Desktop native archive entry %q: %v", header.Name, err)
		}
		data, err := io.ReadAll(io.LimitReader(archive, 256<<20))
		if err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if header.Name == "layerdraw-host-native" {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(destination, header.Name), data, mode); err != nil {
			t.Fatal(err)
		}
	}
}
