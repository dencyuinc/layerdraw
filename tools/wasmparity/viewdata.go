// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	wasmtransport "github.com/dencyuinc/layerdraw/internal/transport/wasm"
	"github.com/dencyuinc/layerdraw/tools/internal/engineoracle"
)

const (
	viewDataCorpusPath       = "tests/conformance/testdata/viewdata_conformance_v1.json"
	viewDataEndpointID       = "viewdata-conformance"
	viewDataDefaultMaxItems  = protocolcommon.CanonicalPositiveInt64("1024")
	viewDataDefaultMaxBytes  = protocolcommon.CanonicalPositiveInt64("1048576")
	viewDataLimitedMaxItems  = protocolcommon.CanonicalPositiveInt64("1")
	viewDataStateCapturedAt  = protocolcommon.Rfc3339Time("2026-01-04T00:00:00Z")
	viewDataStateUpdatedAt   = "2026-01-03T04:05:06Z"
	viewDataStateVersion     = "state-conformance-1"
	viewDataReturnedVariable = "$returned_bytes"
)

type viewDataCorpus struct {
	SchemaVersion           int                            `json:"schema_version"`
	EngineReleaseVariable   string                         `json:"engine_release_variable"`
	OperationLimits         engineprotocol.WorkbenchLimits `json:"operation_limits"`
	RequiredShapes          []string                       `json:"required_shapes"`
	RequiredProjectionModes []string                       `json:"required_projection_modes"`
	RequiredSourceKinds     []string                       `json:"required_source_kinds"`
	RequiredStatePolicies   []string                       `json:"required_state_policies"`
	RequiredFailureClasses  []string                       `json:"required_failure_classes"`
	Normalization           []string                       `json:"normalization"`
	Documents               []viewDataCorpusDocument       `json:"documents"`
	Cases                   []viewDataCorpusCase           `json:"cases"`
}

type viewDataCorpusDocument struct {
	ID    string                      `json:"id"`
	Input engineprotocol.CompileInput `json:"input"`
	Blobs []viewDataCorpusBlob        `json:"blobs"`
}

type viewDataCorpusBlob struct {
	BlobID      string `json:"blob_id"`
	MediaType   string `json:"media_type"`
	BytesBase64 string `json:"bytes_base64"`
	bytes       []byte
}

type viewDataCorpusCase struct {
	Name      string                         `json:"name"`
	Execution string                         `json:"execution"`
	Features  []string                       `json:"features"`
	Source    viewDataCorpusSource           `json:"source"`
	View      string                         `json:"view_address"`
	Limits    engineprotocol.WorkbenchLimits `json:"limits"`
	Mutation  string                         `json:"mutation,omitempty"`
	Repeat    int                            `json:"repeat"`
	Expected  viewDataCorpusExpected         `json:"expected"`
}

type viewDataCorpusSource struct {
	Kind           string                           `json:"kind"`
	Document       string                           `json:"document,omitempty"`
	QueryAddress   string                           `json:"query_address,omitempty"`
	Arguments      map[string]semantic.RecipeScalar `json:"arguments,omitempty"`
	StateSnapshot  *semantic.StateQuerySnapshot     `json:"state_snapshot,omitempty"`
	RecipeDocument string                           `json:"recipe_document,omitempty"`
	BeforeDocument string                           `json:"before_document,omitempty"`
	AfterDocument  string                           `json:"after_document,omitempty"`
}

type viewDataCorpusExpected struct {
	Outcome            string          `json:"outcome"`
	FailureClass       string          `json:"failure_class,omitempty"`
	PublishesViewData  bool            `json:"publishes_view_data"`
	NormalizedResponse json.RawMessage `json:"normalized_response,omitempty"`
}

type viewDataDocumentSpec struct {
	id    string
	entry string
	files map[string][]byte
}

type openedViewDataDocument struct {
	ID         string
	Generation engineprotocol.DocumentGeneration
	Handle     engineprotocol.DocumentHandle
}

func generateViewDataCorpus(output string) error {
	corpus, err := buildViewDataCorpus()
	if err != nil {
		return err
	}
	data, err := canonicalViewDataCorpus(corpus)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	return os.WriteFile(output, data, 0o644)
}

