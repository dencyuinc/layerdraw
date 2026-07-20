// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registrysource

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	"github.com/dencyuinc/layerdraw/internal/registry"
)

// CredentialResolver is the narrow native-secret boundary consumed by the
// Registry broker. Implementations may use Keychain, Credential Manager, or a
// libsecret-compatible store; the connection ref remains opaque to Registry.
type CredentialResolver interface {
	ResolveCredential(context.Context, string) ([]byte, error)
}

type CredentialBroker struct {
	Resolver CredentialResolver
	LeaseTTL time.Duration
	Now      func() time.Time
}

func (b CredentialBroker) ResolveRegistryConnection(ctx context.Context, ref string) (registry.CredentialLease, error) {
	if b.Resolver == nil || ref == "" || len(ref) > 1024 || strings.ContainsAny(ref, "\x00\r\n?&#=") {
		return registry.CredentialLease{}, errors.New("Registry credential reference is invalid")
	}
	ttl := b.LeaseTTL
	if ttl <= 0 || ttl > time.Hour {
		ttl = 15 * time.Minute
	}
	now := time.Now
	if b.Now != nil {
		now = b.Now
	}
	secret, err := b.Resolver.ResolveCredential(ctx, ref)
	if err != nil || len(secret) == 0 || len(secret) > 64<<10 {
		clear(secret)
		return registry.CredentialLease{}, errors.New("Registry credential is unavailable")
	}
	owned := append([]byte(nil), secret...)
	clear(secret)
	return registry.CredentialLease{ConnectionRef: ref, Credential: owned, ExpiresAt: now().Add(ttl)}, nil
}

// AccessPort delegates the complete Engine-produced AuthoringImpact and the
// Registry-produced package HostOperationImpact to the canonical Access
// evaluator. It never derives capabilities from a transport operation name.
type AccessPort struct{ Evaluator accesscore.Evaluator }

func (a AccessPort) EvaluateRegistryPlan(ctx context.Context, input accessprotocol.EvaluateAuthoringInput) (accessprotocol.AuthoringDecision, error) {
	if input.RequestIntent != "apply" || len(input.HostOperationImpacts) != 1 || input.HostOperationImpacts[0].OperationKind != accessprotocol.HostOperationKindPackageTransaction || input.AuthoringImpact == nil {
		return accessprotocol.AuthoringDecision{}, errors.New("Registry Access input is not a closed package transaction")
	}
	return a.Evaluator.Evaluate(ctx, input)
}
