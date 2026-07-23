// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/privatefs"
)

const projectLifecycleVersion = 2

const maxLifecycleEntries = 4096
const maxRecoveryPayloadBytes = 256 << 10
const maxRecoveryReferenceBytes = 2048

type ProjectAvailability string

const (
	ProjectAvailable ProjectAvailability = "available"
	ProjectMissing   ProjectAvailability = "missing"
)

type ProjectOpenDisposition string

const (
	ProjectOpened   ProjectOpenDisposition = "opened"
	ProjectFocused  ProjectOpenDisposition = "focused_existing"
	ProjectRestored ProjectOpenDisposition = "restored"
)

type RecentProject struct {
	ProjectID     runtimeprotocol.DocumentID `json:"project_id"`
	DisplayName   string                     `json:"display_name"`
	LocationLabel string                     `json:"location_label,omitempty"`
	Pinned        bool                       `json:"pinned"`
	LastOpenedAt  protocolcommon.Rfc3339Time `json:"last_opened_at"`
	Availability  ProjectAvailability        `json:"availability"`
}

type CloseBlocker string

const (
	ClosePendingPreview   CloseBlocker = "pending_preview"
	CloseEphemeralEdits   CloseBlocker = "ephemeral_edits"
	CloseAutosavePending  CloseBlocker = "autosave_pending"
	CloseProviderPending  CloseBlocker = "provider_reconcile_pending"
	CloseExternalPending  CloseBlocker = "external_materialization_pending"
	CloseExternalFailed   CloseBlocker = "external_materialization_failed"
	CloseStateStale       CloseBlocker = "committed_state_stale"
	CloseRecoveryRequired CloseBlocker = "recovery_required"
)

type CloseAssessment struct {
	ProjectID         runtimeprotocol.DocumentID `json:"project_id"`
	CommittedRevision runtimeprotocol.RevisionID `json:"committed_revision"`
	CanClose          bool                       `json:"can_close"`
	Blockers          []CloseBlocker             `json:"blockers"`
	Autosave          AutosaveOutcome            `json:"autosave"`
}

type AutosaveOutcome string

const (
	AutosaveIdle        AutosaveOutcome = "idle"
	AutosaveScheduled   AutosaveOutcome = "scheduled"
	AutosaveCommitted   AutosaveOutcome = "committed"
	AutosaveConflict    AutosaveOutcome = "conflict"
	AutosaveNeedsReview AutosaveOutcome = "needs_review"
	AutosaveFailed      AutosaveOutcome = "failed"
)

type EphemeralStateInput struct {
	Session  runtimeprotocol.RuntimeSessionRef `json:"session"`
	Dirty    bool                              `json:"dirty"`
	Recovery *RecoveryArtifact                 `json:"recovery,omitempty"`
}

type RecoveryArtifactKind string

const (
	RecoveryPreviewOperations RecoveryArtifactKind = "preview_operations"
	RecoveryEditorState       RecoveryArtifactKind = "editor_state"
)

// RecoveryArtifact is the bounded material needed to actually reconstruct an
// interrupted edit. Exactly one of Payload or Reference is present.
type RecoveryArtifact struct {
	Kind      RecoveryArtifactKind `json:"kind"`
	Payload   json.RawMessage      `json:"payload,omitempty"`
	Reference string               `json:"reference,omitempty"`
}

type RecoveryCandidate struct {
	ProjectID         runtimeprotocol.DocumentID `json:"project_id"`
	CommittedRevision runtimeprotocol.RevisionID `json:"committed_revision"`
	InterruptedAt     protocolcommon.Rfc3339Time `json:"interrupted_at"`
	PendingPreview    bool                       `json:"pending_preview"`
	EphemeralEdits    bool                       `json:"ephemeral_edits"`
	AutosavePending   bool                       `json:"autosave_pending"`
	ProviderPending   bool                       `json:"provider_reconcile_pending"`
	TerminalBlocker   CloseBlocker               `json:"terminal_blocker,omitempty"`
	Autosave          AutosaveOutcome            `json:"autosave"`
	Recovery          *RecoveryArtifact          `json:"recovery,omitempty"`
	TerminalRecovery  *RecoveryArtifact          `json:"terminal_recovery,omitempty"`
}

