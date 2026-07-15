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
		"x-layerdraw-canonical-collection-order": {"type": "string", "enum": ["authored_field_path", "authoring_impact", "bounded_text_chunk", "child_set", "conflict", "export_binding", "module_scope", "neighbor", "reference_id", "semantic_diff", "semantic_map_entry", "semantic_reference", "source_asset", "source_binding", "source_diff", "source_file", "source_patch", "source_range", "subgraph", "subject_kind"]},
		"x-layerdraw-canonical-enum-order": {"type": "boolean", "const": true},
		"x-layerdraw-canonical-identifier-order": {"type": "boolean", "const": true},
		"x-layerdraw-child-set": {"type": "boolean", "const": true},
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
		"x-layerdraw-query-parameter": {"type": "boolean", "const": true},
		"x-layerdraw-query-recipe": {"type": "boolean", "const": true},
		"x-layerdraw-recipe-predicate": {"type": "string", "enum": ["predicate", "row"]},
		"x-layerdraw-recipe-scalar": {"type": "boolean", "const": true},
		"x-layerdraw-scalar-unicode": {"type": "boolean", "const": true},
		"x-layerdraw-stable-address-roles": {"type": "array", "items": {"$ref": "#/$defs/stableAddressRoleRule"}, "minItems": 1, "uniqueItems": true},
		"x-layerdraw-stable-address-order": {"type": "string", "description": "For an array, require strict Language 1 StableSymbol order using either $item or the named string property of each item."},
		"x-layerdraw-state-read-order": {"type": "boolean", "const": true},
		"x-layerdraw-protocol-invariant": {"type": "string", "enum": ["apply_input", "apply_result", "authoring_impact", "authoring_impact_entry", "bounded_text_chunk", "document_bound_input", "open_document_result", "paged_result", "preview_result", "semantic_operation", "source_edit"]},
		"x-layerdraw-tagged-union": {"$ref": "#/$defs/taggedUnion"},
		"x-layerdraw-state-read": {"type": "boolean", "const": true},
		"x-layerdraw-ts-module": {"type": "string", "minLength": 1},
		"x-layerdraw-unicode-scalar-order": {"type": "boolean", "const": true},
		"x-layerdraw-unique-array-keys": {"type": "array", "items": {"$ref": "#/$defs/uniqueArrayKey"}},
		"x-layerdraw-view-projection": {"type": "string", "enum": ["composed", "diagram", "flow", "matrix", "tree"]},
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
		"stableAddressRoleRule": {
			"type": "object",
			"properties": {
				"address": {"type": "string", "minLength": 1},
				"addresses": {"type": "string", "minLength": 1},
				"kind": {"type": "string", "minLength": 1},
				"owner": {"type": "string", "minLength": 1},
				"owner_policy": {"type": "string", "enum": ["children", "exact", "if_present", "row_only"]}
			},
			"required": ["kind"],
			"oneOf": [
				{"properties": {"address": true}, "required": ["address"]},
				{"properties": {"addresses": true}, "required": ["addresses"]}
			],
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
				"any_non_empty": {"$ref": "#/$defs/fieldNames"},
				"empty": {"$ref": "#/$defs/fieldNames"},
				"error_diagnostic": {"$ref": "#/$defs/fieldNames"},
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
	Required        []string            `json:"required"`
	Forbidden       []string            `json:"forbidden"`
	Empty           []string            `json:"empty"`
	ErrorDiagnostic []string            `json:"error_diagnostic"`
	NonEmpty        []string            `json:"non_empty"`
	AnyNonEmpty     []string            `json:"any_non_empty"`
	AllowedValues   map[string][]string `json:"allowed_values"`
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

type stableAddressRoleRule struct {
	Kind        string `json:"kind"`
	Address     string `json:"address,omitempty"`
	Addresses   string `json:"addresses,omitempty"`
	Owner       string `json:"owner,omitempty"`
	OwnerPolicy string `json:"owner_policy,omitempty"`
}

type schemaType struct {
	Ref                  string                  `json:"$ref,omitempty"`
	Comment              string                  `json:"$comment,omitempty"`
	Type                 any                     `json:"type,omitempty"`
	Description          string                  `json:"description,omitempty"`
	Enum                 []string                `json:"enum,omitempty"`
	Const                any                     `json:"const,omitempty"`
	Properties           map[string]*schemaType  `json:"properties,omitempty"`
	Required             []string                `json:"required,omitempty"`
	Items                *schemaType             `json:"items,omitempty"`
	PropertyNames        *schemaType             `json:"propertyNames,omitempty"`
	AdditionalProperties any                     `json:"additionalProperties,omitempty"`
	Pattern              string                  `json:"pattern,omitempty"`
	Format               string                  `json:"format,omitempty"`
	Minimum              *float64                `json:"minimum,omitempty"`
	Maximum              *float64                `json:"maximum,omitempty"`
	MinLength            *int                    `json:"minLength,omitempty"`
	MaxLength            *int                    `json:"maxLength,omitempty"`
	MinItems             *int                    `json:"minItems,omitempty"`
	UniqueItems          bool                    `json:"uniqueItems,omitempty"`
	MaxItems             *int                    `json:"maxItems,omitempty"`
	OneOf                []*schemaType           `json:"oneOf,omitempty"`
	TaggedUnion          *taggedUnion            `json:"x-layerdraw-tagged-union,omitempty"`
	OutcomeEnvelope      bool                    `json:"x-layerdraw-outcome-envelope,omitempty"`
	OrderedRange         bool                    `json:"x-layerdraw-ordered-range,omitempty"`
	OperatorValue        *operatorValueRule      `json:"x-layerdraw-operator-value,omitempty"`
	ProtocolOffer        bool                    `json:"x-layerdraw-protocol-offer,omitempty"`
	LimitCapability      bool                    `json:"x-layerdraw-limit-capability,omitempty"`
	UniqueArrayKeys      []uniqueArrayKey        `json:"x-layerdraw-unique-array-keys,omitempty"`
	DisjointArrays       []disjointArrayPair     `json:"x-layerdraw-disjoint-arrays,omitempty"`
	DisjointArrayKeys    []disjointArrayKey      `json:"x-layerdraw-disjoint-array-keys,omitempty"`
	DiffSource           bool                    `json:"x-layerdraw-diff-source,omitempty"`
	StableAddressOrder   string                  `json:"x-layerdraw-stable-address-order,omitempty"`
	CanonicalEnumOrder   bool                    `json:"x-layerdraw-canonical-enum-order,omitempty"`
	CanonicalIDOrder     bool                    `json:"x-layerdraw-canonical-identifier-order,omitempty"`
	CanonicalCollection  string                  `json:"x-layerdraw-canonical-collection-order,omitempty"`
	ChildSet             bool                    `json:"x-layerdraw-child-set,omitempty"`
	UnicodeScalarOrder   bool                    `json:"x-layerdraw-unicode-scalar-order,omitempty"`
	OrderedPairs         []orderedPairRule       `json:"x-layerdraw-ordered-pairs,omitempty"`
	AddressOwners        []addressOwnerRule      `json:"x-layerdraw-address-owners,omitempty"`
	AddressTerminalID    *addressTerminalIDRule  `json:"x-layerdraw-address-terminal-id,omitempty"`
	ExportRecipe         bool                    `json:"x-layerdraw-export-recipe,omitempty"`
	QueryParameter       bool                    `json:"x-layerdraw-query-parameter,omitempty"`
	QueryRecipe          bool                    `json:"x-layerdraw-query-recipe,omitempty"`
	RecipePredicate      string                  `json:"x-layerdraw-recipe-predicate,omitempty"`
	RecipeScalar         bool                    `json:"x-layerdraw-recipe-scalar,omitempty"`
	StableAddressRoles   []stableAddressRoleRule `json:"x-layerdraw-stable-address-roles,omitempty"`
	StateRead            bool                    `json:"x-layerdraw-state-read,omitempty"`
	StateReadOrder       bool                    `json:"x-layerdraw-state-read-order,omitempty"`
	ProtocolInvariant    string                  `json:"x-layerdraw-protocol-invariant,omitempty"`
	ViewProjection       string                  `json:"x-layerdraw-view-projection,omitempty"`
	ViewRecipe           bool                    `json:"x-layerdraw-view-recipe,omitempty"`
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
		if context != "JsonValue" && context != "RelationCardinalityMaximum" {
			return fmt.Errorf("%s uses unsupported oneOf; only the generated JsonValue union and RelationCardinalityMaximum scalar are supported", context)
		}
		if context == "RelationCardinalityMaximum" && !isRelationCardinalityMaximum(value) {
			return fmt.Errorf("RelationCardinalityMaximum must be exactly the JSON integer 1 or string many")
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
	if value.MinLength != nil || value.MaxLength != nil {
		if typeValue != "string" {
			return fmt.Errorf("%s length bounds require string type", context)
		}
		if (value.MinLength != nil && *value.MinLength < 0) || (value.MaxLength != nil && *value.MaxLength < 0) ||
			(value.MinLength != nil && value.MaxLength != nil && *value.MinLength > *value.MaxLength) {
			return fmt.Errorf("%s string length bounds must be non-negative and ordered", context)
		}
	}
	if value.MinItems != nil || value.MaxItems != nil {
		if typeValue != "array" {
			return fmt.Errorf("%s item bounds require array type", context)
		}
		if (value.MinItems != nil && *value.MinItems < 0) || (value.MaxItems != nil && *value.MaxItems < 0) ||
			(value.MinItems != nil && value.MaxItems != nil && *value.MinItems > *value.MaxItems) {
			return fmt.Errorf("%s array item bounds must be non-negative and ordered", context)
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
		if value.ProtocolInvariant != "" {
			allowed := map[string]bool{"apply_input": true, "apply_result": true, "authoring_impact": true, "authoring_impact_entry": true, "bounded_text_chunk": true, "document_bound_input": true, "open_document_result": true, "paged_result": true, "preview_result": true, "semantic_operation": true, "source_edit": true}
			if !allowed[value.ProtocolInvariant] {
				return fmt.Errorf("%s has unknown protocol invariant %q", context, value.ProtocolInvariant)
			}
		}
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
				properties := append(append(append(append(append(append([]string{}, variant.Required...), variant.Forbidden...), variant.Empty...), variant.ErrorDiagnostic...), variant.NonEmpty...), variant.AnyNonEmpty...)
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
				for _, property := range append(append(append(append([]string{}, variant.Empty...), variant.ErrorDiagnostic...), variant.NonEmpty...), variant.AnyNonEmpty...) {
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
		for _, assertion := range []struct {
			enabled    bool
			name       string
			properties []string
		}{
			{value.QueryParameter, "query parameter", []string{"value_type", "default", "enum_values", "reserved_enum_values", "format", "min", "max", "min_length", "max_length"}},
			{value.QueryRecipe, "query recipe", []string{"address", "state_input", "parameters", "select", "where", "relation_where", "traverse", "dependencies"}},
			{value.RecipeScalar, "recipe scalar", []string{"kind", "string_value"}},
			{value.StateRead, "state read", []string{"field_path", "value_type"}},
			{value.ChildSet, "child set", []string{"owner_address", "child_kind", "child_addresses"}},
		} {
			if assertion.enabled {
				for _, property := range assertion.properties {
					if value.Properties[property] == nil {
						return fmt.Errorf("%s %s assertion requires %s", context, assertion.name, property)
					}
				}
			}
		}
		if value.RecipePredicate != "" {
			for _, property := range []string{"kind", "children", "child", "field_path", "operand_type", "operator", "value"} {
				if value.Properties[property] == nil {
					return fmt.Errorf("%s recipe predicate assertion requires %s", context, property)
				}
			}
			if value.RecipePredicate == "predicate" && (value.Properties["field"] == nil || value.Properties["predicate"] == nil || value.Properties["type_addresses"] == nil) {
				return fmt.Errorf("%s full predicate assertion lacks field/rows metadata", context)
			}
			if value.RecipePredicate == "row" && value.Properties["column_addresses"] == nil {
				return fmt.Errorf("%s row predicate assertion lacks column_addresses", context)
			}
		}
		if value.ViewProjection != "" {
			pairs := map[string][]string{
				"composed": {"mode", "parent_endpoint", "child_endpoint", "overlay_endpoint", "badge_endpoint", "target_endpoint"},
				"diagram":  {"source_endpoint", "target_endpoint"},
				"flow":     {"source_endpoint", "target_endpoint"},
				"matrix":   {"row_endpoint", "column_endpoint"},
				"tree":     {"parent_endpoint", "child_endpoint"},
			}
			properties, ok := pairs[value.ViewProjection]
			if !ok {
				return fmt.Errorf("%s has unknown View projection assertion %q", context, value.ViewProjection)
			}
			for _, property := range properties {
				if value.Properties[property] == nil {
					return fmt.Errorf("%s View projection assertion requires %s", context, property)
				}
			}
		}
		for _, rule := range value.StableAddressRoles {
			kind := resolvedType(set, document, value.Properties[rule.Kind])
			if kind == nil || (rule.Address == "") == (rule.Addresses == "") {
				return fmt.Errorf("%s has an invalid stable-address role assertion", context)
			}
			kindType, kindErr := scalarType(kind.Type)
			if kindErr != nil || kindType != "string" {
				return fmt.Errorf("%s stable-address role kind must be a string", context)
			}
			selector := rule.Address
			if selector == "" {
				selector = rule.Addresses
			}
			selected := resolvedType(set, document, value.Properties[selector])
			if selected == nil {
				return fmt.Errorf("%s stable-address role selector %q is absent", context, selector)
			}
			selectedType, selectedErr := scalarType(selected.Type)
			if rule.Addresses != "" {
				item := resolvedType(set, document, selected.Items)
				itemType, itemErr := "", error(nil)
				if item != nil {
					itemType, itemErr = scalarType(item.Type)
				}
				if selectedErr != nil || selectedType != "array" || itemErr != nil || itemType != "string" {
					return fmt.Errorf("%s stable-address plural role must select string items", context)
				}
			} else if selectedErr != nil || selectedType != "string" {
				return fmt.Errorf("%s stable-address role must select a string", context)
			}
			if rule.OwnerPolicy != "" {
				owner := resolvedType(set, document, value.Properties[rule.Owner])
				ownerType, ownerErr := "", error(nil)
				if owner != nil {
					ownerType, ownerErr = scalarType(owner.Type)
				}
				if rule.Owner == "" || ownerErr != nil || ownerType != "string" {
					return fmt.Errorf("%s stable-address owner policy requires a string owner property", context)
				}
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
			itemDocument := document
			if item.Ref != "" {
				target, name, err := resolveRef(set, document, item.Ref)
				if err != nil {
					return err
				}
				item = target.Definitions[name]
				itemDocument = target
			}
			ordered := item
			if value.StableAddressOrder != "$item" {
				itemType, err := scalarType(item.Type)
				if err != nil || itemType != "object" || item.Properties[value.StableAddressOrder] == nil {
					return fmt.Errorf("%s stable-address order selector does not name an item property", context)
				}
				ordered = item.Properties[value.StableAddressOrder]
				if ordered.Ref != "" {
					target, name, err := resolveRef(set, itemDocument, ordered.Ref)
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
		if value.CanonicalCollection != "" {
			item := resolvedType(set, document, value.Items)
			itemType, err := "", error(nil)
			if item != nil {
				itemType, err = scalarType(item.Type)
			}
			if err != nil || itemType != "object" {
				return fmt.Errorf("%s canonical collection order requires object items", context)
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

func isRelationCardinalityMaximum(value *schemaType) bool {
	if value == nil || len(value.OneOf) != 2 {
		return false
	}
	one, many := value.OneOf[0], value.OneOf[1]
	oneType, oneErr := scalarType(one.Type)
	manyType, manyErr := scalarType(many.Type)
	return oneErr == nil && manyErr == nil && oneType == "integer" && one.Minimum != nil && *one.Minimum == 1 && one.Maximum != nil && *one.Maximum == 1 &&
		manyType == "string" && many.Const == "many"
}

func validateExportRecipeAssertionShape(set schemaSet, document *schemaDocument, context string, value *schemaType) error {
	required := stringSet(value.Required)
	for _, property := range []string{"exporter_profile", "extension", "filename", "format", "options", "fidelity", "source_refs", "native_maximum_fidelity", "effective_maximum_fidelity", "fidelity_basis", "requires_source_manifest"} {
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
	for _, property := range []string{"address", "category", "dependencies", "exports", "relation_projection_overrides", "reserved_table_column_ids", "shape", "source", "state_input", "state_requirement"} {
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
	if name == "RelationCardinalityMaximum" && isRelationCardinalityMaximum(definition) {
		body.WriteString("type RelationCardinalityMaximum string\n\nconst (\n\tRelationCardinalityMaximumOne RelationCardinalityMaximum = \"1\"\n\tRelationCardinalityMaximumMany RelationCardinalityMaximum = \"many\"\n)\n")
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
		"net/netip"
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
	if document.Definitions["RelationCardinalityMaximum"] != nil {
		body.WriteString(goRelationCardinalityMaximumRuntime)
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

const goRelationCardinalityMaximumRuntime = `// UnmarshalJSON decodes the deliberately scoped Language 1 cardinality maximum scalar union.
func (value *RelationCardinalityMaximum) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case "1":
		*value = RelationCardinalityMaximumOne
		return nil
	case "\"many\"":
		*value = RelationCardinalityMaximumMany
		return nil
	default:
		return fmt.Errorf("relation cardinality maximum must be JSON integer 1 or string many")
	}
}

// MarshalJSON encodes the deliberately scoped Language 1 cardinality maximum scalar union.
func (value RelationCardinalityMaximum) MarshalJSON() ([]byte, error) {
	switch value {
	case RelationCardinalityMaximumOne:
		return []byte("1"), nil
	case RelationCardinalityMaximumMany:
		return []byte("\"many\""), nil
	default:
		return nil, fmt.Errorf("relation cardinality maximum has invalid value %q", value)
	}
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
		if maximum, ok := schema["maxLength"].(float64); ok && utf8.RuneCountInString(text) > int(maximum) { return fmt.Errorf("%s is too long", path) }
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
		if maximum, ok := schema["maxItems"].(float64); ok && len(items) > int(maximum) { return fmt.Errorf("%s has too many items", path) }
		itemSchema, _ := schema["items"].(map[string]any)
		seenItems := map[string]bool{}
		for index, item := range items {
			if err := validateSchema(documentID, itemSchema, item, fmt.Sprintf("%s[%d]", path, index), depth+1); err != nil { return err }
			if unique, _ := schema["uniqueItems"].(bool); unique {
				encoded, err := json.Marshal(item); if err != nil { return fmt.Errorf("%s contains an unencodable unique item",path) }
				key := string(encoded); if seenItems[key] { return fmt.Errorf("%s repeats an item", path) }; seenItems[key] = true
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
		if ordered, _ := schema["x-layerdraw-state-read-order"].(bool); ordered {
			if err := validateStateReadOrder(path, items); err != nil { return err }
		}
		if profile, _ := schema["x-layerdraw-canonical-collection-order"].(string); profile != "" {
			if err := validateCanonicalCollectionOrder(path,items,profile); err != nil { return err }
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
				failure, ok := object["failure"].(map[string]any); if !ok { return fmt.Errorf("%s %s outcome requires failure", path, outcome) }
				if category, present := failure["workbench_category"].(string); present { if outcome == "cancelled" && category != "cancelled" || outcome == "failed" && category == "cancelled" { return fmt.Errorf("%s outcome contradicts Workbench failure category", path) } }
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
		if rawRules, ok := schema["x-layerdraw-stable-address-roles"].([]any); ok {
			if err := validateStableAddressRoles(path, object, rawRules); err != nil { return err }
		}
		if predicateKind, _ := schema["x-layerdraw-recipe-predicate"].(string); predicateKind != "" {
			if err := validateRecipePredicateConsistency(path, object, predicateKind); err != nil { return err }
		}
		if projectionKind, _ := schema["x-layerdraw-view-projection"].(string); projectionKind != "" {
			if err := validateViewProjectionConsistency(path, object, projectionKind); err != nil { return err }
		}
		if enabled, _ := schema["x-layerdraw-export-recipe"].(bool); enabled {
			if err := validateExportRecipeConsistency(path, object); err != nil { return err }
		}
		if enabled, _ := schema["x-layerdraw-query-parameter"].(bool); enabled {
			if err := validateQueryParameterConsistency(path, object); err != nil { return err }
		}
		if enabled, _ := schema["x-layerdraw-query-recipe"].(bool); enabled {
			if err := validateQueryRecipeConsistency(path, object); err != nil { return err }
		}
		if enabled, _ := schema["x-layerdraw-recipe-scalar"].(bool); enabled {
			if err := validateRecipeScalarConsistency(path, object); err != nil { return err }
		}
		if enabled, _ := schema["x-layerdraw-state-read"].(bool); enabled {
			if err := validateStateReadConsistency(path, object); err != nil { return err }
		}
		if enabled, _ := schema["x-layerdraw-view-recipe"].(bool); enabled {
			if err := validateViewRecipeConsistency(path, object); err != nil { return err }
		}
		if enabled, _ := schema["x-layerdraw-child-set"].(bool); enabled {
			if err := validateChildSetConsistency(path,object); err != nil { return err }
		}
		if profile, _ := schema["x-layerdraw-protocol-invariant"].(string); profile != "" {
			if err := validateProtocolInvariant(path, object, profile); err != nil { return err }
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

func semanticSubjectKindRank(kind string) (int, bool) {
	kinds := []string{"project","pack","entity_type","relation_type","layer","entity","relation","query","view","reference","entity_type_column","entity_type_constraint","relation_type_column","relation_type_constraint","entity_row","relation_row","query_parameter","view_table_column","view_export"}
	for index, candidate := range kinds { if kind == candidate { return index, true } }
	return 0, false
}

func compareText(left, right string) int {
	return compareUnicodeScalars(left, right)
}

func compareStableAddressValues(left, right string) (int, bool) {
	return compareStableAddresses(left, right)
}

func sourceOriginOrder(raw any) (int, string, bool) {
	object, ok := raw.(map[string]any); if !ok { return 0, "", false }
	kind, ok := object["kind"].(string); if !ok { return 0, "", false }
	switch kind {
	case "project": return 0, "", true
	case "pack": address, ok := object["pack_address"].(string); return 1, address, ok
	default: return 0, "", false
	}
}

func compareModuleOrder(left, right map[string]any) (int, bool) {
	leftRank, leftPack, leftOK := sourceOriginOrder(left["origin"])
	rightRank, rightPack, rightOK := sourceOriginOrder(right["origin"])
	leftPath, leftPathOK := left["module_path"].(string); rightPath, rightPathOK := right["module_path"].(string)
	if !leftOK || !rightOK || !leftPathOK || !rightPathOK { return 0, false }
	if leftRank != rightRank { if leftRank < rightRank { return -1, true }; return 1, true }
	if compared := compareText(leftPack,rightPack); compared != 0 { return compared, true }
	return compareText(leftPath,rightPath), true
}

func compareRangePosition(left, right map[string]any) (int, bool) {
	leftStart, leftStartOK := left["start_byte"].(string); rightStart, rightStartOK := right["start_byte"].(string)
	leftEnd, leftEndOK := left["end_byte"].(string); rightEnd, rightEndOK := right["end_byte"].(string)
	if !leftStartOK || !rightStartOK || !leftEndOK || !rightEndOK { return 0, false }
	if compared, ok := compareCanonicalUnsignedDecimals(leftStart,rightStart); !ok { return 0, false } else if compared != 0 { return compared, true }
	return compareCanonicalUnsignedDecimals(leftEnd,rightEnd)
}

func compareCanonicalCollection(profile string, left, right any) (int, bool) {
	a, aOK := left.(map[string]any); b, bOK := right.(map[string]any); if !aOK || !bOK { return 0, false }
	stable := func(property string) (int, bool) { l, lOK := a[property].(string); r, rOK := b[property].(string); if !lOK || !rOK { return 0, false }; return compareStableAddressValues(l,r) }
	text := func(property string) (int, bool) { l, lOK := a[property].(string); r, rOK := b[property].(string); if !lOK || !rOK { return 0, false }; return compareText(l,r), true }
	kind := func(property string) (int, bool) { l, lOK := a[property].(string); r, rOK := b[property].(string); if !lOK || !rOK { return 0, false }; lr, lok := semanticSubjectKindRank(l); rr, rok := semanticSubjectKindRank(r); if !lok || !rok { return 0, false }; if lr < rr { return -1,true }; if lr > rr { return 1,true }; return 0,true }
	rangeValue := func() (int, bool) { l, lOK := a["range"].(map[string]any); r, rOK := b["range"].(map[string]any); if !lOK || !rOK { return 0,false }; return compareRangePosition(l,r) }
	identity := func() (int, bool) { l, _ := a["before_address"].(string); if l == "" { l, _ = a["after_address"].(string) }; r, _ := b["before_address"].(string); if r == "" { r, _ = b["after_address"].(string) }; if l == "" || r == "" { return 0,false }; return compareStableAddressValues(l,r) }
	conflictAddress := func() (int, bool) { l, _ := a["target_address"].(string); if l == "" { l, _ = a["owner_address"].(string) }; r, _ := b["target_address"].(string); if r == "" { r, _ = b["owner_address"].(string) }; if l == "" || r == "" { return compareText(l,r),true }; return compareStableAddressValues(l,r) }
	pathProperty := func(property string) func()(int,bool) { return func() (int, bool) { l, lOK := a[property].([]any); r, rOK := b[property].([]any); if !lOK { l = []any{} }; if !rOK { r = []any{} }; for index := 0; index < len(l) && index < len(r); index++ { ls, lok := l[index].(string); rs, rok := r[index].(string); if !lok || !rok { return 0,false }; if compared := compareText(ls,rs); compared != 0 { return compared,true } }; if len(l) < len(r) { return -1,true }; if len(l) > len(r) { return 1,true }; return 0,true } }
	path := pathProperty("path")
	optionalSourceRange := func() (int, bool) { l, lOK := a["source_range"].(map[string]any); r, rOK := b["source_range"].(map[string]any); if !lOK || !rOK { if lOK { return 1,true }; if rOK { return -1,true }; return 0,true }; return compareRangePosition(l,r) }
	chain := func(comparisons ...func() (int,bool)) (int,bool) { for _, comparison := range comparisons { value, ok := comparison(); if !ok || value != 0 { return value,ok } }; return 0,true }
	switch profile {
	case "authored_field_path": return pathProperty("tokens")()
	case "authoring_impact":
		address := func()(int,bool){ l,_:=a["subject_address"].(string); if l=="" { l,_=a["owner_address"].(string) }; r,_:=b["subject_address"].(string); if r=="" { r,_=b["owner_address"].(string) }; if l=="" || r=="" { return compareText(l,r),true }; return compareStableAddressValues(l,r) }
		return chain(address,func()(int,bool){return text("capability")},func()(int,bool){return text("action")})
	case "bounded_text_chunk":
		address := func()(int,bool){ l,_:=a["address"].(string); if l=="" { l,_=a["owner_address"].(string) }; r,_:=b["address"].(string); if r=="" { r,_=b["owner_address"].(string) }; if l=="" || r=="" { return 0,false }; return compareStableAddressValues(l,r) }
		offset := func()(int,bool){ lc,_:=a["source_chunk"].(map[string]any); if lc==nil { lc,_=a["text_chunk"].(map[string]any) }; rc,_:=b["source_chunk"].(map[string]any); if rc==nil { rc,_=b["text_chunk"].(map[string]any) }; l,lOK:=lc["offset"].(string); r,rOK:=rc["offset"].(string); if !lOK || !rOK{return 0,false}; return compareCanonicalUnsignedDecimals(l,r) }
		return chain(address,offset)
	case "child_set": return chain(func()(int,bool){return stable("owner_address")},func()(int,bool){return kind("child_kind")})
	case "conflict": return chain(conflictAddress,func()(int,bool){return text("kind")},path)
	case "reference_id": return text("id")
	case "subject_kind": return kind("kind")
	case "module_scope":
		leftModule, leftOK := a["module"].(map[string]any); rightModule, rightOK := b["module"].(map[string]any); if !leftOK || !rightOK { return 0,false }; return compareModuleOrder(leftModule,rightModule)
	case "neighbor":
		depth := func()(int,bool){ l,lOK:=a["depth"].(float64); r,rOK:=b["depth"].(float64); if !lOK || !rOK { return 0,false }; if l<r{return -1,true}; if l>r{return 1,true}; return 0,true }
		return chain(func()(int,bool){return stable("source_entity_address")},depth,func()(int,bool){return text("direction")},func()(int,bool){return stable("relation_address")},func()(int,bool){return stable("entity_address")})
	case "source_file": return compareModuleOrder(a,b)
	case "source_patch":
		leftRange, leftOK := a["source_range"].(map[string]any); rightRange, rightOK := b["source_range"].(map[string]any); if !leftOK || !rightOK { return 0,false }
		if compared, ok := compareModuleOrder(leftRange,rightRange); !ok || compared != 0 { return compared,ok }
		return compareRangePosition(leftRange,rightRange)
	case "semantic_diff": return chain(identity,func()(int,bool){return text("kind")})
	case "semantic_map_entry": return text("key")
	case "source_diff":
		module := func(value map[string]any) map[string]any { if sourceRange,ok:=value["source_range"].(map[string]any); ok { return sourceRange }; if before,ok:=value["before_module"].(map[string]any); ok { return before }; after,_:=value["after_module"].(map[string]any); return after }
		primary := func()(int,bool){ l,r:=module(a),module(b); if l==nil||r==nil{return 0,false}; return compareModuleOrder(l,r) }
		after := func()(int,bool){ l,lOK:=a["after_module"].(map[string]any); r,rOK:=b["after_module"].(map[string]any); if !lOK||!rOK {if lOK{return 1,true};if rOK{return -1,true};return 0,true}; return compareModuleOrder(l,r) }
		return chain(primary,func()(int,bool){return text("kind")},optionalSourceRange,after)
	case "source_range":
		if compared,ok:=compareModuleOrder(a,b); !ok || compared!=0 { return compared,ok }; return compareRangePosition(a,b)
	case "subgraph":
		l,lOK:=a["subject"].(map[string]any); r,rOK:=b["subject"].(map[string]any); if !lOK || !rOK { return 0,false }; la,laOK:=l["address"].(string); ra,raOK:=r["address"].(string); if !laOK || !raOK { return 0,false }; return compareStableAddressValues(la,ra)
	case "source_asset": return chain(func()(int,bool){return stable("subject_address")},func()(int,bool){return text("locator")})
	case "semantic_reference": return chain(func()(int,bool){return stable("source_address")},rangeValue,func()(int,bool){return stable("target_address")},func()(int,bool){return kind("target_kind")},func()(int,bool){return text("via")})
	case "source_binding":
		owner := func()(int,bool){ l, _ := a["target_owner_address"].(string); r, _ := b["target_owner_address"].(string); if l == "" || r == "" { return compareText(l,r),true }; return compareStableAddressValues(l,r) }
		return chain(func()(int,bool){return stable("source_address")},rangeValue,func()(int,bool){return stable("target_address")},func()(int,bool){return kind("target_kind")},owner,func()(int,bool){return text("via")})
	case "export_binding":
		module := func()(int,bool){ l,lOK := a["module"].(map[string]any); r,rOK := b["module"].(map[string]any); if !lOK || !rOK { return 0,false }; return compareModuleOrder(l,r) }
		boolean := func()(int,bool){ l,lOK := a["re_export"].(bool); r,rOK := b["re_export"].(bool); if !lOK || !rOK { return 0,false }; if l == r { return 0,true }; if !l { return -1,true }; return 1,true }
		return chain(module,rangeValue,func()(int,bool){return text("public_name")},func()(int,bool){return stable("target_address")},boolean)
	default: return 0,false
	}
}

func validateCanonicalCollectionOrder(path string, items []any, profile string) error {
	for index := 1; index < len(items); index++ {
		comparison, ok := compareCanonicalCollection(profile,items[index-1],items[index]); if !ok || comparison >= 0 { return fmt.Errorf("%s is not in strict %s order",path,profile) }
		if profile == "source_patch" {
			left, leftOK := items[index-1].(map[string]any); right, rightOK := items[index].(map[string]any); leftRange, leftRangeOK := left["source_range"].(map[string]any); rightRange, rightRangeOK := right["source_range"].(map[string]any)
			if !leftOK || !rightOK || !leftRangeOK || !rightRangeOK { return fmt.Errorf("%s contains an invalid source patch",path) }
			if moduleComparison, moduleOK := compareModuleOrder(leftRange,rightRange); moduleOK && moduleComparison == 0 {
				leftEnd, leftEndOK := leftRange["end_byte"].(string); rightStart, rightStartOK := rightRange["start_byte"].(string); overlap, decimalsOK := compareCanonicalUnsignedDecimals(leftEnd,rightStart)
				if !leftEndOK || !rightStartOK || !decimalsOK || overlap > 0 { return fmt.Errorf("%s contains overlapping source patches",path) }
			}
		}
	}
	return nil
}

func validateChildSetConsistency(path string, object map[string]any) error {
	owner, ownerOK := object["owner_address"].(string); childKind, childOK := object["child_kind"].(string)
	ownerKind, _, valid := stableAddressSubject(owner); if !ownerOK || !childOK || !valid { return fmt.Errorf("%s has invalid ChildSet authority",path) }
	allowed := map[string]map[string]bool{
		"project":{"entity_type":true,"relation_type":true,"layer":true,"entity":true,"relation":true,"query":true,"view":true,"reference":true},
		"pack":{"entity_type":true,"relation_type":true,"query":true,"view":true,"reference":true},
		"entity_type":{"entity_type_column":true,"entity_type_constraint":true},
		"relation_type":{"relation_type_column":true,"relation_type_constraint":true},
		"entity":{"entity_row":true},"relation":{"relation_row":true},"query":{"query_parameter":true},"view":{"view_table_column":true,"view_export":true},
	}
	if !allowed[ownerKind][childKind] { return fmt.Errorf("%s has an impossible owner/child kind pair",path) }
	return nil
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
		items, itemsOK := object[arrayProperty].([]any); if _, exists := object[arrayProperty]; !exists { items, itemsOK = []any{}, true }
		stringsArray, stringsOK := object[stringsProperty].([]any); if _, exists := object[stringsProperty]; !exists { stringsArray, stringsOK = []any{}, true }
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

func validateRecipeScalarConsistency(path string, object map[string]any) error {
	kind, _ := object["kind"].(string)
	text, _ := object["string_value"].(string)
	switch kind {
	case "date":
		if !regexp.MustCompile(` + "`" + `^[0-9]{4}-[0-9]{2}-[0-9]{2}$` + "`" + `).MatchString(text) || strings.HasPrefix(text, "0000-") { return fmt.Errorf("%s contains an invalid date", path) }
		if _, err := time.Parse("2006-01-02", text); err != nil { return fmt.Errorf("%s contains an invalid date", path) }
	case "datetime":
		if strings.HasPrefix(text, "0000-") || !regexp.MustCompile(` + "`" + `^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,3})?Z$` + "`" + `).MatchString(text) { return fmt.Errorf("%s contains an invalid datetime", path) }
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if err != nil || parsed.UTC().Format(time.RFC3339Nano) != text { return fmt.Errorf("%s contains a non-canonical datetime", path) }
	case "enum":
		if text == "" { return fmt.Errorf("%s contains an empty enum value", path) }
	}
	return nil
}

func validCanonicalHostname(value string) bool {
	if value == "" || len(value) > 253 || value != strings.ToLower(value) || strings.HasSuffix(value, ".") { return false }
	for _, label := range strings.Split(value, ".") { if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' || !regexp.MustCompile(` + "`" + `^[a-z0-9-]+$` + "`" + `).MatchString(label) { return false } }
	return true
}

func canonicalURIAlpha(ch byte) bool { return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' }
func canonicalURIDigit(ch byte) bool { return ch >= '0' && ch <= '9' }
func canonicalURIHex(ch byte) bool { return canonicalURIDigit(ch) || ch >= 'A' && ch <= 'F' || ch >= 'a' && ch <= 'f' }
func canonicalURIUnreserved(ch byte) bool { return canonicalURIAlpha(ch) || canonicalURIDigit(ch) || strings.ContainsRune("-._~",rune(ch)) }
func canonicalURICharacter(ch byte) bool { return canonicalURIUnreserved(ch) || strings.ContainsRune(":/?#[]@!$&'()*+,;=%",rune(ch)) }
func canonicalURIScheme(value string) bool { if value == "" || !canonicalURIAlpha(value[0]) { return false }; for index := 1; index < len(value); index++ { if !canonicalURIAlpha(value[index]) && !canonicalURIDigit(value[index]) && !strings.ContainsRune("+.-",rune(value[index])) { return false } }; return true }
func canonicalURIAllDigits(value string) bool { for index := range len(value) { if !canonicalURIDigit(value[index]) { return false } }; return true }
func canonicalURIComponent(value string, allowEmpty bool, extra string) bool { if value == "" { return allowEmpty }; for index := 0; index < len(value); index++ { if value[index] == '%' { if index+2 >= len(value) || !canonicalURIHex(value[index+1]) || !canonicalURIHex(value[index+2]) { return false }; index += 2; continue }; if !canonicalURIUnreserved(value[index]) && !strings.ContainsRune("!$&'()*+,;="+extra,rune(value[index])) { return false } }; return true }
func canonicalIPLiteral(value string) bool { if address, err := netip.ParseAddr(value); err == nil { return address.Is6() && address.Zone() == "" }; if len(value) < 4 || value[0] != 'v' && value[0] != 'V' { return false }; dot := strings.IndexByte(value,'.'); if dot < 2 { return false }; for index := 1; index < dot; index++ { if !canonicalURIHex(value[index]) { return false } }; return value[dot+1:] != "" && canonicalURIComponent(value[dot+1:],false,":") }
func canonicalURIAuthority(value string) bool { if strings.Count(value,"@") > 1 { return false }; hostPort := value; if userInfo, rest, ok := strings.Cut(value,"@"); ok { if !canonicalURIComponent(userInfo,true,":") { return false }; hostPort = rest }; if strings.HasPrefix(hostPort,"[") { close := strings.IndexByte(hostPort,']'); if close <= 1 { return false }; literal, rest := hostPort[1:close],hostPort[close+1:]; return canonicalIPLiteral(literal) && (rest == "" || strings.HasPrefix(rest,":") && canonicalURIAllDigits(rest[1:])) }; if strings.ContainsAny(hostPort,"[]") { return false }; host := hostPort; if colon := strings.LastIndexByte(hostPort,':'); colon >= 0 { host = hostPort[:colon]; if strings.Contains(host,":") || !canonicalURIAllDigits(hostPort[colon+1:]) { return false } }; return canonicalURIComponent(host,true,"") }
func validCanonicalAbsoluteURI(value string) bool {
	colon := strings.IndexByte(value,':'); if colon <= 0 || !canonicalURIScheme(value[:colon]) || !utf8.ValidString(value) || strings.Contains(value,"\\") { return false }
	for index := 0; index < len(value); index++ { if value[index] >= utf8.RuneSelf || value[index] < 0x20 || value[index] == 0x7f || !canonicalURICharacter(value[index]) { return false }; if value[index] == '%' { if index+2 >= len(value) || !canonicalURIHex(value[index+1]) || !canonicalURIHex(value[index+2]) { return false }; index += 2 } }
	remainder := value[colon+1:]; if strings.Count(remainder,"#") > 1 { return false }; hierarchicalAndQuery, fragment, hasFragment := strings.Cut(remainder,"#"); if hasFragment && !canonicalURIComponent(fragment,true,"/?:@") { return false }; hierarchical, query, hasQuery := strings.Cut(hierarchicalAndQuery,"?"); if hasQuery && !canonicalURIComponent(query,true,"/?:@") { return false }; if strings.HasPrefix(hierarchical,"//") { authorityAndPath := hierarchical[2:]; pathStart := strings.IndexByte(authorityAndPath,'/'); authority, path := authorityAndPath,""; if pathStart >= 0 { authority,path = authorityAndPath[:pathStart],authorityAndPath[pathStart:] }; return canonicalURIAuthority(authority) && canonicalURIComponent(path,true,"/:@") }; return canonicalURIComponent(hierarchical,true,"/:@")
}

func validCanonicalStringFormat(format, value string) bool {
	switch format {
	case "hostname": return validCanonicalHostname(value)
	case "email": match := regexp.MustCompile("^[A-Za-z0-9!#$%&'*+/=?^_\\x60{|}~-]+(?:\\.[A-Za-z0-9!#$%&'*+/=?^_\\x60{|}~-]+)*@([A-Za-z0-9.-]+)$").FindStringSubmatch(value); return match != nil && validCanonicalHostname(strings.ToLower(match[1]))
	case "ipv4": address, err := netip.ParseAddr(value); return err == nil && address.Is4() && address.String() == value
	case "ipv6": address, err := netip.ParseAddr(value); return err == nil && address.Is6() && address.Zone() == "" && address.String() == value
	case "cidr": prefix, err := netip.ParsePrefix(value); return err == nil && prefix == prefix.Masked() && prefix.String() == value
	case "uri": return validCanonicalAbsoluteURI(value)
	}
	return false
}

func validateQueryParameterConsistency(path string, object map[string]any) error {
	valueType, _ := object["value_type"].(string)
	_, hasEnum := object["enum_values"]
	reserved, _ := object["reserved_enum_values"].([]any)
	_, hasFormat := object["format"]
	_, hasMin := object["min"]
	_, hasMax := object["max"]
	_, hasMinLength := object["min_length"]
	_, hasMaxLength := object["max_length"]
	if valueType == "enum" {
		values, ok := object["enum_values"].([]any)
		if !ok || len(values) == 0 { return fmt.Errorf("%s enum requires non-empty enum_values", path) }
		for _, raw := range append(append([]any{}, values...), reserved...) { if text, ok := raw.(string); !ok || text == "" { return fmt.Errorf("%s enum values must be non-empty strings", path) } }
	} else if hasEnum || len(reserved) != 0 {
		return fmt.Errorf("%s enum values are forbidden for %s", path, valueType)
	}
	if hasFormat && valueType != "string" { return fmt.Errorf("%s format is only valid for string parameters", path) }
	if (hasMin || hasMax) && valueType != "integer" && valueType != "number" { return fmt.Errorf("%s numeric bounds are incompatible with %s", path, valueType) }
	if (hasMinLength || hasMaxLength) && valueType != "string" { return fmt.Errorf("%s length bounds are only valid for string parameters", path) }
	if valueType == "integer" {
		for _, property := range []string{"min", "max"} { if raw, ok := object[property].(string); ok { value, err := strconv.ParseInt(raw, 10, 64); if err != nil || value < -9007199254740991 || value > 9007199254740991 { return fmt.Errorf("%s.%s must be a safe integer bound", path, property) } } }
	}
	defaultValue, hasDefault := object["default"].(map[string]any)
	if !hasDefault { return nil }
	if defaultValue["kind"] != valueType { return fmt.Errorf("%s default type does not match value_type", path) }
	if valueType == "enum" {
		text, _ := defaultValue["string_value"].(string)
		active := false
		for _, raw := range object["enum_values"].([]any) { if raw == text { active = true } }
		if !active { return fmt.Errorf("%s enum default is not an active enum value", path) }
	}
	if valueType == "string" {
		text, _ := defaultValue["string_value"].(string)
		length := int64(utf8.RuneCountInString(text))
		if raw, ok := object["min_length"].(string); ok { minimum, _ := strconv.ParseInt(raw, 10, 64); if length < minimum { return fmt.Errorf("%s default is shorter than min_length", path) } }
		if raw, ok := object["max_length"].(string); ok { maximum, _ := strconv.ParseInt(raw, 10, 64); if length > maximum { return fmt.Errorf("%s default is longer than max_length", path) } }
		if format, ok := object["format"].(string); ok && !validCanonicalStringFormat(format, text) { return fmt.Errorf("%s default does not satisfy its canonical string format", path) }
	}
	if valueType == "integer" || valueType == "number" {
		property := "number_value"; if valueType == "integer" { property = "integer_value" }
		text, _ := defaultValue[property].(string); number, err := strconv.ParseFloat(text, 64)
		if err != nil { return fmt.Errorf("%s has an invalid numeric default", path) }
		if raw, ok := object["min"].(string); ok { minimum, _ := strconv.ParseFloat(raw, 64); if number < minimum { return fmt.Errorf("%s default is below min", path) } }
		if raw, ok := object["max"].(string); ok { maximum, _ := strconv.ParseFloat(raw, 64); if number > maximum { return fmt.Errorf("%s default is above max", path) } }
	}
	return nil
}

func predicateHasState(raw any) bool {
	object, ok := raw.(map[string]any); if !ok { return false }
	if object["kind"] == "state" { return true }
	if child, ok := object["child"]; ok && predicateHasState(child) { return true }
	if predicate, ok := object["predicate"]; ok && predicateHasState(predicate) { return true }
	if children, ok := object["children"].([]any); ok { for _, child := range children { if predicateHasState(child) { return true } } }
	return false
}

func contextFieldOperand(field, context string) (recipeOperand, bool) {
	switch field {
	case "id", "display_name", "description": return recipeOperand{kind:"scalar", scalarType:"string"}, true
	case "tags": return recipeOperand{kind:"string_set"}, true
	case "layer": if context == "entity" { return recipeOperand{kind:"address", addressKind:"layer"}, true }
	case "from", "to": if context == "relation" { return recipeOperand{kind:"address", addressKind:"entity"}, true }
	}
	switch field {
	case "address": if context == "entity" { return recipeOperand{kind:"address", addressKind:"entity"}, true }; if context == "relation" { return recipeOperand{kind:"address", addressKind:"relation"}, true }
	case "type": if context == "entity" { return recipeOperand{kind:"address", addressKind:"entity_type"}, true }; if context == "relation" { return recipeOperand{kind:"address", addressKind:"relation_type"}, true }
	}
	return recipeOperand{}, false
}

type queryDependencySets struct {
	layer, entityType, relationType, entity, relation, column, parameter map[string]bool
	state map[string]map[string]any
}

func newQueryDependencySets() queryDependencySets { return queryDependencySets{map[string]bool{},map[string]bool{},map[string]bool{},map[string]bool{},map[string]bool{},map[string]bool{},map[string]bool{},map[string]map[string]any{}} }

func (sets *queryDependencySets) addAddress(kind, address string) bool {
	target := map[string]map[string]bool{"layer":sets.layer,"entity_type":sets.entityType,"relation_type":sets.relationType,"entity":sets.entity,"relation":sets.relation,"entity_type_column":sets.column,"relation_type_column":sets.column,"query_parameter":sets.parameter}[kind]
	if target == nil { return false }; target[address] = true; return true
}

func validateQueryPredicateTree(raw any, context, queryAddress string, parameters map[string]string, sets *queryDependencySets) bool {
	object, ok := raw.(map[string]any); if !ok { return false }
	kind, _ := object["kind"].(string)
	switch kind {
	case "all", "any": children, ok := object["children"].([]any); if !ok { return false }; for _, child := range children { if !validateQueryPredicateTree(child, context, queryAddress, parameters, sets) { return false } }; return true
	case "not": return validateQueryPredicateTree(object["child"], context, queryAddress, parameters, sets)
	case "rows":
		types, ok := object["type_addresses"].([]any); if !ok { return false }
		expectedKind, rowContext := "entity_type", "entity_row"; if context == "relation" { expectedKind, rowContext = "relation_type", "relation_row" }
		for _, rawAddress := range types { address, ok := rawAddress.(string); actual, _, valid := stableAddressSubject(address); if !ok || !valid || actual != expectedKind { return false }; sets.addAddress(actual, address) }
		return validateQueryPredicateTree(object["predicate"], rowContext, queryAddress, parameters, sets)
	case "field":
		field, _ := object["field"].(string); expected, valid := contextFieldOperand(field, context); operand, operandOK := parseRecipeOperand(object["operand_type"]); if !valid || !operandOK || operand != expected { return false }
	case "cell":
		if context != "entity_row" && context != "relation_row" { return false }
		addresses, ok := object["column_addresses"].([]any); if !ok || len(addresses) == 0 { return false }
		expectedKind := "entity_type_column"; if context == "relation_row" { expectedKind = "relation_type_column" }
		for _, rawAddress := range addresses { address, ok := rawAddress.(string); actual, _, valid := stableAddressSubject(address); if !ok || !valid || actual != expectedKind { return false }; sets.column[address] = true }
	case "state":
		field, _ := object["field_path"].(string); valueType := stateFieldValueType(field); operand, operandOK := parseRecipeOperand(object["operand_type"]); if !operandOK || operand != (recipeOperand{kind:"scalar", scalarType:valueType}) { return false }
		key := context+"\x00"+field; sets.state[key] = map[string]any{"subject_kind":context,"field_path":field,"value_type":valueType}
	default: return false
	}
	operand, _ := parseRecipeOperand(object["operand_type"])
	value, hasValue := object["value"].(map[string]any)
	if hasValue {
		if value["kind"] == "parameter" {
			address, ok := value["parameter_address"].(string); actualKind, owner, valid := stableAddressSubject(address); expectedType := operand.scalarType; if operand.kind == "string_set" { expectedType = "string" }
			if !ok || !valid || actualKind != "query_parameter" || owner != queryAddress || parameters[address] != expectedType { return false }; sets.parameter[address] = true
		}
		for _, property := range []string{"address_value"} { if address, ok := value[property].(string); ok { actual, _, valid := stableAddressSubject(address); if !valid || !sets.addAddress(actual, address) { return false } } }
		if values, ok := value["address_values"].([]any); ok { for _, rawAddress := range values { address, ok := rawAddress.(string); actual, _, valid := stableAddressSubject(address); if !ok || !valid || !sets.addAddress(actual, address) { return false } } }
	}
	return true
}

func dependencyArrayEquals(raw any, expected map[string]bool) bool {
	values, ok := raw.([]any); if !ok || len(values) != len(expected) { return false }
	for _, rawValue := range values { value, ok := rawValue.(string); if !ok || !expected[value] { return false } }
	return true
}

func validateQueryRecipeConsistency(path string, object map[string]any) error {
	queryAddress, _ := object["address"].(string)
	parameters := map[string]string{}
	if values, ok := object["parameters"].([]any); ok { for _, raw := range values { value, ok := raw.(map[string]any); address, addressOK := value["address"].(string); valueType, typeOK := value["value_type"].(string); if !ok || !addressOK || !typeOK { return fmt.Errorf("%s has an invalid parameter", path) }; parameters[address] = valueType } }
	sets := newQueryDependencySets()
	selectValue, _ := object["select"].(map[string]any)
	for property, kind := range map[string]string{"layer_addresses":"layer","entity_type_addresses":"entity_type","relation_type_addresses":"relation_type","root_addresses":"entity"} { if values, ok := selectValue[property].([]any); ok { for _, raw := range values { address, ok := raw.(string); if !ok || !sets.addAddress(kind, address) { return fmt.Errorf("%s has an invalid select dependency", path) } } } }
	if traversal, ok := object["traverse"].(map[string]any); ok { if values, ok := traversal["relation_type_addresses"].([]any); ok { for _, raw := range values { address, ok := raw.(string); if !ok { return fmt.Errorf("%s has an invalid traversal dependency", path) }; sets.relationType[address] = true } } }
	where, whereOK := object["where"].(map[string]any); relationWhere, relationOK := object["relation_where"].(map[string]any)
	if !whereOK || !relationOK || !validateQueryPredicateTree(where, "entity", queryAddress, parameters, &sets) || !validateQueryPredicateTree(relationWhere, "relation", queryAddress, parameters, &sets) { return fmt.Errorf("%s contains an invalid typed predicate tree", path) }
	hasState := len(sets.state) != 0
	stateInput, _ := object["state_input"].(string)
	if hasState != (stateInput == "optional" || stateInput == "required") { return fmt.Errorf("%s state_input and state predicates are inconsistent", path) }
	dependencies, ok := object["dependencies"].(map[string]any); if !ok { return fmt.Errorf("%s lacks dependency authority", path) }
	for property, expected := range map[string]map[string]bool{"layer_addresses":sets.layer,"entity_type_addresses":sets.entityType,"relation_type_addresses":sets.relationType,"entity_addresses":sets.entity,"relation_addresses":sets.relation,"column_addresses":sets.column,"parameter_addresses":sets.parameter} { if !dependencyArrayEquals(dependencies[property], expected) { return fmt.Errorf("%s.dependencies.%s is not the exact predicate/select closure", path, property) } }
	stateReads, ok := dependencies["state_reads"].([]any); if !ok || len(stateReads) != len(sets.state) { return fmt.Errorf("%s.dependencies.state_reads is not the exact state-read closure", path) }
	for _, raw := range stateReads { value, ok := raw.(map[string]any); if !ok || sets.state[fmt.Sprint(value["subject_kind"])+"\x00"+fmt.Sprint(value["field_path"])] == nil { return fmt.Errorf("%s.dependencies.state_reads is not the exact state-read closure", path) } }
	return nil
}

func stateFieldValueType(path string) string {
	switch path {
	case "system.created_at", "system.updated_at", "provenance.observed_at", "provenance.verified_at", "provenance.stale_after": return "datetime"
	case "system.created_by.kind", "system.updated_by.kind", "provenance.source.kind", "provenance.verified_by.kind": return "enum"
	case "provenance.confidence": return "number"
	default: return "string"
	}
}

func validateStateReadConsistency(path string, object map[string]any) error {
	field, _ := object["field_path"].(string); valueType, _ := object["value_type"].(string)
	if valueType != stateFieldValueType(field) { return fmt.Errorf("%s value_type does not match the state-field registry", path) }
	return nil
}

func validateStateReadOrder(path string, values []any) error {
	subjectRanks := map[string]int{"entity":0, "relation":1, "entity_row":2, "relation_row":3}
	fieldRanks := map[string]int{
		"system.created_at":0, "system.updated_at":1, "system.created_by.kind":2, "system.created_by.id":3,
		"system.created_by.display_name":4, "system.updated_by.kind":5, "system.updated_by.id":6,
		"system.updated_by.display_name":7, "system.created_revision":8, "system.updated_revision":9,
		"provenance.source.kind":10, "provenance.source.label":11, "provenance.source.uri":12,
		"provenance.source.external_id":13, "provenance.observed_at":14, "provenance.verified_at":15,
		"provenance.stale_after":16, "provenance.verified_by.kind":17, "provenance.verified_by.id":18,
		"provenance.verified_by.display_name":19, "provenance.confidence":20,
	}
	previous := -1
	for _, raw := range values {
		value, ok := raw.(map[string]any); if !ok { return fmt.Errorf("%s contains a non-object state read", path) }
		subject, subjectOK := value["subject_kind"].(string); field, fieldOK := value["field_path"].(string)
		subjectRank, rankedSubject := subjectRanks[subject]; fieldRank, rankedField := fieldRanks[field]
		if !subjectOK || !fieldOK || !rankedSubject || !rankedField || value["value_type"] != stateFieldValueType(field) { return fmt.Errorf("%s contains an invalid state read", path) }
		rank := subjectRank*len(fieldRanks)+fieldRank
		if rank <= previous { return fmt.Errorf("%s is not in strict canonical state-read order", path) }
		previous = rank
	}
	return nil
}

func stableAddressSubject(value string) (string, string, bool) {
	parts := strings.Split(value, ":")
	if len(parts) < 3 || parts[0] != "ldl" { return "", "", false }
	start, rootKind := 3, "project"
	if parts[1] == "pack" { if len(parts) < 4 { return "", "", false }; start, rootKind = 4, "pack" } else if parts[1] != "project" { return "", "", false }
	if len(parts) == start { return rootKind, "", true }
	if (len(parts)-start)%2 != 0 { return "", "", false }
	last := parts[len(parts)-2]
	parent := ""
	if len(parts) >= start+2 { parent = strings.Join(parts[:len(parts)-2], ":") }
	kind := map[string]string{"entity-type":"entity_type", "relation-type":"relation_type", "layer":"layer", "entity":"entity", "relation":"relation", "query":"query", "view":"view", "reference":"reference", "parameter":"query_parameter", "table-column":"view_table_column", "export":"view_export"}[last]
	if last == "row" {
		if len(parts) < start+4 { return "", "", false }
		switch parts[len(parts)-4] { case "entity": kind = "entity_row"; case "relation": kind = "relation_row" }
	}
	if last == "column" || last == "constraint" {
		if len(parts) < start+4 { return "", "", false }
		prefix := map[string]string{"entity-type":"entity_type", "relation-type":"relation_type"}[parts[len(parts)-4]]
		if prefix != "" { kind = prefix+"_"+last }
	}
	if kind == "" { return "", "", false }
	return kind, parent, true
}

func validateStableAddressRoles(path string, object map[string]any, rules []any) error {
	for _, rawRule := range rules {
		rule, ok := rawRule.(map[string]any); if !ok { return fmt.Errorf("%s has an invalid stable-address role rule", path) }
		kindProperty, _ := rule["kind"].(string); expectedKind, ok := object[kindProperty].(string); if !ok { return fmt.Errorf("%s.%s must name a subject kind", path, kindProperty) }
		var values []string
		if property, _ := rule["address"].(string); property != "" { value, ok := object[property].(string); if !ok { return fmt.Errorf("%s.%s must be a StableAddress", path, property) }; values = []string{value} } else {
			property, _ := rule["addresses"].(string); rawValues, ok := object[property].([]any); if !ok { return fmt.Errorf("%s.%s must be StableAddresses", path, property) }
			for _, raw := range rawValues { value, ok := raw.(string); if !ok { return fmt.Errorf("%s.%s contains a non-address", path, property) }; values = append(values, value) }
		}
		ownerProperty, _ := rule["owner"].(string); policy, _ := rule["owner_policy"].(string); owner, ownerPresent := object[ownerProperty].(string)
		for _, value := range values {
			actualKind, actualOwner, valid := stableAddressSubject(value)
			if !valid || actualKind != expectedKind { return fmt.Errorf("%s address %q does not match kind %q", path, value, expectedKind) }
			switch policy {
			case "children": if !ownerPresent || actualOwner != owner { return fmt.Errorf("%s address %q is not a direct child of %q", path, value, owner) }
			case "exact": if (actualOwner != "") != ownerPresent || ownerPresent && actualOwner != owner { return fmt.Errorf("%s has an inexact owner for %q", path, value) }
			case "if_present": if ownerPresent && actualOwner != owner { return fmt.Errorf("%s has a wrong owner for %q", path, value) }
			case "row_only": required := actualKind == "entity_row" || actualKind == "relation_row"; if required != ownerPresent || ownerPresent && actualOwner != owner { return fmt.Errorf("%s has an invalid row owner for %q", path, value) }
			case "":
			default: return fmt.Errorf("%s has an unknown stable-address owner policy", path)
			}
		}
	}
	return nil
}

type recipeOperand struct { kind, scalarType, addressKind string }

func parseRecipeOperand(raw any) (recipeOperand, bool) {
	value, ok := raw.(map[string]any); if !ok { return recipeOperand{}, false }
	operand := recipeOperand{}
	operand.kind, _ = value["kind"].(string); operand.scalarType, _ = value["scalar_type"].(string); operand.addressKind, _ = value["address_kind"].(string)
	switch operand.kind { case "scalar": return operand, operand.scalarType != ""; case "address": return operand, operand.addressKind != ""; case "string_set": return operand, true }
	return recipeOperand{}, false
}

func equalRecipeOperand(left, right recipeOperand) bool { return left == right }

func fieldRecipeOperand(field string) (recipeOperand, bool) {
	switch field {
	case "id", "display_name", "description": return recipeOperand{kind:"scalar", scalarType:"string"}, true
	case "tags": return recipeOperand{kind:"string_set"}, true
	case "layer": return recipeOperand{kind:"address", addressKind:"layer"}, true
	case "from", "to": return recipeOperand{kind:"address", addressKind:"entity"}, true
	}
	return recipeOperand{}, false
}

func recipeScalarKind(raw any) (string, bool) { value, ok := raw.(map[string]any); if !ok { return "", false }; kind, ok := value["kind"].(string); return kind, ok }

func compareRecipeScalars(left, right map[string]any) int {
	kind, _ := left["kind"].(string)
	switch kind {
	case "boolean": a, _ := left["boolean_value"].(bool); b, _ := right["boolean_value"].(bool); if a == b { return 0 }; if !a { return -1 }; return 1
	case "integer": a, _ := strconv.ParseInt(fmt.Sprint(left["integer_value"]), 10, 64); b, _ := strconv.ParseInt(fmt.Sprint(right["integer_value"]), 10, 64); if a < b { return -1 }; if a > b { return 1 }; return 0
	case "number": a, _ := strconv.ParseFloat(fmt.Sprint(left["number_value"]), 64); b, _ := strconv.ParseFloat(fmt.Sprint(right["number_value"]), 64); if a < b { return -1 }; if a > b { return 1 }; return 0
	default: return strings.Compare(fmt.Sprint(left["string_value"]), fmt.Sprint(right["string_value"]))
	}
}

func validateRecipeScalarSet(raw any, scalarType string) bool {
	values, ok := raw.([]any); if !ok { return false }
	for _, rawValue := range values {
		value, ok := rawValue.(map[string]any); if !ok || value["kind"] != scalarType { return false }
	}
	return true
}

func validateRecipePredicateValue(value map[string]any, operator string, operand recipeOperand) bool {
	kind, _ := value["kind"].(string)
	if kind == "parameter" { return operator != "in" && operator != "not_in" && (operand.kind == "scalar" || operand.kind == "string_set" && operator == "contains") }
	if operator == "in" || operator == "not_in" {
		if operand.kind == "scalar" { return kind == "scalar_set" && validateRecipeScalarSet(value["scalar_values"], operand.scalarType) }
		if operand.kind == "address" && kind == "address_set" { values, ok := value["address_values"].([]any); if !ok { return false }; for _, raw := range values { address, ok := raw.(string); actual, _, valid := stableAddressSubject(address); if !ok || !valid || actual != operand.addressKind { return false } }; return true }
		return false
	}
	if operand.kind == "string_set" {
		if operator == "eq" || operator == "ne" { return kind == "scalar_set" && validateRecipeScalarSet(value["scalar_values"], "string") }
		return operator == "contains" && kind == "scalar" && value["scalar_value"] != nil && func() bool { scalar, ok := recipeScalarKind(value["scalar_value"]); return ok && scalar == "string" }()
	}
	if operand.kind == "scalar" { scalar, ok := recipeScalarKind(value["scalar_value"]); return kind == "scalar" && ok && scalar == operand.scalarType }
	if operand.kind == "address" && kind == "address" { address, ok := value["address_value"].(string); actual, _, valid := stableAddressSubject(address); return ok && valid && actual == operand.addressKind }
	return false
}

func validateRecipePredicateConsistency(path string, object map[string]any, predicateKind string) error {
	kind, _ := object["kind"].(string)
	if kind == "all" || kind == "any" { children, _ := object["children"].([]any); for index, raw := range children { child, ok := raw.(map[string]any); if !ok { return fmt.Errorf("%s.children[%d] is invalid", path, index) }; if err := validateRecipePredicateConsistency(fmt.Sprintf("%s.children[%d]", path, index), child, predicateKind); err != nil { return err } }; return nil }
	if kind == "not" { child, _ := object["child"].(map[string]any); return validateRecipePredicateConsistency(path+".child", child, predicateKind) }
	if kind == "rows" { child, _ := object["predicate"].(map[string]any); return validateRecipePredicateConsistency(path+".predicate", child, "row") }
	if kind != "field" && kind != "cell" && kind != "state" { return nil }
	operand, ok := parseRecipeOperand(object["operand_type"]); if !ok { return fmt.Errorf("%s has an invalid operand_type", path) }
	if kind == "field" { if expected, known := fieldRecipeOperand(fmt.Sprint(object["field"])); known && !equalRecipeOperand(operand, expected) { return fmt.Errorf("%s operand_type does not match its field", path) } }
	if kind == "state" { expected := recipeOperand{kind:"scalar", scalarType:stateFieldValueType(fmt.Sprint(object["field_path"]))}; if !equalRecipeOperand(operand, expected) { return fmt.Errorf("%s operand_type does not match its state field", path) } }
	operator, _ := object["operator"].(string)
	compatible := operator == "eq" || operator == "ne" || operator == "exists" || operator == "missing"
	if operator == "lt" || operator == "lte" || operator == "gt" || operator == "gte" { compatible = operand.kind == "scalar" && map[string]bool{"integer":true,"number":true,"date":true,"datetime":true}[operand.scalarType] }
	if operator == "in" || operator == "not_in" { compatible = operand.kind == "scalar" || operand.kind == "address" }
	if operator == "contains" { compatible = operand.kind == "string_set" || operand.kind == "scalar" && operand.scalarType == "string" }
	if operator == "starts_with" || operator == "ends_with" { compatible = operand.kind == "scalar" && operand.scalarType == "string" }
	if !compatible { return fmt.Errorf("%s operator is incompatible with operand_type", path) }
	if operator != "exists" && operator != "missing" { value, ok := object["value"].(map[string]any); if !ok || !validateRecipePredicateValue(value, operator, operand) { return fmt.Errorf("%s value is incompatible with operand_type and operator", path) } }
	return nil
}

func validateViewProjectionConsistency(path string, object map[string]any, kind string) error {
	distinct := func(left, right string) bool { a, aOK := object[left].(string); b, bOK := object[right].(string); return aOK && bOK && a != b }
	if kind != "composed" {
		pairs := map[string][2]string{"diagram":{"source_endpoint","target_endpoint"}, "flow":{"source_endpoint","target_endpoint"}, "matrix":{"row_endpoint","column_endpoint"}, "tree":{"parent_endpoint","child_endpoint"}}
		pair := pairs[kind]; if !distinct(pair[0], pair[1]) { return fmt.Errorf("%s effective %s endpoints must be present and distinct", path, kind) }; return nil
	}
	mode, _ := object["mode"].(string)
	present := func(name string) bool { _, ok := object[name]; return ok }
	switch mode {
	case "nest": if !distinct("parent_endpoint", "child_endpoint") || present("overlay_endpoint") || present("target_endpoint") || present("badge_endpoint") { return fmt.Errorf("%s has invalid nest endpoints", path) }
	case "overlay": if !distinct("overlay_endpoint", "target_endpoint") || present("parent_endpoint") || present("child_endpoint") || present("badge_endpoint") { return fmt.Errorf("%s has invalid overlay endpoints", path) }
	case "badge": if !distinct("badge_endpoint", "target_endpoint") || present("parent_endpoint") || present("child_endpoint") || present("overlay_endpoint") { return fmt.Errorf("%s has invalid badge endpoints", path) }
	case "edge", "hide": if present("parent_endpoint") || present("child_endpoint") || present("overlay_endpoint") || present("target_endpoint") || present("badge_endpoint") { return fmt.Errorf("%s endpoint fields are forbidden for %s", path, mode) }
	}
	return nil
}

func fidelityRank(value string) int { return map[string]int{"lossy":0,"visual_only":1,"traceable_summary":2,"lossless":3}[value] }
func statePolicyRank(value string) int { return map[string]int{"none":0,"optional":1,"required":2}[value] }

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
	native, _ := object["native_maximum_fidelity"].(string); effective, _ := object["effective_maximum_fidelity"].(string); fidelity, _ := object["fidelity"].(string); basis, _ := object["fidelity_basis"].(string); sourceRefs, _ := object["source_refs"].(bool)
	fixedMaximum := map[string]string{"json":"lossless","yaml":"lossless","xlsx":"traceable_summary","html":"traceable_summary","svg":"visual_only","png":"visual_only","pdf":"visual_only","pptx":"visual_only","docx":"visual_only","drawio":"visual_only","bpmn":"lossy"}[format]
	if fixedMaximum != "" && native != fixedMaximum { return fmt.Errorf("%s does not state the exact format-intrinsic native fidelity", path) }
	if format == "csv" || format == "tsv" { expected := "lossy"; if options["bundle"] == true && options["header"] == true && options["source_manifest"] == true { expected = "traceable_summary" }; if native != expected { return fmt.Errorf("%s does not state the exact delimited native fidelity", path) } }
	if format == "markdown" && native != "lossy" && native != "traceable_summary" || format == "mermaid" && native != "lossy" && native != "traceable_summary" { return fmt.Errorf("%s has an impossible context-dependent native fidelity", path) }
	embedded := format == "xlsx" && options["view_data_json"] == true && options["hidden_ids"] == true
	if embedded { if basis != "embedded_viewdata" || effective != "lossless" { return fmt.Errorf("%s has inconsistent embedded ViewData fidelity", path) } } else if basis != "native" || effective != native { return fmt.Errorf("%s has inconsistent native fidelity basis", path) }
	if fidelityRank(fidelity) > fidelityRank(effective) { return fmt.Errorf("%s requested fidelity exceeds effective capability", path) }
	if (fidelity == "lossless" || fidelity == "traceable_summary" || format == "json" || format == "yaml") && !sourceRefs { return fmt.Errorf("%s fidelity requires source_refs", path) }
	embeddedManifest := format == "json" || format == "yaml" || format == "xlsx" && options["view_data_json"] == true
	explicitManifest := (format == "csv" || format == "tsv" || format == "markdown" || format == "mermaid" || format == "bpmn" || format == "drawio") && options["source_manifest"] == true
	minimumManifest := explicitManifest || sourceRefs && !embeddedManifest
	requiresManifest, _ := object["requires_source_manifest"].(bool)
	if minimumManifest && !requiresManifest { return fmt.Errorf("%s omits a required source manifest", path) }
	return nil
}

func viewTableValueMatches(column map[string]any, kind, scalar string, enumValues []string) bool {
	value, ok := column["value_type"].(map[string]any); if !ok || value["kind"] != kind { return false }
	if kind == "scalar" && value["scalar_type"] != scalar { return false }
	if enumValues != nil {
		values, ok := value["enum_values"].([]any); if !ok || len(values) != len(enumValues) { return false }
		for index := range values { if values[index] != enumValues[index] { return false } }
	}
	if kind == "scalar" {
		_, hasEnum := value["enum_values"]; _, hasFormat := value["format"]
		if scalar == "enum" { values, ok := value["enum_values"].([]any); if enumValues == nil && (!hasEnum || !ok || len(values) == 0) { return false } } else if hasEnum { return false }
		if hasFormat && scalar != "string" { return false }
	}
	return true
}

func stateEnumValues(field string) []string {
	switch field {
	case "system.created_by.kind", "system.updated_by.kind", "provenance.verified_by.kind": return []string{"user","agent","service_account","anonymous"}
	case "provenance.source.kind": return []string{"manual","import","api","agent","external_system"}
	default: return nil
	}
}

func containsStateRead(values []any, expected map[string]any) bool {
	for _, raw := range values { value, ok := raw.(map[string]any); if ok && value["subject_kind"] == expected["subject_kind"] && value["field_path"] == expected["field_path"] && value["value_type"] == expected["value_type"] { return true } }
	return false
}

type viewDependencySets struct { query, parameter, layer, entityType, relationType, entity, relation, column map[string]bool }

func newViewDependencySets() viewDependencySets { return viewDependencySets{map[string]bool{},map[string]bool{},map[string]bool{},map[string]bool{},map[string]bool{},map[string]bool{},map[string]bool{},map[string]bool{}} }

func (sets *viewDependencySets) add(address string) {
	kind, _, valid := stableAddressSubject(address); if !valid { return }
	target := map[string]map[string]bool{"query":sets.query,"query_parameter":sets.parameter,"layer":sets.layer,"entity_type":sets.entityType,"relation_type":sets.relationType,"entity":sets.entity,"relation":sets.relation,"entity_type_column":sets.column,"relation_type_column":sets.column}[kind]
	if target != nil { target[address] = true }
}

func addViewDependencyValues(raw any, sets *viewDependencySets) {
	switch value := raw.(type) {
	case string:
		sets.add(value)
	case []any:
		for _, item := range value { if address, ok := item.(string); ok { sets.add(address) } }
	}
}

func collectViewDependencyAddresses(raw any, sets *viewDependencySets) {
	switch value := raw.(type) {
	case []any:
		for _, item := range value { collectViewDependencyAddresses(item,sets) }
	case map[string]any:
		for property, item := range value {
			switch property {
			case "arguments":
				if arguments, ok := item.(map[string]any); ok { for address := range arguments { sets.add(address) } }
			case "query_address", "entity_address", "relation_address", "layer_address", "parameter_address",
				"branch_value_column_address",
				"layer_addresses", "entity_type_addresses", "relation_type_addresses", "entity_addresses", "relation_addresses",
				"column_addresses", "lane_column_addresses", "attribute_column_addresses":
				addViewDependencyValues(item,sets)
			default:
				collectViewDependencyAddresses(item,sets)
			}
		}
	}
}

func dependencyContainsAll(raw any, expected map[string]bool) bool {
	values, ok := raw.([]any); if !ok { return false }; actual := map[string]bool{}
	for _, item := range values { value, ok := item.(string); if !ok { return false }; actual[value] = true }
	for value := range expected { if !actual[value] { return false } }
	return true
}

func validateLocallyDerivableViewDependencies(object map[string]any) bool {
	dependencies, dependenciesOK := object["dependencies"].(map[string]any); source, sourceOK := object["source"].(map[string]any); shape, shapeOK := object["shape"].(map[string]any); overrides, overridesOK := object["relation_projection_overrides"].(map[string]any)
	if !dependenciesOK || !sourceOK || !shapeOK || !overridesOK { return false }
	sets := newViewDependencySets(); collectViewDependencyAddresses(source,&sets); collectViewDependencyAddresses(shape,&sets); for address, override := range overrides { sets.add(address); collectViewDependencyAddresses(override,&sets) }
	if !dependencyArrayEquals(dependencies["query_addresses"],sets.query) { return false }
	_, hasSourceQuery := source["query_address"].(string)
	checks := []struct { property string; values map[string]bool }{{"parameter_addresses",sets.parameter},{"layer_addresses",sets.layer},{"entity_type_addresses",sets.entityType},{"relation_type_addresses",sets.relationType},{"entity_addresses",sets.entity},{"relation_addresses",sets.relation},{"column_addresses",sets.column}}
	for _, check := range checks { if hasSourceQuery { if !dependencyContainsAll(dependencies[check.property],check.values) { return false } } else if !dependencyArrayEquals(dependencies[check.property],check.values) { return false } }
	exports, exportsOK := object["exports"].([]any); exportAddresses, addressesOK := dependencies["export_addresses"].([]any); if !exportsOK || !addressesOK || len(exports) != len(exportAddresses) { return false }
	for index, raw := range exports { export, ok := raw.(map[string]any); address, addressOK := export["address"].(string); dependency, dependencyOK := exportAddresses[index].(string); if !ok || !addressOK || !dependencyOK || address != dependency { return false } }
	return true
}

func validateViewRecipeConsistency(path string, object map[string]any) error {
	address, addressOK := object["address"].(string)
	shape, shapeOK := object["shape"].(map[string]any)
	reservedValues, reservedOK := object["reserved_table_column_ids"].([]any)
	if !addressOK || !shapeOK || !reservedOK { return fmt.Errorf("%s has invalid View recipe authority fields", path) }
	category, _ := object["category"].(string); source, _ := object["source"].(map[string]any); sourceKind, _ := source["kind"].(string); shapeKind, _ := shape["kind"].(string)
	diffCount := 0; if category == "diff" { diffCount++ }; if sourceKind == "diff" { diffCount++ }; if shapeKind == "diff" { diffCount++ }
	if diffCount != 0 && diffCount != 3 { return fmt.Errorf("%s diff category, source, and shape must occur together", path) }
	stateInput, _ := object["state_input"].(string); stateRequirement, _ := object["state_requirement"].(string)
	if statePolicyRank(stateRequirement) < statePolicyRank(stateInput) { return fmt.Errorf("%s state_requirement cannot be weaker than state_input", path) }
	if diffCount == 3 && stateRequirement != "none" { return fmt.Errorf("%s Diff recipes forbid state requirements", path) }
	dependencies, dependenciesOK := object["dependencies"].(map[string]any); if !dependenciesOK { return fmt.Errorf("%s lacks View dependencies", path) }
	stateReads, stateReadsOK := dependencies["state_reads"].([]any); if !stateReadsOK { return fmt.Errorf("%s lacks View state reads", path) }
	var directReads []map[string]any
	if shapeKind == "table" {
		table, tableOK := shape["table"].(map[string]any); columns, columnsOK := table["columns"].([]any)
		if !tableOK || !columnsOK { return fmt.Errorf("%s has invalid table shape authority", path) }
		rowSource, _ := table["row_source"].(string); entityRows := rowSource == "entity" || rowSource == "entity_rows"
		if !entityRows && (table["include_entity_id"] == true || table["include_type"] == true || table["include_layer"] == true) { return fmt.Errorf("%s fixed Entity columns are forbidden for Relation rows", path) }
		if !entityRows { if _, present := table["entity_type_addresses"]; present { return fmt.Errorf("%s entity type selectors are forbidden for Relation rows", path) } }
		available := map[string]bool{}
		if table["include_entity_id"] == true { available["entity_id"] = true }; if table["include_type"] == true { available["entity_type"] = true }; if table["include_layer"] == true { available["entity_layer"] = true }
		automaticColumns, automaticOK := table["automatic_relation_columns"].([]any); if !automaticOK { return fmt.Errorf("%s lacks automatic Relation column authority", path) }
		if rowSource != "automatic_relations" && len(automaticColumns) != 0 { return fmt.Errorf("%s declares automatic Relation columns for a non-automatic row source", path) }
		for _, raw := range automaticColumns { id, ok := raw.(string); if !ok { return fmt.Errorf("%s has a non-string automatic Relation column", path) }; available[id] = true }
		reserved := map[string]bool{}; for _, raw := range reservedValues { value, ok := raw.(string); if !ok { return fmt.Errorf("%s has a non-string table reservation", path) }; reserved[value] = true }
		for _, raw := range columns {
			column, ok := raw.(map[string]any); if !ok { return fmt.Errorf("%s has a non-object table column", path) }
			columnAddress, addressPresent := column["address"].(string); id, idPresent := column["id"].(string)
			if !addressPresent || !idPresent || !hasDirectStableAddressOwner(address, columnAddress) { return fmt.Errorf("%s has a table column outside its View owner", path) }
			if reserved[id] || available[id] { return fmt.Errorf("%s table column ID %q conflicts with reserved or fixed columns", path, id) }; available[id] = true
			columnSource, ok := column["source"].(map[string]any); if !ok { return fmt.Errorf("%s table column lacks a source", path) }
			sourceKind, _ := columnSource["kind"].(string); aggregate, _ := column["aggregate"].(string)
			switch sourceKind {
			case "attribute":
				if rowSource != "entity_rows" && rowSource != "relation_rows" { return fmt.Errorf("%s attribute source has an incompatible row source", path) }
				addresses, ok := columnSource["column_addresses"].([]any); if !ok || len(addresses) == 0 { return fmt.Errorf("%s attribute source must name Columns", path) }
				expectedKind := "entity_type_column"; if rowSource == "relation_rows" { expectedKind = "relation_type_column" }
				for _, rawAddress := range addresses { address, ok := rawAddress.(string); actual, _, valid := stableAddressSubject(address); if !ok || !valid || actual != expectedKind { return fmt.Errorf("%s attribute source names a wrong-owner Column", path) } }
			case "relation_endpoint":
				if rowSource != "relation" && rowSource != "relation_rows" { return fmt.Errorf("%s relation endpoint requires Relation rows", path) }
				field, _ := columnSource["field"].(string); if field == "id" || field == "display_name" { if !viewTableValueMatches(column,"scalar","string",nil) { return fmt.Errorf("%s relation endpoint value_type is wrong", path) } } else if !viewTableValueMatches(column,"stable_address","",nil) { return fmt.Errorf("%s relation endpoint value_type is wrong", path) }
			case "derived_count": if !entityRows || !viewTableValueMatches(column,"scalar","integer",nil) { return fmt.Errorf("%s derived count contract is invalid", path) }
			case "field":
				field, _ := columnSource["field"].(string)
				if field == "id" || field == "display_name" || field == "description" { if !viewTableValueMatches(column,"scalar","string",nil) { return fmt.Errorf("%s field value_type is wrong", path) } } else if field == "tags" { if !viewTableValueMatches(column,"string_set","",nil) { return fmt.Errorf("%s tag field value_type is wrong", path) } } else if !viewTableValueMatches(column,"stable_address","",nil) { return fmt.Errorf("%s address field value_type is wrong", path) }
			case "state":
				field, _ := columnSource["field_path"].(string); valueType := stateFieldValueType(field); enumValues := stateEnumValues(field)
				if !viewTableValueMatches(column,"scalar",valueType,enumValues) { return fmt.Errorf("%s state column value_type is wrong", path) }
				subjects := []string{}
				switch rowSource { case "entity": subjects=[]string{"entity"}; case "entity_rows": subjects=[]string{"entity_row"}; case "relation": subjects=[]string{"relation"}; case "relation_rows": subjects=[]string{"relation_row"}; case "automatic_relations": subjects=[]string{"relation","relation_row"} }
				for _, subject := range subjects { directReads = append(directReads,map[string]any{"subject_kind":subject,"field_path":field,"value_type":valueType}) }
			default: return fmt.Errorf("%s table column has an unknown source", path)
			}
			if (aggregate == "count" || aggregate == "count_distinct") && !viewTableValueMatches(column,"scalar","integer",nil) { return fmt.Errorf("%s count aggregate has a wrong output type", path) }
			if aggregate == "join_unique" && !viewTableValueMatches(column,"scalar","string",nil) { return fmt.Errorf("%s join_unique has a wrong output type", path) }
			if aggregate == "min" || aggregate == "max" { valueType, ok := column["value_type"].(map[string]any); if !ok || valueType["kind"] != "scalar" || !map[string]bool{"integer":true,"number":true,"date":true,"datetime":true,"enum":true}[fmt.Sprint(valueType["scalar_type"])] { return fmt.Errorf("%s min/max has an incompatible output type", path) } }
		}
		if (len(directReads) != 0) != (stateInput == "optional" || stateInput == "required") { return fmt.Errorf("%s state_input and direct Table state reads are inconsistent", path) }
		if sorts, ok := table["sorts"].([]any); ok { for _, raw := range sorts { sortValue, _ := raw.(map[string]any); id, _ := sortValue["column_id"].(string); if !available[id] { return fmt.Errorf("%s sort names unavailable column %q", path, id) } } }
		for _, read := range directReads { if !containsStateRead(stateReads,read) { return fmt.Errorf("%s dependencies omit a direct Table state read", path) } }
	} else if stateInput != "none" { return fmt.Errorf("%s state_input is forbidden without direct Table state reads", path) }
	if _, hasSourceQuery := source["query_address"].(string); !hasSourceQuery && len(stateReads) != len(directReads) { return fmt.Errorf("%s dependencies contain non-derivable state reads", path) }
	exports, _ := object["exports"].([]any)
	for _, raw := range exports { export, ok := raw.(map[string]any); if !ok || export["view_address"] != address || !validateExportInView(export, category, shapeKind, stateRequirement, diffCount == 3) { return fmt.Errorf("%s contains an Export incompatible with its View context", path) } }
	if !validateLocallyDerivableViewDependencies(object) { return fmt.Errorf("%s dependencies omit or misorder locally derivable View dependencies", path) }
	return nil
}

func validateExportInView(object map[string]any, category, shape, stateRequirement string, diff bool) bool {
	format, _ := object["format"].(string); options, _ := object["options"].(map[string]any)
	if format == "json" || format == "yaml" { return object["native_maximum_fidelity"] == "lossless" && object["effective_maximum_fidelity"] == "lossless" && object["fidelity_basis"] == "native" && validateManifestClaim(object, stateRequirement, true) && !(diff && options["state_summary"] == true) }
	matrix := map[string]map[string]string{
		"diagram":{"xlsx":"traceable_summary","html":"traceable_summary","csv":"traceable_summary","tsv":"traceable_summary","svg":"visual_only","png":"visual_only","pdf":"visual_only","pptx":"visual_only","docx":"visual_only","drawio":"visual_only","mermaid":"lossy"},
		"table":{"xlsx":"traceable_summary","csv":"traceable_summary","tsv":"traceable_summary","html":"traceable_summary","pdf":"visual_only","pptx":"visual_only","docx":"visual_only","markdown":"lossy"},
		"matrix":{"xlsx":"traceable_summary","csv":"traceable_summary","tsv":"traceable_summary","html":"traceable_summary","svg":"visual_only","png":"visual_only","pdf":"visual_only","pptx":"visual_only","docx":"visual_only"},
		"tree":{"xlsx":"traceable_summary","csv":"traceable_summary","tsv":"traceable_summary","html":"traceable_summary","mermaid":"traceable_summary","svg":"visual_only","png":"visual_only","pdf":"visual_only","pptx":"visual_only","docx":"visual_only","drawio":"visual_only"},
		"flow":{"xlsx":"traceable_summary","csv":"traceable_summary","tsv":"traceable_summary","html":"traceable_summary","mermaid":"traceable_summary","bpmn":"lossy","svg":"visual_only","png":"visual_only","pdf":"visual_only","pptx":"visual_only","docx":"visual_only","drawio":"visual_only","markdown":"lossy"},
		"context":{"csv":"traceable_summary","tsv":"traceable_summary","xlsx":"traceable_summary","html":"traceable_summary","markdown":"traceable_summary","pdf":"visual_only","pptx":"visual_only","docx":"visual_only"},
		"diff":{"csv":"traceable_summary","tsv":"traceable_summary","xlsx":"traceable_summary","html":"traceable_summary","markdown":"traceable_summary","pdf":"visual_only","pptx":"visual_only","docx":"visual_only"},
	}
	native, allowed := matrix[shape][format]; if !allowed { return false }
	if format == "csv" || format == "tsv" { if options["bundle"] != true || options["header"] != true || options["source_manifest"] != true { native = "lossy" } }
	if (shape == "tree" || shape == "flow") && format == "mermaid" && options["source_manifest"] != true { native = "lossy" }
	if object["native_maximum_fidelity"] != native { return false }
	embedded := format == "xlsx" && options["view_data_json"] == true && options["hidden_ids"] == true
	if embedded { if object["effective_maximum_fidelity"] != "lossless" || object["fidelity_basis"] != "embedded_viewdata" { return false } } else if object["effective_maximum_fidelity"] != native || object["fidelity_basis"] != "native" { return false }
	if format == "xlsx" {
		profile, _ := options["profile"].(string); compatible := false
		switch profile { case "type_workbook": compatible = shape == "table"; case "diagram_workbook", "composed_diagram_workbook", "diagram_inventory_workbook": compatible = shape == "diagram"; case "matrix_workbook": compatible = shape == "matrix"; case "tree_workbook": compatible = shape == "tree"; case "flow_workbook": compatible = shape == "flow"; case "diff_workbook": compatible = shape == "diff"; case "context_workbook": compatible = shape == "context"; case "impact_workbook": compatible = category == "impact" && (shape == "diagram" || shape == "table" || shape == "matrix") }
		if !compatible { return false }
	}
	return validateManifestClaim(object, stateRequirement, format == "xlsx" && options["view_data_json"] == true)
}

func validateManifestClaim(object map[string]any, stateRequirement string, embedded bool) bool {
	options, _ := object["options"].(map[string]any); sourceRefs, _ := object["source_refs"].(bool)
	explicit := (options["kind"] == "csv" || options["kind"] == "tsv") && options["source_manifest"] == true || (options["kind"] == "markdown" || options["kind"] == "mermaid" || options["kind"] == "bpmn" || options["kind"] == "drawio") && options["source_manifest"] == true
	required := explicit || stateRequirement != "none" || sourceRefs && !embedded
	return object["requires_source_manifest"] == required
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
	if values, ok := rule["error_diagnostic"].([]any); ok {
		for _, rawName := range values {
			name, _ := rawName.(string); items, ok := object[name].([]any); if !ok { return fmt.Errorf("%s tagged alternative requires diagnostic array %s", path, name) }
			found := false; for _, rawItem := range items { if item, ok := rawItem.(map[string]any); ok && item["severity"] == "error" { found = true } }
			if !found { return fmt.Errorf("%s tagged alternative requires an error diagnostic in %s", path, name) }
		}
	}
	if values, ok := rule["any_non_empty"].([]any); ok {
		found := false
		for _, rawName := range values {
			name, _ := rawName.(string)
			if items, ok := object[name].([]any); ok && len(items) > 0 { found = true }
		}
		if !found { return fmt.Errorf("%s tagged alternative requires at least one non-empty collection", path) }
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

func protocolObject(value any) (map[string]any, bool) { object, ok := value.(map[string]any); return object, ok }

func protocolString(object map[string]any, name string) string { value, _ := object[name].(string); return value }

func equalProtocolValue(left, right any) bool { return reflect.DeepEqual(left, right) }

func protocolGenerationKey(raw any) (string, string, string, bool) {
	generation, ok := protocolObject(raw); if !ok { return "", "", "", false }
	handle, ok := protocolObject(generation["document_handle"]); if !ok { return "", "", "", false }
	endpoint, endpointOK := handle["endpoint_instance_id"].(string); value, valueOK := handle["value"].(string); number, numberOK := generation["value"].(string)
	return endpoint, value, number, endpointOK && valueOK && numberOK
}

func sameProtocolGeneration(left, right any) bool { le, lh, lg, lok := protocolGenerationKey(left); re, rh, rg, rok := protocolGenerationKey(right); return lok && rok && le == re && lh == rh && lg == rg }

func nextProtocolGeneration(base, proposed any) bool {
	be, bh, bg, bok := protocolGenerationKey(base); pe, ph, pg, pok := protocolGenerationKey(proposed); if !bok || !pok || be != pe || bh != ph { return false }
	b, err := strconv.ParseUint(bg, 10, 64); if err != nil || b == math.MaxUint64 { return false }; p, err := strconv.ParseUint(pg, 10, 64); return err == nil && p == b+1
}

func protocolHasErrorDiagnostic(raw any) bool { values, ok := raw.([]any); if !ok { return false }; for _, item := range values { object, _ := protocolObject(item); if object["severity"] == "error" { return true } }; return false }

func protocolBlobSize(value any) (uint64, error) {
	switch typed := value.(type) {
	case []any:
		var total uint64; for _, item := range typed { size, err := protocolBlobSize(item); if err != nil || math.MaxUint64-total < size { return 0, errors.New("logical BlobRef byte count overflows uint64") }; total += size }; return total, nil
	case map[string]any:
		var total uint64
		if _, blob := typed["blob_id"].(string); blob {
			if _, digest := typed["digest"].(string); digest { if text, size := typed["size"].(string); size { parsed, err := strconv.ParseUint(text, 10, 64); if err != nil { return 0, err }; total = parsed } }
		}
		for _, item := range typed { size, err := protocolBlobSize(item); if err != nil || math.MaxUint64-total < size { return 0, errors.New("logical BlobRef byte count overflows uint64") }; total += size }
		return total, nil
	default: return 0, nil
	}
}

func validatePagedResult(path string, object map[string]any) error {
	items, itemsOK := object["items"].([]any); page, pageOK := protocolObject(object["page"]); if !itemsOK || !pageOK { return fmt.Errorf("%s must contain items and page", path) }
	returnedItems, ok := page["returned_items"].(string); if !ok || returnedItems != strconv.Itoa(len(items)) { return fmt.Errorf("%s.page.returned_items must equal the item count", path) }
	if cursor, present := page["next_cursor"]; present { cursorObject, ok := protocolObject(cursor); if !ok || !sameProtocolGeneration(object["document_generation"], cursorObject["document_generation"]) { return fmt.Errorf("%s.page.next_cursor is bound to another document generation", path) } }
	want, ok := page["returned_bytes"].(string); if !ok { return fmt.Errorf("%s.page.returned_bytes is invalid", path) }
	page["returned_bytes"] = "0"; canonical, err := appendCanonicalJSON(nil, object); page["returned_bytes"] = want; if err != nil { return err }
	blobs, err := protocolBlobSize(object); if err != nil || uint64(len(canonical)) > math.MaxUint64-blobs { return fmt.Errorf("%s logical response byte count overflows", path) }
	if want != strconv.FormatUint(uint64(len(canonical))+blobs, 10) { return fmt.Errorf("%s.page.returned_bytes is not the exact logical response byte count", path) }
	return nil
}

func validateSemanticOperationInvariant(path string, object map[string]any) error {
	operation := protocolString(object, "operation")
	if operation == "create_subject" {
		kind := protocolString(object, "subject_kind"); fields, ok := protocolObject(object["fields"]); if !ok { return fmt.Errorf("%s.fields must be an object", path) }
		common := map[string]bool{"description":true,"tags":true,"annotations":true}
		allowed := map[string][]string{
			"entity_type":{"display_name","representation","icon","image","color"}, "relation_type":{"display_name","semantic_kind","from","to","forward_label","allow_self","duplicate_policy","cardinality","reverse_label","traversal","projections","render","export"},
			"layer":{"display_name","order"}, "entity":{"display_name","type_address","layer_address"}, "query":{"display_name","select","state_input","where","relation_where","traverse","result"},
			"view":{"display_name","category","source","shape","intent","state_input","relation_projection_overrides"}, "reference":{"text"},
			"entity_type_column":{"display_name","value_type","enum_values","reserved_enum_values","required","default","format","min","max","min_length","max_length"},
			"relation_type_column":{"display_name","value_type","enum_values","reserved_enum_values","required","default","format","min","max","min_length","max_length"},
			"entity_type_constraint":{"column_addresses"}, "relation_type_constraint":{"column_addresses"},
			"query_parameter":{"value_type","enum_values","reserved_enum_values","required","default","format","min","max","min_length","max_length"},
			"view_table_column":{"source","label","aggregate"}, "view_export":{"format","filename","fidelity","source_refs","exporter_profile","options"},
		}
		required := map[string][]string{"entity_type":{"display_name","representation"},"relation_type":{"display_name","semantic_kind","from","to","forward_label"},"layer":{"display_name","order"},"entity":{"display_name","type_address","layer_address"},"query":{"display_name","select"},"view":{"display_name","category","source","shape"},"reference":{"text"},"entity_type_column":{"display_name","value_type"},"relation_type_column":{"display_name","value_type"},"entity_type_constraint":{"column_addresses"},"relation_type_constraint":{"column_addresses"},"query_parameter":{"value_type"},"view_table_column":{"source"},"view_export":{"format","filename","fidelity"}}
		list, known := allowed[kind]; if !known { return fmt.Errorf("%s.subject_kind is not creatable", path) }; permits := map[string]bool{}; for _, name := range list { permits[name] = true }; if kind != "reference" && !strings.Contains(kind, "_column") && !strings.Contains(kind, "_constraint") && kind != "query_parameter" && kind != "view_table_column" && kind != "view_export" { for name := range common { permits[name] = true } }
		for name := range fields { if !permits[name] { return fmt.Errorf("%s.fields.%s is foreign to %s", path, name, kind) } }; for _, name := range required[kind] { if _, present := fields[name]; !present { return fmt.Errorf("%s.fields.%s is required for %s", path, name, kind) } }
		if kind == "view" { source, sourceOK := protocolObject(fields["source"]); shape, shapeOK := protocolObject(fields["shape"]); diff := fields["category"] == "diff"; if !sourceOK || !shapeOK || (source["kind"] == "diff") != diff || (shape["kind"] == "diff") != diff { return fmt.Errorf("%s.fields view category, source, and shape disagree", path) }; if shape["kind"] == "matrix" { cell, _ := protocolObject(shape["cell"]); _, columns := cell["attribute_column_addresses"]; if (cell["display"] == "attribute_summary") != columns { return fmt.Errorf("%s.fields.shape matrix attribute columns contradict display", path) } }; if shape["kind"] == "flow" { _, columns := shape["lane_column_addresses"]; if (shape["lane_by"] == "attribute") != columns { return fmt.Errorf("%s.fields.shape flow lane columns contradict lane_by", path) } } }
		if kind == "view_table_column" { source, sourceOK := protocolObject(fields["source"]); if !sourceOK || source["kind"] == "query" || source["kind"] == "diff" { return fmt.Errorf("%s.fields.source is not a table column source", path) } }
		if strings.Contains(kind, "_column") || kind == "query_parameter" { valueType := protocolString(fields,"value_type"); enumValues, hasEnumValues := fields["enum_values"].([]any); if hasEnumValues != (valueType == "enum") { return fmt.Errorf("%s.fields.enum_values must appear exactly for enum value_type", path) }; if _, present := fields["reserved_enum_values"]; present && valueType != "enum" { return fmt.Errorf("%s.fields.reserved_enum_values requires enum value_type", path) }; if _, present := fields["format"]; present && valueType != "string" { return fmt.Errorf("%s.fields.format requires string value_type", path) }; for _, name := range []string{"min","max"} { if raw, present := fields[name]; present { if valueType != "integer" && valueType != "number" { return fmt.Errorf("%s.fields.%s requires numeric value_type", path, name) }; if valueType == "integer" { text, _ := raw.(string); if _, err := strconv.ParseInt(text,10,64); err != nil { return fmt.Errorf("%s.fields.%s must be an integer bound", path, name) } } } }; for _, name := range []string{"min_length","max_length"} { if _, present := fields[name]; present && valueType != "string" { return fmt.Errorf("%s.fields.%s requires string value_type", path, name) } }; if defaultValue, present := fields["default"]; present { typed, ok := protocolObject(defaultValue); if !ok || typed["kind"] != valueType { return fmt.Errorf("%s.fields.default contradicts value_type", path) }; if valueType == "enum" { member, _ := typed["string_value"].(string); found := false; for _, raw := range enumValues { if raw == member { found = true } }; if !found { return fmt.Errorf("%s.fields.default is not an active enum value", path) } } } }
		parentKind, _, valid := stableAddressSubject(protocolString(object,"parent_address")); expected := map[string]string{"entity_type_column":"entity_type","entity_type_constraint":"entity_type","relation_type_column":"relation_type","relation_type_constraint":"relation_type","query_parameter":"query","view_table_column":"view","view_export":"view"}[kind]; if expected == "" { expected = "project" }; if !valid || parentKind != expected { return fmt.Errorf("%s.parent_address cannot own %s", path, kind) }
	}
	if operation == "create_relation" && object["fields"] != nil { fields, ok := protocolObject(object["fields"]); if !ok { return fmt.Errorf("%s.fields must be an object", path) }; for name := range fields { if name != "display_name" && name != "description" && name != "tags" && name != "annotations" { return fmt.Errorf("%s.fields.%s is foreign to relation", path, name) } } }
	return nil
}

func validateProtocolInvariant(path string, object map[string]any, profile string) error {
	switch profile {
	case "authoring_impact_entry":
		if object["capability"] == "graph:write" { address, ok := object["subject_address"].(string); facts, factsOK := protocolObject(object["graph_facts"]); kind := protocolString(object,"subject_kind"); actual, _, valid := stableAddressSubject(address); if !ok || !factsOK || !valid || actual != kind { return fmt.Errorf("%s graph:write entry lacks matching subject identity/facts", path) }; flags, _ := facts["action_flags"].([]any); if len(flags) != 1 || flags[0] != object["action"] { return fmt.Errorf("%s graph action facts contradict entry action", path) } }
	case "authoring_impact":
		entries, _ := object["entries"].([]any); capabilities, _ := object["required_capabilities"].([]any); derived := map[string]bool{}; for _, raw := range entries { entry, _ := protocolObject(raw); capability, _ := entry["capability"].(string); derived[capability] = true }; if len(derived) != len(capabilities) { return fmt.Errorf("%s.required_capabilities contradict entries", path) }; for _, raw := range capabilities { capability, _ := raw.(string); if !derived[capability] { return fmt.Errorf("%s.required_capabilities contradict entries", path) } }
	case "open_document_result":
		if !sameProtocolGeneration(object["document_generation"], map[string]any{"document_handle":object["document_handle"],"value":protocolString(object["document_generation"].(map[string]any),"value")}) { return fmt.Errorf("%s outer handle does not match document generation", path) }
	case "document_bound_input":
		if handle, present := object["document_handle"]; present && !sameProtocolGeneration(object["document_generation"], map[string]any{"document_handle":handle,"value":protocolString(object["document_generation"].(map[string]any),"value")}) { return fmt.Errorf("%s outer handle does not match document generation", path) }
		if cursor, present := object["cursor"]; present { cursorObject, ok := protocolObject(cursor); if !ok || !sameProtocolGeneration(object["document_generation"], cursorObject["document_generation"]) { return fmt.Errorf("%s.cursor is bound to another document generation", path) } }
	case "paged_result": return validatePagedResult(path, object)
	case "bounded_text_chunk":
		blob, _ := protocolObject(object["blob"]); offset, e1 := strconv.ParseUint(protocolString(object,"offset"),10,64); total, e2 := strconv.ParseUint(protocolString(object,"total_bytes"),10,64); size, e3 := strconv.ParseUint(protocolString(blob,"size"),10,64); if e1 != nil || e2 != nil || e3 != nil || offset > total || size > total-offset || blob["media_type"] != "text/plain; charset=utf-8" || blob["lifetime"] != "request" { return fmt.Errorf("%s has an invalid bounded UTF-8 chunk range", path) }; if offset == 0 && size == total && blob["digest"] != object["full_digest"] { return fmt.Errorf("%s complete chunk digest must equal full_digest", path) }
	case "source_edit":
		kind := protocolString(object,"kind"); blob, hasBlob := protocolObject(object["replacement_blob"]); if hasBlob && (blob["digest"] != object["after_digest"] || blob["media_type"] != "text/plain; charset=utf-8" || blob["lifetime"] != "request") { return fmt.Errorf("%s replacement blob is not reconstructable", path) }; if kind == "move" { if object["before_digest"] != object["after_digest"] || equalProtocolValue(object["before_module"],object["after_module"]) { return fmt.Errorf("%s move must preserve bytes and change module identity", path) } }
	case "preview_result":
		if object["status"] == "valid" { if protocolHasErrorDiagnostic(object["diagnostics"]) { return fmt.Errorf("%s valid preview contains an error diagnostic", path) }; impact, _ := protocolObject(object["authoring_impact"]); semanticDiff, _ := protocolObject(object["semantic_diff"]); sourceDiff, _ := protocolObject(object["source_diff"]); hashes, _ := protocolObject(object["resulting_hashes"]); if object["authoring_impact_digest"] != impact["impact_digest"] || !equalProtocolValue(object["required_authoring_capabilities"],impact["required_capabilities"]) || impact["semantic_diff_hash"] != semanticDiff["digest"] || impact["source_diff_hash"] != sourceDiff["digest"] || impact["resulting_definition_hash"] != hashes["definition_hash"] { return fmt.Errorf("%s preview integrity fields disagree", path) }; preview, _ := protocolObject(object["preview_id"]); endpoint, _, _, _ := protocolGenerationKey(object["base_generation"]); if preview["endpoint_instance_id"] != endpoint || !nextProtocolGeneration(object["base_generation"],object["proposed_generation"]) { return fmt.Errorf("%s preview identities disagree", path) } } else if len(object["conflicts"].([]any)) == 0 && !protocolHasErrorDiagnostic(object["diagnostics"]) { return fmt.Errorf("%s invalid preview requires a conflict or error diagnostic", path) }
	case "apply_input":
		preview, _ := protocolObject(object["preview_id"]); endpoint, _, _, ok := protocolGenerationKey(object["base_generation"]); if !ok || preview["endpoint_instance_id"] != endpoint { return fmt.Errorf("%s preview and base generation endpoints disagree", path) }
	case "apply_result":
		impact, _ := protocolObject(object["authoring_impact"]); sourceDiff, _ := protocolObject(object["source_diff"]); hashes, _ := protocolObject(object["resulting_hashes"]); if impact["source_diff_hash"] != sourceDiff["digest"] || impact["resulting_definition_hash"] != hashes["definition_hash"] { return fmt.Errorf("%s apply integrity fields disagree", path) }
	case "semantic_operation": return validateSemanticOperationInvariant(path, object)
	default: return fmt.Errorf("%s has unknown protocol invariant %q", path, profile)
	}
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
	body.WriteString("function hasValidOutcomeEnvelope(value: Record<string, unknown>): boolean { const outcome=value[\"outcome\"]; if (outcome===\"success\") return hasOwn(value,\"payload\")&&!hasOwn(value,\"failure\"); if (outcome===\"rejected\") return !hasOwn(value,\"payload\")&&!hasOwn(value,\"failure\")&&isJSONArray(value[\"diagnostics\"])&&value[\"diagnostics\"].length>0; if (outcome===\"failed\"||outcome===\"cancelled\") { const failure=value[\"failure\"]; if (hasOwn(value,\"payload\")||!isObject(failure)) return false; const category=failure[\"workbench_category\"]; return typeof category!==\"string\"||(outcome===\"cancelled\"?category===\"cancelled\":category!==\"cancelled\"); } return false; }\n\n")
	body.WriteString("function hasValidProtocolOffer(value: Record<string, unknown>): boolean { const range = value[\"supported_range\"]; const bindings = value[\"versions\"]; if (typeof range !== \"string\" || !isJSONArray(bindings)) return false; const parsedRange = parseProtocolVersionRange(range); if (parsedRange === undefined) return false; const seen = new Set<string>(); for (const raw of bindings) { if (!isObject(raw) || typeof raw[\"version\"] !== \"string\") return false; const text = raw[\"version\"]; const version = parseProtocolVersion(text); if (version === undefined || compareProtocolVersions(version, parsedRange[0]) < 0 || compareProtocolVersions(version, parsedRange[1]) > 0 || seen.has(text)) return false; seen.add(text); } return true; }\n\n")
	body.WriteString("function hasValidLimitCapability(value: Record<string, unknown>): boolean { try { const fallback = BigInt(String(value[\"default_value\"])); const effective = BigInt(String(value[\"effective_maximum\"])); const hard = BigInt(String(value[\"hard_maximum\"])); return fallback <= hard && effective <= hard; } catch { return false; } }\n\n")
	body.WriteString("function hasUniqueArrayKey(value: Record<string, unknown>, arrayProperty: string, keyProperty: string): boolean { if (!hasOwn(value,arrayProperty)) return true; const items = value[arrayProperty]; if (!isJSONArray(items)) return false; const seen = new Set<string>(); for (const raw of items) { if (!isObject(raw) || typeof raw[keyProperty] !== \"string\" || seen.has(raw[keyProperty])) return false; seen.add(raw[keyProperty]); } return true; }\n\n")
	body.WriteString("function hasUniqueItems(value: ReadonlyArray<unknown>): boolean { try { return new Set(value.map(canonicalJSONStringify)).size === value.length; } catch { return false; } }\n\n")
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
	body.WriteString("function hasDisjointArrayKey(value: Record<string, unknown>, arrayProperty: string, keyProperty: string, stringsProperty: string): boolean { const items = hasOwn(value,arrayProperty) ? value[arrayProperty] : []; const strings = hasOwn(value,stringsProperty) ? value[stringsProperty] : []; if (!isJSONArray(items) || !isJSONArray(strings) || !strings.every((item) => typeof item === \"string\")) return false; const reserved = new Set(strings); return items.every((item) => isObject(item) && typeof item[keyProperty] === \"string\" && !reserved.has(item[keyProperty])); }\n\n")
	body.WriteString("function compareCanonicalUnsignedDecimals(left: string, right: string): number | undefined { if (!/^(0|[1-9][0-9]*)$/.test(left) || !/^(0|[1-9][0-9]*)$/.test(right)) return undefined; return left.length === right.length ? (left < right ? -1 : left > right ? 1 : 0) : left.length - right.length; }\n\n")
	body.WriteString(tsCanonicalCollectionRuntime)
	body.WriteString(tsProtocolInvariantRuntime)
	body.WriteString("function hasOrderedPair(value: Record<string, unknown>, lowerProperty: string, upperProperty: string, comparison: string): boolean { if (!hasOwn(value, lowerProperty) || !hasOwn(value, upperProperty)) return true; const lower = value[lowerProperty]; const upper = value[upperProperty]; if (typeof lower !== \"string\" || typeof upper !== \"string\") return false; if (comparison === \"unsigned_decimal\") { const ordered = compareCanonicalUnsignedDecimals(lower, upper); return ordered !== undefined && ordered <= 0; } if (comparison === \"finite_binary64\") { const lowerValue = Number(lower); const upperValue = Number(upper); return Number.isFinite(lowerValue) && Number.isFinite(upperValue) && lowerValue <= upperValue; } return false; }\n\n")
	body.WriteString("function hasAddressTerminalID(value: Record<string, unknown>, addressProperty: string, idProperty: string): boolean { const address = value[addressProperty]; const id = value[idProperty]; return typeof address === \"string\" && typeof id === \"string\" && address.split(\":\").at(-1) === id; }\n\n")
	body.WriteString(tsAuthorityRuntime)
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

const tsCanonicalCollectionRuntime = `function semanticSubjectKindRank(kind: string): number {
  return ["project","pack","entity_type","relation_type","layer","entity","relation","query","view","reference","entity_type_column","entity_type_constraint","relation_type_column","relation_type_constraint","entity_row","relation_row","query_parameter","view_table_column","view_export"].indexOf(kind);
}
function compareModuleOrder(left: Record<string,unknown>, right: Record<string,unknown>): number | undefined {
  const origin = (raw: unknown): readonly [number,string] | undefined => {
    if (!isObject(raw) || typeof raw["kind"] !== "string") return undefined;
    if (raw["kind"] === "project") return [0,""];
    return raw["kind"] === "pack" && typeof raw["pack_address"] === "string" ? [1,raw["pack_address"]] : undefined;
  };
  const a = origin(left["origin"]), b = origin(right["origin"]), aPath = left["module_path"], bPath = right["module_path"];
  if (a === undefined || b === undefined || typeof aPath !== "string" || typeof bPath !== "string") return undefined;
  if (a[0] !== b[0]) return a[0]-b[0];
  const pack = compareUnicodeScalars(a[1],b[1]); return pack !== 0 ? pack : compareUnicodeScalars(aPath,bPath);
}
function compareRangePosition(left: Record<string,unknown>, right: Record<string,unknown>): number | undefined {
  const aStart = left["start_byte"], bStart = right["start_byte"], aEnd = left["end_byte"], bEnd = right["end_byte"];
  if (typeof aStart !== "string" || typeof bStart !== "string" || typeof aEnd !== "string" || typeof bEnd !== "string") return undefined;
  const start = compareCanonicalUnsignedDecimals(aStart,bStart); if (start === undefined || start !== 0) return start;
  return compareCanonicalUnsignedDecimals(aEnd,bEnd);
}
function compareCanonicalCollection(profile: string, left: unknown, right: unknown): number | undefined {
  if (!isObject(left) || !isObject(right)) return undefined;
  const stable = (property: string): number | undefined => typeof left[property] === "string" && typeof right[property] === "string" ? compareStableAddresses(left[property],right[property]) : undefined;
  const text = (property: string): number | undefined => typeof left[property] === "string" && typeof right[property] === "string" ? compareUnicodeScalars(left[property],right[property]) : undefined;
  const kind = (property: string): number | undefined => {
    if (typeof left[property] !== "string" || typeof right[property] !== "string") return undefined;
    const a = semanticSubjectKindRank(left[property]), b = semanticSubjectKindRank(right[property]); return a < 0 || b < 0 ? undefined : a-b;
  };
  const range = (): number | undefined => isObject(left["range"]) && isObject(right["range"]) ? compareRangePosition(left["range"],right["range"]) : undefined;
  const identity = (): number | undefined => { const a = left["before_address"] ?? left["after_address"], b = right["before_address"] ?? right["after_address"]; return typeof a === "string" && typeof b === "string" ? compareStableAddresses(a,b) : undefined; };
  const conflictAddress = (): number | undefined => { const a = left["target_address"] ?? left["owner_address"] ?? "", b = right["target_address"] ?? right["owner_address"] ?? ""; return typeof a === "string" && typeof b === "string" ? (a === "" || b === "" ? compareUnicodeScalars(a,b) : compareStableAddresses(a,b)) : undefined; };
  const path = (): number | undefined => { const a = left["path"] ?? [], b = right["path"] ?? []; if (!Array.isArray(a) || !Array.isArray(b)) return undefined; for (let index=0; index<Math.min(a.length,b.length); index++) { if (typeof a[index] !== "string" || typeof b[index] !== "string") return undefined; const value=compareUnicodeScalars(a[index],b[index]); if (value !== 0) return value; } return a.length-b.length; };
  const optionalSourceRange = (): number | undefined => { const a=left["source_range"], b=right["source_range"]; if (!isObject(a) || !isObject(b)) return isObject(a) ? 1 : isObject(b) ? -1 : 0; return compareRangePosition(a,b); };
  const stringArray = (property: string): number | undefined => { const a=left[property] ?? [], b=right[property] ?? []; if (!Array.isArray(a) || !Array.isArray(b)) return undefined; for (let index=0; index<Math.min(a.length,b.length); index++) { if (typeof a[index] !== "string" || typeof b[index] !== "string") return undefined; const value=compareUnicodeScalars(a[index],b[index]); if (value !== 0) return value; } return a.length-b.length; };
  const chain = (...comparisons: ReadonlyArray<() => number | undefined>): number | undefined => { for (const compare of comparisons) { const value = compare(); if (value === undefined || value !== 0) return value; } return 0; };
  if (profile === "authored_field_path") return stringArray("tokens");
  if (profile === "authoring_impact") {
    const address = (): number | undefined => { const a=left["subject_address"] ?? left["owner_address"] ?? "", b=right["subject_address"] ?? right["owner_address"] ?? ""; return typeof a === "string" && typeof b === "string" ? (a === "" || b === "" ? compareUnicodeScalars(a,b) : compareStableAddresses(a,b)) : undefined; };
    return chain(address,() => text("capability"),() => text("action"));
  }
  if (profile === "bounded_text_chunk") {
    const address = (): number | undefined => { const a=left["address"] ?? left["owner_address"], b=right["address"] ?? right["owner_address"]; return typeof a === "string" && typeof b === "string" ? compareStableAddresses(a,b) : undefined; };
    const offset = (): number | undefined => { const a=left["source_chunk"] ?? left["text_chunk"], b=right["source_chunk"] ?? right["text_chunk"]; return isObject(a) && isObject(b) && typeof a["offset"] === "string" && typeof b["offset"] === "string" ? compareCanonicalUnsignedDecimals(a["offset"],b["offset"]) : undefined; };
    return chain(address,offset);
  }
  if (profile === "child_set") return chain(() => stable("owner_address"),() => kind("child_kind"));
  if (profile === "conflict") return chain(conflictAddress,() => text("kind"),path);
  if (profile === "reference_id") return text("id");
  if (profile === "subject_kind") return kind("kind");
  if (profile === "module_scope") return isObject(left["module"]) && isObject(right["module"]) ? compareModuleOrder(left["module"],right["module"]) : undefined;
  if (profile === "neighbor") return chain(() => stable("source_entity_address"),() => typeof left["depth"] === "number" && typeof right["depth"] === "number" ? left["depth"]-right["depth"] : undefined,() => text("direction"),() => stable("relation_address"),() => stable("entity_address"));
  if (profile === "source_file") return compareModuleOrder(left,right);
  if (profile === "source_asset") return chain(() => stable("subject_address"),() => text("locator"));
  if (profile === "source_patch") {
    const leftRange = left["source_range"], rightRange = right["source_range"];
    if (!isObject(leftRange) || !isObject(rightRange)) return undefined;
    const module = compareModuleOrder(leftRange,rightRange);
    return module === 0 ? compareRangePosition(leftRange,rightRange) : module;
  }
  if (profile === "semantic_diff") return chain(identity,() => text("kind"));
  if (profile === "semantic_map_entry") return text("key");
  if (profile === "source_diff") {
    const module = (value: Record<string,unknown>): Record<string,unknown> | undefined => isObject(value["source_range"]) ? value["source_range"] : isObject(value["before_module"]) ? value["before_module"] : isObject(value["after_module"]) ? value["after_module"] : undefined;
    const primary = (): number | undefined => { const a=module(left), b=module(right); return a === undefined || b === undefined ? undefined : compareModuleOrder(a,b); };
    const after = (): number | undefined => { const a=left["after_module"], b=right["after_module"]; if (!isObject(a) || !isObject(b)) return isObject(a) ? 1 : isObject(b) ? -1 : 0; return compareModuleOrder(a,b); };
    return chain(primary,() => text("kind"),optionalSourceRange,after);
  }
  if (profile === "source_range") { const module=compareModuleOrder(left,right); return module === 0 ? compareRangePosition(left,right) : module; }
  if (profile === "subgraph") return isObject(left["subject"]) && isObject(right["subject"]) && typeof left["subject"]["address"] === "string" && typeof right["subject"]["address"] === "string" ? compareStableAddresses(left["subject"]["address"],right["subject"]["address"]) : undefined;
  if (profile === "semantic_reference") return chain(() => stable("source_address"),range,() => stable("target_address"),() => kind("target_kind"),() => text("via"));
  if (profile === "source_binding") {
    const owner = (): number | undefined => { const a = left["target_owner_address"] ?? "", b = right["target_owner_address"] ?? ""; if (typeof a !== "string" || typeof b !== "string") return undefined; return a === "" || b === "" ? compareUnicodeScalars(a,b) : compareStableAddresses(a,b); };
    return chain(() => stable("source_address"),range,() => stable("target_address"),() => kind("target_kind"),owner,() => text("via"));
  }
  if (profile === "export_binding") {
    const module = (): number | undefined => isObject(left["module"]) && isObject(right["module"]) ? compareModuleOrder(left["module"],right["module"]) : undefined;
    const reexport = (): number | undefined => typeof left["re_export"] === "boolean" && typeof right["re_export"] === "boolean" ? Number(left["re_export"])-Number(right["re_export"]) : undefined;
    return chain(module,range,() => text("public_name"),() => stable("target_address"),reexport);
  }
  return undefined;
}
function hasCanonicalCollectionOrder(values: ReadonlyArray<unknown>, profile: string): boolean {
  return values.every((item,index) => {
    if (index === 0) return true;
    const previous = values[index-1];
    if ((compareCanonicalCollection(profile,previous,item) ?? 0) >= 0) return false;
    if (profile !== "source_patch" || !isObject(previous) || !isObject(item) || !isObject(previous["source_range"]) || !isObject(item["source_range"])) return profile !== "source_patch";
    if (compareModuleOrder(previous["source_range"],item["source_range"]) !== 0) return true;
    const leftEnd = previous["source_range"]["end_byte"], rightStart = item["source_range"]["start_byte"];
    if (typeof leftEnd !== "string" || typeof rightStart !== "string") return false;
    const overlap = compareCanonicalUnsignedDecimals(leftEnd,rightStart);
    return overlap !== undefined && overlap <= 0;
  });
}
function hasValidChildSet(value: Record<string,unknown>): boolean {
  const owner = value["owner_address"], child = value["child_kind"];
  if (typeof owner !== "string" || typeof child !== "string") return false;
  const ownerKind = authoritySubject(owner)?.kind;
  const allowed: Readonly<Record<string,ReadonlyArray<string>>> = {
    project:["entity_type","relation_type","layer","entity","relation","query","view","reference"],
    pack:["entity_type","relation_type","query","view","reference"],
    entity_type:["entity_type_column","entity_type_constraint"], relation_type:["relation_type_column","relation_type_constraint"],
    entity:["entity_row"], relation:["relation_row"], query:["query_parameter"], view:["view_table_column","view_export"],
  };
  return ownerKind !== undefined && (allowed[ownerKind]?.includes(child) ?? false);
}

`

const tsProtocolInvariantRuntime = `function protocolGenerationKey(raw: unknown): readonly [string,string,bigint] | undefined {
  if (!isObject(raw) || !isObject(raw["document_handle"])) return undefined; const handle=raw["document_handle"], endpoint=handle["endpoint_instance_id"], value=handle["value"], generation=raw["value"];
  if (typeof endpoint !== "string" || typeof value !== "string" || typeof generation !== "string") return undefined; try { return [endpoint,value,BigInt(generation)]; } catch { return undefined; }
}
function sameProtocolGeneration(left: unknown,right: unknown): boolean { const a=protocolGenerationKey(left), b=protocolGenerationKey(right); return a !== undefined && b !== undefined && a[0]===b[0] && a[1]===b[1] && a[2]===b[2]; }
function nextProtocolGeneration(base: unknown,proposed: unknown): boolean { const a=protocolGenerationKey(base), b=protocolGenerationKey(proposed); return a !== undefined && b !== undefined && a[0]===b[0] && a[1]===b[1] && b[2]===a[2]+1n; }
function protocolHasErrorDiagnostic(raw: unknown): boolean { return isJSONArray(raw) && raw.some((item)=>isObject(item) && item["severity"]==="error"); }
function protocolBlobBytes(raw: unknown): bigint | undefined { if (isJSONArray(raw)) { let total=0n; for (const item of raw) { const size=protocolBlobBytes(item); if (size===undefined) return undefined; total+=size; } return total; } if (!isObject(raw)) return 0n; let total=0n; if (typeof raw["blob_id"]==="string" && typeof raw["digest"]==="string" && typeof raw["size"]==="string") { try { total=BigInt(raw["size"]); } catch { return undefined; } } for (const item of Object.values(raw)) { const size=protocolBlobBytes(item); if (size===undefined) return undefined; total+=size; } return total; }
function protocolPagedResult(value: Record<string,unknown>): boolean {
  const items=value["items"], page=value["page"]; if (!isJSONArray(items)||!isObject(page)||page["returned_items"]!==String(items.length)) return false;
  if (hasOwn(page,"next_cursor") && (!isObject(page["next_cursor"]) || !sameProtocolGeneration(value["document_generation"],page["next_cursor"]["document_generation"]))) return false;
  const returned=page["returned_bytes"]; if (typeof returned!=="string") return false; const copy=JSON.parse(canonicalJSONStringify(value)) as Record<string,unknown>; if (!isObject(copy["page"])) return false; copy["page"]["returned_bytes"]="0"; const blobs=protocolBlobBytes(value); return blobs!==undefined && BigInt(new TextEncoder().encode(canonicalJSONStringify(copy)).length)+blobs===BigInt(returned);
}
function protocolSemanticOperation(value: Record<string,unknown>): boolean {
  if (value["operation"]==="create_subject") { const kind=value["subject_kind"], fields=value["fields"]; if (typeof kind!=="string"||!isObject(fields)) return false;
    const common=["description","tags","annotations"]; const allowed:Readonly<Record<string,ReadonlyArray<string>>>={entity_type:["display_name","representation","icon","image","color",...common],relation_type:["display_name","semantic_kind","from","to","forward_label","allow_self","duplicate_policy","cardinality","reverse_label","traversal","projections","render","export",...common],layer:["display_name","order",...common],entity:["display_name","type_address","layer_address",...common],query:["display_name","select","state_input","where","relation_where","traverse","result",...common],view:["display_name","category","source","shape","intent","state_input","relation_projection_overrides",...common],reference:["text"],entity_type_column:["display_name","value_type","enum_values","reserved_enum_values","required","default","format","min","max","min_length","max_length"],relation_type_column:["display_name","value_type","enum_values","reserved_enum_values","required","default","format","min","max","min_length","max_length"],entity_type_constraint:["column_addresses"],relation_type_constraint:["column_addresses"],query_parameter:["value_type","enum_values","reserved_enum_values","required","default","format","min","max","min_length","max_length"],view_table_column:["source","label","aggregate"],view_export:["format","filename","fidelity","source_refs","exporter_profile","options"]};
    const required:Readonly<Record<string,ReadonlyArray<string>>>={entity_type:["display_name","representation"],relation_type:["display_name","semantic_kind","from","to","forward_label"],layer:["display_name","order"],entity:["display_name","type_address","layer_address"],query:["display_name","select"],view:["display_name","category","source","shape"],reference:["text"],entity_type_column:["display_name","value_type"],relation_type_column:["display_name","value_type"],entity_type_constraint:["column_addresses"],relation_type_constraint:["column_addresses"],query_parameter:["value_type"],view_table_column:["source"],view_export:["format","filename","fidelity"]}; const names=allowed[kind]; if (names===undefined||Object.keys(fields).some((name)=>!names.includes(name))||required[kind]!.some((name)=>!hasOwn(fields,name))) return false;
    if (kind==="view") { const source=fields["source"],shape=fields["shape"],diff=fields["category"]==="diff"; if (!isObject(source)||!isObject(shape)||(source["kind"]==="diff")!==diff||(shape["kind"]==="diff")!==diff) return false; if (shape["kind"]==="matrix") { const cell=shape["cell"]; if (!isObject(cell)||(cell["display"]==="attribute_summary")!==hasOwn(cell,"attribute_column_addresses")) return false; } if (shape["kind"]==="flow"&&(shape["lane_by"]==="attribute")!==hasOwn(shape,"lane_column_addresses")) return false; }
    if (kind==="view_table_column") { const source=fields["source"]; if (!isObject(source)||source["kind"]==="query"||source["kind"]==="diff") return false; }
    if (kind.includes("_column")||kind==="query_parameter") { const type=fields["value_type"],defaultValue=fields["default"],enumValues=fields["enum_values"]; if (hasOwn(fields,"enum_values")!==(type==="enum")||hasOwn(fields,"reserved_enum_values")&&type!=="enum"||hasOwn(fields,"format")&&type!=="string"||(hasOwn(fields,"min")||hasOwn(fields,"max"))&&type!=="integer"&&type!=="number"||(hasOwn(fields,"min_length")||hasOwn(fields,"max_length"))&&type!=="string"||type==="integer"&&[fields["min"],fields["max"]].some((item)=>item!==undefined&&(typeof item!=="string"||!/^[-]?(0|[1-9][0-9]*)$/.test(item)))||hasOwn(fields,"default")&&(!isObject(defaultValue)||defaultValue["kind"]!==type||type==="enum"&&(!isJSONArray(enumValues)||!enumValues.includes(defaultValue["string_value"])))) return false; }
    const owners:Readonly<Record<string,string>>={entity_type_column:"entity_type",entity_type_constraint:"entity_type",relation_type_column:"relation_type",relation_type_constraint:"relation_type",query_parameter:"query",view_table_column:"view",view_export:"view"}; const parent=value["parent_address"]; return typeof parent==="string" && authoritySubject(parent)?.kind===(owners[kind]??"project"); }
  if (value["operation"]==="create_relation" && hasOwn(value,"fields")) return isObject(value["fields"]) && Object.keys(value["fields"]).every((name)=>["display_name","description","tags","annotations"].includes(name)); return true;
}
function hasProtocolInvariant(value: Record<string,unknown>,profile: string): boolean {
  if (profile==="authoring_impact_entry") { if (value["capability"]!=="graph:write") return true; const address=value["subject_address"], facts=value["graph_facts"]; return typeof address==="string"&&isObject(facts)&&authoritySubject(address)?.kind===value["subject_kind"]&&isJSONArray(facts["action_flags"])&&facts["action_flags"].length===1&&facts["action_flags"][0]===value["action"]; }
  if (profile==="authoring_impact") { const entries=value["entries"], capabilities=value["required_capabilities"]; if (!isJSONArray(entries)||!isJSONArray(capabilities)) return false; const derived=new Set(entries.map((item)=>isObject(item)?item["capability"]:undefined)); return derived.size===capabilities.length&&capabilities.every((item)=>derived.has(item)); }
  if (profile==="open_document_result") { return isObject(value["document_generation"])&&sameProtocolGeneration(value["document_generation"],{document_handle:value["document_handle"],value:value["document_generation"]["value"]}); }
  if (profile==="document_bound_input") { if (hasOwn(value,"document_handle")&&(!isObject(value["document_generation"])||!sameProtocolGeneration(value["document_generation"],{document_handle:value["document_handle"],value:value["document_generation"]["value"]}))) return false; return !hasOwn(value,"cursor") || isObject(value["cursor"])&&sameProtocolGeneration(value["document_generation"],value["cursor"]["document_generation"]); }
  if (profile==="paged_result") return protocolPagedResult(value);
  if (profile==="bounded_text_chunk") { const blob=value["blob"]; if (!isObject(blob)||typeof value["offset"]!=="string"||typeof value["total_bytes"]!=="string"||typeof blob["size"]!=="string") return false; try { const offset=BigInt(value["offset"]), total=BigInt(value["total_bytes"]), size=BigInt(blob["size"]); return offset<=total&&size<=total-offset&&blob["media_type"]==="text/plain; charset=utf-8"&&blob["lifetime"]==="request"&&(offset!==0n||size!==total||blob["digest"]===value["full_digest"]); } catch { return false; } }
  if (profile==="source_edit") { const blob=value["replacement_blob"]; if (isObject(blob)&&(blob["digest"]!==value["after_digest"]||blob["media_type"]!=="text/plain; charset=utf-8"||blob["lifetime"]!=="request")) return false; return value["kind"]!=="move" || value["before_digest"]===value["after_digest"]&&canonicalJSONStringify(value["before_module"])!==canonicalJSONStringify(value["after_module"]); }
  if (profile==="preview_result") { if (value["status"]!=="valid") return isJSONArray(value["conflicts"])&&value["conflicts"].length>0||protocolHasErrorDiagnostic(value["diagnostics"]); const impact=value["authoring_impact"], semantic=value["semantic_diff"], source=value["source_diff"], hashes=value["resulting_hashes"], preview=value["preview_id"], base=protocolGenerationKey(value["base_generation"]); return isObject(impact)&&isObject(semantic)&&isObject(source)&&isObject(hashes)&&isObject(preview)&&base!==undefined&&!protocolHasErrorDiagnostic(value["diagnostics"])&&value["authoring_impact_digest"]===impact["impact_digest"]&&canonicalJSONStringify(value["required_authoring_capabilities"])===canonicalJSONStringify(impact["required_capabilities"])&&impact["semantic_diff_hash"]===semantic["digest"]&&impact["source_diff_hash"]===source["digest"]&&impact["resulting_definition_hash"]===hashes["definition_hash"]&&preview["endpoint_instance_id"]===base[0]&&nextProtocolGeneration(value["base_generation"],value["proposed_generation"]); }
  if (profile==="apply_input") { const preview=value["preview_id"], base=protocolGenerationKey(value["base_generation"]); return isObject(preview)&&base!==undefined&&preview["endpoint_instance_id"]===base[0]; }
  if (profile==="apply_result") { const impact=value["authoring_impact"], source=value["source_diff"], hashes=value["resulting_hashes"]; return isObject(impact)&&isObject(source)&&isObject(hashes)&&impact["source_diff_hash"]===source["digest"]&&impact["resulting_definition_hash"]===hashes["definition_hash"]; }
  if (profile==="semantic_operation") return protocolSemanticOperation(value); return false;
}

`

const tsAuthorityRuntime = `function authorityStateType(field: string): string {
  if (["system.created_at","system.updated_at","provenance.observed_at","provenance.verified_at","provenance.stale_after"].includes(field)) return "datetime";
  if (["system.created_by.kind","system.updated_by.kind","provenance.source.kind","provenance.verified_by.kind"].includes(field)) return "enum";
  return field === "provenance.confidence" ? "number" : "string";
}

const authorityStateFields = ["system.created_at","system.updated_at","system.created_by.kind","system.created_by.id","system.created_by.display_name","system.updated_by.kind","system.updated_by.id","system.updated_by.display_name","system.created_revision","system.updated_revision","provenance.source.kind","provenance.source.label","provenance.source.uri","provenance.source.external_id","provenance.observed_at","provenance.verified_at","provenance.stale_after","provenance.verified_by.kind","provenance.verified_by.id","provenance.verified_by.display_name","provenance.confidence"] as const;
const authorityStateSubjects = ["entity","relation","entity_row","relation_row"] as const;

function hasValidStateRead(value: Record<string, unknown>): boolean { return typeof value["field_path"] === "string" && value["value_type"] === authorityStateType(value["field_path"]); }
function hasStateReadOrder(values: ReadonlyArray<unknown>): boolean { let previous = -1; for (const raw of values) { if (!isObject(raw) || !hasValidStateRead(raw)) return false; const subject = authorityStateSubjects.indexOf(raw["subject_kind"] as never); const field = authorityStateFields.indexOf(raw["field_path"] as never); const rank = subject * authorityStateFields.length + field; if (subject < 0 || field < 0 || rank <= previous) return false; previous = rank; } return true; }

function authorityRealDate(value: string): boolean { const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value); if (match === null || match[1] === "0000") return false; const year = Number(match[1]); const month = Number(match[2]); const day = Number(match[3]); const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0); const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]; return month >= 1 && month <= 12 && day >= 1 && day <= days[month - 1]!; }
function hasValidRecipeScalar(value: Record<string, unknown>): boolean { const kind = value["kind"]; const text = value["string_value"]; if (kind === "date") return typeof text === "string" && authorityRealDate(text); if (kind === "datetime") { if (typeof text !== "string") return false; const match = /^(\d{4}-\d{2}-\d{2})T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.(\d{1,3}))?Z$/.exec(text); return match !== null && authorityRealDate(match[1]!) && (match[2] === undefined || !match[2].endsWith("0")); } return kind !== "enum" || typeof text === "string" && text.length > 0; }

function authorityHostname(value: string): boolean { return value.length > 0 && value.length <= 253 && value === value.toLowerCase() && !value.endsWith(".") && value.split(".").every((label) => label.length > 0 && label.length <= 63 && /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(label)); }
function authorityParseIPv4(value: string): ReadonlyArray<number> | undefined { const parts = value.split("."); if (parts.length !== 4 || !parts.every((part) => /^(0|[1-9]\d{0,2})$/.test(part) && Number(part) <= 255)) return undefined; return parts.map(Number); }
function authorityParseIPv6(value: string): ReadonlyArray<number> | undefined {
  if (value.includes("%")) return undefined;
  let expanded = value;
  if (value.includes(".")) { const colon = value.lastIndexOf(":"); const ipv4 = colon < 0 ? undefined : authorityParseIPv4(value.slice(colon + 1)); if (ipv4 === undefined) return undefined; expanded = value.slice(0,colon + 1) + ((ipv4[0]! << 8) | ipv4[1]!).toString(16) + ":" + ((ipv4[2]! << 8) | ipv4[3]!).toString(16); }
  if (!/^[0-9A-Fa-f:]+$/.test(expanded) || expanded.split("::").length > 2) return undefined;
  const hasElision = expanded.includes("::"), halves = expanded.split("::");
  const left = halves[0] === "" ? [] : halves[0]!.split(":"), right = !hasElision || halves[1] === "" ? [] : halves[1]!.split(":");
  if (![...left,...right].every((part) => /^[0-9A-Fa-f]{1,4}$/.test(part))) return undefined;
  const omitted = 8 - left.length - right.length;
  if (hasElision ? omitted < 1 : omitted !== 0) return undefined;
  const words = [...left.map((part) => Number.parseInt(part,16)), ...Array(omitted).fill(0), ...right.map((part) => Number.parseInt(part,16))];
  return words.flatMap((word) => [word >>> 8, word & 255]);
}
function authorityFormatIPv6(bytes: ReadonlyArray<number>): string {
  const mapped = bytes.slice(0,10).every((value) => value === 0) && bytes[10] === 255 && bytes[11] === 255;
  if (mapped) return "::ffff:" + bytes.slice(12).join(".");
  const words = Array.from({length:8},(_,index) => (bytes[index*2]! << 8) | bytes[index*2+1]!);
  let bestStart = -1, bestLength = 0;
  for (let index = 0; index < words.length;) { if (words[index] !== 0) { index++; continue; } let end = index; while (end < words.length && words[end] === 0) end++; if (end-index > bestLength && end-index >= 2) { bestStart=index; bestLength=end-index; } index=end; }
  let result = "";
  for (let index = 0; index < words.length;) { if (index === bestStart) { result += "::"; index += bestLength; continue; } if (result !== "" && !result.endsWith(":")) result += ":"; result += words[index]!.toString(16); index++; }
  return result;
}
function authorityCanonicalIP(value: string): {bytes:ReadonlyArray<number>;bits:number} | undefined { const ipv4=authorityParseIPv4(value); if (ipv4 !== undefined) return {bytes:ipv4,bits:32}; const ipv6=authorityParseIPv6(value); return ipv6 !== undefined && authorityFormatIPv6(ipv6) === value ? {bytes:ipv6,bits:128} : undefined; }
function authorityCanonicalCIDR(value: string): boolean { const parts=value.split("/"); if (parts.length !== 2 || !/^(0|[1-9]\d*)$/.test(parts[1]!)) return false; const address=authorityCanonicalIP(parts[0]!); if (address === undefined) return false; const prefix=Number(parts[1]); if (!Number.isSafeInteger(prefix) || prefix > address.bits) return false; for (let index=0; index<address.bytes.length; index++) { const remaining=prefix-index*8; const mask=remaining >= 8 ? 255 : remaining <= 0 ? 0 : (255 << (8-remaining)) & 255; if ((address.bytes[index]! & mask) !== address.bytes[index]) return false; } return true; }
function authorityURIAlpha(value: string): boolean { return /^[A-Za-z]$/.test(value); }
function authorityURIDigit(value: string): boolean { return /^[0-9]$/.test(value); }
function authorityURIHex(value: string): boolean { return /^[0-9A-Fa-f]$/.test(value); }
function authorityURIUnreserved(value: string): boolean { return authorityURIAlpha(value) || authorityURIDigit(value) || "-._~".includes(value); }
function authorityURIComponent(value: string, allowEmpty: boolean, extra: string): boolean { if (value === "") return allowEmpty; for (let index=0; index<value.length; index++) { const ch=value[index]!; if (ch === "%") { if (index+2 >= value.length || !authorityURIHex(value[index+1]!) || !authorityURIHex(value[index+2]!)) return false; index += 2; continue; } if (!authorityURIUnreserved(ch) && !("!$&'()*+,;="+extra).includes(ch)) return false; } return true; }
function authorityIPLiteral(value: string): boolean { if (authorityParseIPv6(value) !== undefined) return true; if (value.length < 4 || value[0] !== "v" && value[0] !== "V") return false; const dot=value.indexOf("."); if (dot < 2 || !Array.from(value.slice(1,dot)).every(authorityURIHex)) return false; return value.slice(dot+1) !== "" && authorityURIComponent(value.slice(dot+1),false,":"); }
function authorityURIAuthority(value: string): boolean { if (value.split("@").length > 2) return false; let hostPort=value; const at=value.indexOf("@"); if (at >= 0) { if (!authorityURIComponent(value.slice(0,at),true,":")) return false; hostPort=value.slice(at+1); } if (hostPort.startsWith("[")) { const close=hostPort.indexOf("]"); if (close <= 1) return false; const rest=hostPort.slice(close+1); return authorityIPLiteral(hostPort.slice(1,close)) && (rest === "" || rest.startsWith(":") && Array.from(rest.slice(1)).every(authorityURIDigit)); } if (hostPort.includes("[") || hostPort.includes("]")) return false; let host=hostPort; const colon=hostPort.lastIndexOf(":"); if (colon >= 0) { host=hostPort.slice(0,colon); if (host.includes(":") || !Array.from(hostPort.slice(colon+1)).every(authorityURIDigit)) return false; } return authorityURIComponent(host,true,""); }
function authorityAbsoluteURI(value: string): boolean { const colon=value.indexOf(":"); if (colon <= 0 || !/^[A-Za-z][A-Za-z0-9+.-]*$/.test(value.slice(0,colon)) || Array.from(value).some((ch) => ch.codePointAt(0)! >= 128 || ch.codePointAt(0)! < 32 || ch.codePointAt(0) === 127) || value.includes("\\")) return false; for (let index=0; index<value.length; index++) { const ch=value[index]!; if (ch === "%") { if (index+2 >= value.length || !authorityURIHex(value[index+1]!) || !authorityURIHex(value[index+2]!)) return false; index += 2; continue; } if (!authorityURIUnreserved(ch) && !":/?#[]@!$&'()*+,;=%".includes(ch)) return false; } const remainder=value.slice(colon+1); if (remainder.split("#").length > 2) return false; const hash=remainder.indexOf("#"), beforeFragment=hash < 0 ? remainder : remainder.slice(0,hash), fragment=hash < 0 ? undefined : remainder.slice(hash+1); if (fragment !== undefined && !authorityURIComponent(fragment,true,"/?:@")) return false; const question=beforeFragment.indexOf("?"), hierarchical=question < 0 ? beforeFragment : beforeFragment.slice(0,question), query=question < 0 ? undefined : beforeFragment.slice(question+1); if (query !== undefined && !authorityURIComponent(query,true,"/?:@")) return false; if (hierarchical.startsWith("//")) { const authorityAndPath=hierarchical.slice(2), slash=authorityAndPath.indexOf("/"), authority=slash < 0 ? authorityAndPath : authorityAndPath.slice(0,slash), path=slash < 0 ? "" : authorityAndPath.slice(slash); return authorityURIAuthority(authority) && authorityURIComponent(path,true,"/:@"); } return authorityURIComponent(hierarchical,true,"/:@"); }
function authorityStringFormat(format: string, value: string): boolean {
  if (format === "hostname") return authorityHostname(value);
  if (format === "email") { const match = new RegExp("^[A-Za-z0-9!#$%&'*+/=?^_\\x60{|}~-]+(?:\\.[A-Za-z0-9!#$%&'*+/=?^_\\x60{|}~-]+)*@([A-Za-z0-9.-]+)$").exec(value); return match !== null && authorityHostname(match[1]!.toLowerCase()); }
  if (format === "ipv4") return authorityParseIPv4(value) !== undefined;
  if (format === "ipv6") { const parsed=authorityParseIPv6(value); return parsed !== undefined && authorityFormatIPv6(parsed) === value; }
  if (format === "cidr") return authorityCanonicalCIDR(value);
  if (format === "uri") return authorityAbsoluteURI(value);
  return false;
}

function hasValidQueryParameter(value: Record<string, unknown>): boolean {
  const type = value["value_type"]; const reserved = value["reserved_enum_values"];
  if (typeof type !== "string" || !isJSONArray(reserved)) return false;
  const hasEnum = hasOwn(value, "enum_values"), hasFormat = hasOwn(value, "format"), hasMin = hasOwn(value, "min"), hasMax = hasOwn(value, "max"), hasMinLength = hasOwn(value, "min_length"), hasMaxLength = hasOwn(value, "max_length");
  if (type === "enum") { const active = value["enum_values"]; if (!isJSONArray(active) || active.length === 0 || !active.every((item) => typeof item === "string" && item.length > 0) || !reserved.every((item) => typeof item === "string" && item.length > 0)) return false; }
  else if (hasEnum || reserved.length !== 0) return false;
  if (hasFormat && type !== "string" || (hasMin || hasMax) && type !== "integer" && type !== "number" || (hasMinLength || hasMaxLength) && type !== "string") return false;
  if (type === "integer" && [value["min"], value["max"]].some((item) => item !== undefined && (typeof item !== "string" || !matchesCanonicalSafeInteger(item)))) return false;
  const rawDefault = value["default"]; if (rawDefault === undefined) return true; if (!isObject(rawDefault) || rawDefault["kind"] !== type) return false;
  if (type === "enum") return (value["enum_values"] as ReadonlyArray<unknown>).includes(rawDefault["string_value"]);
  if (type === "string") { const text = rawDefault["string_value"]; if (typeof text !== "string") return false; const length = Array.from(text).length; if (typeof value["min_length"] === "string" && BigInt(length) < BigInt(value["min_length"]) || typeof value["max_length"] === "string" && BigInt(length) > BigInt(value["max_length"])) return false; return typeof value["format"] !== "string" || authorityStringFormat(value["format"], text); }
  if (type === "integer" || type === "number") { const property = type === "integer" ? "integer_value" : "number_value"; const text = rawDefault[property]; if (typeof text !== "string") return false; const number = Number(text); return Number.isFinite(number) && (typeof value["min"] !== "string" || number >= Number(value["min"])) && (typeof value["max"] !== "string" || number <= Number(value["max"])); }
  return true;
}

type AuthoritySubject = {kind: string; owner: string};
function authoritySubject(address: string): AuthoritySubject | undefined { const parts = address.split(":"); if (parts.length < 3 || parts[0] !== "ldl") return undefined; let start = 3, root = "project"; if (parts[1] === "pack") { if (parts.length < 4) return undefined; start = 4; root = "pack"; } else if (parts[1] !== "project") return undefined; if (parts.length === start) return {kind: root, owner: ""}; if ((parts.length - start) % 2 !== 0) return undefined; const last = parts.at(-2)!; let kind = new Map<string,string>([["entity-type","entity_type"],["relation-type","relation_type"],["layer","layer"],["entity","entity"],["relation","relation"],["query","query"],["view","view"],["reference","reference"],["parameter","query_parameter"],["table-column","view_table_column"],["export","view_export"]]).get(last); if (last === "row") kind = parts.at(-4) === "entity" ? "entity_row" : parts.at(-4) === "relation" ? "relation_row" : undefined; if (last === "column" || last === "constraint") { const prefix = parts.at(-4) === "entity-type" ? "entity_type" : parts.at(-4) === "relation-type" ? "relation_type" : undefined; kind = prefix === undefined ? undefined : prefix + "_" + last; } return kind === undefined ? undefined : {kind, owner: parts.slice(0, -2).join(":")}; }
type AuthorityRole = {kind: string; address?: string; addresses?: string; owner?: string; owner_policy?: string};
function hasStableAddressRoles(value: Record<string, unknown>, rules: ReadonlyArray<AuthorityRole>): boolean { for (const rule of rules) { const expected = value[rule.kind]; if (typeof expected !== "string") return false; let addresses: ReadonlyArray<unknown>; if (rule.address !== undefined) addresses = [value[rule.address]]; else { const raw = value[rule.addresses!]; if (!isJSONArray(raw)) return false; addresses = raw; } const owner = rule.owner === undefined ? undefined : value[rule.owner]; for (const raw of addresses) { if (typeof raw !== "string") return false; const subject = authoritySubject(raw); if (subject === undefined || subject.kind !== expected) return false; const present = typeof owner === "string"; if (rule.owner_policy === "children" && (!present || subject.owner !== owner) || rule.owner_policy === "exact" && ((subject.owner !== "") !== present || present && subject.owner !== owner) || rule.owner_policy === "if_present" && present && subject.owner !== owner || rule.owner_policy === "row_only" && ((subject.kind === "entity_row" || subject.kind === "relation_row") !== present || present && subject.owner !== owner)) return false; } } return true; }

type AuthorityOperand = {kind: string; scalar: string; address: string};
function authorityOperand(raw: unknown): AuthorityOperand | undefined { if (!isObject(raw) || typeof raw["kind"] !== "string") return undefined; const value = {kind: raw["kind"], scalar: typeof raw["scalar_type"] === "string" ? raw["scalar_type"] : "", address: typeof raw["address_kind"] === "string" ? raw["address_kind"] : ""}; return value.kind === "scalar" && value.scalar !== "" || value.kind === "address" && value.address !== "" || value.kind === "string_set" ? value : undefined; }
function authorityOperandEqual(a: AuthorityOperand, b: AuthorityOperand): boolean { return a.kind === b.kind && a.scalar === b.scalar && a.address === b.address; }
function authorityFieldOperand(field: string): AuthorityOperand | undefined { if (["id","display_name","description"].includes(field)) return {kind:"scalar",scalar:"string",address:""}; if (field === "tags") return {kind:"string_set",scalar:"",address:""}; if (field === "layer") return {kind:"address",scalar:"",address:"layer"}; if (field === "from" || field === "to") return {kind:"address",scalar:"",address:"entity"}; return undefined; }
function authorityScalarCompare(a: Record<string, unknown>, b: Record<string, unknown>): number { if (a["kind"] === "boolean") return Number(a["boolean_value"]) - Number(b["boolean_value"]); if (a["kind"] === "integer") return BigInt(String(a["integer_value"])) < BigInt(String(b["integer_value"])) ? -1 : BigInt(String(a["integer_value"])) > BigInt(String(b["integer_value"])) ? 1 : 0; if (a["kind"] === "number") return Number(a["number_value"]) - Number(b["number_value"]); return compareUnicodeScalars(String(a["string_value"]), String(b["string_value"])); }
function authorityScalarSet(raw: unknown, type: string): boolean { return isJSONArray(raw) && raw.every((item) => isObject(item) && item["kind"] === type); }
function authorityPredicateValue(value: Record<string, unknown>, operator: string, operand: AuthorityOperand): boolean { const kind = value["kind"]; if (kind === "parameter") return operator !== "in" && operator !== "not_in" && (operand.kind === "scalar" || operand.kind === "string_set" && operator === "contains"); if (operator === "in" || operator === "not_in") { if (operand.kind === "scalar") return kind === "scalar_set" && authorityScalarSet(value["scalar_values"], operand.scalar); if (operand.kind === "address" && kind === "address_set" && isJSONArray(value["address_values"])) return value["address_values"].every((item) => typeof item === "string" && authoritySubject(item)?.kind === operand.address); return false; } if (operand.kind === "string_set") return operator === "eq" || operator === "ne" ? kind === "scalar_set" && authorityScalarSet(value["scalar_values"], "string") : operator === "contains" && kind === "scalar" && isObject(value["scalar_value"]) && value["scalar_value"]["kind"] === "string"; if (operand.kind === "scalar") return kind === "scalar" && isObject(value["scalar_value"]) && value["scalar_value"]["kind"] === operand.scalar; return operand.kind === "address" && kind === "address" && typeof value["address_value"] === "string" && authoritySubject(value["address_value"])?.kind === operand.address; }
function hasValidRecipePredicate(value: Record<string, unknown>, predicateKind: string): boolean { const kind = value["kind"]; if (kind === "all" || kind === "any") return isJSONArray(value["children"]) && value["children"].every((item) => isObject(item) && hasValidRecipePredicate(item, predicateKind)); if (kind === "not") return isObject(value["child"]) && hasValidRecipePredicate(value["child"], predicateKind); if (kind === "rows") return isObject(value["predicate"]) && hasValidRecipePredicate(value["predicate"], "row"); if (kind !== "field" && kind !== "cell" && kind !== "state") return true; const operand = authorityOperand(value["operand_type"]); if (operand === undefined) return false; if (kind === "field") { const expected = authorityFieldOperand(String(value["field"])); if (expected !== undefined && !authorityOperandEqual(operand, expected)) return false; } if (kind === "state" && !authorityOperandEqual(operand, {kind:"scalar",scalar:authorityStateType(String(value["field_path"])),address:""})) return false; const operator = value["operator"]; if (typeof operator !== "string") return false; let compatible = ["eq","ne","exists","missing"].includes(operator); if (["lt","lte","gt","gte"].includes(operator)) compatible = operand.kind === "scalar" && ["integer","number","date","datetime"].includes(operand.scalar); if (["in","not_in"].includes(operator)) compatible = operand.kind === "scalar" || operand.kind === "address"; if (operator === "contains") compatible = operand.kind === "string_set" || operand.kind === "scalar" && operand.scalar === "string"; if (["starts_with","ends_with"].includes(operator)) compatible = operand.kind === "scalar" && operand.scalar === "string"; return compatible && (["exists","missing"].includes(operator) || isObject(value["value"]) && authorityPredicateValue(value["value"], operator, operand)); }
function hasValidViewProjection(value: Record<string, unknown>, kind: string): boolean { const distinct = (a: string, b: string): boolean => typeof value[a] === "string" && typeof value[b] === "string" && value[a] !== value[b]; if (kind !== "composed") { const pairs = new Map<string,readonly [string,string]>([["diagram",["source_endpoint","target_endpoint"]],["flow",["source_endpoint","target_endpoint"]],["matrix",["row_endpoint","column_endpoint"]],["tree",["parent_endpoint","child_endpoint"]]]); const pair = pairs.get(kind); return pair !== undefined && distinct(pair[0], pair[1]); } const mode = value["mode"]; const present = (name: string): boolean => hasOwn(value, name); if (mode === "nest") return distinct("parent_endpoint","child_endpoint") && !present("overlay_endpoint") && !present("target_endpoint") && !present("badge_endpoint"); if (mode === "overlay") return distinct("overlay_endpoint","target_endpoint") && !present("parent_endpoint") && !present("child_endpoint") && !present("badge_endpoint"); if (mode === "badge") return distinct("badge_endpoint","target_endpoint") && !present("parent_endpoint") && !present("child_endpoint") && !present("overlay_endpoint"); return (mode === "edge" || mode === "hide") && ["parent_endpoint","child_endpoint","overlay_endpoint","target_endpoint","badge_endpoint"].every((name) => !present(name)); }

function authorityContextOperand(field: string, context: string): AuthorityOperand | undefined { if (["id","display_name","description"].includes(field)) return {kind:"scalar",scalar:"string",address:""}; if (field === "tags") return {kind:"string_set",scalar:"",address:""}; if (field === "layer") return context === "entity" ? {kind:"address",scalar:"",address:"layer"} : undefined; if (field === "from" || field === "to") return context === "relation" ? {kind:"address",scalar:"",address:"entity"} : undefined; if (field === "address") return context === "entity" ? {kind:"address",scalar:"",address:"entity"} : context === "relation" ? {kind:"address",scalar:"",address:"relation"} : undefined; if (field === "type") return context === "entity" ? {kind:"address",scalar:"",address:"entity_type"} : context === "relation" ? {kind:"address",scalar:"",address:"relation_type"} : undefined; return undefined; }
type AuthorityDependencies = {layer:Set<string>; entity_type:Set<string>; relation_type:Set<string>; entity:Set<string>; relation:Set<string>; column:Set<string>; parameter:Set<string>; state:Map<string,Record<string,unknown>>};
function authorityDependencies(): AuthorityDependencies { return {layer:new Set(),entity_type:new Set(),relation_type:new Set(),entity:new Set(),relation:new Set(),column:new Set(),parameter:new Set(),state:new Map()}; }
function authorityAddDependency(sets: AuthorityDependencies, kind: string, address: string): boolean { const target = kind === "entity_type_column" || kind === "relation_type_column" ? sets.column : sets[kind as keyof AuthorityDependencies]; if (!(target instanceof Set)) return false; target.add(address); return true; }
function authorityQueryPredicate(raw: unknown, context: string, query: string, parameters: ReadonlyMap<string,string>, sets: AuthorityDependencies): boolean { if (!isObject(raw)) return false; const kind = raw["kind"]; if (kind === "all" || kind === "any") return isJSONArray(raw["children"]) && raw["children"].every((item) => authorityQueryPredicate(item, context, query, parameters, sets)); if (kind === "not") return authorityQueryPredicate(raw["child"], context, query, parameters, sets); if (kind === "rows") { if (!isJSONArray(raw["type_addresses"])) return false; const expected = context === "entity" ? "entity_type" : "relation_type", row = context === "entity" ? "entity_row" : "relation_row"; for (const item of raw["type_addresses"]) { if (typeof item !== "string" || authoritySubject(item)?.kind !== expected) return false; authorityAddDependency(sets, expected, item); } return authorityQueryPredicate(raw["predicate"], row, query, parameters, sets); } if (kind === "field") { const expected = authorityContextOperand(String(raw["field"]), context), operand = authorityOperand(raw["operand_type"]); if (expected === undefined || operand === undefined || !authorityOperandEqual(expected, operand)) return false; } else if (kind === "cell") { if (context !== "entity_row" && context !== "relation_row" || !isJSONArray(raw["column_addresses"]) || raw["column_addresses"].length === 0) return false; const expected = context === "entity_row" ? "entity_type_column" : "relation_type_column"; for (const item of raw["column_addresses"]) { if (typeof item !== "string" || authoritySubject(item)?.kind !== expected) return false; sets.column.add(item); } } else if (kind === "state") { const field = String(raw["field_path"]), operand = authorityOperand(raw["operand_type"]); if (operand === undefined || !authorityOperandEqual(operand,{kind:"scalar",scalar:authorityStateType(field),address:""})) return false; sets.state.set(context+"\0"+field,{subject_kind:context,field_path:field,value_type:authorityStateType(field)}); } else return false; const operand = authorityOperand(raw["operand_type"])!; if (isObject(raw["value"])) { const value = raw["value"]; if (value["kind"] === "parameter") { const address = value["parameter_address"], expected = operand.kind === "string_set" ? "string" : operand.scalar; if (typeof address !== "string" || authoritySubject(address)?.kind !== "query_parameter" || authoritySubject(address)?.owner !== query || parameters.get(address) !== expected) return false; sets.parameter.add(address); } for (const address of [...(typeof value["address_value"] === "string" ? [value["address_value"]] : []), ...(isJSONArray(value["address_values"]) ? value["address_values"] : [])]) { if (typeof address !== "string") return false; const subject = authoritySubject(address); if (subject === undefined || !authorityAddDependency(sets, subject.kind, address)) return false; } } return true; }
function authoritySetEquals(raw: unknown, expected: ReadonlySet<string>): boolean { return isJSONArray(raw) && raw.length === expected.size && raw.every((item) => typeof item === "string" && expected.has(item)); }
function hasValidQueryRecipe(value: Record<string, unknown>): boolean { const query = value["address"]; if (typeof query !== "string" || !isJSONArray(value["parameters"])) return false; const parameters = new Map<string,string>(); for (const raw of value["parameters"]) { if (!isObject(raw) || typeof raw["address"] !== "string" || typeof raw["value_type"] !== "string") return false; parameters.set(raw["address"],raw["value_type"]); } const sets = authorityDependencies(); const select = value["select"]; if (!isObject(select)) return false; for (const [property,kind] of [["layer_addresses","layer"],["entity_type_addresses","entity_type"],["relation_type_addresses","relation_type"],["root_addresses","entity"]] as const) if (isJSONArray(select[property])) for (const address of select[property]) { if (typeof address !== "string") return false; authorityAddDependency(sets,kind,address); } if (isObject(value["traverse"]) && isJSONArray(value["traverse"]["relation_type_addresses"])) for (const address of value["traverse"]["relation_type_addresses"]) { if (typeof address !== "string") return false; sets.relation_type.add(address); } if (!authorityQueryPredicate(value["where"],"entity",query,parameters,sets) || !authorityQueryPredicate(value["relation_where"],"relation",query,parameters,sets)) return false; const hasState = sets.state.size !== 0; if (hasState !== (value["state_input"] === "optional" || value["state_input"] === "required")) return false; const dependencies = value["dependencies"]; if (!isObject(dependencies)) return false; for (const property of ["layer","entity_type","relation_type","entity","relation","column","parameter"] as const) if (!authoritySetEquals(dependencies[property+"_addresses"],sets[property])) return false; if (!isJSONArray(dependencies["state_reads"]) || dependencies["state_reads"].length !== sets.state.size) return false; return dependencies["state_reads"].every((raw) => isObject(raw) && sets.state.has(String(raw["subject_kind"])+"\0"+String(raw["field_path"]))); }

function authorityFidelityRank(value: unknown): number { return new Map<unknown,number>([["lossy",0],["visual_only",1],["traceable_summary",2],["lossless",3]]).get(value) ?? -1; }
function hasValidExportRecipe(value: Record<string, unknown>): boolean { const format = value["format"], options = value["options"], profile = value["exporter_profile"], extension = value["extension"], filename = value["filename"]; if (typeof format !== "string" || !isObject(options) || !isObject(profile) || options["kind"] !== format || profile["format"] !== format || typeof extension !== "string" || typeof filename !== "string") return false; const expected = new Map<string,string>([["json",".json"],["yaml",".yaml"],["svg",".svg"],["png",".png"],["pdf",".pdf"],["html",".html"],["csv",".csv"],["tsv",".tsv"],["xlsx",".xlsx"],["markdown",".md"],["pptx",".pptx"],["docx",".docx"],["mermaid",".mmd"],["bpmn",".bpmn"],["drawio",".drawio"]]).get(format); if (expected === undefined || extension !== expected || filename === "" || filename === "." || filename === ".." || /[\\/\u0000]/.test(filename) || !filename.endsWith(extension) || filename.slice(0,-extension.length).length === 0) return false; const fixed = new Map<string,string>([["json","lossless"],["yaml","lossless"],["svg","visual_only"],["png","visual_only"],["pdf","visual_only"],["html","traceable_summary"],["xlsx","traceable_summary"],["pptx","visual_only"],["docx","visual_only"],["drawio","visual_only"],["bpmn","lossy"]]).get(format); if (fixed !== undefined && value["native_maximum_fidelity"] !== fixed) return false; if ((format === "csv" || format === "tsv") && value["native_maximum_fidelity"] !== (options["bundle"] === true && options["header"] === true && options["source_manifest"] === true ? "traceable_summary" : "lossy")) return false; const embedded = format === "xlsx" && options["view_data_json"] === true && options["hidden_ids"] === true; if (embedded ? value["fidelity_basis"] !== "embedded_viewdata" || value["effective_maximum_fidelity"] !== "lossless" : value["fidelity_basis"] !== "native" || value["effective_maximum_fidelity"] !== value["native_maximum_fidelity"]) return false; if (authorityFidelityRank(value["fidelity"]) > authorityFidelityRank(value["effective_maximum_fidelity"])) return false; if ((["lossless","traceable_summary"].includes(String(value["fidelity"])) || format === "json" || format === "yaml") && value["source_refs"] !== true) return false; const embeddedManifest = format === "json" || format === "yaml" || format === "xlsx" && options["view_data_json"] === true; const explicitManifest = ["csv","tsv","markdown","mermaid","bpmn","drawio"].includes(format) && options["source_manifest"] === true; return !(explicitManifest || value["source_refs"] === true && !embeddedManifest) || value["requires_source_manifest"] === true; }

function authorityTableValue(value: Record<string, unknown>, kind: string, scalar = "", enumValues?: ReadonlyArray<string>): boolean { if (!isObject(value["value_type"])) return false; const type = value["value_type"]; if (type["kind"] !== kind || kind === "scalar" && type["scalar_type"] !== scalar) return false; if (enumValues !== undefined && (!isJSONArray(type["enum_values"]) || type["enum_values"].length !== enumValues.length || !type["enum_values"].every((item,index) => item === enumValues[index]))) return false; return true; }
function authorityStateEnum(field: string): ReadonlyArray<string> | undefined { if (["system.created_by.kind","system.updated_by.kind","provenance.verified_by.kind"].includes(field)) return ["user","agent","service_account","anonymous"]; return field === "provenance.source.kind" ? ["manual","import","api","agent","external_system"] : undefined; }
function authorityManifest(value: Record<string, unknown>, state: string, embedded: boolean): boolean { const options = value["options"]; if (!isObject(options)) return false; const explicit = (["csv","tsv"].includes(String(options["kind"])) || ["markdown","mermaid","bpmn","drawio"].includes(String(options["kind"]))) && options["source_manifest"] === true; return value["requires_source_manifest"] === (explicit || state !== "none" || value["source_refs"] === true && !embedded); }
function authorityExportInView(value: Record<string, unknown>, category: string, shape: string, state: string, diff: boolean): boolean { const format = String(value["format"]), options = value["options"]; if (!isObject(options)) return false; if (format === "json" || format === "yaml") return value["native_maximum_fidelity"] === "lossless" && value["effective_maximum_fidelity"] === "lossless" && value["fidelity_basis"] === "native" && authorityManifest(value,state,true) && !(diff && options["state_summary"] === true); const matrix: Record<string,Record<string,string>> = {diagram:{xlsx:"traceable_summary",html:"traceable_summary",csv:"traceable_summary",tsv:"traceable_summary",svg:"visual_only",png:"visual_only",pdf:"visual_only",pptx:"visual_only",docx:"visual_only",drawio:"visual_only",mermaid:"lossy"},table:{xlsx:"traceable_summary",csv:"traceable_summary",tsv:"traceable_summary",html:"traceable_summary",pdf:"visual_only",pptx:"visual_only",docx:"visual_only",markdown:"lossy"},matrix:{xlsx:"traceable_summary",csv:"traceable_summary",tsv:"traceable_summary",html:"traceable_summary",svg:"visual_only",png:"visual_only",pdf:"visual_only",pptx:"visual_only",docx:"visual_only"},tree:{xlsx:"traceable_summary",csv:"traceable_summary",tsv:"traceable_summary",html:"traceable_summary",mermaid:"traceable_summary",svg:"visual_only",png:"visual_only",pdf:"visual_only",pptx:"visual_only",docx:"visual_only",drawio:"visual_only"},flow:{xlsx:"traceable_summary",csv:"traceable_summary",tsv:"traceable_summary",html:"traceable_summary",mermaid:"traceable_summary",bpmn:"lossy",svg:"visual_only",png:"visual_only",pdf:"visual_only",pptx:"visual_only",docx:"visual_only",drawio:"visual_only",markdown:"lossy"},context:{csv:"traceable_summary",tsv:"traceable_summary",xlsx:"traceable_summary",html:"traceable_summary",markdown:"traceable_summary",pdf:"visual_only",pptx:"visual_only",docx:"visual_only"},diff:{csv:"traceable_summary",tsv:"traceable_summary",xlsx:"traceable_summary",html:"traceable_summary",markdown:"traceable_summary",pdf:"visual_only",pptx:"visual_only",docx:"visual_only"}}; let native = matrix[shape]?.[format]; if (native === undefined) return false; if ((format === "csv" || format === "tsv") && !(options["bundle"] === true && options["header"] === true && options["source_manifest"] === true) || (shape === "tree" || shape === "flow") && format === "mermaid" && options["source_manifest"] !== true) native = "lossy"; if (value["native_maximum_fidelity"] !== native) return false; const fidelityEmbedded = format === "xlsx" && options["view_data_json"] === true && options["hidden_ids"] === true; if (fidelityEmbedded ? value["effective_maximum_fidelity"] !== "lossless" || value["fidelity_basis"] !== "embedded_viewdata" : value["effective_maximum_fidelity"] !== native || value["fidelity_basis"] !== "native") return false; if (format === "xlsx") { const profile = options["profile"]; const compatible = profile === "type_workbook" && shape === "table" || ["diagram_workbook","composed_diagram_workbook","diagram_inventory_workbook"].includes(String(profile)) && shape === "diagram" || profile === "matrix_workbook" && shape === "matrix" || profile === "tree_workbook" && shape === "tree" || profile === "flow_workbook" && shape === "flow" || profile === "diff_workbook" && shape === "diff" || profile === "context_workbook" && shape === "context" || profile === "impact_workbook" && category === "impact" && ["diagram","table","matrix"].includes(shape); if (!compatible) return false; } return authorityManifest(value,state,format === "xlsx" && options["view_data_json"] === true); }
type AuthorityViewDependencies = {query:Set<string>;parameter:Set<string>;layer:Set<string>;entity_type:Set<string>;relation_type:Set<string>;entity:Set<string>;relation:Set<string>;column:Set<string>};
function authorityViewDependencies(): AuthorityViewDependencies { return {query:new Set(),parameter:new Set(),layer:new Set(),entity_type:new Set(),relation_type:new Set(),entity:new Set(),relation:new Set(),column:new Set()}; }
function authorityAddViewDependencyValues(raw: unknown, sets: AuthorityViewDependencies): void { const values = typeof raw === "string" ? [raw] : isJSONArray(raw) ? raw : []; for (const item of values) { if (typeof item !== "string") continue; const subject=authoritySubject(item); if (subject === undefined) continue; const property=subject.kind === "query_parameter" ? "parameter" : subject.kind === "entity_type_column" || subject.kind === "relation_type_column" ? "column" : subject.kind; const target=sets[property as keyof AuthorityViewDependencies]; if (target instanceof Set) target.add(item); } }
function authorityCollectViewDependencies(raw: unknown, sets: AuthorityViewDependencies): void { if (isJSONArray(raw)) { for (const item of raw) authorityCollectViewDependencies(item,sets); return; } if (!isObject(raw)) return; for (const [property,item] of Object.entries(raw)) { if (property === "arguments") { if (isObject(item)) for (const address of Object.keys(item)) authorityAddViewDependencyValues(address,sets); continue; } if (["query_address","entity_address","relation_address","layer_address","parameter_address","branch_value_column_address","layer_addresses","entity_type_addresses","relation_type_addresses","entity_addresses","relation_addresses","column_addresses","lane_column_addresses","attribute_column_addresses"].includes(property)) { authorityAddViewDependencyValues(item,sets); continue; } authorityCollectViewDependencies(item,sets); } }
function authorityContainsViewDependencies(raw: unknown, expected: ReadonlySet<string>): boolean { return isJSONArray(raw) && [...expected].every((value) => raw.includes(value)); }
function authorityValidViewDependencies(value: Record<string,unknown>): boolean { const dependencies=value["dependencies"], source=value["source"], shape=value["shape"], overrides=value["relation_projection_overrides"], exports=value["exports"]; if (!isObject(dependencies) || !isObject(source) || !isObject(shape) || !isObject(overrides) || !isJSONArray(exports)) return false; const sets=authorityViewDependencies(); authorityCollectViewDependencies(source,sets); authorityCollectViewDependencies(shape,sets); for (const [address,override] of Object.entries(overrides)) { authorityAddViewDependencyValues(address,sets); authorityCollectViewDependencies(override,sets); } if (!authoritySetEquals(dependencies["query_addresses"],sets.query)) return false; const hasSourceQuery=typeof source["query_address"] === "string"; for (const property of ["parameter","layer","entity_type","relation_type","entity","relation","column"] as const) if (hasSourceQuery ? !authorityContainsViewDependencies(dependencies[property+"_addresses"],sets[property]) : !authoritySetEquals(dependencies[property+"_addresses"],sets[property])) return false; const addresses=dependencies["export_addresses"]; return isJSONArray(addresses) && addresses.length === exports.length && exports.every((raw,index) => isObject(raw) && raw["address"] === addresses[index]); }
function hasValidViewRecipe(value: Record<string, unknown>): boolean { const address = value["address"], shapeValue = value["shape"], source = value["source"], reservedRaw = value["reserved_table_column_ids"]; if (typeof address !== "string" || !isObject(shapeValue) || !isObject(source) || !isJSONArray(reservedRaw)) return false; const category = String(value["category"]), sourceKind = String(source["kind"]), shape = String(shapeValue["kind"]); const diffCount = Number(category === "diff") + Number(sourceKind === "diff") + Number(shape === "diff"); if (diffCount !== 0 && diffCount !== 3) return false; const stateInput = String(value["state_input"]), stateRequirement = String(value["state_requirement"]), rank = (item: string): number => ["none","optional","required"].indexOf(item); if (rank(stateRequirement) < rank(stateInput) || diffCount === 3 && stateRequirement !== "none") return false; const direct: Array<Record<string,unknown>> = []; if (shape === "table") { const table = shapeValue["table"]; if (!isObject(table) || !isJSONArray(table["columns"]) || !isJSONArray(table["sorts"])) return false; const row = String(table["row_source"]), entity = row === "entity" || row === "entity_rows"; if (!entity && (table["include_entity_id"] === true || table["include_type"] === true || table["include_layer"] === true || hasOwn(table,"entity_type_addresses"))) return false; const available = new Set<string>(), reserved = new Set(reservedRaw); if (table["include_entity_id"] === true) available.add("entity_id"); if (table["include_type"] === true) available.add("entity_type"); if (table["include_layer"] === true) available.add("entity_layer"); const automatic = table["automatic_relation_columns"]; if (!isJSONArray(automatic) || row !== "automatic_relations" && automatic.length !== 0 || !automatic.every((item) => typeof item === "string")) return false; for (const item of automatic) available.add(item); for (const raw of table["columns"]) { if (!isObject(raw) || typeof raw["id"] !== "string" || typeof raw["address"] !== "string" || !hasDirectStableAddressOwner(address,raw["address"]) || reserved.has(raw["id"]) || available.has(raw["id"]) || !isObject(raw["source"])) return false; available.add(raw["id"]); const column = raw["source"], kind = column["kind"], aggregate = raw["aggregate"]; if (kind === "attribute") { if (row !== "entity_rows" && row !== "relation_rows" || !isJSONArray(column["column_addresses"]) || column["column_addresses"].length === 0) return false; const expected = row === "entity_rows" ? "entity_type_column" : "relation_type_column"; if (!column["column_addresses"].every((item) => typeof item === "string" && authoritySubject(item)?.kind === expected)) return false; } else if (kind === "relation_endpoint") { if (row !== "relation" && row !== "relation_rows") return false; const field = String(column["field"]); if (!authorityTableValue(raw,field === "id" || field === "display_name" ? "scalar" : "stable_address",field === "id" || field === "display_name" ? "string" : "")) return false; } else if (kind === "derived_count") { if (!entity || !authorityTableValue(raw,"scalar","integer")) return false; } else if (kind === "field") { const field = String(column["field"]); if (["id","display_name","description"].includes(field) ? !authorityTableValue(raw,"scalar","string") : field === "tags" ? !authorityTableValue(raw,"string_set") : !authorityTableValue(raw,"stable_address")) return false; } else if (kind === "state") { const field = String(column["field_path"]), type = authorityStateType(field), enums = authorityStateEnum(field); if (!authorityTableValue(raw,"scalar",type,enums)) return false; const subjects = row === "automatic_relations" ? ["relation","relation_row"] : [row === "entity" ? "entity" : row === "entity_rows" ? "entity_row" : row === "relation" ? "relation" : "relation_row"]; for (const subject_kind of subjects) direct.push({subject_kind,field_path:field,value_type:type}); } else return false; if ((aggregate === "count" || aggregate === "count_distinct") && !authorityTableValue(raw,"scalar","integer") || aggregate === "join_unique" && !authorityTableValue(raw,"scalar","string") || (aggregate === "min" || aggregate === "max") && (!isObject(raw["value_type"]) || raw["value_type"]["kind"] !== "scalar" || !["integer","number","date","datetime","enum"].includes(String(raw["value_type"]["scalar_type"])))) return false; } if (!table["sorts"].every((item) => isObject(item) && typeof item["column_id"] === "string" && available.has(item["column_id"]))) return false; } else if (stateInput !== "none") return false; if ((direct.length !== 0) !== (stateInput === "optional" || stateInput === "required")) return false; const dependencies = value["dependencies"]; if (!isObject(dependencies)) return false; const stateReads = dependencies["state_reads"]; if (!isJSONArray(stateReads) || direct.some((read) => !stateReads.some((item: unknown) => isObject(item) && item["subject_kind"] === read["subject_kind"] && item["field_path"] === read["field_path"] && item["value_type"] === read["value_type"]))) return false; if (typeof source["query_address"] !== "string" && stateReads.length !== direct.length) return false; if (!isJSONArray(value["exports"]) || !authorityValidViewDependencies(value)) return false; return value["exports"].every((raw) => isObject(raw) && raw["view_address"] === address && authorityExportInView(raw,category,shape,stateRequirement,diffCount === 3)); }

`

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
		if value.MaxLength != nil {
			parts = append(parts, fmt.Sprintf("Array.from(%s).length <= %d", expression, *value.MaxLength))
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
		if value.MaxItems != nil {
			parts = append(parts, fmt.Sprintf("%s.length <= %d", expression, *value.MaxItems))
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
		if value.StateReadOrder {
			parts = append(parts, "hasStateReadOrder("+expression+")")
		}
		if value.CanonicalCollection != "" {
			parts = append(parts, fmt.Sprintf("hasCanonicalCollectionOrder(%s, %q)", expression, value.CanonicalCollection))
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
				for _, property := range variant.ErrorDiagnostic {
					conditions = append(conditions, fmt.Sprintf("isJSONArray(%s[%q]) && %s[%q].some((item) => isObject(item) && item[\"severity\"] === \"error\")", expression, property, expression, property))
				}
				if len(variant.AnyNonEmpty) != 0 {
					alternatives := make([]string, 0, len(variant.AnyNonEmpty))
					for _, property := range variant.AnyNonEmpty {
						alternatives = append(alternatives, fmt.Sprintf("isJSONArray(%s[%q]) && %s[%q].length > 0", expression, property, expression, property))
					}
					conditions = append(conditions, "("+strings.Join(alternatives, " || ")+")")
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
			parts = append(parts, "hasValidOutcomeEnvelope("+expression+")")
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
		if len(value.StableAddressRoles) != 0 {
			rules, err := json.Marshal(value.StableAddressRoles)
			if err != nil {
				return "", err
			}
			parts = append(parts, "hasStableAddressRoles("+expression+", "+string(rules)+")")
		}
		if value.RecipePredicate != "" {
			parts = append(parts, fmt.Sprintf("hasValidRecipePredicate(%s, %q)", expression, value.RecipePredicate))
		}
		if value.ViewProjection != "" {
			parts = append(parts, fmt.Sprintf("hasValidViewProjection(%s, %q)", expression, value.ViewProjection))
		}
		if value.ExportRecipe {
			parts = append(parts, "hasValidExportRecipe("+expression+")")
		}
		if value.QueryParameter {
			parts = append(parts, "hasValidQueryParameter("+expression+")")
		}
		if value.QueryRecipe {
			parts = append(parts, "hasValidQueryRecipe("+expression+")")
		}
		if value.RecipeScalar {
			parts = append(parts, "hasValidRecipeScalar("+expression+")")
		}
		if value.StateRead {
			parts = append(parts, "hasValidStateRead("+expression+")")
		}
		if value.ViewRecipe {
			parts = append(parts, "hasValidViewRecipe("+expression+")")
		}
		if value.ChildSet {
			parts = append(parts, "hasValidChildSet("+expression+")")
		}
		if value.ProtocolInvariant != "" {
			parts = append(parts, fmt.Sprintf("hasProtocolInvariant(%s, %q)", expression, value.ProtocolInvariant))
		}
		return strings.Join(parts, " && "), nil
	default:
		return "", fmt.Errorf("unsupported schema type %q", typeValue)
	}
}

func writeTSDefinition(body *strings.Builder, set schemaSet, document *schemaDocument, name string, definition *schemaType) error {
	if name == "RelationCardinalityMaximum" && isRelationCardinalityMaximum(definition) {
		body.WriteString("export type RelationCardinalityMaximum = 1 | \"many\";\n")
		return nil
	}
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
