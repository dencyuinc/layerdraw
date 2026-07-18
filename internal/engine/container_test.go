// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

func TestLayerdrawCanonicalGoldenRoundTrip(t *testing.T) {
	t.Parallel()
	instance := New(BuildInfo{})
	input := LayerdrawWriteInput{CompileInput: projectCompileInput("project p \"Project\" {}\n")}
	first, err := instance.WriteLayerdraw(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := instance.WriteLayerdraw(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("canonical writer bytes changed for identical input")
	}
	inspection, err := instance.InspectLayerdraw(context.Background(), LayerdrawInspectInput{Bytes: first})
	if err != nil || !inspection.Supported || inspection.FormatVersion != 1 || inspection.Language != 1 {
		t.Fatalf("InspectLayerdraw() = %+v, %v", inspection, err)
	}
	wantEntries := []string{"document.json", "document.ldl", "layerdraw.index.json", "layerdraw.resolved.json", "manifest.json"}
	if len(inspection.Entries) != len(wantEntries) {
		t.Fatalf("entries = %+v", inspection.Entries)
	}
	for index, entry := range inspection.Entries {
		if entry.Name != wantEntries[index] || entry.Method != zip.Store {
			t.Fatalf("entry[%d] = %+v", index, entry)
		}
	}
	document, err := instance.ReadLayerdraw(context.Background(), LayerdrawReadInput{Bytes: first})
	if err != nil {
		t.Fatal(err)
	}
	if document.Manifest.ProjectAddress != "ldl:project:p" || document.Compilation.DefinitionHash != document.Manifest.DefinitionHash || string(document.ProjectSourceTree["document.ldl"]) != "project p \"Project\" {}\n" {
		t.Fatalf("round trip = %+v", document.Manifest)
	}
	entries := unzipLayerdraw(t, first)
	const resolvedGolden = "{\"format\":\"layerdraw-resolved\",\"format_version\":1,\"installs\":{},\"language\":1}\n"
	if string(entries["layerdraw.resolved.json"]) != resolvedGolden {
		t.Fatalf("resolved golden changed:\n%s", entries["layerdraw.resolved.json"])
	}
	var manifest LayerdrawManifest
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 4 || manifest.Files["document.ldl"] != rawDigest([]byte("project p \"Project\" {}\n")) || manifest.ResolvedFileDigest != rawDigest(entries["layerdraw.resolved.json"]) {
		t.Fatalf("manifest golden contract = %+v", manifest)
	}
}

func TestLayerdrawRoundTripPreservesResolvedDependencyAndAssetClosure(t *testing.T) {
	t.Parallel()
	instance := New(BuildInfo{})
	compileInput := installedPackProjectInput()
	compileInput.ResolvedDependencies.Installs[0].RegistrySource = "registry:official"
	manifest, err := canonicalJSONBytes(compileInput.ResolvedDependencies.Installs[0].Manifest)
	if err != nil {
		t.Fatal(err)
	}
	compileInput.ResolvedDependencies.Installs[0].Manifest = manifest
	archive, err := instance.WriteLayerdraw(context.Background(), LayerdrawWriteInput{CompileInput: compileInput})
	if err != nil {
		t.Fatal(err)
	}
	document, err := instance.ReadLayerdraw(context.Background(), LayerdrawReadInput{Bytes: archive})
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Compilation.NormalizedDocument.Dependencies) != 1 || len(document.InstalledPackTree) != 2 {
		t.Fatalf("resolved closure = dependencies:%+v files:%v", document.Compilation.NormalizedDocument.Dependencies, sortedKeys(document.InstalledPackTree))
	}
	unlistedPackFile := rewriteLayerdraw(t, archive, func(entries map[string][]byte) {
		entries["pack/schema/unlisted.bin"] = []byte("not declared by resolved Pack metadata")
		refreshTestManifest(t, entries)
	})
	if _, err := instance.ReadLayerdraw(context.Background(), LayerdrawReadInput{Bytes: unlistedPackFile}); !IsLayerdrawError(err, LayerdrawErrorForbiddenPortable) {
		t.Fatalf("unlisted Pack file error = %v", err)
	}
	asset := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="1" height="1"/></svg>`)
	assetInput := projectCompileInput("project p \"Project\" {}\nentity_type service \"Service\" {\n  image \"assets/icon.svg\"\n  representation shape rect\n}\n")
	assetInput.ReferencedAssets = []AssetInput{{Origin: SourceOriginProject, Locator: "assets/icon.svg", Bytes: asset, Digest: rawDigest(asset), MediaType: "image/svg+xml", ByteLength: int64(len(asset))}}
	assetArchive, err := instance.WriteLayerdraw(context.Background(), LayerdrawWriteInput{CompileInput: assetInput})
	if err != nil {
		t.Fatal(err)
	}
	assetDocument, err := instance.ReadLayerdraw(context.Background(), LayerdrawReadInput{Bytes: assetArchive})
	if err != nil {
		t.Fatal(err)
	}
	if len(assetDocument.Compilation.NormalizedDocument.Assets) != 1 || !bytes.Equal(assetDocument.Files["assets/icon.svg"], asset) {
		t.Fatalf("asset closure = %+v", assetDocument.Compilation.NormalizedDocument.Assets)
	}
}

func TestLayerdrawRoundTripPreservesOpaqueDerivedArtifacts(t *testing.T) {
	t.Parallel()
	instance := New(BuildInfo{})
	artifacts := map[string][]byte{
		"previews/diagram.png": append([]byte("\x89PNG\r\n\x1a\n"), []byte("opaque-preview")...),
		"exports/diagram.svg":  []byte("opaque export bytes; not a serializer claim"),
	}
	archive, err := instance.WriteLayerdraw(context.Background(), LayerdrawWriteInput{
		CompileInput: projectCompileInput("project p \"Project\" {}\n"),
		Artifacts:    artifacts,
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := instance.ReadLayerdraw(context.Background(), LayerdrawReadInput{Bytes: archive})
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Artifacts) != len(artifacts) || len(document.Compilation.NormalizedDocument.Assets) != 0 {
		t.Fatalf("derived artifacts entered semantic assets: artifacts=%v assets=%+v", sortedKeys(document.Artifacts), document.Compilation.NormalizedDocument.Assets)
	}
	for name, want := range artifacts {
		if !bytes.Equal(document.Artifacts[name], want) {
			t.Fatalf("opaque artifact %q changed", name)
		}
	}
	for _, diagnostic := range document.Compilation.Diagnostics {
		if strings.Contains(strings.ToLower(diagnostic.Message), "asset") {
			t.Fatalf("derived artifact caused asset diagnostic: %+v", diagnostic)
		}
	}
}

func TestLayerdrawUnsupportedVersionInspectionDoesNotCompile(t *testing.T) {
	t.Parallel()
	archive := testZip(t, []testZipEntry{{name: "manifest.json", data: []byte("{\"format\":\"layerdraw-document\",\"format_version\":2,\"language\":2}\n")}, {name: "payload.bin", data: []byte("not semantic content")}})
	instance := New(BuildInfo{})
	inspection, err := instance.InspectLayerdraw(context.Background(), LayerdrawInspectInput{Bytes: archive})
	if err != nil || inspection.Supported || inspection.FormatVersion != 2 || len(inspection.Entries) != 2 {
		t.Fatalf("InspectLayerdraw() = %+v, %v", inspection, err)
	}
	if _, err := instance.ReadLayerdraw(context.Background(), LayerdrawReadInput{Bytes: archive}); !IsLayerdrawError(err, LayerdrawErrorUnsupportedVersion) {
		t.Fatalf("ReadLayerdraw() error = %v", err)
	}
}

func TestLayerdrawRejectsAdversarialArchives(t *testing.T) {
	t.Parallel()
	baseManifest := []byte("{\"format\":\"layerdraw-document\",\"format_version\":1,\"language\":1}\n")
	tests := []struct {
		name   string
		bytes  func(*testing.T) []byte
		limits LayerdrawLimits
		code   string
	}{
		{name: "traversal", bytes: func(t *testing.T) []byte {
			return testZip(t, []testZipEntry{{name: "../manifest.json", data: baseManifest}})
		}, code: LayerdrawErrorUnsafeEntry},
		{name: "absolute", bytes: func(t *testing.T) []byte {
			return testZip(t, []testZipEntry{{name: "/manifest.json", data: baseManifest}})
		}, code: LayerdrawErrorUnsafeEntry},
		{name: "windows absolute", bytes: func(t *testing.T) []byte {
			return testZip(t, []testZipEntry{{name: "C:/manifest.json", data: baseManifest}})
		}, code: LayerdrawErrorUnsafeEntry},
		{name: "backslash", bytes: func(t *testing.T) []byte { return testZip(t, []testZipEntry{{name: `a\b`, data: baseManifest}}) }, code: LayerdrawErrorUnsafeEntry},
		{name: "invalid utf8", bytes: func(t *testing.T) []byte {
			return testZip(t, []testZipEntry{{name: string([]byte{'x', 0xff}), data: baseManifest, nonUTF8: true}})
		}, code: LayerdrawErrorUnsafeEntry},
		{name: "encoded traversal", bytes: func(t *testing.T) []byte {
			return testZip(t, []testZipEntry{{name: "a/%2e%2e/manifest.json", data: baseManifest}})
		}, code: LayerdrawErrorUnsafeEntry},
		{name: "symlink", bytes: func(t *testing.T) []byte {
			return testZip(t, []testZipEntry{{name: "manifest.json", data: baseManifest, mode: os.ModeSymlink | 0o777}})
		}, code: LayerdrawErrorUnsafeEntry},
		{name: "special file", bytes: func(t *testing.T) []byte {
			return testZip(t, []testZipEntry{{name: "manifest.json", data: baseManifest, mode: os.ModeNamedPipe | 0o600}})
		}, code: LayerdrawErrorUnsafeEntry},
		{name: "duplicate", bytes: func(t *testing.T) []byte {
			return testZip(t, []testZipEntry{{name: "manifest.json", data: baseManifest}, {name: "manifest.json", data: baseManifest}})
		}, code: LayerdrawErrorUnsafeEntry},
		{name: "case collision", bytes: func(t *testing.T) []byte {
			return testZip(t, []testZipEntry{{name: "manifest.json", data: baseManifest}, {name: "Manifest.json", data: baseManifest}})
		}, code: LayerdrawErrorUnsafeEntry},
		{name: "unicode collision", bytes: func(t *testing.T) []byte {
			return testZip(t, []testZipEntry{{name: "café", data: []byte("a")}, {name: "cafe\u0301", data: []byte("b")}, {name: "manifest.json", data: baseManifest}})
		}, code: LayerdrawErrorUnsafeEntry},
		{name: "count", bytes: func(t *testing.T) []byte {
			return testZip(t, []testZipEntry{{name: "manifest.json", data: baseManifest}, {name: "a", data: nil}})
		}, limits: LayerdrawLimits{MaxEntries: 1}, code: LayerdrawErrorEntryCountExceeded},
		{name: "entry size", bytes: func(t *testing.T) []byte {
			return testZip(t, []testZipEntry{{name: "manifest.json", data: baseManifest}})
		}, limits: LayerdrawLimits{MaxEntryBytes: 8}, code: LayerdrawErrorEntrySizeExceeded},
		{name: "ratio", bytes: func(t *testing.T) []byte {
			return testZip(t, []testZipEntry{{name: "manifest.json", data: baseManifest}, {name: "bomb", data: bytes.Repeat([]byte("0"), 64<<10), method: zip.Deflate}})
		}, limits: LayerdrawLimits{MaxCompressionRatio: 2}, code: LayerdrawErrorCompressionRatio},
		{name: "truncated", bytes: func(t *testing.T) []byte {
			value := testZip(t, []testZipEntry{{name: "manifest.json", data: baseManifest}})
			return value[:len(value)-12]
		}, code: LayerdrawErrorInvalidArchive},
	}
	instance := New(BuildInfo{})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := instance.InspectLayerdraw(context.Background(), LayerdrawInspectInput{Bytes: test.bytes(t), Limits: test.limits})
			if !IsLayerdrawError(err, test.code) {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
		})
	}
}

func TestLayerdrawRejectsDigestAndDerivedArtifactTampering(t *testing.T) {
	t.Parallel()
	instance := New(BuildInfo{})
	original, err := instance.WriteLayerdraw(context.Background(), LayerdrawWriteInput{CompileInput: projectCompileInput("project p \"P\" {}\n")})
	if err != nil {
		t.Fatal(err)
	}
	digestTampered := rewriteLayerdraw(t, original, func(entries map[string][]byte) {
		entries["document.ldl"] = []byte("project other \"Other\" {}\n")
	})
	if _, err := instance.ReadLayerdraw(context.Background(), LayerdrawReadInput{Bytes: digestTampered}); !IsLayerdrawError(err, LayerdrawErrorDigestMismatch) {
		t.Fatalf("digest tamper error = %v", err)
	}
	derivedTampered := rewriteLayerdraw(t, original, func(entries map[string][]byte) {
		entries["document.json"] = []byte("{}\n")
		var manifest LayerdrawManifest
		if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
			t.Fatal(err)
		}
		manifest.Files["document.json"] = rawDigest(entries["document.json"])
		entries["manifest.json"], err = canonicalArtifact(manifest)
		if err != nil {
			t.Fatal(err)
		}
	})
	if _, err := instance.ReadLayerdraw(context.Background(), LayerdrawReadInput{Bytes: derivedTampered}); !IsLayerdrawError(err, LayerdrawErrorDerivedArtifact) {
		t.Fatalf("derived tamper error = %v", err)
	}
}

func TestLayerdrawWriterRedactsForbiddenPortableContent(t *testing.T) {
	t.Parallel()
	instance := New(BuildInfo{})
	base := projectCompileInput("project p \"P\" {}\n")
	tests := []struct {
		name  string
		input LayerdrawWriteInput
	}{
		{name: "backend artifact", input: LayerdrawWriteInput{CompileInput: base, Artifacts: map[string][]byte{"exports/backend.json": []byte("{\"backend_binding\":{}}")}}},
		{name: "local path", input: LayerdrawWriteInput{CompileInput: base, Artifacts: map[string][]byte{"exports/source.json": []byte("{\"source_path\":\"/Users/private/project\"}")}}},
		{name: "provider secret", input: LayerdrawWriteInput{CompileInput: base, Artifacts: map[string][]byte{"exports/value.bin": []byte("prefix-super-secret-suffix")}, Secrets: [][]byte{[]byte("super-secret")}}},
		{name: "token key", input: LayerdrawWriteInput{CompileInput: base, Artifacts: map[string][]byte{"exports/source.json": []byte("{\"refresh_token\":\"x\"}")}}},
		{name: "outside artifact roots", input: LayerdrawWriteInput{CompileInput: base, Artifacts: map[string][]byte{"state/leases/current.json": []byte("{}")}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := instance.WriteLayerdraw(context.Background(), test.input); !IsLayerdrawError(err, LayerdrawErrorForbiddenPortable) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestLayerdrawStateSnapshotRoundTripAndRedactionMarker(t *testing.T) {
	t.Parallel()
	instance := New(BuildInfo{})
	compileInput := projectCompileInput("project p \"P\" {}\n")
	compiled, err := instance.Compile(context.Background(), compileInput)
	if err != nil {
		t.Fatal(err)
	}
	state := validStateQuerySnapshot(t, compiled.Snapshot(), []StateQuerySubject{})
	state.InaccessibleFieldPaths = []string{"provenance.source.uri"}
	if _, err := instance.WriteLayerdraw(context.Background(), LayerdrawWriteInput{CompileInput: compileInput, StateSnapshots: []StateQuerySnapshot{state}}); !IsLayerdrawError(err, LayerdrawErrorForbiddenPortable) {
		t.Fatalf("missing redaction marker error = %v", err)
	}
	archive, err := instance.WriteLayerdraw(context.Background(), LayerdrawWriteInput{CompileInput: compileInput, StateSnapshots: []StateQuerySnapshot{state}, RedactionPolicyID: "portable-share"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := instance.ReadLayerdraw(context.Background(), LayerdrawReadInput{Bytes: archive})
	if err != nil {
		t.Fatal(err)
	}
	if document.Manifest.Redaction == nil || document.Manifest.Redaction.PolicyID != "portable-share" || len(document.StateSnapshots) != 1 {
		t.Fatalf("state/redaction round trip = %+v, %d", document.Manifest.Redaction, len(document.StateSnapshots))
	}
}

func TestLayerdrawStateSnapshotScalarRoundTrip(t *testing.T) {
	t.Parallel()
	instance := New(BuildInfo{})
	compileInput := projectCompileInput(allDeclarationsFixture)
	compiled, err := instance.Compile(context.Background(), compileInput)
	if err != nil {
		t.Fatal(err)
	}
	entity := compiled.Snapshot().TypedAST.Graph.Entities[0]
	state := validStateQuerySnapshot(t, compiled.Snapshot(), []StateQuerySubject{stateSubject(entity.Address, map[string]TypedScalar{
		"system.updated_at":       datetimeScalar("2026-01-01T00:00:00Z"),
		"system.updated_revision": stringScalar("revision-7"),
		"provenance.source.kind":  enumScalar("manual"),
		"provenance.confidence":   numberScalar(0.75),
	})})
	archive, err := instance.WriteLayerdraw(context.Background(), LayerdrawWriteInput{CompileInput: compileInput, StateSnapshots: []StateQuerySnapshot{state}})
	if err != nil {
		t.Fatal(err)
	}
	document, err := instance.ReadLayerdraw(context.Background(), LayerdrawReadInput{Bytes: archive})
	if err != nil {
		t.Fatal(err)
	}
	for _, decoded := range document.StateSnapshots {
		if len(decoded.Subjects) != 1 || len(decoded.Subjects[0].Fields) != 4 || decoded.Subjects[0].Fields["provenance.confidence"].Float != 0.75 {
			t.Fatalf("decoded state = %+v", decoded)
		}
	}
}

func TestLayerdrawFacadeFailureAndHelperEdges(t *testing.T) {
	t.Parallel()
	cause := context.Canceled
	failure := &LayerdrawError{Code: LayerdrawErrorCancelled, Entry: "entry", cause: cause}
	if failure.Error() != LayerdrawErrorCancelled+": entry" || !errors.Is(failure, cause) || (*LayerdrawError)(nil).Error() != "<nil>" || (&LayerdrawError{Code: "code"}).Error() != "code" || (*LayerdrawError)(nil).Unwrap() != nil {
		t.Fatalf("failure contract = %v", failure)
	}
	instance := New(BuildInfo{})
	if _, err := instance.InspectLayerdraw(context.Background(), LayerdrawInspectInput{Limits: LayerdrawLimits{MaxEntries: -1}}); !IsLayerdrawError(err, LayerdrawErrorInvalidLimits) {
		t.Fatalf("invalid limits error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	archive := testZip(t, []testZipEntry{{name: "manifest.json", data: []byte("{\"format\":\"layerdraw-document\",\"format_version\":1,\"language\":1}\n")}})
	if _, err := instance.InspectLayerdraw(cancelled, LayerdrawInspectInput{Bytes: archive}); !IsLayerdrawError(err, LayerdrawErrorCancelled) {
		t.Fatalf("cancelled inspection error = %v", err)
	}
	if _, err := instance.WriteLayerdraw(cancelled, LayerdrawWriteInput{CompileInput: projectCompileInput("project p \"P\" {}")}); !IsLayerdrawError(err, LayerdrawErrorCancelled) {
		t.Fatalf("cancelled writer error = %v", err)
	}
	if _, err := canonicalJSONBytes([]byte("{} {}")); err == nil {
		t.Fatal("multiple JSON values accepted")
	}
	if _, err := canonicalJSONBytes([]byte("{")); err == nil {
		t.Fatal("malformed JSON accepted")
	}
	var manifest LayerdrawManifest
	if err := decodeJSON([]byte("{\"unknown\":true}"), &manifest, true); err == nil {
		t.Fatal("unknown manifest field accepted")
	}
	if err := validatePortableJSON([]byte("[{\"registry_source\":\"https://user:password@example.invalid\"}]")); err == nil {
		t.Fatal("credential-bearing registry source accepted")
	}
	if err := validatePortableJSON([]byte("{} {}")); err == nil {
		t.Fatal("multiple portable JSON values accepted")
	}
	if err := validatePortableJSON([]byte("{\"a\":1,\"a\":2}")); err == nil {
		t.Fatal("duplicate JSON object key accepted")
	}
	if err := validatePortableJSON([]byte(strings.Repeat("[", 130) + strings.Repeat("]", 130))); err == nil {
		t.Fatal("excessive JSON nesting accepted")
	}
	if err := validatePortableJSON([]byte("{")); err == nil {
		t.Fatal("malformed portable JSON accepted")
	}
	if !forbiddenPortablePath("state/history/events.json") || !forbiddenPortablePath("project.ldbackend.json") || forbiddenPortablePath("state/query-snapshots/a.json") {
		t.Fatal("portable path classification changed")
	}
}

func TestLayerdrawReadZipFilePreservesCancellation(t *testing.T) {
	t.Parallel()
	archive := testZip(t, []testZipEntry{{name: "payload.bin", data: bytes.Repeat([]byte("compressible"), 1<<16), method: zip.Deflate}})
	zipReader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	zipReader.RegisterDecompressor(zip.Deflate, func(compressed io.Reader) io.ReadCloser {
		return &cancelAfterReadReader{reader: flate.NewReader(compressed), cancel: cancel}
	})
	_, err = readZipFile(ctx, zipReader.File[0], DefaultLayerdrawLimits())
	if !IsLayerdrawError(err, LayerdrawErrorCancelled) || IsLayerdrawError(err, LayerdrawErrorTruncated) || !errors.Is(err, context.Canceled) {
		t.Fatalf("readZipFile cancellation = %v", err)
	}
}

type cancelAfterReadReader struct {
	reader    io.ReadCloser
	cancel    context.CancelFunc
	cancelled bool
}

func (r *cancelAfterReadReader) Read(value []byte) (int, error) {
	count, err := r.reader.Read(value)
	if !r.cancelled {
		r.cancelled = true
		r.cancel()
	}
	return count, err
}

func (r *cancelAfterReadReader) Close() error { return r.reader.Close() }

func TestLayerdrawCompressionRatioArithmeticIsOverflowSafe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                     string
		uncompressed, compressed uint64
		maxRatio                 int64
		want                     bool
	}{
		{name: "forged huge compressed size", uncompressed: math.MaxInt64, compressed: math.MaxUint64, maxRatio: math.MaxInt64, want: false},
		{name: "maximum equal ratio", uncompressed: math.MaxUint64 - 1, compressed: (math.MaxUint64 - 1) / 2, maxRatio: 2, want: false},
		{name: "maximum fractional excess", uncompressed: math.MaxUint64, compressed: math.MaxUint64 / 2, maxRatio: 2, want: true},
		{name: "zero compressed size", uncompressed: 101, compressed: 0, maxRatio: 100, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := compressionRatioExceeded(test.uncompressed, test.compressed, test.maxRatio); got != test.want {
				t.Fatalf("compressionRatioExceeded(%d, %d, %d) = %t, want %t", test.uncompressed, test.compressed, test.maxRatio, got, test.want)
			}
		})
	}
}

func TestLayerdrawReaderRejectsMalformedCanonicalContracts(t *testing.T) {
	t.Parallel()
	instance := New(BuildInfo{})
	original, err := instance.WriteLayerdraw(context.Background(), LayerdrawWriteInput{CompileInput: projectCompileInput("project p \"P\" {}\n")})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(map[string][]byte)
		code   string
	}{
		{name: "unknown manifest field", code: LayerdrawErrorManifest, mutate: func(entries map[string][]byte) {
			entries["manifest.json"] = []byte("{\"format\":\"layerdraw-document\",\"format_version\":1,\"language\":1,\"unknown\":true}\n")
		}},
		{name: "resolved envelope", code: LayerdrawErrorResolvedMetadata, mutate: func(entries map[string][]byte) {
			entries["layerdraw.resolved.json"] = []byte("{}\n")
			refreshTestManifest(t, entries)
		}},
		{name: "semantic identity", code: LayerdrawErrorSemanticValidation, mutate: func(entries map[string][]byte) {
			entries["document.ldl"] = []byte("project other \"Other\" {}\n")
			refreshTestManifest(t, entries)
		}},
		{name: "index", code: LayerdrawErrorDerivedArtifact, mutate: func(entries map[string][]byte) {
			entries["layerdraw.index.json"] = []byte("{}\n")
			refreshTestManifest(t, entries)
		}},
		{name: "forbidden state", code: LayerdrawErrorForbiddenPortable, mutate: func(entries map[string][]byte) {
			entries["state/history/events.json"] = []byte("{}\n")
			refreshTestManifest(t, entries)
		}},
		{name: "invalid state", code: LayerdrawErrorStateSnapshot, mutate: func(entries map[string][]byte) {
			entries["state/query-snapshots/"+strings.Repeat("0", 64)+".json"] = []byte("{}\n")
			refreshTestManifest(t, entries)
		}},
		{name: "invalid UTF-8 JSON", code: LayerdrawErrorForbiddenPortable, mutate: func(entries map[string][]byte) {
			entries["exports/invalid.json"] = []byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}
			refreshTestManifest(t, entries)
		}},
		{name: "unclassified root payload", code: LayerdrawErrorForbiddenPortable, mutate: func(entries map[string][]byte) {
			entries["payload.bin"] = []byte("opaque root bytes")
			refreshTestManifest(t, entries)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := rewriteLayerdraw(t, original, test.mutate)
			if _, err := instance.ReadLayerdraw(context.Background(), LayerdrawReadInput{Bytes: archive}); !IsLayerdrawError(err, test.code) {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
		})
	}
	if _, err := instance.InspectLayerdraw(context.Background(), LayerdrawInspectInput{Bytes: testZip(t, []testZipEntry{{name: "other", data: nil}})}); !IsLayerdrawError(err, LayerdrawErrorManifest) {
		t.Fatalf("missing manifest error = %v", err)
	}
	if _, err := instance.InspectLayerdraw(context.Background(), LayerdrawInspectInput{Bytes: testZip(t, []testZipEntry{{name: "manifest.json", data: []byte("{")}})}); !IsLayerdrawError(err, LayerdrawErrorManifest) {
		t.Fatalf("malformed manifest error = %v", err)
	}
}

func TestLayerdrawWriterRejectsInvalidInputsAndLimits(t *testing.T) {
	t.Parallel()
	instance := New(BuildInfo{})
	pack := installedPackProjectInput()
	pack.ResolvedDependencies.Installs[0].RegistrySource = "registry:official"
	tests := []struct {
		name  string
		input LayerdrawWriteInput
		code  string
	}{
		{name: "pack mode", input: LayerdrawWriteInput{CompileInput: rootPackInput()}, code: LayerdrawErrorSemanticValidation},
		{name: "invalid semantic input", input: LayerdrawWriteInput{CompileInput: projectCompileInput("project")}, code: LayerdrawErrorSemanticValidation},
		{name: "invalid limits", input: LayerdrawWriteInput{CompileInput: projectCompileInput("project p \"P\" {}"), Limits: LayerdrawLimits{MaxEntries: -1}}, code: LayerdrawErrorInvalidLimits},
		{name: "count limit", input: LayerdrawWriteInput{CompileInput: projectCompileInput("project p \"P\" {}"), Limits: LayerdrawLimits{MaxEntries: 1}}, code: LayerdrawErrorEntryCountExceeded},
		{name: "entry limit", input: LayerdrawWriteInput{CompileInput: projectCompileInput("project p \"P\" {}"), Limits: LayerdrawLimits{MaxEntryBytes: 8}}, code: LayerdrawErrorEntrySizeExceeded},
		{name: "noncanonical pack manifest", input: LayerdrawWriteInput{CompileInput: pack}, code: LayerdrawErrorResolvedMetadata},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := instance.WriteLayerdraw(context.Background(), test.input); !IsLayerdrawError(err, test.code) {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
		})
	}
	resolved := emptyResolvedDependencies()
	resolved.Installs = []ResolvedPack{{InstallName: "p", RegistrySource: "registry:official", Files: []ResolvedPackFile{{Path: "a", Digest: semanticHash('a')}, {Path: "a", Digest: semanticHash('a')}}}}
	if _, err := canonicalResolved(resolved); err == nil {
		t.Fatal("duplicate resolved file accepted")
	}
	if hasErrorDiagnostics([]Diagnostic{{Severity: "error"}}) != true {
		t.Fatal("error diagnostic not detected")
	}
}

func TestLayerdrawScalarAndMediaHelpers(t *testing.T) {
	t.Parallel()
	values := []TypedScalar{
		{Type: definition.ScalarString, String: "text"},
		{Type: definition.ScalarEnum, String: "value"},
		{Type: definition.ScalarDate, String: "2026-01-01"},
		{Type: definition.ScalarDatetime, String: "2026-01-01T00:00:00Z"},
		{Type: definition.ScalarInteger, Int: 42},
		{Type: definition.ScalarNumber, Float: 0.5},
		{Type: definition.ScalarBoolean, Bool: true},
	}
	for _, value := range values {
		wire, err := recipeScalarToWire(value)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := recipeScalarFromWire(wire)
		if err != nil || decoded != value {
			t.Fatalf("scalar round trip = %+v, %v; want %+v", decoded, err, value)
		}
	}
	if _, err := recipeScalarToWire(TypedScalar{}); err == nil {
		t.Fatal("unknown scalar encoded")
	}
	if _, err := recipeScalarFromWire(recipeScalarWire{Kind: "string"}); err == nil {
		t.Fatal("incomplete scalar decoded")
	}
	for _, malformed := range []recipeScalarWire{
		{Kind: "string", StringValue: layerdrawStringPointer("x"), BooleanValue: func() *bool { value := true; return &value }()},
		{Kind: "integer"},
		{Kind: "integer", IntegerValue: layerdrawStringPointer("not-an-integer")},
		{Kind: "number"},
		{Kind: "number", NumberValue: layerdrawStringPointer("not-a-number")},
		{Kind: "boolean"},
	} {
		if _, err := recipeScalarFromWire(malformed); err == nil {
			t.Fatalf("malformed scalar accepted: %+v", malformed)
		}
	}
	if _, err := recipeScalarFromWire(recipeScalarWire{Kind: "unknown"}); err == nil {
		t.Fatal("unknown scalar decoded")
	}
	png := append([]byte{}, []byte("\x89PNG\r\n\x1a\n")...)
	jpeg := []byte{0xff, 0xd8, 0xff}
	webp := []byte("RIFFxxxxWEBP")
	if imageMediaType("a.png", png) != "image/png" || imageMediaType("a.jpeg", jpeg) != "image/jpeg" || imageMediaType("a.webp", webp) != "image/webp" || imageMediaType("a.svg", nil) != "image/svg+xml" || imageMediaType("a.txt", nil) != "" {
		t.Fatal("media type detection changed")
	}
}

func TestLayerdrawManifestResolvedAndBoundedHelperRejections(t *testing.T) {
	t.Parallel()
	entry := []byte("project p \"P\" {}")
	resolved := []byte("{\"format\":\"layerdraw-resolved\",\"format_version\":1,\"installs\":{},\"language\":1}\n")
	files := map[string][]byte{"manifest.json": []byte("{}"), "document.ldl": entry, "layerdraw.resolved.json": resolved}
	valid := LayerdrawManifest{
		Format: LayerdrawFormat, FormatVersion: 1, Language: 1, Entry: "document.ldl", ProjectAddress: "ldl:project:p",
		DefinitionHash: semanticHash('a'), ResolvedFileDigest: rawDigest(resolved),
		Files: map[string]string{"document.ldl": rawDigest(entry), "layerdraw.resolved.json": rawDigest(resolved)},
	}
	mutations := []func(*LayerdrawManifest){
		func(value *LayerdrawManifest) { value.FormatVersion = 2 },
		func(value *LayerdrawManifest) { value.Entry = "" },
		func(value *LayerdrawManifest) { value.Redaction = &PackageRedaction{} },
		func(value *LayerdrawManifest) { value.Files["manifest.json"] = semanticHash('a') },
		func(value *LayerdrawManifest) { delete(value.Files, "document.ldl") },
		func(value *LayerdrawManifest) { value.Files["document.ldl"] = semanticHash('b') },
		func(value *LayerdrawManifest) { value.Entry = "missing.ldl" },
		func(value *LayerdrawManifest) { value.ResolvedFileDigest = semanticHash('b') },
	}
	for index, mutate := range mutations {
		value := valid
		value.Files = map[string]string{"document.ldl": rawDigest(entry), "layerdraw.resolved.json": rawDigest(resolved)}
		mutate(&value)
		if err := validateLayerdrawManifest(value, files); err == nil {
			t.Fatalf("manifest mutation %d accepted", index)
		}
	}
	if err := validateLayerdrawManifest(valid, files); err != nil {
		t.Fatal(err)
	}
	if _, err := canonicalResolved(ResolvedDependencies{}); err == nil {
		t.Fatal("invalid resolved envelope accepted")
	}
	basePack := ResolvedPack{InstallName: "p", RegistrySource: "registry:official", CanonicalID: "pub/p", Files: []ResolvedPackFile{}, Dependencies: []ResolvedPackDependency{}}
	duplicateInstall := emptyResolvedDependencies()
	duplicateInstall.Installs = []ResolvedPack{basePack, basePack}
	if _, err := canonicalResolved(duplicateInstall); err == nil {
		t.Fatal("duplicate install accepted")
	}
	duplicateDependency := emptyResolvedDependencies()
	basePack.Dependencies = []ResolvedPackDependency{{LocalName: "d", InstallName: "p"}, {LocalName: "d", InstallName: "p"}}
	duplicateDependency.Installs = []ResolvedPack{basePack}
	if _, err := canonicalResolved(duplicateDependency); err == nil {
		t.Fatal("duplicate dependency accepted")
	}
	limits := DefaultLayerdrawLimits()
	limits.MaxTotalBytes = 1
	if err := validatePortableFiles(map[string][]byte{"a": []byte("ab")}, nil, limits); !IsLayerdrawError(err, LayerdrawErrorEntrySizeExceeded) && !IsLayerdrawError(err, LayerdrawErrorTotalSizeExceeded) {
		t.Fatalf("total size error = %v", err)
	}
	limits = DefaultLayerdrawLimits()
	if err := validatePortableFiles(map[string][]byte{"directory/": nil}, nil, limits); !IsLayerdrawError(err, LayerdrawErrorUnsafeEntry) {
		t.Fatalf("trailing slash error = %v", err)
	}
	if _, err := writeCanonicalZip(func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }(), map[string][]byte{"a": nil}); !IsLayerdrawError(err, LayerdrawErrorCancelled) {
		t.Fatalf("cancelled ZIP error = %v", err)
	}
	if _, err := canonicalArtifact(string([]byte{'x', 0xff})); err == nil {
		t.Fatal("invalid UTF-8 canonical artifact accepted")
	}
}

func TestLayerdrawStateHelperRejections(t *testing.T) {
	t.Parallel()
	instance := New(BuildInfo{})
	compiled, err := instance.Compile(context.Background(), projectCompileInput("project p \"P\" {}"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := compiled.Snapshot()
	state := validStateQuerySnapshot(t, snapshot, []StateQuerySubject{})
	state.Format = "bad"
	if _, _, _, err := canonicalStateSnapshot(context.Background(), snapshot, state); !IsLayerdrawError(err, LayerdrawErrorStateSnapshot) {
		t.Fatalf("invalid canonical state error = %v", err)
	}
	if _, err := decodeStateSnapshot([]byte("{\"format\":\"layerdraw-query-state\",\"schema_version\":1,\"definition_project_address\":\"ldl:project:p\",\"definition_hash\":\"" + semanticHash('a') + "\",\"graph_hash\":\"" + semanticHash('b') + "\",\"state_version\":\"1\",\"captured_at\":\"2026-01-01T00:00:00Z\",\"inaccessible_field_paths\":[],\"subjects\":[{\"subject_address\":\"x\",\"own_subject_hash\":\"" + semanticHash('a') + "\",\"redacted_field_paths\":[]}]}")); err == nil {
		t.Fatal("state subject without fields accepted")
	}
	files := map[string][]byte{"state/current.json": []byte("{}")}
	if _, err := validateContainerState(context.Background(), snapshot, files, nil); !IsLayerdrawError(err, LayerdrawErrorStateSnapshot) {
		t.Fatalf("unsupported state entry error = %v", err)
	}
	redacted := validStateQuerySnapshot(t, snapshot, []StateQuerySubject{})
	redacted.InaccessibleFieldPaths = []string{"provenance.source.uri"}
	stateBytes, stateHash, _, err := canonicalStateSnapshot(context.Background(), snapshot, redacted)
	if err != nil {
		t.Fatal(err)
	}
	statePath := "state/query-snapshots/" + strings.TrimPrefix(stateHash, "sha256:") + ".json"
	if _, err := validateContainerState(context.Background(), snapshot, map[string][]byte{statePath: stateBytes}, nil); !IsLayerdrawError(err, LayerdrawErrorStateSnapshot) {
		t.Fatalf("unmarked redacted state error = %v", err)
	}
	if _, err := validateContainerState(context.Background(), snapshot, map[string][]byte{"state/query-snapshots/" + strings.Repeat("0", 64) + ".json": stateBytes}, &PackageRedaction{PolicyID: "share"}); !IsLayerdrawError(err, LayerdrawErrorStateSnapshot) {
		t.Fatalf("state filename hash error = %v", err)
	}
}

type testZipEntry struct {
	name    string
	data    []byte
	method  uint16
	mode    os.FileMode
	nonUTF8 bool
}

func testZip(t *testing.T, entries []testZipEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for _, item := range entries {
		header := &zip.FileHeader{Name: item.name, Method: item.method}
		header.NonUTF8 = item.nonUTF8
		if header.Method == 0 {
			header.Method = zip.Store
		}
		if item.mode != 0 {
			header.SetMode(item.mode)
		}
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(item.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func unzipLayerdraw(t *testing.T, archive []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	result := map[string][]byte{}
	for _, file := range reader.File {
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		value, err := io.ReadAll(opened)
		if err != nil {
			t.Fatal(err)
		}
		if err := opened.Close(); err != nil {
			t.Fatal(err)
		}
		result[file.Name] = value
	}
	return result
}

func rewriteLayerdraw(t *testing.T, archive []byte, mutate func(map[string][]byte)) []byte {
	t.Helper()
	entries := unzipLayerdraw(t, archive)
	mutate(entries)
	names := sortedKeys(entries)
	items := make([]testZipEntry, 0, len(names))
	for _, name := range names {
		items = append(items, testZipEntry{name: name, data: entries[name]})
	}
	return testZip(t, items)
}

func refreshTestManifest(t *testing.T, entries map[string][]byte) {
	t.Helper()
	var manifest LayerdrawManifest
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Files = map[string]string{}
	for name, value := range entries {
		if name != "manifest.json" {
			manifest.Files[name] = rawDigest(value)
		}
	}
	manifest.ResolvedFileDigest = rawDigest(entries["layerdraw.resolved.json"])
	var err error
	entries["manifest.json"], err = canonicalArtifact(manifest)
	if err != nil {
		t.Fatal(err)
	}
}
