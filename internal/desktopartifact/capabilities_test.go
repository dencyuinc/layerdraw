// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopartifact

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

func TestPackagedCapabilityDeclarationMatchesProductionHandshake(t *testing.T) {
	file, err := os.Open("../../deploy/desktop-capabilities.json")
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := DecodeCapabilityDeclaration(file)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("decode=%v close=%v", err, closeErr)
	}
	if err := ValidateProbeParity(declaration, declaration.Manifest, declaration.CapabilityStatuses); err != nil {
		t.Fatal(err)
	}

	changed := append([]protocolcommon.RequestedCapabilityStatus(nil), declaration.CapabilityStatuses...)
	changed[0].Enabled = false
	if err := ValidateProbeParity(declaration, declaration.Manifest, changed); err == nil {
		t.Fatal("capability drift was accepted")
	}
	manifest := declaration.Manifest
	manifest.Components = append([]desktopcontract.ComponentID(nil), manifest.Components...)
	manifest.Components[0], manifest.Components[1] = manifest.Components[1], manifest.Components[0]
	if err := ValidateProbeParity(declaration, manifest, declaration.CapabilityStatuses); err == nil {
		t.Fatal("manifest order drift was accepted")
	}
}

func TestCapabilityDeclarationRejectsMalformedAndUnsafeInputs(t *testing.T) {
	file, err := os.ReadFile("../../deploy/desktop-capabilities.json")
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"unknown field":   bytes.Replace(file, []byte(`"schema_version": 2`), []byte(`"schema_version": 2, "unknown": true`), 1),
		"trailing data":   append(append([]byte(nil), file...), []byte("\n{}")...),
		"wrong schema":    bytes.Replace(file, []byte(`"schema_version": 2`), []byte(`"schema_version": 1`), 1),
		"credential flag": bytes.Replace(file, []byte(`"provider_credentials": false`), []byte(`"provider_credentials": true`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeCapabilityDeclaration(bytes.NewReader(data)); err == nil {
				t.Fatal("invalid capability declaration was accepted")
			}
		})
	}
}

func TestCapabilityStatusClosureIsExact(t *testing.T) {
	file, err := os.Open("../../deploy/desktop-capabilities.json")
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := DecodeCapabilityDeclaration(file)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	cases := []CapabilityDeclaration{
		func() CapabilityDeclaration {
			value := declaration
			value.CapabilityStatuses = append([]protocolcommon.RequestedCapabilityStatus(nil), value.CapabilityStatuses[:len(value.CapabilityStatuses)-1]...)
			return value
		}(),
		func() CapabilityDeclaration {
			value := declaration
			value.CapabilityStatuses = append([]protocolcommon.RequestedCapabilityStatus(nil), value.CapabilityStatuses...)
			value.CapabilityStatuses[0].ProtocolVersion = "2.0"
			return value
		}(),
		func() CapabilityDeclaration {
			value := declaration
			value.CapabilityStatuses = append([]protocolcommon.RequestedCapabilityStatus(nil), value.CapabilityStatuses...)
			value.CapabilityStatuses[0].Enabled = false
			reason := protocolcommon.UnavailableReasonNotConfigured
			value.CapabilityStatuses[0].UnavailableReason = &reason
			return value
		}(),
		func() CapabilityDeclaration {
			value := declaration
			value.Excludes = []string{"source-maps"}
			return value
		}(),
	}
	for index, value := range cases {
		if reflect.DeepEqual(value, declaration) || value.Validate() == nil {
			t.Fatalf("invalid declaration %d was accepted", index)
		}
	}
}
