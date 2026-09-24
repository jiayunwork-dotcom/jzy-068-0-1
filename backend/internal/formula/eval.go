package formula

import (
	"math"
)

// Evaler supplies cell/range values to the evaluator. The engine implements
// it against the current (topologically ordered) workbook state.
type Evaler interface {
	CellValue(sheet string, col, row int) Value
	RangeValues(sheet string, c1, r1, c2, r2 int) Matrix
}

// Matrix is a rectangular set of values in row-major order.
type Matrix struct {
	Rows, Cols int
	Values     []Value
}

// Get returns the value at 0-based (r, c).
func (m Matrix) Get(r, c int) Value { return m.Values[r*m.Cols+c] }

// Flatten returns the matrix values as a slice (scalar args wrap to len 1).
func (m Matrix) Flatten() []Value { return m.Values }

// Eval computes the value of an AST node.
func Eval(n Node, ev Evaler) Value {
	switch x := n.(type) {
	case *NumberNode:
		return Number(x.Value)
	case *StringNode:
		return Text(x.Value)
	case *BoolNode:
		return Boolean(x.Value)
	case *ErrNode:
		return x.Value
	case *RefNode:
		return ev.CellValue(x.Sheet, x.Col, x.Row)
	case *RangeNode:
		// A bare range in scalar position yields #VALUE! (unless 1x1).
		if x.Col1 == x.Col2 && x.Row1 == x.Row2 {
			return ev.CellValue(x.Sheet, x.Col1, x.Row1)
		}
		return ValueErr
	case *UnaryNode:
		v := Eval(x.Inner, ev)
		if v.IsErr() {
			return v
		}
		switch x.Op {
		case "-":
			f, e := v.AsNumber()
			if e.Typ == VError {
				return e
			}
			return Number(-f)
		case "+":
			f, e := v.AsNumber()
			if e.Typ == VError {
				return e
			}
			return Number(f)
		case "%":
			f, e := v.AsNumber()
			if e.Typ == VError {
				return e
			}
			return Number(f / 100)
		}
		return ValueErr
	case *BinaryNode:
		return evalBinary(x, ev)
	case *CallNode:
		fn := lookupFunc(x.Name)
		if fn == nil {
			return NameErr
		}
		return fn(x.Args, ev)
	}
	return ValueErr
}

func evalBinary(x *BinaryNode, ev Evaler) Value {
	// IF has to be lazy, but ordinary binary operators evaluate both sides.
	lv := Eval(x.Left, ev)
	if lv.IsErr() {
		return lv
	}
	rv := Eval(x.Right, ev)
	if rv.IsErr() {
		return rv
	}
	switch x.Op {
	case "+", "-", "*", "/", "^":
		return evalArith(x.Op, lv, rv)
	case "&":
		return Text(lv.AsText() + rv.AsText())
	case "=", "<>", "<", "<=", ">", ">=":
		return evalCompare(x.Op, lv, rv)
	}
	return ValueErr
}

func evalArith(op string, a, b Value) Value {
	af, ea := a.AsNumber()
	if ea.Typ == VError {
		return ea
	}
	bf, eb := b.AsNumber()
	if eb.Typ == VError {
		return eb
	}
	var r float64
	switch op {
	case "+":
		r = af + bf
	case "-":
		r = af - bf
	case "*":
		r = af * bf
	case "/":
		if bf == 0 {
			return Div0Err
		}
		r = af / bf
	case "^":
		r = math.Pow(af, bf)
		if math.IsNaN(r) {
			return ValueErr
		}
	}
	if math.IsInf(r, 0) || math.IsNaN(r) {
		return Errorf("#NUM!")
	}
	return Number(r)
}

// compareOrder: -1 less, 0 equal, 1 greater. Mixed numeric/string follows
// spreadsheet convention: numbers < strings < booleans; blank counts as 0/"".
func evalCompare(op string, a, b Value) Value {
	c := compareValues(a, b)
	var ok bool
	switch op {
	case "=":
		ok = c == 0
	case "<>":
		ok = c != 0
	case "<":
		ok = c < 0
	case "<=":
		ok = c <= 0
	case ">":
		ok = c > 0
	case ">=":
		ok = c >= 0
	}
	return Boolean(ok)
}

func compareValues(a, b Value) int {
	ca, cb := compareKind(a), compareKind(b)
	if ca != cb {
		if ca < cb {
			return -1
		}
		return 1
	}
	switch ca {
	case 0: // number / blank
		af, _ := a.AsNumber()
		bf, _ := b.AsNumber()
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		}
		return 0
	case 1:
		switch {
		case a.Str < b.Str:
			return -1
		case a.Str > b.Str:
			return 1
		}
		return 0
	default:
		at, _ := a.Truth()
		bt, _ := b.Truth()
		if at == bt {
			return 0
		}
		if !at {
			return -1
		}
		return 1
	}
}

// compareKind groups blank with numbers, then strings, then booleans.
func compareKind(v Value) int {
	switch v.Typ {
	case VBlank, VNumber:
		return 0
	case VString:
		return 1
	default:
		return 2
	}
}
