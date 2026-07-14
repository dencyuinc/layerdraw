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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const generatorVersion = "layerdraw-protocolgen/1"

var (
	snakeCase = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
	typeName  = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
)

type schemaDocument struct {
	Schema        string                 `json:"$schema"`
	ID            string                 `json:"$id"`
	Comment       string                 `json:"$comment,omitempty"`
	Title         string                 `json:"title"`
	Package       string                 `json:"x-layerdraw-go-package"`
	Module        string                 `json:"x-layerdraw-ts-module"`
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
	Required  []string `json:"required"`
	Forbidden []string `json:"forbidden"`
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
	AdditionalProperties any                    `json:"additionalProperties,omitempty"`
	Pattern              string                 `json:"pattern,omitempty"`
	Format               string                 `json:"format,omitempty"`
	Minimum              *float64               `json:"minimum,omitempty"`
	Maximum              *float64               `json:"maximum,omitempty"`
	MinLength            *int                   `json:"minLength,omitempty"`
	MinItems             *int                   `json:"minItems,omitempty"`
	OneOf                []*schemaType          `json:"oneOf,omitempty"`
	TaggedUnion          *taggedUnion           `json:"x-layerdraw-tagged-union,omitempty"`
	OutcomeEnvelope      bool                   `json:"x-layerdraw-outcome-envelope,omitempty"`
}

type schemaSet struct {
	documents []*schemaDocument
	byID      map[string]*schemaDocument
	digest    string
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
	patterns := []string{
		"schemas/protocol-common/*.schema.json",
		"schemas/semantic/*.schema.json",
		"schemas/engine-protocol/*.schema.json",
	}
	var paths []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			return schemaSet{}, err
		}
		paths = append(paths, matches...)
	}
	sort.Strings(paths)
	if len(paths) != len(patterns) {
		return schemaSet{}, fmt.Errorf("expected exactly one schema in each initial schema group, found %d", len(paths))
	}
	set := schemaSet{byID: map[string]*schemaDocument{}}
	aggregate := sha256.New()
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return schemaSet{}, err
		}
		if bytes.Contains(data, []byte{'\r'}) {
			return schemaSet{}, fmt.Errorf("%s must use LF line endings", path)
		}
		var document schemaDocument
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&document); err != nil {
			return schemaSet{}, fmt.Errorf("decode %s: %w", path, err)
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
		if set.byID[document.ID] != nil {
			return schemaSet{}, fmt.Errorf("duplicate schema $id %q", document.ID)
		}
		set.byID[document.ID] = &document
		set.documents = append(set.documents, &document)
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
		document.digest = digestDocuments(closure)
	}
	for _, document := range set.documents {
		aggregate.Write([]byte(document.path))
		aggregate.Write([]byte{0})
		aggregate.Write(document.raw)
		aggregate.Write([]byte{0})
	}
	set.digest = "sha256:" + hex.EncodeToString(aggregate.Sum(nil))
	return set, nil
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

