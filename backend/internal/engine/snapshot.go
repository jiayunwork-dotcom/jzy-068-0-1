package engine

import (
	"strings"

	"collabsheet/internal/formula"
)

// setInputLocked parses and stores one cell and wires its dependency edges
// without recalculating. Used by the seeder and by persistence loaders; a
// RecalcAll must follow once all cells are in place.
func (wb *Workbook) setInputLocked(s *Sheet, col, row int, input string) {
	input = strings.TrimSpace(input)
	key := pack(col, row)
	if input == "" {
		return
	}
	c := &Cell{Col: col, Row: row, Input: input}
	s.Cells[key] = c
	c.IsFormula = strings.HasPrefix(input, "=")
	if c.IsFormula {
		ast, err := formula.Parse(input)
		if err != nil {
			c.parseFailed = true
			c.IsFormula = false
			c.Value = formula.ErrorValue(formula.ErrParse)
			c.Display = string(formula.ErrParse)
			return
		}
		c.AST = ast
	} else {
		c.Value = literalValue(input)
		c.Display = c.Value.Display()
	}
}

// LoadCell is the persistence-facing variant; it also wires graph edges so a
// sequence of LoadCell + RecalcAll restores a workbook exactly.
func (wb *Workbook) LoadCell(sheetName string, col, row int, input string) {
	s := wb.Sheet(sheetName)
	if s == nil {
		return
	}
	wb.setInputLocked(s, col, row, input)
}

// FinalizeLoad rebuilds the graph and recomputes everything after loading.
func (wb *Workbook) FinalizeLoad() {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	wb.rebuildGraph()
	wb.recalcAllLocked()
}

// SheetSnapshot is the serializable description of one sheet sent to clients.
type SheetSnapshot struct {
	Name  string         `json:"name"`
	Rows  int            `json:"rows"`
	Cols  int            `json:"cols"`
	Cells []CellSnapshot `json:"cells"`
}

// CellSnapshot carries both the raw input and the computed display state.
type CellSnapshot struct {
	Col       int     `json:"col"`
	Row       int     `json:"row"`
	Input     string  `json:"input"`
	IsFormula bool    `json:"isFormula"`
	Display   string  `json:"display"`
	Kind      string  `json:"kind"`
	Num       float64 `json:"num,omitempty"`
	Str       string  `json:"str,omitempty"`
	Bool      bool    `json:"bool,omitempty"`
	Error     string  `json:"error,omitempty"`
	Circular  bool    `json:"circular,omitempty"`
}

// InputAt returns the raw stored formula/text of one cell ("" if empty).
func (wb *Workbook) InputAt(sheetName string, col, row int) string {
	wb.mu.RLock()
	defer wb.mu.RUnlock()
	s := wb.Sheet(sheetName)
	if s == nil {
		return ""
	}
	if c := s.cell(col, row); c != nil {
		return c.Input
	}
	return ""
}

// SheetDims returns the (rows, cols) size of a sheet (0,0 if unknown).
func (wb *Workbook) SheetDims(sheetName string) (int, int) {
	wb.mu.RLock()
	defer wb.mu.RUnlock()
	if s := wb.Sheet(sheetName); s != nil {
		return s.Rows, s.Cols
	}
	return 0, 0
}

// Snapshot serializes the whole workbook.
func (wb *Workbook) Snapshot() []SheetSnapshot {
	wb.mu.RLock()
	defer wb.mu.RUnlock()
	out := make([]SheetSnapshot, 0, len(wb.Sheets))
	for _, s := range wb.Sheets {
		snap := SheetSnapshot{Name: s.Name, Rows: s.Rows, Cols: s.Cols}
		for _, k := range s.SortedKeys() {
			col, row := unpack(k)
			c := s.Cells[k]
			cs := CellSnapshot{
				Col: col, Row: row, Input: c.Input, IsFormula: c.IsFormula,
				Display: c.Display, Circular: c.Circular,
			}
			switch c.Value.Kind {
			case formula.VNumber:
				cs.Kind = "number"
				cs.Num = c.Value.Num
			case formula.VString:
				cs.Kind = "string"
				cs.Str = c.Value.Str
			case formula.VBoolean:
				cs.Kind = "boolean"
				cs.Bool = c.Value.Bool
			case formula.VError:
				cs.Kind = "error"
				cs.Error = string(c.Value.Error)
			default:
				cs.Kind = "blank"
			}
			snap.Cells = append(snap.Cells, cs)
		}
		out = append(out, snap)
	}
	return out
}
