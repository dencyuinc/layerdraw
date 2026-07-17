// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

const maximumWorkbenchDepth int64 = 32

// WorkbenchConfig bounds the ephemeral in-process Working Document store.
// Zero fields select deterministic defaults. Negative fields are rejected by
// the first Workbench operation without affecting the closed-input compiler.
type WorkbenchConfig struct {
	EndpointInstanceID string
	MaxDocuments       int
	MaxRetainedBytes   int64
	MaxItems           int64
	MaxOutputBytes     int64
	MaxDepth           int64
}

// DefaultWorkbenchConfig returns the Engine's safe in-process defaults.
func DefaultWorkbenchConfig() WorkbenchConfig {
	return WorkbenchConfig{
		MaxDocuments:     32,
		MaxRetainedBytes: 512 << 20,
		MaxItems:         1_024,
		MaxOutputBytes:   8 << 20,
		MaxDepth:         maximumWorkbenchDepth,
	}
}

// WorkbenchLimits are explicit positive per-operation bounds. A document's
// effective limits are fixed at open and cannot be broadened by later reads.
type WorkbenchLimits struct {
	MaxItems       int64 `json:"max_items"`
	MaxOutputBytes int64 `json:"max_output_bytes"`
}

type DocumentHandle struct {
	EndpointInstanceID string `json:"endpoint_instance_id"`
	Value              string `json:"value"`
}

type DocumentGeneration struct {
	DocumentHandle DocumentHandle `json:"document_handle"`
	Value          uint64         `json:"value"`
}

type Cursor struct {
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Value              string             `json:"value"`
}

type DocumentCapabilityState struct {
	ApplyToHandle      bool `json:"apply_to_handle"`
	ExecuteQuery       bool `json:"execute_query"`
	FindSymbols        bool `json:"find_symbols"`
	FindUsages         bool `json:"find_usages"`
	FormatScope        bool `json:"format_scope"`
	GetNeighbors       bool `json:"get_neighbors"`
	InspectSubgraph    bool `json:"inspect_subgraph"`
	ListModules        bool `json:"list_modules"`
	ListReferences     bool `json:"list_references"`
	OrganizeWorkspace  bool `json:"organize_workspace"`
	PreviewFragment    bool `json:"preview_fragment"`
	PreviewOperations  bool `json:"preview_operations"`
	PreviewSourcePatch bool `json:"preview_source_patch"`
	ReadDeclarations   bool `json:"read_declarations"`
	ReadModules        bool `json:"read_modules"`
	ReadReferences     bool `json:"read_references"`
	ReadRows           bool `json:"read_rows"`
	ReadScope          bool `json:"read_scope"`
	ReplaceSourceTree  bool `json:"replace_source_tree"`
}

type WorkingDocumentState struct {
	DefinitionHash *string      `json:"definition_hash,omitempty"`
	Diagnostics    []Diagnostic `json:"diagnostics"`
	GraphHash      *string      `json:"graph_hash,omitempty"`
	Mode           CompileMode  `json:"mode"`
	PackAddress    *string      `json:"pack_address,omitempty"`
	ProjectAddress *string      `json:"project_address,omitempty"`
	SemanticState  string       `json:"semantic_state"`
	StateKind      string       `json:"state_kind"`
}

type OpenDocumentInput struct {
	CompileInput    CompileInput    `json:"compile_input"`
	RequestedLimits WorkbenchLimits `json:"requested_limits"`
}

type OpenDocumentResult struct {
	Capabilities       DocumentCapabilityState `json:"capabilities"`
	DocumentGeneration DocumentGeneration      `json:"document_generation"`
	DocumentHandle     DocumentHandle          `json:"document_handle"`
	EffectiveLimits    WorkbenchLimits         `json:"effective_limits"`
	State              WorkingDocumentState    `json:"state"`
}

type ReplaceSourceTreeInput struct {
	CompileInput       CompileInput       `json:"compile_input"`
	ExpectedGeneration DocumentGeneration `json:"expected_generation"`
}

type ReplaceSourceTreeResult struct {
	Capabilities       DocumentCapabilityState `json:"capabilities"`
	DocumentGeneration DocumentGeneration      `json:"document_generation"`
	State              WorkingDocumentState    `json:"state"`
}

type CloseDocumentInput struct {
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	DocumentHandle     DocumentHandle     `json:"document_handle"`
}

type CloseDocumentResult struct {
	Closed bool `json:"closed"`
}

type TruncationOutcome string

const (
	TruncationComplete        TruncationOutcome = "complete"
	TruncationItemLimit       TruncationOutcome = "item_limit"
	TruncationOutputByteLimit TruncationOutcome = "output_byte_limit"
)

type PageInfo struct {
	NextCursor    *Cursor           `json:"next_cursor,omitempty"`
	ReturnedBytes int64             `json:"returned_bytes"`
	ReturnedItems int64             `json:"returned_items"`
	Truncation    TruncationOutcome `json:"truncation"`
}

