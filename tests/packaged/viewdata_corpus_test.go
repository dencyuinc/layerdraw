// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package packaged_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	transport "github.com/dencyuinc/layerdraw/internal/transport/stdio"
)

type packagedViewDataCorpus struct {
	SchemaVersion   int                            `json:"schema_version"`
	OperationLimits engineprotocol.WorkbenchLimits `json:"operation_limits"`
	Documents       []struct {
		ID    string                      `json:"id"`
		Input engineprotocol.CompileInput `json:"input"`
		Blobs []struct {
			BlobID      string `json:"blob_id"`
			BytesBase64 string `json:"bytes_base64"`
		} `json:"blobs"`
	} `json:"documents"`
	Cases []struct {
		Name      string `json:"name"`
		Execution string `json:"execution"`
		Source    struct {
			Kind           string                           `json:"kind"`
			Document       string                           `json:"document"`
			QueryAddress   semantic.QueryAddress            `json:"query_address"`
			Arguments      map[string]semantic.RecipeScalar `json:"arguments"`
			StateSnapshot  *semantic.StateQuerySnapshot     `json:"state_snapshot"`
			RecipeDocument string                           `json:"recipe_document"`
			BeforeDocument string                           `json:"before_document"`
			AfterDocument  string                           `json:"after_document"`
		} `json:"source"`
		View     semantic.ViewAddress           `json:"view_address"`
		Limits   engineprotocol.WorkbenchLimits `json:"limits"`
		Mutation string                         `json:"mutation"`
		Expected struct {
			Outcome            string          `json:"outcome"`
			PublishesViewData  bool            `json:"publishes_view_data"`
			NormalizedResponse json.RawMessage `json:"normalized_response"`
		} `json:"expected"`
	} `json:"cases"`
}

type packagedOpenedViewData struct {
	ID         string
	Generation engineprotocol.DocumentGeneration
	Handle     engineprotocol.DocumentHandle
}

