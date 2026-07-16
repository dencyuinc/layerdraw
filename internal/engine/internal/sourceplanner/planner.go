// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package sourceplanner implements pure Workbench source planners which transform
// one closed LDL compiler input into another. It owns no handles, files,
// transports, Runtime state, or authorization decisions.
package sourceplanner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const textMediaType = "text/plain; charset=utf-8"

// BlobSource is the closed request attachment set. The planner verifies every
// reference before using its bytes and never retains caller-owned storage.
type PlannerBlobs map[string][]byte

// SourcePlanningBase is one immutable planning generation. Generation is supplied by the
// endpoint-local Working Document owner but is never mutated by this package.
type SourcePlanningBase struct {
	Input      CompileInput
	Generation Generation
}

// SourcePlan contains a generated preview plus the closed candidate input and every
// response attachment referenced by the preview. Candidate and Attachments are
// defensive copies and may be discarded without changing SourcePlanningBase.
type SourcePlan struct {
	Preview     WorkbenchPreviewResult
	Candidate   CompileInput
	Attachments PlannerBlobs
}

// SourcePlanner runs the canonical Go compiler before and after every candidate.
type SourcePlanner struct {
	compiler Compiler
}

func NewSourcePlanner(compiler Compiler) SourcePlanner { return SourcePlanner{compiler: compiler} }

// PreviewSourcePatch plans guarded byte-range replacements.
func (p SourcePlanner) PreviewSourcePatch(ctx context.Context, base SourcePlanningBase, request PreviewSourcePatchInput, blobs PlannerBlobs) (SourcePlan, error) {
	return p.preview(ctx, base, request.Preconditions, request.Limits, func(before Snapshot) (CompileInput, []Diagnostic, []SemanticConflict, error) {
		if values, conflicts := requirePatchPreconditions(request.Preconditions, request.Patch); len(values) != 0 {
			return CompileInput{}, values, conflicts, nil
		}
		return applySourcePatches(ctx, base.Input, before, request.Patch, blobs)
	})
}

// PreviewFragment plans one parsed and owner-scoped LDL fragment.
func (p SourcePlanner) PreviewFragment(ctx context.Context, base SourcePlanningBase, request PreviewFragmentInput, blobs PlannerBlobs) (SourcePlan, error) {
	return p.preview(ctx, base, request.Preconditions, request.Limits, func(before Snapshot) (CompileInput, []Diagnostic, []SemanticConflict, error) {
		if values, conflicts := requireFragmentPreconditions(request.Preconditions, request.Fragment, before); len(values) != 0 {
			return CompileInput{}, values, conflicts, nil
		}
		return applyFragment(ctx, p.compiler, base.Input, before, request.Fragment, blobs)
	})
}

// FormatScope plans canonical formatting of only complete requested syntax
// scopes. It never formats a partial byte range or an unrelated declaration.
func (p SourcePlanner) FormatScope(ctx context.Context, base SourcePlanningBase, request FormatScopeInput) (SourcePlan, error) {
	return p.preview(ctx, base, request.Preconditions, request.Limits, func(before Snapshot) (CompileInput, []Diagnostic, []SemanticConflict, error) {
		if values, conflicts := requireFormatPreconditions(request.Preconditions, request.ScopeAddresses, before); len(values) != 0 {
			return CompileInput{}, values, conflicts, nil
		}
		return formatScopes(ctx, base.Input, before, request.ScopeAddresses)
	})
}

// OrganizeWorkspace plans the explicit standard-layout transaction entirely
// in memory. Source moves are returned in the preview; no filesystem is read or
// written.
func (p SourcePlanner) OrganizeWorkspace(ctx context.Context, base SourcePlanningBase, request OrganizeWorkspaceInput) (SourcePlan, error) {
	return p.preview(ctx, base, request.Preconditions, request.Limits, func(before Snapshot) (CompileInput, []Diagnostic, []SemanticConflict, error) {
		if request.Strategy != "standard_layout" {
			return CompileInput{}, diagnostics("LDL1802", "semantic_operation_conflict", "unsupported organization strategy", nil), nil, nil
		}
		if values, conflicts := requireOrganizationPreconditions(request.Preconditions, before); len(values) != 0 {
			return CompileInput{}, values, conflicts, nil
		}
		return organizeStandardLayout(ctx, base.Input, before)
	})
}

type candidateBuilder func(Snapshot) (CompileInput, []Diagnostic, []SemanticConflict, error)

