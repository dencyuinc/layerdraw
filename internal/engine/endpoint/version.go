// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
)

type protocolNumber struct {
	major uint32
	minor uint32
}

func (number protocolNumber) compare(other protocolNumber) int {
	if number.major < other.major {
		return -1
	}
	if number.major > other.major {
		return 1
	}
	if number.minor < other.minor {
		return -1
	}
	if number.minor > other.minor {
		return 1
	}
	return 0
}

type protocolRange struct {
	lower protocolNumber
	upper protocolNumber
}

type protocolBinding struct {
	version      protocolNumber
	wireVersion  protocolcommon.ProtocolVersion
	schemaDigest protocolcommon.Digest
}

type selectionFailure uint8

const (
	selectionCompatible selectionFailure = iota
	selectionMajorMismatch
	selectionRangeMismatch
	selectionSchemaDigestMismatch
)

func parseProtocolNumber(input string) (protocolNumber, bool) {
	parts := strings.Split(input, ".")
	if len(parts) != 2 || !canonicalUint32Part(parts[0]) || !canonicalUint32Part(parts[1]) {
		return protocolNumber{}, false
	}
	major, majorErr := strconv.ParseUint(parts[0], 10, 32)
	minor, minorErr := strconv.ParseUint(parts[1], 10, 32)
	if majorErr != nil || minorErr != nil {
		return protocolNumber{}, false
	}
	return protocolNumber{major: uint32(major), minor: uint32(minor)}, true
}

func parseProtocolRange(input string) (protocolRange, bool) {
	parts := strings.Split(input, "..")
	if len(parts) != 2 {
		return protocolRange{}, false
	}
	lower, lowerOK := parseProtocolNumber(parts[0])
	upper, upperOK := parseProtocolNumber(parts[1])
	if !lowerOK || !upperOK || lower.major != upper.major || lower.compare(upper) > 0 {
		return protocolRange{}, false
	}
	return protocolRange{lower: lower, upper: upper}, true
}

func canonicalUint32Part(input string) bool {
	if input == "0" {
		return true
	}
	if input == "" || input[0] < '1' || input[0] > '9' {
		return false
	}
	for index := 1; index < len(input); index++ {
		if input[index] < '0' || input[index] > '9' {
			return false
		}
	}
	return true
}

func selectProtocol(catalog []protocolBinding, offer protocolcommon.ProtocolOffer) (protocolBinding, selectionFailure) {
	offeredRange, ok := parseProtocolRange(string(offer.SupportedRange))
	if !ok {
		return protocolBinding{}, selectionRangeMismatch
	}
	sameMajor := false
	rangeOverlap := false
	for _, host := range catalog {
		if host.version.major != offeredRange.lower.major {
			continue
		}
		sameMajor = true
		if host.version.compare(offeredRange.lower) >= 0 && host.version.compare(offeredRange.upper) <= 0 {
			rangeOverlap = true
		}
	}
	if !sameMajor {
		return protocolBinding{}, selectionMajorMismatch
	}
	if !rangeOverlap {
		return protocolBinding{}, selectionRangeMismatch
	}

	offeredBindings := make(map[string]protocolcommon.Digest, len(offer.Versions))
	for _, binding := range offer.Versions {
		offeredBindings[string(binding.Version)] = binding.SchemaDigest
	}
	exactVersionFound := false
	var selected protocolBinding
	selectedFound := false
	for _, host := range catalog {
		if host.version.compare(offeredRange.lower) < 0 || host.version.compare(offeredRange.upper) > 0 {
			continue
		}
		offeredDigest, found := offeredBindings[string(host.wireVersion)]
		if !found {
			continue
		}
		exactVersionFound = true
		if offeredDigest != host.schemaDigest {
			continue
		}
		if !selectedFound || host.version.compare(selected.version) > 0 {
			selected = host
			selectedFound = true
		}
	}
	if selectedFound {
		return selected, selectionCompatible
	}
	if exactVersionFound {
		return protocolBinding{}, selectionSchemaDigestMismatch
	}
	return protocolBinding{}, selectionRangeMismatch
}
