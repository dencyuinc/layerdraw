// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestDecoderDistinguishesCleanEOFAndTruncation(t *testing.T) {
	t.Parallel()
	if _, err := NewDecoder(bytes.NewReader(nil)).ReadFrame(); err != io.EOF {
		t.Fatalf("empty stream error = %v, want io.EOF", err)
	}
	valid, err := MarshalFrame(Frame{Kind: KindRequestControl, StreamID: 1, Payload: []byte("{}")})
	if err != nil {
		t.Fatal(err)
	}
	for cut := 1; cut < HeaderSize; cut++ {
		_, err := NewDecoder(bytes.NewReader(valid[:cut])).ReadFrame()
		assertError(t, err, ErrorTruncatedHeader, true)
	}
	for cut := HeaderSize; cut < len(valid); cut++ {
		_, err := NewDecoder(bytes.NewReader(valid[:cut])).ReadFrame()
		assertError(t, err, ErrorTruncatedBody, true)
	}
	decoder := NewDecoder(bytes.NewReader(valid))
	if _, err := decoder.ReadFrame(); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.ReadFrame(); err != io.EOF {
		t.Fatalf("boundary EOF error = %v", err)
	}
}

func TestDecoderRejectsUntrustworthyHeaderBeforeBodyRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func([]byte)
		code   ErrorCode
	}{
		{"magic", func(header []byte) { header[0] = 'X' }, ErrorBadMagic},
		{"major", func(header []byte) { header[4]++ }, ErrorUnsupportedVersion},
		{"minor", func(header []byte) { header[5]++ }, ErrorUnsupportedVersion},
		{"kind", func(header []byte) { header[6] = 0xff }, ErrorUnknownKind},
		{"flags", func(header []byte) { header[7] = 0x80 }, ErrorInvalidFlags},
		{"name bound", func(header []byte) { binary.BigEndian.PutUint32(header[20:24], MaxNameBytes+1) }, ErrorNameTooLarge},
		{"payload bound", func(header []byte) { binary.BigEndian.PutUint64(header[24:32], MaxPayloadBytes+1) }, ErrorPayloadTooLarge},
		{"body sum overflow", func(header []byte) { binary.BigEndian.PutUint64(header[24:32], ^uint64(0)) }, ErrorLengthOverflow},
		{"offset overflow", func(header []byte) { binary.BigEndian.PutUint64(header[32:40], ^uint64(0)) }, ErrorLengthOverflow},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			header := rawHeader(Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 1, Name: []byte("x"), Payload: []byte("x")})
			test.mutate(header)
			reader := &headerThenPanicReader{header: header}
			_, err := NewDecoder(reader).ReadFrame()
			assertError(t, err, test.code, true)
		})
	}
}

func TestDecoderIsPermanentlyPoisonedAfterFatalError(t *testing.T) {
	t.Parallel()
	bad := rawHeader(Frame{Kind: KindClose})
	bad[0] = 'X'
	valid, err := MarshalFrame(Frame{Kind: KindClose})
	if err != nil {
		t.Fatal(err)
	}
	decoder := NewDecoder(bytes.NewReader(append(bad, valid...)))
	_, first := decoder.ReadFrame()
	assertError(t, first, ErrorBadMagic, true)
	_, second := decoder.ReadFrame()
	assertError(t, second, ErrorBadMagic, true)
	if first != second {
		t.Fatal("decoder did not retain the fatal error")
	}
}

func TestDecoderDrainsRecoverableFrameAndPreservesNextBoundary(t *testing.T) {
	t.Parallel()
	invalid := Frame{Kind: KindBlobChunk, StreamID: 1, Sequence: 1, Name: []byte("blob"), Payload: []byte("short")}
	rawInvalid := append(rawHeader(invalid), invalid.Name...)
	rawInvalid = append(rawInvalid, invalid.Payload...)
	valid, err := MarshalFrame(Frame{Kind: KindCancel, StreamID: 9})
	if err != nil {
		t.Fatal(err)
	}
	decoder := NewDecoder(bytes.NewReader(append(rawInvalid, valid...)))
	frame, err := decoder.ReadFrame()
	assertError(t, err, ErrorNonCanonicalChunk, false)
	if string(frame.Payload) != "short" {
		t.Fatalf("recoverable frame body was not returned: %#v", frame)
	}
	next, err := decoder.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if next.Kind != KindCancel || next.StreamID != 9 {
		t.Fatalf("next frame = %#v", next)
	}
}

func TestDecoderClassifiesUnderlyingReadErrors(t *testing.T) {
	t.Parallel()
	readFailure := errors.New("read failure")
	_, err := NewDecoder(errorReader{err: readFailure}).ReadFrame()
	assertError(t, err, ErrorHeaderRead, true)
	if !errors.Is(err, readFailure) {
		t.Fatal("header read cause was lost")
	}

	frame := Frame{Kind: KindRequestControl, StreamID: 1, Payload: []byte("{}")}
	header := rawHeader(frame)
	_, err = NewDecoder(io.MultiReader(bytes.NewReader(header), errorReader{err: readFailure})).ReadFrame()
	assertError(t, err, ErrorBodyRead, true)
	if !errors.Is(err, readFailure) {
		t.Fatal("body read cause was lost")
	}
	_, err = NewDecoder(nil).ReadFrame()
	assertError(t, err, ErrorHeaderRead, true)
}

func TestUnmarshalFrameRejectsTrailingBytes(t *testing.T) {
	t.Parallel()
	encoded, err := MarshalFrame(Frame{Kind: KindClose})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := UnmarshalFrame(append(encoded, 0))
	assertError(t, err, ErrorTrailingBytes, false)
	if frame.Kind != KindClose {
		t.Fatalf("decoded frame = %#v", frame)
	}
}

func rawHeader(frame Frame) []byte {
	header := make([]byte, HeaderSize)
	copy(header[:4], magic[:])
	header[4] = FramingMajor
	header[5] = FramingMinor
	header[6] = byte(frame.Kind)
	header[7] = byte(frame.Flags)
	binary.BigEndian.PutUint64(header[8:16], frame.StreamID)
	binary.BigEndian.PutUint32(header[16:20], frame.Sequence)
	binary.BigEndian.PutUint32(header[20:24], uint32(len(frame.Name)))
	binary.BigEndian.PutUint64(header[24:32], uint64(len(frame.Payload)))
	binary.BigEndian.PutUint64(header[32:40], frame.Offset)
	return header
}

type headerThenPanicReader struct {
	header []byte
	read   bool
}

func (reader *headerThenPanicReader) Read(destination []byte) (int, error) {
	if reader.read {
		panic("decoder read a body after rejecting its header")
	}
	reader.read = true
	return copy(destination, reader.header), nil
}

type errorReader struct {
	err error
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}
