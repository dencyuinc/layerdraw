// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

const (
	testReleaseDigest = "sha256:5555555555555555555555555555555555555555555555555555555555555555"
	testWrongDigest   = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func validConfig() DescriptorConfig {
	return DescriptorConfig{
		EngineRelease:         engine.DevelopmentVersion,
		SourceRevision:        engine.UnknownSourceRevision,
		ReleaseManifestDigest: testReleaseDigest,
		EndpointInstanceID:    "test-engine",
		Transports:            []string{TransportInProcess},
		Limits:                DefaultLimitPolicy(),
	}
}

func newTestDescriptor(t *testing.T) *Descriptor {
	t.Helper()
	descriptor, err := NewDescriptor(validConfig())
	if err != nil {
		t.Fatal(err)
	}
	return descriptor
}

func validRequest() engineprotocol.HandshakeRequestEnvelope {
	return engineprotocol.HandshakeRequestEnvelope{
		Operation: engineprotocol.HandshakeRequestEnvelopeOperationValue,
		Payload: protocolcommon.HandshakeRequest{
			ClientRelease:        "9.8.7",
			OptionalCapabilities: []protocolcommon.CapabilityID{},
			Protocols: []protocolcommon.ProtocolOffer{{
				Name:           ProtocolName,
				SupportedRange: "1.0..1.0",
				Versions: []protocolcommon.ProtocolVersionBinding{{
					Version:      ProtocolVersion,
					SchemaDigest: protocolcommon.Digest(engineprotocol.SchemaDigest),
				}},
			}},
			RequiredCapabilities: []protocolcommon.CapabilityID{OperationCompile},
		},
		Protocol:  bootstrapProtocolRef(),
		RequestID: "request-1",
	}
}

func negotiate(t *testing.T, descriptor *Descriptor, request engineprotocol.HandshakeRequestEnvelope) (engineprotocol.HandshakeResponseEnvelope, *NegotiatedContext) {
	t.Helper()
	response, negotiated, err := descriptor.Negotiate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return response, negotiated
}

func TestNegotiateCompatibleEngineProtocol(t *testing.T) {
	t.Parallel()
	descriptor := newTestDescriptor(t)
	request := validRequest()
	request.Payload.OptionalCapabilities = []protocolcommon.CapabilityID{"engine.future", OperationHandshake}

	response, negotiated := negotiate(t, descriptor, request)
	if response.Outcome != protocolcommon.OutcomeSuccess || response.Payload == nil || response.Failure != nil {
		t.Fatalf("unexpected success response: %+v", response)
	}
	if response.RequestID != request.RequestID || response.EngineRelease != descriptor.EngineRelease() || response.Payload.HostRelease != response.EngineRelease {
		t.Fatalf("request/release metadata was not preserved: %+v", response)
	}
	if response.Protocol != bootstrapProtocolRef() {
		t.Fatalf("unexpected bootstrap protocol: %+v", response.Protocol)
	}
	if len(response.Payload.NegotiatedProtocols) != 1 {
		t.Fatalf("unexpected negotiated protocols: %+v", response.Payload.NegotiatedProtocols)
	}
	selected := response.Payload.NegotiatedProtocols[0]
	if selected.Name != ProtocolName || selected.Version != ProtocolVersion || selected.SchemaDigest != protocolcommon.Digest(engineprotocol.SchemaDigest) {
		t.Fatalf("wrong generated protocol binding: %+v", selected)
	}
	if response.Payload.EndpointInstanceID != descriptor.EndpointInstanceID() || response.Payload.ReleaseManifestDigest != descriptor.ReleaseManifestDigest() {
		t.Fatalf("wrong endpoint/release identity: %+v", response.Payload)
	}

	statuses := response.Payload.CapabilityStatuses
	if len(statuses) != 3 || !slices.IsSortedFunc(statuses, func(left, right protocolcommon.RequestedCapabilityStatus) int {
		return strings.Compare(string(left.CapabilityID), string(right.CapabilityID))
	}) {
		t.Fatalf("statuses are not complete and sorted: %+v", statuses)
	}
	if statuses[0].CapabilityID != OperationCompile || !statuses[0].Enabled || statuses[0].UnavailableReason != nil {
		t.Fatalf("compile status is not enabled: %+v", statuses[0])
	}
	if statuses[1].CapabilityID != "engine.future" || statuses[1].Enabled || statuses[1].UnavailableReason == nil || *statuses[1].UnavailableReason != protocolcommon.UnavailableReasonUnsupported {
		t.Fatalf("unknown optional status is not explicit: %+v", statuses[1])
	}
	if statuses[2].CapabilityID != OperationHandshake || !statuses[2].Enabled {
		t.Fatalf("handshake status is not enabled: %+v", statuses[2])
	}

	manifest := response.Payload.CapabilityManifest
	if manifest.ManifestScope != protocolcommon.ManifestScopeEndpoint || manifest.ManifestVersion != 1 {
		t.Fatalf("unexpected manifest identity: %+v", manifest)
	}
	if !slices.Equal(manifest.Transports, []string{TransportInProcess}) || len(manifest.Operations) != 2 {
		t.Fatalf("unexpected endpoint catalog: %+v", manifest)
	}
	for _, operation := range []string{OperationCompile, OperationHandshake} {
		capability, found := manifest.Operations[operation]
		if !found || !capability.Enabled || capability.UnavailableReason != nil || capability.ProtocolVersion != ProtocolVersion {
			t.Fatalf("invalid operation capability %s: %+v", operation, capability)
		}
		if capability.RequiredAuthoringCapabilities != nil {
			t.Fatalf("read-only operation unexpectedly requires authoring capability: %+v", capability)
		}
	}
	if manifest.Operations[OperationCompile].Limits == nil || manifest.Operations[OperationHandshake].Limits != nil {
		t.Fatalf("operation limit ownership is incorrect: %+v", manifest.Operations)
	}
	assertEmptyManifestSurfaces(t, manifest)
	computedETag, err := manifestETag(manifest)
	if err != nil || computedETag != manifest.ManifestEtag {
		t.Fatalf("manifest ETag mismatch: got=%s recomputed=%s err=%v", manifest.ManifestEtag, computedETag, err)
	}

	if negotiated == nil || negotiated.ProtocolName() != ProtocolName || negotiated.ProtocolVersion() != ProtocolVersion || negotiated.ProtocolSchemaDigest() != protocolcommon.Digest(engineprotocol.SchemaDigest) {
		t.Fatalf("invalid negotiated context: %+v", negotiated)
	}
	if negotiated.EndpointInstanceID() != descriptor.EndpointInstanceID() || negotiated.ManifestETag() != manifest.ManifestEtag || negotiated.EngineRelease() != response.EngineRelease || negotiated.ReleaseManifestDigest() != descriptor.ReleaseManifestDigest() {
		t.Fatalf("context identity does not match response")
	}
	if !negotiated.SupportsOperation(OperationCompile) || !negotiated.SupportsOperation(OperationHandshake) || negotiated.SupportsOperation("engine.describe") {
		t.Fatalf("context operation set is not exact: %v", negotiated.Operations())
	}
	if _, err := engineprotocol.EncodeHandshakeResponseEnvelope(response); err != nil {
		t.Fatalf("success response violates generated schema: %v", err)
	}
}

func assertEmptyManifestSurfaces(t *testing.T, manifest protocolcommon.CapabilityManifest) {
	t.Helper()
	values := []any{
		manifest.QueryAdapters,
		manifest.RealtimeProfiles,
		manifest.RegistrySources,
		manifest.RendererProfiles,
		manifest.ExporterProfiles,
		manifest.SearchProfiles,
		manifest.EmbeddingProfiles,
		manifest.RequiredLadybugPrimitives,
		manifest.StorageCapabilities,
	}
	for _, value := range values {
		reflected := reflect.ValueOf(value)
		if reflected.IsNil() || reflected.Len() != 0 {
			t.Fatalf("required pure-endpoint surface is not an explicit empty array: %#v", value)
		}
	}
	if manifest.ActorScopeDigest != nil || manifest.AuthoringGrantSummary != nil {
		t.Fatalf("Actor/authorization metadata leaked into pure endpoint manifest: %+v", manifest)
	}
}

func TestNegotiateRejections(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(*engineprotocol.HandshakeRequestEnvelope)
		code string
	}{
		{
			name: "major mismatch",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Payload.Protocols[0].SupportedRange = "2.0..2.1"
				request.Payload.Protocols[0].Versions = []protocolcommon.ProtocolVersionBinding{{Version: "2.1", SchemaDigest: testWrongDigest}}
			},
			code: DiagnosticMajorVersionMismatch,
		},
		{
			name: "same major range mismatch",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Payload.Protocols[0].SupportedRange = "1.1..1.2"
				request.Payload.Protocols[0].Versions = []protocolcommon.ProtocolVersionBinding{{Version: "1.2", SchemaDigest: testWrongDigest}}
			},
			code: DiagnosticVersionRangeMismatch,
		},
		{
			name: "missing exact version binding",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Payload.Protocols[0].SupportedRange = "1.0..1.1"
				request.Payload.Protocols[0].Versions = []protocolcommon.ProtocolVersionBinding{{Version: "1.1", SchemaDigest: testWrongDigest}}
			},
			code: DiagnosticVersionRangeMismatch,
		},
		{
			name: "schema digest mismatch",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Payload.Protocols[0].Versions[0].SchemaDigest = testWrongDigest
			},
			code: DiagnosticSchemaDigestMismatch,
		},
		{
			name: "missing required capability",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Payload.RequiredCapabilities = []protocolcommon.CapabilityID{"engine.alpha", "engine.zeta"}
			},
			code: DiagnosticRequiredCapabilityMissing,
		},
		{
			name: "missing Engine offer",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Payload.Protocols[0].Name = "runtime"
			},
			code: DiagnosticInvalidHandshake,
		},
		{
			name: "duplicate required capability",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Payload.RequiredCapabilities = []protocolcommon.CapabilityID{OperationCompile, OperationCompile}
			},
			code: DiagnosticInvalidHandshake,
		},
		{
			name: "duplicate optional capability",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Payload.OptionalCapabilities = []protocolcommon.CapabilityID{"engine.future", "engine.future"}
			},
			code: DiagnosticInvalidHandshake,
		},
		{
			name: "required optional overlap",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Payload.OptionalCapabilities = []protocolcommon.CapabilityID{OperationCompile}
			},
			code: DiagnosticInvalidHandshake,
		},
		{
			name: "wrong operation identity",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Operation = "engine.compile"
			},
			code: DiagnosticInvalidHandshake,
		},
		{
			name: "wrong protocol name identity",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Protocol.Name = "runtime"
			},
			code: DiagnosticInvalidHandshake,
		},
		{
			name: "wrong bootstrap version identity",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Protocol.Version = "2.0"
			},
			code: DiagnosticInvalidHandshake,
		},
		{
			name: "malformed range",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Payload.Protocols[0].SupportedRange = "1.0 - 1.1"
			},
			code: DiagnosticInvalidHandshake,
		},
		{
			name: "duplicate protocol name",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Payload.Protocols = append(request.Payload.Protocols, request.Payload.Protocols[0])
			},
			code: DiagnosticInvalidHandshake,
		},
		{
			name: "duplicate exact version",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Payload.Protocols[0].Versions = append(request.Payload.Protocols[0].Versions, request.Payload.Protocols[0].Versions[0])
			},
			code: DiagnosticInvalidHandshake,
		},
		{
			name: "invalid release",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Payload.ClientRelease = "release/latest"
			},
			code: DiagnosticInvalidHandshake,
		},
		{
			name: "nil required set",
			edit: func(request *engineprotocol.HandshakeRequestEnvelope) {
				request.Payload.RequiredCapabilities = nil
			},
			code: DiagnosticInvalidHandshake,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			descriptor := newTestDescriptor(t)
			request := validRequest()
			request.RequestID = "reject-" + strings.ReplaceAll(test.name, " ", "-")
			test.edit(&request)
			response, negotiated := negotiate(t, descriptor, request)
			assertRejection(t, response, request.RequestID, descriptor.EngineRelease(), test.code)
			if negotiated != nil {
				t.Fatal("rejection returned a negotiated context")
			}
		})
	}
}

