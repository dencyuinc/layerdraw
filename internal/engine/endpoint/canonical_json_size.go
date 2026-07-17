// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/canonicaljson"
)

type canonicalJSONLimitError = canonicaljson.LimitError

type canonicalJSONSizer struct {
	counter *canonicaljson.Counter
	active  map[uintptr]bool
}

// measureCanonicalJSON returns the exact UTF-8 size emitted by the protocol
// canonical encoder without first materializing the complete JSON payload.
// Object member order cannot change byte length; string escaping mirrors the
// Go protocol generator's SetEscapeHTML(false) encoder.
func measureCanonicalJSON(ctx context.Context, value any, limit int64) (int64, error) {
	counter, err := canonicaljson.NewCounter(ctx, limit)
	if err != nil {
		return 0, err
	}
	sizer := canonicalJSONSizer{counter: counter, active: map[uintptr]bool{}}
	if err := sizer.value(reflect.ValueOf(value), 0); err != nil {
		return 0, err
	}
	return counter.Size(), nil
}

func (s *canonicalJSONSizer) add(amount int64) error {
	return s.counter.Add(amount)
}

func (s *canonicalJSONSizer) value(value reflect.Value, depth int) error {
	if depth > 128 {
		return errors.New("canonical JSON value exceeds maximum depth")
	}
	if !value.IsValid() {
		return s.add(4)
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return s.add(4)
		}
		return s.value(value.Elem(), depth)
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return s.add(4)
		}
		pointer := value.Pointer()
		if s.active[pointer] {
			return errors.New("canonical JSON value contains a cycle")
		}
		s.active[pointer] = true
		defer delete(s.active, pointer)
		return s.value(value.Elem(), depth)
	}

	switch value.Kind() {
	case reflect.Bool:
		if value.Bool() {
			return s.add(4)
		}
		return s.add(5)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return s.add(int64(len(strconv.FormatInt(value.Int(), 10))))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return s.add(int64(len(strconv.FormatUint(value.Uint(), 10))))
	case reflect.String:
		return s.string(value.String())
	case reflect.Struct:
		return s.structure(value, depth+1)
	case reflect.Map:
		return s.object(value, depth+1)
	case reflect.Slice:
		if value.IsNil() {
			return s.add(4)
		}
		return s.sequence(value, depth+1)
	case reflect.Array:
		return s.sequence(value, depth+1)
	default:
		return errors.New("unsupported canonical JSON Go value")
	}
}

func (s *canonicalJSONSizer) string(value string) error {
	return s.counter.String(value)
}

func (s *canonicalJSONSizer) structure(value reflect.Value, depth int) error {
	if err := s.add(1); err != nil {
		return err
	}
	written := 0
	typeInfo := value.Type()
	for index := 0; index < value.NumField(); index++ {
		fieldInfo := typeInfo.Field(index)
		if fieldInfo.PkgPath != "" {
			continue
		}
		name, options, _ := strings.Cut(fieldInfo.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = fieldInfo.Name
		}
		fieldValue := value.Field(index)
		if hasJSONOption(options, "omitempty") && emptyJSONValue(fieldValue) {
			continue
		}
		if written != 0 {
			if err := s.add(1); err != nil {
				return err
			}
		}
		if err := s.string(name); err != nil {
			return err
		}
		if err := s.add(1); err != nil {
			return err
		}
		if err := s.value(fieldValue, depth); err != nil {
			return err
		}
		written++
	}
	return s.add(1)
}

func (s *canonicalJSONSizer) object(value reflect.Value, depth int) error {
	if value.Type().Key().Kind() != reflect.String {
		return errors.New("canonical JSON object key is not a string")
	}
	if value.IsNil() {
		return s.add(4)
	}
	if err := s.add(1); err != nil {
		return err
	}
	iterator := value.MapRange()
	written := 0
	for iterator.Next() {
		if written != 0 {
			if err := s.add(1); err != nil {
				return err
			}
		}
		if err := s.string(iterator.Key().String()); err != nil {
			return err
		}
		if err := s.add(1); err != nil {
			return err
		}
		if err := s.value(iterator.Value(), depth); err != nil {
			return err
		}
		written++
	}
	return s.add(1)
}

func (s *canonicalJSONSizer) sequence(value reflect.Value, depth int) error {
	if err := s.add(1); err != nil {
		return err
	}
	for index := 0; index < value.Len(); index++ {
		if index != 0 {
			if err := s.add(1); err != nil {
				return err
			}
		}
		if err := s.value(value.Index(index), depth); err != nil {
			return err
		}
	}
	return s.add(1)
}

func hasJSONOption(options, target string) bool {
	for options != "" {
		var option string
		option, options, _ = strings.Cut(options, ",")
		if option == target {
			return true
		}
	}
	return false
}

func emptyJSONValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return value.Len() == 0
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Interface, reflect.Pointer:
		return value.IsZero()
	default:
		return false
	}
}
