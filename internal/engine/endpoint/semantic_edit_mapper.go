// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

// MapSemanticEditPlanInput is the single handwritten boundary between the
// complete generated Workbench operation contract and the pure Engine
// planner. Generated validation runs before any planner-domain value exists.
func MapSemanticEditPlanInput(baseInput engine.CompileInput, base engine.Snapshot, preconditions engineprotocol.EngineEditPreconditions, batch engineprotocol.SemanticOperationBatch) (engine.SemanticEditPlanInput, error) {
	if _, err := engineprotocol.EncodeEngineEditPreconditions(preconditions); err != nil {
		return engine.SemanticEditPlanInput{}, fmt.Errorf("map semantic edit preconditions: %w", err)
	}
	return mapSemanticEditPlanInput(baseInput, base, preconditions, batch, nil)
}

// MapPreviewOperationsPlanInput preserves the complete generated request,
// including document generation and explicit response/work bounds.
func MapPreviewOperationsPlanInput(baseInput engine.CompileInput, base engine.Snapshot, input engineprotocol.PreviewOperationsInput) (engine.SemanticEditPlanInput, error) {
	if _, err := engineprotocol.EncodePreviewOperationsInput(input); err != nil {
		return engine.SemanticEditPlanInput{}, fmt.Errorf("map preview operations input: %w", err)
	}
	return mapSemanticEditPlanInput(baseInput, base, input.Preconditions, input.Batch, &input.Limits)
}

func mapSemanticEditPlanInput(baseInput engine.CompileInput, base engine.Snapshot, preconditions engineprotocol.EngineEditPreconditions, batch engineprotocol.SemanticOperationBatch, limits *engineprotocol.WorkbenchLimits) (engine.SemanticEditPlanInput, error) {
	_, err := engineprotocol.EncodeSemanticOperationBatch(batch)
	if err != nil {
		return engine.SemanticEditPlanInput{}, fmt.Errorf("map semantic operation batch: %w", err)
	}
	operations := make([]engine.SemanticOperation, 0, len(batch.Operations))
	for _, generated := range batch.Operations {
		operation, mapErr := mapGeneratedSemanticOperation(generated)
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
			pre.ExpectedSourceDigests = append(pre.ExpectedSourceDigests, engine.ExpectedSemanticSourceDigest{Module: mapGeneratedModuleRef(expected.Module), Digest: string(expected.Digest)})
		}
	}
	plan := engine.SemanticEditPlanInput{BaseInput: baseInput, BaseSnapshot: base, Batch: engine.SemanticOperationBatch{Operations: operations}, Preconditions: pre, Generation: engine.SemanticDocumentGeneration{EndpointInstanceID: string(preconditions.DocumentGeneration.DocumentHandle.EndpointInstanceID), DocumentHandle: preconditions.DocumentGeneration.DocumentHandle.Value, Value: string(preconditions.DocumentGeneration.Value)}}
	if limits != nil {
		maxItems, parseErr := strconv.ParseInt(string(limits.MaxItems), 10, 64)
		if parseErr != nil {
			return engine.SemanticEditPlanInput{}, fmt.Errorf("map max_items: %w", parseErr)
		}
		maxBytes, parseErr := strconv.ParseInt(string(limits.MaxOutputBytes), 10, 64)
		if parseErr != nil {
			return engine.SemanticEditPlanInput{}, fmt.Errorf("map max_output_bytes: %w", parseErr)
		}
		plan.Limits = engine.SemanticPlanLimits{MaxItems: maxItems, MaxOutputBytes: maxBytes}
	}
	return plan, nil
}

func mapGeneratedModuleRef(module semantic.ModuleRef) engine.PlannedModuleRef {
	mapped := engine.PlannedModuleRef{OriginKind: engine.SourceOriginKind(module.Origin.Kind), ModulePath: module.ModulePath}
	if module.Origin.PackAddress != nil {
		mapped.PackAddress = string(*module.Origin.PackAddress)
	}
	return mapped
}

