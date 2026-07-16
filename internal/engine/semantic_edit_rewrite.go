// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func applyRelationEndpoint(input *CompileInput, snapshot Snapshot, operation SemanticOperation) (*SemanticConflict, *Diagnostic) {
	semantic, source, ok := subjectRecord(snapshot, operation.RelationAddress)
	if !ok || semantic.Kind != materialize.SubjectRelation {
		return &SemanticConflict{Kind: ConflictDeleteVsUpdate, TargetAddress: operation.RelationAddress}, nil
	}
	if operation.Endpoint != "from" && operation.Endpoint != "to" {
		d := plannerDiagnostic("LDL1801", "invalid_semantic_operation", "relation endpoint must be from or to")
		return nil, &d
	}
	if _, _, ok := subjectRecord(snapshot, operation.EntityAddress); !ok {
		return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: operation.EntityAddress}, nil
	}
	object := normalizedSubject(snapshot, operation.RelationAddress)
	oldAddress, _ := object[operation.Endpoint+"_address"].(string)
	var candidates []index.SourceBindingRecord
	for _, binding := range snapshot.SourceMap.Bindings {
		if binding.SourceAddress == operation.RelationAddress && binding.TargetAddress == oldAddress && binding.Module.ModulePath == source.Module.ModulePath && binding.Range.StartByte >= source.DeclarationRange.StartByte && binding.Range.EndByte <= source.DeclarationRange.EndByte {
			candidates = append(candidates, binding)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Range.StartByte < candidates[j].Range.StartByte })
	if len(candidates) == 0 {
		return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: operation.RelationAddress}, nil
	}
	selected := candidates[0]
	if operation.Endpoint == "to" {
		selected = candidates[len(candidates)-1]
	}
	ref, ok := sourceReference(snapshot, source.Module.ModulePath, operation.EntityAddress)
	if !ok {
		return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: operation.EntityAddress}, nil
	}
	replaceSourceRange(input, source.Module.ModulePath, selected.Range.StartByte, selected.Range.EndByte, []byte(ref))
	return nil, nil
}

func applyMoveEntity(input *CompileInput, snapshot Snapshot, operation SemanticOperation) (*SemanticConflict, *Diagnostic) {
	semantic, source, ok := subjectRecord(snapshot, operation.EntityAddress)
	if !ok || semantic.Kind != materialize.SubjectEntity {
		return &SemanticConflict{Kind: ConflictDeleteVsUpdate, TargetAddress: operation.EntityAddress}, nil
	}
	target, _, ok := subjectRecord(snapshot, operation.LayerAddress)
	if !ok || target.Kind != materialize.SubjectLayer {
		return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: operation.LayerAddress}, nil
	}
	object := normalizedSubject(snapshot, operation.EntityAddress)
	typeAddress, _ := object["type_address"].(string)
	module := source.Module.ModulePath
	typeRef, ok := sourceReference(snapshot, module, typeAddress)
	if !ok {
		return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: typeAddress}, nil
	}
	layerRef, ok := sourceReference(snapshot, module, operation.LayerAddress)
	if !ok {
		return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: operation.LayerAddress}, nil
	}
	original := input.ProjectSourceTree[module]
	start, end := source.DeclarationRange.StartByte, source.DeclarationRange.EndByte
	item := strings.TrimSpace(string(original[start:end]))
	start, end = extendWholeLine(original, start, end)
	replaceSourceRange(input, module, start, end, nil)
	appendText(input, module, fmt.Sprintf("\nentities %s @%s {\n  %s\n}\n", typeRef, layerRef, item))
	return nil, nil
}

