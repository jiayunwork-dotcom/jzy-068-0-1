package engine

import (
	"testing"

	"collabsheet/internal/formula"
)

func newTestWorkbook(t *testing.T) *Workbook {
	t.Helper()
	wb := NewWorkbook("test")
	wb.AddSheet("S1", 100, 26)
	wb.AddSheet("S2", 100, 26)
	return wb
}

func display(wb *Workbook, sheet string, col, row int) string {
	c := wb.Sheet(sheet).cell(col, row)
	if c == nil {
		return ""
	}
	return c.Display
}

func isCircular(wb *Workbook, sheet string, col, row int) bool {
	c := wb.Sheet(sheet).cell(col, row)
	return c != nil && c.Circular
}

// TestTopologicalRecalc verifies that editing a dependency source refreshes
// the whole downstream chain in dependency-first order and never computes a
// dependent from a stale value.
func TestTopologicalRecalc(t *testing.T) {
	wb := newTestWorkbook(t)
	// A1=10, B1=A1*2, C1=B1+A1, D1=SUM(A1:C1)
	wb.SetCell("S1", 0, 0, "10")
	wb.SetCell("S1", 1, 0, "=A1*2")
	wb.SetCell("S1", 2, 0, "=B1+A1")
	wb.SetCell("S1", 3, 0, "=SUM(A1:C1)")

	if got := display(wb, "S1", 3, 0); got != "60" { // 10+20+30
		t.Fatalf("D1 = %q want 60", got)
	}

	// Instrument evaluation order by tracking through a dependent chain edit.
	changes := wb.SetCell("S1", 0, 0, "20")
	order := map[string]int{}
	for i, ch := range changes {
		order[coordKey(ch.Sheet, ch.Col, ch.Row)] = i
	}
	// A1 before B1 before C1 before D1.
	chain := []string{key("S1", 0, 0), key("S1", 1, 0), key("S1", 2, 0), key("S1", 3, 0)}
	for i := 1; i < len(chain); i++ {
		if order[chain[i-1]] >= order[chain[i]] {
			t.Errorf("recalc order violated: %s (idx %d) must precede %s (idx %d)",
				chain[i-1], order[chain[i-1]], chain[i], order[chain[i]])
		}
	}
	// Final values must all reflect the new A1.
	if got := display(wb, "S1", 1, 0); got != "40" {
		t.Errorf("B1=%q want 40", got)
	}
	if got := display(wb, "S1", 2, 0); got != "60" {
		t.Errorf("C1=%q want 60 (stale value!)", got)
	}
	if got := display(wb, "S1", 3, 0); got != "120" {
		t.Errorf("D1=%q want 120 (20+40+60)", got)
	}
}

func TestNoStaleValueDiamond(t *testing.T) {
	wb := newTestWorkbook(t)
	// Diamond: A1 -> B1,C1 -> D1=B1+C1 ; B1=A1*2 C1=A1+5
	wb.SetCell("S1", 0, 0, "3")
	wb.SetCell("S1", 1, 0, "=A1*2")
	wb.SetCell("S1", 2, 0, "=A1+5")
	wb.SetCell("S1", 3, 0, "=B1+C1")
	if got := display(wb, "S1", 3, 0); got != "14" {
		t.Fatalf("D1=%q want 14", got)
	}
	wb.SetCell("S1", 0, 0, "10")
	if got := display(wb, "S1", 3, 0); got != "35" {
		t.Errorf("D1=%q want 35 (stale diamond)", got)
	}
}

// TestDirectCycle constructs A1=B1+1, B1=A1+1 and expects both cells flagged
// #CIRCULAR! instantly, with no stack overflow / hang.
func TestDirectCycle(t *testing.T) {
	wb := newTestWorkbook(t)
	wb.SetCell("S1", 0, 0, "1")
	wb.SetCell("S1", 1, 0, "=A1+1")
	wb.SetCell("S1", 0, 0, "=B1+1") // now A1<->B1

	a := wb.Sheet("S1").cell(0, 0)
	b := wb.Sheet("S1").cell(1, 0)
	if a.Display != string(formula.ErrCircular) || b.Display != string(formula.ErrCircular) {
		t.Errorf("cycle not flagged: A1=%q B1=%q", a.Display, b.Display)
	}
	if !a.Circular || !b.Circular {
		t.Errorf("circular flags missing")
	}

	// Breaking the cycle restores normal values.
	wb.SetCell("S1", 0, 0, "10")
	if got := display(wb, "S1", 1, 0); got != "11" {
		t.Errorf("after breaking cycle B1=%q want 11", got)
	}
	if isCircular(wb, "S1", 1, 0) {
		t.Errorf("B1 should no longer be circular")
	}
}

