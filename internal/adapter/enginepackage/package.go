// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package enginepackage adapts native Desktop document interchange to the
// Engine-owned .layerdraw reader and writer. Keeping this adapter separate
// prevents generated protocol values from meeting Engine domain types outside
// the endpoint boundary.
package enginepackage

import (
	"bytes"
	"context"
	"errors"
	"sort"

	"github.com/dencyuinc/layerdraw/internal/engine"
)

var ErrInvalidProjection = errors.New("redacted package input is not closed")

type Service struct{ engine engine.Engine }

func New(value engine.Engine) Service { return Service{engine: value} }

func (s Service) Import(ctx context.Context, value []byte, limits engine.LayerdrawLimits) (engine.LayerdrawDocument, error) {
	return s.engine.ReadLayerdraw(ctx, engine.LayerdrawReadInput{Bytes: value, Limits: limits})
}

func (s Service) Export(ctx context.Context, input engine.LayerdrawWriteInput) ([]byte, error) {
	return s.engine.WriteLayerdraw(ctx, input)
}

type RedactedInput struct {
	CompileInput       engine.CompileInput
	ProjectedSnapshots []engine.StateQuerySnapshot
	PolicyID           string
	Secrets            [][]byte
	Limits             engine.LayerdrawLimits
	Projection         ProjectionReceipt
	DerivedArtifacts   map[string][]byte
}

// ProjectionReceipt is issued by the trusted Runtime/Access owner together
// with the projected source and state snapshot. Portable inputs never supply
// it and this adapter does not manufacture Access decisions.
type ProjectionReceipt struct {
	PolicyID              string
	CommittedRevisionHash string
	AccessDecisionDigest  string
}

// ExportRedacted rejects pre-existing derived artifacts. The Engine writer
// recomputes package digests and records the projection policy in the manifest.
func (s Service) ExportRedacted(ctx context.Context, input RedactedInput) ([]byte, error) {
	if input.Projection.PolicyID == "" || input.Projection.PolicyID != input.PolicyID || input.Projection.CommittedRevisionHash == "" || input.Projection.AccessDecisionDigest == "" || len(input.DerivedArtifacts) != 0 {
		return nil, ErrInvalidProjection
	}
	return s.engine.WriteLayerdraw(ctx, engine.LayerdrawWriteInput{CompileInput: input.CompileInput, StateSnapshots: input.ProjectedSnapshots, RedactionPolicyID: input.PolicyID, Secrets: input.Secrets, Limits: input.Limits})
}

func ExportLDLTree(input engine.CompileInput) (map[string][]byte, error) {
	if input.Mode != engine.CompileProject || input.EntryPath == "" || len(input.ProjectSourceTree) == 0 {
		return nil, ErrInvalidProjection
	}
	paths := make([]string, 0, len(input.ProjectSourceTree))
	for path := range input.ProjectSourceTree {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result := make(map[string][]byte, len(paths))
	for _, path := range paths {
		result[path] = bytes.Clone(input.ProjectSourceTree[path])
	}
	return result, nil
}
