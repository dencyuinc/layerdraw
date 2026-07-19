// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package search contains physical native Search/Query/Analysis adapters. It
// deliberately has no LDL, ranking, StableAddress, Access, or result semantics.
package search

import (
	"context"
	"errors"
	"fmt"

	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

var ErrInvalidPlan = errors.New("native search adapter: invalid Engine plan")

type NativeBackend interface {
	ExecutePlan(context.Context, port.PlanKind, []byte) ([]port.RawRow, error)
	Cancel(context.Context, string) error
}

type PlanVerifier interface {
	VerifyPlan(context.Context, port.ExecutionPlan) error
}

type NativeExecutor struct {
	capability port.QueryAdapterCapability
	backend    NativeBackend
	verifier   PlanVerifier
}

func NewNativeExecutor(capability port.QueryAdapterCapability, backend NativeBackend, verifier PlanVerifier) (*NativeExecutor, error) {
	if capability.AdapterID == "" || capability.BackendVersion == "" || capability.PlanProtocolVersion == "" || capability.MaxRows <= 0 || capability.MaxBytes <= 0 || backend == nil || verifier == nil {
		return nil, fmt.Errorf("invalid native search adapter configuration")
	}
	return &NativeExecutor{capability: capability, backend: backend, verifier: verifier}, nil
}

func (e *NativeExecutor) Capabilities(context.Context) (port.QueryAdapterCapability, error) {
	result := e.capability
	result.Primitives = append([]port.SearchPrimitive(nil), result.Primitives...)
	return result, nil
}

func (e *NativeExecutor) Execute(ctx context.Context, plan port.ExecutionPlan) (port.ExecutionResult, error) {
	if plan.PlanID == "" || plan.Token == "" || len(plan.Payload) == 0 || plan.ProtocolVersion != e.capability.PlanProtocolVersion || plan.MaxRows <= 0 || plan.MaxRows > e.capability.MaxRows || plan.MaxBytes <= 0 || plan.MaxBytes > e.capability.MaxBytes {
		return port.ExecutionResult{}, ErrInvalidPlan
	}
	if plan.Kind != port.PlanQuery && plan.Kind != port.PlanSearch && plan.Kind != port.PlanAnalysis && plan.Kind != port.PlanSearchIndex {
		return port.ExecutionResult{}, ErrInvalidPlan
	}
	if err := e.verifier.VerifyPlan(ctx, plan); err != nil {
		return port.ExecutionResult{}, ErrInvalidPlan
	}
	rows, err := e.backend.ExecutePlan(ctx, plan.Kind, append([]byte(nil), plan.Payload...))
	if err != nil {
		return port.ExecutionResult{}, err
	}
	result := port.ExecutionResult{Rows: make([]port.RawRow, 0, min(len(rows), plan.MaxRows))}
	for _, row := range rows {
		if len(result.Rows) == plan.MaxRows {
			result.Truncated = true
			break
		}
		cloned := make(port.RawRow, len(row))
		rowBytes := 0
		for key, value := range row {
			rowBytes += len(key) + len(value.Kind) + len(value.Value)
			cloned[key] = value
		}
		if result.Bytes+rowBytes > plan.MaxBytes {
			result.Truncated = true
			return result, nil
		}
		result.Bytes += rowBytes
		result.Rows = append(result.Rows, cloned)
	}
	return result, nil
}

func (e *NativeExecutor) Cancel(ctx context.Context, planID string) error {
	if planID == "" {
		return ErrInvalidPlan
	}
	return e.backend.Cancel(ctx, planID)
}
