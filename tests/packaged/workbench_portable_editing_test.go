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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	transport "github.com/dencyuinc/layerdraw/internal/transport/stdio"
)

type packagedWorkbenchCorpus struct {
	SchemaVersion int `json:"schema_version"`
	Scenarios     []struct {
		Name   string `json:"name"`
		Source string `json:"source"`
		Patch  struct {
			ModulePath string `json:"module_path"`
			Find       string `json:"find"`
			Replace    string `json:"replace"`
		} `json:"patch"`
		Expected map[string]any `json:"expected"`
	} `json:"scenarios"`
}

func TestPackagedStdioExecutesPortableWorkbenchEditingCorpus(t *testing.T) {
	binary := os.Getenv("LAYERDRAW_ENGINE_BINARY")
	if binary == "" {
		t.Skip("LAYERDRAW_ENGINE_BINARY is not set")
	}
	corpus := readPackagedWorkbenchCorpus(t)
	if corpus.SchemaVersion != 1 || len(corpus.Scenarios) != 1 {
		t.Fatalf("portable Workbench corpus is incomplete: %+v", corpus)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
	packagedWorkbenchHandshake(t, encoder, decoder)

	streamID := uint64(2)
	for _, scenario := range corpus.Scenarios {
		source := []byte(scenario.Source)
		sourceRef := packagedWorkbenchBlobRef("workbench-source", "text/plain; charset=utf-8", source)
		open := engineprotocol.OpenDocumentRequestEnvelope{
			Operation: engineprotocol.OpenDocumentRequestEnvelopeOperationValue,
			Payload: engineprotocol.OpenDocumentInput{
				CompileInput: engineprotocol.CompileInput{
					Mode:                 engineprotocol.CompileModeProject,
					EntryPath:            "document.ldl",
					ProjectSourceTree:    []engineprotocol.SourceFileInput{{Path: "document.ldl", Blob: sourceRef}},
					InstalledPackTree:    []engineprotocol.SourceFileInput{},
					ReferencedAssets:     []engineprotocol.AssetInput{},
					ResolvedDependencies: engineprotocol.ResolvedDependencies{Format: engineprotocol.ResolvedDependenciesFormatValue, FormatVersion: 1, Language: 1, Installs: []engineprotocol.ResolvedPack{}},
					ResourceLimits:       engineprotocol.ResourceLimits{},
				},
				RequestedLimits: packagedWorkbenchLimits(),
			},
			Protocol:  packagedEngineProtocolRef(),
			RequestID: scenario.Name + "-open",
		}
		openControl, err := engineprotocol.EncodeOpenDocumentRequestEnvelope(open)
		if err != nil {
			t.Fatal(err)
		}
		openResponseControl, _ := packagedSendWorkbench(t, encoder, decoder, streamID, openControl, []packagedBlob{{id: sourceRef.BlobID, bytes: source}})
		streamID++
		openResponse, err := engineprotocol.DecodeOpenDocumentResponseEnvelope(openResponseControl)
		if err != nil || openResponse.Outcome != protocolcommon.OutcomeSuccess || openResponse.Payload == nil {
			t.Fatalf("%s open = %+v err=%v", scenario.Name, openResponse, err)
		}
		if got := openResponse.Payload.State.StateKind; got != packagedWantString(t, scenario.Expected, "open_state_kind") {
			t.Fatalf("%s state_kind=%s", scenario.Name, got)
		}
		if got := openResponse.Payload.State.SemanticState; got != packagedWantString(t, scenario.Expected, "open_semantic_state") {
			t.Fatalf("%s semantic_state=%s", scenario.Name, got)
		}
		if got := string(openResponse.Payload.DocumentGeneration.Value); got != packagedWantString(t, scenario.Expected, "initial_generation") {
			t.Fatalf("%s generation=%s", scenario.Name, got)
		}

		start := strings.Index(scenario.Source, scenario.Patch.Find)
		if start < 0 {
			t.Fatalf("%s patch token %q not found", scenario.Name, scenario.Patch.Find)
		}
		replacement := []byte(scenario.Patch.Replace)
		replacementRef := packagedWorkbenchBlobRef("workbench-replacement", "text/plain; charset=utf-8", replacement)
		sourceDigests := []engineprotocol.ExpectedSourceDigest{{
			Module: semantic.ModuleRef{Origin: semantic.SourceOrigin{Kind: semantic.OriginKindProject}, ModulePath: scenario.Patch.ModulePath},
			Digest: sourceRef.Digest,
		}}
		preview := engineprotocol.PreviewSourcePatchRequestEnvelope{
			Operation: engineprotocol.PreviewSourcePatchRequestEnvelopeOperationValue,
			Payload: engineprotocol.PreviewSourcePatchInput{
				Limits: packagedWorkbenchLimits(),
				Preconditions: engineprotocol.EngineEditPreconditions{
					DocumentGeneration:    openResponse.Payload.DocumentGeneration,
					ExpectedChildSets:     []engineprotocol.ExpectedChildSet{},
					ExpectedSourceDigests: &sourceDigests,
					ExpectedSubjectHashes: []engineprotocol.ExpectedHash{},
					ExpectedSubtreeHashes: []engineprotocol.ExpectedHash{},
				},
				Patch: engineprotocol.SourcePatchBatch{Patches: []engineprotocol.SourcePatchInput{{
					ExpectedSourceDigest: sourceRef.Digest,
					ReplacementBlob:      replacementRef,
					SourceRange: semantic.SourceRange{
						Origin:     semantic.SourceOrigin{Kind: semantic.OriginKindProject},
						ModulePath: scenario.Patch.ModulePath,
						StartByte:  protocolcommon.CanonicalUint64(strconv.Itoa(start)),
						EndByte:    protocolcommon.CanonicalUint64(strconv.Itoa(start + len(scenario.Patch.Find))),
					},
				}}},
			},
			Protocol:  packagedEngineProtocolRef(),
			RequestID: scenario.Name + "-preview",
		}
		previewControl, err := engineprotocol.EncodePreviewSourcePatchRequestEnvelope(preview)
		if err != nil {
			t.Fatal(err)
		}
		previewResponseControl, _ := packagedSendWorkbench(t, encoder, decoder, streamID, previewControl, []packagedBlob{{id: replacementRef.BlobID, bytes: replacement}})
		streamID++
		previewResponse, err := engineprotocol.DecodePreviewSourcePatchResponseEnvelope(previewResponseControl)
		if err != nil || previewResponse.Outcome != protocolcommon.OutcomeSuccess || previewResponse.Payload == nil {
			t.Fatalf("%s preview = %+v err=%v", scenario.Name, previewResponse, err)
		}
		if previewResponse.Payload.Status != packagedWantString(t, scenario.Expected, "preview_status") || previewResponse.Payload.PreviewID == nil || previewResponse.Payload.PreviewDigest == nil {
			t.Fatalf("%s preview payload = %+v", scenario.Name, previewResponse.Payload)
		}
		if len(previewResponse.Payload.ChangedSourceFiles) != 1 || previewResponse.Payload.ChangedSourceFiles[0].ModulePath != scenario.Patch.ModulePath {
			t.Fatalf("%s changed files = %+v", scenario.Name, previewResponse.Payload.ChangedSourceFiles)
		}

		apply := engineprotocol.ApplyToHandleRequestEnvelope{
			Operation: engineprotocol.ApplyToHandleRequestEnvelopeOperationValue,
			Payload: engineprotocol.ApplyToHandleInput{
				BaseGeneration: openResponse.Payload.DocumentGeneration,
				PreviewDigest:  *previewResponse.Payload.PreviewDigest,
				PreviewID:      *previewResponse.Payload.PreviewID,
			},
			Protocol:  packagedEngineProtocolRef(),
			RequestID: scenario.Name + "-apply",
		}
		applyControl, err := engineprotocol.EncodeApplyToHandleRequestEnvelope(apply)
		if err != nil {
			t.Fatal(err)
		}
		applyResponseControl, _ := packagedSendWorkbench(t, encoder, decoder, streamID, applyControl, nil)
		streamID++
		applyResponse, err := engineprotocol.DecodeApplyToHandleResponseEnvelope(applyResponseControl)
		if err != nil || applyResponse.Outcome != protocolcommon.OutcomeSuccess || applyResponse.Payload == nil {
			t.Fatalf("%s apply = %+v err=%v", scenario.Name, applyResponse, err)
		}
		if got := string(applyResponse.Payload.DocumentGeneration.Value); got != packagedWantString(t, scenario.Expected, "applied_generation") {
			t.Fatalf("%s applied generation=%s", scenario.Name, got)
		}
		if applyResponse.Payload.AuthoringImpact.ImpactDigest == "" || applyResponse.Payload.ResultingHashes.DefinitionHash == "" || applyResponse.Payload.SourceDiff.Digest == "" {
			t.Fatalf("%s incomplete apply result = %+v", scenario.Name, applyResponse.Payload)
		}

		read := engineprotocol.ReadModulesRequestEnvelope{
			Operation: engineprotocol.ReadModulesRequestEnvelopeOperationValue,
			Payload: engineprotocol.ReadModulesInput{
				DocumentGeneration: applyResponse.Payload.DocumentGeneration,
				Limits:             packagedWorkbenchLimits(),
				Modules:            []semantic.ModuleRef{{Origin: semantic.SourceOrigin{Kind: semantic.OriginKindProject}, ModulePath: scenario.Patch.ModulePath}},
			},
			Protocol:  packagedEngineProtocolRef(),
			RequestID: scenario.Name + "-read",
		}
		readControl, err := engineprotocol.EncodeReadModulesRequestEnvelope(read)
		if err != nil {
			t.Fatal(err)
		}
		readResponseControl, readBlobs := packagedSendWorkbench(t, encoder, decoder, streamID, readControl, nil)
		streamID++
		readResponse, err := engineprotocol.DecodeReadModulesResponseEnvelope(readResponseControl)
		if err != nil || readResponse.Outcome != protocolcommon.OutcomeSuccess || readResponse.Payload == nil || len(readResponse.Payload.Items) != 1 {
			t.Fatalf("%s read = %+v err=%v", scenario.Name, readResponse, err)
		}
		content := readBlobs[readResponse.Payload.Items[0].SourceChunk.Blob.BlobID]
		if !bytes.Contains(content, []byte(packagedWantString(t, scenario.Expected, "final_contains"))) {
			t.Fatalf("%s final content = %q", scenario.Name, content)
		}

		staleApplyControl, err := engineprotocol.EncodeApplyToHandleRequestEnvelope(apply)
		if err != nil {
			t.Fatal(err)
		}
		staleResponseControl, _ := packagedSendWorkbench(t, encoder, decoder, streamID, staleApplyControl, nil)
		streamID++
		staleResponse, err := engineprotocol.DecodeApplyToHandleResponseEnvelope(staleResponseControl)
		if err != nil || string(staleResponse.Outcome) != packagedWantString(t, scenario.Expected, "stale_apply_outcome") || len(staleResponse.Diagnostics) != 1 || staleResponse.Diagnostics[0].Code != packagedWantString(t, scenario.Expected, "stale_apply_diagnostic_code") {
			t.Fatalf("%s stale apply = %+v err=%v", scenario.Name, staleResponse, err)
		}

		closeDocument := engineprotocol.CloseDocumentRequestEnvelope{
			Operation: engineprotocol.CloseDocumentRequestEnvelopeOperationValue,
			Payload: engineprotocol.CloseDocumentInput{
				DocumentHandle:     openResponse.Payload.DocumentHandle,
				DocumentGeneration: applyResponse.Payload.DocumentGeneration,
			},
			Protocol:  packagedEngineProtocolRef(),
			RequestID: scenario.Name + "-close",
		}
		closeControl, err := engineprotocol.EncodeCloseDocumentRequestEnvelope(closeDocument)
		if err != nil {
			t.Fatal(err)
		}
		closeResponseControl, _ := packagedSendWorkbench(t, encoder, decoder, streamID, closeControl, nil)
		streamID++
		closeResponse, err := engineprotocol.DecodeCloseDocumentResponseEnvelope(closeResponseControl)
		if err != nil || string(closeResponse.Outcome) != packagedWantString(t, scenario.Expected, "close_outcome") || closeResponse.Payload == nil || !closeResponse.Payload.Closed {
			t.Fatalf("%s close = %+v err=%v", scenario.Name, closeResponse, err)
		}
	}
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindClose}); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	if err := command.Wait(); err != nil || stderr.Len() != 0 {
		t.Fatalf("portable Workbench stdio=%v stderr=%q", err, stderr.String())
	}
}