func canonicalViewDataCorpus(corpus viewDataCorpus) ([]byte, error) {
	data, err := json.Marshal(corpus)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func buildViewDataCorpus() (viewDataCorpus, error) {
	specs := viewDataDocumentSpecs()
	documents := make([]viewDataCorpusDocument, 0, len(specs))
	documentByID := make(map[string]viewDataCorpusDocument, len(specs))
	for _, spec := range specs {
		document := buildViewDataDocument(spec)
		documents = append(documents, document)
		documentByID[document.ID] = document
	}

	authority, negotiated, err := viewDataAuthority()
	if err != nil {
		return viewDataCorpus{}, err
	}
	var stateSpec viewDataDocumentSpec
	for _, spec := range specs {
		if spec.id == "state" {
			stateSpec = spec
			break
		}
	}
	stateSnapshot, err := viewDataStateSnapshot(stateSpec)
	if err != nil {
		return viewDataCorpus{}, err
	}
	inputs := viewDataCaseInputs(stateSnapshot)
	cases := make([]viewDataCorpusCase, 0, len(inputs))
	for _, input := range inputs {
		switch input.Execution {
		case "materialize":
			normalized, outcome, err := runViewDataCase(context.Background(), authority, negotiated, documentByID, input, false)
			if err != nil {
				return viewDataCorpus{}, fmt.Errorf("build %s: %w", input.Name, err)
			}
			input.Expected = viewDataCorpusExpected{
				Outcome: outcome, FailureClass: viewDataFailureClass(normalized, outcome),
				PublishesViewData: viewDataResponsePublishes(normalized), NormalizedResponse: normalized,
			}
			for repeat := 1; repeat < input.Repeat; repeat++ {
				repeated, repeatedOutcome, repeatErr := runViewDataCase(context.Background(), authority, negotiated, documentByID, input, repeat%2 == 1)
				if repeatErr != nil {
					return viewDataCorpus{}, fmt.Errorf("repeat %s: %w", input.Name, repeatErr)
				}
				if outcome != repeatedOutcome || !bytes.Equal(normalized, repeated) {
					return viewDataCorpus{}, fmt.Errorf("%s is not deterministic", input.Name)
				}
			}
		case "cancel":
			normalized, outcome, err := runCancelledViewDataCase(authority, input.Name)
			if err != nil {
				return viewDataCorpus{}, fmt.Errorf("build %s: %w", input.Name, err)
			}
			input.Expected = viewDataCorpusExpected{Outcome: outcome, FailureClass: "cancelled", PublishesViewData: false, NormalizedResponse: normalized}
		case "malformed_wire":
			normalized, outcome, err := runMalformedViewDataCase(context.Background(), authority, negotiated, input.Name)
			if err != nil {
				return viewDataCorpus{}, fmt.Errorf("build %s: %w", input.Name, err)
			}
			input.Expected = viewDataCorpusExpected{Outcome: outcome, FailureClass: "malformed_wire", PublishesViewData: false, NormalizedResponse: normalized}
		default:
			return viewDataCorpus{}, fmt.Errorf("unsupported execution %q", input.Execution)
		}
		cases = append(cases, input)
	}

	result := viewDataCorpus{
		SchemaVersion:         1,
		EngineReleaseVariable: engineReleaseVariable,
		OperationLimits:       viewDataDefaultLimits(),
		RequiredShapes:        []string{"context", "diagram", "diff", "flow", "matrix", "table", "tree"},
		RequiredProjectionModes: []string{
			"composed_badge", "composed_edge", "composed_hide", "composed_nest", "composed_overlay",
			"diagram_edge", "diagram_hide", "table_automatic", "table_relation", "table_relation_rows",
			"matrix_endpoints", "tree_endpoints", "flow_endpoints", "context_facts",
		},
		RequiredSourceKinds:    []string{"diff", "query"},
		RequiredStatePolicies:  []string{"none", "optional", "required"},
		RequiredFailureClasses: []string{"cancelled", "invalid_input", "limit_exceeded", "malformed_wire"},
		Normalization: []string{
			"engine_release is replaced by $engine_release because each artifact reports its linked release",
			"endpoint-owned document handles and revision IDs are replaced by document-scoped variables",
			"returned_bytes is replaced by $returned_bytes because endpoint-owned handle lengths are transport-specific",
			"hard cancellation and local client cancellation compare as the closed cancelled failure class",
			"malformed wire and typed-client input rejection compare as the closed malformed_wire failure class",
		},
		Documents: documents,
		Cases:     cases,
	}
	if err := validateViewDataCoverage(result); err != nil {
		return viewDataCorpus{}, err
	}
	return result, nil
}

func validateViewDataCoverage(corpus viewDataCorpus) error {
	covered := map[string]bool{}
	documents := map[string]bool{}
	for _, document := range corpus.Documents {
		if document.ID == "" || documents[document.ID] || len(document.Input.ProjectSourceTree) == 0 || len(document.Blobs) == 0 {
			return fmt.Errorf("invalid ViewData corpus document %q", document.ID)
		}
		documents[document.ID] = true
	}
	for _, test := range corpus.Cases {
		if test.Name == "" || test.Repeat < 1 || len(test.Features) == 0 {
			return fmt.Errorf("incomplete ViewData corpus case %q", test.Name)
		}
		if test.Execution != "materialize" && test.Execution != "cancel" && test.Execution != "malformed_wire" {
			return fmt.Errorf("%s has unsupported execution %q", test.Name, test.Execution)
		}
		if test.Source.Kind != "query" && test.Source.Kind != "diff" {
			return fmt.Errorf("%s has unsupported source %q", test.Name, test.Source.Kind)
		}
		if len(test.Expected.NormalizedResponse) == 0 || test.Expected.Outcome == "" {
			return fmt.Errorf("%s has no normalized oracle", test.Name)
		}
		if test.Expected.Outcome != "success" && test.Expected.PublishesViewData {
			return fmt.Errorf("%s publishes partial ViewData", test.Name)
		}
		failureCase := slices.Contains(test.Features, "invalid_input") || slices.Contains(test.Features, "limit_exceeded") ||
			test.Execution == "cancel" || test.Execution == "malformed_wire"
		if !failureCase && (test.Expected.Outcome != "success" || !test.Expected.PublishesViewData) {
			return fmt.Errorf("%s does not publish its required ViewData", test.Name)
		}
		if failureCase && test.Expected.FailureClass == "" {
			return fmt.Errorf("%s has no closed failure class", test.Name)
		}
		for _, feature := range test.Features {
			covered[feature] = true
		}
	}
	for _, required := range [][]string{
		corpus.RequiredShapes, corpus.RequiredProjectionModes, corpus.RequiredSourceKinds,
		corpus.RequiredStatePolicies, corpus.RequiredFailureClasses,
	} {
		for _, feature := range required {
			if !covered[feature] {
				return fmt.Errorf("ViewData corpus does not cover %q", feature)
			}
		}
	}
	return nil
}

func viewDataAuthority() (*endpoint.CompilerEndpoint, *endpoint.NegotiatedContext, error) {
	authority, err := endpoint.NewCompilerEndpoint(endpoint.CompilerEndpointConfig{
		EngineRelease: "0.0.0", SourceRevision: "unknown",
		ReleaseManifestDigest: releaseManifestDigest, EndpointInstanceID: viewDataEndpointID,
		Transports: []string{endpoint.TransportInProcess}, Limits: wasmtransport.BrowserCompilerLimitPolicy(),
	})
	if err != nil {
		return nil, nil, err
	}
	handshake := engineprotocol.HandshakeRequestEnvelope{
		Operation: engineprotocol.HandshakeRequestEnvelopeOperationValue,
		Payload: protocolcommon.HandshakeRequest{
			ClientRelease: "0.0.0", OptionalCapabilities: []protocolcommon.CapabilityID{},
			Protocols: []protocolcommon.ProtocolOffer{{Name: endpoint.ProtocolName, SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{
				Version: endpoint.ProtocolVersion, SchemaDigest: protocolcommon.Digest(engineprotocol.SchemaDigest),
			}}}},
			RequiredCapabilities: []protocolcommon.CapabilityID{
				endpoint.OperationOpenDocument, endpoint.OperationExecuteQuery, endpoint.OperationMaterializeView, endpoint.OperationCloseDocument,
			},
		},
		Protocol: engineProtocolRef(), RequestID: "viewdata-handshake",
	}
	response, negotiated, err := authority.Descriptor.Negotiate(context.Background(), handshake)
	if err != nil || negotiated == nil || response.Outcome != protocolcommon.OutcomeSuccess {
		return nil, nil, fmt.Errorf("negotiate ViewData authority: outcome=%s err=%w", response.Outcome, err)
	}
	return authority, negotiated, nil
}

