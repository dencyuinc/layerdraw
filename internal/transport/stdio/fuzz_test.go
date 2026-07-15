// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func FuzzHeaderDecode(f *testing.F) {
	for _, frame := range validFrames() {
		f.Add(rawHeader(frame))
	}
	f.Add([]byte("LDSP"))
	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > HeaderSize {
			encoded = encoded[:HeaderSize]
		}
		header, err := decodeHeader(encoded)
		if err != nil {
			return
		}
		if header.nameLength > MaxNameBytes || header.payloadLength > MaxPayloadBytes {
			t.Fatalf("accepted unbounded header: %#v", header)
		}
		if header.payloadLength > ^uint64(0)-uint64(header.nameLength) || header.offset > ^uint64(0)-header.payloadLength {
			t.Fatalf("accepted overflowing header: %#v", header)
		}
	})
}

func FuzzHeaderBodyArithmetic(f *testing.F) {
	f.Add(uint32(4), uint64(8), uint64(0), uint8(KindBlobChunk), uint8(FlagFinal))
	f.Add(MaxNameBytes+1, uint64(0), uint64(0), uint8(KindClose), uint8(0))
	f.Add(uint32(0), ^uint64(0), ^uint64(0), uint8(KindRequestControl), uint8(0))
	f.Fuzz(func(t *testing.T, nameLength uint32, payloadLength, offset uint64, kind, flags uint8) {
		frame := rawHeader(Frame{Kind: Kind(kind), Flags: Flags(flags), StreamID: 1, Sequence: 1})
		binary.BigEndian.PutUint32(frame[20:24], nameLength)
		binary.BigEndian.PutUint64(frame[24:32], payloadLength)
		binary.BigEndian.PutUint64(frame[32:40], offset)
		header, err := decodeHeader(frame)
		if err != nil {
			return
		}
		bodyLength := uint64(header.nameLength) + header.payloadLength
		if bodyLength < header.payloadLength || bodyLength > uint64(^uint(0)>>1) {
			t.Fatalf("accepted unsafe body length: %#v", header)
		}
	})
}

func FuzzFrameTruncation(f *testing.F) {
	for _, frame := range validFrames() {
		encoded, err := MarshalFrame(frame)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(encoded, uint32(len(encoded)))
		if len(encoded) > 0 {
			f.Add(encoded, uint32(len(encoded)-1))
		}
	}
	f.Fuzz(func(t *testing.T, encoded []byte, cut uint32) {
		if len(encoded) > 2*int(MaxChunkPayload) {
			t.Skip()
		}
		limit := len(encoded)
		if uint64(cut) < uint64(limit) {
			limit = int(cut)
		}
		_, _ = NewDecoder(bytes.NewReader(encoded[:limit])).ReadFrame()
	})
}