type RecoveryChoice string

const (
	RecoveryRestore RecoveryChoice = "restore"
	RecoveryDiscard RecoveryChoice = "discard"
)

type persistedProject struct {
	ProjectID     runtimeprotocol.DocumentID `json:"project_id"`
	DisplayName   string                     `json:"display_name,omitempty"`
	LocationLabel string                     `json:"location_label,omitempty"`
	Pinned        bool                       `json:"pinned"`
	LastOpenedAt  protocolcommon.Rfc3339Time `json:"last_opened_at"`
	Missing       bool                       `json:"missing"`
}

type persistedRecovery struct {
	ProjectID         runtimeprotocol.DocumentID `json:"project_id"`
	CommittedRevision runtimeprotocol.RevisionID `json:"committed_revision"`
	InterruptedAt     protocolcommon.Rfc3339Time `json:"interrupted_at"`
	AutosavePending   bool                       `json:"autosave_pending"`
	ProviderPending   bool                       `json:"provider_reconcile_pending"`
	TerminalBlocker   CloseBlocker               `json:"terminal_blocker,omitempty"`
	Autosave          AutosaveOutcome            `json:"autosave"`
	Recovery          *RecoveryArtifact          `json:"recovery,omitempty"`
	TerminalRecovery  *RecoveryArtifact          `json:"terminal_recovery,omitempty"`
}

type persistedLifecycle struct {
	Version    int                          `json:"version"`
	Projects   map[string]persistedProject  `json:"projects"`
	Recoveries map[string]persistedRecovery `json:"recoveries"`
}

type sessionLifecycle struct {
	projectID          runtimeprotocol.DocumentID
	session            runtimeprotocol.RuntimeSessionRef
	committedRevision  runtimeprotocol.RevisionID
	pendingPreview     bool
	ephemeralEdits     bool
	autosavePending    bool
	providerPending    bool
	terminalBlocker    CloseBlocker
	recovery           *RecoveryArtifact
	terminalRecovery   *RecoveryArtifact
	autosave           AutosaveOutcome
	autosaveGeneration uint64
	recoveryRequired   bool
	generation         uint64
	inflight           int
	closing            bool
	drained            chan struct{}
}

type projectLifecycle struct {
	mu        sync.Mutex
	path      string
	now       func() time.Time
	state     persistedLifecycle
	sessions  map[runtimeprotocol.RuntimeSessionID]*sessionLifecycle
	byProject map[runtimeprotocol.DocumentID]runtimeprotocol.RuntimeSessionID
	saveFault func() error
}

