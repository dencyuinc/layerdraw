// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopcontract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/internal/registry"
)

type reviewSubmitRequest struct {
	ProposalID string `json:"proposal_id"`
	Decision   string `json:"decision"`
}
type reviewSubmitResult struct {
	ReviewID string `json:"review_id"`
	Revision string `json:"revision"`
}
type hostExportRequest struct {
	DocumentID string `json:"document_id"`
	Format     string `json:"format"`
}
type hostExportResult struct {
	ArtifactID string `json:"artifact_id"`
	MediaType  string `json:"media_type"`
}
type accessAuthorizeReadRequest struct {
	DocumentID string `json:"document_id"`
	Surface    string `json:"surface"`
}
type accessAuthorizeReadResult struct {
	Allowed      bool   `json:"allowed"`
	PolicyDigest string `json:"policy_digest"`
}
type accessManageAgentScopeRequest struct {
	AgentID string `json:"agent_id"`
	Scope   string `json:"scope"`
}
type accessManageAgentScopeResult struct {
	DelegationID string `json:"delegation_id"`
	Generation   string `json:"generation"`
}
type nativeExecuteQueryRequest struct {
	DocumentID string `json:"document_id"`
	Query      string `json:"query"`
}
type nativeExecuteQueryResult struct {
	Rows  []string `json:"rows"`
	Count string   `json:"count"`
}
type nativeExecuteSearchRequest struct {
	DocumentID string `json:"document_id"`
	Terms      string `json:"terms"`
}
type nativeExecuteSearchResult struct {
	Addresses []string `json:"addresses"`
	Count     string   `json:"count"`
}
type nativeExecuteAnalysisRequest struct {
	DocumentID string `json:"document_id"`
	Analysis   string `json:"analysis"`
}
type nativeExecuteAnalysisResult struct {
	Findings []string `json:"findings"`
	Digest   string   `json:"digest"`
}
type searchIndexInspectRequest struct {
	IndexID string `json:"index_id"`
}
type searchIndexInspectResult struct {
	Revision      string `json:"revision"`
	DocumentCount string `json:"document_count"`
}
type searchIndexUpdateRequest struct {
	IndexID    string `json:"index_id"`
	DocumentID string `json:"document_id"`
	Content    string `json:"content"`
}
type searchIndexUpdateResult struct {
	Revision          string `json:"revision"`
	IndexedDocumentID string `json:"indexed_document_id"`
}
type searchIndexSearchRequest struct {
	IndexID string `json:"index_id"`
	Query   string `json:"query"`
}
type searchIndexSearchResult struct {
	DocumentIDs []string `json:"document_ids"`
	Count       string   `json:"count"`
}
type embeddingDocumentRequest struct {
	DocumentID string `json:"document_id"`
	Content    string `json:"content"`
	Profile    string `json:"profile"`
}
type embeddingDocumentResult struct {
	EmbeddingID string `json:"embedding_id"`
	Digest      string `json:"digest"`
}
type embeddingQueryRequest struct {
	Query   string `json:"query"`
	Profile string `json:"profile"`
}
type embeddingQueryResult struct {
	VectorDigest string `json:"vector_digest"`
	Dimensions   string `json:"dimensions"`
}
type mcpInvokeToolRequest struct {
	ConnectionID string `json:"connection_id"`
	ToolName     string `json:"tool_name"`
	Arguments    string `json:"arguments"`
}
type mcpInvokeToolResult struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
}
type mcpReadResourceRequest struct {
	ConnectionID string `json:"connection_id"`
	URI          string `json:"uri"`
}
type mcpReadResourceResult struct {
	URI       string `json:"uri"`
	MediaType string `json:"media_type"`
	Content   string `json:"content"`
}
type mcpConnectRequest struct {
	ServerID string `json:"server_id"`
	Endpoint string `json:"endpoint"`
}
type mcpConnectResult struct {
	ConnectionID    string `json:"connection_id"`
	ProtocolVersion string `json:"protocol_version"`
}
type mcpDisconnectRequest struct {
	ConnectionID string `json:"connection_id"`
	Reason       string `json:"reason"`
}
type mcpDisconnectResult struct {
	ConnectionID string `json:"connection_id"`
	Disconnected bool   `json:"disconnected"`
}

