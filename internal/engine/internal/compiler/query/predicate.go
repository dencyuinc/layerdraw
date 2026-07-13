// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package query

import (
	"sort"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

type predicateOperand struct {
	typeInfo OperandType
	columns  []definition.Column
}

func (c *compiler) compilePredicateRoot(source resolve.DeclarationSource, member authoredMember, ownerKind resolve.SubjectKind, subject string) Predicate {
	if member.block == nil || len(member.args) != 1 {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "predicate root requires one boolean group", subject, "")
		return Predicate{Kind: PredicateAll, Children: []Predicate{}}
	}
	root := authoredMember{head: member.args[0].raw, block: member.block, span: member.span}
	return c.compilePredicateMember(source, root, ownerKind, subject)
}

func (c *compiler) compilePredicateMember(source resolve.DeclarationSource, member authoredMember, ownerKind resolve.SubjectKind, subject string) Predicate {
	switch PredicateKind(member.head) {
	case PredicateAll, PredicateAny:
		kind := PredicateKind(member.head)
		predicate := Predicate{Kind: kind, Children: []Predicate{}}
		if member.block == nil || len(member.args) != 0 {
			c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "boolean predicate group requires only a block", subject, "")
			return predicate
		}
		for _, child := range readMembers(member.block) {
			predicate.Children = append(predicate.Children, c.compilePredicateMember(source, child, ownerKind, subject))
		}
		return predicate
	case PredicateNot:
		predicate := Predicate{Kind: PredicateNot}
		children := readMembers(member.block)
		if member.block == nil || len(member.args) != 0 || len(children) != 1 {
			c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "not predicate requires exactly one child", subject, "")
			return predicate
		}
		child := c.compilePredicateMember(source, children[0], ownerKind, subject)
		predicate.Child = &child
		return predicate
	case PredicateField:
		return c.compileFieldPredicate(source, member, ownerKind, subject)
	case PredicateState:
		return c.compileStatePredicate(source, member, stateSubject(ownerKind, false), subject)
	case PredicateRows:
		return c.compileRowsPredicate(source, member, ownerKind, subject)
	default:
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "unknown predicate kind", subject, "")
		return Predicate{Kind: PredicateAll, Children: []Predicate{}}
	}
}

func (c *compiler) compileFieldPredicate(source resolve.DeclarationSource, member authoredMember, ownerKind resolve.SubjectKind, subject string) Predicate {
	predicate := Predicate{Kind: PredicateField}
	if member.block != nil || len(member.args) < 2 {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "field predicate requires field and operator", subject, "")
		return predicate
	}
	predicate.Field = member.args[0].raw
	operand, valid := fieldOperand(ownerKind, predicate.Field)
	if !valid {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[0].span, "field is invalid in this predicate context", subject, "")
		return predicate
	}
	predicate.OperandType = operand.typeInfo
	operator, valid := parseOperator(member.args[1])
	if !valid || !operatorCompatible(operator, operand.typeInfo) {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[1].span, "operator is incompatible with field type", subject, "")
		return predicate
	}
	predicate.Operator = operator
	predicate.Value = c.compilePredicateValue(source, subject, operator, operand, member.args[2:])
	return predicate
}

func (c *compiler) compileStatePredicate(source resolve.DeclarationSource, member authoredMember, stateSubjectKind StateSubjectKind, subject string) Predicate {
	predicate := Predicate{Kind: PredicateState}
	if member.block != nil || len(member.args) < 2 {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "state predicate requires field path and operator", subject, "")
		return predicate
	}
	field, valid := stateField(StateFieldPath(member.args[0].raw))
	if !valid {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[0].span, "unknown query state field path", subject, "")
		return predicate
	}
	predicate.FieldPath = field.path
	predicate.OperandType = OperandType{Kind: OperandScalar, ScalarType: field.column.ValueType}
	operator, valid := parseOperator(member.args[1])
	if !valid || !operatorCompatible(operator, predicate.OperandType) {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[1].span, "operator is incompatible with state field type", subject, "")
		return predicate
	}
	predicate.Operator = operator
	predicate.Value = c.compilePredicateValue(source, subject, operator, predicateOperand{typeInfo: predicate.OperandType, columns: []definition.Column{field.column}}, member.args[2:])
	c.stateReads = append(c.stateReads, StateReadDependency{SubjectKind: stateSubjectKind, FieldPath: field.path, ValueType: field.column.ValueType})
	return predicate
}

