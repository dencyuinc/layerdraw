// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package packaged_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	if manifest.ReleaseVersion == "" || len(manifest.Artifacts) != 4 {
		t.Fatalf("incomplete fixed release manifest: %+v", manifest)
	}
	for _, artifact := range manifest.Artifacts {
		for _, relative := range []string{artifact.Path, artifact.Legal.SPDX.Path, artifact.Legal.CycloneDX.Path, artifact.Legal.Notices.Path} {
			if data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative))); err != nil || len(data) == 0 {
				t.Fatalf("%s release file %s: bytes=%d err=%v", artifact.ArtifactID, relative, len(data), err)
			}
		}
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
