package workbook

import "testing"

func rawOf(wb *Workbook, t *testing.T, sheet, addr string) string {
	sh := wb.SheetByName(sheet)
	c, r, err := ParseAddr(addr)
	if err != nil {
		t.Fatal(err)
	}
	cell := sh.Get(c, r)
	if cell == nil {
		return ""
	}
	return cell.Raw
}

func TestInsertRowsMovesContentAndFormulas(t *testing.T) {
	wb := New()
	s := NewSheet("S1", 10)
	wb.Sheets = []*Sheet{s}
	s.SetRaw(0, 0, "10")    // A1
	s.SetRaw(0, 2, "30")    // A3
	s.SetRaw(1, 5, "=A3*2") // B6 references A3

	if err := wb.InsertRows("S1", 2, 1); err != nil { // insert before row 3
		t.Fatal(err)
	}
	if s.Rows != 11 {
		t.Fatalf("rows=%d want 11", s.Rows)
	}
	if got := rawOf(wb, t, "S1", "A1"); got != "10" {
		t.Errorf("A1=%q", got)
	}
	if got := rawOf(wb, t, "S1", "A3"); got != "" {
		t.Errorf("A3 should be empty, got %q", got)
	}
	if got := rawOf(wb, t, "S1", "A4"); got != "30" {
		t.Errorf("A3 content should move to A4, got %q", got)
	}
	if got := rawOf(wb, t, "S1", "B7"); got != "=A4*2" {
		t.Errorf("B6 should move to B7 with rewritten formula =A4*2, got %q", got)
	}
}

func TestDeleteRowsMovesContentAndFormulas(t *testing.T) {
	wb := New()
	s := NewSheet("S1", 10)
	wb.Sheets = []*Sheet{s}
	s.SetRaw(0, 0, "10")
	s.SetRaw(0, 2, "30")
	s.SetRaw(1, 4, "=A3+A1") // B5

	if err := wb.DeleteRows("S1", 2, 1); err != nil { // delete row 3
		t.Fatal(err)
	}
	if got := rawOf(wb, t, "S1", "A3"); got != "" {
		t.Errorf("A3 (deleted) should be empty, got %q", got)
	}
	if got := rawOf(wb, t, "S1", "B4"); got != "=#REF!+A1" {
		t.Errorf("B5 should move to B4 with A3 -> #REF!, got %q", got)
	}
}

func TestInsertColsKeepsAbsoluteColumn(t *testing.T) {
	wb := New()
	s := NewSheet("S1", 10)
	wb.Sheets = []*Sheet{s}
	s.SetRaw(2, 0, "=SUM($C:$C)") // not a valid formula here; use cells
	s.SetRaw(2, 0, "=SUM($C$1:C5)")

	if err := wb.InsertCols("S1", 2, 1); err != nil { // before column C
		t.Fatal(err)
	}
	if s.Cols != Cols+1 {
		t.Fatalf("cols=%d want %d", s.Cols, Cols+1)
	}
	// D1 now holds the formula; absolute $C$1 stays, relative C5 -> D5.
	if got := rawOf(wb, t, "S1", "D1"); got != "=SUM($C$1:D5)" {
		t.Errorf("got %q", got)
	}
}

func TestCrossSheetFormulaAdjustedFromOtherSheet(t *testing.T) {
	wb := New()
	s1 := NewSheet("S1", 10)
	s2 := NewSheet("S2", 10)
	wb.Sheets = []*Sheet{s1, s2}
	s2.SetRaw(0, 0, "=S1!A3")

	if err := wb.InsertRows("S1", 2, 1); err != nil {
		t.Fatal(err)
	}
	if got := rawOf(wb, t, "S2", "A1"); got != "=S1!A4" {
		t.Errorf("cross-sheet ref should be rewritten to S1!A4, got %q", got)
	}
}