func readPackagedWorkbenchCorpus(t *testing.T) packagedWorkbenchCorpus {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "conformance", "testdata", "workbench_portable_editing_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus packagedWorkbenchCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	return corpus
}

func packagedWorkbenchHandshake(t *testing.T, encoder *transport.Encoder, decoder *transport.Decoder) {
	t.Helper()
	handshake := packagedHandshake()
	handshake.Payload.RequiredCapabilities = []protocolcommon.CapabilityID{
		"engine.open_document",
		"engine.preview_source_patch",
		"engine.apply_to_handle",
		"engine.read_modules",
		"engine.close_document",
	}
	control, err := engineprotocol.EncodeHandshakeRequestEnvelope(handshake)
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindRequestControl, StreamID: 1, Payload: control}); err != nil {
		t.Fatal(err)
	}
	responseFrame := packagedReadFrame(t, decoder)
	response, err := engineprotocol.DecodeHandshakeResponseEnvelope(responseFrame.Payload)
	if err != nil || response.Outcome != protocolcommon.OutcomeSuccess || response.Payload == nil {
		t.Fatalf("workbench handshake = %+v err=%v", response, err)
	}
	packagedReadFrame(t, decoder)
}

func packagedSendWorkbench(t *testing.T, encoder *transport.Encoder, decoder *transport.Decoder, streamID uint64, control []byte, blobs []packagedBlob) ([]byte, map[string][]byte) {
	t.Helper()
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindRequestControl, StreamID: streamID, Payload: control}); err != nil {
		t.Fatal(err)
	}
	if ready := packagedReadFrame(t, decoder); ready.Kind != transport.KindRequestReady || ready.StreamID != streamID {
		t.Fatalf("workbench ready = %#v", ready)
	}
	frames := make([]transport.Frame, 0, len(blobs)+1)
	for index, blob := range blobs {
		frames = append(frames, transport.Frame{Kind: transport.KindBlobChunk, Flags: transport.FlagFinal, StreamID: streamID, Sequence: uint32(index + 1), Name: []byte(blob.id), Payload: blob.bytes})
	}
	frames = append(frames, transport.Frame{Kind: transport.KindBundleEnd, StreamID: streamID, Sequence: uint32(len(blobs) + 1)})
	if err := encoder.WriteFrames(frames); err != nil {
		t.Fatal(err)
	}
	response := packagedReadFrame(t, decoder)
	if response.Kind != transport.KindResponseControl || response.StreamID != streamID {
		t.Fatalf("workbench response = %#v", response)
	}
	outputs := map[string][]byte{}
	validator := transport.NewBundleValidator()
	for {
		frame := packagedReadFrame(t, decoder)
		if err := validator.Accept(frame); err != nil {
			t.Fatal(err)
		}
		if frame.Kind == transport.KindBundleEnd {
			break
		}
		outputs[string(frame.Name)] = append(outputs[string(frame.Name)], frame.Payload...)
	}
	return response.Payload, outputs
}

func packagedWorkbenchBlobRef(id, mediaType string, data []byte) protocolcommon.BlobRef {
	sum := sha256.Sum256(data)
	return protocolcommon.BlobRef{
		BlobID:    id,
		Digest:    protocolcommon.Digest("sha256:" + hex.EncodeToString(sum[:])),
		Lifetime:  protocolcommon.BlobLifetimeRequest,
		MediaType: mediaType,
		Size:      protocolcommon.CanonicalUint64(strconv.Itoa(len(data))),
	}
}

func packagedWorkbenchLimits() engineprotocol.WorkbenchLimits {
	return engineprotocol.WorkbenchLimits{MaxItems: "64", MaxOutputBytes: "65536"}
}

func packagedEngineProtocolRef() engineprotocol.EngineProtocolRef {
	return engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue}
}

func packagedWantString(t *testing.T, expected map[string]any, key string) string {
	t.Helper()
	value, ok := expected[key].(string)
	if !ok || value == "" {
		t.Fatalf("expected %s is not a nonempty string: %#v", key, expected[key])
	}
	return value
}
