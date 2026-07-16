// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

var errSemanticConflict = errors.New("semantic edit conflict")

// PlanSemanticEdits applies one generated-equivalent semantic operation batch
// to a private copy of an immutable compiled source tree. It never publishes,
// stores, authorizes, or mutates a handle. A valid candidate is returned only
// after the complete canonical compiler accepts the resulting closed input.
func (e Engine) PlanSemanticEdits(ctx context.Context, input SemanticEditPlanInput) (SemanticEditPlan, error) {
	if ctx == nil {
		return SemanticEditPlan{}, invariantFailure(stageStart)
	}
	if err := semanticEditCancellation(ctx, "semantic_edit_start"); err != nil {
		return SemanticEditPlan{}, err
	}
	if input.BaseInput.Mode != CompileProject || len(input.Batch.Operations) == 0 {
		return invalidPlan(nil, []Diagnostic{plannerDiagnostic("LDL1801", "invalid_semantic_operation_batch", "semantic operations require a non-empty Project batch")}), nil
	}
	if input.Limits.MaxItems > 0 && int64(len(input.Batch.Operations)) > input.Limits.MaxItems {
		return SemanticEditPlan{}, semanticPlanLimitError("semantic_operations", input.Limits.MaxItems, int64(len(input.Batch.Operations)))
	}

	// Recompile rather than trusting a caller-assembled snapshot. This binds all
	// ranges, ownership, hashes, and semantic authority to the supplied bytes.
	baseResult, err := e.Compile(ctx, input.BaseInput)
	if err != nil {
		return SemanticEditPlan{}, err
	}
	head := baseResult.Snapshot()
	if len(head.Diagnostics) != 0 {
		return invalidPlan(nil, head.Diagnostics), nil
	}
	ancestor := input.BaseSnapshot
	if ancestor.DefinitionHash == "" {
		ancestor = head
	}
	if ancestor.DefinitionHash != head.DefinitionHash {
		if !validSemanticRebaseAuthority(input.RebaseAuthority, input.Generation, ancestor, head) {
			return invalidPlan([]SemanticConflict{{Kind: ConflictStaleRevision}}, nil), nil
		}
	}
	if conflicts := validateSemanticPreconditions(ancestor, input.Preconditions, input.Batch); len(conflicts) != 0 {
		return invalidPlan(conflicts, nil), nil
	}
	if ancestor.DefinitionHash != head.DefinitionHash {
		if conflicts := validateSemanticRebase(ancestor, head, input.Batch); len(conflicts) != 0 {
			return invalidPlan(conflicts, nil), nil
		}
	}

	candidateInput := cloneSemanticCompileInput(input.BaseInput)
	current := head
	for operationIndex, operation := range input.Batch.Operations {
		if err := semanticEditCancellation(ctx, "semantic_edit_operation"); err != nil {
			return SemanticEditPlan{}, err
		}
		beforeOperationTree := cloneSourceTree(candidateInput.ProjectSourceTree)
		conflict, diagnostic := applySemanticOperation(&candidateInput, current, operation)
		if conflict != nil {
			return invalidPlan([]SemanticConflict{*conflict}, nil), nil
		}
		if diagnostic != nil {
			if diagnostic.Arguments == nil {
				diagnostic.Arguments = map[string]string{}
			}
			diagnostic.Arguments["operation_index"] = strconv.Itoa(operationIndex)
			return invalidPlan(nil, []Diagnostic{*diagnostic}), nil
		}

		// Recompile every accepted overlay step. Besides allowing later operations
		// to address newly created subjects, this prevents an invalid intermediate
		// candidate from becoming authority for later target resolution.
		previous := current
		compiled, compileErr := e.Compile(ctx, candidateInput)
		if compileErr != nil {
			return SemanticEditPlan{}, compileErr
		}
		current = compiled.Snapshot()
		if len(current.Diagnostics) != 0 && operationIndex+1 < len(input.Batch.Operations) {
			// Atomic batches may be temporarily invalid (for example, deleting a
			// referenced schema before deleting its instances). Preserve the last
			// authoritative semantic index, but rebase all source ranges over the
			// private overlay so later existing targets remain addressable. Only the
			// complete final tree is accepted as semantic authority.
			current = rebaseSnapshotSourceRanges(previous, beforeOperationTree, candidateInput.ProjectSourceTree)
			continue
		}
		if len(current.Diagnostics) != 0 {
			for i := range current.Diagnostics {
				if current.Diagnostics[i].Arguments == nil {
					current.Diagnostics[i].Arguments = map[string]string{}
				}
				current.Diagnostics[i].Arguments["operation_index"] = strconv.Itoa(operationIndex)
			}
			return invalidPlan(nil, current.Diagnostics), nil
		}
	}

	if err := semanticEditCancellation(ctx, "semantic_edit_diff"); err != nil {
		return SemanticEditPlan{}, err
	}
	sourceDiff, semanticDiff, impact, derivedErr := BuildCanonicalAuthoringPlan(ctx, head, current, input.BaseInput.ProjectSourceTree, candidateInput.ProjectSourceTree, input.Limits)
	if derivedErr != nil {
		return SemanticEditPlan{}, derivedErr
	}
	changed := changedSourcePaths(input.BaseInput.ProjectSourceTree, candidateInput.ProjectSourceTree)
	return SemanticEditPlan{
		Status: "valid", ChangedSourceFiles: changed, SourceTree: cloneSourceTree(candidateInput.ProjectSourceTree),
		SourceDiff: sourceDiff, SemanticDiff: semanticDiff, AuthoringImpact: &impact, Result: &current,
		Conflicts: []SemanticConflict{}, Diagnostics: []Diagnostic{},
	}, nil
}

func validSemanticRebaseAuthority(authority *SemanticRebaseAuthority, requested SemanticDocumentGeneration, ancestor, head Snapshot) bool {
	if authority == nil || authority.AncestorDefinitionHash != ancestor.DefinitionHash || authority.CurrentDefinitionHash != head.DefinitionHash {
		return false
	}
	prior, current := authority.AncestorGeneration, authority.CurrentGeneration
	return prior.EndpointInstanceID != "" && prior.EndpointInstanceID == current.EndpointInstanceID &&
		prior.DocumentHandle != "" && prior.DocumentHandle == current.DocumentHandle &&
		prior.Value != "" && current.Value != "" && prior.Value != current.Value && requested == current
}

func semanticEditCancellation(ctx context.Context, stage string) error {
	if err := ctx.Err(); err != nil {
		return &CompileError{Code: ErrorCodeCancelled, Category: ErrorCategoryCancelled, Stage: stage, cause: err}
	}
	return nil
}

