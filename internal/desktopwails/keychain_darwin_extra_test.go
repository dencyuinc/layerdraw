// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build darwin

package desktopwails

import (
	"context"
	"errors"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

// TestKeychainCredentialPortFailsClosed covers the darwin keychain resolver
// guard branches with a stubbed security(1) reader.
func TestKeychainCredentialPortFailsClosed(t *testing.T) {
	ctx := context.Background()
	port := newPlatformCredentialPort()
	if result := port.Resolve(ctx, desktopcontract.CredentialRef{ID: "keychain:"}); result.Outcome == protocolcommon.OutcomeSuccess {
		t.Fatalf("empty account resolved: %+v", result)
	}
	original := readKeychainCredential
	t.Cleanup(func() { readKeychainCredential = original })
	readKeychainCredential = func(context.Context, string) ([]byte, error) { return nil, errors.New("security unavailable") }
	if result := port.Resolve(ctx, desktopcontract.CredentialRef{ID: "keychain:account"}); result.Outcome == protocolcommon.OutcomeSuccess {
		t.Fatalf("failed keychain read resolved: %+v", result)
	}
	readKeychainCredential = func(context.Context, string) ([]byte, error) { return []byte("\n"), nil }
	if result := port.Resolve(ctx, desktopcontract.CredentialRef{ID: "keychain:account"}); result.Outcome == protocolcommon.OutcomeSuccess {
		t.Fatalf("newline-only secret resolved: %+v", result)
	}
	readKeychainCredential = func(context.Context, string) ([]byte, error) { return []byte("secret\n"), nil }
	if result := port.Resolve(ctx, desktopcontract.CredentialRef{ID: "keychain:account"}); result.Outcome != protocolcommon.OutcomeSuccess || string(result.Value) != "secret" {
		t.Fatalf("secret=%q outcome=%v", result.Value, result.Outcome)
	}
}
