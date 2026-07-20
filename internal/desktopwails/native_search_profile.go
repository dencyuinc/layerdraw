// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package desktopwails

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func packagedEmbeddingProfile() port.EmbeddingProfile {
	digest := sha256.Sum256([]byte("layerdraw.desktop.local_projection.v1"))
	return port.EmbeddingProfile{
		ProfileID: "layerdraw.desktop.local", ModelID: "local_projection", ModelVersion: "1",
		ModelDigest: "sha256:" + hex.EncodeToString(digest[:]), Dimensions: 16,
		Normalization: "unit", MaxInputBytes: 4096,
	}
}

func packagedSearchProfile() port.SearchProfile {
	profile := port.SearchProfile{ProfileID: "layerdraw.desktop.default", LexicalCandidateLimit: 256, SemanticCandidateLimit: 256, MaxHits: 100, RRFK: 60, LexicalWeight: 1, SemanticWeight: 1, SnippetMaxBytes: 256}
	encoded, _ := json.Marshal(profile)
	digest := sha256.Sum256(encoded)
	profile.SpecificationDigest = "sha256:" + hex.EncodeToString(digest[:])
	return profile
}

func packagedSearchIdentity(snapshot port.DocumentSnapshotRef, accessDigest string) port.SearchIndexIdentity {
	search, embedding := packagedSearchProfile(), packagedEmbeddingProfile()
	return port.SearchIndexIdentity{
		DocumentSnapshotRef: snapshot, SearchProfileID: search.ProfileID, SearchProfileDigest: search.SpecificationDigest,
		EmbeddingProfileID: embedding.ProfileID, EmbeddingProfileDigest: embedding.ModelDigest,
		AccessProjectionDigest: accessDigest, LadybugBackendVersion: "0.17.0", IndexSchemaVersion: "1",
	}
}