func validateSemanticRebase(ancestor, head Snapshot, batch SemanticOperationBatch) []SemanticConflict {
	ancestorSubjects := semanticSubjectsByAddress(ancestor)
	headSubjects := semanticSubjectsByAddress(head)
	conflicts := make([]SemanticConflict, 0)
	createdInBatch := map[string]bool{}
	validateAvailable := func(address string) {
		if !createdInBatch[address] {
			validateRebaseSubject(&conflicts, ancestorSubjects, headSubjects, address)
		}
	}
	var validateValueAddresses func(SemanticValue)
	validateValueAddresses = func(value SemanticValue) {
		if value.Kind == SemanticValueAddress {
			validateAvailable(value.Address)
		}
		for _, item := range value.Array {
			validateValueAddresses(item)
		}
		for _, item := range value.Map {
			validateValueAddresses(item.Value)
		}
	}
	for _, operation := range batch.Operations {
		target := operation.TargetAddress
		switch operation.Kind {
		case OperationUpdateSubjectField:
			if _, ok := headSubjects[target]; !ok {
				conflicts = append(conflicts, SemanticConflict{Kind: ConflictDeleteVsUpdate, TargetAddress: target, Path: append([]string(nil), operation.Path...)})
				continue
			}
			beforeValue, beforeOK := semanticObjectPath(normalizedSubject(ancestor, target), operation.Path)
			headValue, headOK := semanticObjectPath(normalizedSubject(head, target), operation.Path)
			if beforeOK != headOK || !reflect.DeepEqual(beforeValue, headValue) {
				conflicts = append(conflicts, SemanticConflict{Kind: ConflictSameFieldChanged, TargetAddress: target, Path: append([]string(nil), operation.Path...)})
			}
			if subject := ancestorSubjects[target]; subject.Kind == materialize.SubjectEntityRow || subject.Kind == materialize.SubjectRelationRow {
				validateRebaseRowSchema(&conflicts, ancestor, head, target)
			}
			if operation.Value != nil {
				validateValueAddresses(*operation.Value)
			}
		case OperationMoveEntityToLayer:
			validateRebaseField(&conflicts, ancestor, head, operation.EntityAddress, []string{"layer_address"})
			validateAvailable(operation.LayerAddress)
		case OperationUpdateRelationEndpoint:
			validateRebaseField(&conflicts, ancestor, head, operation.RelationAddress, []string{operation.Endpoint + "_address"})
			validateAvailable(operation.EntityAddress)
		case OperationDeleteSubject, OperationRenameSubject:
			if !createdInBatch[operation.TargetAddress] {
				validateRebaseSubtree(&conflicts, ancestor, head, operation.TargetAddress)
				validateRebaseDependencies(&conflicts, ancestor, head, operation.TargetAddress)
			}
		case OperationDeleteRow:
			validateRebaseSubject(&conflicts, ancestorSubjects, headSubjects, operation.RowAddress)
			validateRebaseRowSchema(&conflicts, ancestor, head, operation.RowAddress)
		case OperationMigrateProjectIdentity:
			validateRebaseSubtree(&conflicts, ancestor, head, operation.ProjectAddress)
		case OperationUpsertRow:
			validateAvailable(operation.OwnerAddress)
			kind := rowKindForOwner(ancestorSubjects[operation.OwnerAddress])
			address := predictedChildAddress(operation.OwnerAddress, kind, operation.ID)
			if _, existed := ancestorSubjects[address]; existed {
				validateRebaseSubject(&conflicts, ancestorSubjects, headSubjects, address)
				validateRebaseRowSchema(&conflicts, ancestor, head, address)
			} else if childSetHashFor(ancestor, operation.OwnerAddress, kind) != childSetHashFor(head, operation.OwnerAddress, kind) {
				conflicts = append(conflicts, SemanticConflict{Kind: ConflictChildSetChanged, OwnerAddress: operation.OwnerAddress, ChildKind: kind})
			}
			typeAddress, _ := normalizedSubject(ancestor, operation.OwnerAddress)["type_address"].(string)
			if typeAddress != "" {
				validateRebaseSubtree(&conflicts, ancestor, head, typeAddress)
			}
			for _, cell := range operation.Values {
				validateAvailable(cell.ColumnAddress)
				validateValueAddresses(cell.Value)
			}
		case OperationCreateSubject, OperationCreateRelation:
			kind := operation.SubjectKind
			if operation.Kind == OperationCreateRelation {
				kind = materialize.SubjectRelation
			}
			if childSetHashFor(ancestor, operation.ParentAddress, kind) != childSetHashFor(head, operation.ParentAddress, kind) {
				conflicts = append(conflicts, SemanticConflict{Kind: ConflictChildSetChanged, OwnerAddress: operation.ParentAddress, ChildKind: kind})
			}
			validateAvailable(operation.ParentAddress)
			if operation.Kind == OperationCreateRelation {
				for _, address := range []string{operation.TypeAddress, operation.FromAddress, operation.ToAddress} {
					validateAvailable(address)
				}
			}
			for _, field := range operation.Fields {
				validateValueAddresses(field.Value)
			}
			createdInBatch[predictedChildAddress(operation.ParentAddress, kind, operation.ID)] = true
		}
	}
	sortSemanticConflicts(conflicts)
	return uniqueSemanticConflicts(conflicts)
}

func validateRebaseSubtree(conflicts *[]SemanticConflict, ancestor, head Snapshot, address string) {
	before, beforeOK := subtreeHashFor(ancestor, address)
	after, afterOK := subtreeHashFor(head, address)
	if !beforeOK || !afterOK {
		validateRebaseSubject(conflicts, semanticSubjectsByAddress(ancestor), semanticSubjectsByAddress(head), address)
		return
	}
	if before != after {
		*conflicts = append(*conflicts, SemanticConflict{Kind: ConflictSubtreeChanged, TargetAddress: address})
	}
}

func subtreeHashFor(snapshot Snapshot, address string) (string, bool) {
	for _, subtree := range snapshot.SubtreeHashes {
		if subtree.OwnerAddress == address {
			return subtree.Hash, true
		}
	}
	return "", false
}

func validateRebaseDependencies(conflicts *[]SemanticConflict, ancestor, head Snapshot, target string) {
	priorSources, currentSources := referenceSources(ancestor, target), referenceSources(head, target)
	if !reflect.DeepEqual(priorSources, currentSources) {
		*conflicts = append(*conflicts, SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: target})
		return
	}
	prior, current := semanticSubjectsByAddress(ancestor), semanticSubjectsByAddress(head)
	for _, source := range priorSources {
		validateRebaseSubject(conflicts, prior, current, source)
	}
}

func referenceSources(snapshot Snapshot, target string) []string {
	set := map[string]bool{}
	for _, reference := range snapshot.SemanticIndex.References {
		if reference.TargetAddress == target {
			set[reference.SourceAddress] = true
		}
	}
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return compareStableAddressText(values[i], values[j]) < 0 })
	return values
}

