// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import "context"

// HostBinding is the framework-neutral surface mapped mechanically by Wails,
// stdio, or another trusted host. It adds no Registry decisions of its own.
type HostBinding struct{ registry *Registry }

func NewHostBinding(registry *Registry) (*HostBinding, error) {
	if registry == nil {
		return nil, fail(FailureUnavailable, "registry_binding", true, nil)
	}
	return &HostBinding{registry: registry}, nil
}
func (h *HostBinding) ListSources() []RegistrySource { return h.registry.Sources() }
func (h *HostBinding) ConfigureSource(source RegistrySource) error {
	return h.registry.ConfigureSource(source)
}
func (h *HostBinding) SetConnected(sourceID string, connected bool) error {
	return h.registry.SetConnected(sourceID, connected)
}
func (h *HostBinding) Search(ctx context.Context, input SearchInput) ([]ArtifactRelease, error) {
	return h.registry.Search(ctx, input)
}
func (h *HostBinding) Plan(ctx context.Context, input PlanRequest) (InstallPlan, error) {
	return h.registry.Plan(ctx, input)
}
func (h *HostBinding) Commit(ctx context.Context, input RuntimeCommitInput) (RuntimeCommitResult, error) {
	return h.registry.Commit(ctx, input)
}
func (h *HostBinding) Transaction(transactionID string) (Transaction, bool) {
	return h.registry.Transaction(transactionID)
}
func (h *HostBinding) AuthorArtifact(ctx context.Context, input AuthorArtifactRequest) (AuthoredArtifact, error) {
	return h.registry.AuthorArtifact(ctx, input)
}