func applyUpdateField(input *CompileInput, snapshot Snapshot, operation SemanticOperation) (*SemanticConflict, *Diagnostic) {
	semantic, source, ok := subjectRecord(snapshot, operation.TargetAddress)
	if !ok {
		return &SemanticConflict{Kind: ConflictDeleteVsUpdate, TargetAddress: operation.TargetAddress}, nil
	}
	if len(operation.Path) == 0 || len(operation.Path) > 2 {
		d := plannerDiagnostic("LDL1801", "invalid_authored_field_path", "semantic field path must have one or two tokens")
		return nil, &d
	}
	module := source.Module.ModulePath
	field := operation.Path[0]
	if field == "display_name" {
		if operation.Action != "set" || operation.Value == nil || operation.Value.Kind != SemanticValueString {
			d := plannerDiagnostic("LDL1801", "invalid_authored_field_value", "display_name requires a string value")
			return nil, &d
		}
		span, ok := declarationHeaderValueSpan(snapshot, source, syntax.TokenString)
		if !ok {
			return &SemanticConflict{Kind: ConflictSameFieldChanged, TargetAddress: operation.TargetAddress, Path: operation.Path}, nil
		}
		replaceSourceRange(input, module, span.Start, span.End, []byte(strconv.Quote(operation.Value.String)))
		return nil, nil
	}
	if semantic.Kind == materialize.SubjectLayer && field == "order" {
		if operation.Action != "set" || operation.Value == nil || operation.Value.Kind != SemanticValueInteger {
			d := plannerDiagnostic("LDL1801", "invalid_authored_field_value", "Layer order requires an integer")
			return nil, &d
		}
		span, ok := declarationHeaderValueSpan(snapshot, source, syntax.TokenInteger)
		if !ok {
			return &SemanticConflict{Kind: ConflictSameFieldChanged, TargetAddress: operation.TargetAddress, Path: operation.Path}, nil
		}
		replaceSourceRange(input, module, span.Start, span.End, []byte(strconv.FormatInt(operation.Value.Integer, 10)))
		return nil, nil
	}
	if semantic.Kind == materialize.SubjectReference && field == "text" {
		if operation.Action != "set" || operation.Value == nil || operation.Value.Kind != SemanticValueString {
			d := plannerDiagnostic("LDL1801", "invalid_authored_field_value", "Reference text requires a string value")
			return nil, &d
		}
		id := terminalID(operation.TargetAddress)
		replacement := renderReference(id, operation.Value.String)
		replaceSourceRange(input, module, source.DeclarationRange.StartByte, source.DeclarationRange.EndByte, []byte(strings.TrimSuffix(replacement, "\n")))
		return nil, nil
	}
	if len(operation.Path) == 2 && field == "values" {
		return applyRowCell(input, snapshot, source, operation)
	}
	if len(operation.Path) == 2 && field == "annotations" {
		return applyAnnotationField(input, snapshot, source, operation)
	}
	var rendered string
	if operation.Action == "set" {
		if operation.Value == nil {
			d := plannerDiagnostic("LDL1801", "invalid_authored_field_value", "set requires a value")
			return nil, &d
		}
		value, ok := renderSemanticValue(snapshot, module, *operation.Value)
		if !ok {
			return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: operation.TargetAddress, Path: operation.Path}, nil
		}
		if special, handled, valid := renderSpecialField(snapshot, module, field, *operation.Value, 0); handled {
			if !valid {
				return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: operation.TargetAddress, Path: operation.Path}, nil
			}
			rendered = special
		} else {
			rendered = renderField(field, value)
		}
	} else if operation.Action != "remove" {
		d := plannerDiagnostic("LDL1801", "invalid_semantic_operation", "field action must be set or remove")
		return nil, &d
	}
	if !rewriteBlockField(input, snapshot, source, field, rendered, operation.Action == "remove") {
		return &SemanticConflict{Kind: ConflictSameFieldChanged, TargetAddress: operation.TargetAddress, Path: operation.Path}, nil
	}
	return nil, nil
}

func applyRowCell(input *CompileInput, snapshot Snapshot, source index.SourceSubjectRecord, operation SemanticOperation) (*SemanticConflict, *Diagnostic) {
	object := normalizedSubject(snapshot, operation.TargetAddress)
	values, _ := object["values"].(map[string]any)
	if values == nil {
		values = map[string]any{}
	}
	column := operation.Path[1]
	if operation.Action == "remove" {
		values[column] = nil
	} else if operation.Value != nil {
		values[column] = semanticValuePlain(*operation.Value)
	} else {
		d := plannerDiagnostic("LDL1801", "invalid_authored_field_value", "row cell set requires a value")
		return nil, &d
	}
	owner := ""
	if subject, _, ok := subjectRecord(snapshot, operation.TargetAddress); ok && subject.OwnerAddress != nil {
		owner = *subject.OwnerAddress
	}
	rowID := terminalID(operation.TargetAddress)
	if conflict, diag := applyDelete(input, snapshot, operation.TargetAddress); conflict != nil || diag != nil {
		return conflict, diag
	}
	cells := make([]SemanticRowCell, 0, len(values))
	for address, value := range values {
		cell := SemanticRowCell{ColumnAddress: address, Value: plainSemanticValue(value)}
		if value == nil {
			cell.Value = SemanticValue{Kind: SemanticValueAbsent}
		}
		cells = append(cells, cell)
	}
	sort.Slice(cells, func(i, j int) bool { return cells[i].ColumnAddress < cells[j].ColumnAddress })
	return appendRowGroup(input, snapshot, source.Module.ModulePath, owner, rowID, cells, nil)
}

func applyAnnotationField(input *CompileInput, snapshot Snapshot, source index.SourceSubjectRecord, operation SemanticOperation) (*SemanticConflict, *Diagnostic) {
	object := normalizedSubject(snapshot, operation.TargetAddress)
	annotations, _ := object["annotations"].(map[string]any)
	if annotations == nil {
		annotations = map[string]any{}
	}
	if operation.Action == "remove" {
		delete(annotations, operation.Path[1])
	} else if operation.Value != nil && operation.Value.Kind == SemanticValueString {
		annotations[operation.Path[1]] = operation.Value.String
	} else {
		d := plannerDiagnostic("LDL1801", "invalid_authored_field_value", "annotation values must be strings")
		return nil, &d
	}
	keys := make([]string, 0, len(annotations))
	for key := range annotations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+": "+strconv.Quote(fmt.Sprint(annotations[key])))
	}
	rendered := "annotations { " + strings.Join(parts, ", ") + " }"
	if !rewriteBlockField(input, snapshot, source, "annotations", rendered, len(keys) == 0) {
		return &SemanticConflict{Kind: ConflictSameFieldChanged, TargetAddress: operation.TargetAddress, Path: operation.Path}, nil
	}
	return nil, nil
}