func (c *compiler) compileRowsPredicate(source resolve.DeclarationSource, member authoredMember, ownerKind resolve.SubjectKind, subject string) Predicate {
	predicate := Predicate{Kind: PredicateRows, TypeAddresses: []string{}}
	if member.block == nil || len(member.args) != 3 || member.args[1].raw != "types" || !member.args[2].list {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "rows predicate requires quantifier, types list, and one row predicate", subject, "")
		return predicate
	}
	switch RowQuantifier(member.args[0].raw) {
	case RowsAny, RowsAll, RowsNone:
		predicate.Quantifier = RowQuantifier(member.args[0].raw)
	default:
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[0].span, "invalid row quantifier", subject, "")
	}
	typeKind := resolve.KindEntityType
	if ownerKind == resolve.KindRelation {
		typeKind = resolve.KindRelationType
	}
	predicate.TypeAddresses = c.boundList(source, subject, typeKind, member.args[2])
	children := readMembers(member.block)
	if len(children) != 1 {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "rows predicate requires exactly one complete child predicate", subject, "")
		return predicate
	}
	row := c.compileRowPredicateMember(source, children[0], ownerKind, subject, predicate.TypeAddresses)
	predicate.Row = &row
	return predicate
}

func (c *compiler) compileRowPredicateMember(source resolve.DeclarationSource, member authoredMember, ownerKind resolve.SubjectKind, subject string, typeAddresses []string) RowPredicate {
	switch PredicateKind(member.head) {
	case PredicateAll, PredicateAny:
		predicate := RowPredicate{Kind: PredicateKind(member.head), Children: []RowPredicate{}}
		if member.block == nil || len(member.args) != 0 {
			c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "row boolean group requires only a block", subject, "")
			return predicate
		}
		for _, child := range readMembers(member.block) {
			predicate.Children = append(predicate.Children, c.compileRowPredicateMember(source, child, ownerKind, subject, typeAddresses))
		}
		return predicate
	case PredicateNot:
		predicate := RowPredicate{Kind: PredicateNot}
		children := readMembers(member.block)
		if member.block == nil || len(member.args) != 0 || len(children) != 1 {
			c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "row not predicate requires exactly one child", subject, "")
			return predicate
		}
		child := c.compileRowPredicateMember(source, children[0], ownerKind, subject, typeAddresses)
		predicate.Child = &child
		return predicate
	case "cell":
		return c.compileCellPredicate(source, member, subject, typeAddresses)
	case PredicateState:
		state := c.compileStatePredicate(source, member, stateSubject(ownerKind, true), subject)
		return RowPredicate{Kind: PredicateState, FieldPath: state.FieldPath, OperandType: state.OperandType, Operator: state.Operator, Value: state.Value}
	default:
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "unknown row predicate kind", subject, "")
		return RowPredicate{Kind: PredicateAll, Children: []RowPredicate{}}
	}
}