func validateRebaseRowSchema(conflicts *[]SemanticConflict, ancestor, head Snapshot, rowAddress string) {
	owner := ownerAddressFor(ancestor, rowAddress)
	if owner == "" {
		return
	}
	validateRebaseSubject(conflicts, semanticSubjectsByAddress(ancestor), semanticSubjectsByAddress(head), owner)
	typeAddress, _ := normalizedSubject(ancestor, owner)["type_address"].(string)
	if typeAddress != "" {
		validateRebaseSubtree(conflicts, ancestor, head, typeAddress)
	}
}

func semanticSubjectsByAddress(snapshot Snapshot) map[string]index.SemanticSubject {
	out := make(map[string]index.SemanticSubject, len(snapshot.SemanticIndex.Subjects))
	for _, subject := range snapshot.SemanticIndex.Subjects {
		out[subject.Address] = subject
	}
	return out
}

func validateRebaseSubject(conflicts *[]SemanticConflict, ancestor, head map[string]index.SemanticSubject, address string) {
	prior, existed := ancestor[address]
	current, present := head[address]
	if !existed || !present {
		*conflicts = append(*conflicts, SemanticConflict{Kind: ConflictDeleteVsUpdate, TargetAddress: address})
	} else if prior.OwnHash != current.OwnHash {
		*conflicts = append(*conflicts, SemanticConflict{Kind: ConflictSubjectChanged, TargetAddress: address})
	}
}

func validateRebaseField(conflicts *[]SemanticConflict, ancestor, head Snapshot, address string, path []string) {
	if normalizedSubject(head, address) == nil {
		*conflicts = append(*conflicts, SemanticConflict{Kind: ConflictDeleteVsUpdate, TargetAddress: address, Path: path})
		return
	}
	beforeValue, beforeOK := semanticObjectPath(normalizedSubject(ancestor, address), path)
	afterValue, afterOK := semanticObjectPath(normalizedSubject(head, address), path)
	if beforeOK != afterOK || !reflect.DeepEqual(beforeValue, afterValue) {
		*conflicts = append(*conflicts, SemanticConflict{Kind: ConflictSameFieldChanged, TargetAddress: address, Path: path})
	}
}

