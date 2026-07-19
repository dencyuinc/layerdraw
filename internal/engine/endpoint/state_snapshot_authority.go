// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"fmt"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

// CanonicalizeStateQuerySnapshot validates and canonicalizes the generated
// protocol value once, then delegates semantic hashing to the Engine authority.
func CanonicalizeStateQuerySnapshot(input semantic.StateQuerySnapshot) ([]byte, protocolcommon.Digest, error) {
	canonical, err := semantic.EncodeStateQuerySnapshot(input)
	if err != nil {
		return nil, "", fmt.Errorf("encode StateQuerySnapshot: %w", err)
	}
	snapshot, err := stateQuerySnapshotFromProtocol(input)
	if err != nil {
		return nil, "", fmt.Errorf("map StateQuerySnapshot: %w", err)
	}
	_, hash, err := engine.CanonicalizeStateQuerySnapshot(snapshot)
	if err != nil {
		return nil, "", err
	}
	return canonical, protocolcommon.Digest(hash), nil
}

// StateFieldRegistry returns an independently owned protocol projection of
// the Engine's complete closed Language 1 state-field registry.
func StateFieldRegistry() []semantic.StateFieldPath {
	registry := engine.StateFieldRegistry()
	result := make([]semantic.StateFieldPath, len(registry))
	for index, path := range registry {
		result[index] = semantic.StateFieldPath(path)
	}
	return result
}

// CompareStableAddresses delegates protocol address ordering to Engine.
func CompareStableAddresses(left, right semantic.StableAddress) int {
	return engine.CompareStableAddresses(string(left), string(right))
}
