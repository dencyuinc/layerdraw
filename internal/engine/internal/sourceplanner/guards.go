// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package sourceplanner

import ()

func requirePatchPreconditions(preconditions EngineEditPreconditions, patch SourcePatchBatch) ([]Diagnostic, []SemanticConflict) {
	for _, item := range patch.Patches {
		if !hasSourcePrecondition(preconditions, item.SourceRange.ModulePath) {
			return missingGuard("source patch requires an exact module source-digest precondition")
		}
	}
	return nil, nil
}

func requireFragmentPreconditions(preconditions EngineEditPreconditions, fragment FragmentInput, before Snapshot) ([]Diagnostic, []SemanticConflict) {
	ownerGuard := false
	for _, item := range preconditions.ExpectedSubtreeHashes {
		ownerGuard = ownerGuard || item.Address == fragment.InsertionOwner
	}
	for _, item := range preconditions.ExpectedChildSets {
		ownerGuard = ownerGuard || item.OwnerAddress == fragment.InsertionOwner
	}
	if fragment.Intent == "replace" && fragment.ReplacementTarget != nil {
		ownerGuard = false
		for _, item := range preconditions.ExpectedSubjectHashes {
			ownerGuard = ownerGuard || item.Address == *fragment.ReplacementTarget
		}
	}
	if !ownerGuard {
		return missingGuard("fragment requires an owner/target semantic precondition")
	}
	module := beforeModuleForFragment(before, fragment)
	if module == "" || !hasSourcePrecondition(preconditions, module) {
		return missingGuard("fragment requires an exact destination source-digest precondition")
	}
	return nil, nil
}

func requireFormatPreconditions(preconditions EngineEditPreconditions, addresses []StableAddress, before Snapshot) ([]Diagnostic, []SemanticConflict) {
	for _, address := range addresses {
		hash := false
		for _, item := range preconditions.ExpectedSubjectHashes {
			hash = hash || item.Address == address
		}
		module := ""
		for _, item := range before.SourceMap.Subjects {
			if item.Address == string(address) && item.Module != nil {
				module = item.Module.ModulePath
				break
			}
		}
		if !hash || module == "" || !hasSourcePrecondition(preconditions, module) {
			return missingGuard("format scope requires exact subject and module source preconditions")
		}
	}
	return nil, nil
}

func requireOrganizationPreconditions(preconditions EngineEditPreconditions, before Snapshot) ([]Diagnostic, []SemanticConflict) {
	for _, file := range before.SourceMap.Files {
		if file.Origin.Kind == "project" && !hasSourcePrecondition(preconditions, file.ModulePath) {
			return missingGuard("workspace organization requires every project module source digest")
		}
	}
	return nil, nil
}

func hasSourcePrecondition(preconditions EngineEditPreconditions, module string) bool {
	if preconditions.ExpectedSourceDigests == nil {
		return false
	}
	for _, item := range *preconditions.ExpectedSourceDigests {
		if item.Module.Origin.Kind == OriginKindProject && item.Module.ModulePath == module {
			return true
		}
	}
	return false
}

func beforeModuleForFragment(before Snapshot, fragment FragmentInput) string {
	if fragment.Placement != nil && fragment.Placement.ModulePath != nil {
		return string(*fragment.Placement.ModulePath)
	}
	address := fragment.InsertionOwner
	if fragment.ReplacementTarget != nil {
		address = *fragment.ReplacementTarget
	}
	for _, item := range before.SourceMap.Subjects {
		if item.Address == string(address) && item.Module != nil {
			return item.Module.ModulePath
		}
	}
	for _, file := range before.SourceMap.Files {
		if file.Origin.Kind == "project" {
			return file.ModulePath
		}
	}
	return ""
}

func missingGuard(message string) ([]Diagnostic, []SemanticConflict) {
	return diagnostics("LDL1801", "stale_revision_or_semantic_hash", message, nil), []SemanticConflict{{Kind: "stale_revision"}}
}
