// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/canonicaljson"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

func TestMeasureDocumentQueryLogicalBytesCoversCompleteResult(t *testing.T) {
	t.Parallel()
	result := completeLogicalQueryResult()
	result.ReturnedItems = queryResultItemCount(result.Result)

	measured, err := MeasureDocumentQueryLogicalBytes(context.Background(), result, 1<<20)
	if err != nil || measured <= 0 {
		t.Fatalf("MeasureDocumentQueryLogicalBytes() = (%d, %v)", measured, err)
	}
	result.ReturnedBytes = measured
	measuredAgain, err := MeasureDocumentQueryLogicalBytes(context.Background(), result, measured)
	if err != nil || measuredAgain != measured {
		t.Fatalf("exact-limit measurement = (%d, %v), want %d", measuredAgain, err, measured)
	}
	// Every canonical byte boundary must fail closed. Walking all prefixes also
	// exercises error propagation from each nested result component.
	for limit := int64(1); limit < measured; limit++ {
		if _, err := MeasureDocumentQueryLogicalBytes(context.Background(), result, limit); !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) {
			t.Fatalf("limit %d error = %v", limit, err)
		}
	}
}

func TestMeasureDocumentQueryLogicalBytesRejectsInvalidResults(t *testing.T) {
	t.Parallel()
	valid := completeLogicalQueryResult()
	valid.ReturnedItems = queryResultItemCount(valid.Result)

	wrongCount := valid
	wrongCount.ReturnedItems++
	if _, err := MeasureDocumentQueryLogicalBytes(context.Background(), wrongCount, 1<<20); !IsWorkbenchError(err, WorkbenchErrorInvariant) {
		t.Fatalf("wrong item count error = %v", err)
	}
	if _, err := MeasureDocumentQueryLogicalBytes(nil, valid, 1<<20); !IsWorkbenchError(err, WorkbenchErrorInvariant) {
		t.Fatalf("nil context error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := MeasureDocumentQueryLogicalBytes(cancelled, valid, 1<<20); !IsWorkbenchError(err, WorkbenchErrorCancelled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled measurement error = %v", err)
	}
	if _, err := MeasureDocumentQueryLogicalBytes(context.Background(), valid, 1); !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) {
		t.Fatalf("limited measurement error = %v", err)
	}

	for _, test := range []struct {
		name   string
		mutate func(*ExecuteDocumentQueryResult)
	}{
		{"malformed Unicode", func(value *ExecuteDocumentQueryResult) { value.Result.QueryAddress = string([]byte{0xff}) }},
		{"unsafe integer", func(value *ExecuteDocumentQueryResult) {
			value.Result.Arguments = map[string]TypedScalar{"integer": {Type: definition.ScalarInteger, Int: 9_007_199_254_740_992}}
		}},
		{"NaN number", func(value *ExecuteDocumentQueryResult) {
			value.Result.Arguments = map[string]TypedScalar{"number": {Type: definition.ScalarNumber, Float: math.NaN()}}
		}},
		{"infinite number", func(value *ExecuteDocumentQueryResult) {
			value.Result.Arguments = map[string]TypedScalar{"number": {Type: definition.ScalarNumber, Float: math.Inf(1)}}
		}},
		{"negative zero", func(value *ExecuteDocumentQueryResult) {
			value.Result.Arguments = map[string]TypedScalar{"number": {Type: definition.ScalarNumber, Float: math.Copysign(0, -1)}}
		}},
		{"unknown scalar", func(value *ExecuteDocumentQueryResult) {
			value.Result.Arguments = map[string]TypedScalar{"unknown": {Type: "unknown"}}
		}},
		{"negative diagnostic range", func(value *ExecuteDocumentQueryResult) {
			value.Result.Diagnostics[0].Range.StartByte = -1
		}},
		{"negative related range", func(value *ExecuteDocumentQueryResult) {
			value.Result.Diagnostics[0].Related[0].Range.EndByte = -1
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := completeLogicalQueryResult()
			test.mutate(&value)
			value.ReturnedItems = queryResultItemCount(value.Result)
			if _, err := MeasureDocumentQueryLogicalBytes(context.Background(), value, 1<<20); !IsWorkbenchError(err, WorkbenchErrorInvariant) {
				t.Fatalf("invalid result error = %v", err)
			}
		})
	}
}

