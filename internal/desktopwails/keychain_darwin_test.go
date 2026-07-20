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

func TestKeychainCredentialPortUsesOpaqueAccountAndClosedFailures(t *testing.T) {
	original := readKeychainCredential
	t.Cleanup(func() { readKeychainCredential = original })
	var account string
	readKeychainCredential = func(_ context.Context, value string) ([]byte, error) {
		account = value
		return []byte("secret\n"), nil
	}
	result := (keychainCredentialPort{}).Resolve(context.Background(), desktopcontract.CredentialRef{ID: "keychain:account-1"})
	if result.Outcome != protocolcommon.OutcomeSuccess || string(result.Value) != "secret" || account != "account-1" {
		t.Fatalf("keychain result=%+v account=%q", result, account)
	}
	clear(result.Value)

	readKeychainCredential = func(context.Context, string) ([]byte, error) { return nil, errors.New("private keychain failure") }
	failed := (keychainCredentialPort{}).Resolve(context.Background(), desktopcontract.CredentialRef{ID: "keychain:missing"})
	if failed.Outcome != protocolcommon.OutcomeFailed || failed.Failure == nil || failed.Failure.Code != desktopcontract.FailureCredential || failed.Failure.Recovery != desktopcontract.RecoveryReconnect {
		t.Fatalf("closed failure=%+v", failed)
	}
	if invalid := (keychainCredentialPort{}).Resolve(context.Background(), desktopcontract.CredentialRef{ID: "keychain:bad\naccount"}); invalid.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("invalid account=%+v", invalid)
	}
}
