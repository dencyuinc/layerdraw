// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"errors"
	"math"
	"strconv"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/canonicaljson"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

// MeasureDocumentQueryLogicalBytes counts the exact Engine Protocol payload
// bytes for a query result without constructing protocol DTOs or JSON bytes.
func MeasureDocumentQueryLogicalBytes(ctx context.Context, result ExecuteDocumentQueryResult, limit int64) (int64, error) {
	if result.ReturnedItems != queryResultItemCount(result.Result) {
		return 0, &WorkbenchError{Code: "engine.workbench.query_result_item_invariant", Category: WorkbenchErrorInvariant}
	}
	counter, err := canonicaljson.NewCounter(ctx, limit)
	if err != nil {
		return 0, queryLogicalSizeError(err, limit)
	}
	sizer := queryLogicalSizer{counter: counter}
	if err := sizer.executeResult(result); err != nil {
		return 0, queryLogicalSizeError(err, limit)
	}
	return counter.Size(), nil
}

func queryLogicalSizeError(err error, limit int64) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &WorkbenchError{Code: "engine.workbench.cancelled", Category: WorkbenchErrorCancelled, cause: err}
	}
	var limitError *canonicaljson.LimitError
	if errors.As(err, &limitError) {
		return workbenchLimit("max_output_bytes", limit, limitError.Observed)
	}
	return &WorkbenchError{Code: "engine.workbench.query_output_invariant", Category: WorkbenchErrorInvariant, cause: err}
}

type queryLogicalSizer struct {
	counter *canonicaljson.Counter
}

func (s *queryLogicalSizer) executeResult(value ExecuteDocumentQueryResult) error {
	if err := s.object(4); err != nil {
		return err
	}
	if err := s.field("document_generation"); err != nil {
		return err
	}
	if err := s.generation(value.DocumentGeneration); err != nil {
		return err
	}
	if err := s.field("result"); err != nil {
		return err
	}
	if err := s.result(value.Result); err != nil {
		return err
	}
	if err := s.field("returned_bytes"); err != nil {
		return err
	}
	if err := s.counter.String("0"); err != nil {
		return err
	}
	if err := s.field("returned_items"); err != nil {
		return err
	}
	return s.counter.String(strconv.FormatInt(value.ReturnedItems, 10))
}

func (s *queryLogicalSizer) generation(value DocumentGeneration) error {
	if err := s.object(2); err != nil {
		return err
	}
	if err := s.field("document_handle"); err != nil {
		return err
	}
	if err := s.object(2); err != nil {
		return err
	}
	if err := s.field("endpoint_instance_id"); err != nil {
		return err
	}
	if err := s.counter.String(value.DocumentHandle.EndpointInstanceID); err != nil {
		return err
	}
	if err := s.field("value"); err != nil {
		return err
	}
	if err := s.counter.String(value.DocumentHandle.Value); err != nil {
		return err
	}
	if err := s.field("value"); err != nil {
		return err
	}
	return s.counter.String(strconv.FormatUint(value.Value, 10))
}

