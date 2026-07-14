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
	encoded, err := encodeHeader(frame)
	if err != nil {
		return err
	}

	encoder.mu.Lock()
	defer encoder.mu.Unlock()
	if encoder.failed != nil {
		return encoder.failed
	}
	if err := writeAll(encoder.writer, encoded[:]); err != nil {
		return encoder.poison(ErrorWriteHeader, err)
	}
	if err := writeAll(encoder.writer, frame.Name); err != nil {
		return encoder.poison(ErrorWriteName, err)
	}
	if err := writeAll(encoder.writer, frame.Payload); err != nil {
		return encoder.poison(ErrorWritePayload, err)
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
