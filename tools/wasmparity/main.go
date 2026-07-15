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
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
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
	RequiredFeatures      []string     `json:"required_features"`
	Normalization         []string     `json:"normalization"`
	Cases                 []parityCase `json:"cases"`
}

type parityCase struct {
	Name      string         `json:"name"`
	Features  []string       `json:"features"`
	Execution string         `json:"execution"`
	Request   parityRequest  `json:"request"`
	Expected  parityExpected `json:"expected"`
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
	Outcome  string             `json:"outcome"`
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
		SourceRevision:        "unknown",
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
	result := parityCorpus{
		SchemaVersion:         1,
		EngineReleaseVariable: engineReleaseVariable,
		RequiredFeatures: []string{
			"asset", "cancellation", "deterministic_rejection", "installed_pack", "large_graph",
			"multi_module", "project", "resource_limit", "root_pack", "all_declarations",
		},
		Normalization: []string{
			"engine_release is replaced by $engine_release because each artifact reports its linked release",
			"cancellation compares the normalized cancelled outcome because hard-cancel transports publish no Engine payload",
			"public clients bind output bytes by blob_id because their API orders blobs by response references rather than transport publication order",
		},
		Cases: make([]parityCase, len(inputs)),
	}
	for index, input := range inputs {
		slices.SortFunc(input.blobs, func(left, right parityInputBlob) int {
			return strings.Compare(left.BlobID, right.BlobID)
		})
		control, err := engineprotocol.EncodeCompileRequestEnvelope(input.request)
		if err != nil {
			return parityCorpus{}, fmt.Errorf("encode %s request: %w", input.name, err)
		}
		request, err := engineprotocol.DecodeCompileRequestEnvelope(control)
		if err != nil {
			return parityCorpus{}, fmt.Errorf("decode %s request: %w", input.name, err)
		}
		sink := &memoryBlobSink{}
		dispatchContext := context.Background()
		if input.execution == "cancel" {
			cancelled, cancel := context.WithCancel(dispatchContext)
			cancel()
			dispatchContext = cancelled
		}
		response, err := authority.Dispatcher.DispatchCompile(dispatchContext, negotiated, request, &memoryBlobSource{blobs: input.blobs}, sink)
		if err != nil {
			return parityCorpus{}, fmt.Errorf("dispatch %s: %w", input.name, err)
		}
		switch response.Outcome {
		case protocolcommon.OutcomeSuccess:
			if response.Payload == nil || len(response.Diagnostics) != 0 || response.Payload.DefinitionHash == "" || len(response.Payload.SubjectSemanticHashes) == 0 || len(sink.blobs) == 0 {
				return parityCorpus{}, fmt.Errorf("%s did not produce a complete successful semantic response", input.name)
			}
		case protocolcommon.OutcomeRejected:
			if response.Payload != nil || len(response.Diagnostics) == 0 || len(sink.blobs) != 0 {
				return parityCorpus{}, fmt.Errorf("%s did not produce a closed rejection", input.name)
			}
		case protocolcommon.OutcomeFailed, protocolcommon.OutcomeCancelled:
			if response.Payload != nil || response.Failure == nil || len(sink.blobs) != 0 {
				return parityCorpus{}, fmt.Errorf("%s did not produce a closed terminal failure", input.name)
			}
		default:
			return parityCorpus{}, fmt.Errorf("%s produced unsupported outcome %q", input.name, response.Outcome)
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
			Name:      input.name,
			Features:  slices.Clone(input.features),
			Execution: input.execution,
			Request: parityRequest{
				ControlBase64: base64.StdEncoding.EncodeToString(control),
				Blobs:         publicInputs,
			},
			Expected: parityExpected{Outcome: string(response.Outcome), Response: normalizedResponse, Blobs: outputs},
		}
	}
	if err := validateCoverage(result); err != nil {
		return parityCorpus{}, err
	}
	return result, nil
}

func validateCoverage(corpus parityCorpus) error {
	covered := map[string]bool{}
	for _, test := range corpus.Cases {
		if test.Execution != "compile" && test.Execution != "cancel" {
			return fmt.Errorf("%s has unsupported execution %q", test.Name, test.Execution)
		}
		for _, feature := range test.Features {
			covered[feature] = true
		}
	}
	for _, feature := range corpus.RequiredFeatures {
		if !covered[feature] {
			return fmt.Errorf("portable corpus does not cover %q", feature)
		}
	}
	return nil
}

type compileCase struct {
	name      string
	features  []string
	execution string
	request   engineprotocol.CompileRequestEnvelope
	blobs     []parityInputBlob
}

