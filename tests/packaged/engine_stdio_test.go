// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package packaged_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	transport "github.com/dencyuinc/layerdraw/internal/transport/stdio"
)

func TestPackagedEngineStdioHandshakeProjectAndPack(t *testing.T) {
	binary := os.Getenv("LAYERDRAW_ENGINE_BINARY")
	if binary == "" {
		t.Skip("LAYERDRAW_ENGINE_BINARY is not set")
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
	encoder := transport.NewEncoder(stdin)
	decoder := transport.NewDecoder(stdout)

	handshake := packagedHandshake()
	handshakeBytes, err := engineprotocol.EncodeHandshakeRequestEnvelope(handshake)
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindRequestControl, StreamID: 1, Payload: handshakeBytes}); err != nil {
		t.Fatal(err)
	}
	control := packagedReadFrame(t, decoder)
	response, err := engineprotocol.DecodeHandshakeResponseEnvelope(control.Payload)
	if err != nil || response.Outcome != protocolcommon.OutcomeSuccess || response.Payload == nil || response.Payload.EndpointInstanceID == "" {
		t.Fatalf("handshake = %+v, %v", response, err)
	}
	firstInstanceID := response.Payload.EndpointInstanceID
	bundle := os.Getenv("LAYERDRAW_BUNDLE_DIR")
	if bundle == "" {
		t.Fatal("LAYERDRAW_BUNDLE_DIR is not set for stdio artifact verification")
	}
	releaseManifestBytes, err := os.ReadFile(filepath.Join(bundle, "layerdraw-engine.release-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest := sha256.Sum256(releaseManifestBytes)
	if got, want := response.Payload.ReleaseManifestDigest, protocolcommon.Digest("sha256:"+hex.EncodeToString(manifestDigest[:])); got != want {
		t.Fatalf("release manifest digest = %q, want %q", got, want)
	}
	packagedReadFrame(t, decoder)

	projectSource := []byte("project p \"Project\" {}\n")
	projectRef := packagedBlobRef("project-source", "text/plain; charset=utf-8", projectSource)
	project := packagedBaseCompile("project", engineprotocol.CompileModeProject)
	project.Payload.EntryPath = "document.ldl"
	project.Payload.ProjectSourceTree = []engineprotocol.SourceFileInput{{Path: "document.ldl", Blob: projectRef}}
	projectResponse := packagedSendCompile(t, encoder, decoder, 2, project, []packagedBlob{{id: projectRef.BlobID, bytes: projectSource}})
	if projectResponse.Outcome != protocolcommon.OutcomeSuccess || projectResponse.Payload == nil || projectResponse.Payload.NormalizedArtifact.Project == nil {
		t.Fatalf("project response = %+v", projectResponse)
	}

	packSource := []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nexport { service }\n")
	manifest, err := json.Marshal(map[string]any{"format": "layerdraw-pack", "format_version": 1, "id": "pub/schema", "name": "schema", "version": "1.0.0", "language": 1, "entry": "pack.ldl", "dependencies": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	fileRef := packagedBlobRef("pack-file", "text/plain; charset=utf-8", packSource)
	manifestRef := packagedBlobRef("pack-manifest", "application/json", manifest)
	root := engineprotocol.CanonicalPackSelector("pub/schema")
	pack := packagedBaseCompile("pack", engineprotocol.CompileModePack)
	pack.Payload.EntryPath, pack.Payload.RootPackID = "pack.ldl", &root
	pack.Payload.InstalledPackTree = []engineprotocol.SourceFileInput{{Path: "pack/schema/pack.ldl", Blob: fileRef}}
	pack.Payload.ResolvedDependencies.Installs = []engineprotocol.ResolvedPack{{InstallName: "schema", CanonicalID: root, Version: "1.0.0", Digest: protocolcommon.Digest("sha256:" + strings.Repeat("a", 64)), Path: "pack/schema", Entry: "pack.ldl", Files: []engineprotocol.ResolvedPackFile{{Path: "pack.ldl", Digest: fileRef.Digest}}, Dependencies: []engineprotocol.ResolvedPackDependency{}, ManifestPath: "manifest.json", Manifest: manifestRef}}
	packResponse := packagedSendCompile(t, encoder, decoder, 3, pack, []packagedBlob{{id: fileRef.BlobID, bytes: packSource}, {id: manifestRef.BlobID, bytes: manifest}})
	if packResponse.Outcome != protocolcommon.OutcomeSuccess || packResponse.Payload == nil || packResponse.Payload.NormalizedArtifact.Pack == nil {
		t.Fatalf("pack response = %+v", packResponse)
	}

	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindClose}); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	if err := command.Wait(); err != nil {
		t.Fatalf("packaged stdio: %v, stderr=%q", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if secondInstanceID := packagedHandshakeInstanceID(t, binary); secondInstanceID == firstInstanceID {
		t.Fatalf("replacement reused endpoint instance ID %q", firstInstanceID)
	}
}

type packagedBlob struct {
	id    string
	bytes []byte
}

func packagedSendCompile(t *testing.T, encoder *transport.Encoder, decoder *transport.Decoder, streamID uint64, request engineprotocol.CompileRequestEnvelope, blobs []packagedBlob) engineprotocol.CompileResponseEnvelope {
	t.Helper()
	encoded, err := engineprotocol.EncodeCompileRequestEnvelope(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindRequestControl, StreamID: streamID, Payload: encoded}); err != nil {
		t.Fatal(err)
	}
	if ready := packagedReadFrame(t, decoder); ready.Kind != transport.KindRequestReady || ready.StreamID != streamID {
		t.Fatalf("ready = %#v", ready)
	}
	frames := make([]transport.Frame, 0, len(blobs)+1)
	for index, blob := range blobs {
		frames = append(frames, transport.Frame{Kind: transport.KindBlobChunk, Flags: transport.FlagFinal, StreamID: streamID, Sequence: uint32(index + 1), Name: []byte(blob.id), Payload: blob.bytes})
	}
	frames = append(frames, transport.Frame{Kind: transport.KindBundleEnd, StreamID: streamID, Sequence: uint32(len(blobs) + 1)})
	if err := encoder.WriteFrames(frames); err != nil {
		t.Fatal(err)
	}
	control := packagedReadFrame(t, decoder)
	response, err := engineprotocol.DecodeCompileResponseEnvelope(control.Payload)
	if err != nil {
		t.Fatal(err)
	}
	validator := transport.NewBundleValidator()
	for {
		frame := packagedReadFrame(t, decoder)
		if err := validator.Accept(frame); err != nil {
			t.Fatal(err)
		}
		if frame.Kind == transport.KindBundleEnd {
			break
		}
	}
	return response
}

func packagedHandshake() engineprotocol.HandshakeRequestEnvelope {
	return engineprotocol.HandshakeRequestEnvelope{
		Operation: engineprotocol.HandshakeRequestEnvelopeOperationValue,
		Payload: protocolcommon.HandshakeRequest{
			ClientRelease: "1.0.0", OptionalCapabilities: []protocolcommon.CapabilityID{},
			Protocols:            []protocolcommon.ProtocolOffer{{Name: endpoint.ProtocolName, SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: endpoint.ProtocolVersion, SchemaDigest: protocolcommon.Digest(engineprotocol.SchemaDigest)}}}},
			RequiredCapabilities: []protocolcommon.CapabilityID{endpoint.OperationCompile},
		},
		Protocol: engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue}, RequestID: "packaged-handshake",
	}
}