func (s *queryLogicalSizer) result(value QueryResult) error {
	if err := s.object(16); err != nil {
		return err
	}
	if err := s.field("arguments"); err != nil {
		return err
	}
	if err := s.arguments(value.Arguments); err != nil {
		return err
	}
	if err := s.field("cycle_refs"); err != nil {
		return err
	}
	if err := s.cycleRefs(value.CycleRefs); err != nil {
		return err
	}
	if err := s.field("diagnostics"); err != nil {
		return err
	}
	if err := s.diagnostics(value.Diagnostics); err != nil {
		return err
	}
	for _, field := range []struct {
		name   string
		values []string
	}{
		{"induced_relation_addresses", value.InducedRelationAddresses},
		{"path_relation_addresses", value.PathRelationAddresses},
	} {
		if err := s.field(field.name); err != nil {
			return err
		}
		if err := s.strings(field.values); err != nil {
			return err
		}
	}
	if err := s.field("paths"); err != nil {
		return err
	}
	if err := s.paths(value.Paths); err != nil {
		return err
	}
	for _, field := range []struct {
		name   string
		values []string
	}{
		{"primary_entity_addresses", value.PrimaryEntityAddresses},
		{"query_address", []string{value.QueryAddress}},
		{"reached_entity_addresses", value.ReachedEntityAddresses},
		{"seed_entity_addresses", value.SeedEntityAddresses},
		{"selected_relation_addresses", value.SelectedRelationAddresses},
	} {
		if err := s.field(field.name); err != nil {
			return err
		}
		if field.name == "query_address" {
			if err := s.counter.String(value.QueryAddress); err != nil {
				return err
			}
			continue
		}
		if err := s.strings(field.values); err != nil {
			return err
		}
	}
	if err := s.field("state_input"); err != nil {
		return err
	}
	stateInputFields := 1
	if value.StateInput.Kind == "snapshot" {
		stateInputFields = 5
	}
	if err := s.object(stateInputFields); err != nil {
		return err
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"kind", value.StateInput.Kind},
		{"snapshot_hash", value.StateInput.SnapshotHash},
		{"state_version", value.StateInput.StateVersion},
		{"captured_at", value.StateInput.CapturedAt},
		{"definition_hash", value.StateInput.DefinitionHash},
	} {
		if field.name != "kind" && value.StateInput.Kind != "snapshot" {
			continue
		}
		if err := s.field(field.name); err != nil {
			return err
		}
		if err := s.counter.String(field.value); err != nil {
			return err
		}
	}
	if err := s.field("state_policy"); err != nil {
		return err
	}
	if err := s.counter.String(value.StatePolicy); err != nil {
		return err
	}
	if err := s.field("state_reads"); err != nil {
		return err
	}
	if err := s.stateReads(value.StateReads); err != nil {
		return err
	}
	for _, field := range []struct {
		name   string
		values []string
	}{
		{"support_entity_addresses", value.SupportEntityAddresses},
		{"traversed_entity_addresses", value.TraversedEntityAddresses},
	} {
		if err := s.field(field.name); err != nil {
			return err
		}
		if err := s.strings(field.values); err != nil {
			return err
		}
	}
	return nil
}

