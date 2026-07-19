// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
)

func TestDelegationSnapshotLoadingFailsClosed(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		store, err := loadDelegations(t.TempDir())
		if err != nil || store == nil {
			t.Fatalf("store=%v err=%v", store, err)
		}
	})
	for _, test := range []struct {
		name    string
		content string
		mode    os.FileMode
	}{
		{name: "permissive", content: `{"version":1,"records":[],"revoked":{}}`, mode: 0o644},
		{name: "invalid json", content: `{`, mode: 0o600},
		{name: "unknown field", content: `{"version":1,"records":[],"revoked":{},"extra":true}`, mode: 0o600},
		{name: "trailing value", content: `{"version":1,"records":[],"revoked":{}} {}`, mode: 0o600},
		{name: "invalid snapshot", content: `{"version":2,"records":[],"revoked":{}}`, mode: 0o600},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(delegationPath(root), []byte(test.content), test.mode); err != nil {
				t.Fatal(err)
			}
			if _, err := loadDelegations(root); err == nil {
				t.Fatal("unsafe delegation snapshot accepted")
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "target")
		if err := os.WriteFile(target, []byte(`{"version":1,"records":[],"revoked":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, delegationPath(root)); err != nil {
			t.Fatal(err)
		}
		if _, err := loadDelegations(root); err == nil {
			t.Fatal("delegation symlink accepted")
		}
	})
}

func TestDelegationHostRejectsInvalidManagementRoutes(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root, "project p \"P\" {}\n")
	host := newTestHost(t, filepath.Join(root, "data"), nil)
	opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.DelegateAgent(context.Background(), nil, accesscore.Delegation{}); !errors.Is(err, accesscore.ErrInvalidDelegation) {
		t.Fatalf("nil parent session: %v", err)
	}
	if _, err := host.OpenDelegatedDocument(context.Background(), opened.Session.Open.CommittedRevision.DocumentID, "missing"); !errors.Is(err, accesscore.ErrGrantStale) {
		t.Fatalf("missing delegation: %v", err)
	}
	if err := host.RevokeDelegation("missing"); !errors.Is(err, accesscore.ErrGrantStale) {
		t.Fatalf("missing revoke: %v", err)
	}
	now := host.config.Clock.Now()
	bad := accesscore.Delegation{ID: "escalated", ParentActor: accessprotocol.ActorRef{ActorID: "local-owner", Kind: "user"}, Agent: accessprotocol.ActorRef{ActorID: "agent", Kind: "agent"}, DocumentID: string(opened.Session.Open.CommittedRevision.DocumentID), LocalScopeID: opened.Session.Open.Session.Scope.LocalScopeID, AuthoringCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapability("unknown")}, Permissions: accesscore.AgentPermissions{Read: true}, IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	if _, err := host.DelegateAgent(context.Background(), opened.Session, bad); !errors.Is(err, accesscore.ErrInvalidDelegation) {
		t.Fatalf("escalated delegation: %v", err)
	}
	delegated := *opened.Session
	delegated.delegationID = "already-delegated"
	if _, err := host.DelegateAgent(context.Background(), &delegated, bad); !errors.Is(err, accesscore.ErrInvalidDelegation) {
		t.Fatalf("delegated parent: %v", err)
	}
	wrongScope := *opened.Session
	wrongScope.Open.Session.Scope.DocumentID = "wrong-document"
	if _, err := host.DelegateAgent(context.Background(), &wrongScope, bad); err == nil {
		t.Fatal("mismatched parent scope accepted")
	}

	originalRoot := host.config.Root
	host.config.Root = filepath.Join(root, "missing", "data")
	if err := host.saveDelegations(accesscore.NewDelegationStore().Snapshot()); err == nil {
		t.Fatal("delegation persistence to missing root succeeded")
	}
	valid := bad
	valid.ID = "transactional"
	valid.AuthoringCapabilities = []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite}
	if _, err := host.DelegateAgent(context.Background(), opened.Session, valid); err == nil {
		t.Fatal("delegation reported success after persistence failure")
	}
	host.config.Root = originalRoot
	if _, err := host.authority.delegationStore().Resolve(valid.ID, now); !errors.Is(err, accesscore.ErrGrantStale) {
		t.Fatalf("failed persistence changed live store: %v", err)
	}
	if _, err := host.DelegateAgent(context.Background(), opened.Session, valid); err != nil {
		t.Fatal(err)
	}
	if _, err := host.OpenDelegatedDocument(context.Background(), "missing-document", valid.ID); err == nil {
		t.Fatal("delegation opened an unknown document")
	}
	host.config.Root = filepath.Join(root, "missing", "data")
	if err := host.RevokeDelegation(valid.ID); err == nil {
		t.Fatal("revocation reported success after persistence failure")
	}
	host.config.Root = originalRoot
	if _, err := host.authority.delegationStore().Resolve(valid.ID, now); err != nil {
		t.Fatalf("failed revocation changed live store: %v", err)
	}

	// A snapshot larger than the fixed bound is rejected without decoding.
	oversizedRoot := t.TempDir()
	if err := os.WriteFile(delegationPath(oversizedRoot), make([]byte, (4<<20)+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDelegations(oversizedRoot); err == nil {
		t.Fatal("oversized delegation snapshot accepted")
	}
}

func TestCommittedRevisionEqualityCoversOptionalProviderVersion(t *testing.T) {
	base := runtimeprotocol.CommittedRevisionRef{DocumentID: "doc", RevisionID: "rev", DefinitionHash: digestJSON("definition"), GraphHash: digestJSON("graph")}
	if !sameCommittedRevision(base, base) {
		t.Fatal("identical revisions differ")
	}
	changed := base
	changed.GraphHash = digestJSON("changed")
	if sameCommittedRevision(base, changed) {
		t.Fatal("changed graph hash matched")
	}
	provider := runtimeprotocol.ProviderVersionToken("provider-1")
	withProvider := base
	withProvider.ProviderVersion = &provider
	if sameCommittedRevision(base, withProvider) || !sameCommittedRevision(withProvider, withProvider) {
		t.Fatal("optional provider version mismatch")
	}
	otherProvider := runtimeprotocol.ProviderVersionToken("provider-2")
	changedProvider := withProvider
	changedProvider.ProviderVersion = &otherProvider
	if sameCommittedRevision(withProvider, changedProvider) {
		t.Fatal("changed provider version matched")
	}
}
