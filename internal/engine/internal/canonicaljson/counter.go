// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package canonicaljson provides allocation-free canonical JSON byte counting
// primitives for Engine-owned logical response limits.
package canonicaljson

import (
	"context"
	"errors"
	"math"
	"unicode/utf8"
)

type LimitError struct {
	Observed int64
}

func (e *LimitError) Error() string {
	return "canonical JSON exceeds its byte limit"
}

type Counter struct {
	ctx   context.Context
	limit int64
	size  int64
}

func NewCounter(ctx context.Context, limit int64) (*Counter, error) {
	if ctx == nil {
		return nil, errors.New("canonical JSON measurement requires a context")
	}
	if limit < 0 {
		return nil, errors.New("canonical JSON byte limit must not be negative")
	}
	return &Counter{ctx: ctx, limit: limit}, nil
}

func (c *Counter) Add(amount int64) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if amount < 0 || amount > math.MaxInt64-c.size {
		return errors.New("canonical JSON byte count overflows int64")
	}
	observed := c.size + amount
	if c.limit > 0 && observed > c.limit {
		return &LimitError{Observed: observed}
	}
	c.size = observed
	return nil
}

// AddMeasured measures one logical value against the same context and applies
// its complete size atomically to this counter.
func (c *Counter) AddMeasured(measure func(*Counter) error) error {
	if measure == nil {
		return errors.New("canonical JSON measurement callback is required")
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}
	measured := &Counter{ctx: c.ctx}
	if err := measure(measured); err != nil {
		return err
	}
	return c.Add(measured.size)
}

func (c *Counter) String(value string) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	amount := int64(2)
	for offset := 0; offset < len(value); {
		if err := c.ctx.Err(); err != nil {
			return err
		}
		character, size := utf8.DecodeRuneInString(value[offset:])
		if character == utf8.RuneError && size == 1 {
			return errors.New("canonical JSON string contains malformed Unicode")
		}
		offset += size
		encoded := int64(size)
		switch character {
		case '"', '\\', '\b', '\f', '\n', '\r', '\t':
			encoded = 2
		case '\u2028', '\u2029':
			encoded = 6
		default:
			if character < 0x20 {
				encoded = 6
			}
		}
		if encoded > math.MaxInt64-amount {
			return errors.New("canonical JSON string byte count overflows int64")
		}
		amount += encoded
	}
	return c.Add(amount)
}

func (c *Counter) Size() int64 {
	return c.size
}
