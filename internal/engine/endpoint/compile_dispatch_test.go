// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

type memoryBlobSource struct {
	definitions []BlobDefinition
	calls       int
}

func (source *memoryBlobSource) Definitions(context.Context) ([]BlobDefinition, error) {
	source.calls++
	return source.definitions, nil
}

type memoryBlobSink struct {
	blobs []OutputBlob
	calls int
	err   error
}

type callbackBlobSource struct {
	definitions []BlobDefinition
	callback    func()
}

func (source *callbackBlobSource) Definitions(context.Context) ([]BlobDefinition, error) {
	if source.callback != nil {
		source.callback()
	}
	return source.definitions, nil
}

type cancelDuringReadCloser struct {
	reader io.Reader
	cancel context.CancelFunc
	once   sync.Once
}

func (reader *cancelDuringReadCloser) Read(buffer []byte) (int, error) {
	count, err := reader.reader.Read(buffer[:min(len(buffer), 1)])
	reader.once.Do(reader.cancel)
	return count, err
}

func (*cancelDuringReadCloser) Close() error { return nil }

type cancelDuringPublishSink struct {
	cancel context.CancelFunc
	calls  int
}

type trackingReadCloser struct {
	reader io.Reader
	closes int
}

func (reader *trackingReadCloser) Read(buffer []byte) (int, error) {
	return reader.reader.Read(buffer)
}

func (reader *trackingReadCloser) Close() error {
	reader.closes++
	return nil
}

func (sink *cancelDuringPublishSink) Publish(ctx context.Context, _ []OutputBlob) error {
	sink.calls++
	sink.cancel()
	return ctx.Err()
}

func (sink *memoryBlobSink) Publish(_ context.Context, blobs []OutputBlob) error {
	sink.calls++
	if sink.err != nil {
		return sink.err
	}
	sink.blobs = cloneOutputBlobs(blobs)
	return nil
}

func compileContext(t *testing.T) *NegotiatedContext {
	t.Helper()
	_, negotiated := negotiate(t, newTestDescriptor(t), validRequest())
	return negotiated
}

func compileRequest(source []byte) engineprotocol.CompileRequestEnvelope {
	ref := testBlobRef("source", "text/plain; charset=utf-8", source)
	return engineprotocol.CompileRequestEnvelope{
		Operation: engineprotocol.CompileRequestEnvelopeOperationValue,
		Payload: engineprotocol.CompileInput{
			EntryPath: "document.ldl", InstalledPackTree: []engineprotocol.SourceFileInput{}, Mode: engineprotocol.CompileModeProject,
			ProjectSourceTree: []engineprotocol.SourceFileInput{{Path: "document.ldl", Blob: ref}}, ReferencedAssets: []engineprotocol.AssetInput{},
			ResolvedDependencies: engineprotocol.ResolvedDependencies{Format: engineprotocol.ResolvedDependenciesFormatValue, FormatVersion: 1, Language: 1, Installs: []engineprotocol.ResolvedPack{}},
			ResourceLimits:       engineprotocol.ResourceLimits{},
		},
		Protocol: bootstrapProtocolRef(), RequestID: "compile-request",
	}
}

func testBlobRef(id, mediaType string, value []byte) protocolcommon.BlobRef {
	digest := sha256.Sum256(value)
	return protocolcommon.BlobRef{BlobID: id, Digest: protocolcommon.Digest("sha256:" + hex.EncodeToString(digest[:])), Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: mediaType, Size: protocolcommon.CanonicalUint64(strconv.Itoa(len(value)))}
}

func sourceFor(ref protocolcommon.BlobRef, value []byte) *memoryBlobSource {
	return &memoryBlobSource{definitions: []BlobDefinition{{BlobID: ref.BlobID, Reader: io.NopCloser(bytes.NewReader(value))}}}
}

