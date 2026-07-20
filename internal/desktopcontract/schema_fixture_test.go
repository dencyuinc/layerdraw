// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopcontract

import (
	"encoding/json"
	"fmt"
	"regexp"
	"regexp/syntax"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

type schemaAuthority struct {
	documents map[string]map[string]any
}

func loadSchemaAuthority(t *testing.T) *schemaAuthority {
	t.Helper()
	authority := &schemaAuthority{documents: map[string]map[string]any{}}
	for _, path := range []string{"schemas/protocol-common/v1.schema.json", "schemas/semantic/v1.schema.json", "schemas/access-protocol/v1.schema.json", "schemas/engine-protocol/v1.schema.json", "schemas/runtime-protocol/v1.schema.json"} {
		var document map[string]any
		if err := json.Unmarshal(fixture(t, path), &document); err != nil {
			t.Fatal(err)
		}
		id, _ := document["$id"].(string)
		if id == "" {
			t.Fatalf("schema %s has no ID", path)
		}
		authority.documents[id] = document
	}
	return authority
}

func (a *schemaAuthority) operationDefinitions(t *testing.T, documentID string) map[string][2]string {
	t.Helper()
	document := a.documents[documentID]
	definitions, _ := document["$defs"].(map[string]any)
	result := map[string][2]string{}
	for name, raw := range definitions {
		if !strings.HasSuffix(name, "RequestEnvelope") {
			continue
		}
		definition, _ := raw.(map[string]any)
		properties, _ := definition["properties"].(map[string]any)
		operationSchema, _ := properties["operation"].(map[string]any)
		operation, _ := operationSchema["const"].(string)
		responseName := strings.TrimSuffix(name, "RequestEnvelope") + "ResponseEnvelope"
		if operation == "" || definitions[responseName] == nil {
			t.Fatalf("schema operation definition is incomplete: %s", name)
		}
		result[operation] = [2]string{name, responseName}
	}
	return result
}

func (a *schemaAuthority) fixture(t *testing.T, documentID, definitionName, requestID string) []byte {
	t.Helper()
	document := a.documents[documentID]
	definitions := document["$defs"].(map[string]any)
	node := definitions[definitionName].(map[string]any)
	value, err := a.generate(documentID, node, definitionName, 0)
	if err != nil {
		t.Fatalf("generate %s: %v", definitionName, err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s did not generate an object", definitionName)
	}
	object["request_id"] = requestID
	fixGeneratedFixtureInvariants(object)
	if strings.HasSuffix(definitionName, "ResponseEnvelope") {
		fixLogicalResponseByteCounts(object["payload"])
	}
	wire, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func fixGeneratedFixtureInvariants(value any) {
	walkJSON(value, func(object map[string]any) {
		base, hasBase := object["base_generation"].(map[string]any)
		proposed, hasProposed := object["proposed_generation"].(map[string]any)
		if hasBase && hasProposed {
			base["value"] = "0"
			proposed["value"] = "1"
		}
		if format, ok := object["format"].(string); ok {
			if options, hasOptions := object["options"].(map[string]any); hasOptions {
				if profile, hasProfile := object["exporter_profile"].(map[string]any); hasProfile {
					options["kind"] = format
					profile["format"] = format
					if format == "bpmn" {
						object["extension"] = ".bpmn"
						object["filename"] = "fixture.bpmn"
						object["native_maximum_fidelity"] = "lossy"
						object["effective_maximum_fidelity"] = "lossy"
						object["fidelity"] = "lossy"
						object["fidelity_basis"] = "native"
						object["source_refs"] = false
						object["requires_source_manifest"] = false
					}
				}
			}
		}
	})
}

func fixLogicalResponseByteCounts(payload any) {
	if payload == nil {
		return
	}
	walkJSON(payload, func(object map[string]any) {
		if _, ok := object["returned_bytes"]; ok {
			object["returned_bytes"] = "0"
		}
	})
	wire, err := json.Marshal(payload)
	if err != nil {
		return
	}
	count := fmt.Sprintf("%d", len(wire))
	walkJSON(payload, func(object map[string]any) {
		if _, ok := object["returned_bytes"]; ok {
			object["returned_bytes"] = count
		}
	})
}

func walkJSON(value any, visit func(map[string]any)) {
	switch typed := value.(type) {
	case map[string]any:
		visit(typed)
		for _, child := range typed {
			walkJSON(child, visit)
		}
	case []any:
		for _, child := range typed {
			walkJSON(child, visit)
		}
	}
}

func (a *schemaAuthority) generate(documentID string, node map[string]any, path string, depth int) (any, error) {
	if depth > 96 {
		return nil, fmt.Errorf("schema recursion at %s", path)
	}
	if reference, _ := node["$ref"].(string); reference != "" {
		resolvedID, pointer := documentID, reference
		if before, after, found := strings.Cut(reference, "#"); found {
			if before != "" {
				resolvedID = before
			}
			pointer = after
		}
		resolved, err := a.resolve(resolvedID, pointer)
		if err != nil {
			return nil, err
		}
		return a.generate(resolvedID, resolved, path, depth+1)
	}
	if constant, exists := node["const"]; exists {
		return constant, nil
	}
	if choices, ok := node["enum"].([]any); ok && len(choices) > 0 {
		if strings.HasSuffix(path, ".outcome") {
			for _, choice := range choices {
				if choice == "success" {
					return choice, nil
				}
			}
		}
		return choices[0], nil
	}
	for _, keyword := range []string{"oneOf", "anyOf"} {
		if choices, ok := node[keyword].([]any); ok && len(choices) > 0 {
			for _, choice := range choices {
				candidate := choice.(map[string]any)
				if candidate["type"] == "null" && len(choices) > 1 {
					continue
				}
				return a.generate(documentID, candidate, path, depth+1)
			}
		}
	}
	typeName, _ := node["type"].(string)
	switch typeName {
	case "object":
		return a.generateObject(documentID, node, path, depth)
	case "array":
		count := intNumber(node["minItems"])
		items, _ := node["items"].(map[string]any)
		result := make([]any, 0, count)
		for index := 0; index < count; index++ {
			item, err := a.generate(documentID, items, fmt.Sprintf("%s[%d]", path, index), depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, item)
		}
		return result, nil
	case "string":
		if format, _ := node["format"].(string); format == "date-time" {
			return "2026-07-20T00:00:00Z", nil
		} else if format == "canonical-source-path" {
			return "document.ldl", nil
		}
		if pattern, _ := node["pattern"].(string); pattern != "" {
			example, err := regexExample(pattern)
			if err != nil {
				return nil, err
			}
			minimum := intNumber(node["minLength"])
			compiled, compileErr := regexp.Compile(pattern)
			for len(example) < minimum && compileErr == nil {
				candidate := example + "0"
				if compiled.MatchString(candidate) {
					example = candidate
					continue
				}
				candidate = example + "a"
				if compiled.MatchString(candidate) {
					example = candidate
					continue
				}
				break
			}
			return example, nil
		}
		minimum := intNumber(node["minLength"])
		if minimum < 1 {
			minimum = 1
		}
		return strings.Repeat("x", minimum), nil
	case "integer", "number":
		minimum := intNumber(node["minimum"])
		return minimum, nil
	case "boolean":
		return false, nil
	case "null":
		return nil, nil
	case "":
		if _, ok := node["properties"]; ok {
			return a.generateObject(documentID, node, path, depth)
		}
	}
	return nil, fmt.Errorf("unsupported schema at %s", path)
}

func (a *schemaAuthority) generateObject(documentID string, node map[string]any, path string, depth int) (map[string]any, error) {
	result := map[string]any{}
	properties, _ := node["properties"].(map[string]any)
	required := stringSet(node["required"])
	if node["x-layerdraw-outcome-envelope"] == true {
		required["payload"] = true
	}
	keys := make([]string, 0, len(required))
	for key := range required {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		property, ok := properties[key].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("missing required property %s.%s", path, key)
		}
		value, err := a.generate(documentID, property, path+"."+key, depth+1)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	if tagged, ok := node["x-layerdraw-tagged-union"].(map[string]any); ok {
		discriminator, _ := tagged["property"].(string)
		variant, _ := result[discriminator].(string)
		variants, _ := tagged["variants"].(map[string]any)
		contract, _ := variants[variant].(map[string]any)
		if len(stringSet(contract["any_non_empty"])) > 0 {
			variantNames := make([]string, 0, len(variants))
			for name := range variants {
				variantNames = append(variantNames, name)
			}
			sort.Strings(variantNames)
			for _, name := range variantNames {
				candidate, _ := variants[name].(map[string]any)
				if len(stringSet(candidate["any_non_empty"])) == 0 {
					variant, contract = name, candidate
					result[discriminator] = name
					break
				}
			}
		}
		for key := range stringSet(contract["required"]) {
			if _, exists := result[key]; !exists {
				value, err := a.generate(documentID, properties[key].(map[string]any), path+"."+key, depth+1)
				if err != nil {
					return nil, err
				}
				result[key] = value
			}
		}
		for key := range stringSet(contract["forbidden"]) {
			delete(result, key)
		}
		for key := range stringSet(contract["empty"]) {
			result[key] = []any{}
		}
		for key := range stringSet(contract["non_empty"]) {
			property := properties[key].(map[string]any)
			items := property["items"].(map[string]any)
			item, err := a.generate(documentID, items, path+"."+key+"[0]", depth+1)
			if err != nil {
				return nil, err
			}
			result[key] = []any{item}
		}
	}
	return result, nil
}

func (a *schemaAuthority) resolve(documentID, pointer string) (map[string]any, error) {
	document, ok := a.documents[documentID]
	if !ok {
		return nil, fmt.Errorf("unknown schema %s", documentID)
	}
	var value any = document
	for _, token := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		if token == "" {
			continue
		}
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid pointer %s#%s", documentID, pointer)
		}
		value = object[strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")]
	}
	resolved, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unresolved pointer %s#%s", documentID, pointer)
	}
	return resolved, nil
}

func stringSet(raw any) map[string]bool {
	result := map[string]bool{}
	items, _ := raw.([]any)
	for _, item := range items {
		if value, ok := item.(string); ok {
			result[value] = true
		}
	}
	return result
}

func intNumber(raw any) int {
	value, _ := raw.(float64)
	return int(value)
}

func regexExample(pattern string) (string, error) {
	expression, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return "", err
	}
	return regexNodeExample(expression), nil
}

