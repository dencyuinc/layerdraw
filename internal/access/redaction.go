// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package access

import "errors"

type ReadSurface string

const (
	SurfaceSearch ReadSurface = "search"
	SurfaceQuery  ReadSurface = "query"
	SurfaceReview ReadSurface = "review"
	SurfaceExport ReadSurface = "export"
	SurfaceMCP    ReadSurface = "mcp"
)

var ErrReadDenied = errors.New("access: read denied")

// ProjectionPolicy is resolved by the trusted host before a result crosses
// the Access boundary. nil AllowedFields means unrestricted fields; an empty
// non-nil map allows no fields.
type ProjectionPolicy struct {
	Read, Export    bool
	AllowedSubjects map[string]bool
	AllowedFields   map[string]bool
}

type Record struct {
	SubjectAddress string
	Fields         map[string]any
}

// Project applies subject filtering and field redaction before Search, Query,
// Review, export, or MCP results leave the trusted process. It copies all
// returned maps so callers cannot recover redacted values through aliasing.
func Project(surface ReadSurface, policy ProjectionPolicy, records []Record) ([]Record, error) {
	if !policy.Read || (surface == SurfaceExport && !policy.Export) {
		return nil, ErrReadDenied
	}
	result := make([]Record, 0, len(records))
	for _, record := range records {
		if policy.AllowedSubjects != nil && !policy.AllowedSubjects[record.SubjectAddress] {
			continue
		}
		projected := Record{SubjectAddress: record.SubjectAddress, Fields: map[string]any{}}
		for field, value := range record.Fields {
			if policy.AllowedFields == nil || policy.AllowedFields[field] {
				projected.Fields[field] = value
			}
		}
		result = append(result, projected)
	}
	return result, nil
}