func buildViewDataDocument(spec viewDataDocumentSpec) viewDataCorpusDocument {
	compiled := projectCase("viewdata-"+spec.id, spec.entry, spec.files, []string{"project"}, nil, engineprotocol.ResourceLimits{})
	blobs := make([]viewDataCorpusBlob, len(compiled.blobs))
	for index, blob := range compiled.blobs {
		blobs[index] = viewDataCorpusBlob{
			BlobID: blob.BlobID, MediaType: blob.MediaType,
			BytesBase64: base64.StdEncoding.EncodeToString(blob.bytes), bytes: slices.Clone(blob.bytes),
		}
	}
	return viewDataCorpusDocument{ID: spec.id, Input: compiled.request.Payload, Blobs: blobs}
}

func runViewDataCase(
	ctx context.Context,
	authority *endpoint.CompilerEndpoint,
	negotiated *endpoint.NegotiatedContext,
	documents map[string]viewDataCorpusDocument,
	test viewDataCorpusCase,
	reverse bool,
) (json.RawMessage, string, error) {
	opened := map[string]openedViewDataDocument{}
	open := func(id string) (openedViewDataDocument, error) {
		if value, ok := opened[id]; ok {
			return value, nil
		}
		document, ok := documents[id]
		if !ok {
			return openedViewDataDocument{}, fmt.Errorf("unknown document %q", id)
		}
		if reverse {
			document.Input.ProjectSourceTree = slices.Clone(document.Input.ProjectSourceTree)
			slices.Reverse(document.Input.ProjectSourceTree)
			document.Blobs = slices.Clone(document.Blobs)
			slices.Reverse(document.Blobs)
		}
		request := engineprotocol.OpenDocumentRequestEnvelope{
			Operation: engineprotocol.OpenDocumentRequestEnvelopeOperationValue,
			Payload:   engineprotocol.OpenDocumentInput{CompileInput: document.Input, RequestedLimits: viewDataDefaultLimits()},
			Protocol:  engineProtocolRef(), RequestID: "viewdata-" + test.Name + "-open-" + id,
		}
		control, err := engineprotocol.EncodeOpenDocumentRequestEnvelope(request)
		if err != nil {
			return openedViewDataDocument{}, err
		}
		dispatched, err := dispatchViewData(ctx, authority, negotiated, endpoint.OperationOpenDocument, control, viewDataBlobSource(document.Blobs))
		if err != nil {
			return openedViewDataDocument{}, err
		}
		response, err := engineprotocol.DecodeOpenDocumentResponseEnvelope(dispatched.Control)
		if err != nil || response.Outcome != protocolcommon.OutcomeSuccess || response.Payload == nil {
			return openedViewDataDocument{}, fmt.Errorf("open %s outcome=%s err=%w", id, response.Outcome, err)
		}
		value := openedViewDataDocument{ID: id, Generation: response.Payload.DocumentGeneration, Handle: response.Payload.DocumentHandle}
		opened[id] = value
		return value, nil
	}

	var materialize engineprotocol.MaterializeViewInput
	switch test.Source.Kind {
	case "query":
		document, err := open(test.Source.Document)
		if err != nil {
			return nil, "", err
		}
		arguments := cloneRecipeScalars(test.Source.Arguments, reverse)
		queryRequest := engineprotocol.ExecuteQueryRequestEnvelope{
			Operation: engineprotocol.ExecuteQueryRequestEnvelopeOperationValue,
			Payload: engineprotocol.ExecuteQueryInput{
				Arguments: arguments, DocumentGeneration: document.Generation, Limits: viewDataDefaultLimits(),
				QueryAddress: semantic.QueryAddress(test.Source.QueryAddress),
			},
			Protocol: engineProtocolRef(), RequestID: "viewdata-" + test.Name + "-query",
		}
		queryControl, err := engineprotocol.EncodeExecuteQueryRequestEnvelope(queryRequest)
		if err != nil {
			return nil, "", err
		}
		queryDispatch, err := dispatchViewData(ctx, authority, negotiated, endpoint.OperationExecuteQuery, queryControl, &memoryBlobSource{})
		if err != nil {
			return nil, "", err
		}
		queryResponse, err := engineprotocol.DecodeExecuteQueryResponseEnvelope(queryDispatch.Control)
		if err != nil || queryResponse.Outcome != protocolcommon.OutcomeSuccess || queryResponse.Payload == nil {
			return nil, "", fmt.Errorf("execute %s outcome=%s failure=%+v diagnostics=%+v err=%v", test.Name, queryResponse.Outcome, queryResponse.Failure, queryResponse.Diagnostics, err)
		}
		queryResult := queryResponse.Payload.Result
		if test.Mutation == "mismatched_query" {
			queryResult.QueryAddress = "ldl:project:p:query:missing"
		}
		materialize = engineprotocol.MaterializeViewInput{
			Kind: "query", Limits: test.Limits, ViewAddress: semantic.ViewAddress(test.View),
			Query: &engineprotocol.MaterializeQueryViewInput{
				DocumentGeneration: document.Generation, QueryResult: queryResult, StateSnapshot: test.Source.StateSnapshot,
			},
		}
	case "diff":
		recipe, err := open(test.Source.RecipeDocument)
		if err != nil {
			return nil, "", err
		}
		before, err := open(test.Source.BeforeDocument)
		if err != nil {
			return nil, "", err
		}
		after, err := open(test.Source.AfterDocument)
		if err != nil {
			return nil, "", err
		}
		materialize = engineprotocol.MaterializeViewInput{
			Kind: "diff", Limits: test.Limits, ViewAddress: semantic.ViewAddress(test.View),
			Diff: &engineprotocol.MaterializeDiffViewInput{
				RecipeGeneration: recipe.Generation, BeforeGeneration: before.Generation, AfterGeneration: after.Generation,
			},
		}
	default:
		return nil, "", fmt.Errorf("unsupported source kind %q", test.Source.Kind)
	}

	request := engineprotocol.MaterializeViewRequestEnvelope{
		Operation: engineprotocol.MaterializeViewRequestEnvelopeOperationValue,
		Payload:   materialize, Protocol: engineProtocolRef(), RequestID: "viewdata-" + test.Name + "-materialize",
	}
	control, err := engineprotocol.EncodeMaterializeViewRequestEnvelope(request)
	if err != nil {
		return nil, "", err
	}
	dispatched, err := dispatchViewData(ctx, authority, negotiated, endpoint.OperationMaterializeView, control, &memoryBlobSource{})
	if err != nil {
		return nil, "", err
	}
	normalized, err := normalizeViewDataResponse(dispatched.Control, opened)
	if err != nil {
		return nil, "", err
	}
	for _, document := range opened {
		if err := closeViewDataDocument(context.Background(), authority, negotiated, document, test.Name); err != nil {
			return nil, "", err
		}
	}
	return normalized, string(dispatched.Outcome), nil
}

