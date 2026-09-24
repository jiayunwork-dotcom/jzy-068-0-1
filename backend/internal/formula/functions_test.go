package formula

import "testing"

type staticEvaler struct {
	cells map[string]Value
	sheet string
}

func (e *staticEvaler) key(sheet string, c, r int) string {
	if sheet == "" {
		sheet = e.sheet
	}
	return sheet + "!" + AddrTest(c, r)
}

func (e *staticEvaler) CellValue(sheet string, c, r int) Value {
	if v, ok := e.cells[e.key(sheet, c, r)]; ok {
		return v
	}
	return Blank()
}

func (e *staticEvaler) RangeValues(sheet string, c1, r1, c2, r2 int) Matrix {
	m := Matrix{Rows: r2 - r1 + 1, Cols: c2 - c1 + 1}
	for r := r1; r <= r2; r++ {
		for c := c1; c <= c2; c++ {
			m.Values = append(m.Values, e.CellValue(sheet, c, r))
		}
	}
	return m
}

// AddrTest mirrors workbook.Addr without an import cycle (test-only).
func AddrTest(c, r int) string {
	return colLetters(c) + itoaTest2(r+1)
}

func itoaTest2(n int) string {
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

func evalStr(t *testing.T, expr string, ev Evaler) Value {
	t.Helper()
	ast, err := Parse(expr)
	if err != nil {
		t.Fatalf("parse %q: %v", expr, err)
	}
	return Eval(ast, ev)
}

func TestArithmeticPrecedence(t *testing.T) {
	ev := &staticEvaler{cells: map[string]Value{}, sheet: "S"}
	cases := map[string]float64{
		"=2+3*4":   14,
		"=(2+3)*4": 20,
		"=2^3^2":   512, // right associative: 2^(3^2)
		"=10-2-3":  5,   // left associative
		"=20/4/5":  1,
		"=50%":     0.5,
		"=-2^2":    4, // unary minus binds tighter than ^ in our grammar
	}
	for expr, want := range cases {
		v := evalStr(t, expr, ev)
		f, _ := v.AsNumber()
		if f != want {
			t.Errorf("%s = %v, want %v", expr, f, want)
		}
	}
	// Comparison returns a boolean.
	if v := evalStr(t, "=1+1=2", ev); v.Typ != VBool || v.Num != 1 {
		t.Fatalf("1+1=2 = %v", v.Display())
	}
}

func TestStringConcatAndEscapes(t *testing.T) {
	ev := &staticEvaler{cells: map[string]Value{}, sheet: "S"}
	v := evalStr(t, `="a""b"&"c"`, ev)
	if v.Str != `a"bc` {
		t.Fatalf("got %q", v.Str)
	}
}

func TestRangeFunctions(t *testing.T) {
	ev := &staticEvaler{sheet: "S", cells: map[string]Value{
		"S!A1": Number(1), "S!A2": Number(2), "S!A3": Number(3),
		"S!A4": Number(4), "S!A5": Text("ignore"),
	}}
	assertNum := func(expr string, want float64) {
		t.Helper()
		v := evalStr(t, expr, ev)
		f, _ := v.AsNumber()
		if f != want {
			t.Errorf("%s = %v, want %v", expr, f, want)
		}
	}
	assertNum("=SUM(A1:A4)", 10)
	assertNum("=AVERAGE(A1:A4)", 2.5)
	assertNum("=MIN(A1:A4)", 1)
	assertNum("=MAX(A1:A4)", 4)
	assertNum("=COUNT(A1:A5)", 4)
	assertNum("=ROUND(3.14159,2)", 3.14)
	assertNum("=ABS(-7)", 7)
}

func TestLogicFunctions(t *testing.T) {
	ev := &staticEvaler{sheet: "S", cells: map[string]Value{
		"S!A1": Number(150),
	}}
	if v := evalStr(t, `=IF(A1>100,"超标","正常")`, ev); v.Str != "超标" {
		t.Fatalf("IF = %q", v.Str)
	}
	if v := evalStr(t, "=AND(TRUE,1>0,2)", ev); v.Typ != VBool || v.Num != 1 {
		t.Fatalf("AND = %v", v.Display())
	}
	if v := evalStr(t, "=OR(FALSE,FALSE)", ev); v.Typ != VBool || v.Num != 0 {
		t.Fatalf("OR = %v", v.Display())
	}
	if v := evalStr(t, "=OR(FALSE,TRUE)", ev); v.Num != 1 {
		t.Fatalf("OR = %v", v.Display())
	}
	if v := evalStr(t, "=NOT(FALSE)", ev); v.Num != 1 {
		t.Fatalf("NOT = %v", v.Display())
	}
}

func TestIFLazinessAvoidsBranch(t *testing.T) {
	ev := &staticEvaler{sheet: "S", cells: map[string]Value{
		"S!A1": Number(1),
	}}
	// The taken branch is fine; the not-taken branch divides by zero but must
	// never be evaluated.
	v := evalStr(t, `=IF(A1>0,"ok",1/0)`, ev)
	if v.Str != "ok" {
		t.Fatalf("lazy IF failed: %v", v.Display())
	}
}

func TestLookupFunctions(t *testing.T) {
	ev := &staticEvaler{sheet: "S", cells: map[string]Value{
		"S!A1": Text("服务器"), "S!B1": Number(12000),
		"S!A2": Text("域名"), "S!B2": Number(200),
		"S!A3": Text("设计"), "S!B3": Number(3000),
	}}
	if v := evalStr(t, `=VLOOKUP("设计",A1:B3,2,FALSE)`, ev); v.Num != 3000 {
		t.Fatalf("VLOOKUP = %v", v.Display())
	}
	if v := evalStr(t, `=INDEX(B1:B3,2)`, ev); v.Num != 200 {
		t.Fatalf("INDEX = %v", v.Display())
	}
	if v := evalStr(t, `=MATCH("域名",A1:A3,0)`, ev); v.Num != 2 {
		t.Fatalf("MATCH = %v", v.Display())
	}
}

func TestNestedExampleFromSpec(t *testing.T) {
	ev := &staticEvaler{sheet: "S", cells: map[string]Value{}}
	for i := 1; i <= 10; i++ {
		ev.cells["S!A"+itoaTest2(i)] = Number(float64(i * 10))
	}
	v := evalStr(t, `=IF(SUM(A1:A10)>100,"超标","正常")`, ev)
	if v.Str != "超标" {
		t.Fatalf("got %q", v.Str)
	}
}

func TestDivisionByZero(t *testing.T) {
	ev := &staticEvaler{cells: map[string]Value{}, sheet: "S"}
	v := evalStr(t, "=1/0", ev)
	if v.Typ != VError || v.Str != "#DIV/0!" {
		t.Fatalf("got %v", v.Display())
	}
}

func TestErrorsPropagate(t *testing.T) {
	ev := &staticEvaler{sheet: "S", cells: map[string]Value{
		"S!A1": Div0Err,
	}}
	v := evalStr(t, "=SUM(A1,1)", ev)
	if v.Typ != VError {
		t.Fatalf("error must propagate through SUM, got %v", v.Display())
	}
}
