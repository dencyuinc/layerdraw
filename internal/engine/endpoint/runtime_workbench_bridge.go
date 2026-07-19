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
	"strconv"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

const LocalCompileInputBlobID = "local-document-compile-input"

type RuntimeEngineBridge struct {
	engine   engine.Engine
	endpoint protocolcommon.EndpointInstanceID
	mu       sync.Mutex
	next     uint64
	docs     map[string]*bridgeDocument
	latest   map[string]string
}

type bridgeDocument struct {
	input      engine.CompileInput
	snapshot   engine.Snapshot
	working    BridgeWorking
	prepared   *BridgePrepared
	preparedIn *engine.CompileInput
}

type BridgeWorking struct {
	Handle, Generation, DocumentID, RevisionID string
	DefinitionHash, GraphHash                  protocolcommon.Digest
}

type BridgePrepared struct {
	AuthoringImpact semantic.AuthoringImpact
	DefinitionHash  protocolcommon.Digest
	GraphHash       protocolcommon.Digest
	EncodedInput    []byte
}

func NewRuntimeEngineBridge(instance engine.Engine, endpointID protocolcommon.EndpointInstanceID) *RuntimeEngineBridge {
	return &RuntimeEngineBridge{engine: instance, endpoint: endpointID, docs: map[string]*bridgeDocument{}, latest: map[string]string{}}
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
	w.mu.Lock()
	doc := w.docs[working.Handle]
	if doc == nil || doc.working != working {
		w.mu.Unlock()
		return BridgePrepared{}, errors.New("stale working document")
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
	prepared := BridgePrepared{AuthoringImpact: *wire.AuthoringImpact, DefinitionHash: protocolcommon.Digest(plan.Result.DefinitionHash), GraphHash: protocolcommon.Digest(*plan.Result.GraphHash), EncodedInput: encoded}
	w.mu.Lock()
	doc = w.docs[working.Handle]
	if doc == nil || doc.working != working {
		w.mu.Unlock()
		return BridgePrepared{}, errors.New("stale working document")
	}
	doc.prepared, doc.preparedIn = &prepared, &candidate
	w.mu.Unlock()
	return prepared, nil
}

func (w *RuntimeEngineBridge) Checkpoint(ctx context.Context, working BridgeWorking, prepared BridgePrepared, revisionID string) (BridgeWorking, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	doc := w.docs[working.Handle]
	if doc == nil || doc.prepared == nil || doc.preparedIn == nil || doc.working != working || doc.prepared.DefinitionHash != prepared.DefinitionHash || doc.prepared.GraphHash != prepared.GraphHash {
		return BridgeWorking{}, errors.New("stale prepared revision")
	}
	compiled, err := w.engine.Compile(ctx, *doc.preparedIn)
	if err != nil {
		return BridgeWorking{}, err
	}
	generation, _ := strconv.ParseUint(working.Generation, 10, 64)
	doc.input, doc.snapshot = cloneCompileInput(*doc.preparedIn), compiled.Snapshot()
	doc.working = BridgeWorking{Handle: working.Handle, Generation: strconv.FormatUint(generation+1, 10), DocumentID: working.DocumentID, RevisionID: revisionID, DefinitionHash: prepared.DefinitionHash, GraphHash: prepared.GraphHash}
	doc.prepared, doc.preparedIn = nil, nil
	return doc.working, nil
}

func (w *RuntimeEngineBridge) Close(working BridgeWorking) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if doc := w.docs[working.Handle]; doc != nil && doc.working != working {
		return errors.New("stale working document")
	}
	delete(w.docs, working.Handle)
	return nil
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
func (w *RuntimeEngineBridge) Opened(documentID, revisionID string) (BridgeWorking, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	doc := w.docs[w.latest[documentID+"\x00"+revisionID]]
	if doc == nil {
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