func mapGeneratedSemanticOperation(generated engineprotocol.SemanticOperation) (engine.SemanticOperation, error) {
	if generated.NonCreateSemanticOperation != nil {
		value := generated.NonCreateSemanticOperation
		op := engine.SemanticOperation{Kind: engine.SemanticOperationKind(value.Operation)}
		copyGeneratedString := func(source any, target *string) {
			reflected := reflect.ValueOf(source)
			if reflected.Kind() == reflect.Pointer && !reflected.IsNil() {
				*target = fmt.Sprint(reflected.Elem().Interface())
			}
		}
		copyGeneratedString(value.Action, &op.Action)
		copyGeneratedString(value.Endpoint, &op.Endpoint)
		copyGeneratedString(value.EntityAddress, &op.EntityAddress)
		copyGeneratedString(value.FromAddress, &op.FromAddress)
		copyGeneratedString(value.ID, &op.ID)
		if value.RowID != nil {
			op.ID = string(*value.RowID)
		}
		copyGeneratedString(value.LayerAddress, &op.LayerAddress)
		copyGeneratedString(value.NewID, &op.NewID)
		copyGeneratedString(value.NewProjectID, &op.NewProjectID)
		copyGeneratedString(value.OwnerAddress, &op.OwnerAddress)
		copyGeneratedString(value.ParentAddress, &op.ParentAddress)
		copyGeneratedString(value.ProjectAddress, &op.ProjectAddress)
		copyGeneratedString(value.RelationAddress, &op.RelationAddress)
		copyGeneratedString(value.RowAddress, &op.RowAddress)
		copyGeneratedString(value.TargetAddress, &op.TargetAddress)
		copyGeneratedString(value.ToAddress, &op.ToAddress)
		copyGeneratedString(value.TypeAddress, &op.TypeAddress)
		if value.Path != nil {
			op.Path = append([]string(nil), (*value.Path)...)
		}
		if value.Value != nil {
			mapped, err := mapGeneratedTaggedValue(*value.Value)
			if err != nil {
				return op, err
			}
			op.Value = &mapped
		}
		if value.Fields != nil {
			op.Fields = mapGeneratedStructFields(reflect.ValueOf(*value.Fields))
		}
		if value.Values != nil {
			for _, cell := range *value.Values {
				mapped, err := mapGeneratedTaggedValue(cell.Value)
				if err != nil {
					return op, err
				}
				op.Values = append(op.Values, engine.SemanticRowCell{ColumnAddress: string(cell.ColumnAddress), Value: mapped})
			}
		}
		if value.ExplicitAbsentColumnAddresses != nil {
			for _, address := range *value.ExplicitAbsentColumnAddresses {
				op.ExplicitAbsentColumnAddresses = append(op.ExplicitAbsentColumnAddresses, string(address))
			}
		}
		if value.Placement != nil {
			op.Placement = mapGeneratedPlacement(value.Placement)
		}
		if err := validateMappedOperation(op); err != nil {
			return op, err
		}
		return op, nil
	}
	if generated.CreateSubjectOperation == nil {
		return engine.SemanticOperation{}, fmt.Errorf("generated semantic operation has no alternative")
	}
	union := reflect.ValueOf(*generated.CreateSubjectOperation)
	for index := 0; index < union.NumField(); index++ {
		alternative := union.Field(index)
		if alternative.IsNil() {
			continue
		}
		operation := alternative.Elem()
		op := engine.SemanticOperation{Kind: engine.OperationCreateSubject, ParentAddress: fmt.Sprint(operation.FieldByName("ParentAddress").Interface()), SubjectKind: engine.SemanticSubjectKind(fmt.Sprint(operation.FieldByName("SubjectKind").Interface())), ID: fmt.Sprint(operation.FieldByName("ID").Interface()), Fields: mapGeneratedStructFields(operation.FieldByName("Fields"))}
		if placement := operation.FieldByName("Placement"); placement.IsValid() && !placement.IsNil() {
			op.Placement = mapGeneratedPlacement(placement.Interface().(*engineprotocol.PlacementHint))
		}
		if err := validateMappedOperation(op); err != nil {
			return op, err
		}
		return op, nil
	}
	return engine.SemanticOperation{}, fmt.Errorf("generated create operation has no alternative")
}

