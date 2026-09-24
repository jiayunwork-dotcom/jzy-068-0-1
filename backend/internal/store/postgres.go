// Package store persists the workbook to PostgreSQL 16 as a JSONB snapshot.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	_ "github.com/lib/pq"

	"collabsheet/internal/workbook"
)

// Postgres stores the single shared workbook in table collab_workbook.
type Postgres struct {
	db      *sql.DB
	bookID  string
	timeout time.Duration
}

// New opens the database, creates the schema and returns the store.
func New(dsn, bookID string) (*Postgres, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	p := &Postgres{db: db, bookID: bookID, timeout: 5 * time.Second}
	if err := p.waitReady(); err != nil {
		return nil, err
	}
	if err := p.initSchema(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Postgres) waitReady() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := p.db.PingContext(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (p *Postgres) initSchema() error {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	_, err := p.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS collab_workbook (
    id         TEXT PRIMARY KEY,
    data       JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`)
	return err
}

// Save upserts the whole workbook snapshot.
func (p *Postgres) Save(wb *workbook.Workbook) error {
	data, err := json.Marshal(wb)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	_, err = p.db.ExecContext(ctx, `
INSERT INTO collab_workbook (id, data, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, updated_at = now()`,
		p.bookID, data)
	return err
}

// Load reads the workbook snapshot; ErrNotFound is returned when empty.
var ErrNotFound = errors.New("workbook not found")

func (p *Postgres) Load() (*workbook.Workbook, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	var data []byte
	err := p.db.QueryRowContext(ctx,
		`SELECT data FROM collab_workbook WHERE id = $1`, p.bookID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var wb workbook.Workbook
	if err := json.Unmarshal(data, &wb); err != nil {
		return nil, err
	}
	return &wb, nil
}

// Close releases the connection pool.
func (p *Postgres) Close() error { return p.db.Close() }
