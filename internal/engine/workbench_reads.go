// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

const (
	listModulesCursorPrefix      = "list_modules_cursor_"
	readModulesCursorPrefix      = "read_modules_cursor_"
	findSymbolsCursorPrefix      = "find_symbols_cursor_"
	inspectSubgraphCursorPrefix  = "inspect_subgraph_cursor_"
	readDeclarationsCursorPrefix = "read_declarations_cursor_"
	readRowsCursorPrefix         = "read_rows_cursor_"
	getNeighborsCursorPrefix     = "get_neighbors_cursor_"
	findUsagesCursorPrefix       = "find_usages_cursor_"
	readScopeCursorPrefix        = "read_scope_cursor_"
	listReferencesCursorPrefix   = "list_references_cursor_"
	readReferencesCursorPrefix   = "read_references_cursor_"
	workbenchReadIteratedPage    = "iterated_page"
	workbenchReadTextItem        = "text_item"
	workbenchReadSubgraphPage    = "subgraph_page"
	workbenchReadNeighborNode    = "neighbor_node"
	workbenchReadNeighborEdge    = "neighbor_edge"
	workbenchReadSubgraphNode    = "subgraph_node"
	workbenchReadSubgraphEdge    = "subgraph_edge"
)

type pageBuilder[T any] func([]T, PageInfo) any
type valueIterator[T any] func(func(T) bool) error

func checkWorkbenchReadBoundary(ctx context.Context, boundary, address string) error {
	if hook, ok := ctx.(interface{ onWorkbenchReadBoundary(string, string) }); ok {
		hook.onWorkbenchReadBoundary(boundary, address)
	}
	return checkWorkbenchContext(ctx)
}

func paginateIteratedValues[T any](ctx context.Context, store *workbenchStore, generation DocumentGeneration, limits WorkbenchLimits, cursor *Cursor, prefix string, digest [32]byte, iterate valueIterator[T], build pageBuilder[T]) ([]T, PageInfo, error) {
	position, err := store.decodeCursor(cursor, prefix, generation, digest)
	if err != nil {
		return nil, PageInfo{}, err
	}
	if position.byteOffset != 0 {
		return nil, PageInfo{}, invalidCursor()
	}
	start := position.itemOffset
	buffer := make([]T, 0, limits.MaxItems+1)
	var matched uint64
	completed := true
	err = iterate(func(value T) bool {
		if matched < start {
			matched++
			return true
		}
		matched++
		buffer = append(buffer, value)
		if int64(len(buffer)) > limits.MaxItems {
			completed = false
			return false
		}
		return true
	})
	if err != nil {
		return nil, PageInfo{}, err
	}
	if matched < start {
		return nil, PageInfo{}, invalidCursor()
	}
	items := make([]T, 0, min(int(limits.MaxItems), len(buffer)))
	var acceptedPage PageInfo
	for index := 0; index < len(buffer) && int64(len(items)) < limits.MaxItems; index++ {
		if err := checkWorkbenchReadBoundary(ctx, workbenchReadIteratedPage, ""); err != nil {
			return nil, PageInfo{}, err
		}
		candidate := append(slicesClone(items), buffer[index])
		hasMore := index+1 < len(buffer) || !completed
		page := PageInfo{ReturnedItems: int64(len(candidate)), Truncation: TruncationComplete}
		if hasMore {
			page.Truncation = TruncationOutputByteLimit
			if int64(len(candidate)) == limits.MaxItems {
				page.Truncation = TruncationItemLimit
			}
			next := store.encodeCursor(prefix, generation, digest, cursorPosition{itemOffset: start + uint64(len(candidate))})
			page.NextCursor = &next
		}
		measured, measureErr := measureLogicalResult(build(candidate, page), 0)
		if measureErr != nil {
			return nil, PageInfo{}, measureErr
		}
		if measured > limits.MaxOutputBytes {
			if len(items) == 0 {
				return nil, PageInfo{}, workbenchLimit("max_output_bytes", limits.MaxOutputBytes, measured)
			}
			return items, acceptedPage, nil
		}
		page.ReturnedBytes = measured
		items, acceptedPage = candidate, page
	}
	if len(items) == 0 {
		page := PageInfo{Truncation: TruncationComplete}
		measured, measureErr := measureLogicalResult(build(items, page), 0)
		if measureErr != nil {
			return nil, PageInfo{}, measureErr
		}
		if measured > limits.MaxOutputBytes {
			return nil, PageInfo{}, workbenchLimit("max_output_bytes", limits.MaxOutputBytes, measured)
		}
		page.ReturnedBytes = measured
		return items, page, nil
	}
	return items, acceptedPage, nil
}

func iterateSlice[T any](values []T) valueIterator[T] {
	return func(yield func(T) bool) error {
		for _, value := range values {
			if !yield(value) {
				break
			}
		}
		return nil
	}
}

func paginateValues[T any](ctx context.Context, store *workbenchStore, generation DocumentGeneration, limits WorkbenchLimits, cursor *Cursor, prefix string, digest [32]byte, values []T, build pageBuilder[T]) ([]T, PageInfo, error) {
	return paginateIteratedValues(ctx, store, generation, limits, cursor, prefix, digest, iterateSlice(values), build)
}

type textSource struct {
	address    string
	kind       SemanticSubjectKind
	module     ModuleRef
	rangeValue SourceRange
	bytes      []byte
}

