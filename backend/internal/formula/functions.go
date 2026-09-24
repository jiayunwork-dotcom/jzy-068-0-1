package formula

import (
	"fmt"
	"math"
	"strings"
)

// evalCall dispatches built-in functions. Arguments are kept as AST nodes so
// that IF can evaluate lazily and so range arguments can be resolved to
// matrices rather than scalars.
func (e *Evaluator) evalCall(c *CallNode) Value {
	fn, ok := builtins[c.Name]
	if !ok {
		return ErrorValue(ErrName)
	}
	return fn(e, c.Args)
}

type builtinFunc func(e *Evaluator, args []Node) Value

// builtins is populated in init() to avoid a package initialization cycle:
// the function values call Evaluator methods that themselves dispatch
// through builtins via evalCall.
var builtins map[string]builtinFunc

func init() {
	builtins = map[string]builtinFunc{
		// math
		"SUM":     fnSum,
		"AVERAGE": fnAverage,
		"MIN":     fnMin,
		"MAX":     fnMax,
		"COUNT":   fnCount,
		"ROUND":   fnRound,
		"ABS":     fnAbs,
		// logical
		"IF":    fnIf,
		"AND":   fnAnd,
		"OR":    fnOr,
		"NOT":   fnNot,
		"TRUE":  func(e *Evaluator, a []Node) Value { return BoolValue(true) },
		"FALSE": func(e *Evaluator, a []Node) Value { return BoolValue(false) },
		// lookup
		"VLOOKUP": fnVLookup,
		"INDEX":   fnIndex,
		"MATCH":   fnMatch,
	}
}

// ---- argument resolution -------------------------------------------------

// argScalar evaluates one argument as a scalar value.
func (e *Evaluator) argScalar(n Node) Value {
	if rn, ok := n.(*RefNode); ok && rn.Ref.IsRange {
		// A 1x1 range is fine.
		if rn.Ref.Col1 == rn.Ref.Col2 && rn.Ref.Row1 == rn.Ref.Row2 {
			return e.Eval(n)
		}
		return ErrorValue(ErrValue)
	}
	return e.Eval(n)
}

// argMatrix resolves an argument to a flat list of values. Range refs are
// expanded; scalars become a single element. Blank args produce nothing.
func (e *Evaluator) argFlat(n Node) ([]Value, Value) {
	switch x := n.(type) {
	case *BlankArgNode:
		return nil, Value{}
	case *RefNode:
		sheet := x.Ref.Sheet
		if sheet == "" {
			sheet = e.ctx.CurrentSheet()
		}
		if x.Ref.IsRange {
			m, err := e.ctx.GetRange(x.Ref)
			if err != nil {
				return nil, ErrorValue(ErrRef)
			}
			return m.Values(), Value{}
		}
		v, ok := e.ctx.GetCell(sheet, x.Ref.Col1, x.Ref.Row1)
		if !ok {
			return nil, Value{}
		}
		return []Value{v}, Value{}
	}
	v := e.Eval(n)
	if v.IsError() {
		return nil, v
	}
	return []Value{v}, Value{}
}

// numbersForAgg flattens all arguments into numeric values using spreadsheet
// aggregation rules: matrix cells must be numbers (booleans/blank/text are
// skipped); scalar args coerce via AsNumber (so TRUE, "5", blank work).
// Any error propagates immediately.
func (e *Evaluator) numbersForAgg(args []Node) ([]float64, Value) {
	var nums []float64
	for _, a := range args {
		if rn, ok := a.(*RefNode); ok && rn.Ref.IsRange {
			ref := rn.Ref
			if ref.Sheet == "" {
				ref.Sheet = e.ctx.CurrentSheet()
			}
			m, err := e.ctx.GetRange(ref)
			if err != nil {
				return nil, ErrorValue(ErrRef)
			}
			for _, v := range m.Values() {
				if v.Kind == VError {
					return nil, v
				}
				if v.Kind == VNumber {
					nums = append(nums, v.Num)
				}
			}
			continue
		}
		flat, errv := e.argFlat(a)
		if errv.IsError() {
			return nil, errv
		}
		for _, v := range flat {
			if v.Kind == VError {
				return nil, v
			}
			f, err := v.AsNumber()
			if err != nil {
				return nil, errToValue(err)
			}
			if v.Kind != VBlank {
				nums = append(nums, f)
			}
		}
	}
	return nums, Value{}
}

