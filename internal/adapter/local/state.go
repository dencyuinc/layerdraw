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
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type leaseDisk struct {
	Lease   port.StateLease `json:"lease"`
	OwnerID string          `json:"owner_id"`
}
type auditDisk struct {
	Ref         port.AuditEventRef          `json:"ref"`
	OperationID runtimeprotocol.OperationID `json:"operation_id"`
	Event       protocolcommon.BlobRef      `json:"event"`
}
type stateDisk struct {
	Head        port.StateHead     `json:"head"`
	Snapshot    port.StateSnapshot `json:"snapshot"`
	Lease       *leaseDisk         `json:"lease,omitempty"`
	NextFencing uint64             `json:"next_fencing"`
	Audits      []auditDisk        `json:"audits"`
}

func (s *State) statePath(scope runtimeprotocol.RuntimeScope) (string, error) {
	d, e := s.scopeDir(scope)
	return filepath.Join(d, "state", "current.json"), e
}
func (s *State) loadState(scope runtimeprotocol.RuntimeScope) (stateDisk, error) {
	p, e := s.statePath(scope)
	if e != nil {
		return stateDisk{}, e
	}
	var d stateDisk
	if e = s.readJSON(p, &d); e != nil {
		return d, e
	}
	if !reflect.DeepEqual(d.Head, d.Snapshot.Head) {
		return d, port.ErrIndeterminate
	}
	if !validStateHead(d.Head) || !validStateSnapshot(d.Snapshot) {
		return d, invalidPersisted("state")
	}
	headVersion, _ := parseNN(d.Head.StateVersion)
	auditOps := map[runtimeprotocol.OperationID]bool{}
	auditIDs := map[string]bool{}
	for _, a := range d.Audits {
		auditVersion, e := parseNN(a.Ref.StateVersion)
		if !validAudit(a) || e != nil || auditVersion > headVersion || auditOps[a.OperationID] || auditIDs[a.Ref.EventID] {
			return d, invalidPersisted("state audit")
		}
		auditOps[a.OperationID] = true
		auditIDs[a.Ref.EventID] = true
	}
	if d.Lease != nil {
		if _, e := safeID(string(d.Lease.Lease.LeaseToken)); e != nil || d.Lease.OwnerID == "" {
			return d, invalidPersisted("state lease")
		}
		if _, e := parseUint(d.Lease.Lease.FencingToken); e != nil {
			return d, invalidPersisted("state lease")
		}
		fencing, _ := parseUint(d.Lease.Lease.FencingToken)
		if d.Lease.Lease.ExpiresAt.IsZero() || fencing > d.NextFencing {
			return d, invalidPersisted("state lease")
		}
	}
	return d, nil
}
func (s *State) saveState(scope runtimeprotocol.RuntimeScope, d stateDisk) error {
	p, e := s.statePath(scope)
	if e != nil {
		return e
	}
	return s.writeJSON(p, d)
}

func (s *State) GetHead(ctx context.Context, in port.GetStateHeadInput) (port.StateHead, error) {
	if err := ctx.Err(); err != nil {
		return port.StateHead{}, err
	}
	d, e := s.loadState(in.Scope)
	if e != nil {
		return port.StateHead{}, e
	}
	return clone(d.Head)
}
func (s *State) ReadState(ctx context.Context, in port.ReadStateInput) (port.StateSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return port.StateSnapshot{}, err
	}
	d, e := s.loadState(in.Scope)
	if e != nil {
		return port.StateSnapshot{}, e
	}
	if in.ExpectedStateVersion != nil && *in.ExpectedStateVersion != d.Head.StateVersion {
		return port.StateSnapshot{}, port.ErrConflict
	}
	return clone(d.Snapshot)
}

func leaseActive(l *leaseDisk, now time.Time) bool { return l != nil && now.Before(l.Lease.ExpiresAt) }
func (s *State) validateLeaseDisk(d stateDisk, token runtimeprotocol.LeaseToken) (port.StateLease, error) {
	if !leaseActive(d.Lease, s.now()) || d.Lease.Lease.LeaseToken != token {
		return port.StateLease{}, port.ErrConflict
	}
	return d.Lease.Lease, nil
}

