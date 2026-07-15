// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"bytes"
	"io"
)

// Decoder reads one bounded frame at a time. It is intentionally not safe for
// concurrent use; one connection reader must own it.
type Decoder struct {
	reader io.Reader
	failed error
}

func NewDecoder(reader io.Reader) *Decoder {
	return &Decoder{reader: reader}
}

// ReadFrame returns io.EOF only when no header byte was read at a frame
// boundary. Any partial header/body or untrustworthy header is a fatal error.
// For a fully drained, structurally invalid frame, the returned Frame remains
// available together with a nonfatal validation error.
func (decoder *Decoder) ReadFrame() (Frame, error) {
	if decoder == nil || decoder.reader == nil {
		return Frame{}, newError(ErrorHeaderRead, StageHeader, true, nil)
	}
	if decoder.failed != nil {
		return Frame{}, decoder.failed
	}
	var encoded [HeaderSize]byte
	read, err := io.ReadFull(decoder.reader, encoded[:])
	if err != nil {
		if read == 0 && err == io.EOF {
			return Frame{}, io.EOF
		}
		return Frame{}, decoder.poison(fatalReadError(StageHeader, err))
	}
	header, err := decodeHeader(encoded[:])
	if err != nil {
		return Frame{}, decoder.poison(err)
	}

	frame := Frame{
		Kind:     header.kind,
		Flags:    header.flags,
		StreamID: header.streamID,
		Sequence: header.sequence,
		Offset:   header.offset,
		Name:     make([]byte, int(header.nameLength)),
		Payload:  make([]byte, int(header.payloadLength)),
	}
	if err := readBody(decoder.reader, frame.Name); err != nil {
		return Frame{}, decoder.poison(err)
	}
	if err := readBody(decoder.reader, frame.Payload); err != nil {
		return Frame{}, decoder.poison(err)
	}
	if err := ValidateFrame(frame); err != nil {
		return frame, err
	}
	return frame, nil
}

func (decoder *Decoder) poison(err error) error {
	if IsFatal(err) {
		decoder.failed = err
	}
	return err
}

func readBody(reader io.Reader, destination []byte) error {
	if len(destination) == 0 {
		return nil
	}
	_, err := io.ReadFull(reader, destination)
	if err != nil {
		return fatalReadError(StageBody, err)
	}
	return nil
}

// UnmarshalFrame decodes exactly one frame and rejects trailing bytes.
func UnmarshalFrame(encoded []byte) (Frame, error) {
	reader := bytes.NewReader(encoded)
	frame, err := NewDecoder(reader).ReadFrame()
	if err != nil {
		return frame, err
	}
	if reader.Len() != 0 {
		return frame, newError(ErrorTrailingBytes, StageValidation, false, nil)
	}
	return frame, nil
}