func paginateTextValues[T any](ctx context.Context, store *workbenchStore, generation DocumentGeneration, limits WorkbenchLimits, cursor *Cursor, prefix string, digest [32]byte, sources []textSource, makeItem func(textSource, BoundedTextChunk) T, attachmentSize func(T) int64, build pageBuilder[T]) ([]T, PageInfo, error) {
	position, err := store.decodeCursor(cursor, prefix, generation, digest)
	if err != nil {
		return nil, PageInfo{}, err
	}
	if position.itemOffset > uint64(len(sources)) {
		return nil, PageInfo{}, invalidCursor()
	}
	itemIndex, byteOffset := int(position.itemOffset), int(position.byteOffset)
	if itemIndex == len(sources) && byteOffset != 0 {
		return nil, PageInfo{}, invalidCursor()
	}
	if itemIndex < len(sources) && (byteOffset < 0 || byteOffset > len(sources[itemIndex].bytes) || !utf8.Valid(sources[itemIndex].bytes[byteOffset:])) {
		return nil, PageInfo{}, invalidCursor()
	}
	items := make([]T, 0, paginationCapacity(limits.MaxItems, len(sources)-itemIndex))
	var acceptedPage PageInfo
	for itemIndex < len(sources) && int64(len(items)) < limits.MaxItems {
		source := sources[itemIndex]
		if err := checkWorkbenchReadBoundary(ctx, workbenchReadTextItem, source.address); err != nil {
			return nil, PageInfo{}, err
		}
		remaining := source.bytes[byteOffset:]
		fullChunk := boundedChunk(source.bytes, byteOffset, len(remaining))
		fullItem := makeItem(source, fullChunk)
		candidate := append(slicesClone(items), fullItem)
		nextItem := itemIndex + 1
		page := textPage(store, generation, digest, prefix, candidate, limits, nextItem, 0, len(sources))
		measured, measureErr := measureLogicalResult(build(candidate, page), attachmentBytes(candidate, attachmentSize))
		if measureErr != nil {
			return nil, PageInfo{}, measureErr
		}
		if measured <= limits.MaxOutputBytes {
			page.ReturnedBytes = measured
			items, acceptedPage = candidate, page
			itemIndex, byteOffset = nextItem, 0
			continue
		}
		if len(items) != 0 {
			return items, acceptedPage, nil
		}

		chunkLength := largestTextChunk(remaining, func(length int) bool {
			chunk := boundedChunk(source.bytes, byteOffset, length)
			item := makeItem(source, chunk)
			continuationItem, continuationByte := itemIndex, byteOffset+length
			if continuationByte == len(source.bytes) {
				continuationItem, continuationByte = itemIndex+1, 0
			}
			candidatePage := textPage(store, generation, digest, prefix, []T{item}, limits, continuationItem, continuationByte, len(sources))
			candidateBytes, err := measureLogicalResult(build([]T{item}, candidatePage), attachmentSize(item))
			return err == nil && candidateBytes <= limits.MaxOutputBytes
		})
		if chunkLength == 0 {
			return nil, PageInfo{}, workbenchLimit("max_output_bytes", limits.MaxOutputBytes, measured)
		}
		chunk := boundedChunk(source.bytes, byteOffset, chunkLength)
		item := makeItem(source, chunk)
		continuationItem, continuationByte := itemIndex, byteOffset+chunkLength
		if continuationByte == len(source.bytes) {
			continuationItem, continuationByte = itemIndex+1, 0
		}
		page = textPage(store, generation, digest, prefix, []T{item}, limits, continuationItem, continuationByte, len(sources))
		measured, measureErr = measureLogicalResult(build([]T{item}, page), attachmentSize(item))
		if measureErr != nil {
			return nil, PageInfo{}, measureErr
		}
		page.ReturnedBytes = measured
		return []T{item}, page, nil
	}
	if len(items) == 0 {
		page := PageInfo{Truncation: TruncationComplete}
		measured, err := measureLogicalResult(build(items, page), 0)
		if err != nil {
			return nil, PageInfo{}, err
		}
		if measured > limits.MaxOutputBytes {
			return nil, PageInfo{}, workbenchLimit("max_output_bytes", limits.MaxOutputBytes, measured)
		}
		page.ReturnedBytes = measured
		return items, page, nil
	}
	return items, acceptedPage, nil
}

func textPage[T any](store *workbenchStore, generation DocumentGeneration, digest [32]byte, prefix string, items []T, limits WorkbenchLimits, nextItem, nextByte, total int) PageInfo {
	page := PageInfo{ReturnedItems: int64(len(items)), Truncation: TruncationComplete}
	if nextItem < total || nextByte != 0 {
		page.Truncation = TruncationOutputByteLimit
		if int64(len(items)) == limits.MaxItems && nextByte == 0 {
			page.Truncation = TruncationItemLimit
		}
		next := store.encodeCursor(prefix, generation, digest, cursorPosition{itemOffset: uint64(nextItem), byteOffset: uint64(nextByte)})
		page.NextCursor = &next
	}
	return page
}

func largestTextChunk(value []byte, fits func(int) bool) int {
	low, high, best := 1, len(value), 0
	for low <= high {
		mid := low + (high-low)/2
		end := mid
		for end > 0 && !utf8.Valid(value[:end]) {
			end--
		}
		if end == 0 {
			low = mid + 1
			continue
		}
		if fits(end) {
			best = end
			low = mid + 1
		} else {
			high = end - 1
		}
	}
	return best
}

func boundedChunk(full []byte, offset, length int) BoundedTextChunk {
	chunk := full[offset : offset+length]
	digest := digestBytesForWorkbench(chunk)
	return BoundedTextChunk{
		Blob: WorkbenchBlobRef{
			BlobID:    "workbench-" + strings.TrimPrefix(digest, "sha256:") + "-" + strconv.Itoa(offset),
			Digest:    digest,
			Lifetime:  "request",
			MediaType: "text/plain; charset=utf-8",
			Size:      int64(length),
		},
		Bytes:      append([]byte(nil), chunk...),
		FullDigest: digestBytesForWorkbench(full),
		Offset:     int64(offset),
		TotalBytes: int64(len(full)),
	}
}

func attachmentBytes[T any](items []T, size func(T) int64) int64 {
	var total int64
	for _, item := range items {
		total = saturatingAdd(total, size(item))
	}
	return total
}

func measureLogicalResult(value any, attachments int64) (int64, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return 0, &WorkbenchError{Code: "engine.workbench.output_invariant", Category: WorkbenchErrorInvariant, cause: err}
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return 0, &WorkbenchError{Code: "engine.workbench.output_invariant", Category: WorkbenchErrorInvariant, cause: err}
	}
	normalized, err := normalizeLogicalWireValue(decoded)
	if err != nil {
		return 0, &WorkbenchError{Code: "engine.workbench.output_invariant", Category: WorkbenchErrorInvariant, cause: err}
	}
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(normalized); err != nil {
		return 0, &WorkbenchError{Code: "engine.workbench.output_invariant", Category: WorkbenchErrorInvariant, cause: err}
	}
	return saturatingAdd(int64(canonical.Len()-1), attachments), nil
}

func normalizeLogicalWireValue(value any) (any, error) {
	switch typed := value.(type) {
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			normalized, err := normalizeLogicalWireValue(item)
			if err != nil {
				return nil, err
			}
			result[index] = normalized
		}
		return result, nil
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			wireKey := key
			switch key {
			case "Origin":
				wireKey = "origin"
			case "ModulePath":
				wireKey = "module_path"
			case "StartByte":
				wireKey = "start_byte"
			case "EndByte":
				wireKey = "end_byte"
			case "Kind":
				wireKey = "kind"
			case "PackAddress":
				wireKey = "pack_address"
			}
			if wireKey == "pack_address" && item == "" {
				continue
			}
			normalized, err := normalizeLogicalWireValue(item)
			if err != nil {
				return nil, err
			}
			result[wireKey] = normalized
		}
		for _, key := range []string{"returned_bytes", "returned_items", "byte_length", "offset", "total_bytes", "size", "start_byte", "end_byte", "traversal_index"} {
			if number, ok := result[key].(json.Number); ok {
				result[key] = number.String()
			}
		}
		if _, generation := result["document_handle"]; generation {
			if number, ok := result["value"].(json.Number); ok {
				result["value"] = number.String()
			}
		}
		return result, nil
	case json.Number:
		if _, err := strconv.ParseFloat(typed.String(), 64); err != nil {
			return nil, fmt.Errorf("invalid JSON number %q", typed)
		}
		return typed, nil
	default:
		return value, nil
	}
}

