// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

func (m *viewMaterializer) readViewState(subjectAddress string, fieldPath query.StateFieldPath) optionalScalar {
	read := StateReadRef{SubjectAddress: subjectAddress, FieldPath: string(fieldPath)}
	m.directStateReads[read] = true
	if m.stateInput.Kind == "none" {
		if !m.missingStateWarn {
			m.missingStateWarn = true
			m.addWarning("LDL1605", "optional_query_state_missing_or_stale", "optional View state is unavailable; state fields evaluate as missing", m.input.Recipe.Address, "")
		}
		return optionalScalar{}
	}
	if m.validatedState == nil {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "validated View state is absent", m.input.Recipe.Address, "")
		return optionalScalar{}
	}
	if m.validatedState.inaccessible[fieldPath] {
		m.denyViewStateRead(read, "state field is inaccessible")
		return optionalScalar{}
	}
	subject, exists := m.validatedState.subjects[subjectAddress]
	if !exists {
		return optionalScalar{}
	}
	if subject.ownSubjectHash != m.validatedState.currentHashes[subjectAddress] {
		m.markStaleViewStateSubject(subjectAddress)
		return optionalScalar{}
	}
	if subject.redacted[fieldPath] {
		m.denyViewStateRead(read, "state field is redacted")
		return optionalScalar{}
	}
	value, present := subject.fields[fieldPath]
	if !present {
		return optionalScalar{}
	}
	return optionalScalar{value: value, present: true}
}

func (m *viewMaterializer) markStaleViewStateSubject(subjectAddress string) {
	if m.staleState[subjectAddress] {
		return
	}
	m.staleState[subjectAddress] = true
	if m.input.Recipe.StateRequirement == query.StateRequired {
		m.addDiag("LDL1604", "required_query_state_unavailable_or_stale", "required View state record is stale", subjectAddress, m.input.Recipe.Address)
		return
	}
	m.addWarning("LDL1605", "optional_query_state_missing_or_stale", "optional View state record is stale; its fields evaluate as missing", subjectAddress, m.input.Recipe.Address)
}

func (m *viewMaterializer) denyViewStateRead(read StateReadRef, message string) {
	if m.deniedStateReads[read] {
		return
	}
	m.deniedStateReads[read] = true
	m.addDiag("LDL1904", "query_state_field_forbidden_or_redacted", message, read.SubjectAddress, m.input.Recipe.Address)
}

func stateTableCell(value optionalScalar, source ViewDataSourceRefs, read StateReadRef) TableCell {
	source.State.Reads = canonicalStateReads(append(source.State.Reads, read))
	if !value.present {
		return absentTableCell(source)
	}
	return presentTableCell(scalarViewValue(definition.Scalar(value.value)), source)
}
