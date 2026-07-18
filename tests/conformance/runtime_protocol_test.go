// SPDX-License-Identifier: Apache-2.0

package conformance_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
)

func TestRuntimeCanonicalFixturesRoundTripInGeneratedGo(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		roundTrip func([]byte) ([]byte, error)
	}{
		{"handshake", "handshake-request.json", func(data []byte) ([]byte, error) {
			value, err := runtimeprotocol.DecodeRuntimeHandshakeRequestEnvelope(data)
			if err != nil {
				return nil, err
			}
			return runtimeprotocol.EncodeRuntimeHandshakeRequestEnvelope(value)
		}},
		{"handshake failure", "handshake-failed.json", func(data []byte) ([]byte, error) {
			value, err := runtimeprotocol.DecodeRuntimeHandshakeResponseEnvelope(data)
			if err != nil {
				return nil, err
			}
			return runtimeprotocol.EncodeRuntimeHandshakeResponseEnvelope(value)
		}},
		{"commit", "commit-request.json", func(data []byte) ([]byte, error) {
			value, err := runtimeprotocol.DecodeCommitOperationsRequestEnvelope(data)
			if err != nil {
				return nil, err
			}
			return runtimeprotocol.EncodeCommitOperationsRequestEnvelope(value)
		}},
		{"commit failure", "commit-failed.json", func(data []byte) ([]byte, error) {
			value, err := runtimeprotocol.DecodeCommitOperationsResponseEnvelope(data)
			if err != nil {
				return nil, err
			}
			return runtimeprotocol.EncodeCommitOperationsResponseEnvelope(value)
		}},
		{"recovery", "operation-recovering.json", func(data []byte) ([]byte, error) {
			value, err := runtimeprotocol.DecodeRuntimeOperationStatus(data)
			if err != nil {
				return nil, err
			}
			return runtimeprotocol.EncodeRuntimeOperationStatus(value)
		}},
		{"recovery audit pending", "operation-audit-pending.json", func(data []byte) ([]byte, error) {
			value, err := runtimeprotocol.DecodeRuntimeOperationStatus(data)
			if err != nil {
				return nil, err
			}
			return runtimeprotocol.EncodeRuntimeOperationStatus(value)
		}},
		{"recovery needs review", "operation-needs-review.json", func(data []byte) ([]byte, error) {
			value, err := runtimeprotocol.DecodeRuntimeOperationStatus(data)
			if err != nil {
				return nil, err
			}
			return runtimeprotocol.EncodeRuntimeOperationStatus(value)
		}},
		{"history", "revision-page.json", func(data []byte) ([]byte, error) {
			value, err := runtimeprotocol.DecodeRevisionPage(data)
			if err != nil {
				return nil, err
			}
			return runtimeprotocol.EncodeRevisionPage(value)
		}},
		{"access decision", "access-decision.json", func(data []byte) ([]byte, error) {
			value, err := accessprotocol.DecodeAuthoringDecision(data)
			if err != nil {
				return nil, err
			}
			return accessprotocol.EncodeAuthoringDecision(value)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := runtimeFixture(t, test.file)
			first, err := test.roundTrip(input)
			if err != nil {
				t.Fatal(err)
			}
			second, err := test.roundTrip(first)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first, second) {
				t.Fatalf("codec is not canonical:\n%s\n%s", first, second)
			}
		})
	}
}

