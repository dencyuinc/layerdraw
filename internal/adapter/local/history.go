// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
package local

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type historyDisk struct {
	Items []runtimeprotocol.RevisionMetadata `json:"items"`
}

func validHistoryDisk(scope runtimeprotocol.RuntimeScope, h historyDisk) bool {
	seenRev, seenOp, seenProvider := map[runtimeprotocol.RevisionID]bool{}, map[runtimeprotocol.OperationID]bool{}, map[runtimeprotocol.ProviderVersionToken]runtimeprotocol.RevisionID{}
	for i, m := range h.Items {
		if _, e := runtimeprotocol.EncodeRevisionMetadata(m); e != nil || !validRevision(scope, m.Revision) || seenRev[m.Revision.RevisionID] || seenOp[m.OperationID] {
			return false
		}
		seenRev[m.Revision.RevisionID] = true
		seenOp[m.OperationID] = true
		if m.Revision.ProviderVersion != nil {
			if prior, ok := seenProvider[*m.Revision.ProviderVersion]; ok && prior != m.Revision.RevisionID {
				return false
			}
			seenProvider[*m.Revision.ProviderVersion] = m.Revision.RevisionID
		}
		if i > 0 {
			p := h.Items[i-1]
			if p.CommittedAt < m.CommittedAt || (p.CommittedAt == m.CommittedAt && p.Revision.RevisionID < m.Revision.RevisionID) {
				return false
			}
		}
	}
	return true
}

func (s *History) historyPath(scope runtimeprotocol.RuntimeScope) (string, error) {
	d, e := s.scopeDir(scope)
	return filepath.Join(d, "history", "revisions.json"), e
}

func (s *History) loadHistory(scope runtimeprotocol.RuntimeScope) (historyDisk, error) {
	p, err := s.historyPath(scope)
	if err != nil {
		return historyDisk{}, err
	}
	var h historyDisk
	if err := s.readJSON(p, &h); err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return historyDisk{Items: []runtimeprotocol.RevisionMetadata{}}, nil
		}
		return h, err
	}
	if !validHistoryDisk(scope, h) {
		return h, invalidPersisted("history")
	}
	return h, nil
}

func (s *History) AppendRevision(ctx context.Context, in port.AppendRevisionInput) (runtimeprotocol.RevisionMetadata, error) {
	if err := ctx.Err(); err != nil {
		return runtimeprotocol.RevisionMetadata{}, err
	}
	if in.Metadata.Revision.DocumentID != in.Scope.DocumentID {
		return runtimeprotocol.RevisionMetadata{}, port.ErrConflict
	}
	if _, err := runtimeprotocol.EncodeRevisionMetadata(in.Metadata); err != nil {
		return runtimeprotocol.RevisionMetadata{}, port.ErrConflict
	}
	err := s.withLock(in.Scope, func(_ string) error {
		h, err := s.loadHistory(in.Scope)
		if err != nil {
			return err
		}
		for _, m := range h.Items {
			if m.Revision.RevisionID == in.Metadata.Revision.RevisionID || m.OperationID == in.Metadata.OperationID {
				if reflect.DeepEqual(m, in.Metadata) {
					return nil
				}
				return port.ErrConflict
			}
			if m.Revision.ProviderVersion != nil && in.Metadata.Revision.ProviderVersion != nil && *m.Revision.ProviderVersion == *in.Metadata.Revision.ProviderVersion && !reflect.DeepEqual(m.Revision, in.Metadata.Revision) {
				return port.ErrConflict
			}
		}
		h.Items = append(h.Items, in.Metadata)
		sort.Slice(h.Items, func(i, j int) bool {
			if h.Items[i].CommittedAt != h.Items[j].CommittedAt {
				return h.Items[i].CommittedAt > h.Items[j].CommittedAt
			}
			return h.Items[i].Revision.RevisionID > h.Items[j].Revision.RevisionID
		})
		if !validHistoryDisk(in.Scope, h) {
			return port.ErrConflict
		}
		p, _ := s.historyPath(in.Scope)
		return s.writeJSON(p, h)
	})
	return in.Metadata, err
}

func (s *History) GetRevision(ctx context.Context, in port.GetRevisionMetadataInput) (runtimeprotocol.RevisionMetadata, error) {
	if err := ctx.Err(); err != nil {
		return runtimeprotocol.RevisionMetadata{}, err
	}
	h, err := s.loadHistory(in.Scope)
	if err != nil {
		return runtimeprotocol.RevisionMetadata{}, err
	}
	for _, m := range h.Items {
		if m.Revision.RevisionID == in.RevisionID {
			return clone(m)
		}
	}
	return runtimeprotocol.RevisionMetadata{}, port.ErrNotFound
}

