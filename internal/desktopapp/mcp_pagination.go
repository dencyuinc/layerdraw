// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"encoding/json"
	"errors"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
)

type mcpPageAdapter struct {
	bind    func([]byte, json.RawMessage) ([]byte, error)
	inspect func([]byte) (int, json.RawMessage, error)
}

func newMCPPageAdapter[Request, Response, Cursor any](
	decodeRequest func([]byte) (Request, error),
	encodeRequest func(Request) ([]byte, error),
	decodeCursor func([]byte) (Cursor, error),
	encodeCursor func(Cursor) ([]byte, error),
	hasCursor func(Request) bool,
	setCursor func(*Request, *Cursor),
	decodeResponse func([]byte) (Response, error),
	page func(Response) (int, *Cursor),
) mcpPageAdapter {
	return mcpPageAdapter{
		bind: func(control []byte, continuation json.RawMessage) ([]byte, error) {
			request, err := decodeRequest(control)
			if err != nil {
				return nil, err
			}
			if hasCursor(request) {
				return nil, errors.New("generated request contains an untrusted cursor")
			}
			if len(continuation) == 0 {
				return append([]byte(nil), control...), nil
			}
			cursor, err := decodeCursor(continuation)
			if err != nil {
				return nil, err
			}
			setCursor(&request, &cursor)
			return encodeRequest(request)
		},
		inspect: func(control []byte) (int, json.RawMessage, error) {
			response, err := decodeResponse(control)
			if err != nil {
				return 0, nil, err
			}
			items, cursor := page(response)
			if cursor == nil {
				return items, nil, nil
			}
			encoded, err := encodeCursor(*cursor)
			return items, json.RawMessage(encoded), err
		},
	}
}

