// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var transactionIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

// DiskTransactionStore is a durable, process-safe CAS store for Desktop and
// local hosts. Each record is replaced atomically and every state mutation is
// an append validated by validateTransactionAppend.
type DiskTransactionStore struct {
	root string
	mu   sync.Mutex
}

func NewDiskTransactionStore(root string) (*DiskTransactionStore, error) {
	if root == "" {
		return nil, errors.New("registry transaction root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	return &DiskTransactionStore{root: abs}, nil
}

func (s *DiskTransactionStore) CreateRegistryTransaction(ctx context.Context, tx Transaction) error {
	if !transactionIDPattern.MatchString(tx.Plan.TransactionID) || len(tx.Events) == 0 {
		return errors.New("invalid registry transaction record")
	}
	release, err := s.lock(ctx)
	if err != nil {
		return err
	}
	defer release()
	path := s.path(tx.Plan.TransactionID)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	data, err := json.Marshal(tx)
	if err == nil {
		_, err = file.Write(data)
	}
	if syncErr := file.Sync(); err == nil {
		err = syncErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}
func (s *DiskTransactionStore) GetRegistryTransaction(ctx context.Context, id string) (Transaction, bool, error) {
	if !transactionIDPattern.MatchString(id) {
		return Transaction{}, false, errors.New("invalid registry transaction id")
	}
	select {
	case <-ctx.Done():
		return Transaction{}, false, ctx.Err()
	default:
	}
	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return Transaction{}, false, nil
	}
	if err != nil {
		return Transaction{}, false, err
	}
	var tx Transaction
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&tx); err != nil {
		return Transaction{}, false, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Transaction{}, false, err
	}
	if tx.Plan.TransactionID != id || len(tx.Events) == 0 {
		return Transaction{}, false, errors.New("registry transaction record identity mismatch")
	}
	return cloneTransaction(tx), true, nil
}
func (s *DiskTransactionStore) CompareAndSwapRegistryTransaction(ctx context.Context, id string, expected uint64, next Transaction) (bool, error) {
	if !transactionIDPattern.MatchString(id) || next.Plan.TransactionID != id {
		return false, errors.New("invalid registry transaction id")
	}
	release, err := s.lock(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	current, ok, err := s.getUnlocked(id)
	if err != nil || !ok {
		return false, err
	}
	if transactionVersion(current) != expected {
		return false, nil
	}
	if err := validateTransactionAppend(current, next); err != nil {
		return false, err
	}
	data, err := json.Marshal(next)
	if err != nil {
		return false, err
	}
	temp, err := os.CreateTemp(s.root, "."+id+"-*")
	if err != nil {
		return false, err
	}
	tempName := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return false, err
	}
	if _, err := temp.Write(data); err != nil {
		return false, err
	}
	if err := temp.Sync(); err != nil {
		return false, err
	}
	if err := temp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tempName, s.path(id)); err != nil {
		return false, err
	}
	committed = true
	dir, err := os.Open(s.root)
	if err != nil {
		return false, err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return false, syncErr
	}
	if closeErr != nil {
		return false, closeErr
	}
	return true, nil
}
func (s *DiskTransactionStore) getUnlocked(id string) (Transaction, bool, error) {
	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return Transaction{}, false, nil
	}
	if err != nil {
		return Transaction{}, false, err
	}
	var tx Transaction
	if err := json.Unmarshal(data, &tx); err != nil {
		return Transaction{}, false, err
	}
	return tx, true, nil
}
func (s *DiskTransactionStore) path(id string) string { return filepath.Join(s.root, id+".json") }
func (s *DiskTransactionStore) lock(ctx context.Context) (func(), error) {
	s.mu.Lock()
	lockPath := filepath.Join(s.root, "transactions.lock")
	for {
		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_ = file.Close()
			return func() { _ = os.Remove(lockPath); s.mu.Unlock() }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			s.mu.Unlock()
			return nil, err
		}
		select {
		case <-ctx.Done():
			s.mu.Unlock()
			return nil, ctx.Err()
		case <-time.After(2 * time.Millisecond):
		}
	}
}
func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	}
	return errors.New("registry transaction record has trailing JSON")
}
