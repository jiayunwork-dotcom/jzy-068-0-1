package engine

import (
	"testing"

	"collabsheet/internal/formula"
	"collabsheet/internal/workbook"
)

func mustAddr(t *testing.T, addr string) (int, int) {
	t.Helper()
	c, r, err := workbook.ParseAddr(addr)
	if err != nil {
		t.Fatalf("bad addr %s: %v", addr, err)
	}
	return c, r
}

func newSingleSheet() *workbook.Workbook {
	wb := workbook.New()
	s := workbook.NewSheet("S1", workbook.DefaultRows)
	wb.Sheets = []*workbook.Sheet{s}
	return wb
}

func setCell(t *testing.T, wb *workbook.Workbook, sheet, addr, raw string) {
	t.Helper()
	sh := wb.SheetByName(sheet)
	c, r := mustAddr(t, addr)
	sh.SetRaw(c, r, raw)
}

// TestTopologicalRecalculation verifies that editing an input recomputes the
// whole downstream chain strictly dependencies-first, and no formula reads a
// stale value.
func TestTopologicalRecalculation(t *testing.T) {
	wb := newSingleSheet()
	setCell(t, wb, "S1", "A1", "10")
	setCell(t, wb, "S1", "B1", "=A1*2")
	setCell(t, wb, "S1", "C1", "=B1+5")
	setCell(t, wb, "S1", "D1", "=C1+B1")
	eng := New(wb)

	assertValue := func(addr string, want float64) {
		t.Helper()
		c, r := mustAddr(t, addr)
		got := eng.Result("S1", c, r).Value
		if got.Typ != formula.VNumber || got.Num != want {
			t.Fatalf("%s = %v, want %v", addr, got.Display(), want)
		}
	}
	assertValue("B1", 20)
	assertValue("C1", 25)
	assertValue("D1", 45)

	// Record evaluation order during the edit and assert topological order:
	// A1 (literal, not observed) then B1 before C1/D1.
	var order []string
	eng.OnEvaluate = func(k Key) { order = append(order, workbook.Addr(k.Col, k.Row)) }

	setCell(t, wb, "S1", "A1", "100")
	eng.RecalcAfterEdit("S1", 0, 0)

	pos := map[string]int{}
	for i, a := range order {
		pos[a] = i
	}
	if _, ok := pos["B1"]; !ok {
		t.Fatalf("B1 was not recomputed, order=%v", order)
	}
	if pos["C1"] <= pos["B1"] {
		t.Fatalf("C1 must recompute after B1, order=%v", order)
	}
	if pos["D1"] <= pos["C1"] || pos["D1"] <= pos["B1"] {
		t.Fatalf("D1 must recompute after C1 and B1, order=%v", order)
	}

	// And no stale values: A1=100 => B1=200, C1=205, D1=405.
	assertValue("B1", 200)
	assertValue("C1", 205)
	assertValue("D1", 405)
}

// TestNoStaleRead ensures a branching chain never reads an old intermediate
// value: two dependents of A1 where one also depends on the other.
func TestNoStaleRead(t *testing.T) {
	wb := newSingleSheet()
	setCell(t, wb, "S1", "A1", "1")
	setCell(t, wb, "S1", "A2", "=A1")
	setCell(t, wb, "S1", "A3", "=A2+A1")
	setCell(t, wb, "S1", "A4", "=A3+A2+A1")
	eng := New(wb)

	setCell(t, wb, "S1", "A1", "10")
	eng.RecalcAfterEdit("S1", 0, 0)

	c2, r2 := mustAddr(t, "A2")
	c3, r3 := mustAddr(t, "A3")
	c4, r4 := mustAddr(t, "A4")
	if v := eng.Result("S1", c2, r2).Value.Num; v != 10 {
		t.Fatalf("A2=%v want 10", v)
	}
	if v := eng.Result("S1", c3, r3).Value.Num; v != 20 {
		t.Fatalf("A3=%v want 20", v)
	}
	if v := eng.Result("S1", c4, r4).Value.Num; v != 40 {
		t.Fatalf("A4=%v want 40", v)
	}
}

