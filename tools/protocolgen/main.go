// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Command protocolgen validates LayerDraw's language-neutral protocol schemas
// and deterministically emits the Go and TypeScript wire packages.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const generatorVersion = "layerdraw-protocolgen/1"

const (
	schemaDialectID            = "https://schemas.layerdraw.dev/meta/protocol/v1"
	schemaVocabulary           = "https://schemas.layerdraw.dev/vocab/protocol/v1"
	formatAssertionVocabulary  = "https://json-schema.org/draft/2020-12/vocab/format-assertion"
	schemaDialectPath          = "schemas/meta/layerdraw-protocol-schema-v1.json"
	dialectIdentityCode        = "DIALECT_IDENTITY"
	dialectRootShapeCode       = "DIALECT_ROOT_SHAPE"
	dialectVocabularyCode      = "DIALECT_VOCABULARY_INVENTORY"
	dialectVocabularyValueCode = "DIALECT_VOCABULARY_REQUIRED"
	dialectKeywordCode         = "DIALECT_KEYWORD_INVENTORY"
	dialectKeywordShapeCode    = "DIALECT_KEYWORD_SCHEMA"
	dialectDefinitionCode      = "DIALECT_DEFINITION_INVENTORY"
	dialectDefinitionShapeCode = "DIALECT_DEFINITION_SCHEMA"
)

var (
	snakeCase                   = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
	typeName                    = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	requiredDialectVocabularies = mustDecodeDialectObject(`{
		"https://json-schema.org/draft/2020-12/vocab/applicator": true,
		"https://json-schema.org/draft/2020-12/vocab/content": true,
		"https://json-schema.org/draft/2020-12/vocab/core": true,
		"https://json-schema.org/draft/2020-12/vocab/format-annotation": true,
		"https://json-schema.org/draft/2020-12/vocab/format-assertion": true,
		"https://json-schema.org/draft/2020-12/vocab/meta-data": true,
		"https://json-schema.org/draft/2020-12/vocab/unevaluated": true,
		"https://json-schema.org/draft/2020-12/vocab/validation": true,
		"https://schemas.layerdraw.dev/vocab/protocol/v1": true
	}`)
	requiredDialectKeywordSchemas = mustDecodeDialectObject(`{
		"x-layerdraw-address-terminal-id": {"$ref": "#/$defs/addressTerminalIDRule"},
		"x-layerdraw-address-owners": {"type": "array", "items": {"$ref": "#/$defs/addressOwnerRule"}, "minItems": 1, "uniqueItems": true},
		"x-layerdraw-canonical-enum-order": {"type": "boolean", "const": true},
		"x-layerdraw-canonical-identifier-order": {"type": "boolean", "const": true},
		"x-layerdraw-diff-source": {"type": "boolean"},
		"x-layerdraw-disjoint-array-keys": {"type": "array", "items": {"$ref": "#/$defs/disjointArrayKey"}, "minItems": 1, "uniqueItems": true},
		"x-layerdraw-disjoint-arrays": {"type": "array", "items": {"$ref": "#/$defs/disjointArrayPair"}, "minItems": 1, "uniqueItems": true},
		"x-layerdraw-export-recipe": {"type": "boolean", "const": true},
		"x-layerdraw-go-package": {"type": "string", "minLength": 1},
		"x-layerdraw-limit-capability": {"type": "boolean"},
		"x-layerdraw-max-json-bytes": {"type": "integer", "minimum": 1024},
		"x-layerdraw-max-json-depth": {"type": "integer", "minimum": 1},
		"x-layerdraw-operator-value": {"$ref": "#/$defs/operatorValue"},
		"x-layerdraw-ordered-pairs": {"type": "array", "items": {"$ref": "#/$defs/orderedPairRule"}, "minItems": 1, "uniqueItems": true},
		"x-layerdraw-ordered-range": {"type": "boolean"},
		"x-layerdraw-outcome-envelope": {"type": "boolean"},
		"x-layerdraw-protocol-offer": {"type": "boolean"},
		"x-layerdraw-scalar-unicode": {"type": "boolean", "const": true},
		"x-layerdraw-stable-address-order": {"type": "string", "description": "For an array, require strict Language 1 StableSymbol order using either $item or the named string property of each item."},
		"x-layerdraw-tagged-union": {"$ref": "#/$defs/taggedUnion"},
		"x-layerdraw-ts-module": {"type": "string", "minLength": 1},
		"x-layerdraw-unicode-scalar-order": {"type": "boolean", "const": true},
		"x-layerdraw-unique-array-keys": {"type": "array", "items": {"$ref": "#/$defs/uniqueArrayKey"}},
		"x-layerdraw-view-recipe": {"type": "boolean", "const": true}
	}`)
	requiredDialectDefinitions = mustDecodeDialectObject(`{
		"addressTerminalIDRule": {
			"type": "object",
			"properties": {"address": {"type": "string", "minLength": 1}, "id": {"type": "string", "minLength": 1}},
			"required": ["address", "id"],
			"additionalProperties": false
		},
		"addressOwnerRule": {
			"type": "object",
			"properties": {
				"children": {"type": "string", "minLength": 1},
				"owner": {"type": "string", "minLength": 1},
				"selector": {"type": "string", "minLength": 1}
			},
			"required": ["owner", "children", "selector"],
			"additionalProperties": false
		},
		"disjointArrayPair": {
			"type": "object",
			"properties": {"left": {"type": "string", "minLength": 1}, "right": {"type": "string", "minLength": 1}},
			"required": ["left", "right"],
			"additionalProperties": false
		},
		"disjointArrayKey": {
			"type": "object",
			"properties": {
				"array": {"type": "string", "minLength": 1},
				"property": {"type": "string", "minLength": 1},
				"strings": {"type": "string", "minLength": 1}
			},
			"required": ["array", "property", "strings"],
			"additionalProperties": false
		},
		"fieldNames": {"type": "array", "items": {"type": "string", "minLength": 1}, "uniqueItems": true},
		"operatorValue": {
			"type": "object",
			"properties": {
				"operator": {"type": "string", "minLength": 1},
				"value": {"type": "string", "minLength": 1},
				"valueless": {"type": "array", "items": {"type": "string", "minLength": 1}, "minItems": 1, "uniqueItems": true}
			},
			"required": ["operator", "value", "valueless"],
			"additionalProperties": false
		},
		"orderedPairRule": {
			"type": "object",
			"properties": {
				"comparison": {"type": "string", "enum": ["finite_binary64", "unsigned_decimal"]},
				"lower": {"type": "string", "minLength": 1},
				"upper": {"type": "string", "minLength": 1}
			},
			"required": ["lower", "upper", "comparison"],
			"additionalProperties": false
		},
		"taggedUnion": {
			"type": "object",
			"properties": {
				"property": {"type": "string", "minLength": 1},
				"variants": {"type": "object", "minProperties": 2, "additionalProperties": {"$ref": "#/$defs/taggedVariant"}}
			},
			"required": ["property", "variants"],
			"additionalProperties": false
		},
		"taggedVariant": {
			"type": "object",
			"properties": {
				"allowed_values": {"type": "object", "minProperties": 1, "additionalProperties": {"type": "array", "items": {"type": "string", "minLength": 1}, "minItems": 1, "uniqueItems": true}},
				"empty": {"$ref": "#/$defs/fieldNames"},
				"forbidden": {"$ref": "#/$defs/fieldNames"},
				"non_empty": {"$ref": "#/$defs/fieldNames"},
				"required": {"$ref": "#/$defs/fieldNames"}
			},
			"additionalProperties": false
		},
		"uniqueArrayKey": {
			"type": "object",
			"properties": {"array": {"type": "string", "minLength": 1}, "property": {"type": "string", "minLength": 1}},
			"required": ["array", "property"],
			"additionalProperties": false
		}
	}`)
)

type schemaDocument struct {
	Schema        string                 `json:"$schema"`
	ID            string                 `json:"$id"`
	Comment       string                 `json:"$comment,omitempty"`
	Title         string                 `json:"title"`
	Package       string                 `json:"x-layerdraw-go-package"`
	Module        string                 `json:"x-layerdraw-ts-module"`
	MaxJSONBytes  int                    `json:"x-layerdraw-max-json-bytes"`
	MaxJSONDepth  int                    `json:"x-layerdraw-max-json-depth"`
	ScalarUnicode bool                   `json:"x-layerdraw-scalar-unicode"`
	Definitions   map[string]*schemaType `json:"$defs"`
	AdditionalRaw map[string]any         `json:"-"`
	path          string
	raw           []byte
	fileDigest    string
	digest        string
}

type taggedUnion struct {
	Property string                   `json:"property"`
	Variants map[string]taggedVariant `json:"variants"`
}

type taggedVariant struct {
	Required      []string            `json:"required"`
	Forbidden     []string            `json:"forbidden"`
	Empty         []string            `json:"empty"`
	NonEmpty      []string            `json:"non_empty"`
	AllowedValues map[string][]string `json:"allowed_values"`
}

type operatorValueRule struct {
	Operator  string   `json:"operator"`
	Value     string   `json:"value"`
	Valueless []string `json:"valueless"`
}

type uniqueArrayKey struct {
	Array    string `json:"array"`
	Property string `json:"property"`
}

type disjointArrayPair struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}

type disjointArrayKey struct {
	Array    string `json:"array"`
	Property string `json:"property"`
	Strings  string `json:"strings"`
}

type orderedPairRule struct {
	Lower      string `json:"lower"`
	Upper      string `json:"upper"`
	Comparison string `json:"comparison"`
}

type addressTerminalIDRule struct {
	Address string `json:"address"`
	ID      string `json:"id"`
}

type addressOwnerRule struct {
	Owner    string `json:"owner"`
	Children string `json:"children"`
	Selector string `json:"selector"`
}

type schemaType struct {
	Ref                  string                 `json:"$ref,omitempty"`
	Comment              string                 `json:"$comment,omitempty"`
	Type                 any                    `json:"type,omitempty"`
	Description          string                 `json:"description,omitempty"`
	Enum                 []string               `json:"enum,omitempty"`
	Const                any                    `json:"const,omitempty"`
	Properties           map[string]*schemaType `json:"properties,omitempty"`
	Required             []string               `json:"required,omitempty"`
	Items                *schemaType            `json:"items,omitempty"`
	PropertyNames        *schemaType            `json:"propertyNames,omitempty"`
	AdditionalProperties any                    `json:"additionalProperties,omitempty"`
	Pattern              string                 `json:"pattern,omitempty"`
	Format               string                 `json:"format,omitempty"`
	Minimum              *float64               `json:"minimum,omitempty"`
	Maximum              *float64               `json:"maximum,omitempty"`
	MinLength            *int                   `json:"minLength,omitempty"`
	MinItems             *int                   `json:"minItems,omitempty"`
	UniqueItems          bool                   `json:"uniqueItems,omitempty"`
	OneOf                []*schemaType          `json:"oneOf,omitempty"`
	TaggedUnion          *taggedUnion           `json:"x-layerdraw-tagged-union,omitempty"`
	OutcomeEnvelope      bool                   `json:"x-layerdraw-outcome-envelope,omitempty"`
	OrderedRange         bool                   `json:"x-layerdraw-ordered-range,omitempty"`
	OperatorValue        *operatorValueRule     `json:"x-layerdraw-operator-value,omitempty"`
	ProtocolOffer        bool                   `json:"x-layerdraw-protocol-offer,omitempty"`
	LimitCapability      bool                   `json:"x-layerdraw-limit-capability,omitempty"`
	UniqueArrayKeys      []uniqueArrayKey       `json:"x-layerdraw-unique-array-keys,omitempty"`
	DisjointArrays       []disjointArrayPair    `json:"x-layerdraw-disjoint-arrays,omitempty"`
	DisjointArrayKeys    []disjointArrayKey     `json:"x-layerdraw-disjoint-array-keys,omitempty"`
	DiffSource           bool                   `json:"x-layerdraw-diff-source,omitempty"`
	StableAddressOrder   string                 `json:"x-layerdraw-stable-address-order,omitempty"`
	CanonicalEnumOrder   bool                   `json:"x-layerdraw-canonical-enum-order,omitempty"`
	CanonicalIDOrder     bool                   `json:"x-layerdraw-canonical-identifier-order,omitempty"`
	UnicodeScalarOrder   bool                   `json:"x-layerdraw-unicode-scalar-order,omitempty"`
	OrderedPairs         []orderedPairRule      `json:"x-layerdraw-ordered-pairs,omitempty"`
	AddressOwners        []addressOwnerRule     `json:"x-layerdraw-address-owners,omitempty"`
	AddressTerminalID    *addressTerminalIDRule `json:"x-layerdraw-address-terminal-id,omitempty"`
	ExportRecipe         bool                   `json:"x-layerdraw-export-recipe,omitempty"`
	ViewRecipe           bool                   `json:"x-layerdraw-view-recipe,omitempty"`
}

type schemaSet struct {
	documents []*schemaDocument
	byID      map[string]*schemaDocument
	dialect   digestSource
	digest    string
}

type digestSource struct {
	path       string
	raw        []byte
	fileDigest string
}

type generatedFile struct {
	path string
	data []byte
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "protocolgen:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("protocolgen", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || flags.Arg(0) != "generate" {
		return errors.New("usage: protocolgen [-root path] generate")
	}
	set, err := loadSchemas(*root)
	if err != nil {
		return err
	}
	files, err := generate(set)
	if err != nil {
		return err
	}
	for _, file := range files {
		path := filepath.Join(*root, filepath.FromSlash(file.path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, file.data, 0o644); err != nil {
			return err
		}
	}
	return removeStaleGenerated(*root, files)
}

func loadSchemas(root string) (schemaSet, error) {
	expected := []struct {
		path, id, packageName, module string
	}{
		{"schemas/protocol-common/v1.schema.json", "https://schemas.layerdraw.dev/protocol-common/v1", "protocolcommon", "common"},
		{"schemas/semantic/v1.schema.json", "https://schemas.layerdraw.dev/semantic/v1", "semantic", "semantic"},
		{"schemas/engine-protocol/v1.schema.json", "https://schemas.layerdraw.dev/engine-protocol/v1", "engineprotocol", "engine"},
	}
	var paths []string
	expectedByPath := map[string]struct{ id, packageName, module string }{}
	for _, group := range expected {
		directory := filepath.Dir(filepath.Join(root, filepath.FromSlash(group.path)))
		matches, err := filepath.Glob(filepath.Join(directory, "*.schema.json"))
		if err != nil {
			return schemaSet{}, err
		}
		wanted := filepath.Join(root, filepath.FromSlash(group.path))
		if len(matches) != 1 || matches[0] != wanted {
			return schemaSet{}, fmt.Errorf("%s must contain exactly v1.schema.json, found %v", filepath.ToSlash(filepath.Dir(group.path)), matches)
		}
		paths = append(paths, wanted)
		expectedByPath[filepath.ToSlash(group.path)] = struct{ id, packageName, module string }{group.id, group.packageName, group.module}
	}
	sort.Strings(paths)
	dialect, err := loadSchemaDialect(root)
	if err != nil {
		return schemaSet{}, err
	}
	set := schemaSet{byID: map[string]*schemaDocument{}, dialect: dialect}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return schemaSet{}, err
		}
		data, err = normalizeSchemaBytes(data)
		if err != nil {
			return schemaSet{}, fmt.Errorf("normalize %s: %w", path, err)
		}
		if err := rejectDuplicateJSONObjectKeys(data); err != nil {
			return schemaSet{}, fmt.Errorf("decode %s: %w", path, err)
		}
		var document schemaDocument
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&document); err != nil {
			return schemaSet{}, fmt.Errorf("decode %s: %w", path, err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return schemaSet{}, fmt.Errorf("decode %s: trailing JSON value", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return schemaSet{}, err
		}
		document.path = filepath.ToSlash(relative)
		document.raw = data
		digest := sha256.Sum256(data)
		document.fileDigest = "sha256:" + hex.EncodeToString(digest[:])
		if err := validateDocument(&document); err != nil {
			return schemaSet{}, fmt.Errorf("%s: %w", document.path, err)
		}
		group := expectedByPath[document.path]
		if document.ID != group.id || document.Package != group.packageName || document.Module != group.module {
			return schemaSet{}, fmt.Errorf("%s has unexpected group identity", document.path)
		}
		if set.byID[document.ID] != nil {
			return schemaSet{}, fmt.Errorf("duplicate schema $id %q", document.ID)
		}
		set.byID[document.ID] = &document
		set.documents = append(set.documents, &document)
	}
	for _, document := range set.documents[1:] {
		if document.MaxJSONBytes != set.documents[0].MaxJSONBytes || document.MaxJSONDepth != set.documents[0].MaxJSONDepth {
			return schemaSet{}, errors.New("all schema groups must declare identical JSON byte and depth limits")
		}
	}
	for _, document := range set.documents {
		for name, definition := range document.Definitions {
			if err := validateType(set, document, name, definition, map[*schemaType]bool{}); err != nil {
				return schemaSet{}, fmt.Errorf("%s $defs.%s: %w", document.path, name, err)
			}
		}
	}
	for _, document := range set.documents {
		closure := schemaClosure(set, document)
		document.digest = digestDocuments(closure, set.dialect)
	}
	aggregateSources := []digestSource{set.dialect}
	for _, document := range set.documents {
		aggregateSources = append(aggregateSources, digestSource{path: document.path, raw: document.raw, fileDigest: document.fileDigest})
	}
	sort.Slice(aggregateSources, func(i, j int) bool { return aggregateSources[i].path < aggregateSources[j].path })
	set.digest = digestSources(aggregateSources)
	return set, nil
}

func loadSchemaDialect(root string) (digestSource, error) {
	directory := filepath.Join(root, "schemas", "meta")
	matches, err := filepath.Glob(filepath.Join(directory, "*.json"))
	if err != nil {
		return digestSource{}, err
	}
	wanted := filepath.Join(root, filepath.FromSlash(schemaDialectPath))
	if len(matches) != 1 || matches[0] != wanted {
		return digestSource{}, fmt.Errorf("schemas/meta must contain exactly layerdraw-protocol-schema-v1.json, found %v", matches)
	}
	data, err := os.ReadFile(wanted)
	if err != nil {
		return digestSource{}, err
	}
	data, err = normalizeSchemaBytes(data)
	if err != nil {
		return digestSource{}, fmt.Errorf("normalize %s: %w", wanted, err)
	}
	if err := rejectDuplicateJSONObjectKeys(data); err != nil {
		return digestSource{}, fmt.Errorf("decode %s: %w", wanted, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		return digestSource{}, fmt.Errorf("decode %s: %w", wanted, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return digestSource{}, fmt.Errorf("decode %s: trailing JSON value", wanted)
	}
	if document["$schema"] != "https://json-schema.org/draft/2020-12/schema" || document["$id"] != schemaDialectID {
		return digestSource{}, dialectDiagnostic(dialectIdentityCode, "LayerDraw schema dialect must have $schema %q and $id %q", "https://json-schema.org/draft/2020-12/schema", schemaDialectID)
	}
	expectedRootApplicator := []any{map[string]any{"$ref": "https://json-schema.org/draft/2020-12/schema"}}
	if document["$dynamicAnchor"] != "meta" || !reflect.DeepEqual(document["allOf"], expectedRootApplicator) {
		return digestSource{}, dialectDiagnostic(dialectRootShapeCode, "LayerDraw schema dialect must retain $dynamicAnchor meta and the draft 2020-12 meta-schema applicator")
	}
	vocabulary, ok := document["$vocabulary"].(map[string]any)
	if !ok {
		return digestSource{}, dialectDiagnostic(dialectVocabularyCode, "LayerDraw schema dialect $vocabulary must contain exactly %s", dialectInventory(requiredDialectVocabularies))
	}
	if err := validateDialectInventory(dialectVocabularyCode, "$vocabulary", vocabulary, requiredDialectVocabularies); err != nil {
		return digestSource{}, err
	}
	for _, name := range sortedKeys(requiredDialectVocabularies) {
		if vocabulary[name] != true {
			return digestSource{}, dialectDiagnostic(dialectVocabularyValueCode, "LayerDraw schema dialect vocabulary %s must be boolean true", name)
		}
	}
	properties, ok := document["properties"].(map[string]any)
	if !ok {
		return digestSource{}, dialectDiagnostic(dialectKeywordCode, "LayerDraw schema dialect properties must contain exactly %s", dialectInventory(requiredDialectKeywordSchemas))
	}
	if err := validateDialectInventory(dialectKeywordCode, "keyword", properties, requiredDialectKeywordSchemas); err != nil {
		return digestSource{}, err
	}
	for _, name := range sortedKeys(requiredDialectKeywordSchemas) {
		if !reflect.DeepEqual(properties[name], requiredDialectKeywordSchemas[name]) {
			return digestSource{}, dialectDiagnostic(dialectKeywordShapeCode, "LayerDraw schema dialect keyword %s must have schema %s", name, dialectJSON(requiredDialectKeywordSchemas[name]))
		}
	}
	definitions, ok := document["$defs"].(map[string]any)
	if !ok {
		return digestSource{}, dialectDiagnostic(dialectDefinitionCode, "LayerDraw schema dialect $defs must contain exactly %s", dialectInventory(requiredDialectDefinitions))
	}
	if err := validateDialectInventory(dialectDefinitionCode, "$defs", definitions, requiredDialectDefinitions); err != nil {
		return digestSource{}, err
	}
	for _, name := range sortedKeys(requiredDialectDefinitions) {
		if !reflect.DeepEqual(definitions[name], requiredDialectDefinitions[name]) {
			return digestSource{}, dialectDiagnostic(dialectDefinitionShapeCode, "LayerDraw schema dialect definition %s must have schema %s", name, dialectJSON(requiredDialectDefinitions[name]))
		}
	}
	digest := sha256.Sum256(data)
	return digestSource{path: schemaDialectPath, raw: data, fileDigest: "sha256:" + hex.EncodeToString(digest[:])}, nil
}

func mustDecodeDialectObject(source string) map[string]any {
	var result map[string]any
	if err := json.Unmarshal([]byte(source), &result); err != nil {
		panic(err)
	}
	return result
}

func dialectDiagnostic(code, format string, arguments ...any) error {
	return fmt.Errorf("%s: %s", code, fmt.Sprintf(format, arguments...))
}

func dialectInventory(values map[string]any) string {
	return strings.Join(sortedKeys(values), ", ")
}

func dialectJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func validateDialectInventory(code, label string, actual, expected map[string]any) error {
	actualNames := sortedKeys(actual)
	expectedNames := sortedKeys(expected)
	if !reflect.DeepEqual(actualNames, expectedNames) {
		return dialectDiagnostic(code, "LayerDraw schema dialect %s inventory must be exactly [%s], found [%s]", label, strings.Join(expectedNames, ", "), strings.Join(actualNames, ", "))
	}
	return nil
}

func normalizeSchemaBytes(data []byte) ([]byte, error) {
	if !bytes.Contains(data, []byte{'\r'}) {
		return data, nil
	}
	normalized := make([]byte, 0, len(data))
	for index := 0; index < len(data); index++ {
		if data[index] != '\r' {
			normalized = append(normalized, data[index])
			continue
		}
		if index+1 >= len(data) || data[index+1] != '\n' {
			return nil, errors.New("bare carriage return is forbidden")
		}
		normalized = append(normalized, '\n')
		index++
	}
	return normalized, nil
}

func rejectDuplicateJSONObjectKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return scanUniqueSchemaJSONValue(decoder)
}

func scanUniqueSchemaJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			rawKey, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := rawKey.(string)
			if !ok {
				return errors.New("JSON object key must be a string")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = true
			if err := scanUniqueSchemaJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return errors.New("JSON object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := scanUniqueSchemaJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return errors.New("JSON array is not closed")
		}
	default:
		return errors.New("JSON has an unexpected delimiter")
	}
	return nil
}

