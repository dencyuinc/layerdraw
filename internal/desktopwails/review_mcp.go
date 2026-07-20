// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	reviewapp "github.com/dencyuinc/layerdraw/internal/application/review"
	"github.com/dencyuinc/layerdraw/internal/mcphost"
)

type reviewMCPOwner struct{ shared *sharedOwner }

func (o reviewMCPOwner) Capabilities(context.Context) (mcphost.CapabilitySnapshot, error) {
	base, err := o.shared.Snapshot(context.Background())
	if err != nil {
		return mcphost.CapabilitySnapshot{}, err
	}
	schema := json.RawMessage(`{"type":"object","additionalProperties":true}`)
	base.Operations = map[string]mcphost.OperationCapability{}
	for _, operation := range []string{"review.list_proposals", "review.create_proposal", "review.comment", "review.approve_apply", "review.withdraw"} {
		base.Operations[operation] = mcphost.OperationCapability{Enabled: true, InputSchema: schema, OutputSchema: schema}
	}
	return base, nil
}

func (o reviewMCPOwner) Invoke(ctx context.Context, request mcphost.OwnerRequest) (mcphost.OwnerResponse, error) {
	o.shared.mu.RLock()
	owner, local := o.shared.review, o.shared.local
	o.shared.mu.RUnlock()
	if owner == nil || local == nil {
		return mcphost.OwnerResponse{}, &mcphost.OwnerError{Code: mcphost.ErrorCapabilityUnavailable}
	}
	var value any
	var err error
	switch request.Operation {
	case "review.list_proposals":
		value = owner.Snapshot()
	case "review.create_proposal":
		var input reviewapp.CreateInput
		if decodeErr := json.Unmarshal(request.Arguments, &input); decodeErr != nil {
			err = reviewapp.ErrInvalid
		} else {
			value, err = owner.Create(ctx, input)
		}
	case "review.comment":
		var input reviewapp.CommentInput
		if decodeErr := json.Unmarshal(request.Arguments, &input); decodeErr != nil {
			err = reviewapp.ErrInvalid
		} else {
			value, err = owner.Comment(ctx, input)
		}
	case "review.approve_apply":
		var input struct {
			ProposalID string `json:"proposal_id"`
			Generation uint64 `json:"generation"`
		}
		if request.Binding == nil || json.Unmarshal(request.Arguments, &input) != nil {
			err = reviewapp.ErrInvalid
		} else {
			session, actor, bindingErr := local.ReviewBinding(request.Binding.DocumentID)
			if bindingErr != nil {
				err = bindingErr
			} else {
				operation := runtimeprotocol.OperationID(fmt.Sprintf("review_mcp_%s_%d", input.ProposalID, input.Generation))
				value, err = owner.ApproveAndApply(ctx, reviewapp.ApprovalInput{ProposalID: input.ProposalID, Generation: input.Generation, Session: session, Approver: actor, OperationID: operation, IdempotencyKey: runtimeprotocol.IdempotencyKey(operation), Trigger: runtimeprotocol.CommitTriggerAgentApply})
			}
		}
	case "review.withdraw":
		var input struct {
			ProposalID string `json:"proposal_id"`
			Generation uint64 `json:"generation"`
		}
		if request.Binding == nil || json.Unmarshal(request.Arguments, &input) != nil {
			err = reviewapp.ErrInvalid
		} else {
			_, actor, bindingErr := local.ReviewBinding(request.Binding.DocumentID)
			if bindingErr != nil {
				err = bindingErr
			} else {
				value, err = owner.Withdraw(ctx, input.ProposalID, input.Generation, actor)
			}
		}
	default:
		return mcphost.OwnerResponse{}, &mcphost.OwnerError{Code: mcphost.ErrorCapabilityUnavailable}
	}
	if err != nil {
		if errors.Is(err, reviewapp.ErrInvalid) || errors.Is(err, reviewapp.ErrConflict) {
			return mcphost.OwnerResponse{}, &mcphost.OwnerError{Code: mcphost.ErrorInvalidRequest}
		}
		return mcphost.OwnerResponse{}, &mcphost.OwnerError{Code: mcphost.ErrorOwnerFailure}
	}
	content, err := json.Marshal(value)
	if err != nil {
		return mcphost.OwnerResponse{}, err
	}
	return mcphost.OwnerResponse{Content: content, Items: 1, Outcome: protocolcommon.OutcomeSuccess}, nil
}

func (reviewMCPOwner) ReadResource(context.Context, mcphost.ResourceRequest) (mcphost.ResourceResponse, error) {
	return mcphost.ResourceResponse{}, &mcphost.OwnerError{Code: mcphost.ErrorCapabilityUnavailable}
}

var _ mcphost.Owner = reviewMCPOwner{}
