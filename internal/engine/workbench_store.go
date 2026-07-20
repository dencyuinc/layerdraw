// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/sourceplanner"
)

const (
	documentHandlePrefix = "document_"
	cursorPayloadVersion = byte(1)
)

type workbenchStore struct {
	mu            sync.RWMutex
	config        WorkbenchConfig
	endpointID    string
	secret        [32]byte
	documents     map[string]*workingDocument
	retainedBytes int64
	nextSequence  uint64
	initErr       error
}

type workingDocument struct {
	handle     DocumentHandle
	generation uint64
	limits     WorkbenchLimits
	sequence   uint64
	snapshot   *workingSnapshot
	preview    *retainedPreview
}

type retainedPreview struct {
	baseGeneration  uint64
	previewID       sourceplanner.PreviewID
	previewDigest   sourceplanner.Digest
	candidate       CompileInput
	sourceDiff      sourceplanner.SourceDiff
	authoringImpact sourceplanner.AuthoringImpact
	resultingHashes sourceplanner.ResultingHashes
	attachments     SourcePlannerBlobs
	retained        int64
}

type workingSnapshot struct {
	compiled      Snapshot
	input         CompileInput
	mode          CompileMode
	modules       []ModuleReadItem
	moduleBytes   map[moduleKey][]byte
	rows          map[string]materialize.AttributeRow
	rowAddresses  map[string][]string
	subjects      map[string]index.SemanticSubject
	adjacency     map[string]index.AdjacencyRecord
	entities      map[string]materialize.Entity
	relations     map[string]materialize.Relation
	searchNames   map[string]string
	sourceRanges  map[string]SourceRange
	referenceText map[string]string
	state         WorkingDocumentState
	capabilities  DocumentCapabilityState
	retained      int64
}

type moduleKey struct {
	kind        resolve.OriginKind
	packAddress string
	path        string
}

type cursorPosition struct {
	itemOffset uint64
	byteOffset uint64
}

