// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package exportrecipe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type registryDigestPayload struct {
	Format        string                          `json:"format"`
	Profiles      map[string]registryProfileEntry `json:"profiles"`
	SchemaVersion int                             `json:"schema_version"`
}

type registryProfileEntry struct {
	Format              Format `json:"format"`
	ID                  string `json:"id"`
	SpecificationDigest string `json:"specification_digest"`
}

type specificationPayload struct {
	Format        Format `json:"format"`
	ID            string `json:"id"`
	Language      int    `json:"language"`
	SchemaVersion int    `json:"schema_version"`
}

var formatOrder = []Format{
	FormatJSON, FormatYAML, FormatSVG, FormatPNG, FormatPDF, FormatHTML,
	FormatCSV, FormatTSV, FormatXLSX, FormatMarkdown, FormatPPTX, FormatDOCX,
	FormatMermaid, FormatBPMN, FormatDrawIO,
}

// BuiltinRegistry returns a defensive snapshot of the embedded Language 1
// profile registry. The embedded specifications identify serializer profiles;
// they do not define or execute serializer behavior in this package.
func BuiltinRegistry() ProfileRegistry {
	profiles := make([]ProfileSpecification, 0, len(formatOrder))
	for _, format := range formatOrder {
		id := "layerdraw/" + string(format) + "@1"
		specification, _ := json.Marshal(specificationPayload{Format: format, ID: id, Language: 1, SchemaVersion: 1})
		profiles = append(profiles, ProfileSpecification{
			ID: id, Format: format, SpecificationDigest: digest(specification),
			Specification: append([]byte{}, specification...),
		})
	}
	registry := ProfileRegistry{Format: "layerdraw-exporter-profiles", SchemaVersion: 1, Profiles: profiles}
	registry.Digest = registryDigest(registry)
	return cloneRegistry(registry)
}

func cloneRegistry(registry ProfileRegistry) ProfileRegistry {
	out := registry
	out.Profiles = append([]ProfileSpecification{}, registry.Profiles...)
	for index := range out.Profiles {
		out.Profiles[index].Specification = append([]byte{}, registry.Profiles[index].Specification...)
	}
	return out
}

func registryDigest(registry ProfileRegistry) string {
	return digest(registryCanonicalBytes(registry))
}

func registryCanonicalBytes(registry ProfileRegistry) []byte {
	entries := make(map[string]registryProfileEntry, len(registry.Profiles))
	for _, profile := range registry.Profiles {
		entries[profile.ID] = registryProfileEntry{Format: profile.Format, ID: profile.ID, SpecificationDigest: profile.SpecificationDigest}
	}
	// This closed schema contains only strings, an integer, and an object map.
	// Field declarations are in RFC 8785 key order. Profile IDs are restricted
	// to ASCII, for which encoding/json map ordering equals JCS UTF-16 ordering.
	payload, _ := json.Marshal(registryDigestPayload{Format: registry.Format, Profiles: entries, SchemaVersion: registry.SchemaVersion})
	return payload
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}
