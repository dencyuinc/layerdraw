// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"testing"
)

func TestBuildRegistryProjectMutationInstallsAndRemovesClosedPack(t *testing.T) {
	instance := New(BuildInfo{})
	base := CompileInput{Mode: CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte("project p \"Project\" {}\n")}, InstalledPackTree: map[string][]byte{}, ResolvedDependencies: ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: []ResolvedPack{}}}
	source := []byte("entity_type service \"Service\" {}\nexport { service }\n")
	manifest, err := canonicalArtifact(RegistryPackManifest{Format: LayerdrawPackFormat, FormatVersion: 1, ID: "example/schema", Name: "schema", Version: "1.0.0", Language: 1, Entry: "pack.ldl", Dependencies: map[string]RegistryPackDependency{}})
	if err != nil {
		t.Fatal(err)
	}
	archive := packArchive(t, map[string][]byte{"manifest.json": manifest, "pack.ldl": source})
	installed, err := instance.BuildRegistryProjectMutation(context.Background(), RegistryProjectMutationInput{Base: base, Artifacts: []RegistryProjectArtifactInput{{Bytes: archive, RegistrySource: "official"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(installed.Input.ResolvedDependencies.Installs) != 1 || string(installed.Input.InstalledPackTree["pack/schema/pack.ldl"]) != string(source) || !containsAuthoringCapability(installed.AuthoringImpact.RequiredCapabilities, CapabilityPackageManage) || installed.AuthoringImpact.ImpactDigest == "" {
		t.Fatalf("installed=%+v impact=%+v", installed.Input.ResolvedDependencies, installed.AuthoringImpact)
	}
	removed, err := instance.BuildRegistryProjectMutation(context.Background(), RegistryProjectMutationInput{Base: installed.Input, RemoveCanonicalIDs: []string{"example/schema"}})
	if err != nil || len(removed.Input.ResolvedDependencies.Installs) != 0 || len(removed.Input.InstalledPackTree) != 0 || !containsAuthoringCapability(removed.AuthoringImpact.RequiredCapabilities, CapabilityPackageManage) {
		t.Fatalf("removed=%+v err=%v", removed, err)
	}
}
