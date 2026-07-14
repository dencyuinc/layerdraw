// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Command wasmparity generates the transport-neutral Project/Pack parity
// corpus from the canonical in-process Go dispatcher.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	wasmtransport "github.com/dencyuinc/layerdraw/internal/transport/wasm"
)

const (
	engineReleaseVariable = "$engine_release"
	releaseManifestDigest = "sha256:5555555555555555555555555555555555555555555555555555555555555555"
)

type parityCorpus struct {
	SchemaVersion         int          `json:"schema_version"`
	EngineReleaseVariable string       `json:"engine_release_variable"`
	Cases                 []parityCase `json:"cases"`
}

type parityCase struct {
	Name     string         `json:"name"`
	Request  parityRequest  `json:"request"`
	Expected parityExpected `json:"expected"`
}

type parityRequest struct {
	ControlBase64 string            `json:"control_base64"`
	Blobs         []parityInputBlob `json:"blobs"`
}

type parityInputBlob struct {
	BlobID      string `json:"blob_id"`
	MediaType   string `json:"media_type"`
	BytesBase64 string `json:"bytes_base64"`
	bytes       []byte
}

type parityExpected struct {
	Response json.RawMessage    `json:"response"`
	Blobs    []parityOutputBlob `json:"blobs"`
}

type parityOutputBlob struct {
	BlobID      string `json:"blob_id"`
	Lifetime    string `json:"lifetime"`
	MediaType   string `json:"media_type"`
	Size        string `json:"size"`
	Digest      string `json:"digest"`
	BytesBase64 string `json:"bytes_base64"`
}

type memoryBlobSource struct {
	blobs []parityInputBlob
}

func (source *memoryBlobSource) Definitions(context.Context) ([]endpoint.BlobDefinition, error) {
	definitions := make([]endpoint.BlobDefinition, len(source.blobs))
	for index, blob := range source.blobs {
		definitions[index] = endpoint.BlobDefinition{
			BlobID: blob.BlobID,
			Owned:  &endpoint.OwnedBlob{Bytes: slices.Clone(blob.bytes), Release: func() {}},
		}
	}
	return definitions, nil
}

type memoryBlobSink struct {
	blobs []endpoint.OutputBlob
}

func (sink *memoryBlobSink) Publish(_ context.Context, blobs []endpoint.OutputBlob) error {
	sink.blobs = make([]endpoint.OutputBlob, len(blobs))
	for index, blob := range blobs {
		sink.blobs[index] = endpoint.OutputBlob{Ref: blob.Ref, Bytes: slices.Clone(blob.Bytes)}
	}
	return nil
}

func main() {
	output := flag.String("output", "tests/conformance/testdata/engine_compile_parity_v1.json", "generated corpus output")
	flag.Parse()
	if err := generate(*output); err != nil {
		fmt.Fprintln(os.Stderr, "wasmparity:", err)
		os.Exit(1)
	}
}

func generate(output string) error {
	corpus, err := buildCorpus()
	if err != nil {
		return err
	}
	data, err := canonicalCorpus(corpus)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	return os.WriteFile(output, data, 0o644)
}