func newProjectLifecycle(root string, now func() time.Time) (*projectLifecycle, error) {
	value := &projectLifecycle{
		path: filepath.Join(root, "project-lifecycle.json"), now: now,
		state:    persistedLifecycle{Version: projectLifecycleVersion, Projects: map[string]persistedProject{}, Recoveries: map[string]persistedRecovery{}},
		sessions: map[runtimeprotocol.RuntimeSessionID]*sessionLifecycle{}, byProject: map[runtimeprotocol.DocumentID]runtimeprotocol.RuntimeSessionID{},
	}
	info, err := os.Lstat(value.path)
	if errors.Is(err, os.ErrNotExist) {
		return value, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !privatefs.PermissionsMatch(info, 0o600) || info.Size() > 1<<20 {
		return nil, fmt.Errorf("desktop lifecycle metadata requires recovery")
	}
	data, err := os.ReadFile(value.path)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value.state); err != nil || value.state.Version != projectLifecycleVersion || value.state.Projects == nil || value.state.Recoveries == nil {
		return nil, fmt.Errorf("desktop lifecycle metadata requires recovery")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("desktop lifecycle metadata requires recovery")
	}
	if len(value.state.Projects) > maxLifecycleEntries || len(value.state.Recoveries) > maxLifecycleEntries {
		return nil, fmt.Errorf("desktop lifecycle metadata requires recovery")
	}
	for key, project := range value.state.Projects {
		_, idErr := runtimeprotocol.EncodeDocumentID(project.ProjectID)
		_, timeErr := time.Parse(time.RFC3339Nano, string(project.LastOpenedAt))
		if key != string(project.ProjectID) || idErr != nil || timeErr != nil {
			return nil, fmt.Errorf("desktop lifecycle metadata requires recovery")
		}
	}
	for key, recovery := range value.state.Recoveries {
		if recovery.Autosave == "" {
			recovery.Autosave = AutosaveIdle
			value.state.Recoveries[key] = recovery
		}
		_, idErr := runtimeprotocol.EncodeDocumentID(recovery.ProjectID)
		_, revisionErr := runtimeprotocol.EncodeRevisionID(recovery.CommittedRevision)
		_, timeErr := time.Parse(time.RFC3339Nano, string(recovery.InterruptedAt))
		if key != string(recovery.ProjectID) || idErr != nil || revisionErr != nil || timeErr != nil || !validAutosaveOutcome(recovery.Autosave) || !validTerminalBlocker(recovery.TerminalBlocker) || !validRecoveryArtifact(recovery.Recovery) || !validRecoveryArtifact(recovery.TerminalRecovery) || (recovery.TerminalBlocker == "") != (recovery.TerminalRecovery == nil) {
			return nil, fmt.Errorf("desktop lifecycle metadata requires recovery")
		}
	}
	return value, nil
}

func validTerminalBlocker(value CloseBlocker) bool {
	return value == "" || value == CloseExternalPending || value == CloseExternalFailed || value == CloseStateStale
}

func validRecoveryArtifact(value *RecoveryArtifact) bool {
	if value == nil {
		return true
	}
	if value.Kind != RecoveryPreviewOperations && value.Kind != RecoveryEditorState {
		return false
	}
	hasPayload := len(value.Payload) != 0
	hasReference := value.Reference != ""
	if hasPayload == hasReference {
		return false
	}
	if hasPayload {
		trimmed := bytes.TrimSpace(value.Payload)
		return len(value.Payload) <= maxRecoveryPayloadBytes && len(trimmed) != 0 && (trimmed[0] == '{' || trimmed[0] == '[') && json.Valid(trimmed)
	}
	return len(value.Reference) <= maxRecoveryReferenceBytes
}

func cloneRecoveryArtifact(value *RecoveryArtifact) *RecoveryArtifact {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Payload = append(json.RawMessage(nil), value.Payload...)
	return &copy
}

func validAutosaveOutcome(value AutosaveOutcome) bool {
	switch value {
	case AutosaveIdle, AutosaveScheduled, AutosaveCommitted, AutosaveConflict, AutosaveNeedsReview, AutosaveFailed:
		return true
	default:
		return false
	}
}

func (l *projectLifecycle) opened(session runtimeprotocol.RuntimeSessionRef, revision runtimeprotocol.CommittedRevisionRef, providerPending bool, displayName string, locationLabel string) (runtimeprotocol.RuntimeSessionRef, ProjectOpenDisposition, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	projectID := session.Scope.DocumentID
	if existingID, ok := l.byProject[projectID]; ok {
		if existing := l.sessions[existingID]; existing != nil {
			return existing.session, ProjectFocused, nil
		}
	}
	now := protocolcommon.Rfc3339Time(l.now().UTC().Format(time.RFC3339Nano))
	priorProject, hadProject := l.state.Projects[string(projectID)]
	priorRecovery, hadRecovery := l.state.Recoveries[string(projectID)]
	project := priorProject
	project.ProjectID, project.LastOpenedAt, project.Missing = projectID, now, false
	if displayName != "" {
		project.DisplayName = displayName
	}
	if locationLabel != "" {
		project.LocationLabel = locationLabel
	}
	l.state.Projects[string(projectID)] = project
	l.state.Recoveries[string(projectID)] = persistedRecovery{ProjectID: projectID, CommittedRevision: revision.RevisionID, InterruptedAt: now, ProviderPending: providerPending, Autosave: AutosaveIdle}
	l.sessions[session.RuntimeSessionID] = &sessionLifecycle{projectID: projectID, session: session, committedRevision: revision.RevisionID, providerPending: providerPending, autosave: AutosaveIdle, generation: 1}
	l.byProject[projectID] = session.RuntimeSessionID
	if err := l.saveLocked(); err != nil {
		delete(l.sessions, session.RuntimeSessionID)
		delete(l.byProject, projectID)
		if hadProject {
			l.state.Projects[string(projectID)] = priorProject
		} else {
			delete(l.state.Projects, string(projectID))
		}
		if hadRecovery {
			l.state.Recoveries[string(projectID)] = priorRecovery
		} else {
			delete(l.state.Recoveries, string(projectID))
		}
		return runtimeprotocol.RuntimeSessionRef{}, "", err
	}
	return session, ProjectOpened, nil
}