func semanticObjectPath(object map[string]any, path []string) (any, bool) {
	var current any = object
	for _, token := range path {
		mapping, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mapping[token]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func childSetHashFor(snapshot Snapshot, owner string, kind SemanticSubjectKind) string {
	for _, childSet := range snapshot.ChildSetHashes {
		if childSet.OwnerAddress == owner && childSet.ChildKind == kind {
			return childSet.Hash
		}
	}
	return ""
}

func invalidPlan(conflicts []SemanticConflict, diagnostics []Diagnostic) SemanticEditPlan {
	if conflicts == nil {
		conflicts = []SemanticConflict{}
	}
	if diagnostics == nil {
		diagnostics = []Diagnostic{}
	}
	sortSemanticConflicts(conflicts)
	emptySource := PlannedSourceDiff{Edits: []PlannedSourceEdit{}}
	emptySource.Digest = digestJSON(emptySource.Edits)
	emptySemantic := PlannedSemanticDiff{Entries: []SemanticDiffEntry{}}
	emptySemantic.Digest = digestJSON(emptySemantic.Entries)
	return SemanticEditPlan{Status: "invalid", SourceTree: map[string][]byte{}, ChangedSourceFiles: []string{}, SourceDiff: emptySource, SemanticDiff: emptySemantic, Conflicts: conflicts, Diagnostics: diagnostics}
}

func plannerDiagnostic(code, key, message string) Diagnostic {
	return Diagnostic{Code: code, Severity: "error", MessageKey: key, Message: message, Arguments: map[string]string{}, Related: []resolve.DiagnosticRelated{}}
}

func cloneSemanticCompileInput(input CompileInput) CompileInput {
	copy := input
	copy.ProjectSourceTree = cloneSourceTree(input.ProjectSourceTree)
	copy.InstalledPackTree = cloneSourceTree(input.InstalledPackTree)
	copy.ReferencedAssets = append([]AssetInput(nil), input.ReferencedAssets...)
	for i := range copy.ReferencedAssets {
		copy.ReferencedAssets[i].Bytes = bytes.Clone(input.ReferencedAssets[i].Bytes)
	}
	return copy
}

func cloneSourceTree(input map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(input))
	for path, source := range input {
		out[path] = bytes.Clone(source)
	}
	return out
}

func semanticDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestJSON(value any) string {
	data, err := materialize.Canonicalize(value)
	if err != nil {
		return ""
	}
	return semanticDigest(data)
}

func validateSemanticPreconditions(snapshot Snapshot, pre SemanticEditPreconditions, batch SemanticOperationBatch) []SemanticConflict {
	subjects := make(map[string]index.SemanticSubject, len(snapshot.SemanticIndex.Subjects))
	for _, subject := range snapshot.SemanticIndex.Subjects {
		subjects[subject.Address] = subject
	}
	subtrees := make(map[string]string, len(snapshot.SubtreeHashes))
	for _, subtree := range snapshot.SubtreeHashes {
		subtrees[subtree.OwnerAddress] = subtree.Hash
	}
	childSets := make(map[string]string, len(snapshot.ChildSetHashes))
	for _, childSet := range snapshot.ChildSetHashes {
		childSets[childSet.OwnerAddress+"\x00"+string(childSet.ChildKind)] = childSet.Hash
	}
	files := make(map[string]string, len(snapshot.SourceMap.Files))
	for _, file := range snapshot.SourceMap.Files {
		module := PlannedModuleRef{OriginKind: SourceOriginKind(file.Origin.Kind), PackAddress: file.Origin.PackAddress, ModulePath: file.ModulePath}
		files[semanticModuleIdentity(module)] = file.Digest
	}
	providedSubjects := map[string]string{}
	providedSubtrees := map[string]string{}
	providedChildren := map[string]string{}
	var conflicts []SemanticConflict
	for _, expected := range pre.ExpectedSubjectHashes {
		providedSubjects[expected.Address] = expected.Hash
		actual, ok := subjects[expected.Address]
		if !ok || actual.OwnHash != expected.Hash {
			conflicts = append(conflicts, SemanticConflict{Kind: ConflictSubjectChanged, TargetAddress: expected.Address})
		}
	}
	for _, expected := range pre.ExpectedSubtreeHashes {
		providedSubtrees[expected.Address] = expected.Hash
		if subtrees[expected.Address] != expected.Hash {
			conflicts = append(conflicts, SemanticConflict{Kind: ConflictSubtreeChanged, TargetAddress: expected.Address})
		}
	}
	for _, expected := range pre.ExpectedChildSets {
		key := expected.OwnerAddress + "\x00" + string(expected.ChildKind)
		providedChildren[key] = expected.Hash
		if childSets[key] != expected.Hash {
			conflicts = append(conflicts, SemanticConflict{Kind: ConflictChildSetChanged, OwnerAddress: expected.OwnerAddress, ChildKind: expected.ChildKind})
		}
	}
	for _, expected := range pre.ExpectedSourceDigests {
		if files[semanticModuleIdentity(expected.Module)] != expected.Digest {
			conflicts = append(conflicts, SemanticConflict{Kind: ConflictSubjectChanged, Path: []string{string(expected.Module.OriginKind), expected.Module.PackAddress, expected.Module.ModulePath}})
		}
	}
	if len(conflicts) != 0 {
		sortSemanticConflicts(conflicts)
		return uniqueSemanticConflicts(conflicts)
	}

	requireSubject := func(address string) {
		if address == "" {
			return
		}
		if _, created := subjects[address]; !created {
			return
		}
		if _, ok := providedSubjects[address]; !ok {
			conflicts = append(conflicts, SemanticConflict{Kind: ConflictSubjectChanged, TargetAddress: address})
		}
	}
	requireSubtree := func(address string) {
		if address == "" {
			return
		}
		if _, ok := providedSubtrees[address]; !ok {
			conflicts = append(conflicts, SemanticConflict{Kind: ConflictSubtreeChanged, TargetAddress: address})
		}
	}
	createdInBatch := map[string]bool{}
	createdKinds := map[string]SemanticSubjectKind{}
	requireAvailableSubject := func(address string) {
		if !createdInBatch[address] {
			requireSubject(address)
		}
	}
	var requireValueAddresses func(SemanticValue)
	requireValueAddresses = func(value SemanticValue) {
		if value.Kind == SemanticValueAddress {
			requireAvailableSubject(value.Address)
		}
		for _, item := range value.Array {
			requireValueAddresses(item)
		}
		for _, item := range value.Map {
			requireValueAddresses(item.Value)
		}
	}
	requireChild := func(owner string, kind SemanticSubjectKind) {
		if createdInBatch[owner] {
			return
		}
		key := owner + "\x00" + string(kind)
		if _, ok := providedChildren[key]; !ok {
			conflicts = append(conflicts, SemanticConflict{Kind: ConflictChildSetChanged, OwnerAddress: owner, ChildKind: kind})
		}
	}
	for _, operation := range batch.Operations {
		switch operation.Kind {
		case OperationCreateSubject:
			requireAvailableSubject(operation.ParentAddress)
			requireChild(operation.ParentAddress, operation.SubjectKind)
			for _, field := range operation.Fields {
				requireValueAddresses(field.Value)
			}
			createdAddress := predictedChildAddress(operation.ParentAddress, operation.SubjectKind, operation.ID)
			createdInBatch[createdAddress], createdKinds[createdAddress] = true, operation.SubjectKind
		case OperationCreateRelation:
			requireAvailableSubject(operation.ParentAddress)
			requireChild(operation.ParentAddress, materialize.SubjectRelation)
			for _, address := range []string{operation.TypeAddress, operation.FromAddress, operation.ToAddress} {
				requireAvailableSubject(address)
			}
			for _, field := range operation.Fields {
				requireValueAddresses(field.Value)
			}
			createdAddress := predictedChildAddress(operation.ParentAddress, materialize.SubjectRelation, operation.ID)
			createdInBatch[createdAddress], createdKinds[createdAddress] = true, materialize.SubjectRelation
		case OperationUpsertRow:
			requireAvailableSubject(operation.OwnerAddress)
			ownerSubject := subjects[operation.OwnerAddress]
			if kind := createdKinds[operation.OwnerAddress]; kind != "" {
				ownerSubject.Kind = kind
			}
			rowKind := rowKindForOwner(ownerSubject)
			rowAddress := predictedChildAddress(operation.OwnerAddress, rowKind, operation.ID)
			if createdInBatch[rowAddress] {
				continue
			} else if _, ok := subjects[rowAddress]; ok {
				requireSubject(rowAddress)
			} else {
				requireChild(operation.OwnerAddress, rowKind)
				createdInBatch[rowAddress] = true
			}
			if !createdInBatch[operation.OwnerAddress] {
				typeAddress, _ := normalizedSubject(snapshot, operation.OwnerAddress)["type_address"].(string)
				requireSubtree(typeAddress)
			}
			for _, cell := range operation.Values {
				requireAvailableSubject(cell.ColumnAddress)
				requireValueAddresses(cell.Value)
			}
		case OperationDeleteRow:
			if createdInBatch[operation.RowAddress] {
				continue
			}
			requireSubject(operation.RowAddress)
			if subject, ok := subjects[operation.RowAddress]; ok && subject.OwnerAddress != nil {
				requireChild(*subject.OwnerAddress, subject.Kind)
				requireSubject(*subject.OwnerAddress)
				typeAddress, _ := normalizedSubject(snapshot, *subject.OwnerAddress)["type_address"].(string)
				requireSubtree(typeAddress)
			}
		case OperationDeleteSubject, OperationRenameSubject:
			if createdInBatch[operation.TargetAddress] {
				continue
			}
			requireSubject(operation.TargetAddress)
			if subject, ok := subjects[operation.TargetAddress]; ok {
				owner := ownerForSubject(snapshot, subject)
				requireChild(owner, subject.Kind)
				if subject.SubtreeHash != nil {
					requireSubtree(operation.TargetAddress)
				}
				for _, ref := range snapshot.SemanticIndex.References {
					if ref.TargetAddress == operation.TargetAddress {
						requireSubject(ref.SourceAddress)
					}
				}
			}
		case OperationMigrateProjectIdentity:
			requireSubtree(operation.ProjectAddress)
		case OperationUpdateRelationEndpoint:
			requireSubject(operation.RelationAddress)
			requireAvailableSubject(operation.EntityAddress)
		case OperationMoveEntityToLayer:
			requireSubject(operation.EntityAddress)
			requireAvailableSubject(operation.LayerAddress)
		case OperationUpdateSubjectField:
			requireSubject(operation.TargetAddress)
			if operation.Value != nil {
				requireValueAddresses(*operation.Value)
			}
			if len(operation.Path) == 2 && operation.Path[0] == "values" {
				requireAvailableSubject(operation.Path[1])
				if subject, ok := subjects[operation.TargetAddress]; ok && subject.OwnerAddress != nil {
					requireSubject(*subject.OwnerAddress)
					typeAddress, _ := normalizedSubject(snapshot, *subject.OwnerAddress)["type_address"].(string)
					requireSubtree(typeAddress)
				}
			}
		}
	}
	sortSemanticConflicts(conflicts)
	return uniqueSemanticConflicts(conflicts)
}

func semanticModuleIdentity(module PlannedModuleRef) string {
	return string(module.OriginKind) + "\x00" + module.PackAddress + "\x00" + module.ModulePath
}

func ownerForSubject(snapshot Snapshot, subject index.SemanticSubject) string {
	if subject.OwnerAddress != nil {
		return *subject.OwnerAddress
	}
	return rootProjectAddress(snapshot)
}

func rowKindForOwner(subject index.SemanticSubject) SemanticSubjectKind {
	if subject.Kind == materialize.SubjectRelation {
		return materialize.SubjectRelationRow
	}
	return materialize.SubjectEntityRow
}

func predictedChildAddress(owner string, kind SemanticSubjectKind, id string) string {
	segment := string(kind)
	if kind == materialize.SubjectEntityRow || kind == materialize.SubjectRelationRow {
		segment = "row"
	}
	segment = strings.ReplaceAll(segment, "_", "-")
	return owner + ":" + segment + ":" + id
}

func sortSemanticConflicts(conflicts []SemanticConflict) {
	order := map[SemanticConflictKind]int{
		ConflictStaleRevision: 0, ConflictSubjectChanged: 1, ConflictSubtreeChanged: 2, ConflictChildSetChanged: 3,
		ConflictSameFieldChanged: 4, ConflictDeleteVsUpdate: 5, ConflictDuplicateIdentity: 6, ConflictReferenceBroken: 7,
		ConflictSchemaRowIncompatible: 8, ConflictPlacementChanged: 9, ConflictProjectIdentityChanged: 10,
	}
	sort.SliceStable(conflicts, func(i, j int) bool {
		a, b := conflicts[i], conflicts[j]
		if order[a.Kind] != order[b.Kind] {
			return order[a.Kind] < order[b.Kind]
		}
		if a.TargetAddress != b.TargetAddress {
			if a.TargetAddress == "" || b.TargetAddress == "" {
				return a.TargetAddress < b.TargetAddress
			}
			return compareStableAddressText(a.TargetAddress, b.TargetAddress) < 0
		}
		if a.OwnerAddress != b.OwnerAddress {
			if a.OwnerAddress == "" || b.OwnerAddress == "" {
				return a.OwnerAddress < b.OwnerAddress
			}
			return compareStableAddressText(a.OwnerAddress, b.OwnerAddress) < 0
		}
		if a.ChildKind != b.ChildKind {
			return semanticSubjectKindOrder(a.ChildKind) < semanticSubjectKindOrder(b.ChildKind)
		}
		return strings.Join(a.Path, "\x00") < strings.Join(b.Path, "\x00")
	})
}

func semanticSubjectKindOrder(kind SemanticSubjectKind) int {
	order := map[SemanticSubjectKind]int{
		"": -1, materialize.SubjectProject: 0, materialize.SubjectPack: 1, materialize.SubjectEntityType: 2,
		materialize.SubjectRelationType: 3, materialize.SubjectLayer: 4, materialize.SubjectEntity: 5,
		materialize.SubjectRelation: 6, materialize.SubjectQuery: 7, materialize.SubjectView: 8,
		materialize.SubjectReference: 9, materialize.SubjectEntityTypeColumn: 10,
		materialize.SubjectEntityTypeConstraint: 11, materialize.SubjectRelationTypeColumn: 12,
		materialize.SubjectRelationTypeConstraint: 13, materialize.SubjectEntityRow: 14,
		materialize.SubjectRelationRow:    15,
		materialize.SubjectQueryParameter: 16, materialize.SubjectViewTableColumn: 17,
		materialize.SubjectViewExport: 18,
	}
	if rank, ok := order[kind]; ok {
		return rank
	}
	return 99
}

func uniqueSemanticConflicts(input []SemanticConflict) []SemanticConflict {
	out := input[:0]
	last := ""
	for _, conflict := range input {
		key := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s", conflict.Kind, conflict.TargetAddress, conflict.OwnerAddress, conflict.ChildKind, strings.Join(conflict.Path, "\x00"))
		if key != last {
			out = append(out, conflict)
			last = key
		}
	}
	return out
}

func applySemanticOperation(input *CompileInput, snapshot Snapshot, operation SemanticOperation) (*SemanticConflict, *Diagnostic) {
	if operation.Kind == "" {
		d := plannerDiagnostic("LDL1801", "invalid_semantic_operation", "missing semantic operation discriminator")
		return nil, &d
	}
	switch operation.Kind {
	case OperationRenameSubject:
		return applyRename(input, snapshot, operation.TargetAddress, operation.NewID, false)
	case OperationMigrateProjectIdentity:
		return applyRename(input, snapshot, operation.ProjectAddress, operation.NewProjectID, true)
	case OperationDeleteSubject:
		return applyDelete(input, snapshot, operation.TargetAddress)
	case OperationDeleteRow:
		return applyDelete(input, snapshot, operation.RowAddress)
	case OperationUpdateRelationEndpoint:
		return applyRelationEndpoint(input, snapshot, operation)
	case OperationMoveEntityToLayer:
		return applyMoveEntity(input, snapshot, operation)
	case OperationUpdateSubjectField:
		return applyUpdateField(input, snapshot, operation)
	case OperationCreateRelation:
		return applyCreateRelation(input, snapshot, operation)
	case OperationUpsertRow:
		return applyUpsertRow(input, snapshot, operation)
	case OperationCreateSubject:
		return applyCreateSubject(input, snapshot, operation)
	default:
		d := plannerDiagnostic("LDL1801", "invalid_semantic_operation", "unknown semantic operation")
		return nil, &d
	}
}

func subjectRecord(snapshot Snapshot, address string) (index.SemanticSubject, index.SourceSubjectRecord, bool) {
	var semantic index.SemanticSubject
	found := false
	for _, subject := range snapshot.SemanticIndex.Subjects {
		if subject.Address == address {
			semantic = subject
			found = true
			break
		}
	}
	if !found {
		return semantic, index.SourceSubjectRecord{}, false
	}
	for _, source := range snapshot.SourceMap.Subjects {
		if source.Address == address {
			return semantic, source, source.Module != nil && source.DeclarationRange != nil
		}
	}
	return semantic, index.SourceSubjectRecord{}, false
}

func applyDelete(input *CompileInput, snapshot Snapshot, address string) (*SemanticConflict, *Diagnostic) {
	return applyDeleteWithReservation(input, snapshot, address, true)
}

func applyDeleteWithReservation(input *CompileInput, snapshot Snapshot, address string, reserve bool) (*SemanticConflict, *Diagnostic) {
	semantic, source, ok := subjectRecord(snapshot, address)
	if !ok {
		return &SemanticConflict{Kind: ConflictDeleteVsUpdate, TargetAddress: address}, nil
	}
	if source.Module.Origin.Kind != "project" {
		return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: address}, nil
	}
	path := source.Module.ModulePath
	data := input.ProjectSourceTree[path]
	start, end := source.DeclarationRange.StartByte, source.DeclarationRange.EndByte
	if semantic.Kind == materialize.SubjectLayer || semantic.Kind == materialize.SubjectEntity || semantic.Kind == materialize.SubjectRelation || semantic.Kind == materialize.SubjectEntityRow || semantic.Kind == materialize.SubjectRelationRow {
		if groupStart, groupEnd, only := soleFactGroupMemberRange(snapshot, source, semantic.Kind); only {
			start, end = groupStart, groupEnd
		}
	}
	start, end = extendWholeLine(data, start, end)
	beforeTree := cloneSourceTree(input.ProjectSourceTree)
	replaceSourceRange(input, path, start, end, nil)
	if reserve {
		rebased := rebaseSnapshotSourceRanges(snapshot, beforeTree, input.ProjectSourceTree)
		if !materializeDeletionReservation(input, rebased, semantic, address) {
			return &SemanticConflict{Kind: ConflictPlacementChanged, TargetAddress: address}, nil
		}
	}
	return nil, nil
}

