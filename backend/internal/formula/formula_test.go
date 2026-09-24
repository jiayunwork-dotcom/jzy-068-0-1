package formula

import "testing"

// sheetCtx is a tiny in-memory evaluation context for parser/eval tests.
type sheetCtx struct {
	sheets map[string]map[[2]int]Value
	cur    string
}

func newCtx() *sheetCtx {
	return &sheetCtx{sheets: map[string]map[[2]int]Value{}, cur: "S1"}
}

func (c *sheetCtx) set(col, row int, v Value) *sheetCtx {
	m := c.sheets[c.cur]
	if m == nil {
		m = map[[2]int]Value{}
		c.sheets[c.cur] = m
	}
	m[[2]int{col, row}] = v
	return c
}

func (c *sheetCtx) GetCell(sheet string, col, row int) (Value, bool) {
	if sheet == "" {
		sheet = c.cur
	}
	if v, ok := c.sheets[sheet][[2]int{col, row}]; ok {
		return v, true
	}
	return BlankValue(), true
}

func (c *sheetCtx) GetRange(r Ref) (*Matrix, error) {
	sheet := r.Sheet
	if sheet == "" {
		sheet = c.cur
	}
	m := NewMatrix(r.Row2-r.Row1+1, r.Col2-r.Col1+1)
	for rr := 0; rr < m.Rows; rr++ {
		for cc := 0; cc < m.Cols; cc++ {
			if v, ok := c.sheets[sheet][[2]int{r.Col1 + cc, r.Row1 + rr}]; ok {
				m.Data[rr][cc] = v
			}
		}
	}
	return m, nil
}

func (c *sheetCtx) CurrentSheet() string { return c.cur }

func evalString(t *testing.T, ctx EvalContext, text string) Value {
	t.Helper()
	ast, err := Parse(text)
	if err != nil {
		t.Fatalf("parse %q: %v", text, err)
	}
	return NewEvaluator(ctx).Eval(ast)
}

func TestArithmeticPrecedence(t *testing.T) {
	ctx := newCtx()
	cases := map[string]float64{
		"=1+2*3":            7,
		"=(1+2)*3":          9,
		"=2^3^2":            512, // right associative: 2^(3^2)
		"=10-2-3":           5,
		"=20/4/5":           1,
		"=50%":              0.5,
		"=2+3*4^2":          50,
		"=-2^2":             4, // unary minus binds tighter than ^
		"=2*-3":             -6,
		"=1/0":              0, // error case, handled separately
		"=ROUND(3.14159,2)": 3.14,
		"=ABS(-7)":          7,
		"=7/2":              3.5,
	}
	for text, want := range cases {
		if text == "=1/0" {
			v := evalString(t, ctx, text)
			if !v.IsError() || v.Error != ErrDivZero {
				t.Errorf("%s = %v, want #DIV/0!", text, v.Display())
			}
			continue
		}
		v := evalString(t, ctx, text)
		if v.Kind != VNumber || v.Num != want {
			t.Errorf("%s = %s, want %v", text, v.Display(), want)
		}
	}
}

func TestComparisonsAndStrings(t *testing.T) {
	ctx := newCtx()
	boolCases := map[string]bool{
		"=2>1":       true,
		"=2<=2":      true,
		"=3<>4":      true,
		"=1=1":       true,
		`="a"="a"`:   true,
		`="a"<"b"`:   true,
		`="ab"&"cd"`: false, // this is string concat, handled below
	}
	for text, want := range boolCases {
		if text == `="ab"&"cd"` {
			continue
		}
		v := evalString(t, ctx, text)
		if v.Kind != VBoolean || v.Bool != want {
			t.Errorf("%s = %s want %v", text, v.Display(), want)
		}
	}
	v := evalString(t, ctx, `="ab"&"cd"`)
	if v.Str != "abcd" {
		t.Errorf("concat = %q", v.Str)
	}
	// special characters inside strings survive: quotes, operators, comma
	v = evalString(t, ctx, `="he said ""hi"", a,b:c"`)
	if v.Str != `he said "hi", a,b:c` {
		t.Errorf("special string = %q", v.Str)
	}
}

