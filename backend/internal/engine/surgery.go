package engine

import "collabsheet/internal/formula"

// Structural edits ("sheet surgery") move stored cells to their new
// coordinates and rewrite every formula in every sheet via formula.ShiftAST.
// Afterwards the dependency graph is rebuilt and a full recalculation runs.

func (wb *Workbook) InsertRow(sheetName string, index int) []CellChange {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	s := wb.Sheet(sheetName)
	if s == nil || index < 0 || index > s.Rows {
		return nil
	}
	wb.rowSurgeryLocked(s, index, true)
	return wb.recalcAllLocked()
}

func (wb *Workbook) DeleteRow(sheetName string, index int) []CellChange {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	s := wb.Sheet(sheetName)
	if s == nil || index < 0 || index >= s.Rows {
		return nil
	}
	wb.rowSurgeryLocked(s, index, false)
	return wb.recalcAllLocked()
}

func (wb *Workbook) InsertCol(sheetName string, index int) []CellChange {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	s := wb.SheetByNameOrFirst(sheetName)
	if s == nil || index < 0 || index > s.Cols {
		return nil
	}
	wb.colSurgeryLocked(s, index, true)
	return wb.recalcAllLocked()
}

func (wb *Workbook) DeleteCol(sheetName string, index int) []CellChange {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	s := wb.SheetByNameOrFirst(sheetName)
	if s == nil || index < 0 || index >= s.Cols {
		return nil
	}
	wb.colSurgeryLocked(s, index, false)
	return wb.recalcAllLocked()
}

// SheetByNameOrFirst resolves a sheet name; the empty string denotes the
// workbook's first sheet, which keeps the common single-target API simple.
func (wb *Workbook) SheetByNameOrFirst(name string) *Sheet {
	if name != "" {
		return wb.Sheet(name)
	}
	if len(wb.Sheets) > 0 {
		return wb.Sheets[0]
	}
	return nil
}

func (wb *Workbook) rowSurgeryLocked(target *Sheet, index int, insert bool) {
	kind := formula.ShiftInsertRow
	if !insert {
		kind = formula.ShiftDeleteRow
	}
	wb.moveCellsRows(target, index, insert)
	wb.rewriteAllFormulas(kind, target.Name, index)
	if insert {
		target.Rows++
	} else {
		target.Rows--
	}
	wb.rebuildGraph()
}

func (wb *Workbook) colSurgeryLocked(target *Sheet, index int, insert bool) {
	kind := formula.ShiftInsertCol
	if !insert {
		kind = formula.ShiftDeleteCol
	}
	wb.moveCellsCols(target, index, insert)
	wb.rewriteAllFormulas(kind, target.Name, index)
	if insert {
		target.Cols++
	} else {
		target.Cols--
	}
	wb.rebuildGraph()
}

func (wb *Workbook) moveCellsRows(s *Sheet, index int, insert bool) {
	next := make(map[uint64]*Cell, len(s.Cells))
	for k, c := range s.Cells {
		col, row := unpack(k)
		switch {
		case insert && row >= index:
			row++
		case !insert && row == index:
			continue
		case !insert && row > index:
			row--
		}
		c.Row = row
		next[pack(col, row)] = c
	}
	s.Cells = next
}

func (wb *Workbook) moveCellsCols(s *Sheet, index int, insert bool) {
	next := make(map[uint64]*Cell, len(s.Cells))
	for k, c := range s.Cells {
		col, row := unpack(k)
		switch {
		case insert && col >= index:
			col++
		case !insert && col == index:
			continue
		case !insert && col > index:
			col--
		}
		c.Col = col
		next[pack(col, row)] = c
	}
	s.Cells = next
}

// rewriteAllFormulas shifts references in formulas on every sheet, including
// cross-sheet references into the surgical target.
func (wb *Workbook) rewriteAllFormulas(kind formula.ShiftKind, targetName string, index int) {
	for _, s := range wb.Sheets {
		for _, c := range s.Cells {
			if !c.IsFormula || c.AST == nil {
				continue
			}
			c.AST = formula.ShiftAST(c.AST, kind, s.Name, targetName, index)
			c.Input = "=" + formula.Print(c.AST)
		}
	}
}
