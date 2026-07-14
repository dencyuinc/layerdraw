// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
)

func TestEncoderCompletesShortWrites(t *testing.T) {
	t.Parallel()
	frame := Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 7, Sequence: 1, Name: []byte("blob"), Payload: []byte("payload")}
	writer := &shortWriter{maximum: 1}
	if err := NewEncoder(writer).WriteFrame(frame); err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalFrame(writer.buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !equalFrame(decoded, frame) {
		t.Fatalf("short-write result = %#v", decoded)
	}
}

func TestEncoderClassifiesBrokenWrites(t *testing.T) {
	t.Parallel()
	frame := Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 7, Sequence: 1, Name: []byte("blob"), Payload: []byte("payload")}
	failure := errors.New("broken pipe")
	tests := []struct {
		name   string
		writer io.Writer
		code   ErrorCode
	}{
		{"nil", nil, ErrorInvalidWriter},
		{"zero progress", zeroWriter{}, ErrorWriteHeader},
		{"invalid count", invalidCountWriter{}, ErrorWriteHeader},
		{"header", &budgetWriter{remaining: 0, err: failure}, ErrorWriteHeader},
		{"name", &budgetWriter{remaining: HeaderSize, err: failure}, ErrorWriteName},
		{"payload", &budgetWriter{remaining: HeaderSize + len(frame.Name), err: failure}, ErrorWritePayload},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := NewEncoder(test.writer).WriteFrame(frame)
			assertError(t, err, test.code, true)
		})
	}
	var nilEncoder *Encoder
	assertError(t, nilEncoder.WriteFrame(frame), ErrorInvalidWriter, true)
	broken := &budgetWriter{err: failure}
	encoder := NewEncoder(broken)
	first := encoder.WriteFrame(frame)
	assertError(t, first, ErrorWriteHeader, true)
	calls := broken.calls
	second := encoder.WriteFrame(frame)
	assertError(t, second, ErrorWriteHeader, true)
	if first != second || broken.calls != calls {
		t.Fatal("encoder wrote again after a fatal failure")
	}
}

func TestEncoderRejectsInvalidFrameWithoutWriting(t *testing.T) {
	t.Parallel()
	var destination bytes.Buffer
	err := NewEncoder(&destination).WriteFrame(Frame{Kind: KindClose, StreamID: 1})
	assertError(t, err, ErrorInvalidStreamID, false)
	if destination.Len() != 0 {
		t.Fatalf("invalid frame wrote %d bytes", destination.Len())
	}
}

func TestEncoderConcurrentFramesDoNotInterleave(t *testing.T) {
	t.Parallel()
	const count = 64
	writer := &shortWriter{maximum: 3}
	encoder := NewEncoder(writer)
	var group sync.WaitGroup
	for index := 1; index <= count; index++ {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			frame := Frame{Kind: KindRequestControl, StreamID: uint64(index), Payload: []byte(`{"request":true}`)}
			if err := encoder.WriteFrame(frame); err != nil {
				t.Errorf("write %d: %v", index, err)
			}
		}()
	}
	group.Wait()

	seen := make(map[uint64]bool, count)
	decoder := NewDecoder(bytes.NewReader(writer.buffer.Bytes()))
	for {
		frame, err := decoder.ReadFrame()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if frame.Kind != KindRequestControl || seen[frame.StreamID] {
			t.Fatalf("interleaved or duplicate frame: %#v", frame)
		}
		seen[frame.StreamID] = true
	}
	if len(seen) != count {
		t.Fatalf("decoded %d frames, want %d", len(seen), count)
	}
}

type shortWriter struct {
	mu      sync.Mutex
	maximum int
	buffer  bytes.Buffer
}

func (writer *shortWriter) Write(value []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(value) > writer.maximum {
		value = value[:writer.maximum]
	}
	return writer.buffer.Write(value)
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }

type invalidCountWriter struct{}

func (invalidCountWriter) Write(value []byte) (int, error) { return len(value) + 1, nil }

type budgetWriter struct {
	remaining int
	err       error
	calls     int
}

func (writer *budgetWriter) Write(value []byte) (int, error) {
	writer.calls++
	if writer.remaining == 0 {
		return 0, writer.err
	}
	if len(value) > writer.remaining {
		written := writer.remaining
		writer.remaining = 0
		return written, writer.err
	}
	writer.remaining -= len(value)
	return len(value), nil
}
