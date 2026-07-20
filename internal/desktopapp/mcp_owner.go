// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/mcphost"
)

type MCPCapabilitySource interface {
	Snapshot(context.Context) (mcphost.CapabilitySnapshot, error)
}
type MCPResourceSource interface {
	Read(context.Context, mcphost.ResourceRequest) (mcphost.ResourceResponse, error)
}

// DesktopMCPOwner is the concrete production owner adapter. Every operation
// must exist in Desktop's closed generated/owner binding table; ClientSet then
// performs the exact generated request and response decoding used by Wails.
type DesktopMCPOwner struct {
	clients      desktopcontract.ClientSet
	capabilities MCPCapabilitySource
	resources    MCPResourceSource
	methods      map[string]string
}

func NewDesktopMCPOwner(clients desktopcontract.ClientSet, capabilities MCPCapabilitySource, resources MCPResourceSource) (*DesktopMCPOwner, error) {
	if clients.Validate() != nil || capabilities == nil {
		return nil, errors.New("desktop MCP owner composition is incomplete")
	}
	methods := map[string]string{}
	for _, binding := range desktopcontract.GeneratedBindingTable() {
		methods[binding.Operation] = binding.GeneratedMethod
	}
	return &DesktopMCPOwner{clients: clients, capabilities: capabilities, resources: resources, methods: methods}, nil
}
func (o *DesktopMCPOwner) Capabilities(ctx context.Context) (mcphost.CapabilitySnapshot, error) {
	snapshot, err := o.capabilities.Snapshot(ctx)
	if err != nil {
		return mcphost.CapabilitySnapshot{}, err
	}
	for operation, capability := range snapshot.Operations {
		if capability.Enabled && o.methods[operation] == "" {
			return mcphost.CapabilitySnapshot{}, errors.New("MCP capability has no generated Desktop owner binding")
		}
	}
	return snapshot, nil
}
func (o *DesktopMCPOwner) Invoke(ctx context.Context, request mcphost.OwnerRequest) (mcphost.OwnerResponse, error) {
	method := o.methods[request.Operation]
	if method == "" {
		return mcphost.OwnerResponse{}, &mcphost.OwnerError{Code: mcphost.ErrorCapabilityUnavailable}
	}
	control, err := adaptMCPPageRequest(request.Operation, request.Arguments, request.Continuation)
	if err != nil {
		return mcphost.OwnerResponse{}, &mcphost.OwnerError{Code: mcphost.ErrorInvalidCursor}
	}
	result, err := o.clients.Invoke(ctx, method, desktopcontract.Exchange{Operation: request.Operation, Control: control, Blobs: []desktopcontract.Blob{}})
	if err != nil {
		return mcphost.OwnerResponse{}, err
	}
	outcome, err := ownerOutcome(result.Control)
	if err != nil {
		return mcphost.OwnerResponse{}, err
	}
	items, nextCursor, err := inspectMCPPage(request.Operation, result.Control)
	if err != nil {
		return mcphost.OwnerResponse{}, err
	}
	return mcphost.OwnerResponse{Content: append(json.RawMessage(nil), result.Control...), NextCursor: nextCursor, Items: items, Outcome: outcome}, nil
}
func (o *DesktopMCPOwner) ReadResource(ctx context.Context, request mcphost.ResourceRequest) (mcphost.ResourceResponse, error) {
	if o.resources == nil {
		return mcphost.ResourceResponse{}, &mcphost.OwnerError{Code: mcphost.ErrorCapabilityUnavailable}
	}
	return o.resources.Read(ctx, request)
}
func ownerOutcome(control []byte) (protocolcommon.Outcome, error) {
	var value struct {
		Outcome protocolcommon.Outcome `json:"outcome"`
		OK      *bool                  `json:"ok"`
	}
	if json.Unmarshal(control, &value) != nil {
		return "", errors.New("owner response outcome is invalid")
	}
	if value.Outcome != "" {
		switch value.Outcome {
		case protocolcommon.OutcomeSuccess, protocolcommon.OutcomeRejected, protocolcommon.OutcomeFailed, protocolcommon.OutcomeCancelled:
			return value.Outcome, nil
		}
		return "", errors.New("owner response outcome is invalid")
	}
	if value.OK != nil {
		if *value.OK {
			return protocolcommon.OutcomeSuccess, nil
		}
		return protocolcommon.OutcomeRejected, nil
	}
	return "", errors.New("owner response outcome is absent")
}

type canonicalMCPComposition struct {
	host      *mcphost.Host
	transport *mcphost.LocalTransport
}

func composeCanonicalMCP(clients desktopcontract.ClientSet, capabilities MCPCapabilitySource, resources MCPResourceSource, limits mcphost.Limits) (canonicalMCPComposition, error) {
	owner, err := NewDesktopMCPOwner(clients, capabilities, resources)
	if err != nil {
		return canonicalMCPComposition{}, err
	}
	transport := &mcphost.LocalTransport{}
	host, err := mcphost.New(mcphost.Config{Owner: owner, Transport: transport, Limits: limits})
	if err != nil {
		return canonicalMCPComposition{}, err
	}
	return canonicalMCPComposition{host: host, transport: transport}, nil
}