func regexNodeExample(expression *syntax.Regexp) string {
	switch expression.Op {
	case syntax.OpNoMatch, syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText, syntax.OpEndText, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return ""
	case syntax.OpLiteral:
		return string(expression.Rune)
	case syntax.OpCharClass:
		for index := 0; index+1 < len(expression.Rune); index += 2 {
			start, end := expression.Rune[index], expression.Rune[index+1]
			if end >= '0' && start <= utf8.MaxRune {
				candidate := start
				if candidate < '0' {
					candidate = '0'
				}
				if candidate <= end && candidate >= ' ' && candidate != '\\' {
					return string(candidate)
				}
			}
		}
		return "a"
	case syntax.OpAnyCharNotNL, syntax.OpAnyChar:
		return "a"
	case syntax.OpCapture:
		return regexNodeExample(expression.Sub[0])
	case syntax.OpConcat:
		var builder strings.Builder
		for _, sub := range expression.Sub {
			builder.WriteString(regexNodeExample(sub))
		}
		return builder.String()
	case syntax.OpAlternate:
		return regexNodeExample(expression.Sub[0])
	case syntax.OpQuest, syntax.OpStar:
		return ""
	case syntax.OpPlus:
		return regexNodeExample(expression.Sub[0])
	case syntax.OpRepeat:
		return strings.Repeat(regexNodeExample(expression.Sub[0]), expression.Min)
	}
	return ""
}

