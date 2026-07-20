// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"context"
	"errors"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	reviewapp "github.com/dencyuinc/layerdraw/internal/application/review"
)

func (h *Host) Repreview(ctx context.Context, input reviewapp.RepreviewInput) (reviewapp.RepreviewResult, error) {
	preview, err := h.Preview(ctx, runtimeprotocol.PreviewOperationsInput{Session: input.Session, OperationBatch: input.Batch})
	if err != nil {
		return reviewapp.RepreviewResult{}, err
	}
	impact := preview.PreviewEvaluation.AuthoringImpact
	return reviewapp.RepreviewResult{CurrentRevision: input.Batch.BaseRevision, DefinitionHash: preview.DefinitionHash, GraphHash: preview.GraphHash, OperationBatch: input.Batch, AuthoringProof: preview.AuthoringProof, PreviewDecision: preview.PreviewEvaluation.AuthoringDecision, Evidence: reviewapp.Evidence{SemanticDiff: semantic.SemanticDiff{Digest: impact.SemanticDiffHash, Entries: []semantic.SemanticDiffEntry{}}, AuthoringImpact: impact, Diagnostics: []semantic.Diagnostic{}, AffectedUsages: []semantic.StableAddress{}, AffectedRows: []semantic.StableAddress{}, AffectedViews: []semantic.StableAddress{}, RenderPreviews: []reviewapp.ArtifactPreview{}}}, nil
}

func (h *Host) AuthorizeApprover(ctx context.Context, input reviewapp.ApprovalRequest) (accessprotocol.AuthoringDecision, error) {
	scope, err := h.authority.ResolveScope(ctx, input.Revision.DocumentID)
	if err != nil {
		return accessprotocol.AuthoringDecision{}, err
	}
	grant, _, err := h.authority.ResolveGrant(ctx, scope)
	if err != nil || grant.ActorRef != input.Approver {
		return accessprotocol.AuthoringDecision{}, errors.New("Review approver is not the current local owner")
	}
	return (accesscore.Evaluator{}).Evaluate(ctx, accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &input.Impact, GrantSnapshot: grant, HostOperationImpacts: []accessprotocol.HostOperationImpact{}, RequestIntent: "apply"})
}

var _ reviewapp.RuntimePort = (*Host)(nil)
var _ reviewapp.AccessPort = (*Host)(nil)