func soleFactGroupMemberRange(snapshot Snapshot, source index.SourceSubjectRecord, kind SemanticSubjectKind) (int, int, bool) {
	for _, file := range snapshot.LosslessSyntaxTree.Files {
		if file.ModulePath != source.Module.ModulePath || file.Origin.Kind != source.Module.Origin.Kind {
			continue
		}
		var declaration *syntax.Node
		syntax.Walk(file.Root, func(node *syntax.Node) {
			if node.Kind == syntax.NodeDeclaration && node.Span.Start <= source.DeclarationRange.StartByte && node.Span.End >= source.DeclarationRange.EndByte {
				if declaration == nil || node.Span.End-node.Span.Start < declaration.Span.End-declaration.Span.Start {
					declaration = node
				}
			}
		})
		if declaration == nil {
			return 0, 0, false
		}
		members := 0
		for _, candidate := range snapshot.SourceMap.Subjects {
			if candidate.Kind == kind && candidate.Module != nil && candidate.Module.ModulePath == source.Module.ModulePath && candidate.DeclarationRange != nil && candidate.DeclarationRange.StartByte >= declaration.Span.Start && candidate.DeclarationRange.EndByte <= declaration.Span.End {
				members++
			}
		}
		return declaration.Span.Start, declaration.Span.End, members == 1
	}
	return 0, 0, false
}

