// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
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
	f.Add("1.0..1.3", []byte{3, 3, 0, 2, 0, 1, 0, 0, 0})
	f.Add("1.0..1.3", []byte{4, 3, 1, 2, 0, 1, 0, 0, 0})
	f.Add("2.0..2.1", []byte{1, 0, 0})
	f.Add("malformed", []byte{2, 0, 0, 0, 1})
	f.Fuzz(func(t *testing.T, rangeText string, data []byte) {
		if len(rangeText) > 64 || len(data) > 128 {
			t.Skip()
		}
		catalog := fuzzProtocolCatalog()
		count := 1
		if len(data) != 0 {
			count += int(data[0] % maxProtocolVersionsPerOffer)
		}
		bindings := make([]protocolcommon.ProtocolVersionBinding, count)
		seen := map[protocolcommon.ProtocolVersion]bool{}
		duplicates := false
		for index := range bindings {
			minor, selector := byte(0), byte(0)
			if len(data) != 0 {
				minor = data[(index*2+1)%len(data)] % 5
				selector = data[(index*2+2)%len(data)]
			}
			version := protocolcommon.ProtocolVersion(protocolNumberString(protocolNumber{major: 1, minor: uint32(minor)}))
			digest := protocolcommon.Digest(testWrongDigest)
			if selector&1 == 0 && int(minor) < len(catalog) {
				digest = catalog[int(minor)].schemaDigest
			}
			if seen[version] {
				duplicates = true
			}
			seen[version] = true
			bindings[index] = protocolcommon.ProtocolVersionBinding{Version: version, SchemaDigest: digest}
		}
		offer := protocolcommon.ProtocolOffer{
			Name:           ProtocolName,
			SupportedRange: protocolcommon.ProtocolVersionRange(rangeText),
			Versions:       bindings,
		}

		first, firstFailure := selectProtocol(catalog, offer)
		reversedCatalog := slices.Clone(catalog)
		slices.Reverse(reversedCatalog)
		reversedOffer := offer
		reversedOffer.Versions = slices.Clone(offer.Versions)
		slices.Reverse(reversedOffer.Versions)
		second, secondFailure := selectProtocol(reversedCatalog, reversedOffer)
		if !duplicates && (firstFailure != secondFailure || first != second) {
			t.Fatalf("selection depends on catalog/offer order: %+v/%v %+v/%v offer=%+v", first, firstFailure, second, secondFailure, offer)
		}

		_, schemaErr := protocolcommon.EncodeProtocolOffer(offer)
		if duplicates && schemaErr == nil {
			t.Fatalf("generated schema accepted duplicate offered versions: %+v", offer)
		}
		if schemaErr != nil {
			return
		}
		expected, found := expectedProtocolSelection(catalog, offer)
		if found != (firstFailure == selectionCompatible) || (found && expected != first) {
			t.Fatalf("selection is not the highest exact version/digest member: got=%+v/%v want=%+v/%v", first, firstFailure, expected, found)
		}
	})
}

