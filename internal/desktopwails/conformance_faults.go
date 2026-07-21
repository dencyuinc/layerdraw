// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

// conformanceProjectFaultRecovery injects failures through the same composed
// owners used by the installed Desktop worker. These checks intentionally run
// inside project_open instead of citing source-tree-only unit tests as
// installed evidence.
func conformanceProjectFaultRecovery(ctx context.Context) error {
	for _, scenario := range []func(context.Context) error{
		conformanceMissingFileFault,
		conformanceConcurrentOpenFault,
		conformanceStaleRevisionFault,
		conformanceCorruptStateFault,
	} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := scenario(ctx); err != nil {
			return err
		}
	}
	return nil
}

func conformanceMissingFileFault(ctx context.Context) error {
	instance, err := newConformanceInstance(ctx, false)
	if err != nil {
		return err
	}
	defer instance.close(context.Background())
	path := filepath.Join(instance.root, "removed-before-open.ldl")
	if err := os.WriteFile(path, []byte(conformanceAuthoringSource), 0o600); err != nil {
		return err
	}
	token, err := instance.vault.issue(path)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	result := instance.app.OpenProject(ctx, token)
	if result.Outcome != protocolcommon.OutcomeFailed || result.Failure == nil || result.Failure.Code != desktopcontract.FailureProjectMissing || result.Failure.Recovery != desktopcontract.RecoveryLocate {
		return errors.New("missing project did not produce the recoverable locate boundary")
	}
	return nil
}

func conformanceConcurrentOpenFault(ctx context.Context) error {
	instance, err := newConformanceInstance(ctx, false)
	if err != nil {
		return err
	}
	defer instance.close(context.Background())
	path := filepath.Join(instance.root, "concurrent.ldl")
	if err := os.WriteFile(path, []byte(conformanceAuthoringSource), 0o600); err != nil {
		return err
	}
	const count = 4
	tokens := make([]string, count)
	for index := range tokens {
		tokens[index], err = instance.vault.issue(path)
		if err != nil {
			return err
		}
	}
	results := make(chan desktopcontract.Result[desktopapp.ProjectOpenResult], count)
	var group sync.WaitGroup
	for _, token := range tokens {
		group.Add(1)
		go func(value string) {
			defer group.Done()
			results <- instance.app.OpenProject(ctx, value)
		}(token)
	}
	group.Wait()
	close(results)
	var session runtimeprotocol.RuntimeSessionRef
	opened, focused := 0, 0
	for result := range results {
		if result.Outcome != protocolcommon.OutcomeSuccess {
			return errors.New("concurrent project open failed")
		}
		if session.RuntimeSessionID == "" {
			session = result.Value.Open.Session
		} else if result.Value.Open.Session != session {
			return errors.New("concurrent project open published multiple sessions")
		}
		switch result.Value.Disposition {
		case desktopapp.ProjectOpened:
			opened++
		case desktopapp.ProjectFocused:
			focused++
		}
	}
	if opened != 1 || focused != count-1 {
		return errors.New("concurrent project open did not converge on one owner")
	}
	if result := instance.app.CloseProject(ctx, session); result.Outcome != protocolcommon.OutcomeSuccess {
		return errors.New("concurrent project session was not closable")
	}
	return nil
}

func conformanceStaleRevisionFault(ctx context.Context) error {
	instance, _, input, err := conformanceAuthoringInput(ctx, "fault_stale")
	if err != nil {
		return err
	}
	defer instance.close(context.Background())
	preview := instance.app.Preview(ctx, runtimeprotocol.PreviewOperationsInput{Session: input.Session, OperationBatch: input.OperationBatch})
	if preview.Outcome != protocolcommon.OutcomeSuccess {
		return errors.New("stale revision setup preview failed")
	}
	input.AuthoringProof = preview.Value.AuthoringProof
	if committed := instance.app.Commit(ctx, input); committed.Outcome != protocolcommon.OutcomeSuccess {
		return errors.New("stale revision setup commit failed")
	}
	input.OperationID = "conformance_fault_stale_retry"
	input.IdempotencyKey = "conformance_fault_stale_retry_idempotency"
	stale := instance.app.Commit(ctx, input)
	if stale.Outcome != protocolcommon.OutcomeFailed || stale.Failure == nil || stale.Failure.Code != desktopcontract.FailureProjectConflict {
		return errors.New("stale revision was not rejected at the installed commit boundary")
	}
	return nil
}

func conformanceCorruptStateFault(ctx context.Context) error {
	root, err := os.MkdirTemp("", "layerdraw-packaged-corrupt-state-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	corrupt := []byte(`{"version":99,"bindings":{}}`)
	path := filepath.Join(root, "local-document-bindings.json")
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		return err
	}
	base, err := NewSharedConfig(root)
	if err != nil {
		return err
	}
	application, _, err := compose(base, conformanceRuntime{}, nil)
	if err != nil {
		return err
	}
	started := application.Start(ctx)
	if started.Outcome != protocolcommon.OutcomeFailed || started.Failure == nil || started.Failure.Recovery != desktopcontract.RecoveryOpenRecovery || application.State() != desktopcontract.LifecycleRecovery {
		return errors.New("corrupt persisted state did not enter explicit recovery")
	}
	readback, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(readback, corrupt) {
		return errors.New("corrupt persisted state was reset during recovery")
	}
	return nil
}