func ownerPayloadFixture(operation string, response bool) any {
	requests := map[string]any{
		"review.submit":             reviewSubmitRequest{"proposal-1", "approve"},
		"host.export":               hostExportRequest{"document-1", "svg"},
		"access.authorize_read":     accessAuthorizeReadRequest{"document-1", "review"},
		"access.manage_agent_scope": accessManageAgentScopeRequest{"agent-1", "document-1"},
		"native.execute_query":      nativeExecuteQueryRequest{"document-1", "query OpenItems"},
		"native.execute_search":     nativeExecuteSearchRequest{"document-1", "open items"},
		"native.execute_analysis":   nativeExecuteAnalysisRequest{"document-1", "dependency-impact"},
		"search_index.inspect":      searchIndexInspectRequest{"index-1"},
		"search_index.update":       searchIndexUpdateRequest{"index-1", "document-1", "Open items"},
		"search_index.search":       searchIndexSearchRequest{"index-1", "open"},
		"embedding.document":        embeddingDocumentRequest{"document-1", "Open items", "default"},
		"embedding.query":           embeddingQueryRequest{"open items", "default"},
		"mcp.invoke_tool":           mcpInvokeToolRequest{"connection-1", "layerdraw.search", `{"query":"open"}`},
		"mcp.read_resource":         mcpReadResourceRequest{"connection-1", "layerdraw://document/document-1"},
		"mcp.connect":               mcpConnectRequest{"server-1", "stdio://layerdraw-mcp"},
		"mcp.disconnect":            mcpDisconnectRequest{"connection-1", "completed"},
	}
	results := map[string]any{
		"review.submit":             reviewSubmitResult{"review-1", "7"},
		"host.export":               hostExportResult{"artifact-1", "image/svg+xml"},
		"access.authorize_read":     accessAuthorizeReadResult{true, "sha256:1111111111111111111111111111111111111111111111111111111111111111"},
		"access.manage_agent_scope": accessManageAgentScopeResult{"delegation-1", "1"},
		"native.execute_query":      nativeExecuteQueryResult{[]string{"row-1"}, "1"},
		"native.execute_search":     nativeExecuteSearchResult{[]string{"ldl:project:fixture:entity:item"}, "1"},
		"native.execute_analysis":   nativeExecuteAnalysisResult{[]string{"no-breaking-change"}, "sha256:2222222222222222222222222222222222222222222222222222222222222222"},
		"search_index.inspect":      searchIndexInspectResult{"3", "1"},
		"search_index.update":       searchIndexUpdateResult{"4", "document-1"},
		"search_index.search":       searchIndexSearchResult{[]string{"document-1"}, "1"},
		"embedding.document":        embeddingDocumentResult{"embedding-1", "sha256:3333333333333333333333333333333333333333333333333333333333333333"},
		"embedding.query":           embeddingQueryResult{"sha256:4444444444444444444444444444444444444444444444444444444444444444", "384"},
		"mcp.invoke_tool":           mcpInvokeToolResult{"call-1", `{"items":["document-1"]}`},
		"mcp.read_resource":         mcpReadResourceResult{"layerdraw://document/document-1", "application/json", `{"document_id":"document-1"}`},
		"mcp.connect":               mcpConnectResult{"connection-1", "2025-06-18"},
		"mcp.disconnect":            mcpDisconnectResult{"connection-1", true},
	}
	if response {
		return results[operation]
	}
	return requests[operation]
}

func decodeOwnerOperationPayload(operation string, wire []byte, response bool) error {
	expected := ownerPayloadFixture(operation, response)
	if expected == nil {
		return errors.New("unknown owner operation codec")
	}
	destination := reflect.New(reflect.TypeOf(expected)).Interface()
	if err := decodeStrictParity(wire, destination); err != nil {
		return err
	}
	if !reflect.DeepEqual(reflect.ValueOf(destination).Elem().Interface(), expected) {
		return errors.New("owner operation payload is incomplete")
	}
	return nil
}