func packagedBaseCompile(requestID string, mode engineprotocol.CompileMode) engineprotocol.CompileRequestEnvelope {
	return engineprotocol.CompileRequestEnvelope{
		Operation: engineprotocol.CompileRequestEnvelopeOperationValue,
		Payload: engineprotocol.CompileInput{
			Mode: mode, ProjectSourceTree: []engineprotocol.SourceFileInput{}, InstalledPackTree: []engineprotocol.SourceFileInput{}, ReferencedAssets: []engineprotocol.AssetInput{},
			ResolvedDependencies: engineprotocol.ResolvedDependencies{Format: engineprotocol.ResolvedDependenciesFormatValue, FormatVersion: 1, Language: 1, Installs: []engineprotocol.ResolvedPack{}}, ResourceLimits: engineprotocol.ResourceLimits{},
		},
		Protocol: engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue}, RequestID: requestID,
	}
}

func packagedBlobRef(id, mediaType string, value []byte) protocolcommon.BlobRef {
	digest := sha256.Sum256(value)
	return protocolcommon.BlobRef{BlobID: id, Digest: protocolcommon.Digest("sha256:" + hex.EncodeToString(digest[:])), Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: mediaType, Size: protocolcommon.CanonicalUint64(strconv.Itoa(len(value)))}
}

func packagedReadFrame(t *testing.T, decoder *transport.Decoder) transport.Frame {
	t.Helper()
	frame, err := decoder.ReadFrame()
	if err != nil {
		if err == io.EOF {
			t.Fatal("unexpected EOF")
		}
		t.Fatal(err)
	}
	return frame
}

func packagedHandshakeInstanceID(t *testing.T, binary string) protocolcommon.EndpointInstanceID {
	t.Helper()
	command := exec.Command(binary, "stdio")
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
	request := packagedHandshake()
	request.RequestID = "replacement-handshake"
	encoded, err := engineprotocol.EncodeHandshakeRequestEnvelope(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.WriteFrames([]transport.Frame{{Kind: transport.KindRequestControl, StreamID: 1, Payload: encoded}, {Kind: transport.KindClose}}); err != nil {
		t.Fatal(err)
	}
	control := packagedReadFrame(t, decoder)
	response, err := engineprotocol.DecodeHandshakeResponseEnvelope(control.Payload)
	if err != nil || response.Payload == nil {
		t.Fatalf("replacement handshake = %+v, %v", response, err)
	}
	packagedReadFrame(t, decoder)
	_ = stdin.Close()
	if err := command.Wait(); err != nil || stderr.Len() != 0 {
		t.Fatalf("replacement process: %v, stderr=%q", err, stderr.String())
	}
	return response.Payload.EndpointInstanceID
}
