// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"unicode/utf8"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
)

const (
	// HeaderSize is the exact LDSP framing 1.0 fixed header width.
	HeaderSize = 40

	FramingMajor uint8 = 1
	FramingMinor uint8 = 0

	MaxNameBytes        uint32 = 4096
	MaxChunkPayload     uint64 = 1 << 20
	MaxStreamErrorBytes uint32 = 128
	// MaxControlPayload is tied to the generated Engine wire JSON ceiling.
	MaxControlPayload uint64 = engineprotocol.MaxWireJSONBytes
	// MaxPayloadBytes is the absolute pre-allocation payload ceiling.
	MaxPayloadBytes uint64 = MaxControlPayload
)

var magic = [4]byte{'L', 'D', 'S', 'P'}

// Kind is the closed LDSP framing 1.0 kind enum.
type Kind uint8

const (
	KindRequestControl  Kind = 0x01
	KindRequestReady    Kind = 0x02
	KindBlobChunk       Kind = 0x03
	KindBundleEnd       Kind = 0x04
	KindCancel          Kind = 0x05
	KindResponseControl Kind = 0x06
	KindClose           Kind = 0x07
	KindStreamError     Kind = 0x08
)

func (kind Kind) String() string {
	switch kind {
	case KindRequestControl:
		return "REQUEST_CONTROL"
	case KindRequestReady:
		return "REQUEST_READY"
	case KindBlobChunk:
		return "BLOB_CHUNK"
	case KindBundleEnd:
		return "BUNDLE_END"
	case KindCancel:
		return "CANCEL"
	case KindResponseControl:
		return "RESPONSE_CONTROL"
	case KindClose:
		return "CLOSE"
	case KindStreamError:
		return "STREAM_ERROR"
	default:
		return "UNKNOWN"
	}
}

// Flags contains the kind-specific framing bits.
type Flags uint8

const FlagFinal Flags = 0x01

// Frame is one decoded LDSP framing 1.0 frame. Name and Payload are owned by
// the decoder for a read frame and are not normalized or interpreted.
type Frame struct {
	Kind     Kind
	Flags    Flags
	StreamID uint64
	Sequence uint32
	Name     []byte
	Payload  []byte
	Offset   uint64
}

type wireHeader struct {
	kind          Kind
	flags         Flags
	streamID      uint64
	sequence      uint32
	nameLength    uint32
	payloadLength uint64
	offset        uint64
}

func knownKind(kind Kind) bool {
	return kind >= KindRequestControl && kind <= KindStreamError
}

func validFlags(kind Kind, flags Flags) bool {
	if kind == KindBlobChunk {
		return flags == 0 || flags == FlagFinal
	}
	return flags == 0
}

func decodeHeader(encoded []byte) (wireHeader, error) {
	if len(encoded) != HeaderSize {
		return wireHeader{}, newError(ErrorTruncatedHeader, StageHeader, true, nil)
	}
	if !bytes.Equal(encoded[:4], magic[:]) {
		return wireHeader{}, newError(ErrorBadMagic, StageHeader, true, nil)
	}
	if encoded[4] != FramingMajor || encoded[5] != FramingMinor {
		return wireHeader{}, newError(ErrorUnsupportedVersion, StageHeader, true, nil)
	}

	header := wireHeader{
		kind:          Kind(encoded[6]),
		flags:         Flags(encoded[7]),
		streamID:      binary.BigEndian.Uint64(encoded[8:16]),
		sequence:      binary.BigEndian.Uint32(encoded[16:20]),
		nameLength:    binary.BigEndian.Uint32(encoded[20:24]),
		payloadLength: binary.BigEndian.Uint64(encoded[24:32]),
		offset:        binary.BigEndian.Uint64(encoded[32:40]),
	}
	if !knownKind(header.kind) {
		return wireHeader{}, newError(ErrorUnknownKind, StageHeader, true, nil)
	}
	if !validFlags(header.kind, header.flags) {
		return wireHeader{}, newError(ErrorInvalidFlags, StageHeader, true, nil)
	}
	if header.payloadLength > math.MaxUint64-uint64(header.nameLength) ||
		header.offset > math.MaxUint64-header.payloadLength {
		return wireHeader{}, newError(ErrorLengthOverflow, StageHeader, true, nil)
	}
	if header.nameLength > MaxNameBytes {
		return wireHeader{}, newError(ErrorNameTooLarge, StageHeader, true, nil)
	}
	if header.payloadLength > MaxPayloadBytes {
		return wireHeader{}, newError(ErrorPayloadTooLarge, StageHeader, true, nil)
	}
	maxInt := uint64(^uint(0) >> 1)
	if uint64(header.nameLength)+header.payloadLength > maxInt {
		return wireHeader{}, newError(ErrorLengthOverflow, StageHeader, true, nil)
	}
	return header, nil
}

func encodeHeader(frame Frame) ([HeaderSize]byte, error) {
	var encoded [HeaderSize]byte
	if err := ValidateFrame(frame); err != nil {
		return encoded, err
	}
	copy(encoded[:4], magic[:])
	encoded[4] = FramingMajor
	encoded[5] = FramingMinor
	encoded[6] = byte(frame.Kind)
	encoded[7] = byte(frame.Flags)
	binary.BigEndian.PutUint64(encoded[8:16], frame.StreamID)
	binary.BigEndian.PutUint32(encoded[16:20], frame.Sequence)
	binary.BigEndian.PutUint32(encoded[20:24], uint32(len(frame.Name)))
	binary.BigEndian.PutUint64(encoded[24:32], uint64(len(frame.Payload)))
	binary.BigEndian.PutUint64(encoded[32:40], frame.Offset)
	return encoded, nil
}

