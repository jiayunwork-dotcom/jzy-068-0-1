package formula

import (
	"math"
	"strings"
)

// Func is a built-in: it receives unevaluated arguments for laziness.
type Func func(args []Node, ev Evaler) Value

var funcs = map[string]Func{}

func register(name string, f Func) { funcs[strings.ToUpper(name)] = f }

func lookupFunc(name string) Func { return funcs[strings.ToUpper(name)] }

// argValue evaluates one argument node: ranges become #VALUE! unless 1x1.
func argValue(n Node, ev Evaler) Value {
	if r, ok := n.(*RangeNode); ok {
		if r.Col1 == r.Col2 && r.Row1 == r.Row2 {
			return ev.CellValue(r.Sheet, r.Col1, r.Row1)
		}
		return ValueErr
	}
	return Eval(n, ev)
}

// argNumbers expands ranges into their numeric members (blanks and strings
// skipped, as in SUM). Propagates the first error seen.
func argNumbers(n Node, ev Evaler) ([]float64, Value) {
	var out []float64
	var errv Value
	walkArg(n, ev, func(v Value) {
		if errv.Typ == VError {
			return
		}
		if v.Typ == VError {
			errv = v
			return
		}
		if v.Typ == VNumber || v.Typ == VBool {
			f, e := v.AsNumber()
			if e.Typ == VError {
				errv = e
				return
			}
			out = append(out, f)
		}
	})
	return out, errv
}

// argAll expands ranges into every value including blanks/text.
func argAll(n Node, ev Evaler) ([]Value, Value) {
	var out []Value
	var errv Value
	walkArg(n, ev, func(v Value) {
		if errv.Typ == VError {
			return
		}
		if v.Typ == VError {
			errv = v
			return
		}
		out = append(out, v)
	})
	return out, errv
}

func walkArg(n Node, ev Evaler, f func(Value)) {
	switch x := n.(type) {
	case *RangeNode:
		m := ev.RangeValues(x.Sheet, x.Col1, x.Row1, x.Col2, x.Row2)
		for _, v := range m.Values {
			f(v)
		}
	case *RefNode:
		f(ev.CellValue(x.Sheet, x.Col, x.Row))
	default:
		f(Eval(n, ev))
	}
}