func TestRuntimeGeneratedGoRejectsUnknownFieldsAndInvalidClosedOutcomes(t *testing.T) {
	for _, name := range []string{"commit-result-preview-impact-only.json", "commit-result-preview-decision-only.json"} {
		if _, err := runtimeprotocol.DecodeRuntimeCommitResult(runtimeFixture(t, name)); err == nil {
			t.Fatalf("one-sided preview evaluation was accepted: %s", name)
		}
	}
	var needsReview map[string]any
	if err := json.Unmarshal(runtimeFixture(t, "operation-needs-review.json"), &needsReview); err != nil {
		t.Fatal(err)
	}
	needsReview["operation_result"].(map[string]any)["diagnostics"] = []any{}
	if _, err := runtimeprotocol.DecodeRuntimeOperationStatus(mustJSONRuntime(t, needsReview)); err == nil {
		t.Fatal("needs_review without LDL1903 evidence was accepted")
	}
	var handshake map[string]any
	if err := json.Unmarshal(runtimeFixture(t, "handshake-request.json"), &handshake); err != nil {
		t.Fatal(err)
	}
	handshake["unknown_minor_field"] = true
	if _, err := runtimeprotocol.DecodeRuntimeHandshakeRequestEnvelope(mustJSONRuntime(t, handshake)); err == nil {
		t.Fatal("unknown top-level field was accepted")
	}
	delete(handshake, "unknown_minor_field")
	correctUnits := map[string]string{
		"max_blob_bytes": "bytes", "max_blob_total_bytes": "bytes", "max_commit_operations": "items",
		"max_history_items": "items", "max_output_bytes": "bytes", "max_state_mutations": "items",
	}
	for field, correctUnit := range correctUnits {
		limits := make(map[string]any, len(correctUnits))
		for name, unit := range correctUnits {
			limits[name] = map[string]any{"hard_maximum": "10", "unit": unit}
		}
		wrongUnit := "bytes"
		if correctUnit == "bytes" {
			wrongUnit = "items"
		}
		limits[field].(map[string]any)["unit"] = wrongUnit
		handshake["payload"].(map[string]any)["client_limits"] = limits
		if _, err := runtimeprotocol.DecodeRuntimeHandshakeRequestEnvelope(mustJSONRuntime(t, handshake)); err == nil {
			t.Fatalf("invalid unit for %s was accepted", field)
		}
	}

	var commit map[string]any
	if err := json.Unmarshal(runtimeFixture(t, "commit-request.json"), &commit); err != nil {
		t.Fatal(err)
	}
	payload := commit["payload"].(map[string]any)
	payload["operation_batch"].(map[string]any)["base_revision"].(map[string]any)["document_id"] = ""
	if _, err := runtimeprotocol.DecodeCommitOperationsRequestEnvelope(mustJSONRuntime(t, commit)); err == nil {
		t.Fatal("malformed revision scope was accepted")
	}

	invalidResult := []byte(`{"operation_id":"operation_1","idempotency_key":"idem_commit_000001","status":"rejected","diagnostics":[],"result_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	if _, err := runtimeprotocol.DecodeOperationResult(invalidResult); err == nil {
		t.Fatal("rejected outcome without a stable failure code was accepted")
	}

	invalidRecovery := []byte(`{"operation_id":"operation_1","idempotency_key":"idem_commit_000001","phase":"final"}`)
	if _, err := runtimeprotocol.DecodeRuntimeOperationStatus(invalidRecovery); err == nil {
		t.Fatal("final recovery phase without OperationResult was accepted")
	}
	invalidAuditRecovery := []byte(`{"operation_id":"operation_1","idempotency_key":"idem_commit_000001","phase":"audit_pending","recovery_started_at":"2026-07-18T10:00:00Z"}`)
	if _, err := runtimeprotocol.DecodeRuntimeOperationStatus(invalidAuditRecovery); err == nil {
		t.Fatal("audit_pending recovery accepted recovering-only state")
	}

	wrongRuntimeProtocol := []byte(`{"operation":"runtime.list_revisions","payload":{"max_items":"1","max_output_bytes":"1024","session":{"runtime_session_id":"runtime_session_fixture_1","scope":{"access_fingerprint":"sha256:1111111111111111111111111111111111111111111111111111111111111111","document_id":"doc_fixture","local_scope_id":"local_fixture"},"session_generation":"1"}},"protocol":{"name":"engine","version":"1.0"},"request_id":"runtime-list-wrong-protocol"}`)
	if _, err := runtimeprotocol.DecodeListRevisionsRequestEnvelope(wrongRuntimeProtocol); err == nil {
		t.Fatal("list revisions accepted a non-Runtime protocol name")
	}

	invalidDecision := []byte(`{"access_fingerprint":"sha256:1111111111111111111111111111111111111111111111111111111111111111","approval_rule_refs":[],"constraint_violations":[],"decision_digest":"sha256:3333333333333333333333333333333333333333333333333333333333333333","diagnostics":[],"evaluation_digest":"sha256:4444444444444444444444444444444444444444444444444444444444444444","host_operation_impact_digests":[],"missing_capabilities":[],"outcome":"deny","required_capabilities":[]}`)
	if _, err := accessprotocol.DecodeAuthoringDecision(invalidDecision); err == nil {
		t.Fatal("deny without a reason was accepted")
	}
	unsortedResources := []byte(`{"action":"stage","impact_digest":"sha256:3333333333333333333333333333333333333333333333333333333333333333","operation_kind":"asset_stage","required_authoring_capabilities":["asset:write"],"resource_refs":["z","a"],"resource_scope":{"document_id":"doc_fixture","local_scope_id":"local_fixture"}}`)
	if _, err := accessprotocol.DecodeHostOperationImpact(unsortedResources); err == nil {
		t.Fatal("non-canonical HostOperationImpact resource set was accepted")
	}
	unsortedDecisionDigests := []byte(`{"access_fingerprint":"sha256:1111111111111111111111111111111111111111111111111111111111111111","approval_rule_refs":[],"constraint_violations":[],"decision_digest":"sha256:3333333333333333333333333333333333333333333333333333333333333333","diagnostics":[],"evaluation_digest":"sha256:4444444444444444444444444444444444444444444444444444444444444444","host_operation_impact_digests":["sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"],"missing_capabilities":[],"outcome":"allow","required_capabilities":[]}`)
	if _, err := accessprotocol.DecodeAuthoringDecision(unsortedDecisionDigests); err == nil {
		t.Fatal("non-canonical HostOperationImpact digest set was accepted")
	}
	unsortedApprovalRules := []byte(`{"access_fingerprint":"sha256:1111111111111111111111111111111111111111111111111111111111111111","approval_rule_refs":["z","a"],"constraint_violations":[],"decision_digest":"sha256:3333333333333333333333333333333333333333333333333333333333333333","diagnostics":[],"evaluation_digest":"sha256:4444444444444444444444444444444444444444444444444444444444444444","host_operation_impact_digests":[],"missing_capabilities":[],"outcome":"approval_required","required_capabilities":[]}`)
	if _, err := accessprotocol.DecodeAuthoringDecision(unsortedApprovalRules); err == nil {
		t.Fatal("non-canonical approval rule set was accepted")
	}
}

func runtimeFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(protocolRepositoryRoot(t), "schemas", "fixtures", "runtime", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustJSONRuntime(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