func canonicalCorpus(corpus parityCorpus) ([]byte, error) {
	data, err := json.Marshal(corpus)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func buildCorpus() (parityCorpus, error) {
	authority, err := endpoint.NewCompilerEndpoint(endpoint.CompilerEndpointConfig{
		EngineRelease:         "0.0.0",
		SourceRevision:        engine.UnknownSourceRevision,
		ReleaseManifestDigest: releaseManifestDigest,
		EndpointInstanceID:    "parity-in-process",
		Transports:            []string{endpoint.TransportInProcess},
		Limits:                wasmtransport.BrowserCompilerLimitPolicy(),
	})
	if err != nil {
		return parityCorpus{}, err
	}
	handshake := engineprotocol.HandshakeRequestEnvelope{
		Operation: engineprotocol.HandshakeRequestEnvelopeOperationValue,
		Payload: protocolcommon.HandshakeRequest{
			ClientRelease:        "0.0.0",
			OptionalCapabilities: []protocolcommon.CapabilityID{},
			Protocols: []protocolcommon.ProtocolOffer{{
				Name:           endpoint.ProtocolName,
				SupportedRange: "1.0..1.0",
				Versions: []protocolcommon.ProtocolVersionBinding{{
					Version:      endpoint.ProtocolVersion,
					SchemaDigest: protocolcommon.Digest(engineprotocol.SchemaDigest),
				}},
			}},
			RequiredCapabilities: []protocolcommon.CapabilityID{endpoint.OperationCompile},
		},
		Protocol:  engineProtocolRef(),
		RequestID: "parity-handshake",
	}
	handshakeResponse, negotiated, err := authority.Descriptor.Negotiate(context.Background(), handshake)
	if err != nil || negotiated == nil || handshakeResponse.Outcome != protocolcommon.OutcomeSuccess {
		return parityCorpus{}, fmt.Errorf("negotiate parity dispatcher: outcome=%s err=%w", handshakeResponse.Outcome, err)
	}

	inputs, err := compileCases()
	if err != nil {
		return parityCorpus{}, err
	}
	result := parityCorpus{SchemaVersion: 1, EngineReleaseVariable: engineReleaseVariable, Cases: make([]parityCase, len(inputs))}
	for index, input := range inputs {
		control, err := engineprotocol.EncodeCompileRequestEnvelope(input.request)
		if err != nil {
			return parityCorpus{}, fmt.Errorf("encode %s request: %w", input.name, err)
		}
		request, err := engineprotocol.DecodeCompileRequestEnvelope(control)
		if err != nil {
			return parityCorpus{}, fmt.Errorf("decode %s request: %w", input.name, err)
		}
		sink := &memoryBlobSink{}
		response, err := authority.Dispatcher.DispatchCompile(context.Background(), negotiated, request, &memoryBlobSource{blobs: input.blobs}, sink)
		if err != nil {
			return parityCorpus{}, fmt.Errorf("dispatch %s: %w", input.name, err)
		}
		if response.Outcome != protocolcommon.OutcomeSuccess || response.Payload == nil || len(response.Diagnostics) != 0 || response.Payload.DefinitionHash == "" || len(response.Payload.SubjectSemanticHashes) == 0 {
			return parityCorpus{}, fmt.Errorf("%s did not produce a complete successful semantic response", input.name)
		}
		responseBytes, err := engineprotocol.EncodeCompileResponseEnvelope(response)
		if err != nil {
			return parityCorpus{}, fmt.Errorf("encode %s response: %w", input.name, err)
		}
		var semantics map[string]any
		if err := json.Unmarshal(responseBytes, &semantics); err != nil {
			return parityCorpus{}, err
		}
		semantics["engine_release"] = engineReleaseVariable
		normalizedResponse, err := json.Marshal(semantics)
		if err != nil {
			return parityCorpus{}, err
		}
		outputs := make([]parityOutputBlob, len(sink.blobs))
		for outputIndex, blob := range sink.blobs {
			outputs[outputIndex] = parityOutputBlob{
				BlobID:      blob.Ref.BlobID,
				Lifetime:    string(blob.Ref.Lifetime),
				MediaType:   blob.Ref.MediaType,
				Size:        string(blob.Ref.Size),
				Digest:      string(blob.Ref.Digest),
				BytesBase64: base64.StdEncoding.EncodeToString(blob.Bytes),
			}
		}
		publicInputs := make([]parityInputBlob, len(input.blobs))
		for blobIndex, blob := range input.blobs {
			publicInputs[blobIndex] = parityInputBlob{BlobID: blob.BlobID, MediaType: blob.MediaType, BytesBase64: base64.StdEncoding.EncodeToString(blob.bytes)}
		}
		result.Cases[index] = parityCase{
			Name: input.name,
			Request: parityRequest{
				ControlBase64: base64.StdEncoding.EncodeToString(control),
				Blobs:         publicInputs,
			},
			Expected: parityExpected{Response: normalizedResponse, Blobs: outputs},
		}
	}
	return result, nil
}

type compileCase struct {
	name    string
	request engineprotocol.CompileRequestEnvelope
	blobs   []parityInputBlob
}

func compileCases() ([]compileCase, error) {
	projectSource := []byte("project p \"Project\" {}\nentity_type service \"Service\" {\n  representation shape rect\n}\n")
	projectRef := blobRef("project-source", "text/plain; charset=utf-8", projectSource)
	project := compileCase{
		name: "canonical_project",
		request: engineprotocol.CompileRequestEnvelope{
			Operation: engineprotocol.CompileRequestEnvelopeOperationValue,
			Payload: engineprotocol.CompileInput{
				EntryPath: "main.ldl", InstalledPackTree: []engineprotocol.SourceFileInput{}, Mode: engineprotocol.CompileModeProject,
				ProjectSourceTree: []engineprotocol.SourceFileInput{{Path: "main.ldl", Blob: projectRef}}, ReferencedAssets: []engineprotocol.AssetInput{},
				ResolvedDependencies: emptyDependencies(), ResourceLimits: engineprotocol.ResourceLimits{},
			},
			Protocol: engineProtocolRef(), RequestID: "parity-project-request",
		},
		blobs: []parityInputBlob{{BlobID: projectRef.BlobID, MediaType: projectRef.MediaType, bytes: projectSource}},
	}

	packSource := []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nexport { service }\n")
	manifest, err := json.Marshal(map[string]any{
		"format": "layerdraw-pack", "format_version": 1, "id": "pub/schema", "name": "schema", "version": "1.0.0", "language": 1, "entry": "pack.ldl", "dependencies": map[string]any{},
	})
	if err != nil {
		return nil, err
	}
	packSourceRef := blobRef("pack-source", "text/plain; charset=utf-8", packSource)
	manifestRef := blobRef("pack-manifest", "application/json", manifest)
	rootPackID := engineprotocol.CanonicalPackSelector("pub/schema")
	packDependencies := emptyDependencies()
	packDependencies.Installs = []engineprotocol.ResolvedPack{{
		InstallName: "schema", CanonicalID: rootPackID, Version: "1.0.0", Digest: protocolcommon.Digest("sha256:" + strings.Repeat("a", 64)),
		Path: "pack/schema", Entry: "pack.ldl", Files: []engineprotocol.ResolvedPackFile{{Path: "pack.ldl", Digest: packSourceRef.Digest}},
		Dependencies: []engineprotocol.ResolvedPackDependency{}, ManifestPath: "manifest.json", Manifest: manifestRef,
	}}
	pack := compileCase{
		name: "canonical_root_pack",
		request: engineprotocol.CompileRequestEnvelope{
			Operation: engineprotocol.CompileRequestEnvelopeOperationValue,
			Payload: engineprotocol.CompileInput{
				EntryPath: "pack.ldl", InstalledPackTree: []engineprotocol.SourceFileInput{{Path: "pack/schema/pack.ldl", Blob: packSourceRef}}, Mode: engineprotocol.CompileModePack,
				ProjectSourceTree: []engineprotocol.SourceFileInput{}, ReferencedAssets: []engineprotocol.AssetInput{}, ResolvedDependencies: packDependencies,
				ResourceLimits: engineprotocol.ResourceLimits{}, RootPackID: &rootPackID,
			},
			Protocol: engineProtocolRef(), RequestID: "parity-pack-request",
		},
		blobs: []parityInputBlob{
			{BlobID: manifestRef.BlobID, MediaType: manifestRef.MediaType, bytes: manifest},
			{BlobID: packSourceRef.BlobID, MediaType: packSourceRef.MediaType, bytes: packSource},
		},
	}
	return []compileCase{project, pack}, nil
}

func engineProtocolRef() engineprotocol.EngineProtocolRef {
	return engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue}
}

func emptyDependencies() engineprotocol.ResolvedDependencies {
	return engineprotocol.ResolvedDependencies{Format: engineprotocol.ResolvedDependenciesFormatValue, FormatVersion: 1, Installs: []engineprotocol.ResolvedPack{}, Language: 1}
}

func blobRef(id, mediaType string, value []byte) protocolcommon.BlobRef {
	digest := sha256.Sum256(value)
	return protocolcommon.BlobRef{
		BlobID: id, Digest: protocolcommon.Digest("sha256:" + hex.EncodeToString(digest[:])), Lifetime: protocolcommon.BlobLifetimeRequest,
		MediaType: mediaType, Size: protocolcommon.CanonicalUint64(strconv.Itoa(len(value))),
	}
}

func corpusEqual(left, right parityCorpus) (bool, error) {
	leftBytes, err := canonicalCorpus(left)
	if err != nil {
		return false, err
	}
	rightBytes, err := canonicalCorpus(right)
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftBytes, rightBytes), nil
}