func init() {
	register("SUM", func(a []Node, ev Evaler) Value {
		var sum float64
		for _, n := range a {
			nums, e := argNumbers(n, ev)
			if e.Typ == VError {
				return e
			}
			for _, f := range nums {
				sum += f
			}
		}
		return Number(sum)
	})

	register("AVERAGE", func(a []Node, ev Evaler) Value {
		var sum, n float64
		for _, x := range a {
			nums, e := argNumbers(x, ev)
			if e.Typ == VError {
				return e
			}
			for _, f := range nums {
				sum += f
				n++
			}
		}
		if n == 0 {
			return Div0Err
		}
		return Number(sum / n)
	})

	register("MIN", minMax(false))
	register("MAX", minMax(true))

	register("COUNT", func(a []Node, ev Evaler) Value {
		var c float64
		for _, n := range a {
			all, e := argAll(n, ev)
			if e.Typ == VError {
				return e
			}
			for _, v := range all {
				if v.Typ == VNumber || v.Typ == VBool {
					c++
				}
			}
		}
		return Number(c)
	})

	register("ROUND", func(a []Node, ev Evaler) Value {
		if len(a) < 1 || len(a) > 2 {
			return NAErr
		}
		v := argValue(a[0], ev)
		if v.IsErr() {
			return v
		}
		f, e := v.AsNumber()
		if e.Typ == VError {
			return e
		}
		digits := 0.0
		if len(a) == 2 {
			dv := argValue(a[1], ev)
			if dv.IsErr() {
				return dv
			}
			digits, e = dv.AsNumber()
			if e.Typ == VError {
				return e
			}
		}
		pow := math.Pow(10, digits)
		return Number(math.Round(f*pow) / pow)
	})

	register("ABS", func(a []Node, ev Evaler) Value {
		if len(a) != 1 {
			return NAErr
		}
		v := argValue(a[0], ev)
		if v.IsErr() {
			return v
		}
		f, e := v.AsNumber()
		if e.Typ == VError {
			return e
		}
		return Number(math.Abs(f))
	})

	register("IF", func(a []Node, ev Evaler) Value {
		if len(a) < 2 || len(a) > 3 {
			return NAErr
		}
		c := Eval(a[0], ev)
		if c.IsErr() {
			return c
		}
		t, e := c.Truth()
		if e.Typ == VError {
			return e
		}
		if t {
			return Eval(a[1], ev)
		}
		if len(a) == 3 {
			return Eval(a[2], ev)
		}
		return Boolean(false)
	})

	register("AND", boolReduce(true, true)) // identity=true, stop at false
	register("OR", boolReduce(false, true)) // identity=false, stop at true

	register("NOT", func(a []Node, ev Evaler) Value {
		if len(a) != 1 {
			return NAErr
		}
		v := argValue(a[0], ev)
		if v.IsErr() {
			return v
		}
		b, e := v.Truth()
		if e.Typ == VError {
			return e
		}
		return Boolean(!b)
	})

	register("VLOOKUP", func(a []Node, ev Evaler) Value {
		if len(a) < 3 || len(a) > 4 {
			return NAErr
		}
		key := argValue(a[0], ev)
		if key.IsErr() {
			return key
		}
		rng, ok := a[1].(*RangeNode)
		if !ok {
			return ValueErr
		}
		idxV := argValue(a[2], ev)
		if idxV.IsErr() {
			return idxV
		}
		idx, e := idxV.AsNumber()
		if e.Typ == VError {
			return e
		}
		cols := rng.Col2 - rng.Col1 + 1
		if idx < 1 || idx > float64(cols) || math.Trunc(idx) != idx {
			return RefErr
		}
		approx := true
		if len(a) == 4 {
			av := argValue(a[3], ev)
			if av.IsErr() {
				return av
			}
			b, e := av.Truth()
			if e.Typ == VError {
				return e
			}
			approx = b
		}
		rows := rng.Row2 - rng.Row1 + 1
		if approx {
			// Data must be ascending; find the last key <= target.
			last := -1
			for i := 0; i < rows; i++ {
				c := ev.CellValue(rng.Sheet, rng.Col1, rng.Row1+i)
				cmp := compareValues(c, key)
				if cmp == 0 {
					last = i
					break
				}
				if cmp < 0 {
					last = i
					continue
				}
				break
			}
			if last < 0 {
				return NAErr
			}
			return ev.CellValue(rng.Sheet, rng.Col1+int(idx)-1, rng.Row1+last)
		}
		for i := 0; i < rows; i++ {
			c := ev.CellValue(rng.Sheet, rng.Col1, rng.Row1+i)
			if compareValues(c, key) == 0 {
				return ev.CellValue(rng.Sheet, rng.Col1+int(idx)-1, rng.Row1+i)
			}
		}
		return NAErr
	})

	register("INDEX", func(a []Node, ev Evaler) Value {
		if len(a) < 2 || len(a) > 3 {
			return NAErr
		}
		rng, ok := a[0].(*RangeNode)
		if !ok {
			return ValueErr
		}
		rv := argValue(a[1], ev)
		if rv.IsErr() {
			return rv
		}
		rowIdx, e := rv.AsNumber()
		if e.Typ == VError {
			return e
		}
		colIdx := 1.0
		if len(a) == 3 {
			cv := argValue(a[2], ev)
			if cv.IsErr() {
				return cv
			}
			colIdx, e = cv.AsNumber()
			if e.Typ == VError {
				return e
			}
		}
		rows := rng.Row2 - rng.Row1 + 1
		cols := rng.Col2 - rng.Col1 + 1
		if rowIdx < 1 || rowIdx > float64(rows) || colIdx < 1 || colIdx > float64(cols) {
			return RefErr
		}
		return ev.CellValue(rng.Sheet, rng.Col1+int(colIdx)-1, rng.Row1+int(rowIdx)-1)
	})

	register("MATCH", func(a []Node, ev Evaler) Value {
		if len(a) < 2 || len(a) > 3 {
			return NAErr
		}
		key := argValue(a[0], ev)
		if key.IsErr() {
			return key
		}
		rng, ok := a[1].(*RangeNode)
		if !ok || rng.Col1 != rng.Col2 && rng.Row1 != rng.Row2 {
			return ValueErr
		}
		matchType := 1.0
		if len(a) == 3 {
			mv := argValue(a[2], ev)
			if mv.IsErr() {
				return mv
			}
			matchType, _ = mv.AsNumber()
		}
		vertical := rng.Col1 == rng.Col2
		n := 0
		at := func(i int) Value {
			if vertical {
				return ev.CellValue(rng.Sheet, rng.Col1, rng.Row1+i)
			}
			return ev.CellValue(rng.Sheet, rng.Col1+i, rng.Row1)
		}
		if vertical {
			n = rng.Row2 - rng.Row1 + 1
		} else {
			n = rng.Col2 - rng.Col1 + 1
		}
		switch {
		case matchType == 0:
			for i := 0; i < n; i++ {
				if compareValues(at(i), key) == 0 {
					return Number(float64(i + 1))
				}
			}
		case matchType > 0:
			for i := 0; i < n; i++ {
				if compareValues(at(i), key) > 0 {
					if i == 0 {
						return NAErr
					}
					return Number(float64(i))
				}
			}
			return Number(float64(n))
		default:
			for i := 0; i < n; i++ {
				if compareValues(at(i), key) < 0 {
					if i == 0 {
						return NAErr
					}
					return Number(float64(i))
				}
				if compareValues(at(i), key) == 0 {
					return Number(float64(i + 1))
				}
			}
			return Number(float64(n))
		}
		return NAErr
	})
}

func minMax(wantMax bool) Func {
	return func(a []Node, ev Evaler) Value {
		found := false
		var best float64
		for _, n := range a {
			nums, e := argNumbers(n, ev)
			if e.Typ == VError {
				return e
			}
			for _, f := range nums {
				if !found {
					best, found = f, true
				} else if wantMax && f > best || !wantMax && f < best {
					best = f
				}
			}
		}
		if !found {
			return Number(0)
		}
		return Number(best)
	}
}

func boolReduce(identity, stop bool) Func {
	return func(a []Node, ev Evaler) Value {
		for _, n := range a {
			all, e := argAll(n, ev)
			if e.Typ == VError {
				return e
			}
			for _, v := range all {
				if v.Typ == VBlank {
					continue
				}
				b, e := v.Truth()
				if e.Typ == VError {
					return e
				}
				if b == stop {
					return Boolean(stop)
				}
			}
		}
		return Boolean(identity)
	}
}
