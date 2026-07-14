// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"bytes"
	"math"
	"sort"
	"unicode/utf8"
)

// ChunkDescriptor is one deterministic canonical chunk boundary.
type ChunkDescriptor struct {
	Sequence uint32
	Offset   uint64
	Length   uint64
	Final    bool
}

// ChunkPlan describes canonical chunks followed by EndSequence. It allocates
// independently of the blob size.
type ChunkPlan struct {
	Size          uint64
	FirstSequence uint32
	ChunkCount    uint32
	EndSequence   uint32
}

// PlanChunks plans the unique framing 1.0 chunking for a blob. A zero-byte blob
// receives one zero-length FINAL chunk. EndSequence is reserved for BUNDLE_END
// or for the next blob plan and therefore must not wrap.
func PlanChunks(size uint64, firstSequence uint32) (ChunkPlan, error) {
	if firstSequence == 0 {
		return ChunkPlan{}, newError(ErrorInvalidSequence, StagePlan, false, nil)
	}
	count := uint64(1)
	if size > 0 {
		count = 1 + (size-1)/MaxChunkPayload
	}
	if count > math.MaxUint32 || uint64(firstSequence)+count > math.MaxUint32 {
		return ChunkPlan{}, newError(ErrorSequenceOverflow, StagePlan, false, nil)
	}
	return ChunkPlan{
		Size:          size,
		FirstSequence: firstSequence,
		ChunkCount:    uint32(count),
		EndSequence:   firstSequence + uint32(count),
	}, nil
}

// Chunk returns one descriptor without slicing or allocating blob bytes.
func (plan ChunkPlan) Chunk(index uint32) (ChunkDescriptor, error) {
	if index >= plan.ChunkCount || plan.ChunkCount == 0 {
		return ChunkDescriptor{}, newError(ErrorChunkIndex, StagePlan, false, nil)
	}
	canonical, err := PlanChunks(plan.Size, plan.FirstSequence)
	if err != nil {
		return ChunkDescriptor{}, err
	}
	if canonical.ChunkCount != plan.ChunkCount || canonical.EndSequence != plan.EndSequence {
		return ChunkDescriptor{}, newError(ErrorNonCanonicalChunk, StagePlan, false, nil)
	}
	offset := uint64(index) * MaxChunkPayload
	length := MaxChunkPayload
	final := index+1 == plan.ChunkCount
	if final {
		length = plan.Size - offset
	}
	return ChunkDescriptor{
		Sequence: plan.FirstSequence + index,
		Offset:   offset,
		Length:   length,
		Final:    final,
	}, nil
}

// SortBlobIDs returns a raw UTF-8 byte-ordered copy. It performs no Unicode
// normalization, and rejects byte-identical duplicate definitions.
func SortBlobIDs(blobIDs []string) ([]string, error) {
	ordered := append([]string(nil), blobIDs...)
	for _, blobID := range ordered {
		if blobID == "" || !utf8.ValidString(blobID) || len(blobID) > int(MaxNameBytes) {
			return nil, newError(ErrorInvalidName, StagePlan, false, nil)
		}
	}
	sort.Slice(ordered, func(left, right int) bool {
		return bytes.Compare([]byte(ordered[left]), []byte(ordered[right])) < 0
	})
	for index := 1; index < len(ordered); index++ {
		if ordered[index-1] == ordered[index] {
			return nil, newError(ErrorBlobOrder, StagePlan, false, nil)
		}
	}
	return ordered, nil
}

// BundleValidator enforces sequence, raw name ordering, contiguity, offsets,
// FINAL placement, and BUNDLE_END for one request or response bundle.
type BundleValidator struct {
	nextSequence uint32
	streamID     uint64
	currentName  []byte
	nextOffset   uint64
	currentFinal bool
	ended        bool
}

func NewBundleValidator() *BundleValidator {
	return &BundleValidator{nextSequence: 1}
}

// Accept validates one BLOB_CHUNK or BUNDLE_END in wire order.
func (validator *BundleValidator) Accept(frame Frame) error {
	if validator == nil || validator.ended {
		return newError(ErrorBundleClosed, StageValidation, false, nil)
	}
	if frame.Kind != KindBlobChunk && frame.Kind != KindBundleEnd {
		return newError(ErrorBundleFrameKind, StageValidation, false, nil)
	}
	if err := ValidateFrame(frame); err != nil {
		return err
	}
	if validator.streamID != 0 && frame.StreamID != validator.streamID {
		return newError(ErrorInvalidStreamID, StageValidation, false, nil)
	}
	if frame.Sequence != validator.nextSequence {
		return newError(ErrorInvalidSequence, StageValidation, false, nil)
	}

	if frame.Kind == KindBundleEnd {
		if len(validator.currentName) != 0 && !validator.currentFinal {
			return newError(ErrorBlobNotFinal, StageValidation, false, nil)
		}
		validator.streamID = frame.StreamID
		validator.ended = true
		return nil
	}

	sameBlob := bytes.Equal(frame.Name, validator.currentName)
	if len(validator.currentName) == 0 || !sameBlob {
		if len(validator.currentName) != 0 && !validator.currentFinal {
			return newError(ErrorBlobNotFinal, StageValidation, false, nil)
		}
		if len(validator.currentName) != 0 && bytes.Compare(frame.Name, validator.currentName) <= 0 {
			return newError(ErrorBlobOrder, StageValidation, false, nil)
		}
		if frame.Offset != 0 {
			return newError(ErrorInvalidOffset, StageValidation, false, nil)
		}
	} else {
		if validator.currentFinal {
			return newError(ErrorChunkAfterFinal, StageValidation, false, nil)
		}
		if frame.Offset != validator.nextOffset {
			return newError(ErrorInvalidOffset, StageValidation, false, nil)
		}
	}
	if validator.nextSequence == math.MaxUint32 {
		return newError(ErrorSequenceOverflow, StageValidation, false, nil)
	}

	validator.streamID = frame.StreamID
	validator.currentName = append(validator.currentName[:0], frame.Name...)
	validator.nextOffset = frame.Offset + uint64(len(frame.Payload))
	validator.currentFinal = frame.Flags&FlagFinal != 0
	validator.nextSequence++
	return nil
}