func (s *State) AcquireLease(ctx context.Context, in port.AcquireLeaseInput) (port.StateLease, error) {
	if err := ctx.Err(); err != nil {
		return port.StateLease{}, err
	}
	if in.OwnerID == "" || len(in.OwnerID) > 256 || strings.ContainsAny(in.OwnerID, "/\\\x00") || in.TTL <= 0 {
		return port.StateLease{}, port.ErrConflict
	}
	var out port.StateLease
	err := s.withLock(in.Scope, func(_ string) error {
		d, e := s.loadState(in.Scope)
		if e != nil {
			return e
		}
		if leaseActive(d.Lease, s.now()) {
			return port.ErrConflict
		}
		token, e := randomToken(s.random, "lease_")
		if e != nil {
			return e
		}
		d.NextFencing++
		out = port.StateLease{LeaseToken: runtimeprotocol.LeaseToken(token), FencingToken: protocolcommon.CanonicalUint64(strconv.FormatUint(d.NextFencing, 10)), ExpiresAt: s.now().Add(in.TTL)}
		d.Lease = &leaseDisk{Lease: out, OwnerID: in.OwnerID}
		return s.saveState(in.Scope, d)
	})
	return cloneResult(out, err)
}
func (s *State) RenewLease(ctx context.Context, in port.RenewLeaseInput) (port.StateLease, error) {
	if err := ctx.Err(); err != nil {
		return port.StateLease{}, err
	}
	if in.TTL <= 0 {
		return port.StateLease{}, port.ErrConflict
	}
	var out port.StateLease
	err := s.withLock(in.Scope, func(_ string) error {
		d, e := s.loadState(in.Scope)
		if e != nil {
			return e
		}
		if _, e = s.validateLeaseDisk(d, in.LeaseToken); e != nil {
			return e
		}
		d.Lease.Lease.ExpiresAt = s.now().Add(in.TTL)
		out = d.Lease.Lease
		return s.saveState(in.Scope, d)
	})
	return cloneResult(out, err)
}
func (s *State) ReleaseLease(ctx context.Context, in port.ReleaseLeaseInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.withLock(in.Scope, func(_ string) error {
		d, e := s.loadState(in.Scope)
		if e != nil {
			return e
		}
		if _, e = s.validateLeaseDisk(d, in.LeaseToken); e != nil {
			return e
		}
		d.Lease = nil
		return s.saveState(in.Scope, d)
	})
}
func (s *State) ValidateLease(ctx context.Context, in port.ValidateLeaseInput) (port.StateLease, error) {
	if err := ctx.Err(); err != nil {
		return port.StateLease{}, err
	}
	d, e := s.loadState(in.Scope)
	if e != nil {
		return port.StateLease{}, e
	}
	return s.validateLeaseDisk(d, in.LeaseToken)
}