func schemaClosure(set schemaSet, root *schemaDocument) []*schemaDocument {
	seen := map[string]bool{}
	var visitDocument func(*schemaDocument)
	var visitType func(*schemaDocument, *schemaType)
	visitType = func(current *schemaDocument, value *schemaType) {
		if value == nil {
			return
		}
		if value.Ref != "" {
			target, _, err := resolveRef(set, current, value.Ref)
			if err == nil {
				visitDocument(target)
			}
		}
		for _, property := range value.Properties {
			visitType(current, property)
		}
		visitType(current, value.Items)
		visitType(current, value.PropertyNames)
		if nested, ok := value.AdditionalProperties.(*schemaType); ok {
			visitType(current, nested)
		}
		for _, branch := range value.OneOf {
			visitType(current, branch)
		}
	}
	visitDocument = func(document *schemaDocument) {
		if seen[document.ID] {
			return
		}
		seen[document.ID] = true
		for _, definition := range document.Definitions {
			visitType(document, definition)
		}
	}
	visitDocument(root)
	closure := make([]*schemaDocument, 0, len(seen))
	for _, document := range set.documents {
		if seen[document.ID] {
			closure = append(closure, document)
		}
	}
	sort.Slice(closure, func(i, j int) bool { return closure[i].path < closure[j].path })
	return closure
}

func digestDocuments(documents []*schemaDocument, dialect digestSource) string {
	sources := []digestSource{dialect}
	for _, document := range documents {
		sources = append(sources, digestSource{path: document.path, raw: document.raw, fileDigest: document.fileDigest})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].path < sources[j].path })
	return digestSources(sources)
}