func TestSchemaAuthorityBuildsOperationSpecificGeneratedSuccessFixtures(t *testing.T) {
	authority := loadSchemaAuthority(t)
	documents := map[BindingTarget]string{
		TargetEngine:  "https://schemas.layerdraw.dev/engine-protocol/v1",
		TargetRuntime: "https://schemas.layerdraw.dev/runtime-protocol/v1",
	}
	definitions := map[BindingTarget]map[string][2]string{}
	expectedCount := 0
	for target, documentID := range documents {
		definitions[target] = authority.operationDefinitions(t, documentID)
		expectedCount += len(definitions[target])
	}
	count := 0
	for _, binding := range GeneratedBindingTable() {
		documentID, generated := documents[binding.Target]
		if !generated {
			continue
		}
		names, ok := definitions[binding.Target][binding.Operation]
		if !ok {
			t.Fatalf("missing operation-specific schema fixture %s", binding.Operation)
		}
		request := authority.fixture(t, documentID, names[0], "fixture-"+binding.GeneratedMethod)
		if err := decodeExact(binding, request); err != nil {
			t.Fatalf("%s request fixture: %v\n%s", binding.GeneratedMethod, err, request)
		}
		response := authority.fixture(t, documentID, names[1], "fixture-"+binding.GeneratedMethod)
		if err := decodeExactResponse(binding, response); err != nil {
			t.Fatalf("%s response fixture: %v\n%s", binding.GeneratedMethod, err, response)
		}
		count++
	}
	if count != expectedCount {
		t.Fatalf("generated schema fixture closure = %d, want %d", count, expectedCount)
	}
}