func applyRename(input *CompileInput, snapshot Snapshot, address, newID string, project bool) (*SemanticConflict, *Diagnostic) {
	semantic, source, ok := subjectRecord(snapshot, address)
	if !ok {
		return &SemanticConflict{Kind: ConflictDeleteVsUpdate, TargetAddress: address}, nil
	}
	if source.Module.Origin.Kind != "project" || newID == "" {
		return &SemanticConflict{Kind: ConflictProjectIdentityChanged, TargetAddress: address}, nil
	}
	if !project && semantic.Kind == materialize.SubjectProject {
		return &SemanticConflict{Kind: ConflictProjectIdentityChanged, TargetAddress: address}, nil
	}
	for _, subject := range snapshot.SemanticIndex.Subjects {
		if subject.Address == renamedAddress(address, newID) {
			return &SemanticConflict{Kind: ConflictDuplicateIdentity, TargetAddress: address}, nil
		}
	}
	type replacement struct {
		path       string
		start, end int
		text       string
	}
	var replacements []replacement
	path := source.Module.ModulePath
	idSpan, ok := declarationIDSpan(snapshot, source, semantic.Kind)
	if !ok {
		return &SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: address}, nil
	}
	replacements = append(replacements, replacement{path: path, start: idSpan.Start, end: idSpan.End, text: newID})
	if !project {
		for _, binding := range snapshot.SourceMap.Bindings {
			if binding.TargetAddress != address || binding.Module.Origin.Kind != "project" {
				continue
			}
			replacements = append(replacements, replacement{path: binding.Module.ModulePath, start: binding.Range.StartByte, end: binding.Range.EndByte, text: replaceTerminalBinding(input.ProjectSourceTree[binding.Module.ModulePath][binding.Range.StartByte:binding.Range.EndByte], newID)})
		}
		// Row owner identifiers participate in row identity rather than ordinary
		// reference binding extraction. Resolve them through the declared owner
		// relationship and replace only that first row token.
		for _, row := range snapshot.SemanticIndex.Subjects {
			if row.OwnerAddress == nil || *row.OwnerAddress != address || (row.Kind != materialize.SubjectEntityRow && row.Kind != materialize.SubjectRelationRow) {
				continue
			}
			_, rowSource, present := subjectRecord(snapshot, row.Address)
			if !present {
				continue
			}
			if ownerSpan, present := declarationFirstIdentifierSpan(snapshot, rowSource); present {
				replacements = append(replacements, replacement{path: rowSource.Module.ModulePath, start: ownerSpan.Start, end: ownerSpan.End, text: newID})
			}
		}
	}
	sort.SliceStable(replacements, func(i, j int) bool {
		if replacements[i].path != replacements[j].path {
			return replacements[i].path > replacements[j].path
		}
		return replacements[i].start > replacements[j].start
	})
	for _, edit := range replacements {
		replaceSourceRange(input, edit.path, edit.start, edit.end, []byte(edit.text))
	}
	if !materializeRenameMove(input, snapshot, semantic, address, newID, project) {
		return &SemanticConflict{Kind: ConflictPlacementChanged, TargetAddress: address}, nil
	}
	return nil, nil
}

func materializeDeletionReservation(input *CompileInput, snapshot Snapshot, subject index.SemanticSubject, address string) bool {
	id := terminalID(address)
	category := reservationCategory(subject.Kind)
	if category == "" {
		return false
	}
	if subject.Kind == materialize.SubjectEntityRow || subject.Kind == materialize.SubjectRelationRow {
		if subject.OwnerAddress == nil {
			return false
		}
		_, owner, ok := subjectRecord(snapshot, *subject.OwnerAddress)
		return ok && upsertOwnerIdentifierList(input, snapshot, owner, "", "reserve_rows", id)
	}
	if !isTopLevelReservationKind(subject.Kind) && subject.OwnerAddress != nil {
		_, owner, ok := subjectRecord(snapshot, *subject.OwnerAddress)
		return ok && upsertOwnerIdentifierList(input, snapshot, owner, "reserve", category, id)
	}
	return upsertTopLevelIdentifierList(input, input.EntryPath, "reserved", category, id)
}

func isTopLevelReservationKind(kind SemanticSubjectKind) bool {
	switch kind {
	case materialize.SubjectEntityType, materialize.SubjectRelationType, materialize.SubjectLayer, materialize.SubjectEntity, materialize.SubjectRelation, materialize.SubjectQuery, materialize.SubjectView, materialize.SubjectReference:
		return true
	default:
		return false
	}
}

func reservationCategory(kind SemanticSubjectKind) string {
	switch kind {
	case materialize.SubjectEntityType:
		return "entity_types"
	case materialize.SubjectRelationType:
		return "relation_types"
	case materialize.SubjectLayer:
		return "layers"
	case materialize.SubjectEntity:
		return "entities"
	case materialize.SubjectRelation:
		return "relations"
	case materialize.SubjectQuery:
		return "queries"
	case materialize.SubjectView:
		return "views"
	case materialize.SubjectReference:
		return "references"
	case materialize.SubjectEntityTypeColumn, materialize.SubjectRelationTypeColumn:
		return "columns"
	case materialize.SubjectEntityTypeConstraint, materialize.SubjectRelationTypeConstraint:
		return "constraints"
	case materialize.SubjectQueryParameter:
		return "parameters"
	case materialize.SubjectViewTableColumn:
		return "table_columns"
	case materialize.SubjectViewExport:
		return "exports"
	case materialize.SubjectEntityRow, materialize.SubjectRelationRow:
		return "rows"
	default:
		return ""
	}
}

