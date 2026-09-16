// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

const LocalCompileInputBlobID = "local-document-compile-input"

type RuntimeEngineBridge struct {
	engine           runtimeBridgeEngine
	endpoint         protocolcommon.EndpointInstanceID
	mu               sync.Mutex
	next             uint64
	docs             map[string]*bridgeDocument
	latest           map[string]string
	preparedBytes    int64
	maxPreparedBytes int64
}

// This private seam uses only Engine's facade; Runtime and storage never see
// retained compiler values. It also permits operation-count regression tests.
type runtimeBridgeEngine interface {
	Compile(context.Context, engine.CompileInput) (engine.CompileResult, error)
	PlanSemanticEdits(context.Context, engine.SemanticEditPlanInput) (engine.SemanticEditPlan, error)
	ExecuteQuery(context.Context, engine.QueryExecutionInput) (engine.QueryExecutionResponse, error)
	MaterializeView(context.Context, engine.ViewMaterializationInput) engine.ViewMaterializationResponse
}

type bridgeDocument struct {
	input    engine.CompileInput
	snapshot engine.Snapshot
	working  BridgeWorking
	prepared *bridgeCandidate
}

type bridgeCandidate struct {
	requestKey    [32]byte
	reusable      bool
	publication   BridgePrepared
	input         engine.CompileInput
	snapshot      engine.Snapshot
	retainedBytes int64
}

type BridgeWorking struct {
	Handle, Generation, DocumentID, RevisionID string
	DefinitionHash, GraphHash                  protocolcommon.Digest
}

type BridgePrepared struct {
	AuthoringImpact semantic.AuthoringImpact
	DefinitionHash  protocolcommon.Digest
	GraphHash       protocolcommon.Digest
	Preview         engineprotocol.WorkbenchPreviewResult
	EncodedInput    []byte
	source          *LocalSource
}

// Source returns a facade-owned source projection, never a mutable compiler
// snapshot. LocalSource's accessors already return detached source bytes.
func (p BridgePrepared) Source() (LocalSource, bool) {
	if p.source == nil {
		return LocalSource{}, false
	}
	return *p.source, true
}

type BridgeView struct {
	Address, DisplayName, Shape string
}

func (w *RuntimeEngineBridge) Views(working BridgeWorking) ([]BridgeView, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	doc := w.docs[working.Handle]
	if doc == nil || doc.working != working {
		return nil, errors.New("stale working document")
	}
	result := make([]BridgeView, 0, len(doc.snapshot.TypedAST.Views))
	for _, item := range doc.snapshot.TypedAST.Views {
		if item.Source.Query == nil {
			continue
		}
		result = append(result, BridgeView{Address: item.Address, DisplayName: item.DisplayName, Shape: string(item.Shape.Kind)})
	}
	return result, nil
}

// Preconditions derives the full optimistic-concurrency expectation set from
// the working document's compiled snapshot (subject/subtree/child-set hashes
// and source digests). Hosts use it when a client edits against "current
// head" without holding compile evidence of its own.
func (w *RuntimeEngineBridge) Preconditions(working BridgeWorking) (engineprotocol.EngineEditPreconditions, error) {
	w.mu.Lock()
	doc := w.docs[working.Handle]
	if doc == nil || doc.working != working {
		w.mu.Unlock()
		return engineprotocol.EngineEditPreconditions{}, errors.New("stale working document")
	}
	snapshot := doc.snapshot
	w.mu.Unlock()
	pre := engineprotocol.EngineEditPreconditions{
		ExpectedSubjectHashes: []engineprotocol.ExpectedHash{},
		ExpectedSubtreeHashes: []engineprotocol.ExpectedHash{},
		ExpectedChildSets:     []engineprotocol.ExpectedChildSet{},
	}
	for _, value := range snapshot.SubjectSemanticHashes {
		pre.ExpectedSubjectHashes = append(pre.ExpectedSubjectHashes, engineprotocol.ExpectedHash{Address: semantic.StableAddress(value.Address), Hash: protocolcommon.Digest(value.Hash)})
	}
	for _, value := range snapshot.SubtreeHashes {
		pre.ExpectedSubtreeHashes = append(pre.ExpectedSubtreeHashes, engineprotocol.ExpectedHash{Address: semantic.StableAddress(value.OwnerAddress), Hash: protocolcommon.Digest(value.Hash)})
	}
	for _, value := range snapshot.ChildSetHashes {
		pre.ExpectedChildSets = append(pre.ExpectedChildSets, engineprotocol.ExpectedChildSet{OwnerAddress: semantic.StableAddress(value.OwnerAddress), ChildKind: semantic.SubjectKind(value.ChildKind), Hash: protocolcommon.Digest(value.Hash)})
	}
	sources := []engineprotocol.ExpectedSourceDigest{}
	for _, file := range snapshot.SourceMap.Files {
		origin := semantic.SourceOrigin{Kind: semantic.OriginKind(file.Origin.Kind)}
		if file.Origin.PackAddress != "" {
			value := semantic.PackRootAddress(file.Origin.PackAddress)
			origin.PackAddress = &value
		}
		sources = append(sources, engineprotocol.ExpectedSourceDigest{Module: semantic.ModuleRef{Origin: origin, ModulePath: file.ModulePath}, Digest: protocolcommon.Digest(file.Digest)})
	}
	pre.ExpectedSourceDigests = &sources
	return pre, nil
}