func (c *compiler) compileCellPredicate(source resolve.DeclarationSource, member authoredMember, subject string, typeAddresses []string) RowPredicate {
	predicate := RowPredicate{Kind: PredicateCell, ColumnAddresses: []string{}}
	if member.block != nil || len(member.args) < 2 {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "cell predicate requires column and operator", subject, "")
		return predicate
	}
	bindings := c.bindingsAt(subject, resolve.KindColumn, member.args[0].span)
	if len(bindings) == 0 {
		c.diag("LDL1301", "unknown_or_ambiguous_symbol", source, member.args[0].span, "cell predicate lacks a resolver-owned column binding", subject, "")
		return predicate
	}
	allowedOwners := stringSet(typeAddresses)
	var columns []definition.Column
	seen := map[string]bool{}
	for _, binding := range bindings {
		if !allowedOwners[binding.TargetOwnerAddress] {
			c.diag("LDL1301", "unknown_or_ambiguous_symbol", source, member.args[0].span, "column binding owner is outside the row type selector", subject, "")
			continue
		}
		column, exists := c.columns[binding.TargetAddress]
		if !exists {
			c.diag("LDL1301", "unknown_or_ambiguous_symbol", source, member.args[0].span, "column binding is absent from the typed definition", subject, "")
			continue
		}
		if seen[column.Address] {
			continue
		}
		seen[column.Address] = true
		columns = append(columns, column)
		predicate.ColumnAddresses = append(predicate.ColumnAddresses, column.Address)
	}
	c.sortAddresses(predicate.ColumnAddresses)
	if len(columns) == 0 {
		return predicate
	}
	valueType := columns[0].ValueType
	for _, column := range columns[1:] {
		if column.ValueType != valueType {
			c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[0].span, "row column is ambiguous across incompatible owner types", subject, "")
			return predicate
		}
	}
	predicate.OperandType = OperandType{Kind: OperandScalar, ScalarType: valueType}
	operator, valid := parseOperator(member.args[1])
	if !valid || !operatorCompatible(operator, predicate.OperandType) {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[1].span, "operator is incompatible with row column type", subject, "")
		return predicate
	}
	predicate.Operator = operator
	predicate.Value = c.compilePredicateValue(source, subject, operator, predicateOperand{typeInfo: predicate.OperandType, columns: columns}, member.args[2:])
	return predicate
}

func fieldOperand(ownerKind resolve.SubjectKind, field string) (predicateOperand, bool) {
	scalar := func(valueType definition.ScalarType) predicateOperand {
		return predicateOperand{typeInfo: OperandType{Kind: OperandScalar, ScalarType: valueType}, columns: []definition.Column{{ValueType: valueType}}}
	}
	address := func(kind resolve.SubjectKind) predicateOperand {
		return predicateOperand{typeInfo: OperandType{Kind: OperandAddress, AddressKind: kind}}
	}
	if ownerKind == resolve.KindEntity {
		switch field {
		case "id", "display_name", "description":
			return scalar(definition.ScalarString), true
		case "address":
			return address(resolve.KindEntity), true
		case "type":
			return address(resolve.KindEntityType), true
		case "layer":
			return address(resolve.KindLayer), true
		case "tags":
			return predicateOperand{typeInfo: OperandType{Kind: OperandStringSet, ScalarType: definition.ScalarString}}, true
		}
	}
	if ownerKind == resolve.KindRelation {
		switch field {
		case "id", "display_name", "description":
			return scalar(definition.ScalarString), true
		case "address":
			return address(resolve.KindRelation), true
		case "type":
			return address(resolve.KindRelationType), true
		case "from", "to":
			return address(resolve.KindEntity), true
		case "tags":
			return predicateOperand{typeInfo: OperandType{Kind: OperandStringSet, ScalarType: definition.ScalarString}}, true
		}
	}
	return predicateOperand{}, false
}

func parseOperator(value authoredValue) (Operator, bool) {
	operators := map[string]Operator{
		"==": OperatorEqual, "!=": OperatorNotEqual, "<": OperatorLess, "<=": OperatorLessEqual,
		">": OperatorGreater, ">=": OperatorGreaterEq, "in": OperatorIn, "not_in": OperatorNotIn,
		"contains": OperatorContains, "starts_with": OperatorStartsWith, "ends_with": OperatorEndsWith,
		"exists": OperatorExists, "missing": OperatorMissing,
	}
	operator, valid := operators[value.raw]
	return operator, valid
}

func operatorCompatible(operator Operator, operand OperandType) bool {
	switch operator {
	case OperatorEqual, OperatorNotEqual:
		return true
	case OperatorLess, OperatorLessEqual, OperatorGreater, OperatorGreaterEq:
		return operand.Kind == OperandScalar && (operand.ScalarType == definition.ScalarInteger || operand.ScalarType == definition.ScalarNumber || operand.ScalarType == definition.ScalarDate || operand.ScalarType == definition.ScalarDatetime)
	case OperatorIn, OperatorNotIn:
		return operand.Kind == OperandScalar || operand.Kind == OperandAddress
	case OperatorContains:
		return operand.Kind == OperandStringSet || operand.Kind == OperandScalar && operand.ScalarType == definition.ScalarString
	case OperatorStartsWith, OperatorEndsWith:
		return operand.Kind == OperandScalar && operand.ScalarType == definition.ScalarString
	case OperatorExists, OperatorMissing:
		return true
	default:
		return false
	}
}