func runCancelledViewDataCase(authority *endpoint.CompilerEndpoint, name string) (json.RawMessage, string, error) {
	response, err := authority.Dispatcher.DispatchCancellationResponse(endpoint.OperationMaterializeView, "viewdata-"+name+"-materialize", "0.0.0")
	if err != nil {
		return nil, "", err
	}
	normalized, err := normalizeViewDataResponse(response.Control, map[string]openedViewDataDocument{})
	if err != nil {
		return nil, "", err
	}
	return normalized, normalizedViewDataOutcome(normalized), nil
}

func runMalformedViewDataCase(ctx context.Context, authority *endpoint.CompilerEndpoint, negotiated *endpoint.NegotiatedContext, name string) (json.RawMessage, string, error) {
	control := []byte(fmt.Sprintf(`{"operation":"engine.materialize_view","payload":{},"protocol":{"name":"engine","version":"1.0"},"request_id":"viewdata-%s-materialize"}`, name))
	dispatched, err := dispatchViewData(ctx, authority, negotiated, endpoint.OperationMaterializeView, control, &memoryBlobSource{})
	if err != nil {
		return nil, "", err
	}
	normalized, err := normalizeViewDataResponse(dispatched.Control, map[string]openedViewDataDocument{})
	if err != nil {
		return nil, "", err
	}
	return normalized, normalizedViewDataOutcome(normalized), nil
}

