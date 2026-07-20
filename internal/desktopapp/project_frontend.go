// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"errors"

	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

// ProjectPublicationDTO is a JSON-only snapshot for the trusted frontend
// adapter. BrowserEditor and Viewer instances are deliberately absent.
type ProjectPublicationDTO struct {
	Phase   string                     `json:"phase"`
	Project *ProjectPublicationContext `json:"project,omitempty"`
}

type ProjectPublicationContext struct {
	ProjectID             runtimeprotocol.DocumentID               `json:"project_id"`
	SessionGeneration     uint64                                   `json:"session_generation"`
	DisplayName           string                                   `json:"display_name"`
	AuthoritativeRevision runtimeprotocol.CommittedRevisionRef     `json:"authoritative_revision"`
	OpenInput             runtimeprotocol.OpenRuntimeDocumentInput `json:"open_input"`
	Persistence           string                                   `json:"persistence"`
}

func (a *Application) ProjectPublication(ctx context.Context) (ProjectPublicationDTO, error) {
	publication := ProjectPublicationDTO{Phase: string(a.State())}
	session, generation, persistence, ok := a.projects.active()
	if !ok {
		return publication, nil
	}
	done, host, failure := a.beginHost(desktopcontract.ComponentRuntime)
	if failure != nil {
		return ProjectPublicationDTO{}, errors.New("desktop project publication is unavailable")
	}
	defer done()
	opened, err := host.SessionFor(session)
	if err != nil {
		return ProjectPublicationDTO{}, err
	}
	revision := opened.Open.CommittedRevision
	publication.Project = &ProjectPublicationContext{
		ProjectID: session.Scope.DocumentID, SessionGeneration: generation,
		DisplayName: string(session.Scope.DocumentID), AuthoritativeRevision: revision,
		OpenInput:   runtimeprotocol.OpenRuntimeDocumentInput{DocumentID: session.Scope.DocumentID},
		Persistence: persistence,
	}
	return publication, nil
}