func (c *compiler) compilePredicateValue(source resolve.DeclarationSource, subject string, operator Operator, operand predicateOperand, values []authoredValue) *PredicateValue {
	if operator == OperatorExists || operator == OperatorMissing {
		if len(values) != 0 {
			c.diag("LDL1601", "invalid_query_or_arguments", source, values[0].span, "exists and missing forbid a predicate value", subject, "")
		}
		return nil
	}
	if len(values) != 1 {
		span := source.Range
		if len(values) > 1 {
			span = values[1].span
		}
		c.diag("LDL1601", "invalid_query_or_arguments", source, span, "predicate operator requires exactly one value", subject, "")
		return nil
	}
	value := values[0]
	if value.parameter {
		return c.compileParameterValue(source, subject, operator, operand, value)
	}
	switch operand.typeInfo.Kind {
	case OperandAddress:
		return c.compileAddressValue(source, subject, operator, operand.typeInfo.AddressKind, value)
	case OperandStringSet:
		return c.compileStringSetValue(source, subject, operator, value)
	case OperandScalar:
		return c.compileScalarValue(source, subject, operator, operand.columns, value)
	default:
		return nil
	}
}

func (c *compiler) compileParameterValue(source resolve.DeclarationSource, subject string, operator Operator, operand predicateOperand, value authoredValue) *PredicateValue {
	if operator == OperatorIn || operator == OperatorNotIn || operand.typeInfo.Kind != OperandScalar {
		c.diag("LDL1601", "invalid_query_or_arguments", source, value.span, "scalar parameters are incompatible with this predicate operand", subject, "")
		return nil
	}
	address, ok := c.singleBindingAt(subject, resolve.KindParameter, value.span, source)
	if !ok {
		return nil
	}
	parameter, exists := c.parameters[address]
	if !exists || parameter.ValueType != operand.typeInfo.ScalarType {
		c.diag("LDL1601", "invalid_query_or_arguments", source, value.span, "query parameter type is incompatible with predicate operand", subject, "")
		return nil
	}
	return &PredicateValue{Kind: ValueParameter, ParameterAddress: address}
}

func (c *compiler) compileAddressValue(source resolve.DeclarationSource, subject string, operator Operator, kind resolve.SubjectKind, value authoredValue) *PredicateValue {
	if operator == OperatorIn || operator == OperatorNotIn {
		if !value.list {
			c.diag("LDL1601", "invalid_query_or_arguments", source, value.span, "in and not_in require a list", subject, "")
			return nil
		}
		return &PredicateValue{Kind: ValueLiteral, Addresses: c.boundList(source, subject, kind, value)}
	}
	if value.list {
		c.diag("LDL1601", "invalid_query_or_arguments", source, value.span, "address predicate requires one symbol", subject, "")
		return nil
	}
	address, ok := c.singleBindingAt(subject, kind, value.span, source)
	if !ok {
		return nil
	}
	return &PredicateValue{Kind: ValueLiteral, Address: &address}
}