func applyCreateRelation(input *CompileInput, snapshot Snapshot, operation SemanticOperation) (*SemanticConflict, *Diagnostic) {
	project := rootProjectAddress(snapshot)
	if operation.ParentAddress != project {
		return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: operation.ParentAddress}, nil
	}
	module, conflict := placementModule(snapshot, operation.Placement, project, input.EntryPath)
	if conflict != nil {
		return conflict, nil
	}
	typeRef, ok := sourceReference(snapshot, module, operation.TypeAddress)
	if !ok {
		return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: operation.TypeAddress}, nil
	}
	fromRef, ok := sourceReference(snapshot, module, operation.FromAddress)
	if !ok {
		return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: operation.FromAddress}, nil
	}
	toRef, ok := sourceReference(snapshot, module, operation.ToAddress)
	if !ok {
		return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: operation.ToAddress}, nil
	}
	fields := semanticMap(operation.Fields)
	display := ""
	if value, ok := fields["display_name"]; ok {
		display, _ = renderSemanticValue(snapshot, module, value)
	}
	body := renderFields(snapshot, module, fields, map[string]bool{"display_name": true}, 4)
	item := fmt.Sprintf("  %s: %s -> %s", operation.ID, fromRef, toRef)
	if display != "" {
		item += " " + display
	}
	if body != "" {
		item += " {\n" + body + "  }"
	}
	appendText(input, module, fmt.Sprintf("\nrelations %s {\n%s\n}\n", typeRef, item))
	return nil, nil
}

func applyUpsertRow(input *CompileInput, snapshot Snapshot, operation SemanticOperation) (*SemanticConflict, *Diagnostic) {
	owner, ownerSource, ok := subjectRecord(snapshot, operation.OwnerAddress)
	if !ok || (owner.Kind != materialize.SubjectEntity && owner.Kind != materialize.SubjectRelation) {
		return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: operation.OwnerAddress}, nil
	}
	rowKind := rowKindForOwner(owner)
	address := predictedChildAddress(operation.OwnerAddress, rowKind, operation.ID)
	if _, existing, ok := subjectRecord(snapshot, address); ok {
		if conflict, diag := applyDelete(input, snapshot, address); conflict != nil || diag != nil {
			return conflict, diag
		}
		ownerSource = existing
	}
	return appendRowGroup(input, snapshot, ownerSource.Module.ModulePath, operation.OwnerAddress, operation.ID, operation.Values, operation.ExplicitAbsentColumnAddresses)
}

func appendRowGroup(input *CompileInput, snapshot Snapshot, module, ownerAddress, rowID string, cells []SemanticRowCell, absent []string) (*SemanticConflict, *Diagnostic) {
	ownerRef, ok := sourceReference(snapshot, module, ownerAddress)
	if !ok {
		return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: ownerAddress}, nil
	}
	byColumn := map[string]SemanticValue{}
	for _, cell := range cells {
		byColumn[cell.ColumnAddress] = cell.Value
	}
	for _, address := range absent {
		byColumn[address] = SemanticValue{Kind: SemanticValueAbsent}
	}
	columns := make([]string, 0, len(byColumn))
	for address := range byColumn {
		columns = append(columns, address)
	}
	sort.Strings(columns)
	columnRefs := make([]string, 0, len(columns))
	values := make([]string, 0, len(columns))
	for _, address := range columns {
		ref, found := sourceReference(snapshot, module, address)
		if !found {
			return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: address}, nil
		}
		columnRefs = append(columnRefs, ref)
		value, found := renderSemanticValue(snapshot, module, byColumn[address])
		if !found {
			return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: address}, nil
		}
		values = append(values, value)
	}
	owner := normalizedSubject(snapshot, ownerAddress)
	prefix := "rows"
	typeAddress, _ := owner["type_address"].(string)
	typeRef, typeOK := sourceReference(snapshot, module, typeAddress)
	if !typeOK {
		return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: typeAddress}, nil
	}
	if _, ok := owner["from_address"]; ok {
		prefix = "relation_rows"
	}
	appendText(input, module, fmt.Sprintf("\n%s %s [%s] {\n  %s %s: %s\n}\n", prefix, typeRef, strings.Join(columnRefs, ", "), ownerRef, rowID, strings.Join(values, ", ")))
	return nil, nil
}

