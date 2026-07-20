// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

//go:build darwin

package desktopwails

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

const keychainService = "LayerDraw"
const maxCredentialBytes = 64 << 10

// keychainCredentialPort resolves one opaque account ID through the macOS
// Keychain. Secret material is returned only as an owned byte slice; callers
// must clear it immediately after constructing the provider session.
type keychainCredentialPort struct{}

func newPlatformCredentialPort() desktopcontract.CredentialPort { return keychainCredentialPort{} }

var readKeychainCredential = func(ctx context.Context, account string) ([]byte, error) {
	command := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", keychainService, "-a", account, "-w")
	command.Stderr = nil
	return command.Output()
}

func (keychainCredentialPort) Resolve(ctx context.Context, ref desktopcontract.CredentialRef) desktopcontract.Result[[]byte] {
	account := strings.TrimPrefix(ref.ID, "keychain:")
	if account == "" || len(account) > 512 || strings.ContainsAny(account, "\x00\r\n") {
		return closedCredentialFailure()
	}
	// The secret is stdout, never an argv/environment value. Stderr is not
	// surfaced because provider/keychain text must not cross Desktop failures.
	value, err := readKeychainCredential(ctx, account)
	if err != nil || len(value) == 0 || len(value) > maxCredentialBytes {
		clear(value)
		return closedCredentialFailure()
	}
	value = bytes.TrimSuffix(value, []byte{'\n'})
	if len(value) == 0 {
		clear(value)
		return closedCredentialFailure()
	}
	return desktopcontract.Result[[]byte]{Outcome: protocolcommon.OutcomeSuccess, Value: value}
}

func closedCredentialFailure() desktopcontract.Result[[]byte] {
	return desktopcontract.Result[[]byte]{Outcome: protocolcommon.OutcomeFailed, Failure: &desktopcontract.Failure{
		Code: desktopcontract.FailureCredential, Component: desktopcontract.ComponentAccess,
		Retryable: true, Recovery: desktopcontract.RecoveryReconnect,
	}}
}