func (l *projectLifecycle) session(ref runtimeprotocol.RuntimeSessionRef) (*sessionLifecycle, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	value := l.sessions[ref.RuntimeSessionID]
	if value == nil || value.session != ref {
		return nil, false
	}
	copy := *value
	return &copy, true
}

func (l *projectLifecycle) existing(projectID runtimeprotocol.DocumentID) (runtimeprotocol.RuntimeSessionRef, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	id, ok := l.byProject[projectID]
	if !ok || l.sessions[id] == nil {
		return runtimeprotocol.RuntimeSessionRef{}, false
	}
	return l.sessions[id].session, true
}

func (l *projectLifecycle) mutate(ref runtimeprotocol.RuntimeSessionRef, generation uint64, fn func(*sessionLifecycle)) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	value := l.sessions[ref.RuntimeSessionID]
	if value == nil || value.session != ref || (value.closing && generation == 0) || (generation != 0 && value.generation != generation) {
		return errors.New("project session generation is stale")
	}
	prior := *value
	priorRecovery := l.state.Recoveries[string(value.projectID)]
	fn(value)
	l.syncRecoveryLocked(value)
	if err := l.saveLocked(); err != nil {
		*value = prior
		l.state.Recoveries[string(value.projectID)] = priorRecovery
		return err
	}
	return nil
}

func (l *projectLifecycle) completeAutosave(ref runtimeprotocol.RuntimeSessionRef, generation, autosaveGeneration uint64, fn func(*sessionLifecycle)) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	value := l.sessions[ref.RuntimeSessionID]
	if value == nil || value.session != ref || value.generation != generation || value.autosaveGeneration != autosaveGeneration {
		return errors.New("autosave completion generation is stale")
	}
	prior := *value
	priorRecovery := l.state.Recoveries[string(value.projectID)]
	fn(value)
	l.syncRecoveryLocked(value)
	if err := l.saveLocked(); err != nil {
		*value = prior
		l.state.Recoveries[string(value.projectID)] = priorRecovery
		value.recoveryRequired = true
		return err
	}
	return nil
}

func (l *projectLifecycle) autosaveRecoveryRequired(ref runtimeprotocol.RuntimeSessionRef) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	value := l.sessions[ref.RuntimeSessionID]
	return value != nil && value.session == ref && value.recoveryRequired
}

func (l *projectLifecycle) requireRecovery(ref runtimeprotocol.RuntimeSessionRef) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if value := l.sessions[ref.RuntimeSessionID]; value != nil && value.session == ref {
		value.recoveryRequired = true
	}
}

func (l *projectLifecycle) begin(ref runtimeprotocol.RuntimeSessionRef) (uint64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	value := l.sessions[ref.RuntimeSessionID]
	if value == nil || value.session != ref || value.closing {
		return 0, errors.New("project session is closing or stale")
	}
	value.inflight++
	return value.generation, nil
}

