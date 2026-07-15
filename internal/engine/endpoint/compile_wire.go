// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"encoding/base64"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
)

// compileWireStats counts the exact encoding/json representation used as the
// generated codec's validated input. It deliberately does not materialize the
// JSON: a candidate can therefore be rejected before either the generated
// encoder or an endpoint-side whole-value marshal allocates an oversized
// buffer. JsonValue is handled specially because it is the only generated
// response member with a custom MarshalJSON implementation.
type compileWireStats struct {
	bytes int64
	depth int64
}

type compileMappingBudget struct {
	used    int64
	maximum int64
}

type compileMappingLimitError struct {
	limit compileResponseLimit
}

func (err *compileMappingLimitError) Error() string {
	return fmt.Sprintf("compile mapping requires at least %d control bytes", err.limit.observed)
}

func newCompileMappingBudget(maximum int64) *compileMappingBudget {
	return &compileMappingBudget{maximum: maximum}
}

// claim retains exact JSON fragments which occupy disjoint positions in the
// eventual response. Punctuation between claimed fragments is intentionally
// omitted, making used a safe lower bound rather than an item-count heuristic.
func (budget *compileMappingBudget) claim(value any) error {
	if budget == nil {
		return nil
	}
	stats, err := measureCompileWireJSON(value)
	if err != nil {
		return err
	}
	budget.used = addWireBytes(budget.used, stats.bytes)
	if budget.used > budget.maximum {
		return &compileMappingLimitError{limit: compileResponseLimit{
			resource: "control_output_bytes",
			limit:    budget.maximum,
			observed: budget.used,
		}}
	}
	return nil
}

var jsonValueType = reflect.TypeFor[protocolcommon.JsonValue]()

type compileWireField struct {
	index     int
	name      string
	omitEmpty bool
}

var compileWireFields sync.Map

func measureCompileWireJSON(value any) (compileWireStats, error) {
	return measureCompileWireValue(reflect.ValueOf(value))
}

func measureCompileWireValue(value reflect.Value) (compileWireStats, error) {
	if !value.IsValid() {
		return compileWireStats{bytes: 4}, nil
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return compileWireStats{bytes: 4}, nil
		}
		return measureCompileWireValue(value.Elem())
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return compileWireStats{bytes: 4}, nil
		}
		return measureCompileWireValue(value.Elem())
	}
	if value.Type() == jsonValueType {
		return measureProtocolJSONValue(value.Interface().(protocolcommon.JsonValue))
	}

	switch value.Kind() {
	case reflect.Bool:
		if value.Bool() {
			return compileWireStats{bytes: 4}, nil
		}
		return compileWireStats{bytes: 5}, nil
	case reflect.String:
		return compileWireStats{bytes: encodedJSONStringBytes(value.String(), true)}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return compileWireStats{bytes: int64(len(strconv.FormatInt(value.Int(), 10)))}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return compileWireStats{bytes: int64(len(strconv.FormatUint(value.Uint(), 10)))}, nil
	case reflect.Float32:
		return measureJSONFloat(value.Float(), 32)
	case reflect.Float64:
		return measureJSONFloat(value.Float(), 64)
	case reflect.Slice:
		if value.IsNil() {
			return compileWireStats{bytes: 4}, nil
		}
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return compileWireStats{bytes: int64(base64.StdEncoding.EncodedLen(value.Len()) + 2)}, nil
		}
		return measureCompileWireArray(value)
	case reflect.Array:
		return measureCompileWireArray(value)
	case reflect.Map:
		if value.IsNil() {
			return compileWireStats{bytes: 4}, nil
		}
		if value.Type().Key().Kind() != reflect.String {
			return compileWireStats{}, fmt.Errorf("unsupported compile JSON map key %s", value.Type().Key())
		}
		result := compileWireStats{bytes: 2, depth: 1}
		iterator := value.MapRange()
		for count := 0; iterator.Next(); count++ {
			if count != 0 {
				result.bytes++
			}
			result.bytes = addWireBytes(result.bytes, encodedJSONStringBytes(iterator.Key().String(), true)+1)
			item, err := measureCompileWireValue(iterator.Value())
			if err != nil {
				return compileWireStats{}, err
			}
			result.bytes = addWireBytes(result.bytes, item.bytes)
			result.depth = max(result.depth, addWireDepth(item.depth, 1))
		}
		return result, nil
	case reflect.Struct:
		result := compileWireStats{bytes: 2, depth: 1}
		included := 0
		for _, field := range cachedCompileWireFields(value.Type()) {
			fieldValue := value.Field(field.index)
			if field.omitEmpty && fieldValue.IsZero() {
				continue
			}
			if included != 0 {
				result.bytes++
			}
			included++
			result.bytes = addWireBytes(result.bytes, encodedJSONStringBytes(field.name, true)+1)
			item, err := measureCompileWireValue(fieldValue)
			if err != nil {
				return compileWireStats{}, err
			}
			result.bytes = addWireBytes(result.bytes, item.bytes)
			result.depth = max(result.depth, addWireDepth(item.depth, 1))
		}
		return result, nil
	default:
		return compileWireStats{}, fmt.Errorf("unsupported compile JSON value %s", value.Type())
	}
}

