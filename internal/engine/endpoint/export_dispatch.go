// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"encoding/json"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

func runPlanExport(payload engineprotocol.PlanExportInput) func(context.Context, workbenchDriver, map[string][]byte) (any, []OutputBlob, error) {
	return func(ctx context.Context, driver workbenchDriver, _ map[string][]byte) (any, []OutputBlob, error) {
		input, err := mapPlanExportInput(payload)
		if err != nil {
			return nil, nil, &engine.WorkbenchError{Code: "export.invocation_invalid", Category: engine.WorkbenchErrorInputInvalid}
		}
		plan, err := driver.PlanExport(ctx, input)
		if err != nil {
			return nil, nil, err
		}
		encoded, err := json.Marshal(plan)
		if err != nil {
			return nil, nil, &engine.WorkbenchError{Code: "export.plan_invariant", Category: engine.WorkbenchErrorInvariant}
		}
		mapped, err := semantic.DecodeExportPlan(encoded)
		if err != nil {
			return nil, nil, &engine.WorkbenchError{Code: "export.plan_invariant", Category: engine.WorkbenchErrorInvariant}
		}
		return engineprotocol.PlanExportResult{ExportPlan: mapped}, nil, nil
	}
}

func mapPlanExportInput(payload engineprotocol.PlanExportInput) (engine.ExportPlanInput, error) {
	viewJSON, err := semantic.EncodeViewData(payload.ViewData)
	if err != nil {
		return engine.ExportPlanInput{}, err
	}
	recipeJSON, err := semantic.EncodeExportRecipe(payload.Recipe)
	if err != nil {
		return engine.ExportPlanInput{}, err
	}
	serializerOptionsJSON, err := semantic.EncodeExportOptions(payload.Recipe.Options)
	if err != nil {
		return engine.ExportPlanInput{}, err
	}
	requirementsJSON, err := semantic.EncodeResolvedExportProfileRequirements(payload.ResolvedRequirements)
	if err != nil {
		return engine.ExportPlanInput{}, err
	}
	var source engine.ExportPlanSourceRefs
	if err := remapExportValue(payload.ViewData.Source, &source); err != nil {
		return engine.ExportPlanInput{}, err
	}
	view := engine.ExportPlanViewData{
		Kind: string(payload.ViewData.Kind), ViewAddress: string(payload.ViewData.ViewAddress), RevisionKind: payload.ViewData.Revision.Kind,
		StatePolicy: payload.ViewData.StatePolicy, Source: source, CanonicalJSON: viewJSON,
	}
	if payload.ViewData.Revision.DefinitionHash != nil {
		value := string(*payload.ViewData.Revision.DefinitionHash)
		view.DefinitionHash = &value
	}
	if err := remapExportValue(payload.ViewData.StateInput, &view.StateInput); err != nil {
		return engine.ExportPlanInput{}, err
	}
	profile := engine.ExportPlanProfileRef{
		ID: payload.Recipe.ExporterProfile.ID, Format: string(payload.Recipe.ExporterProfile.Format),
		RegistrySchemaVersion: payload.Recipe.ExporterProfile.RegistrySchemaVersion,
		RegistryDigest:        string(payload.Recipe.ExporterProfile.RegistryDigest), SpecificationDigest: string(payload.Recipe.ExporterProfile.SpecificationDigest),
	}
	recipe := engine.ExportPlanRecipe{
		Address: string(payload.Recipe.Address), ViewAddress: string(payload.Recipe.ViewAddress), Format: string(payload.Recipe.Format),
		Filename: payload.Recipe.Filename, Extension: payload.Recipe.Extension, Fidelity: string(payload.Recipe.Fidelity),
		NativeMaximumFidelity: string(payload.Recipe.NativeMaximumFidelity), EffectiveMaximumFidelity: string(payload.Recipe.EffectiveMaximumFidelity),
		FidelityBasis: payload.Recipe.FidelityBasis, ExporterProfile: profile, RequiresSourceManifest: payload.Recipe.RequiresSourceManifest,
		CanonicalJSON: recipeJSON, SerializerOptions: serializerOptionsJSON,
		Options: engine.ExportPlanOptions{Bundle: payload.Recipe.Options.Bundle, StateSummary: payload.Recipe.Options.StateSummary, Orientation: payload.Recipe.Options.Orientation, PageSize: payload.Recipe.Options.PageSize},
	}
	result := engine.ExportPlanInput{
		ViewData: view, Recipe: recipe,
		Requirements: engine.ExportPlanRequirements{
			ExporterProfile:      mapExportPlanProfile(payload.ResolvedRequirements.ExporterProfile),
			SerializerProfile:    mapExportPlanProfile(payload.ResolvedRequirements.SerializerProfile),
			RequiredAssetDigests: exportDigestStrings(payload.ResolvedRequirements.RequiredAssetDigests),
			RequiredFontDigests:  exportDigestStrings(payload.ResolvedRequirements.RequiredFontDigests), CanonicalJSON: requirementsJSON,
		},
	}
	if payload.StateSummary != nil {
		canonical, err := semantic.EncodeExternalStateSummary(*payload.StateSummary)
		if err != nil {
			return engine.ExportPlanInput{}, err
		}
		payloadJSON, err := protocolcommon.EncodeJsonValue(payload.StateSummary.Payload)
		if err != nil {
			return engine.ExportPlanInput{}, err
		}
		result.StateSummary = &engine.ExportPlanStateSummary{
			Format: string(payload.StateSummary.Format), SchemaVersion: payload.StateSummary.SchemaVersion,
			DefinitionHash: string(payload.StateSummary.DefinitionHash), StateVersion: payload.StateSummary.StateVersion, PayloadHash: string(payload.StateSummary.PayloadHash),
			PayloadCanonicalJSON: payloadJSON, CanonicalJSON: canonical,
		}
	}
	return result, nil
}

func mapExportPlanProfile(value semantic.ExporterProfileRef) engine.ExportPlanProfileRef {
	return engine.ExportPlanProfileRef{ID: value.ID, Format: string(value.Format), RegistrySchemaVersion: value.RegistrySchemaVersion, RegistryDigest: string(value.RegistryDigest), SpecificationDigest: string(value.SpecificationDigest)}
}

func exportDigestStrings(values []protocolcommon.Digest) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}

func remapExportValue(input, output any) error {
	encoded, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, output)
}