func applyCreateSubject(input *CompileInput, snapshot Snapshot, operation SemanticOperation) (*SemanticConflict, *Diagnostic) {
	module, conflict := placementModule(snapshot, operation.Placement, operation.ParentAddress, input.EntryPath)
	if conflict != nil {
		return conflict, nil
	}
	fields := semanticMap(operation.Fields)
	declaration, child, ok := renderCreatedSubject(snapshot, module, operation.SubjectKind, operation.ID, fields)
	if !ok {
		d := plannerDiagnostic("LDL1801", "invalid_create_subject", "create_subject fields cannot be represented as canonical LDL")
		return nil, &d
	}
	if !child {
		appendText(input, module, "\n"+declaration+"\n")
		return nil, nil
	}
	_, ownerSource, ok := subjectRecord(snapshot, operation.ParentAddress)
	if !ok || ownerSource.Module.ModulePath != module {
		return &SemanticConflict{Kind: ConflictPlacementChanged, OwnerAddress: operation.ParentAddress}, nil
	}
	if !insertOwnerChild(input, snapshot, ownerSource, operation.SubjectKind, declaration) {
		return &SemanticConflict{Kind: ConflictPlacementChanged, OwnerAddress: operation.ParentAddress, ChildKind: operation.SubjectKind}, nil
	}
	return nil, nil
}

func renderCreatedSubject(snapshot Snapshot, module string, kind SemanticSubjectKind, id string, fields map[string]SemanticValue) (string, bool, bool) {
	display := renderedFieldValue(snapshot, module, fields, "display_name")
	switch kind {
	case materialize.SubjectEntityType:
		return fmt.Sprintf("entity_type %s %s {\n%s}", id, display, renderFields(snapshot, module, fields, map[string]bool{"display_name": true}, 2)), false, display != ""
	case materialize.SubjectRelationType:
		semantic := renderedFieldValue(snapshot, module, fields, "semantic_kind")
		return fmt.Sprintf("relation_type %s %s %s {\n%s}", id, display, semantic, renderFields(snapshot, module, fields, map[string]bool{"display_name": true, "semantic_kind": true}, 2)), false, display != "" && semantic != ""
	case materialize.SubjectLayer:
		order := renderedFieldValue(snapshot, module, fields, "order")
		body := renderFields(snapshot, module, fields, map[string]bool{"display_name": true, "order": true}, 4)
		item := fmt.Sprintf("  %s %s @%s", id, display, order)
		if body != "" {
			item += " {\n" + body + "  }"
		}
		return "layers {\n" + item + "\n}", false, display != "" && order != ""
	case materialize.SubjectEntity:
		typeRef := renderedFieldValue(snapshot, module, fields, "type_address")
		layerRef := renderedFieldValue(snapshot, module, fields, "layer_address")
		body := renderFields(snapshot, module, fields, map[string]bool{"display_name": true, "type_address": true, "layer_address": true}, 4)
		item := fmt.Sprintf("  %s %s", id, display)
		if body != "" {
			item += " {\n" + body + "  }"
		}
		return fmt.Sprintf("entities %s @%s {\n%s\n}", typeRef, layerRef, item), false, display != "" && typeRef != "" && layerRef != ""
	case materialize.SubjectQuery:
		return fmt.Sprintf("query %s %s {\n%s}", id, display, renderFields(snapshot, module, fields, map[string]bool{"display_name": true}, 2)), false, display != ""
	case materialize.SubjectView:
		category := renderedFieldValue(snapshot, module, fields, "category")
		return fmt.Sprintf("view %s %s %s {\n%s}", id, display, category, renderFields(snapshot, module, fields, map[string]bool{"display_name": true, "category": true}, 2)), false, display != "" && category != ""
	case materialize.SubjectReference:
		text := fields["text"]
		if text.Kind != SemanticValueString {
			return "", false, false
		}
		return strings.TrimSuffix(renderReference(id, text.String), "\n"), false, true
	case materialize.SubjectEntityTypeColumn, materialize.SubjectRelationTypeColumn:
		return renderColumn(snapshot, module, id, fields, true), true, display != ""
	case materialize.SubjectEntityTypeConstraint, materialize.SubjectRelationTypeConstraint:
		columns := renderedFieldValue(snapshot, module, fields, "column_addresses")
		return fmt.Sprintf("unique %s %s", id, columns), true, columns != ""
	case materialize.SubjectQueryParameter:
		return renderColumn(snapshot, module, id, fields, false), true, renderedFieldValue(snapshot, module, fields, "value_type") != ""
	case materialize.SubjectViewTableColumn:
		return "column " + id + " {\n" + renderFields(snapshot, module, fields, nil, 2) + "}", true, true
	case materialize.SubjectViewExport:
		format := renderedFieldValue(snapshot, module, fields, "format")
		filename := renderedFieldValue(snapshot, module, fields, "filename")
		return fmt.Sprintf("export %s %s %s {\n%s}", id, format, filename, renderFields(snapshot, module, fields, map[string]bool{"format": true, "filename": true}, 2)), true, format != "" && filename != ""
	default:
		return "", false, false
	}
}

