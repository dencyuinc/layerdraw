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

// hmacPlanAuthority never leaves this package. Callers can bind an Engine to
// it, but cannot obtain a raw signer for caller-constructed plans.
type hmacPlanAuthority struct{ key []byte }

func newHMACPlanAuthority(key []byte) (*hmacPlanAuthority, error) {
	if len(key) < 32 {
		return nil, ErrInvalidPlan
	}
	return &hmacPlanAuthority{key: append([]byte(nil), key...)}, nil
}
func (a *hmacPlanAuthority) sign(plan port.ExecutionPlan) (port.ExecutionPlan, error) {
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
func (a *hmacPlanAuthority) VerifyPlan(_ context.Context, plan port.ExecutionPlan) error {
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

type authorizedSearchEngine struct {
	engine    port.SearchEngine
	authority *hmacPlanAuthority
}

// BindEnginePlanAuthority returns an Engine decorator and a verify-only
// adapter capability. The HMAC signing primitive is intentionally not
// exported, so Wails/MCP/host callers cannot authorize arbitrary payloads.
func BindEnginePlanAuthority(engine port.SearchEngine, key []byte) (port.SearchEngine, PlanVerifier, error) {
	if engine == nil {
		return nil, nil, ErrInvalidPlan
	}
	authority, err := newHMACPlanAuthority(key)
	if err != nil {
		return nil, nil, err
	}
	return &authorizedSearchEngine{engine: engine, authority: authority}, authority, nil
}

func (e *authorizedSearchEngine) PrepareSearchIndex(ctx context.Context, input port.SearchIndexPreparationInput) (port.ExecutionPlan, error) {
	plan, err := e.engine.PrepareSearchIndex(ctx, input)
	if err != nil {
		return port.ExecutionPlan{}, err
	}
	return e.authority.sign(plan)
}
func (e *authorizedSearchEngine) PrepareSearch(ctx context.Context, input port.SearchPreparationInput) (port.PreparedSearch, error) {
	prepared, err := e.engine.PrepareSearch(ctx, input)
	if err != nil {
		return port.PreparedSearch{}, err
	}
	prepared.Plan, err = e.authority.sign(prepared.Plan)
	return prepared, err
}
func (e *authorizedSearchEngine) CompleteSearch(ctx context.Context, input port.CompleteSearchInput) ([]byte, error) {
	return e.engine.CompleteSearch(ctx, input)
}
func (e *authorizedSearchEngine) PrepareQuery(ctx context.Context, input port.BoundExecutionRequest) (port.ExecutionPlan, error) {
	plan, err := e.engine.PrepareQuery(ctx, input)
	if err != nil {
		return port.ExecutionPlan{}, err
	}
	return e.authority.sign(plan)
}
func (e *authorizedSearchEngine) CompleteQuery(ctx context.Context, input port.CompleteExecutionInput) ([]byte, error) {
	return e.engine.CompleteQuery(ctx, input)
}
func (e *authorizedSearchEngine) PrepareAnalysis(ctx context.Context, input port.BoundExecutionRequest) (port.ExecutionPlan, error) {
	plan, err := e.engine.PrepareAnalysis(ctx, input)
	if err != nil {
		return port.ExecutionPlan{}, err
	}
	return e.authority.sign(plan)
}
func (e *authorizedSearchEngine) CompleteAnalysis(ctx context.Context, input port.CompleteExecutionInput) ([]byte, error) {
	return e.engine.CompleteAnalysis(ctx, input)
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
	Statements       []LadybugStatement     `json:"statements"`
	PhysicalIndex    *port.PhysicalIndexRef `json:"physical_index,omitempty"`
	PhysicalEvidence []LadybugIndexEvidence `json:"physical_evidence,omitempty"`
}
type LadybugStatement struct {
	Query      string                   `json:"query"`
	Parameters map[string]port.RawValue `json:"parameters"`
}
type LadybugIndexEvidence struct {
	TableName                 string   `json:"table_name"`
	IndexName                 string   `json:"index_name"`
	IndexType                 string   `json:"index_type"`
	PropertyNames             []string `json:"property_names"`
	ContentColumns            []string `json:"content_columns"`
	PrimaryKey                string   `json:"primary_key"`
	AllowNonPrimary           bool     `json:"allow_non_primary,omitempty"`
	Relation                  bool     `json:"relation,omitempty"`
	ExpectedDocumentSetDigest string   `json:"expected_document_set_digest,omitempty"`
}
type LadybugSession interface {
	ExecutePrepared(context.Context, LadybugStatement, port.ExecutionLimits, port.RowSink) error
	ApplyIndex(context.Context, []LadybugStatement, *port.PhysicalIndexRef, []LadybugIndexEvidence, port.ExecutionLimits, port.RowSink) error
	Interrupt()
	InspectIndex(context.Context, port.PhysicalIndexRef) error
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
	if kind == port.PlanSearchIndex {
		if plan.PhysicalIndex == nil || len(plan.PhysicalEvidence) == 0 {
			return BackendExecution{}, ErrInvalidPlan
		}
		if err := d.session.ApplyIndex(ctx, plan.Statements, plan.PhysicalIndex, plan.PhysicalEvidence, limits, sink); err != nil {
			return BackendExecution{}, err
		}
		return BackendExecution{Complete: true, PhysicalIndex: plan.PhysicalIndex}, nil
	}
	for _, statement := range plan.Statements {
		if statement.Query == "" {
			return BackendExecution{}, ErrInvalidPlan
		}
		if err := d.session.ExecutePrepared(ctx, statement, limits, sink); err != nil {
			return BackendExecution{}, err
		}
	}
	return BackendExecution{Complete: true}, nil
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