func normalizedViewDataOutcome(response json.RawMessage) string {
	var value struct {
		Outcome string `json:"outcome"`
	}
	if json.Unmarshal(response, &value) != nil {
		return ""
	}
	return value.Outcome
}

func dispatchViewData(
	ctx context.Context,
	authority *endpoint.CompilerEndpoint,
	negotiated *endpoint.NegotiatedContext,
	operation string,
	control []byte,
	source endpoint.BlobSource,
) (endpoint.DispatchResponse, error) {
	plan, terminal, err := authority.Dispatcher.PrepareDispatch(ctx, negotiated, operation, control)
	if err != nil {
		return endpoint.DispatchResponse{}, err
	}
	if terminal != nil {
		return *terminal, nil
	}
	if plan == nil {
		return endpoint.DispatchResponse{}, fmt.Errorf("%s produced no plan", operation)
	}
	sink := &memoryBlobSink{}
	response, err := plan.ExecuteDispatch(ctx, source, sink)
	if err != nil {
		return endpoint.DispatchResponse{}, err
	}
	if len(sink.blobs) != 0 {
		return endpoint.DispatchResponse{}, fmt.Errorf("%s published unexpected blobs", operation)
	}
	return response, nil
}

func closeViewDataDocument(
	ctx context.Context,
	authority *endpoint.CompilerEndpoint,
	negotiated *endpoint.NegotiatedContext,
	document openedViewDataDocument,
	caseName string,
) error {
	request := engineprotocol.CloseDocumentRequestEnvelope{
		Operation: engineprotocol.CloseDocumentRequestEnvelopeOperationValue,
		Payload:   engineprotocol.CloseDocumentInput{DocumentGeneration: document.Generation, DocumentHandle: document.Handle},
		Protocol:  engineProtocolRef(), RequestID: "viewdata-" + caseName + "-close-" + document.ID,
	}
	control, err := engineprotocol.EncodeCloseDocumentRequestEnvelope(request)
	if err != nil {
		return err
	}
	response, err := dispatchViewData(ctx, authority, negotiated, endpoint.OperationCloseDocument, control, &memoryBlobSource{})
	if err != nil || response.Outcome != protocolcommon.OutcomeSuccess {
		return fmt.Errorf("close %s outcome=%s err=%w", document.ID, response.Outcome, err)
	}
	return nil
}