func slicesClone[T any](values []T) []T {
	return append([]T(nil), values...)
}

func paginationCapacity(maxItems int64, available int) int {
	if maxItems < int64(available) {
		return int(maxItems)
	}
	return available
}

func (e Engine) ListModules(ctx context.Context, input ListModulesInput) (ListModulesResult, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return ListModulesResult{}, err
	}
	if err := validateReadLimits(input.Limits, document.limits); err != nil {
		return ListModulesResult{}, err
	}
	digest := requestDigest(struct{}{})
	items, page, err := paginateIteratedValues(ctx, e.workbench, input.DocumentGeneration, input.Limits, input.Cursor, listModulesCursorPrefix, digest, iterateSlice(snapshot.modules), func(items []ModuleReadItem, page PageInfo) any {
		return ListModulesResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}
	})
	return ListModulesResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}, err
}

// ReadModules returns selected retained source bytes for every immutable
// generation, including generations whose semantic materialization failed.
func (e Engine) ReadModules(ctx context.Context, input ReadModulesInput) (ReadModulesResult, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return ReadModulesResult{}, err
	}
	if !snapshot.capabilities.ReadModules {
		return ReadModulesResult{}, operationDisabled("read_modules")
	}
	if err := validateReadLimits(input.Limits, document.limits); err != nil {
		return ReadModulesResult{}, err
	}
	if int64(len(input.Modules)) > document.limits.MaxItems {
		return ReadModulesResult{}, workbenchLimit("max_items", document.limits.MaxItems, int64(len(input.Modules)))
	}
	if err := validateCanonicalModules(input.Modules); err != nil {
		return ReadModulesResult{}, err
	}
	sources := make([]textSource, 0, len(input.Modules))
	for _, module := range input.Modules {
		key := moduleKey{kind: module.Origin.Kind, packAddress: module.Origin.PackAddress, path: module.ModulePath}
		value, exists := snapshot.moduleBytes[key]
		if !exists {
			return ReadModulesResult{}, notFound()
		}
		sources = append(sources, textSource{module: module, bytes: value})
	}
	digest := requestDigest(input.Modules)
	items, page, err := paginateTextValues(ctx, e.workbench, input.DocumentGeneration, input.Limits, input.Cursor, readModulesCursorPrefix, digest, sources,
		func(source textSource, chunk BoundedTextChunk) ModuleContentReadItem {
			return ModuleContentReadItem{Module: source.module, SourceChunk: chunk}
		},
		func(item ModuleContentReadItem) int64 { return int64(len(item.SourceChunk.Bytes)) },
		func(items []ModuleContentReadItem, page PageInfo) any {
			return ReadModulesResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}
		})
	return ReadModulesResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}, err
}

func (e Engine) FindSymbols(ctx context.Context, input FindSymbolsInput) (FindSymbolsResult, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return FindSymbolsResult{}, err
	}
	if !snapshot.capabilities.FindSymbols {
		return FindSymbolsResult{}, operationDisabled("find_symbols")
	}
	if err := validateReadLimits(input.Limits, document.limits); err != nil {
		return FindSymbolsResult{}, err
	}
	if input.Query == "" || !utf8.ValidString(input.Query) || utf8.RuneCountInString(input.Query) > 1024 || (input.MatchMode != "exact" && input.MatchMode != "prefix" && input.MatchMode != "substring") || (input.CaseMode != "sensitive" && input.CaseMode != "unicode_simple_fold") {
		return FindSymbolsResult{}, &WorkbenchError{Code: "engine.workbench.invalid_symbol_query", Category: WorkbenchErrorInputInvalid}
	}
	if input.OwnerAddresses != nil && len(input.OwnerAddresses) == 0 || input.SubjectKinds != nil && len(input.SubjectKinds) == 0 {
		return FindSymbolsResult{}, &WorkbenchError{Code: "engine.workbench.empty_filter", Category: WorkbenchErrorInputInvalid}
	}
	if err := validateSortedUnique(input.OwnerAddresses, false); err != nil {
		return FindSymbolsResult{}, err
	}
	if err := validateSortedUniqueKinds(input.SubjectKinds); err != nil {
		return FindSymbolsResult{}, err
	}
	if err := validateReadSelectionCount(len(input.OwnerAddresses)+len(input.SubjectKinds), document.limits); err != nil {
		return FindSymbolsResult{}, err
	}
	names := snapshot.searchNames
	ranges := snapshot.sourceRanges
	ownerFilter, kindFilter := stringSet(input.OwnerAddresses), kindSet(input.SubjectKinds)
	iterate := func(yield func(SymbolReadItem) bool) error {
		for _, subject := range snapshot.compiled.SemanticIndex.Subjects {
			if err := checkWorkbenchContext(ctx); err != nil {
				return err
			}
			if len(ownerFilter) != 0 && (subject.OwnerAddress == nil || !ownerFilter[*subject.OwnerAddress]) || len(kindFilter) != 0 && !kindFilter[subject.Kind] {
				continue
			}
			rangeValue, exists := ranges[subject.Address]
			if !exists {
				continue
			}
			name := names[subject.Address]
			fields := []struct{ field, value string }{{"address", subject.Address}, {"id", terminalID(subject.Address)}, {"display_name", name}}
			for _, field := range fields {
				if field.value != "" && symbolMatch(field.value, input.Query, input.MatchMode, input.CaseMode) {
					if !yield(SymbolReadItem{Address: subject.Address, DisplayName: name, Kind: subject.Kind, MatchedField: field.field, MatchedValue: field.value, SourceRange: rangeValue}) {
						return nil
					}
					break
				}
			}
		}
		return nil
	}
	digest := requestDigest(struct {
		CaseMode  string
		MatchMode string
		Owners    []string
		Query     string
		Kinds     []SemanticSubjectKind
	}{input.CaseMode, input.MatchMode, input.OwnerAddresses, input.Query, input.SubjectKinds})
	items, page, err := paginateIteratedValues(ctx, e.workbench, input.DocumentGeneration, input.Limits, input.Cursor, findSymbolsCursorPrefix, digest, iterate, func(items []SymbolReadItem, page PageInfo) any {
		return FindSymbolsResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}
	})
	return FindSymbolsResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}, err
}

