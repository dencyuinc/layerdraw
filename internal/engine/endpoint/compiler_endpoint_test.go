// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

func TestNewCompilerEndpointComposesValidatedAuthorities(t *testing.T) {
	t.Parallel()
	config := CompilerEndpointConfig{
		EngineRelease:         "0.0.0-dev",
		SourceRevision:        "unknown",
		ReleaseManifestDigest: "sha256:5555555555555555555555555555555555555555555555555555555555555555",
		EndpointInstanceID:    "compiler-endpoint-test",
		Transports:            []string{"wasm_worker"},
		Limits:                DefaultLimitPolicy(),
	}
	authority, err := NewCompilerEndpoint(config)
	if err != nil {
		t.Fatal(err)
	}
	if authority.Descriptor == nil || authority.Dispatcher == nil {
		t.Fatalf("incomplete authority: %+v", authority)
	}
	if authority.Descriptor.EngineRelease() != "0.0.0-dev" || authority.Descriptor.EndpointInstanceID() != "compiler-endpoint-test" {
		t.Fatalf("composition lost injected identity: %+v", authority.Descriptor)
	}

	config.ReleaseManifestDigest = "not-a-digest"
	if authority, err = NewCompilerEndpoint(config); err == nil || authority != nil {
		t.Fatalf("invalid composition accepted: authority=%+v err=%v", authority, err)
	}
}

func TestFixedLimitPolicyMapsEveryPublishedLimit(t *testing.T) {
	t.Parallel()
	values := CompileEffectiveLimits{
		MaxProjectSourceFiles: 1,
		MaxProjectSourceBytes: 2,
		MaxPackFiles:          3,
		MaxPackBytes:          4,
		MaxAssets:             5,
		MaxAssetBytes:         6,
		MaxRasterDimension:    7,
		MaxRasterPixels:       8,
		MaxDeclarations:       9,
	}
	policy := FixedLimitPolicy(values)
	if policy.Defaults != policy.HardMaximums || limitsToValues(policy.Defaults) != (limitValues{
		maxProjectSourceFiles: 1,
		maxProjectSourceBytes: 2,
		maxPackFiles:          3,
		maxPackBytes:          4,
		maxAssets:             5,
		maxAssetBytes:         6,
		maxRasterDimension:    7,
		maxRasterPixels:       8,
		maxDeclarations:       9,
	}) {
		t.Fatalf("fixed policy mapping drifted: %+v", policy)
	}
}

func TestEndpointPublicCallerMisuseGuards(t *testing.T) {
	t.Parallel()
	request := engineprotocol.CompileRequestEnvelope{RequestID: "request"}
	var nilDispatcher *CompileDispatcher
	if plan, terminal, err := nilDispatcher.PrepareCompile(context.Background(), nil, request); err == nil || plan != nil || terminal != nil {
		t.Fatalf("nil dispatcher accepted: plan=%v terminal=%v err=%v", plan, terminal, err)
	}
	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	if plan, terminal, err := dispatcher.PrepareCompile(nil, nil, request); err == nil || plan != nil || terminal != nil {
		t.Fatalf("nil context accepted: plan=%v terminal=%v err=%v", plan, terminal, err)
	}
	request.RequestID = ""
	if plan, terminal, err := dispatcher.PrepareCompile(context.Background(), nil, request); err == nil || plan != nil || terminal != nil {
		t.Fatalf("empty request ID accepted: plan=%v terminal=%v err=%v", plan, terminal, err)
	}

	var nilPlan *CompilePlan
	if nilPlan.BlobRequirements() != nil || nilPlan.AdmissionBudget() != (CompileAdmissionBudget{}) {
		t.Fatal("nil plan metadata accessors are unsafe")
	}
	if _, err := nilPlan.Execute(context.Background(), nil, nil); err == nil {
		t.Fatal("nil plan execution was accepted")
	}
	nilPlan.Abort()

	var nilDescriptor *Descriptor
	if nilDescriptor.EngineRelease() != "" || nilDescriptor.SourceRevision() != "" || nilDescriptor.ReleaseManifestDigest() != "" || nilDescriptor.EndpointInstanceID() != "" || nilDescriptor.Transports() != nil || nilDescriptor.Operations() != nil || nilDescriptor.Limits() != (LimitPolicy{}) {
		t.Fatal("nil descriptor accessors are unsafe")
	}
	if _, _, err := nilDescriptor.Negotiate(context.Background(), engineprotocol.HandshakeRequestEnvelope{}); err == nil {
		t.Fatal("nil descriptor negotiation was accepted")
	}
	if _, err := nilDescriptor.RejectHandshakeConnectionState("request"); err == nil {
		t.Fatal("nil descriptor connection-state rejection was accepted")
	}
	descriptor := newTestDescriptor(t)
	if _, _, err := descriptor.Negotiate(nil, engineprotocol.HandshakeRequestEnvelope{}); err == nil {
		t.Fatal("nil negotiation context was accepted")
	}
	if _, _, err := descriptor.Negotiate(context.Background(), engineprotocol.HandshakeRequestEnvelope{}); err == nil {
		t.Fatal("untrustworthy handshake request ID was accepted")
	}
	rejected, err := descriptor.RejectHandshakeConnectionState("request")
	if err != nil || rejected.Outcome != "rejected" || len(rejected.Diagnostics) != 1 || rejected.Diagnostics[0].Code != DiagnosticHandshakeInvalidConnectionState {
		t.Fatalf("invalid connection-state response=%+v err=%v", rejected, err)
	}

	var nilContext *NegotiatedContext
	if nilContext.EndpointInstanceID() != "" || nilContext.ManifestETag() != "" || nilContext.EngineRelease() != "" || nilContext.ReleaseManifestDigest() != "" || nilContext.ProtocolName() != "" || nilContext.ProtocolVersion() != "" || nilContext.ProtocolSchemaDigest() != "" || nilContext.Operations() != nil || nilContext.SupportsOperation(OperationCompile) || nilContext.DefaultCompileLimits() != (engine.ResourceLimits{}) || nilContext.EffectiveMaximumCompileLimits() != (engine.ResourceLimits{}) {
		t.Fatal("nil negotiated context accessors are unsafe")
	}
}