// Subjects exposes the Engine-compiled semantic index subjects of a working
// document (address + kind). It is a serialization of Engine output; no
// symbol semantics are computed here.
func (w *RuntimeEngineBridge) Subjects(working BridgeWorking) ([]semantic.SemanticSubject, error) {
	w.mu.Lock()
	doc := w.docs[working.Handle]
	if doc == nil || doc.working != working {
		w.mu.Unlock()
		return nil, errors.New("stale working document")
	}
	source := doc.snapshot.SemanticIndex.Subjects
	subjects := make([]semantic.SemanticSubject, 0, len(source))
	for _, subject := range source {
		subjects = append(subjects, semantic.SemanticSubject{
			Address: semantic.StableAddress(subject.Address),
			Kind:    semantic.SubjectKind(subject.Kind),
			OwnHash: protocolcommon.Digest(subject.OwnHash),
		})
	}
	w.mu.Unlock()
	return subjects, nil
}

func (w *RuntimeEngineBridge) MaterializeQueryView(ctx context.Context, working BridgeWorking, address string) (semantic.ViewData, error) {
	w.mu.Lock()
	doc := w.docs[working.Handle]
	if doc == nil || doc.working != working {
		w.mu.Unlock()
		return semantic.ViewData{}, errors.New("stale working document")
	}
	snapshot := doc.snapshot
	w.mu.Unlock()
	var recipe *engine.CompiledViewRecipe
	for index := range snapshot.TypedAST.Views {
		if snapshot.TypedAST.Views[index].Address == address {
			recipe = &snapshot.TypedAST.Views[index]
			break
		}
	}
	if recipe == nil || recipe.Source.Query == nil {
		return semantic.ViewData{}, errors.New("query-backed view is unavailable")
	}
	var queryRecipe *engine.CompiledQueryRecipe
	for index := range snapshot.TypedAST.Queries {
		if snapshot.TypedAST.Queries[index].Address == recipe.Source.Query.QueryAddress {
			queryRecipe = &snapshot.TypedAST.Queries[index]
			break
		}
	}
	if queryRecipe == nil || snapshot.TypedAST.Graph == nil {
		return semantic.ViewData{}, errors.New("view query is unavailable")
	}
	arguments := map[string]engine.TypedScalar{}
	for _, argument := range recipe.Source.Query.Arguments {
		arguments[argument.ParameterAddress] = argument.Value
	}
	queryResult, err := w.engine.ExecuteQuery(ctx, engine.QueryExecutionInput{Recipe: *queryRecipe, Graph: *snapshot.TypedAST.Graph, Definition: snapshot.QueryDefinitionIdentity(), Arguments: arguments})
	if err != nil || queryResult.Status != "ok" || queryResult.Result == nil {
		return semantic.ViewData{}, errors.New("view query failed")
	}
	materialized := w.engine.MaterializeView(ctx, engine.ViewMaterializationInput{Recipe: *recipe, Query: &engine.QueryViewMaterializationInput{RevisionID: working.RevisionID, Snapshot: snapshot, QueryResult: *queryResult.Result}})
	if materialized.Status != "ok" || materialized.Result == nil {
		return semantic.ViewData{}, errors.New("view materialization failed")
	}
	return mapViewData(ctx, *materialized.Result)
}

func NewRuntimeEngineBridge(instance engine.Engine, endpointID protocolcommon.EndpointInstanceID) *RuntimeEngineBridge {
	return &RuntimeEngineBridge{engine: instance, endpoint: endpointID, docs: map[string]*bridgeDocument{}, latest: map[string]string{}, maxPreparedBytes: engine.DefaultWorkbenchConfig().MaxRetainedBytes}
}