func registryInputFixture(operation registry.WireOperation) any {
	source := registry.RegistrySource{SourceID: "source-1", Kind: registry.SourceOfficial, EndpointRef: "https://registry.example", TrustPolicyID: "policy-1", CachePolicy: "immutable", Priority: 1, Revision: 1}
	switch operation {
	case registry.WireListSources:
		return struct{}{}
	case registry.WireConfigureSource:
		return registry.ConfigureSourceInput{Source: source}
	case registry.WireConnectSource:
		return registry.RegistryConnectionInput{SourceID: "source-1", ConnectionRef: "connection-1"}
	case registry.WireDisconnectSource:
		return registry.SourceIDInput{SourceID: "source-1"}
	case registry.WireSearch:
		return registry.SearchInput{Query: "open items"}
	case registry.WirePlan:
		return registry.PlanRequest{Action: registry.ActionInstall, ProjectID: "project-1", BaseRevision: "revision-1", ExpectedDefinitionHash: "sha256:1", ExpectedResolvedLockDigest: "sha256:2", Requested: registry.ArtifactIdentity{Kind: registry.ArtifactPack, CanonicalID: "example/items", Version: "1.0.0"}, DependencySnapshot: registry.ProjectDependencySnapshot{ResolvedLockDigest: "sha256:2", Installs: []registry.LockedArtifact{}}}
	case registry.WireCommit:
		return registry.WireCommitInput{TransactionID: "transaction-1", PlanDigest: "sha256:3", OperationID: "operation-1", IdempotencyKey: "idempotency-1"}
	case registry.WireGetTransaction, registry.WireRecoverTransaction:
		return registry.TransactionIDInput{TransactionID: "transaction-1"}
	case registry.WireAuthorArtifact:
		return registry.AuthorArtifactRequest{Kind: registry.ArtifactPack, ProjectID: "project-1", OutputName: "items", PublisherID: "example", Version: "1.0.0"}
	}
	return nil
}

func registryResultFixture(operation registry.WireOperation) any {
	source := registry.RegistrySource{SourceID: "source-1", Kind: registry.SourceOfficial, EndpointRef: "https://registry.example", TrustPolicyID: "policy-1", CachePolicy: "immutable", Priority: 1, Connected: true, Revision: 1}
	release := registry.ArtifactRelease{Identity: registry.ArtifactIdentity{Kind: registry.ArtifactPack, CanonicalID: "example/items", Version: "1.0.0"}, SourceID: "source-1", PublisherID: "example", Digest: "sha256:1", ManifestDigest: "sha256:2", DependencyMetadataDigest: "sha256:3", Size: 1, Dependencies: []registry.Dependency{}, Compatibility: []registry.CompatibilityDecision{}, License: "MIT", ProvenanceDigest: "sha256:4"}
	plan := registry.InstallPlan{TransactionID: "transaction-1", PlanDigest: "sha256:5", Action: registry.ActionInstall, ProjectID: "project-1", BaseRevision: "revision-1", ExpectedDefinitionHash: "sha256:1", ExpectedResolvedLockDigest: "sha256:2", Artifacts: []registry.PlanArtifact{}, RequiredCapabilities: []string{}, TrustPolicyDigests: []string{}, SourceBindings: []registry.SourcePlanBinding{}, DependencySnapshot: registry.ProjectDependencySnapshot{ResolvedLockDigest: "sha256:2", Installs: []registry.LockedArtifact{}}, ResolvedLockDelta: registry.ResolvedLockDelta{Added: []registry.LockedArtifact{}, Updated: []registry.LockedArtifact{}, Removed: []registry.LockedArtifact{}, Pinned: []registry.LockedArtifact{}}, ExpiresAt: mustFixtureTime(), AuthoringImpactDigests: []string{}}
	switch operation {
	case registry.WireListSources:
		return []registry.RegistrySource{source}
	case registry.WireConfigureSource, registry.WireConnectSource, registry.WireDisconnectSource:
		return source
	case registry.WireSearch:
		return []registry.ArtifactRelease{release}
	case registry.WirePlan:
		return plan
	case registry.WireCommit, registry.WireGetTransaction, registry.WireRecoverTransaction:
		return registry.Transaction{Plan: plan, Events: []registry.TransactionEvent{}}
	case registry.WireAuthorArtifact:
		return release
	}
	return nil
}

