// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type disabledFeatureManifest struct {
	SchemaVersion    int               `json:"schema_version"`
	Delivery         string            `json:"delivery"`
	NormativeMatrix  string            `json:"normative_matrix"`
	DisabledFeatures []disabledFeature `json:"disabled_features"`
}

type disabledFeature struct {
	FeatureID  string `json:"feature_id"`
	Feature    string `json:"feature"`
	Status     string `json:"status"`
	ReasonCode string `json:"reason_code"`
	Reason     string `json:"reason"`
}

type conformanceFeatureManifest struct {
	SchemaVersion   int    `json:"schema_version"`
	Delivery        string `json:"delivery"`
	NormativeMatrix string `json:"normative_matrix"`
	Features        map[string]struct {
		Feature   string            `json:"feature"`
		Delivered bool              `json:"delivered"`
		Evidence  []json.RawMessage `json:"evidence"`
	} `json:"features"`
	AcceptanceSuites   json.RawMessage `json:"acceptance_suites"`
	Faults             json.RawMessage `json:"faults"`
	ReleaseEvidence    json.RawMessage `json:"release_evidence"`
	PerformanceBudgets json.RawMessage `json:"performance_budgets"`
}

func validateDisabledFeatures(disabledPath, conformancePath string) error {
	var disabled disabledFeatureManifest
	if err := decodeArtifactStrict(disabledPath, &disabled); err != nil {
		return fmt.Errorf("invalid disabled feature manifest: %w", err)
	}
	if disabled.SchemaVersion != 1 || disabled.Delivery != "desktop" || disabled.NormativeMatrix != "docs/blueprint.md#1311-feature-x-delivery-matrix" {
		return errors.New("disabled feature manifest identity is invalid")
	}
	var conformance conformanceFeatureManifest
	if err := decodeArtifactStrict(conformancePath, &conformance); err != nil {
		return fmt.Errorf("invalid Desktop conformance manifest: %w", err)
	}
	if conformance.SchemaVersion != 1 || len(conformance.Features) == 0 {
		return errors.New("Desktop conformance feature matrix is invalid")
	}
	expected := make(map[string]string)
	for id, feature := range conformance.Features {
		if !feature.Delivered {
			expected[id] = feature.Feature
		}
	}
	if len(disabled.DisabledFeatures) != len(expected) {
		return fmt.Errorf("disabled feature closure is incomplete: manifest=%d matrix=%d", len(disabled.DisabledFeatures), len(expected))
	}
	seen := make(map[string]bool, len(disabled.DisabledFeatures))
	for _, feature := range disabled.DisabledFeatures {
		name, ok := expected[feature.FeatureID]
		if !ok || seen[feature.FeatureID] || feature.Feature != name {
			return fmt.Errorf("disabled feature %s does not match the Desktop matrix", feature.FeatureID)
		}
		if feature.Status != "disabled" || strings.TrimSpace(feature.ReasonCode) == "" || strings.TrimSpace(feature.Reason) == "" {
			return fmt.Errorf("disabled feature %s has no explicit disabled status and reason", feature.FeatureID)
		}
		seen[feature.FeatureID] = true
	}
	return nil
}

func decodeArtifactStrict(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("artifact is not a regular file")
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("artifact contains trailing JSON")
	}
	return nil
}
