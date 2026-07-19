// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package search contains physical native Search/Query/Analysis adapters. It
// deliberately has no LDL, ranking, StableAddress, Access, or result semantics.
package search

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

var (
	ErrInvalidPlan          = errors.New("native search adapter: invalid Engine plan")
	ErrExecutionLimit       = errors.New("native search adapter: execution limit reached")
	ErrPhysicalIndexMissing = errors.New("native search adapter: physical index missing")
)

type BackendExecution struct {
	Complete      bool
	PhysicalIndex *port.PhysicalIndexRef
}

// NativeBackend must stream through sink. A sink error is a hard stop: drivers
// may not continue pulling rows from Ladybug after it returns.
type NativeBackend interface {
	ExecutePlan(context.Context, port.PlanKind, []byte, port.ExecutionLimits, port.RowSink) (BackendExecution, error)
	Cancel(context.Context, string) error
	InspectPhysicalIndex(context.Context, port.PhysicalIndexRef) error
}

type PlanVerifier interface {
	VerifyPlan(context.Context, port.ExecutionPlan) error
}

// HMACPlanAuthority is concrete Engine-to-adapter provenance. The signing half
// belongs in Engine composition; native consumers receive only the verifier.
type HMACPlanAuthority struct{ key []byte }

func NewHMACPlanAuthority(key []byte) (*HMACPlanAuthority, error) {
	if len(key) < 32 {
		return nil, ErrInvalidPlan
	}
	return &HMACPlanAuthority{key: append([]byte(nil), key...)}, nil
}
func (a *HMACPlanAuthority) Sign(plan port.ExecutionPlan) (port.ExecutionPlan, error) {
	plan.Token = ""
	data, err := json.Marshal(plan)
	if err != nil {
		return plan, err
	}
	mac := hmac.New(sha256.New, a.key)
	_, _ = mac.Write(data)
	plan.Token = base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return plan, nil
}
func (a *HMACPlanAuthority) VerifyPlan(_ context.Context, plan port.ExecutionPlan) error {
	token := plan.Token
	plan.Token = ""
	data, err := json.Marshal(plan)
	if err != nil {
		return ErrInvalidPlan
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return ErrInvalidPlan
	}
	mac := hmac.New(sha256.New, a.key)
	_, _ = mac.Write(data)
	if !hmac.Equal(decoded, mac.Sum(nil)) {
		return ErrInvalidPlan
	}
	return nil
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
	r := e.capability
	r.Primitives = append([]port.SearchPrimitive(nil), r.Primitives...)
	return r, nil
}

type boundedSink struct {
	rows                     []port.RawRow
	bytes, maxRows, maxBytes int
	truncated                bool
}

func (s *boundedSink) Push(row port.RawRow) error {
	if len(s.rows) >= s.maxRows {
		s.truncated = true
		return ErrExecutionLimit
	}
	clone := make(port.RawRow, len(row))
	size := 0
	for k, v := range row {
		size += len(k) + len(v.Kind) + len(v.Value)
		clone[k] = v
	}
	if s.bytes+size > s.maxBytes {
		s.truncated = true
		return ErrExecutionLimit
	}
	s.bytes += size
	s.rows = append(s.rows, clone)
	return nil
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
	sink := &boundedSink{rows: make([]port.RawRow, 0, min(plan.MaxRows, 256)), maxRows: plan.MaxRows, maxBytes: plan.MaxBytes}
	execution, err := e.backend.ExecutePlan(ctx, plan.Kind, append([]byte(nil), plan.Payload...), port.ExecutionLimits{MaxRows: plan.MaxRows, MaxBytes: plan.MaxBytes}, sink)
	if err != nil && !errors.Is(err, ErrExecutionLimit) {
		return port.ExecutionResult{}, err
	}
	return port.ExecutionResult{Rows: sink.rows, Bytes: sink.bytes, Truncated: sink.truncated || errors.Is(err, ErrExecutionLimit), Complete: execution.Complete && err == nil, PhysicalIndex: execution.PhysicalIndex}, nil
}
func (e *NativeExecutor) Cancel(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidPlan
	}
	return e.backend.Cancel(ctx, id)
}
func (e *NativeExecutor) InspectPhysicalIndex(ctx context.Context, ref port.PhysicalIndexRef) error {
	return e.backend.InspectPhysicalIndex(ctx, ref)
}

// LadybugPlan is the private Engine-issued physical payload understood by the
// native driver. Query text never crosses the public Runtime/Wails/MCP surface.
type LadybugPlan struct {
	Statements    []LadybugStatement     `json:"statements"`
	PhysicalIndex *port.PhysicalIndexRef `json:"physical_index,omitempty"`
}
type LadybugStatement struct {
	Query      string                   `json:"query"`
	Parameters map[string]port.RawValue `json:"parameters"`
}
type LadybugSession interface {
	ExecutePrepared(context.Context, LadybugStatement, port.ExecutionLimits, port.RowSink) error
	Interrupt()
	InspectIndex(context.Context, port.PhysicalIndexRef) error
}
type PhysicalIndexRecorder interface {
	RecordPhysicalIndex(context.Context, port.PhysicalIndexRef) error
}

// LadybugNativeDriver is the concrete transaction/streaming driver used by
// Desktop composition. A platform Ladybug session supplies the C/Go binding;
// this type owns plan decoding, bounded prepared execution and index evidence.
type LadybugNativeDriver struct {
	session   LadybugSession
	mu        sync.Mutex
	cancelled bool
}

func NewLadybugNativeDriver(session LadybugSession) (*LadybugNativeDriver, error) {
	if session == nil {
		return nil, fmt.Errorf("ladybug session is required")
	}
	return &LadybugNativeDriver{session: session}, nil
}
func (d *LadybugNativeDriver) ExecutePlan(ctx context.Context, kind port.PlanKind, payload []byte, limits port.ExecutionLimits, sink port.RowSink) (BackendExecution, error) {
	var plan LadybugPlan
	if err := json.Unmarshal(payload, &plan); err != nil || len(plan.Statements) == 0 {
		return BackendExecution{}, ErrInvalidPlan
	}
	for _, statement := range plan.Statements {
		if statement.Query == "" {
			return BackendExecution{}, ErrInvalidPlan
		}
		if err := d.session.ExecutePrepared(ctx, statement, limits, sink); err != nil {
			return BackendExecution{}, err
		}
	}
	execution := BackendExecution{Complete: true, PhysicalIndex: plan.PhysicalIndex}
	if kind == port.PlanSearchIndex && plan.PhysicalIndex == nil {
		return BackendExecution{}, ErrInvalidPlan
	}
	if kind == port.PlanSearchIndex {
		recorder, ok := d.session.(PhysicalIndexRecorder)
		if !ok || recorder.RecordPhysicalIndex(ctx, *plan.PhysicalIndex) != nil {
			return BackendExecution{}, ErrPhysicalIndexMissing
		}
	}
	return execution, nil
}
func (d *LadybugNativeDriver) Cancel(context.Context, string) error {
	d.mu.Lock()
	d.cancelled = true
	d.mu.Unlock()
	d.session.Interrupt()
	return nil
}
func (d *LadybugNativeDriver) InspectPhysicalIndex(ctx context.Context, ref port.PhysicalIndexRef) error {
	return d.session.InspectIndex(ctx, ref)
}
