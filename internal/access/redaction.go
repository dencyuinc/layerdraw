// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package access

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

import "github.com/dencyuinc/layerdraw/gen/go/accessprotocol"

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
type ReadRequest struct {
	Surface    ReadSurface
	Actor      accessprotocol.ActorRef
	DocumentID string
}
type UnredactedSource interface {
	ReadUnredacted(context.Context, ReadRequest) ([]Record, error)
}
type ProjectionPolicyResolver interface {
	ResolveProjectionPolicy(context.Context, ReadRequest) (ProjectionPolicy, error)
}

// ReadBoundary is the only exported result-reading facade. Raw provider output
// remains behind the private source field and is always projected before return.
type ReadBoundary struct {
	source   UnredactedSource
	policies ProjectionPolicyResolver
}

func NewReadBoundary(source UnredactedSource, policies ProjectionPolicyResolver) (*ReadBoundary, error) {
	if source == nil || policies == nil {
		return nil, errors.New("access: incomplete read boundary")
	}
	return &ReadBoundary{source: source, policies: policies}, nil
}
func (b *ReadBoundary) Read(ctx context.Context, request ReadRequest) ([]Record, error) {
	policy, err := b.policies.ResolveProjectionPolicy(ctx, request)
	if err != nil {
		return nil, err
	}
	// Deny before consulting the unredacted provider. Besides avoiding leaks,
	// this prevents a denied export/MCP request from triggering raw reads.
	if err := AuthorizeReadSurface(request.Surface, policy); err != nil {
		return nil, err
	}
	records, err := b.source.ReadUnredacted(ctx, request)
	if err != nil {
		return nil, err
	}
	return project(request.Surface, policy, records)
}

func project(surface ReadSurface, policy ProjectionPolicy, records []Record) ([]Record, error) {
	if err := AuthorizeReadSurface(surface, policy); err != nil {
		return nil, err
	}
	result := make([]Record, 0, len(records))
	for _, record := range records {
		if policy.AllowedSubjects != nil && !policy.AllowedSubjects[record.SubjectAddress] {
			continue
		}
		projected := Record{SubjectAddress: record.SubjectAddress, Fields: map[string]any{}}
		for field, value := range record.Fields {
			if policy.AllowedFields == nil || policy.AllowedFields[field] {
				encoded, err := json.Marshal(value)
				if err != nil {
					return nil, fmt.Errorf("access: field %q is not projectable: %w", field, err)
				}
				var clone any
				if err := json.Unmarshal(encoded, &clone); err != nil {
					return nil, fmt.Errorf("access: field %q is not projectable: %w", field, err)
				}
				projected.Fields[field] = clone
			}
		}
		result = append(result, projected)
	}
	return result, nil
}

// AuthorizeReadSurface is the shared fail-closed gate used by typed host reads
// and record-producing Search/Query/Review/export/MCP boundaries alike.
func AuthorizeReadSurface(surface ReadSurface, policy ProjectionPolicy) error {
	switch surface {
	case SurfaceSearch, SurfaceQuery, SurfaceReview, SurfaceMCP:
		if policy.Read {
			return nil
		}
	case SurfaceExport:
		if policy.Read && policy.Export {
			return nil
		}
	}
	return ErrReadDenied
}
