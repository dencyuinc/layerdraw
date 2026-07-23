// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"errors"

	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/registry"
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
	Views                 []ProjectViewDTO                         `json:"views"`
	LibraryProject        LibraryProjectContextDTO                 `json:"library_project"`
}

type LibraryProjectContextDTO struct {
	ProjectID          string                             `json:"project_id"`
	Revision           string                             `json:"revision"`
	DefinitionHash     string                             `json:"definition_hash"`
	ResolvedLockDigest string                             `json:"resolved_lock_digest"`
	DependencySnapshot registry.ProjectDependencySnapshot `json:"dependency_snapshot"`
}

type ProjectViewDTO struct {
	Address string `json:"address"`
	Label   string `json:"label"`
	Shape   string `json:"shape"`
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
	views, err := host.ProjectViews(ctx, session)
	if err != nil {
		return ProjectPublicationDTO{}, err
	}
	viewDTOs := make([]ProjectViewDTO, 0, len(views))
	for _, view := range views {
		viewDTOs = append(viewDTOs, ProjectViewDTO{Address: view.Address, Label: view.Label, Shape: view.Shape})
	}
	registryState, err := host.CurrentRegistryProjectState(ctx, opened.PortableID)
	if err != nil {
		return ProjectPublicationDTO{}, err
	}
	displayName := opened.DisplayName
	if displayName == "" {
		displayName = string(session.Scope.DocumentID)
	}
	publication.Project = &ProjectPublicationContext{
		ProjectID: session.Scope.DocumentID, SessionGeneration: generation,
		DisplayName: displayName, AuthoritativeRevision: revision,
		OpenInput:   runtimeprotocol.OpenRuntimeDocumentInput{DocumentID: session.Scope.DocumentID},
		Persistence: persistence, Views: viewDTOs,
		LibraryProject: LibraryProjectContextDTO{ProjectID: registryState.ProjectID, Revision: registryState.Revision, DefinitionHash: registryState.DefinitionHash, ResolvedLockDigest: registryState.DependencySnapshot.ResolvedLockDigest, DependencySnapshot: registryState.DependencySnapshot},
	}
	return publication, nil
}

// ActiveProjectSession returns the app-owned runtime session result for the
// requested document so the trusted frontend adopts it instead of opening a
// second session. Durable commits then flow through the same session the app
// lifecycle tracks.
func (a *Application) ActiveProjectSession(documentID runtimeprotocol.DocumentID) (runtimeprotocol.OpenRuntimeDocumentResult, error) {
	session, _, _, ok := a.projects.active()
	if !ok || session.Scope.DocumentID != documentID {
		return runtimeprotocol.OpenRuntimeDocumentResult{}, errors.New("the requested project session is not open")
	}
	done, host, failure := a.beginHost(desktopcontract.ComponentRuntime)
	if failure != nil {
		return runtimeprotocol.OpenRuntimeDocumentResult{}, errors.New("desktop project session is unavailable")
	}
	defer done()
	opened, err := host.SessionFor(session)
	if err != nil {
		return runtimeprotocol.OpenRuntimeDocumentResult{}, err
	}
	return opened.Open, nil
}