func TestDispatchCompileProjectSuccessPublishesExactOpaqueArtifacts(t *testing.T) {
	sourceBytes := []byte("project p \"Project\" {}\n")
	request := compileRequest(sourceBytes)
	source := sourceFor(request.Payload.ProjectSourceTree[0].Blob, sourceBytes)
	sink := &memoryBlobSink{}
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	response, err := dispatcher.DispatchCompile(context.Background(), compileContext(t), request, source, sink)
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeSuccess || response.Payload == nil || response.Failure != nil || len(response.Diagnostics) != 0 {
		t.Fatalf("unexpected response: %+v", response)
	}
	if source.calls != 1 || sink.calls != 1 || len(sink.blobs) != 2 {
		t.Fatalf("blob calls source=%d sink=%d blobs=%d", source.calls, sink.calls, len(sink.blobs))
	}
	if response.Payload.NormalizedArtifact.Project == nil || response.Payload.NormalizedArtifact.Pack != nil || response.Payload.NormalizedArtifact.GraphHash == nil {
		t.Fatalf("invalid Project union: %+v", response.Payload.NormalizedArtifact)
	}
	canonicalRef := response.Payload.NormalizedArtifact.Project.CanonicalJSON
	artifactRef := response.Payload.NormalizedArtifact.Project.ArtifactJSON
	if canonicalRef.MediaType != engineprotocol.NormalizedProjectCanonicalBlobRefMediaTypeValue || artifactRef.MediaType != engineprotocol.NormalizedProjectArtifactBlobRefMediaTypeValue {
		t.Fatalf("wrong artifact media types")
	}
	if canonicalRef.Lifetime != engineprotocol.NormalizedProjectCanonicalBlobRefLifetimeValue || artifactRef.Lifetime != engineprotocol.NormalizedProjectArtifactBlobRefLifetimeValue {
		t.Fatalf("wrong artifact lifetimes")
	}
	if !bytes.HasSuffix(sink.blobs[0].Bytes, []byte("\n")) || bytes.HasSuffix(sink.blobs[1].Bytes, []byte("\n")) || !bytes.Equal(sink.blobs[0].Bytes[:len(sink.blobs[0].Bytes)-1], sink.blobs[1].Bytes) {
		t.Fatalf("canonical/public byte profiles were not preserved")
	}
	if _, err := engineprotocol.EncodeCompileResponseEnvelope(response); err != nil {
		t.Fatalf("invalid response: %v", err)
	}
}