func renderColumn(snapshot Snapshot, module, id string, fields map[string]SemanticValue, display bool) string {
	parts := []string{id}
	if display {
		parts = append(parts, renderedFieldValue(snapshot, module, fields, "display_name"))
	}
	parts = append(parts, renderedFieldValue(snapshot, module, fields, "value_type"))
	order := []string{"enum_values", "reserved_enum_values", "required", "default", "format", "min", "max", "min_length", "max_length"}
	for _, key := range order {
		value, ok := fields[key]
		if !ok {
			continue
		}
		rendered, valid := renderSemanticValue(snapshot, module, value)
		if !valid {
			continue
		}
		switch key {
		case "required":
			if rendered == "true" {
				parts = append(parts, "required")
			}
		case "enum_values":
			parts[len(parts)-1] = "enum " + rendered
		default:
			parts = append(parts, key, rendered)
		}
	}
	return strings.Join(parts, " ")
}

func insertOwnerChild(input *CompileInput, snapshot Snapshot, owner index.SourceSubjectRecord, kind SemanticSubjectKind, declaration string) bool {
	path := owner.Module.ModulePath
	source := input.ProjectSourceTree[path]
	start, end := owner.DeclarationRange.StartByte, owner.DeclarationRange.EndByte
	if start < 0 || end > len(source) {
		return false
	}
	blockName := ""
	switch kind {
	case materialize.SubjectEntityTypeColumn, materialize.SubjectRelationTypeColumn:
		blockName = "columns"
	case materialize.SubjectQueryParameter:
		blockName = "parameters"
	case materialize.SubjectViewTableColumn:
		blockName = "table"
	}
	if blockName != "" {
		if open, close, ok := findNamedBlock(source, start, end, blockName); ok {
			indent := lineIndent(source, open) + "  "
			replaceSourceRange(input, path, close, close, []byte("\n"+indent+indentMultiline(declaration, indent)))
			return true
		}
	}
	close := bytes.LastIndexByte(source[start:end], '}')
	if close < 0 {
		return false
	}
	close += start
	indent := lineIndent(source, start) + "  "
	text := "\n" + indent
	if blockName != "" {
		text += blockName + " {\n" + indent + "  " + indentMultiline(declaration, indent+"  ") + "\n" + indent + "}"
	} else {
		text += indentMultiline(declaration, indent)
	}
	replaceSourceRange(input, path, close, close, []byte(text))
	return true
}

func placementModule(snapshot Snapshot, placement *SemanticPlacementHint, owner, fallback string) (string, *SemanticConflict) {
	if placement != nil && placement.GroupAnchorAddress != "" {
		_, source, ok := subjectRecord(snapshot, placement.GroupAnchorAddress)
		if !ok {
			return "", &SemanticConflict{Kind: ConflictPlacementChanged, TargetAddress: placement.GroupAnchorAddress}
		}
		if placement.ModulePath != "" && placement.ModulePath != source.Module.ModulePath {
			return "", &SemanticConflict{Kind: ConflictPlacementChanged, TargetAddress: placement.GroupAnchorAddress}
		}
		return source.Module.ModulePath, nil
	}
	if placement != nil && placement.ModulePath != "" {
		for _, file := range snapshot.SourceMap.Files {
			if file.Origin.Kind == "project" && file.ModulePath == placement.ModulePath {
				return placement.ModulePath, nil
			}
		}
		return "", &SemanticConflict{Kind: ConflictPlacementChanged, Path: []string{placement.ModulePath}}
	}
	if _, source, ok := subjectRecord(snapshot, owner); ok {
		return source.Module.ModulePath, nil
	}
	return fallback, nil
}

func appendText(input *CompileInput, module, text string) {
	source := input.ProjectSourceTree[module]
	if len(source) != 0 && source[len(source)-1] != '\n' {
		source = append(source, '\n')
	}
	input.ProjectSourceTree[module] = append(source, []byte(strings.TrimPrefix(text, "\n"))...)
}

func renderReference(id, text string) string {
	delim := "LDL_REFERENCE"
	for strings.Contains(text, "\n"+delim+"\n") || strings.HasSuffix(text, "\n"+delim) {
		delim += "_TEXT"
	}
	return fmt.Sprintf("reference %s <<-%s\n%s\n%s\n", id, delim, strings.TrimSuffix(text, "\n"), delim)
}

func semanticMap(entries []SemanticMapEntry) map[string]SemanticValue {
	out := map[string]SemanticValue{}
	for _, entry := range entries {
		out[entry.Key] = entry.Value
	}
	return out
}

func renderedFieldValue(snapshot Snapshot, module string, fields map[string]SemanticValue, key string) string {
	rendered, _ := renderSemanticValue(snapshot, module, fields[key])
	return rendered
}

func renderFields(snapshot Snapshot, module string, fields map[string]SemanticValue, skip map[string]bool, indent int) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		if skip == nil || !skip[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var out strings.Builder
	prefix := strings.Repeat(" ", indent)
	for _, key := range keys {
		value := fields[key]
		if special, handled, valid := renderSpecialField(snapshot, module, key, value, indent); handled {
			if valid {
				out.WriteString(prefix)
				out.WriteString(special)
				out.WriteByte('\n')
			}
			continue
		}
		rendered, ok := renderSemanticValue(snapshot, module, value)
		if !ok {
			continue
		}
		out.WriteString(prefix)
		out.WriteString(renderField(key, rendered))
		out.WriteByte('\n')
	}
	return out.String()
}

