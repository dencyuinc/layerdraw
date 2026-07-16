// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
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
	if groupStart, groupEnd, only := soleFactGroupMemberRange(snapshot, source, semantic.Kind); only {
		oldLayer, _ := object["layer_address"].(string)
		for _, binding := range snapshot.SourceMap.Bindings {
			if binding.Module.ModulePath == module && binding.TargetAddress == oldLayer && binding.Range.StartByte >= groupStart && binding.Range.EndByte <= groupEnd {
				replaceSourceRange(input, module, binding.Range.StartByte, binding.Range.EndByte, []byte(layerRef))
				return nil, nil
			}
		}
	}
	start, end := source.DeclarationRange.StartByte, source.DeclarationRange.EndByte
	for _, comment := range source.CommentRanges {
		if comment.ModulePath == module && comment.StartByte < start {
			start = comment.StartByte
		}
	}
	if attached := attachedLeadingCommentStart(original, start); attached < start {
		start = attached
	}
	item := dedentBlock(string(original[start:end]))
	_, declarationEnd, found := enclosingDeclarationSpan(snapshot, source)
	if !found {
		return &SemanticConflict{Kind: ConflictPlacementChanged, TargetAddress: operation.EntityAddress}, nil
	}
	start, end = extendWholeLine(original, start, end)
	replaceSourceRange(input, module, start, end, nil)
	insertion := declarationEnd - (end - start)
	replaceSourceRange(input, module, insertion, insertion, []byte(fmt.Sprintf("\nentities %s @%s {\n  %s\n}\n", typeRef, layerRef, indentMultiline(item, "  "))))
	return nil, nil
}

func attachedLeadingCommentStart(source []byte, declarationStart int) int {
	lineStart, _ := extendWholeLine(source, declarationStart, declarationStart)
	start := lineStart
	for start > 0 {
		previousEnd := start - 1
		previousStart := previousEnd
		for previousStart > 0 && source[previousStart-1] != '\n' {
			previousStart--
		}
		line := strings.TrimSpace(string(source[previousStart:previousEnd]))
		if !strings.HasPrefix(line, "//") {
			break
		}
		start = previousStart
	}
	return start
}

func dedentBlock(text string) string {
	lines := strings.Split(strings.Trim(text, "\r\n"), "\n")
	indent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		width := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent < 0 || width < indent {
			indent = width
		}
	}
	if indent <= 0 {
		return strings.Join(lines, "\n")
	}
	for index, line := range lines {
		if len(line) >= indent {
			lines[index] = line[indent:]
		}
	}
	return strings.Join(lines, "\n")
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
		replaceSourceRange(input, module, span.Start, span.End, []byte(quoteLDLString(operation.Value.String)))
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
		if !referenceTextRepresentable(operation.Value.String) {
			d := plannerDiagnostic("LDL1801", "unrepresentable_reference_text", "non-empty Reference text must end in LF")
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
		authoredValue := *operation.Value
		if field == "image" {
			resolved, ok := resolveAuthoredAssetValue(*input, snapshot, module, authoredValue)
			if !ok {
				return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: operation.TargetAddress, Path: operation.Path}, nil
			}
			authoredValue = resolved
		}
		if special, handled, valid := renderSpecialField(snapshot, module, field, authoredValue, 0); handled {
			if !valid {
				return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: operation.TargetAddress, Path: operation.Path}, nil
			}
			rendered = special
		} else {
			value, ok := renderSemanticValue(snapshot, module, authoredValue)
			if !ok {
				return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: operation.TargetAddress, Path: operation.Path}, nil
			}
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
	value := SemanticValue{Kind: SemanticValueAbsent}
	if operation.Action != "remove" {
		if operation.Value == nil {
			d := plannerDiagnostic("LDL1801", "invalid_authored_field_value", "row cell set requires a value")
			return nil, &d
		}
		value = *operation.Value
	}
	if rewriteRowCellValue(input, snapshot, source, operation.Path[1], value) {
		return nil, nil
	}
	return &SemanticConflict{Kind: ConflictSameFieldChanged, TargetAddress: operation.TargetAddress, Path: append([]string(nil), operation.Path...)}, nil
}

func rewriteRowCellValue(input *CompileInput, snapshot Snapshot, source index.SourceSubjectRecord, columnAddress string, value SemanticValue) bool {
	path := source.Module.ModulePath
	rendered, ok := renderSemanticValue(snapshot, path, value)
	if !ok {
		return false
	}
	row, _, ok := subjectRecord(snapshot, source.Address)
	if !ok || row.OwnerAddress == nil {
		return false
	}
	owner := normalizedSubject(snapshot, *row.OwnerAddress)
	typeAddress, _ := owner["type_address"].(string)
	column, _, ok := subjectRecord(snapshot, columnAddress)
	if !ok || column.OwnerAddress == nil || *column.OwnerAddress != typeAddress {
		return false
	}
	for _, file := range snapshot.LosslessSyntaxTree.Files {
		if file.ModulePath != path || file.Origin.Kind != source.Module.Origin.Kind {
			continue
		}
		var declaration, row, header, cells *syntax.Node
		syntax.Walk(file.Root, func(node *syntax.Node) {
			if node.Span.Start > source.DeclarationRange.StartByte || node.Span.End < source.DeclarationRange.EndByte {
				return
			}
			switch node.Kind {
			case syntax.NodeDeclaration:
				if declaration == nil || node.Span.End-node.Span.Start < declaration.Span.End-declaration.Span.Start {
					declaration = node
				}
			case syntax.NodeRowItem:
				row = node
			}
		})
		if declaration == nil || row == nil {
			continue
		}
		syntax.Walk(declaration, func(node *syntax.Node) {
			if node.Kind == syntax.NodeColumnHeader {
				header = node
			}
		})
		syntax.Walk(row, func(node *syntax.Node) {
			if node.Kind == syntax.NodeCells {
				cells = node
			}
		})
		if header == nil || cells == nil {
			continue
		}
		type headerColumn struct {
			text  string
			start int
		}
		columns := []headerColumn{}
		syntax.Walk(header, func(node *syntax.Node) {
			if node.Kind == syntax.NodeSymbolRef && node.Span.Start >= 0 && node.Span.End <= len(file.Source) {
				columns = append(columns, headerColumn{text: string(file.Source[node.Span.Start:node.Span.End]), start: node.Span.Start})
			}
		})
		sort.Slice(columns, func(i, j int) bool { return columns[i].start < columns[j].start })
		valueSpans := []syntax.Span{}
		for _, child := range cells.Children {
			switch typed := child.(type) {
			case *syntax.Node:
				if typed.Kind == syntax.NodeValue {
					valueSpans = append(valueSpans, typed.Span)
				}
			case syntax.TokenElement:
				if typed.Token.Kind == syntax.TokenUnderscore {
					valueSpans = append(valueSpans, typed.Token.Span)
				}
			}
		}
		if len(columns) != len(valueSpans) {
			return false
		}
		for index, headerColumn := range columns {
			headerID := headerColumn.text
			if separator := strings.LastIndex(headerID, "."); separator >= 0 {
				headerID = headerID[separator+1:]
			}
			if headerID == terminalID(columnAddress) {
				replaceSourceRange(input, path, valueSpans[index].Start, valueSpans[index].End, []byte(rendered))
				return true
			}
		}
	}
	return false
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
		parts = append(parts, key+": "+quoteLDLString(fmt.Sprint(annotations[key])))
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
	body, valid := renderFields(snapshot, module, fields, map[string]bool{"display_name": true}, 4)
	if !valid {
		d := plannerDiagnostic("LDL1801", "invalid_create_relation", "relation fields cannot be represented as canonical LDL")
		return nil, &d
	}
	item := fmt.Sprintf("  %s: %s -> %s", operation.ID, fromRef, toRef)
	if display != "" {
		item += " " + display
	}
	if body != "" {
		item += " {\n" + body + "  }"
	}
	declaration := fmt.Sprintf("relations %s {\n%s\n}", typeRef, item)
	if !insertPlacedDeclaration(input, snapshot, module, operation.Placement, materialize.SubjectRelation, operation.ParentAddress, operation.ID, declaration) {
		return &SemanticConflict{Kind: ConflictPlacementChanged, OwnerAddress: operation.ParentAddress, ChildKind: materialize.SubjectRelation}, nil
	}
	return nil, nil
}