func (e Engine) ReadDeclarations(ctx context.Context, input ReadDeclarationsInput) (ReadDeclarationsResult, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return ReadDeclarationsResult{}, err
	}
	if !snapshot.capabilities.ReadDeclarations {
		return ReadDeclarationsResult{}, operationDisabled("read_declarations")
	}
	if err := validateReadLimits(input.Limits, document.limits); err != nil {
		return ReadDeclarationsResult{}, err
	}
	if err := validateSortedUnique(input.Addresses, true); err != nil {
		return ReadDeclarationsResult{}, err
	}
	if err := validateReadSelectionCount(len(input.Addresses), document.limits); err != nil {
		return ReadDeclarationsResult{}, err
	}
	sources, err := declarationSources(snapshot, input.Addresses)
	if err != nil {
		return ReadDeclarationsResult{}, err
	}
	digest := requestDigest(input.Addresses)
	items, page, err := paginateTextValues(ctx, e.workbench, input.DocumentGeneration, input.Limits, input.Cursor, readDeclarationsCursorPrefix, digest, sources,
		func(source textSource, chunk BoundedTextChunk) DeclarationReadItem {
			return DeclarationReadItem{Address: source.address, Kind: source.kind, SourceChunk: chunk, SourceRange: source.rangeValue}
		},
		func(item DeclarationReadItem) int64 { return int64(len(item.SourceChunk.Bytes)) },
		func(items []DeclarationReadItem, page PageInfo) any {
			return ReadDeclarationsResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}
		})
	return ReadDeclarationsResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}, err
}

func (e Engine) ReadRows(ctx context.Context, input ReadRowsInput) (ReadRowsResult, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return ReadRowsResult{}, err
	}
	if !snapshot.capabilities.ReadRows {
		return ReadRowsResult{}, operationDisabled("read_rows")
	}
	if err := validateReadLimits(input.Limits, document.limits); err != nil {
		return ReadRowsResult{}, err
	}
	if err := validateSortedUnique(input.OwnerAddresses, true); err != nil {
		return ReadRowsResult{}, err
	}
	if err := validateReadSelectionCount(len(input.OwnerAddresses), document.limits); err != nil {
		return ReadRowsResult{}, err
	}
	rows, subjects := snapshot.rows, snapshot.subjects
	iterate := func(yield func(RowReadItem) bool) error {
		for _, owner := range input.OwnerAddresses {
			if _, exists := subjects[owner]; !exists {
				return notFound()
			}
			for _, members := range snapshot.compiled.SemanticIndex.Rows {
				if members.OwnerAddress != owner {
					continue
				}
				for _, address := range members.Addresses {
					row, exists := rows[address]
					if !exists {
						return invariantRead()
					}
					keys := make([]string, 0, len(row.Values))
					for key := range row.Values {
						keys = append(keys, key)
					}
					sort.Strings(keys)
					cells := make([]RowCell, 0, len(keys))
					for _, key := range keys {
						cells = append(cells, RowCell{ColumnAddress: key, Value: row.Values[key]})
					}
					if !yield(RowReadItem{OwnerAddress: owner, RowAddress: address, Values: cells}) {
						return nil
					}
				}
			}
		}
		return nil
	}
	digest := requestDigest(input.OwnerAddresses)
	items, page, err := paginateIteratedValues(ctx, e.workbench, input.DocumentGeneration, input.Limits, input.Cursor, readRowsCursorPrefix, digest, iterate, func(items []RowReadItem, page PageInfo) any {
		return ReadRowsResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}
	})
	return ReadRowsResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}, err
}

func (e Engine) FindUsages(ctx context.Context, input FindUsagesInput) (FindUsagesResult, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return FindUsagesResult{}, err
	}
	if !snapshot.capabilities.FindUsages {
		return FindUsagesResult{}, operationDisabled("find_usages")
	}
	if err := validateReadLimits(input.Limits, document.limits); err != nil {
		return FindUsagesResult{}, err
	}
	if err := validateSortedUnique(input.TargetAddresses, true); err != nil {
		return FindUsagesResult{}, err
	}
	if err := validateReadSelectionCount(len(input.TargetAddresses), document.limits); err != nil {
		return FindUsagesResult{}, err
	}
	targets := stringSet(input.TargetAddresses)
	iterate := func(yield func(UsageReadItem) bool) error {
		for _, reference := range snapshot.compiled.SemanticIndex.References {
			if targets[reference.TargetAddress] && !yield(UsageReadItem{Range: reference.Range, SourceAddress: reference.SourceAddress, TargetAddress: reference.TargetAddress, TargetKind: reference.TargetKind, Via: reference.Via}) {
				break
			}
		}
		return nil
	}
	digest := requestDigest(input.TargetAddresses)
	items, page, err := paginateIteratedValues(ctx, e.workbench, input.DocumentGeneration, input.Limits, input.Cursor, findUsagesCursorPrefix, digest, iterate, func(items []UsageReadItem, page PageInfo) any {
		return FindUsagesResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}
	})
	return FindUsagesResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}, err
}

func (e Engine) GetNeighbors(ctx context.Context, input GetNeighborsInput) (GetNeighborsResult, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return GetNeighborsResult{}, err
	}
	if !snapshot.capabilities.GetNeighbors {
		return GetNeighborsResult{}, operationDisabled("get_neighbors")
	}
	if err := validateReadLimits(input.Limits, document.limits); err != nil {
		return GetNeighborsResult{}, err
	}
	if err := validateSortedUnique(input.EntityAddresses, true); err != nil {
		return GetNeighborsResult{}, err
	}
	if err := validateReadSelectionCount(len(input.EntityAddresses), document.limits); err != nil {
		return GetNeighborsResult{}, err
	}
	if input.Depth < 1 || input.Depth > e.workbench.config.MaxDepth || (input.Direction != "both" && input.Direction != "incoming" && input.Direction != "outgoing") {
		return GetNeighborsResult{}, &WorkbenchError{Code: "engine.workbench.invalid_traversal", Category: WorkbenchErrorInputInvalid}
	}
	iterate, err := neighborIterator(ctx, snapshot, input.EntityAddresses, input.Direction, input.Depth)
	if err != nil {
		return GetNeighborsResult{}, err
	}
	digest := requestDigest(struct {
		Roots     []string
		Direction string
		Depth     int64
	}{input.EntityAddresses, input.Direction, input.Depth})
	items, page, err := paginateIteratedValues(ctx, e.workbench, input.DocumentGeneration, input.Limits, input.Cursor, getNeighborsCursorPrefix, digest, iterate, func(items []NeighborReadItem, page PageInfo) any {
		return GetNeighborsResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}
	})
	return GetNeighborsResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}, err
}

func (e Engine) InspectSubgraph(ctx context.Context, input InspectSubgraphInput) (InspectSubgraphResult, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return InspectSubgraphResult{}, err
	}
	if !snapshot.capabilities.InspectSubgraph {
		return InspectSubgraphResult{}, operationDisabled("inspect_subgraph")
	}
	if err := validateReadLimits(input.Limits, document.limits); err != nil {
		return InspectSubgraphResult{}, err
	}
	if err := validateSortedUnique(input.RootAddresses, true); err != nil {
		return InspectSubgraphResult{}, err
	}
	if err := validateReadSelectionCount(len(input.RootAddresses), document.limits); err != nil {
		return InspectSubgraphResult{}, err
	}
	if input.Depth < 0 || input.Depth > e.workbench.config.MaxDepth {
		return InspectSubgraphResult{}, &WorkbenchError{Code: "engine.workbench.invalid_depth", Category: WorkbenchErrorInputInvalid}
	}
	iterate, err := subgraphIterator(ctx, snapshot, input.RootAddresses, input.Depth)
	if err != nil {
		return InspectSubgraphResult{}, err
	}
	digest := requestDigest(struct {
		Roots []string
		Depth int64
	}{input.RootAddresses, input.Depth})
	items, page, err := paginateSubgraphValues(ctx, e.workbench, input.DocumentGeneration, input.Limits, input.Cursor, digest, iterate, snapshot)
	return InspectSubgraphResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page, Relations: relationFactsForItems(items, snapshot)}, err
}

