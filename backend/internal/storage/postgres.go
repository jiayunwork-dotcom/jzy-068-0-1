// Package storage persists workbook cells and document metadata to
// PostgreSQL 16. The dependency graph and computed values are derived at load
// time, so only raw cell inputs (and sheet shapes) need storing.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"collabsheet/internal/engine"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// defaultWorkbook is the id of the single document served by this app.
const defaultWorkbook = "default"

// Store is the PostgreSQL-backed persistence layer.
type Store struct {
	db *sql.DB
}

// Open connects to Postgres and ensures the schema exists.
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	s := &Store{db: db}
	if err := s.waitReady(ctx); err != nil {
		return nil, err
	}
	if err := s.migrate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) waitReady(ctx context.Context) error {
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		if err := s.db.PingContext(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("postgres not reachable: %w", lastErr)
}

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS workbooks (
			id TEXT PRIMARY KEY,
			version BIGINT NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS sheets (
			workbook_id TEXT NOT NULL REFERENCES workbooks(id) ON DELETE CASCADE,
			sheet_index INT NOT NULL,
			name TEXT NOT NULL,
			rows INT NOT NULL,
			cols INT NOT NULL,
			PRIMARY KEY (workbook_id, sheet_index)
		)`,
		`CREATE TABLE IF NOT EXISTS cells (
			workbook_id TEXT NOT NULL,
			sheet_index INT NOT NULL,
			col INT NOT NULL,
			row INT NOT NULL,
			input TEXT NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (workbook_id, sheet_index, col, row)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cells_sheet ON cells(workbook_id, sheet_index)`,
	}
	for _, q := range stmts {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// LoadWorkbook reads a workbook by id. If nothing is stored yet and seed is
// non-nil, seed is persisted first (first-ever launch).
func (s *Store) LoadWorkbook(ctx context.Context, id string, seed *engine.Workbook) (*engine.Workbook, int64, error) {
	var version int64
	err := s.db.QueryRowContext(ctx,
		`SELECT version FROM workbooks WHERE id=$1`, id).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		if seed == nil {
			return nil, 0, nil
		}
		if err := s.replaceWorkbook(ctx, id, seed, 0); err != nil {
			return nil, 0, err
		}
		wb := engine.NewWorkbook(id)
		if err := s.loadInto(ctx, id, wb); err != nil {
			return nil, 0, err
		}
		wb.FinalizeLoad()
		return wb, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	wb := engine.NewWorkbook(id)
	if err := s.loadInto(ctx, id, wb); err != nil {
		return nil, 0, err
	}
	wb.FinalizeLoad()
	return wb, version, nil
}

func (s *Store) loadInto(ctx context.Context, id string, wb *engine.Workbook) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT sheet_index, name, rows, cols FROM sheets
		 WHERE workbook_id=$1 ORDER BY sheet_index`, id)
	if err != nil {
		return err
	}
	type sheetRow struct {
		index      int
		name       string
		rows, cols int
	}
	var sheetRows []sheetRow
	for rows.Next() {
		var sr sheetRow
		if err := rows.Scan(&sr.index, &sr.name, &sr.rows, &sr.cols); err != nil {
			rows.Close()
			return err
		}
		sheetRows = append(sheetRows, sr)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(sheetRows) == 0 {
		return nil
	}
	for _, sr := range sheetRows {
		wb.AddSheet(sr.name, sr.rows, sr.cols)
	}
	cr, err := s.db.QueryContext(ctx,
		`SELECT sheet_index, col, row, input FROM cells
		 WHERE workbook_id=$1 ORDER BY sheet_index, row, col`, id)
	if err != nil {
		return err
	}
	defer cr.Close()
	names := make([]string, len(sheetRows))
	for _, sr := range sheetRows {
		names[sr.index] = sr.name
	}
	for cr.Next() {
		var idx, col, row int
		var input string
		if err := cr.Scan(&idx, &col, &row, &input); err != nil {
			return err
		}
		wb.LoadCell(names[idx], col, row, input)
	}
	return cr.Err()
}

// replaceWorkbook wipes and rewrites every row for a workbook (used at seed
// time and, if desired, after structural edits).
func (s *Store) replaceWorkbook(ctx context.Context, id string, wb *engine.Workbook, version int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO workbooks(id, version) VALUES($1,$2)
		 ON CONFLICT (id) DO UPDATE SET version=$2, updated_at=now()`,
		id, version); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cells WHERE workbook_id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sheets WHERE workbook_id=$1`, id); err != nil {
		return err
	}
	for i, sheet := range wb.Snapshot() {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO sheets(workbook_id, sheet_index, name, rows, cols)
			 VALUES($1,$2,$3,$4,$5)`, id, i, sheet.Name, sheet.Rows, sheet.Cols); err != nil {
			return err
		}
		for _, c := range sheet.Cells {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO cells(workbook_id, sheet_index, col, row, input)
				 VALUES($1,$2,$3,$4,$5)`,
				id, i, c.Col, c.Row, c.Input); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// WriteCell upserts one cell. An empty input deletes the row.
func (s *Store) WriteCell(sheetName string, col, row int, input string) error {
	return s.WriteCellWB(defaultWorkbook, sheetName, col, row, input)
}

// WriteCellWB is the workbook-aware variant used by the server.
func (s *Store) WriteCellWB(wbID, sheetName string, col, row int, input string) error {
	ctx := context.Background()
	idx, err := s.sheetIndex(ctx, wbID, sheetName)
	if err != nil {
		return err
	}
	if input == "" {
		_, err = s.db.ExecContext(ctx,
			`DELETE FROM cells WHERE workbook_id=$1 AND sheet_index=$2 AND col=$3 AND row=$4`,
			wbID, idx, col, row)
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO cells(workbook_id, sheet_index, col, row, input)
		 VALUES($1,$2,$3,$4,$5)
		 ON CONFLICT (workbook_id, sheet_index, col, row)
		 DO UPDATE SET input=$5, updated_at=now()`,
		wbID, idx, col, row, input)
	return err
}

func (s *Store) sheetIndex(ctx context.Context, wbID, name string) (int, error) {
	var idx int
	err := s.db.QueryRowContext(ctx,
		`SELECT sheet_index FROM sheets WHERE workbook_id=$1 AND name=$2`,
		wbID, name).Scan(&idx)
	return idx, err
}

// WriteVersion bumps the workbook version counter.
func (s *Store) WriteVersion(v int64) error {
	return s.WriteVersionWB(defaultWorkbook, v)
}

// WriteVersionWB is the workbook-aware variant.
func (s *Store) WriteVersionWB(wbID string, v int64) error {
	_, err := s.db.ExecContext(context.Background(),
		`UPDATE workbooks SET version=$2, updated_at=now() WHERE id=$1`, wbID, v)
	return err
}

// ReplaceAll persists a full workbook snapshot after a structural edit.
func (s *Store) ReplaceAll(wbID string, wb *engine.Workbook, version int64) error {
	return s.replaceWorkbook(context.Background(), wbID, wb, version)
}

// DB exposes the underlying handle for health checks.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the pool.
func (s *Store) Close() error { return s.db.Close() }
