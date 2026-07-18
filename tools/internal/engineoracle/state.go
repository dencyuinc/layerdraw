// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package engineoracle exposes transport-neutral facts used to generate
// conformance oracles without leaking Engine-domain types into wire tools.
package engineoracle

import (
	"context"
	"fmt"

	"github.com/dencyuinc/layerdraw/internal/engine"
)

type StateSeed struct {
	DefinitionHash string
	GraphHash      string
	ProjectAddress string
	SubjectHashes  map[string]string
}

func CompileStateSeed(entry string, files map[string][]byte) (StateSeed, error) {
	compiler := engine.New(engine.BuildInfo{})
	result, err := compiler.Compile(context.Background(), engine.CompileInput{
		Mode: engine.CompileProject, EntryPath: entry, ProjectSourceTree: files,
		ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1},
	})
	if err != nil {
		return StateSeed{}, err
	}
	snapshot := result.Snapshot()
	if len(snapshot.Diagnostics) != 0 || snapshot.TypedAST.Project == nil || snapshot.GraphHash == nil {
		return StateSeed{}, fmt.Errorf("state oracle source did not compile: %+v", snapshot.Diagnostics)
	}
	hashes := make(map[string]string, len(snapshot.SubjectSemanticHashes))
	for _, subject := range snapshot.SubjectSemanticHashes {
		hashes[subject.Address] = subject.Hash
	}
	return StateSeed{
		DefinitionHash: snapshot.DefinitionHash,
		GraphHash:      *snapshot.GraphHash,
		ProjectAddress: snapshot.TypedAST.Project.Address,
		SubjectHashes:  hashes,
	}, nil
}