func paginateSubgraphValues(ctx context.Context, store *workbenchStore, generation DocumentGeneration, limits WorkbenchLimits, cursor *Cursor, digest [32]byte, iterate valueIterator[SubgraphReadItem], snapshot *workingSnapshot) ([]SubgraphReadItem, PageInfo, error) {
	position, err := store.decodeCursor(cursor, inspectSubgraphCursorPrefix, generation, digest)
	if err != nil {
		return nil, PageInfo{}, err
	}
	baseOffset := position.itemOffset
	window := make([]SubgraphReadItem, 0, int(limits.MaxItems)+1)
	var selected uint64
	hasMore := false
	if err := iterate(func(value SubgraphReadItem) bool {
		if selected < baseOffset {
			selected++
			return true
		}
		if len(window) >= int(limits.MaxItems)+1 {
			hasMore = true
			return false
		}
		window = append(window, value)
		selected++
		return true
	}); err != nil {
		return nil, PageInfo{}, err
	}
	if selected < baseOffset || len(window) == 0 && position.byteOffset != 0 {
		return nil, PageInfo{}, invalidCursor()
	}
	if len(window) > int(limits.MaxItems) {
		hasMore = true
		window = window[:limits.MaxItems]
	}
	index, nestedOffset := 0, int(position.byteOffset)
	items := make([]SubgraphReadItem, 0, min(int(limits.MaxItems), len(window)))
	var accepted PageInfo
	for index < len(window) && int64(len(items)) < limits.MaxItems {
		if err := checkWorkbenchReadBoundary(ctx, workbenchReadSubgraphPage, window[index].Subject.Address); err != nil {
			return nil, PageInfo{}, err
		}
		chunk, nextNested, totalNested, ok := subgraphItemChunk(window[index], nestedOffset, int(limits.MaxItems))
		if !ok {
			return nil, PageInfo{}, invalidCursor()
		}
		candidate := append(slicesClone(items), chunk)
		nextItem := baseOffset + uint64(index)
		if nextNested == totalNested {
			nextItem, nextNested = baseOffset+uint64(index+1), 0
		}
		more := nextNested != 0 || index+1 < len(window) || hasMore
		truncation := TruncationOutputByteLimit
		if nextNested != 0 || int64(len(candidate)) == limits.MaxItems {
			truncation = TruncationItemLimit
		}
		page := subgraphPage(store, generation, digest, candidate, nextItem, nextNested, more, truncation)
		measured, measureErr := measureLogicalResult(InspectSubgraphResult{DocumentGeneration: generation, Items: candidate, Page: page, Relations: relationFactsForItems(candidate, snapshot)}, 0)
		if measureErr != nil {
			return nil, PageInfo{}, measureErr
		}
		if measured > limits.MaxOutputBytes {
			if len(items) != 0 {
				return items, accepted, nil
			}
			available := totalNested - nestedOffset
			best := 0
			for low, high := 1, min(available, int(limits.MaxItems)); low <= high; {
				middle := low + (high-low)/2
				trial, trialNext, _, _ := subgraphItemChunk(window[index], nestedOffset, middle)
				trialItem, trialOffset := baseOffset+uint64(index), trialNext
				if trialNext == totalNested {
					trialItem, trialOffset = baseOffset+uint64(index+1), 0
				}
				trialMore := trialOffset != 0 || index+1 < len(window) || hasMore
				trialPage := subgraphPage(store, generation, digest, []SubgraphReadItem{trial}, trialItem, trialOffset, trialMore, TruncationOutputByteLimit)
				trialBytes, trialErr := measureLogicalResult(InspectSubgraphResult{DocumentGeneration: generation, Items: []SubgraphReadItem{trial}, Page: trialPage, Relations: relationFactsForItems([]SubgraphReadItem{trial}, snapshot)}, 0)
				if trialErr == nil && trialBytes <= limits.MaxOutputBytes {
					best = middle
					low = middle + 1
				} else {
					high = middle - 1
				}
			}
			if best == 0 {
				return nil, PageInfo{}, workbenchLimit("max_output_bytes", limits.MaxOutputBytes, measured)
			}
			chunk, nextNested, totalNested, _ = subgraphItemChunk(window[index], nestedOffset, best)
			nextItem = baseOffset + uint64(index)
			if nextNested == totalNested {
				nextItem, nextNested = baseOffset+uint64(index+1), 0
			}
			more = nextNested != 0 || index+1 < len(window) || hasMore
			page = subgraphPage(store, generation, digest, []SubgraphReadItem{chunk}, nextItem, nextNested, more, TruncationOutputByteLimit)
			measured, _ = measureLogicalResult(InspectSubgraphResult{DocumentGeneration: generation, Items: []SubgraphReadItem{chunk}, Page: page, Relations: relationFactsForItems([]SubgraphReadItem{chunk}, snapshot)}, 0)
			page.ReturnedBytes = measured
			return []SubgraphReadItem{chunk}, page, nil
		}
		page.ReturnedBytes = measured
		items, accepted = candidate, page
		if nextNested != 0 {
			return items, accepted, nil
		}
		index, nestedOffset = index+1, 0
	}
	if len(items) == 0 {
		page := subgraphPage(store, generation, digest, items, baseOffset+uint64(index), nestedOffset, false, TruncationComplete)
		measured, measureErr := measureLogicalResult(InspectSubgraphResult{DocumentGeneration: generation, Items: items, Page: page, Relations: []SubgraphRelationFact{}}, 0)
		if measureErr != nil || measured > limits.MaxOutputBytes {
			if measureErr != nil {
				return nil, PageInfo{}, measureErr
			}
			return nil, PageInfo{}, workbenchLimit("max_output_bytes", limits.MaxOutputBytes, measured)
		}
		page.ReturnedBytes = measured
		return items, page, nil
	}
	return items, accepted, nil
}

