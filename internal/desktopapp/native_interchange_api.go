// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	nativeexport "github.com/dencyuinc/layerdraw/internal/exporter"
)

type NativePublishRequest struct {
	RequestID  string `json:"request_id"`
	ArtifactID string `json:"artifact_id"`
	Extension  string `json:"extension"`
}

type NativePublishResult struct {
	Published bool `json:"published"`
}

type ExternalImportRequest struct {
	RequestID string `json:"request_id"`
	Profile   string `json:"profile"`
	Extension string `json:"extension"`
}

func (a *Application) NativeExportProfiles() desktopcontract.Result[[]nativeexport.Profile] {
	if a.config.NativeInterchange == nil {
		return failed[[]nativeexport.Profile](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentNativeExporters, false, desktopcontract.RecoveryConfigureAdapter)
	}
	return desktopcontract.Result[[]nativeexport.Profile]{Outcome: protocolcommon.OutcomeSuccess, Value: a.config.NativeInterchange.Profiles()}
}

func (a *Application) SerializeNativeExport(ctx context.Context, input nativeexport.SerializeInput) (result desktopcontract.Result[NativeSerializeResult]) {
	if a.config.NativeInterchange == nil {
		return failed[NativeSerializeResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentNativeExporters, false, desktopcontract.RecoveryConfigureAdapter)
	}
	defer func() {
		if recover() != nil {
			result = failed[NativeSerializeResult](desktopcontract.FailureBackendPanic, desktopcontract.ComponentNativeExporters, false, desktopcontract.RecoveryExit)
		}
	}()
	value, err := a.config.NativeInterchange.Serialize(ctx, input)
	if err != nil {
		return nativeFailure[NativeSerializeResult](err)
	}
	return desktopcontract.Result[NativeSerializeResult]{Outcome: protocolcommon.OutcomeSuccess, Value: value}
}

func (a *Application) PublishNativeExportDialog(ctx context.Context, input NativePublishRequest) (result desktopcontract.Result[NativePublishResult]) {
	if a.config.NativeInterchange == nil || input.RequestID == "" || input.ArtifactID == "" || input.Extension == "" {
		return failed[NativePublishResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentNativeExporters, false, desktopcontract.RecoveryConfigureAdapter)
	}
	selection := safeDialogSelect(ctx, a.config.Dialogs, desktopcontract.DialogRequest{Kind: desktopcontract.DialogExport, RequestID: input.RequestID, Extensions: []string{input.Extension}})
	if selection.Outcome == protocolcommon.OutcomeCancelled {
		return cancelled[NativePublishResult](desktopcontract.ComponentBindingShell)
	}
	if selection.Outcome != protocolcommon.OutcomeSuccess || selection.Value.Token == "" {
		return failed[NativePublishResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentBindingShell, true, desktopcontract.RecoveryRetry)
	}
	defer func() {
		if recover() != nil {
			result = failed[NativePublishResult](desktopcontract.FailureBackendPanic, desktopcontract.ComponentNativeExporters, false, desktopcontract.RecoveryExit)
		}
	}()
	if err := a.config.NativeInterchange.Publish(ctx, selection.Value.Token, input.ArtifactID); err != nil {
		return nativeFailure[NativePublishResult](err)
	}
	return desktopcontract.Result[NativePublishResult]{Outcome: protocolcommon.OutcomeSuccess, Value: NativePublishResult{Published: true}}
}

func (a *Application) ImportExternalDialog(ctx context.Context, input ExternalImportRequest) (result desktopcontract.Result[nativeexport.ImportPreview]) {
	if a.config.NativeInterchange == nil || input.RequestID == "" || input.Profile == "" || input.Extension == "" {
		return failed[nativeexport.ImportPreview](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentNativeExporters, false, desktopcontract.RecoveryConfigureAdapter)
	}
	selection := safeDialogSelect(ctx, a.config.Dialogs, desktopcontract.DialogRequest{Kind: desktopcontract.DialogImport, RequestID: input.RequestID, Extensions: []string{input.Extension}})
	if selection.Outcome == protocolcommon.OutcomeCancelled {
		return cancelled[nativeexport.ImportPreview](desktopcontract.ComponentBindingShell)
	}
	if selection.Outcome != protocolcommon.OutcomeSuccess || selection.Value.Token == "" {
		return failed[nativeexport.ImportPreview](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentBindingShell, true, desktopcontract.RecoveryRetry)
	}
	defer func() {
		if recover() != nil {
			result = failed[nativeexport.ImportPreview](desktopcontract.FailureBackendPanic, desktopcontract.ComponentNativeExporters, false, desktopcontract.RecoveryExit)
		}
	}()
	value, err := a.config.NativeInterchange.Import(ctx, selection.Value.Token, input.Profile)
	if err != nil {
		return nativeFailure[nativeexport.ImportPreview](err)
	}
	return desktopcontract.Result[nativeexport.ImportPreview]{Outcome: protocolcommon.OutcomeSuccess, Value: value}
}

func nativeFailure[T any](err error) desktopcontract.Result[T] {
	if nativeexport.IsFailure(err, nativeexport.FailureCancelled) {
		return cancelled[T](desktopcontract.ComponentNativeExporters)
	}
	return failed[T](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentNativeExporters, true, desktopcontract.RecoveryRetry)
}
