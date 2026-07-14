// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"bytes"
	"errors"
	"io"
	"sync"
)

// Encoder serializes complete frames. Concurrent callers are safe and cannot
// interleave header, name, or payload bytes.
type Encoder struct {
	mu     sync.Mutex
	writer io.Writer
	failed error
}

func NewEncoder(writer io.Writer) *Encoder {
	return &Encoder{writer: writer}
}

// WriteFrame validates and writes one canonical framing 1.0 frame.
func (encoder *Encoder) WriteFrame(frame Frame) error {
	if encoder == nil || encoder.writer == nil {
		return newError(ErrorInvalidWriter, StageWrite, true, nil)
	}
	return encoder.WriteFrames([]Frame{frame})
}

// WriteFrames validates every frame before acquiring the writer and then
// writes the complete sequence under one lease. This is the response-bundle
// commit primitive: concurrent response bundles cannot interleave.
func (encoder *Encoder) WriteFrames(frames []Frame) error {
	if encoder == nil || encoder.writer == nil {
		return newError(ErrorInvalidWriter, StageWrite, true, nil)
	}
	type encodedFrame struct {
		header [HeaderSize]byte
		frame  Frame
	}
	encoded := make([]encodedFrame, len(frames))
	for index, frame := range frames {
		header, err := encodeHeader(frame)
		if err != nil {
			return err
		}
		encoded[index] = encodedFrame{header: header, frame: frame}
	}

	encoder.mu.Lock()
	defer encoder.mu.Unlock()
	if encoder.failed != nil {
		return encoder.failed
	}
	for _, item := range encoded {
		if err := writeAll(encoder.writer, item.header[:]); err != nil {
			return encoder.poison(ErrorWriteHeader, err)
		}
		if err := writeAll(encoder.writer, item.frame.Name); err != nil {
			return encoder.poison(ErrorWriteName, err)
		}
		if err := writeAll(encoder.writer, item.frame.Payload); err != nil {
			return encoder.poison(ErrorWritePayload, err)
		}
	}
	return nil
}

func (encoder *Encoder) poison(code ErrorCode, cause error) error {
	encoder.failed = newError(code, StageWrite, true, cause)
	return encoder.failed
}

func writeAll(writer io.Writer, remaining []byte) error {
	for len(remaining) > 0 {
		written, err := writer.Write(remaining)
		if written < 0 || written > len(remaining) {
			return errors.New("invalid io.Writer count")
		}
		remaining = remaining[written:]
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

// MarshalFrame returns the exact canonical bytes for one frame.
func MarshalFrame(frame Frame) ([]byte, error) {
	var destination bytes.Buffer
	if err := NewEncoder(&destination).WriteFrame(frame); err != nil {
		return nil, err
	}
	return destination.Bytes(), nil
}