func newWorkbenchStore(input WorkbenchConfig) *workbenchStore {
	config, ok := effectiveWorkbenchConfig(input)
	store := &workbenchStore{config: config, documents: map[string]*workingDocument{}}
	if !ok {
		store.initErr = &WorkbenchError{Code: "engine.workbench.invalid_config", Category: WorkbenchErrorInvariant}
		return store
	}
	if _, err := rand.Read(store.secret[:]); err != nil {
		store.initErr = &WorkbenchError{Code: "engine.workbench.entropy_unavailable", Category: WorkbenchErrorInvariant, cause: err}
		return store
	}
	store.endpointID = config.EndpointInstanceID
	if store.endpointID == "" {
		store.endpointID = "engine-" + hex.EncodeToString(store.secret[:12])
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`).MatchString(store.endpointID) {
		store.initErr = &WorkbenchError{Code: "engine.workbench.invalid_endpoint_instance_id", Category: WorkbenchErrorInvariant}
	}
	return store
}

func effectiveWorkbenchConfig(input WorkbenchConfig) (WorkbenchConfig, bool) {
	defaults := DefaultWorkbenchConfig()
	if input.MaxDocuments < 0 || input.MaxRetainedBytes < 0 || input.MaxItems < 0 || input.MaxOutputBytes < 0 || input.MaxDepth < 0 || input.MaxDepth > maximumWorkbenchDepth {
		return input, false
	}
	if input.MaxDocuments == 0 {
		input.MaxDocuments = defaults.MaxDocuments
	}
	if input.MaxRetainedBytes == 0 {
		input.MaxRetainedBytes = defaults.MaxRetainedBytes
	}
	if input.MaxItems == 0 {
		input.MaxItems = defaults.MaxItems
	}
	if input.MaxOutputBytes == 0 {
		input.MaxOutputBytes = defaults.MaxOutputBytes
	}
	if input.MaxDepth == 0 {
		input.MaxDepth = defaults.MaxDepth
	}
	return input, input.MaxDocuments > 0 && input.MaxRetainedBytes > 0 && input.MaxItems > 0 && input.MaxOutputBytes > 0 && input.MaxDepth > 0
}

// OpenDocument compiles and retains one immutable Working Document generation.
// Semantic diagnostics are an accepted, lossless generation; platform,
// resource, and cancellation failures do not allocate a handle.
func (e Engine) OpenDocument(ctx context.Context, input OpenDocumentInput) (OpenDocumentResult, error) {
	if err := e.checkWorkbench(ctx); err != nil {
		return OpenDocumentResult{}, err
	}
	limits, err := e.workbench.effectiveLimits(input.RequestedLimits)
	if err != nil {
		return OpenDocumentResult{}, err
	}
	snapshot, err := e.compileWorkingSnapshot(ctx, input.CompileInput)
	if err != nil {
		return OpenDocumentResult{}, err
	}
	if snapshot.retained > e.workbench.config.MaxRetainedBytes {
		return OpenDocumentResult{}, workbenchLimit("snapshot_bytes", e.workbench.config.MaxRetainedBytes, snapshot.retained)
	}
	handle, err := e.workbench.newHandle()
	if err != nil {
		return OpenDocumentResult{}, err
	}
	if err := checkWorkbenchContext(ctx); err != nil {
		return OpenDocumentResult{}, err
	}

	store := e.workbench
	store.mu.Lock()
	if err := checkWorkbenchContext(ctx); err != nil {
		store.mu.Unlock()
		return OpenDocumentResult{}, err
	}
	store.evictToMakeRoomLocked("", snapshot.retained, true)
	store.nextSequence++
	document := &workingDocument{handle: handle, generation: 1, limits: limits, sequence: store.nextSequence, snapshot: snapshot}
	store.documents[handle.Value] = document
	store.retainedBytes += snapshot.retained
	store.mu.Unlock()

	generation := DocumentGeneration{DocumentHandle: handle, Value: 1}
	return OpenDocumentResult{
		Capabilities:       snapshot.capabilities,
		DocumentGeneration: generation,
		DocumentHandle:     handle,
		EffectiveLimits:    limits,
		State:              cloneWorkingState(snapshot.state),
	}, nil
}

// ReplaceSourceTree compiles outside the store lock and atomically swaps only
// if the expected immutable generation is still current.
func (e Engine) ReplaceSourceTree(ctx context.Context, input ReplaceSourceTreeInput) (ReplaceSourceTreeResult, error) {
	document, _, err := e.acquireSnapshot(ctx, input.ExpectedGeneration)
	if err != nil {
		return ReplaceSourceTreeResult{}, err
	}
	replacement, err := e.compileWorkingSnapshot(ctx, input.CompileInput)
	if err != nil {
		return ReplaceSourceTreeResult{}, err
	}
	if replacement.retained > e.workbench.config.MaxRetainedBytes {
		return ReplaceSourceTreeResult{}, workbenchLimit("snapshot_bytes", e.workbench.config.MaxRetainedBytes, replacement.retained)
	}
	if err := checkWorkbenchContext(ctx); err != nil {
		return ReplaceSourceTreeResult{}, err
	}

	store := e.workbench
	store.mu.Lock()
	current, exists := store.documents[document.handle.Value]
	if !exists || current != document {
		store.mu.Unlock()
		return ReplaceSourceTreeResult{}, invalidHandle()
	}
	if current.generation != input.ExpectedGeneration.Value {
		store.mu.Unlock()
		return ReplaceSourceTreeResult{}, staleGeneration()
	}
	if current.generation == math.MaxUint64 {
		store.mu.Unlock()
		return ReplaceSourceTreeResult{}, &WorkbenchError{Code: "engine.workbench.generation_overflow", Category: WorkbenchErrorInvariant}
	}
	if err := checkWorkbenchContext(ctx); err != nil {
		store.mu.Unlock()
		return ReplaceSourceTreeResult{}, err
	}
	store.retainedBytes -= current.retainedBytes()
	store.evictToMakeRoomLocked(current.handle.Value, replacement.retained, false)
	current.snapshot = replacement
	current.preview = nil
	current.generation++
	store.retainedBytes += replacement.retained
	newGeneration := current.generation
	store.mu.Unlock()

	generation := DocumentGeneration{DocumentHandle: document.handle, Value: newGeneration}
	return ReplaceSourceTreeResult{
		Capabilities:       replacement.capabilities,
		DocumentGeneration: generation,
		State:              cloneWorkingState(replacement.state),
	}, nil
}

// CloseDocument is idempotent for every authentic handle created by this
// endpoint. Other operations uniformly reject closed, evicted, and unknown
// handles without disclosing which case occurred.
func (e Engine) CloseDocument(ctx context.Context, input CloseDocumentInput) (CloseDocumentResult, error) {
	if err := e.checkWorkbench(ctx); err != nil {
		return CloseDocumentResult{}, err
	}
	if input.DocumentHandle != input.DocumentGeneration.DocumentHandle || input.DocumentGeneration.Value == 0 {
		return CloseDocumentResult{}, invalidHandle()
	}
	if err := e.workbench.authenticateHandle(input.DocumentHandle); err != nil {
		return CloseDocumentResult{}, err
	}
	store := e.workbench
	store.mu.Lock()
	if err := checkWorkbenchContext(ctx); err != nil {
		store.mu.Unlock()
		return CloseDocumentResult{}, err
	}
	document, exists := store.documents[input.DocumentHandle.Value]
	if !exists {
		store.mu.Unlock()
		return CloseDocumentResult{Closed: true}, nil
	}
	if document.generation != input.DocumentGeneration.Value {
		store.mu.Unlock()
		return CloseDocumentResult{}, staleGeneration()
	}
	delete(store.documents, input.DocumentHandle.Value)
	store.retainedBytes -= document.retainedBytes()
	store.mu.Unlock()
	return CloseDocumentResult{Closed: true}, nil
}

func (e Engine) compileWorkingSnapshot(ctx context.Context, input CompileInput) (*workingSnapshot, error) {
	limits, ok := input.ResourceLimits.Effective()
	if !ok {
		return nil, workbenchLimit("resource_limits", 0, -1)
	}
	closed, err := cloneClosedInput(ctx, input, limits)
	if err != nil {
		return nil, mapCompileWorkbenchError(err)
	}
	result, err := e.Compile(ctx, closed)
	if err != nil {
		return nil, mapCompileWorkbenchError(err)
	}
	compiled := result.Snapshot()
	snapshot, err := buildWorkingSnapshot(closed, compiled)
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func buildWorkingSnapshot(input CompileInput, compiled Snapshot) (*workingSnapshot, error) {
	snapshot := &workingSnapshot{
		compiled:    compiled,
		input:       cloneWorkbenchCompileInput(input),
		mode:        input.Mode,
		moduleBytes: map[moduleKey][]byte{},
	}
	for path, value := range input.ProjectSourceTree {
		key := moduleKey{kind: resolve.OriginProject, path: path}
		snapshot.moduleBytes[key] = bytes.Clone(value)
	}
	if len(compiled.LosslessSyntaxTree.Files) != 0 {
		for _, file := range compiled.LosslessSyntaxTree.Files {
			key := moduleKey{kind: file.Origin.Kind, packAddress: file.Origin.PackAddress, path: file.ModulePath}
			snapshot.moduleBytes[key] = bytes.Clone(file.Source)
		}
	}
	for _, install := range input.ResolvedDependencies.Installs {
		parts := strings.Split(install.CanonicalID, "/")
		if len(parts) != 2 {
			continue
		}
		packAddress := "ldl:pack:" + parts[0] + ":" + parts[1]
		origin := resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: packAddress}
		for _, file := range install.Files {
			physicalPath := install.Path + "/" + file.Path
			if value, exists := input.InstalledPackTree[physicalPath]; exists {
				key := moduleKey{kind: origin.Kind, packAddress: origin.PackAddress, path: file.Path}
				snapshot.moduleBytes[key] = bytes.Clone(value)
			}
		}
		if install.ManifestPath != "" {
			key := moduleKey{kind: origin.Kind, packAddress: origin.PackAddress, path: install.ManifestPath}
			snapshot.moduleBytes[key] = bytes.Clone(install.Manifest)
		}
	}
	if compiled.SourceMap.SchemaVersion != 0 {
		for _, file := range compiled.SourceMap.Files {
			snapshot.modules = append(snapshot.modules, ModuleReadItem{
				ByteLength: int64(file.ByteLength), Digest: file.Digest,
				Module: ModuleRef{ModulePath: file.ModulePath, Origin: file.Origin},
			})
		}
	} else {
		keys := make([]moduleKey, 0, len(snapshot.moduleBytes))
		for key := range snapshot.moduleBytes {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].kind != keys[j].kind {
				return keys[i].kind == resolve.OriginProject
			}
			if keys[i].packAddress != keys[j].packAddress {
				return keys[i].packAddress < keys[j].packAddress
			}
			return keys[i].path < keys[j].path
		})
		for _, key := range keys {
			value := snapshot.moduleBytes[key]
			snapshot.modules = append(snapshot.modules, ModuleReadItem{
				ByteLength: int64(len(value)), Digest: digestBytesForWorkbench(value),
				Module: ModuleRef{ModulePath: key.path, Origin: resolve.SourceOrigin{Kind: key.kind, PackAddress: key.packAddress}},
			})
		}
	}
	snapshot.state = workingState(input.Mode, compiled)
	snapshot.capabilities = workingCapabilities(compiled)
	snapshot.rows = materializedRows(compiled)
	snapshot.rowAddresses = rowAddressesByOwner(compiled)
	snapshot.subjects = semanticSubjectsByAddress(compiled)
	snapshot.adjacency = adjacencyByEntity(compiled)
	snapshot.entities = entityMap(compiled)
	snapshot.relations = relationMap(compiled)
	snapshot.searchNames = subjectNames(compiled)
	snapshot.sourceRanges = sourceSubjectRanges(compiled)
	snapshot.referenceText = referenceTexts(compiled)
	snapshot.retained = retainedSnapshotBytes(snapshot)
	return snapshot, nil
}

func workingState(mode CompileMode, compiled Snapshot) WorkingDocumentState {
	state := WorkingDocumentState{Mode: mode, Diagnostics: resolve.CloneDiagnostics(compiled.Diagnostics), SemanticState: "unavailable"}
	if mode == CompilePack {
		state.StateKind = "pack_unavailable"
		if compiled.NormalizedPackArtifact != nil {
			address := compiled.NormalizedPackArtifact.Pack.Address
			state.PackAddress = &address
		}
	} else {
		state.StateKind = "project_unavailable"
		if compiled.NormalizedDocument != nil {
			address := compiled.NormalizedDocument.Project.Address
			state.ProjectAddress = &address
		}
	}
	if compiled.DefinitionHash == "" {
		return state
	}
	definitionHash := compiled.DefinitionHash
	state.DefinitionHash = &definitionHash
	state.SemanticState = "available"
	if compiled.GraphHash != nil {
		graphHash := *compiled.GraphHash
		state.GraphHash = &graphHash
	}
	if mode == CompilePack {
		state.StateKind = "pack_available"
	} else {
		state.StateKind = "project_available"
	}
	return state
}

func workingCapabilities(compiled Snapshot) DocumentCapabilityState {
	available := compiled.DefinitionHash != "" && compiled.SemanticIndex.SchemaVersion != 0 && compiled.SourceMap.SchemaVersion != 0
	project := available && compiled.NormalizedDocument != nil && compiled.GraphHash != nil
	return DocumentCapabilityState{
		ApplyToHandle:      project,
		ExecuteQuery:       project && compiled.TypedAST.Graph != nil,
		FindSymbols:        available,
		FindUsages:         available,
		FormatScope:        project,
		GetNeighbors:       project,
		InspectSubgraph:    project,
		ListModules:        true,
		ListReferences:     available,
		MaterializeView:    project && compiled.TypedAST.Graph != nil && len(compiled.TypedAST.Views) != 0,
		PlanExport:         project && compiled.TypedAST.Graph != nil && len(compiled.TypedAST.Views) != 0,
		OrganizeWorkspace:  project,
		PreviewFragment:    project,
		PreviewOperations:  project,
		PreviewSourcePatch: project,
		ReadDeclarations:   available,
		ReadModules:        true,
		ReadReferences:     available,
		ReadRows:           project,
		ReadScope:          available,
		ReplaceSourceTree:  true,
	}
}

func retainedSnapshotBytes(snapshot *workingSnapshot) int64 {
	// Count every retained owner rather than sampling a few public artifacts.
	// The estimator deliberately includes conservative map/runtime overhead and
	// separately owned clones even when their byte contents are equal.
	total := retainedOwnedBytes(snapshot.compiled)
	total = saturatingAdd(total, retainedOwnedBytes(snapshot.input))
	total = saturatingAdd(total, retainedOwnedBytes(snapshot.modules))
	total = saturatingAdd(total, retainedOwnedBytes(snapshot.moduleBytes))
	total = saturatingAdd(total, retainedOwnedBytes(snapshot.rows))
	total = saturatingAdd(total, retainedOwnedBytes(snapshot.rowAddresses))
	total = saturatingAdd(total, retainedOwnedBytes(snapshot.subjects))
	total = saturatingAdd(total, retainedOwnedBytes(snapshot.adjacency))
	total = saturatingAdd(total, retainedOwnedBytes(snapshot.entities))
	total = saturatingAdd(total, retainedOwnedBytes(snapshot.relations))
	total = saturatingAdd(total, retainedOwnedBytes(snapshot.searchNames))
	total = saturatingAdd(total, retainedOwnedBytes(snapshot.sourceRanges))
	total = saturatingAdd(total, retainedOwnedBytes(snapshot.referenceText))
	total = saturatingAdd(total, retainedOwnedBytes(snapshot.state))
	total = saturatingAdd(total, retainedOwnedBytes(snapshot.capabilities))
	total = saturatingAdd(total, retainedOwnedBytes(snapshot.mode))
	return total
}

func retainedOwnedBytes(value any) int64 {
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() {
		return 0
	}
	return saturatingAdd(int64(reflected.Type().Size()), retainedDynamicBytes(reflected, map[uintptr]bool{}))
}

func retainedDynamicBytes(value reflect.Value, seen map[uintptr]bool) int64 {
	if !value.IsValid() {
		return 0
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return 0
		}
		element := value.Elem()
		return saturatingAdd(int64(element.Type().Size()), retainedDynamicBytes(element, seen))
	case reflect.Pointer:
		if value.IsNil() {
			return 0
		}
		pointer := value.Pointer()
		if seen[pointer] {
			return 0
		}
		seen[pointer] = true
		element := value.Elem()
		return saturatingAdd(int64(element.Type().Size()), retainedDynamicBytes(element, seen))
	case reflect.String:
		return int64(value.Len())
	case reflect.Slice:
		if value.IsNil() {
			return 0
		}
		total := saturatingMultiply(int64(value.Cap()), int64(value.Type().Elem().Size()))
		for index := 0; index < value.Len(); index++ {
			total = saturatingAdd(total, retainedDynamicBytes(value.Index(index), seen))
		}
		return total
	case reflect.Map:
		if value.IsNil() {
			return 0
		}
		entryBytes := saturatingAdd(int64(value.Type().Key().Size()), int64(value.Type().Elem().Size()))
		total := saturatingAdd(128, saturatingMultiply(int64(value.Len()), saturatingAdd(entryBytes, 32)))
		iterator := value.MapRange()
		for iterator.Next() {
			total = saturatingAdd(total, retainedDynamicBytes(iterator.Key(), seen))
			total = saturatingAdd(total, retainedDynamicBytes(iterator.Value(), seen))
		}
		return total
	case reflect.Struct:
		var total int64
		for index := 0; index < value.NumField(); index++ {
			total = saturatingAdd(total, retainedDynamicBytes(value.Field(index), seen))
		}
		return total
	case reflect.Array:
		var total int64
		for index := 0; index < value.Len(); index++ {
			total = saturatingAdd(total, retainedDynamicBytes(value.Index(index), seen))
		}
		return total
	default:
		return 0
	}
}

func saturatingMultiply(left, right int64) int64 {
	if left <= 0 || right <= 0 {
		return 0
	}
	if left > math.MaxInt64/right {
		return math.MaxInt64
	}
	return left * right
}

func saturatingAdd(left, right int64) int64 {
	if right > 0 && left > math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
}

func (e Engine) checkWorkbench(ctx context.Context) error {
	if e.workbench == nil {
		return &WorkbenchError{Code: "engine.workbench.uninitialized", Category: WorkbenchErrorInvariant}
	}
	if e.workbench.initErr != nil {
		return e.workbench.initErr
	}
	return checkWorkbenchContext(ctx)
}

func checkWorkbenchContext(ctx context.Context) error {
	if ctx == nil {
		return &WorkbenchError{Code: "engine.workbench.nil_context", Category: WorkbenchErrorInvariant}
	}
	if err := ctx.Err(); err != nil {
		return &WorkbenchError{Code: "engine.workbench.cancelled", Category: WorkbenchErrorCancelled, cause: err}
	}
	return nil
}

func (s *workbenchStore) effectiveLimits(requested WorkbenchLimits) (WorkbenchLimits, error) {
	if requested.MaxItems <= 0 || requested.MaxOutputBytes <= 0 {
		return WorkbenchLimits{}, &WorkbenchError{Code: "engine.workbench.invalid_limits", Category: WorkbenchErrorInputInvalid}
	}
	return WorkbenchLimits{
		MaxItems:       min(requested.MaxItems, s.config.MaxItems),
		MaxOutputBytes: min(requested.MaxOutputBytes, s.config.MaxOutputBytes),
	}, nil
}

func validateReadLimits(requested, effective WorkbenchLimits) error {
	if requested.MaxItems <= 0 || requested.MaxOutputBytes <= 0 {
		return &WorkbenchError{Code: "engine.workbench.invalid_limits", Category: WorkbenchErrorInputInvalid}
	}
	if requested.MaxItems > effective.MaxItems {
		return workbenchLimit("max_items", effective.MaxItems, requested.MaxItems)
	}
	if requested.MaxOutputBytes > effective.MaxOutputBytes {
		return workbenchLimit("max_output_bytes", effective.MaxOutputBytes, requested.MaxOutputBytes)
	}
	return nil
}

func validateReadSelectionCount(count int, effective WorkbenchLimits) error {
	if int64(count) > effective.MaxItems {
		return workbenchLimit("selection_items", effective.MaxItems, int64(count))
	}
	return nil
}

func (e Engine) acquireSnapshot(ctx context.Context, generation DocumentGeneration) (*workingDocument, *workingSnapshot, error) {
	if err := e.checkWorkbench(ctx); err != nil {
		return nil, nil, err
	}
	if generation.Value == 0 {
		return nil, nil, staleGeneration()
	}
	if err := e.workbench.authenticateHandle(generation.DocumentHandle); err != nil {
		return nil, nil, err
	}
	e.workbench.mu.RLock()
	document, exists := e.workbench.documents[generation.DocumentHandle.Value]
	if !exists {
		e.workbench.mu.RUnlock()
		return nil, nil, invalidHandle()
	}
	if document.generation != generation.Value {
		e.workbench.mu.RUnlock()
		return nil, nil, staleGeneration()
	}
	snapshot := document.snapshot
	e.workbench.mu.RUnlock()
	return document, snapshot, nil
}

func (s *workbenchStore) newHandle() (DocumentHandle, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return DocumentHandle{}, &WorkbenchError{Code: "engine.workbench.entropy_unavailable", Category: WorkbenchErrorInvariant, cause: err}
	}
	mac := hmac.New(sha256.New, s.secret[:])
	mac.Write([]byte("document\x00"))
	mac.Write(entropy[:])
	value := append(entropy[:], mac.Sum(nil)[:16]...)
	return DocumentHandle{EndpointInstanceID: s.endpointID, Value: documentHandlePrefix + base64.RawURLEncoding.EncodeToString(value)}, nil
}

func (s *workbenchStore) authenticateHandle(handle DocumentHandle) error {
	if handle.EndpointInstanceID != s.endpointID || !strings.HasPrefix(handle.Value, documentHandlePrefix) {
		return invalidHandle()
	}
	encoded := strings.TrimPrefix(handle.Value, documentHandlePrefix)
	value, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(value) != 32 {
		return invalidHandle()
	}
	mac := hmac.New(sha256.New, s.secret[:])
	mac.Write([]byte("document\x00"))
	mac.Write(value[:16])
	if !hmac.Equal(value[16:], mac.Sum(nil)[:16]) {
		return invalidHandle()
	}
	return nil
}

func (s *workbenchStore) evictToMakeRoomLocked(protected string, required int64, reserveDocument bool) {
	for (reserveDocument && len(s.documents) >= s.config.MaxDocuments) || s.retainedBytes > s.config.MaxRetainedBytes-required {
		var candidate *workingDocument
		for _, document := range s.documents {
			if document.handle.Value == protected {
				continue
			}
			if candidate == nil || document.sequence < candidate.sequence || (document.sequence == candidate.sequence && document.handle.Value < candidate.handle.Value) {
				candidate = document
			}
		}
		if candidate == nil {
			return
		}
		delete(s.documents, candidate.handle.Value)
		s.retainedBytes -= candidate.retainedBytes()
	}
}

func (d *workingDocument) retainedBytes() int64 {
	if d == nil {
		return 0
	}
	total := int64(0)
	if d.snapshot != nil {
		total = saturatingAdd(total, d.snapshot.retained)
	}
	if d.preview != nil {
		total = saturatingAdd(total, d.preview.retained)
	}
	return total
}

func (s *workbenchStore) encodeCursor(prefix string, generation DocumentGeneration, requestDigest [32]byte, position cursorPosition) Cursor {
	payload := make([]byte, 41)
	payload[0] = cursorPayloadVersion
	binary.BigEndian.PutUint64(payload[1:9], position.itemOffset)
	binary.BigEndian.PutUint64(payload[9:17], position.byteOffset)
	copy(payload[17:33], requestDigest[:16])
	binary.BigEndian.PutUint64(payload[33:41], generation.Value)
	mac := hmac.New(sha256.New, s.secret[:])
	mac.Write([]byte(prefix))
	mac.Write([]byte{0})
	mac.Write([]byte(generation.DocumentHandle.Value))
	mac.Write([]byte{0})
	mac.Write(payload)
	encoded := append(payload, mac.Sum(nil)[:16]...)
	return Cursor{DocumentGeneration: generation, Value: prefix + base64.RawURLEncoding.EncodeToString(encoded)}
}

func (s *workbenchStore) decodeCursor(cursor *Cursor, prefix string, generation DocumentGeneration, requestDigest [32]byte) (cursorPosition, error) {
	if cursor == nil {
		return cursorPosition{}, nil
	}
	if cursor.DocumentGeneration != generation || !strings.HasPrefix(cursor.Value, prefix) {
		return cursorPosition{}, invalidCursor()
	}
	encoded := strings.TrimPrefix(cursor.Value, prefix)
	value, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(value) != 57 {
		return cursorPosition{}, invalidCursor()
	}
	payload := value[:41]
	if payload[0] != cursorPayloadVersion || binary.BigEndian.Uint64(payload[33:41]) != generation.Value || !bytes.Equal(payload[17:33], requestDigest[:16]) {
		return cursorPosition{}, invalidCursor()
	}
	mac := hmac.New(sha256.New, s.secret[:])
	mac.Write([]byte(prefix))
	mac.Write([]byte{0})
	mac.Write([]byte(generation.DocumentHandle.Value))
	mac.Write([]byte{0})
	mac.Write(payload)
	if !hmac.Equal(value[41:], mac.Sum(nil)[:16]) {
		return cursorPosition{}, invalidCursor()
	}
	return cursorPosition{itemOffset: binary.BigEndian.Uint64(payload[1:9]), byteOffset: binary.BigEndian.Uint64(payload[9:17])}, nil
}

func requestDigest(value any) [32]byte {
	encoded, _ := json.Marshal(value)
	return sha256.Sum256(encoded)
}

func cloneWorkingState(input WorkingDocumentState) WorkingDocumentState {
	output := input
	output.Diagnostics = resolve.CloneDiagnostics(input.Diagnostics)
	if input.DefinitionHash != nil {
		value := *input.DefinitionHash
		output.DefinitionHash = &value
	}
	if input.GraphHash != nil {
		value := *input.GraphHash
		output.GraphHash = &value
	}
	if input.ProjectAddress != nil {
		value := *input.ProjectAddress
		output.ProjectAddress = &value
	}
	if input.PackAddress != nil {
		value := *input.PackAddress
		output.PackAddress = &value
	}
	return output
}

func digestBytesForWorkbench(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func mapCompileWorkbenchError(err error) error {
	var compileError *CompileError
	if !errorsAsCompile(err, &compileError) {
		return &WorkbenchError{Code: "engine.workbench.compile_invariant", Category: WorkbenchErrorInvariant}
	}
	switch compileError.Category {
	case ErrorCategoryCancelled:
		return &WorkbenchError{Code: "engine.workbench.cancelled", Category: WorkbenchErrorCancelled, cause: err}
	case ErrorCategoryResource:
		return &WorkbenchError{Code: compileError.Code, Category: WorkbenchErrorLimitExceeded, Resource: compileError.Resource, Limit: compileError.Limit, Observed: compileError.Observed, cause: err}
	default:
		return &WorkbenchError{Code: "engine.workbench.compile_invariant", Category: WorkbenchErrorInvariant, cause: err}
	}
}

func errorsAsCompile(err error, target **CompileError) bool {
	return errors.As(err, target)
}

func invalidHandle() *WorkbenchError {
	return &WorkbenchError{Code: "engine.workbench.handle_invalid", Category: WorkbenchErrorHandleInvalid}
}

func staleGeneration() *WorkbenchError {
	return &WorkbenchError{Code: "engine.workbench.generation_stale", Category: WorkbenchErrorGenerationStale}
}

func invalidCursor() *WorkbenchError {
	return &WorkbenchError{Code: "engine.workbench.cursor_invalid", Category: WorkbenchErrorCursorInvalid}
}

func workbenchLimit(resource string, limit, observed int64) *WorkbenchError {
	return &WorkbenchError{Code: "engine.workbench.limit_exceeded", Category: WorkbenchErrorLimitExceeded, Resource: resource, Limit: limit, Observed: observed}
}

func operationDisabled(operation string) *WorkbenchError {
	return &WorkbenchError{Code: "engine.workbench.operation_disabled." + operation, Category: WorkbenchErrorOperationDisabled}
}

func validateSortedUnique(values []string, required bool) error {
	if required && len(values) == 0 {
		return &WorkbenchError{Code: "engine.workbench.empty_input", Category: WorkbenchErrorInputInvalid}
	}
	for index, value := range values {
		if value == "" || (index > 0 && (values[index-1] == value || materialize.LessStableAddress(resolve.Result{}, value, values[index-1]))) {
			return &WorkbenchError{Code: "engine.workbench.noncanonical_input", Category: WorkbenchErrorInputInvalid}
		}
	}
	return nil
}

func validateSortedUniqueKinds(values []SemanticSubjectKind) error {
	for index, value := range values {
		if !validSemanticSubjectKind(value) || (index > 0 && values[index-1] >= value) {
			return &WorkbenchError{Code: "engine.workbench.noncanonical_input", Category: WorkbenchErrorInputInvalid}
		}
	}
	return nil
}

func validateCanonicalModules(values []ModuleRef) error {
	if len(values) == 0 {
		return &WorkbenchError{Code: "engine.workbench.empty_input", Category: WorkbenchErrorInputInvalid}
	}
	for index, value := range values {
		if !isCanonicalWorkbenchSourcePath(value.ModulePath) || value.Origin.Kind != resolve.OriginProject && value.Origin.Kind != resolve.OriginPack || value.Origin.Kind == resolve.OriginProject && value.Origin.PackAddress != "" || value.Origin.Kind == resolve.OriginPack && value.Origin.PackAddress == "" {
			return &WorkbenchError{Code: "engine.workbench.noncanonical_input", Category: WorkbenchErrorInputInvalid}
		}
		if index > 0 && !lessModuleRef(values[index-1], value) {
			return &WorkbenchError{Code: "engine.workbench.noncanonical_input", Category: WorkbenchErrorInputInvalid}
		}
	}
	return nil
}

func isCanonicalWorkbenchSourcePath(value string) bool {
	if value == "" || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\\x00") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func lessModuleRef(left, right ModuleRef) bool {
	if left.Origin.Kind != right.Origin.Kind {
		return left.Origin.Kind == resolve.OriginProject
	}
	if left.Origin.PackAddress != right.Origin.PackAddress {
		return left.Origin.PackAddress < right.Origin.PackAddress
	}
	return left.ModulePath < right.ModulePath
}

func validSemanticSubjectKind(value SemanticSubjectKind) bool {
	switch value {
	case materialize.SubjectEntity,
		materialize.SubjectEntityRow,
		materialize.SubjectEntityType,
		materialize.SubjectEntityTypeColumn,
		materialize.SubjectEntityTypeConstraint,
		materialize.SubjectLayer,
		materialize.SubjectPack,
		materialize.SubjectProject,
		materialize.SubjectQuery,
		materialize.SubjectQueryParameter,
		materialize.SubjectReference,
		materialize.SubjectRelation,
		materialize.SubjectRelationRow,
		materialize.SubjectRelationType,
		materialize.SubjectRelationTypeColumn,
		materialize.SubjectRelationTypeConstraint,
		materialize.SubjectView,
		materialize.SubjectViewExport,
		materialize.SubjectViewTableColumn:
		return true
	default:
		return false
	}
}

func (snapshot *workingSnapshot) source(rangeValue SourceRange) ([]byte, error) {
	key := moduleKey{kind: rangeValue.Origin.Kind, packAddress: rangeValue.Origin.PackAddress, path: rangeValue.ModulePath}
	value, exists := snapshot.moduleBytes[key]
	if !exists || rangeValue.StartByte < 0 || rangeValue.EndByte < rangeValue.StartByte || rangeValue.EndByte > len(value) {
		return nil, &WorkbenchError{Code: "engine.workbench.source_range_invariant", Category: WorkbenchErrorInvariant}
	}
	return value[rangeValue.StartByte:rangeValue.EndByte], nil
}

func materializedRows(snapshot Snapshot) map[string]materialize.AttributeRow {
	rows := map[string]materialize.AttributeRow{}
	if snapshot.NormalizedDocument == nil {
		return rows
	}
	for _, entity := range snapshot.NormalizedDocument.Entities {
		for _, row := range entity.Rows {
			rows[row.Address] = row
		}
	}
	for _, relation := range snapshot.NormalizedDocument.Relations {
		for _, row := range relation.Rows {
			rows[row.Address] = row
		}
	}
	return rows
}

func rowAddressesByOwner(snapshot Snapshot) map[string][]string {
	result := make(map[string][]string, len(snapshot.SemanticIndex.Rows))
	for _, rows := range snapshot.SemanticIndex.Rows {
		result[rows.OwnerAddress] = slices.Clone(rows.Addresses)
	}
	return result
}
