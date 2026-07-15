// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
)

func TestKindStringIsClosed(t *testing.T) {
	t.Parallel()
	expected := []string{
		"UNKNOWN",
		"REQUEST_CONTROL",
		"REQUEST_READY",
		"BLOB_CHUNK",
		"BUNDLE_END",
		"CANCEL",
		"RESPONSE_CONTROL",
		"CLOSE",
		"STREAM_ERROR",
		"UNKNOWN",
	}
	for value, want := range expected {
		if got := Kind(value).String(); got != want {
			t.Fatalf("Kind(%d).String() = %q, want %q", value, got, want)
		}
	}
}

func TestFrameRoundTripsEveryKind(t *testing.T) {
	t.Parallel()
	for _, frame := range validFrames() {
		frame := frame
		t.Run(frame.Kind.String(), func(t *testing.T) {
			t.Parallel()
			encoded, err := MarshalFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := UnmarshalFrame(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if !equalFrame(decoded, frame) {
				t.Fatalf("round trip mismatch\n got: %#v\nwant: %#v", decoded, frame)
			}
		})
	}
}

func TestHeaderUsesExactBigEndianLayout(t *testing.T) {
	t.Parallel()
	frame := Frame{
		Kind:     KindBlobChunk,
		Flags:    FlagFinal,
		StreamID: 0x0102030405060708,
		Sequence: 0x11121314,
		Name:     []byte("blob"),
		Payload:  []byte{0x00, 0xff},
		Offset:   0x2122232425262728,
	}
	encoded, err := MarshalFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	wantHeader := "4c445350010003010102030405060708111213140000000400000000000000022122232425262728"
	if got := hex.EncodeToString(encoded[:HeaderSize]); got != wantHeader {
		t.Fatalf("header = %s, want %s", got, wantHeader)
	}
	if got := string(encoded[HeaderSize : HeaderSize+4]); got != "blob" {
		t.Fatalf("name = %q", got)
	}
}

func TestValidateFrameMalformedTable(t *testing.T) {
	t.Parallel()
	megabyte := make([]byte, MaxChunkPayload)
	tests := []struct {
		name  string
		frame Frame
		code  ErrorCode
		fatal bool
	}{
		{"unknown kind", Frame{Kind: 0xff}, ErrorUnknownKind, true},
		{"request flags", Frame{Kind: KindRequestControl, Flags: FlagFinal, StreamID: 1, Payload: []byte("{}")}, ErrorInvalidFlags, true},
		{"request zero stream", Frame{Kind: KindRequestControl, Payload: []byte("{}")}, ErrorInvalidStreamID, false},
		{"request sequence", Frame{Kind: KindRequestControl, StreamID: 1, Sequence: 1, Payload: []byte("{}")}, ErrorInvalidSequence, false},
		{"request name", Frame{Kind: KindRequestControl, StreamID: 1, Name: []byte("x"), Payload: []byte("{}")}, ErrorInvalidName, false},
		{"request offset", Frame{Kind: KindRequestControl, StreamID: 1, Payload: []byte("{}"), Offset: 1}, ErrorInvalidOffset, false},
		{"request empty", Frame{Kind: KindRequestControl, StreamID: 1}, ErrorInvalidPayload, false},
		{"request malformed utf8", Frame{Kind: KindRequestControl, StreamID: 1, Payload: []byte{'{', '"', 0xff, '"', ':', '1', '}'}}, ErrorInvalidControl, false},
		{"request invalid json", Frame{Kind: KindRequestControl, StreamID: 1, Payload: []byte("{")}, ErrorInvalidControl, false},
		{"ready zero stream", Frame{Kind: KindRequestReady}, ErrorInvalidStreamID, false},
		{"ready sequence", Frame{Kind: KindRequestReady, StreamID: 1, Sequence: 1}, ErrorInvalidSequence, false},
		{"ready name", Frame{Kind: KindRequestReady, StreamID: 1, Name: []byte("x")}, ErrorInvalidName, false},
		{"ready payload", Frame{Kind: KindRequestReady, StreamID: 1, Payload: []byte("x")}, ErrorInvalidPayload, false},
		{"ready offset", Frame{Kind: KindRequestReady, StreamID: 1, Offset: 1}, ErrorInvalidOffset, false},
		{"chunk zero stream", Frame{Kind: KindBlobChunk, Flags: FlagFinal, Sequence: 1, Name: []byte("x")}, ErrorInvalidStreamID, false},
		{"chunk zero sequence", Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Name: []byte("x")}, ErrorInvalidSequence, false},
		{"chunk empty name", Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 1}, ErrorInvalidName, false},
		{"chunk malformed name", Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 1, Name: []byte{0xff}}, ErrorInvalidName, false},
		{"chunk short nonfinal", Frame{Kind: KindBlobChunk, StreamID: 1, Sequence: 1, Name: []byte("x"), Payload: []byte("x")}, ErrorNonCanonicalChunk, false},
		{"chunk empty final offset", Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 1, Name: []byte("x"), Offset: 1}, ErrorNonCanonicalChunk, false},
		{"chunk oversized", Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 1, Name: []byte("x"), Payload: append(megabyte, 0)}, ErrorChunkTooLarge, false},
		{"end zero stream", Frame{Kind: KindBundleEnd, Sequence: 1}, ErrorInvalidStreamID, false},
		{"end zero sequence", Frame{Kind: KindBundleEnd, StreamID: 1}, ErrorInvalidSequence, false},
		{"end body", Frame{Kind: KindBundleEnd, StreamID: 1, Sequence: 1, Payload: []byte("x")}, ErrorInvalidPayload, false},
		{"cancel zero stream", Frame{Kind: KindCancel}, ErrorInvalidStreamID, false},
		{"cancel body", Frame{Kind: KindCancel, StreamID: 1, Name: []byte("x")}, ErrorInvalidName, false},
		{"close nonzero stream", Frame{Kind: KindClose, StreamID: 1}, ErrorInvalidStreamID, false},
		{"close body", Frame{Kind: KindClose, Payload: []byte("x")}, ErrorInvalidPayload, false},
		{"error zero stream", Frame{Kind: KindStreamError, Name: []byte("x")}, ErrorInvalidStreamID, false},
		{"error sequence", Frame{Kind: KindStreamError, StreamID: 1, Sequence: 1, Name: []byte("x")}, ErrorInvalidSequence, false},
		{"error empty name", Frame{Kind: KindStreamError, StreamID: 1}, ErrorInvalidName, false},
		{"error long name", Frame{Kind: KindStreamError, StreamID: 1, Name: bytes.Repeat([]byte{'x'}, 129)}, ErrorInvalidName, false},
		{"error nonascii name", Frame{Kind: KindStreamError, StreamID: 1, Name: []byte("雪")}, ErrorInvalidName, false},
		{"error payload", Frame{Kind: KindStreamError, StreamID: 1, Name: []byte("x"), Payload: []byte("x")}, ErrorInvalidPayload, false},
		{"error offset", Frame{Kind: KindStreamError, StreamID: 1, Name: []byte("x"), Offset: 1}, ErrorInvalidOffset, false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateFrame(test.frame)
			assertError(t, err, test.code, test.fatal)
		})
	}
}