func (s *State) WriteState(ctx context.Context, in port.WriteStateInput) (port.StateWriteResult, error) {
	if err := ctx.Err(); err != nil {
		return port.StateWriteResult{}, err
	}
	cloned, cloneErr := clone(in)
	if cloneErr != nil {
		return port.StateWriteResult{}, cloneErr
	}
	in = cloned
	if _, e := runtimeprotocol.EncodeRuntimeScope(in.Scope); e != nil {
		return port.StateWriteResult{}, port.ErrConflict
	}
	if _, e := runtimeprotocol.EncodeStateMutation(in.Mutation); e != nil {
		return port.StateWriteResult{}, port.ErrConflict
	}
	if _, e := runtimeprotocol.EncodeOperationID(in.OperationID); e != nil {
		return port.StateWriteResult{}, port.ErrConflict
	}
	if _, e := runtimeprotocol.EncodeIdempotencyKey(in.IdempotencyKey); e != nil {
		return port.StateWriteResult{}, port.ErrConflict
	}
	if in.Mutation.ExpectedStateVersion != in.ExpectedStateVersion || !reflect.DeepEqual(in.Mutation.MutationBlob.Scope, in.Scope) || !validDigest(in.Mutation.MutationDigest) || !validBlobRef(in.Mutation.MutationBlob.Blob) {
		return port.StateWriteResult{}, port.ErrConflict
	}
	if _, e := parseNN(in.ExpectedStateVersion); e != nil || in.ExpectedBackendVersion == "" || !validDigest(in.ExpectedDefinitionHash) {
		return port.StateWriteResult{}, port.ErrConflict
	}
	if _, e := safeID(string(in.LeaseToken)); e != nil {
		return port.StateWriteResult{}, port.ErrConflict
	}
	for a, d := range in.ExpectedSubjectHashes {
		if _, e := semantic.EncodeStableAddress(a); e != nil || !validDigest(d) {
			return port.StateWriteResult{}, port.ErrConflict
		}
	}
	seenAffected := map[semantic.StableAddress]bool{}
	var prior semantic.StableAddress
	for i, a := range in.Mutation.AffectedSubjects {
		if _, e := semantic.EncodeStableAddress(a); e != nil || seenAffected[a] || (i > 0 && string(prior) >= string(a)) {
			return port.StateWriteResult{}, port.ErrConflict
		}
		seenAffected[a] = true
		prior = a
	}
	var out port.StateWriteResult
	err := s.withLock(in.Scope, func(_ string) error {
		d, e := s.loadState(in.Scope)
		if e != nil {
			return e
		}
		if _, e = s.validateLeaseDisk(d, in.LeaseToken); e != nil {
			return e
		}
		if d.Head.StateVersion != in.ExpectedStateVersion || d.Head.BackendVersion != in.ExpectedBackendVersion || d.Head.DefinitionHash != in.ExpectedDefinitionHash || !reflect.DeepEqual(d.Head.SubjectHashes, in.ExpectedSubjectHashes) {
			return port.ErrConflict
		}
		v, e := parseNN(d.Head.StateVersion)
		if e != nil {
			return port.ErrIndeterminate
		}
		backend, e := strconv.ParseUint(string(d.Head.BackendVersion), 10, 64)
		if e != nil {
			return port.ErrIndeterminate
		}
		d.Head.StateVersion = protocolcommon.CanonicalNonNegativeInt64(strconv.FormatUint(v+1, 10))
		d.Head.BackendVersion = runtimeprotocol.ProviderVersionToken(strconv.FormatUint(backend+1, 10))
		d.Head.CapturedAt = protocolcommon.Rfc3339Time(s.now().UTC().Format(time.RFC3339Nano))
		if d.Head.SubjectHashes == nil {
			d.Head.SubjectHashes = map[semantic.StableAddress]protocolcommon.Digest{}
		}
		for _, a := range in.Mutation.AffectedSubjects {
			d.Head.SubjectHashes[a] = in.Mutation.MutationDigest
		}
		for i := range d.Snapshot.Records {
			if seenAffected[d.Snapshot.Records[i].SubjectAddress] {
				d.Snapshot.Records[i].OwnSubjectHash = in.Mutation.MutationDigest
			}
		}
		d.Snapshot.Head = d.Head
		d.Snapshot.Contents = in.Mutation.MutationBlob.Blob
		d.Snapshot.Records = append([]port.StateRecord(nil), d.Snapshot.Records...)
		out.Head = d.Head
		if e = s.writeStateSnapshot(in.Scope, d.Snapshot); e != nil {
			return e
		}
		return s.saveState(in.Scope, d)
	})
	return out, err
}
func (s *State) writeStateSnapshot(scope runtimeprotocol.RuntimeScope, snap port.StateSnapshot) error {
	d, e := s.scopeDir(scope)
	if e != nil {
		return e
	}
	id, e := safeID(string(snap.Head.StateVersion))
	if e != nil {
		return e
	}
	if !validStateSnapshot(snap) {
		return port.ErrConflict
	}
	p := filepath.Join(d, "state", "snapshots", id+".json")
	var existing port.StateSnapshot
	if err := s.readJSON(p, &existing); err == nil {
		if reflect.DeepEqual(existing, snap) {
			return nil
		}
		return port.ErrConflict
	} else if !errors.Is(err, port.ErrNotFound) {
		return err
	}
	return s.writeJSON(p, snap)
}

