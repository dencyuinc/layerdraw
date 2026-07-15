// SPDX-License-Identifier: Apache-2.0

package conformance_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

type normalizedFixtureManifest struct {
	SchemaVersion int                         `json:"schema_version"`
	Fixtures      []normalizedFixtureContract `json:"fixtures"`
}

type normalizedFixtureContract struct {
	Kind            string               `json:"kind"`
	Format          string               `json:"format"`
	RootAddress     string               `json:"root_address"`
	ControlEnvelope string               `json:"control_envelope"`
	Source          string               `json:"source"`
	PackManifest    string               `json:"pack_manifest"`
	CanonicalJSON   normalizedFixtureRef `json:"canonical_json"`
	ArtifactJSON    normalizedFixtureRef `json:"artifact_json"`
}

type normalizedFixtureRef struct {
	Role      string `json:"role"`
	BlobID    string `json:"blob_id"`
	File      string `json:"file"`
	Digest    string `json:"digest"`
	Size      string `json:"size"`
	MediaType string `json:"media_type"`
	Lifetime  string `json:"lifetime"`
}

type normalizedPublication struct {
	Kind        string
	Branch      string
	RootAddress string
	Canonical   normalizedFixtureRef
	Artifact    normalizedFixtureRef
}

type normalizedGeneration struct {
	Kind      string
	Root      string
	Canonical []byte
	Artifact  []byte
}

func TestNormalizedArtifactFixtureContract(t *testing.T) {
	t.Parallel()
	root := protocolRepositoryRoot(t)
	manifest := readNormalizedFixtureManifest(t, root)
	if manifest.SchemaVersion != 1 || len(manifest.Fixtures) != 2 {
		t.Fatalf("normalized fixture manifest is incomplete: %+v", manifest)
	}

	for _, fixture := range manifest.Fixtures {
		fixture := fixture
		t.Run(fixture.Kind, func(t *testing.T) {
			t.Parallel()
			generation := compileNormalizedFixture(t, root, fixture)
			canonical := readRepositoryFile(t, root, fixture.CanonicalJSON.File)
			artifact := readRepositoryFile(t, root, fixture.ArtifactJSON.File)
			if !bytes.Equal(generation.Canonical, canonical) || !bytes.Equal(generation.Artifact, artifact) {
				t.Fatalf("checked-in %s normalized bodies differ from the canonical Engine generation", fixture.Kind)
			}
			if bytes.HasSuffix(canonical, []byte{'\n'}) || !bytes.Equal(artifact, append(append([]byte{}, canonical...), '\n')) {
				t.Fatal("canonical/public normalized byte profiles are not no-LF/exactly-one-LF")
			}
			assertNormalizedBodyShape(t, canonical, fixture)

			envelopeBytes := readRepositoryFile(t, root, fixture.ControlEnvelope)
			envelope, err := engineprotocol.DecodeCompileResponseEnvelope(envelopeBytes)
			if err != nil {
				t.Fatalf("decode control envelope: %v", err)
			}
			controlCanonical, err := engineprotocol.EncodeCompileResponseEnvelope(envelope)
			if err != nil {
				t.Fatalf("encode control envelope: %v", err)
			}
			roundTrip, err := engineprotocol.DecodeCompileResponseEnvelope(controlCanonical)
			if err != nil {
				t.Fatalf("decode canonical control envelope: %v", err)
			}
			controlAgain, err := engineprotocol.EncodeCompileResponseEnvelope(roundTrip)
			if err != nil || !bytes.Equal(controlCanonical, controlAgain) {
				t.Fatalf("control envelope canonical round trip drifted: %v", err)
			}
			canonicalControlPath := strings.Replace(fixture.ControlEnvelope, "/engine/", "/conformance/engine/", 1)
			if shared := bytes.TrimSuffix(readRepositoryFile(t, root, canonicalControlPath), []byte{'\n'}); !bytes.Equal(controlCanonical, shared) {
				t.Fatal("generated Go control-envelope bytes differ from the shared canonical fixture")
			}

			publication := publicationFromEnvelope(t, envelope)
			assertPublicationMatchesManifest(t, publication, fixture)
			blobs := map[string][]byte{
				fixture.CanonicalJSON.BlobID: canonical,
				fixture.ArtifactJSON.BlobID:  artifact,
			}
			if err := verifyNormalizedPublication(publication, blobs, generation); err != nil {
				t.Fatalf("verify normalized publication: %v", err)
			}
		})
	}
}