func measureCompileWireArray(value reflect.Value) (compileWireStats, error) {
	result := compileWireStats{bytes: 2, depth: 1}
	for index := range value.Len() {
		if index != 0 {
			result.bytes++
		}
		item, err := measureCompileWireValue(value.Index(index))
		if err != nil {
			return compileWireStats{}, err
		}
		result.bytes = addWireBytes(result.bytes, item.bytes)
		result.depth = max(result.depth, addWireDepth(item.depth, 1))
	}
	return result, nil
}

func measureProtocolJSONValue(value protocolcommon.JsonValue) (compileWireStats, error) {
	switch value.Kind {
	case protocolcommon.JsonValueKindNull:
		return compileWireStats{bytes: 4}, nil
	case protocolcommon.JsonValueKindBoolean:
		if value.Boolean {
			return compileWireStats{bytes: 4}, nil
		}
		return compileWireStats{bytes: 5}, nil
	case protocolcommon.JsonValueKindString:
		return compileWireStats{bytes: encodedJSONStringBytes(value.String, true)}, nil
	case protocolcommon.JsonValueKindArray:
		result := compileWireStats{bytes: 2, depth: 1}
		for index, item := range value.Array {
			if index != 0 {
				result.bytes++
			}
			stats, err := measureProtocolJSONValue(item)
			if err != nil {
				return compileWireStats{}, err
			}
			result.bytes = addWireBytes(result.bytes, stats.bytes)
			result.depth = max(result.depth, addWireDepth(stats.depth, 1))
		}
		return result, nil
	case protocolcommon.JsonValueKindObject:
		result := compileWireStats{bytes: 2, depth: 1}
		count := 0
		for key, item := range value.Object {
			if count != 0 {
				result.bytes++
			}
			count++
			result.bytes = addWireBytes(result.bytes, encodedJSONStringBytes(key, true)+1)
			stats, err := measureProtocolJSONValue(item)
			if err != nil {
				return compileWireStats{}, err
			}
			result.bytes = addWireBytes(result.bytes, stats.bytes)
			result.depth = max(result.depth, addWireDepth(stats.depth, 1))
		}
		return result, nil
	default:
		return compileWireStats{}, fmt.Errorf("invalid protocol JsonValue kind %q", value.Kind)
	}
}

func measureJSONFloat(value float64, bits int) (compileWireStats, error) {
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return compileWireStats{}, fmt.Errorf("unsupported JSON float %v", value)
	}
	return compileWireStats{bytes: int64(len(strconv.FormatFloat(value, 'g', -1, bits)))}, nil
}

func cachedCompileWireFields(valueType reflect.Type) []compileWireField {
	if cached, ok := compileWireFields.Load(valueType); ok {
		return cached.([]compileWireField)
	}
	result := make([]compileWireField, 0, valueType.NumField())
	for index := range valueType.NumField() {
		field := valueType.Field(index)
		if field.PkgPath != "" || field.Anonymous {
			continue
		}
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, options, _ := strings.Cut(tag, ",")
		if name == "" {
			name = field.Name
		}
		omitEmpty := options == "omitempty" || strings.Contains(options, ",omitempty") || strings.Contains(options, "omitempty,")
		result = append(result, compileWireField{index: index, name: name, omitEmpty: omitEmpty})
	}
	actual, _ := compileWireFields.LoadOrStore(valueType, result)
	return actual.([]compileWireField)
}

// encodedJSONStringBytes mirrors encoding/json's appendString size. The outer
// response marshal applies its default HTML escaping even to bytes returned by
// JsonValue.MarshalJSON, so every response string is measured with that same
// codec-input behavior.
func encodedJSONStringBytes(value string, escapeHTML bool) int64 {
	length := int64(2)
	for index := 0; index < len(value); {
		current := value[index]
		if current < utf8.RuneSelf {
			index++
			switch {
			case current == '\\' || current == '"' || current == '\b' || current == '\f' || current == '\n' || current == '\r' || current == '\t':
				length += 2
			case current < 0x20 || escapeHTML && (current == '<' || current == '>' || current == '&'):
				length += 6
			default:
				length++
			}
			continue
		}
		runeValue, size := utf8.DecodeRuneInString(value[index:])
		if runeValue == utf8.RuneError && size == 1 {
			length += 6
			index++
			continue
		}
		if runeValue == '\u2028' || runeValue == '\u2029' {
			length += 6
		} else {
			length += int64(size)
		}
		index += size
	}
	return length
}

func addWireBytes(left, right int64) int64 {
	if right > math.MaxInt64-left {
		return math.MaxInt64
	}
	return left + right
}

func addWireDepth(value, increment int64) int64 {
	if value > math.MaxInt64-increment {
		return math.MaxInt64
	}
	return value + increment
}
