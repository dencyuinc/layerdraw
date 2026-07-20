// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package review

import (
	"context"
	"sync"
)

type MemoryStore struct {
	mu    sync.Mutex
	state Snapshot
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{state: Snapshot{Version: 1, Proposals: []Proposal{}}}
}
func (s *MemoryStore) Load(context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneSnapshot(s.state), nil
}
func (s *MemoryStore) Save(_ context.Context, state Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateSnapshot(state); err != nil {
		return err
	}
	s.state = cloneSnapshot(state)
	return nil
}