func renderSpecialField(snapshot Snapshot, module, key string, value SemanticValue, indent int) (string, bool, bool) {
	entries := semanticMap(value.Map)
	switch key {
	case "representation":
		if value.Kind != SemanticValueMap {
			return "", true, false
		}
		kind := rawString(entries["kind"])
		if kind == "" {
			return "", true, false
		}
		result := "representation " + kind
		if shape := rawString(entries["shape"]); shape != "" {
			result += " " + shape
		}
		return result, true, true
	case "from", "to":
		if value.Kind != SemanticValueMap {
			return "", true, false
		}
		role := rawString(entries["role"])
		if role == "" {
			return "", true, false
		}
		parts := []string{key, role}
		for _, pair := range []struct{ field, source string }{{"types", "entity_type_addresses"}, {"layers", "layer_addresses"}} {
			if item, ok := entries[pair.source]; ok {
				rendered, valid := renderSemanticValue(snapshot, module, item)
				if !valid {
					return "", true, false
				}
				parts = append(parts, pair.field, rendered)
			}
		}
		return strings.Join(parts, " "), true, true
	case "cardinality":
		if value.Kind != SemanticValueMap {
			return "", true, false
		}
		prefix := strings.Repeat(" ", indent+2)
		lines := []string{"cardinality {"}
		for _, name := range []string{"to_per_from", "from_per_to"} {
			bound, ok := entries[name]
			if !ok || bound.Kind != SemanticValueMap {
				return "", true, false
			}
			parts := semanticMap(bound.Map)
			min, _ := renderSemanticValue(snapshot, module, parts["min"])
			max, _ := renderSemanticValue(snapshot, module, parts["max"])
			if max == `"many"` {
				max = "*"
			}
			lines = append(lines, prefix+name+" "+min+".."+max)
		}
		lines = append(lines, strings.Repeat(" ", indent)+"}")
		return strings.Join(lines, "\n"), true, true
	case "select":
		return renderAddressBlock(snapshot, module, "select", entries, indent, map[string]string{"layer_addresses": "layers", "entity_type_addresses": "entity_types", "relation_type_addresses": "relation_types", "root_addresses": "roots"}), true, true
	case "shape":
		if value.Kind != SemanticValueMap {
			return "", true, false
		}
		kind := rawString(entries["kind"])
		if kind == "" {
			return "", true, false
		}
		delete(entries, "kind")
		return renderAddressBlock(snapshot, module, kind, entries, indent, map[string]string{"entity_type_addresses": "entity_types", "relation_type_addresses": "relation_types", "lane_column_addresses": "lane_columns"}), true, true
	case "source":
		if value.Kind != SemanticValueMap {
			return "", true, false
		}
		kind := rawString(entries["kind"])
		switch kind {
		case "field":
			field := rawString(entries["field"])
			return "source field " + field, true, field != ""
		case "attribute":
			columns, ok := entries["column_addresses"]
			if !ok || len(columns.Array) == 0 {
				return "", true, false
			}
			rendered, valid := renderSemanticValue(snapshot, module, columns.Array[0])
			return "source attribute " + rendered, true, valid
		case "query":
			query, valid := renderSemanticValue(snapshot, module, entries["query_address"])
			if !valid {
				return "", true, false
			}
			return "source query " + query + " {}", true, true
		case "state":
			path := rawString(entries["field_path"])
			return "source state " + path, true, path != ""
		}
		return "", true, false
	case "traverse":
		if value.Kind != SemanticValueMap {
			return "", true, false
		}
		direction := rawString(entries["direction"])
		min, _ := renderSemanticValue(snapshot, module, entries["min_depth"])
		max, _ := renderSemanticValue(snapshot, module, entries["max_depth"])
		cycle := rawString(entries["cycle_policy"])
		if direction == "" || min == "" || max == "" || cycle == "" {
			return "", true, false
		}
		result := "traverse " + direction + " " + min + ".." + max + " " + cycle
		if rels, ok := entries["relation_type_addresses"]; ok {
			rendered, valid := renderSemanticValue(snapshot, module, rels)
			if !valid {
				return "", true, false
			}
			result += " relations " + rendered
		}
		return result, true, true
	case "annotations":
		if value.Kind == SemanticValueArray {
			parts := []string{}
			for _, item := range value.Array {
				if item.Kind != SemanticValueMap {
					continue
				}
				entry := semanticMap(item.Map)
				name := rawString(entry["key"])
				text := rawString(entry["value"])
				parts = append(parts, name+": "+strconv.Quote(text))
			}
			return "annotations { " + strings.Join(parts, ", ") + " }", true, true
		}
	case "forward_label":
		if value.Kind == SemanticValueString {
			return "label " + strconv.Quote(value.String), true, true
		}
	case "description", "display_name", "intent", "icon", "color", "reverse_label", "filename", "label":
		if value.Kind == SemanticValueString {
			return key + " " + strconv.Quote(value.String), true, true
		}
	case "source_refs", "diagnostics", "state_summary", "interactive", "embed_assets", "bundle", "header", "source_manifest", "lookup_sheets", "hidden_ids", "formulas", "view_data_json":
		if value.Kind == SemanticValueBoolean {
			if value.Boolean {
				return key, true, true
			}
			return "", true, true
		}
	}
	return "", false, false
}

