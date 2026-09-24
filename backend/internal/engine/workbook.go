// Package engine is the spreadsheet core: it owns the workbook model, the
// cell dependency graph, topologically ordered recalculation, circular
// reference detection and reference-preserving row/column surgery.
package engine

import (
	"sort"
	"strings"
	"sync"

	"collabsheet/internal/formula"
)

// Cell is one stored grid cell.
type Cell struct {
	Col         int
	Row         int
	Input       string // user-entered text; "" means the cell is empty
	IsFormula   bool
	AST         formula.Node // parsed formula, nil for literals
	parseFailed bool

	// Last computed value and display text.
	Value    formula.Value
	Display  string
	Circular bool
}

// Sheet holds cells in a sparse map keyed by packed coordinates.
type Sheet struct {
	ID    int
	Name  string
	Rows  int
	Cols  int
	Cells map[uint64]*Cell // keys are (col,row) local pairs
}

// Workbook is the whole document: several named sheets plus a dependency
// graph spanning all of them.
type Workbook struct {
	mu     sync.RWMutex
	ID     string
	Sheets []*Sheet
	byName map[string]*Sheet
	graph  *Graph
	nextID int
}

func NewWorkbook(id string) *Workbook {
	return &Workbook{
		ID:     id,
		byName: map[string]*Sheet{},
		graph:  NewGraph(),
	}
}

func (wb *Workbook) AddSheet(name string, rows, cols int) *Sheet {
	s := &Sheet{ID: wb.nextID, Name: name, Rows: rows, Cols: cols, Cells: map[uint64]*Cell{}}
	wb.nextID++
	wb.Sheets = append(wb.Sheets, s)
	wb.byName[name] = s
	return s
}

func (wb *Workbook) Sheet(name string) *Sheet {
	return wb.byName[name]
}

// pack combines a col/row pair into a local sheet-map key.
func pack(col, row int) uint64 { return uint64(col)<<32 | uint64(uint32(row)) }

func unpack(k uint64) (col, row int) {
	return int(k >> 32), int(k & keyRowMask)
}

// nodeKey returns the global graph key of a cell in this sheet.
func (s *Sheet) nodeKey(col, row int) uint64 { return makeKey(s.ID, col, row) }

func (s *Sheet) cell(col, row int) *Cell {
	if col < 0 || row < 0 || col >= s.Cols || row >= s.Rows {
		return nil
	}
	return s.Cells[pack(col, row)]
}

// SortedKeys returns non-empty-cell keys in (row,col) order.
func (s *Sheet) SortedKeys() []uint64 {
	keys := make([]uint64, 0, len(s.Cells))
	for k, c := range s.Cells {
		if c.Input != "" {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		ci, ri := unpack(keys[i])
		cj, rj := unpack(keys[j])
		if ri != rj {
			return ri < rj
		}
		return ci < cj
	})
	return keys
}

// Lock exposes the model lock to the collaboration layer.
func (wb *Workbook) Lock()    { wb.mu.Lock() }
func (wb *Workbook) Unlock()  { wb.mu.Unlock() }
func (wb *Workbook) RLock()   { wb.mu.RLock() }
func (wb *Workbook) RUnlock() { wb.mu.RUnlock() }

// trimInput decides whether user text denotes an empty cell.
func trimInput(in string) string { return strings.TrimSpace(in) }