func TestLogical(t *testing.T) {
	ctx := newCtx()
	cases := map[string]bool{
		`=IF(1>0,"y","n")="y"`: true,
		"=AND(TRUE,TRUE,1)":    true,
		"=AND(TRUE,FALSE)":     false,
		"=OR(FALSE,5)":         true,
		"=NOT(1=2)":            true,
		`=IF(2>1,10/2,0)`:      true, // equals 5
	}
	for text, want := range cases {
		v := evalString(t, ctx, text)
		if v.IsError() {
			t.Errorf("%s error %s", text, v.Display())
			continue
		}
		if v.Kind == VBoolean && v.Bool != want {
			t.Errorf("%s = %v want %v", text, v.Bool, want)
		}
	}
	// Nested IF + SUM
	ctx.set(0, 0, NumberValue(60)).set(0, 1, NumberValue(70))
	v := evalString(t, ctx, `=IF(SUM(A1:A2)>100,"超标","正常")`)
	if v.Str != "超标" {
		t.Errorf("nested = %q want 超标", v.Str)
	}
	// IF must not evaluate the untaken branch (1/0 not hit).
	v = evalString(t, ctx, `=IF(1>0,"ok",1/0)`)
	if v.Str != "ok" {
		t.Errorf("lazy IF = %s want ok", v.Display())
	}
}

func TestAggregatesAndRefs(t *testing.T) {
	ctx := newCtx()
	for i := 0; i < 5; i++ {
		ctx.set(0, i, NumberValue(float64((i+1)*10))) // A1..A5 = 10..50
	}
	checks := map[string]string{
		"=SUM(A1:A5)":     "150",
		"=AVERAGE(A1:A5)": "30",
		"=MIN(A1:A5)":     "10",
		"=MAX(A1:A5)":     "50",
		"=COUNT(A1:A5)":   "5",
		"=SUM(A1:A5,B1)":  "150", // B1 blank
		"=A1+A2":          "30",
	}
	for text, want := range checks {
		v := evalString(t, ctx, text)
		if v.Display() != want {
			t.Errorf("%s = %s want %s", text, v.Display(), want)
		}
	}
	// Absolute refs evaluate the same cell.
	v := evalString(t, ctx, "=$A$3")
	if v.Num != 30 {
		t.Errorf("$A$3 = %s want 30", v.Display())
	}
	// Cross-sheet reference.
	ctx.sheets["S2"] = map[[2]int]Value{{1, 2}: NumberValue(42)}
	v = evalString(t, ctx, "=S2!B3")
	if v.Num != 42 {
		t.Errorf("S2!B3 = %s want 42", v.Display())
	}
	v = evalString(t, ctx, "=SUM(S2!B3:B3)")
	if v.Num != 42 {
		t.Errorf("SUM(S2!B3:B3) = %s want 42", v.Display())
	}
	// Empty average is #DIV/0!.
	v = evalString(t, ctx, "=AVERAGE(C1:C5)")
	if v.Error != ErrDivZero {
		t.Errorf("empty avg = %s want #DIV/0!", v.Display())
	}
}

func TestLookupFuncs(t *testing.T) {
	ctx := newCtx()
	// A1:B4 : A / .15, B / .1, C / .05
	data := [][3]float64{
		{0, 0, 0}, {1, 0, 0.15},
	}
	_ = data
	ctx.sheets[ctx.cur] = map[[2]int]Value{
		{0, 0}: StringValue("A"), {1, 0}: NumberValue(0.15),
		{0, 1}: StringValue("B"), {1, 1}: NumberValue(0.10),
		{0, 2}: StringValue("C"), {1, 2}: NumberValue(0.05),
	}
	if v := evalString(t, ctx, `=VLOOKUP("B",A1:B4,2,FALSE)`); v.Num != 0.10 {
		t.Errorf("vlookup exact = %s want 0.1", v.Display())
	}
	if v := evalString(t, ctx, `=MATCH("B",A1:A3,0)`); v.Num != 2 {
		t.Errorf("match = %s want 2", v.Display())
	}
	if v := evalString(t, ctx, `=INDEX(B1:B3,2)`); v.Num != 0.10 {
		t.Errorf("index = %s want 0.1", v.Display())
	}
	if v := evalString(t, ctx, `=VLOOKUP("Z",A1:B4,2,FALSE)`); v.Error != ErrNA {
		t.Errorf("vlookup missing = %s want #N/A", v.Display())
	}
}

func TestDeepNesting(t *testing.T) {
	ctx := newCtx()
	text := "=IF(AND(SUM(A1:A3)>5,OR(MAX(A1:A3)>=3,NOT(FALSE))),ROUND(AVERAGE(A1:A3)*2,1),0)"
	for i := 0; i < 3; i++ {
		ctx.set(0, i, NumberValue(float64(i+1)))
	}
	v := evalString(t, ctx, text)
	// avg=2 *2=4
	if v.Num != 4 {
		t.Errorf("deep nested = %s want 4", v.Display())
	}
}