func validateMappedOperation(operation engine.SemanticOperation) error {
	if operation.Value != nil {
		if err := validateMappedValue(*operation.Value); err != nil {
			return err
		}
	}
	for _, field := range operation.Fields {
		if err := validateMappedValue(field.Value); err != nil {
			return fmt.Errorf("map create field %q: %w", field.Key, err)
		}
	}
	for _, cell := range operation.Values {
		if err := validateMappedValue(cell.Value); err != nil {
			return fmt.Errorf("map row cell %q: %w", cell.ColumnAddress, err)
		}
	}
	return nil
}

func validateMappedValue(value engine.SemanticValue) error {
	if value.Kind == "" {
		return fmt.Errorf("unsupported generated authored value")
	}
	for _, item := range value.Array {
		if err := validateMappedValue(item); err != nil {
			return err
		}
	}
	for _, item := range value.Map {
		if err := validateMappedValue(item.Value); err != nil {
			return err
		}
	}
	return nil
}

func mapGeneratedPlacement(value *engineprotocol.PlacementHint) *engine.SemanticPlacementHint {
	out := &engine.SemanticPlacementHint{Position: string(value.Position)}
	if value.ModulePath != nil {
		out.ModulePath = string(*value.ModulePath)
	}
	if value.GroupAnchorAddress != nil {
		out.GroupAnchorAddress = string(*value.GroupAnchorAddress)
	}
	return out
}

func mapGeneratedTaggedValue(value engineprotocol.SemanticOperationValue) (engine.SemanticValue, error) {
	out := engine.SemanticValue{Kind: engine.SemanticValueKind(value.Kind)}
	switch value.Kind {
	case engineprotocol.SemanticOperationValueKindAbsent:
	case engineprotocol.SemanticOperationValueKindAddress:
		out.Address = string(*value.Address)
	case engineprotocol.SemanticOperationValueKindBoolean:
		out.Boolean = *value.Boolean
	case engineprotocol.SemanticOperationValueKindDecimal:
		out.Decimal = string(*value.Decimal)
	case engineprotocol.SemanticOperationValueKindInteger:
		integer, err := strconv.ParseInt(string(*value.Integer), 10, 64)
		if err != nil {
			return out, err
		}
		out.Integer = integer
	case engineprotocol.SemanticOperationValueKindString:
		out.String = *value.String
	case engineprotocol.SemanticOperationValueKindBlob:
		size, err := strconv.ParseUint(string(value.Blob.Size), 10, 64)
		if err != nil {
			return out, fmt.Errorf("map semantic blob size: %w", err)
		}
		out.Blob = string(value.Blob.Digest)
		out.BlobRef = &engine.SemanticBlobRef{BlobID: value.Blob.BlobID, Digest: string(value.Blob.Digest), Lifetime: string(value.Blob.Lifetime), MediaType: value.Blob.MediaType, Size: size}
	case engineprotocol.SemanticOperationValueKindArray:
		for _, item := range *value.Array {
			mapped, err := mapGeneratedTaggedValue(item)
			if err != nil {
				return out, err
			}
			out.Array = append(out.Array, mapped)
		}
	case engineprotocol.SemanticOperationValueKindMap:
		for _, item := range *value.Map {
			mapped, err := mapGeneratedTaggedValue(item.Value)
			if err != nil {
				return out, err
			}
			out.Map = append(out.Map, engine.SemanticMapEntry{Key: item.Key, Value: mapped})
		}
	default:
		return out, fmt.Errorf("unknown semantic operation value kind %q", value.Kind)
	}
	return out, nil
}