// TestIndirectCycle builds A1->B1->C1->D1->A1.
func TestIndirectCycle(t *testing.T) {
	wb := newTestWorkbook(t)
	wb.SetCell("S1", 0, 0, "=B1+1")
	wb.SetCell("S1", 1, 0, "=C1+1")
	wb.SetCell("S1", 2, 0, "=D1+1")
	wb.SetCell("S1", 3, 0, "=A1+1")
	for col := 0; col < 4; col++ {
		c := wb.Sheet("S1").cell(col, 0)
		if c.Display != string(formula.ErrCircular) {
			t.Errorf("cell col %d: %q want #CIRCULAR!", col, c.Display)
		}
	}
	// A dependent *outside* the cycle propagates the error rather than hangs.
	wb.SetCell("S1", 4, 0, "=D1*2")
	if got := display(wb, "S1", 4, 0); got != string(formula.ErrCircular) {
		t.Errorf("E1=%q want propagated #CIRCULAR!", got)
	}
}

func TestSelfCycle(t *testing.T) {
	wb := newTestWorkbook(t)
	wb.SetCell("S1", 0, 0, "=A1+1")
	if got := display(wb, "S1", 0, 0); got != string(formula.ErrCircular) {
		t.Errorf("self cycle = %q want #CIRCULAR!", got)
	}
}

// ---- row/column insert & delete reference shifting ----------------------

func TestInsertRowShiftsRefs(t *testing.T) {
	wb := newTestWorkbook(t)
	// B1 (col1,row0) = A3 (col0,row2) * 2 ; A3 = 100
	wb.SetCell("S1", 0, 2, "100")
	wb.SetCell("S1", 1, 0, "=A3*2")
	if got := display(wb, "S1", 1, 0); got != "200" {
		t.Fatalf("setup B1=%q want 200", got)
	}

	// Insert a row before row 3 (0-based index 2): A3 moves to A4 and the
	// formula in B1 must be rewritten to A4.
	wb.InsertRow("S1", 2)

	b := wb.Sheet("S1").cell(1, 0)
	if b.Input != "=A4*2" {
		t.Errorf("formula after insert = %q want =A4*2", b.Input)
	}
	if got := b.Display; got != "200" {
		t.Errorf("B1 after insert = %q want 200", got)
	}
	// The value physically moved to A4 (0-based row 3).
	if got := display(wb, "S1", 0, 3); got != "100" {
		t.Errorf("A4=%q want 100", got)
	}
}

func TestInsertRowAbsoluteDoesNotMove(t *testing.T) {
	wb := newTestWorkbook(t)
	wb.SetCell("S1", 0, 2, "100")
	wb.SetCell("S1", 1, 0, "=$A$3*2")
	wb.InsertRow("S1", 2)
	b := wb.Sheet("S1").cell(1, 0)
	if b.Input != "=$A$3*2" { // top-level binary stays unparenthesized
		t.Errorf("absolute ref moved: %q", b.Input)
	}
	// $A$3 now points at the freshly inserted blank row.
	if got := b.Display; got != "0" {
		t.Errorf("absolute B1 = %q want 0 (points at new blank row)", got)
	}
}

func TestDeleteRowShiftsAndRefErrors(t *testing.T) {
	wb := newTestWorkbook(t)
	wb.SetCell("S1", 0, 2, "100") // A3
	wb.SetCell("S1", 0, 3, "200") // A4
	wb.SetCell("S1", 1, 0, "=A3+A4")

	// Delete row 3 (0-based 2): the A3 reference becomes #REF!, A4 becomes A3.
	wb.DeleteRow("S1", 2)
	b := wb.Sheet("S1").cell(1, 0)
	if b.Input != "=#REF!+A3" {
		t.Errorf("formula after delete = %q", b.Input)
	}
	if got := b.Display; got != string(formula.ErrRef) {
		t.Errorf("B1 after delete = %q want #REF!", got)
	}
	// Remaining cell moved up.
	if got := display(wb, "S1", 0, 2); got != "200" {
		t.Errorf("A3 after delete = %q want 200", got)
	}
}

func TestInsertColShiftsRefs(t *testing.T) {
	wb := newTestWorkbook(t)
	wb.SetCell("S1", 2, 0, "7") // C1
	wb.SetCell("S1", 0, 1, "=C1")
	wb.InsertCol("S1", 2) // insert before column C
	a := wb.Sheet("S1").cell(0, 1)
	if a.Input != "=D1" {
		t.Errorf("ref after col insert = %q want =D1", a.Input)
	}
	if got := a.Display; got != "7" {
		t.Errorf("value = %q want 7", got)
	}
}

func TestRangeShiftsOnInsert(t *testing.T) {
	wb := newTestWorkbook(t)
	for r := 0; r < 4; r++ {
		wb.SetCell("S1", 0, r, "10") // A1..A4
	}
	wb.SetCell("S1", 1, 5, "=SUM(A1:A4)") // B6
	// Insert a row at index 2: the range must expand to A1:A5 and B6 moves
	// down to B7.
	wb.InsertRow("S1", 2)
	c := wb.Sheet("S1").cell(1, 6)
	if c.Input != "=SUM(A1:A5)" {
		t.Errorf("range shifted to %q want =SUM(A1:A5)", c.Input)
	}
	if got := c.Display; got != "40" {
		t.Errorf("sum = %q want 40", got)
	}
}