// ---- math ----------------------------------------------------------------

func fnSum(e *Evaluator, args []Node) Value {
	nums, ev := e.numbersForAgg(args)
	if ev.IsError() {
		return ev
	}
	var sum float64
	for _, n := range nums {
		sum += n
	}
	return NumberValue(sum)
}

func fnAverage(e *Evaluator, args []Node) Value {
	nums, ev := e.numbersForAgg(args)
	if ev.IsError() {
		return ev
	}
	if len(nums) == 0 {
		return ErrorValue(ErrDivZero)
	}
	var sum float64
	for _, n := range nums {
		sum += n
	}
	return NumberValue(sum / float64(len(nums)))
}

func fnMin(e *Evaluator, args []Node) Value {
	nums, ev := e.numbersForAgg(args)
	if ev.IsError() {
		return ev
	}
	if len(nums) == 0 {
		return NumberValue(0)
	}
	m := nums[0]
	for _, n := range nums[1:] {
		if n < m {
			m = n
		}
	}
	return NumberValue(m)
}

func fnMax(e *Evaluator, args []Node) Value {
	nums, ev := e.numbersForAgg(args)
	if ev.IsError() {
		return ev
	}
	if len(nums) == 0 {
		return NumberValue(0)
	}
	m := nums[0]
	for _, n := range nums[1:] {
		if n > m {
			m = n
		}
	}
	return NumberValue(m)
}

func fnCount(e *Evaluator, args []Node) Value {
	count := 0
	for _, a := range args {
		if rn, ok := a.(*RefNode); ok && rn.Ref.IsRange {
			ref := rn.Ref
			if ref.Sheet == "" {
				ref.Sheet = e.ctx.CurrentSheet()
			}
			m, err := e.ctx.GetRange(ref)
			if err != nil {
				return ErrorValue(ErrRef)
			}
			for _, v := range m.Values() {
				if v.Kind == VError {
					return v
				}
				if v.Kind == VNumber {
					count++
				}
			}
			continue
		}
		v := e.argScalar(a)
		if v.Kind == VError {
			return v
		}
		if v.Kind == VNumber {
			count++
		}
	}
	return NumberValue(float64(count))
}

func fnRound(e *Evaluator, args []Node) Value {
	if len(args) < 1 || len(args) > 2 {
		return ErrorValue(ErrValue)
	}
	xv := e.argScalar(args[0])
	if xv.IsError() {
		return xv
	}
	x, err := xv.AsNumber()
	if err != nil {
		return errToValue(err)
	}
	digits := 0
	if len(args) == 2 {
		dv := e.argScalar(args[1])
		if dv.IsError() {
			return dv
		}
		d, err := dv.AsNumber()
		if err != nil {
			return errToValue(err)
		}
		digits = int(d)
	}
	pow := math.Pow(10, float64(digits))
	shifted := x * pow
	// Half away from zero (spreadsheet ROUND), unlike Go's math.Round? Go's
	// Round is half-away-from-zero already.
	r := math.Round(shifted) / pow
	return NumberValue(r)
}

func fnAbs(e *Evaluator, args []Node) Value {
	if len(args) != 1 {
		return ErrorValue(ErrValue)
	}
	v := e.argScalar(args[0])
	if v.IsError() {
		return v
	}
	f, err := v.AsNumber()
	if err != nil {
		return errToValue(err)
	}
	return NumberValue(math.Abs(f))
}

// ---- logical -------------------------------------------------------------

func fnIf(e *Evaluator, args []Node) Value {
	if len(args) < 2 || len(args) > 3 {
		return ErrorValue(ErrValue)
	}
	cond := e.argScalar(args[0])
	if cond.IsError() {
		return cond
	}
	b, err := cond.AsBoolean()
	if err != nil {
		return errToValue(err)
	}
	if b {
		if _, ok := args[1].(*BlankArgNode); ok {
			return NumberValue(0)
		}
		return e.Eval(args[1])
	}
	if len(args) == 3 {
		if _, ok := args[2].(*BlankArgNode); ok {
			return NumberValue(0)
		}
		return e.Eval(args[2])
	}
	return BoolValue(false)
}