func compileCases() ([]compileCase, error) {
	limit := protocolcommon.CanonicalNonNegativeInt64("1")
	large := largeGraphSource(128)
	cases := []compileCase{
		projectCase("single_module_project", "document.ldl", map[string][]byte{
			"document.ldl": []byte("project p \"Project\" {}\nentity_type service \"Service\" {\n  representation shape rect\n}\n"),
		}, []string{"project"}, nil, engineprotocol.ResourceLimits{}),
		projectCase("multi_module_project", "document.ldl", map[string][]byte{
			"document.ldl": []byte("import { service } from \"./types.ldl\"\nproject p \"Project\" {}\n"),
			"types.ldl":    []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nexport { service }\n"),
		}, []string{"project", "multi_module"}, nil, engineprotocol.ResourceLimits{}),
		projectCase("asset_project", "document.ldl", map[string][]byte{
			"document.ldl": []byte(assetFixture),
		}, []string{"project", "asset"}, []assetFixtureValue{{locator: "icon.png", mediaType: "image/png", bytes: testPNG}}, engineprotocol.ResourceLimits{}),
		projectCase("all_declarations_project", "document.ldl", map[string][]byte{
			"document.ldl": []byte(allDeclarationsFixture),
		}, []string{"project", "all_declarations"}, nil, engineprotocol.ResourceLimits{}),
		projectCase("deterministic_rejection", "document.ldl", map[string][]byte{
			"document.ldl": []byte("project p \"Project\" {}\nentity_type duplicate \"B\" {}\nentity_type duplicate \"C\" {}\n"),
			"z.ldl":        []byte("entity_type duplicate \"A\" {}\n"),
		}, []string{"project", "multi_module", "deterministic_rejection"}, nil, engineprotocol.ResourceLimits{}),
		projectCase("resource_limit_rejection", "document.ldl", map[string][]byte{
			"document.ldl": []byte("project p \"Project\" {}\nentity_type first \"First\" {}\nentity_type second \"Second\" {}\n"),
		}, []string{"project", "resource_limit"}, nil, engineprotocol.ResourceLimits{MaxDeclarations: &limit}),
		projectCase("representative_large_graph", "document.ldl", map[string][]byte{
			"document.ldl": []byte(large),
		}, []string{"project", "large_graph"}, nil, engineprotocol.ResourceLimits{}),
	}
	installed, root, err := packCases()
	if err != nil {
		return nil, err
	}
	cases = slices.Insert(cases, 2, installed, root)
	cancelled := projectCase("cancellation", "document.ldl", map[string][]byte{
		"document.ldl": []byte(large),
	}, []string{"project", "cancellation"}, nil, engineprotocol.ResourceLimits{})
	cancelled.execution = "cancel"
	cases = append(cases, cancelled)
	return cases, nil
}

type assetFixtureValue struct {
	locator   string
	mediaType string
	bytes     []byte
}

func projectCase(name, entry string, files map[string][]byte, features []string, assets []assetFixtureValue, limits engineprotocol.ResourceLimits) compileCase {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	sources := make([]engineprotocol.SourceFileInput, 0, len(paths))
	blobs := make([]parityInputBlob, 0, len(paths)+len(assets))
	for _, path := range paths {
		value := files[path]
		ref := blobRef("project-"+strings.NewReplacer("/", "-", ".", "-").Replace(path), "text/plain; charset=utf-8", value)
		sources = append(sources, engineprotocol.SourceFileInput{Path: engineprotocol.CanonicalSourcePath(path), Blob: ref})
		blobs = append(blobs, parityInputBlob{BlobID: ref.BlobID, MediaType: ref.MediaType, bytes: value})
	}
	assetInputs := make([]engineprotocol.AssetInput, 0, len(assets))
	for index, asset := range assets {
		ref := blobRef(fmt.Sprintf("project-asset-%d", index), asset.mediaType, asset.bytes)
		assetInputs = append(assetInputs, engineprotocol.AssetInput{
			Blob: ref, Digest: ref.Digest, Locator: asset.locator, MediaType: asset.mediaType, Origin: engineprotocol.SourceOriginKindProject,
		})
		blobs = append(blobs, parityInputBlob{BlobID: ref.BlobID, MediaType: ref.MediaType, bytes: asset.bytes})
	}
	return compileCase{
		name: name, features: features, execution: "compile",
		request: engineprotocol.CompileRequestEnvelope{
			Operation: engineprotocol.CompileRequestEnvelopeOperationValue,
			Payload: engineprotocol.CompileInput{
				EntryPath: engineprotocol.CanonicalSourcePath(entry), InstalledPackTree: []engineprotocol.SourceFileInput{}, Mode: engineprotocol.CompileModeProject,
				ProjectSourceTree: sources, ReferencedAssets: assetInputs, ResolvedDependencies: emptyDependencies(), ResourceLimits: portableResourceLimits(limits),
			},
			Protocol: engineProtocolRef(), RequestID: "parity-" + name + "-request",
		},
		blobs: blobs,
	}
}