func TestNormalizedArtifactFixtureBoundaryNegatives(t *testing.T) {
	t.Parallel()
	root := protocolRepositoryRoot(t)
	manifest := readNormalizedFixtureManifest(t, root)
	publications := map[string]normalizedPublication{}
	generations := map[string]normalizedGeneration{}
	blobSets := map[string]map[string][]byte{}
	for _, fixture := range manifest.Fixtures {
		generation := compileNormalizedFixture(t, root, fixture)
		generations[fixture.Kind] = generation
		envelope, err := engineprotocol.DecodeCompileResponseEnvelope(readRepositoryFile(t, root, fixture.ControlEnvelope))
		if err != nil {
			t.Fatal(err)
		}
		publications[fixture.Kind] = publicationFromEnvelope(t, envelope)
		blobSets[fixture.Kind] = map[string][]byte{
			fixture.CanonicalJSON.BlobID: readRepositoryFile(t, root, fixture.CanonicalJSON.File),
			fixture.ArtifactJSON.BlobID:  readRepositoryFile(t, root, fixture.ArtifactJSON.File),
		}
	}

	project := publications["project"]
	projectBlobs := cloneBlobSet(blobSets["project"])
	projectGeneration := generations["project"]
	pack := publications["pack"]

	tests := []struct {
		name        string
		publication normalizedPublication
		blobs       map[string][]byte
		generation  normalizedGeneration
	}{
		{
			name: "missing body", publication: project,
			blobs: func() map[string][]byte {
				value := cloneBlobSet(projectBlobs)
				delete(value, project.Canonical.BlobID)
				return value
			}(),
			generation: projectGeneration,
		},
		{
			name: "digest mismatch", publication: mutatePublication(project, func(value *normalizedPublication) {
				value.Canonical.Digest = "sha256:" + string(bytes.Repeat([]byte{'0'}, 64))
			}), blobs: projectBlobs, generation: projectGeneration,
		},
		{
			name: "size mismatch", publication: mutatePublication(project, func(value *normalizedPublication) {
				value.Artifact.Size = strconv.Itoa(len(projectGeneration.Artifact) + 1)
			}), blobs: projectBlobs, generation: projectGeneration,
		},
		{
			name: "swapped canonical and public refs", publication: mutatePublication(project, func(value *normalizedPublication) {
				value.Canonical, value.Artifact = value.Artifact, value.Canonical
			}), blobs: projectBlobs, generation: projectGeneration,
		},
		{
			name: "wrong Project Pack media type", publication: mutatePublication(project, func(value *normalizedPublication) {
				value.Canonical.MediaType = pack.Canonical.MediaType
			}), blobs: projectBlobs, generation: projectGeneration,
		},
		{
			name: "wrong role media type", publication: mutatePublication(project, func(value *normalizedPublication) {
				value.Canonical.MediaType = project.Artifact.MediaType
			}), blobs: projectBlobs, generation: projectGeneration,
		},
		{
			name: "branch mismatch", publication: mutatePublication(project, func(value *normalizedPublication) {
				value.Branch = "pack"
			}), blobs: projectBlobs, generation: projectGeneration,
		},
		{
			name: "root mismatch", publication: mutatePublication(project, func(value *normalizedPublication) {
				value.RootAddress = "ldl:project:other"
			}), blobs: projectBlobs, generation: projectGeneration,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if err := verifyNormalizedPublication(test.publication, test.blobs, test.generation); err == nil {
				t.Fatal("invalid normalized publication was accepted")
			}
		})
	}

	t.Run("mismatched fixture generation", func(t *testing.T) {
		driftCanonical := bytes.Replace(projectGeneration.Canonical, []byte(`"Fixture"`), []byte(`"Drifted"`), 1)
		if bytes.Equal(driftCanonical, projectGeneration.Canonical) {
			t.Fatal("failed to construct drifted normalized fixture")
		}
		driftArtifact := append(append([]byte{}, driftCanonical...), '\n')
		drift := project
		drift.Canonical.Digest = rawNormalizedDigest(driftCanonical)
		drift.Canonical.Size = strconv.Itoa(len(driftCanonical))
		drift.Artifact.Digest = rawNormalizedDigest(driftArtifact)
		drift.Artifact.Size = strconv.Itoa(len(driftArtifact))
		blobs := map[string][]byte{drift.Canonical.BlobID: driftCanonical, drift.Artifact.BlobID: driftArtifact}
		schemaDigest := engineprotocol.SchemaDigest
		if err := verifyNormalizedPublication(drift, blobs, projectGeneration); err == nil {
			t.Fatal("body/ref pair from a different Engine generation was accepted")
		}
		if engineprotocol.SchemaDigest != schemaDigest {
			t.Fatal("normalized fixture mutation unexpectedly changed the Engine control schema digest")
		}
	})
}

