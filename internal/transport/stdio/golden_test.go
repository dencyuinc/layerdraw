// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

type goldenManifest struct {
	Format         string          `json:"format"`
	FramingVersion string          `json:"framing_version"`
	HeaderBytes    int             `json:"header_bytes"`
	ByteOrder      string          `json:"byte_order"`
	Fixtures       []goldenFixture `json:"fixtures"`
}

type goldenFixture struct {
	File       string `json:"file"`
	SHA256     string `json:"sha256"`
	WireBytes  int    `json:"wire_bytes"`
	Kind       string `json:"kind"`
	KindValue  uint8  `json:"kind_value"`
	Flags      uint8  `json:"flags"`
	StreamID   string `json:"stream_id"`
	Sequence   uint32 `json:"sequence"`
	NameHex    string `json:"name_hex"`
	PayloadHex string `json:"payload_hex"`
	Offset     string `json:"offset"`
}

func TestAuthoritativeGoldenCorpus(t *testing.T) {
	t.Parallel()
	directory := filepath.Join("..", "..", "..", "tests", "conformance", "stdio", "v1")
	manifestBytes, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest goldenManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Format != "layerdraw-stdio-framing-golden-v1" || manifest.FramingVersion != "1.0" ||
		manifest.HeaderBytes != HeaderSize || manifest.ByteOrder != "big-endian" {
		t.Fatalf("invalid manifest metadata: %#v", manifest)
	}
	if len(manifest.Fixtures) != int(KindStreamError) {
		t.Fatalf("fixture count = %d", len(manifest.Fixtures))
	}
	seen := make(map[Kind]bool, len(manifest.Fixtures))
	for _, fixture := range manifest.Fixtures {
		fixture := fixture
		t.Run(fixture.File, func(t *testing.T) {
			encoded, err := os.ReadFile(filepath.Join(directory, fixture.File))
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded) != fixture.WireBytes {
				t.Fatalf("wire bytes = %d, want %d", len(encoded), fixture.WireBytes)
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(encoded))
			if digest != fixture.SHA256 {
				t.Fatalf("sha256 = %s, want %s", digest, fixture.SHA256)
			}
			frame, err := UnmarshalFrame(encoded)
			if err != nil {
				t.Fatal(err)
			}
			streamID, err := strconv.ParseUint(fixture.StreamID, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			offset, err := strconv.ParseUint(fixture.Offset, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			name, err := hex.DecodeString(fixture.NameHex)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := hex.DecodeString(fixture.PayloadHex)
			if err != nil {
				t.Fatal(err)
			}
			want := Frame{
				Kind:     Kind(fixture.KindValue),
				Flags:    Flags(fixture.Flags),
				StreamID: streamID,
				Sequence: fixture.Sequence,
				Name:     name,
				Payload:  payload,
				Offset:   offset,
			}
			if frame.Kind.String() != fixture.Kind || !equalFrame(frame, want) {
				t.Fatalf("decoded fixture = %#v, want %#v", frame, want)
			}
			reencoded, err := MarshalFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			if hex.EncodeToString(reencoded) != hex.EncodeToString(encoded) {
				t.Fatal("fixture is not byte-stable after round trip")
			}
			seen[frame.Kind] = true
		})
	}
	for kind := KindRequestControl; kind <= KindStreamError; kind++ {
		if !seen[kind] {
			t.Errorf("missing fixture for %s", kind)
		}
	}
}