func renderAddressBlock(snapshot Snapshot, module, head string, entries map[string]SemanticValue, indent int, names map[string]string) string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	prefix := strings.Repeat(" ", indent+2)
	lines := []string{head + " {"}
	for _, key := range keys {
		rendered, ok := renderSemanticValue(snapshot, module, entries[key])
		if !ok {
			continue
		}
		name := key
		if mapped := names[key]; mapped != "" {
			name = mapped
		}
		lines = append(lines, prefix+name+" "+rendered)
	}
	lines = append(lines, strings.Repeat(" ", indent)+"}")
	return strings.Join(lines, "\n")
}
func rawString(value SemanticValue) string {
	if value.Kind == SemanticValueString {
		return value.String
	}
	return ""
}
func renderField(key, rendered string) string {
	return key + " " + rendered
}

func renderSemanticValue(snapshot Snapshot, module string, value SemanticValue) (string, bool) {
	switch value.Kind {
	case SemanticValueAbsent:
		return "_", true
	case SemanticValueAddress:
		return sourceReference(snapshot, module, value.Address)
	case SemanticValueBoolean:
		return strconv.FormatBool(value.Boolean), true
	case SemanticValueDecimal:
		return value.Decimal, value.Decimal != ""
	case SemanticValueInteger:
		return strconv.FormatInt(value.Integer, 10), true
	case SemanticValueString:
		if bareSemanticStrings[value.String] {
			return value.String, true
		}
		return strconv.Quote(value.String), true
	case SemanticValueBlob:
		return "", false
	case SemanticValueArray:
		parts := make([]string, 0, len(value.Array))
		for _, item := range value.Array {
			rendered, ok := renderSemanticValue(snapshot, module, item)
			if !ok {
				return "", false
			}
			parts = append(parts, rendered)
		}
		return "[" + strings.Join(parts, ", ") + "]", true
	case SemanticValueMap:
		parts := make([]string, 0, len(value.Map))
		for _, entry := range value.Map {
			rendered, ok := renderSemanticValue(snapshot, module, entry.Value)
			if !ok {
				return "", false
			}
			parts = append(parts, entry.Key+" "+rendered)
		}
		return "{ " + strings.Join(parts, " ") + " }", true
	default:
		return "", false
	}
}

var bareSemanticStrings = map[string]bool{
	"asset": true, "attribute": true, "attribute_summary": true, "auto": true, "automatic": true, "automatic_relations": true,
	"boolean": true, "both": true, "bottom_left": true, "bottom_right": true, "bottom_to_top": true,
	"context": true, "control": true, "count": true, "count_distinct": true, "data": true, "date": true, "datetime": true, "detail": true, "diagram": true, "diff": true,
	"entity": true, "entity_rows": true, "entity_type": true, "enum": true, "error": true, "exists": true,
	"flow": true, "force": true, "forward": true, "grid": true, "hierarchy": true, "incoming": true, "integer": true, "inventory": true,
	"bpmn": true, "csv": true, "docx": true, "drawio": true, "html": true, "json": true, "markdown": true, "mermaid": true, "pdf": true, "png": true, "pptx": true, "svg": true, "tsv": true, "xlsx": true, "yaml": true,
	"join_unique": true, "layer": true, "layered": true, "left_to_right": true, "lossless": true, "lossy": true,
	"manual": true, "matrix": true, "max": true, "message": true, "min": true, "none": true, "normal": true, "number": true,
	"outgoing": true, "path_refs": true, "radial": true, "reference": true, "relation": true, "relation_refs": true, "relation_rows": true, "relation_types": true,
	"right_to_left": true, "sequence": true, "shape": true, "string": true, "summary": true, "table": true, "top_left": true, "top_right": true, "top_to_bottom": true, "topology": true, "traceable_summary": true, "tree": true, "visual_only": true,
}

func terminalID(address string) string {
	return address[strings.LastIndex(address, ":")+1:]
}

func rootProjectAddress(snapshot Snapshot) string {
	if snapshot.NormalizedDocument != nil {
		return snapshot.NormalizedDocument.Project.Address
	}
	return ""
}

func normalizedSubject(snapshot Snapshot, address string) map[string]any {
	var root any
	if json.Unmarshal(snapshot.CanonicalJSON, &root) != nil {
		return nil
	}
	return findAddressObject(root, address)
}

func findAddressObject(value any, address string) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		if typed["address"] == address {
			return typed
		}
		for _, child := range typed {
			if found := findAddressObject(child, address); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range typed {
			if found := findAddressObject(child, address); found != nil {
				return found
			}
		}
	}
	return nil
}

