// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package materialize

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// HashDomain is a Language 1 SHA-256 domain.
type HashDomain string

const (
	DomainDefinition HashDomain = "definition"
	DomainGraph      HashDomain = "graph"
	DomainSubject    HashDomain = "subject"
	DomainSubtree    HashDomain = "subtree"
	DomainChildSet   HashDomain = "child-set"
	DomainStateQuery HashDomain = "state-query-snapshot"
)

var semanticDomains = map[HashDomain]bool{
	DomainDefinition: true,
	DomainGraph:      true,
	DomainSubject:    true,
	DomainSubtree:    true,
	DomainChildSet:   true,
	DomainStateQuery: true,
}

// Canonicalize applies LDL string normalization and emits RFC 8785 JSON. It
// returns the JSON value bytes without the artifact-level trailing LF.
func Canonicalize(value any) ([]byte, error) {
	if err := validateCanonicalStrings(reflect.ValueOf(value), map[canonicalVisit]bool{}); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical input: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode canonical input: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("canonical input contains more than one JSON value")
		}
		return nil, fmt.Errorf("finish canonical input: %w", err)
	}
	var out bytes.Buffer
	if err := appendCanonical(&out, decoded); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

type canonicalVisit struct {
	Type    reflect.Type
	Pointer uintptr
	Length  int
}

func validateCanonicalStrings(value reflect.Value, visited map[canonicalVisit]bool) error {
	if !value.IsValid() {
		return nil
	}
	for value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if value.Kind() == reflect.String && !utf8.ValidString(value.String()) {
		return errors.New("canonical JSON string is not valid UTF-8")
	}
	if value.Kind() == reflect.Pointer || value.Kind() == reflect.Map || value.Kind() == reflect.Slice {
		if value.IsNil() {
			return nil
		}
		length := 0
		if value.Kind() != reflect.Pointer {
			length = value.Len()
		}
		visit := canonicalVisit{Type: value.Type(), Pointer: value.Pointer(), Length: length}
		if visited[visit] {
			return nil
		}
		visited[visit] = true
	}
	switch value.Kind() {
	case reflect.Pointer:
		return validateCanonicalStrings(value.Elem(), visited)
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			if err := validateCanonicalStrings(iterator.Key(), visited); err != nil {
				return err
			}
			if err := validateCanonicalStrings(iterator.Value(), visited); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			if err := validateCanonicalStrings(value.Index(index), visited); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if value.Type().Field(index).PkgPath != "" {
				continue
			}
			if err := validateCanonicalStrings(value.Field(index), visited); err != nil {
				return err
			}
		}
	}
	return nil
}