func TestPackagedStdioExecutesViewDataCorpus(t *testing.T) {
	binary := os.Getenv("LAYERDRAW_ENGINE_BINARY")
	if binary == "" {
		t.Skip("LAYERDRAW_ENGINE_BINARY is not set")
	}
	data, err := os.ReadFile(filepath.Join("..", "conformance", "testdata", "viewdata_conformance_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus packagedViewDataCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != 1 || len(corpus.Documents) != 6 || len(corpus.Cases) != 20 {
		t.Fatalf("incomplete ViewData corpus: documents=%d cases=%d", len(corpus.Documents), len(corpus.Cases))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
	encoder, frameDecoder := transport.NewEncoder(stdin), transport.NewDecoder(stdout)
	streamID := uint64(1)
	handshake := packagedViewDataHandshake()
	control, err := engineprotocol.EncodeHandshakeRequestEnvelope(handshake)
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindRequestControl, StreamID: streamID, Payload: control}); err != nil {
		t.Fatal(err)
	}
	handshakeFrame := packagedReadFrame(t, frameDecoder)
	if handshakeFrame.Kind != transport.KindResponseControl || handshakeFrame.StreamID != streamID {
		t.Fatalf("handshake response=%#v", handshakeFrame)
	}
	handshakeControl := handshakeFrame.Payload
	if end := packagedReadFrame(t, frameDecoder); end.Kind != transport.KindBundleEnd || end.StreamID != streamID {
		t.Fatalf("handshake end=%#v", end)
	}
	response, err := engineprotocol.DecodeHandshakeResponseEnvelope(handshakeControl)
	if err != nil || response.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("ViewData handshake=%+v err=%v", response, err)
	}
	streamID++

	documents := make(map[string]int, len(corpus.Documents))
	for index, document := range corpus.Documents {
		documents[document.ID] = index
	}
	for _, testCase := range corpus.Cases {
		if testCase.Execution != "materialize" {
			continue
		}
		opened := map[string]packagedOpenedViewData{}
		open := func(id string) packagedOpenedViewData {
			if value, ok := opened[id]; ok {
				return value
			}
			index, ok := documents[id]
			if !ok {
				t.Fatalf("%s unknown document %s", testCase.Name, id)
			}
			document := corpus.Documents[index]
			request := engineprotocol.OpenDocumentRequestEnvelope{
				Operation: engineprotocol.OpenDocumentRequestEnvelopeOperationValue,
				Payload:   engineprotocol.OpenDocumentInput{CompileInput: document.Input, RequestedLimits: corpus.OperationLimits},
				Protocol:  packagedProtocolRef(), RequestID: "packaged-viewdata-" + testCase.Name + "-open-" + id,
			}
			control, encodeErr := engineprotocol.EncodeOpenDocumentRequestEnvelope(request)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			blobs := make([]packagedViewDataBlob, len(document.Blobs))
			for index, blob := range document.Blobs {
				value, decodeErr := base64.StdEncoding.DecodeString(blob.BytesBase64)
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				blobs[index] = packagedViewDataBlob{ID: blob.BlobID, Bytes: value}
			}
			responseControl := packagedViewDataExchange(t, encoder, frameDecoder, streamID, control, blobs)
			streamID++
			response, decodeErr := engineprotocol.DecodeOpenDocumentResponseEnvelope(responseControl)
			if decodeErr != nil || response.Outcome != protocolcommon.OutcomeSuccess || response.Payload == nil {
				t.Fatalf("%s open %s=%+v err=%v", testCase.Name, id, response, decodeErr)
			}
			value := packagedOpenedViewData{ID: id, Generation: response.Payload.DocumentGeneration, Handle: response.Payload.DocumentHandle}
			opened[id] = value
			return value
		}

		var input engineprotocol.MaterializeViewInput
		switch testCase.Source.Kind {
		case "query":
			document := open(testCase.Source.Document)
			arguments := testCase.Source.Arguments
			if arguments == nil {
				arguments = map[string]semantic.RecipeScalar{}
			}
			request := engineprotocol.ExecuteQueryRequestEnvelope{
				Operation: engineprotocol.ExecuteQueryRequestEnvelopeOperationValue,
				Payload:   engineprotocol.ExecuteQueryInput{Arguments: arguments, DocumentGeneration: document.Generation, Limits: corpus.OperationLimits, QueryAddress: testCase.Source.QueryAddress},
				Protocol:  packagedProtocolRef(), RequestID: "viewdata-" + testCase.Name + "-query",
			}
			control, encodeErr := engineprotocol.EncodeExecuteQueryRequestEnvelope(request)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			responseControl := packagedViewDataExchange(t, encoder, frameDecoder, streamID, control, nil)
			streamID++
			response, decodeErr := engineprotocol.DecodeExecuteQueryResponseEnvelope(responseControl)
			if decodeErr != nil || response.Outcome != protocolcommon.OutcomeSuccess || response.Payload == nil {
				t.Fatalf("%s query=%+v err=%v", testCase.Name, response, decodeErr)
			}
			result := response.Payload.Result
			if testCase.Mutation == "mismatched_query" {
				result.QueryAddress = "ldl:project:p:query:missing"
			}
			input = engineprotocol.MaterializeViewInput{
				Kind: "query", Limits: testCase.Limits, ViewAddress: testCase.View,
				Query: &engineprotocol.MaterializeQueryViewInput{DocumentGeneration: document.Generation, QueryResult: result, StateSnapshot: testCase.Source.StateSnapshot},
			}
		case "diff":
			recipe, before, after := open(testCase.Source.RecipeDocument), open(testCase.Source.BeforeDocument), open(testCase.Source.AfterDocument)
			input = engineprotocol.MaterializeViewInput{
				Kind: "diff", Limits: testCase.Limits, ViewAddress: testCase.View,
				Diff: &engineprotocol.MaterializeDiffViewInput{RecipeGeneration: recipe.Generation, BeforeGeneration: before.Generation, AfterGeneration: after.Generation},
			}
		default:
			t.Fatalf("%s unsupported source %s", testCase.Name, testCase.Source.Kind)
		}
		request := engineprotocol.MaterializeViewRequestEnvelope{
			Operation: engineprotocol.MaterializeViewRequestEnvelopeOperationValue,
			Payload:   input, Protocol: packagedProtocolRef(), RequestID: "viewdata-" + testCase.Name + "-materialize",
		}
		control, err := engineprotocol.EncodeMaterializeViewRequestEnvelope(request)
		if err != nil {
			t.Fatal(err)
		}
		actual := packagedViewDataExchange(t, encoder, frameDecoder, streamID, control, nil)
		streamID++
		packagedAssertViewDataResponse(t, actual, testCase.Expected.NormalizedResponse, testCase.Expected.Outcome, testCase.Expected.PublishesViewData, opened)

		ids := make([]string, 0, len(opened))
		for id := range opened {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			document := opened[id]
			request := engineprotocol.CloseDocumentRequestEnvelope{
				Operation: engineprotocol.CloseDocumentRequestEnvelopeOperationValue,
				Payload:   engineprotocol.CloseDocumentInput{DocumentGeneration: document.Generation, DocumentHandle: document.Handle},
				Protocol:  packagedProtocolRef(), RequestID: "packaged-viewdata-" + testCase.Name + "-close-" + id,
			}
			control, encodeErr := engineprotocol.EncodeCloseDocumentRequestEnvelope(request)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			responseControl := packagedViewDataExchange(t, encoder, frameDecoder, streamID, control, nil)
			streamID++
			response, decodeErr := engineprotocol.DecodeCloseDocumentResponseEnvelope(responseControl)
			if decodeErr != nil || response.Outcome != protocolcommon.OutcomeSuccess {
				t.Fatalf("%s close %s=%+v err=%v", testCase.Name, id, response, decodeErr)
			}
		}
	}
	malformed := []byte(`{"operation":"engine.materialize_view","payload":{},"protocol":{"name":"engine","version":"1.0"},"request_id":"viewdata-malformed_materialize_wire-materialize"}`)
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindRequestControl, StreamID: streamID, Payload: malformed}); err != nil {
		t.Fatal(err)
	}
	malformedResponse := packagedReadFrame(t, frameDecoder)
	if malformedResponse.Kind != transport.KindResponseControl || malformedResponse.StreamID != streamID || bytes.Contains(malformedResponse.Payload, []byte(`"view_data"`)) {
		t.Fatalf("malformed ViewData response=%#v", malformedResponse)
	}
	var malformedEnvelope struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(malformedResponse.Payload, &malformedEnvelope); err != nil || malformedEnvelope.Outcome == "success" {
		t.Fatalf("malformed ViewData envelope=%+v err=%v", malformedEnvelope, err)
	}
	if end := packagedReadFrame(t, frameDecoder); end.Kind != transport.KindBundleEnd || end.StreamID != streamID {
		t.Fatalf("malformed ViewData end=%#v", end)
	}
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindClose}); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	if err := command.Wait(); err != nil || stderr.Len() != 0 {
		t.Fatalf("ViewData sidecar=%v stderr=%q", err, stderr.String())
	}
}