func (s *State) AppendAuditEvent(ctx context.Context, in port.AppendAuditEventInput) (port.AuditEventRef, error) {
	if err := ctx.Err(); err != nil {
		return port.AuditEventRef{}, err
	}
	if !validDigest(in.EventDigest) || in.Event.Digest != in.EventDigest || !validBlobRef(in.Event) {
		return port.AuditEventRef{}, port.ErrConflict
	}
	if _, e := runtimeprotocol.EncodeOperationID(in.OperationID); e != nil {
		return port.AuditEventRef{}, port.ErrConflict
	}
	if _, e := parseNN(in.ExpectedStateVersion); e != nil {
		return port.AuditEventRef{}, port.ErrConflict
	}
	var out port.AuditEventRef
	err := s.withLock(in.Scope, func(_ string) error {
		d, e := s.loadState(in.Scope)
		if e != nil {
			return e
		}
		if d.Head.StateVersion != in.ExpectedStateVersion {
			return port.ErrConflict
		}
		for _, a := range d.Audits {
			if a.OperationID == in.OperationID {
				if a.Ref.EventDigest == in.EventDigest && reflect.DeepEqual(a.Event, in.Event) {
					out = a.Ref
					return nil
				}
				return port.ErrConflict
			}
		}
		id, e := safeID(strconv.Itoa(len(in.OperationID)) + ":" + string(in.OperationID) + ":" + string(in.EventDigest))
		if e != nil {
			return e
		}
		out = port.AuditEventRef{EventID: "audit_" + id[:32], EventDigest: in.EventDigest, StateVersion: in.ExpectedStateVersion}
		d.Audits = append(d.Audits, auditDisk{Ref: out, OperationID: in.OperationID, Event: in.Event})
		sort.Slice(d.Audits, func(i, j int) bool {
			vi, _ := parseNN(d.Audits[i].Ref.StateVersion)
			vj, _ := parseNN(d.Audits[j].Ref.StateVersion)
			if vi != vj {
				return vi < vj
			}
			return d.Audits[i].Ref.EventID < d.Audits[j].Ref.EventID
		})
		return s.saveState(in.Scope, d)
	})
	return out, err
}

func auditCursor(anchor port.AuditEventRef) runtimeprotocol.RuntimeCursor {
	raw := "layerdraw-local-audit-v2:" + string(anchor.StateVersion) + "\x00" + anchor.EventID
	return runtimeprotocol.RuntimeCursor(base64.RawURLEncoding.EncodeToString([]byte(raw)))
}
func parseAuditCursor(c *runtimeprotocol.RuntimeCursor, audits []auditDisk) (int, error) {
	if c == nil {
		return 0, nil
	}
	b, e := base64.RawURLEncoding.DecodeString(string(*c))
	if e != nil {
		return 0, port.ErrConflict
	}
	v := string(b)
	prefix := "layerdraw-local-audit-v2:"
	if !strings.HasPrefix(v, prefix) {
		return 0, port.ErrConflict
	}
	parts := strings.SplitN(strings.TrimPrefix(v, prefix), "\x00", 2)
	if len(parts) != 2 {
		return 0, port.ErrConflict
	}
	for i, a := range audits {
		if string(a.Ref.StateVersion) == parts[0] && a.Ref.EventID == parts[1] {
			return i + 1, nil
		}
	}
	return 0, port.ErrConflict
}
func parseAuditMaxItems(value protocolcommon.CanonicalPositiveSafeInteger) (int, error) {
	max, err := strconv.ParseInt(string(value), 10, strconv.IntSize)
	if _, encodeErr := protocolcommon.EncodeCanonicalPositiveSafeInteger(value); err != nil || encodeErr != nil || max == 0 {
		return 0, port.ErrConflict
	}
	return int(max), nil
}