func fuzzProtocolCatalog() []protocolBinding {
	return []protocolBinding{
		{version: protocolNumber{major: 1, minor: 0}, wireVersion: "1.0", schemaDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		{version: protocolNumber{major: 1, minor: 1}, wireVersion: "1.1", schemaDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111"},
		{version: protocolNumber{major: 1, minor: 2}, wireVersion: "1.2", schemaDigest: "sha256:2222222222222222222222222222222222222222222222222222222222222222"},
		{version: protocolNumber{major: 1, minor: 3}, wireVersion: "1.3", schemaDigest: "sha256:3333333333333333333333333333333333333333333333333333333333333333"},
	}
}

func expectedProtocolSelection(catalog []protocolBinding, offer protocolcommon.ProtocolOffer) (protocolBinding, bool) {
	parsed, ok := parseProtocolRange(string(offer.SupportedRange))
	if !ok {
		return protocolBinding{}, false
	}
	var result protocolBinding
	found := false
	for _, host := range catalog {
		if host.version.compare(parsed.lower) < 0 || host.version.compare(parsed.upper) > 0 {
			continue
		}
		for _, binding := range offer.Versions {
			if binding.Version == host.wireVersion && binding.SchemaDigest == host.schemaDigest && (!found || host.version.compare(result.version) > 0) {
				result, found = host, true
			}
		}
	}
	return result, found
}

func FuzzCapabilityNormalization(f *testing.F) {
	f.Add([]byte{1, 1, 0, 2})
	f.Add([]byte{3, 2, 0, 0, 1, 2, 3})
	f.Add([]byte{2, 2, 0, 1, 1, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 128 {
			t.Skip()
		}
		requiredInput, optionalInput := fuzzCapabilityInputs(data)
		required, optional, detail, valid := normalizeCapabilityRequests(requiredInput, optionalInput)
		reversedRequired, reversedOptional := slices.Clone(requiredInput), slices.Clone(optionalInput)
		slices.Reverse(reversedRequired)
		slices.Reverse(reversedOptional)
		requiredAgain, optionalAgain, detailAgain, validAgain := normalizeCapabilityRequests(reversedRequired, reversedOptional)
		if valid != validAgain || detail != detailAgain {
			t.Fatalf("normalization validity depends on order: %v/%s %v/%s", valid, detail, validAgain, detailAgain)
		}
		if !valid {
			return
		}
		if !slices.Equal(required, requiredAgain) || !slices.Equal(optional, optionalAgain) {
			t.Fatalf("normalization depends on input permutation: %v/%v %v/%v", required, optional, requiredAgain, optionalAgain)
		}
		if !slices.IsSorted(required) || !slices.IsSorted(optional) {
			t.Fatal("normalized capabilities are not sorted")
		}
		idempotentRequired, idempotentOptional, _, idempotent := normalizeCapabilityRequests(required, optional)
		if !idempotent || !slices.Equal(required, idempotentRequired) || !slices.Equal(optional, idempotentOptional) {
			t.Fatalf("normalization is not idempotent: %v/%v -> %v/%v", required, optional, idempotentRequired, idempotentOptional)
		}
		statuses := requestedCapabilityStatuses([]string{OperationCompile, OperationHandshake}, required, optional, ProtocolVersion)
		if !slices.IsSortedFunc(statuses, func(left, right protocolcommon.RequestedCapabilityStatus) int {
			return stringCompare(string(left.CapabilityID), string(right.CapabilityID))
		}) {
			t.Fatalf("capability statuses are not canonical: %+v", statuses)
		}
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
			if _, err := protocolcommon.EncodeRequestedCapabilityStatus(status); err != nil {
				t.Fatalf("normalized status is not encodable: %v", err)
			}
		}
	})
}

func fuzzCapabilityInputs(data []byte) ([]protocolcommon.CapabilityID, []protocolcommon.CapabilityID) {
	universe := []protocolcommon.CapabilityID{OperationCompile, OperationHandshake, "engine.alpha", "engine.beta", "engine.gamma", "future.delta"}
	if len(data) == 0 {
		return []protocolcommon.CapabilityID{}, []protocolcommon.CapabilityID{}
	}
	requiredCount := int(data[0] % 9)
	optionalCount := int(data[len(data)-1] % 9)
	required := make([]protocolcommon.CapabilityID, requiredCount)
	optional := make([]protocolcommon.CapabilityID, optionalCount)
	for index := range required {
		required[index] = universe[int(data[(index+1)%len(data)])%len(universe)]
	}
	for index := range optional {
		optional[index] = universe[int(data[(requiredCount+index+1)%len(data)])%len(universe)]
	}
	return required, optional
}

func FuzzNegotiationTerminalEncodability(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4})
	f.Add([]byte{1, 0, 0, 1, 1, 0})
	f.Add([]byte{2, 5, 4, 3, 2, 1, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 128 {
			t.Skip()
		}
		descriptor := newTestDescriptor(t)
		request := validRequest()
		request.Payload.RequiredCapabilities, request.Payload.OptionalCapabilities = fuzzCapabilityInputs(data)
		if len(request.Payload.RequiredCapabilities) == 0 {
			request.Payload.RequiredCapabilities = []protocolcommon.CapabilityID{OperationCompile}
		}
		selector := byte(0)
		if len(data) != 0 {
			selector = data[0]
		}
		digest := protocolcommon.Digest(engineprotocol.SchemaDigest)
		if selector&1 != 0 {
			digest = testWrongDigest
		}
		request.Payload.Protocols[0].SupportedRange = "1.0..1.1"
		request.Payload.Protocols[0].Versions = []protocolcommon.ProtocolVersionBinding{
			{Version: "1.0", SchemaDigest: digest},
			{Version: "1.1", SchemaDigest: testWrongDigest},
		}
		request.Payload.Protocols = append(request.Payload.Protocols, protocolcommon.ProtocolOffer{
			Name: "future", SupportedRange: "1.0..1.0",
			Versions: []protocolcommon.ProtocolVersionBinding{{Version: "1.0", SchemaDigest: testWrongDigest}},
		})
		if selector&2 != 0 {
			request.Payload.Protocols[0].SupportedRange = "1.1..1.1"
		}
		if selector&4 != 0 {
			request.Payload.ClientLimits = &protocolcommon.CompileResourceLimitConstraints{MaxAssets: positive(int64(selector%9) + 1)}
		}

		first, _, err := descriptor.Negotiate(context.Background(), request)
		if err != nil || first.Outcome == protocolcommon.OutcomeFailed {
			t.Fatalf("bounded typed request produced an error/invariant failure: outcome=%s err=%v", first.Outcome, err)
		}
		firstBytes, err := engineprotocol.EncodeHandshakeResponseEnvelope(first)
		if err != nil || len(firstBytes) > maximumHandshakeResponseBytes {
			t.Fatalf("terminal response is not bounded/canonical: bytes=%d err=%v", len(firstBytes), err)
		}

		permuted := request
		permuted.Payload.RequiredCapabilities = slices.Clone(request.Payload.RequiredCapabilities)
		permuted.Payload.OptionalCapabilities = slices.Clone(request.Payload.OptionalCapabilities)
		permuted.Payload.Protocols = slices.Clone(request.Payload.Protocols)
		slices.Reverse(permuted.Payload.RequiredCapabilities)
		slices.Reverse(permuted.Payload.OptionalCapabilities)
		slices.Reverse(permuted.Payload.Protocols)
		for index := range permuted.Payload.Protocols {
			permuted.Payload.Protocols[index].Versions = slices.Clone(permuted.Payload.Protocols[index].Versions)
			slices.Reverse(permuted.Payload.Protocols[index].Versions)
		}
		second, _, err := descriptor.Negotiate(context.Background(), permuted)
		if err != nil || second.Outcome == protocolcommon.OutcomeFailed {
			t.Fatalf("permuted request produced an error/invariant failure: outcome=%s err=%v", second.Outcome, err)
		}
		secondBytes, err := engineprotocol.EncodeHandshakeResponseEnvelope(second)
		if err != nil || !bytes.Equal(firstBytes, secondBytes) {
			t.Fatalf("terminal response depends on bounded input order: err=%v\nfirst=%s\nsecond=%s", err, firstBytes, secondBytes)
		}
	})
}