func subgraphItemChunk(value SubgraphReadItem, offset, limit int) (SubgraphReadItem, int, int, bool) {
	rows := value.Facts.RowAddresses
	incoming, outgoing := []string{}, []string{}
	if value.Adjacency != nil {
		incoming, outgoing = value.Adjacency.Incoming, value.Adjacency.Outgoing
	}
	total := len(rows) + len(incoming) + len(outgoing)
	if offset < 0 || offset > total {
		return SubgraphReadItem{}, 0, total, false
	}
	result := value
	result.Facts.RowAddresses = []string{}
	if value.Adjacency != nil {
		result.Adjacency = &SubgraphAdjacency{EntityAddress: value.Adjacency.EntityAddress, Incoming: []string{}, Outgoing: []string{}}
	}
	end := min(total, offset+limit)
	for position := offset; position < end; position++ {
		switch {
		case position < len(rows):
			result.Facts.RowAddresses = append(result.Facts.RowAddresses, rows[position])
		case position < len(rows)+len(incoming):
			result.Adjacency.Incoming = append(result.Adjacency.Incoming, incoming[position-len(rows)])
		default:
			result.Adjacency.Outgoing = append(result.Adjacency.Outgoing, outgoing[position-len(rows)-len(incoming)])
		}
	}
	return result, end, total, true
}

func subgraphPage(store *workbenchStore, generation DocumentGeneration, digest [32]byte, items []SubgraphReadItem, nextItem uint64, nextNested int, more bool, truncation TruncationOutcome) PageInfo {
	page := PageInfo{ReturnedItems: int64(len(items)), Truncation: TruncationComplete}
	if more {
		page.Truncation = truncation
		next := store.encodeCursor(inspectSubgraphCursorPrefix, generation, digest, cursorPosition{itemOffset: nextItem, byteOffset: uint64(nextNested)})
		page.NextCursor = &next
	}
	return page
}

func (e Engine) ReadScope(ctx context.Context, input ReadScopeInput) (ReadScopeResult, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return ReadScopeResult{}, err
	}
	if !snapshot.capabilities.ReadScope {
		return ReadScopeResult{}, operationDisabled("read_scope")
	}
	if err := validateReadLimits(input.Limits, document.limits); err != nil {
		return ReadScopeResult{}, err
	}
	if input.OwnerAddress == "" {
		return ReadScopeResult{}, &WorkbenchError{Code: "engine.workbench.invalid_owner", Category: WorkbenchErrorInputInvalid}
	}
	addresses, err := scopeAddresses(snapshot, input.OwnerAddress, document.limits.MaxItems)
	if err != nil {
		return ReadScopeResult{}, err
	}
	sources, err := declarationSources(snapshot, addresses)
	if err != nil {
		return ReadScopeResult{}, err
	}
	digest := requestDigest(input.OwnerAddress)
	items, page, err := paginateTextValues(ctx, e.workbench, input.DocumentGeneration, input.Limits, input.Cursor, readScopeCursorPrefix, digest, sources,
		func(source textSource, chunk BoundedTextChunk) ScopeReadItem {
			return ScopeReadItem{OwnerAddress: input.OwnerAddress, SourceChunk: chunk, SourceRange: source.rangeValue}
		},
		func(item ScopeReadItem) int64 { return int64(len(item.SourceChunk.Bytes)) },
		func(items []ScopeReadItem, page PageInfo) any {
			return ReadScopeResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}
		})
	return ReadScopeResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}, err
}

func (e Engine) ListReferences(ctx context.Context, input ListReferencesInput) (ListReferencesResult, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return ListReferencesResult{}, err
	}
	if !snapshot.capabilities.ListReferences {
		return ListReferencesResult{}, operationDisabled("list_references")
	}
	if err := validateReadLimits(input.Limits, document.limits); err != nil {
		return ListReferencesResult{}, err
	}
	texts := snapshot.referenceText
	ranges := snapshot.sourceRanges
	iterate := func(yield func(ReferenceSummaryReadItem) bool) error {
		for _, subject := range snapshot.compiled.SemanticIndex.Subjects {
			if subject.Kind != materialize.SubjectReference {
				continue
			}
			text, textExists := texts[subject.Address]
			rangeValue, rangeExists := ranges[subject.Address]
			if !textExists || !rangeExists {
				return invariantRead()
			}
			if !yield(ReferenceSummaryReadItem{Address: subject.Address, SourceRange: rangeValue, TextDigest: digestBytesForWorkbench([]byte(text))}) {
				break
			}
		}
		return nil
	}
	digest := requestDigest(struct{}{})
	items, page, err := paginateIteratedValues(ctx, e.workbench, input.DocumentGeneration, input.Limits, input.Cursor, listReferencesCursorPrefix, digest, iterate, func(items []ReferenceSummaryReadItem, page PageInfo) any {
		return ListReferencesResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}
	})
	return ListReferencesResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}, err
}

func (e Engine) ReadReferences(ctx context.Context, input ReadReferencesInput) (ReadReferencesResult, error) {
	document, snapshot, err := e.acquireSnapshot(ctx, input.DocumentGeneration)
	if err != nil {
		return ReadReferencesResult{}, err
	}
	if !snapshot.capabilities.ReadReferences {
		return ReadReferencesResult{}, operationDisabled("read_references")
	}
	if err := validateReadLimits(input.Limits, document.limits); err != nil {
		return ReadReferencesResult{}, err
	}
	if err := validateSortedUnique(input.Addresses, true); err != nil {
		return ReadReferencesResult{}, err
	}
	if err := validateReadSelectionCount(len(input.Addresses), document.limits); err != nil {
		return ReadReferencesResult{}, err
	}
	texts, ranges := snapshot.referenceText, snapshot.sourceRanges
	sources := make([]textSource, 0, len(input.Addresses))
	for _, address := range input.Addresses {
		text, textExists := texts[address]
		rangeValue, rangeExists := ranges[address]
		if !textExists || !rangeExists {
			return ReadReferencesResult{}, notFound()
		}
		sources = append(sources, textSource{address: address, kind: materialize.SubjectReference, rangeValue: rangeValue, bytes: []byte(text)})
	}
	digest := requestDigest(input.Addresses)
	items, page, err := paginateTextValues(ctx, e.workbench, input.DocumentGeneration, input.Limits, input.Cursor, readReferencesCursorPrefix, digest, sources,
		func(source textSource, chunk BoundedTextChunk) ReferenceContentReadItem {
			return ReferenceContentReadItem{Address: source.address, SourceRange: source.rangeValue, TextChunk: chunk}
		},
		func(item ReferenceContentReadItem) int64 { return int64(len(item.TextChunk.Bytes)) },
		func(items []ReferenceContentReadItem, page PageInfo) any {
			return ReadReferencesResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}
		})
	return ReadReferencesResult{DocumentGeneration: input.DocumentGeneration, Items: items, Page: page}, err
}

