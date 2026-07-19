// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
)

func delegationPath(root string) string { return filepath.Join(root, "access-delegations.json") }

func loadDelegations(root string) (*accesscore.DelegationStore, error) {
	path := delegationPath(root)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return accesscore.NewDelegationStore(), nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 4<<20 {
		return nil, fmt.Errorf("localdocument: insecure delegation snapshot")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snapshot accesscore.DelegationSnapshot
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("localdocument: trailing delegation snapshot data")
	}
	return accesscore.NewDelegationStoreFromSnapshot(snapshot)
}

func (h *Host) saveDelegations(snapshot accesscore.DelegationSnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(h.config.Root, ".access-delegations-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(metadataFileMode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, delegationPath(h.config.Root)); err != nil {
		return err
	}
	dir, err := os.Open(h.config.Root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (h *Host) DelegateAgent(ctx context.Context, session *Session, requested accesscore.Delegation) (accesscore.Delegation, error) {
	if session == nil || session.delegationID != "" {
		return accesscore.Delegation{}, accesscore.ErrInvalidDelegation
	}
	grant, _, err := h.authority.ResolveGrant(ctx, session.Open.Session.Scope)
	if err != nil {
		return accesscore.Delegation{}, err
	}
	h.delegationMu.Lock()
	defer h.delegationMu.Unlock()
	candidate, err := h.authority.delegationStore().Clone()
	if err != nil {
		return accesscore.Delegation{}, err
	}
	record, err := candidate.Delegate(grant, requested)
	if err != nil {
		return accesscore.Delegation{}, err
	}
	if err := h.saveDelegations(candidate.Snapshot()); err != nil {
		return accesscore.Delegation{}, err
	}
	h.authority.replaceDelegationStore(candidate)
	return record, nil
}

func (h *Host) OpenDelegatedDocument(ctx context.Context, documentID runtimeprotocol.DocumentID, delegationID string) (OpenResult, error) {
	if _, err := h.authority.delegationStore().Resolve(delegationID, h.config.Clock.Now()); err != nil {
		return OpenResult{}, err
	}
	opened, err := h.OpenDocument(ctx, documentID)
	if err != nil {
		return OpenResult{}, err
	}
	opened.Session.delegationID = delegationID
	ctx = h.accessContext(ctx, opened.Session)
	_, summary, err := h.authority.ResolveGrant(ctx, opened.Session.Open.Session.Scope)
	if err != nil {
		_ = h.Close(context.Background(), opened.Session)
		return OpenResult{}, err
	}
	opened.Session.Open.AccessSummary = summary
	return opened, nil
}

func (h *Host) RevokeDelegation(id string) error {
	h.delegationMu.Lock()
	defer h.delegationMu.Unlock()
	candidate, err := h.authority.delegationStore().Clone()
	if err != nil {
		return err
	}
	if err := candidate.Revoke(id); err != nil {
		return err
	}
	if err := h.saveDelegations(candidate.Snapshot()); err != nil {
		return err
	}
	h.authority.replaceDelegationStore(candidate)
	h.mu.Lock()
	for sessionID, session := range h.sessions {
		if session.delegationID == id {
			if cancel := h.autosaves[sessionID]; cancel != nil {
				cancel()
				delete(h.autosaves, sessionID)
			}
		}
	}
	h.mu.Unlock()
	return nil
}