func historyCursor(anchor runtimeprotocol.RevisionMetadata) runtimeprotocol.RuntimeCursor {
	raw := "layerdraw-local-history-v2:" + string(anchor.CommittedAt) + "\x00" + string(anchor.Revision.RevisionID)
	return runtimeprotocol.RuntimeCursor(base64.RawURLEncoding.EncodeToString([]byte(raw)))
}
func parseHistoryCursor(cursor *runtimeprotocol.RuntimeCursor, h historyDisk) (int, error) {
	if cursor == nil {
		return 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(string(*cursor))
	if err != nil {
		return 0, port.ErrConflict
	}
	v := string(b)
	prefix := "layerdraw-local-history-v2:"
	if !strings.HasPrefix(v, prefix) {
		return 0, port.ErrConflict
	}
	parts := strings.SplitN(strings.TrimPrefix(v, prefix), "\x00", 2)
	if len(parts) != 2 {
		return 0, port.ErrConflict
	}
	for i, m := range h.Items {
		if string(m.CommittedAt) == parts[0] && string(m.Revision.RevisionID) == parts[1] {
			return i + 1, nil
		}
	}
	return 0, port.ErrConflict
}

func (s *History) ListRevisions(ctx context.Context, in port.ListRevisionsInput) (runtimeprotocol.RevisionPage, error) {
	if err := ctx.Err(); err != nil {
		return runtimeprotocol.RevisionPage{}, err
	}
	max, err := strconv.ParseUint(string(in.MaxItems), 10, 53)
	if _, encodeErr := protocolcommon.EncodeCanonicalPositiveSafeInteger(in.MaxItems); err != nil || encodeErr != nil || max == 0 {
		return runtimeprotocol.RevisionPage{}, port.ErrConflict
	}
	maxBytes, err := strconv.ParseInt(string(in.MaxOutputBytes), 10, 64)
	if _, encodeErr := protocolcommon.EncodeCanonicalPositiveInt64(in.MaxOutputBytes); err != nil || encodeErr != nil || maxBytes < 2 {
		return runtimeprotocol.RevisionPage{}, port.ErrConflict
	}
	h, err := s.loadHistory(in.Scope)
	if err != nil {
		return runtimeprotocol.RevisionPage{}, err
	}
	off, err := parseHistoryCursor(in.Cursor, h)
	if err != nil {
		return runtimeprotocol.RevisionPage{}, err
	}
	if off > len(h.Items) {
		return runtimeprotocol.RevisionPage{}, port.ErrConflict
	}
	items := make([]runtimeprotocol.RevisionMetadata, 0)
	used := 2 // JSON array brackets are part of the deterministic Items bound.
	for i := off; i < len(h.Items) && uint64(len(items)) < max; i++ {
		b, _ := json.Marshal(h.Items[i])
		additional := len(b)
		if len(items) != 0 {
			additional++
		}
		if int64(used+additional) > maxBytes {
			break
		}
		items = append(items, h.Items[i])
		used += additional
	}
	if len(items) == 0 && off < len(h.Items) {
		return runtimeprotocol.RevisionPage{}, port.ErrConflict
	}
	nextOff := off + len(items)
	var next *string
	if nextOff < len(h.Items) {
		v := string(historyCursor(items[len(items)-1]))
		next = &v
	}
	encodedItems, _ := json.Marshal(items)
	used = len(encodedItems)
	page := protocolcommon.PageInfo{NextCursor: next, ResultTruncated: next != nil, ReturnedBytes: protocolcommon.CanonicalUint64(strconv.Itoa(used)), ReturnedItems: protocolcommon.CanonicalUint64(strconv.Itoa(len(items)))}
	return runtimeprotocol.RevisionPage{Items: items, Page: page}, nil
}

func (s *History) ResolveProviderVersion(ctx context.Context, in port.ResolveProviderVersionInput) (port.ProviderRevisionRef, error) {
	if err := ctx.Err(); err != nil {
		return port.ProviderRevisionRef{}, err
	}
	h, err := s.loadHistory(in.Scope)
	if err != nil {
		return port.ProviderRevisionRef{}, err
	}
	var found *runtimeprotocol.CommittedRevisionRef
	for _, m := range h.Items {
		if m.Revision.ProviderVersion != nil && *m.Revision.ProviderVersion == in.ProviderVersion {
			if found != nil && !reflect.DeepEqual(*found, m.Revision) {
				return port.ProviderRevisionRef{}, port.ErrIndeterminate
			}
			v := m.Revision
			found = &v
		}
	}
	if found == nil {
		return port.ProviderRevisionRef{}, port.ErrNotFound
	}
	return port.ProviderRevisionRef{Revision: *found, ProviderVersion: in.ProviderVersion}, nil
}
