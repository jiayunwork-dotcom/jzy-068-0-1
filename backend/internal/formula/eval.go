package formula

import (
	"fmt"
	"math"
)

// Lookup resolves a single cell during evaluation. Returning an error here
// surfaces as #ERROR! (structural problems); normal formula errors should be
// returned inside a Value of kind VError so they propagate per-cell.
type Lookup func(sheet string, col, row int) (Value, bool)

// RangeLookup resolves a whole rectangle for a range reference. Cells outside
// the sheet dimensions are returned as blanks/errors as the implementation
// sees fit.
type RangeLookup func(ref Ref) (*Matrix, error)

// EvalContext provides the workbook the evaluator reads from.
type EvalContext interface {
	GetCell(sheet string, col, row int) (Value, bool)
	GetRange(r Ref) (*Matrix, error)
	CurrentSheet() string
}

// Evaluator holds no per-cell state; the same instance serves the whole
// recalculation pass.
type Evaluator struct {
	ctx EvalContext
}

func NewEvaluator(ctx EvalContext) *Evaluator {
	return &Evaluator{ctx: ctx}
}

// Eval evaluates an AST node. Range references inside a scalar context are
// collapsed: single-cell ranges behave like scalars, multi-cell ranges are
// passed to functions as *Matrix (see CallNode handling).
func (e *Evaluator) Eval(n Node) Value {
	switch x := n.(type) {
	case *NumberNode:
		return NumberValue(x.Value)
	case *StringNode:
		return StringValue(x.Value)
	case *BoolNode:
		return BoolValue(x.Value)
	case *ErrorNode:
		return ErrorValue(x.Value)
	case *RefErrNode:
		return ErrorValue(ErrRef)
	case *BlankArgNode:
		return BlankValue()
	case *RefNode:
		return e.evalRef(x.Ref)
	case *UnaryNode:
		return e.evalUnary(x)
	case *BinaryNode:
		return e.evalBinary(x)
	case *CallNode:
		return e.evalCall(x)
	}
	return ErrorValue(ErrValue)
}

func (e *Evaluator) evalRef(r Ref) Value {
	if r.IsError {
		return ErrorValue(ErrRef)
	}
	sheet := r.Sheet
	if sheet == "" {
		sheet = e.ctx.CurrentSheet()
	}
	if r.IsRange {
		// A range in scalar position: allowed only when it covers one cell.
		if r.Col1 == r.Col2 && r.Row1 == r.Row2 {
			v, ok := e.ctx.GetCell(sheet, r.Col1, r.Row1)
			if !ok {
				return BlankValue()
			}
			return v
		}
		return ErrorValue(ErrValue)
	}
	v, ok := e.ctx.GetCell(sheet, r.Col1, r.Row1)
	if !ok {
		return BlankValue()
	}
	return v
}

func (e *Evaluator) evalUnary(u *UnaryNode) Value {
	v := e.Eval(u.Expr)
	if v.IsError() {
		return v
	}
	switch u.Op {
	case "-":
		if u.Postfix { // impossible, percent handled below
			break
		}
		f, err := v.AsNumber()
		if err != nil {
			return errToValue(err)
		}
		return NumberValue(-f)
	case "+":
		f, err := v.AsNumber()
		if err != nil {
			return errToValue(err)
		}
		return NumberValue(f)
	case "%":
		f, err := v.AsNumber()
		if err != nil {
			return errToValue(err)
		}
		return NumberValue(f / 100)
	}
	return ErrorValue(ErrValue)
}

func (e *Evaluator) evalBinary(b *BinaryNode) Value {
	lv := e.Eval(b.Lhs)
	rv := e.Eval(b.Rhs)
	if lv.IsError() {
		return lv
	}
	if rv.IsError() {
		return rv
	}
	switch b.Op {
	case "+", "-", "*", "/", "^":
		return arith(b.Op, lv, rv)
	case "&":
		return StringValue(lv.Display() + rv.Display())
	case "=", "<>", "<", "<=", ">", ">=":
		return compareOp(b.Op, lv, rv)
	}
	return ErrorValue(ErrValue)
}

func arith(op string, a, b Value) Value {
	an, err := a.AsNumber()
	if err != nil {
		return errToValue(err)
	}
	bn, err := b.AsNumber()
	if err != nil {
		return errToValue(err)
	}
	switch op {
	case "+":
		return NumberValue(an + bn)
	case "-":
		return NumberValue(an - bn)
	case "*":
		return NumberValue(an * bn)
	case "/":
		if bn == 0 {
			return ErrorValue(ErrDivZero)
		}
		return NumberValue(an / bn)
	case "^":
		r := math.Pow(an, bn)
		if math.IsNaN(r) || math.IsInf(r, 0) {
			return ErrorValue(ErrNum)
		}
		return NumberValue(r)
	}
	return ErrorValue(ErrValue)
}

func compareOp(op string, a, b Value) Value {
	c, err := Compare(a, b)
	if err != nil {
		return errToValue(err)
	}
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
	return BoolValue(ok)
}

// errToValue converts an AsNumber/AsBoolean error carrying a spreadsheet
// error code into the matching Value.
func errToValue(err error) Value {
	if err == nil {
		return BlankValue()
	}
	msg := err.Error()
	switch Error(msg) {
	case ErrValue, ErrDivZero, ErrName, ErrNA,
		ErrRef, ErrNum, ErrCircular, ErrParse:
		return ErrorValue(Error(msg))
	}
	// errors constructed with extra context start with the code prefix
	for _, code := range []Error{ErrValue, ErrDivZero, ErrName, ErrNA, ErrRef, ErrNum, ErrCircular, ErrParse} {
		if hasPrefix(msg, string(code)) {
			return ErrorValue(code)
		}
	}
	return ErrorValue(ErrValue)
}

func hasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}

var _ = fmt.Sprintf