func (c *compiler) compileStringSetValue(source resolve.DeclarationSource, subject string, operator Operator, value authoredValue) *PredicateValue {
	if operator == OperatorEqual || operator == OperatorNotEqual {
		if !value.list {
			c.diag("LDL1601", "invalid_query_or_arguments", source, value.span, "tag set equality requires a list", subject, "")
			return nil
		}
		seen := map[string]syntax.Span{}
		var scalars []definition.Scalar
		for _, item := range listItems(value) {
			scalar, ok := tagScalar(item)
			if !ok {
				c.diag("LDL1601", "invalid_query_or_arguments", source, item.span, "invalid tag predicate value", subject, "")
				continue
			}
			if previous, duplicate := seen[scalar.String]; duplicate {
				c.diagRelated("LDL1601", "invalid_query_or_arguments", source, item.span, "duplicate tag predicate value", subject, "", source, previous)
				continue
			}
			seen[scalar.String] = item.span
			scalars = append(scalars, scalar)
		}
		sort.SliceStable(scalars, func(i, j int) bool { return scalars[i].String < scalars[j].String })
		return &PredicateValue{Kind: ValueLiteral, Scalars: scalars}
	}
	if value.list {
		c.diag("LDL1601", "invalid_query_or_arguments", source, value.span, "tag contains requires one string", subject, "")
		return nil
	}
	scalar, ok := tagScalar(value)
	if !ok {
		c.diag("LDL1601", "invalid_query_or_arguments", source, value.span, "invalid tag predicate value", subject, "")
		return nil
	}
	return &PredicateValue{Kind: ValueLiteral, Scalar: &scalar}
}

func tagScalar(value authoredValue) (definition.Scalar, bool) {
	switch value.kind {
	case syntax.TokenIdentifier:
		return definition.Scalar{Type: definition.ScalarString, String: definition.NormalizeText(value.raw)}, true
	case syntax.TokenString:
		text, ok := authoredString(value)
		return definition.Scalar{Type: definition.ScalarString, String: text}, ok
	default:
		return definition.Scalar{}, false
	}
}

func (c *compiler) compileScalarValue(source resolve.DeclarationSource, subject string, operator Operator, columns []definition.Column, value authoredValue) *PredicateValue {
	if len(columns) == 0 {
		return nil
	}
	// String pattern operands are strings, not complete values of a formatted
	// Column. A hostname suffix such as ".example.com" must not be parsed as a
	// complete hostname. Exact equality and membership still use the complete
	// Column schema and its canonical normalization.
	if operator == OperatorContains || operator == OperatorStartsWith || operator == OperatorEndsWith {
		columns = []definition.Column{{ValueType: definition.ScalarString}}
	}
	if operator == OperatorIn || operator == OperatorNotIn {
		if !value.list {
			c.diag("LDL1601", "invalid_query_or_arguments", source, value.span, "in and not_in require a list", subject, "")
			return nil
		}
		var scalars []definition.Scalar
		seen := map[definition.Scalar]syntax.Span{}
		for _, item := range listItems(value) {
			scalar, ok := c.normalizePredicateScalar(source, subject, columns, item)
			if !ok {
				continue
			}
			if previous, duplicate := seen[scalar]; duplicate {
				c.diagRelated("LDL1601", "invalid_query_or_arguments", source, item.span, "duplicate predicate list value", subject, "", source, previous)
				continue
			}
			seen[scalar] = item.span
			scalars = append(scalars, scalar)
		}
		return &PredicateValue{Kind: ValueLiteral, Scalars: scalars}
	}
	if value.list {
		c.diag("LDL1601", "invalid_query_or_arguments", source, value.span, "scalar predicate requires one value", subject, "")
		return nil
	}
	scalar, ok := c.normalizePredicateScalar(source, subject, columns, value)
	if !ok {
		return nil
	}
	return &PredicateValue{Kind: ValueLiteral, Scalar: &scalar}
}

func (c *compiler) normalizePredicateScalar(source resolve.DeclarationSource, subject string, columns []definition.Column, value authoredValue) (definition.Scalar, bool) {
	var normalized definition.Scalar
	for index, column := range columns {
		scalar, valid := definition.NormalizeScalarLiteral(value.raw, value.kind, column)
		if !valid {
			c.diag("LDL1601", "invalid_query_or_arguments", source, value.span, "predicate literal does not satisfy its scalar schema", subject, "")
			return definition.Scalar{}, false
		}
		if index > 0 && scalar != normalized {
			c.diag("LDL1601", "invalid_query_or_arguments", source, value.span, "predicate literal normalizes incompatibly across row columns", subject, "")
			return definition.Scalar{}, false
		}
		normalized = scalar
	}
	return normalized, true
}