func compileNormalizedFixture(t *testing.T, root string, fixture normalizedFixtureContract) normalizedGeneration {
	t.Helper()
	source := readRepositoryFile(t, root, fixture.Source)
	input := engine.CompileInput{Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": source}, ResolvedDependencies: conformanceEmptyResolved()}
	if fixture.Kind == "pack" {
		manifest := readRepositoryFile(t, root, fixture.PackManifest)
		input = engine.CompileInput{
			Mode: engine.CompilePack, EntryPath: "pack.ldl", RootPackID: "pub/schema",
			InstalledPackTree: map[string][]byte{"pack/schema/pack.ldl": source},
			ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: []engine.ResolvedPack{{
				InstallName: "root", CanonicalID: "pub/schema", Version: "1.0.0", Digest: conformanceDigest(source), Path: "pack/schema", Entry: "pack.ldl",
				ManifestPath: "manifest.json", Manifest: manifest, Files: []engine.ResolvedPackFile{{Path: "pack.ldl", Digest: conformanceDigest(source)}},
			}}},
		}
	}
	result, err := engine.New(engine.BuildInfo{}).Compile(context.Background(), input)
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("compile %s fixture: err=%v diagnostics=%+v", fixture.Kind, err, result.Diagnostics)
	}
	snapshot := result.Snapshot()
	generation := normalizedGeneration{Kind: fixture.Kind, Canonical: snapshot.CanonicalJSON, Artifact: snapshot.ArtifactJSON}
	switch fixture.Kind {
	case "project":
		if snapshot.NormalizedDocument == nil || snapshot.NormalizedPackArtifact != nil || snapshot.NormalizedDocument.Format != "layerdraw-normalized" {
			t.Fatal("Engine did not produce the exact Project normalized union member")
		}
		generation.Root = snapshot.NormalizedDocument.Project.Address
	case "pack":
		if snapshot.NormalizedPackArtifact == nil || snapshot.NormalizedDocument != nil || snapshot.NormalizedPackArtifact.Format != "layerdraw-normalized-pack" {
			t.Fatal("Engine did not produce the exact Pack normalized union member")
		}
		generation.Root = snapshot.NormalizedPackArtifact.Pack.Address
	default:
		t.Fatalf("unknown normalized fixture kind %q", fixture.Kind)
	}
	if generation.Root != fixture.RootAddress {
		t.Fatalf("compiled root %q differs from fixture root %q", generation.Root, fixture.RootAddress)
	}
	return generation
}

