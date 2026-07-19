// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopcontract

import (
	"context"
	"errors"
)

type BindingTarget string

const (
	TargetEngine   BindingTarget = "engine_client"
	TargetRuntime  BindingTarget = "runtime_client"
	TargetRegistry BindingTarget = "registry_client"
	TargetReview   BindingTarget = "review_client"
	TargetHost     BindingTarget = "host_client"
)

// Exchange is the mechanical generated-binding unit. Control contains an
// existing generated protocol envelope; blobs retain their protocol identity.
// The shell does not decode domain payloads.
type Exchange struct {
	Operation string `json:"operation"`
	Control   []byte `json:"control"`
	Blobs     []Blob `json:"blobs"`
}

type Blob struct {
	ID    string `json:"blob_id"`
	Bytes []byte `json:"bytes"`
}

type ExchangeResult struct {
	Control []byte `json:"control"`
	Blobs   []Blob `json:"blobs"`
}

// ProtocolClient is implemented by existing Engine, Runtime, Registry, Review,
// and Host client facades. Generated Wails methods select a target and forward
// the exchange without semantic conversion.
type ProtocolClient interface {
	Exchange(context.Context, Exchange) (ExchangeResult, error)
}

type ClientSet struct {
	Engine   ProtocolClient
	Runtime  ProtocolClient
	Registry ProtocolClient
	Review   ProtocolClient
	Host     ProtocolClient
}

func (c ClientSet) Validate() error {
	if c.Engine == nil || c.Runtime == nil || c.Registry == nil || c.Review == nil || c.Host == nil {
		return errors.New("desktop contract: complete binding client set is required")
	}
	return nil
}

type BindingMethod struct {
	GeneratedMethod string        `json:"generated_method"`
	Target          BindingTarget `json:"target"`
	OperationPrefix string        `json:"operation_prefix"`
}

var generatedBindingTable = []BindingMethod{
	{"EngineExchange", TargetEngine, "engine."},
	{"RuntimeExchange", TargetRuntime, "runtime."},
	{"RegistryExchange", TargetRegistry, "registry."},
	{"ReviewExchange", TargetReview, "review."},
	{"HostExchange", TargetHost, "host."},
}

func GeneratedBindingTable() []BindingMethod {
	return append([]BindingMethod(nil), generatedBindingTable...)
}

func ResolveBinding(method, operation string) (BindingTarget, error) {
	for _, binding := range generatedBindingTable {
		if binding.GeneratedMethod == method {
			if len(operation) <= len(binding.OperationPrefix) || operation[:len(binding.OperationPrefix)] != binding.OperationPrefix {
				return "", errors.New("desktop contract: operation does not match generated binding target")
			}
			return binding.Target, nil
		}
	}
	return "", errors.New("desktop contract: generated binding method is not approved")
}
