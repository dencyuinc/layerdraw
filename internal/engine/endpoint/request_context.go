// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
)

// RequestContext converts generated Engine request deadline metadata into the
// single context policy shared by every transport. A nil deadline creates only
// a cancellable child. The generated validator remains authoritative for the
// lexical RFC 3339 contract.
func RequestContext(parent context.Context, deadline *protocolcommon.Rfc3339Time) (context.Context, context.CancelFunc, error) {
	if parent == nil {
		return nil, nil, fmt.Errorf("nil request parent context")
	}
	if deadline == nil {
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel, nil
	}
	if _, err := protocolcommon.EncodeRfc3339Time(*deadline); err != nil {
		return nil, nil, fmt.Errorf("invalid generated request deadline: %w", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, string(*deadline))
	if err != nil {
		return nil, nil, fmt.Errorf("parse generated request deadline: %w", err)
	}
	ctx, cancel := context.WithDeadline(parent, parsed)
	return ctx, cancel, nil
}

// RequestContextFromControl extracts the shared optional deadline field from a
// generated Engine operation envelope without exposing generated scalar types
// to byte transports.
func RequestContextFromControl(parent context.Context, control []byte) (context.Context, context.CancelFunc, error) {
	var meta struct {
		DeadlineAt *protocolcommon.Rfc3339Time `json:"deadline_at,omitempty"`
	}
	if err := json.Unmarshal(control, &meta); err != nil {
		return nil, nil, fmt.Errorf("decode request deadline metadata: %w", err)
	}
	return RequestContext(parent, meta.DeadlineAt)
}