func (s *State) ListAuditEvents(ctx context.Context, in port.ListAuditEventsInput) (port.AuditEventPage, error) {
	if err := ctx.Err(); err != nil {
		return port.AuditEventPage{}, err
	}
	max, e := parseAuditMaxItems(in.MaxItems)
	if e != nil {
		return port.AuditEventPage{}, e
	}
	d, e := s.loadState(in.Scope)
	if e != nil {
		return port.AuditEventPage{}, e
	}
	off, e := parseAuditCursor(in.Cursor, d.Audits)
	if e != nil {
		return port.AuditEventPage{}, e
	}
	if off > len(d.Audits) {
		return port.AuditEventPage{}, port.ErrConflict
	}
	end := len(d.Audits)
	if max < len(d.Audits)-off {
		end = off + max
	}
	items := make([]port.AuditEventRef, end-off)
	for i := range items {
		items[i] = d.Audits[off+i].Ref
	}
	b, _ := json.Marshal(items)
	var next *string
	if end < len(d.Audits) {
		v := string(auditCursor(items[len(items)-1]))
		next = &v
	}
	return port.AuditEventPage{Items: items, Page: protocolcommon.PageInfo{NextCursor: next, ResultTruncated: next != nil, ReturnedBytes: protocolcommon.CanonicalUint64(strconv.Itoa(len(b))), ReturnedItems: protocolcommon.CanonicalUint64(strconv.Itoa(len(items)))}}, nil
}

func (s *State) ExportSnapshot(ctx context.Context, in port.ExportStateSnapshotInput) (port.StateSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return port.StateSnapshot{}, err
	}
	current, e := s.loadState(in.Scope)
	if e != nil {
		return port.StateSnapshot{}, e
	}
	requested, e := parseNN(in.StateVersion)
	if e != nil {
		return port.StateSnapshot{}, port.ErrConflict
	}
	committed, e := parseNN(current.Head.StateVersion)
	if e != nil {
		return port.StateSnapshot{}, port.ErrIndeterminate
	}
	if requested > committed {
		return port.StateSnapshot{}, port.ErrNotFound
	}
	d, e := s.scopeDir(in.Scope)
	if e != nil {
		return port.StateSnapshot{}, e
	}
	id, e := safeID(string(in.StateVersion))
	if e != nil {
		return port.StateSnapshot{}, e
	}
	var snap port.StateSnapshot
	if e = s.readJSON(filepath.Join(d, "state", "snapshots", id+".json"), &snap); e != nil {
		return snap, e
	}
	if snap.Head.StateVersion != in.StateVersion {
		return snap, port.ErrIndeterminate
	}
	if !validStateSnapshot(snap) {
		return snap, invalidPersisted("state snapshot")
	}
	return clone(snap)
}

// InitializeState bootstraps a backend before Runtime opens the document.
func (s *State) InitializeState(ctx context.Context, scope runtimeprotocol.RuntimeScope, snapshot port.StateSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cloned, err := clone(snapshot)
	if err != nil {
		return err
	}
	snapshot = cloned
	if _, e := runtimeprotocol.EncodeRuntimeScope(scope); e != nil || !validStateSnapshot(snapshot) {
		return port.ErrConflict
	}
	return s.withLock(scope, func(_ string) error {
		p, _ := s.statePath(scope)
		var existing stateDisk
		if e := s.readJSON(p, &existing); e == nil {
			return port.ErrConflict
		} else if !errors.Is(e, port.ErrNotFound) {
			return e
		}
		d := stateDisk{Head: snapshot.Head, Snapshot: snapshot, Audits: []auditDisk{}}
		if e := s.writeStateSnapshot(scope, snapshot); e != nil {
			return e
		}
		return s.saveState(scope, d)
	})
}
