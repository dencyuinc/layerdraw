// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"sort"
	"strings"
)

type RegistryProjectArtifactInput struct {
	Bytes          []byte
	RegistrySource string
}

type RegistryProjectMutationInput struct {
	Base               CompileInput
	Artifacts          []RegistryProjectArtifactInput
	RemoveCanonicalIDs []string
	ResourceLimits     ResourceLimits
}

type RegistryProjectMutation struct {
	Input           CompileInput
	Snapshot        Snapshot
	AuthoringImpact PlannedAuthoringImpact
}

// BuildRegistryProjectMutation applies an exact, already-resolved Pack delta
// to one closed CompileInput. It never reads a path, resolves a version, or
// downloads a dependency. Every added/updated Pack is re-read under the
// canonical archive policy before the resulting Project is compiled.
func (e Engine) BuildRegistryProjectMutation(ctx context.Context, in RegistryProjectMutationInput) (RegistryProjectMutation, error) {
	if in.Base.Mode != CompileProject || len(in.Base.ProjectSourceTree) == 0 {
		return RegistryProjectMutation{}, errors.New("Registry project baseline is invalid")
	}
	beforeResult, err := e.Compile(ctx, in.Base)
	if err != nil {
		return RegistryProjectMutation{}, err
	}
	before := beforeResult.Snapshot()
	if before.NormalizedDocument == nil || before.GraphHash == nil || hasErrorDiagnostics(before.Diagnostics) {
		return RegistryProjectMutation{}, errors.New("Registry project baseline is not publishable")
	}
	next := cloneRegistryCompileInput(in.Base)
	if next.InstalledPackTree == nil {
		next.InstalledPackTree = map[string][]byte{}
	}
	removed := map[string]bool{}
	for _, id := range in.RemoveCanonicalIDs {
		if id == "" || removed[id] {
			return RegistryProjectMutation{}, errors.New("Registry removal set is invalid")
		}
		removed[id] = true
	}
	installs := make(map[string]ResolvedPack, len(next.ResolvedDependencies.Installs)+len(in.Artifacts))
	for _, installed := range next.ResolvedDependencies.Installs {
		if !removed[installed.CanonicalID] {
			installs[installed.CanonicalID] = installed
		} else {
			removePackTree(next.InstalledPackTree, installed.Path)
		}
	}
	type stagedPack struct {
		artifact RegistryProjectArtifactInput
		pack     RegistryPackArtifact
	}
	staged := make([]stagedPack, 0, len(in.Artifacts))
	for _, artifact := range in.Artifacts {
		pack, readErr := e.ReadRegistryPack(ctx, artifact.Bytes, LayerdrawLimits{})
		if readErr != nil || pack.Manifest.ID == "" {
			return RegistryProjectMutation{}, errors.New("Registry Pack artifact is invalid")
		}
		if prior, exists := installs[pack.Manifest.ID]; exists {
			removePackTree(next.InstalledPackTree, prior.Path)
		}
		staged = append(staged, stagedPack{artifact: artifact, pack: pack})
		installs[pack.Manifest.ID] = ResolvedPack{CanonicalID: pack.Manifest.ID, InstallName: pack.Manifest.Name, Version: pack.Manifest.Version, Digest: rawDigest(artifact.Bytes), Path: path.Join("pack", pack.Manifest.Name), Entry: pack.Manifest.Entry, RegistrySource: artifact.RegistrySource, ManifestPath: "manifest.json", Manifest: append([]byte(nil), pack.Files["manifest.json"]...), Files: []ResolvedPackFile{}, Dependencies: []ResolvedPackDependency{}}
	}
	for _, stagedPack := range staged {
		resolved := installs[stagedPack.pack.Manifest.ID]
		filePaths := make([]string, 0, len(stagedPack.pack.Digests))
		for file := range stagedPack.pack.Digests {
			if path.Ext(file) == ".ldl" {
				filePaths = append(filePaths, file)
			}
		}
		sort.Strings(filePaths)
		for _, file := range filePaths {
			resolved.Files = append(resolved.Files, ResolvedPackFile{Path: file, Digest: stagedPack.pack.Digests[file]})
			next.InstalledPackTree[path.Join(resolved.Path, file)] = append([]byte(nil), stagedPack.pack.Files[file]...)
		}
		dependencyNames := make([]string, 0, len(stagedPack.pack.Manifest.Dependencies))
		for localName := range stagedPack.pack.Manifest.Dependencies {
			dependencyNames = append(dependencyNames, localName)
		}
		sort.Strings(dependencyNames)
		for _, localName := range dependencyNames {
			dependency := stagedPack.pack.Manifest.Dependencies[localName]
			target, ok := installs[dependency.ID]
			if !ok || target.Version != dependency.Version {
				return RegistryProjectMutation{}, errors.New("Registry Pack dependency closure is incomplete")
			}
			resolved.Dependencies = append(resolved.Dependencies, ResolvedPackDependency{LocalName: localName, InstallName: target.InstallName})
		}
		installs[resolved.CanonicalID] = resolved
	}
	next.ResolvedDependencies = ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: LayerdrawLanguage, Installs: make([]ResolvedPack, 0, len(installs))}
	for _, installed := range installs {
		next.ResolvedDependencies.Installs = append(next.ResolvedDependencies.Installs, installed)
	}
	sort.Slice(next.ResolvedDependencies.Installs, func(i, j int) bool {
		return next.ResolvedDependencies.Installs[i].InstallName < next.ResolvedDependencies.Installs[j].InstallName
	})
	afterResult, err := e.Compile(ctx, next)
	if err != nil {
		return RegistryProjectMutation{}, err
	}
	after := afterResult.Snapshot()
	if after.NormalizedDocument == nil || after.GraphHash == nil || hasErrorDiagnostics(after.Diagnostics) {
		return RegistryProjectMutation{}, errors.New("Registry project mutation is not publishable")
	}
	_, _, impact, err := BuildCanonicalAuthoringPlan(ctx, before, after, in.Base.ProjectSourceTree, next.ProjectSourceTree, SemanticPlanLimits{MaxItems: 1 << 20, MaxOutputBytes: 64 << 20})
	if err != nil {
		return RegistryProjectMutation{}, err
	}
	if !containsAuthoringCapability(impact.RequiredCapabilities, CapabilityPackageManage) {
		impact.RequiredCapabilities = append(impact.RequiredCapabilities, CapabilityPackageManage)
		sort.Slice(impact.RequiredCapabilities, func(i, j int) bool {
			return string(impact.RequiredCapabilities[i]) < string(impact.RequiredCapabilities[j])
		})
		impact.ImpactDigest = digestJSON(authoringImpactWireValue(impact, false))
	}
	return RegistryProjectMutation{Input: next, Snapshot: after, AuthoringImpact: impact}, nil
}

func removePackTree(tree map[string][]byte, prefix string) {
	prefix = strings.TrimSuffix(prefix, "/") + "/"
	for name := range tree {
		if strings.HasPrefix(name, prefix) {
			delete(tree, name)
		}
	}
}

func containsAuthoringCapability(values []AuthoringCapability, want AuthoringCapability) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func cloneRegistryCompileInput(input CompileInput) CompileInput {
	encoded, _ := json.Marshal(input)
	var result CompileInput
	_ = json.Unmarshal(encoded, &result)
	return result
}