func digestDocuments(documents []*schemaDocument) string {
	hash := sha256.New()
	for _, document := range documents {
		hash.Write([]byte(document.path))
		hash.Write([]byte{0})
		hash.Write(document.raw)
		hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func validateDocument(document *schemaDocument) error {
	if document.Schema != "https://json-schema.org/draft/2020-12/schema" {
		return fmt.Errorf("$schema must pin JSON Schema draft 2020-12")
	}
	if document.ID == "" || document.Title == "" || document.Package == "" || document.Module == "" {
		return errors.New("$id, title, x-layerdraw-go-package, and x-layerdraw-ts-module are required")
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
	if typeValue == "object" {
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
			if value.Properties[value.TaggedUnion.Property] == nil || len(value.TaggedUnion.Variants) < 2 {
				return fmt.Errorf("%s has invalid tagged union", context)
			}
			for tag, variant := range value.TaggedUnion.Variants {
				if tag == "" {
					return fmt.Errorf("%s has empty tagged union value", context)
				}
				for _, property := range append(append([]string{}, variant.Required...), variant.Forbidden...) {
					if value.Properties[property] == nil {
						return fmt.Errorf("%s tagged union refers to unknown property %q", context, property)
					}
				}
			}
		}
		if value.OutcomeEnvelope && value.Properties["outcome"] == nil {
			return fmt.Errorf("%s outcome envelope requires outcome", context)
		}
	}
	if typeValue == "array" {
		if value.Items == nil {
			return fmt.Errorf("%s array requires items", context)
		}
		return validateType(set, document, context+"[]", value.Items, seen)
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
	fmt.Fprintf(&body, "const SchemaDigest = %q\n\n", document.digest)
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
	formatted, err := format.Source([]byte(body.String()))
	if err != nil {
		return nil, fmt.Errorf("format generated Go for %s: %w\n%s", document.Package, err, body.String())
	}
	return formatted, nil
}

func writeGoDefinition(body *strings.Builder, set schemaSet, document *schemaDocument, aliases map[string]string, name string, definition *schemaType) error {
	typeValue, err := scalarType(definition.Type)
	if err != nil {
		return err
	}
	if typeValue == "object" && len(definition.Properties) != 0 {
		fmt.Fprintf(body, "type %s struct {\n", name)
		required := stringSet(definition.Required)
		for _, propertyName := range sortedKeys(definition.Properties) {
			property := definition.Properties[propertyName]
			expression, err := goType(set, document, aliases, property)
			if err != nil {
				return err
			}
			if !required[propertyName] && !strings.HasPrefix(expression, "[]") && !strings.HasPrefix(expression, "map[") && expression != "any" {
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
		return "int", nil
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
	for _, name := range sortedKeys(document.Definitions) {
		fmt.Fprintf(&body, "// Decode%s decodes and validates one %s JSON value.\n", name, name)
		fmt.Fprintf(&body, "func Decode%s(data []byte) (%s, error) {\n\tvar result %s\n\traw, err := decodeWireJSON(data)\n\tif err != nil { return result, err }\n\tif err := validateNamed(schemaDocumentID, %q, raw); err != nil { return result, err }\n\tdecoder := json.NewDecoder(bytes.NewReader(data))\n\tdecoder.DisallowUnknownFields()\n\tif err := decoder.Decode(&result); err != nil { return result, err }\n\treturn result, nil\n}\n\n", name, name, name, name)
		fmt.Fprintf(&body, "// Encode%s validates and emits canonical UTF-8 JSON.\n", name)
		fmt.Fprintf(&body, "func Encode%s(value %s) ([]byte, error) {\n\tencoded, err := json.Marshal(value)\n\tif err != nil { return nil, err }\n\traw, err := decodeWireJSON(encoded)\n\tif err != nil { return nil, err }\n\tif err := validateNamed(schemaDocumentID, %q, raw); err != nil { return nil, err }\n\treturn appendCanonicalJSON(nil, raw)\n}\n\n", name, name, name)
	}
	body.WriteString(goCodecRuntime)
	formatted, err := format.Source([]byte(body.String()))
	if err != nil {
		return nil, fmt.Errorf("format generated Go codec for %s: %w\n%s", document.Package, err, body.String())
	}
	return formatted, nil
}

const goCodecRuntime = `func decodeWireJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil { return nil, err }
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) { return nil, errors.New("protocol JSON must contain exactly one value") }
	return value, nil
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
	if depth > 256 { return fmt.Errorf("%s exceeds maximum protocol value depth", path) }
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
		if pattern, ok := schema["pattern"].(string); ok && !regexp.MustCompile(pattern).MatchString(text) { return fmt.Errorf("%s has invalid string form", path) }
		if minimum, ok := schema["minLength"].(float64); ok && utf8.RuneCountInString(text) < int(minimum) { return fmt.Errorf("%s is too short", path) }
		if format, _ := schema["format"].(string); format != "" {
			switch format {
			case "int64-decimal":
				if !regexp.MustCompile(` + "`" + `^-?(0|[1-9][0-9]*)$` + "`" + `).MatchString(text) { return fmt.Errorf("%s is not a canonical int64", path) }
				if _, err := strconv.ParseInt(text, 10, 64); err != nil { return fmt.Errorf("%s is outside int64", path) }
			case "uint64-decimal":
				if !regexp.MustCompile(` + "`" + `^(0|[1-9][0-9]*)$` + "`" + `).MatchString(text) { return fmt.Errorf("%s is not a canonical uint64", path) }
				if _, err := strconv.ParseUint(text, 10, 64); err != nil { return fmt.Errorf("%s is outside uint64", path) }
			case "date-time":
				if _, err := time.Parse(time.RFC3339Nano, text); err != nil { return fmt.Errorf("%s is not RFC 3339", path) }
			}
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok || !regexp.MustCompile(` + "`" + `^-?(0|[1-9][0-9]*)$` + "`" + `).MatchString(number.String()) { return fmt.Errorf("%s must be a canonical integer", path) }
		integer, err := strconv.ParseInt(number.String(), 10, 64)
		if err != nil || integer < -9007199254740991 || integer > 9007199254740991 { return fmt.Errorf("%s integer is outside the portable safe range", path) }
		if minimum, ok := schema["minimum"].(float64); ok && integer < int64(minimum) { return fmt.Errorf("%s is below its minimum", path) }
		if maximum, ok := schema["maximum"].(float64); ok && integer > int64(maximum) { return fmt.Errorf("%s is above its maximum", path) }
	case "array":
		items, ok := value.([]any)
		if !ok { return fmt.Errorf("%s must be an array", path) }
		if minimum, ok := schema["minItems"].(float64); ok && len(items) < int(minimum) { return fmt.Errorf("%s has too few items", path) }
		itemSchema, _ := schema["items"].(map[string]any)
		for index, item := range items {
			if err := validateSchema(documentID, itemSchema, item, fmt.Sprintf("%s[%d]", path, index), depth+1); err != nil { return err }
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
			tag, _ := object[property].(string)
			variants, _ := rawUnion["variants"].(map[string]any)
			rawVariant, exists := variants[tag]
			if !exists { return fmt.Errorf("%s has unknown tagged union value %q", path, tag) }
			variant, _ := rawVariant.(map[string]any)
			if err := validatePresenceRule(path, object, variant); err != nil { return err }
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
	default:
		return fmt.Errorf("%s uses unsupported generated schema type %q", path, typeName)
	}
	return nil
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
		if !regexp.MustCompile(` + "`" + `^-?(0|[1-9][0-9]*)$` + "`" + `).MatchString(text) { return nil, errors.New("protocol canonical JSON permits only integer numbers") }
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
	body.WriteString("function isObject(value: unknown): value is Record<string, unknown> {\n")
	body.WriteString("  return typeof value === \"object\" && value !== null && !Array.isArray(value);\n")
	body.WriteString("}\n\n")
	body.WriteString("function hasOnlyKeys(value: Record<string, unknown>, allowed: ReadonlySet<string>): boolean {\n")
	body.WriteString("  return Object.keys(value).every((key) => allowed.has(key));\n")
	body.WriteString("}\n\n")
	body.WriteString("function isJSONCompatible(value: unknown): boolean {\n")
	body.WriteString("  if (value === null || typeof value === \"string\" || typeof value === \"boolean\") return true;\n")
	body.WriteString("  if (Array.isArray(value)) return value.every(isJSONCompatible);\n")
	body.WriteString("  return isObject(value) && Object.values(value).every(isJSONCompatible);\n")
	body.WriteString("}\n\n")
	body.WriteString("function matchesCanonicalInt64(value: string): boolean {\n")
	body.WriteString("  if (!/^-?(0|[1-9][0-9]*)$/.test(value)) return false;\n")
	body.WriteString("  try { const parsed = BigInt(value); return parsed >= -(2n ** 63n) && parsed <= (2n ** 63n) - 1n; } catch { return false; }\n")
	body.WriteString("}\n\n")
	body.WriteString("function matchesCanonicalUint64(value: string): boolean {\n")
	body.WriteString("  if (!/^(0|[1-9][0-9]*)$/.test(value)) return false;\n")
	body.WriteString("  try { const parsed = BigInt(value); return parsed <= (2n ** 64n) - 1n; } catch { return false; }\n")
	body.WriteString("}\n\n")
	body.WriteString("function isRFC3339(value: string): boolean {\n")
	body.WriteString("  return /^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}(?:\\.\\d+)?(?:Z|[+-]\\d{2}:\\d{2})$/.test(value) && !Number.isNaN(Date.parse(value));\n")
	body.WriteString("}\n\n")
	body.WriteString("function canonicalJSONStringify(value: unknown): string {\n")
	body.WriteString("  if (value === null || typeof value === \"boolean\" || typeof value === \"string\") return JSON.stringify(value).replace(/[\\u2028\\u2029]/g, (character) => character === \"\\u2028\" ? \"\\\\u2028\" : \"\\\\u2029\");\n")
	body.WriteString("  if (typeof value === \"number\") { if (!Number.isSafeInteger(value)) throw new TypeError(\"protocol numbers must be safe integers\"); return String(value); }\n")
	body.WriteString("  if (Array.isArray(value)) return `[${value.map(canonicalJSONStringify).join(\",\")}]`;\n")
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
		fmt.Fprintf(&body, "\nexport function is%s(value: unknown): value is %s {\n  return %s;\n}\n", name, name, predicate)
		fmt.Fprintf(&body, "\nexport function decode%s(input: string): %s {\n  const value: unknown = JSON.parse(input);\n  if (!is%s(value)) throw new TypeError(%q);\n  return value;\n}\n", name, name, name, "invalid "+name)
		fmt.Fprintf(&body, "\nexport function encode%s(value: %s): string {\n  if (!is%s(value)) throw new TypeError(%q);\n  return canonicalJSONStringify(value);\n}\n", name, name, name, "invalid "+name)
		body.WriteString("\n")
	}
	return append(bytes.TrimRight([]byte(body.String()), "\n"), '\n'), nil
}

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
		parts := []string{"typeof " + expression + " === \"string\""}
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
		if value.Format == "uint64-decimal" {
			parts = append(parts, "matchesCanonicalUint64("+expression+")")
		}
		if value.Format == "date-time" {
			parts = append(parts, "isRFC3339("+expression+")")
		}
		return strings.Join(parts, " && "), nil
	case "integer":
		parts := []string{"typeof " + expression + " === \"number\"", "Number.isSafeInteger(" + expression + ")"}
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
		parts := []string{"Array.isArray(" + expression + ")", expression + ".every((item) => " + item + ")"}
		if value.MinItems != nil {
			parts = append(parts, fmt.Sprintf("%s.length >= %d", expression, *value.MinItems))
		}
		return strings.Join(parts, " && "), nil
	case "object":
		if len(value.Properties) == 0 {
			if additional, ok := value.AdditionalProperties.(*schemaType); ok {
				item, err := tsPredicate(set, document, additional, "item")
				if err != nil {
					return "", err
				}
				return "isObject(" + expression + ") && Object.values(" + expression + ").every((item) => " + item + ")", nil
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
				parts = append(parts, fmt.Sprintf("%q in %s", key, expression), "("+predicate+")")
			} else {
				parts = append(parts, "(!("+fmt.Sprintf("%q", key)+" in "+expression+") || ("+predicate+"))")
			}
		}
		if value.TaggedUnion != nil {
			var variants []string
			for _, tag := range sortedKeys(value.TaggedUnion.Variants) {
				variant := value.TaggedUnion.Variants[tag]
				conditions := []string{expression + "[" + fmt.Sprintf("%q", value.TaggedUnion.Property) + "] === " + fmt.Sprintf("%q", tag)}
				for _, property := range variant.Required {
					conditions = append(conditions, fmt.Sprintf("%q in %s", property, expression))
				}
				for _, property := range variant.Forbidden {
					conditions = append(conditions, fmt.Sprintf("!(%q in %s)", property, expression))
				}
				variants = append(variants, "("+strings.Join(conditions, " && ")+")")
			}
			parts = append(parts, "("+strings.Join(variants, " || ")+")")
		}
		if value.OutcomeEnvelope {
			outcome := expression + "[\"outcome\"]"
			diagnostics := expression + "[\"diagnostics\"]"
			parts = append(parts, "(("+outcome+" === \"success\" && \"payload\" in "+expression+" && !(\"failure\" in "+expression+")) || ("+outcome+" === \"rejected\" && !(\"payload\" in "+expression+") && !(\"failure\" in "+expression+") && Array.isArray("+diagnostics+") && "+diagnostics+".length > 0) || (("+outcome+" === \"failed\" || "+outcome+" === \"cancelled\") && !(\"payload\" in "+expression+") && \"failure\" in "+expression+"))")
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