func viewDataBlobSource(blobs []viewDataCorpusBlob) *memoryBlobSource {
	values := make([]parityInputBlob, len(blobs))
	for index, blob := range blobs {
		values[index] = parityInputBlob{BlobID: blob.BlobID, MediaType: blob.MediaType, bytes: slices.Clone(blob.bytes)}
	}
	return &memoryBlobSource{blobs: values}
}

func normalizeViewDataResponse(control []byte, documents map[string]openedViewDataDocument) (json.RawMessage, error) {
	var value any
	if err := json.Unmarshal(control, &value); err != nil {
		return nil, err
	}
	handles := map[string]string{}
	for id, document := range documents {
		handles[document.Handle.Value] = id
	}
	value = normalizeViewDataValue(value, handles)
	return json.Marshal(value)
}

func normalizeViewDataValue(value any, handles map[string]string) any {
	switch current := value.(type) {
	case []any:
		for index := range current {
			current[index] = normalizeViewDataValue(current[index], handles)
		}
	case map[string]any:
		for key, child := range current {
			switch key {
			case "engine_release":
				current[key] = engineReleaseVariable
			case "returned_bytes":
				current[key] = viewDataReturnedVariable
			case "endpoint_instance_id":
				current[key] = "$endpoint"
			default:
				current[key] = normalizeViewDataValue(child, handles)
			}
		}
	case string:
		if id, ok := handles[current]; ok {
			return "$document:" + id
		}
		for handle, id := range handles {
			if strings.HasPrefix(current, "workbench:"+handle+":") {
				return "$revision:" + id
			}
		}
	}
	return value
}

func viewDataResponsePublishes(response json.RawMessage) bool {
	var value struct {
		Outcome string `json:"outcome"`
		Payload *struct {
			ViewData json.RawMessage `json:"view_data"`
		} `json:"payload"`
	}
	if json.Unmarshal(response, &value) != nil {
		return false
	}
	return value.Outcome == "success" && value.Payload != nil && len(value.Payload.ViewData) != 0
}

func viewDataFailureClass(response json.RawMessage, outcome string) string {
	if outcome == "rejected" {
		return "invalid_input"
	}
	var value struct {
		Failure *struct {
			Code string `json:"code"`
		} `json:"failure"`
	}
	if json.Unmarshal(response, &value) == nil && value.Failure != nil {
		if value.Failure.Code == "engine.workbench.limit_exceeded" {
			return "limit_exceeded"
		}
		if value.Failure.Code == "engine.workbench.cancelled" {
			return "cancelled"
		}
	}
	return ""
}

func cloneRecipeScalars(values map[string]semantic.RecipeScalar, reverse bool) map[string]semantic.RecipeScalar {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if reverse {
		slices.Reverse(keys)
	}
	result := make(map[string]semantic.RecipeScalar, len(values))
	for _, key := range keys {
		result[key] = values[key]
	}
	return result
}

