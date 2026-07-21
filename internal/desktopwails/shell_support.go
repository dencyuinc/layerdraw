// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

const errorSurfaceEvent = "layerdraw:error-surface"

type applicationCommandRouter struct {
	application *desktopapp.Application
	mu          sync.Mutex
	generation  uint64
}

func newApplicationCommandRouter(application *desktopapp.Application) *applicationCommandRouter {
	return &applicationCommandRouter{application: application, generation: 1}
}

func (r *applicationCommandRouter) Status(context.Context) ([]desktopcontract.CommandStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.statusLocked(), nil
}

func (r *applicationCommandRouter) statusLocked() []desktopcontract.CommandStatus {
	generation := protocolcommon.CanonicalUint64(fmt.Sprintf("%d", r.generation))
	result := make([]desktopcontract.CommandStatus, 0, 8)
	for _, id := range []desktopcontract.CommandID{
		desktopcontract.CommandNewProject, desktopcontract.CommandOpenProject, desktopcontract.CommandSaveProject,
		desktopcontract.CommandCloseProject, desktopcontract.CommandUndo, desktopcontract.CommandRedo,
		desktopcontract.CommandSearch, desktopcontract.CommandSettings,
	} {
		state := desktopcontract.CommandUnavailable
		if id == desktopcontract.CommandNewProject || id == desktopcontract.CommandOpenProject || id == desktopcontract.CommandSettings {
			state = desktopcontract.CommandAvailable
		}
		result = append(result, desktopcontract.CommandStatus{ID: id, State: state, Generation: generation})
	}
	return result
}

func (r *applicationCommandRouter) Route(ctx context.Context, input desktopcontract.CommandInvocation) (desktopcontract.CommandStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var current desktopcontract.CommandStatus
	for _, status := range r.statusLocked() {
		if status.ID == input.ID {
			current = status
			break
		}
	}
	if !input.Validate() || current.State != desktopcontract.CommandAvailable || current.Generation != input.StatusGeneration {
		return current, errors.New("desktop command unavailable")
	}
	switch input.ID {
	case desktopcontract.CommandNewProject:
		result := r.application.CreateProjectDialog(ctx, "native-command-new")
		if result.Outcome != protocolcommon.OutcomeSuccess && result.Outcome != protocolcommon.OutcomeCancelled {
			return current, errors.New("desktop command failed")
		}
	case desktopcontract.CommandOpenProject:
		result := r.application.OpenProjectDialog(ctx, "native-command-open")
		if result.Outcome != protocolcommon.OutcomeSuccess && result.Outcome != protocolcommon.OutcomeCancelled {
			return current, errors.New("desktop command failed")
		}
	case desktopcontract.CommandSettings:
		// The bound NativeShell.UpdateSettings method owns the actual mutation.
	default:
		return current, errors.New("desktop command unavailable")
	}
	return current, nil
}

type projectCrashRecovery struct{ application *desktopapp.Application }

func (p projectCrashRecovery) Preserve(context.Context, desktopcontract.CrashContext) (desktopcontract.RecoveryRef, error) {
	candidates := p.application.RecoveryCandidates()
	if candidates.Outcome != protocolcommon.OutcomeSuccess || len(candidates.Value) == 0 {
		return desktopcontract.RecoveryRef{}, errors.New("durable project recovery unavailable")
	}
	return desktopcontract.RecoveryRef{ID: string(candidates.Value[0].ProjectID)}, nil
}

type wailsErrorSurface struct{ runtime NativeRuntime }

func (p wailsErrorSurface) Present(ctx context.Context, value desktopcontract.ErrorSurface) error {
	p.runtime.Emit(ctx, errorSurfaceEvent, value)
	return nil
}

func invokeNativeCommand(ctx context.Context, shell *desktopapp.NativeShell, id desktopcontract.CommandID, source desktopcontract.CommandSource) bool {
	statuses := shell.CommandStatus(ctx)
	if statuses.Outcome != protocolcommon.OutcomeSuccess {
		return false
	}
	for _, status := range statuses.Value {
		if status.ID == id {
			result := shell.InvokeCommand(ctx, desktopcontract.CommandInvocation{ID: id, Source: source, StatusGeneration: status.Generation})
			return result.Outcome == protocolcommon.OutcomeSuccess
		}
	}
	return false
}
