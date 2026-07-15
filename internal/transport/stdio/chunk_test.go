// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"math"
	"slices"
	"testing"
)

func TestPlanChunksBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		size        uint64
		count       uint32
		lastOffset  uint64
		lastLength  uint64
		endSequence uint32
	}{
		{"zero", 0, 1, 0, 0, 2},
		{"one", 1, 1, 0, 1, 2},
		{"chunk minus one", MaxChunkPayload - 1, 1, 0, MaxChunkPayload - 1, 2},
		{"chunk", MaxChunkPayload, 1, 0, MaxChunkPayload, 2},
		{"chunk plus one", MaxChunkPayload + 1, 2, MaxChunkPayload, 1, 3},
		{"two chunks", 2 * MaxChunkPayload, 2, MaxChunkPayload, MaxChunkPayload, 3},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			plan, err := PlanChunks(test.size, 1)
			if err != nil {
				t.Fatal(err)
			}
			if plan.ChunkCount != test.count || plan.EndSequence != test.endSequence {
				t.Fatalf("plan = %#v", plan)
			}
			last, err := plan.Chunk(plan.ChunkCount - 1)
			if err != nil {
				t.Fatal(err)
			}
			if last.Offset != test.lastOffset || last.Length != test.lastLength || !last.Final {
				t.Fatalf("last = %#v", last)
			}
			for index := uint32(0); index+1 < plan.ChunkCount; index++ {
				chunk, err := plan.Chunk(index)
				if err != nil {
					t.Fatal(err)
				}
				if chunk.Sequence != index+1 || chunk.Offset != uint64(index)*MaxChunkPayload || chunk.Length != MaxChunkPayload || chunk.Final {
					t.Fatalf("chunk %d = %#v", index, chunk)
				}
			}
		})
	}
}

func TestPlanChunksRejectsArithmeticOverflow(t *testing.T) {
	t.Parallel()
	_, err := PlanChunks(0, 0)
	assertError(t, err, ErrorInvalidSequence, false)
	_, err = PlanChunks(0, math.MaxUint32)
	assertError(t, err, ErrorSequenceOverflow, false)
	_, err = PlanChunks(math.MaxUint64, 1)
	assertError(t, err, ErrorSequenceOverflow, false)
	plan, err := PlanChunks(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = plan.Chunk(1)
	assertError(t, err, ErrorChunkIndex, false)
	_, err = (ChunkPlan{}).Chunk(0)
	assertError(t, err, ErrorChunkIndex, false)
	forged := plan
	forged.ChunkCount++
	_, err = forged.Chunk(0)
	assertError(t, err, ErrorNonCanonicalChunk, false)
}

func TestSortBlobIDsUsesRawUTF8Identity(t *testing.T) {
	t.Parallel()
	decomposed := "e\u0301"
	precomposed := "é"
	input := []string{precomposed, "z", decomposed, "a"}
	ordered, err := SortBlobIDs(input)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", decomposed, "z", precomposed}
	if !slices.Equal(ordered, want) {
		t.Fatalf("order = %q, want %q", ordered, want)
	}
	if !slices.Equal(input, []string{precomposed, "z", decomposed, "a"}) {
		t.Fatal("input order was mutated")
	}
	for _, invalid := range [][]string{{""}, {"a", "a"}, {string([]byte{0xff})}} {
		_, err := SortBlobIDs(invalid)
		if err == nil {
			t.Fatalf("accepted invalid IDs %q", invalid)
		}
	}
}

func TestBundleValidatorAcceptsCanonicalSequence(t *testing.T) {
	t.Parallel()
	validator := NewBundleValidator()
	frames := []Frame{
		{Kind: KindBlobChunk, StreamID: 8, Sequence: 1, Name: []byte("a"), Payload: make([]byte, MaxChunkPayload)},
		{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 8, Sequence: 2, Name: []byte("a"), Payload: []byte("tail"), Offset: MaxChunkPayload},
		{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 8, Sequence: 3, Name: []byte("b"), Payload: nil},
		{Kind: KindBundleEnd, StreamID: 8, Sequence: 4},
	}
	for _, frame := range frames {
		if err := validator.Accept(frame); err != nil {
			t.Fatalf("Accept(%#v): %v", frame, err)
		}
	}
	assertError(t, validator.Accept(Frame{Kind: KindBundleEnd, StreamID: 8, Sequence: 4}), ErrorBundleClosed, false)
	var nilValidator *BundleValidator
	assertError(t, nilValidator.Accept(frames[0]), ErrorBundleClosed, false)
}

func TestBundleValidatorRejectsNonCanonicalState(t *testing.T) {
	t.Parallel()
	full := make([]byte, MaxChunkPayload)
	firstNonFinal := Frame{Kind: KindBlobChunk, StreamID: 1, Sequence: 1, Name: []byte("b"), Payload: full}
	firstFinal := Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 1, Name: []byte("b"), Payload: []byte("x")}
	tests := []struct {
		name  string
		setup []Frame
		frame Frame
		code  ErrorCode
	}{
		{"wrong kind", nil, Frame{Kind: KindCancel, StreamID: 1}, ErrorBundleFrameKind},
		{"wrong sequence", nil, Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 2, Name: []byte("a")}, ErrorInvalidSequence},
		{"first offset", nil, Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 1, Name: []byte("a"), Payload: []byte("x"), Offset: 1}, ErrorInvalidOffset},
		{"next offset", []Frame{firstNonFinal}, Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 2, Name: []byte("b"), Payload: []byte("x"), Offset: MaxChunkPayload + 1}, ErrorInvalidOffset},
		{"new before final", []Frame{firstNonFinal}, Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 2, Name: []byte("c")}, ErrorBlobNotFinal},
		{"end before final", []Frame{firstNonFinal}, Frame{Kind: KindBundleEnd, StreamID: 1, Sequence: 2}, ErrorBlobNotFinal},
		{"chunk after final", []Frame{firstFinal}, Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 2, Name: []byte("b"), Payload: []byte("x"), Offset: 1}, ErrorChunkAfterFinal},
		{"descending name", []Frame{firstFinal}, Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: 2, Name: []byte("a")}, ErrorBlobOrder},
		{"different stream", []Frame{firstFinal}, Frame{Kind: KindBundleEnd, StreamID: 2, Sequence: 2}, ErrorInvalidStreamID},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			validator := NewBundleValidator()
			for _, frame := range test.setup {
				if err := validator.Accept(frame); err != nil {
					t.Fatalf("setup: %v", err)
				}
			}
			assertError(t, validator.Accept(test.frame), test.code, false)
		})
	}
	validator := NewBundleValidator()
	validator.nextSequence = math.MaxUint32
	assertError(t, validator.Accept(Frame{Kind: KindBlobChunk, Flags: FlagFinal, StreamID: 1, Sequence: math.MaxUint32, Name: []byte("a")}), ErrorSequenceOverflow, false)
	retry := NewBundleValidator()
	assertError(t, retry.Accept(Frame{Kind: KindBundleEnd, StreamID: 1, Sequence: 2}), ErrorInvalidSequence, false)
	if err := retry.Accept(Frame{Kind: KindBundleEnd, StreamID: 2, Sequence: 1}); err != nil {
		t.Fatalf("rejected frame mutated validator state: %v", err)
	}
}