func applyUpsertRow(input *CompileInput, snapshot Snapshot, operation SemanticOperation) (*SemanticConflict, *Diagnostic) {
	owner, ownerSource, ok := subjectRecord(snapshot, operation.OwnerAddress)
	if !ok || (owner.Kind != materialize.SubjectEntity && owner.Kind != materialize.SubjectRelation) {
		return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: operation.OwnerAddress}, nil
	}
	rowKind := rowKindForOwner(owner)
	address := predictedChildAddress(operation.OwnerAddress, rowKind, operation.ID)
	if _, _, ok := subjectRecord(snapshot, address); ok {
		beforeDelete := cloneSourceTree(input.ProjectSourceTree)
		if conflict, diag := applyDeleteWithReservation(input, snapshot, address, false); conflict != nil || diag != nil {
			return conflict, diag
		}
		snapshot = rebaseSnapshotSourceRanges(snapshot, beforeDelete, input.ProjectSourceTree)
		removeOverlaySubject(&snapshot, address)
	}
	return appendRowGroupPlaced(input, snapshot, ownerSource.Module.ModulePath, operation.OwnerAddress, operation.ID, operation.Values, operation.ExplicitAbsentColumnAddresses, operation.Placement)
}

func appendRowGroup(input *CompileInput, snapshot Snapshot, module, ownerAddress, rowID string, cells []SemanticRowCell, absent []string) (*SemanticConflict, *Diagnostic) {
	return appendRowGroupPlaced(input, snapshot, module, ownerAddress, rowID, cells, absent, nil)
}

func appendRowGroupPlaced(input *CompileInput, snapshot Snapshot, module, ownerAddress, rowID string, cells []SemanticRowCell, absent []string, placement *SemanticPlacementHint) (*SemanticConflict, *Diagnostic) {
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
	declaration := fmt.Sprintf("%s %s [%s] {\n  %s %s: %s\n}", prefix, typeRef, strings.Join(columnRefs, ", "), ownerRef, rowID, strings.Join(values, ", "))
	rowKind := materialize.SubjectEntityRow
	if prefix == "relation_rows" {
		rowKind = materialize.SubjectRelationRow
	}
	if !insertPlacedDeclaration(input, snapshot, module, placement, rowKind, ownerAddress, rowID, declaration) {
		return &SemanticConflict{Kind: ConflictPlacementChanged, OwnerAddress: ownerAddress, ChildKind: rowKind}, nil
	}
	return nil, nil
}

func applyCreateSubject(input *CompileInput, snapshot Snapshot, operation SemanticOperation) (*SemanticConflict, *Diagnostic) {
	module, conflict := placementModule(snapshot, operation.Placement, operation.ParentAddress, input.EntryPath)
	if conflict != nil {
		return conflict, nil
	}
	fields := semanticMap(operation.Fields)
	if image, present := fields["image"]; present {
		resolved, ok := resolveAuthoredAssetValue(*input, snapshot, module, image)
		if !ok {
			d := plannerDiagnostic("LDL1801", "invalid_create_subject", "image does not identify a declared project asset")
			return nil, &d
		}
		fields["image"] = resolved
	}
	declaration, child, ok := renderCreatedSubject(snapshot, module, operation.SubjectKind, operation.ID, fields)
	if !ok {
		d := plannerDiagnostic("LDL1801", "invalid_create_subject", "create_subject fields cannot be represented as canonical LDL")
		return nil, &d
	}
	if !child {
		if !insertPlacedDeclaration(input, snapshot, module, operation.Placement, operation.SubjectKind, operation.ParentAddress, operation.ID, declaration) {
			return &SemanticConflict{Kind: ConflictPlacementChanged, OwnerAddress: operation.ParentAddress, ChildKind: operation.SubjectKind}, nil
		}
		return nil, nil
	}
	_, ownerSource, ok := subjectRecord(snapshot, operation.ParentAddress)
	if !ok || ownerSource.Module.ModulePath != module {
		return &SemanticConflict{Kind: ConflictPlacementChanged, OwnerAddress: operation.ParentAddress}, nil
	}
	if !insertOwnerChildPlaced(input, snapshot, ownerSource, operation.SubjectKind, operation.ID, operation.Placement, declaration) {
		return &SemanticConflict{Kind: ConflictPlacementChanged, OwnerAddress: operation.ParentAddress, ChildKind: operation.SubjectKind}, nil
	}
	return nil, nil
}

func renderCreatedSubject(snapshot Snapshot, module string, kind SemanticSubjectKind, id string, fields map[string]SemanticValue) (string, bool, bool) {
	display := renderedFieldValue(snapshot, module, fields, "display_name")
	switch kind {
	case materialize.SubjectEntityType:
		body, valid := renderFields(snapshot, module, fields, map[string]bool{"display_name": true}, 2)
		return fmt.Sprintf("entity_type %s %s {\n%s}", id, display, body), false, display != "" && valid
	case materialize.SubjectRelationType:
		semantic := renderedFieldValue(snapshot, module, fields, "semantic_kind")
		body, valid := renderFields(snapshot, module, fields, map[string]bool{"display_name": true, "semantic_kind": true}, 2)
		return fmt.Sprintf("relation_type %s %s %s {\n%s}", id, display, semantic, body), false, display != "" && semantic != "" && valid
	case materialize.SubjectLayer:
		order := renderedFieldValue(snapshot, module, fields, "order")
		body, valid := renderFields(snapshot, module, fields, map[string]bool{"display_name": true, "order": true}, 4)
		item := fmt.Sprintf("  %s %s @%s", id, display, order)
		if body != "" {
			item += " {\n" + body + "  }"
		}
		return "layers {\n" + item + "\n}", false, display != "" && order != "" && valid
	case materialize.SubjectEntity:
		typeRef := renderedFieldValue(snapshot, module, fields, "type_address")
		layerRef := renderedFieldValue(snapshot, module, fields, "layer_address")
		body, valid := renderFields(snapshot, module, fields, map[string]bool{"display_name": true, "type_address": true, "layer_address": true}, 4)
		item := fmt.Sprintf("  %s %s", id, display)
		if body != "" {
			item += " {\n" + body + "  }"
		}
		return fmt.Sprintf("entities %s @%s {\n%s\n}", typeRef, layerRef, item), false, display != "" && typeRef != "" && layerRef != "" && valid
	case materialize.SubjectQuery:
		body, valid := renderFields(snapshot, module, fields, map[string]bool{"display_name": true}, 2)
		return fmt.Sprintf("query %s %s {\n%s}", id, display, body), false, display != "" && valid
	case materialize.SubjectView:
		category := renderedFieldValue(snapshot, module, fields, "category")
		body, valid := renderFields(snapshot, module, fields, map[string]bool{"display_name": true, "category": true}, 2)
		return fmt.Sprintf("view %s %s %s {\n%s}", id, display, category, body), false, display != "" && category != "" && valid
	case materialize.SubjectReference:
		text := fields["text"]
		if text.Kind != SemanticValueString || !referenceTextRepresentable(text.String) {
			return "", false, false
		}
		return strings.TrimSuffix(renderReference(id, text.String), "\n"), false, true
	case materialize.SubjectEntityTypeColumn, materialize.SubjectRelationTypeColumn:
		column, valid := renderColumn(snapshot, module, id, fields, true)
		return column, true, display != "" && valid
	case materialize.SubjectEntityTypeConstraint, materialize.SubjectRelationTypeConstraint:
		columns, ok := renderOwnerLocalAddressList(fields["column_addresses"], snapshot, module)
		return fmt.Sprintf("unique %s %s", id, columns), true, ok
	case materialize.SubjectQueryParameter:
		column, valid := renderColumn(snapshot, module, id, fields, false)
		return column, true, renderedFieldValue(snapshot, module, fields, "value_type") != "" && valid
	case materialize.SubjectViewTableColumn:
		body, valid := renderFields(snapshot, module, fields, nil, 2)
		return "column " + id + " {\n" + body + "}", true, valid
	case materialize.SubjectViewExport:
		format := renderedFieldValue(snapshot, module, fields, "format")
		filename := renderedFieldValue(snapshot, module, fields, "filename")
		body, valid := renderFields(snapshot, module, fields, map[string]bool{"format": true, "filename": true}, 2)
		return fmt.Sprintf("export %s %s %s {\n%s}", id, format, filename, body), true, format != "" && filename != "" && valid
	default:
		return "", false, false
	}
}

