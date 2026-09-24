package formula

import "testing"

func adjust(t *testing.T, expr, home string, op Shift) string {
	t.Helper()
	ast, err := Parse(expr)
	if err != nil {
		t.Fatalf("parse %q: %v", expr, err)
	}
	got := AdjustReferences(ast, home, op).String()
	if got[0] != '=' {
		got = "=" + got
	}
	return got
}

func TestInsertRowShiftsRefs(t *testing.T) {
	op := Shift{Axis: AxisRow, TargetSheet: "S1", Index: 2, Count: 1, Delete: false} // insert before row 3 (0-based 2)
	cases := map[string]string{
		"=A3":           "=A4", // at or after insertion point shifts down
		"=A2":           "=A2", // before it stays
		"=A$3":          "=A$3",
		"=$A3":          "=$A4",
		"=$A$3":         "=$A$3",
		"=SUM(A1:A10)":  "=SUM(A1:A11)",
		"=SUM($A$1:A2)": "=SUM($A$1:A2)",
	}
	for in, want := range cases {
		if got := adjust(t, in, "S1", op); got != want {
			t.Errorf("insert row: %s -> %s, want %s", in, got, want)
		}
	}
}

func TestCrossSheetShift(t *testing.T) {
	// Inserting a row on S1 moves S1 references in formulas living on S2...
	opHere := Shift{Axis: AxisRow, TargetSheet: "S1", Index: 2, Count: 1, Delete: false}
	if got := adjust(t, "=S1!A3", "S2", opHere); got != "=S1!A4" {
		t.Errorf("cross-sheet ref should move with target sheet, got %s", got)
	}
	// ...but a reference to S3 is untouched.
	if got := adjust(t, "=S3!A3", "S2", opHere); got != "=S3!A3" {
		t.Errorf("other-sheet ref should not move, got %s", got)
	}
	// Inserting on the formula's own sheet moves same-sheet refs only.
	opOwn := Shift{Axis: AxisRow, TargetSheet: "S2", Index: 2, Count: 1, Delete: false}
	if got := adjust(t, "=A3+S1!A3", "S2", opOwn); got != "=A4+S1!A3" {
		t.Errorf("own-sheet vs cross-sheet mix wrong: %s", got)
	}
}

func TestInsertColShiftsRefs(t *testing.T) {
	op := Shift{Axis: AxisCol, TargetSheet: "S1", Index: 2, Count: 1, Delete: false} // before column C
	cases := map[string]string{
		"=C1":         "=D1",
		"=B1":         "=B1",
		"=$C1":        "=$C1", // absolute column stays
		"=C$1":        "=D$1", // absolute row irrelevant to col shift
		"=SUM(B1:D1)": "=SUM(B1:E1)",
	}
	for in, want := range cases {
		if got := adjust(t, in, "S1", op); got != want {
			t.Errorf("insert col: %s -> %s, want %s", in, got, want)
		}
	}
}

func TestDeleteRowRefs(t *testing.T) {
	op := Shift{Axis: AxisRow, TargetSheet: "S1", Index: 2, Count: 1, Delete: true} // delete row 3
	cases := map[string]string{
		"=A4":          "=A3",    // after the hole moves up
		"=A2":          "=A2",    // before stays
		"=A3":          "=#REF!", // the deleted row itself becomes #REF!
		"=SUM(A1:A10)": "=SUM(A1:A9)",
	}
	for in, want := range cases {
		if got := adjust(t, in, "S1", op); got != want {
			t.Errorf("delete row: %s -> %s, want %s", in, got, want)
		}
	}
}

func TestDeleteColAbsolute(t *testing.T) {
	op := Shift{Axis: AxisCol, TargetSheet: "S1", Index: 2, Count: 1, Delete: true} // delete C
	if got := adjust(t, "=$C1", "S1", op); got != "=#REF!" {
		t.Errorf("deleted absolute col ref -> %s, want #REF!", got)
	}
	if got := adjust(t, "=D1", "S1", op); got != "=C1" {
		t.Errorf("D after deleting C -> %s, want C1", got)
	}
}
