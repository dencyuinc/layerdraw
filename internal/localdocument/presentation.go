// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"context"

	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
)

type ProjectView struct {
	Address string `json:"address"`
	Label   string `json:"label"`
	Shape   string `json:"shape"`
}

func (h *Host) ProjectViews(ctx context.Context, ref runtimeprotocol.RuntimeSessionRef) ([]ProjectView, error) {
	session, err := h.SessionFor(ref)
	if err != nil {
		return nil, err
	}
	ctx = h.accessContext(ctx, session)
	if err := h.authority.AuthorizeRead(ctx, ref.Scope, accesscore.SurfaceReview); err != nil {
		return nil, err
	}
	views, err := h.workbench.views(session.working)
	if err != nil {
		return nil, err
	}
	result := make([]ProjectView, 0, len(views))
	for _, view := range views {
		label := view.DisplayName
		if label == "" {
			label = view.Address
		}
		result = append(result, ProjectView{Address: view.Address, Label: label, Shape: view.Shape})
	}
	return result, nil
}

func (h *Host) MaterializeProjectView(ctx context.Context, ref runtimeprotocol.RuntimeSessionRef, address string) (semantic.ViewData, error) {
	session, err := h.SessionFor(ref)
	if err != nil {
		return semantic.ViewData{}, err
	}
	ctx = h.accessContext(ctx, session)
	if err := h.authority.AuthorizeRead(ctx, ref.Scope, accesscore.SurfaceReview); err != nil {
		return semantic.ViewData{}, err
	}
	return h.workbench.materializeView(ctx, session.working, address)
}