type ModuleRef struct {
	ModulePath string       `json:"module_path"`
	Origin     SourceOrigin `json:"origin"`
}

type ModuleReadItem struct {
	ByteLength int64     `json:"byte_length"`
	Digest     string    `json:"digest"`
	Module     ModuleRef `json:"module"`
}

type ListModulesInput struct {
	Cursor             *Cursor            `json:"cursor,omitempty"`
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Limits             WorkbenchLimits    `json:"limits"`
}

type ListModulesResult struct {
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Items              []ModuleReadItem   `json:"items"`
	Page               PageInfo           `json:"page"`
}

type FindSymbolsInput struct {
	CaseMode           string                `json:"case_mode"`
	Cursor             *Cursor               `json:"cursor,omitempty"`
	DocumentGeneration DocumentGeneration    `json:"document_generation"`
	Limits             WorkbenchLimits       `json:"limits"`
	MatchMode          string                `json:"match_mode"`
	OwnerAddresses     []string              `json:"owner_addresses,omitempty"`
	Query              string                `json:"query"`
	SubjectKinds       []SemanticSubjectKind `json:"subject_kinds,omitempty"`
}

type SymbolReadItem struct {
	Address      string              `json:"address"`
	DisplayName  string              `json:"display_name"`
	Kind         SemanticSubjectKind `json:"kind"`
	MatchedField string              `json:"matched_field"`
	MatchedValue string              `json:"matched_value"`
	SourceRange  SourceRange         `json:"source_range"`
}

type FindSymbolsResult struct {
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Items              []SymbolReadItem   `json:"items"`
	Page               PageInfo           `json:"page"`
}

// BoundedTextChunk owns an independent byte slice. Bytes are the in-process
// attachment; the endpoint mapper can turn them into the protocol BlobRef.
type BoundedTextChunk struct {
	Blob       WorkbenchBlobRef `json:"blob"`
	Bytes      []byte           `json:"-"`
	FullDigest string           `json:"full_digest"`
	Offset     int64            `json:"offset"`
	TotalBytes int64            `json:"total_bytes"`
}