func boolsForLogic(e *Evaluator, args []Node) ([]bool, Value) {
	var out []bool
	for _, a := range args {
		if rn, ok := a.(*RefNode); ok && rn.Ref.IsRange {
			ref := rn.Ref
			if ref.Sheet == "" {
				ref.Sheet = e.ctx.CurrentSheet()
			}
			m, err := e.ctx.GetRange(ref)
			if err != nil {
				return nil, ErrorValue(ErrRef)
			}
			for _, v := range m.Values() {
				if v.Kind == VError {
					return nil, v
				}
				switch v.Kind {
				case VBoolean:
					out = append(out, v.Bool)
				case VNumber:
					out = append(out, v.Num != 0)
				default:
					// blank/text inside a range are skipped
				}
			}
			continue
		}
		v := e.argScalar(a)
		if v.IsError() {
			return nil, v
		}
		if v.Kind == VBlank {
			continue
		}
		b, err := v.AsBoolean()
		if err != nil {
			return nil, errToValue(err)
		}
		out = append(out, b)
	}
	return out, Value{}
}

func fnAnd(e *Evaluator, args []Node) Value {
	bs, ev := boolsForLogic(e, args)
	if ev.IsError() {
		return ev
	}
	if len(bs) == 0 {
		return ErrorValue(ErrValue)
	}
	for _, b := range bs {
		if !b {
			return BoolValue(false)
		}
	}
	return BoolValue(true)
}

func fnOr(e *Evaluator, args []Node) Value {
	bs, ev := boolsForLogic(e, args)
	if ev.IsError() {
		return ev
	}
	if len(bs) == 0 {
		return ErrorValue(ErrValue)
	}
	for _, b := range bs {
		if b {
			return BoolValue(true)
		}
	}
	return BoolValue(false)
}

func fnNot(e *Evaluator, args []Node) Value {
	if len(args) != 1 {
		return ErrorValue(ErrValue)
	}
	v := e.argScalar(args[0])
	if v.IsError() {
		return v
	}
	b, err := v.AsBoolean()
	if err != nil {
		return errToValue(err)
	}
	return BoolValue(!b)
}

// ---- lookup --------------------------------------------------------------

// matrixArg resolves an argument which must be a range to its matrix.
func (e *Evaluator) matrixArg(n Node) (*Matrix, Value) {
	rn, ok := n.(*RefNode)
	if !ok || !rn.Ref.IsRange {
		return nil, ErrorValue(ErrValue)
	}
	ref := rn.Ref
	if ref.Sheet == "" {
		ref.Sheet = e.ctx.CurrentSheet()
	}
	m, err := e.ctx.GetRange(ref)
	if err != nil {
		return nil, ErrorValue(ErrRef)
	}
	return m, Value{}
}

func fnVLookup(e *Evaluator, args []Node) Value {
	if len(args) < 3 || len(args) > 4 {
		return ErrorValue(ErrValue)
	}
	key := e.argScalar(args[0])
	if key.IsError() {
		return key
	}
	tbl, ev := e.matrixArg(args[1])
	if ev.IsError() {
		return ev
	}
	colIdxV := e.argScalar(args[2])
	if colIdxV.IsError() {
		return colIdxV
	}
	colIdx, err := colIdxV.AsNumber()
	if err != nil || colIdx != math.Trunc(colIdx) {
		return ErrorValue(ErrValue)
	}
	if colIdx < 1 || int(colIdx) > tbl.Cols {
		return ErrorValue(ErrRef)
	}
	approx := true
	if len(args) == 4 {
		rv := e.argScalar(args[3])
		if rv.IsError() {
			return rv
		}
		b, err := rv.AsBoolean()
		if err != nil {
			return errToValue(err)
		}
		approx = b
	}

	if approx {
		// Approximate match: largest first-column value <= key, data assumed
		// sorted ascending.
		matchRow := -1
		for r := 0; r < tbl.Rows; r++ {
			cv := tbl.Data[r][0]
			if cv.Kind == VError {
				return cv
			}
			cmp, err := Compare(cv, key)
			if err != nil {
				return errToValue(err)
			}
			if cmp <= 0 {
				matchRow = r
			} else {
				break
			}
		}
		if matchRow < 0 {
			return ErrorValue(ErrNA)
		}
		return tbl.Data[matchRow][int(colIdx)-1]
	}
	// Exact match, top to bottom.
	for r := 0; r < tbl.Rows; r++ {
		eq, err := Equal(tbl.Data[r][0], key)
		if err != nil {
			return errToValue(err)
		}
		if eq {
			return tbl.Data[r][int(colIdx)-1]
		}
	}
	return ErrorValue(ErrNA)
}

