// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"slices"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
)

func FuzzProtocolRangeGrammar(f *testing.F) {
	for _, seed := range []string{
		"1.0..1.0",
		"0.0..0.0",
		"4294967295.0..4294967295.4294967295",
		"1.2..1.1",
		"01.0..01.1",
		"1.0.0",
		"",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		parsed, ok := parseProtocolRange(input)
		if !ok {
			return
		}
		if parsed.lower.major != parsed.upper.major || parsed.lower.compare(parsed.upper) > 0 {
			t.Fatalf("accepted invalid range %q: %+v", input, parsed)
		}
		lower, lowerOK := parseProtocolNumber(protocolNumberString(parsed.lower))
		upper, upperOK := parseProtocolNumber(protocolNumberString(parsed.upper))
		if !lowerOK || !upperOK || lower != parsed.lower || upper != parsed.upper {
			t.Fatalf("accepted range is not canonically round-trippable: %q", input)
		}
		if input != protocolNumberString(parsed.lower)+".."+protocolNumberString(parsed.upper) {
			t.Fatalf("accepted non-canonical range %q", input)
		}
	})
}

func FuzzProtocolSelection(f *testing.F) {
	f.Add("1.0..1.2", "1.2", "sha256:2222222222222222222222222222222222222222222222222222222222222222")
	f.Add("2.0..2.1", "2.0", testWrongDigest)
	f.Add("malformed", "1.0", testWrongDigest)
	f.Fuzz(func(t *testing.T, rangeText, versionText, digestText string) {
		if len(rangeText) > 128 || len(versionText) > 64 || len(digestText) > 128 {
			t.Skip()
		}
		catalog := []protocolBinding{
			{version: protocolNumber{major: 1, minor: 0}, wireVersion: "1.0", schemaDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			{version: protocolNumber{major: 1, minor: 1}, wireVersion: "1.1", schemaDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111"},
			{version: protocolNumber{major: 1, minor: 2}, wireVersion: "1.2", schemaDigest: "sha256:2222222222222222222222222222222222222222222222222222222222222222"},
		}
		offer := protocolcommon.ProtocolOffer{
			Name:           ProtocolName,
			SupportedRange: protocolcommon.ProtocolVersionRange(rangeText),
			Versions: []protocolcommon.ProtocolVersionBinding{{
				Version:      protocolcommon.ProtocolVersion(versionText),
				SchemaDigest: protocolcommon.Digest(digestText),
			}},
		}
		first, firstFailure := selectProtocol(catalog, offer)
		slices.Reverse(catalog)
		second, secondFailure := selectProtocol(catalog, offer)
		if firstFailure != secondFailure || first != second {
			t.Fatalf("selection depends on catalog order: %+v/%v %+v/%v", first, firstFailure, second, secondFailure)
		}
		if firstFailure == selectionCompatible {
			if first.wireVersion != offer.Versions[0].Version || first.schemaDigest != offer.Versions[0].SchemaDigest {
				t.Fatalf("selected value is not an exact offered version/digest: %+v %+v", first, offer)
			}
		}
	})
}

func FuzzCapabilityNormalization(f *testing.F) {
	f.Add(OperationCompile, "engine.future")
	f.Add("engine.unknown", OperationCompile)
	f.Add(OperationCompile, OperationCompile)
	f.Fuzz(func(t *testing.T, requiredText, optionalText string) {
		if len(requiredText) > 128 || len(optionalText) > 128 {
			t.Skip()
		}
		requiredInput := []protocolcommon.CapabilityID{protocolcommon.CapabilityID(requiredText)}
		optionalInput := []protocolcommon.CapabilityID{protocolcommon.CapabilityID(optionalText)}
		required, optional, _, valid := normalizeCapabilityRequests(requiredInput, optionalInput)
		if !valid {
			return
		}
		if !slices.IsSorted(required) || !slices.IsSorted(optional) {
			t.Fatal("normalized capabilities are not sorted")
		}
		statuses := requestedCapabilityStatuses([]string{OperationCompile, OperationHandshake}, required, optional, ProtocolVersion)
		for _, status := range statuses {
			known := status.CapabilityID == OperationCompile || status.CapabilityID == OperationHandshake
			if status.Enabled != known {
				t.Fatalf("capability changed known/unknown status: %+v", status)
			}
			if known && status.UnavailableReason != nil {
				t.Fatalf("enabled capability has unavailable reason: %+v", status)
			}
			if !known && (status.UnavailableReason == nil || *status.UnavailableReason != protocolcommon.UnavailableReasonUnsupported) {
				t.Fatalf("unknown optional/required capability was not explicit: %+v", status)
			}
		}
	})
}

func protocolNumberString(number protocolNumber) string {
	return uint32String(number.major) + "." + uint32String(number.minor)
}

func uint32String(value uint32) string {
	if value == 0 {
		return "0"
	}
	buffer := [10]byte{}
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}