func stateSubject(ownerKind resolve.SubjectKind, row bool) StateSubjectKind {
	if ownerKind == resolve.KindRelation {
		if row {
			return StateSubjectRelationRow
		}
		return StateSubjectRelation
	}
	if row {
		return StateSubjectEntityRow
	}
	return StateSubjectEntity
}

type stateFieldDefinition struct {
	path   StateFieldPath
	column definition.Column
}

var stateFieldRegistry = []stateFieldDefinition{
	{StateSystemCreatedAt, definition.Column{ValueType: definition.ScalarDatetime}},
	{StateSystemUpdatedAt, definition.Column{ValueType: definition.ScalarDatetime}},
	{StateSystemCreatedByKind, enumColumn("user", "agent", "service_account", "anonymous")},
	{StateSystemCreatedByID, definition.Column{ValueType: definition.ScalarString}},
	{StateSystemCreatedByDisplayName, definition.Column{ValueType: definition.ScalarString}},
	{StateSystemUpdatedByKind, enumColumn("user", "agent", "service_account", "anonymous")},
	{StateSystemUpdatedByID, definition.Column{ValueType: definition.ScalarString}},
	{StateSystemUpdatedByDisplayName, definition.Column{ValueType: definition.ScalarString}},
	{StateSystemCreatedRevision, definition.Column{ValueType: definition.ScalarString}},
	{StateSystemUpdatedRevision, definition.Column{ValueType: definition.ScalarString}},
	{StateProvenanceSourceKind, enumColumn("manual", "import", "api", "agent", "external_system")},
	{StateProvenanceSourceLabel, definition.Column{ValueType: definition.ScalarString}},
	{StateProvenanceSourceURI, definition.Column{ValueType: definition.ScalarString}},
	{StateProvenanceSourceExternalID, definition.Column{ValueType: definition.ScalarString}},
	{StateProvenanceObservedAt, definition.Column{ValueType: definition.ScalarDatetime}},
	{StateProvenanceVerifiedAt, definition.Column{ValueType: definition.ScalarDatetime}},
	{StateProvenanceStaleAfter, definition.Column{ValueType: definition.ScalarDatetime}},
	{StateProvenanceVerifiedByKind, enumColumn("user", "agent", "service_account", "anonymous")},
	{StateProvenanceVerifiedByID, definition.Column{ValueType: definition.ScalarString}},
	{StateProvenanceVerifiedByDisplayName, definition.Column{ValueType: definition.ScalarString}},
	{StateProvenanceConfidence, boundedNumber(0, 1)},
}

func enumColumn(values ...string) definition.Column {
	return definition.Column{ValueType: definition.ScalarEnum, EnumValues: values, ReservedEnumValues: []string{}}
}

func boundedNumber(minimum, maximum float64) definition.Column {
	return definition.Column{ValueType: definition.ScalarNumber, Min: &minimum, Max: &maximum}
}

func stateField(path StateFieldPath) (stateFieldDefinition, bool) {
	for _, field := range stateFieldRegistry {
		if field.path == path {
			return field, true
		}
	}
	return stateFieldDefinition{}, false
}

func sortStateReads(reads []StateReadDependency) {
	subjectRank := map[StateSubjectKind]int{StateSubjectEntity: 0, StateSubjectRelation: 1, StateSubjectEntityRow: 2, StateSubjectRelationRow: 3}
	fieldRank := map[StateFieldPath]int{}
	for index, field := range stateFieldRegistry {
		fieldRank[field.path] = index
	}
	sort.SliceStable(reads, func(i, j int) bool {
		if subjectRank[reads[i].SubjectKind] != subjectRank[reads[j].SubjectKind] {
			return subjectRank[reads[i].SubjectKind] < subjectRank[reads[j].SubjectKind]
		}
		return fieldRank[reads[i].FieldPath] < fieldRank[reads[j].FieldPath]
	})
}

func dedupeStateReads(reads []StateReadDependency) []StateReadDependency {
	if len(reads) < 2 {
		return reads
	}
	out := reads[:0]
	for _, read := range reads {
		if len(out) != 0 && out[len(out)-1] == read {
			continue
		}
		out = append(out, read)
	}
	return out
}
