// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/mcphost"
)

type canonicalTransportStub struct{ started, stopped bool }

func (t *canonicalTransportStub) Start(_ context.Context, _ mcphost.Handler) error {
	t.started = true
	return nil
}
func (t *canonicalTransportStub) Shutdown(context.Context) error { t.stopped = true; return nil }

type canonicalOwnerStub struct{}

func (canonicalOwnerStub) Capabilities(context.Context) (mcphost.CapabilitySnapshot, error) {
	digest := protocolcommon.Digest("sha256:0000000000000000000000000000000000000000000000000000000000000000")
	return mcphost.CapabilitySnapshot{ManifestETag: "desktop", Operations: map[string]mcphost.OperationCapability{}, Resources: []mcphost.ResourceCapability{}, GrantSummary: accessprotocol.AuthoringGrantSummary{AccessFingerprint: digest, ConstrainedCapabilities: []semantic.AuthoringCapability{}, GrantedCapabilities: []semantic.AuthoringCapability{}, PolicyEtag: digest}}, nil
}
func (canonicalOwnerStub) Invoke(context.Context, mcphost.OwnerRequest) (mcphost.OwnerResponse, error) {
	return mcphost.OwnerResponse{Content: json.RawMessage(`{}`)}, nil
}
func (canonicalOwnerStub) ReadResource(context.Context, mcphost.ResourceRequest) (mcphost.ResourceResponse, error) {
	return mcphost.ResourceResponse{Content: json.RawMessage(`{}`), MimeType: "application/json"}, nil
}

func TestCanonicalMCPPortOwnsInProcessLifecycle(t *testing.T) {
	transport := &canonicalTransportStub{}
	host, err := mcphost.New(mcphost.Config{Owner: canonicalOwnerStub{}, Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	port := BindCanonicalMCPHost(host)
	if port == nil {
		t.Fatal("nil canonical port")
	}
	if result := port.Start(context.Background()); !result.Validate() || result.Outcome != protocolcommon.OutcomeSuccess || !transport.started {
		t.Fatalf("start=%+v", result)
	}
	if result := port.Shutdown(context.Background()); !result.Validate() || result.Outcome != protocolcommon.OutcomeSuccess || !transport.stopped {
		t.Fatalf("stop=%+v", result)
	}
}

func TestCanonicalConstructorRejectsMissingOrAmbiguousMCPHost(t *testing.T) {
	if _, err := NewCanonical(Config{}); err == nil {
		t.Fatal("missing host accepted")
	}
	host, err := mcphost.New(mcphost.Config{Owner: canonicalOwnerStub{}, Transport: &canonicalTransportStub{}})
	if err != nil {
		t.Fatal(err)
	}
	config := Config{MCPHost: host}
	config.HostPorts.MCP = mcpPortStub{}
	if _, err = NewCanonical(config); err == nil {
		t.Fatal("ambiguous MCP composition accepted")
	}
}

func TestCanonicalMCPPortMapsLifecycleFailures(t *testing.T) {
	if BindCanonicalMCPHost(nil) != nil {
		t.Fatal("nil host was bound")
	}
	transport := &canonicalTransportStub{}
	owner := canonicalOwnerStub{}
	host, err := mcphost.New(mcphost.Config{Owner: owner, Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	port := BindCanonicalMCPHost(host)
	if result := port.Shutdown(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("idempotent=%+v", result)
	}
	transportFailure := &failingCanonicalTransport{}
	host, err = mcphost.New(mcphost.Config{Owner: owner, Transport: transportFailure})
	if err != nil {
		t.Fatal(err)
	}
	port = BindCanonicalMCPHost(host)
	if result := port.Start(context.Background()); !result.Validate() || result.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("start=%+v", result)
	}
}

type failingCanonicalTransport struct{}

func (*failingCanonicalTransport) Start(context.Context, mcphost.Handler) error {
	return context.Canceled
}
func (*failingCanonicalTransport) Shutdown(context.Context) error { return context.Canceled }