func fnIndex(e *Evaluator, args []Node) Value {
	if len(args) < 2 || len(args) > 3 {
		return ErrorValue(ErrValue)
	}
	m, ev := e.matrixArg(args[0])
	if ev.IsError() {
		return ev
	}
	rv := e.argScalar(args[1])
	if rv.IsError() {
		return rv
	}
	row, err := rv.AsNumber()
	if err != nil || row != math.Trunc(row) {
		return ErrorValue(ErrValue)
	}
	if len(args) == 2 {
		// 1-D semantics: nth element in row-major order.
		idx := int(row)
		if idx < 1 || idx > m.Rows*m.Cols {
			return ErrorValue(ErrRef)
		}
		idx--
		return m.Data[idx/m.Cols][idx%m.Cols]
	}
	cv := e.argScalar(args[2])
	if cv.IsError() {
		return cv
	}
	col, err := cv.AsNumber()
	if err != nil || col != math.Trunc(col) {
		return ErrorValue(ErrValue)
	}
	if row < 1 || int(row) > m.Rows || col < 1 || int(col) > m.Cols {
		return ErrorValue(ErrRef)
	}
	return m.Data[int(row)-1][int(col)-1]
}

func fnMatch(e *Evaluator, args []Node) Value {
	if len(args) < 2 || len(args) > 3 {
		return ErrorValue(ErrValue)
	}
	key := e.argScalar(args[0])
	if key.IsError() {
		return key
	}
	rn, ok := args[1].(*RefNode)
	if !ok || !rn.Ref.IsRange {
		return ErrorValue(ErrValue)
	}
	ref := rn.Ref
	if ref.Sheet == "" {
		ref.Sheet = e.ctx.CurrentSheet()
	}
	m, err := e.ctx.GetRange(ref)
	if err != nil {
		return ErrorValue(ErrRef)
	}
	// INDEX/MATCH work on a one-dimensional lookup array.
	var vec []Value
	if m.Rows == 1 {
		vec = m.Data[0]
	} else if m.Cols == 1 {
		for r := range m.Data {
			vec = append(vec, m.Data[r][0])
		}
	} else {
		return ErrorValue(ErrNA)
	}
	matchType := 1.0
	if len(args) == 3 {
		tv := e.argScalar(args[2])
		if tv.IsError() {
			return tv
		}
		t, err := tv.AsNumber()
		if err != nil {
			return errToValue(err)
		}
		matchType = t
	}

	switch matchType {
	case 0: // exact, first match
		for i, v := range vec {
			if v.Kind == VError {
				continue
			}
			eq, err := Equal(v, key)
			if err != nil {
				return errToValue(err)
			}
			if eq {
				return NumberValue(float64(i + 1))
			}
		}
		return ErrorValue(ErrNA)
	case 1: // largest <= key, ascending
		best := -1
		for i, v := range vec {
			cmp, err := Compare(v, key)
			if err != nil {
				return errToValue(err)
			}
			if cmp <= 0 {
				best = i
			} else {
				break
			}
		}
		if best < 0 {
			return ErrorValue(ErrNA)
		}
		return NumberValue(float64(best + 1))
	case -1: // smallest >= key, descending
		best := -1
		for i, v := range vec {
			cmp, err := Compare(v, key)
			if err != nil {
				return errToValue(err)
			}
			if cmp >= 0 {
				best = i
			} else {
				break
			}
		}
		if best < 0 {
			return ErrorValue(ErrNA)
		}
		return NumberValue(float64(best + 1))
	}
	return ErrorValue(ErrNA)
}

var _ = strings.TrimSpace
var _ = fmt.Sprintf