func packCases() (compileCase, compileCase, error) {
	packSource := []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nexport { service }\n")
	projectSource := []byte("import { service } from \"schema\"\nproject p \"Project\" {}\n")
	manifest, err := json.Marshal(map[string]any{
		"format": "layerdraw-pack", "format_version": 1, "id": "pub/schema", "name": "schema", "version": "1.0.0", "language": 1, "entry": "pack.ldl", "dependencies": map[string]any{},
	})
	if err != nil {
		return compileCase{}, compileCase{}, err
	}
	packSourceRef := blobRef("pack-source", "text/plain; charset=utf-8", packSource)
	projectSourceRef := blobRef("installed-pack-project-source", "text/plain; charset=utf-8", projectSource)
	manifestRef := blobRef("pack-manifest", "application/json", manifest)
	rootPackID := engineprotocol.CanonicalPackSelector("pub/schema")
	dependencies := emptyDependencies()
	dependencies.Installs = []engineprotocol.ResolvedPack{{
		InstallName: "schema", CanonicalID: rootPackID, Version: "1.0.0", Digest: protocolcommon.Digest("sha256:" + strings.Repeat("a", 64)),
		Path: "pack/schema", Entry: "pack.ldl", Files: []engineprotocol.ResolvedPackFile{{Path: "pack.ldl", Digest: packSourceRef.Digest}},
		Dependencies: []engineprotocol.ResolvedPackDependency{}, ManifestPath: "manifest.json", Manifest: manifestRef,
	}}
	commonBlobs := []parityInputBlob{
		{BlobID: manifestRef.BlobID, MediaType: manifestRef.MediaType, bytes: manifest},
		{BlobID: packSourceRef.BlobID, MediaType: packSourceRef.MediaType, bytes: packSource},
	}
	installed := compileCase{
		name: "installed_pack_project", features: []string{"project", "installed_pack"}, execution: "compile",
		request: engineprotocol.CompileRequestEnvelope{
			Operation: engineprotocol.CompileRequestEnvelopeOperationValue,
			Payload: engineprotocol.CompileInput{
				EntryPath: "document.ldl", InstalledPackTree: []engineprotocol.SourceFileInput{{Path: "pack/schema/pack.ldl", Blob: packSourceRef}}, Mode: engineprotocol.CompileModeProject,
				ProjectSourceTree: []engineprotocol.SourceFileInput{{Path: "document.ldl", Blob: projectSourceRef}}, ReferencedAssets: []engineprotocol.AssetInput{},
				ResolvedDependencies: dependencies, ResourceLimits: portableResourceLimits(engineprotocol.ResourceLimits{}),
			},
			Protocol: engineProtocolRef(), RequestID: "parity-installed-pack-project-request",
		},
		blobs: append(slices.Clone(commonBlobs), parityInputBlob{BlobID: projectSourceRef.BlobID, MediaType: projectSourceRef.MediaType, bytes: projectSource}),
	}
	root := compileCase{
		name: "root_pack", features: []string{"root_pack"}, execution: "compile",
		request: engineprotocol.CompileRequestEnvelope{
			Operation: engineprotocol.CompileRequestEnvelopeOperationValue,
			Payload: engineprotocol.CompileInput{
				EntryPath: "pack.ldl", InstalledPackTree: []engineprotocol.SourceFileInput{{Path: "pack/schema/pack.ldl", Blob: packSourceRef}}, Mode: engineprotocol.CompileModePack,
				ProjectSourceTree: []engineprotocol.SourceFileInput{}, ReferencedAssets: []engineprotocol.AssetInput{}, ResolvedDependencies: dependencies,
				ResourceLimits: portableResourceLimits(engineprotocol.ResourceLimits{}), RootPackID: &rootPackID,
			},
			Protocol: engineProtocolRef(), RequestID: "parity-root-pack-request",
		},
		blobs: slices.Clone(commonBlobs),
	}
	return installed, root, nil
}