func TestFrameAbsoluteBoundsAndGeneratedControlLimit(t *testing.T) {
	t.Parallel()
	if MaxControlPayload != uint64(engineprotocol.MaxWireJSONBytes) {
		t.Fatalf("control limit %d drifted from generated limit %d", MaxControlPayload, engineprotocol.MaxWireJSONBytes)
	}
	control := []byte(`{"value":"` + strings.Repeat("a", int(MaxControlPayload)-12) + `"}`)
	if len(control) != int(MaxControlPayload) {
		t.Fatalf("test control length = %d", len(control))
	}
	if err := ValidateFrame(Frame{Kind: KindRequestControl, StreamID: 1, Payload: control}); err != nil {
		t.Fatalf("exact maximum rejected: %v", err)
	}
	tooLongName := bytes.Repeat([]byte{'n'}, int(MaxNameBytes)+1)
	assertError(t, ValidateFrame(Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 1, Name: tooLongName}), ErrorNameTooLarge, true)
	tooLargePayload := make([]byte, MaxPayloadBytes+1)
	assertError(t, ValidateFrame(Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 1, Name: []byte("x"), Payload: tooLargePayload}), ErrorPayloadTooLarge, true)
}

func TestBlobPayloadIsOpaqueBinary(t *testing.T) {
	t.Parallel()
	payload := make([]byte, 256)
	for index := range payload {
		payload[index] = byte(index)
	}
	payload = append(payload, []byte("LDSP\r\n\x00")...)
	frame := Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 1, Name: []byte("raw/\x00/blob"), Payload: payload}
	encoded, err := MarshalFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalFrame(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.Payload, payload) {
		t.Fatal("opaque blob payload changed")
	}
}

func TestFramingErrorDoesNotExposeCauseText(t *testing.T) {
	t.Parallel()
	secret := errors.New("secret request and blob contents")
	err := newError(ErrorBodyRead, StageBody, true, secret)
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error leaked cause: %q", err)
	}
	if !errors.Is(err, secret) {
		t.Fatal("cause is not programmatically available")
	}
	if IsFatal(io.EOF) {
		t.Fatal("clean EOF must not be fatal")
	}
	if _, ok := CodeOf(io.EOF); ok {
		t.Fatal("plain EOF unexpectedly has a framing code")
	}
	var nilError *FramingError
	if nilError.Error() != "<nil>" || nilError.Unwrap() != nil {
		t.Fatal("nil framing error methods are not safe")
	}
}

func validFrames() []Frame {
	return []Frame{
		{Kind: KindRequestControl, StreamID: 1, Payload: []byte(`{"operation":"engine.handshake"}`)},
		{Kind: KindRequestReady, StreamID: 1},
		{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 1, Name: []byte("asset/雪"), Payload: []byte{0x00, 0xff, 'L', 'D', 'S', 'P'}},
		{Kind: KindBundleEnd, StreamID: 1, Sequence: 2},
		{Kind: KindCancel, StreamID: 2},
		{Kind: KindResponseControl, StreamID: 1, Payload: []byte(`{"outcome":"success"}`)},
		{Kind: KindClose},
		{Kind: KindStreamError, StreamID: 1, Name: []byte("framing.invalid_control")},
	}
}

func equalFrame(left, right Frame) bool {
	return left.Kind == right.Kind && left.Flags == right.Flags && left.StreamID == right.StreamID &&
		left.Sequence == right.Sequence && left.Offset == right.Offset &&
		slices.Equal(left.Name, right.Name) && slices.Equal(left.Payload, right.Payload)
}

func assertError(t *testing.T, err error, code ErrorCode, fatal ...bool) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s", code)
	}
	got, ok := CodeOf(err)
	if !ok || got != code {
		t.Fatalf("error code = %q/%t, want %q: %v", got, ok, code, err)
	}
	if len(fatal) > 0 && IsFatal(err) != fatal[0] {
		t.Fatalf("fatal = %t, want %t: %v", IsFatal(err), fatal[0], err)
	}
}
