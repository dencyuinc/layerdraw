// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package search

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	lbug "github.com/LadybugDB/go-ladybug"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

// GoLadybugSession is the production native binding. Release builds use the
// ladybug_native tag and bundle the pinned Ladybug library.
type GoLadybugSession struct {
	db                       *lbug.Database
	conn                     *lbug.Connection
	databasePath, markerRoot string
	mu                       sync.Mutex
}

func OpenGoLadybugSession(databasePath, markerRoot string) (*GoLadybugSession, error) {
	if !filepath.IsAbs(databasePath) || !filepath.IsAbs(markerRoot) {
		return nil, fmt.Errorf("absolute Ladybug paths required")
	}
	if err := os.MkdirAll(markerRoot, 0o700); err != nil {
		return nil, err
	}
	db, err := lbug.OpenDatabase(databasePath, lbug.SystemConfig{})
	if err != nil {
		return nil, err
	}
	conn, err := lbug.OpenConnection(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &GoLadybugSession{db: db, conn: conn, databasePath: databasePath, markerRoot: markerRoot}, nil
}
func (s *GoLadybugSession) Close()     { s.mu.Lock(); defer s.mu.Unlock(); s.conn.Close(); s.db.Close() }
func (s *GoLadybugSession) Interrupt() { s.conn.Interrupt() }
func (s *GoLadybugSession) ExecutePrepared(ctx context.Context, statement LadybugStatement, _ port.ExecutionLimits, sink port.RowSink) error {
	prepared, err := s.conn.Prepare(statement.Query)
	if err != nil {
		return err
	}
	defer prepared.Close()
	args := map[string]any{}
	for key, value := range statement.Parameters {
		converted, err := ladybugValue(value)
		if err != nil {
			return err
		}
		args[key] = converted
	}
	result, err := s.conn.Execute(prepared, args)
	if err != nil {
		return err
	}
	defer result.Close()
	for result.HasNext() {
		select {
		case <-ctx.Done():
			s.conn.Interrupt()
			return ctx.Err()
		default:
		}
		tuple, err := result.Next()
		if err != nil {
			return err
		}
		values, err := tuple.GetAsMap()
		tuple.Close()
		if err != nil {
			return err
		}
		row := port.RawRow{}
		for key, value := range values {
			row[key] = port.RawValue{Kind: fmt.Sprintf("%T", value), Value: fmt.Sprint(value)}
		}
		if err := sink.Push(row); err != nil {
			s.conn.Interrupt()
			return err
		}
	}
	return nil
}
func ladybugValue(value port.RawValue) (any, error) {
	switch value.Kind {
	case "string":
		return value.Value, nil
	case "int64":
		return strconv.ParseInt(value.Value, 10, 64)
	case "float64":
		return strconv.ParseFloat(value.Value, 64)
	case "bool":
		return strconv.ParseBool(value.Value)
	default:
		return nil, ErrInvalidPlan
	}
}
func (s *GoLadybugSession) RecordPhysicalIndex(_ context.Context, ref port.PhysicalIndexRef) error {
	data, err := json.Marshal(ref)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.markerRoot, ref.IdentityDigest+".json"), data, 0o600)
}
func (s *GoLadybugSession) InspectIndex(_ context.Context, ref port.PhysicalIndexRef) error {
	data, err := os.ReadFile(filepath.Join(s.markerRoot, ref.IdentityDigest+".json"))
	if err != nil {
		return ErrPhysicalIndexMissing
	}
	var stored port.PhysicalIndexRef
	if json.Unmarshal(data, &stored) != nil || stored != ref {
		return ErrPhysicalIndexMissing
	}
	if _, err := os.Stat(s.databasePath); err != nil {
		return ErrPhysicalIndexMissing
	}
	return nil
}
