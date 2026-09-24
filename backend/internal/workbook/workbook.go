// Package workbook holds the in-memory document model: sheets, cells and the
// structural operations (insert/delete rows and columns) that move content
// and rewrite formulas.
package workbook

import (
	"fmt"
	"strings"

	"collabsheet/internal/formula"
)

const (
	DefaultRows = 1000
	Cols        = 26 // A..Z
)

// Cell is the raw user content of one grid cell plus optional precomputed value.
type Cell struct {
	// Raw is exactly what the user typed: "" empty, "12", "hello" or "=SUM(A1)".
	Raw string `json:"raw"`
}

// Sheet is one grid. Rows can grow beyond the default when rows are inserted.
type Sheet struct {
	Name  string           `json:"name"`
	Rows  int              `json:"rows"`
	Cols  int              `json:"cols"`
	Cells map[string]*Cell `json:"cells"`
}

func NewSheet(name string, rows int) *Sheet {
	return &Sheet{Name: name, Rows: rows, Cols: Cols, Cells: map[string]*Cell{}}
}

// Addr formats a 0-based coordinate as an A1 address.
func Addr(col, row int) string {
	return fmt.Sprintf("%s%d", ColName(col), row+1)
}

// ColName converts a 0-based column index to letters (A, B, ..., Z).
func ColName(col int) string {
	col++
	var b []byte
	for col > 0 {
		col--
		b = append([]byte{byte('A' + col%26)}, b...)
		col /= 26
	}
	return string(b)
}

// ParseAddr parses A1 / $A$1 into 0-based coordinates.
func ParseAddr(s string) (col, row int, err error) {
	s = strings.TrimPrefix(s, "$")
	i := 0
	for i < len(s) && s[i] >= 'A' && s[i] <= 'Z' {
		i++
	}
	if i == 0 {
		return 0, 0, fmt.Errorf("bad address %q", s)
	}
	colLetters := s[:i]
	rest := s[i:]
	rest = strings.TrimPrefix(rest, "$")
	v := 0
	for _, c := range colLetters {
		v = v*26 + int(c-'A'+1)
	}
	col = v - 1
	n := 0
	for _, c := range rest {
		if c < '0' || c > '9' {
			return 0, 0, fmt.Errorf("bad address %q", s)
		}
		n = n*10 + int(c-'0')
	}
	if n == 0 {
		return 0, 0, fmt.Errorf("bad address %q", s)
	}
	return col, n - 1, nil
}

// Workbook is the whole document.
type Workbook struct {
	Sheets []*Sheet `json:"sheets"`
}

func New() *Workbook { return &Workbook{} }

// SheetByName returns the sheet or nil.
func (wb *Workbook) SheetByName(name string) *Sheet {
	for _, s := range wb.Sheets {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// SetRaw sets raw content of a cell (empty string deletes it).
func (s *Sheet) SetRaw(col, row int, raw string) {
	a := Addr(col, row)
	if strings.TrimSpace(raw) == "" {
		delete(s.Cells, a)
		return
	}
	s.Cells[a] = &Cell{Raw: raw}
}

// Get returns the cell at coordinates or nil.
func (s *Sheet) Get(col, row int) *Cell {
	return s.Cells[Addr(col, row)]
}

// InsertRows inserts count rows before the 0-based index, shifting content
// down and rewriting formulas across the whole workbook.
func (wb *Workbook) InsertRows(sheetName string, index, count int) error {
	s := wb.SheetByName(sheetName)
	if s == nil {
		return fmt.Errorf("no such sheet: %s", sheetName)
	}
	if index < 0 || index > s.Rows {
		return fmt.Errorf("row index out of range: %d", index)
	}
	wb.shiftCells(s, formula.AxisRow, index, count, false)
	s.Rows += count
	wb.rewriteFormulas(formula.Shift{
		Axis: formula.AxisRow, TargetSheet: sheetName, Index: index, Count: count, Delete: false,
	})
	return nil
}

// DeleteRows removes count rows starting at the 0-based index.
func (wb *Workbook) DeleteRows(sheetName string, index, count int) error {
	s := wb.SheetByName(sheetName)
	if s == nil {
		return fmt.Errorf("no such sheet: %s", sheetName)
	}
	if index < 0 || index+count > s.Rows || count <= 0 {
		return fmt.Errorf("bad delete rows range")
	}
	wb.shiftCells(s, formula.AxisRow, index, count, true)
	s.Rows -= count
	wb.rewriteFormulas(formula.Shift{
		Axis: formula.AxisRow, TargetSheet: sheetName, Index: index, Count: count, Delete: true,
	})
	return nil
}

// InsertCols inserts count columns before index.
func (wb *Workbook) InsertCols(sheetName string, index, count int) error {
	s := wb.SheetByName(sheetName)
	if s == nil {
		return fmt.Errorf("no such sheet: %s", sheetName)
	}
	if index < 0 || index > s.Cols {
		return fmt.Errorf("col index out of range: %d", index)
	}
	wb.shiftCells(s, formula.AxisCol, index, count, false)
	s.Cols += count
	wb.rewriteFormulas(formula.Shift{
		Axis: formula.AxisCol, TargetSheet: sheetName, Index: index, Count: count, Delete: false,
	})
	return nil
}

// DeleteCols removes count columns starting at index.
func (wb *Workbook) DeleteCols(sheetName string, index, count int) error {
	s := wb.SheetByName(sheetName)
	if s == nil {
		return fmt.Errorf("no such sheet: %s", sheetName)
	}
	if index < 0 || index+count > s.Cols || count <= 0 {
		return fmt.Errorf("bad delete cols range")
	}
	wb.shiftCells(s, formula.AxisCol, index, count, true)
	s.Cols -= count
	wb.rewriteFormulas(formula.Shift{
		Axis: formula.AxisCol, TargetSheet: sheetName, Index: index, Count: count, Delete: true,
	})
	return nil
}

// shiftCells moves raw cell content on one sheet. Iteration order is chosen
// so moved cells never overwrite not-yet-moved content.
func (wb *Workbook) shiftCells(s *Sheet, axis formula.Axis, index, count int, del bool) {
	next := map[string]*Cell{}
	for a, c := range s.Cells {
		col, row, err := ParseAddr(a)
		if err != nil {
			next[a] = c
			continue
		}
		if axis == formula.AxisRow {
			nv, removed := formula.ShiftIndex(row, index, count, del)
			if removed {
				continue
			}
			row = nv
		} else {
			nv, removed := formula.ShiftIndex(col, index, count, del)
			if removed {
				continue
			}
			col = nv
		}
		next[Addr(col, row)] = c
	}
	s.Cells = next
}

// rewriteFormulas parses every formula in the workbook, adjusts its references
// and writes canonical formula text back. Unparseable formulas are untouched.
func (wb *Workbook) rewriteFormulas(op formula.Shift) {
	for _, sh := range wb.Sheets {
		for a, c := range sh.Cells {
			if !strings.HasPrefix(c.Raw, "=") {
				continue
			}
			ast, err := formula.Parse(c.Raw)
			if err != nil {
				continue
			}
			ast = formula.AdjustReferences(ast, sh.Name, op)
			c.Raw = "=" + ast.String()
			sh.Cells[a] = c
		}
	}
}