func mapGeneratedStructFields(value reflect.Value) []engine.SemanticMapEntry {
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	out := make([]engine.SemanticMapEntry, 0, value.NumField())
	typeOf := value.Type()
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		if field.Kind() == reflect.Pointer && field.IsNil() {
			continue
		}
		name := strings.Split(typeOf.Field(index).Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		out = append(out, engine.SemanticMapEntry{Key: name, Value: mapGeneratedPlainValue(field)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func mapGeneratedPlainValue(value reflect.Value) engine.SemanticValue {
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		value = value.Elem()
	}
	typeOf := value.Type()
	name, packagePath := typeOf.Name(), typeOf.PkgPath()
	if value.Kind() == reflect.String {
		text := value.String()
		if name == "RelationCardinalityMaximum" && text == "1" {
			return engine.SemanticValue{Kind: engine.SemanticValueInteger, Integer: 1}
		}
		if strings.Contains(packagePath, "/gen/go/semantic") && (name == "StableAddress" || strings.HasSuffix(name, "Address")) {
			return engine.SemanticValue{Kind: engine.SemanticValueAddress, Address: text}
		}
		if strings.Contains(packagePath, "/gen/go/protocolcommon") && strings.Contains(name, "Canonical") && (strings.Contains(name, "Integer") || strings.Contains(name, "Int64")) {
			integer, _ := strconv.ParseInt(text, 10, 64)
			return engine.SemanticValue{Kind: engine.SemanticValueInteger, Integer: integer}
		}
		if strings.Contains(packagePath, "/gen/go/semantic") && strings.Contains(name, "FiniteDecimal") {
			return engine.SemanticValue{Kind: engine.SemanticValueDecimal, Decimal: text}
		}
		if strings.Contains(packagePath, "/gen/go/semantic") && name != "Color" && name != "LocalIdentifier" {
			return engine.SemanticValue{Kind: engine.SemanticValueToken, String: text}
		}
		return engine.SemanticValue{Kind: engine.SemanticValueString, String: text}
	}
	switch value.Kind() {
	case reflect.Bool:
		return engine.SemanticValue{Kind: engine.SemanticValueBoolean, Boolean: value.Bool()}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return engine.SemanticValue{Kind: engine.SemanticValueInteger, Integer: value.Int()}
	case reflect.Slice, reflect.Array:
		items := make([]engine.SemanticValue, 0, value.Len())
		for index := 0; index < value.Len(); index++ {
			items = append(items, mapGeneratedPlainValue(value.Index(index)))
		}
		return engine.SemanticValue{Kind: engine.SemanticValueArray, Array: items}
	case reflect.Struct:
		if typeOf.Name() == "BlobRef" {
			size, err := strconv.ParseUint(fmt.Sprint(value.FieldByName("Size").Interface()), 10, 64)
			if err != nil {
				return engine.SemanticValue{}
			}
			ref := &engine.SemanticBlobRef{BlobID: fmt.Sprint(value.FieldByName("BlobID").Interface()), Digest: fmt.Sprint(value.FieldByName("Digest").Interface()), Lifetime: fmt.Sprint(value.FieldByName("Lifetime").Interface()), MediaType: fmt.Sprint(value.FieldByName("MediaType").Interface()), Size: size}
			return engine.SemanticValue{Kind: engine.SemanticValueBlob, Blob: ref.Digest, BlobRef: ref}
		}
		if alternative, ok := generatedUnionAlternative(value); ok {
			return mapGeneratedPlainValue(alternative)
		}
		return engine.SemanticValue{Kind: engine.SemanticValueMap, Map: mapGeneratedStructFields(value)}
	case reflect.Map:
		keys := value.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface()) })
		entries := make([]engine.SemanticMapEntry, 0, len(keys))
		for _, key := range keys {
			entries = append(entries, engine.SemanticMapEntry{Key: fmt.Sprint(key.Interface()), Value: mapGeneratedPlainValue(value.MapIndex(key))})
		}
		return engine.SemanticValue{Kind: engine.SemanticValueMap, Map: entries}
	}
	return engine.SemanticValue{}
}

func generatedUnionAlternative(value reflect.Value) (reflect.Value, bool) {
	typeOf := value.Type()
	selected := reflect.Value{}
	for index := 0; index < value.NumField(); index++ {
		if strings.Split(typeOf.Field(index).Tag.Get("json"), ",")[0] != "-" || value.Field(index).Kind() != reflect.Pointer {
			return reflect.Value{}, false
		}
		if !value.Field(index).IsNil() {
			if selected.IsValid() {
				return reflect.Value{}, false
			}
			selected = value.Field(index).Elem()
		}
	}
	return selected, selected.IsValid()
}