type packagedViewDataBlob struct {
	ID    string
	Bytes []byte
}

func packagedViewDataExchange(t *testing.T, encoder *transport.Encoder, decoder *transport.Decoder, streamID uint64, control []byte, blobs []packagedViewDataBlob) []byte {
	t.Helper()
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindRequestControl, StreamID: streamID, Payload: control}); err != nil {
		t.Fatal(err)
	}
	ready := packagedReadFrame(t, decoder)
	if ready.Kind != transport.KindRequestReady || ready.StreamID != streamID {
		t.Fatalf("stream %d ready=%#v", streamID, ready)
	}
	frames := make([]transport.Frame, 0, len(blobs)+1)
	for index, blob := range blobs {
		frames = append(frames, transport.Frame{Kind: transport.KindBlobChunk, Flags: transport.FlagFinal, StreamID: streamID, Sequence: uint32(index + 1), Name: []byte(blob.ID), Payload: blob.Bytes})
	}
	frames = append(frames, transport.Frame{Kind: transport.KindBundleEnd, StreamID: streamID, Sequence: uint32(len(frames) + 1)})
	if err := encoder.WriteFrames(frames); err != nil {
		t.Fatal(err)
	}
	response := packagedReadFrame(t, decoder)
	if response.Kind != transport.KindResponseControl || response.StreamID != streamID {
		t.Fatalf("stream %d response=%#v", streamID, response)
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
		t.Fatalf("stream %d published unexpected blob %q", streamID, frame.Name)
	}
	return response.Payload
}