func assertRejection(t *testing.T, response engineprotocol.HandshakeResponseEnvelope, requestID string, release protocolcommon.ReleaseVersion, code string) {
	t.Helper()
	if response.Outcome != protocolcommon.OutcomeRejected || response.Payload != nil || response.Failure != nil || len(response.Diagnostics) != 1 {
		t.Fatalf("invalid rejected envelope: %+v", response)
	}
	if response.Diagnostics[0].Code != code || response.Diagnostics[0].Severity != protocolcommon.ProtocolDiagnosticSeverityError || len(response.Diagnostics[0].Related) != 0 || response.Diagnostics[0].Remediation == nil {
		t.Fatalf("invalid stable diagnostic: %+v", response.Diagnostics[0])
	}
	if response.RequestID != requestID || response.EngineRelease != release || response.Protocol != bootstrapProtocolRef() {
		t.Fatalf("rejection metadata mismatch: %+v", response)
	}
	if _, err := engineprotocol.EncodeHandshakeResponseEnvelope(response); err != nil {
		t.Fatalf("rejected response violates generated schema: %v", err)
	}
}

func TestRejectionSafeDetailsAreStableAndMinimal(t *testing.T) {
	t.Parallel()
	descriptor := newTestDescriptor(t)
	request := validRequest()
	request.Payload.RequiredCapabilities = []protocolcommon.CapabilityID{"engine.zeta", "engine.alpha"}
	response, _ := negotiate(t, descriptor, request)
	data := response.Diagnostics[0].Data
	if data == nil {
		t.Fatal("missing safe diagnostic data")
	}
	missing := (*data)[DiagnosticDataMissingCapabilities]
	if missing.Kind != protocolcommon.JsonValueKindArray || len(missing.Array) != 2 || missing.Array[0].String != "engine.alpha" || missing.Array[1].String != "engine.zeta" {
		t.Fatalf("missing capability details are not sorted/request-scoped: %+v", missing)
	}
	encoded, err := engineprotocol.EncodeHandshakeResponseEnvelope(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"unknown", "source_revision", "credential", "actor_context", "runtime_session", "backend_locator", "entitlement"} {
		if bytes.Contains(bytes.ToLower(encoded), []byte(forbidden)) {
			t.Fatalf("safe rejection leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestClientLimitsProduceEffectiveManifestAndContext(t *testing.T) {
	t.Parallel()
	config := validConfig()
	hard := config.Limits.HardMaximums
	hard.MaxAssets *= 2
	hard.MaxPackFiles *= 2
	config.Limits.HardMaximums = hard
	descriptor, err := NewDescriptor(config)
	if err != nil {
		t.Fatal(err)
	}
	request := validRequest()
	request.Payload.ClientLimits = &protocolcommon.CompileResourceLimitConstraints{
		MaxAssets:    positive(5),
		MaxPackFiles: positive(hard.MaxPackFiles * 2),
	}
	response, negotiated := negotiate(t, descriptor, request)
	manifest := response.Payload.CapabilityManifest
	if manifest.ManifestScope != protocolcommon.ManifestScopeEffective {
		t.Fatalf("client-scoped limits did not produce an effective manifest: %s", manifest.ManifestScope)
	}
	assetLimit := manifest.Limits.MaxAssets
	if assetLimit.DefaultValue != "4096" || assetLimit.HardMaximum != "8192" || assetLimit.EffectiveMaximum != "5" || assetLimit.Unit != protocolcommon.ItemResourceLimitCapabilityUnitValue {
		t.Fatalf("invalid asset limit negotiation: %+v", assetLimit)
	}
	packLimit := manifest.Limits.MaxPackFiles
	if packLimit.DefaultValue != "16384" || packLimit.HardMaximum != "32768" || packLimit.EffectiveMaximum != "32768" {
		t.Fatalf("client raised or corrupted hard maximum: %+v", packLimit)
	}
	operation := manifest.Operations[OperationCompile]
	if operation.Limits == nil || operation.Limits.MaxAssets == nil || *operation.Limits.MaxAssets != "5" || operation.Limits.MaxPackFiles == nil || *operation.Limits.MaxPackFiles != "32768" {
		t.Fatalf("compile operation limits are incomplete: %+v", operation.Limits)
	}
	if negotiated.DefaultCompileLimits().MaxAssets != 5 || negotiated.EffectiveMaximumCompileLimits().MaxAssets != 5 {
		t.Fatalf("default was not capped by client maximum: default=%+v max=%+v", negotiated.DefaultCompileLimits(), negotiated.EffectiveMaximumCompileLimits())
	}
	if negotiated.DefaultCompileLimits().MaxPackFiles != engine.DefaultResourceLimits().MaxPackFiles || negotiated.EffectiveMaximumCompileLimits().MaxPackFiles != hard.MaxPackFiles {
		t.Fatalf("higher client ceiling changed default/hard semantics: default=%+v max=%+v", negotiated.DefaultCompileLimits(), negotiated.EffectiveMaximumCompileLimits())
	}

	invalid := validRequest()
	zero := protocolcommon.CanonicalPositiveInt64("0")
	invalid.Payload.ClientLimits = &protocolcommon.CompileResourceLimitConstraints{MaxAssets: &zero}
	rejected, context := negotiate(t, descriptor, invalid)
	assertRejection(t, rejected, invalid.RequestID, descriptor.EngineRelease(), DiagnosticInvalidHandshake)
	if context != nil {
		t.Fatal("invalid zero client limit returned a context")
	}
}

func TestDescriptorValidationAndDefensiveCopies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(*DescriptorConfig)
	}{
		{"invalid release", func(config *DescriptorConfig) { config.EngineRelease = "v1" }},
		{"invalid revision", func(config *DescriptorConfig) { config.SourceRevision = "/tmp/revision" }},
		{"invalid release digest", func(config *DescriptorConfig) { config.ReleaseManifestDigest = "sha256:ABC" }},
		{"invalid endpoint ID", func(config *DescriptorConfig) { config.EndpointInstanceID = "host/path" }},
		{"missing transport", func(config *DescriptorConfig) { config.Transports = nil }},
		{"invalid transport", func(config *DescriptorConfig) { config.Transports = []string{"HTTP Secret"} }},
		{"long transport", func(config *DescriptorConfig) { config.Transports = []string{"a" + strings.Repeat("b", 64)} }},
		{"duplicate transport", func(config *DescriptorConfig) { config.Transports = []string{TransportInProcess, TransportInProcess} }},
		{"zero default", func(config *DescriptorConfig) { config.Limits.Defaults.MaxAssets = 0 }},
		{"zero hard maximum", func(config *DescriptorConfig) { config.Limits.HardMaximums.MaxAssets = 0 }},
		{"default exceeds maximum", func(config *DescriptorConfig) { config.Limits.HardMaximums.MaxAssets = 1 }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := validConfig()
			test.edit(&config)
			if _, err := NewDescriptor(config); err == nil {
				t.Fatal("invalid descriptor was accepted")
			}
		})
	}

	config := validConfig()
	config.SourceRevision = "2420c79361ba6875a997ff1053f559c051a4b14b"
	config.Transports = []string{"wasm_worker", TransportInProcess}
	descriptor, err := NewDescriptor(config)
	if err != nil {
		t.Fatal(err)
	}
	config.Transports[0] = "mutated"
	transports := descriptor.Transports()
	if !slices.Equal(transports, []string{TransportInProcess, "wasm_worker"}) {
		t.Fatalf("transports were not sorted/copied: %v", transports)
	}
	transports[0] = "mutated"
	operations := descriptor.Operations()
	operations[0] = "engine.describe"
	if descriptor.Transports()[0] != TransportInProcess || descriptor.Operations()[0] != OperationCompile {
		t.Fatal("descriptor getter exposed mutable storage")
	}
	if descriptor.SourceRevision() != "2420c79361ba6875a997ff1053f559c051a4b14b" || descriptor.Limits() != DefaultLimitPolicy() {
		t.Fatal("descriptor metadata getters changed validated values")
	}
}

