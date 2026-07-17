// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestMeasureCanonicalJSONMatchesProtocolEscaping(t *testing.T) {
	t.Parallel()
	text := "<tag>&\n\"\\\b\f\r\t\u0001\u2028\u2029😀"
	type fixture struct {
		Array      [2]uint16      `json:"array"`
		Boolean    bool           `json:"boolean"`
		EmptyMap   map[string]any `json:"empty_map"`
		EmptySlice []string       `json:"empty_slice"`
		Ignored    string         `json:"-"`
		Map        map[string]any `json:"map"`
		NilMap     map[string]any `json:"nil_map"`
		NilPointer *string        `json:"nil_pointer"`
		NilSlice   []string       `json:"nil_slice"`
		Omitted    string         `json:"omitted,omitempty"`
		Pointer    *string        `json:"pointer,omitempty"`
		Signed     int64          `json:"signed"`
		Text       string         `json:"text"`
		Unsigned   uint64         `json:"unsigned"`
	}
	value := fixture{
		Array:      [2]uint16{7, 9},
		Boolean:    true,
		EmptyMap:   map[string]any{},
		EmptySlice: []string{},
		Ignored:    "not encoded",
		Map: map[string]any{
			"array":  []any{nil, false, int64(-7), uint64(9), text},
			"object": map[string]any{"value": text},
		},
		Pointer:  &text,
		Signed:   -42,
		Text:     text,
		Unsigned: 42,
	}

	got, err := measureCanonicalJSON(context.Background(), value, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	encoded := canonicalJSONForTest(t, value)
	if got != int64(len(encoded)) {
		t.Fatalf("measureCanonicalJSON() = %d, encoded length = %d\n%s", got, len(encoded), encoded)
	}
	if bytes.Contains(encoded, []byte(`\u003c`)) || !bytes.Contains(encoded, []byte(`\u2028`)) {
		t.Fatalf("unexpected canonical escaping: %s", encoded)
	}
	if nullSize, err := measureCanonicalJSON(context.Background(), nil, 0); err != nil || nullSize != 4 {
		t.Fatalf("nil size = (%d, %v)", nullSize, err)
	}
}

func TestMeasureCanonicalJSONRejectsInvalidOrUnboundedValues(t *testing.T) {
	t.Parallel()
	if _, err := measureCanonicalJSON(nil, "value", 100); err == nil {
		t.Fatal("nil context was accepted")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := measureCanonicalJSON(cancelled, "value", 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled measurement error = %v", err)
	}

	if _, err := measureCanonicalJSON(context.Background(), "value", 1); err == nil {
		t.Fatal("byte limit was not enforced")
	} else {
		var limitError *canonicalJSONLimitError
		if !errors.As(err, &limitError) || limitError.Observed <= 1 || limitError.Error() == "" {
			t.Fatalf("byte limit error = %v", err)
		}
	}

	for name, value := range map[string]any{
		"float":          1.5,
		"non-string map": map[int]string{1: "value"},
		"invalid UTF-8":  string([]byte{0xff}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := measureCanonicalJSON(context.Background(), value, 1<<20); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}

	var cycle any
	cycle = &cycle
	if _, err := measureCanonicalJSON(context.Background(), cycle, 1<<20); err == nil {
		t.Fatal("pointer cycle was accepted")
	}

	var tooDeep any = "leaf"
	for range 130 {
		tooDeep = []any{tooDeep}
	}
	if _, err := measureCanonicalJSON(context.Background(), tooDeep, 1<<20); err == nil {
		t.Fatal("over-depth value was accepted")
	}
}

func TestCanonicalJSONEmptyValueMatchesEncodingJSON(t *testing.T) {
	t.Parallel()
	zero := 0
	nonzero := 1
	tests := []struct {
		value any
		want  bool
	}{
		{[0]string{}, true},
		{[1]string{}, false},
		{map[string]string{}, true},
		{[]string{}, true},
		{"", true},
		{false, true},
		{int64(0), true},
		{uint64(0), true},
		{(*int)(nil), true},
		{&zero, false},
		{nonzero, false},
		{struct{}{}, false},
	}
	for _, test := range tests {
		if got := emptyJSONValue(reflect.ValueOf(test.value)); got != test.want {
			t.Errorf("emptyJSONValue(%#v) = %v, want %v", test.value, got, test.want)
		}
	}
}

func TestCanonicalJSONForTestUsesNoHTMLEscaping(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode("<value>"); err != nil {
		t.Fatal(err)
	}
	if got := bytes.TrimSuffix(output.Bytes(), []byte{'\n'}); !bytes.Equal(got, []byte(`"<value>"`)) {
		t.Fatalf("encoded value = %s", got)
	}
}