func (p SourcePlanner) preview(ctx context.Context, base SourcePlanningBase, preconditions EngineEditPreconditions, limits WorkbenchLimits, build candidateBuilder) (plan SourcePlan, err error) {
	defer func() {
		if err == nil {
			err = enforceSourcePlanLimits(plan, limits)
		}
	}()
	if ctx == nil {
		return SourcePlan{}, fmt.Errorf("nil source planner context")
	}
	if err := ctx.Err(); err != nil {
		return SourcePlan{}, err
	}
	beforeResult, err := p.compiler.Compile(ctx, cloneCompileInput(base.Input))
	if err != nil {
		return SourcePlan{}, err
	}
	before := beforeResult.Snapshot()
	if len(before.Diagnostics) != 0 {
		return invalidPlan(base, emptyDiffs(), mapCompilerDiagnostics(before.Diagnostics), nil), nil
	}
	if diagnostics, conflicts := checkPreconditions(base.Generation, preconditions, before); len(diagnostics) != 0 || len(conflicts) != 0 {
		return invalidPlan(base, emptyDiffs(), diagnostics, conflicts), nil
	}
	candidate, candidateDiagnostics, conflicts, err := build(before)
	if err != nil {
		return SourcePlan{}, err
	}
	if len(candidateDiagnostics) != 0 || len(conflicts) != 0 {
		return invalidPlan(base, emptyDiffs(), candidateDiagnostics, conflicts), nil
	}
	if err := ctx.Err(); err != nil {
		return SourcePlan{}, err
	}
	afterResult, err := p.compiler.Compile(ctx, cloneCompileInput(candidate))
	if err != nil {
		return SourcePlan{}, err
	}
	after := afterResult.Snapshot()
	if len(after.Diagnostics) != 0 {
		return invalidPlan(base, emptyDiffs(), mapCompilerDiagnostics(after.Diagnostics), nil), nil
	}
	return finalizePlan(ctx, base, before, candidate, after)
}

// SourcePlannerLimitError is a pure execution limit failure. Dispatchers map
// it to the generated Workbench limit_exceeded failure category; it is never a
// semantic rejection or a partially published preview.
type SourcePlannerLimitError struct {
	Resource        string
	Limit, Observed uint64
}

func (e *SourcePlannerLimitError) Error() string {
	return fmt.Sprintf("source planner %s observed %d exceeds limit %d", e.Resource, e.Observed, e.Limit)
}

func enforceSourcePlanLimits(plan SourcePlan, limits WorkbenchLimits) error {
	maxItems, maxBytes := limits.MaxItems, limits.MaxOutputBytes
	if maxItems == 0 || maxBytes == 0 {
		return fmt.Errorf("invalid source planner limits")
	}
	items := uint64(len(plan.Preview.ChangedSourceFiles) + len(plan.Preview.Conflicts) + len(plan.Preview.Diagnostics) + len(plan.Preview.SemanticDiff.Entries) + len(plan.Preview.SourceDiff.Edits))
	if plan.Preview.AuthoringImpact != nil {
		items += uint64(len(plan.Preview.AuthoringImpact.Entries))
	}
	if items > maxItems {
		return &SourcePlannerLimitError{Resource: "max_items", Limit: maxItems, Observed: items}
	}
	encoded, err := json.Marshal(plan.Preview)
	if err != nil {
		return err
	}
	observedBytes := uint64(len(encoded))
	for _, value := range plan.Attachments {
		observedBytes += uint64(len(value))
	}
	if observedBytes > maxBytes {
		return &SourcePlannerLimitError{Resource: "max_output_bytes", Limit: maxBytes, Observed: observedBytes}
	}
	return nil
}

func cloneCompileInput(input CompileInput) CompileInput {
	out := input
	out.ProjectSourceTree = cloneTree(input.ProjectSourceTree)
	out.InstalledPackTree = cloneTree(input.InstalledPackTree)
	out.ReferencedAssets = append([]AssetInput(nil), input.ReferencedAssets...)
	for i := range out.ReferencedAssets {
		out.ReferencedAssets[i].Bytes = bytes.Clone(out.ReferencedAssets[i].Bytes)
	}
	return out
}

func cloneTree(tree map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(tree))
	for path, source := range tree {
		out[path] = bytes.Clone(source)
	}
	return out
}

func resolveBlob(ref BlobRef, source PlannerBlobs, mediaType string) ([]byte, *Diagnostic) {
	value, ok := source[ref.BlobID]
	if !ok {
		d := diagnostic("LDL1802", "semantic_operation_conflict", "required request blob is missing", nil)
		return nil, &d
	}
	if ref.Lifetime != BlobLifetimeRequest || (mediaType != "" && ref.MediaType != mediaType) {
		d := diagnostic("LDL1802", "semantic_operation_conflict", "request blob has an invalid lifetime or media type", nil)
		return nil, &d
	}
	if ref.Size != uint64(len(value)) || ref.Digest != digest(value) {
		d := diagnostic("LDL1801", "stale_revision_or_semantic_hash", "request blob size or digest does not match its bytes", nil)
		return nil, &d
	}
	return bytes.Clone(value), nil
}

func digest(value []byte) Digest {
	sum := sha256.Sum256(value)
	return Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func diagnostic(code, key, message string, sourceRange *SourceRange) Diagnostic {
	messageCopy := message
	return Diagnostic{
		Arguments: map[string]DiagnosticArgumentValue{}, Code: code, Message: &messageCopy,
		MessageKey: key, ProtocolVersion: 1, Range: sourceRange, Related: []DiagnosticRelated{},
		Severity: DiagnosticSeverityError,
	}
}

func diagnostics(code, key, message string, sourceRange *SourceRange) []Diagnostic {
	return []Diagnostic{diagnostic(code, key, message, sourceRange)}
}