func assertNormalizedBodyShape(t *testing.T, body []byte, fixture normalizedFixtureContract) {
	t.Helper()
	var value map[string]json.RawMessage
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("decode Engine-produced normalized body: %v", err)
	}
	var schemaVersion, language int
	var format string
	if err := json.Unmarshal(value["schema_version"], &schemaVersion); err != nil || schemaVersion != 1 {
		t.Fatalf("schema_version is not exactly 1: %v", err)
	}
	if err := json.Unmarshal(value["language"], &language); err != nil || language != 1 {
		t.Fatalf("language is not exactly 1: %v", err)
	}
	if err := json.Unmarshal(value["format"], &format); err != nil || format != fixture.Format {
		t.Fatalf("format %q differs from %q: %v", format, fixture.Format, err)
	}
	wantKeys := map[string]bool{}
	for _, key := range []string{"assets", "dependencies", "entity_types", "format", "identity", "language", "queries", "references", "relation_types", "schema_version", "views"} {
		wantKeys[key] = true
	}
	rootMember := fixture.Kind
	if fixture.Kind == "project" {
		for _, key := range []string{"project", "layers", "entities", "relations"} {
			wantKeys[key] = true
		}
	} else {
		wantKeys["pack"] = true
	}
	if len(value) != len(wantKeys) {
		t.Fatalf("%s top-level member count=%d, want %d", fixture.Kind, len(value), len(wantKeys))
	}
	for key := range value {
		if !wantKeys[key] {
			t.Fatalf("%s normalized body contains forbidden top-level member %q", fixture.Kind, key)
		}
	}
	var root struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(value[rootMember], &root); err != nil || root.Address != fixture.RootAddress {
		t.Fatalf("%s root does not agree with fixture metadata: %+v %v", fixture.Kind, root, err)
	}
}

func publicationFromEnvelope(t *testing.T, envelope engineprotocol.CompileResponseEnvelope) normalizedPublication {
	t.Helper()
	if envelope.Payload == nil {
		t.Fatal("success fixture has no payload")
	}
	artifact := envelope.Payload.NormalizedArtifact
	switch artifact.Kind {
	case engineprotocol.CompileModeProject:
		if artifact.Project == nil || artifact.Pack != nil {
			t.Fatal("Project control envelope branch is not closed")
		}
		return normalizedPublication{
			Kind: "project", Branch: "project", RootAddress: string(artifact.Project.ProjectAddress),
			Canonical: normalizedFixtureRef{Role: "normalized_project_canonical_json", BlobID: artifact.Project.CanonicalJSON.BlobID, Digest: string(artifact.Project.CanonicalJSON.Digest), Size: string(artifact.Project.CanonicalJSON.Size), MediaType: string(artifact.Project.CanonicalJSON.MediaType), Lifetime: string(artifact.Project.CanonicalJSON.Lifetime)},
			Artifact:  normalizedFixtureRef{Role: "normalized_project_artifact_json", BlobID: artifact.Project.ArtifactJSON.BlobID, Digest: string(artifact.Project.ArtifactJSON.Digest), Size: string(artifact.Project.ArtifactJSON.Size), MediaType: string(artifact.Project.ArtifactJSON.MediaType), Lifetime: string(artifact.Project.ArtifactJSON.Lifetime)},
		}
	case engineprotocol.CompileModePack:
		if artifact.Pack == nil || artifact.Project != nil {
			t.Fatal("Pack control envelope branch is not closed")
		}
		return normalizedPublication{
			Kind: "pack", Branch: "pack", RootAddress: string(artifact.Pack.PackAddress),
			Canonical: normalizedFixtureRef{Role: "normalized_pack_canonical_json", BlobID: artifact.Pack.CanonicalJSON.BlobID, Digest: string(artifact.Pack.CanonicalJSON.Digest), Size: string(artifact.Pack.CanonicalJSON.Size), MediaType: string(artifact.Pack.CanonicalJSON.MediaType), Lifetime: string(artifact.Pack.CanonicalJSON.Lifetime)},
			Artifact:  normalizedFixtureRef{Role: "normalized_pack_artifact_json", BlobID: artifact.Pack.ArtifactJSON.BlobID, Digest: string(artifact.Pack.ArtifactJSON.Digest), Size: string(artifact.Pack.ArtifactJSON.Size), MediaType: string(artifact.Pack.ArtifactJSON.MediaType), Lifetime: string(artifact.Pack.ArtifactJSON.Lifetime)},
		}
	default:
		t.Fatalf("unknown normalized artifact kind %q", artifact.Kind)
		return normalizedPublication{}
	}
}