func largeGraphSource(count int) string {
	var source strings.Builder
	source.WriteString("project p \"Project\" {}\nlayers {\n app \"Application\" @1\n}\nentity_type service \"Service\" {\n representation shape rect\n}\n")
	source.WriteString("relation_type link \"Link\" dependency {\n duplicate_policy allow\n from source types [service] layers [app]\n to target types [service] layers [app]\n label \"links\"\n}\nentities service @app {\n")
	for index := range count {
		fmt.Fprintf(&source, "n%03d \"Node %03d\"\n", index, index)
	}
	source.WriteString("}\nrelations link {\n")
	for index := 0; index < count-1; index++ {
		fmt.Fprintf(&source, "r%03d: n%03d -> n%03d\n", index, index, index+1)
	}
	source.WriteString("}\n")
	return source.String()
}

func portableResourceLimits(overrides engineprotocol.ResourceLimits) engineprotocol.ResourceLimits {
	value := func(number int64) *protocolcommon.CanonicalNonNegativeInt64 {
		result := protocolcommon.CanonicalNonNegativeInt64(strconv.FormatInt(number, 10))
		return &result
	}
	portable := wasmtransport.BrowserCompilerLimitPolicy().Defaults
	result := engineprotocol.ResourceLimits{
		MaxProjectSourceFiles: value(portable.MaxProjectSourceFiles),
		MaxProjectSourceBytes: value(portable.MaxProjectSourceBytes),
		MaxPackFiles:          value(portable.MaxPackFiles),
		MaxPackBytes:          value(portable.MaxPackBytes),
		MaxAssets:             value(portable.MaxAssets),
		MaxAssetBytes:         value(portable.MaxAssetBytes),
		MaxRasterDimension:    value(portable.MaxRasterDimension),
		MaxRasterPixels:       value(portable.MaxRasterPixels),
		MaxDeclarations:       value(portable.MaxDeclarations),
	}
	if overrides.MaxProjectSourceFiles != nil {
		result.MaxProjectSourceFiles = overrides.MaxProjectSourceFiles
	}
	if overrides.MaxProjectSourceBytes != nil {
		result.MaxProjectSourceBytes = overrides.MaxProjectSourceBytes
	}
	if overrides.MaxPackFiles != nil {
		result.MaxPackFiles = overrides.MaxPackFiles
	}
	if overrides.MaxPackBytes != nil {
		result.MaxPackBytes = overrides.MaxPackBytes
	}
	if overrides.MaxAssets != nil {
		result.MaxAssets = overrides.MaxAssets
	}
	if overrides.MaxAssetBytes != nil {
		result.MaxAssetBytes = overrides.MaxAssetBytes
	}
	if overrides.MaxRasterDimension != nil {
		result.MaxRasterDimension = overrides.MaxRasterDimension
	}
	if overrides.MaxRasterPixels != nil {
		result.MaxRasterPixels = overrides.MaxRasterPixels
	}
	if overrides.MaxDeclarations != nil {
		result.MaxDeclarations = overrides.MaxDeclarations
	}
	return result
}

var testPNG = validTestPNG()

func validTestPNG() []byte {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		panic(err)
	}
	return encoded.Bytes()
}

const assetFixture = `project p "Project" {}
entity_type service "Service" {
  image "icon.png"
  representation shape rect
}
`

const allDeclarationsFixture = `project p "Project" {
  description "Root description"
}
layers {
  app "Application" @10
}
entity_type service "Service" {
  representation shape rect
  columns {
    environment "Environment" enum [prod, dev] required default prod
    note "Note" string
  }
  unique env_unique [environment]
}
relation_type link "Link" dependency {
  duplicate_policy allow
  from source types [service] layers [app]
  to target types [service] layers [app]
  label "links"
  columns {
    weight "Weight" number
  }
}
entities service @app {
  alpha "Alpha"
  beta "Beta"
}
rows service [environment, note] {
  alpha primary: prod, "api"
}
relations link {
  alpha_beta: alpha -> beta "Alpha to Beta"
}
relation_rows link [weight] {
  alpha_beta primary: 1.5
}
query scope "Scope" {
  parameters {
    environment enum [prod, dev] default prod
  }
  select {
    entity_types [service]
    relation_types [link]
    roots [alpha]
  }
  result [seed_entities, induced_relations]
}
view inventory "Inventory" inventory {
  source query scope {}
  table {
    rows entity_rows
    entity_types [service]
    entity_id
    column environment {
      source attribute environment entity_types [service]
    }
  }
  export data json "inventory.json" {
    fidelity lossless
    source_refs
    diagnostics
  }
}
reference guide <<-TEXT
Use the graph as the source of truth.
TEXT
`

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