func packagedAssertViewDataResponse(t *testing.T, actual []byte, expected json.RawMessage, outcome string, publishes bool, opened map[string]packagedOpenedViewData) {
	t.Helper()
	var got, want any
	if err := json.Unmarshal(actual, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(expected, &want); err != nil {
		t.Fatal(err)
	}
	handles := map[string]string{}
	for id, document := range opened {
		handles[document.Handle.Value] = id
	}
	got = packagedNormalizeViewData(got, handles)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ViewData response differs from in-process oracle\ngot:  %s\nwant: %s", mustJSON(got), mustJSON(want))
	}
	value := got.(map[string]any)
	payload, hasPayload := value["payload"].(map[string]any)
	_, hasViewData := payload["view_data"]
	if value["outcome"] != outcome || (hasPayload && hasViewData) != publishes {
		t.Fatalf("ViewData terminality=%v/%v want=%s/%v", value["outcome"], hasViewData, outcome, publishes)
	}
}

func packagedNormalizeViewData(value any, handles map[string]string) any {
	switch current := value.(type) {
	case []any:
		for index := range current {
			current[index] = packagedNormalizeViewData(current[index], handles)
		}
	case map[string]any:
		for key, child := range current {
			switch key {
			case "engine_release":
				current[key] = "$engine_release"
			case "returned_bytes":
				current[key] = "$returned_bytes"
			case "endpoint_instance_id":
				current[key] = "$endpoint"
			default:
				current[key] = packagedNormalizeViewData(child, handles)
			}
		}
	case string:
		if id, ok := handles[current]; ok {
			return "$document:" + id
		}
		for handle, id := range handles {
			if strings.HasPrefix(current, "workbench:"+handle+":") {
				return "$revision:" + id
			}
		}
	}
	return value
}

func packagedViewDataHandshake() engineprotocol.HandshakeRequestEnvelope {
	return engineprotocol.HandshakeRequestEnvelope{
		Operation: engineprotocol.HandshakeRequestEnvelopeOperationValue,
		Payload: protocolcommon.HandshakeRequest{
			ClientRelease: "1.0.0", OptionalCapabilities: []protocolcommon.CapabilityID{},
			Protocols:            []protocolcommon.ProtocolOffer{{Name: endpoint.ProtocolName, SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: endpoint.ProtocolVersion, SchemaDigest: protocolcommon.Digest(engineprotocol.SchemaDigest)}}}},
			RequiredCapabilities: []protocolcommon.CapabilityID{endpoint.OperationOpenDocument, endpoint.OperationExecuteQuery, endpoint.OperationMaterializeView, endpoint.OperationCloseDocument},
		},
		Protocol: packagedProtocolRef(), RequestID: "packaged-viewdata-handshake",
	}
}

func packagedProtocolRef() engineprotocol.EngineProtocolRef {
	return engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue}
}

func mustJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}
