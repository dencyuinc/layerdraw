// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

func TestMaterializeDocumentViewUsesRetainedWorkbenchGeneration(t *testing.T) {
	t.Parallel()
	instance := New(BuildInfo{})
	opened := openWorkbench(t, instance, projectCompileInput(diagramViewSource()))
	if !opened.Capabilities.MaterializeView {
		t.Fatalf("materialize capability = %+v", opened.Capabilities)
	}
	queryResult, err := instance.ExecuteDocumentQuery(context.Background(), ExecuteDocumentQueryInput{
		Arguments: map[string]TypedScalar{
			"ldl:project:p:query:prod_scope:parameter:environment": {Type: definition.ScalarEnum, String: "prod"},
		},
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             generousWorkbenchLimits,
		QueryAddress:       "ldl:project:p:query:prod_scope",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := instance.MaterializeDocumentView(context.Background(), MaterializeDocumentViewInput{
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             generousWorkbenchLimits,
		QueryResult:        queryResult.Result,
		ViewAddress:        "ldl:project:p:view:topology",
	})
	if err != nil {
		t.Fatal(err)
	}
	base, valid := result.ViewData.Base()
	if result.DocumentGeneration != opened.DocumentGeneration || result.ViewData.Diagram == nil || !valid || base.Kind != ViewDataDiagram {
		t.Fatalf("materialized view = %+v", result)
	}
}

func TestMaterializeDocumentViewRejectsInvalidLookupGenerationAndQuery(t *testing.T) {
	t.Parallel()
	instance := New(BuildInfo{})
	opened := openWorkbench(t, instance, projectCompileInput(diagramViewSource()))
	base := MaterializeDocumentViewInput{
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             generousWorkbenchLimits,
		QueryResult:        QueryResult{QueryAddress: "ldl:project:p:query:prod_scope", StatePolicy: "none", StateInput: QueryStateInputRef{Kind: "none"}},
		ViewAddress:        "ldl:project:p:view:topology",
	}

	empty := base
	empty.ViewAddress = ""
	if _, err := instance.MaterializeDocumentView(context.Background(), empty); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("empty view address error = %v", err)
	}
	missing := base
	missing.ViewAddress = "ldl:project:p:view:missing"
	if _, err := instance.MaterializeDocumentView(context.Background(), missing); !IsWorkbenchError(err, WorkbenchErrorNotFound) {
		t.Fatalf("missing view error = %v", err)
	}
	stale := base
	stale.DocumentGeneration.Value++
	if _, err := instance.MaterializeDocumentView(context.Background(), stale); !IsWorkbenchError(err, WorkbenchErrorGenerationStale) {
		t.Fatalf("stale generation error = %v", err)
	}
	limited := base
	limited.Limits = WorkbenchLimits{}
	if _, err := instance.MaterializeDocumentView(context.Background(), limited); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("invalid limits error = %v", err)
	}
	mismatched := base
	mismatched.QueryResult.QueryAddress = "ldl:project:p:query:other"
	if _, err := instance.MaterializeDocumentView(context.Background(), mismatched); err == nil {
		t.Fatal("mismatched QueryResult was accepted")
	} else if rejection, ok := err.(*ViewMaterializationRejection); !ok || len(rejection.Diagnostics) == 0 || rejection.Error() != "engine.workbench.view_materialization_rejected" {
		t.Fatalf("mismatched QueryResult error = %#v", err)
	}
}