func renderOwnerLocalAddressList(value SemanticValue, snapshot Snapshot, module string) (string, bool) {
	if value.Kind != SemanticValueArray || len(value.Array) == 0 {
		return "", false
	}
	parts := make([]string, 0, len(value.Array))
	for _, item := range value.Array {
		if item.Kind != SemanticValueAddress {
			return "", false
		}
		reference, ok := sourceReference(snapshot, module, item.Address)
		if !ok {
			return "", false
		}
		parts = append(parts, reference)
	}
	return "[" + strings.Join(parts, ", ") + "]", true
}

func renderColumn(snapshot Snapshot, module, id string, fields map[string]SemanticValue, display bool) (string, bool) {
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
		if key == "default" {
			rendered, valid = renderRecipeScalar(snapshot, module, value)
		}
		if !valid {
			return "", false
		}
		switch key {
		case "required":
			if rendered == "true" {
				parts = append(parts, "required")
			}
		case "enum_values":
			parts[len(parts)-1] = "enum " + rendered
		case "reserved_enum_values":
			parts = append(parts, "reserve_values", rendered)
		default:
			parts = append(parts, key, rendered)
		}
	}
	return strings.Join(parts, " "), true
}

func insertOwnerChild(input *CompileInput, snapshot Snapshot, owner index.SourceSubjectRecord, kind SemanticSubjectKind, id, declaration string) bool {
	return insertOwnerChildPlaced(input, snapshot, owner, kind, id, nil, declaration)
}