func TestCrossSheetRefShiftsOnOtherSheetInsert(t *testing.T) {
	wb := newTestWorkbook(t)
	wb.SetCell("S2", 0, 2, "55") // S2!A3
	wb.SetCell("S1", 0, 0, "=S2!A3")
	// Inserting a row in S2 moves S2!A3 -> S2!A4 even though formula is on S1.
	wb.InsertRow("S2", 2)
	a := wb.Sheet("S1").cell(0, 0)
	if a.Input != "=S2!A4" {
		t.Errorf("cross-sheet ref = %q want =S2!A4", a.Input)
	}
	if got := a.Display; got != "55" {
		t.Errorf("cross-sheet value = %q want 55", got)
	}
}

func TestFormulaAboveInsertionUntouched(t *testing.T) {
	wb := newTestWorkbook(t)
	wb.SetCell("S1", 0, 0, "1")
	wb.SetCell("S1", 1, 0, "=A1")
	wb.InsertRow("S1", 5) // insert far below
	c := wb.Sheet("S1").cell(1, 0)
	if c.Input != "=A1" {
		t.Errorf("unrelated formula rewritten: %q", c.Input)
	}
}

// TestMixedAnchorShifts locks the per-anchor rule: in $A3 the column is
// absolute but the row relative, so inserting a row moves the row while the
// column anchor stays; inserting a column moves neither.
func TestMixedAnchorShifts(t *testing.T) {
	wb := newTestWorkbook(t)
	wb.SetCell("S1", 0, 2, "8") // A3
	wb.SetCell("S1", 1, 0, "=$A3")

	wb.InsertRow("S1", 2)
	if got := wb.Sheet("S1").cell(1, 0).Input; got != "=$A4" {
		t.Errorf("mixed anchor after row insert = %q want =$A4", got)
	}
	// Undo the row shift mentally via delete: deleting the inserted row (idx 2)
	// removes a blank row and restores $A3.
	wb.DeleteRow("S1", 2)
	if got := wb.Sheet("S1").cell(1, 0).Input; got != "=$A3" {
		t.Errorf("mixed anchor after deleting inserted row = %q want =$A3", got)
	}
	// A column insert physically shifts the formula cell B1 -> C1; the
	// absolute column anchor inside ($A3) must still not move.
	wb.InsertCol("S1", 0)
	if got := wb.Sheet("S1").cell(2, 0).Input; got != "=$A3" {
		t.Errorf("absolute col anchor moved on col insert: %q want =$A3", got)
	}
}

// TestRangeDeleteContraction: deleting a row inside a range shifts the lower
// endpoint and leaves the range intact (not #REF!).
func TestRangeDeleteContraction(t *testing.T) {
	wb := newTestWorkbook(t)
	for r := 0; r < 4; r++ {
		wb.SetCell("S1", 0, r, "10") // A1..A4
	}
	wb.SetCell("S1", 1, 5, "=SUM(A1:A4)") // B6
	// Delete an interior row (row 2, 0-based index 1): A4 endpoint moves to A3,
	// the range contracts to A1:A3 and still sums the surviving values.
	wb.DeleteRow("S1", 1)
	c := wb.Sheet("S1").cell(1, 4) // B6 moved up to row index 4 (B5)
	if c == nil {
		t.Fatal("SUM cell missing after delete")
	}
	if c.Input != "=SUM(A1:A3)" {
		t.Errorf("contracted range = %q want =SUM(A1:A3)", c.Input)
	}
	if got := c.Display; got != "30" {
		t.Errorf("sum after interior delete = %q want 30", got)
	}
}

// TestFormulaCellDependsOnLiteralAfterUndoChain guards the exact required
// scenario at the engine level in addition to the hub-level test.
func TestDependentChainRollback(t *testing.T) {
	wb := newTestWorkbook(t)
	wb.SetCell("S1", 0, 0, "5")
	wb.SetCell("S1", 1, 0, "=A1+1")
	wb.SetCell("S1", 2, 0, "=B1*10")
	if got := display(wb, "S1", 2, 0); got != "60" {
		t.Fatalf("C1 = %q want 60", got)
	}
	wb.SetCell("S1", 0, 0, "") // simulate undo of the A1 literal
	if got := display(wb, "S1", 1, 0); got != "1" {
		t.Errorf("B1 = %q want 1", got)
	}
	if got := display(wb, "S1", 2, 0); got != "10" {
		t.Errorf("C1 = %q want 10 (indirect dependent must roll back)", got)
	}
}

func coordKey(sheet string, col, row int) string { return key(sheet, col, row) }
func key(sheet string, col, row int) string {
	return sheet + "!" + string(rune('A'+col)) + itoa(row+1)
}
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