func (s *queryLogicalSizer) arguments(values map[string]definition.Scalar) error {
	return s.unorderedObject(len(values), func(measured *queryLogicalSizer) error {
		for address, value := range values {
			if err := measured.field(address); err != nil {
				return err
			}
			if err := measured.scalar(value); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *queryLogicalSizer) scalar(value definition.Scalar) error {
	if err := s.object(2); err != nil {
		return err
	}
	if err := s.field("kind"); err != nil {
		return err
	}
	if err := s.counter.String(string(value.Type)); err != nil {
		return err
	}
	switch value.Type {
	case definition.ScalarString, definition.ScalarEnum, definition.ScalarDate, definition.ScalarDatetime:
		if err := s.field("string_value"); err != nil {
			return err
		}
		return s.counter.String(value.String)
	case definition.ScalarInteger:
		if value.Int < -9_007_199_254_740_991 || value.Int > 9_007_199_254_740_991 {
			return errors.New("query integer exceeds the protocol safe range")
		}
		if err := s.field("integer_value"); err != nil {
			return err
		}
		return s.counter.String(strconv.FormatInt(value.Int, 10))
	case definition.ScalarNumber:
		if math.IsNaN(value.Float) || math.IsInf(value.Float, 0) || math.Signbit(value.Float) && value.Float == 0 {
			return errors.New("invalid query finite decimal")
		}
		if err := s.field("number_value"); err != nil {
			return err
		}
		return s.counter.String(workbenchCanonicalBinary64(value.Float))
	case definition.ScalarBoolean:
		if err := s.field("boolean_value"); err != nil {
			return err
		}
		if value.Bool {
			return s.counter.Add(4)
		}
		return s.counter.Add(5)
	default:
		return errors.New("unsupported query scalar")
	}
}

func (s *queryLogicalSizer) paths(values []QueryPath) error {
	if err := s.array(len(values)); err != nil {
		return err
	}
	for _, value := range values {
		if err := s.path(value); err != nil {
			return err
		}
	}
	return nil
}

func (s *queryLogicalSizer) path(value QueryPath) error {
	if err := s.object(2); err != nil {
		return err
	}
	if err := s.field("entity_addresses"); err != nil {
		return err
	}
	if err := s.strings(value.EntityAddresses); err != nil {
		return err
	}
	if err := s.field("relation_addresses"); err != nil {
		return err
	}
	return s.strings(value.RelationAddresses)
}

func (s *queryLogicalSizer) cycleRefs(values []QueryCycleRef) error {
	if err := s.array(len(values)); err != nil {
		return err
	}
	for _, value := range values {
		if err := s.object(6); err != nil {
			return err
		}
		for _, field := range []struct{ name, value string }{
			{"from_entity_address", value.FromEntityAddress},
			{"kind", value.Kind},
			{"orientation", value.Orientation},
			{"relation_address", value.RelationAddress},
		} {
			if err := s.field(field.name); err != nil {
				return err
			}
			if err := s.counter.String(field.value); err != nil {
				return err
			}
		}
		if err := s.field("retained_path"); err != nil {
			return err
		}
		if err := s.path(value.RetainedPath); err != nil {
			return err
		}
		if err := s.field("to_entity_address"); err != nil {
			return err
		}
		if err := s.counter.String(value.ToEntityAddress); err != nil {
			return err
		}
	}
	return nil
}

func (s *queryLogicalSizer) stateReads(values []StateReadRef) error {
	if err := s.array(len(values)); err != nil {
		return err
	}
	for _, value := range values {
		if err := s.object(2); err != nil {
			return err
		}
		if err := s.field("field_path"); err != nil {
			return err
		}
		if err := s.counter.String(value.FieldPath); err != nil {
			return err
		}
		if err := s.field("subject_address"); err != nil {
			return err
		}
		if err := s.counter.String(value.SubjectAddress); err != nil {
			return err
		}
	}
	return nil
}

func (s *queryLogicalSizer) diagnostics(values []Diagnostic) error {
	if err := s.array(len(values)); err != nil {
		return err
	}
	for _, value := range values {
		if err := s.diagnostic(value); err != nil {
			return err
		}
	}
	return nil
}

func (s *queryLogicalSizer) diagnostic(value Diagnostic) error {
	fields := 6
	if value.Message != "" {
		fields++
	}
	if value.Range != nil {
		fields++
	}
	if value.SubjectAddress != "" {
		fields++
	}
	if value.OwnerAddress != "" {
		fields++
	}
	if err := s.object(fields); err != nil {
		return err
	}
	if err := s.field("arguments"); err != nil {
		return err
	}
	if err := s.unorderedObject(len(value.Arguments), func(measured *queryLogicalSizer) error {
		for key, argument := range value.Arguments {
			if err := measured.field(key); err != nil {
				return err
			}
			if err := measured.object(2); err != nil {
				return err
			}
			if err := measured.field("kind"); err != nil {
				return err
			}
			if err := measured.counter.String("string"); err != nil {
				return err
			}
			if err := measured.field("string_value"); err != nil {
				return err
			}
			if err := measured.counter.String(argument); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	for _, field := range []struct{ name, value string }{
		{"code", value.Code},
		{"message_key", value.MessageKey},
	} {
		if err := s.field(field.name); err != nil {
			return err
		}
		if err := s.counter.String(field.value); err != nil {
			return err
		}
	}
	if value.Message != "" {
		if err := s.field("message"); err != nil {
			return err
		}
		if err := s.counter.String(value.Message); err != nil {
			return err
		}
	}
	if value.OwnerAddress != "" {
		if err := s.field("owner_address"); err != nil {
			return err
		}
		if err := s.counter.String(value.OwnerAddress); err != nil {
			return err
		}
	}
	if err := s.field("protocol_version"); err != nil {
		return err
	}
	if err := s.counter.Add(1); err != nil {
		return err
	}
	if value.Range != nil {
		if err := s.field("range"); err != nil {
			return err
		}
		if err := s.sourceRange(*value.Range); err != nil {
			return err
		}
	}
	if err := s.field("related"); err != nil {
		return err
	}
	if err := s.related(value.Related); err != nil {
		return err
	}
	if err := s.field("severity"); err != nil {
		return err
	}
	if err := s.counter.String(value.Severity); err != nil {
		return err
	}
	if value.SubjectAddress != "" {
		if err := s.field("subject_address"); err != nil {
			return err
		}
		if err := s.counter.String(value.SubjectAddress); err != nil {
			return err
		}
	}
	return nil
}

func (s *queryLogicalSizer) related(values []resolve.DiagnosticRelated) error {
	if err := s.array(len(values)); err != nil {
		return err
	}
	for _, value := range values {
		fields := 1
		if value.Message != "" {
			fields++
		}
		if value.OwnerAddress != "" {
			fields++
		}
		if value.Range != nil {
			fields++
		}
		if value.SubjectAddress != "" {
			fields++
		}
		if err := s.object(fields); err != nil {
			return err
		}
		if value.Message != "" {
			if err := s.field("message"); err != nil {
				return err
			}
			if err := s.counter.String(value.Message); err != nil {
				return err
			}
		}
		if value.OwnerAddress != "" {
			if err := s.field("owner_address"); err != nil {
				return err
			}
			if err := s.counter.String(value.OwnerAddress); err != nil {
				return err
			}
		}
		if value.Range != nil {
			if err := s.field("range"); err != nil {
				return err
			}
			if err := s.sourceRange(*value.Range); err != nil {
				return err
			}
		}
		if err := s.field("relation"); err != nil {
			return err
		}
		if err := s.counter.String(value.Relation); err != nil {
			return err
		}
		if value.SubjectAddress != "" {
			if err := s.field("subject_address"); err != nil {
				return err
			}
			if err := s.counter.String(value.SubjectAddress); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *queryLogicalSizer) sourceRange(value SourceRange) error {
	if value.StartByte < 0 || value.EndByte < 0 {
		return errors.New("negative query diagnostic source range")
	}
	if err := s.object(4); err != nil {
		return err
	}
	if err := s.field("end_byte"); err != nil {
		return err
	}
	if err := s.counter.String(strconv.Itoa(value.EndByte)); err != nil {
		return err
	}
	if err := s.field("module_path"); err != nil {
		return err
	}
	if err := s.counter.String(value.ModulePath); err != nil {
		return err
	}
	if err := s.field("origin"); err != nil {
		return err
	}
	originFields := 1
	if value.Origin.PackAddress != "" {
		originFields++
	}
	if err := s.object(originFields); err != nil {
		return err
	}
	if err := s.field("kind"); err != nil {
		return err
	}
	if err := s.counter.String(string(value.Origin.Kind)); err != nil {
		return err
	}
	if value.Origin.PackAddress != "" {
		if err := s.field("pack_address"); err != nil {
			return err
		}
		if err := s.counter.String(value.Origin.PackAddress); err != nil {
			return err
		}
	}
	if err := s.field("start_byte"); err != nil {
		return err
	}
	return s.counter.String(strconv.Itoa(value.StartByte))
}

func (s *queryLogicalSizer) strings(values []string) error {
	if err := s.array(len(values)); err != nil {
		return err
	}
	for _, value := range values {
		if err := s.counter.String(value); err != nil {
			return err
		}
	}
	return nil
}

func (s *queryLogicalSizer) field(name string) error {
	if err := s.counter.String(name); err != nil {
		return err
	}
	return s.counter.Add(1)
}

func (s *queryLogicalSizer) object(fields int) error {
	return s.container(fields)
}

func (s *queryLogicalSizer) array(items int) error {
	return s.container(items)
}

func (s *queryLogicalSizer) container(items int) error {
	if items < 0 {
		return errors.New("negative canonical JSON container length")
	}
	amount := int64(2)
	if items > 1 {
		amount += int64(items - 1)
	}
	return s.counter.Add(amount)
}

// Object member order does not affect canonical JSON byte length. Measuring
// dynamic maps as one transaction avoids sorting and makes limit observations
// deterministic without rescanning shared key prefixes.
func (s *queryLogicalSizer) unorderedObject(fields int, measure func(*queryLogicalSizer) error) error {
	return s.counter.AddMeasured(func(counter *canonicaljson.Counter) error {
		measured := &queryLogicalSizer{counter: counter}
		if err := measured.object(fields); err != nil {
			return err
		}
		return measure(measured)
	})
}