// mcpPageAdapters is deliberately closed over generated paginated operations.
// A continuation can never be injected through an untyped JSON path.
var mcpPageAdapters = map[string]mcpPageAdapter{
	"engine.list_modules": newMCPPageAdapter(engineprotocol.DecodeListModulesRequestEnvelope, engineprotocol.EncodeListModulesRequestEnvelope, engineprotocol.DecodeModuleCursor, engineprotocol.EncodeModuleCursor,
		func(request engineprotocol.ListModulesRequestEnvelope) bool { return request.Payload.Cursor != nil },
		func(request *engineprotocol.ListModulesRequestEnvelope, cursor *engineprotocol.ModuleCursor) {
			request.Payload.Cursor = cursor
		},
		engineprotocol.DecodeListModulesResponseEnvelope, func(response engineprotocol.ListModulesResponseEnvelope) (int, *engineprotocol.ModuleCursor) {
			if response.Payload == nil {
				return 0, nil
			}
			return len(response.Payload.Items), response.Payload.Page.NextCursor
		}),
	"engine.find_symbols": newMCPPageAdapter(engineprotocol.DecodeFindSymbolsRequestEnvelope, engineprotocol.EncodeFindSymbolsRequestEnvelope, engineprotocol.DecodeSymbolCursor, engineprotocol.EncodeSymbolCursor,
		func(request engineprotocol.FindSymbolsRequestEnvelope) bool { return request.Payload.Cursor != nil },
		func(request *engineprotocol.FindSymbolsRequestEnvelope, cursor *engineprotocol.SymbolCursor) {
			request.Payload.Cursor = cursor
		},
		engineprotocol.DecodeFindSymbolsResponseEnvelope, func(response engineprotocol.FindSymbolsResponseEnvelope) (int, *engineprotocol.SymbolCursor) {
			if response.Payload == nil {
				return 0, nil
			}
			return len(response.Payload.Items), response.Payload.Page.NextCursor
		}),
	"engine.read_declarations": newMCPPageAdapter(engineprotocol.DecodeReadDeclarationsRequestEnvelope, engineprotocol.EncodeReadDeclarationsRequestEnvelope, engineprotocol.DecodeDeclarationCursor, engineprotocol.EncodeDeclarationCursor,
		func(request engineprotocol.ReadDeclarationsRequestEnvelope) bool {
			return request.Payload.Cursor != nil
		},
		func(request *engineprotocol.ReadDeclarationsRequestEnvelope, cursor *engineprotocol.DeclarationCursor) {
			request.Payload.Cursor = cursor
		},
		engineprotocol.DecodeReadDeclarationsResponseEnvelope, func(response engineprotocol.ReadDeclarationsResponseEnvelope) (int, *engineprotocol.DeclarationCursor) {
			if response.Payload == nil {
				return 0, nil
			}
			return len(response.Payload.Items), response.Payload.Page.NextCursor
		}),
	"engine.read_rows": newMCPPageAdapter(engineprotocol.DecodeReadRowsRequestEnvelope, engineprotocol.EncodeReadRowsRequestEnvelope, engineprotocol.DecodeRowCursor, engineprotocol.EncodeRowCursor,
		func(request engineprotocol.ReadRowsRequestEnvelope) bool { return request.Payload.Cursor != nil },
		func(request *engineprotocol.ReadRowsRequestEnvelope, cursor *engineprotocol.RowCursor) {
			request.Payload.Cursor = cursor
		},
		engineprotocol.DecodeReadRowsResponseEnvelope, func(response engineprotocol.ReadRowsResponseEnvelope) (int, *engineprotocol.RowCursor) {
			if response.Payload == nil {
				return 0, nil
			}
			return len(response.Payload.Items), response.Payload.Page.NextCursor
		}),
	"engine.get_neighbors": newMCPPageAdapter(engineprotocol.DecodeGetNeighborsRequestEnvelope, engineprotocol.EncodeGetNeighborsRequestEnvelope, engineprotocol.DecodeNeighborCursor, engineprotocol.EncodeNeighborCursor,
		func(request engineprotocol.GetNeighborsRequestEnvelope) bool { return request.Payload.Cursor != nil },
		func(request *engineprotocol.GetNeighborsRequestEnvelope, cursor *engineprotocol.NeighborCursor) {
			request.Payload.Cursor = cursor
		},
		engineprotocol.DecodeGetNeighborsResponseEnvelope, func(response engineprotocol.GetNeighborsResponseEnvelope) (int, *engineprotocol.NeighborCursor) {
			if response.Payload == nil {
				return 0, nil
			}
			return len(response.Payload.Items), response.Payload.Page.NextCursor
		}),
	"engine.inspect_subgraph": newMCPPageAdapter(engineprotocol.DecodeInspectSubgraphRequestEnvelope, engineprotocol.EncodeInspectSubgraphRequestEnvelope, engineprotocol.DecodeSubgraphCursor, engineprotocol.EncodeSubgraphCursor,
		func(request engineprotocol.InspectSubgraphRequestEnvelope) bool { return request.Payload.Cursor != nil },
		func(request *engineprotocol.InspectSubgraphRequestEnvelope, cursor *engineprotocol.SubgraphCursor) {
			request.Payload.Cursor = cursor
		},
		engineprotocol.DecodeInspectSubgraphResponseEnvelope, func(response engineprotocol.InspectSubgraphResponseEnvelope) (int, *engineprotocol.SubgraphCursor) {
			if response.Payload == nil {
				return 0, nil
			}
			return len(response.Payload.Items) + len(response.Payload.Relations), response.Payload.Page.NextCursor
		}),
	"engine.find_usages": newMCPPageAdapter(engineprotocol.DecodeFindUsagesRequestEnvelope, engineprotocol.EncodeFindUsagesRequestEnvelope, engineprotocol.DecodeUsageCursor, engineprotocol.EncodeUsageCursor,
		func(request engineprotocol.FindUsagesRequestEnvelope) bool { return request.Payload.Cursor != nil },
		func(request *engineprotocol.FindUsagesRequestEnvelope, cursor *engineprotocol.UsageCursor) {
			request.Payload.Cursor = cursor
		},
		engineprotocol.DecodeFindUsagesResponseEnvelope, func(response engineprotocol.FindUsagesResponseEnvelope) (int, *engineprotocol.UsageCursor) {
			if response.Payload == nil {
				return 0, nil
			}
			return len(response.Payload.Items), response.Payload.Page.NextCursor
		}),
	"engine.list_references": newMCPPageAdapter(engineprotocol.DecodeListReferencesRequestEnvelope, engineprotocol.EncodeListReferencesRequestEnvelope, engineprotocol.DecodeReferenceSummaryCursor, engineprotocol.EncodeReferenceSummaryCursor,
		func(request engineprotocol.ListReferencesRequestEnvelope) bool { return request.Payload.Cursor != nil },
		func(request *engineprotocol.ListReferencesRequestEnvelope, cursor *engineprotocol.ReferenceSummaryCursor) {
			request.Payload.Cursor = cursor
		},
		engineprotocol.DecodeListReferencesResponseEnvelope, func(response engineprotocol.ListReferencesResponseEnvelope) (int, *engineprotocol.ReferenceSummaryCursor) {
			if response.Payload == nil {
				return 0, nil
			}
			return len(response.Payload.Items), response.Payload.Page.NextCursor
		}),
	"engine.read_references": newMCPPageAdapter(engineprotocol.DecodeReadReferencesRequestEnvelope, engineprotocol.EncodeReadReferencesRequestEnvelope, engineprotocol.DecodeReferenceContentCursor, engineprotocol.EncodeReferenceContentCursor,
		func(request engineprotocol.ReadReferencesRequestEnvelope) bool { return request.Payload.Cursor != nil },
		func(request *engineprotocol.ReadReferencesRequestEnvelope, cursor *engineprotocol.ReferenceContentCursor) {
			request.Payload.Cursor = cursor
		},
		engineprotocol.DecodeReadReferencesResponseEnvelope, func(response engineprotocol.ReadReferencesResponseEnvelope) (int, *engineprotocol.ReferenceContentCursor) {
			if response.Payload == nil {
				return 0, nil
			}
			return len(response.Payload.Items), response.Payload.Page.NextCursor
		}),
	"runtime.list_revisions": {
		bind: func(control []byte, continuation json.RawMessage) ([]byte, error) {
			request, err := runtimeprotocol.DecodeListRevisionsRequestEnvelope(control)
			if err != nil {
				return nil, err
			}
			if request.Payload.Cursor != nil {
				return nil, errors.New("generated request contains an untrusted cursor")
			}
			if len(continuation) == 0 {
				return append([]byte(nil), control...), nil
			}
			cursor, err := runtimeprotocol.DecodeRuntimeCursor(continuation)
			if err != nil {
				return nil, err
			}
			request.Payload.Cursor = &cursor
			return runtimeprotocol.EncodeListRevisionsRequestEnvelope(request)
		},
		inspect: func(control []byte) (int, json.RawMessage, error) {
			response, err := runtimeprotocol.DecodeListRevisionsResponseEnvelope(control)
			if err != nil || response.Payload == nil {
				return 0, nil, err
			}
			if response.Payload.Page.NextCursor == nil {
				return len(response.Payload.Items), nil, nil
			}
			cursor, err := runtimeprotocol.EncodeRuntimeCursor(runtimeprotocol.RuntimeCursor(*response.Payload.Page.NextCursor))
			return len(response.Payload.Items), cursor, err
		},
	},
}

func adaptMCPPageRequest(operation string, control []byte, continuation json.RawMessage) ([]byte, error) {
	adapter, ok := mcpPageAdapters[operation]
	if !ok {
		if len(continuation) == 0 {
			return append([]byte(nil), control...), nil
		}
		return nil, errors.New("operation does not support MCP continuation")
	}
	return adapter.bind(control, continuation)
}

func inspectMCPPage(operation string, control []byte) (int, json.RawMessage, error) {
	adapter, ok := mcpPageAdapters[operation]
	if !ok {
		return 0, nil, nil
	}
	return adapter.inspect(control)
}
