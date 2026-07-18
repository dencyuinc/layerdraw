// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package canonicaljson

import (
	"bytes"
	"fmt"

	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

// ValidateViewData proves that encoded is the generated semantic codec's
// canonical representation of one complete ViewData value.
func ValidateViewData(encoded []byte) error {
	value, err := semantic.DecodeViewData(encoded)
	if err != nil {
		return fmt.Errorf("decode ViewData: %w", err)
	}
	canonical, err := semantic.EncodeViewData(value)
	if err != nil {
		return fmt.Errorf("encode ViewData: %w", err)
	}
	return requireEqual(encoded, canonical, "ViewData")
}

// ValidateExportRecipe proves that encoded is the generated semantic codec's
// canonical representation of one complete ExportRecipe value.
func ValidateExportRecipe(encoded []byte) error {
	value, err := semantic.DecodeExportRecipe(encoded)
	if err != nil {
		return fmt.Errorf("decode ExportRecipe: %w", err)
	}
	canonical, err := semantic.EncodeExportRecipe(value)
	if err != nil {
		return fmt.Errorf("encode ExportRecipe: %w", err)
	}
	return requireEqual(encoded, canonical, "ExportRecipe")
}

// ValidateResolvedExportProfileRequirements proves that encoded is the
// generated semantic codec's canonical representation of one complete
// ResolvedExportProfileRequirements value.
func ValidateResolvedExportProfileRequirements(encoded []byte) error {
	value, err := semantic.DecodeResolvedExportProfileRequirements(encoded)
	if err != nil {
		return fmt.Errorf("decode ResolvedExportProfileRequirements: %w", err)
	}
	canonical, err := semantic.EncodeResolvedExportProfileRequirements(value)
	if err != nil {
		return fmt.Errorf("encode ResolvedExportProfileRequirements: %w", err)
	}
	return requireEqual(encoded, canonical, "ResolvedExportProfileRequirements")
}

// ValidateExternalStateSummary proves that encoded is the generated semantic
// codec's canonical representation of one complete ExternalStateSummary value.
func ValidateExternalStateSummary(encoded []byte) error {
	value, err := semantic.DecodeExternalStateSummary(encoded)
	if err != nil {
		return fmt.Errorf("decode ExternalStateSummary: %w", err)
	}
	canonical, err := semantic.EncodeExternalStateSummary(value)
	if err != nil {
		return fmt.Errorf("encode ExternalStateSummary: %w", err)
	}
	return requireEqual(encoded, canonical, "ExternalStateSummary")
}

func requireEqual(encoded, canonical []byte, name string) error {
	if !bytes.Equal(encoded, canonical) {
		return fmt.Errorf("%s bytes are not the generated canonical encoding", name)
	}
	return nil
}
