// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package graph compiles resolved LDL graph facts against typed definitions.
package graph

import (
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

type Input struct {
	Resolve    resolve.Result
	Definition definition.Result
}

type Result struct {
	stageGeneration resolve.StageGeneration
	Graph           *MasterGraph
	Diagnostics     []resolve.Diagnostic
	HasErrors       bool
}

// MatchesResolve reports whether this graph result belongs to the supplied
// Resolve invocation, including for rejected compilations.
func (r Result) MatchesResolve(resolved resolve.Result) bool {
	return r.stageGeneration.Matches(resolved.Generation())
}

// MasterGraph is the canonical typed graph fact collection. Every slice uses
// structured StableSymbol order; adjacency relation addresses use Relation
// StableSymbol order.
type MasterGraph struct {
	Entities  []Entity
	Relations []Relation
	Outgoing  []Adjacency
	Incoming  []Adjacency
}

type Entity struct {
	definition.Common
	ID             string
	Address        string
	DisplayName    string
	TypeAddress    string
	LayerAddress   string
	Rows           []AttributeRow
	ReservedRowIDs []string
}

type Relation struct {
	definition.Common
	ID             string
	Address        string
	DisplayName    *string
	TypeAddress    string
	FromAddress    string
	ToAddress      string
	Rows           []AttributeRow
	ReservedRowIDs []string
	CrossLayer     bool
}

type AttributeRow struct {
	ID      string
	Address string
	Values  []Cell
}

type Cell struct {
	ColumnAddress string
	Value         definition.Scalar
}

type Adjacency struct {
	EntityAddress     string
	RelationAddresses []string
}
