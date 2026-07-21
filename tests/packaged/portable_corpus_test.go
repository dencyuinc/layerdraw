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
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	transport "github.com/dencyuinc/layerdraw/internal/transport/stdio"
)

type portableCorpus struct {
	SchemaVersion    int      `json:"schema_version"`
	RequiredFeatures []string `json:"required_features"`
	Normalization    []string `json:"normalization"`
	Cases            []struct {
		Name      string   `json:"name"`
		Features  []string `json:"features"`
		Execution string   `json:"execution"`
		Request   struct {
			ControlBase64 string `json:"control_base64"`
			Blobs         []struct {
				BlobID      string `json:"blob_id"`
				BytesBase64 string `json:"bytes_base64"`
			} `json:"blobs"`
		} `json:"request"`
		Expected struct {
			Outcome  string          `json:"outcome"`
			Response json.RawMessage `json:"response"`
			Blobs    []struct {
				BlobID      string `json:"blob_id"`
				BytesBase64 string `json:"bytes_base64"`
			} `json:"blobs"`
		} `json:"expected"`
	} `json:"cases"`
}

func TestPackagedStdioExecutesPortableCompilerCorpus(t *testing.T) {
	binary := os.Getenv("LAYERDRAW_ENGINE_BINARY")
	if binary == "" {
		t.Skip("LAYERDRAW_ENGINE_BINARY is not set")
	}
	data, err := os.ReadFile(filepath.Join("..", "conformance", "testdata", "engine_compile_parity_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus portableCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != 1 || len(corpus.Cases) != 14 || len(corpus.RequiredFeatures) != 13 || len(corpus.Normalization) != 3 {
		t.Fatalf("portable corpus is incomplete: cases=%d features=%d normalization=%d", len(corpus.Cases), len(corpus.RequiredFeatures), len(corpus.Normalization))
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
	handshakeBytes, err := engineprotocol.EncodeHandshakeRequestEnvelope(packagedHandshake())
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindRequestControl, StreamID: 1, Payload: handshakeBytes}); err != nil {
		t.Fatal(err)
	}
	handshakeFrame := packagedReadFrame(t, decoder)
	handshake, err := engineprotocol.DecodeHandshakeResponseEnvelope(handshakeFrame.Payload)
	if err != nil || handshake.Outcome != protocolcommon.OutcomeSuccess || handshake.Payload == nil {
		t.Fatalf("portable corpus handshake=%+v err=%v", handshake, err)
	}
	packagedReadFrame(t, decoder)

	streamID := uint64(2)
	for _, testCase := range corpus.Cases {
		if testCase.Execution == "cancel" {
			continue
		}
		control, err := base64.StdEncoding.DecodeString(testCase.Request.ControlBase64)
		if err != nil {
			t.Fatalf("%s control: %v", testCase.Name, err)
		}
		if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindRequestControl, StreamID: streamID, Payload: control}); err != nil {
			t.Fatal(err)
		}
		if ready := packagedReadFrame(t, decoder); ready.Kind != transport.KindRequestReady || ready.StreamID != streamID {
			t.Fatalf("%s ready=%#v", testCase.Name, ready)
		}
		frames := make([]transport.Frame, 0, len(testCase.Request.Blobs)+1)
		for index, blob := range testCase.Request.Blobs {
			value, err := base64.StdEncoding.DecodeString(blob.BytesBase64)
			if err != nil {
				t.Fatalf("%s %s: %v", testCase.Name, blob.BlobID, err)
			}
			frames = append(frames, transport.Frame{Kind: transport.KindBlobChunk, Flags: transport.FlagFinal, StreamID: streamID, Sequence: uint32(index + 1), Name: []byte(blob.BlobID), Payload: value})
		}
		frames = append(frames, transport.Frame{Kind: transport.KindBundleEnd, StreamID: streamID, Sequence: uint32(len(frames) + 1)})
		if err := encoder.WriteFrames(frames); err != nil {
			t.Fatal(err)
		}
		responseFrame := packagedReadFrame(t, decoder)
		if responseFrame.Kind != transport.KindResponseControl || responseFrame.StreamID != streamID {
			t.Fatalf("%s response frame=%#v", testCase.Name, responseFrame)
		}
		var actual, expected map[string]any
		if err := json.Unmarshal(responseFrame.Payload, &actual); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(testCase.Expected.Response, &expected); err != nil {
			t.Fatal(err)
		}
		actual["engine_release"] = "$engine_release"
		if !reflect.DeepEqual(actual, expected) || actual["outcome"] != testCase.Expected.Outcome {
			t.Fatalf("%s response differs from in-process oracle", testCase.Name)
		}
		actualBlobs := map[string][]byte{}
		actualOrder := []string{}
		validator := transport.NewBundleValidator()
		for {
			frame := packagedReadFrame(t, decoder)
			if err := validator.Accept(frame); err != nil {
				t.Fatal(err)
			}
			if frame.Kind == transport.KindBundleEnd {
				break
			}
			id := string(frame.Name)
			if _, exists := actualBlobs[id]; !exists {
				actualOrder = append(actualOrder, id)
			}
			actualBlobs[id] = append(actualBlobs[id], frame.Payload...)
		}
		wantOrder := make([]string, len(testCase.Expected.Blobs))
		for index, blob := range testCase.Expected.Blobs {
			wantOrder[index] = blob.BlobID
			want, err := base64.StdEncoding.DecodeString(blob.BytesBase64)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(actualBlobs[blob.BlobID], want) {
				t.Fatalf("%s %s bytes differ from in-process oracle", testCase.Name, blob.BlobID)
			}
		}
		if !reflect.DeepEqual(actualOrder, wantOrder) {
			t.Fatalf("%s output order=%v want=%v", testCase.Name, actualOrder, wantOrder)
		}
		streamID++
	}
	if err := encoder.WriteFrame(transport.Frame{Kind: transport.KindClose}); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	if err := command.Wait(); err != nil || stderr.Len() != 0 {
		t.Fatalf("portable corpus sidecar=%v stderr=%q", err, stderr.String())
	}
}