func (l *projectLifecycle) end(ref runtimeprotocol.RuntimeSessionRef, generation uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	value := l.sessions[ref.RuntimeSessionID]
	if value == nil || value.session != ref || value.generation != generation || value.inflight == 0 {
		return
	}
	value.inflight--
	if value.closing && value.inflight == 0 && value.drained != nil {
		close(value.drained)
		value.drained = nil
	}
}

func (l *projectLifecycle) fenceClose(ctxDone <-chan struct{}, ref runtimeprotocol.RuntimeSessionRef) (CloseAssessment, error) {
	l.mu.Lock()
	value := l.sessions[ref.RuntimeSessionID]
	if value == nil || value.session != ref || value.closing {
		l.mu.Unlock()
		return CloseAssessment{}, errors.New("project session is closing or stale")
	}
	value.closing = true
	var drained <-chan struct{}
	if value.inflight > 0 {
		value.drained = make(chan struct{})
		drained = value.drained
	}
	l.mu.Unlock()
	if drained != nil {
		select {
		case <-drained:
		case <-ctxDone:
			l.rollbackClose(ref)
			return CloseAssessment{}, errors.New("close drain cancelled")
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	value = l.sessions[ref.RuntimeSessionID]
	if value == nil || value.session != ref || !value.closing || value.inflight != 0 {
		return CloseAssessment{}, errors.New("project session close fence lost")
	}
	value.generation++
	return assess(value), nil
}

func (l *projectLifecycle) rollbackClose(ref runtimeprotocol.RuntimeSessionRef) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if value := l.sessions[ref.RuntimeSessionID]; value != nil && value.session == ref {
		value.closing = false
		value.drained = nil
	}
}

func (l *projectLifecycle) assessment(ref runtimeprotocol.RuntimeSessionRef) (CloseAssessment, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	value := l.sessions[ref.RuntimeSessionID]
	if value == nil || value.session != ref {
		return CloseAssessment{}, false
	}
	return assess(value), true
}

func assess(value *sessionLifecycle) CloseAssessment {
	blockers := make([]CloseBlocker, 0, 4)
	if value.pendingPreview {
		blockers = append(blockers, ClosePendingPreview)
	}
	if value.ephemeralEdits {
		blockers = append(blockers, CloseEphemeralEdits)
	}
	if value.autosavePending {
		blockers = append(blockers, CloseAutosavePending)
	}
	if value.providerPending {
		blockers = append(blockers, CloseProviderPending)
	}
	if value.terminalBlocker != "" {
		blockers = append(blockers, value.terminalBlocker)
	}
	if value.recoveryRequired {
		blockers = append(blockers, CloseRecoveryRequired)
	}
	return CloseAssessment{ProjectID: value.projectID, CommittedRevision: value.committedRevision, CanClose: len(blockers) == 0, Blockers: blockers, Autosave: value.autosave}
}

func (l *projectLifecycle) detach(ref runtimeprotocol.RuntimeSessionRef) (*sessionLifecycle, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	value := l.sessions[ref.RuntimeSessionID]
	if value == nil || value.session != ref || !value.closing || value.inflight != 0 {
		return nil, nil
	}
	copy := *value
	recovery, hadRecovery := l.state.Recoveries[string(value.projectID)]
	delete(l.sessions, ref.RuntimeSessionID)
	delete(l.byProject, value.projectID)
	delete(l.state.Recoveries, string(value.projectID))
	if err := l.saveLocked(); err != nil {
		l.sessions[ref.RuntimeSessionID] = value
		l.byProject[value.projectID] = ref.RuntimeSessionID
		if hadRecovery {
			l.state.Recoveries[string(value.projectID)] = recovery
		}
		return nil, err
	}
	return &copy, nil
}

func (l *projectLifecycle) restore(value *sessionLifecycle) error {
	if value == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	value.closing = false
	value.inflight = 0
	value.drained = nil
	value.generation++
	if l.sessions[value.session.RuntimeSessionID] != nil || l.byProject[value.projectID] != "" {
		return errors.New("project lifecycle session already registered")
	}
	l.sessions[value.session.RuntimeSessionID] = value
	l.byProject[value.projectID] = value.session.RuntimeSessionID
	now := protocolcommon.Rfc3339Time(l.now().UTC().Format(time.RFC3339Nano))
	l.state.Recoveries[string(value.projectID)] = persistedRecovery{ProjectID: value.projectID, CommittedRevision: value.committedRevision, InterruptedAt: now, AutosavePending: value.autosavePending, ProviderPending: value.providerPending, TerminalBlocker: value.terminalBlocker, Autosave: value.autosave, Recovery: cloneRecoveryArtifact(value.recovery), TerminalRecovery: cloneRecoveryArtifact(value.terminalRecovery)}
	if err := l.saveLocked(); err != nil {
		delete(l.sessions, value.session.RuntimeSessionID)
		delete(l.byProject, value.projectID)
		delete(l.state.Recoveries, string(value.projectID))
		return err
	}
	return nil
}

func (l *projectLifecycle) markMissing(projectID runtimeprotocol.DocumentID, missing bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	project, ok := l.state.Projects[string(projectID)]
	if !ok {
		return nil
	}
	prior := project
	project.Missing = missing
	l.state.Projects[string(projectID)] = project
	if err := l.saveLocked(); err != nil {
		l.state.Projects[string(projectID)] = prior
		return err
	}
	return nil
}

func (l *projectLifecycle) missing(projectID runtimeprotocol.DocumentID) (bool, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	project, ok := l.state.Projects[string(projectID)]
	return project.Missing, ok
}

func (l *projectLifecycle) pin(projectID runtimeprotocol.DocumentID, pinned bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	project, ok := l.state.Projects[string(projectID)]
	if !ok {
		return errors.New("unknown project")
	}
	prior := project
	project.Pinned = pinned
	l.state.Projects[string(projectID)] = project
	if err := l.saveLocked(); err != nil {
		l.state.Projects[string(projectID)] = prior
		return err
	}
	return nil
}

func (l *projectLifecycle) recent() []RecentProject {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]RecentProject, 0, len(l.state.Projects))
	for _, project := range l.state.Projects {
		availability := ProjectAvailable
		if project.Missing {
			availability = ProjectMissing
		}
		displayName := project.DisplayName
		if displayName == "" {
			displayName = string(project.ProjectID)
		}
		result = append(result, RecentProject{ProjectID: project.ProjectID, DisplayName: displayName, LocationLabel: project.LocationLabel, Pinned: project.Pinned, LastOpenedAt: project.LastOpenedAt, Availability: availability})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Pinned != result[j].Pinned {
			return result[i].Pinned
		}
		if result[i].LastOpenedAt != result[j].LastOpenedAt {
			return result[i].LastOpenedAt > result[j].LastOpenedAt
		}
		return result[i].ProjectID < result[j].ProjectID
	})
	return result
}