func mustFixtureTime() time.Time {
	return time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
}

func operationSchemaContext(t *testing.T, binding BindingMethod) (*schemaAuthority, string, [2]string) {
	t.Helper()
	authority := loadSchemaAuthority(t)
	documentID := "https://schemas.layerdraw.dev/engine-protocol/v1"
	if binding.Target == TargetRuntime {
		documentID = "https://schemas.layerdraw.dev/runtime-protocol/v1"
	}
	names := authority.operationDefinitions(t, documentID)[binding.Operation]
	return authority, documentID, names
}

func operationRequestFixture(t *testing.T, binding BindingMethod, requestID string) []byte {
	t.Helper()
	if binding.Target == TargetEngine || binding.Target == TargetRuntime {
		authority, documentID, names := operationSchemaContext(t, binding)
		return authority.fixture(t, documentID, names[0], requestID)
	}
	if binding.Target == TargetRegistry {
		wire, err := json.Marshal(registry.WireRequest{WireVersion: registry.RegistryWireVersion, Operation: registry.WireOperation(binding.Operation), RequestID: requestID, Input: rawFixture(t, registryInputFixture(registry.WireOperation(binding.Operation)))})
		if err != nil {
			t.Fatal(err)
		}
		return wire
	}
	wire, err := json.Marshal(struct {
		Operation string `json:"operation"`
		RequestID string `json:"request_id"`
		Payload   any    `json:"payload"`
	}{binding.Operation, requestID, ownerPayloadFixture(binding.Operation, false)})
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func operationResponseFixture(t *testing.T, binding BindingMethod, requestID string) []byte {
	t.Helper()
	if binding.Target == TargetEngine || binding.Target == TargetRuntime {
		authority, documentID, names := operationSchemaContext(t, binding)
		return authority.fixture(t, documentID, names[1], requestID)
	}
	if binding.Target == TargetRegistry {
		wire, err := json.Marshal(registry.WireResponse{WireVersion: registry.RegistryWireVersion, Operation: registry.WireOperation(binding.Operation), RequestID: requestID, OK: true, Value: rawFixture(t, registryResultFixture(registry.WireOperation(binding.Operation)))})
		if err != nil {
			t.Fatal(err)
		}
		return wire
	}
	wire, err := json.Marshal(struct {
		Operation string `json:"operation"`
		RequestID string `json:"request_id"`
		Payload   any    `json:"payload"`
	}{binding.Operation, requestID, ownerPayloadFixture(binding.Operation, true)})
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func rawFixture(t *testing.T, value any) json.RawMessage {
	t.Helper()
	wire, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

type normalizedOperationFixture struct {
	Operation       string
	Request, Result string
}

type browserWorkerAdapterFixture struct {
	Adapter  string          `json:"adapter"`
	Route    string          `json:"route"`
	Request  json.RawMessage `json:"request"`
	Response json.RawMessage `json:"response"`
}

type desktopWailsAdapterFixture struct {
	Adapter        string         `json:"adapter"`
	Method         string         `json:"method"`
	Exchange       Exchange       `json:"exchange"`
	ExchangeResult ExchangeResult `json:"exchange_result"`
}

func validateTypedControls(binding BindingMethod, request, response []byte, decoder OwnerDecoder) error {
	if binding.Target == TargetEngine || binding.Target == TargetRuntime || binding.Target == TargetRegistry {
		if err := decodeExact(binding, request); err != nil {
			return err
		}
		return decodeExactResponse(binding, response)
	}
	if _, err := decoder.DecodeRequest(binding.Operation, request); err != nil {
		return err
	}
	_, err := decoder.DecodeResponse(binding.Operation, response)
	return err
}

func normalizedControls(t *testing.T, operation string, request, response []byte) normalizedOperationFixture {
	t.Helper()
	return normalizedOperationFixture{operation, canonicalJSONWithoutRequestID(t, request), canonicalJSONWithoutRequestID(t, response)}
}

func canonicalJSONWithoutRequestID(t *testing.T, wire []byte) string {
	t.Helper()
	normalized := normalizedJSONWithoutRequestID(t, wire)
	canonical, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	return string(canonical)
}

func decodeBrowserWorkerAdapter(t *testing.T, wire []byte, binding BindingMethod, decoder OwnerDecoder) normalizedOperationFixture {
	t.Helper()
	var adapter browserWorkerAdapterFixture
	if err := decodeStrictParity(wire, &adapter); err != nil || adapter.Adapter != "browser-worker" || adapter.Route != binding.Operation {
		t.Fatalf("%s Browser adapter envelope: %v", binding.GeneratedMethod, err)
	}
	if err := validateTypedControls(binding, adapter.Request, adapter.Response, decoder); err != nil {
		t.Fatalf("%s Browser owner/generated codec: %v", binding.GeneratedMethod, err)
	}
	return normalizedControls(t, binding.Operation, adapter.Request, adapter.Response)
}

func decodeDesktopWailsAdapter(t *testing.T, wire []byte, binding BindingMethod, decoder OwnerDecoder) normalizedOperationFixture {
	t.Helper()
	var adapter desktopWailsAdapterFixture
	if err := decodeStrictParity(wire, &adapter); err != nil || adapter.Adapter != "desktop-wails" || adapter.Method != binding.GeneratedMethod || adapter.Exchange.Operation != binding.Operation || adapter.ExchangeResult.Operation != binding.Operation {
		t.Fatalf("%s Desktop adapter envelope: %v", binding.GeneratedMethod, err)
	}
	if err := validateTypedControls(binding, adapter.Exchange.Control, adapter.ExchangeResult.Control, decoder); err != nil {
		t.Fatalf("%s Desktop owner/generated codec: %v", binding.GeneratedMethod, err)
	}
	return normalizedControls(t, binding.Operation, adapter.Exchange.Control, adapter.ExchangeResult.Control)
}

func TestAllBindingsUseOperationSpecificSuccessFixturesThroughDistinctAdapters(t *testing.T) {
	ownerDecoder := strictOwnerDecoder{}
	clients := completeClientSet(t)
	for _, binding := range GeneratedBindingTable() {
		browserRequest := operationRequestFixture(t, binding, "browser-"+binding.GeneratedMethod)
		browserResponse := operationResponseFixture(t, binding, "browser-"+binding.GeneratedMethod)
		desktopRequest := operationRequestFixture(t, binding, "desktop-"+binding.GeneratedMethod)
		desktopResponse := operationResponseFixture(t, binding, "desktop-"+binding.GeneratedMethod)
		browserAdapter := rawFixture(t, browserWorkerAdapterFixture{"browser-worker", binding.Operation, browserRequest, browserResponse})
		desktopAdapter := rawFixture(t, desktopWailsAdapterFixture{"desktop-wails", binding.GeneratedMethod, Exchange{Operation: binding.Operation, Control: desktopRequest}, ExchangeResult{Operation: binding.Operation, Control: desktopResponse}})
		if bytes.Equal(browserAdapter, desktopAdapter) {
			t.Fatalf("%s adapters are identical", binding.GeneratedMethod)
		}
		browserNormalized := decodeBrowserWorkerAdapter(t, browserAdapter, binding, ownerDecoder)
		desktopNormalized := decodeDesktopWailsAdapter(t, desktopAdapter, binding, ownerDecoder)
		if !reflect.DeepEqual(browserNormalized, desktopNormalized) {
			t.Fatalf("%s canonical result parity drift", binding.GeneratedMethod)
		}
		if err := validateTypedControls(binding, []byte(`{}`), desktopResponse, ownerDecoder); err == nil {
			t.Fatalf("%s accepted malformed request fixture", binding.GeneratedMethod)
		}
		if err := validateTypedControls(binding, desktopRequest, []byte(`{}`), ownerDecoder); err == nil {
			t.Fatalf("%s accepted malformed result fixture", binding.GeneratedMethod)
		}
		invoked, err := clients.Invoke(context.Background(), binding.GeneratedMethod, Exchange{Operation: binding.Operation, Control: desktopRequest})
		if err != nil || invoked.Operation != binding.Operation || !reflect.DeepEqual(normalizedJSONWithoutRequestID(t, invoked.Control), normalizedJSONWithoutRequestID(t, desktopResponse)) {
			t.Fatalf("%s production Invoke did not preserve validated canonical result: %v", binding.GeneratedMethod, err)
		}
	}
}

func TestInvokeRejectsMalformedCrossOperationAndMismatchedResponses(t *testing.T) {
	clients := completeClientSet(t)
	compile, _ := findBinding("EngineCompile", "engine.compile")
	request := operationRequestFixture(t, compile, "invoke-negative")
	clients.Engine.Compile = func(context.Context, Exchange) (ExchangeResult, error) {
		return ExchangeResult{Operation: compile.Operation, Control: []byte(`{}`)}, nil
	}
	if _, err := clients.Invoke(context.Background(), compile.GeneratedMethod, Exchange{Operation: compile.Operation, Control: request}); err == nil {
		t.Fatal("malformed response crossed Wails")
	}
	clients = completeClientSet(t)
	clients.Engine.Compile = func(context.Context, Exchange) (ExchangeResult, error) {
		return ExchangeResult{Operation: "engine.handshake", Control: operationResponseFixture(t, compile, "invoke-negative")}, nil
	}
	if _, err := clients.Invoke(context.Background(), compile.GeneratedMethod, Exchange{Operation: compile.Operation, Control: request}); err == nil {
		t.Fatal("cross-operation outer response crossed Wails")
	}
	clients = completeClientSet(t)
	clients.Engine.Compile = func(context.Context, Exchange) (ExchangeResult, error) {
		return ExchangeResult{Operation: compile.Operation, Control: operationResponseFixture(t, compile, "other-request")}, nil
	}
	if _, err := clients.Invoke(context.Background(), compile.GeneratedMethod, Exchange{Operation: compile.Operation, Control: request}); err == nil {
		t.Fatal("mismatched response request ID crossed Wails")
	}
	registryList, _ := findBinding("RegistryListSources", "registry.list_sources")
	registrySearch, _ := findBinding("RegistrySearch", "registry.search")
	registryRequest := operationRequestFixture(t, registryList, "registry-negative")
	clients = completeClientSet(t)
	clients.Registry.ListSources = func(context.Context, Exchange) (ExchangeResult, error) {
		return ExchangeResult{Operation: registryList.Operation, Control: operationResponseFixture(t, registrySearch, "registry-negative")}, nil
	}
	if _, err := clients.Invoke(context.Background(), registryList.GeneratedMethod, Exchange{Operation: registryList.Operation, Control: registryRequest}); err == nil {
		t.Fatal("cross-operation Registry result crossed Wails")
	}
	review, _ := findBinding("ReviewSubmit", "review.submit")
	export, _ := findBinding("HostExport", "host.export")
	reviewRequest := operationRequestFixture(t, review, "owner-negative")
	clients = completeClientSet(t)
	clients.Review.Submit = func(context.Context, Exchange) (ExchangeResult, error) {
		return ExchangeResult{Operation: review.Operation, Control: operationResponseFixture(t, export, "owner-negative")}, nil
	}
	if _, err := clients.Invoke(context.Background(), review.GeneratedMethod, Exchange{Operation: review.Operation, Control: reviewRequest}); err == nil {
		t.Fatal("cross-operation owner result crossed Wails")
	}
}
