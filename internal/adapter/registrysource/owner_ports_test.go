// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registrysource

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
)

type credentialResolverFunc func(context.Context, string) ([]byte, error)

func (f credentialResolverFunc) ResolveCredential(ctx context.Context, ref string) ([]byte, error) {
	return f(ctx, ref)
}

func TestCredentialBrokerOwnsAndBoundsSecretLease(t *testing.T) {
	now := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	secret := []byte("secret")
	broker := CredentialBroker{
		Resolver: credentialResolverFunc(func(context.Context, string) ([]byte, error) { return secret, nil }),
		LeaseTTL: 5 * time.Minute,
		Now:      func() time.Time { return now },
	}
	lease, err := broker.ResolveRegistryConnection(context.Background(), "keychain:official")
	if err != nil || lease.ConnectionRef != "keychain:official" || string(lease.Credential) != "secret" || !lease.ExpiresAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	if string(secret) == "secret" {
		t.Fatal("resolver-owned secret was not cleared")
	}
	lease.Credential[0] = 'X'
	if string(secret) == string(lease.Credential) {
		t.Fatal("lease aliases resolver storage")
	}
	for _, ref := range []string{"", "https://x?token=secret", "keychain:x\nvalue"} {
		if _, err := broker.ResolveRegistryConnection(context.Background(), ref); err == nil {
			t.Fatalf("invalid credential ref accepted: %q", ref)
		}
	}
	failing := CredentialBroker{Resolver: credentialResolverFunc(func(context.Context, string) ([]byte, error) { return nil, errors.New("no") })}
	if _, err := failing.ResolveRegistryConnection(context.Background(), "keychain:x"); err == nil {
		t.Fatal("resolver error accepted")
	}
}

func TestAccessPortRequiresClosedPackageTransactionAndEvaluatesCapabilities(t *testing.T) {
	digest := func(value string) protocolcommon.Digest { return protocolcommon.Digest("sha256:" + value) }
	impact := semantic.AuthoringImpact{
		BaseDefinitionHash: digest("base"), ResultingDefinitionHash: digest("next"), SemanticDiffHash: digest("semantic"), SourceDiffHash: digest("source"),
		ImpactDigest: digest("impact"), Entries: []semantic.AuthoringImpactEntry{}, RequiredCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilitySchemaWrite},
	}
	host, err := accesscore.HostOperationImpact(accessprotocol.HostOperationKindPackageTransaction, "update", accessprotocol.HostResourceScope{DocumentID: "doc", LocalScopeID: "local"}, []string{"example/demo"})
	if err != nil {
		t.Fatal(err)
	}
	grant := accessprotocol.AuthoringGrantSnapshot{
		AccessFingerprint: digest("access"), ActorRef: accessprotocol.ActorRef{ActorID: "user", Kind: "user"}, HostDocumentID: "doc", LocalScopeID: "local",
		IssuedAt: "2026-07-21T00:00:00Z", MembershipVersion: "1", PolicyRefs: []accessprotocol.PolicyRef{},
		GrantedCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityPackageManage, semantic.AuthoringCapabilitySchemaWrite},
	}
	port := AccessPort{}
	input := accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &impact, GrantSnapshot: grant, HostOperationImpacts: []accessprotocol.HostOperationImpact{host}, RequestIntent: "apply"}
	decision, err := port.EvaluateRegistryPlan(context.Background(), input)
	if err != nil || decision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow || len(decision.RequiredCapabilities) != 2 {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
	input.RequestIntent = "preview"
	if _, err := port.EvaluateRegistryPlan(context.Background(), input); err == nil {
		t.Fatal("non-apply Registry decision accepted")
	}
	input.RequestIntent = "apply"
	input.GrantSnapshot.GrantedCapabilities = []semantic.AuthoringCapability{semantic.AuthoringCapabilityPackageManage}
	decision, err = port.EvaluateRegistryPlan(context.Background(), input)
	if err != nil || decision.Outcome != accessprotocol.AuthoringDecisionOutcomeDeny {
		t.Fatalf("missing schema capability decision=%+v err=%v", decision, err)
	}
}