func viewDataDefaultLimits() engineprotocol.WorkbenchLimits {
	return engineprotocol.WorkbenchLimits{MaxItems: viewDataDefaultMaxItems, MaxOutputBytes: viewDataDefaultMaxBytes}
}

func viewDataLimitedItems() engineprotocol.WorkbenchLimits {
	return engineprotocol.WorkbenchLimits{MaxItems: viewDataLimitedMaxItems, MaxOutputBytes: viewDataDefaultMaxBytes}
}

func viewDataStateSnapshot(spec viewDataDocumentSpec) (*semantic.StateQuerySnapshot, error) {
	seed, err := engineoracle.CompileStateSeed(spec.entry, spec.files)
	if err != nil {
		return nil, err
	}
	rowAddress := "ldl:project:p:entity:alpha:row:primary"
	ownHash := seed.SubjectHashes[rowAddress]
	if ownHash == "" {
		return nil, fmt.Errorf("state corpus row hash is absent")
	}
	updated := viewDataStateUpdatedAt
	return &semantic.StateQuerySnapshot{
		Format: semantic.StateQuerySnapshotFormatValue, SchemaVersion: 1,
		DefinitionProjectAddress: semantic.ProjectRootAddress(seed.ProjectAddress),
		DefinitionHash:           protocolcommon.Digest(seed.DefinitionHash), GraphHash: protocolcommon.Digest(seed.GraphHash),
		StateVersion: viewDataStateVersion, CapturedAt: viewDataStateCapturedAt,
		InaccessibleFieldPaths: []semantic.StateFieldPath{},
		Subjects: []semantic.StateQuerySubject{{
			SubjectAddress: semantic.StableAddress(rowAddress), OwnSubjectHash: protocolcommon.Digest(ownHash),
			Fields: map[string]semantic.RecipeScalar{
				"system.updated_at": {Kind: "datetime", StringValue: &updated},
			},
			RedactedFieldPaths: []semantic.StateFieldPath{},
		}},
	}, nil
}