func digestSources(sources []digestSource) string {
	hash := sha256.New()
	for _, source := range sources {
		hash.Write([]byte(source.path))
		hash.Write([]byte{0})
		hash.Write(source.raw)
		hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func validateDocument(document *schemaDocument) error {
	if document.Schema != schemaDialectID {
		return fmt.Errorf("$schema must require the LayerDraw protocol dialect %s", schemaDialectID)
	}
	if document.ID == "" || document.Title == "" || document.Package == "" || document.Module == "" {
		return errors.New("$id, title, x-layerdraw-go-package, and x-layerdraw-ts-module are required")
	}
	if document.MaxJSONBytes < 1024 || document.MaxJSONDepth < 1 {
		return errors.New("x-layerdraw-max-json-bytes and x-layerdraw-max-json-depth must be positive protocol limits")
	}
	if !document.ScalarUnicode {
		return errors.New("DIALECT_SCHEMA_SCALAR_UNICODE: every protocol schema must assert x-layerdraw-scalar-unicode: true at its root")
	}
	if len(document.Definitions) == 0 {
		return errors.New("$defs must not be empty")
	}
	for name := range document.Definitions {
		if !typeName.MatchString(name) {
			return fmt.Errorf("definition %q must be UpperCamelCase", name)
		}
	}
	return nil
}

func validateType(set schemaSet, document *schemaDocument, context string, value *schemaType, seen map[*schemaType]bool) error {
	if value == nil || seen[value] {
		return nil
	}
	seen[value] = true
	if value.Ref != "" {
		if _, _, err := resolveRef(set, document, value.Ref); err != nil {
			return err
		}
		return nil
	}
	typeValue, err := scalarType(value.Type)
	if err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	if len(value.OneOf) != 0 {
		if context != "JsonValue" {
			return fmt.Errorf("%s uses unsupported oneOf; only the generated JsonValue union is supported", context)
		}
		for index, branch := range value.OneOf {
			if err := validateType(set, document, fmt.Sprintf("%s.oneOf[%d]", context, index), branch, seen); err != nil {
				return err
			}
		}
		return nil
	}
	if typeValue == "" {
		return fmt.Errorf("%s has neither $ref, type, nor oneOf", context)
	}
	if len(value.Enum) != 0 && typeValue != "string" {
		return fmt.Errorf("%s enum must have string type", context)
	}
	if value.Format != "" {
		allowed := map[string]bool{
			"canonical-source-path": true, "date-time": true, "finite-binary64-decimal": true,
			"int64-decimal": true, "nonnegative-int64-decimal": true, "nonnegative-safe-integer-decimal": true,
			"positive-finite-binary64-decimal": true, "positive-int64-decimal": true, "positive-safe-integer-decimal": true,
			"protocol-version": true, "protocol-version-or-range": true, "protocol-version-range": true,
			"safe-integer-decimal": true, "uint64-decimal": true,
		}
		if !allowed[value.Format] {
			return fmt.Errorf("%s has unsupported format %q", context, value.Format)
		}
	}
	if typeValue == "integer" {
		if value.Minimum == nil || value.Maximum == nil {
			return fmt.Errorf("%s integer requires explicit minimum and maximum", context)
		}
		if math.Trunc(*value.Minimum) != *value.Minimum || math.Trunc(*value.Maximum) != *value.Maximum || *value.Minimum > *value.Maximum || *value.Minimum < -9007199254740991 || *value.Maximum > 9007199254740991 {
			return fmt.Errorf("%s integer bounds must be integral, ordered, and portable-safe", context)
		}
	}
	if typeValue == "object" {
		if value.PropertyNames != nil {
			if err := validateType(set, document, context+".propertyNames", value.PropertyNames, seen); err != nil {
				return err
			}
			propertyNameType := value.PropertyNames
			if propertyNameType.Ref != "" {
				target, name, err := resolveRef(set, document, propertyNameType.Ref)
				if err != nil {
					return err
				}
				propertyNameType = target.Definitions[name]
			}
			if nameType, err := scalarType(propertyNameType.Type); err != nil || nameType != "string" {
				return fmt.Errorf("%s propertyNames must validate strings", context)
			}
		}
		required := map[string]bool{}
		for _, name := range value.Required {
			if required[name] {
				return fmt.Errorf("%s repeats required property %q", context, name)
			}
			required[name] = true
			if value.Properties[name] == nil {
				return fmt.Errorf("%s requires unknown property %q", context, name)
			}
		}
		for name, property := range value.Properties {
			if !snakeCase.MatchString(name) {
				return fmt.Errorf("%s property %q must be lower_snake_case", context, name)
			}
			if err := validateType(set, document, context+"."+name, property, seen); err != nil {
				return err
			}
		}
		switch additional := value.AdditionalProperties.(type) {
		case nil:
			return fmt.Errorf("%s must declare additionalProperties explicitly", context)
		case bool:
			if additional && len(value.Properties) != 0 {
				return fmt.Errorf("%s open records are forbidden; add an explicit extensions property", context)
			}
		case map[string]any:
			data, _ := json.Marshal(additional)
			var nested schemaType
			if err := json.Unmarshal(data, &nested); err != nil {
				return err
			}
			value.AdditionalProperties = &nested
			if err := validateType(set, document, context+".additionalProperties", &nested, seen); err != nil {
				return err
			}
		case *schemaType:
			if err := validateType(set, document, context+".additionalProperties", additional, seen); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s has invalid additionalProperties", context)
		}
		if value.TaggedUnion != nil {
			discriminator := value.Properties[value.TaggedUnion.Property]
			if discriminator == nil || len(value.TaggedUnion.Variants) < 2 || !stringSet(value.Required)[value.TaggedUnion.Property] {
				return fmt.Errorf("%s has invalid tagged union", context)
			}
			if discriminator.Ref != "" {
				target, name, err := resolveRef(set, document, discriminator.Ref)
				if err != nil {
					return err
				}
				discriminator = target.Definitions[name]
			}
			discriminatorType, err := scalarType(discriminator.Type)
			if err != nil {
				return err
			}
			tags := stringSet(discriminator.Enum)
			if discriminatorType == "boolean" {
				tags = map[string]bool{"false": true, "true": true}
			} else if discriminatorType != "string" {
				return fmt.Errorf("%s tagged union discriminator must be a string enum or boolean", context)
			}
			if len(tags) != len(value.TaggedUnion.Variants) {
				return fmt.Errorf("%s tagged union variants must exactly match discriminator enum", context)
			}
			for tag, variant := range value.TaggedUnion.Variants {
				if !tags[tag] {
					return fmt.Errorf("%s has unknown tagged union value %q", context, tag)
				}
				requiredVariant := stringSet(variant.Required)
				forbiddenVariant := stringSet(variant.Forbidden)
				emptyVariant := stringSet(variant.Empty)
				nonEmptyVariant := stringSet(variant.NonEmpty)
				properties := append(append(append(append([]string{}, variant.Required...), variant.Forbidden...), variant.Empty...), variant.NonEmpty...)
				for property := range variant.AllowedValues {
					properties = append(properties, property)
				}
				for _, property := range properties {
					if value.Properties[property] == nil {
						return fmt.Errorf("%s tagged union refers to unknown property %q", context, property)
					}
					memberships := 0
					for _, set := range []map[string]bool{requiredVariant, forbiddenVariant, emptyVariant, nonEmptyVariant} {
						if set[property] {
							memberships++
						}
					}
					if memberships > 1 && !(requiredVariant[property] && (emptyVariant[property] || nonEmptyVariant[property]) && memberships == 2) {
						return fmt.Errorf("%s tagged union gives contradictory rules for %q", context, property)
					}
				}
				for _, property := range append(append([]string{}, variant.Empty...), variant.NonEmpty...) {
					propertyType := value.Properties[property]
					if propertyType.Ref != "" {
						target, name, err := resolveRef(set, document, propertyType.Ref)
						if err != nil {
							return err
						}
						propertyType = target.Definitions[name]
					}
					typeName, err := scalarType(propertyType.Type)
					if err != nil || typeName != "array" {
						return fmt.Errorf("%s tagged union empty/non_empty rule requires array property %q", context, property)
					}
				}
				for property, allowedValues := range variant.AllowedValues {
					if forbiddenVariant[property] || len(allowedValues) == 0 {
						return fmt.Errorf("%s tagged union has invalid allowed_values rule for %q", context, property)
					}
					propertyType := value.Properties[property]
					if propertyType.Ref != "" {
						target, name, err := resolveRef(set, document, propertyType.Ref)
						if err != nil {
							return err
						}
						propertyType = target.Definitions[name]
					}
					typeName, err := scalarType(propertyType.Type)
					propertyValues := stringSet(propertyType.Enum)
					seenValues := map[string]bool{}
					if err != nil || typeName != "string" || len(propertyValues) == 0 {
						return fmt.Errorf("%s tagged union allowed_values requires string-enum property %q", context, property)
					}
					for _, allowedValue := range allowedValues {
						if !propertyValues[allowedValue] || seenValues[allowedValue] {
							return fmt.Errorf("%s tagged union has invalid allowed value %q for %q", context, allowedValue, property)
						}
						seenValues[allowedValue] = true
					}
				}
			}
		}
		if value.DiffSource {
			required := stringSet(value.Required)
			for _, property := range []string{"kind", "before", "after", "query_address", "arguments"} {
				if value.Properties[property] == nil {
					return fmt.Errorf("%s diff source assertion requires %s", context, property)
				}
			}
			kind := value.Properties["kind"]
			if kind.Ref != "" {
				target, name, err := resolveRef(set, document, kind.Ref)
				if err != nil {
					return err
				}
				kind = target.Definitions[name]
			}
			before := value.Properties["before"]
			after := value.Properties["after"]
			arguments := value.Properties["arguments"]
			kindType, kindErr := scalarType(kind.Type)
			beforeType, beforeErr := scalarType(before.Type)
			afterType, afterErr := scalarType(after.Type)
			argumentsType, argumentsErr := scalarType(arguments.Type)
			if kindErr != nil || kindType != "string" || !stringSet(kind.Enum)["diff"] ||
				beforeErr != nil || beforeType != "string" || before.MinLength == nil || *before.MinLength < 1 ||
				afterErr != nil || afterType != "string" || after.MinLength == nil || *after.MinLength < 1 ||
				argumentsErr != nil || argumentsType != "object" || !required["arguments"] {
				return fmt.Errorf("%s has invalid diff source assertion shape", context)
			}
		}
		if value.OperatorValue != nil {
			rule := value.OperatorValue
			operator := value.Properties[rule.Operator]
			valueProperty := value.Properties[rule.Value]
			if operator == nil || valueProperty == nil || len(rule.Valueless) == 0 {
				return fmt.Errorf("%s has invalid operator/value rule", context)
			}
			if operator.Ref != "" {
				target, name, err := resolveRef(set, document, operator.Ref)
				if err != nil {
					return err
				}
				operator = target.Definitions[name]
			}
			operators := stringSet(operator.Enum)
			for _, valueless := range rule.Valueless {
				if !operators[valueless] {
					return fmt.Errorf("%s operator/value rule names unknown operator %q", context, valueless)
				}
			}
		}
		if value.ProtocolOffer {
			if value.Properties["supported_range"] == nil || value.Properties["versions"] == nil {
				return fmt.Errorf("%s protocol offer requires supported_range and versions", context)
			}
		}
		if value.LimitCapability {
			required := stringSet(value.Required)
			for _, property := range []string{"default_value", "effective_maximum", "hard_maximum", "unit"} {
				if value.Properties[property] == nil || !required[property] {
					return fmt.Errorf("%s limit capability requires %s", context, property)
				}
			}
		}
		for _, rule := range value.UniqueArrayKeys {
			array := value.Properties[rule.Array]
			if array == nil || array.Items == nil || rule.Property == "" {
				return fmt.Errorf("%s has invalid unique array key rule", context)
			}
			item := array.Items
			if item.Ref != "" {
				target, name, err := resolveRef(set, document, item.Ref)
				if err != nil {
					return err
				}
				item = target.Definitions[name]
			}
			itemType, err := scalarType(item.Type)
			if err != nil || itemType != "object" || item.Properties[rule.Property] == nil {
				return fmt.Errorf("%s unique array key rule does not name an object property", context)
			}
		}
		for _, rule := range value.DisjointArrays {
			if rule.Left == "" || rule.Right == "" || rule.Left == rule.Right {
				return fmt.Errorf("%s has invalid disjoint array rule", context)
			}
			for _, property := range []string{rule.Left, rule.Right} {
				array := value.Properties[property]
				if array == nil || array.Items == nil {
					return fmt.Errorf("%s disjoint array rule names non-array property %q", context, property)
				}
				item := array.Items
				if item.Ref != "" {
					target, name, err := resolveRef(set, document, item.Ref)
					if err != nil {
						return err
					}
					item = target.Definitions[name]
				}
				itemType, err := scalarType(item.Type)
				if err != nil || itemType != "string" {
					return fmt.Errorf("%s disjoint array rule requires string-array property %q", context, property)
				}
			}
		}
		for _, rule := range value.DisjointArrayKeys {
			objects := resolvedType(set, document, value.Properties[rule.Array])
			stringsArray := resolvedType(set, document, value.Properties[rule.Strings])
			if objects == nil || stringsArray == nil || rule.Array == rule.Strings || rule.Property == "" {
				return fmt.Errorf("%s has invalid disjoint array-key rule", context)
			}
			objectsType, objectsErr := scalarType(objects.Type)
			objectItem := resolvedType(set, document, objects.Items)
			stringsType, stringsErr := scalarType(stringsArray.Type)
			stringItem := resolvedType(set, document, stringsArray.Items)
			if objectsErr != nil || objectsType != "array" || objectItem == nil || stringsErr != nil || stringsType != "array" || stringItem == nil {
				return fmt.Errorf("%s disjoint array-key rule requires array properties", context)
			}
			objectItemType, objectItemErr := scalarType(objectItem.Type)
			key := resolvedType(set, document, objectItem.Properties[rule.Property])
			keyType, keyErr := "", error(nil)
			if key != nil {
				keyType, keyErr = scalarType(key.Type)
			}
			stringItemType, stringItemErr := scalarType(stringItem.Type)
			if objectItemErr != nil || objectItemType != "object" || keyErr != nil || keyType != "string" || stringItemErr != nil || stringItemType != "string" {
				return fmt.Errorf("%s disjoint array-key rule requires object string keys and string-array values", context)
			}
		}
		for _, rule := range value.OrderedPairs {
			lower := resolvedType(set, document, value.Properties[rule.Lower])
			upper := resolvedType(set, document, value.Properties[rule.Upper])
			if lower == nil || upper == nil || rule.Lower == rule.Upper {
				return fmt.Errorf("%s has invalid ordered-pair rule", context)
			}
			lowerType, lowerErr := scalarType(lower.Type)
			upperType, upperErr := scalarType(upper.Type)
			if lowerErr != nil || lowerType != "string" || upperErr != nil || upperType != "string" {
				return fmt.Errorf("%s ordered-pair rule requires string properties", context)
			}
			switch rule.Comparison {
			case "unsigned_decimal":
				allowed := map[string]bool{"nonnegative-int64-decimal": true, "nonnegative-safe-integer-decimal": true, "uint64-decimal": true}
				if !allowed[lower.Format] || !allowed[upper.Format] {
					return fmt.Errorf("%s unsigned ordered-pair rule requires canonical unsigned-decimal formats", context)
				}
			case "finite_binary64":
				allowed := map[string]bool{"finite-binary64-decimal": true, "positive-finite-binary64-decimal": true}
				if !allowed[lower.Format] || !allowed[upper.Format] {
					return fmt.Errorf("%s finite ordered-pair rule requires canonical binary64 formats", context)
				}
			default:
				return fmt.Errorf("%s ordered-pair rule has unknown comparison %q", context, rule.Comparison)
			}
		}
		if value.AddressTerminalID != nil {
			rule := value.AddressTerminalID
			address := resolvedType(set, document, value.Properties[rule.Address])
			id := resolvedType(set, document, value.Properties[rule.ID])
			required := stringSet(value.Required)
			if address == nil || id == nil || rule.Address == rule.ID || !required[rule.Address] || !required[rule.ID] {
				return fmt.Errorf("%s has invalid address terminal-ID rule", context)
			}
			addressType, addressErr := scalarType(address.Type)
			idType, idErr := scalarType(id.Type)
			if addressErr != nil || addressType != "string" || idErr != nil || idType != "string" {
				return fmt.Errorf("%s address terminal-ID rule requires string properties", context)
			}
		}
		if value.ExportRecipe {
			if err := validateExportRecipeAssertionShape(set, document, context, value); err != nil {
				return err
			}
		}
		if value.ViewRecipe {
			if err := validateViewRecipeAssertionShape(set, document, context, value); err != nil {
				return err
			}
		}
		if value.OutcomeEnvelope {
			for _, property := range []string{"outcome", "payload", "failure", "diagnostics"} {
				if value.Properties[property] == nil {
					return fmt.Errorf("%s outcome envelope requires %s metadata", context, property)
				}
			}
		}
		if value.OrderedRange {
			required := stringSet(value.Required)
			if value.Properties["start_byte"] == nil || value.Properties["end_byte"] == nil || !required["start_byte"] || !required["end_byte"] {
				return fmt.Errorf("%s ordered range requires start_byte and end_byte", context)
			}
		}
		for _, rule := range value.AddressOwners {
			owner := resolvedType(set, document, value.Properties[rule.Owner])
			children := resolvedType(set, document, value.Properties[rule.Children])
			if owner == nil || children == nil || rule.Owner == rule.Children {
				return fmt.Errorf("%s has invalid address-owner rule", context)
			}
			ownerType, ownerErr := scalarType(owner.Type)
			if ownerErr != nil || ownerType != "string" {
				return fmt.Errorf("%s address-owner rule owner %q must be a string property", context, rule.Owner)
			}
			switch rule.Selector {
			case "$value":
				childrenType, childrenErr := scalarType(children.Type)
				if childrenErr != nil || childrenType != "string" {
					return fmt.Errorf("%s address-owner $value rule requires string children property %q", context, rule.Children)
				}
			case "$propertyNames":
				childrenType, childrenErr := scalarType(children.Type)
				if childrenErr != nil || childrenType != "object" || children.PropertyNames == nil {
					return fmt.Errorf("%s address-owner $propertyNames rule requires an object with propertyNames", context)
				}
			case "":
				return fmt.Errorf("%s address-owner rule requires selector", context)
			default:
				childrenType, childrenErr := scalarType(children.Type)
				item := resolvedType(set, document, children.Items)
				if childrenErr != nil || childrenType != "array" || item == nil {
					return fmt.Errorf("%s address-owner selector %q requires an array of objects", context, rule.Selector)
				}
				itemType, itemErr := scalarType(item.Type)
				selected := resolvedType(set, document, item.Properties[rule.Selector])
				selectedType, selectedErr := "", error(nil)
				if selected != nil {
					selectedType, selectedErr = scalarType(selected.Type)
				}
				if itemErr != nil || itemType != "object" || selectedErr != nil || selectedType != "string" {
					return fmt.Errorf("%s address-owner selector %q must name a string item property", context, rule.Selector)
				}
			}
		}
	}
	if typeValue == "array" {
		if value.Items == nil {
			return fmt.Errorf("%s array requires items", context)
		}
		if value.StableAddressOrder != "" {
			item := value.Items
			if item.Ref != "" {
				target, name, err := resolveRef(set, document, item.Ref)
				if err != nil {
					return err
				}
				item = target.Definitions[name]
			}
			ordered := item
			if value.StableAddressOrder != "$item" {
				itemType, err := scalarType(item.Type)
				if err != nil || itemType != "object" || item.Properties[value.StableAddressOrder] == nil {
					return fmt.Errorf("%s stable-address order selector does not name an item property", context)
				}
				ordered = item.Properties[value.StableAddressOrder]
				if ordered.Ref != "" {
					target, name, err := resolveRef(set, document, ordered.Ref)
					if err != nil {
						return err
					}
					ordered = target.Definitions[name]
				}
			}
			orderedType, err := scalarType(ordered.Type)
			if err != nil || orderedType != "string" {
				return fmt.Errorf("%s stable-address order selector must resolve to strings", context)
			}
		}
		if value.UniqueItems {
			item := value.Items
			if item.Ref != "" {
				target, name, err := resolveRef(set, document, item.Ref)
				if err != nil {
					return err
				}
				item = target.Definitions[name]
			}
			itemType, err := scalarType(item.Type)
			if err != nil || itemType != "string" {
				return fmt.Errorf("%s uniqueItems currently requires string items", context)
			}
		}
		if value.CanonicalEnumOrder {
			item := resolvedType(set, document, value.Items)
			itemType, err := "", error(nil)
			if item != nil {
				itemType, err = scalarType(item.Type)
			}
			if err != nil || itemType != "string" || len(item.Enum) == 0 || !value.UniqueItems {
				return fmt.Errorf("%s canonical enum order requires string-enum items and uniqueItems", context)
			}
		}
		if value.CanonicalIDOrder {
			item := resolvedType(set, document, value.Items)
			itemType, err := "", error(nil)
			if item != nil {
				itemType, err = scalarType(item.Type)
			}
			if err != nil || itemType != "string" || !value.UniqueItems {
				return fmt.Errorf("%s canonical identifier order requires string items and uniqueItems", context)
			}
		}
		if value.UnicodeScalarOrder {
			item := resolvedType(set, document, value.Items)
			itemType, err := "", error(nil)
			if item != nil {
				itemType, err = scalarType(item.Type)
			}
			if err != nil || itemType != "string" || !value.UniqueItems {
				return fmt.Errorf("%s Unicode scalar order requires string items and uniqueItems", context)
			}
		}
		return validateType(set, document, context+"[]", value.Items, seen)
	}
	return nil
}

func validateExportRecipeAssertionShape(set schemaSet, document *schemaDocument, context string, value *schemaType) error {
	required := stringSet(value.Required)
	for _, property := range []string{"exporter_profile", "extension", "filename", "format", "options"} {
		if value.Properties[property] == nil || !required[property] {
			return fmt.Errorf("%s export recipe assertion requires %s", context, property)
		}
	}
	format := resolvedType(set, document, value.Properties["format"])
	extension := resolvedType(set, document, value.Properties["extension"])
	filename := resolvedType(set, document, value.Properties["filename"])
	options := resolvedType(set, document, value.Properties["options"])
	profile := resolvedType(set, document, value.Properties["exporter_profile"])
	if format == nil || extension == nil || filename == nil || options == nil || profile == nil {
		return fmt.Errorf("%s has unresolved export recipe assertion properties", context)
	}
	formatType, formatErr := scalarType(format.Type)
	extensionType, extensionErr := scalarType(extension.Type)
	filenameType, filenameErr := scalarType(filename.Type)
	optionsType, optionsErr := scalarType(options.Type)
	profileType, profileErr := scalarType(profile.Type)
	if formatErr != nil || formatType != "string" || extensionErr != nil || extensionType != "string" || filenameErr != nil || filenameType != "string" || optionsErr != nil || optionsType != "object" || profileErr != nil || profileType != "object" {
		return fmt.Errorf("%s has invalid export recipe assertion property types", context)
	}
	expectedFormats := stringSet([]string{"bpmn", "csv", "docx", "drawio", "html", "json", "markdown", "mermaid", "pdf", "png", "pptx", "svg", "tsv", "xlsx", "yaml"})
	if !reflect.DeepEqual(stringSet(format.Enum), expectedFormats) {
		return fmt.Errorf("%s export recipe assertion requires the complete format enum", context)
	}
	for name, object := range map[string]*schemaType{"options": options, "exporter_profile": profile} {
		kind := "kind"
		if name == "exporter_profile" {
			kind = "format"
		}
		selected := resolvedType(set, document, object.Properties[kind])
		selectedType, selectedErr := "", error(nil)
		if selected != nil {
			selectedType, selectedErr = scalarType(selected.Type)
		}
		if selectedErr != nil || selectedType != "string" || !stringSet(object.Required)[kind] {
			return fmt.Errorf("%s export recipe assertion requires %s.%s", context, name, kind)
		}
	}
	return nil
}

func validateViewRecipeAssertionShape(set schemaSet, document *schemaDocument, context string, value *schemaType) error {
	required := stringSet(value.Required)
	for _, property := range []string{"address", "reserved_table_column_ids", "shape"} {
		if value.Properties[property] == nil || !required[property] {
			return fmt.Errorf("%s view recipe assertion requires %s", context, property)
		}
	}
	address := resolvedType(set, document, value.Properties["address"])
	reserved := resolvedType(set, document, value.Properties["reserved_table_column_ids"])
	shape := resolvedType(set, document, value.Properties["shape"])
	if address == nil || reserved == nil || shape == nil {
		return fmt.Errorf("%s has unresolved view recipe assertion properties", context)
	}
	addressType, addressErr := scalarType(address.Type)
	reservedType, reservedErr := scalarType(reserved.Type)
	reservedItem := resolvedType(set, document, reserved.Items)
	reservedItemType, reservedItemErr := "", error(nil)
	if reservedItem != nil {
		reservedItemType, reservedItemErr = scalarType(reservedItem.Type)
	}
	shapeType, shapeErr := scalarType(shape.Type)
	table := resolvedType(set, document, shape.Properties["table"])
	if addressErr != nil || addressType != "string" || reservedErr != nil || reservedType != "array" || reservedItemErr != nil || reservedItemType != "string" || shapeErr != nil || shapeType != "object" || table == nil {
		return fmt.Errorf("%s has invalid view recipe assertion property types", context)
	}
	tableType, tableErr := scalarType(table.Type)
	columns := resolvedType(set, document, table.Properties["columns"])
	columnItem := (*schemaType)(nil)
	if columns != nil {
		columnItem = resolvedType(set, document, columns.Items)
	}
	columnAddress, columnID := (*schemaType)(nil), (*schemaType)(nil)
	if columnItem != nil {
		columnAddress = resolvedType(set, document, columnItem.Properties["address"])
		columnID = resolvedType(set, document, columnItem.Properties["id"])
	}
	columnsType, columnsErr := "", error(nil)
	columnItemType, columnItemErr := "", error(nil)
	columnAddressType, columnAddressErr := "", error(nil)
	columnIDType, columnIDErr := "", error(nil)
	if columns != nil {
		columnsType, columnsErr = scalarType(columns.Type)
	}
	if columnItem != nil {
		columnItemType, columnItemErr = scalarType(columnItem.Type)
	}
	if columnAddress != nil {
		columnAddressType, columnAddressErr = scalarType(columnAddress.Type)
	}
	if columnID != nil {
		columnIDType, columnIDErr = scalarType(columnID.Type)
	}
	if tableErr != nil || tableType != "object" || columnsErr != nil || columnsType != "array" || columnItemErr != nil || columnItemType != "object" || columnAddressErr != nil || columnAddressType != "string" || columnIDErr != nil || columnIDType != "string" {
		return fmt.Errorf("%s view recipe assertion requires table column identities", context)
	}
	return nil
}

func scalarType(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	case []any:
		return "union", nil
	default:
		return "", fmt.Errorf("unsupported type declaration %T", value)
	}
}

func resolveRef(set schemaSet, current *schemaDocument, ref string) (*schemaDocument, string, error) {
	parts := strings.SplitN(ref, "#/$defs/", 2)
	if len(parts) != 2 || parts[1] == "" {
		return nil, "", fmt.Errorf("unsupported $ref %q", ref)
	}
	target := current
	if parts[0] != "" {
		target = set.byID[parts[0]]
		if target == nil {
			return nil, "", fmt.Errorf("unknown schema $id in $ref %q", ref)
		}
	}
	if target.Definitions[parts[1]] == nil {
		return nil, "", fmt.Errorf("unknown definition in $ref %q", ref)
	}
	return target, parts[1], nil
}

func resolvedType(set schemaSet, document *schemaDocument, value *schemaType) *schemaType {
	if value == nil || value.Ref == "" {
		return value
	}
	target, name, err := resolveRef(set, document, value.Ref)
	if err != nil {
		return nil
	}
	return target.Definitions[name]
}

func generate(set schemaSet) ([]generatedFile, error) {
	var files []generatedFile
	for _, document := range set.documents {
		goData, err := generateGo(set, document)
		if err != nil {
			return nil, err
		}
		files = append(files, generatedFile{path: "gen/go/" + document.Package + "/types.gen.go", data: goData})
		codecData, err := generateGoCodec(set, document)
		if err != nil {
			return nil, err
		}
		files = append(files, generatedFile{path: "gen/go/" + document.Package + "/codec.gen.go", data: codecData})
		tsData, err := generateTypeScript(set, document)
		if err != nil {
			return nil, err
		}
		files = append(files, generatedFile{path: "packages/protocol/src/" + document.Module + ".gen.ts", data: tsData})
	}
	manifest := struct {
		SchemaVersion    int               `json:"schema_version"`
		GeneratorVersion string            `json:"generator_version"`
		AggregateDigest  string            `json:"aggregate_digest"`
		GroupDigests     map[string]string `json:"group_digests"`
		FileDigests      map[string]string `json:"file_digests"`
	}{SchemaVersion: 1, GeneratorVersion: generatorVersion, AggregateDigest: set.digest, GroupDigests: map[string]string{}, FileDigests: map[string]string{}}
	manifest.FileDigests[set.dialect.path] = set.dialect.fileDigest
	for _, document := range set.documents {
		manifest.GroupDigests[document.Module] = document.digest
		manifest.FileDigests[document.path] = document.fileDigest
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	files = append(files, generatedFile{path: "gen/schema-digests.json", data: append(data, '\n')})
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	return files, nil
}

func generateGo(set schemaSet, document *schemaDocument) ([]byte, error) {
	aliases := goImportAliases(set, document)
	var body strings.Builder
	fmt.Fprintf(&body, "// Code generated by %s; DO NOT EDIT.\n", generatorVersion)
	fmt.Fprintf(&body, "// Schema digest: %s\n", document.digest)
	body.WriteString("// SPDX-License-Identifier: Apache-2.0\n\n")
	fmt.Fprintf(&body, "// Package %s contains generated LayerDraw wire values.\npackage %s\n\n", document.Package, document.Package)
	if len(aliases) != 0 {
		body.WriteString("import (\n")
		keys := sortedKeys(aliases)
		for _, id := range keys {
			target := set.byID[id]
			fmt.Fprintf(&body, "\t%s \"github.com/dencyuinc/layerdraw/gen/go/%s\"\n", aliases[id], target.Package)
		}
		body.WriteString(")\n\n")
	}
	fmt.Fprintf(&body, "const (\n\tSchemaDigest = %q\n\tMaxWireJSONBytes = %d\n\tMaxWireJSONDepth = %d\n)\n\n", document.digest, document.MaxJSONBytes, document.MaxJSONDepth)
	for _, name := range sortedKeys(document.Definitions) {
		definition := document.Definitions[name]
		if definition.Description != "" {
			fmt.Fprintf(&body, "// %s %s\n", name, strings.TrimSpace(definition.Description))
		}
		if err := writeGoDefinition(&body, set, document, aliases, name, definition); err != nil {
			return nil, fmt.Errorf("generate Go %s.%s: %w", document.Package, name, err)
		}
		body.WriteString("\n")
	}
	if err := writeGoBlobCollectors(&body, set, document, aliases); err != nil {
		return nil, err
	}
	formatted, err := format.Source([]byte(body.String()))
	if err != nil {
		return nil, fmt.Errorf("format generated Go for %s: %w\n%s", document.Package, err, body.String())
	}
	return formatted, nil
}

func writeGoBlobCollectors(body *strings.Builder, set schemaSet, document *schemaDocument, aliases map[string]string) error {
	commonAlias := aliases["https://schemas.layerdraw.dev/protocol-common/v1"]
	if commonAlias == "" || document.Definitions["CompileInput"] == nil || document.Definitions["CompileResult"] == nil {
		return nil
	}
	for _, name := range []string{"CompileInput", "CompileResult"} {
		fmt.Fprintf(body, "// Collect%sBlobRefs validates %s and returns detached BlobRefs in canonical wire-property traversal order.\n", name, name)
		fmt.Fprintf(body, "func Collect%sBlobRefs(value %s) ([]%s.BlobRef, error) {\n", name, name, commonAlias)
		fmt.Fprintf(body, "\tif _, err := Encode%s(value); err != nil { return nil, err }\n", name)
		fmt.Fprintf(body, "\trefs := make([]%s.BlobRef, 0)\n", commonAlias)
		counter := 0
		if err := writeGoBlobCollectorStatements(body, set, document, document.Definitions[name], "value", "\t", commonAlias, &counter); err != nil {
			return err
		}
		body.WriteString("\treturn refs, nil\n}\n\n")
	}
	return nil
}

func writeGoBlobCollectorStatements(body *strings.Builder, set schemaSet, document *schemaDocument, value *schemaType, expression, indent, commonAlias string, counter *int) error {
	resolvedDocument, resolved, err := dereferenceSchemaType(set, document, value)
	if err != nil {
		return err
	}
	if isBlobRefSchema(resolved) {
		fmt.Fprintf(body, "%srefs = append(refs, %s.BlobRef{BlobID: string(%s.BlobID), Digest: %s.Digest(%s.Digest), Lifetime: %s.BlobLifetime(%s.Lifetime), MediaType: string(%s.MediaType), Size: %s.CanonicalUint64(%s.Size)})\n", indent, commonAlias, expression, commonAlias, expression, commonAlias, expression, expression, commonAlias, expression)
		return nil
	}
	typeValue, err := scalarType(resolved.Type)
	if err != nil {
		return err
	}
	switch typeValue {
	case "array":
		if !schemaContainsBlobRefs(set, resolvedDocument, resolved.Items, map[*schemaType]bool{}, map[*schemaType]bool{}) {
			return nil
		}
		item := fmt.Sprintf("blobItem%d", *counter)
		*counter++
		fmt.Fprintf(body, "%sfor _, %s := range %s {\n", indent, item, expression)
		if err := writeGoBlobCollectorStatements(body, set, resolvedDocument, resolved.Items, item, indent+"\t", commonAlias, counter); err != nil {
			return err
		}
		fmt.Fprintf(body, "%s}\n", indent)
	case "object":
		required := stringSet(resolved.Required)
		for _, propertyName := range sortedKeys(resolved.Properties) {
			property := resolved.Properties[propertyName]
			if !schemaContainsBlobRefs(set, resolvedDocument, property, map[*schemaType]bool{}, map[*schemaType]bool{}) {
				continue
			}
			access := expression + "." + exportedName(propertyName)
			if required[propertyName] {
				if err := writeGoBlobCollectorStatements(body, set, resolvedDocument, property, access, indent, commonAlias, counter); err != nil {
					return err
				}
				continue
			}
			fmt.Fprintf(body, "%sif %s != nil {\n", indent, access)
			if err := writeGoBlobCollectorStatements(body, set, resolvedDocument, property, "(*"+access+")", indent+"\t", commonAlias, counter); err != nil {
				return err
			}
			fmt.Fprintf(body, "%s}\n", indent)
		}
	}
	return nil
}

func dereferenceSchemaType(set schemaSet, document *schemaDocument, value *schemaType) (*schemaDocument, *schemaType, error) {
	for value != nil && value.Ref != "" {
		target, name, err := resolveRef(set, document, value.Ref)
		if err != nil {
			return nil, nil, err
		}
		document, value = target, target.Definitions[name]
	}
	return document, value, nil
}

func isBlobRefSchema(value *schemaType) bool {
	if value == nil || len(value.Properties) != 5 {
		return false
	}
	required := stringSet(value.Required)
	for _, property := range []string{"blob_id", "digest", "lifetime", "media_type", "size"} {
		if value.Properties[property] == nil || !required[property] {
			return false
		}
	}
	return true
}

func schemaContainsBlobRefs(set schemaSet, document *schemaDocument, value *schemaType, visiting, memo map[*schemaType]bool) bool {
	resolvedDocument, resolved, err := dereferenceSchemaType(set, document, value)
	if err != nil || resolved == nil {
		return false
	}
	if result, ok := memo[resolved]; ok {
		return result
	}
	if visiting[resolved] {
		return false
	}
	if isBlobRefSchema(resolved) {
		memo[resolved] = true
		return true
	}
	visiting[resolved] = true
	defer delete(visiting, resolved)
	found := false
	typeValue, _ := scalarType(resolved.Type)
	switch typeValue {
	case "array":
		found = schemaContainsBlobRefs(set, resolvedDocument, resolved.Items, visiting, memo)
	case "object":
		for _, property := range resolved.Properties {
			if schemaContainsBlobRefs(set, resolvedDocument, property, visiting, memo) {
				found = true
				break
			}
		}
	}
	if !found {
		for _, branch := range resolved.OneOf {
			if schemaContainsBlobRefs(set, resolvedDocument, branch, visiting, memo) {
				found = true
				break
			}
		}
	}
	memo[resolved] = found
	return found
}

func writeGoDefinition(body *strings.Builder, set schemaSet, document *schemaDocument, aliases map[string]string, name string, definition *schemaType) error {
	if name == "JsonValue" && len(definition.OneOf) != 0 {
		body.WriteString("type JsonValueKind string\n\n")
		body.WriteString("const (\n\tJsonValueKindNull JsonValueKind = \"null\"\n\tJsonValueKindBoolean JsonValueKind = \"boolean\"\n\tJsonValueKindString JsonValueKind = \"string\"\n\tJsonValueKindArray JsonValueKind = \"array\"\n\tJsonValueKindObject JsonValueKind = \"object\"\n)\n\n")
		body.WriteString("type JsonValue struct {\n\tKind JsonValueKind\n\tBoolean bool\n\tString string\n\tArray []JsonValue\n\tObject map[string]JsonValue\n}\n")
		return nil
	}
	typeValue, err := scalarType(definition.Type)
	if err != nil {
		return err
	}
	if typeValue == "object" && len(definition.Properties) != 0 {
		for _, propertyName := range sortedKeys(definition.Properties) {
			property := definition.Properties[propertyName]
			if constant, ok := property.Const.(string); ok {
				constantType := name + exportedName(propertyName)
				fmt.Fprintf(body, "type %s string\n\nconst %sValue %s = %q\n\n", constantType, constantType, constantType, constant)
			}
		}
		fmt.Fprintf(body, "type %s struct {\n", name)
		required := stringSet(definition.Required)
		for _, propertyName := range sortedKeys(definition.Properties) {
			property := definition.Properties[propertyName]
			expression := ""
			if _, ok := property.Const.(string); ok {
				expression = name + exportedName(propertyName)
			} else {
				var err error
				expression, err = goType(set, document, aliases, property)
				if err != nil {
					return err
				}
			}
			if !required[propertyName] && !strings.HasPrefix(expression, "*") {
				expression = "*" + expression
			}
			tag := propertyName
			if !required[propertyName] {
				tag += ",omitempty"
			}
			fmt.Fprintf(body, "\t%s %s `json:%q`\n", exportedName(propertyName), expression, tag)
		}
		body.WriteString("}\n")
		return nil
	}
	expression, err := goType(set, document, aliases, definition)
	if err != nil {
		return err
	}
	if typeValue == "string" && len(definition.Enum) != 0 {
		fmt.Fprintf(body, "type %s string\n\nconst (\n", name)
		for _, enumValue := range definition.Enum {
			fmt.Fprintf(body, "\t%s%s %s = %q\n", name, exportedName(enumValue), name, enumValue)
		}
		body.WriteString(")\n")
		return nil
	}
	fmt.Fprintf(body, "type %s %s\n", name, expression)
	return nil
}

func goType(set schemaSet, document *schemaDocument, aliases map[string]string, value *schemaType) (string, error) {
	if value.Ref != "" {
		target, name, err := resolveRef(set, document, value.Ref)
		if err != nil {
			return "", err
		}
		if target.ID == document.ID {
			return name, nil
		}
		return aliases[target.ID] + "." + name, nil
	}
	if len(value.OneOf) != 0 {
		return "any", nil
	}
	typeValue, err := scalarType(value.Type)
	if err != nil {
		return "", err
	}
	switch typeValue {
	case "string":
		return "string", nil
	case "integer":
		return "int64", nil
	case "number":
		return "float64", nil
	case "boolean":
		return "bool", nil
	case "null", "union":
		return "any", nil
	case "array":
		item, err := goType(set, document, aliases, value.Items)
		return "[]" + item, err
	case "object":
		if len(value.Properties) != 0 {
			return "struct{}", nil
		}
		if additional, ok := value.AdditionalProperties.(*schemaType); ok {
			nested, err := goType(set, document, aliases, additional)
			return "map[string]" + nested, err
		}
		return "map[string]any", nil
	default:
		return "", fmt.Errorf("unsupported schema type %q", typeValue)
	}
}

func goImportAliases(set schemaSet, document *schemaDocument) map[string]string {
	used := map[string]bool{}
	var visit func(*schemaType)
	visit = func(value *schemaType) {
		if value == nil {
			return
		}
		if value.Ref != "" {
			target, _, err := resolveRef(set, document, value.Ref)
			if err == nil && target.ID != document.ID {
				used[target.ID] = true
			}
		}
		for _, property := range value.Properties {
			visit(property)
		}
		visit(value.Items)
		visit(value.PropertyNames)
		if nested, ok := value.AdditionalProperties.(*schemaType); ok {
			visit(nested)
		}
		for _, branch := range value.OneOf {
			visit(branch)
		}
	}
	for _, definition := range document.Definitions {
		visit(definition)
	}
	aliases := map[string]string{}
	for id := range used {
		aliases[id] = set.byID[id].Package
	}
	return aliases
}

func generateGoCodec(set schemaSet, document *schemaDocument) ([]byte, error) {
	var body strings.Builder
	fmt.Fprintf(&body, "// Code generated by %s; DO NOT EDIT.\n", generatorVersion)
	fmt.Fprintf(&body, "// Schema digest: %s\n", document.digest)
	body.WriteString("// SPDX-License-Identifier: Apache-2.0\n\n")
	fmt.Fprintf(&body, "package %s\n\n", document.Package)
	body.WriteString(`import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
		"time"
		"unicode/utf16"
		"unicode/utf8"
	)

`)
	body.WriteString("var schemaDocuments = func() map[string]map[string]any {\n\tdocuments := map[string]map[string]any{}\n")
	for _, closureDocument := range schemaClosure(set, document) {
		fmt.Fprintf(&body, "\t{\n\t\tvar document map[string]any\n\t\tif err := json.Unmarshal([]byte(%q), &document); err != nil { panic(err) }\n\t\tdocuments[%q] = document\n\t}\n", string(closureDocument.raw), closureDocument.ID)
	}
	body.WriteString("\treturn documents\n}()\n\n")
	fmt.Fprintf(&body, "const schemaDocumentID = %q\n\n", document.ID)
	if document.Definitions["JsonValue"] != nil {
		body.WriteString(goJSONValueRuntime)
	}
	for _, name := range sortedKeys(document.Definitions) {
		fmt.Fprintf(&body, "// Decode%s decodes and validates one %s JSON value.\n", name, name)
		fmt.Fprintf(&body, "func Decode%s(data []byte) (%s, error) {\n\tvar result %s\n\traw, err := decodeWireJSON(data)\n\tif err != nil { return result, err }\n\tif err := validateNamed(schemaDocumentID, %q, raw); err != nil { return result, err }\n\tdecoder := json.NewDecoder(bytes.NewReader(data))\n\tdecoder.DisallowUnknownFields()\n\tif err := decoder.Decode(&result); err != nil { return result, err }\n\treturn result, nil\n}\n\n", name, name, name, name)
		fmt.Fprintf(&body, "// Encode%s validates and emits canonical UTF-8 JSON.\n", name)
		fmt.Fprintf(&body, "func Encode%s(value %s) ([]byte, error) {\n\tif err := validateGoWireValue(reflect.ValueOf(value), map[visit]bool{}, 0); err != nil { return nil, err }\n\tencoded, err := marshalWireJSON(value)\n\tif err != nil { return nil, err }\n\traw, err := decodeWireJSON(encoded)\n\tif err != nil { return nil, err }\n\tif err := validateNamed(schemaDocumentID, %q, raw); err != nil { return nil, err }\n\tcanonical, err := appendCanonicalJSON(nil, raw)\n\tif err != nil { return nil, err }\n\tif err := validateWireJSONBytes(canonical); err != nil { return nil, err }\n\treturn canonical, nil\n}\n\n", name, name, name)
	}
	body.WriteString(goCodecRuntime)
	formatted, err := format.Source([]byte(body.String()))
	if err != nil {
		return nil, fmt.Errorf("format generated Go codec for %s: %w\n%s", document.Package, err, body.String())
	}
	return formatted, nil
}

const goJSONValueRuntime = `// UnmarshalJSON decodes the closed recursive canonical JSON value union.
func (value *JsonValue) UnmarshalJSON(data []byte) error {
	raw, err := decodeWireJSON(data)
	if err != nil { return err }
	if err := validateNamed(schemaDocumentID, "JsonValue", raw); err != nil { return err }
	decoded, err := jsonValueFromRaw(raw)
	if err != nil { return err }
	*value = decoded
	return nil
}

// MarshalJSON encodes the closed recursive canonical JSON value union.
func (value JsonValue) MarshalJSON() ([]byte, error) {
	raw, err := jsonValueToRaw(value)
	if err != nil { return nil, err }
	if err := validateNamed(schemaDocumentID, "JsonValue", raw); err != nil { return nil, err }
	encoded, err := appendCanonicalJSON(nil, raw)
	if err != nil { return nil, err }
	if err := validateWireJSONBytes(encoded); err != nil { return nil, err }
	return encoded, nil
}

func jsonValueFromRaw(raw any) (JsonValue, error) {
	switch typed := raw.(type) {
	case nil:
		return JsonValue{Kind: JsonValueKindNull}, nil
	case bool:
		return JsonValue{Kind: JsonValueKindBoolean, Boolean: typed}, nil
	case string:
		return JsonValue{Kind: JsonValueKindString, String: typed}, nil
	case []any:
		items := make([]JsonValue, len(typed))
		for index, item := range typed {
			decoded, err := jsonValueFromRaw(item)
			if err != nil { return JsonValue{}, err }
			items[index] = decoded
		}
		return JsonValue{Kind: JsonValueKindArray, Array: items}, nil
	case map[string]any:
		items := make(map[string]JsonValue, len(typed))
		for key, item := range typed {
			decoded, err := jsonValueFromRaw(item)
			if err != nil { return JsonValue{}, err }
			items[key] = decoded
		}
		return JsonValue{Kind: JsonValueKindObject, Object: items}, nil
	default:
		return JsonValue{}, fmt.Errorf("unsupported JsonValue member %T", raw)
	}
}

func jsonValueToRaw(value JsonValue) (any, error) {
	return jsonValueToRawState(value, map[visit]bool{}, 0)
}

func jsonValueToRawState(value JsonValue, active map[visit]bool, depth int) (any, error) {
	if err := validateJSONValueInactiveFields(value); err != nil { return nil, err }
	switch value.Kind {
	case JsonValueKindNull:
		return nil, nil
	case JsonValueKindBoolean:
		return value.Boolean, nil
	case JsonValueKindString:
		if !utf8.ValidString(value.String) { return nil, errors.New("JsonValue contains malformed Unicode") }
		return value.String, nil
	case JsonValueKindArray:
		if depth >= MaxWireJSONDepth { return nil, fmt.Errorf("JsonValue exceeds depth %d", MaxWireJSONDepth) }
		if value.Array != nil {
			key := visit{pointer: reflect.ValueOf(value.Array).Pointer(), kind: reflect.Slice}
			if active[key] { return nil, errors.New("JsonValue contains an array cycle") }
			active[key] = true
			defer delete(active, key)
		}
		items := make([]any, len(value.Array))
		for index, item := range value.Array {
			encoded, err := jsonValueToRawState(item, active, depth+1)
			if err != nil { return nil, err }
			items[index] = encoded
		}
		return items, nil
	case JsonValueKindObject:
		if depth >= MaxWireJSONDepth { return nil, fmt.Errorf("JsonValue exceeds depth %d", MaxWireJSONDepth) }
		if value.Object != nil {
			key := visit{pointer: reflect.ValueOf(value.Object).Pointer(), kind: reflect.Map}
			if active[key] { return nil, errors.New("JsonValue contains an object cycle") }
			active[key] = true
			defer delete(active, key)
		}
		items := make(map[string]any, len(value.Object))
		for key, item := range value.Object {
			if !utf8.ValidString(key) { return nil, errors.New("JsonValue object key contains malformed Unicode") }
			encoded, err := jsonValueToRawState(item, active, depth+1)
			if err != nil { return nil, err }
			items[key] = encoded
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unknown JsonValue kind %q", value.Kind)
	}
}

func validateJSONValueInactiveFields(value JsonValue) error {
	invalidBoolean := value.Boolean
	invalidString := value.String != ""
	invalidArray := value.Array != nil
	invalidObject := value.Object != nil
	switch value.Kind {
	case JsonValueKindNull:
		if invalidBoolean || invalidString || invalidArray || invalidObject { return errors.New("JsonValue null kind has an active variant field") }
	case JsonValueKindBoolean:
		if invalidString || invalidArray || invalidObject { return errors.New("JsonValue boolean kind has an inactive variant field") }
	case JsonValueKindString:
		if invalidBoolean || invalidArray || invalidObject { return errors.New("JsonValue string kind has an inactive variant field") }
	case JsonValueKindArray:
		if invalidBoolean || invalidString || invalidObject { return errors.New("JsonValue array kind has an inactive variant field") }
	case JsonValueKindObject:
		if invalidBoolean || invalidString || invalidArray { return errors.New("JsonValue object kind has an inactive variant field") }
	default:
		return fmt.Errorf("unknown JsonValue kind %q", value.Kind)
	}
	return nil
}

`

const goCodecRuntime = `type visit struct { pointer uintptr; kind reflect.Kind }

func marshalWireJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil { return nil, err }
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

func validateGoWireValue(value reflect.Value, active map[visit]bool, depth int) error {
	if !value.IsValid() { return nil }
	if value.Kind() == reflect.Interface {
		if value.IsNil() { return nil }
		return validateGoWireValue(value.Elem(), active, depth)
	}
	if value.Kind() == reflect.Pointer && value.IsNil() { return nil }
	if value.CanInterface() {
		if marshaler, ok := value.Interface().(json.Marshaler); ok {
			encoded, err := marshaler.MarshalJSON()
			if err != nil { return err }
			raw, err := decodeWireJSON(encoded)
			if err != nil { return err }
			return validateGoWireValue(reflect.ValueOf(raw), active, depth)
		}
	}
	switch value.Kind() {
	case reflect.Pointer:
		key := visit{pointer: value.Pointer(), kind: value.Kind()}
		if active[key] { return errors.New("protocol value contains a cycle") }
		active[key] = true
		defer delete(active, key)
		return validateGoWireValue(value.Elem(), active, depth)
	case reflect.String:
		if !utf8.ValidString(value.String()) { return errors.New("protocol value contains malformed Unicode") }
	case reflect.Struct:
		if depth >= MaxWireJSONDepth { return fmt.Errorf("protocol value exceeds depth %d", MaxWireJSONDepth) }
		for index := 0; index < value.NumField(); index++ {
			if value.Type().Field(index).PkgPath != "" { continue }
			if err := validateGoWireValue(value.Field(index), active, depth+1); err != nil { return err }
		}
	case reflect.Slice:
		if value.IsNil() { return nil }
		if depth >= MaxWireJSONDepth { return fmt.Errorf("protocol value exceeds depth %d", MaxWireJSONDepth) }
		key := visit{pointer: value.Pointer(), kind: value.Kind()}
		if active[key] { return errors.New("protocol value contains a cycle") }
		active[key] = true
		defer delete(active, key)
		for index := 0; index < value.Len(); index++ {
			if err := validateGoWireValue(value.Index(index), active, depth+1); err != nil { return err }
		}
	case reflect.Array:
		if depth >= MaxWireJSONDepth { return fmt.Errorf("protocol value exceeds depth %d", MaxWireJSONDepth) }
		for index := 0; index < value.Len(); index++ {
			if err := validateGoWireValue(value.Index(index), active, depth+1); err != nil { return err }
		}
	case reflect.Map:
		if value.IsNil() { return nil }
		if depth >= MaxWireJSONDepth { return fmt.Errorf("protocol value exceeds depth %d", MaxWireJSONDepth) }
		key := visit{pointer: value.Pointer(), kind: value.Kind()}
		if active[key] { return errors.New("protocol value contains a cycle") }
		active[key] = true
		defer delete(active, key)
		iterator := value.MapRange()
		for iterator.Next() {
			if err := validateGoWireValue(iterator.Key(), active, depth+1); err != nil { return err }
			if err := validateGoWireValue(iterator.Value(), active, depth+1); err != nil { return err }
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		integer := value.Int()
		if integer < -9007199254740991 || integer > 9007199254740991 { return errors.New("protocol numbers must be canonical safe integers") }
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if value.Uint() > 9007199254740991 { return errors.New("protocol numbers must be canonical safe integers") }
	case reflect.Float32, reflect.Float64:
		number := value.Float()
		if math.IsInf(number, 0) || math.IsNaN(number) || math.Trunc(number) != number || math.Signbit(number) && number == 0 || number < -9007199254740991 || number > 9007199254740991 { return errors.New("protocol numbers must be canonical safe integers") }
	}
	return nil
}

func decodeWireJSON(data []byte) (any, error) {
	if err := validateWireJSONBytes(data); err != nil { return nil, err }
	if err := rejectDuplicateJSONKeys(data); err != nil { return nil, err }
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil { return nil, err }
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) { return nil, errors.New("protocol JSON must contain exactly one value") }
	return value, nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanUniqueJSONValue(decoder); err != nil { return err }
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil { return errors.New("protocol JSON must contain exactly one value") }
		return err
	}
	return nil
}

func scanUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil { return err }
	delimiter, ok := token.(json.Delim)
	if !ok { return nil }
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			rawKey, err := decoder.Token()
			if err != nil { return err }
			key, ok := rawKey.(string)
			if !ok { return errors.New("protocol JSON object key must be a string") }
			if seen[key] { return fmt.Errorf("protocol JSON contains duplicate object key %q", key) }
			seen[key] = true
			if err := scanUniqueJSONValue(decoder); err != nil { return err }
		}
		closing, err := decoder.Token()
		if err != nil { return err }
		if closing != json.Delim('}') { return errors.New("protocol JSON object is not closed") }
	case '[':
		for decoder.More() {
			if err := scanUniqueJSONValue(decoder); err != nil { return err }
		}
		closing, err := decoder.Token()
		if err != nil { return err }
		if closing != json.Delim(']') { return errors.New("protocol JSON array is not closed") }
	default:
		return errors.New("protocol JSON has an unexpected delimiter")
	}
	return nil
}

func validateWireJSONBytes(data []byte) error {
	if len(data) > MaxWireJSONBytes { return fmt.Errorf("protocol JSON exceeds %d UTF-8 bytes", MaxWireJSONBytes) }
	if !utf8.Valid(data) { return errors.New("protocol JSON contains malformed UTF-8") }
	depth := 0
	for index := 0; index < len(data); index++ {
		switch data[index] {
		case '"':
			end, err := scanJSONString(data, index)
			if err != nil { return err }
			index = end
		case '{', '[':
			depth++
			if depth > MaxWireJSONDepth { return fmt.Errorf("protocol JSON exceeds depth %d", MaxWireJSONDepth) }
		case '}', ']':
			depth--
		case '-':
			end := scanJSONToken(data, index)
			if err := validateCanonicalJSONNumber(data[index:end]); err != nil { return err }
			index = end-1
		default:
			if data[index] >= '0' && data[index] <= '9' {
				end := scanJSONToken(data, index)
				if err := validateCanonicalJSONNumber(data[index:end]); err != nil { return err }
				index = end-1
			}
		}
	}
	return nil
}

func scanJSONToken(data []byte, start int) int {
	index := start
	for index < len(data) && !strings.ContainsRune("{}[],: \t\r\n", rune(data[index])) { index++ }
	return index
}

func validateCanonicalJSONNumber(token []byte) error {
	text := string(token)
	if !regexp.MustCompile(` + "`" + `^(0|-[1-9][0-9]*|[1-9][0-9]*)$` + "`" + `).MatchString(text) {
		return fmt.Errorf("protocol JSON number %q is not a canonical integer", text)
	}
	integer, err := strconv.ParseInt(text, 10, 64)
	if err != nil || integer < -9007199254740991 || integer > 9007199254740991 {
		return fmt.Errorf("protocol JSON number %q is outside the portable safe range", text)
	}
	return nil
}

func isCanonicalSourcePath(value string) bool {
	if value == "" || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\\x00") { return false }
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." { return false }
	}
	return true
}

func isCanonicalBinary64(text string, positive bool) bool {
	if !regexp.MustCompile(` + "`" + `^-?(0|[1-9][0-9]*)(\.[0-9]+)?(e[+-][1-9][0-9]*)?$` + "`" + `).MatchString(text) { return false }
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) || math.Signbit(value) && value == 0 || positive && value <= 0 { return false }
	return canonicalBinary64(value) == text
}

func canonicalBinary64(value float64) string {
	if value == 0 { return "0" }
	negative := math.Signbit(value)
	if negative { value = -value }
	scientific := strconv.FormatFloat(value, 'e', -1, 64)
	parts := strings.SplitN(scientific, "e", 2)
	digits := strings.ReplaceAll(parts[0], ".", "")
	exponent, _ := strconv.Atoi(parts[1])
	decimalPosition := exponent + 1
	var result string
	switch {
	case decimalPosition > 0 && decimalPosition <= 21:
		if decimalPosition >= len(digits) { result = digits + strings.Repeat("0", decimalPosition-len(digits)) } else { result = digits[:decimalPosition] + "." + digits[decimalPosition:] }
	case decimalPosition <= 0 && decimalPosition > -6:
		result = "0." + strings.Repeat("0", -decimalPosition) + digits
	default:
		result = digits[:1]
		if len(digits) > 1 { result += "." + digits[1:] }
		if exponent >= 0 { result += "e+" + strconv.Itoa(exponent) } else { result += "e" + strconv.Itoa(exponent) }
	}
	if negative { return "-" + result }
	return result
}

type protocolVersionValue struct { major, minor uint32 }

func parseProtocolVersion(text string) (protocolVersionValue, string, bool) {
	if !regexp.MustCompile(` + "`" + `^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$` + "`" + `).MatchString(text) { return protocolVersionValue{}, "", false }
	parts := strings.Split(text, ".")
	major, majorErr := strconv.ParseUint(parts[0], 10, 32)
	minor, minorErr := strconv.ParseUint(parts[1], 10, 32)
	if majorErr != nil || minorErr != nil { return protocolVersionValue{}, "", false }
	return protocolVersionValue{major: uint32(major), minor: uint32(minor)}, text, true
}

func parseProtocolVersionRange(text string) (protocolVersionValue, protocolVersionValue, bool) {
	parts := strings.Split(text, "..")
	if len(parts) != 2 { return protocolVersionValue{}, protocolVersionValue{}, false }
	lower, _, lowerOK := parseProtocolVersion(parts[0])
	upper, _, upperOK := parseProtocolVersion(parts[1])
	if !lowerOK || !upperOK || lower.major != upper.major || compareProtocolVersions(lower, upper) > 0 { return protocolVersionValue{}, protocolVersionValue{}, false }
	return lower, upper, true
}

func compareProtocolVersions(left, right protocolVersionValue) int {
	if left.major < right.major || left.major == right.major && left.minor < right.minor { return -1 }
	if left != right { return 1 }
	return 0
}

func scanJSONString(data []byte, start int) (int, error) {
	for index := start+1; index < len(data); index++ {
		if data[index] == '"' { return index, nil }
		if data[index] < 0x20 { return 0, errors.New("protocol JSON string contains an unescaped control character") }
		if data[index] != '\\' { continue }
		index++
		if index >= len(data) { return 0, errors.New("protocol JSON string has a truncated escape") }
		if data[index] != 'u' { continue }
		code, err := parseHexCodeUnit(data, index+1)
		if err != nil { return 0, err }
		index += 4
		if code >= 0xdc00 && code <= 0xdfff { return 0, errors.New("protocol JSON string has an unpaired low surrogate") }
		if code < 0xd800 || code > 0xdbff { continue }
		if index+6 >= len(data) || data[index+1] != '\\' || data[index+2] != 'u' { return 0, errors.New("protocol JSON string has an unpaired high surrogate") }
		low, err := parseHexCodeUnit(data, index+3)
		if err != nil || low < 0xdc00 || low > 0xdfff { return 0, errors.New("protocol JSON string has an invalid surrogate pair") }
		index += 6
	}
	return 0, errors.New("protocol JSON string is unterminated")
}

func parseHexCodeUnit(data []byte, start int) (uint16, error) {
	if start+4 > len(data) { return 0, errors.New("protocol JSON string has a truncated Unicode escape") }
	value, err := strconv.ParseUint(string(data[start:start+4]), 16, 16)
	if err != nil { return 0, errors.New("protocol JSON string has an invalid Unicode escape") }
	return uint16(value), nil
}

func validateNamed(documentID, name string, value any) error {
	document := schemaDocuments[documentID]
	definitions, ok := document["$defs"].(map[string]any)
	if !ok { return errors.New("generated schema has no definitions") }
	schema, ok := definitions[name].(map[string]any)
	if !ok { return fmt.Errorf("generated schema has no definition %s", name) }
	return validateSchema(documentID, schema, value, "$", 0)
}

func validateSchema(documentID string, schema map[string]any, value any, path string, depth int) error {
	if ref, ok := schema["$ref"].(string); ok {
		parts := strings.SplitN(ref, "#/$defs/", 2)
		if len(parts) != 2 { return fmt.Errorf("%s has invalid generated reference", path) }
		if parts[0] != "" { documentID = parts[0] }
		document := schemaDocuments[documentID]
		definitions, _ := document["$defs"].(map[string]any)
		target, _ := definitions[parts[1]].(map[string]any)
		if target == nil { return fmt.Errorf("%s has unresolved generated reference", path) }
		return validateSchema(documentID, target, value, path, depth+1)
	}
	if branches, ok := schema["oneOf"].([]any); ok {
		matches := 0
		for _, rawBranch := range branches {
			branch, _ := rawBranch.(map[string]any)
			if branch != nil && validateSchema(documentID, branch, value, path, depth+1) == nil { matches++ }
		}
		if matches != 1 { return fmt.Errorf("%s must match exactly one schema alternative", path) }
		return nil
	}
	typeName, _ := schema["type"].(string)
	switch typeName {
	case "null":
		if value != nil { return fmt.Errorf("%s must be null", path) }
	case "boolean":
		if _, ok := value.(bool); !ok { return fmt.Errorf("%s must be a boolean", path) }
	case "string":
		text, ok := value.(string)
		if !ok { return fmt.Errorf("%s must be a string", path) }
		if constant, ok := schema["const"].(string); ok && text != constant { return fmt.Errorf("%s must equal %q", path, constant) }
		if values, ok := schema["enum"].([]any); ok {
			matched := false
			for _, candidate := range values { if text == candidate { matched = true; break } }
			if !matched { return fmt.Errorf("%s has unknown enum value %q", path, text) }
		}
		if pattern, ok := schema["pattern"].(string); ok && !regexp.MustCompile(strings.ReplaceAll(pattern, "(?:", "(")).MatchString(text) { return fmt.Errorf("%s has invalid string form", path) }
		if minimum, ok := schema["minLength"].(float64); ok && utf8.RuneCountInString(text) < int(minimum) { return fmt.Errorf("%s is too short", path) }
		if format, _ := schema["format"].(string); format != "" {
			switch format {
			case "canonical-source-path":
				if !isCanonicalSourcePath(text) { return fmt.Errorf("%s is not a canonical source path", path) }
			case "int64-decimal":
				if !regexp.MustCompile(` + "`" + `^(0|-[1-9][0-9]*|[1-9][0-9]*)$` + "`" + `).MatchString(text) { return fmt.Errorf("%s is not a canonical int64", path) }
				if _, err := strconv.ParseInt(text, 10, 64); err != nil { return fmt.Errorf("%s is outside int64", path) }
			case "nonnegative-int64-decimal":
				if !regexp.MustCompile(` + "`" + `^(0|[1-9][0-9]*)$` + "`" + `).MatchString(text) { return fmt.Errorf("%s is not a canonical non-negative int64", path) }
				if _, err := strconv.ParseInt(text, 10, 64); err != nil { return fmt.Errorf("%s is outside non-negative int64", path) }
			case "uint64-decimal":
				if !regexp.MustCompile(` + "`" + `^(0|[1-9][0-9]*)$` + "`" + `).MatchString(text) { return fmt.Errorf("%s is not a canonical uint64", path) }
				if _, err := strconv.ParseUint(text, 10, 64); err != nil { return fmt.Errorf("%s is outside uint64", path) }
			case "positive-int64-decimal":
				integer, err := strconv.ParseInt(text, 10, 64)
				if err != nil || integer <= 0 { return fmt.Errorf("%s is not a positive canonical int64", path) }
			case "safe-integer-decimal":
				integer, err := strconv.ParseInt(text, 10, 64)
				if err != nil || integer < -9007199254740991 || integer > 9007199254740991 { return fmt.Errorf("%s is outside the Language 1 safe-integer range", path) }
			case "nonnegative-safe-integer-decimal":
				integer, err := strconv.ParseInt(text, 10, 64)
				if err != nil || integer < 0 || integer > 9007199254740991 { return fmt.Errorf("%s is outside the non-negative Language 1 safe-integer range", path) }
			case "positive-safe-integer-decimal":
				integer, err := strconv.ParseInt(text, 10, 64)
				if err != nil || integer <= 0 || integer > 9007199254740991 { return fmt.Errorf("%s is outside the positive Language 1 safe-integer range", path) }
			case "finite-binary64-decimal":
				if !isCanonicalBinary64(text, false) { return fmt.Errorf("%s is not a canonical finite binary64", path) }
			case "positive-finite-binary64-decimal":
				if !isCanonicalBinary64(text, true) { return fmt.Errorf("%s is not a positive canonical finite binary64", path) }
			case "protocol-version":
				if _, _, ok := parseProtocolVersion(text); !ok { return fmt.Errorf("%s is not a canonical protocol version", path) }
			case "protocol-version-range":
				if _, _, ok := parseProtocolVersionRange(text); !ok { return fmt.Errorf("%s is not a canonical ordered protocol range", path) }
			case "protocol-version-or-range":
				if _, _, ok := parseProtocolVersion(text); !ok { if _, _, rangeOK := parseProtocolVersionRange(text); !rangeOK { return fmt.Errorf("%s is not a canonical protocol version or range", path) } }
			case "date-time":
				if !regexp.MustCompile(` + "`" + `^[0-9]{4}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]{1,9})?Z$` + "`" + `).MatchString(text) { return fmt.Errorf("%s is not canonical UTC RFC 3339", path) }
				if _, err := time.Parse(time.RFC3339Nano, text); err != nil { return fmt.Errorf("%s is not a real UTC RFC 3339 calendar instant", path) }
			}
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok || !regexp.MustCompile(` + "`" + `^(0|-[1-9][0-9]*|[1-9][0-9]*)$` + "`" + `).MatchString(number.String()) { return fmt.Errorf("%s must be a canonical integer", path) }
		integer, err := strconv.ParseInt(number.String(), 10, 64)
		if err != nil || integer < -9007199254740991 || integer > 9007199254740991 { return fmt.Errorf("%s integer is outside the portable safe range", path) }
		if minimum, ok := schema["minimum"].(float64); ok && integer < int64(minimum) { return fmt.Errorf("%s is below its minimum", path) }
		if maximum, ok := schema["maximum"].(float64); ok && integer > int64(maximum) { return fmt.Errorf("%s is above its maximum", path) }
	case "array":
		items, ok := value.([]any)
		if !ok { return fmt.Errorf("%s must be an array", path) }
		if minimum, ok := schema["minItems"].(float64); ok && len(items) < int(minimum) { return fmt.Errorf("%s has too few items", path) }
		itemSchema, _ := schema["items"].(map[string]any)
		seenItems := map[string]bool{}
		for index, item := range items {
			if err := validateSchema(documentID, itemSchema, item, fmt.Sprintf("%s[%d]", path, index), depth+1); err != nil { return err }
			if unique, _ := schema["uniqueItems"].(bool); unique {
				text, ok := item.(string)
				if !ok { return fmt.Errorf("%s uniqueItems requires generated string items", path) }
				if seenItems[text] { return fmt.Errorf("%s repeats item %q", path, text) }
				seenItems[text] = true
			}
		}
		if selector, _ := schema["x-layerdraw-stable-address-order"].(string); selector != "" {
			for index := 1; index < len(items); index++ {
				left, leftOK := stableAddressOrderValue(items[index-1], selector)
				right, rightOK := stableAddressOrderValue(items[index], selector)
				comparison, compared := compareStableAddresses(left, right)
				if !leftOK || !rightOK || !compared || comparison >= 0 { return fmt.Errorf("%s is not in strict StableSymbol order", path) }
			}
		}
		if ordered, _ := schema["x-layerdraw-canonical-identifier-order"].(bool); ordered {
			for index := 1; index < len(items); index++ {
				left, leftOK := items[index-1].(string)
				right, rightOK := items[index].(string)
				if !leftOK || !rightOK || !isCanonicalLocalIdentifier(left) || !isCanonicalLocalIdentifier(right) || left >= right { return fmt.Errorf("%s is not in strict canonical identifier order", path) }
			}
			if len(items) == 1 { if text, ok := items[0].(string); !ok || !isCanonicalLocalIdentifier(text) { return fmt.Errorf("%s contains a noncanonical identifier", path) } }
		}
		if ordered, _ := schema["x-layerdraw-canonical-enum-order"].(bool); ordered {
			values, _ := itemSchema["enum"].([]any)
			ranks := map[string]int{}
			for index, raw := range values { if text, ok := raw.(string); ok { ranks[text] = index } }
			for index := 1; index < len(items); index++ {
				left, leftOK := items[index-1].(string)
				right, rightOK := items[index].(string)
				leftRank, leftRankOK := ranks[left]
				rightRank, rightRankOK := ranks[right]
				if !leftOK || !rightOK || !leftRankOK || !rightRankOK || leftRank >= rightRank { return fmt.Errorf("%s is not in strict schema-enum order", path) }
			}
		}
		if ordered, _ := schema["x-layerdraw-unicode-scalar-order"].(bool); ordered {
			for index := 1; index < len(items); index++ {
				left, leftOK := items[index-1].(string)
				right, rightOK := items[index].(string)
				if !leftOK || !rightOK || compareUnicodeScalars(left, right) >= 0 { return fmt.Errorf("%s is not in strict Unicode scalar order", path) }
			}
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok { return fmt.Errorf("%s must be an object", path) }
		properties, _ := schema["properties"].(map[string]any)
		required, _ := schema["required"].([]any)
		for _, rawName := range required {
			name, _ := rawName.(string)
			if _, exists := object[name]; !exists { return fmt.Errorf("%s.%s is required", path, name) }
		}
		for name, item := range object {
			if propertyNames, ok := schema["propertyNames"].(map[string]any); ok {
				if err := validateSchema(documentID, propertyNames, name, path+" property name", depth+1); err != nil { return err }
			}
			if rawProperty, exists := properties[name]; exists {
				property, _ := rawProperty.(map[string]any)
				if err := validateSchema(documentID, property, item, path+"."+name, depth+1); err != nil { return err }
				continue
			}
			switch additional := schema["additionalProperties"].(type) {
			case bool:
				if !additional { return fmt.Errorf("%s contains unknown property %q", path, name) }
			case map[string]any:
				if err := validateSchema(documentID, additional, item, path+"."+name, depth+1); err != nil { return err }
			default:
				return fmt.Errorf("%s contains property not covered by generated schema", path)
			}
		}
		if rawUnion, ok := schema["x-layerdraw-tagged-union"].(map[string]any); ok {
			property, _ := rawUnion["property"].(string)
			tag := fmt.Sprint(object[property])
			variants, _ := rawUnion["variants"].(map[string]any)
			rawVariant, exists := variants[tag]
			if !exists { return fmt.Errorf("%s has unknown tagged union value %q", path, tag) }
			variant, _ := rawVariant.(map[string]any)
			if err := validatePresenceRule(path, object, variant); err != nil { return err }
		}
		if enabled, _ := schema["x-layerdraw-diff-source"].(bool); enabled {
			if err := validateDiffSource(path, object); err != nil { return err }
		}
		if rawRule, ok := schema["x-layerdraw-operator-value"].(map[string]any); ok {
			operatorProperty, _ := rawRule["operator"].(string)
			valueProperty, _ := rawRule["value"].(string)
			operator, operatorPresent := object[operatorProperty].(string)
			if operatorPresent {
				valueless := false
				if values, ok := rawRule["valueless"].([]any); ok { for _, candidate := range values { if operator == candidate { valueless = true } } }
				_, valuePresent := object[valueProperty]
				if valueless && valuePresent { return fmt.Errorf("%s operator %s forbids %s", path, operator, valueProperty) }
				if !valueless && !valuePresent { return fmt.Errorf("%s operator %s requires %s", path, operator, valueProperty) }
			}
		}
		if enabled, _ := schema["x-layerdraw-protocol-offer"].(bool); enabled {
			if err := validateProtocolOffer(path, object); err != nil { return err }
		}
		if enabled, _ := schema["x-layerdraw-limit-capability"].(bool); enabled {
			if err := validateLimitCapability(path, object); err != nil { return err }
		}
		if rules, ok := schema["x-layerdraw-unique-array-keys"].([]any); ok {
			for _, rawRule := range rules {
				rule, _ := rawRule.(map[string]any)
				arrayName, _ := rule["array"].(string)
				propertyName, _ := rule["property"].(string)
				items, _ := object[arrayName].([]any)
				seen := map[string]bool{}
				for _, rawItem := range items {
					item, _ := rawItem.(map[string]any)
					key, _ := item[propertyName].(string)
					if seen[key] { return fmt.Errorf("%s.%s repeats %s %q", path, arrayName, propertyName, key) }
					seen[key] = true
				}
			}
		}
		if rules, ok := schema["x-layerdraw-disjoint-arrays"].([]any); ok {
			for _, rawRule := range rules {
				rule, _ := rawRule.(map[string]any)
				leftName, _ := rule["left"].(string)
				rightName, _ := rule["right"].(string)
				left, _ := object[leftName].([]any)
				right, _ := object[rightName].([]any)
				seen := map[string]bool{}
				for _, rawItem := range left { item, _ := rawItem.(string); seen[item] = true }
				for _, rawItem := range right {
					item, _ := rawItem.(string)
					if seen[item] { return fmt.Errorf("%s.%s and %s.%s overlap at %q", path, leftName, path, rightName, item) }
				}
			}
		}
		if rules, ok := schema["x-layerdraw-disjoint-array-keys"].([]any); ok {
			if err := validateDisjointArrayKeys(path, object, rules); err != nil { return err }
		}
		if rules, ok := schema["x-layerdraw-ordered-pairs"].([]any); ok {
			if err := validateOrderedPairs(path, object, rules); err != nil { return err }
		}
		if enabled, _ := schema["x-layerdraw-outcome-envelope"].(bool); enabled {
			outcome, _ := object["outcome"].(string)
			switch outcome {
			case "success":
				if _, ok := object["payload"]; !ok { return fmt.Errorf("%s success requires payload", path) }
				if _, ok := object["failure"]; ok { return fmt.Errorf("%s success forbids failure", path) }
			case "rejected":
				if _, ok := object["payload"]; ok { return fmt.Errorf("%s rejected outcome forbids payload", path) }
				if _, ok := object["failure"]; ok { return fmt.Errorf("%s rejected outcome forbids failure", path) }
				diagnostics, _ := object["diagnostics"].([]any)
				if len(diagnostics) == 0 { return fmt.Errorf("%s rejected outcome requires diagnostics", path) }
			case "failed", "cancelled":
				if _, ok := object["payload"]; ok { return fmt.Errorf("%s %s outcome forbids payload", path, outcome) }
				if _, ok := object["failure"]; !ok { return fmt.Errorf("%s %s outcome requires failure", path, outcome) }
			}
		}
		if enabled, _ := schema["x-layerdraw-ordered-range"].(bool); enabled {
			start, startOK := object["start_byte"].(string)
			end, endOK := object["end_byte"].(string)
			startValue, startErr := strconv.ParseUint(start, 10, 64)
			endValue, endErr := strconv.ParseUint(end, 10, 64)
			if !startOK || !endOK || startErr != nil || endErr != nil || startValue > endValue { return fmt.Errorf("%s range start_byte must not exceed end_byte", path) }
		}
		if rules, ok := schema["x-layerdraw-address-owners"].([]any); ok {
			if err := validateAddressOwners(path, object, rules); err != nil { return err }
		}
		if rule, ok := schema["x-layerdraw-address-terminal-id"].(map[string]any); ok {
			addressProperty, _ := rule["address"].(string)
			idProperty, _ := rule["id"].(string)
			address, addressOK := object[addressProperty].(string)
			id, idOK := object[idProperty].(string)
			parts := strings.Split(address, ":")
			if !addressOK || !idOK || len(parts) == 0 || parts[len(parts)-1] != id { return fmt.Errorf("%s.%s must equal the terminal ID of %s.%s", path, idProperty, path, addressProperty) }
		}
		if enabled, _ := schema["x-layerdraw-export-recipe"].(bool); enabled {
			if err := validateExportRecipeConsistency(path, object); err != nil { return err }
		}
		if enabled, _ := schema["x-layerdraw-view-recipe"].(bool); enabled {
			if err := validateViewRecipeConsistency(path, object); err != nil { return err }
		}
	default:
		return fmt.Errorf("%s uses unsupported generated schema type %q", path, typeName)
	}
	return nil
}

func stableAddressOrderValue(value any, selector string) (string, bool) {
	if selector == "$item" {
		text, ok := value.(string)
		return text, ok
	}
	object, ok := value.(map[string]any)
	if !ok { return "", false }
	text, ok := object[selector].(string)
	return text, ok
}

func isCanonicalLocalIdentifier(value string) bool { return regexp.MustCompile(` + "`" + `^[a-z][a-z0-9_]*$` + "`" + `).MatchString(value) }

func compareUnicodeScalars(left, right string) int {
	leftRunes, rightRunes := []rune(left), []rune(right)
	for index := 0; index < len(leftRunes) && index < len(rightRunes); index++ {
		if leftRunes[index] < rightRunes[index] { return -1 }
		if leftRunes[index] > rightRunes[index] { return 1 }
	}
	if len(leftRunes) < len(rightRunes) { return -1 }
	if len(leftRunes) > len(rightRunes) { return 1 }
	return 0
}

func compareCanonicalUnsignedDecimals(left, right string) (int, bool) {
	canonical := regexp.MustCompile(` + "`" + `^(0|[1-9][0-9]*)$` + "`" + `)
	if !canonical.MatchString(left) || !canonical.MatchString(right) { return 0, false }
	if len(left) < len(right) { return -1, true }
	if len(left) > len(right) { return 1, true }
	if left < right { return -1, true }
	if left > right { return 1, true }
	return 0, true
}

func validateOrderedPairs(path string, object map[string]any, rules []any) error {
	for _, rawRule := range rules {
		rule, _ := rawRule.(map[string]any)
		lowerProperty, _ := rule["lower"].(string)
		upperProperty, _ := rule["upper"].(string)
		comparison, _ := rule["comparison"].(string)
		rawLower, lowerPresent := object[lowerProperty]
		rawUpper, upperPresent := object[upperProperty]
		if !lowerPresent || !upperPresent { continue }
		lower, lowerOK := rawLower.(string)
		upper, upperOK := rawUpper.(string)
		if !lowerOK || !upperOK { return fmt.Errorf("%s ordered pair requires string values", path) }
		switch comparison {
		case "unsigned_decimal":
			ordered, ok := compareCanonicalUnsignedDecimals(lower, upper)
			if !ok || ordered > 0 { return fmt.Errorf("%s.%s must not exceed %s.%s", path, lowerProperty, path, upperProperty) }
		case "finite_binary64":
			lowerValue, lowerErr := strconv.ParseFloat(lower, 64)
			upperValue, upperErr := strconv.ParseFloat(upper, 64)
			if lowerErr != nil || upperErr != nil || math.IsNaN(lowerValue) || math.IsNaN(upperValue) || math.IsInf(lowerValue, 0) || math.IsInf(upperValue, 0) || lowerValue > upperValue { return fmt.Errorf("%s.%s must not exceed %s.%s", path, lowerProperty, path, upperProperty) }
		default:
			return fmt.Errorf("%s has unsupported ordered-pair comparison", path)
		}
	}
	return nil
}

func validateDisjointArrayKeys(path string, object map[string]any, rules []any) error {
	for _, rawRule := range rules {
		rule, _ := rawRule.(map[string]any)
		arrayProperty, _ := rule["array"].(string)
		keyProperty, _ := rule["property"].(string)
		stringsProperty, _ := rule["strings"].(string)
		items, itemsOK := object[arrayProperty].([]any)
		stringsArray, stringsOK := object[stringsProperty].([]any)
		if !itemsOK || !stringsOK { return fmt.Errorf("%s disjoint array-key assertion requires arrays", path) }
		reserved := map[string]bool{}
		for _, raw := range stringsArray { text, ok := raw.(string); if !ok { return fmt.Errorf("%s.%s contains a non-string", path, stringsProperty) }; reserved[text] = true }
		for _, raw := range items {
			item, ok := raw.(map[string]any); if !ok { return fmt.Errorf("%s.%s contains a non-object", path, arrayProperty) }
			key, ok := item[keyProperty].(string); if !ok { return fmt.Errorf("%s.%s item has no string %s", path, arrayProperty, keyProperty) }
			if reserved[key] { return fmt.Errorf("%s.%s %s %q overlaps %s", path, arrayProperty, keyProperty, key, stringsProperty) }
		}
	}
	return nil
}

func validateExportRecipeConsistency(path string, object map[string]any) error {
	format, formatOK := object["format"].(string)
	options, optionsOK := object["options"].(map[string]any)
	profile, profileOK := object["exporter_profile"].(map[string]any)
	extension, extensionOK := object["extension"].(string)
	filename, filenameOK := object["filename"].(string)
	if !formatOK || !optionsOK || !profileOK || !extensionOK || !filenameOK || options["kind"] != format || profile["format"] != format { return fmt.Errorf("%s has inconsistent Export format authority", path) }
	expected, exists := map[string]string{"json":".json","yaml":".yaml","svg":".svg","png":".png","pdf":".pdf","html":".html","csv":".csv","tsv":".tsv","xlsx":".xlsx","markdown":".md","pptx":".pptx","docx":".docx","mermaid":".mmd","bpmn":".bpmn","drawio":".drawio"}[format]
	if !exists || extension != expected { return fmt.Errorf("%s extension does not match Export format", path) }
	stem := strings.TrimSuffix(filename, extension)
	if filename == "" || filename == "." || filename == ".." || strings.ContainsAny(filename, "/\\\x00") || !strings.HasSuffix(filename, extension) || stem == "" { return fmt.Errorf("%s filename is not a canonical Export basename", path) }
	return nil
}

func validateViewRecipeConsistency(path string, object map[string]any) error {
	address, addressOK := object["address"].(string)
	shape, shapeOK := object["shape"].(map[string]any)
	reservedValues, reservedOK := object["reserved_table_column_ids"].([]any)
	if !addressOK || !shapeOK || !reservedOK { return fmt.Errorf("%s has invalid View recipe authority fields", path) }
	if shape["kind"] != "table" { return nil }
	table, tableOK := shape["table"].(map[string]any)
	columns, columnsOK := table["columns"].([]any)
	if !tableOK || !columnsOK { return fmt.Errorf("%s has invalid table shape authority", path) }
	reserved := map[string]bool{}
	for _, raw := range reservedValues { value, ok := raw.(string); if !ok { return fmt.Errorf("%s has a non-string table reservation", path) }; reserved[value] = true }
	for _, raw := range columns {
		column, ok := raw.(map[string]any); if !ok { return fmt.Errorf("%s has a non-object table column", path) }
		columnAddress, addressPresent := column["address"].(string)
		id, idPresent := column["id"].(string)
		if !addressPresent || !idPresent || !hasDirectStableAddressOwner(address, columnAddress) { return fmt.Errorf("%s has a table column outside its View owner", path) }
		if reserved[id] { return fmt.Errorf("%s table column ID %q overlaps reserved_table_column_ids", path, id) }
	}
	return nil
}

func validateAddressOwners(path string, object map[string]any, rules []any) error {
	for _, rawRule := range rules {
		rule, _ := rawRule.(map[string]any)
		ownerProperty, _ := rule["owner"].(string)
		childrenProperty, _ := rule["children"].(string)
		selector, _ := rule["selector"].(string)
		rawOwner, ownerPresent := object[ownerProperty]
		if !ownerPresent { continue }
		owner, ownerOK := rawOwner.(string)
		if !ownerOK { return fmt.Errorf("%s.%s must be a StableAddress owner", path, ownerProperty) }
		rawChildren, childrenPresent := object[childrenProperty]
		if !childrenPresent { continue }
		var children []string
		switch selector {
		case "$value":
			child, ok := rawChildren.(string)
			if !ok { return fmt.Errorf("%s.%s must be a child StableAddress", path, childrenProperty) }
			children = append(children, child)
		case "$propertyNames":
			values, ok := rawChildren.(map[string]any)
			if !ok { return fmt.Errorf("%s.%s must be an address-keyed object", path, childrenProperty) }
			for child := range values { children = append(children, child) }
		default:
			values, ok := rawChildren.([]any)
			if !ok { return fmt.Errorf("%s.%s must be an array of child records", path, childrenProperty) }
			for _, rawChild := range values {
				childObject, ok := rawChild.(map[string]any)
				if !ok { return fmt.Errorf("%s.%s contains a non-object child", path, childrenProperty) }
				child, ok := childObject[selector].(string)
				if !ok { return fmt.Errorf("%s.%s child has no address selector %s", path, childrenProperty, selector) }
				children = append(children, child)
			}
		}
		for _, child := range children {
			if !hasDirectStableAddressOwner(owner, child) { return fmt.Errorf("%s child address %q is not directly owned by %q", path, child, owner) }
		}
	}
	return nil
}

func hasDirectStableAddressOwner(owner, child string) bool {
	parts := strings.Split(child, ":")
	return len(parts) >= 2 && strings.Join(parts[:len(parts)-2], ":") == owner
}

func compareStableAddresses(left, right string) (int, bool) {
	leftOrigin, leftComponents, leftPath, leftOK := stableAddressTuple(left)
	rightOrigin, rightComponents, rightPath, rightOK := stableAddressTuple(right)
	if !leftOK || !rightOK { return 0, false }
	if leftOrigin != rightOrigin { if leftOrigin < rightOrigin { return -1, true }; return 1, true }
	for index := 0; index < len(leftComponents) && index < len(rightComponents); index++ {
		if leftComponents[index] != rightComponents[index] { if leftComponents[index] < rightComponents[index] { return -1, true }; return 1, true }
	}
	if len(leftComponents) != len(rightComponents) { if len(leftComponents) < len(rightComponents) { return -1, true }; return 1, true }
	if len(leftPath) != len(rightPath) { if len(leftPath) < len(rightPath) { return -1, true }; return 1, true }
	for index := range leftPath {
		leftRank, leftRankOK := stableAddressKindRank(leftPath[index][0])
		rightRank, rightRankOK := stableAddressKindRank(rightPath[index][0])
		if !leftRankOK || !rightRankOK { return 0, false }
		if leftRank != rightRank { if leftRank < rightRank { return -1, true }; return 1, true }
		if leftPath[index][1] != rightPath[index][1] { if leftPath[index][1] < rightPath[index][1] { return -1, true }; return 1, true }
	}
	return 0, true
}

func stableAddressTuple(value string) (int, []string, [][2]string, bool) {
	parts := strings.Split(value, ":")
	if len(parts) < 3 || parts[0] != "ldl" { return 0, nil, nil, false }
	origin, pathStart := 0, 3
	components := []string{parts[2]}
	switch parts[1] {
	case "project":
	case "pack":
		if len(parts) < 4 { return 0, nil, nil, false }
		origin, pathStart, components = 1, 4, []string{parts[2], parts[3]}
	default:
		return 0, nil, nil, false
	}
	if (len(parts)-pathStart)%2 != 0 { return 0, nil, nil, false }
	path := make([][2]string, 0, (len(parts)-pathStart)/2)
	for index := pathStart; index < len(parts); index += 2 { path = append(path, [2]string{parts[index], parts[index+1]}) }
	return origin, components, path, true
}

func stableAddressKindRank(kind string) (int, bool) {
	ranks := map[string]int{"entity-type": 0, "relation-type": 1, "layer": 2, "entity": 3, "relation": 4, "query": 5, "view": 6, "reference": 7, "column": 8, "constraint": 9, "row": 10, "parameter": 11, "table-column": 12, "export": 13}
	rank, ok := ranks[kind]
	return rank, ok
}

func validatePresenceRule(path string, object map[string]any, rule map[string]any) error {
	if values, ok := rule["required"].([]any); ok {
		for _, rawName := range values {
			name, _ := rawName.(string)
			if _, ok := object[name]; !ok { return fmt.Errorf("%s tagged alternative requires %s", path, name) }
		}
	}
	if values, ok := rule["forbidden"].([]any); ok {
		for _, rawName := range values {
			name, _ := rawName.(string)
			if _, ok := object[name]; ok { return fmt.Errorf("%s tagged alternative forbids %s", path, name) }
		}
	}
	if values, ok := rule["empty"].([]any); ok {
		for _, rawName := range values {
			name, _ := rawName.(string)
			items, ok := object[name].([]any)
			if !ok || len(items) != 0 { return fmt.Errorf("%s tagged alternative requires empty %s", path, name) }
		}
	}
	if values, ok := rule["non_empty"].([]any); ok {
		for _, rawName := range values {
			name, _ := rawName.(string)
			items, ok := object[name].([]any)
			if !ok || len(items) == 0 { return fmt.Errorf("%s tagged alternative requires non-empty %s", path, name) }
		}
	}
	if rules, ok := rule["allowed_values"].(map[string]any); ok {
		for property, rawValues := range rules {
			value, present := object[property]
			if !present { continue }
			allowed := false
			if values, ok := rawValues.([]any); ok { for _, candidate := range values { if value == candidate { allowed = true } } }
			if !allowed { return fmt.Errorf("%s tagged alternative forbids value %q for %s", path, value, property) }
		}
	}
	return nil
}

func validateDiffSource(path string, object map[string]any) error {
	if object["kind"] != "diff" { return nil }
	before, beforeOK := object["before"].(string)
	after, afterOK := object["after"].(string)
	if !beforeOK || !afterOK || before == "" || after == "" || before == after {
		return fmt.Errorf("%s diff source requires nonempty unequal before and after", path)
	}
	if _, hasQuery := object["query_address"]; !hasQuery {
		arguments, ok := object["arguments"].(map[string]any)
		if !ok || len(arguments) != 0 { return fmt.Errorf("%s diff source without query_address requires empty arguments", path) }
	}
	return nil
}

func validateProtocolOffer(path string, object map[string]any) error {
	rangeText, _ := object["supported_range"].(string)
	lower, upper, ok := parseProtocolVersionRange(rangeText)
	if !ok { return fmt.Errorf("%s has an invalid supported_range", path) }
	versions, _ := object["versions"].([]any)
	seen := map[string]bool{}
	for _, rawBinding := range versions {
		binding, _ := rawBinding.(map[string]any)
		versionText, _ := binding["version"].(string)
		version, _, valid := parseProtocolVersion(versionText)
		if !valid || compareProtocolVersions(version, lower) < 0 || compareProtocolVersions(version, upper) > 0 { return fmt.Errorf("%s version %q is outside supported_range", path, versionText) }
		if seen[versionText] { return fmt.Errorf("%s repeats protocol version %q", path, versionText) }
		seen[versionText] = true
	}
	return nil
}

func validateLimitCapability(path string, object map[string]any) error {
	defaultValue, defaultErr := strconv.ParseInt(fmt.Sprint(object["default_value"]), 10, 64)
	effective, effectiveErr := strconv.ParseInt(fmt.Sprint(object["effective_maximum"]), 10, 64)
	hard, hardErr := strconv.ParseInt(fmt.Sprint(object["hard_maximum"]), 10, 64)
	if defaultErr != nil || effectiveErr != nil || hardErr != nil || defaultValue > hard || effective > hard { return fmt.Errorf("%s default and effective limits must not exceed the hard maximum", path) }
	return nil
}

func appendCanonicalJSON(destination []byte, value any) ([]byte, error) {
	switch typed := value.(type) {
	case nil:
		return append(destination, "null"...), nil
	case bool:
		return strconv.AppendBool(destination, typed), nil
	case string:
		var buffer bytes.Buffer
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(typed); err != nil { return nil, err }
		return append(destination, bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})...), nil
	case json.Number:
		text := typed.String()
		if !regexp.MustCompile(` + "`" + `^(0|-[1-9][0-9]*|[1-9][0-9]*)$` + "`" + `).MatchString(text) { return nil, errors.New("protocol canonical JSON permits only canonical integer numbers") }
		return append(destination, text...), nil
	case []any:
		destination = append(destination, '[')
		for index, item := range typed {
			if index != 0 { destination = append(destination, ',') }
			var err error
			destination, err = appendCanonicalJSON(destination, item)
			if err != nil { return nil, err }
		}
		return append(destination, ']'), nil
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed { keys = append(keys, key) }
		sort.Slice(keys, func(i, j int) bool {
			left, right := utf16.Encode([]rune(keys[i])), utf16.Encode([]rune(keys[j]))
			for index := 0; index < len(left) && index < len(right); index++ {
				if left[index] != right[index] { return left[index] < right[index] }
			}
			return len(left) < len(right)
		})
		destination = append(destination, '{')
		for index, key := range keys {
			if index != 0 { destination = append(destination, ',') }
			var err error
			destination, err = appendCanonicalJSON(destination, key)
			if err != nil { return nil, err }
			destination = append(destination, ':')
			destination, err = appendCanonicalJSON(destination, typed[key])
			if err != nil { return nil, err }
		}
		return append(destination, '}'), nil
	default:
		return nil, fmt.Errorf("unsupported canonical JSON value %T", value)
	}
}
`

func generateTypeScript(set schemaSet, document *schemaDocument) ([]byte, error) {
	imports := tsImports(set, document)
	var body strings.Builder
	fmt.Fprintf(&body, "// Code generated by %s; DO NOT EDIT.\n", generatorVersion)
	fmt.Fprintf(&body, "// Schema digest: %s\n", document.digest)
	body.WriteString("// SPDX-License-Identifier: Apache-2.0\n\n")
	for _, module := range sortedKeys(imports) {
		names := imports[module]
		sort.Strings(names)
		fmt.Fprintf(&body, "import type { %s } from \"./%s.gen.js\";\n", strings.Join(names, ", "), module)
		validators := make([]string, len(names))
		for index, name := range names {
			validators[index] = "is" + name
		}
		fmt.Fprintf(&body, "import { %s } from \"./%s.gen.js\";\n", strings.Join(validators, ", "), module)
	}
	if len(imports) != 0 {
		body.WriteString("\n")
	}
	fmt.Fprintf(&body, "export const schemaDigest = %q as const;\n\n", document.digest)
	fmt.Fprintf(&body, "export const maxWireJSONBytes = %d as const;\n", document.MaxJSONBytes)
	fmt.Fprintf(&body, "export const maxWireJSONDepth = %d as const;\n\n", document.MaxJSONDepth)
	body.WriteString("function isObject(value: unknown): value is Record<string, unknown> {\n")
	body.WriteString("  if (typeof value !== \"object\" || value === null || Array.isArray(value)) return false;\n")
	body.WriteString("  const prototype = Object.getPrototypeOf(value); if (prototype !== Object.prototype && prototype !== null) return false;\n")
	body.WriteString("  for (const key of Reflect.ownKeys(value)) { if (typeof key !== \"string\" || !hasScalarUnicode(key)) return false; const descriptor = Object.getOwnPropertyDescriptor(value, key); if (descriptor === undefined || !descriptor.enumerable || !(\"value\" in descriptor)) return false; }\n")
	body.WriteString("  return true;\n")
	body.WriteString("}\n\n")
	body.WriteString("function isJSONArray(value: unknown): value is ReadonlyArray<unknown> {\n")
	body.WriteString("  if (!Array.isArray(value) || Object.getPrototypeOf(value) !== Array.prototype) return false;\n")
	body.WriteString("  const descriptors = Object.getOwnPropertyDescriptors(value); const keys = Reflect.ownKeys(value); if (keys.some((key) => typeof key !== \"string\") || keys.length !== value.length + 1) return false;\n")
	body.WriteString("  for (let index = 0; index < value.length; index++) { const descriptor = descriptors[String(index)]; if (descriptor === undefined || !descriptor.enumerable || !(\"value\" in descriptor)) return false; }\n")
	body.WriteString("  return Object.keys(value).length === value.length;\n")
	body.WriteString("}\n\n")
	body.WriteString("function hasOwn(value: Record<string, unknown>, key: string): boolean {\n")
	body.WriteString("  return Object.prototype.propertyIsEnumerable.call(value, key);\n")
	body.WriteString("}\n\n")
	body.WriteString("function hasOnlyKeys(value: Record<string, unknown>, allowed: ReadonlySet<string>): boolean {\n")
	body.WriteString("  return Object.keys(value).every((key) => allowed.has(key));\n")
	body.WriteString("}\n\n")
	body.WriteString("function isJSONCompatible(value: unknown, active: Set<object> = new Set<object>(), depth = 0): boolean {\n")
	body.WriteString("  if (value === null || typeof value === \"boolean\") return true;\n")
	body.WriteString("  if (typeof value === \"string\") return hasScalarUnicode(value);\n")
	body.WriteString("  const array = isJSONArray(value); if (!array && !isObject(value)) return false;\n")
	body.WriteString("  if (depth >= maxWireJSONDepth || active.has(value)) return false; active.add(value);\n")
	body.WriteString("  try { return array ? value.every((item) => isJSONCompatible(item, active, depth + 1)) : Object.values(value).every((item) => isJSONCompatible(item, active, depth + 1)); } finally { active.delete(value); }\n")
	body.WriteString("}\n\n")
	body.WriteString("function hasScalarUnicode(value: unknown): value is string {\n")
	body.WriteString("  if (typeof value !== \"string\") return false;\n")
	body.WriteString("  for (let index = 0; index < value.length; index++) { const code = value.charCodeAt(index); if (code >= 0xd800 && code <= 0xdbff) { const low = value.charCodeAt(index + 1); if (!(low >= 0xdc00 && low <= 0xdfff)) return false; index++; } else if (code >= 0xdc00 && code <= 0xdfff) return false; }\n")
	body.WriteString("  return true;\n")
	body.WriteString("}\n\n")
	body.WriteString("function matchesCanonicalInt64(value: string): boolean {\n")
	body.WriteString("  if (!/^(0|-[1-9][0-9]*|[1-9][0-9]*)$/.test(value)) return false;\n")
	body.WriteString("  try { const parsed = BigInt(value); return parsed >= -(2n ** 63n) && parsed <= (2n ** 63n) - 1n; } catch { return false; }\n")
	body.WriteString("}\n\n")
	body.WriteString("function matchesCanonicalNonNegativeInt64(value: string): boolean {\n")
	body.WriteString("  if (!/^(0|[1-9][0-9]*)$/.test(value)) return false;\n")
	body.WriteString("  try { return BigInt(value) <= (2n ** 63n) - 1n; } catch { return false; }\n")
	body.WriteString("}\n\n")
	body.WriteString("function matchesCanonicalUint64(value: string): boolean {\n")
	body.WriteString("  if (!/^(0|[1-9][0-9]*)$/.test(value)) return false;\n")
	body.WriteString("  try { const parsed = BigInt(value); return parsed <= (2n ** 64n) - 1n; } catch { return false; }\n")
	body.WriteString("}\n\n")
	body.WriteString("function matchesCanonicalPositiveInt64(value: string): boolean {\n")
	body.WriteString("  if (!/^[1-9][0-9]*$/.test(value)) return false; try { return BigInt(value) <= (2n ** 63n) - 1n; } catch { return false; }\n")
	body.WriteString("}\n\n")
	body.WriteString("function matchesCanonicalSafeInteger(value: string, minimum = -(2n ** 53n) + 1n): boolean {\n")
	body.WriteString("  if (!/^(0|-[1-9][0-9]*|[1-9][0-9]*)$/.test(value)) return false; try { const parsed = BigInt(value); return parsed >= minimum && parsed <= (2n ** 53n) - 1n; } catch { return false; }\n")
	body.WriteString("}\n\n")
	body.WriteString("function matchesCanonicalNonNegativeSafeInteger(value: string): boolean {\n")
	body.WriteString("  return /^(0|[1-9][0-9]*)$/.test(value) && matchesCanonicalSafeInteger(value, 0n);\n")
	body.WriteString("}\n\n")
	body.WriteString("function matchesCanonicalPositiveSafeInteger(value: string): boolean {\n")
	body.WriteString("  return /^[1-9][0-9]*$/.test(value) && matchesCanonicalSafeInteger(value, 1n);\n")
	body.WriteString("}\n\n")
	body.WriteString("function matchesCanonicalBinary64(value: string, positive: boolean): boolean {\n")
	body.WriteString("  if (!/^-?(0|[1-9][0-9]*)(?:\\.[0-9]+)?(?:e[+-][1-9][0-9]*)?$/.test(value)) return false; const parsed = Number(value); return Number.isFinite(parsed) && !Object.is(parsed, -0) && (!positive || parsed > 0) && String(parsed) === value;\n")
	body.WriteString("}\n\n")
	body.WriteString("function parseProtocolVersion(value: string): readonly [number, number] | undefined {\n")
	body.WriteString("  const match = /^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$/.exec(value); if (match === null) return undefined; const major = Number(match[1]); const minor = Number(match[2]); if (!Number.isSafeInteger(major) || !Number.isSafeInteger(minor) || major > 0xffffffff || minor > 0xffffffff) return undefined; return [major, minor];\n")
	body.WriteString("}\n\n")
	body.WriteString("function parseProtocolVersionRange(value: string): readonly [readonly [number, number], readonly [number, number]] | undefined {\n")
	body.WriteString("  const parts = value.split(\"..\"); if (parts.length !== 2) return undefined; const lower = parseProtocolVersion(parts[0]!); const upper = parseProtocolVersion(parts[1]!); if (lower === undefined || upper === undefined || lower[0] !== upper[0] || compareProtocolVersions(lower, upper) > 0) return undefined; return [lower, upper];\n")
	body.WriteString("}\n\n")
	body.WriteString("function compareProtocolVersions(left: readonly [number, number], right: readonly [number, number]): number { return left[0] === right[0] ? left[1] - right[1] : left[0] - right[0]; }\n\n")
	body.WriteString("function matchesCanonicalSourcePath(value: string): boolean { return value !== \"\" && !value.startsWith(\"/\") && !value.includes(\"\\\\\") && !value.includes(\"\\0\") && value.split(\"/\").every((segment) => segment !== \"\" && segment !== \".\" && segment !== \"..\"); }\n\n")
	body.WriteString("function hasOperatorValueRule(value: Record<string, unknown>, operatorProperty: string, valueProperty: string, valueless: ReadonlySet<string>): boolean { const operator = value[operatorProperty]; if (typeof operator !== \"string\") return true; return valueless.has(operator) ? !hasOwn(value, valueProperty) : hasOwn(value, valueProperty); }\n\n")
	body.WriteString("function hasValidProtocolOffer(value: Record<string, unknown>): boolean { const range = value[\"supported_range\"]; const bindings = value[\"versions\"]; if (typeof range !== \"string\" || !isJSONArray(bindings)) return false; const parsedRange = parseProtocolVersionRange(range); if (parsedRange === undefined) return false; const seen = new Set<string>(); for (const raw of bindings) { if (!isObject(raw) || typeof raw[\"version\"] !== \"string\") return false; const text = raw[\"version\"]; const version = parseProtocolVersion(text); if (version === undefined || compareProtocolVersions(version, parsedRange[0]) < 0 || compareProtocolVersions(version, parsedRange[1]) > 0 || seen.has(text)) return false; seen.add(text); } return true; }\n\n")
	body.WriteString("function hasValidLimitCapability(value: Record<string, unknown>): boolean { try { const fallback = BigInt(String(value[\"default_value\"])); const effective = BigInt(String(value[\"effective_maximum\"])); const hard = BigInt(String(value[\"hard_maximum\"])); return fallback <= hard && effective <= hard; } catch { return false; } }\n\n")
	body.WriteString("function hasUniqueArrayKey(value: Record<string, unknown>, arrayProperty: string, keyProperty: string): boolean { const items = value[arrayProperty]; if (!isJSONArray(items)) return false; const seen = new Set<string>(); for (const raw of items) { if (!isObject(raw) || typeof raw[keyProperty] !== \"string\" || seen.has(raw[keyProperty])) return false; seen.add(raw[keyProperty]); } return true; }\n\n")
	body.WriteString("function hasUniqueItems(value: ReadonlyArray<unknown>): boolean { return new Set(value).size === value.length; }\n\n")
	body.WriteString("function stableAddressOrderValue(value: unknown, selector: string): string | undefined { if (selector === \"$item\") return typeof value === \"string\" ? value : undefined; return isObject(value) && typeof value[selector] === \"string\" ? value[selector] : undefined; }\n\n")
	body.WriteString("function hasStableAddressOrder(value: ReadonlyArray<unknown>, selector: string): boolean { for (let index = 1; index < value.length; index++) { const left = stableAddressOrderValue(value[index - 1], selector); const right = stableAddressOrderValue(value[index], selector); if (left === undefined || right === undefined || compareStableAddresses(left, right) >= 0) return false; } return true; }\n\n")
	body.WriteString("function hasCanonicalEnumOrder(value: ReadonlyArray<unknown>, values: ReadonlyArray<string>): boolean { const ranks = new Map(values.map((item, index) => [item, index])); for (let index = 1; index < value.length; index++) { const left = value[index - 1]; const right = value[index]; if (typeof left !== \"string\" || typeof right !== \"string\" || ranks.get(left) === undefined || ranks.get(right) === undefined || ranks.get(left)! >= ranks.get(right)!) return false; } return true; }\n\n")
	body.WriteString("function isCanonicalLocalIdentifier(value: unknown): value is string { return typeof value === \"string\" && /^[a-z][a-z0-9_]*$/.test(value); }\n\n")
	body.WriteString("function hasCanonicalIdentifierOrder(value: ReadonlyArray<unknown>): boolean { return value.every(isCanonicalLocalIdentifier) && value.every((item, index) => index === 0 || (value[index - 1] as string) < (item as string)); }\n\n")
	body.WriteString("function compareUnicodeScalars(left: string, right: string): number { const leftScalars = Array.from(left, (item) => item.codePointAt(0)!); const rightScalars = Array.from(right, (item) => item.codePointAt(0)!); for (let index = 0; index < Math.min(leftScalars.length, rightScalars.length); index++) { if (leftScalars[index] !== rightScalars[index]) return leftScalars[index]! - rightScalars[index]!; } return leftScalars.length - rightScalars.length; }\n\n")
	body.WriteString("function hasUnicodeScalarOrder(value: ReadonlyArray<unknown>): boolean { return value.every((item) => typeof item === \"string\") && value.every((item, index) => index === 0 || compareUnicodeScalars(value[index - 1] as string, item as string) < 0); }\n\n")
	body.WriteString("function hasDirectStableAddressOwner(owner: string, child: string): boolean { const parts = child.split(\":\"); return parts.length >= 2 && parts.slice(0, -2).join(\":\") === owner; }\n\n")
	body.WriteString("function hasAddressOwner(value: Record<string, unknown>, ownerProperty: string, childrenProperty: string, selector: string): boolean { if (!hasOwn(value, ownerProperty)) return true; const owner = value[ownerProperty]; if (typeof owner !== \"string\") return false; const rawChildren = value[childrenProperty]; let children: ReadonlyArray<unknown>; if (selector === \"$value\") children = [rawChildren]; else if (selector === \"$propertyNames\") { if (!isObject(rawChildren)) return false; children = Object.keys(rawChildren); } else { if (!isJSONArray(rawChildren)) return false; children = rawChildren.map((item) => isObject(item) ? item[selector] : undefined); } return children.every((child) => typeof child === \"string\" && hasDirectStableAddressOwner(owner, child)); }\n\n")
	body.WriteString("function compareStableAddresses(left: string, right: string): number { const leftTuple = stableAddressTuple(left); const rightTuple = stableAddressTuple(right); if (leftTuple === undefined || rightTuple === undefined) return 0; if (leftTuple.origin !== rightTuple.origin) return leftTuple.origin - rightTuple.origin; for (let index = 0; index < Math.min(leftTuple.components.length, rightTuple.components.length); index++) { const compared = compareASCII(leftTuple.components[index]!, rightTuple.components[index]!); if (compared !== 0) return compared; } if (leftTuple.components.length !== rightTuple.components.length) return leftTuple.components.length - rightTuple.components.length; if (leftTuple.path.length !== rightTuple.path.length) return leftTuple.path.length - rightTuple.path.length; for (let index = 0; index < leftTuple.path.length; index++) { const leftSegment = leftTuple.path[index]!; const rightSegment = rightTuple.path[index]!; const kind = stableAddressKindRank(leftSegment[0]) - stableAddressKindRank(rightSegment[0]); if (kind !== 0) return kind; const id = compareASCII(leftSegment[1], rightSegment[1]); if (id !== 0) return id; } return 0; }\n\n")
	body.WriteString("function stableAddressTuple(value: string): { origin: number; components: ReadonlyArray<string>; path: ReadonlyArray<readonly [string, string]> } | undefined { const parts = value.split(\":\"); if (parts.length < 3 || parts[0] !== \"ldl\") return undefined; let origin: number; let components: ReadonlyArray<string>; let pathStart: number; if (parts[1] === \"project\") { origin = 0; components = [parts[2]!]; pathStart = 3; } else if (parts[1] === \"pack\" && parts.length >= 4) { origin = 1; components = [parts[2]!, parts[3]!]; pathStart = 4; } else return undefined; if ((parts.length - pathStart) % 2 !== 0) return undefined; const path: Array<readonly [string, string]> = []; for (let index = pathStart; index < parts.length; index += 2) path.push([parts[index]!, parts[index + 1]!]); return {origin, components, path}; }\n\n")
	body.WriteString("function stableAddressKindRank(kind: string): number { return new Map<string, number>([[\"entity-type\",0],[\"relation-type\",1],[\"layer\",2],[\"entity\",3],[\"relation\",4],[\"query\",5],[\"view\",6],[\"reference\",7],[\"column\",8],[\"constraint\",9],[\"row\",10],[\"parameter\",11],[\"table-column\",12],[\"export\",13]]).get(kind) ?? Number.MAX_SAFE_INTEGER; }\n\n")
	body.WriteString("function compareASCII(left: string, right: string): number { return left < right ? -1 : left > right ? 1 : 0; }\n\n")
	body.WriteString("function hasDisjointArrays(value: Record<string, unknown>, leftProperty: string, rightProperty: string): boolean { const left = hasOwn(value, leftProperty) ? value[leftProperty] : []; const right = hasOwn(value, rightProperty) ? value[rightProperty] : []; if (!isJSONArray(left) || !isJSONArray(right)) return false; const seen = new Set(left); return right.every((item) => !seen.has(item)); }\n\n")
	body.WriteString("function hasDisjointArrayKey(value: Record<string, unknown>, arrayProperty: string, keyProperty: string, stringsProperty: string): boolean { const items = value[arrayProperty]; const strings = value[stringsProperty]; if (!isJSONArray(items) || !isJSONArray(strings) || !strings.every((item) => typeof item === \"string\")) return false; const reserved = new Set(strings); return items.every((item) => isObject(item) && typeof item[keyProperty] === \"string\" && !reserved.has(item[keyProperty])); }\n\n")
	body.WriteString("function compareCanonicalUnsignedDecimals(left: string, right: string): number | undefined { if (!/^(0|[1-9][0-9]*)$/.test(left) || !/^(0|[1-9][0-9]*)$/.test(right)) return undefined; return left.length === right.length ? (left < right ? -1 : left > right ? 1 : 0) : left.length - right.length; }\n\n")
	body.WriteString("function hasOrderedPair(value: Record<string, unknown>, lowerProperty: string, upperProperty: string, comparison: string): boolean { if (!hasOwn(value, lowerProperty) || !hasOwn(value, upperProperty)) return true; const lower = value[lowerProperty]; const upper = value[upperProperty]; if (typeof lower !== \"string\" || typeof upper !== \"string\") return false; if (comparison === \"unsigned_decimal\") { const ordered = compareCanonicalUnsignedDecimals(lower, upper); return ordered !== undefined && ordered <= 0; } if (comparison === \"finite_binary64\") { const lowerValue = Number(lower); const upperValue = Number(upper); return Number.isFinite(lowerValue) && Number.isFinite(upperValue) && lowerValue <= upperValue; } return false; }\n\n")
	body.WriteString("function hasAddressTerminalID(value: Record<string, unknown>, addressProperty: string, idProperty: string): boolean { const address = value[addressProperty]; const id = value[idProperty]; return typeof address === \"string\" && typeof id === \"string\" && address.split(\":\").at(-1) === id; }\n\n")
	body.WriteString("function hasValidExportRecipe(value: Record<string, unknown>): boolean { const format = value[\"format\"]; const options = value[\"options\"]; const profile = value[\"exporter_profile\"]; const extension = value[\"extension\"]; const filename = value[\"filename\"]; if (typeof format !== \"string\" || !isObject(options) || !isObject(profile) || options[\"kind\"] !== format || profile[\"format\"] !== format || typeof extension !== \"string\" || typeof filename !== \"string\") return false; const expected = new Map<string, string>([[\"json\",\".json\"],[\"yaml\",\".yaml\"],[\"svg\",\".svg\"],[\"png\",\".png\"],[\"pdf\",\".pdf\"],[\"html\",\".html\"],[\"csv\",\".csv\"],[\"tsv\",\".tsv\"],[\"xlsx\",\".xlsx\"],[\"markdown\",\".md\"],[\"pptx\",\".pptx\"],[\"docx\",\".docx\"],[\"mermaid\",\".mmd\"],[\"bpmn\",\".bpmn\"],[\"drawio\",\".drawio\"]]).get(format); return expected !== undefined && extension === expected && filename !== \"\" && filename !== \".\" && filename !== \"..\" && !/[\\\\/\\u0000]/.test(filename) && filename.endsWith(extension) && filename.slice(0, -extension.length).length > 0; }\n\n")
	body.WriteString("function hasValidViewRecipe(value: Record<string, unknown>): boolean { const address = value[\"address\"]; const shape = value[\"shape\"]; const reservedValues = value[\"reserved_table_column_ids\"]; if (typeof address !== \"string\" || !isObject(shape) || !isJSONArray(reservedValues) || !reservedValues.every((item) => typeof item === \"string\")) return false; if (shape[\"kind\"] !== \"table\") return true; const table = shape[\"table\"]; if (!isObject(table) || !isJSONArray(table[\"columns\"])) return false; const reserved = new Set(reservedValues); return table[\"columns\"].every((item) => isObject(item) && typeof item[\"address\"] === \"string\" && typeof item[\"id\"] === \"string\" && hasDirectStableAddressOwner(address, item[\"address\"]) && !reserved.has(item[\"id\"])); }\n\n")
	body.WriteString("function hasValidDiffSource(value: Record<string, unknown>): boolean { if (value[\"kind\"] !== \"diff\") return true; const before = value[\"before\"]; const after = value[\"after\"]; if (typeof before !== \"string\" || typeof after !== \"string\" || before.length === 0 || after.length === 0 || before === after) return false; return hasOwn(value, \"query_address\") || (isObject(value[\"arguments\"]) && Object.keys(value[\"arguments\"]).length === 0); }\n\n")
	body.WriteString("function isRFC3339(value: string): boolean {\n")
	body.WriteString("  const match = /^([0-9]{4})-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](?:\\.[0-9]{1,9})?Z$/.exec(value);\n")
	body.WriteString("  if (match === null) return false; const year = Number(match[1]); const month = Number(match[2]); const day = Number(match[3]); const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0); const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]; return day >= 1 && day <= days[month - 1]!;\n")
	body.WriteString("}\n\n")
	body.WriteString(tsWirePreflight)
	body.WriteString("function canonicalJSONStringify(value: unknown): string {\n")
	body.WriteString("  if (value === null || typeof value === \"boolean\") return JSON.stringify(value);\n")
	body.WriteString("  if (typeof value === \"string\") { if (!hasScalarUnicode(value)) throw new TypeError(\"protocol strings must contain Unicode scalar values\"); return JSON.stringify(value).replace(/[\\u2028\\u2029]/g, (character) => character === \"\\u2028\" ? \"\\\\u2028\" : \"\\\\u2029\"); }\n")
	body.WriteString("  if (typeof value === \"number\") { if (!Number.isSafeInteger(value) || Object.is(value, -0)) throw new TypeError(\"protocol numbers must be canonical safe integers\"); return String(value); }\n")
	body.WriteString("  if (isJSONArray(value)) return `[${value.map(canonicalJSONStringify).join(\",\")}]`;\n")
	body.WriteString("  if (isObject(value)) return `{${Object.keys(value).sort().map((key) => `${canonicalJSONStringify(key)}:${canonicalJSONStringify(value[key])}`).join(\",\")}}`;\n")
	body.WriteString("  throw new TypeError(\"unsupported protocol JSON value\");\n")
	body.WriteString("}\n\n")
	for _, name := range sortedKeys(document.Definitions) {
		definition := document.Definitions[name]
		if definition.Description != "" {
			fmt.Fprintf(&body, "/** %s */\n", strings.ReplaceAll(strings.TrimSpace(definition.Description), "*/", "* /"))
		}
		if err := writeTSDefinition(&body, set, document, name, definition); err != nil {
			return nil, fmt.Errorf("generate TypeScript %s.%s: %w", document.Module, name, err)
		}
		predicate, err := tsPredicate(set, document, definition, "value")
		if err != nil {
			return nil, fmt.Errorf("generate TypeScript validator %s.%s: %w", document.Module, name, err)
		}
		if name == "JsonValue" {
			predicate = "isJSONCompatible(value)"
		}
		fmt.Fprintf(&body, "\nexport function is%s(value: unknown): value is %s {\n  return isProgrammaticWireValue(value, () => %s);\n}\n", name, name, predicate)
		fmt.Fprintf(&body, "\nexport function decode%s(input: string): %s {\n  validateWireJSONText(input);\n  const value: unknown = JSON.parse(input);\n  if (!is%s(value)) throw new TypeError(%q);\n  return value;\n}\n", name, name, name, "invalid "+name)
		fmt.Fprintf(&body, "\nexport function encode%s(value: %s): string {\n  validateProgrammaticWireValue(value);\n  if (!is%s(value)) throw new TypeError(%q);\n  const encoded = canonicalJSONStringify(value);\n  validateWireJSONText(encoded);\n  const emitted: unknown = JSON.parse(encoded);\n  if (!is%s(emitted)) throw new TypeError(%q);\n  return encoded;\n}\n", name, name, name, "invalid "+name, name, "encoded value is invalid "+name)
		body.WriteString("\n")
	}
	if err := writeTSBlobCollectors(&body, set, document); err != nil {
		return nil, err
	}
	return append(bytes.TrimRight([]byte(body.String()), "\n"), '\n'), nil
}

func writeTSBlobCollectors(body *strings.Builder, set schemaSet, document *schemaDocument) error {
	if document.Definitions["CompileInput"] == nil || document.Definitions["CompileResult"] == nil {
		return nil
	}
	for _, name := range []string{"CompileInput", "CompileResult"} {
		functionName := "collect" + name + "BlobRefs"
		fmt.Fprintf(body, "export function %s(value: %s): ReadonlyArray<BlobRef> {\n", functionName, name)
		fmt.Fprintf(body, "  validateProgrammaticWireValue(value);\n  if (!is%s(value)) throw new TypeError(%q);\n", name, "invalid "+name)
		body.WriteString("  const refs: Array<BlobRef> = [];\n")
		counter := 0
		if err := writeTSBlobCollectorStatements(body, set, document, document.Definitions[name], "value", "  ", &counter); err != nil {
			return err
		}
		body.WriteString("  return refs;\n}\n\n")
	}
	return nil
}

func writeTSBlobCollectorStatements(body *strings.Builder, set schemaSet, document *schemaDocument, value *schemaType, expression, indent string, counter *int) error {
	resolvedDocument, resolved, err := dereferenceSchemaType(set, document, value)
	if err != nil {
		return err
	}
	if isBlobRefSchema(resolved) {
		fmt.Fprintf(body, "%srefs.push({blob_id: %s.blob_id, digest: %s.digest, lifetime: %s.lifetime, media_type: %s.media_type, size: %s.size});\n", indent, expression, expression, expression, expression, expression)
		return nil
	}
	typeValue, err := scalarType(resolved.Type)
	if err != nil {
		return err
	}
	switch typeValue {
	case "array":
		if !schemaContainsBlobRefs(set, resolvedDocument, resolved.Items, map[*schemaType]bool{}, map[*schemaType]bool{}) {
			return nil
		}
		item := fmt.Sprintf("blobItem%d", *counter)
		*counter++
		fmt.Fprintf(body, "%sfor (const %s of %s) {\n", indent, item, expression)
		if err := writeTSBlobCollectorStatements(body, set, resolvedDocument, resolved.Items, item, indent+"  ", counter); err != nil {
			return err
		}
		fmt.Fprintf(body, "%s}\n", indent)
	case "object":
		required := stringSet(resolved.Required)
		for _, propertyName := range sortedKeys(resolved.Properties) {
			property := resolved.Properties[propertyName]
			if !schemaContainsBlobRefs(set, resolvedDocument, property, map[*schemaType]bool{}, map[*schemaType]bool{}) {
				continue
			}
			access := expression + "[" + fmt.Sprintf("%q", propertyName) + "]"
			if required[propertyName] {
				if err := writeTSBlobCollectorStatements(body, set, resolvedDocument, property, access, indent, counter); err != nil {
					return err
				}
				continue
			}
			fmt.Fprintf(body, "%sif (%s !== undefined) {\n", indent, access)
			if err := writeTSBlobCollectorStatements(body, set, resolvedDocument, property, access, indent+"  ", counter); err != nil {
				return err
			}
			fmt.Fprintf(body, "%s}\n", indent)
		}
	}
	return nil
}

const tsWirePreflight = `function utf8ByteLength(value: string): number {
  let bytes = 0;
  for (let index = 0; index < value.length; index++) {
    const code = value.charCodeAt(index);
    if (code <= 0x7f) bytes++;
    else if (code <= 0x7ff) bytes += 2;
    else if (code >= 0xd800 && code <= 0xdbff) { const low = value.charCodeAt(index + 1); if (!(low >= 0xdc00 && low <= 0xdfff)) throw new TypeError("protocol JSON contains an unpaired high surrogate"); bytes += 4; index++; }
    else if (code >= 0xdc00 && code <= 0xdfff) throw new TypeError("protocol JSON contains an unpaired low surrogate");
    else bytes += 3;
  }
  return bytes;
}

function validateProgrammaticWireValue(value: unknown, active: Set<object> = new Set<object>(), depth = 0): void {
  if (value === null || typeof value === "boolean") return;
  if (typeof value === "string") { if (!hasScalarUnicode(value)) throw new TypeError("protocol value contains malformed Unicode"); return; }
  if (typeof value === "number") { if (!Number.isSafeInteger(value) || Object.is(value, -0)) throw new TypeError("protocol numbers must be canonical safe integers"); return; }
  const array = isJSONArray(value); if (!array && !isObject(value)) throw new TypeError("unsupported protocol JSON value");
  if (active.has(value)) throw new TypeError("protocol value contains a cycle");
  if (depth >= maxWireJSONDepth) throw new TypeError("protocol value exceeds depth " + maxWireJSONDepth);
  active.add(value);
  try {
    if (array) { for (const item of value) validateProgrammaticWireValue(item, active, depth + 1); }
    else { for (const item of Object.values(value)) validateProgrammaticWireValue(item, active, depth + 1); }
  } finally { active.delete(value); }
}

function isProgrammaticWireValue(value: unknown, predicate: () => boolean): boolean {
  try { validateProgrammaticWireValue(value); return predicate(); } catch { return false; }
}

function validateWireJSONText(input: string): void {
  if (utf8ByteLength(input) > maxWireJSONBytes) throw new TypeError("protocol JSON exceeds " + maxWireJSONBytes + " UTF-8 bytes");
  let depth = 0;
  for (let index = 0; index < input.length; index++) {
    const character = input[index]!;
    if (character === '"') { index = scanJSONString(input, index); continue; }
    if (character === "{" || character === "[") { depth++; if (depth > maxWireJSONDepth) throw new TypeError("protocol JSON exceeds depth " + maxWireJSONDepth); continue; }
    if (character === "}" || character === "]") { depth--; continue; }
    if (character === "-" || (character >= "0" && character <= "9")) { const end = scanJSONToken(input, index); validateCanonicalJSONNumber(input.slice(index, end)); index = end - 1; }
  }
	const end = scanUniqueJSONValue(input, skipJSONWhitespace(input, 0));
	if (skipJSONWhitespace(input, end) !== input.length) throw new TypeError("protocol JSON must contain exactly one value");
}

function skipJSONWhitespace(input: string, start: number): number {
  let index = start; while (index < input.length && /[ \t\r\n]/.test(input[index]!)) index++; return index;
}

function scanUniqueJSONValue(input: string, start: number): number {
  let index = skipJSONWhitespace(input, start);
  const character = input[index];
  if (character === '"') return scanJSONString(input, index) + 1;
  if (character === "[") {
    index = skipJSONWhitespace(input, index + 1); if (input[index] === "]") return index + 1;
    for (;;) { index = skipJSONWhitespace(input, scanUniqueJSONValue(input, index)); if (input[index] === "]") return index + 1; if (input[index] !== ",") throw new TypeError("protocol JSON array is malformed"); index++; }
  }
  if (character === "{") {
    const keys = new Set<string>(); index = skipJSONWhitespace(input, index + 1); if (input[index] === "}") return index + 1;
    for (;;) {
      if (input[index] !== '"') throw new TypeError("protocol JSON object key must be a string");
      const keyEnd = scanJSONString(input, index); const key: unknown = JSON.parse(input.slice(index, keyEnd + 1));
      if (typeof key !== "string") throw new TypeError("protocol JSON object key must be a string");
      if (keys.has(key)) throw new TypeError("protocol JSON contains duplicate object key " + key); keys.add(key);
      index = skipJSONWhitespace(input, keyEnd + 1); if (input[index] !== ":") throw new TypeError("protocol JSON object is missing a colon");
      index = skipJSONWhitespace(input, scanUniqueJSONValue(input, index + 1)); if (input[index] === "}") return index + 1; if (input[index] !== ",") throw new TypeError("protocol JSON object is malformed"); index = skipJSONWhitespace(input, index + 1);
    }
  }
  const end = scanJSONToken(input, index); if (end === index) throw new TypeError("protocol JSON value is malformed"); return end;
}

function scanJSONToken(input: string, start: number): number {
  let index = start;
  while (index < input.length && !/[{}\[\],:\s]/.test(input[index]!)) index++;
  return index;
}

function validateCanonicalJSONNumber(value: string): void {
  if (!/^(0|-[1-9][0-9]*|[1-9][0-9]*)$/.test(value)) throw new TypeError("protocol JSON number " + value + " is not a canonical integer");
  const parsed = BigInt(value);
  if (parsed < -9007199254740991n || parsed > 9007199254740991n) throw new TypeError("protocol JSON number " + value + " is outside the portable safe range");
}

function scanJSONString(input: string, start: number): number {
  for (let index = start + 1; index < input.length; index++) {
    const code = input.charCodeAt(index);
    if (code === 0x22) return index;
    if (code < 0x20) throw new TypeError("protocol JSON string contains an unescaped control character");
    if (code !== 0x5c) continue;
    index++;
    if (index >= input.length) throw new TypeError("protocol JSON string has a truncated escape");
    if (input[index] !== "u") continue;
    const unit = parseHexCodeUnit(input, index + 1); index += 4;
    if (unit >= 0xdc00 && unit <= 0xdfff) throw new TypeError("protocol JSON string has an unpaired low surrogate");
    if (unit < 0xd800 || unit > 0xdbff) continue;
    if (input[index + 1] !== "\\" || input[index + 2] !== "u") throw new TypeError("protocol JSON string has an unpaired high surrogate");
    const low = parseHexCodeUnit(input, index + 3);
    if (low < 0xdc00 || low > 0xdfff) throw new TypeError("protocol JSON string has an invalid surrogate pair");
    index += 6;
  }
  throw new TypeError("protocol JSON string is unterminated");
}

function parseHexCodeUnit(input: string, start: number): number {
  const text = input.slice(start, start + 4);
  if (!/^[0-9a-fA-F]{4}$/.test(text)) throw new TypeError("protocol JSON string has an invalid Unicode escape");
  return Number.parseInt(text, 16);
}

`

func tsPredicate(set schemaSet, document *schemaDocument, value *schemaType, expression string) (string, error) {
	if value.Ref != "" {
		_, name, err := resolveRef(set, document, value.Ref)
		return "is" + name + "(" + expression + ")", err
	}
	if len(value.OneOf) != 0 {
		var branches []string
		for _, branch := range value.OneOf {
			predicate, err := tsPredicate(set, document, branch, expression)
			if err != nil {
				return "", err
			}
			branches = append(branches, "("+predicate+")")
		}
		return strings.Join(branches, " || "), nil
	}
	typeValue, err := scalarType(value.Type)
	if err != nil {
		return "", err
	}
	switch typeValue {
	case "union":
		return "isJSONCompatible(" + expression + ")", nil
	case "string":
		parts := []string{"typeof " + expression + " === \"string\"", "hasScalarUnicode(" + expression + ")"}
		if value.Const != nil {
			parts = append(parts, expression+" === "+fmt.Sprintf("%q", value.Const))
		}
		if len(value.Enum) != 0 {
			values := make([]string, len(value.Enum))
			for index, enumValue := range value.Enum {
				values[index] = fmt.Sprintf("%q", enumValue)
			}
			parts = append(parts, "["+strings.Join(values, ", ")+"].includes("+expression+")")
		}
		if value.Pattern != "" {
			parts = append(parts, "new RegExp("+fmt.Sprintf("%q", value.Pattern)+").test("+expression+")")
		}
		if value.MinLength != nil {
			parts = append(parts, fmt.Sprintf("Array.from(%s).length >= %d", expression, *value.MinLength))
		}
		if value.Format == "int64-decimal" {
			parts = append(parts, "matchesCanonicalInt64("+expression+")")
		}
		if value.Format == "nonnegative-int64-decimal" {
			parts = append(parts, "matchesCanonicalNonNegativeInt64("+expression+")")
		}
		if value.Format == "uint64-decimal" {
			parts = append(parts, "matchesCanonicalUint64("+expression+")")
		}
		if value.Format == "date-time" {
			parts = append(parts, "isRFC3339("+expression+")")
		}
		if value.Format == "positive-int64-decimal" {
			parts = append(parts, "matchesCanonicalPositiveInt64("+expression+")")
		}
		if value.Format == "safe-integer-decimal" {
			parts = append(parts, "matchesCanonicalSafeInteger("+expression+")")
		}
		if value.Format == "nonnegative-safe-integer-decimal" {
			parts = append(parts, "matchesCanonicalNonNegativeSafeInteger("+expression+")")
		}
		if value.Format == "positive-safe-integer-decimal" {
			parts = append(parts, "matchesCanonicalPositiveSafeInteger("+expression+")")
		}
		if value.Format == "finite-binary64-decimal" {
			parts = append(parts, "matchesCanonicalBinary64("+expression+", false)")
		}
		if value.Format == "positive-finite-binary64-decimal" {
			parts = append(parts, "matchesCanonicalBinary64("+expression+", true)")
		}
		if value.Format == "protocol-version" {
			parts = append(parts, "parseProtocolVersion("+expression+") !== undefined")
		}
		if value.Format == "protocol-version-range" {
			parts = append(parts, "parseProtocolVersionRange("+expression+") !== undefined")
		}
		if value.Format == "protocol-version-or-range" {
			parts = append(parts, "(parseProtocolVersion("+expression+") !== undefined || parseProtocolVersionRange("+expression+") !== undefined)")
		}
		if value.Format == "canonical-source-path" {
			parts = append(parts, "matchesCanonicalSourcePath("+expression+")")
		}
		return strings.Join(parts, " && "), nil
	case "integer":
		parts := []string{"typeof " + expression + " === \"number\"", "Number.isSafeInteger(" + expression + ")", "!Object.is(" + expression + ", -0)"}
		if value.Minimum != nil {
			parts = append(parts, fmt.Sprintf("%s >= %v", expression, *value.Minimum))
		}
		if value.Maximum != nil {
			parts = append(parts, fmt.Sprintf("%s <= %v", expression, *value.Maximum))
		}
		return strings.Join(parts, " && "), nil
	case "number":
		parts := []string{"typeof " + expression + " === \"number\"", "Number.isFinite(" + expression + ")"}
		if value.Minimum != nil {
			parts = append(parts, fmt.Sprintf("%s >= %v", expression, *value.Minimum))
		}
		if value.Maximum != nil {
			parts = append(parts, fmt.Sprintf("%s <= %v", expression, *value.Maximum))
		}
		return strings.Join(parts, " && "), nil
	case "boolean":
		return "typeof " + expression + " === \"boolean\"", nil
	case "null":
		return expression + " === null", nil
	case "array":
		item, err := tsPredicate(set, document, value.Items, "item")
		if err != nil {
			return "", err
		}
		parts := []string{"isJSONArray(" + expression + ")", expression + ".every((item) => " + item + ")"}
		if value.MinItems != nil {
			parts = append(parts, fmt.Sprintf("%s.length >= %d", expression, *value.MinItems))
		}
		if value.UniqueItems {
			parts = append(parts, "hasUniqueItems("+expression+")")
		}
		if value.StableAddressOrder != "" {
			parts = append(parts, fmt.Sprintf("hasStableAddressOrder(%s, %q)", expression, value.StableAddressOrder))
		}
		if value.CanonicalEnumOrder {
			item := resolvedType(set, document, value.Items)
			values := make([]string, len(item.Enum))
			for index, enumValue := range item.Enum {
				values[index] = fmt.Sprintf("%q", enumValue)
			}
			parts = append(parts, fmt.Sprintf("hasCanonicalEnumOrder(%s, [%s])", expression, strings.Join(values, ", ")))
		}
		if value.CanonicalIDOrder {
			parts = append(parts, "hasCanonicalIdentifierOrder("+expression+")")
		}
		if value.UnicodeScalarOrder {
			parts = append(parts, "hasUnicodeScalarOrder("+expression+")")
		}
		return strings.Join(parts, " && "), nil
	case "object":
		if len(value.Properties) == 0 {
			if additional, ok := value.AdditionalProperties.(*schemaType); ok {
				item, err := tsPredicate(set, document, additional, "item")
				if err != nil {
					return "", err
				}
				parts := []string{"isObject(" + expression + ")", "Object.values(" + expression + ").every((item) => " + item + ")"}
				if value.PropertyNames != nil {
					name, err := tsPredicate(set, document, value.PropertyNames, "key")
					if err != nil {
						return "", err
					}
					parts = append(parts, "Object.keys("+expression+").every((key) => "+name+")")
				}
				return strings.Join(parts, " && "), nil
			}
			return "isObject(" + expression + ")", nil
		}
		keys := sortedKeys(value.Properties)
		quoted := make([]string, len(keys))
		for index, key := range keys {
			quoted[index] = fmt.Sprintf("%q", key)
		}
		parts := []string{"isObject(" + expression + ")", "hasOnlyKeys(" + expression + ", new Set([" + strings.Join(quoted, ", ") + "]))"}
		required := stringSet(value.Required)
		for _, key := range keys {
			access := expression + "[" + fmt.Sprintf("%q", key) + "]"
			predicate, err := tsPredicate(set, document, value.Properties[key], access)
			if err != nil {
				return "", err
			}
			if required[key] {
				parts = append(parts, fmt.Sprintf("hasOwn(%s, %q)", expression, key), "("+predicate+")")
			} else {
				parts = append(parts, "(!hasOwn("+expression+", "+fmt.Sprintf("%q", key)+") || ("+predicate+"))")
			}
		}
		if value.TaggedUnion != nil {
			var variants []string
			for _, tag := range sortedKeys(value.TaggedUnion.Variants) {
				variant := value.TaggedUnion.Variants[tag]
				tagLiteral := fmt.Sprintf("%q", tag)
				discriminator := value.Properties[value.TaggedUnion.Property]
				if discriminator != nil {
					resolved := discriminator
					if resolved.Ref != "" {
						if target, name, err := resolveRef(set, document, resolved.Ref); err == nil {
							resolved = target.Definitions[name]
						}
					}
					if discriminatorType, _ := scalarType(resolved.Type); discriminatorType == "boolean" {
						tagLiteral = tag
					}
				}
				conditions := []string{expression + "[" + fmt.Sprintf("%q", value.TaggedUnion.Property) + "] === " + tagLiteral}
				for _, property := range variant.Required {
					conditions = append(conditions, fmt.Sprintf("hasOwn(%s, %q)", expression, property))
				}
				for _, property := range variant.Forbidden {
					conditions = append(conditions, fmt.Sprintf("!hasOwn(%s, %q)", expression, property))
				}
				for _, property := range variant.Empty {
					conditions = append(conditions, fmt.Sprintf("isJSONArray(%s[%q]) && %s[%q].length === 0", expression, property, expression, property))
				}
				for _, property := range variant.NonEmpty {
					conditions = append(conditions, fmt.Sprintf("isJSONArray(%s[%q]) && %s[%q].length > 0", expression, property, expression, property))
				}
				for _, property := range sortedKeys(variant.AllowedValues) {
					values := variant.AllowedValues[property]
					literals := make([]string, len(values))
					for index, allowedValue := range values {
						literals[index] = fmt.Sprintf("%q", allowedValue)
					}
					conditions = append(conditions, fmt.Sprintf("(!hasOwn(%s, %q) || new Set([%s]).has(%s[%q] as string))", expression, property, strings.Join(literals, ", "), expression, property))
				}
				variants = append(variants, "("+strings.Join(conditions, " && ")+")")
			}
			parts = append(parts, "("+strings.Join(variants, " || ")+")")
		}
		if value.DiffSource {
			parts = append(parts, "hasValidDiffSource("+expression+")")
		}
		if value.OutcomeEnvelope {
			outcome := expression + "[\"outcome\"]"
			diagnostics := expression + "[\"diagnostics\"]"
			parts = append(parts, "(("+outcome+" === \"success\" && hasOwn("+expression+", \"payload\") && !hasOwn("+expression+", \"failure\")) || ("+outcome+" === \"rejected\" && !hasOwn("+expression+", \"payload\") && !hasOwn("+expression+", \"failure\") && isJSONArray("+diagnostics+") && "+diagnostics+".length > 0) || (("+outcome+" === \"failed\" || "+outcome+" === \"cancelled\") && !hasOwn("+expression+", \"payload\") && hasOwn("+expression+", \"failure\")))")
		}
		if value.OrderedRange {
			parts = append(parts, "BigInt("+expression+"[\"start_byte\"]) <= BigInt("+expression+"[\"end_byte\"])")
		}
		if value.OperatorValue != nil {
			values := make([]string, len(value.OperatorValue.Valueless))
			for index, valueless := range value.OperatorValue.Valueless {
				values[index] = fmt.Sprintf("%q", valueless)
			}
			parts = append(parts, fmt.Sprintf("hasOperatorValueRule(%s, %q, %q, new Set([%s]))", expression, value.OperatorValue.Operator, value.OperatorValue.Value, strings.Join(values, ", ")))
		}
		if value.ProtocolOffer {
			parts = append(parts, "hasValidProtocolOffer("+expression+")")
		}
		if value.LimitCapability {
			parts = append(parts, "hasValidLimitCapability("+expression+")")
		}
		for _, rule := range value.UniqueArrayKeys {
			parts = append(parts, fmt.Sprintf("hasUniqueArrayKey(%s, %q, %q)", expression, rule.Array, rule.Property))
		}
		for _, rule := range value.DisjointArrays {
			parts = append(parts, fmt.Sprintf("hasDisjointArrays(%s, %q, %q)", expression, rule.Left, rule.Right))
		}
		for _, rule := range value.DisjointArrayKeys {
			parts = append(parts, fmt.Sprintf("hasDisjointArrayKey(%s, %q, %q, %q)", expression, rule.Array, rule.Property, rule.Strings))
		}
		for _, rule := range value.OrderedPairs {
			parts = append(parts, fmt.Sprintf("hasOrderedPair(%s, %q, %q, %q)", expression, rule.Lower, rule.Upper, rule.Comparison))
		}
		for _, rule := range value.AddressOwners {
			parts = append(parts, fmt.Sprintf("hasAddressOwner(%s, %q, %q, %q)", expression, rule.Owner, rule.Children, rule.Selector))
		}
		if value.AddressTerminalID != nil {
			parts = append(parts, fmt.Sprintf("hasAddressTerminalID(%s, %q, %q)", expression, value.AddressTerminalID.Address, value.AddressTerminalID.ID))
		}
		if value.ExportRecipe {
			parts = append(parts, "hasValidExportRecipe("+expression+")")
		}
		if value.ViewRecipe {
			parts = append(parts, "hasValidViewRecipe("+expression+")")
		}
		return strings.Join(parts, " && "), nil
	default:
		return "", fmt.Errorf("unsupported schema type %q", typeValue)
	}
}

func writeTSDefinition(body *strings.Builder, set schemaSet, document *schemaDocument, name string, definition *schemaType) error {
	typeValue, err := scalarType(definition.Type)
	if err != nil {
		return err
	}
	if typeValue == "object" && len(definition.Properties) != 0 {
		fmt.Fprintf(body, "export interface %s {\n", name)
		required := stringSet(definition.Required)
		for _, propertyName := range sortedKeys(definition.Properties) {
			expression, err := tsType(set, document, definition.Properties[propertyName])
			if err != nil {
				return err
			}
			optional := ""
			if !required[propertyName] {
				optional = "?"
			}
			fmt.Fprintf(body, "  %s%s: %s;\n", propertyName, optional, expression)
		}
		body.WriteString("}\n")
		return nil
	}
	expression, err := tsType(set, document, definition)
	if err != nil {
		return err
	}
	fmt.Fprintf(body, "export type %s = %s;\n", name, expression)
	return nil
}

func tsType(set schemaSet, document *schemaDocument, value *schemaType) (string, error) {
	if value.Ref != "" {
		_, name, err := resolveRef(set, document, value.Ref)
		return name, err
	}
	if len(value.OneOf) != 0 {
		var branches []string
		for _, branch := range value.OneOf {
			expression, err := tsType(set, document, branch)
			if err != nil {
				return "", err
			}
			branches = append(branches, expression)
		}
		return strings.Join(branches, " | "), nil
	}
	typeValue, err := scalarType(value.Type)
	if err != nil {
		return "", err
	}
	switch typeValue {
	case "string":
		if constant, ok := value.Const.(string); ok {
			return fmt.Sprintf("%q", constant), nil
		}
		if len(value.Enum) != 0 {
			values := make([]string, len(value.Enum))
			for i, enumValue := range value.Enum {
				values[i] = fmt.Sprintf("%q", enumValue)
			}
			return strings.Join(values, " | "), nil
		}
		return "string", nil
	case "integer", "number":
		return "number", nil
	case "boolean":
		return "boolean", nil
	case "null":
		return "null", nil
	case "union":
		return "unknown", nil
	case "array":
		item, err := tsType(set, document, value.Items)
		if err != nil {
			return "", err
		}
		return "ReadonlyArray<" + item + ">", nil
	case "object":
		if additional, ok := value.AdditionalProperties.(*schemaType); ok {
			nested, err := tsType(set, document, additional)
			return "{ readonly [key: string]: " + nested + " }", err
		}
		return "Readonly<Record<string, unknown>>", nil
	default:
		return "", fmt.Errorf("unsupported schema type %q", typeValue)
	}
}

func tsImports(set schemaSet, document *schemaDocument) map[string][]string {
	imports := map[string][]string{}
	seen := map[string]bool{}
	var visit func(*schemaType)
	visit = func(value *schemaType) {
		if value == nil {
			return
		}
		if value.Ref != "" {
			target, name, err := resolveRef(set, document, value.Ref)
			key := ""
			if err == nil {
				key = target.Module + ":" + name
			}
			if err == nil && target.ID != document.ID && !seen[key] {
				imports[target.Module] = append(imports[target.Module], name)
				seen[key] = true
			}
		}
		for _, property := range value.Properties {
			visit(property)
		}
		visit(value.Items)
		visit(value.PropertyNames)
		if nested, ok := value.AdditionalProperties.(*schemaType); ok {
			visit(nested)
		}
		for _, branch := range value.OneOf {
			visit(branch)
		}
	}
	for _, definition := range document.Definitions {
		visit(definition)
	}
	return imports
}

func removeStaleGenerated(root string, generated []generatedFile) error {
	wanted := map[string]bool{}
	for _, file := range generated {
		wanted[file.path] = true
	}
	roots := []string{"gen/go", "packages/protocol/src"}
	for _, generatedRoot := range roots {
		absolute := filepath.Join(root, filepath.FromSlash(generatedRoot))
		if _, err := os.Stat(absolute); errors.Is(err, os.ErrNotExist) {
			continue
		}
		err := filepath.WalkDir(absolute, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return walkErr
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if (strings.HasSuffix(relative, ".gen.go") || strings.HasSuffix(relative, ".gen.ts")) && !wanted[relative] {
				return os.Remove(path)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func exportedName(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '_' || r == '-' || r == ':' || r == '.' })
	var result strings.Builder
	for _, part := range parts {
		if initialism := map[string]string{
			"api": "API", "ast": "AST", "html": "HTML", "http": "HTTP", "id": "ID", "ids": "IDs",
			"json": "JSON", "ldl": "LDL", "mcp": "MCP", "sha": "SHA", "sql": "SQL", "ts": "TS",
			"uri": "URI", "url": "URL", "utf": "UTF", "wasm": "WASM",
		}[strings.ToLower(part)]; initialism != "" {
			result.WriteString(initialism)
			continue
		}
		runes := []rune(part)
		if len(runes) == 0 {
			continue
		}
		result.WriteRune(unicode.ToUpper(runes[0]))
		result.WriteString(string(runes[1:]))
	}
	return result.String()
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}
