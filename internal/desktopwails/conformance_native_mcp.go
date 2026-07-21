// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package desktopwails

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/mcphost"
	layerruntime "github.com/dencyuinc/layerdraw/internal/runtime"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func conformanceNativeMCP(ctx context.Context, instance *conformanceInstance, tools []mcphost.Tool) error {
	for _, name := range []string{"layerdraw.search", "layerdraw.run_query", "layerdraw.analyze_graph"} {
		found := false
		for _, tool := range tools {
			found = found || tool.Name == name
		}
		if !found {
			return fmt.Errorf("bundled native MCP tool %s is absent", name)
		}
	}
	opened, err := instance.openProject(ctx, conformanceProjectSource)
	if err != nil {
		return fmt.Errorf("native MCP project open failed: %w", err)
	}
	revision := opened.Open.CommittedRevision
	accessDigest := string(opened.Open.AccessSummary.AccessFingerprint)
	snapshot := port.DocumentSnapshotRef{Kind: port.SnapshotHostRevision, HostDocumentID: string(revision.DocumentID), CommittedRevision: string(revision.RevisionID), DefinitionHash: string(revision.DefinitionHash)}
	binding := &mcphost.Binding{DocumentID: opened.Open.Session.Scope.DocumentID, RevisionDigest: revision.DefinitionHash, AccessFingerprint: opened.Open.Session.Scope.AccessFingerprint}
	call := func(name, operation, requestID string, payload any) error {
		payloadBytes, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return marshalErr
		}
		raw, marshalErr := json.Marshal(nativeSearchEnvelope{Operation: operation, Payload: payloadBytes, Protocol: runtimeprotocol.RuntimeProtocolRef{Name: runtimeprotocol.RuntimeProtocolRefNameValue, Version: "1.0"}, RequestID: requestID})
		if marshalErr != nil {
			return marshalErr
		}
		result := instance.app.MCPCallTool(ctx, mcphost.CallToolRequest{Name: name, RequestID: requestID, Arguments: raw, Binding: binding})
		var response nativeSearchResponseEnvelope
		if result.Failure != nil || decodeExactBytes(result.Content, &response) != nil || response.Outcome != protocolcommon.OutcomeSuccess || len(response.Payload) == 0 {
			return fmt.Errorf("native MCP tool %s failed: %+v", name, result.Failure)
		}
		return nil
	}
	bound := func(request []byte) port.BoundExecutionRequest {
		return port.BoundExecutionRequest{Session: &opened.Open.Session, Snapshot: snapshot, AccessProjectionDigest: accessDigest, Request: request, MaxOutputBytes: 1 << 20}
	}
	if err := call("layerdraw.run_query", "native.execute_query", "conformance-native-query", bound([]byte(`{"kind":"structural_query","root_addresses":["ldl:project:p:entity:alpha"]}`))); err != nil {
		return err
	}
	profile, embedding := packagedSearchProfile(), packagedEmbeddingProfile()
	search := layerruntime.SearchRequest{Session: &opened.Open.Session, Snapshot: snapshot, AccessProjectionDigest: accessDigest, SearchProfile: profile, EmbeddingProfile: &embedding, IndexIdentity: packagedSearchIdentity(snapshot, accessDigest), Mode: "lexical", QueryText: "Service", EngineRequest: []byte(`{"kind":"search_documents","mode":"lexical","query_text":"Service"}`), MaxOutputBytes: 1 << 20}
	if err := call("layerdraw.search", "native.execute_search", "conformance-native-search", search); err != nil {
		return err
	}
	analysis := bound([]byte(`{"kind":"analyze_graph","algorithm":"page_rank","entity_addresses":["ldl:project:p:entity:alpha","ldl:project:p:entity:beta"],"relation_addresses":["ldl:project:p:relation:alpha_beta"],"parameters":{}}`))
	return call("layerdraw.analyze_graph", "native.execute_analysis", "conformance-native-analysis", analysis)
}