func (w *RuntimeEngineBridge) Open(ctx context.Context, documentID, revisionID string, definitionHash, graphHash protocolcommon.Digest, encoded []byte) (BridgeWorking, error) {
	input, err := DecodeLocalCompileInput(encoded)
	if err != nil {
		return BridgeWorking{}, err
	}
	compiled, err := w.engine.Compile(ctx, input)
	if err != nil {
		return BridgeWorking{}, err
	}
	snapshot := compiled.Snapshot()
	if len(snapshot.Diagnostics) != 0 || protocolcommon.Digest(snapshot.DefinitionHash) != definitionHash || snapshot.GraphHash == nil || protocolcommon.Digest(*snapshot.GraphHash) != graphHash {
		return BridgeWorking{}, errors.New("revision semantic identity mismatch")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.next++
	handle := fmt.Sprintf("document_local_%016x", w.next)
	working := BridgeWorking{Handle: handle, Generation: "1", DocumentID: documentID, RevisionID: revisionID, DefinitionHash: definitionHash, GraphHash: graphHash}
	w.docs[handle] = &bridgeDocument{input: input, snapshot: snapshot, working: working}
	w.latest[documentID+"\x00"+revisionID] = handle
	return working, nil
}

func (w *RuntimeEngineBridge) Preview(ctx context.Context, working BridgeWorking, batch engineprotocol.SemanticOperationBatch, preconditions engineprotocol.EngineEditPreconditions, maxOperations int64) (BridgePrepared, error) {
	if err := ctx.Err(); err != nil {
		return BridgePrepared{}, err
	}
	if maxOperations <= 0 || int64(len(batch.Operations)) > maxOperations {
		return BridgePrepared{}, errors.New("semantic operation limit exceeded")
	}
	if preconditions.DocumentGeneration.DocumentHandle.EndpointInstanceID != w.endpoint || preconditions.DocumentGeneration.DocumentHandle.Value != working.Handle || string(preconditions.DocumentGeneration.Value) != working.Generation {
		return BridgePrepared{}, errors.New("stale document generation")
	}
	request, err := json.Marshal(struct {
		Batch         engineprotocol.SemanticOperationBatch
		Preconditions engineprotocol.EngineEditPreconditions
		Limit         int64
	}{batch, preconditions, maxOperations})
	if err != nil {
		return BridgePrepared{}, err
	}
	key := sha256.Sum256(request)
	w.mu.Lock()
	doc := w.docs[working.Handle]
	if doc == nil || doc.working != working {
		w.mu.Unlock()
		return BridgePrepared{}, errors.New("stale working document")
	}
	if retained := doc.prepared; retained != nil && retained.reusable && retained.requestKey == key {
		publication := retained.publication
		w.mu.Unlock()
		return detachBridgePrepared(publication)
	}
	baseInput, baseSnapshot := cloneCompileInput(doc.input), doc.snapshot
	w.mu.Unlock()
	mapped, err := MapSemanticEditPlanInput(baseInput, baseSnapshot, preconditions, batch)
	if err != nil {
		return BridgePrepared{}, err
	}
	mapped.Limits = engine.SemanticPlanLimits{MaxItems: maxOperations, MaxOutputBytes: 64 << 20}
	plan, err := w.engine.PlanSemanticEdits(ctx, mapped)
	if err != nil {
		return BridgePrepared{}, err
	}
	if plan.Status != "valid" || plan.Result == nil || plan.AuthoringImpact == nil || plan.Result.GraphHash == nil {
		return BridgePrepared{}, errors.New("engine rejected semantic operation batch")
	}
	baseGeneration := preconditions.DocumentGeneration
	proposed := baseGeneration
	value, err := strconv.ParseUint(string(baseGeneration.Value), 10, 64)
	if err != nil || value == ^uint64(0) {
		return BridgePrepared{}, errors.New("invalid generation")
	}
	proposed.Value = protocolcommon.CanonicalUint64(strconv.FormatUint(value+1, 10))
	identity := SemanticPreviewIdentity{BaseGeneration: baseGeneration, ProposedGeneration: proposed, PreviewID: engineprotocol.PreviewID{EndpointInstanceID: w.endpoint, Value: "preview_local_" + string(plan.AuthoringImpact.ImpactDigest)[7:23]}}
	wire, _, err := MapSemanticEditPlanResult(plan, identity, mapped.Limits)
	if err != nil || wire.AuthoringImpact == nil {
		return BridgePrepared{}, fmt.Errorf("map Engine preview: %w", err)
	}
	candidate := cloneCompileInput(baseInput)
	candidate.ProjectSourceTree = cloneByteMap(plan.SourceTree)
	encoded, err := EncodeLocalCompileInput(candidate)
	if err != nil {
		return BridgePrepared{}, err
	}
	prepared := BridgePrepared{AuthoringImpact: *wire.AuthoringImpact, DefinitionHash: protocolcommon.Digest(plan.Result.DefinitionHash), GraphHash: protocolcommon.Digest(*plan.Result.GraphHash), Preview: wire, EncodedInput: encoded}
	retained, err := makeBridgeCandidate(prepared, candidate, *plan.Result)
	if err != nil {
		return BridgePrepared{}, err
	}
	retained.requestKey, retained.reusable = key, true
	if err := ctx.Err(); err != nil {
		return BridgePrepared{}, err
	}
	w.mu.Lock()
	doc = w.docs[working.Handle]
	if doc == nil || doc.working != working {
		w.mu.Unlock()
		return BridgePrepared{}, errors.New("stale working document")
	}
	if err := w.retainCandidateLocked(doc, retained); err != nil {
		w.mu.Unlock()
		return BridgePrepared{}, err
	}
	w.mu.Unlock()
	return detachBridgePrepared(retained.publication)
}

// RetainRegistryPrepared binds an Engine-produced Registry candidate to the
// same working handle/checkpoint lifecycle as semantic operation previews.
func (w *RuntimeEngineBridge) RetainRegistryPrepared(ctx context.Context, working BridgeWorking, prepared BridgePrepared) error {
	input, err := DecodeLocalCompileInput(prepared.EncodedInput)
	if err != nil {
		return err
	}
	compiled, err := w.engine.Compile(ctx, input)
	if err != nil {
		return err
	}
	snapshot := compiled.Snapshot()
	if protocolcommon.Digest(snapshot.DefinitionHash) != prepared.DefinitionHash || snapshot.GraphHash == nil || protocolcommon.Digest(*snapshot.GraphHash) != prepared.GraphHash {
		return errors.New("Registry prepared semantic identity mismatch")
	}
	retained, err := makeBridgeCandidate(prepared, input, snapshot)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	doc := w.docs[working.Handle]
	if doc == nil || doc.working != working {
		return errors.New("stale working document")
	}
	return w.retainCandidateLocked(doc, retained)
}

func (w *RuntimeEngineBridge) Checkpoint(ctx context.Context, working BridgeWorking, prepared BridgePrepared, revisionID string) (BridgeWorking, error) {
	if err := ctx.Err(); err != nil {
		return BridgeWorking{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	doc := w.docs[working.Handle]
	if doc == nil || doc.prepared == nil || doc.working != working {
		return BridgeWorking{}, errors.New("stale prepared revision")
	}
	retained := doc.prepared
	if retained.publication.DefinitionHash != prepared.DefinitionHash || retained.publication.GraphHash != prepared.GraphHash || !bytes.Equal(retained.publication.EncodedInput, prepared.EncodedInput) || !reflect.DeepEqual(retained.publication.AuthoringImpact, prepared.AuthoringImpact) {
		return BridgeWorking{}, errors.New("stale prepared revision")
	}
	generation, err := strconv.ParseUint(working.Generation, 10, 64)
	if err != nil || generation == math.MaxUint64 || revisionID == "" {
		return BridgeWorking{}, errors.New("invalid checkpoint generation")
	}
	doc.input, doc.snapshot = retained.input, retained.snapshot
	doc.working = BridgeWorking{Handle: working.Handle, Generation: strconv.FormatUint(generation+1, 10), DocumentID: working.DocumentID, RevisionID: revisionID, DefinitionHash: prepared.DefinitionHash, GraphHash: prepared.GraphHash}
	w.preparedBytes -= retained.retainedBytes
	doc.prepared = nil
	if key := working.DocumentID + "\x00" + working.RevisionID; w.latest[key] == working.Handle {
		delete(w.latest, key)
	}
	w.latest[working.DocumentID+"\x00"+revisionID] = working.Handle
	return doc.working, nil
}

func (w *RuntimeEngineBridge) Close(working BridgeWorking) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if doc := w.docs[working.Handle]; doc != nil && doc.working != working {
		return errors.New("stale working document")
	}
	if doc := w.docs[working.Handle]; doc != nil && doc.prepared != nil {
		w.preparedBytes -= doc.prepared.retainedBytes
	}
	if key := working.DocumentID + "\x00" + working.RevisionID; w.latest[key] == working.Handle {
		delete(w.latest, key)
	}
	delete(w.docs, working.Handle)
	return nil
}

func makeBridgeCandidate(prepared BridgePrepared, input engine.CompileInput, snapshot engine.Snapshot) (*bridgeCandidate, error) {
	publication, err := detachBridgePrepared(prepared)
	if err != nil {
		return nil, err
	}
	source, err := sourceFromSnapshot(input, snapshot)
	if err != nil {
		return nil, err
	}
	publication.source = &source
	// Budget the new retained snapshot and both private source representations.
	// This is done once when admitting a candidate, never on a cache hit.
	metadata, err := json.Marshal(struct {
		Snapshot engine.Snapshot
		Preview  engineprotocol.WorkbenchPreviewResult
	}{snapshot, publication.Preview})
	if err != nil {
		return nil, err
	}
	return &bridgeCandidate{publication: publication, input: input, snapshot: snapshot, retainedBytes: int64(len(metadata)) + 3*int64(len(publication.EncodedInput))}, nil
}

func (w *RuntimeEngineBridge) retainCandidateLocked(doc *bridgeDocument, candidate *bridgeCandidate) error {
	previous := int64(0)
	if doc.prepared != nil {
		previous = doc.prepared.retainedBytes
	}
	if candidate.retainedBytes > w.maxPreparedBytes || w.preparedBytes-previous > w.maxPreparedBytes-candidate.retainedBytes {
		return errors.New("prepared revision retention limit exceeded")
	}
	w.preparedBytes += candidate.retainedBytes - previous
	doc.prepared = candidate
	return nil
}

// Copy only the public projection at this ownership boundary. The retained AST
// and source input never undergo a serialization round-trip for copying.
func detachBridgePrepared(input BridgePrepared) (BridgePrepared, error) {
	encoded, source := input.EncodedInput, input.source
	input.EncodedInput = nil
	data, err := json.Marshal(input)
	if err != nil {
		return BridgePrepared{}, err
	}
	var result BridgePrepared
	if err := json.Unmarshal(data, &result); err != nil {
		return BridgePrepared{}, err
	}
	result.EncodedInput, result.source = bytes.Clone(encoded), source
	return result, nil
}
func (w *RuntimeEngineBridge) Working(handle string) (BridgeWorking, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	doc := w.docs[handle]
	if doc == nil {
		return BridgeWorking{}, false
	}
	return doc.working, true
}

// SourceDigest returns the canonical semantic-source digest for the currently
// checkpointed Engine input. It deliberately excludes a merely prepared
// candidate, so hosts cannot advance an external baseline before publication.
func (w *RuntimeEngineBridge) SourceDigest(handle string) (protocolcommon.Digest, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	doc := w.docs[handle]
	if doc == nil {
		return "", false
	}
	encoded, err := EncodeLocalCompileInput(doc.input)
	if err != nil {
		return "", false
	}
	return LocalCompileInputRef(encoded).Digest, true
}

func (w *RuntimeEngineBridge) SearchEncodedInput(handle string) ([]byte, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	doc := w.docs[handle]
	if doc == nil {
		return nil, false
	}
	encoded, err := EncodeLocalCompileInput(doc.input)
	return encoded, err == nil
}

func (w *RuntimeEngineBridge) Opened(documentID, revisionID string) (BridgeWorking, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	doc := w.docs[w.latest[documentID+"\x00"+revisionID]]
	if doc == nil || doc.working.DocumentID != documentID || doc.working.RevisionID != revisionID {
		return BridgeWorking{}, false
	}
	return doc.working, true
}

func EncodeLocalCompileInput(input engine.CompileInput) ([]byte, error) { return json.Marshal(input) }
func DecodeLocalCompileInput(data []byte) (engine.CompileInput, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var input engine.CompileInput
	err := decoder.Decode(&input)
	return input, err
}
func LocalCompileInputRef(data []byte) protocolcommon.BlobRef {
	sum := sha256.Sum256(data)
	return protocolcommon.BlobRef{BlobID: LocalCompileInputBlobID, Digest: protocolcommon.Digest("sha256:" + hex.EncodeToString(sum[:])), Lifetime: protocolcommon.BlobLifetimePersistent, MediaType: "application/vnd.layerdraw.compile-input+json", Size: protocolcommon.CanonicalUint64(strconv.Itoa(len(data)))}
}

func cloneCompileInput(input engine.CompileInput) engine.CompileInput {
	data, _ := json.Marshal(input)
	var result engine.CompileInput
	_ = json.Unmarshal(data, &result)
	return result
}
func cloneByteMap(input map[string][]byte) map[string][]byte {
	result := make(map[string][]byte, len(input))
	for key, value := range input {
		result[key] = bytes.Clone(value)
	}
	return result
}
func digestBytes(data []byte) protocolcommon.Digest {
	sum := sha256.Sum256(data)
	return protocolcommon.Digest("sha256:" + hex.EncodeToString(sum[:]))
}