func viewDataCaseInputs(state *semantic.StateQuerySnapshot) []viewDataCorpusCase {
	environment := "prod"
	arguments := map[string]semantic.RecipeScalar{
		"ldl:project:p:query:prod_scope:parameter:environment": {Kind: "enum", StringValue: &environment},
	}
	query := func(name, view string, features ...string) viewDataCorpusCase {
		return viewDataCorpusCase{
			Name: name, Execution: "materialize", Features: append([]string{"query", "none"}, features...),
			Source: viewDataCorpusSource{Kind: "query", Document: "structural", QueryAddress: "ldl:project:p:query:prod_scope", Arguments: cloneRecipeScalars(arguments, false)},
			View:   "ldl:project:p:view:" + view, Limits: viewDataDefaultLimits(), Repeat: 2,
		}
	}
	cases := []viewDataCorpusCase{
		query("diagram", "topology", "diagram", "diagram_edge"),
		query("table_automatic", "table_automatic", "table", "table_automatic"),
		query("table_relation", "table_relation", "table", "table_relation"),
		query("table_relation_rows", "table_relation_rows", "table", "table_relation_rows"),
		query("matrix", "matrix", "matrix", "matrix_endpoints"),
		query("tree", "tree", "tree", "tree_endpoints"),
		query("flow", "flow", "flow", "flow_endpoints"),
		query("context", "context", "context", "context_facts"),
		{
			Name: "state_optional_absent", Execution: "materialize", Features: []string{"query", "table", "optional"},
			Source: viewDataCorpusSource{Kind: "query", Document: "state", QueryAddress: "ldl:project:p:query:prod_scope", Arguments: map[string]semantic.RecipeScalar{}},
			View:   "ldl:project:p:view:state_optional", Limits: viewDataDefaultLimits(), Repeat: 2,
		},
		{
			Name: "state_optional_present", Execution: "materialize", Features: []string{"query", "table", "optional"},
			Source: viewDataCorpusSource{Kind: "query", Document: "state", QueryAddress: "ldl:project:p:query:prod_scope", Arguments: map[string]semantic.RecipeScalar{}, StateSnapshot: state},
			View:   "ldl:project:p:view:state_optional", Limits: viewDataDefaultLimits(), Repeat: 2,
		},
		{
			Name: "state_required_present", Execution: "materialize", Features: []string{"query", "table", "required"},
			Source: viewDataCorpusSource{Kind: "query", Document: "state", QueryAddress: "ldl:project:p:query:prod_scope", Arguments: map[string]semantic.RecipeScalar{}, StateSnapshot: state},
			View:   "ldl:project:p:view:state_required", Limits: viewDataDefaultLimits(), Repeat: 2,
		},
		{
			Name: "state_required_missing", Execution: "materialize", Features: []string{"query", "table", "required", "invalid_input"},
			Source: viewDataCorpusSource{Kind: "query", Document: "state", QueryAddress: "ldl:project:p:query:prod_scope", Arguments: map[string]semantic.RecipeScalar{}},
			View:   "ldl:project:p:view:state_required", Limits: viewDataDefaultLimits(), Repeat: 2,
		},
		{
			Name: "composed_diagram", Execution: "materialize",
			Features: []string{"query", "diagram", "none", "composed_badge", "composed_edge", "composed_hide", "composed_nest", "composed_overlay", "diagram_edge"},
			Source:   viewDataCorpusSource{Kind: "query", Document: "composed", QueryAddress: "ldl:project:p:query:scope", Arguments: map[string]semantic.RecipeScalar{}},
			View:     "ldl:project:p:view:composed", Limits: viewDataDefaultLimits(), Repeat: 2,
		},
		{
			Name: "hidden_diagram_projection", Execution: "materialize", Features: []string{"query", "diagram", "none", "diagram_hide"},
			Source: viewDataCorpusSource{Kind: "query", Document: "composed", QueryAddress: "ldl:project:p:query:scope", Arguments: map[string]semantic.RecipeScalar{}},
			View:   "ldl:project:p:view:hidden", Limits: viewDataDefaultLimits(), Repeat: 2,
		},
		{
			Name: "definition_diff", Execution: "materialize", Features: []string{"diff", "none"},
			Source: viewDataCorpusSource{Kind: "diff", RecipeDocument: "diff_after", BeforeDocument: "diff_before", AfterDocument: "diff_after"},
			View:   "ldl:project:p:view:changes", Limits: viewDataDefaultLimits(), Repeat: 2,
		},
		{
			Name: "mismatched_query_result", Execution: "materialize", Features: []string{"query", "invalid_input", "none"},
			Source: viewDataCorpusSource{Kind: "query", Document: "structural", QueryAddress: "ldl:project:p:query:prod_scope", Arguments: cloneRecipeScalars(arguments, false)},
			View:   "ldl:project:p:view:context", Limits: viewDataDefaultLimits(), Mutation: "mismatched_query", Repeat: 2,
		},
		{
			Name: "materialization_item_limit", Execution: "materialize", Features: []string{"query", "limit_exceeded", "none"},
			Source: viewDataCorpusSource{Kind: "query", Document: "structural", QueryAddress: "ldl:project:p:query:prod_scope", Arguments: cloneRecipeScalars(arguments, false)},
			View:   "ldl:project:p:view:context", Limits: viewDataLimitedItems(), Repeat: 2,
		},
		{
			Name: "deterministic_source_map_locale", Execution: "materialize", Features: []string{"query", "context", "none", "deterministic_source_order", "deterministic_map_order", "locale_independent"},
			Source: viewDataCorpusSource{Kind: "query", Document: "deterministic", QueryAddress: "ldl:project:p:query:scope", Arguments: map[string]semantic.RecipeScalar{}},
			View:   "ldl:project:p:view:context", Limits: viewDataDefaultLimits(), Repeat: 4,
		},
		{
			Name: "cancelled_materialization", Execution: "cancel", Features: []string{"query", "cancelled", "none"},
			Source: viewDataCorpusSource{Kind: "query", Document: "structural", QueryAddress: "ldl:project:p:query:prod_scope", Arguments: cloneRecipeScalars(arguments, false)},
			View:   "ldl:project:p:view:context", Limits: viewDataDefaultLimits(), Repeat: 1,
		},
		{
			Name: "malformed_materialize_wire", Execution: "malformed_wire", Features: []string{"query", "malformed_wire", "invalid_input", "none"},
			Source: viewDataCorpusSource{Kind: "query", Document: "structural", QueryAddress: "ldl:project:p:query:prod_scope", Arguments: cloneRecipeScalars(arguments, false)},
			View:   "ldl:project:p:view:context", Limits: viewDataDefaultLimits(), Repeat: 1,
		},
	}
	return cases
}