// WorkbenchBlobRef is the transport-neutral control metadata paired with the
// independently owned in-process attachment bytes.
type WorkbenchBlobRef struct {
	BlobID    string `json:"blob_id"`
	Digest    string `json:"digest"`
	Lifetime  string `json:"lifetime"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
}

type ModuleContentReadItem struct {
	Module      ModuleRef        `json:"module"`
	SourceChunk BoundedTextChunk `json:"source_chunk"`
}

type ReadModulesInput struct {
	Cursor             *Cursor            `json:"cursor,omitempty"`
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Limits             WorkbenchLimits    `json:"limits"`
	Modules            []ModuleRef        `json:"modules"`
}

type ReadModulesResult struct {
	DocumentGeneration DocumentGeneration      `json:"document_generation"`
	Items              []ModuleContentReadItem `json:"items"`
	Page               PageInfo                `json:"page"`
}

type DeclarationReadItem struct {
	Address     string              `json:"address"`
	Kind        SemanticSubjectKind `json:"kind"`
	SourceChunk BoundedTextChunk    `json:"source_chunk"`
	SourceRange SourceRange         `json:"source_range"`
}

type ReadDeclarationsInput struct {
	Addresses          []string           `json:"addresses"`
	Cursor             *Cursor            `json:"cursor,omitempty"`
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Limits             WorkbenchLimits    `json:"limits"`
}

type ReadDeclarationsResult struct {
	DocumentGeneration DocumentGeneration    `json:"document_generation"`
	Items              []DeclarationReadItem `json:"items"`
	Page               PageInfo              `json:"page"`
}

type RowCell struct {
	ColumnAddress string           `json:"column_address"`
	Value         NormalizedScalar `json:"value"`
}

func (c RowCell) MarshalJSON() ([]byte, error) {
	value := map[string]any{"column_address": c.ColumnAddress}
	switch c.Value.Type {
	case definition.ScalarString, definition.ScalarEnum, definition.ScalarDate, definition.ScalarDatetime:
		value["value"] = map[string]any{"kind": "string", "string": c.Value.String}
	case definition.ScalarInteger:
		value["value"] = map[string]any{"kind": "integer", "integer": strconv.FormatInt(c.Value.Int, 10)}
	case definition.ScalarNumber:
		if math.IsNaN(c.Value.Float) || math.IsInf(c.Value.Float, 0) || math.Signbit(c.Value.Float) && c.Value.Float == 0 {
			return nil, fmt.Errorf("invalid normalized finite decimal")
		}
		value["value"] = map[string]any{"kind": "decimal", "decimal": workbenchCanonicalBinary64(c.Value.Float)}
	case definition.ScalarBoolean:
		value["value"] = map[string]any{"kind": "boolean", "boolean": c.Value.Bool}
	default:
		return nil, fmt.Errorf("unsupported normalized scalar type %q", c.Value.Type)
	}
	return json.Marshal(value)
}

func workbenchCanonicalBinary64(value float64) string {
	if value == 0 {
		return "0"
	}
	negative := math.Signbit(value)
	if negative {
		value = -value
	}
	scientific := strconv.FormatFloat(value, 'e', -1, 64)
	parts := strings.SplitN(scientific, "e", 2)
	digits := strings.ReplaceAll(parts[0], ".", "")
	exponent, _ := strconv.Atoi(parts[1])
	decimalPosition := exponent + 1
	var result string
	switch {
	case decimalPosition > 0 && decimalPosition <= 21:
		if decimalPosition >= len(digits) {
			result = digits + strings.Repeat("0", decimalPosition-len(digits))
		} else {
			result = digits[:decimalPosition] + "." + digits[decimalPosition:]
		}
	case decimalPosition <= 0 && decimalPosition > -6:
		result = "0." + strings.Repeat("0", -decimalPosition) + digits
	default:
		result = digits[:1]
		if len(digits) > 1 {
			result += "." + digits[1:]
		}
		if exponent >= 0 {
			result += "e+" + strconv.Itoa(exponent)
		} else {
			result += "e" + strconv.Itoa(exponent)
		}
	}
	if negative {
		return "-" + result
	}
	return result
}

type RowReadItem struct {
	OwnerAddress string    `json:"owner_address"`
	RowAddress   string    `json:"row_address"`
	Values       []RowCell `json:"values"`
}

type ReadRowsInput struct {
	Cursor             *Cursor            `json:"cursor,omitempty"`
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Limits             WorkbenchLimits    `json:"limits"`
	OwnerAddresses     []string           `json:"owner_addresses"`
}

type ReadRowsResult struct {
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Items              []RowReadItem      `json:"items"`
	Page               PageInfo           `json:"page"`
}

type ExecuteDocumentQueryInput struct {
	Arguments          map[string]TypedScalar `json:"arguments"`
	DocumentGeneration DocumentGeneration     `json:"document_generation"`
	Limits             WorkbenchLimits        `json:"limits"`
	QueryAddress       string                 `json:"query_address"`
}

type ExecuteDocumentQueryResult struct {
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Result             QueryResult        `json:"result"`
	ReturnedBytes      int64              `json:"returned_bytes"`
	ReturnedItems      int64              `json:"returned_items"`
}

type UsageReadItem struct {
	Range         SourceRange         `json:"range"`
	SourceAddress string              `json:"source_address"`
	TargetAddress string              `json:"target_address"`
	TargetKind    SemanticSubjectKind `json:"target_kind"`
	Via           string              `json:"via"`
}

type FindUsagesInput struct {
	Cursor             *Cursor            `json:"cursor,omitempty"`
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Limits             WorkbenchLimits    `json:"limits"`
	TargetAddresses    []string           `json:"target_addresses"`
}

type FindUsagesResult struct {
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Items              []UsageReadItem    `json:"items"`
	Page               PageInfo           `json:"page"`
}

type NeighborReadItem struct {
	Depth               int64  `json:"depth"`
	Direction           string `json:"direction"`
	EntityAddress       string `json:"entity_address"`
	RelationAddress     string `json:"relation_address"`
	SourceEntityAddress string `json:"source_entity_address"`
	TraversalIndex      uint64 `json:"traversal_index"`
}

type GetNeighborsInput struct {
	Cursor             *Cursor            `json:"cursor,omitempty"`
	Depth              int64              `json:"depth"`
	Direction          string             `json:"direction"`
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	EntityAddresses    []string           `json:"entity_addresses"`
	Limits             WorkbenchLimits    `json:"limits"`
}

type GetNeighborsResult struct {
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Items              []NeighborReadItem `json:"items"`
	Page               PageInfo           `json:"page"`
}

type SubgraphGraphFacts struct {
	EntityTypeAddress   *string  `json:"entity_type_address,omitempty"`
	FromAddress         *string  `json:"from_address,omitempty"`
	Kind                string   `json:"kind"`
	LayerAddress        *string  `json:"layer_address,omitempty"`
	RelationTypeAddress *string  `json:"relation_type_address,omitempty"`
	RowAddresses        []string `json:"row_addresses"`
	ToAddress           *string  `json:"to_address,omitempty"`
}

type SubgraphSubject struct {
	Address      string              `json:"address"`
	Kind         SemanticSubjectKind `json:"kind"`
	Module       *ModuleRef          `json:"module,omitempty"`
	OwnHash      string              `json:"own_hash"`
	OwnerAddress *string             `json:"owner_address,omitempty"`
	SubtreeHash  *string             `json:"subtree_hash,omitempty"`
}

type SubgraphAdjacency struct {
	EntityAddress string   `json:"entity_address"`
	Incoming      []string `json:"incoming"`
	Outgoing      []string `json:"outgoing"`
}

type SubgraphReadItem struct {
	Adjacency      *SubgraphAdjacency `json:"adjacency,omitempty"`
	Depth          int64              `json:"depth"`
	Facts          SubgraphGraphFacts `json:"facts"`
	Subject        SubgraphSubject    `json:"subject"`
	TraversalIndex uint64             `json:"traversal_index"`
}

type SubgraphRelationFact struct {
	FromAddress     string `json:"from_address"`
	RelationAddress string `json:"relation_address"`
	ToAddress       string `json:"to_address"`
	TypeAddress     string `json:"type_address"`
}

type InspectSubgraphInput struct {
	Cursor             *Cursor            `json:"cursor,omitempty"`
	Depth              int64              `json:"depth"`
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Limits             WorkbenchLimits    `json:"limits"`
	RootAddresses      []string           `json:"root_addresses"`
}

type InspectSubgraphResult struct {
	DocumentGeneration DocumentGeneration     `json:"document_generation"`
	Items              []SubgraphReadItem     `json:"items"`
	Page               PageInfo               `json:"page"`
	Relations          []SubgraphRelationFact `json:"relations"`
}

type ScopeReadItem struct {
	OwnerAddress string           `json:"owner_address"`
	SourceChunk  BoundedTextChunk `json:"source_chunk"`
	SourceRange  SourceRange      `json:"source_range"`
}

type ReadScopeInput struct {
	Cursor             *Cursor            `json:"cursor,omitempty"`
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Limits             WorkbenchLimits    `json:"limits"`
	OwnerAddress       string             `json:"owner_address"`
}

type ReadScopeResult struct {
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Items              []ScopeReadItem    `json:"items"`
	Page               PageInfo           `json:"page"`
}

type ReferenceSummaryReadItem struct {
	Address     string      `json:"address"`
	SourceRange SourceRange `json:"source_range"`
	TextDigest  string      `json:"text_digest"`
}

type ListReferencesInput struct {
	Cursor             *Cursor            `json:"cursor,omitempty"`
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Limits             WorkbenchLimits    `json:"limits"`
}

type ListReferencesResult struct {
	DocumentGeneration DocumentGeneration         `json:"document_generation"`
	Items              []ReferenceSummaryReadItem `json:"items"`
	Page               PageInfo                   `json:"page"`
}

type ReferenceContentReadItem struct {
	Address     string           `json:"address"`
	SourceRange SourceRange      `json:"source_range"`
	TextChunk   BoundedTextChunk `json:"text_chunk"`
}

type ReadReferencesInput struct {
	Addresses          []string           `json:"addresses"`
	Cursor             *Cursor            `json:"cursor,omitempty"`
	DocumentGeneration DocumentGeneration `json:"document_generation"`
	Limits             WorkbenchLimits    `json:"limits"`
}

type ReadReferencesResult struct {
	DocumentGeneration DocumentGeneration         `json:"document_generation"`
	Items              []ReferenceContentReadItem `json:"items"`
	Page               PageInfo                   `json:"page"`
}

type WorkbenchErrorCategory string

const (
	WorkbenchErrorCancelled         WorkbenchErrorCategory = "cancelled"
	WorkbenchErrorCursorInvalid     WorkbenchErrorCategory = "cursor_invalid"
	WorkbenchErrorGenerationStale   WorkbenchErrorCategory = "generation_stale"
	WorkbenchErrorHandleInvalid     WorkbenchErrorCategory = "handle_invalid"
	WorkbenchErrorInputInvalid      WorkbenchErrorCategory = "input_invalid"
	WorkbenchErrorInvariant         WorkbenchErrorCategory = "invariant"
	WorkbenchErrorLimitExceeded     WorkbenchErrorCategory = "limit_exceeded"
	WorkbenchErrorNotFound          WorkbenchErrorCategory = "not_found"
	WorkbenchErrorOperationDisabled WorkbenchErrorCategory = "operation_disabled"
)

type WorkbenchError struct {
	Code     string
	Category WorkbenchErrorCategory
	Resource string
	Limit    int64
	Observed int64
	cause    error
}

func (e *WorkbenchError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Resource != "" {
		return fmt.Sprintf("%s: %s observed %d exceeds limit %d", e.Code, e.Resource, e.Observed, e.Limit)
	}
	return e.Code
}

func (e *WorkbenchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func IsWorkbenchError(err error, category WorkbenchErrorCategory) bool {
	var target *WorkbenchError
	return errors.As(err, &target) && target.Category == category
}

type NormalizedScalar = materialize.Scalar