func insertOwnerChildPlaced(input *CompileInput, snapshot Snapshot, owner index.SourceSubjectRecord, kind SemanticSubjectKind, id string, placement *SemanticPlacementHint, declaration string) bool {
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
	blockOpen, blockClose, hasBlock := 0, 0, false
	if blockName != "" {
		blockOpen, blockClose, hasBlock = findNamedBlock(source, start, end, blockName)
	}
	type childCandidate struct {
		address string
		source  index.SourceSubjectRecord
	}
	candidates := make([]childCandidate, 0)
	for _, semantic := range snapshot.SemanticIndex.Subjects {
		if semantic.Kind != kind || semantic.OwnerAddress == nil || *semantic.OwnerAddress != owner.Address {
			continue
		}
		_, childSource, ok := subjectRecord(snapshot, semantic.Address)
		if !ok || childSource.Module.ModulePath != path || childSource.DeclarationRange.StartByte < start || childSource.DeclarationRange.EndByte > end {
			continue
		}
		candidates = append(candidates, childCandidate{address: semantic.Address, source: childSource})
	}
	insertAt := func(at int, indent string) bool {
		replaceSourceRange(input, path, at, at, []byte(indent+indentMultiline(declaration, indent)+"\n"))
		return true
	}
	insertBefore := func(candidate index.SourceSubjectRecord) bool {
		candidateStart, _ := extendWholeLine(source, candidate.DeclarationRange.StartByte, candidate.DeclarationRange.EndByte)
		return insertAt(candidateStart, lineIndent(source, candidate.DeclarationRange.StartByte))
	}
	insertAtBlockEnd := func() bool {
		if !hasBlock {
			return false
		}
		blockIndent := lineIndent(source, blockOpen)
		childIndent := blockIndent + "  "
		lineStart := blockClose
		for lineStart > 0 && source[lineStart-1] != '\n' {
			lineStart--
		}
		if len(bytes.TrimSpace(source[lineStart:blockClose])) == 0 {
			replaceSourceRange(input, path, lineStart, lineStart, []byte(childIndent+indentMultiline(declaration, childIndent)+"\n"))
			return true
		}
		replaceSourceRange(input, path, blockClose, blockClose, []byte("\n"+childIndent+indentMultiline(declaration, childIndent)+"\n"+blockIndent))
		return true
	}
	insertAfterGroup := func() bool {
		if hasBlock {
			return insertAtBlockEnd()
		}
		if len(candidates) == 0 {
			return false
		}
		last := candidates[0].source
		for _, candidate := range candidates[1:] {
			if candidate.source.DeclarationRange.EndByte > last.DeclarationRange.EndByte {
				last = candidate.source
			}
		}
		_, candidateEnd := extendWholeLine(source, last.DeclarationRange.StartByte, last.DeclarationRange.EndByte)
		return insertAt(candidateEnd, lineIndent(source, last.DeclarationRange.StartByte))
	}
	if placement != nil && placement.GroupAnchorAddress != "" {
		position := placement.Position
		ownerAnchor := placement.GroupAnchorAddress == owner.Address
		anchorSemantic, anchor, ok := subjectRecord(snapshot, placement.GroupAnchorAddress)
		if !ok || anchor.Module.ModulePath != path || (!ownerAnchor && (anchorSemantic.Kind != kind || anchorSemantic.OwnerAddress == nil || *anchorSemantic.OwnerAddress != owner.Address)) {
			return false
		}
		if ownerAnchor && position != "end" {
			return false
		}
		if position == "before" || position == "after" {
			anchorStart, anchorEnd := extendWholeLine(source, anchor.DeclarationRange.StartByte, anchor.DeclarationRange.EndByte)
			if position == "after" {
				return insertAt(anchorEnd, lineIndent(source, anchor.DeclarationRange.StartByte))
			}
			return insertAt(anchorStart, lineIndent(source, anchor.DeclarationRange.StartByte))
		}
		if position != "end" {
			return false
		}
		// Explicit end means the end of this owner's same-kind group, not
		// merely after whichever member happened to be supplied as anchor.
		if insertAfterGroup() {
			return true
		}
	} else if placement != nil && placement.ModulePath != "" && placement.Position == "end" {
		if insertAfterGroup() {
			return true
		}
	} else if len(candidates) != 0 {
		// Standard placement is canonical for newly planned children while
		// retaining the relative byte order of every existing declaration.
		newAddress := predictedChildAddress(owner.Address, kind, id)
		sort.Slice(candidates, func(i, j int) bool {
			return compareStableAddressText(candidates[i].address, candidates[j].address) < 0
		})
		for _, candidate := range candidates {
			if compareStableAddressText(newAddress, candidate.address) < 0 {
				return insertBefore(candidate.source)
			}
		}
		return insertAfterGroup()
	}
	if blockName != "" {
		if hasBlock {
			return insertAtBlockEnd()
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

func insertPlacedDeclaration(input *CompileInput, snapshot Snapshot, module string, placement *SemanticPlacementHint, kind SemanticSubjectKind, owner, id, declaration string) bool {
	if placement == nil || placement.GroupAnchorAddress == "" {
		forceEnd := placement != nil && placement.ModulePath != "" && placement.Position == "end"
		return insertStandardDeclaration(input, snapshot, module, kind, owner, id, declaration, forceEnd)
	}
	anchorKind, anchor, ok := subjectRecord(snapshot, placement.GroupAnchorAddress)
	if !ok || anchor.Module.ModulePath != module || anchorKind.Kind != kind || ownerForSubject(snapshot, anchorKind) != owner {
		return false
	}
	data := input.ProjectSourceTree[module]
	position := placement.Position
	if position == "" {
		position = "end"
	}
	if item, grouped := groupedDeclarationItem(kind, declaration); grouped {
		start, end := extendWholeLine(data, anchor.DeclarationRange.StartByte, anchor.DeclarationRange.EndByte)
		indent := lineIndent(data, anchor.DeclarationRange.StartByte)
		item = indentMultiline(strings.TrimSpace(item), indent)
		switch position {
		case "before":
			replaceSourceRange(input, module, start, start, []byte(indent+item+"\n"))
			return true
		case "after":
			replaceSourceRange(input, module, end, end, []byte(indent+item+"\n"))
			return true
		case "end":
			_, declarationEnd, found := enclosingDeclarationSpan(snapshot, anchor)
			if !found {
				return false
			}
			close := bytes.LastIndexByte(data[:declarationEnd], '}')
			if close < 0 {
				return false
			}
			replaceSourceRange(input, module, close, close, []byte(indent+item+"\n"))
			return true
		default:
			return false
		}
	}
	declarationStart, declarationEnd, found := enclosingDeclarationSpan(snapshot, anchor)
	if !found {
		return false
	}
	declarationStart, declarationEnd = extendWholeLine(data, declarationStart, declarationEnd)
	switch position {
	case "before":
		replaceSourceRange(input, module, declarationStart, declarationStart, []byte(declaration+"\n"))
	case "after":
		replaceSourceRange(input, module, declarationEnd, declarationEnd, []byte(declaration+"\n"))
	case "end":
		groupEnd := contiguousKindGroupEnd(snapshot, module, anchor, kind, owner)
		if groupEnd < 0 {
			return false
		}
		replaceSourceRange(input, module, groupEnd, groupEnd, []byte(declaration+"\n"))
	default:
		return false
	}
	return true
}

func contiguousKindGroupEnd(snapshot Snapshot, module string, anchor index.SourceSubjectRecord, kind SemanticSubjectKind, owner string) int {
	type declaration struct {
		start, end int
		kind       SemanticSubjectKind
		owner      string
	}
	bySpan := map[[2]int]declaration{}
	anchorStart, anchorEnd, found := enclosingDeclarationSpan(snapshot, anchor)
	if !found {
		return -1
	}
	for _, semantic := range snapshot.SemanticIndex.Subjects {
		_, source, ok := subjectRecord(snapshot, semantic.Address)
		if !ok || source.Module.ModulePath != module {
			continue
		}
		start, end, ok := enclosingDeclarationSpan(snapshot, source)
		if !ok {
			continue
		}
		key := [2]int{start, end}
		if _, exists := bySpan[key]; !exists {
			bySpan[key] = declaration{start: start, end: end, kind: semantic.Kind, owner: ownerForSubject(snapshot, semantic)}
		}
	}
	declarations := make([]declaration, 0, len(bySpan))
	for _, value := range bySpan {
		declarations = append(declarations, value)
	}
	sort.Slice(declarations, func(i, j int) bool { return declarations[i].start < declarations[j].start })
	anchorIndex := -1
	for i, value := range declarations {
		if value.start == anchorStart && value.end == anchorEnd {
			anchorIndex = i
			break
		}
	}
	if anchorIndex < 0 {
		return -1
	}
	end := declarations[anchorIndex].end
	for i := anchorIndex + 1; i < len(declarations); i++ {
		if declarations[i].kind != kind || declarations[i].owner != owner {
			break
		}
		end = declarations[i].end
	}
	data := snapshot.LosslessSyntaxTree.Files
	for _, file := range data {
		if file.ModulePath == module && file.Origin.Kind == resolve.OriginProject {
			_, end = extendWholeLine(file.Source, end, end)
			break
		}
	}
	return end
}

func insertStandardDeclaration(input *CompileInput, snapshot Snapshot, module string, kind SemanticSubjectKind, owner, id, declaration string, forceEnd bool) bool {
	data, exists := input.ProjectSourceTree[module]
	if !exists {
		return false
	}
	newAddress := predictedChildAddress(owner, kind, id)
	newItem, grouped := groupedDeclarationItem(kind, declaration)
	type candidate struct {
		semantic index.SemanticSubject
		source   index.SourceSubjectRecord
		start    int
		end      int
		groupKey string
	}
	candidates := make([]candidate, 0)
	for _, semantic := range snapshot.SemanticIndex.Subjects {
		if semantic.Kind != kind || semantic.Address == newAddress {
			continue
		}
		_, source, ok := subjectRecord(snapshot, semantic.Address)
		if !ok || source.Module.Origin.Kind != resolve.OriginProject || source.Module.ModulePath != module {
			continue
		}
		start, end, ok := enclosingDeclarationSpan(snapshot, source)
		if !ok || start < 0 || end > len(data) {
			continue
		}
		candidates = append(candidates, candidate{semantic: semantic, source: source, start: start, end: end, groupKey: declarationGroupKey(data[start:end])})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return compareStableAddressText(candidates[i].semantic.Address, candidates[j].semantic.Address) < 0
	})
	if forceEnd && len(candidates) != 0 {
		selected := candidates[0]
		for _, candidate := range candidates[1:] {
			if candidate.end > selected.end {
				selected = candidate
			}
		}
		if grouped {
			indent := lineIndent(data, selected.source.DeclarationRange.StartByte)
			newItem = indentMultiline(strings.TrimSpace(newItem), indent)
			close := bytes.LastIndexByte(data[:selected.end], '}')
			if close < selected.start {
				return false
			}
			replaceSourceRange(input, module, close, close, []byte(indent+newItem+"\n"))
			return true
		}
		_, end := extendWholeLine(data, selected.start, selected.end)
		replaceSourceRange(input, module, end, end, []byte(declaration+"\n"))
		return true
	}
	if grouped {
		groupKey := declarationGroupKey([]byte(declaration))
		compatible := make([]candidate, 0, len(candidates))
		for _, item := range candidates {
			if item.groupKey == groupKey {
				compatible = append(compatible, item)
			}
		}
		if len(compatible) != 0 {
			selected := compatible[len(compatible)-1]
			before := false
			for _, item := range compatible {
				if compareStableAddressText(newAddress, item.semantic.Address) < 0 {
					selected, before = item, true
					break
				}
			}
			indent := lineIndent(data, selected.source.DeclarationRange.StartByte)
			newItem = indentMultiline(strings.TrimSpace(newItem), indent)
			if before {
				start, _ := extendWholeLine(data, selected.source.DeclarationRange.StartByte, selected.source.DeclarationRange.StartByte)
				replaceSourceRange(input, module, start, start, []byte(indent+newItem+"\n"))
				return true
			}
			close := bytes.LastIndexByte(data[:selected.end], '}')
			if close < selected.start {
				return false
			}
			replaceSourceRange(input, module, close, close, []byte(indent+newItem+"\n"))
			return true
		}
	}
	if len(candidates) != 0 {
		selected := candidates[len(candidates)-1]
		before := false
		for _, item := range candidates {
			if compareStableAddressText(newAddress, item.semantic.Address) < 0 {
				selected, before = item, true
				break
			}
		}
		start, end := extendWholeLine(data, selected.start, selected.end)
		if before {
			replaceSourceRange(input, module, start, start, []byte(declaration+"\n"))
		} else {
			replaceSourceRange(input, module, end, end, []byte(declaration+"\n"))
		}
		return true
	}
	newRank := standardPlacementRank(kind)
	var beforeStart, afterEnd = -1, -1
	for _, semantic := range snapshot.SemanticIndex.Subjects {
		rank := standardPlacementRank(semantic.Kind)
		if rank < 0 {
			continue
		}
		_, source, ok := subjectRecord(snapshot, semantic.Address)
		if !ok || source.Module.Origin.Kind != resolve.OriginProject || source.Module.ModulePath != module {
			continue
		}
		start, end, ok := enclosingDeclarationSpan(snapshot, source)
		if !ok {
			continue
		}
		start, end = extendWholeLine(data, start, end)
		if rank > newRank && (beforeStart < 0 || start < beforeStart) {
			beforeStart = start
		}
		if rank < newRank && end > afterEnd {
			afterEnd = end
		}
	}
	if beforeStart >= 0 {
		replaceSourceRange(input, module, beforeStart, beforeStart, []byte(declaration+"\n"))
		return true
	}
	if afterEnd >= 0 {
		replaceSourceRange(input, module, afterEnd, afterEnd, []byte(declaration+"\n"))
		return true
	}
	appendText(input, module, "\n"+declaration+"\n")
	return true
}

func declarationGroupKey(declaration []byte) string {
	open := bytes.IndexByte(declaration, '{')
	if open < 0 {
		return ""
	}
	return strings.Join(strings.Fields(string(declaration[:open])), " ")
}

func standardPlacementRank(kind SemanticSubjectKind) int {
	switch kind {
	case materialize.SubjectProject:
		return 0
	case materialize.SubjectEntityType:
		return 1
	case materialize.SubjectRelationType:
		return 2
	case materialize.SubjectLayer:
		return 3
	case materialize.SubjectEntity:
		return 4
	case materialize.SubjectRelation:
		return 5
	case materialize.SubjectEntityRow:
		return 6
	case materialize.SubjectRelationRow:
		return 7
	case materialize.SubjectQuery:
		return 8
	case materialize.SubjectView:
		return 9
	case materialize.SubjectReference:
		return 10
	default:
		return -1
	}
}

func groupedDeclarationItem(kind SemanticSubjectKind, declaration string) (string, bool) {
	switch kind {
	case materialize.SubjectLayer, materialize.SubjectEntity, materialize.SubjectRelation, materialize.SubjectEntityRow, materialize.SubjectRelationRow:
		first, last := strings.IndexByte(declaration, '\n'), strings.LastIndex(declaration, "\n}")
		if first < 0 || last <= first {
			return "", false
		}
		return declaration[first+1 : last], true
	default:
		return "", false
	}
}

func enclosingDeclarationSpan(snapshot Snapshot, source index.SourceSubjectRecord) (int, int, bool) {
	for _, file := range snapshot.LosslessSyntaxTree.Files {
		if file.ModulePath != source.Module.ModulePath || file.Origin.Kind != source.Module.Origin.Kind {
			continue
		}
		var best *syntax.Node
		syntax.Walk(file.Root, func(node *syntax.Node) {
			if node.Kind != syntax.NodeDeclaration || node.Span.Start > source.DeclarationRange.StartByte || node.Span.End < source.DeclarationRange.EndByte {
				return
			}
			if best == nil || node.Span.End-node.Span.Start < best.Span.End-best.Span.Start {
				best = node
			}
		})
		if best != nil {
			return best.Span.Start, best.Span.End, true
		}
	}
	return 0, 0, false
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
	for referenceContainsDelimiterLine(text, delim) {
		delim += "_TEXT"
	}
	return fmt.Sprintf("reference %s <<-%s\n%s%s\n", id, delim, text, delim)
}

func referenceTextRepresentable(text string) bool { return text == "" || strings.HasSuffix(text, "\n") }

func referenceContainsDelimiterLine(text, delimiter string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSuffix(line, "\r") == delimiter {
			return true
		}
	}
	return false
}

func semanticMap(entries []SemanticMapEntry) map[string]SemanticValue {
	out := map[string]SemanticValue{}
	for _, entry := range entries {
		out[entry.Key] = entry.Value
	}
	return out
}

func renderedFieldValue(snapshot Snapshot, module string, fields map[string]SemanticValue, key string) string {
	value := fields[key]
	if value.Kind == SemanticValueString && syntacticTokenField(key) {
		return value.String
	}
	rendered, _ := renderSemanticValue(snapshot, module, value)
	return rendered
}

func syntacticTokenField(key string) bool {
	switch key {
	case "aggregate", "category", "cycle_policy", "direction", "duplicate_policy", "fidelity", "format", "group_by", "kind", "layout", "lane_by", "semantic_kind", "value_type":
		return true
	default:
		return false
	}
}

func renderFields(snapshot Snapshot, module string, fields map[string]SemanticValue, skip map[string]bool, indent int) (string, bool) {
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
			if !valid {
				return "", false
			}
			out.WriteString(prefix)
			out.WriteString(special)
			out.WriteByte('\n')
			continue
		}
		rendered, ok := renderSemanticValue(snapshot, module, value)
		if !ok {
			return "", false
		}
		out.WriteString(prefix)
		out.WriteString(renderField(key, rendered))
		out.WriteByte('\n')
	}
	return out.String(), true
}

