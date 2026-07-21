// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package desktopartifact validates the capability declaration shipped in the
// Desktop installer against the compiled Desktop contract and probe result.
package desktopartifact

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

const SchemaVersion = 2

type SecurityDeclaration struct {
	PreconfiguredMCPEndpoints bool `json:"preconfigured_mcp_endpoints"`
	ProviderCredentials       bool `json:"provider_credentials"`
	SigningSecrets            bool `json:"signing_secrets"`
}

type CapabilityDeclaration struct {
	SchemaVersion      uint32                                     `json:"schema_version"`
	Manifest           desktopcontract.Manifest                   `json:"desktop_manifest"`
	CapabilityStatuses []protocolcommon.RequestedCapabilityStatus `json:"capability_statuses"`
	Excludes           []string                                   `json:"excludes"`
	Security           SecurityDeclaration                        `json:"security"`
}

func DecodeCapabilityDeclaration(reader io.Reader) (CapabilityDeclaration, error) {
	var value CapabilityDeclaration
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return CapabilityDeclaration{}, fmt.Errorf("invalid capability declaration: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return CapabilityDeclaration{}, errors.New("invalid capability declaration: trailing JSON content")
	}
	if err := value.Validate(); err != nil {
		return CapabilityDeclaration{}, err
	}
	return value, nil
}

func (value CapabilityDeclaration) Validate() error {
	if value.SchemaVersion != SchemaVersion {
		return errors.New("unsupported capability declaration schema")
	}
	if err := value.Manifest.Validate(); err != nil {
		return fmt.Errorf("packaged capability manifest is not exact: %w", err)
	}
	if !reflect.DeepEqual(value.Manifest, desktopcontract.DefaultManifest()) {
		return errors.New("packaged capability manifest order does not match the compiled Desktop contract")
	}
	if err := validateStatuses(value.Manifest, value.CapabilityStatuses); err != nil {
		return err
	}
	if value.Security.PreconfiguredMCPEndpoints || value.Security.ProviderCredentials || value.Security.SigningSecrets {
		return errors.New("packaged security declaration exposes runtime credentials or endpoints")
	}
	wantExcludes := []string{"development-servers", "source-maps", "test-fixtures"}
	if !reflect.DeepEqual(value.Excludes, wantExcludes) {
		return errors.New("development-only exclusion closure is not exact")
	}
	return nil
}

// ValidateProbeParity requires byte-model equality between the signed
// declaration and the manifest/status catalog emitted by the installed binary.
func ValidateProbeParity(value CapabilityDeclaration, manifest desktopcontract.Manifest, statuses []protocolcommon.RequestedCapabilityStatus) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if !reflect.DeepEqual(value.Manifest, manifest) {
		return errors.New("installed capability manifest differs from the packaged declaration")
	}
	if !reflect.DeepEqual(value.CapabilityStatuses, statuses) {
		return errors.New("installed capability statuses differ from the packaged declaration")
	}
	return nil
}

func validateStatuses(manifest desktopcontract.Manifest, statuses []protocolcommon.RequestedCapabilityStatus) error {
	want := append(append([]protocolcommon.CapabilityID(nil), manifest.RequiredCapabilities...), manifest.OptionalCapabilities...)
	required := make(map[protocolcommon.CapabilityID]bool, len(manifest.RequiredCapabilities))
	for _, id := range manifest.RequiredCapabilities {
		required[id] = true
	}
	if len(statuses) != len(want) {
		return errors.New("packaged capability status closure is incomplete")
	}
	for index, id := range want {
		status := statuses[index]
		if status.CapabilityID != id || status.ProtocolVersion != desktopcontract.DesktopProtocolVersion {
			return fmt.Errorf("packaged capability status %d does not match %q", index, id)
		}
		if status.Enabled && status.UnavailableReason != nil {
			return fmt.Errorf("enabled packaged capability %q has an unavailable reason", id)
		}
		if required[id] && !status.Enabled {
			return fmt.Errorf("required packaged capability %q is disabled", id)
		}
		if !status.Enabled && (status.UnavailableReason == nil || *status.UnavailableReason != protocolcommon.UnavailableReasonNotConfigured) {
			return fmt.Errorf("disabled packaged capability %q lacks the exact unavailable reason", id)
		}
	}
	return nil
}
