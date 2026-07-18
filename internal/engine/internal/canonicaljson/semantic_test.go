// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package canonicaljson

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

func TestGeneratedSemanticCanonicalValidators(t *testing.T) {
	fixtureJSON, err := os.ReadFile("../../../../schemas/fixtures/conformance/export-plan-transport-parity-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Input struct {
			ViewData             json.RawMessage `json:"view_data"`
			Recipe               json.RawMessage `json:"recipe"`
			ResolvedRequirements json.RawMessage `json:"resolved_requirements"`
		} `json:"input"`
	}
	if err := json.Unmarshal(fixtureJSON, &fixture); err != nil {
		t.Fatal(err)
	}
	view, err := semantic.DecodeViewData(fixture.Input.ViewData)
	if err != nil {
		t.Fatal(err)
	}
	viewJSON, err := semantic.EncodeViewData(view)
	if err != nil {
		t.Fatal(err)
	}
	recipe, err := semantic.DecodeExportRecipe(fixture.Input.Recipe)
	if err != nil {
		t.Fatal(err)
	}
	recipeJSON, err := semantic.EncodeExportRecipe(recipe)
	if err != nil {
		t.Fatal(err)
	}
	requirements, err := semantic.DecodeResolvedExportProfileRequirements(fixture.Input.ResolvedRequirements)
	if err != nil {
		t.Fatal(err)
	}
	requirementsJSON, err := semantic.EncodeResolvedExportProfileRequirements(requirements)
	if err != nil {
		t.Fatal(err)
	}
	payload := protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindBoolean, Boolean: true}
	payloadJSON, err := protocolcommon.EncodeJsonValue(payload)
	if err != nil {
		t.Fatal(err)
	}
	payloadHash := sha256.Sum256(payloadJSON)
	stateJSON, err := semantic.EncodeExternalStateSummary(semantic.ExternalStateSummary{
		DefinitionHash: protocolcommon.Digest("sha256:" + fmt.Sprintf("%064x", 1)),
		Format:         semantic.ExternalStateSummaryFormatValue,
		Payload:        payload,
		PayloadHash:    protocolcommon.Digest(fmt.Sprintf("sha256:%x", payloadHash)),
		SchemaVersion:  1,
		StateVersion:   "s1",
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		canonical []byte
		validate  func([]byte) error
	}{
		{name: "ViewData", canonical: viewJSON, validate: ValidateViewData},
		{name: "ExportRecipe", canonical: recipeJSON, validate: ValidateExportRecipe},
		{name: "ResolvedExportProfileRequirements", canonical: requirementsJSON, validate: ValidateResolvedExportProfileRequirements},
		{name: "ExternalStateSummary", canonical: stateJSON, validate: ValidateExternalStateSummary},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.validate(test.canonical); err != nil {
				t.Fatalf("canonical value rejected: %v", err)
			}
			if err := test.validate(append(test.canonical, '\n')); err == nil {
				t.Fatal("noncanonical bytes accepted")
			}
			if err := test.validate([]byte(`{}`)); err == nil {
				t.Fatal("schema-invalid value accepted")
			}
		})
	}
}