func TestQueryLogicalSizerRejectsInvalidContainerAndMapsDeadline(t *testing.T) {
	t.Parallel()
	counter, err := canonicaljson.NewCounter(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := (&queryLogicalSizer{counter: counter}).container(-1); err == nil {
		t.Fatal("negative container length was accepted")
	}
	if err := queryLogicalSizeError(context.DeadlineExceeded, 100); !IsWorkbenchError(err, WorkbenchErrorCancelled) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
}

func TestQueryLogicalSizerMeasuresDynamicObjectsDeterministically(t *testing.T) {
	t.Parallel()
	values := map[string]definition.Scalar{
		"\ue000":     {Type: definition.ScalarString, String: "a relatively long value"},
		"\U00010000": {Type: definition.ScalarBoolean, Bool: true},
		"aa":         {Type: definition.ScalarInteger, Int: 42},
		"a":          {Type: definition.ScalarString, String: "short"},
	}
	var observed int64
	for attempt := 0; attempt < 100; attempt++ {
		limited, err := canonicaljson.NewCounter(context.Background(), 32)
		if err != nil {
			t.Fatal(err)
		}
		err = (&queryLogicalSizer{counter: limited}).arguments(values)
		var limitError *canonicaljson.LimitError
		if !errors.As(err, &limitError) {
			t.Fatalf("attempt %d error = %v", attempt, err)
		}
		if attempt == 0 {
			observed = limitError.Observed
			continue
		}
		if limitError.Observed != observed {
			t.Fatalf("attempt %d observed = %d, want deterministic %d", attempt, limitError.Observed, observed)
		}
	}
}

func completeLogicalQueryResult() ExecuteDocumentQueryResult {
	projectRange := &SourceRange{
		Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: "project.ldl", StartByte: 1, EndByte: 2,
	}
	packRange := &SourceRange{
		Origin: resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: "ldl:pack:layerdraw:aws"}, ModulePath: "network.ldl", StartByte: 3, EndByte: 4,
	}
	return ExecuteDocumentQueryResult{
		DocumentGeneration: DocumentGeneration{
			DocumentHandle: DocumentHandle{EndpointInstanceID: "engine-test", Value: "document_1234567890abcdef"}, Value: 7,
		},
		Result: QueryResult{
			QueryAddress: "ldl:project:p:query:q",
			Arguments: map[string]TypedScalar{
				"boolean_false": {Type: definition.ScalarBoolean},
				"boolean_true":  {Type: definition.ScalarBoolean, Bool: true},
				"date":          {Type: definition.ScalarDate, String: "2026-07-17"},
				"datetime":      {Type: definition.ScalarDatetime, String: "2026-07-17T12:00:00Z"},
				"enum":          {Type: definition.ScalarEnum, String: "prod"},
				"integer":       {Type: definition.ScalarInteger, Int: -42},
				"number":        {Type: definition.ScalarNumber, Float: 1.5},
				"string":        {Type: definition.ScalarString, String: "<quoted>\n\"\\\u2028😀"},
			},
			CycleRefs: []QueryCycleRef{{
				Kind: "cycle", FromEntityAddress: "entity:a", ToEntityAddress: "entity:b", RelationAddress: "relation:r", Orientation: "outgoing",
				RetainedPath: QueryPath{EntityAddresses: []string{"entity:a", "entity:b"}, RelationAddresses: []string{"relation:r"}},
			}},
			Diagnostics: []Diagnostic{{
				Code: "LDL1605", Severity: "warning", MessageKey: "query_warning", Message: "warning message",
				Arguments: map[string]string{"detail": "value"}, OwnerAddress: "query:q", SubjectAddress: "entity:a", Range: projectRange,
				Related: []resolve.DiagnosticRelated{{
					Relation: "caused_by", Message: "related message", OwnerAddress: "query:q", SubjectAddress: "entity:b", Range: packRange,
				}},
			}},
			InducedRelationAddresses:  []string{"relation:i"},
			PathRelationAddresses:     []string{"relation:r"},
			Paths:                     []QueryPath{{EntityAddresses: []string{"entity:a", "entity:b"}, RelationAddresses: []string{"relation:r"}}},
			PrimaryEntityAddresses:    []string{"entity:a"},
			ReachedEntityAddresses:    []string{"entity:b"},
			SeedEntityAddresses:       []string{"entity:a"},
			SelectedRelationAddresses: []string{"relation:r"},
			StateInput:                QueryStateInputRef{Kind: "none"},
			StatePolicy:               "optional",
			StateReads:                []StateReadRef{{SubjectAddress: "entity:a", FieldPath: "system.updated_at"}},
			SupportEntityAddresses:    []string{"entity:a"},
			TraversedEntityAddresses:  []string{"entity:b"},
		},
	}
}
