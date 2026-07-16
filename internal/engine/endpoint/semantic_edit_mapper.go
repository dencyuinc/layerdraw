// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

// MapSemanticEditPlanInput is the single handwritten boundary between the
// complete generated Workbench operation contract and the pure Engine
// planner. Generated validation runs before any planner-domain value exists.
func MapSemanticEditPlanInput(baseInput engine.CompileInput, base engine.Snapshot, preconditions engineprotocol.EngineEditPreconditions, batch engineprotocol.SemanticOperationBatch) (engine.SemanticEditPlanInput, error) {
	encoded, err := engineprotocol.EncodeSemanticOperationBatch(batch)
	if err != nil {
		return engine.SemanticEditPlanInput{}, fmt.Errorf("map semantic operation batch: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var wire struct {
		Operations []map[string]any `json:"operations"`
	}
	if err := decoder.Decode(&wire); err != nil {
		return engine.SemanticEditPlanInput{}, fmt.Errorf("decode validated semantic operation batch: %w", err)
	}
	operations := make([]engine.SemanticOperation, 0, len(wire.Operations))
	for _, raw := range wire.Operations {
		operation, mapErr := mapSemanticOperation(raw)
		if mapErr != nil {
			return engine.SemanticEditPlanInput{}, mapErr
		}
		operations = append(operations, operation)
	}
	pre := engine.SemanticEditPreconditions{
		ExpectedSubjectHashes: make([]engine.ExpectedSemanticHash, 0, len(preconditions.ExpectedSubjectHashes)),
		ExpectedSubtreeHashes: make([]engine.ExpectedSemanticHash, 0, len(preconditions.ExpectedSubtreeHashes)),
		ExpectedChildSets:     make([]engine.ExpectedSemanticChildSet, 0, len(preconditions.ExpectedChildSets)),
	}
	for _, expected := range preconditions.ExpectedSubjectHashes {
		pre.ExpectedSubjectHashes = append(pre.ExpectedSubjectHashes, engine.ExpectedSemanticHash{Address: string(expected.Address), Hash: string(expected.Hash)})
	}
	for _, expected := range preconditions.ExpectedSubtreeHashes {
		pre.ExpectedSubtreeHashes = append(pre.ExpectedSubtreeHashes, engine.ExpectedSemanticHash{Address: string(expected.Address), Hash: string(expected.Hash)})
	}
	for _, expected := range preconditions.ExpectedChildSets {
		pre.ExpectedChildSets = append(pre.ExpectedChildSets, engine.ExpectedSemanticChildSet{OwnerAddress: string(expected.OwnerAddress), ChildKind: engine.SemanticSubjectKind(expected.ChildKind), Hash: string(expected.Hash)})
	}
	if preconditions.ExpectedSourceDigests != nil {
		for _, expected := range *preconditions.ExpectedSourceDigests {
			pre.ExpectedSourceDigests = append(pre.ExpectedSourceDigests, engine.ExpectedSemanticSourceDigest{ModulePath: expected.Module.ModulePath, Digest: string(expected.Digest)})
		}
	}
	return engine.SemanticEditPlanInput{BaseInput: baseInput, BaseSnapshot: base, Batch: engine.SemanticOperationBatch{Operations: operations}, Preconditions: pre}, nil
}

func mapSemanticOperation(raw map[string]any) (engine.SemanticOperation, error) {
	op := engine.SemanticOperation{Kind: engine.SemanticOperationKind(stringValue(raw, "operation")), SubjectKind: engine.SemanticSubjectKind(stringValue(raw, "subject_kind")), ParentAddress: stringValue(raw, "parent_address"), TargetAddress: stringValue(raw, "target_address"), OwnerAddress: stringValue(raw, "owner_address"), ProjectAddress: stringValue(raw, "project_address"), RelationAddress: stringValue(raw, "relation_address"), RowAddress: stringValue(raw, "row_address"), EntityAddress: stringValue(raw, "entity_address"), LayerAddress: stringValue(raw, "layer_address"), TypeAddress: stringValue(raw, "type_address"), FromAddress: stringValue(raw, "from_address"), ToAddress: stringValue(raw, "to_address"), ID: stringValue(raw, "id"), NewID: stringValue(raw, "new_id"), NewProjectID: stringValue(raw, "new_project_id"), Endpoint: stringValue(raw, "endpoint"), Action: stringValue(raw, "action")}
	if path, ok := raw["path"].([]any); ok {
		for _, item := range path {
			op.Path = append(op.Path, fmt.Sprint(item))
		}
	}
	if value, ok := raw["value"]; ok {
		mapped, err := mapTaggedSemanticValue(value)
		if err != nil {
			return op, err
		}
		op.Value = &mapped
	}
	if fields, ok := raw["fields"].(map[string]any); ok {
		op.Fields = mapPlainObject(fields)
	}
	if values, ok := raw["values"].([]any); ok {
		for _, item := range values {
			cell, ok := item.(map[string]any)
			if !ok {
				return op, fmt.Errorf("map semantic row cell")
			}
			value, err := mapTaggedSemanticValue(cell["value"])
			if err != nil {
				return op, err
			}
			op.Values = append(op.Values, engine.SemanticRowCell{ColumnAddress: stringValue(cell, "column_address"), Value: value})
		}
	}
	if absent, ok := raw["explicit_absent_column_addresses"].([]any); ok {
		for _, item := range absent {
			op.ExplicitAbsentColumnAddresses = append(op.ExplicitAbsentColumnAddresses, fmt.Sprint(item))
		}
	}
	if placement, ok := raw["placement"].(map[string]any); ok {
		op.Placement = &engine.SemanticPlacementHint{ModulePath: stringValue(placement, "module_path"), GroupAnchorAddress: stringValue(placement, "group_anchor_address"), Position: stringValue(placement, "position")}
	}
	return op, nil
}

func mapTaggedSemanticValue(input any) (engine.SemanticValue, error) {
	raw, ok := input.(map[string]any)
	if !ok {
		return engine.SemanticValue{}, fmt.Errorf("semantic operation value is not an object")
	}
	kind := engine.SemanticValueKind(stringValue(raw, "kind"))
	out := engine.SemanticValue{Kind: kind}
	switch kind {
	case engine.SemanticValueAbsent:
	case engine.SemanticValueAddress:
		out.Address = stringValue(raw, "address")
	case engine.SemanticValueBoolean:
		out.Boolean, _ = raw["boolean"].(bool)
	case engine.SemanticValueDecimal:
		out.Decimal = stringValue(raw, "decimal")
	case engine.SemanticValueInteger:
		value, err := numberInt64(raw["integer"])
		if err != nil {
			return out, err
		}
		out.Integer = value
	case engine.SemanticValueString:
		out.String = stringValue(raw, "string")
	case engine.SemanticValueBlob:
		if blob, ok := raw["blob"].(map[string]any); ok {
			out.Blob = stringValue(blob, "digest")
		}
	case engine.SemanticValueArray:
		if values, ok := raw["array"].([]any); ok {
			for _, value := range values {
				mapped, err := mapTaggedSemanticValue(value)
				if err != nil {
					return out, err
				}
				out.Array = append(out.Array, mapped)
			}
		}
	case engine.SemanticValueMap:
		if entries, ok := raw["map"].([]any); ok {
			for _, value := range entries {
				entry, ok := value.(map[string]any)
				if !ok {
					return out, fmt.Errorf("semantic map entry is not an object")
				}
				mapped, err := mapTaggedSemanticValue(entry["value"])
				if err != nil {
					return out, err
				}
				out.Map = append(out.Map, engine.SemanticMapEntry{Key: stringValue(entry, "key"), Value: mapped})
			}
		}
	default:
		return out, fmt.Errorf("unknown semantic operation value kind %q", kind)
	}
	return out, nil
}

func mapPlainObject(input map[string]any) []engine.SemanticMapEntry {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]engine.SemanticMapEntry, 0, len(keys))
	for _, key := range keys {
		out = append(out, engine.SemanticMapEntry{Key: key, Value: mapPlainValue(input[key])})
	}
	return out
}
func mapPlainValue(input any) engine.SemanticValue {
	switch value := input.(type) {
	case nil:
		return engine.SemanticValue{Kind: engine.SemanticValueAbsent}
	case bool:
		return engine.SemanticValue{Kind: engine.SemanticValueBoolean, Boolean: value}
	case string:
		if strings.HasPrefix(value, "ldl:") {
			return engine.SemanticValue{Kind: engine.SemanticValueAddress, Address: value}
		}
		return engine.SemanticValue{Kind: engine.SemanticValueString, String: value}
	case json.Number:
		if integer, err := value.Int64(); err == nil {
			return engine.SemanticValue{Kind: engine.SemanticValueInteger, Integer: integer}
		}
		return engine.SemanticValue{Kind: engine.SemanticValueDecimal, Decimal: value.String()}
	case []any:
		items := make([]engine.SemanticValue, 0, len(value))
		for _, item := range value {
			items = append(items, mapPlainValue(item))
		}
		return engine.SemanticValue{Kind: engine.SemanticValueArray, Array: items}
	case map[string]any:
		return engine.SemanticValue{Kind: engine.SemanticValueMap, Map: mapPlainObject(value)}
	}
	return engine.SemanticValue{Kind: engine.SemanticValueString, String: fmt.Sprint(input)}
}
func stringValue(input map[string]any, key string) string {
	if value, ok := input[key].(string); ok {
		return value
	}
	if value, ok := input[key].(json.Number); ok {
		return value.String()
	}
	return ""
}
func numberInt64(input any) (int64, error) {
	switch value := input.(type) {
	case json.Number:
		return value.Int64()
	case string:
		return strconv.ParseInt(value, 10, 64)
	case float64:
		return int64(value), nil
	}
	return 0, fmt.Errorf("invalid semantic integer")
}
