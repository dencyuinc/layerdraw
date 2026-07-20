// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"encoding/json"
	"errors"
)

type ReviewCommentRequest struct {
	ProposalID string          `json:"proposal_id"`
	Generation uint64          `json:"generation"`
	CommentID  string          `json:"comment_id"`
	Body       string          `json:"body"`
	Target     json.RawMessage `json:"target"`
}

type ReviewApprovalRequest struct {
	ProposalID string `json:"proposal_id"`
	Generation uint64 `json:"generation"`
}

func (a *Application) ReviewSnapshot() (any, error) {
	if a.config.ReviewOwner == nil {
		return nil, errors.New("Review owner is unavailable")
	}
	return a.config.ReviewOwner.ReviewSnapshot(), nil
}

func (a *Application) ReviewComment(ctx context.Context, input ReviewCommentRequest) (any, error) {
	if a.config.ReviewOwner == nil {
		return nil, errors.New("Review owner is unavailable")
	}
	return a.config.ReviewOwner.ReviewComment(ctx, input, a.localActor)
}

func (a *Application) ReviewApproveAndApply(ctx context.Context, input ReviewApprovalRequest) (any, error) {
	if a.config.ReviewOwner == nil {
		return nil, errors.New("Review owner is unavailable")
	}
	sessions := a.projects.sessionRefs()
	if len(sessions) != 1 {
		return nil, errors.New("Review approval requires one active project")
	}
	return a.config.ReviewOwner.ReviewApproveAndApply(ctx, input, sessions[0], a.localActor)
}

func (a *Application) ReviewWithdraw(ctx context.Context, proposalID string, generation uint64) (any, error) {
	if a.config.ReviewOwner == nil {
		return nil, errors.New("Review owner is unavailable")
	}
	return a.config.ReviewOwner.ReviewWithdraw(ctx, proposalID, generation, a.localActor)
}