func FuzzManifestETagOrderIndependence(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3})
	f.Add([]byte{3, 3, 2, 1, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64 {
			t.Skip()
		}
		transports := []string{"in_process", "native_rpc", "stdio", "wasm_worker"}
		for index := len(transports) - 1; index > 0; index-- {
			selector := byte(index)
			if len(data) != 0 {
				selector = data[index%len(data)]
			}
			other := int(selector) % (index + 1)
			transports[index], transports[other] = transports[other], transports[index]
		}
		config := validConfig()
		config.Transports = slices.Clone(transports)
		first, err := NewDescriptor(config)
		if err != nil {
			t.Fatal(err)
		}
		slices.Reverse(config.Transports)
		second, err := NewDescriptor(config)
		if err != nil {
			t.Fatal(err)
		}
		firstManifest, err := first.capabilityManifest(false, first.protocols[0], first.limits.HardMaximums)
		if err != nil {
			t.Fatal(err)
		}
		secondManifest, err := second.capabilityManifest(false, second.protocols[0], second.limits.HardMaximums)
		if err != nil {
			t.Fatal(err)
		}
		firstBytes, firstErr := protocolcommon.EncodeCapabilityManifest(firstManifest)
		secondBytes, secondErr := protocolcommon.EncodeCapabilityManifest(secondManifest)
		if firstErr != nil || secondErr != nil || firstManifest.ManifestEtag != secondManifest.ManifestEtag || !bytes.Equal(firstBytes, secondBytes) {
			t.Fatalf("transport insertion order changed canonical manifest/ETag: %v/%v %s/%s", firstErr, secondErr, firstManifest.ManifestEtag, secondManifest.ManifestEtag)
		}

		reordered := firstManifest
		reordered.Operations = map[string]protocolcommon.OperationCapability{}
		reordered.Operations[OperationHandshake] = firstManifest.Operations[OperationHandshake]
		reordered.Operations[OperationCompile] = firstManifest.Operations[OperationCompile]
		etag, err := manifestETag(reordered)
		if err != nil || etag != firstManifest.ManifestEtag {
			t.Fatalf("operation map insertion order changed manifest ETag: %s != %s (%v)", etag, firstManifest.ManifestEtag, err)
		}
	})
}

func stringCompare(left, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
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