func semanticValuePlain(value SemanticValue) any {
	switch value.Kind {
	case SemanticValueAbsent:
		return nil
	case SemanticValueAddress:
		return value.Address
	case SemanticValueBoolean:
		return value.Boolean
	case SemanticValueDecimal:
		f, _ := strconv.ParseFloat(value.Decimal, 64)
		return f
	case SemanticValueInteger:
		return value.Integer
	case SemanticValueString:
		return value.String
	case SemanticValueArray:
		out := make([]any, len(value.Array))
		for i, v := range value.Array {
			out[i] = semanticValuePlain(v)
		}
		return out
	case SemanticValueMap:
		out := map[string]any{}
		for _, e := range value.Map {
			out[e.Key] = semanticValuePlain(e.Value)
		}
		return out
	}
	return nil
}

func plainSemanticValue(value any) SemanticValue {
	switch v := value.(type) {
	case nil:
		return SemanticValue{Kind: SemanticValueAbsent}
	case string:
		return SemanticValue{Kind: SemanticValueString, String: v}
	case bool:
		return SemanticValue{Kind: SemanticValueBoolean, Boolean: v}
	case float64:
		if v == float64(int64(v)) {
			return SemanticValue{Kind: SemanticValueInteger, Integer: int64(v)}
		}
		return SemanticValue{Kind: SemanticValueDecimal, Decimal: strconv.FormatFloat(v, 'g', -1, 64)}
	}
	return SemanticValue{Kind: SemanticValueAbsent}
}

func declarationHeaderValueSpan(snapshot Snapshot, source index.SourceSubjectRecord, tokenKind syntax.TokenKind) (syntax.Span, bool) {
	for _, file := range snapshot.LosslessSyntaxTree.Files {
		if file.ModulePath != source.Module.ModulePath || file.Origin.Kind != source.Module.Origin.Kind {
			continue
		}
		for _, token := range file.Tokens {
			if token.Span.Start >= source.DeclarationRange.StartByte && token.Span.End <= source.DeclarationRange.EndByte && token.Kind == tokenKind {
				return token.Span, true
			}
		}
	}
	return syntax.Span{}, false
}

func rewriteBlockField(input *CompileInput, snapshot Snapshot, source index.SourceSubjectRecord, field, rendered string, remove bool) bool {
	path := source.Module.ModulePath
	data := input.ProjectSourceTree[path]
	start, end := source.DeclarationRange.StartByte, source.DeclarationRange.EndByte
	if start < 0 || end > len(data) {
		return false
	}
	declIndent := lineIndent(data, start)
	fieldIndent := declIndent + "  "
	lines := lineSpans(data, start, end)
	for _, line := range lines {
		raw := string(data[line[0]:line[1]])
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(raw, fieldIndent) && (trimmed == field || strings.HasPrefix(trimmed, field+" ") || strings.HasPrefix(trimmed, field+"{")) {
			s, e := line[0], line[1]
			if e < len(data) && data[e] == '\n' {
				e++
			}
			if remove {
				replaceSourceRange(input, path, s, e, nil)
			} else {
				replaceSourceRange(input, path, s, line[1], []byte(fieldIndent+rendered))
			}
			return true
		}
	}
	if remove {
		return true
	}
	close := bytes.LastIndexByte(data[start:end], '}')
	if close < 0 {
		replaceSourceRange(input, path, end, end, []byte(" {\n"+fieldIndent+rendered+"\n"+declIndent+"}"))
		return true
	}
	close += start
	replaceSourceRange(input, path, close, close, []byte("\n"+fieldIndent+rendered))
	return true
}

func lineSpans(data []byte, start, end int) [][2]int {
	var out [][2]int
	p := start
	for p < end {
		q := bytes.IndexByte(data[p:end], '\n')
		if q < 0 {
			out = append(out, [2]int{p, end})
			break
		}
		q += p
		out = append(out, [2]int{p, q})
		p = q + 1
	}
	return out
}
func lineIndent(data []byte, pos int) string {
	for pos > 0 && data[pos-1] != '\n' {
		pos--
	}
	end := pos
	for end < len(data) && (data[end] == ' ' || data[end] == '\t') {
		end++
	}
	return string(data[pos:end])
}
func indentMultiline(text, indent string) string { return strings.ReplaceAll(text, "\n", "\n"+indent) }

func findNamedBlock(data []byte, start, end int, name string) (int, int, bool) {
	segment := data[start:end]
	needle := []byte(name)
	at := bytes.Index(segment, needle)
	for at >= 0 {
		absolute := start + at
		brace := bytes.IndexByte(data[absolute:end], '{')
		if brace < 0 {
			return 0, 0, false
		}
		brace += absolute
		depth := 0
		for i := brace; i < end; i++ {
			switch data[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return brace, i, true
				}
			}
		}
		next := bytes.Index(segment[at+len(needle):], needle)
		if next < 0 {
			break
		}
		at += len(needle) + next
	}
	return 0, 0, false
}