func verifyNormalizedPublication(publication normalizedPublication, blobs map[string][]byte, generation normalizedGeneration) error {
	if publication.Kind != generation.Kind || publication.Branch != publication.Kind || publication.RootAddress != generation.Root {
		return fmt.Errorf("kind/branch/root does not agree with the Engine generation")
	}
	want := map[string]struct{ role, media string }{
		"project:canonical": {"normalized_project_canonical_json", "application/vnd.layerdraw.normalized-project.v1+json"},
		"project:artifact":  {"normalized_project_artifact_json", "application/vnd.layerdraw.project.v1+json"},
		"pack:canonical":    {"normalized_pack_canonical_json", "application/vnd.layerdraw.normalized-pack.v1+json"},
		"pack:artifact":     {"normalized_pack_artifact_json", "application/vnd.layerdraw.pack.v1+json"},
	}
	for _, item := range []struct {
		name string
		ref  normalizedFixtureRef
		want []byte
	}{{"canonical", publication.Canonical, generation.Canonical}, {"artifact", publication.Artifact, generation.Artifact}} {
		expected := want[publication.Kind+":"+item.name]
		if item.ref.Role != expected.role || item.ref.MediaType != expected.media || item.ref.Lifetime != "request" {
			return fmt.Errorf("%s role/media/lifetime mismatch", item.name)
		}
		body, ok := blobs[item.ref.BlobID]
		if !ok {
			return fmt.Errorf("%s body is missing", item.name)
		}
		size, err := strconv.ParseUint(item.ref.Size, 10, 64)
		if err != nil || uint64(len(body)) != size {
			return fmt.Errorf("%s raw size mismatch", item.name)
		}
		if item.ref.Digest != rawNormalizedDigest(body) {
			return fmt.Errorf("%s raw digest mismatch", item.name)
		}
		if !bytes.Equal(body, item.want) {
			return fmt.Errorf("%s body belongs to a different Engine generation", item.name)
		}
	}
	if publication.Canonical.BlobID == publication.Artifact.BlobID || bytes.HasSuffix(generation.Canonical, []byte{'\n'}) || !bytes.Equal(generation.Artifact, append(append([]byte{}, generation.Canonical...), '\n')) {
		return fmt.Errorf("canonical/public byte roles are not distinct no-LF/exactly-one-LF bindings")
	}
	return nil
}

func assertPublicationMatchesManifest(t *testing.T, publication normalizedPublication, fixture normalizedFixtureContract) {
	t.Helper()
	fixtureCanonical := fixture.CanonicalJSON
	fixtureCanonical.File = ""
	fixtureArtifact := fixture.ArtifactJSON
	fixtureArtifact.File = ""
	if publication.Kind != fixture.Kind || publication.RootAddress != fixture.RootAddress || publication.Canonical != fixtureCanonical || publication.Artifact != fixtureArtifact {
		t.Fatalf("control envelope refs do not exactly match normalized fixture manifest\npublication=%+v\nfixture=%+v", publication, fixture)
	}
}

func readNormalizedFixtureManifest(t *testing.T, root string) normalizedFixtureManifest {
	t.Helper()
	data := readRepositoryFile(t, root, "schemas/fixtures/normalized/v1/manifest.json")
	var manifest normalizedFixtureManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func readRepositoryFile(t *testing.T, root, relative string) []byte {
	t.Helper()
	value, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func rawNormalizedDigest(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func cloneBlobSet(value map[string][]byte) map[string][]byte {
	copy := make(map[string][]byte, len(value))
	for key, body := range value {
		copy[key] = append([]byte{}, body...)
	}
	return copy
}

func mutatePublication(value normalizedPublication, mutate func(*normalizedPublication)) normalizedPublication {
	mutate(&value)
	return value
}
