// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"fmt"
	"unicode/utf8"
)

const (
	MaxRequestIDCodePoints = 128
	MaxRequestIDBytes      = 512
)

// ValidateRequestID applies the one trustworthy-correlation rule shared by
// handshake, compile, and transports before any generated response is keyed
// by caller-controlled metadata.
func ValidateRequestID(requestID string) error {
	if requestID == "" || !utf8.ValidString(requestID) || len(requestID) > MaxRequestIDBytes || utf8.RuneCountInString(requestID) > MaxRequestIDCodePoints {
		return fmt.Errorf("request ID must contain 1-%d code points and at most %d UTF-8 bytes", MaxRequestIDCodePoints, MaxRequestIDBytes)
	}
	return nil
}