func declarationSources(snapshot *workingSnapshot, addresses []string) ([]textSource, error) {
	subjects := map[string]index.SourceSubjectRecord{}
	kinds := map[string]SemanticSubjectKind{}
	for _, subject := range snapshot.compiled.SourceMap.Subjects {
		subjects[subject.Address] = subject
	}
	for _, subject := range snapshot.compiled.SemanticIndex.Subjects {
		kinds[subject.Address] = subject.Kind
	}
	result := make([]textSource, 0, len(addresses))
	for _, address := range addresses {
		subject, exists := subjects[address]
		kind, kindExists := kinds[address]
		if !exists || !kindExists || subject.DeclarationRange == nil {
			return nil, notFound()
		}
		value, err := snapshot.source(*subject.DeclarationRange)
		if err != nil {
			return nil, err
		}
		result = append(result, textSource{address: address, kind: kind, rangeValue: *subject.DeclarationRange, bytes: value})
	}
	return result, nil
}

func sourceSubjectRanges(snapshot Snapshot) map[string]SourceRange {
	result := map[string]SourceRange{}
	for _, subject := range snapshot.SourceMap.Subjects {
		if subject.DeclarationRange != nil {
			result[subject.Address] = *subject.DeclarationRange
		}
	}
	return result
}

func subjectNames(snapshot Snapshot) map[string]string {
	result := map[string]string{}
	if document := snapshot.NormalizedDocument; document != nil {
		result[document.Project.Address] = document.Project.DisplayName
		for _, item := range document.EntityTypes {
			result[item.Address] = item.DisplayName
			for _, child := range item.Columns {
				result[child.Address] = child.DisplayName
			}
		}
		for _, item := range document.RelationTypes {
			result[item.Address] = item.DisplayName
			for _, child := range item.Columns {
				result[child.Address] = child.DisplayName
			}
		}
		for _, item := range document.Layers {
			result[item.Address] = item.DisplayName
		}
		for _, item := range document.Entities {
			result[item.Address] = item.DisplayName
			for _, row := range item.Rows {
				result[row.Address] = row.ID
			}
		}
		for _, item := range document.Relations {
			if item.DisplayName != nil {
				result[item.Address] = *item.DisplayName
			}
			for _, row := range item.Rows {
				result[row.Address] = row.ID
			}
		}
		for _, item := range document.Queries {
			result[item.Address] = item.DisplayName
		}
		for _, item := range document.Views {
			result[item.Address] = item.DisplayName
		}
	}
	if pack := snapshot.NormalizedPackArtifact; pack != nil {
		result[pack.Pack.Address] = pack.Pack.CanonicalID
		for _, item := range pack.EntityTypes {
			result[item.Address] = item.DisplayName
			for _, child := range item.Columns {
				result[child.Address] = child.DisplayName
			}
		}
		for _, item := range pack.RelationTypes {
			result[item.Address] = item.DisplayName
			for _, child := range item.Columns {
				result[child.Address] = child.DisplayName
			}
		}
		for _, item := range pack.Queries {
			result[item.Address] = item.DisplayName
		}
		for _, item := range pack.Views {
			result[item.Address] = item.DisplayName
		}
	}
	return result
}

func referenceTexts(snapshot Snapshot) map[string]string {
	result := map[string]string{}
	if snapshot.NormalizedDocument != nil {
		for _, item := range snapshot.NormalizedDocument.References {
			result[item.Address] = item.Text
		}
	}
	if snapshot.NormalizedPackArtifact != nil {
		for _, item := range snapshot.NormalizedPackArtifact.References {
			result[item.Address] = item.Text
		}
	}
	return result
}

func terminalID(address string) string {
	if index := strings.LastIndexByte(address, ':'); index >= 0 {
		return address[index+1:]
	}
	return address
}

func symbolMatch(value, query, mode, caseMode string) bool {
	if caseMode == "unicode_simple_fold" {
		value, query = simpleFold(value), simpleFold(query)
	}
	switch mode {
	case "exact":
		return value == query
	case "prefix":
		return strings.HasPrefix(value, query)
	default:
		return strings.Contains(value, query)
	}
}

func simpleFold(value string) string {
	return strings.Map(func(r rune) rune {
		minimum := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < minimum {
				minimum = next
			}
		}
		return minimum
	}, value)
}

func stringSet(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}
func kindSet(values []SemanticSubjectKind) map[SemanticSubjectKind]bool {
	result := map[SemanticSubjectKind]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}
func semanticSubjectsByAddress(snapshot Snapshot) map[string]index.SemanticSubject {
	result := make(map[string]index.SemanticSubject, len(snapshot.SemanticIndex.Subjects))
	for _, subject := range snapshot.SemanticIndex.Subjects {
		result[subject.Address] = subject
	}
	return result
}

func adjacencyByEntity(snapshot Snapshot) map[string]index.AdjacencyRecord {
	result := make(map[string]index.AdjacencyRecord, len(snapshot.SemanticIndex.Adjacency))
	for _, record := range snapshot.SemanticIndex.Adjacency {
		result[record.EntityAddress] = record
	}
	return result
}

func neighborIterator(ctx context.Context, snapshot *workingSnapshot, roots []string, direction string, maxDepth int64) (valueIterator[NeighborReadItem], error) {
	adjacency := snapshot.adjacency
	relations := snapshot.relations
	for _, root := range roots {
		if _, exists := adjacency[root]; !exists {
			return nil, notFound()
		}
	}
	return func(yield func(NeighborReadItem) bool) error {
		type queueItem struct {
			address string
			depth   int64
		}
		queue := make([]queueItem, 0, len(roots))
		visited := make(map[string]bool, len(roots))
		for _, root := range roots {
			queue = append(queue, queueItem{address: root})
			visited[root] = true
		}
		var traversalIndex uint64
		for head := 0; head < len(queue); head++ {
			current := queue[head]
			if err := checkWorkbenchReadBoundary(ctx, workbenchReadNeighborNode, current.address); err != nil {
				return err
			}
			if current.depth >= maxDepth {
				continue
			}
			record, exists := adjacency[current.address]
			if !exists {
				return invariantRead()
			}
			for _, edgeDirection := range [...]string{"incoming", "outgoing"} {
				if direction != "both" && direction != edgeDirection {
					continue
				}
				addresses := record.Incoming
				if edgeDirection == "outgoing" {
					addresses = record.Outgoing
				}
				for _, relationAddress := range addresses {
					if err := checkWorkbenchReadBoundary(ctx, workbenchReadNeighborEdge, relationAddress); err != nil {
						return err
					}
					relation, exists := relations[relationAddress]
					if !exists {
						return invariantRead()
					}
					neighbor := relation.FromAddress
					if edgeDirection == "outgoing" {
						neighbor = relation.ToAddress
					}
					item := NeighborReadItem{Depth: current.depth + 1, Direction: edgeDirection, EntityAddress: neighbor, RelationAddress: relationAddress, SourceEntityAddress: current.address, TraversalIndex: traversalIndex}
					traversalIndex++
					if !yield(item) {
						return nil
					}
					if !visited[neighbor] {
						visited[neighbor] = true
						queue = append(queue, queueItem{address: neighbor, depth: current.depth + 1})
					}
				}
			}
		}
		return nil
	}, nil
}