// TestDirectCycle detects A1->B1->A1 and marks both #CYCLE! without looping.
func TestDirectCycle(t *testing.T) {
	wb := newSingleSheet()
	setCell(t, wb, "S1", "A1", "=B1+1")
	setCell(t, wb, "S1", "B1", "=A1+1")
	eng := New(wb)

	for _, addr := range []string{"A1", "B1"} {
		c, r := mustAddr(t, addr)
		res := eng.Result("S1", c, r)
		if !res.IsCycle || res.Value.Str != "#CYCLE!" {
			t.Fatalf("%s should be a cycle, got %q cycle=%v", addr, res.Value.Display(), res.IsCycle)
		}
	}

	// Breaking the cycle must clear the error and recompute normally:
	// set B1=5 => A1=6.
	setCell(t, wb, "S1", "B1", "5")
	cb, rb := mustAddr(t, "B1")
	eng.RecalcAfterEdit("S1", cb, rb)

	if eng.IsCycle("S1", cb, rb) {
		t.Fatal("B1 should no longer be cyclic")
	}
	ca, ra := mustAddr(t, "A1")
	res := eng.Result("S1", ca, ra)
	if res.IsCycle {
		t.Fatalf("A1 should no longer be cyclic")
	}
	if res.Value.Num != 6 {
		t.Fatalf("A1=%v want 6 after breaking cycle", res.Value.Display())
	}
}

// TestIndirectCycle detects a longer ring A1->B1->C1->D1->A1.
func TestIndirectCycle(t *testing.T) {
	wb := newSingleSheet()
	setCell(t, wb, "S1", "A1", "=B1")
	setCell(t, wb, "S1", "B1", "=C1")
	setCell(t, wb, "S1", "C1", "=D1")
	setCell(t, wb, "S1", "D1", "=A1")
	setCell(t, wb, "S1", "E1", "=A1+1") // outside but dependent -> propagates error
	eng := New(wb)

	for _, addr := range []string{"A1", "B1", "C1", "D1"} {
		c, r := mustAddr(t, addr)
		if !eng.IsCycle("S1", c, r) {
			t.Fatalf("%s must be flagged cyclic", addr)
		}
	}
	ce, re := mustAddr(t, "E1")
	ev := eng.Result("S1", ce, re).Value
	if ev.Typ != formula.VError || ev.Str != "#CYCLE!" {
		t.Fatalf("E1 must propagate #CYCLE!, got %q", ev.Display())
	}
	if eng.IsCycle("S1", ce, re) {
		t.Fatal("E1 is a dependent, not a member of the cycle SCC")
	}
}

// TestSelfCycle covers A1==A1.
func TestSelfCycle(t *testing.T) {
	wb := newSingleSheet()
	setCell(t, wb, "S1", "A1", "=A1+1")
	eng := New(wb)
	c, r := mustAddr(t, "A1")
	if !eng.IsCycle("S1", c, r) {
		t.Fatal("A1 referencing itself must be cyclic")
	}
}

// TestNestedFunctions exercises deep nesting and IF/SUM interplay.
func TestNestedFunctions(t *testing.T) {
	wb := newSingleSheet()
	for i := 1; i <= 10; i++ {
		setCell(t, wb, "S1", cellAddr("A", i), itoaTest(i*10))
	}
	setCell(t, wb, "S1", "B1", `=IF(SUM(A1:A10)>100,"超标","正常")`)
	eng := New(wb)
	c, r := mustAddr(t, "B1")
	if v := eng.Result("S1", c, r).Value.Str; v != "超标" {
		t.Fatalf("B1=%q want 超标", v)
	}
}

func cellAddr(col string, row int) string { return col + itoaTest(row) }

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	out := ""
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	return out
}