func TestManifestETagDeterminismAndSensitivity(t *testing.T) {
	t.Parallel()
	base := newTestDescriptor(t)
	baseResponse, _ := negotiate(t, base, validRequest())
	baseManifest := baseResponse.Payload.CapabilityManifest
	baseETag := baseManifest.ManifestEtag

	metadataConfig := validConfig()
	metadataConfig.EngineRelease = "1.2.3"
	metadataConfig.SourceRevision = "1234567"
	metadataConfig.ReleaseManifestDigest = "sha256:6666666666666666666666666666666666666666666666666666666666666666"
	metadataConfig.EndpointInstanceID = "another-engine"
	metadata, err := NewDescriptor(metadataConfig)
	if err != nil {
		t.Fatal(err)
	}
	metadataResponse, _ := negotiate(t, metadata, validRequest())
	if metadataResponse.Payload.CapabilityManifest.ManifestEtag != baseETag {
		t.Fatal("non-manifest release/instance metadata changed manifest ETag")
	}

	transportConfig := validConfig()
	transportConfig.Transports = []string{TransportInProcess, "wasm_worker"}
	transport, err := NewDescriptor(transportConfig)
	if err != nil {
		t.Fatal(err)
	}
	transportResponse, _ := negotiate(t, transport, validRequest())
	if transportResponse.Payload.CapabilityManifest.ManifestEtag == baseETag {
		t.Fatal("transport change did not change manifest ETag")
	}

	limitConfig := validConfig()
	limitConfig.Limits.HardMaximums.MaxAssets++
	limits, err := NewDescriptor(limitConfig)
	if err != nil {
		t.Fatal(err)
	}
	limitResponse, _ := negotiate(t, limits, validRequest())
	if limitResponse.Payload.CapabilityManifest.ManifestEtag == baseETag {
		t.Fatal("limit change did not change manifest ETag")
	}

	reordered := baseManifest
	reordered.Operations = map[string]protocolcommon.OperationCapability{
		OperationHandshake: baseManifest.Operations[OperationHandshake],
		OperationCompile:   baseManifest.Operations[OperationCompile],
	}
	reorderedETag, err := manifestETag(reordered)
	if err != nil || reorderedETag != baseETag {
		t.Fatalf("map insertion order changed ETag: %s %v", reorderedETag, err)
	}

	changed := baseManifest
	changed.RendererProfiles = []protocolcommon.ProfileCapability{{ID: "renderer.test", Version: "1", Enabled: true}}
	changedETag, err := manifestETag(changed)
	if err != nil || changedETag == baseETag {
		t.Fatalf("profile change did not change ETag: %s %v", changedETag, err)
	}

	disabled := baseManifest
	disabled.Operations = cloneOperationMap(baseManifest.Operations)
	disabledCompile := disabled.Operations[OperationCompile]
	disabledCompile.Enabled = false
	disabledReason := protocolcommon.UnavailableReasonDegraded
	disabledCompile.UnavailableReason = &disabledReason
	disabled.Operations[OperationCompile] = disabledCompile
	disabledETag, err := manifestETag(disabled)
	if err != nil || disabledETag == baseETag {
		t.Fatalf("operation enabled-state change did not change ETag: %s %v", disabledETag, err)
	}

	authoring := baseManifest
	authoring.Operations = cloneOperationMap(baseManifest.Operations)
	authoringCompile := authoring.Operations[OperationCompile]
	requiredAuthoring := []string{"schema:write"}
	authoringCompile.RequiredAuthoringCapabilities = &requiredAuthoring
	authoring.Operations[OperationCompile] = authoringCompile
	authoringETag, err := manifestETag(authoring)
	if err != nil || authoringETag == baseETag {
		t.Fatalf("authoring requirement change did not change ETag: %s %v", authoringETag, err)
	}
}