// SemanticHash returns the Language 1 domain-separated hash of value.
func SemanticHash(domain HashDomain, value any) (string, error) {
	if !semanticDomains[domain] {
		return "", fmt.Errorf("unsupported Language 1 hash domain %q", domain)
	}
	payload, err := Canonicalize(value)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, _ = h.Write([]byte("layerdraw-language-1\x00" + string(domain) + "\x00"))
	_, _ = h.Write(payload)
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func appendCanonical(out *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		out.WriteString(strconv.FormatBool(typed))
	case string:
		return appendCanonicalString(out, normalizeString(typed))
	case json.Number:
		number, err := canonicalNumber(string(typed))
		if err != nil {
			return err
		}
		out.WriteString(number)
	case []any:
		out.WriteByte('[')
		for index, item := range typed {
			if index != 0 {
				out.WriteByte(',')
			}
			if err := appendCanonical(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		return appendCanonicalObject(out, typed)
	default:
		return fmt.Errorf("unsupported canonical JSON value %T", value)
	}
	return nil
}

func appendCanonicalObject(out *bytes.Buffer, value map[string]any) error {
	normalized := make(map[string]any, len(value))
	keys := make([]string, 0, len(value))
	for key, item := range value {
		key = normalizeString(key)
		if _, exists := normalized[key]; exists {
			return fmt.Errorf("duplicate JSON key after NFC normalization: %q", key)
		}
		normalized[key] = item
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool { return compareUTF16(keys[left], keys[right]) < 0 })
	out.WriteByte('{')
	for index, key := range keys {
		if index != 0 {
			out.WriteByte(',')
		}
		if err := appendCanonicalString(out, key); err != nil {
			return err
		}
		out.WriteByte(':')
		if err := appendCanonical(out, normalized[key]); err != nil {
			return err
		}
	}
	out.WriteByte('}')
	return nil
}

func appendCanonicalString(out *bytes.Buffer, value string) error {
	if !utf8.ValidString(value) {
		return errors.New("canonical JSON string is not valid UTF-8")
	}
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\f':
			out.WriteString(`\f`)
		case '\r':
			out.WriteString(`\r`)
		default:
			if r < 0x20 {
				_, _ = fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
	return nil
}

func canonicalNumber(text string) (string, error) {
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return "", fmt.Errorf("invalid finite JSON number %q", text)
	}
	if value == 0 {
		return "0", nil
	}
	negative := value < 0
	if negative {
		value = -value
	}
	scientific := strconv.FormatFloat(value, 'e', -1, 64)
	parts := strings.Split(scientific, "e")
	if len(parts) != 2 {
		return "", fmt.Errorf("cannot canonicalize JSON number %q", text)
	}
	exponent, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", fmt.Errorf("cannot canonicalize JSON exponent %q: %w", text, err)
	}
	digits := strings.ReplaceAll(parts[0], ".", "")
	k := exponent + 1
	n := len(digits)
	var normalized string
	switch {
	case k > 0 && k <= 21:
		if k >= n {
			normalized = digits + strings.Repeat("0", k-n)
		} else {
			normalized = digits[:k] + "." + digits[k:]
		}
	case k <= 0 && k > -6:
		normalized = "0." + strings.Repeat("0", -k) + digits
	default:
		normalized = digits[:1]
		if n > 1 {
			normalized += "." + digits[1:]
		}
		exp := k - 1
		if exp >= 0 {
			normalized += "e+" + strconv.Itoa(exp)
		} else {
			normalized += "e" + strconv.Itoa(exp)
		}
	}
	if negative {
		normalized = "-" + normalized
	}
	return normalized, nil
}

func normalizeString(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return norm.NFC.String(value)
}

// NormalizeString exposes the single Language 1 NFC/LF rule to downstream
// generated-artifact packages without duplicating canonicalization semantics.
func NormalizeString(value string) string {
	return normalizeString(value)
}

func compareUTF16(left, right string) int {
	a := utf16.Encode([]rune(left))
	b := utf16.Encode([]rune(right))
	for index := 0; index < len(a) && index < len(b); index++ {
		if a[index] < b[index] {
			return -1
		}
		if a[index] > b[index] {
			return 1
		}
	}
	return len(a) - len(b)
}

func deepClone[T any](value T) T {
	cloned := cloneValue(reflect.ValueOf(value))
	return cloned.Interface().(T)
}

func cloneValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.New(value.Type().Elem())
		out.Elem().Set(cloneValue(value.Elem()))
		return out
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.New(value.Type()).Elem()
		out.Set(cloneValue(value.Elem()))
		return out
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := 0; index < value.Len(); index++ {
			out.Index(index).Set(cloneValue(value.Index(index)))
		}
		return out
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			out.SetMapIndex(cloneValue(iterator.Key()), cloneValue(iterator.Value()))
		}
		return out
	case reflect.Struct:
		out := reflect.New(value.Type()).Elem()
		out.Set(value)
		for index := 0; index < value.NumField(); index++ {
			if out.Field(index).CanSet() && value.Field(index).CanInterface() {
				out.Field(index).Set(cloneValue(value.Field(index)))
			}
		}
		return out
	default:
		return value
	}
}