func materializeRenameMove(input *CompileInput, snapshot Snapshot, subject index.SemanticSubject, address, newID string, project bool) bool {
	kind := moveSourceKind(subject.Kind)
	if project {
		kind = "project"
	}
	if kind == "" {
		return false
	}
	parts := []string{kind}
	if !isTopLevelReservationKind(subject.Kind) && subject.OwnerAddress != nil {
		parts = append(parts, terminalID(*subject.OwnerAddress))
	}
	parts = append(parts, terminalID(address), "->", newID)
	return appendTopLevelBlockEntry(input, input.EntryPath, "moves", strings.Join(parts, " "))
}

func moveSourceKind(kind SemanticSubjectKind) string {
	switch kind {
	case materialize.SubjectEntityType:
		return "entity_type"
	case materialize.SubjectRelationType:
		return "relation_type"
	case materialize.SubjectLayer:
		return "layer"
	case materialize.SubjectEntity:
		return "entity"
	case materialize.SubjectRelation:
		return "relation"
	case materialize.SubjectQuery:
		return "query"
	case materialize.SubjectView:
		return "view"
	case materialize.SubjectReference:
		return "reference"
	case materialize.SubjectEntityTypeColumn:
		return "entity_type_column"
	case materialize.SubjectRelationTypeColumn:
		return "relation_type_column"
	case materialize.SubjectEntityTypeConstraint:
		return "entity_type_constraint"
	case materialize.SubjectRelationTypeConstraint:
		return "relation_type_constraint"
	case materialize.SubjectEntityRow:
		return "entity_row"
	case materialize.SubjectRelationRow:
		return "relation_row"
	case materialize.SubjectQueryParameter:
		return "query_parameter"
	case materialize.SubjectViewTableColumn:
		return "view_table_column"
	case materialize.SubjectViewExport:
		return "view_export"
	default:
		return ""
	}
}

func upsertTopLevelIdentifierList(input *CompileInput, module, block, category, id string) bool {
	data, ok := input.ProjectSourceTree[module]
	if !ok {
		return false
	}
	if open, close, found := findNamedBlock(data, 0, len(data), block); found {
		return upsertIdentifierList(input, module, open, close, category, id, lineIndent(data, open)+"  ")
	}
	appendCanonicalIdentityBlock(input, module, block, block+" {\n  "+category+" ["+id+"]\n}\n")
	return true
}

func upsertOwnerIdentifierList(input *CompileInput, snapshot Snapshot, owner index.SourceSubjectRecord, block, category, id string) bool {
	path := owner.Module.ModulePath
	data := input.ProjectSourceTree[path]
	start, end := owner.DeclarationRange.StartByte, owner.DeclarationRange.EndByte
	if block == "" {
		return insertOrUpdateOwnerStatement(input, data, path, start, end, category, id)
	}
	if open, close, found := findNamedBlock(data, start, end, block); found {
		return upsertIdentifierList(input, path, open, close, category, id, lineIndent(data, open)+"  ")
	}
	close := bytes.LastIndexByte(data[start:end], '}')
	if close < 0 {
		return false
	}
	close += start
	indent := lineIndent(data, start) + "  "
	text := "\n" + indent + block + " {\n" + indent + "  " + category + " [" + id + "]\n" + indent + "}"
	replaceSourceRange(input, path, close, close, []byte(text))
	return true
}

func insertOrUpdateOwnerStatement(input *CompileInput, data []byte, path string, start, end int, statement, id string) bool {
	for _, span := range lineSpans(data, start, end) {
		line := strings.TrimSpace(string(data[span[0]:span[1]]))
		if strings.HasPrefix(line, statement+" [") {
			return replaceIdentifierListLine(input, data, path, span[0], span[1], statement, id)
		}
	}
	close := bytes.LastIndexByte(data[start:end], '}')
	if close < 0 {
		indent := lineIndent(data, start)
		replaceSourceRange(input, path, end, end, []byte(" {\n"+indent+"  "+statement+" ["+id+"]\n"+indent+"}"))
		return true
	}
	close += start
	indent := lineIndent(data, start) + "  "
	replaceSourceRange(input, path, close, close, []byte(indent+statement+" ["+id+"]\n"))
	return true
}

func upsertIdentifierList(input *CompileInput, path string, open, close int, category, id, indent string) bool {
	data := input.ProjectSourceTree[path]
	insertAt := close
	for _, span := range lineSpans(data, open+1, close) {
		line := strings.TrimSpace(string(data[span[0]:span[1]]))
		if strings.HasPrefix(line, category+" [") {
			return replaceIdentifierListLine(input, data, path, span[0], span[1], category, id)
		}
		if fields := strings.Fields(line); len(fields) > 0 && reservationCategoryRank(fields[0]) > reservationCategoryRank(category) && insertAt == close {
			insertAt = span[0]
		}
	}
	replaceSourceRange(input, path, insertAt, insertAt, []byte(indent+category+" ["+id+"]\n"))
	return true
}

func reservationCategoryRank(category string) int {
	order := []string{"entity_types", "relation_types", "layers", "entities", "relations", "queries", "views", "references", "columns", "constraints", "parameters", "table_columns", "exports", "rows"}
	for index, candidate := range order {
		if candidate == category {
			return index
		}
	}
	return len(order)
}

func replaceIdentifierListLine(input *CompileInput, data []byte, path string, start, end int, category, id string) bool {
	raw := string(data[start:end])
	left, right := strings.Index(raw, "["), strings.LastIndex(raw, "]")
	if left < 0 || right < left {
		return false
	}
	set := map[string]bool{id: true}
	for _, value := range strings.Fields(strings.ReplaceAll(raw[left+1:right], ",", " ")) {
		set[value] = true
	}
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	sort.Strings(values)
	replacement := raw[:left+1] + strings.Join(values, ", ") + raw[right:]
	replaceSourceRange(input, path, start, end, []byte(replacement))
	return true
}

func appendTopLevelBlockEntry(input *CompileInput, module, block, entry string) bool {
	data, ok := input.ProjectSourceTree[module]
	if !ok {
		return false
	}
	if open, close, found := findNamedBlock(data, 0, len(data), block); found {
		indent := lineIndent(data, open) + "  "
		insertAt := close
		entryKey := moveEntrySortKey(entry)
		for _, span := range lineSpans(data, open+1, close) {
			line := strings.TrimSpace(string(data[span[0]:span[1]]))
			if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "///") {
				continue
			}
			if moveEntrySortKey(line) > entryKey {
				insertAt = span[0]
				break
			}
		}
		replaceSourceRange(input, module, insertAt, insertAt, []byte(indent+entry+"\n"))
		return true
	}
	appendCanonicalIdentityBlock(input, module, block, block+" {\n  "+entry+"\n}\n")
	return true
}