// ValidateFrame checks all context-free framing 1.0 invariants. Errors after a
// complete bounded body has been read are nonfatal because the next boundary
// remains known; session state may still choose to terminate that stream.
func ValidateFrame(frame Frame) error {
	if !knownKind(frame.Kind) {
		return newError(ErrorUnknownKind, StageValidation, true, nil)
	}
	if !validFlags(frame.Kind, frame.Flags) {
		return newError(ErrorInvalidFlags, StageValidation, true, nil)
	}
	if uint64(len(frame.Name)) > uint64(MaxNameBytes) {
		return newError(ErrorNameTooLarge, StageValidation, true, nil)
	}
	if uint64(len(frame.Payload)) > MaxPayloadBytes {
		return newError(ErrorPayloadTooLarge, StageValidation, true, nil)
	}
	if frame.Offset > math.MaxUint64-uint64(len(frame.Payload)) {
		return newError(ErrorLengthOverflow, StageValidation, true, nil)
	}

	emptyName := len(frame.Name) == 0
	emptyPayload := len(frame.Payload) == 0
	zeroCommon := frame.Sequence == 0 && emptyName && emptyPayload && frame.Offset == 0

	switch frame.Kind {
	case KindRequestControl, KindResponseControl:
		if frame.StreamID == 0 {
			return newError(ErrorInvalidStreamID, StageValidation, false, nil)
		}
		if frame.Sequence != 0 {
			return newError(ErrorInvalidSequence, StageValidation, false, nil)
		}
		if !emptyName {
			return newError(ErrorInvalidName, StageValidation, false, nil)
		}
		if frame.Offset != 0 {
			return newError(ErrorInvalidOffset, StageValidation, false, nil)
		}
		if emptyPayload || uint64(len(frame.Payload)) > MaxControlPayload {
			return newError(ErrorInvalidPayload, StageValidation, false, nil)
		}
		if !utf8.Valid(frame.Payload) || !json.Valid(frame.Payload) {
			return newError(ErrorInvalidControl, StageValidation, false, nil)
		}
	case KindRequestReady:
		if frame.StreamID == 0 {
			return newError(ErrorInvalidStreamID, StageValidation, false, nil)
		}
		if !zeroCommon {
			return invalidEmptyFrame(frame)
		}
	case KindBlobChunk:
		if frame.StreamID == 0 {
			return newError(ErrorInvalidStreamID, StageValidation, false, nil)
		}
		if frame.Sequence == 0 {
			return newError(ErrorInvalidSequence, StageValidation, false, nil)
		}
		if emptyName || !utf8.Valid(frame.Name) {
			return newError(ErrorInvalidName, StageValidation, false, nil)
		}
		if uint64(len(frame.Payload)) > MaxChunkPayload {
			return newError(ErrorChunkTooLarge, StageValidation, false, nil)
		}
		if frame.Flags&FlagFinal == 0 && uint64(len(frame.Payload)) != MaxChunkPayload {
			return newError(ErrorNonCanonicalChunk, StageValidation, false, nil)
		}
		if frame.Flags&FlagFinal != 0 && emptyPayload && frame.Offset != 0 {
			return newError(ErrorNonCanonicalChunk, StageValidation, false, nil)
		}
	case KindBundleEnd:
		if frame.StreamID == 0 {
			return newError(ErrorInvalidStreamID, StageValidation, false, nil)
		}
		if frame.Sequence == 0 {
			return newError(ErrorInvalidSequence, StageValidation, false, nil)
		}
		if !emptyName {
			return newError(ErrorInvalidName, StageValidation, false, nil)
		}
		if !emptyPayload {
			return newError(ErrorInvalidPayload, StageValidation, false, nil)
		}
		if frame.Offset != 0 {
			return newError(ErrorInvalidOffset, StageValidation, false, nil)
		}
	case KindCancel:
		if frame.StreamID == 0 {
			return newError(ErrorInvalidStreamID, StageValidation, false, nil)
		}
		if !zeroCommon {
			return invalidEmptyFrame(frame)
		}
	case KindClose:
		if frame.StreamID != 0 {
			return newError(ErrorInvalidStreamID, StageValidation, false, nil)
		}
		if !zeroCommon {
			return invalidEmptyFrame(frame)
		}
	case KindStreamError:
		if frame.StreamID == 0 {
			return newError(ErrorInvalidStreamID, StageValidation, false, nil)
		}
		if frame.Sequence != 0 {
			return newError(ErrorInvalidSequence, StageValidation, false, nil)
		}
		if len(frame.Name) == 0 || len(frame.Name) > int(MaxStreamErrorBytes) || !ascii(frame.Name) {
			return newError(ErrorInvalidName, StageValidation, false, nil)
		}
		if !emptyPayload {
			return newError(ErrorInvalidPayload, StageValidation, false, nil)
		}
		if frame.Offset != 0 {
			return newError(ErrorInvalidOffset, StageValidation, false, nil)
		}
	}
	return nil
}

func invalidEmptyFrame(frame Frame) error {
	if frame.Sequence != 0 {
		return newError(ErrorInvalidSequence, StageValidation, false, nil)
	}
	if len(frame.Name) != 0 {
		return newError(ErrorInvalidName, StageValidation, false, nil)
	}
	if len(frame.Payload) != 0 {
		return newError(ErrorInvalidPayload, StageValidation, false, nil)
	}
	return newError(ErrorInvalidOffset, StageValidation, false, nil)
}

func ascii(value []byte) bool {
	for _, current := range value {
		if current >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