func (l *projectLifecycle) recoveries() []RecoveryCandidate {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]RecoveryCandidate, 0, len(l.state.Recoveries))
	for _, value := range l.state.Recoveries {
		if _, currentlyOpen := l.byProject[value.ProjectID]; currentlyOpen {
			continue
		}
		artifact := cloneRecoveryArtifact(value.Recovery)
		result = append(result, RecoveryCandidate{ProjectID: value.ProjectID, CommittedRevision: value.CommittedRevision, InterruptedAt: value.InterruptedAt, PendingPreview: artifact != nil && artifact.Kind == RecoveryPreviewOperations, EphemeralEdits: artifact != nil, AutosavePending: value.AutosavePending, ProviderPending: value.ProviderPending, TerminalBlocker: value.TerminalBlocker, Autosave: value.Autosave, Recovery: artifact, TerminalRecovery: cloneRecoveryArtifact(value.TerminalRecovery)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].InterruptedAt < result[j].InterruptedAt })
	return result
}

func (l *projectLifecycle) recovery(projectID runtimeprotocol.DocumentID) (persistedRecovery, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	value, ok := l.state.Recoveries[string(projectID)]
	return value, ok
}

func (l *projectLifecycle) applyRecovery(ref runtimeprotocol.RuntimeSessionRef, recovery persistedRecovery) error {
	return l.mutate(ref, 0, func(value *sessionLifecycle) {
		value.committedRevision = recovery.CommittedRevision
		value.recovery = cloneRecoveryArtifact(recovery.Recovery)
		value.pendingPreview = value.recovery != nil && value.recovery.Kind == RecoveryPreviewOperations
		value.ephemeralEdits = value.recovery != nil
		value.autosavePending = false // timers do not survive process death
		value.providerPending = recovery.ProviderPending
		value.terminalBlocker = recovery.TerminalBlocker
		value.terminalRecovery = cloneRecoveryArtifact(recovery.TerminalRecovery)
		value.autosave = recovery.Autosave
		if recovery.AutosavePending {
			value.autosave = AutosaveNeedsReview
		}
	})
}