func renderSpecialField(snapshot Snapshot, module, key string, value SemanticValue, indent int) (string, bool, bool) {
	entries := semanticMap(value.Map)
	if value.Kind == SemanticValueString && syntacticTokenField(key) {
		return key + " " + value.String, true, value.String != ""
	}
	switch key {
	case "image":
		if value.Kind == SemanticValueString {
			return "image " + quoteLDLString(value.String), true, value.String != ""
		}
		if value.Kind != SemanticValueMap {
			return "", true, false
		}
		digest, media := rawString(entries["digest"]), rawString(entries["media_type"])
		for _, asset := range snapshot.SourceMap.Assets {
			if asset.Digest == digest && asset.MediaType == media {
				return "image " + quoteLDLString(asset.AuthoredPath), true, true
			}
		}
		return "", true, false
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
			if max == `"many"` || max == "many" {
				max = "*"
			}
			lines = append(lines, prefix+name+" "+min+".."+max)
		}
		lines = append(lines, strings.Repeat(" ", indent)+"}")
		return strings.Join(lines, "\n"), true, true
	case "select":
		return renderAddressBlock(snapshot, module, "select", entries, indent, map[string]string{"layer_addresses": "layers", "entity_type_addresses": "entity_types", "relation_type_addresses": "relation_types", "root_addresses": "roots"}), true, true
	case "where", "relation_where":
		predicate, ok := renderPredicate(snapshot, module, value, indent)
		return key + " " + predicate, true, ok
	case "result":
		if value.Kind != SemanticValueArray {
			return "", true, false
		}
		parts := make([]string, 0, len(value.Array))
		for _, item := range value.Array {
			if item.Kind != SemanticValueString && item.Kind != SemanticValueToken {
				return "", true, false
			}
			parts = append(parts, item.String)
		}
		return "result [" + strings.Join(parts, ", ") + "]", true, true
	case "shape":
		if value.Kind != SemanticValueMap {
			return "", true, false
		}
		kind := rawString(entries["kind"])
		if kind == "" {
			return "", true, false
		}
		delete(entries, "kind")
		return renderViewShape(snapshot, module, kind, entries, indent)
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
			column, ownerKind, owners, valid := renderColumnBindingSet(snapshot, module, columns.Array)
			if !valid {
				return "", true, false
			}
			return "source attribute " + column + " " + ownerKind + " " + owners, true, true
		case "query":
			query, valid := renderSemanticValue(snapshot, module, entries["query_address"])
			if !valid {
				return "", true, false
			}
			arguments, valid := renderArgumentObject(snapshot, module, entries["arguments"])
			return "source query " + query + " " + arguments, true, valid
		case "diff":
			before, after := rawString(entries["before"]), rawString(entries["after"])
			arguments, valid := renderArgumentObject(snapshot, module, entries["arguments"])
			return "source diff " + quoteLDLString(before) + " -> " + quoteLDLString(after) + " " + arguments, true, valid && before != "" && after != ""
		case "relation_endpoint":
			return "source relation_endpoint " + rawString(entries["endpoint"]) + " " + rawString(entries["field"]), true, rawString(entries["endpoint"]) != "" && rawString(entries["field"]) != ""
		case "derived_count":
			result := "source derived_count " + rawString(entries["direction"])
			if relations, present := entries["relation_type_addresses"]; present {
				rendered, valid := renderSemanticValue(snapshot, module, relations)
				return result + " relations " + rendered, true, valid
			}
			return result, true, rawString(entries["direction"]) != ""
		case "state":
			path := rawString(entries["field_path"])
			return "source state " + path, true, path != ""
		}
		return "", true, false
	case "projections", "render":
		if value.Kind != SemanticValueMap {
			return "", true, false
		}
		return renderRelationPrimitiveSet(snapshot, module, key, value.Map, indent)
	case "traversal", "export":
		if value.Kind != SemanticValueMap {
			return "", true, false
		}
		return renderNestedMap(snapshot, module, key, value.Map, indent, nil)
	case "relation_projection_overrides":
		return renderViewProjectionOverrides(snapshot, module, value, indent)
	case "options":
		if value.Kind != SemanticValueMap {
			return "", true, false
		}
		return renderExportOptions(snapshot, module, value.Map, indent)
	case "exporter_profile":
		if value.Kind != SemanticValueMap {
			return "", true, false
		}
		id := rawString(entries["id"])
		return "profile " + quoteLDLString(id), true, id != ""
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
					return "", true, false
				}
				entry := semanticMap(item.Map)
				name := rawString(entry["key"])
				text := rawString(entry["value"])
				parts = append(parts, name+": "+quoteLDLString(text))
			}
			return "annotations { " + strings.Join(parts, ", ") + " }", true, true
		}
	case "forward_label":
		if value.Kind == SemanticValueString {
			return "label " + quoteLDLString(value.String), true, true
		}
	case "reverse_label":
		if value.Kind == SemanticValueString {
			return "reverse " + quoteLDLString(value.String), true, true
		}
	case "description", "display_name", "intent", "icon", "color", "filename", "label":
		if value.Kind == SemanticValueString {
			return key + " " + quoteLDLString(value.String), true, true
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

func resolveAuthoredAssetValue(input CompileInput, snapshot Snapshot, module string, value SemanticValue) (SemanticValue, bool) {
	if value.Kind != SemanticValueMap {
		return SemanticValue{}, false
	}
	entries := semanticMap(value.Map)
	digest, mediaType := rawString(entries["digest"]), rawString(entries["media_type"])
	if digest == "" || mediaType == "" {
		return SemanticValue{}, false
	}
	for _, asset := range snapshot.SourceMap.Assets {
		if asset.Digest == digest && asset.MediaType == mediaType {
			return SemanticValue{Kind: SemanticValueString, String: asset.AuthoredPath}, true
		}
	}
	for _, asset := range input.ReferencedAssets {
		if asset.Origin != SourceOriginProject || asset.Digest != digest || asset.MediaType != mediaType {
			continue
		}
		authored := portableRelativePath(path.Dir(module), asset.Locator)
		if locator, ok := resolve.ResolveAuthoredAssetLocator(module, authored); ok && locator == asset.Locator {
			return SemanticValue{Kind: SemanticValueString, String: authored}, true
		}
	}
	return SemanticValue{}, false
}

func portableRelativePath(fromDirectory, locator string) string {
	if fromDirectory == "." {
		return locator
	}
	from, to := strings.Split(fromDirectory, "/"), strings.Split(locator, "/")
	common := 0
	for common < len(from) && common < len(to) && from[common] == to[common] {
		common++
	}
	parts := make([]string, 0, len(from)-common+len(to)-common)
	for index := common; index < len(from); index++ {
		parts = append(parts, "..")
	}
	parts = append(parts, to[common:]...)
	return strings.Join(parts, "/")
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

func renderArgumentObject(snapshot Snapshot, module string, value SemanticValue) (string, bool) {
	if value.Kind == "" {
		return "{}", true
	}
	if value.Kind != SemanticValueMap {
		return "", false
	}
	parts := make([]string, 0, len(value.Map))
	for _, entry := range value.Map {
		rendered, ok := renderRecipeScalar(snapshot, module, entry.Value)
		if !ok {
			return "", false
		}
		parts = append(parts, entry.Key+": "+rendered)
	}
	return "{ " + strings.Join(parts, ", ") + " }", true
}

func renderRecipeScalar(snapshot Snapshot, module string, value SemanticValue) (string, bool) {
	if value.Kind != SemanticValueMap {
		return renderSemanticValue(snapshot, module, value)
	}
	entries := semanticMap(value.Map)
	switch rawString(entries["kind"]) {
	case "boolean":
		return renderSemanticValue(snapshot, module, entries["boolean_value"])
	case "integer":
		return renderSemanticValue(snapshot, module, entries["integer_value"])
	case "number":
		return renderSemanticValue(snapshot, module, entries["number_value"])
	case "string":
		return renderSemanticValue(snapshot, module, entries["string_value"])
	default:
		return "", false
	}
}

func renderPredicate(snapshot Snapshot, module string, value SemanticValue, indent int) (string, bool) {
	if value.Kind != SemanticValueMap {
		return "", false
	}
	entries := semanticMap(value.Map)
	kind := rawString(entries["kind"])
	switch kind {
	case "all", "any":
		children := entries["children"]
		if children.Kind != SemanticValueArray {
			return "", false
		}
		lines := []string{kind + " {"}
		for _, child := range children.Array {
			rendered, ok := renderPredicate(snapshot, module, child, indent+2)
			if !ok {
				return "", false
			}
			lines = append(lines, strings.Repeat(" ", indent+2)+indentMultiline(rendered, strings.Repeat(" ", indent+2)))
		}
		lines = append(lines, strings.Repeat(" ", indent)+"}")
		return strings.Join(lines, "\n"), true
	case "not":
		child, ok := renderPredicate(snapshot, module, entries["child"], indent+2)
		if !ok {
			return "", false
		}
		return "not {\n" + strings.Repeat(" ", indent+2) + indentMultiline(child, strings.Repeat(" ", indent+2)) + "\n" + strings.Repeat(" ", indent) + "}", true
	case "field", "state", "cell":
		field := rawString(entries["field"])
		if kind == "state" {
			field = rawString(entries["field_path"])
		}
		if kind == "cell" {
			addresses := entries["column_addresses"]
			if len(addresses.Array) == 0 {
				return "", false
			}
			var valid bool
			field, _, _, valid = renderColumnBindingSet(snapshot, module, addresses.Array)
			if !valid {
				return "", false
			}
		}
		operator := rawString(entries["operator"])
		line := kind + " " + field + " " + operator
		if predicateValue, present := entries["value"]; present {
			rendered, ok := renderPredicateValue(snapshot, module, predicateValue)
			if !ok {
				return "", false
			}
			line += " " + rendered
		}
		return line, field != "" && operator != ""
	case "rows":
		quantifier := rawString(entries["quantifier"])
		types, ok := renderSemanticValue(snapshot, module, entries["type_addresses"])
		if !ok {
			return "", false
		}
		child, ok := renderPredicate(snapshot, module, entries["predicate"], indent+2)
		if !ok {
			return "", false
		}
		return "rows " + quantifier + " types " + types + " {\n" + strings.Repeat(" ", indent+2) + indentMultiline(child, strings.Repeat(" ", indent+2)) + "\n" + strings.Repeat(" ", indent) + "}", true
	default:
		return "", false
	}
}

func renderPredicateValue(snapshot Snapshot, module string, value SemanticValue) (string, bool) {
	if value.Kind != SemanticValueMap {
		return "", false
	}
	entries := semanticMap(value.Map)
	switch rawString(entries["kind"]) {
	case "address":
		return renderSemanticValue(snapshot, module, entries["address_value"])
	case "addresses":
		return renderSemanticValue(snapshot, module, entries["address_values"])
	case "parameter":
		address := rawString(entries["parameter_address"])
		return "$" + terminalID(address), address != ""
	case "scalar":
		return renderRecipeScalar(snapshot, module, entries["scalar_value"])
	case "scalars":
		values := entries["scalar_values"]
		parts := make([]string, 0, len(values.Array))
		for _, item := range values.Array {
			rendered, ok := renderRecipeScalar(snapshot, module, item)
			if !ok {
				return "", false
			}
			parts = append(parts, rendered)
		}
		return "[" + strings.Join(parts, ", ") + "]", true
	default:
		return "", false
	}
}

func renderRelationPrimitiveSet(snapshot Snapshot, module, key string, values []SemanticMapEntry, indent int) (string, bool, bool) {
	lines := []string{}
	head := "projection"
	if key == "render" {
		head = "render"
	}
	for _, primitive := range values {
		if primitive.Value.Kind != SemanticValueMap {
			return "", true, false
		}
		rendered, _, ok := renderNestedMap(snapshot, module, head+" "+primitive.Key, primitive.Value.Map, indent, nil)
		if !ok {
			return "", true, false
		}
		lines = append(lines, rendered)
	}
	return strings.Join(lines, "\n"), true, true
}

func renderNestedMap(snapshot Snapshot, module, head string, values []SemanticMapEntry, indent int, flags map[string]bool) (string, bool, bool) {
	lines := []string{head + " {"}
	prefix := strings.Repeat(" ", indent+2)
	for _, entry := range values {
		if entry.Key == "kind" {
			continue
		}
		name := nestedFieldName(entry.Key)
		if entry.Key == "placements" && entry.Value.Kind == SemanticValueArray {
			for _, placement := range entry.Value.Array {
				parts := semanticMap(placement.Map)
				address, aok := renderSemanticValue(snapshot, module, parts["entity_address"])
				x, xok := renderSemanticValue(snapshot, module, parts["x"])
				y, yok := renderSemanticValue(snapshot, module, parts["y"])
				width, wok := renderSemanticValue(snapshot, module, parts["width"])
				height, hok := renderSemanticValue(snapshot, module, parts["height"])
				if !aok || !xok || !yok || !wok || !hok {
					return "", true, false
				}
				lines = append(lines, prefix+"place "+strings.Join([]string{address, x, y, width, height}, " "))
			}
			continue
		}
		if entry.Key == "sorts" && entry.Value.Kind == SemanticValueArray {
			for _, sortValue := range entry.Value.Array {
				parts := semanticMap(sortValue.Map)
				lines = append(lines, prefix+"sort "+rawString(parts["column_id"])+" "+rawString(parts["direction"])+" nulls "+rawString(parts["absent"]))
			}
			continue
		}
		if entry.Key == "include" && entry.Value.Kind == SemanticValueArray {
			parts := make([]string, 0, len(entry.Value.Array))
			for _, value := range entry.Value.Array {
				parts = append(parts, rawString(value))
			}
			lines = append(lines, prefix+name+" ["+strings.Join(parts, ", ")+"]")
			continue
		}
		if entry.Value.Kind == SemanticValueBoolean && flags != nil && flags[entry.Key] {
			if !entry.Value.Boolean && (entry.Key == "incoming" || entry.Key == "outgoing") {
				return "", true, false
			}
			if entry.Value.Boolean {
				lines = append(lines, prefix+name)
			}
			continue
		}
		if entry.Value.Kind == SemanticValueMap {
			nested, _, ok := renderNestedMap(snapshot, module, name, entry.Value.Map, indent+2, flags)
			if !ok {
				return "", true, false
			}
			lines = append(lines, prefix+indentMultiline(nested, prefix))
			continue
		}
		rendered, ok := renderSemanticValue(snapshot, module, entry.Value)
		if entry.Value.Kind == SemanticValueString && nestedTokenField(entry.Key) {
			rendered, ok = entry.Value.String, entry.Value.String != ""
		}
		if !ok {
			return "", true, false
		}
		lines = append(lines, prefix+name+" "+rendered)
	}
	lines = append(lines, strings.Repeat(" ", indent)+"}")
	return strings.Join(lines, "\n"), true, true
}

func nestedTokenField(key string) bool {
	switch key {
	case "abstraction", "aggregate", "arrow", "badge_endpoint", "child_endpoint", "column_endpoint", "conflict", "connector_kind", "cycle_policy", "default_direction", "direction", "display", "edge_label", "frame_label", "frame_style", "group_by", "kind", "label", "lane_by", "layout", "line", "mode", "overlay_endpoint", "parent_endpoint", "position", "row_endpoint", "row_mode", "row_source", "semantic", "shared_child_policy", "source_endpoint", "target_endpoint":
		return true
	default:
		return false
	}
}

func nestedFieldName(key string) string {
	switch key {
	case "entity_type_addresses":
		return "entity_types"
	case "relation_type_addresses":
		return "relation_types"
	case "attribute_column_addresses":
		return "attributes"
	case "label_field":
		return "label"
	case "include_entity_id":
		return "entity_id"
	case "include_entity_rows":
		return "entity_rows"
	case "include_relation_rows":
		return "relation_rows"
	case "include_layer":
		return "layer"
	case "include_type":
		return "type"
	case "row_source":
		return "rows"
	default:
		return key
	}
}

func renderViewShape(snapshot Snapshot, module, kind string, entries map[string]SemanticValue, indent int) (string, bool, bool) {
	if entries["lane_by"].String == "attribute" && len(entries["lane_column_addresses"].Array) > 0 {
		column, _, _, ok := renderColumnBindingSet(snapshot, module, entries["lane_column_addresses"].Array)
		if !ok {
			return "", true, false
		}
		entries["lane_by"] = SemanticValue{Kind: SemanticValueToken, String: "attribute." + column}
		delete(entries, "lane_column_addresses")
	}
	values := make([]SemanticMapEntry, 0, len(entries))
	for key, value := range entries {
		values = append(values, SemanticMapEntry{Key: key, Value: value})
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Key < values[j].Key })
	flags := map[string]bool{"composed": true, "detect_moves": true, "include_entity_id": true, "include_entity_rows": true, "include_layer": true, "include_relation_rows": true, "include_type": true, "incoming": true, "outgoing": true, "preserve_parallel": true}
	return renderNestedMap(snapshot, module, kind, values, indent, flags)
}

func renderColumnBindingSet(snapshot Snapshot, module string, values []SemanticValue) (column, ownerKind, owners string, ok bool) {
	addresses := make([]string, 0, len(values))
	for _, value := range values {
		if value.Kind != SemanticValueAddress {
			return "", "", "", false
		}
		addresses = append(addresses, value.Address)
	}
	sort.Slice(addresses, func(i, j int) bool { return compareStableAddressText(addresses[i], addresses[j]) < 0 })
	if len(addresses) == 0 {
		return "", "", "", false
	}
	column = terminalID(addresses[0])
	ownerAddresses := make([]SemanticValue, 0, len(addresses))
	seenOwners := map[string]bool{}
	subjects := semanticSubjectsByAddress(snapshot)
	for _, address := range addresses {
		if terminalID(address) != column {
			return "", "", "", false
		}
		subject, exists := subjects[address]
		if !exists || subject.OwnerAddress == nil {
			return "", "", "", false
		}
		kind := ""
		switch subject.Kind {
		case materialize.SubjectEntityTypeColumn:
			kind = "entity_types"
		case materialize.SubjectRelationTypeColumn:
			kind = "relation_types"
		default:
			return "", "", "", false
		}
		if ownerKind != "" && ownerKind != kind {
			return "", "", "", false
		}
		ownerKind = kind
		if !seenOwners[*subject.OwnerAddress] {
			seenOwners[*subject.OwnerAddress] = true
			ownerAddresses = append(ownerAddresses, SemanticValue{Kind: SemanticValueAddress, Address: *subject.OwnerAddress})
		}
	}
	renderedOwners, valid := renderSemanticValue(snapshot, module, SemanticValue{Kind: SemanticValueArray, Array: ownerAddresses})
	return column, ownerKind, renderedOwners, valid
}

func renderViewProjectionOverrides(snapshot Snapshot, module string, value SemanticValue, indent int) (string, bool, bool) {
	if value.Kind != SemanticValueArray {
		return "", true, false
	}
	lines := []string{}
	for _, item := range value.Array {
		entries := semanticMap(item.Map)
		address, ok := renderSemanticValue(snapshot, module, entries["key"])
		if !ok || entries["value"].Kind != SemanticValueMap {
			return "", true, false
		}
		body := []string{}
		for _, primitive := range entries["value"].Map {
			head := primitive.Key
			if primitive.Key == "render" {
				for _, renderPrimitive := range primitive.Value.Map {
					nested, _, valid := renderNestedMap(snapshot, module, "render "+renderPrimitive.Key, renderPrimitive.Value.Map, indent+2, nil)
					if !valid {
						return "", true, false
					}
					body = append(body, strings.Repeat(" ", indent+2)+indentMultiline(nested, strings.Repeat(" ", indent+2)))
				}
				continue
			}
			nested, _, valid := renderNestedMap(snapshot, module, head, primitive.Value.Map, indent+2, nil)
			if !valid {
				return "", true, false
			}
			body = append(body, strings.Repeat(" ", indent+2)+indentMultiline(nested, strings.Repeat(" ", indent+2)))
		}
		lines = append(lines, "relation_projection "+address+" {\n"+strings.Join(body, "\n")+"\n"+strings.Repeat(" ", indent)+"}")
	}
	return strings.Join(lines, "\n"), true, true
}

func renderExportOptions(snapshot Snapshot, module string, values []SemanticMapEntry, indent int) (string, bool, bool) {
	flags := map[string]bool{"bundle": true, "diagnostics": true, "embed_assets": true, "formulas": true, "header": true, "hidden_ids": true, "interactive": true, "legend": true, "lookup_sheets": true, "source_manifest": true, "state_summary": true, "view_data_json": true}
	lines := []string{}
	for _, entry := range values {
		if entry.Key == "kind" {
			continue
		}
		if entry.Value.Kind == SemanticValueBoolean && flags[entry.Key] {
			if entry.Value.Boolean {
				lines = append(lines, entry.Key)
			}
			continue
		}
		rendered, ok := renderSemanticValue(snapshot, module, entry.Value)
		if entry.Value.Kind == SemanticValueString && exportOptionTokenField(entry.Key) {
			rendered, ok = entry.Value.String, entry.Value.String != ""
		}
		if !ok {
			return "", true, false
		}
		lines = append(lines, entry.Key+" "+rendered)
	}
	return strings.Join(lines, "\n"+strings.Repeat(" ", indent)), true, true
}

func exportOptionTokenField(key string) bool {
	switch key {
	case "fit", "orientation", "page_size", "profile":
		return true
	default:
		return false
	}
}
func rawString(value SemanticValue) string {
	if value.Kind == SemanticValueString || value.Kind == SemanticValueToken {
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
		return quoteLDLString(value.String), true
	case SemanticValueToken:
		return value.String, value.String != ""
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

func quoteLDLString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
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
	case SemanticValueToken:
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
	if fieldStart, fieldEnd, found := authoredFieldNodeSpan(snapshot, source, field); found {
		if remove {
			lineStart, lineEnd := extendWholeLine(data, fieldStart, fieldEnd)
			replaceSourceRange(input, path, lineStart, lineEnd, nil)
		} else {
			replaceSourceRange(input, path, fieldStart, fieldEnd, []byte(indentMultiline(rendered, fieldIndent)))
		}
		return true
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

func authoredFieldNodeSpan(snapshot Snapshot, source index.SourceSubjectRecord, field string) (int, int, bool) {
	for _, file := range snapshot.LosslessSyntaxTree.Files {
		if file.ModulePath != source.Module.ModulePath || file.Origin.Kind != source.Module.Origin.Kind {
			continue
		}
		container := smallestSubjectSyntaxNode(file.Root, source.DeclarationRange.StartByte, source.DeclarationRange.EndByte)
		if container == nil {
			continue
		}
		for _, child := range container.Children {
			block, ok := child.(*syntax.Node)
			if !ok || block.Kind != syntax.NodeBlock {
				continue
			}
			for _, entry := range block.Children {
				node, ok := entry.(*syntax.Node)
				if ok && (node.Kind == syntax.NodeStatement || node.Kind == syntax.NodeNestedBlock) && firstNodeToken(node) == field {
					return node.Span.Start, node.Span.End, true
				}
			}
		}
	}
	return 0, 0, false
}

func smallestSubjectSyntaxNode(root *syntax.Node, start, end int) *syntax.Node {
	var best *syntax.Node
	syntax.Walk(root, func(node *syntax.Node) {
		switch node.Kind {
		case syntax.NodeDeclaration, syntax.NodeLayerItem, syntax.NodeEntityItem, syntax.NodeRelationItem:
		default:
			return
		}
		if node.Span.Start > start || node.Span.End < end {
			return
		}
		if best == nil || node.Span.End-node.Span.Start < best.Span.End-best.Span.Start {
			best = node
		}
	})
	return best
}

func firstNodeToken(node *syntax.Node) string {
	for _, child := range node.Children {
		if token, ok := child.(syntax.TokenElement); ok {
			return token.Token.Raw
		}
	}
	return ""
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
	if start < 0 || end < start || end > len(data) {
		return 0, 0, false
	}
	tokens := syntax.Lex(data[start:end]).Tokens
	bestDepth := int(^uint(0) >> 1)
	bestOpen, bestClose := 0, 0
	depth := 0
	for index, token := range tokens {
		switch token.Kind {
		case syntax.TokenLBrace:
			depth++
		case syntax.TokenRBrace:
			if depth > 0 {
				depth--
			}
		}
		if token.Kind != syntax.TokenIdentifier || token.Raw != name {
			continue
		}
		next := index + 1
		for next < len(tokens) && (tokens[next].Kind == syntax.TokenNewline || tokens[next].Kind == syntax.TokenLineComment || tokens[next].Kind == syntax.TokenDocComment) {
			next++
		}
		if next >= len(tokens) || tokens[next].Kind != syntax.TokenLBrace || depth >= bestDepth {
			continue
		}
		braceDepth := 0
		for closeIndex := next; closeIndex < len(tokens); closeIndex++ {
			switch tokens[closeIndex].Kind {
			case syntax.TokenLBrace:
				braceDepth++
			case syntax.TokenRBrace:
				braceDepth--
				if braceDepth == 0 {
					bestDepth = depth
					bestOpen = start + tokens[next].Span.Start
					bestClose = start + tokens[closeIndex].Span.Start
					closeIndex = len(tokens)
				}
			}
		}
	}
	return bestOpen, bestClose, bestDepth != int(^uint(0)>>1)
}