func relationMap(snapshot Snapshot) map[string]materialize.Relation {
	result := map[string]materialize.Relation{}
	if snapshot.NormalizedDocument != nil {
		for _, relation := range snapshot.NormalizedDocument.Relations {
			result[relation.Address] = relation
		}
	}
	return result
}

func entityMap(snapshot Snapshot) map[string]materialize.Entity {
	result := map[string]materialize.Entity{}
	if snapshot.NormalizedDocument != nil {
		for _, entity := range snapshot.NormalizedDocument.Entities {
			result[entity.Address] = entity
		}
	}
	return result
}

func subgraphIterator(ctx context.Context, snapshot *workingSnapshot, roots []string, depth int64) (valueIterator[SubgraphReadItem], error) {
	entities, relations := snapshot.entities, snapshot.relations
	for _, root := range roots {
		if _, exists := entities[root]; !exists {
			return nil, notFound()
		}
	}
	adjacency := snapshot.adjacency
	return func(yield func(SubgraphReadItem) bool) error {
		type queueItem struct {
			address string
			depth   int64
		}
		queue := make([]queueItem, 0, len(roots))
		visitedEntities := make(map[string]bool, len(roots))
		seenRelations := make(map[string]bool)
		var traversalIndex uint64
		emitEntity := func(address string, itemDepth int64) (bool, error) {
			entity, entityExists := entities[address]
			subject, subjectExists := snapshot.subjects[address]
			if !entityExists || !subjectExists {
				return false, invariantRead()
			}
			item := SubgraphReadItem{Depth: itemDepth, Subject: subgraphSubject(subject), TraversalIndex: traversalIndex, Facts: SubgraphGraphFacts{Kind: "entity", EntityTypeAddress: cloneStringPointer(entity.TypeAddress), LayerAddress: cloneStringPointer(entity.LayerAddress), RowAddresses: []string{}}}
			traversalIndex++
			if depth > 0 {
				item.Facts.RowAddresses = snapshot.rowAddresses[address]
			}
			if itemDepth < depth {
				record, exists := adjacency[address]
				if !exists {
					return false, invariantRead()
				}
				item.Adjacency = &SubgraphAdjacency{EntityAddress: address, Incoming: record.Incoming, Outgoing: record.Outgoing}
			}
			return yield(item), nil
		}
		for _, root := range roots {
			queue = append(queue, queueItem{address: root})
			visitedEntities[root] = true
			keepGoing, err := emitEntity(root, 0)
			if err != nil || !keepGoing {
				return err
			}
		}
		for head := 0; head < len(queue); head++ {
			current := queue[head]
			if err := checkWorkbenchReadBoundary(ctx, workbenchReadSubgraphNode, current.address); err != nil {
				return err
			}
			if current.depth >= depth {
				continue
			}
			record, exists := adjacency[current.address]
			if !exists {
				return invariantRead()
			}
			for _, edgeDirection := range [...]string{"incoming", "outgoing"} {
				addresses := record.Incoming
				if edgeDirection == "outgoing" {
					addresses = record.Outgoing
				}
				for _, relationAddress := range addresses {
					if err := checkWorkbenchReadBoundary(ctx, workbenchReadSubgraphEdge, relationAddress); err != nil {
						return err
					}
					relation, relationExists := relations[relationAddress]
					subject, subjectExists := snapshot.subjects[relationAddress]
					if !relationExists || !subjectExists {
						return invariantRead()
					}
					neighbor := relation.FromAddress
					if edgeDirection == "outgoing" {
						neighbor = relation.ToAddress
					}
					if !seenRelations[relationAddress] {
						seenRelations[relationAddress] = true
						item := SubgraphReadItem{Depth: current.depth + 1, Subject: subgraphSubject(subject), TraversalIndex: traversalIndex, Facts: SubgraphGraphFacts{Kind: "relation", RelationTypeAddress: cloneStringPointer(relation.TypeAddress), FromAddress: cloneStringPointer(relation.FromAddress), ToAddress: cloneStringPointer(relation.ToAddress), RowAddresses: snapshot.rowAddresses[relationAddress]}}
						traversalIndex++
						if !yield(item) {
							return nil
						}
					}
					if !visitedEntities[neighbor] {
						visitedEntities[neighbor] = true
						queue = append(queue, queueItem{address: neighbor, depth: current.depth + 1})
						keepGoing, err := emitEntity(neighbor, current.depth+1)
						if err != nil || !keepGoing {
							return err
						}
					}
				}
			}
		}
		return nil
	}, nil
}

func subgraphSubject(value index.SemanticSubject) SubgraphSubject {
	result := SubgraphSubject{Address: value.Address, Kind: value.Kind, OwnHash: value.OwnHash, OwnerAddress: cloneOptionalString(value.OwnerAddress), SubtreeHash: cloneOptionalString(value.SubtreeHash)}
	if value.Module != nil {
		result.Module = &ModuleRef{ModulePath: value.Module.ModulePath, Origin: value.Module.Origin}
	}
	return result
}

func relationFactsForItems(items []SubgraphReadItem, snapshot *workingSnapshot) []SubgraphRelationFact {
	relations := snapshot.relations
	result := []SubgraphRelationFact{}
	for _, item := range items {
		if relation, exists := relations[item.Subject.Address]; exists {
			result = append(result, SubgraphRelationFact{FromAddress: relation.FromAddress, RelationAddress: relation.Address, ToAddress: relation.ToAddress, TypeAddress: relation.TypeAddress})
		}
	}
	sort.Slice(result, func(left, right int) bool {
		return materialize.LessStableAddress(resolve.Result{}, result[left].RelationAddress, result[right].RelationAddress)
	})
	return result
}

func scopeAddresses(snapshot *workingSnapshot, owner string, maxItems int64) ([]string, error) {
	if _, exists := snapshot.subjects[owner]; !exists {
		return nil, notFound()
	}
	children := map[string][]string{}
	for _, members := range snapshot.compiled.SemanticIndex.ScopedReads.ChildrenByOwner {
		children[members.OwnerAddress] = members.Addresses
	}
	selected := map[string]bool{owner: true}
	queue := []string{owner}
	for len(queue) != 0 {
		current := queue[0]
		queue = queue[1:]
		for _, child := range children[current] {
			if !selected[child] {
				if int64(len(selected)) >= maxItems {
					return nil, workbenchLimit("scope_items", maxItems, int64(len(selected)+1))
				}
				selected[child] = true
				queue = append(queue, child)
			}
		}
	}
	result := []string{}
	for _, subject := range snapshot.compiled.SemanticIndex.Subjects {
		if selected[subject.Address] {
			result = append(result, subject.Address)
		}
	}
	return result, nil
}

func cloneStringPointer(value string) *string { result := value; return &result }
func cloneOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
func notFound() *WorkbenchError {
	return &WorkbenchError{Code: "engine.workbench.not_found", Category: WorkbenchErrorNotFound}
}
func invariantRead() *WorkbenchError {
	return &WorkbenchError{Code: "engine.workbench.index_invariant", Category: WorkbenchErrorInvariant}
}