func (l *projectLifecycle) syncRecoveryLocked(value *sessionLifecycle) {
	prior := l.state.Recoveries[string(value.projectID)]
	prior.ProjectID = value.projectID
	prior.CommittedRevision = value.committedRevision
	if prior.InterruptedAt == "" {
		prior.InterruptedAt = protocolcommon.Rfc3339Time(l.now().UTC().Format(time.RFC3339Nano))
	}
	prior.AutosavePending = value.autosavePending
	prior.ProviderPending = value.providerPending
	prior.TerminalBlocker = value.terminalBlocker
	prior.TerminalRecovery = cloneRecoveryArtifact(value.terminalRecovery)
	prior.Autosave = value.autosave
	prior.Recovery = cloneRecoveryArtifact(value.recovery)
	l.state.Recoveries[string(value.projectID)] = prior
}

func (l *projectLifecycle) hasRecovery(projectID runtimeprotocol.DocumentID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, found := l.state.Recoveries[string(projectID)]
	if _, open := l.byProject[projectID]; open {
		return false
	}
	return found
}

func (l *projectLifecycle) discardRecovery(projectID runtimeprotocol.DocumentID) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	prior, ok := l.state.Recoveries[string(projectID)]
	if !ok {
		return errors.New("unknown recovery")
	}
	delete(l.state.Recoveries, string(projectID))
	if err := l.saveLocked(); err != nil {
		l.state.Recoveries[string(projectID)] = prior
		return err
	}
	return nil
}

func (l *projectLifecycle) allAssessments() []CloseAssessment {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]CloseAssessment, 0, len(l.sessions))
	for _, value := range l.sessions {
		result = append(result, assess(value))
	}
	return result
}

func (l *projectLifecycle) sessionRefs() []runtimeprotocol.RuntimeSessionRef {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]runtimeprotocol.RuntimeSessionRef, 0, len(l.sessions))
	for _, value := range l.sessions {
		result = append(result, value.session)
	}
	return result
}

func (l *projectLifecycle) active() (runtimeprotocol.RuntimeSessionRef, uint64, string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.sessions) != 1 {
		return runtimeprotocol.RuntimeSessionRef{}, 0, "", false
	}
	for _, value := range l.sessions {
		persistence := "clean"
		switch {
		case value.providerPending:
			persistence = "reconcile_pending"
		case value.pendingPreview:
			persistence = "preview_pending"
		case value.ephemeralEdits:
			persistence = "ephemeral"
		case value.autosavePending:
			persistence = "durable_pending"
		}
		return value.session, value.generation, persistence, true
	}
	return runtimeprotocol.RuntimeSessionRef{}, 0, "", false
}

func (l *projectLifecycle) saveLocked() error {
	if l.saveFault != nil {
		if err := l.saveFault(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(l.state)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(l.path), ".project-lifecycle-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, l.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(l.path))
	if err != nil {
		return err
	}
	err = privatefs.SyncDirectory(dir)
	_ = dir.Close()
	if err != nil {
		return err
	}
	ok = true
	return nil
}
