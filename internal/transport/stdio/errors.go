// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package stdio

import (
	"errors"
	"fmt"
	"io"
)

// ErrorCode is a stable, data-independent framing failure classification.
type ErrorCode string

const (
	ErrorBadMagic           ErrorCode = "bad_magic"
	ErrorUnsupportedVersion ErrorCode = "unsupported_version"
	ErrorUnknownKind        ErrorCode = "unknown_kind"
	ErrorInvalidFlags       ErrorCode = "invalid_flags"
	ErrorLengthOverflow     ErrorCode = "length_overflow"
	ErrorNameTooLarge       ErrorCode = "name_too_large"
	ErrorPayloadTooLarge    ErrorCode = "payload_too_large"
	ErrorTruncatedHeader    ErrorCode = "truncated_header"
	ErrorTruncatedBody      ErrorCode = "truncated_body"
	ErrorHeaderRead         ErrorCode = "header_read"
	ErrorBodyRead           ErrorCode = "body_read"
	ErrorInvalidStreamID    ErrorCode = "invalid_stream_id"
	ErrorInvalidSequence    ErrorCode = "invalid_sequence"
	ErrorInvalidName        ErrorCode = "invalid_name"
	ErrorInvalidPayload     ErrorCode = "invalid_payload"
	ErrorInvalidOffset      ErrorCode = "invalid_offset"
	ErrorInvalidControl     ErrorCode = "invalid_control"
	ErrorChunkTooLarge      ErrorCode = "chunk_too_large"
	ErrorNonCanonicalChunk  ErrorCode = "noncanonical_chunk"
	ErrorTrailingBytes      ErrorCode = "trailing_bytes"
	ErrorWriteHeader        ErrorCode = "write_header"
	ErrorWriteName          ErrorCode = "write_name"
	ErrorWritePayload       ErrorCode = "write_payload"
	ErrorInvalidWriter      ErrorCode = "invalid_writer"
	ErrorSequenceOverflow   ErrorCode = "sequence_overflow"
	ErrorChunkIndex         ErrorCode = "chunk_index"
	ErrorBundleClosed       ErrorCode = "bundle_closed"
	ErrorBundleFrameKind    ErrorCode = "bundle_frame_kind"
	ErrorBlobOrder          ErrorCode = "blob_order"
	ErrorBlobNotFinal       ErrorCode = "blob_not_final"
	ErrorChunkAfterFinal    ErrorCode = "chunk_after_final"
)

// ErrorStage identifies the framing operation which failed without exposing
// request or blob data.
type ErrorStage string

const (
	StageHeader     ErrorStage = "header"
	StageBody       ErrorStage = "body"
	StageValidation ErrorStage = "validation"
	StageWrite      ErrorStage = "write"
	StagePlan       ErrorStage = "plan"
)

// FramingError reports whether the byte boundary remains trustworthy. A fatal
// error requires the caller to stop reading and must never trigger a magic scan.
type FramingError struct {
	Code  ErrorCode
	Stage ErrorStage
	Fatal bool
	cause error
}

func newError(code ErrorCode, stage ErrorStage, fatal bool, cause error) *FramingError {
	return &FramingError{Code: code, Stage: stage, Fatal: fatal, cause: cause}
}

func (err *FramingError) Error() string {
	if err == nil {
		return "<nil>"
	}
	return fmt.Sprintf("ldsp framing %s: %s", err.Stage, err.Code)
}

// Unwrap exposes only the underlying I/O classification to programmatic
// callers. Error returns intentionally omit the underlying message.
func (err *FramingError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

// IsFatal reports whether framing is no longer trustworthy. A clean io.EOF
// between frames is not fatal.
func IsFatal(err error) bool {
	var framingErr *FramingError
	return errors.As(err, &framingErr) && framingErr.Fatal
}

// CodeOf returns the stable framing code carried by err.
func CodeOf(err error) (ErrorCode, bool) {
	var framingErr *FramingError
	if !errors.As(err, &framingErr) {
		return "", false
	}
	return framingErr.Code, true
}

func truncatedCode(stage ErrorStage) ErrorCode {
	if stage == StageHeader {
		return ErrorTruncatedHeader
	}
	return ErrorTruncatedBody
}

func readCode(stage ErrorStage) ErrorCode {
	if stage == StageHeader {
		return ErrorHeaderRead
	}
	return ErrorBodyRead
}

func fatalReadError(stage ErrorStage, err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return newError(truncatedCode(stage), stage, true, io.ErrUnexpectedEOF)
	}
	return newError(readCode(stage), stage, true, err)
}
