// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"errors"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	nativeexport "github.com/dencyuinc/layerdraw/internal/exporter"
)

type nativePortHarness struct {
	publishedToken, publishedID string
	panicSerialize              bool
}

func (*nativePortHarness) Profiles() []nativeexport.Profile { return nativeexport.Profiles() }
func (h *nativePortHarness) Serialize(context.Context, nativeexport.SerializeInput) (NativeSerializeResult, error) {
	if h.panicSerialize {
		panic("secret native path")
	}
	return NativeSerializeResult{Artifact: NativeArtifactRef{ArtifactID: "artifact_1", LogicalPath: "view.json", MediaType: "application/json", ContentDigest: protocolcommon.Digest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}, Manifest: []byte(`{"format":"layerdraw-export-sources"}`)}, nil
}
func (h *nativePortHarness) Publish(_ context.Context, token, id string) error {
	h.publishedToken, h.publishedID = token, id
	if token == "failed" {
		return errors.New("disk full at /secret")
	}
	return nil
}
func (*nativePortHarness) Import(context.Context, string, string) (nativeexport.ImportPreview, error) {
	return nativeexport.ImportPreview{Profile: nativeexport.OperationsJSONProfile}, nil
}

type nativeDialogHarness struct {
	outcome protocolcommon.Outcome
	token   string
}

func (h nativeDialogHarness) Select(context.Context, desktopcontract.DialogRequest) desktopcontract.Result[desktopcontract.DialogSelection] {
	if h.outcome == protocolcommon.OutcomeCancelled {
		return desktopcontract.Result[desktopcontract.DialogSelection]{Outcome: protocolcommon.OutcomeCancelled, Failure: &desktopcontract.Failure{Code: desktopcontract.FailureDialogCancelled, Component: desktopcontract.ComponentBindingShell, Recovery: desktopcontract.RecoveryRetry}}
	}
	return desktopcontract.Result[desktopcontract.DialogSelection]{Outcome: protocolcommon.OutcomeSuccess, Value: desktopcontract.DialogSelection{Token: h.token}}
}

func TestNativeInterchangeApplicationFlowAndClosedFailures(t *testing.T) {
	port := &nativePortHarness{}
	app := &Application{config: Config{NativeInterchange: port, Dialogs: nativeDialogHarness{outcome: protocolcommon.OutcomeSuccess, token: "opaque"}}}
	if profiles := app.NativeExportProfiles(); profiles.Outcome != protocolcommon.OutcomeSuccess || len(profiles.Value) != 3 {
		t.Fatalf("profiles=%+v", profiles)
	}
	serialized := app.SerializeNativeExport(context.Background(), nativeexport.SerializeInput{})
	if serialized.Outcome != protocolcommon.OutcomeSuccess || serialized.Value.Artifact.ArtifactID == "" {
		t.Fatalf("serialize=%+v", serialized)
	}
	published := app.PublishNativeExportDialog(context.Background(), NativePublishRequest{RequestID: "publish", ArtifactID: serialized.Value.Artifact.ArtifactID, Extension: "json"})
	if published.Outcome != protocolcommon.OutcomeSuccess || !published.Value.Published || port.publishedToken != "opaque" {
		t.Fatalf("publish=%+v port=%+v", published, port)
	}
	imported := app.ImportExternalDialog(context.Background(), ExternalImportRequest{RequestID: "import", Profile: nativeexport.OperationsJSONProfile, Extension: "json"})
	if imported.Outcome != protocolcommon.OutcomeSuccess || imported.Value.Profile != nativeexport.OperationsJSONProfile {
		t.Fatalf("import=%+v", imported)
	}
	app.config.Dialogs = nativeDialogHarness{outcome: protocolcommon.OutcomeCancelled}
	if value := app.PublishNativeExportDialog(context.Background(), NativePublishRequest{RequestID: "cancel", ArtifactID: "artifact_1", Extension: "json"}); value.Outcome != protocolcommon.OutcomeCancelled {
		t.Fatalf("cancel=%+v", value)
	}
	port.panicSerialize = true
	if value := app.SerializeNativeExport(context.Background(), nativeexport.SerializeInput{}); value.Failure == nil || value.Failure.Code != desktopcontract.FailureBackendPanic {
		t.Fatalf("panic leaked=%+v", value)
	}
}

func TestNativeInterchangeUnavailableAndInvalidInputsFailClosed(t *testing.T) {
	app := &Application{config: Config{Dialogs: nativeDialogHarness{outcome: protocolcommon.OutcomeSuccess, token: "opaque"}}}
	if value := app.NativeExportProfiles(); value.Failure == nil || value.Failure.Code != desktopcontract.FailureAdapterUnavailable {
		t.Fatalf("profiles=%+v", value)
	}
	if value := app.PublishNativeExportDialog(context.Background(), NativePublishRequest{}); value.Failure == nil {
		t.Fatalf("invalid publish=%+v", value)
	}
	if value := app.ImportExternalDialog(context.Background(), ExternalImportRequest{}); value.Failure == nil {
		t.Fatalf("invalid import=%+v", value)
	}
}
