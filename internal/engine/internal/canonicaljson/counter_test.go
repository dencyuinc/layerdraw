// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package canonicaljson

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestCounterCountsCanonicalJSONStrings(t *testing.T) {
	t.Parallel()
	counter, err := NewCounter(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	value := "plain\"\\\b\f\n\r\t\u0001\u2028\u2029😀"
	if err := counter.String(value); err != nil {
		t.Fatal(err)
	}
	// Quotes: 2, plain: 5, seven two-byte escapes: 14, three six-byte
	// escapes: 18, emoji: 4.
	if got, want := counter.Size(), int64(43); got != want {
		t.Fatalf("Size() = %d, want %d", got, want)
	}
	if err := counter.Add(7); err != nil || counter.Size() != 50 {
		t.Fatalf("Add() = %v, size = %d", err, counter.Size())
	}
}

func TestCounterRejectsInvalidConstructionAndArithmetic(t *testing.T) {
	t.Parallel()
	if _, err := NewCounter(nil, 1); err == nil {
		t.Fatal("nil context was accepted")
	}
	if _, err := NewCounter(context.Background(), -1); err == nil {
		t.Fatal("negative limit was accepted")
	}

	counter, err := NewCounter(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := counter.Add(-1); err == nil {
		t.Fatal("negative amount was accepted")
	}
	counter.size = math.MaxInt64
	if err := counter.Add(1); err == nil {
		t.Fatal("overflow was accepted")
	}
}

func TestCounterEnforcesContextAndByteLimit(t *testing.T) {
	t.Parallel()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	counter, err := NewCounter(cancelled, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := counter.Add(1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Add() error = %v", err)
	}
	if err := counter.String("value"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled String() error = %v", err)
	}

	limited, err := NewCounter(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	err = limited.String("a")
	var limitError *LimitError
	if !errors.As(err, &limitError) || limitError.Observed != 3 || limitError.Error() == "" {
		t.Fatalf("limited String() error = %#v", err)
	}
	if limited.Size() != 0 {
		t.Fatalf("failed String changed size to %d", limited.Size())
	}
}

func TestCounterAddsMeasuredValuesAtomically(t *testing.T) {
	t.Parallel()
	counter, err := NewCounter(context.Background(), 4)
	if err != nil {
		t.Fatal(err)
	}
	err = counter.AddMeasured(func(measured *Counter) error {
		return measured.String("abc")
	})
	var limitError *LimitError
	if !errors.As(err, &limitError) || limitError.Observed != 5 {
		t.Fatalf("AddMeasured() error = %#v", err)
	}
	if counter.Size() != 0 {
		t.Fatalf("failed AddMeasured changed size to %d", counter.Size())
	}
	if err := counter.AddMeasured(nil); err == nil {
		t.Fatal("nil measurement callback was accepted")
	}
}

func TestCounterRejectsMalformedUnicode(t *testing.T) {
	t.Parallel()
	counter, err := NewCounter(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := counter.String(string([]byte{0xff})); err == nil {
		t.Fatal("malformed UTF-8 was accepted")
	}
	if counter.Size() != 0 {
		t.Fatalf("malformed string changed size to %d", counter.Size())
	}
	if err := counter.String("\ufffd"); err != nil {
		t.Fatalf("valid replacement character was rejected: %v", err)
	}
}

func TestCounterPollsContextWhileValidatingStrings(t *testing.T) {
	t.Parallel()
	ctx := &cancelDuringStringContext{remaining: 4}
	counter, err := NewCounter(ctx, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := counter.String(strings.Repeat("a", 1_000)); !errors.Is(err, context.Canceled) {
		t.Fatalf("String() error = %v, want cancellation during validation", err)
	}
	if counter.Size() != 0 {
		t.Fatalf("cancelled validation changed size to %d", counter.Size())
	}
}

type cancelDuringStringContext struct {
	remaining int
}

func (c *cancelDuringStringContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *cancelDuringStringContext) Done() <-chan struct{}       { return nil }
func (c *cancelDuringStringContext) Value(any) any               { return nil }
func (c *cancelDuringStringContext) Err() error {
	if c.remaining == 0 {
		return context.Canceled
	}
	c.remaining--
	return nil
}
