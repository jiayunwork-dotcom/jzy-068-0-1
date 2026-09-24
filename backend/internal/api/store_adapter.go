package api

import (
	"collabsheet/internal/collab"
	"collabsheet/internal/engine"
	"collabsheet/internal/storage"
)

// PgStoreAdapter narrows the PostgreSQL store to the collab.Store interface,
// binding every write to the currently served workbook.
type PgStoreAdapter struct {
	Store  *storage.Store
	BookID string
	WB     *engine.Workbook
}

var _ collab.Store = (*PgStoreAdapter)(nil)

func (a *PgStoreAdapter) WriteCell(sheet string, col, row int, value string) error {
	return a.Store.WriteCellWB(a.BookID, sheet, col, row, value)
}

func (a *PgStoreAdapter) WriteVersion(v int64) error {
	return a.Store.WriteVersionWB(a.BookID, v)
}

func (a *PgStoreAdapter) ReplaceWorkbook(version int64) error {
	return a.Store.ReplaceAll(a.BookID, a.WB, version)
}