func appendCanonicalIdentityBlock(input *CompileInput, module, block, declaration string) {
	data := input.ProjectSourceTree[module]
	other := "reserved"
	if block == "reserved" {
		other = "moves"
	}
	start, end, found := namedBlockDeclarationSpan(data, other)
	if !found {
		appendText(input, module, "\n"+declaration)
		return
	}
	if block == "moves" {
		start, _ = extendWholeLine(data, start, start)
		replaceSourceRange(input, module, start, start, []byte(declaration))
		return
	}
	_, end = extendWholeLine(data, end, end)
	replaceSourceRange(input, module, end, end, []byte(declaration))
}

func namedBlockDeclarationSpan(data []byte, name string) (int, int, bool) {
	tokens := syntax.Lex(data).Tokens
	for index, token := range tokens {
		if token.Kind != syntax.TokenIdentifier || token.Raw != name {
			continue
		}
		next := index + 1
		for next < len(tokens) && (tokens[next].Kind == syntax.TokenNewline || tokens[next].Kind == syntax.TokenLineComment || tokens[next].Kind == syntax.TokenDocComment) {
			next++
		}
		if next >= len(tokens) || tokens[next].Kind != syntax.TokenLBrace {
			continue
		}
		depth := 0
		for closeIndex := next; closeIndex < len(tokens); closeIndex++ {
			switch tokens[closeIndex].Kind {
			case syntax.TokenLBrace:
				depth++
			case syntax.TokenRBrace:
				depth--
				if depth == 0 {
					return token.Span.Start, tokens[closeIndex].Span.End, true
				}
			}
		}
	}
	return 0, 0, false
}

func moveEntrySortKey(entry string) string {
	fields := strings.Fields(entry)
	if len(fields) < 4 {
		return "~" + entry
	}
	kindRank := map[string]int{"project": 0, "entity_type": 1, "relation_type": 2, "layer": 3, "entity": 4, "relation": 5, "query": 6, "view": 7, "reference": 8, "entity_type_column": 9, "relation_type_column": 10, "entity_type_constraint": 11, "relation_type_constraint": 12, "entity_row": 13, "relation_row": 14, "query_parameter": 15, "view_table_column": 16, "view_export": 17}
	rank := kindRank[fields[0]]
	owner, old := "", fields[1]
	if len(fields) >= 5 && fields[3] == "->" {
		owner, old = fields[1], fields[2]
	}
	return fmt.Sprintf("%02d\x00%s\x00%s", rank, owner, old)
}

func declarationFirstIdentifierSpan(snapshot Snapshot, source index.SourceSubjectRecord) (syntax.Span, bool) {
	for _, file := range snapshot.LosslessSyntaxTree.Files {
		if file.ModulePath != source.Module.ModulePath || file.Origin.Kind != source.Module.Origin.Kind {
			continue
		}
		for _, token := range file.Tokens {
			if token.Kind == syntax.TokenIdentifier && token.Span.Start >= source.DeclarationRange.StartByte && token.Span.End <= source.DeclarationRange.EndByte {
				return token.Span, true
			}
		}
	}
	return syntax.Span{}, false
}

func renamedAddress(address, newID string) string {
	if split := strings.LastIndex(address, ":"); split >= 0 {
		return address[:split+1] + newID
	}
	return address
}

func replaceTerminalBinding(raw []byte, newID string) string {
	text := string(raw)
	if dot := strings.LastIndex(text, "."); dot >= 0 {
		return text[:dot+1] + newID
	}
	if strings.HasPrefix(text, "$") {
		return "$" + newID
	}
	return newID
}

func declarationIDSpan(snapshot Snapshot, source index.SourceSubjectRecord, kind SemanticSubjectKind) (syntax.Span, bool) {
	for _, file := range snapshot.LosslessSyntaxTree.Files {
		if file.ModulePath != source.Module.ModulePath || file.Origin.Kind != source.Module.Origin.Kind {
			continue
		}
		var candidates []syntax.Token
		for _, token := range file.Tokens {
			if token.Span.Start < source.DeclarationRange.StartByte || token.Span.End > source.DeclarationRange.EndByte || token.Kind != syntax.TokenIdentifier {
				continue
			}
			candidates = append(candidates, token)
		}
		if len(candidates) == 0 {
			return syntax.Span{}, false
		}
		switch kind {
		case materialize.SubjectProject, materialize.SubjectEntityType, materialize.SubjectRelationType, materialize.SubjectQuery, materialize.SubjectView, materialize.SubjectReference:
			if len(candidates) > 1 {
				return candidates[1].Span, true
			}
		case materialize.SubjectEntityRow, materialize.SubjectRelationRow:
			if len(candidates) > 1 {
				return candidates[1].Span, true
			}
		default:
			return candidates[0].Span, true
		}
	}
	return syntax.Span{}, false
}

func replaceSourceRange(input *CompileInput, path string, start, end int, replacement []byte) {
	source := input.ProjectSourceTree[path]
	if start < 0 || start > end || end > len(source) {
		return
	}
	updated := make([]byte, 0, len(source)-(end-start)+len(replacement))
	updated = append(updated, source[:start]...)
	updated = append(updated, replacement...)
	updated = append(updated, source[end:]...)
	input.ProjectSourceTree[path] = updated
}

func extendWholeLine(data []byte, start, end int) (int, int) {
	endAtLineBoundary := end > 0 && end <= len(data) && data[end-1] == '\n'
	for start > 0 && data[start-1] != '\n' {
		start--
	}
	if endAtLineBoundary {
		return start, end
	}
	for end < len(data) && data[end] != '\n' {
		end++
	}
	if end < len(data) {
		end++
	}
	return start, end
}

func sourceReference(snapshot Snapshot, modulePath, address string) (string, bool) {
	for _, binding := range snapshot.SourceMap.Bindings {
		if binding.Module.Origin.Kind != "project" || binding.Module.ModulePath != modulePath || binding.TargetAddress != address {
			continue
		}
		for _, file := range snapshot.LosslessSyntaxTree.Files {
			if file.Origin.Kind == "project" && file.ModulePath == modulePath && binding.Range.StartByte >= 0 && binding.Range.EndByte <= len(file.Source) {
				return string(file.Source[binding.Range.StartByte:binding.Range.EndByte]), true
			}
		}
	}
	for _, subject := range snapshot.SemanticIndex.Subjects {
		if subject.Address == address {
			if i := strings.LastIndex(address, ":"); i >= 0 {
				return address[i+1:], true
			}
		}
	}
	return "", false
}
