// SPDX-License-Identifier: Apache-2.0

package conformance_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	wasmtransport "github.com/dencyuinc/layerdraw/internal/transport/wasm"
)

func TestEngineWASMWorkerV1ContractMatchesGoAuthority(t *testing.T) {
	data, err := os.ReadFile("testdata/engine_wasm_worker_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		WorkerProtocol        string `json:"worker_protocol"`
		WorkerProtocolVersion int    `json:"worker_protocol_version"`
		TransportID           string `json:"transport_id"`
		IdentifierLimits      struct {
			EndpointGenerationUTF8Bytes int `json:"endpoint_generation_utf8_bytes"`
			ExchangeIDUTF8Bytes         int `json:"exchange_id_utf8_bytes"`
			BlobIDUTF8Bytes             int `json:"blob_id_utf8_bytes"`
		} `json:"identifier_limits"`
		TransportLimits    wasmtransport.TransportLimits     `json:"transport_limits"`
		FailureDefinitions []wasmtransport.FailureDefinition `json:"failure_definitions"`
		OuterMessages      map[string]map[string][]string    `json:"outer_messages"`
	}
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	if contract.WorkerProtocol != wasmtransport.WorkerProtocol || contract.WorkerProtocolVersion != wasmtransport.WorkerProtocolVersion || contract.TransportID != wasmtransport.TransportID {
		t.Fatalf("worker identity drift: %+v", contract)
	}
	if contract.IdentifierLimits.EndpointGenerationUTF8Bytes != 128 || contract.IdentifierLimits.ExchangeIDUTF8Bytes != 128 || int64(contract.IdentifierLimits.BlobIDUTF8Bytes) != contract.TransportLimits.MaxBlobIDBytes {
		t.Fatalf("identifier limit drift: %+v", contract.IdentifierLimits)
	}
	if contract.TransportLimits != wasmtransport.BrowserTransportLimits() {
		t.Fatalf("transport limit drift: got %+v want %+v", contract.TransportLimits, wasmtransport.BrowserTransportLimits())
	}
	if !reflect.DeepEqual(contract.FailureDefinitions, wasmtransport.FailureDefinitions()) {
		t.Fatalf("failure vocabulary drift: got %+v want %+v", contract.FailureDefinitions, wasmtransport.FailureDefinitions())
	}
	if len(contract.OuterMessages["host_to_worker"]) != 3 || len(contract.OuterMessages["worker_to_host"]) != 4 {
		t.Fatalf("outer grammar is not closed: %+v", contract.OuterMessages)
	}
}