func cloneOperationMap(input map[string]protocolcommon.OperationCapability) map[string]protocolcommon.OperationCapability {
	result := make(map[string]protocolcommon.OperationCapability, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func TestMutationIsolationAcrossResponsesAndContext(t *testing.T) {
	t.Parallel()
	descriptor := newTestDescriptor(t)
	request := validRequest()
	request.Payload.OptionalCapabilities = []protocolcommon.CapabilityID{"engine.future"}
	first, negotiated := negotiate(t, descriptor, request)
	originalETag := negotiated.ManifestETag()
	originalDefaults := negotiated.DefaultCompileLimits()

	first.Payload.CapabilityManifest.Transports[0] = "mutated"
	delete(first.Payload.CapabilityManifest.Operations, OperationCompile)
	// Map values are copied on lookup, so mutate a retrieved compile limit and put it back.
	handshake := first.Payload.CapabilityManifest.Operations[OperationHandshake]
	required := []string{"schema:write"}
	handshake.RequiredAuthoringCapabilities = &required
	first.Payload.CapabilityManifest.Operations[OperationHandshake] = handshake
	first.Payload.CapabilityStatuses[0].CapabilityID = "engine.changed"
	if first.Payload.CapabilityStatuses[1].UnavailableReason != nil {
		*first.Payload.CapabilityStatuses[1].UnavailableReason = protocolcommon.UnavailableReasonNotAuthorized
	}
	returnedOperations := negotiated.Operations()
	returnedOperations[0] = "engine.changed"

	second, secondContext := negotiate(t, descriptor, request)
	if second.Payload.CapabilityManifest.Transports[0] != TransportInProcess || len(second.Payload.CapabilityManifest.Operations) != 2 || second.Payload.CapabilityStatuses[0].CapabilityID != OperationCompile {
		t.Fatalf("caller mutation contaminated later response: %+v", second.Payload)
	}
	if second.Payload.CapabilityStatuses[1].UnavailableReason == nil || *second.Payload.CapabilityStatuses[1].UnavailableReason != protocolcommon.UnavailableReasonUnsupported {
		t.Fatalf("caller mutation contaminated unavailable reason: %+v", second.Payload.CapabilityStatuses)
	}
	if negotiated.ManifestETag() != originalETag || secondContext.ManifestETag() != originalETag || negotiated.Operations()[0] != OperationCompile || negotiated.DefaultCompileLimits() != originalDefaults {
		t.Fatal("caller mutation contaminated immutable context")
	}
}

func TestCancellationAndCommonTerminalEnvelopes(t *testing.T) {
	t.Parallel()
	descriptor := newTestDescriptor(t)
	request := validRequest()

	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()
	response, negotiated, err := descriptor.Negotiate(cancelledContext, request)
	if err != nil {
		t.Fatal(err)
	}
	assertCancellation(t, response, request.RequestID, descriptor.EngineRelease())
	if negotiated != nil {
		t.Fatal("cancelled handshake returned a context")
	}

	staged := &stagedCancellationContext{cancelAt: 4}
	response, negotiated, err = descriptor.Negotiate(staged, request)
	if err != nil {
		t.Fatal(err)
	}
	assertCancellation(t, response, request.RequestID, descriptor.EngineRelease())
	if negotiated != nil {
		t.Fatal("mid-negotiation cancellation returned a context")
	}

	failed, err := descriptor.failedResponse("failed-request")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Outcome != protocolcommon.OutcomeFailed || failed.Failure == nil || failed.Failure.Code != FailureHandshakeInvariant || failed.Failure.Category != protocolcommon.ProtocolFailureCategoryInvariant || failed.Failure.Retryable || failed.Payload != nil || len(failed.Diagnostics) != 0 {
		t.Fatalf("invalid failed envelope: %+v", failed)
	}
}

func assertCancellation(t *testing.T, response engineprotocol.HandshakeResponseEnvelope, requestID string, release protocolcommon.ReleaseVersion) {
	t.Helper()
	if response.Outcome != protocolcommon.OutcomeCancelled || response.Failure == nil || response.Failure.Code != FailureHandshakeCancelled || response.Failure.Category != protocolcommon.ProtocolFailureCategoryCancelled || !response.Failure.Retryable || response.Payload != nil || len(response.Diagnostics) != 0 {
		t.Fatalf("invalid cancelled envelope: %+v", response)
	}
	if response.RequestID != requestID || response.EngineRelease != release || response.Protocol != bootstrapProtocolRef() {
		t.Fatalf("cancelled metadata mismatch: %+v", response)
	}
}

type stagedCancellationContext struct {
	calls    atomic.Int32
	cancelAt int32
}

func (staged *stagedCancellationContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (staged *stagedCancellationContext) Done() <-chan struct{}       { return nil }
func (staged *stagedCancellationContext) Value(any) any               { return nil }
func (staged *stagedCancellationContext) Err() error {
	if staged.calls.Add(1) >= staged.cancelAt {
		return context.Canceled
	}
	return nil
}

func TestNegotiateCallerMisuseCannotFabricateMetadata(t *testing.T) {
	t.Parallel()
	descriptor := newTestDescriptor(t)
	request := validRequest()
	request.RequestID = ""
	if _, _, err := descriptor.Negotiate(context.Background(), request); err == nil {
		t.Fatal("empty request ID did not fail outside the wire envelope")
	}
	request.RequestID = string([]byte{0xff})
	if _, _, err := descriptor.Negotiate(context.Background(), request); err == nil {
		t.Fatal("invalid UTF-8 request ID did not fail outside the wire envelope")
	}
	request.RequestID = "valid"
	if _, _, err := descriptor.Negotiate(nil, request); err == nil {
		t.Fatal("nil context was accepted")
	}
	var nilDescriptor *Descriptor
	if _, _, err := nilDescriptor.Negotiate(context.Background(), request); err == nil {
		t.Fatal("nil descriptor was accepted")
	}
}

func TestNegotiationIsDeterministicAcrossInputOrder(t *testing.T) {
	t.Parallel()
	descriptor := newTestDescriptor(t)
	firstRequest := validRequest()
	firstRequest.Payload.Protocols = append([]protocolcommon.ProtocolOffer{{
		Name:           "runtime",
		SupportedRange: "1.0..1.0",
		Versions:       []protocolcommon.ProtocolVersionBinding{{Version: "1.0", SchemaDigest: testWrongDigest}},
	}}, firstRequest.Payload.Protocols...)
	firstRequest.Payload.RequiredCapabilities = []protocolcommon.CapabilityID{OperationHandshake, OperationCompile}
	firstRequest.Payload.OptionalCapabilities = []protocolcommon.CapabilityID{"engine.zeta", "engine.alpha"}
	secondRequest := firstRequest
	secondRequest.Payload.Protocols = slices.Clone(firstRequest.Payload.Protocols)
	slices.Reverse(secondRequest.Payload.Protocols)
	secondRequest.Payload.RequiredCapabilities = slices.Clone(firstRequest.Payload.RequiredCapabilities)
	slices.Reverse(secondRequest.Payload.RequiredCapabilities)
	secondRequest.Payload.OptionalCapabilities = slices.Clone(firstRequest.Payload.OptionalCapabilities)
	slices.Reverse(secondRequest.Payload.OptionalCapabilities)

	first, _ := negotiate(t, descriptor, firstRequest)
	second, _ := negotiate(t, descriptor, secondRequest)
	firstBytes, err := engineprotocol.EncodeHandshakeResponseEnvelope(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := engineprotocol.EncodeHandshakeResponseEnvelope(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatalf("set-like input order changed response\nfirst=%s\nsecond=%s", firstBytes, secondBytes)
	}
}

func TestConcurrentNegotiationIsRaceFreeAndIsolated(t *testing.T) {
	descriptor := newTestDescriptor(t)
	const workers = 64
	var wait sync.WaitGroup
	errors := make(chan error, workers)
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			request := validRequest()
			request.RequestID = fmt.Sprintf("concurrent-%02d", index)
			if index%2 == 0 {
				request.Payload.OptionalCapabilities = []protocolcommon.CapabilityID{"engine.future"}
			}
			if index%3 == 0 {
				request.Payload.ClientLimits = &protocolcommon.CompileResourceLimitConstraints{MaxAssets: positive(int64(index + 1))}
			}
			response, negotiated, err := descriptor.Negotiate(context.Background(), request)
			if err != nil {
				errors <- err
				return
			}
			if response.Outcome != protocolcommon.OutcomeSuccess || negotiated == nil || response.RequestID != request.RequestID {
				errors <- fmt.Errorf("unexpected result for %s", request.RequestID)
				return
			}
			response.Payload.CapabilityManifest.Transports[0] = "mutated"
		}(index)
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	final, _ := negotiate(t, descriptor, validRequest())
	if final.Payload.CapabilityManifest.Transports[0] != TransportInProcess {
		t.Fatal("concurrent response mutation contaminated descriptor")
	}
}

func TestSelectsHighestExactCompatibleVersionAfterDigest(t *testing.T) {
	t.Parallel()
	catalog := []protocolBinding{
		{version: protocolNumber{major: 1, minor: 0}, wireVersion: "1.0", schemaDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		{version: protocolNumber{major: 1, minor: 2}, wireVersion: "1.2", schemaDigest: "sha256:2222222222222222222222222222222222222222222222222222222222222222"},
		{version: protocolNumber{major: 1, minor: 1}, wireVersion: "1.1", schemaDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111"},
	}
	offer := protocolcommon.ProtocolOffer{
		Name:           ProtocolName,
		SupportedRange: "1.0..1.2",
		Versions: []protocolcommon.ProtocolVersionBinding{
			{Version: "1.0", SchemaDigest: catalog[0].schemaDigest},
			{Version: "1.1", SchemaDigest: catalog[2].schemaDigest},
			{Version: "1.2", SchemaDigest: catalog[1].schemaDigest},
		},
	}
	selected, failure := selectProtocol(catalog, offer)
	if failure != selectionCompatible || selected.wireVersion != "1.2" {
		t.Fatalf("did not select highest compatible exact version: %+v %v", selected, failure)
	}
	offer.Versions[2].SchemaDigest = testWrongDigest
	selected, failure = selectProtocol(catalog, offer)
	if failure != selectionCompatible || selected.wireVersion != "1.1" {
		t.Fatalf("digest constraint did not fall back to highest compatible exact version: %+v %v", selected, failure)
	}
	slices.Reverse(catalog)
	slices.Reverse(offer.Versions)
	permuted, permutedFailure := selectProtocol(catalog, offer)
	if permutedFailure != selectionCompatible || permuted != selected {
		t.Fatalf("selection depends on insertion order: %+v/%v versus %+v/%v", selected, failure, permuted, permutedFailure)
	}
}

func TestProtocolRangeGrammar(t *testing.T) {
	t.Parallel()
	valid := []string{"0.0..0.0", "1.0..1.9", "4294967295.4294967295..4294967295.4294967295"}
	for _, input := range valid {
		if _, ok := parseProtocolRange(input); !ok {
			t.Errorf("valid range rejected: %q", input)
		}
	}
	invalid := []string{
		"", "1.0", "1.0...1.1", "1.0..", "..1.0", "1.0..2.0", "1.2..1.1",
		"01.0..01.1", "1.00..1.01", "1.0.0..1.1.0", "v1.0..1.1", "1.*..1.1",
		"1.0 || 1.1", " 1.0..1.1", "1.0..1.1 ", "4294967296.0..4294967296.1",
	}
	for _, input := range invalid {
		if _, ok := parseProtocolRange(input); ok {
			t.Errorf("invalid range accepted: %q", input)
		}
	}
	comparisons := []struct {
		left, right protocolNumber
		want        int
	}{
		{protocolNumber{1, 0}, protocolNumber{1, 0}, 0},
		{protocolNumber{0, 9}, protocolNumber{1, 0}, -1},
		{protocolNumber{2, 0}, protocolNumber{1, 9}, 1},
		{protocolNumber{1, 1}, protocolNumber{1, 2}, -1},
		{protocolNumber{1, 3}, protocolNumber{1, 2}, 1},
	}
	for _, test := range comparisons {
		if got := test.left.compare(test.right); got != test.want {
			t.Errorf("compare(%+v,%+v)=%d want %d", test.left, test.right, got, test.want)
		}
	}
}

func TestGeneratedCanonicalResponseIsRepeatable(t *testing.T) {
	t.Parallel()
	descriptor := newTestDescriptor(t)
	request := validRequest()
	first, _ := negotiate(t, descriptor, request)
	second, _ := negotiate(t, descriptor, request)
	firstBytes, err := engineprotocol.EncodeHandshakeResponseEnvelope(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := engineprotocol.EncodeHandshakeResponseEnvelope(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatalf("repeated handshakes differ\nfirst=%s\nsecond=%s", firstBytes, secondBytes)
	}
	var object map[string]any
	if err := json.Unmarshal(firstBytes, &object); err != nil || object["outcome"] != "success" {
		t.Fatalf("canonical response is not valid JSON: %v %v", object, err)
	}
}

func TestSharedCanonicalHandshakeFixturesMatchPolicy(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	requestBytes, err := os.ReadFile(filepath.Join(root, "schemas", "fixtures", "conformance", "engine", "handshake-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := engineprotocol.DecodeHandshakeRequestEnvelope(requestBytes)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRequest, err := engineprotocol.EncodeHandshakeRequestEnvelope(request)
	if err != nil || !bytes.Equal(canonicalRequest, bytes.TrimSpace(requestBytes)) {
		t.Fatalf("request fixture is not canonical: %v\nwant=%s\ngot=%s", err, requestBytes, canonicalRequest)
	}

	config := validConfig()
	config.EndpointInstanceID = "fixture-engine"
	descriptor, err := NewDescriptor(config)
	if err != nil {
		t.Fatal(err)
	}
	response, context := negotiate(t, descriptor, request)
	if context == nil {
		t.Fatal("fixture request did not negotiate")
	}
	responseBytes, err := engineprotocol.EncodeHandshakeResponseEnvelope(response)
	if err != nil {
		t.Fatal(err)
	}
	wantSuccess, err := os.ReadFile(filepath.Join(root, "schemas", "fixtures", "conformance", "engine", "handshake-success.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(responseBytes, bytes.TrimSpace(wantSuccess)) {
		t.Fatalf("success fixture differs from Go policy\nwant=%s\ngot=%s", wantSuccess, responseBytes)
	}

	rejectedRequest := request
	rejectedRequest.RequestID = "fixture-handshake-rejected"
	rejectedRequest.Payload.Protocols = []protocolcommon.ProtocolOffer{{
		Name:           ProtocolName,
		SupportedRange: "2.0..2.1",
		Versions:       []protocolcommon.ProtocolVersionBinding{{Version: "2.1", SchemaDigest: testWrongDigest}},
	}}
	rejected, rejectedContext := negotiate(t, descriptor, rejectedRequest)
	if rejectedContext != nil {
		t.Fatal("rejection fixture unexpectedly negotiated")
	}
	rejectedBytes, err := engineprotocol.EncodeHandshakeResponseEnvelope(rejected)
	if err != nil {
		t.Fatal(err)
	}
	wantRejected, err := os.ReadFile(filepath.Join(root, "schemas", "fixtures", "conformance", "engine", "handshake-rejected.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rejectedBytes, bytes.TrimSpace(wantRejected)) {
		t.Fatalf("rejection fixture differs from Go policy\nwant=%s\ngot=%s", wantRejected, rejectedBytes)
	}
}

func TestNilGetterValuesAreSafe(t *testing.T) {
	t.Parallel()
	var descriptor *Descriptor
	if descriptor.EngineRelease() != "" || descriptor.SourceRevision() != "" || descriptor.ReleaseManifestDigest() != "" || descriptor.EndpointInstanceID() != "" || descriptor.Transports() != nil || descriptor.Operations() != nil || descriptor.Limits() != (LimitPolicy{}) {
		t.Fatal("nil descriptor getters returned non-zero data")
	}
	var negotiated *NegotiatedContext
	if negotiated.EndpointInstanceID() != "" || negotiated.ManifestETag() != "" || negotiated.EngineRelease() != "" || negotiated.ReleaseManifestDigest() != "" || negotiated.ProtocolName() != "" || negotiated.ProtocolVersion() != "" || negotiated.ProtocolSchemaDigest() != "" || negotiated.Operations() != nil || negotiated.SupportsOperation(OperationCompile) || negotiated.DefaultCompileLimits() != (engine.ResourceLimits{}) || negotiated.EffectiveMaximumCompileLimits() != (engine.ResourceLimits{}) {
		t.Fatal("nil negotiated context getters returned non-zero data")
	}
}

func TestAllClientLimitKeysAreNegotiated(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		set  func(*protocolcommon.CompileResourceLimitConstraints, *protocolcommon.CanonicalPositiveInt64)
		get  func(engine.ResourceLimits) int64
	}{
		{"max_project_source_files", func(value *protocolcommon.CompileResourceLimitConstraints, limit *protocolcommon.CanonicalPositiveInt64) {
			value.MaxProjectSourceFiles = limit
		}, func(value engine.ResourceLimits) int64 { return value.MaxProjectSourceFiles }},
		{"max_project_source_bytes", func(value *protocolcommon.CompileResourceLimitConstraints, limit *protocolcommon.CanonicalPositiveInt64) {
			value.MaxProjectSourceBytes = limit
		}, func(value engine.ResourceLimits) int64 { return value.MaxProjectSourceBytes }},
		{"max_pack_files", func(value *protocolcommon.CompileResourceLimitConstraints, limit *protocolcommon.CanonicalPositiveInt64) {
			value.MaxPackFiles = limit
		}, func(value engine.ResourceLimits) int64 { return value.MaxPackFiles }},
		{"max_pack_bytes", func(value *protocolcommon.CompileResourceLimitConstraints, limit *protocolcommon.CanonicalPositiveInt64) {
			value.MaxPackBytes = limit
		}, func(value engine.ResourceLimits) int64 { return value.MaxPackBytes }},
		{"max_assets", func(value *protocolcommon.CompileResourceLimitConstraints, limit *protocolcommon.CanonicalPositiveInt64) {
			value.MaxAssets = limit
		}, func(value engine.ResourceLimits) int64 { return value.MaxAssets }},
		{"max_asset_bytes", func(value *protocolcommon.CompileResourceLimitConstraints, limit *protocolcommon.CanonicalPositiveInt64) {
			value.MaxAssetBytes = limit
		}, func(value engine.ResourceLimits) int64 { return value.MaxAssetBytes }},
		{"max_raster_dimension", func(value *protocolcommon.CompileResourceLimitConstraints, limit *protocolcommon.CanonicalPositiveInt64) {
			value.MaxRasterDimension = limit
		}, func(value engine.ResourceLimits) int64 { return value.MaxRasterDimension }},
		{"max_raster_pixels", func(value *protocolcommon.CompileResourceLimitConstraints, limit *protocolcommon.CanonicalPositiveInt64) {
			value.MaxRasterPixels = limit
		}, func(value engine.ResourceLimits) int64 { return value.MaxRasterPixels }},
		{"max_declarations", func(value *protocolcommon.CompileResourceLimitConstraints, limit *protocolcommon.CanonicalPositiveInt64) {
			value.MaxDeclarations = limit
		}, func(value engine.ResourceLimits) int64 { return value.MaxDeclarations }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			constraints := &protocolcommon.CompileResourceLimitConstraints{}
			test.set(constraints, positive(1))
			defaults, maximums, err := effectiveLimits(DefaultLimitPolicy(), constraints)
			if err != nil {
				t.Fatal(err)
			}
			if test.get(defaults) != 1 || test.get(maximums) != 1 {
				t.Fatalf("limit was not applied: default=%+v maximum=%+v", defaults, maximums)
			}
		})
	}
	invalid := protocolcommon.CanonicalPositiveInt64("invalid")
	if _, err := constrain(5, &invalid); err == nil {
		t.Fatal("invalid positive ceiling was accepted")
	}
	for _, test := range tests {
		constraints := &protocolcommon.CompileResourceLimitConstraints{}
		test.set(constraints, &invalid)
		if _, _, err := effectiveLimits(DefaultLimitPolicy(), constraints); err == nil || !strings.Contains(err.Error(), test.name) {
			t.Fatalf("invalid %s ceiling was not safely identified: %v", test.name, err)
		}
	}
}

func TestInternalHelpersFailClosedAndCloneDeeply(t *testing.T) {
	t.Parallel()
	descriptor := newTestDescriptor(t)
	response, negotiatedContext, err := descriptor.invariantFailure("invariant", fmt.Errorf("private cause /tmp/secret"))
	if err != nil || negotiatedContext != nil || response.Outcome != protocolcommon.OutcomeFailed || response.Failure == nil || strings.Contains(response.Failure.Message, "private") {
		t.Fatalf("invariant was not safely mapped: response=%+v context=%+v err=%v", response, negotiatedContext, err)
	}

	broken := *descriptor
	broken.limits = LimitPolicy{}
	request := validRequest()
	response, negotiatedContext, err = broken.Negotiate(context.Background(), request)
	if err != nil || negotiatedContext != nil || response.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("manifest invariant did not fail closed: response=%+v context=%+v err=%v", response, negotiatedContext, err)
	}

	invalidRelease := *descriptor
	invalidRelease.engineRelease = "invalid"
	if _, _, err := invalidRelease.invariantFailure("request", fmt.Errorf("cause")); err == nil {
		t.Fatal("an impossible invalid failed envelope did not return an error")
	}
	if _, _, err := invalidRelease.reject("request", invalidHandshakeDiagnostic(DiagnosticReasonInvalidEnvelope)); err == nil {
		t.Fatal("an impossible invalid rejection envelope did not return an error")
	}
	if _, _, err := invalidRelease.cancelled("request"); err == nil {
		t.Fatal("an impossible invalid cancellation envelope did not return an error")
	}

	stableAddress := "project.example/entity.one"
	remediation := "remediate"
	source := &protocolcommon.ProtocolDiagnosticSource{
		ModulePath:    "main.ldl",
		Span:          protocolcommon.ProtocolDiagnosticSpan{StartByte: "0", EndByte: "1"},
		StableAddress: &stableAddress,
	}
	nested := protocolcommon.JsonObject{
		"array": {Kind: protocolcommon.JsonValueKindArray, Array: []protocolcommon.JsonValue{{Kind: protocolcommon.JsonValueKindObject, Object: map[string]protocolcommon.JsonValue{"value": stringJSON("original")}}}},
	}
	diagnostics := []protocolcommon.ProtocolDiagnostic{{
		Code: DiagnosticInvalidHandshake, Message: "message", Severity: protocolcommon.ProtocolDiagnosticSeverityError,
		Data: &nested, Remediation: &remediation, Source: source,
		Related: []protocolcommon.ProtocolDiagnosticRelated{{Message: "related", Relation: "because", Source: source}},
	}}
	cloned := cloneDiagnostics(diagnostics)
	remediation = "changed"
	stableAddress = "changed"
	nestedValue := nested["array"]
	nestedValue.Array[0].Object["value"] = stringJSON("changed")
	nested["array"] = nestedValue
	if *cloned[0].Remediation != "remediate" || *cloned[0].Source.StableAddress != "project.example/entity.one" || *cloned[0].Related[0].Source.StableAddress != "project.example/entity.one" || cloned[0].Data == nil || (*cloned[0].Data)["array"].Array[0].Object["value"].String != "original" {
		t.Fatalf("diagnostic clone retained mutable aliases: %+v", cloned)
	}
	if cloneDiagnostics(nil) != nil || cloneDiagnosticSource(nil) != nil || cloneJSONMap(nil) != nil {
		t.Fatal("nil clone helpers did not preserve absence")
	}

	if _, err := selectionDiagnostic(selectionFailure(99), validRequest().Payload.Protocols[0], descriptor.protocols); err == nil {
		t.Fatal("unknown selection failure did not fail closed")
	}
	if _, err := upgradeData("not a range"); err == nil {
		t.Fatal("invalid upgrade diagnostic data was accepted")
	}
	if _, err := manifestETag(protocolcommon.CapabilityManifest{}); err == nil {
		t.Fatal("invalid manifest projection was accepted")
	}
	if _, _, detail, valid := normalizeCapabilityRequests(nil, []protocolcommon.CapabilityID{}); valid || detail != DiagnosticReasonInvalidCapabilitySets {
		t.Fatalf("nil capability set was not rejected: valid=%v detail=%s", valid, detail)
	}
}