func TestDispatchCompilePackSuccessHasNoProjectOnlyPayload(t *testing.T) {
	packSource := []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nexport { service }\n")
	manifest, err := json.Marshal(map[string]any{"format": "layerdraw-pack", "format_version": 1, "id": "pub/schema", "name": "schema", "version": "1.0.0", "language": 1, "entry": "pack.ldl", "dependencies": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	fileRef := testBlobRef("pack-file", "text/plain; charset=utf-8", packSource)
	manifestRef := testBlobRef("pack-manifest", "application/json", manifest)
	request := compileRequest([]byte("unused"))
	request.Payload.Mode = engineprotocol.CompileModePack
	request.Payload.EntryPath = "pack.ldl"
	root := engineprotocol.CanonicalPackSelector("pub/schema")
	request.Payload.RootPackID = &root
	request.Payload.ProjectSourceTree = []engineprotocol.SourceFileInput{}
	request.Payload.InstalledPackTree = []engineprotocol.SourceFileInput{{Path: "pack/schema/pack.ldl", Blob: fileRef}}
	request.Payload.ResolvedDependencies.Installs = []engineprotocol.ResolvedPack{{InstallName: "schema", CanonicalID: root, Version: "1.0.0", Digest: protocolcommon.Digest("sha256:" + strings.Repeat("a", 64)), Path: "pack/schema", Entry: "pack.ldl", Files: []engineprotocol.ResolvedPackFile{{Path: "pack.ldl", Digest: fileRef.Digest}}, Dependencies: []engineprotocol.ResolvedPackDependency{}, ManifestPath: "manifest.json", Manifest: manifestRef}}
	source := &memoryBlobSource{definitions: []BlobDefinition{{BlobID: fileRef.BlobID, Reader: io.NopCloser(bytes.NewReader(packSource))}, {BlobID: manifestRef.BlobID, Reader: io.NopCloser(bytes.NewReader(manifest))}}}
	sink := &memoryBlobSink{}
	response, err := NewCompileDispatcher(engine.New(engine.BuildInfo{})).DispatchCompile(context.Background(), compileContext(t), request, source, sink)
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeSuccess || response.Payload == nil || response.Payload.NormalizedArtifact.Pack == nil || response.Payload.NormalizedArtifact.Project != nil || response.Payload.NormalizedArtifact.GraphHash != nil || len(response.Payload.NormalizedArtifact.SearchDocuments) != 0 {
		t.Fatalf("invalid Pack response: %+v", response)
	}
}

func TestDispatchCompilePublishesGeneratedRecipeDocuments(t *testing.T) {
	value := []byte(allRecipeDeclarationsFixture)
	request := compileRequest(value)
	sink := &memoryBlobSink{}
	response, err := NewCompileDispatcher(engine.New(engine.BuildInfo{})).DispatchCompile(context.Background(), compileContext(t), request, sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), sink)
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeSuccess || response.Payload == nil {
		t.Fatalf("unexpected response: %+v failure=%+v", response, response.Failure)
	}
	recipes := response.Payload.CompiledRecipes
	if len(recipes.Queries) != 1 || len(recipes.Views) != 1 || len(recipes.Exports) != 1 || len(sink.blobs) != 5 {
		t.Fatalf("incomplete recipes: %+v blobs=%d", recipes, len(sink.blobs))
	}
	byID := map[string][]byte{}
	for _, blob := range sink.blobs {
		byID[blob.Ref.BlobID] = blob.Bytes
	}
	if _, err := semantic.DecodeCompiledQueryRecipeDocument(byID[recipes.Queries[0].CanonicalJSON.BlobID]); err != nil {
		t.Fatalf("Query recipe: %v", err)
	}
	if _, err := semantic.DecodeCompiledViewRecipeDocument(byID[recipes.Views[0].CanonicalJSON.BlobID]); err != nil {
		t.Fatalf("View recipe: %v", err)
	}
	if _, err := semantic.DecodeCompiledExportRecipeDocument(byID[recipes.Exports[0].CanonicalJSON.BlobID]); err != nil {
		t.Fatalf("Export recipe: %v", err)
	}
}

func TestDispatchCompileRejectsDuplicatesBeforeBlobEnumeration(t *testing.T) {
	value := []byte("project p \"Project\" {}")
	request := compileRequest(value)
	request.Payload.ProjectSourceTree = append(request.Payload.ProjectSourceTree, request.Payload.ProjectSourceTree[0])
	source := &memoryBlobSource{}
	response, err := NewCompileDispatcher(engine.New(engine.BuildInfo{})).DispatchCompile(context.Background(), compileContext(t), request, source, &memoryBlobSink{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeRejected || response.Payload != nil || response.Failure != nil || len(response.Diagnostics) != 1 || response.Diagnostics[0].Code != "LDL1201" || source.calls != 0 {
		t.Fatalf("duplicate was not rejected before blobs: %+v calls=%d", response, source.calls)
	}
}

func TestDispatchCompileBlobBoundaryFailuresAreAtomic(t *testing.T) {
	value := []byte("project p \"Project\" {}")
	base := compileRequest(value)
	tests := []struct {
		name, code string
		mutate     func(*engineprotocol.CompileRequestEnvelope, *memoryBlobSource)
	}{
		{"missing", FailureCompileMissingBlob, func(_ *engineprotocol.CompileRequestEnvelope, source *memoryBlobSource) {
			source.definitions = []BlobDefinition{}
		}},
		{"duplicate definition", FailureCompileDuplicateBlob, func(request *engineprotocol.CompileRequestEnvelope, source *memoryBlobSource) {
			ref := request.Payload.ProjectSourceTree[0].Blob
			source.definitions = append(source.definitions, BlobDefinition{BlobID: ref.BlobID, Reader: io.NopCloser(bytes.NewReader(value))})
		}},
		{"digest", FailureCompileBlobDigestMismatch, func(request *engineprotocol.CompileRequestEnvelope, _ *memoryBlobSource) {
			request.Payload.ProjectSourceTree[0].Blob.Digest = protocolcommon.Digest("sha256:" + strings.Repeat("f", 64))
		}},
		{"malformed digest", FailureCompileInvalidRequest, func(request *engineprotocol.CompileRequestEnvelope, _ *memoryBlobSource) {
			request.Payload.ProjectSourceTree[0].Blob.Digest = "not-a-digest"
		}},
		{"short", FailureCompileBlobSizeMismatch, func(request *engineprotocol.CompileRequestEnvelope, _ *memoryBlobSource) {
			request.Payload.ProjectSourceTree[0].Blob.Size = protocolcommon.CanonicalUint64(strconv.Itoa(len(value) + 1))
		}},
		{"long", FailureCompileBlobSizeMismatch, func(request *engineprotocol.CompileRequestEnvelope, _ *memoryBlobSource) {
			request.Payload.ProjectSourceTree[0].Blob.Size = protocolcommon.CanonicalUint64(strconv.Itoa(len(value) - 1))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := base
			request.Payload.ProjectSourceTree = slices.Clone(base.Payload.ProjectSourceTree)
			source := sourceFor(request.Payload.ProjectSourceTree[0].Blob, value)
			test.mutate(&request, source)
			sink := &memoryBlobSink{}
			response, err := NewCompileDispatcher(engine.New(engine.BuildInfo{})).DispatchCompile(context.Background(), compileContext(t), request, source, sink)
			if err != nil {
				t.Fatal(err)
			}
			if response.Outcome != protocolcommon.OutcomeFailed || response.Failure == nil || response.Failure.Code != test.code || response.Payload != nil || sink.calls != 0 {
				t.Fatalf("response=%+v sink=%d", response, sink.calls)
			}
		})
	}
}

func TestDispatchCompileClosesEveryEnumeratedBlobDefinition(t *testing.T) {
	value := []byte("project p \"Project\" {}")
	request := compileRequest(value)
	first := &trackingReadCloser{reader: bytes.NewReader(value)}
	second := &trackingReadCloser{reader: bytes.NewReader(value)}
	source := &memoryBlobSource{definitions: []BlobDefinition{{BlobID: "source", Reader: first}, {BlobID: "source", Reader: second}}}
	response, err := NewCompileDispatcher(engine.New(engine.BuildInfo{})).DispatchCompile(context.Background(), compileContext(t), request, source, &memoryBlobSink{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeFailed || response.Failure == nil || response.Failure.Code != FailureCompileDuplicateBlob || first.closes != 1 || second.closes != 1 {
		t.Fatalf("definitions were not atomically rejected and closed: response=%+v closes=%d/%d", response, first.closes, second.closes)
	}
}

func TestDispatchCompileNegotiatedLimitCannotBeRaised(t *testing.T) {
	negotiated := compileContext(t)
	negotiated.defaultLimits.MaxProjectSourceBytes = 4
	negotiated.effectiveMaximums.MaxProjectSourceBytes = 4
	value := protocolcommon.CanonicalNonNegativeInt64("5")
	request := compileRequest([]byte("project p \"Project\" {}"))
	request.Payload.ResourceLimits.MaxProjectSourceBytes = &value
	source := &memoryBlobSource{}
	response, err := NewCompileDispatcher(engine.New(engine.BuildInfo{})).DispatchCompile(context.Background(), negotiated, request, source, &memoryBlobSink{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeRejected || len(response.Diagnostics) != 1 || response.Diagnostics[0].MessageKey != "invalid_closed_input_resource_limit_maximum" || source.calls != 0 {
		t.Fatalf("response=%+v calls=%d", response, source.calls)
	}
}

func TestDispatchCompileSinkFailureAndCancellationPublishNoSuccess(t *testing.T) {
	value := []byte("project p \"Project\" {}")
	request := compileRequest(value)
	sink := &memoryBlobSink{err: errors.New("sink unavailable")}
	response, err := NewCompileDispatcher(engine.New(engine.BuildInfo{})).DispatchCompile(context.Background(), compileContext(t), request, sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), sink)
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeFailed || response.Failure == nil || response.Failure.Code != FailureCompileBlobSink || response.Payload != nil {
		t.Fatalf("unexpected sink failure: %+v", response)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	response, err = NewCompileDispatcher(engine.New(engine.BuildInfo{})).DispatchCompile(cancelled, compileContext(t), request, sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), &memoryBlobSink{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeCancelled || response.Failure == nil || response.Failure.Code != FailureCompileCancelled {
		t.Fatalf("unexpected cancellation: %+v", response)
	}
}

func TestDispatchCompileConcurrentDeterminismAndIsolation(t *testing.T) {
	value := []byte("project p \"Project\" {}")
	request := compileRequest(value)
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	negotiated := compileContext(t)
	const count = 32
	responses := make([][]byte, count)
	blobSets := make([][]OutputBlob, count)
	var wait sync.WaitGroup
	for i := 0; i < count; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			sink := &memoryBlobSink{}
			response, err := dispatcher.DispatchCompile(context.Background(), negotiated, request, sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), sink)
			if err != nil {
				t.Errorf("dispatch: %v", err)
				return
			}
			encoded, err := engineprotocol.EncodeCompileResponseEnvelope(response)
			if err != nil {
				t.Errorf("encode: %v", err)
				return
			}
			responses[index] = encoded
			blobSets[index] = sink.blobs
		}(i)
	}
	wait.Wait()
	for i := 1; i < count; i++ {
		if !bytes.Equal(responses[0], responses[i]) || !equalOutputBlobs(blobSets[0], blobSets[i]) {
			t.Fatalf("dispatch %d differs", i)
		}
	}
	blobSets[0][0].Bytes[0] ^= 0xff
	if bytes.Equal(blobSets[0][0].Bytes, blobSets[1][0].Bytes) {
		t.Fatal("output mutation crossed dispatches")
	}
}

func TestDispatchCompileOwnsRequestBeforeBlobCallbacksAndLaterMutation(t *testing.T) {
	value := []byte("project p \"Project\" {}")
	request := compileRequest(value)
	originalRef := request.Payload.ProjectSourceTree[0].Blob
	source := &callbackBlobSource{
		definitions: []BlobDefinition{{BlobID: originalRef.BlobID, Reader: io.NopCloser(bytes.NewReader(value))}},
		callback: func() {
			request.Payload.ProjectSourceTree[0].Path = "changed.ldl"
			request.Payload.ProjectSourceTree[0].Blob.BlobID = "changed"
			request.Payload.ResolvedDependencies.Installs = append(request.Payload.ResolvedDependencies.Installs, engineprotocol.ResolvedPack{})
		},
	}
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	negotiated := compileContext(t)
	firstSink := &memoryBlobSink{}
	first, err := dispatcher.DispatchCompile(context.Background(), negotiated, request, source, firstSink)
	if err != nil {
		t.Fatal(err)
	}
	if first.Outcome != protocolcommon.OutcomeSuccess || first.Payload == nil || firstSink.calls != 1 {
		t.Fatalf("callback mutation crossed request ownership: %+v", first)
	}

	baseline, err := engineprotocol.EncodeCompileResponseEnvelope(first)
	if err != nil {
		t.Fatal(err)
	}
	baselineBlobs := cloneOutputBlobs(firstSink.blobs)
	first.Payload.StableAddresses[0] = "ldl:project:mutated"
	first.Diagnostics = append(first.Diagnostics, semantic.Diagnostic{})
	firstSink.blobs[0].Bytes[0] ^= 0xff

	cleanRequest := compileRequest(value)
	secondSink := &memoryBlobSink{}
	second, err := dispatcher.DispatchCompile(context.Background(), negotiated, cleanRequest, sourceFor(cleanRequest.Payload.ProjectSourceTree[0].Blob, value), secondSink)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := engineprotocol.EncodeCompileResponseEnvelope(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseline, encoded) || !equalOutputBlobs(baselineBlobs, secondSink.blobs) {
		t.Fatal("returned payload or blob mutation contaminated a later dispatch")
	}
}

func TestDispatchCompileCancellationDuringBlobReadAndPublish(t *testing.T) {
	value := []byte("project p \"Project\" {}")
	request := compileRequest(value)
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	negotiated := compileContext(t)

	readContext, cancelRead := context.WithCancel(context.Background())
	readSource := &memoryBlobSource{definitions: []BlobDefinition{{BlobID: request.Payload.ProjectSourceTree[0].Blob.BlobID, Reader: &cancelDuringReadCloser{reader: bytes.NewReader(value), cancel: cancelRead}}}}
	readSink := &memoryBlobSink{}
	response, err := dispatcher.DispatchCompile(readContext, negotiated, request, readSource, readSink)
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeCancelled || response.Failure == nil || readSink.calls != 0 {
		t.Fatalf("read cancellation published output: %+v sink=%d", response, readSink.calls)
	}

	publishContext, cancelPublish := context.WithCancel(context.Background())
	publishSink := &cancelDuringPublishSink{cancel: cancelPublish}
	response, err = dispatcher.DispatchCompile(publishContext, negotiated, request, sourceFor(request.Payload.ProjectSourceTree[0].Blob, value), publishSink)
	if err != nil {
		t.Fatal(err)
	}
	if response.Outcome != protocolcommon.OutcomeCancelled || response.Failure == nil || publishSink.calls != 1 {
		t.Fatalf("publish cancellation escaped: %+v calls=%d", response, publishSink.calls)
	}
}

func TestDispatchCompileInputPermutationIsDeterministic(t *testing.T) {
	entry := []byte("import { service } from \"./types.ldl\"\nproject p \"Project\" {}\n")
	types := []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nexport { service }\n")
	request := compileRequest(entry)
	typesRef := testBlobRef("types", "text/plain; charset=utf-8", types)
	request.Payload.ProjectSourceTree = append(request.Payload.ProjectSourceTree, engineprotocol.SourceFileInput{Path: "types.ldl", Blob: typesRef})
	definitions := []BlobDefinition{{BlobID: "source", Reader: io.NopCloser(bytes.NewReader(entry))}, {BlobID: "types", Reader: io.NopCloser(bytes.NewReader(types))}}

	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	negotiated := compileContext(t)
	firstSink := &memoryBlobSink{}
	first, err := dispatcher.DispatchCompile(context.Background(), negotiated, request, &memoryBlobSource{definitions: definitions}, firstSink)
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, err := engineprotocol.EncodeCompileResponseEnvelope(first)
	if err != nil {
		t.Fatal(err)
	}

	slices.Reverse(request.Payload.ProjectSourceTree)
	secondDefinitions := []BlobDefinition{{BlobID: "types", Reader: io.NopCloser(bytes.NewReader(types))}, {BlobID: "source", Reader: io.NopCloser(bytes.NewReader(entry))}}
	secondSink := &memoryBlobSink{}
	second, err := dispatcher.DispatchCompile(context.Background(), negotiated, request, &memoryBlobSource{definitions: secondDefinitions}, secondSink)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := engineprotocol.EncodeCompileResponseEnvelope(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) || !equalOutputBlobs(firstSink.blobs, secondSink.blobs) {
		t.Fatal("semantically unordered input permutations changed compile output")
	}
}

func TestDispatchCompileFailureJSONDoesNotLeakImplementationDetails(t *testing.T) {
	secret := "/private/tenant/source.ldl at parse stage: internal/engine panic stack"
	request := compileRequest([]byte("project p \"Project\" {}"))
	response, err := NewCompileDispatcher(engine.New(engine.BuildInfo{})).DispatchCompile(context.Background(), compileContext(t), request, errorBlobSource{err: errors.New(secret)}, &memoryBlobSink{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := engineprotocol.EncodeCompileResponseEnvelope(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{secret, "/private/tenant", "internal/engine", "panic stack", "parse stage"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("failure leaked %q: %s", forbidden, encoded)
		}
	}
}

func equalOutputBlobs(left, right []OutputBlob) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].Ref != right[i].Ref || !bytes.Equal(left[i].Bytes, right[i].Bytes) {
			return false
		}
	}
	return true
}

const allRecipeDeclarationsFixture = `project p "Project" {
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
