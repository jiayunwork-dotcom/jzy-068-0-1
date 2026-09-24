// Package formula implements the spreadsheet formula language:
// lexical analysis, parsing, AST representation and evaluation.
package formula

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ValueKind enumerates the runtime value types a formula can produce.
type ValueKind int

const (
	VBlank ValueKind = iota
	VNumber
	VString
	VBoolean
	VError
)

// Error values are spreadsheet error codes that propagate through
// evaluation (e.g. referencing an error cell yields the same error).
type Error string

const (
	ErrValue    Error = "#VALUE!"
	ErrDivZero  Error = "#DIV/0!"
	ErrName     Error = "#NAME?"
	ErrNA       Error = "#N/A"
	ErrRef      Error = "#REF!"
	ErrCircular Error = "#CIRCULAR!"
	ErrNum      Error = "#NUM!"
	ErrParse    Error = "#ERROR!"
)

// Value is the runtime value. Numbers are float64, as in spreadsheets.
type Value struct {
	Kind  ValueKind
	Num   float64
	Str   string
	Bool  bool
	Error Error
}

// Constructors.
func NumberValue(v float64) Value { return Value{Kind: VNumber, Num: v} }
func StringValue(s string) Value  { return Value{Kind: VString, Str: s} }
func BoolValue(b bool) Value      { return Value{Kind: VBoolean, Bool: b} }
func ErrorValue(e Error) Value    { return Value{Kind: VError, Error: e} }
func BlankValue() Value           { return Value{Kind: VBlank} }

// IsError reports whether v (or any member of a matrix) is an error.
func (v Value) IsError() bool { return v.Kind == VError }

// AsError returns the error carried by the value.
func (v Value) AsError() error { return fmt.Errorf("%s", v.Error) }

// Display renders a value the way a cell would show it.
func (v Value) Display() string {
	switch v.Kind {
	case VBlank:
		return ""
	case VNumber:
		return FormatNumber(v.Num)
	case VString:
		return v.Str
	case VBoolean:
		if v.Bool {
			return "TRUE"
		}
		return "FALSE"
	case VError:
		return string(v.Error)
	}
	return ""
}

// FormatNumber renders floats without an exponent for the common range
// and drops a trailing ".0" for integers.
func FormatNumber(n float64) string {
	if math.IsInf(n, 0) {
		return string(ErrNum)
	}
	if math.IsNaN(n) {
		return string(ErrNum)
	}
	if n == math.Trunc(n) && math.Abs(n) < 1e15 {
		return strconv.FormatFloat(n, 'f', 0, 64)
	}
	s := strconv.FormatFloat(n, 'g', -1, 64)
	return s
}

// AsNumber converts the value to a number following spreadsheet coercion
// rules. Blank -> 0, booleans -> 1/0, numeric strings are parsed.
func (v Value) AsNumber() (float64, error) {
	switch v.Kind {
	case VNumber:
		return v.Num, nil
	case VBlank:
		return 0, nil
	case VBoolean:
		if v.Bool {
			return 1, nil
		}
		return 0, nil
	case VError:
		return 0, v.AsError()
	case VString:
		f, err := strconv.ParseFloat(strings.TrimSpace(v.Str), 64)
		if err != nil {
			return 0, fmt.Errorf("%s: %q is not a number", ErrValue, v.Str)
		}
		return f, nil
	}
	return 0, fmt.Errorf("%s", ErrValue)
}

// AsBoolean converts a value to a boolean (spreadsheet truthiness rules).
func (v Value) AsBoolean() (bool, error) {
	switch v.Kind {
	case VBoolean:
		return v.Bool, nil
	case VNumber:
		return v.Num != 0, nil
	case VBlank:
		return false, nil
	case VError:
		return false, v.AsError()
	case VString:
		s := strings.ToUpper(strings.TrimSpace(v.Str))
		if s == "TRUE" {
			return true, nil
		}
		if s == "FALSE" || s == "" {
			return false, nil
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f != 0, nil
		}
		return false, fmt.Errorf("%s: %q is not boolean", ErrValue, v.Str)
	}
	return false, fmt.Errorf("%s", ErrValue)
}

// Compare returns -1/0/1 comparing two values. Numbers sort below strings,
// strings sort below booleans; blanks sort below everything. An error value
// is never comparable.
func Compare(a, b Value) (int, error) {
	if a.Kind == VError {
		return 0, a.AsError()
	}
	if b.Kind == VError {
		return 0, b.AsError()
	}
	// Blank vs blank / blank vs a value.
	if a.Kind == VBlank && b.Kind == VBlank {
		return 0, nil
	}
	if a.Kind == VBlank {
		return -1, nil
	}
	if b.Kind == VBlank {
		return 1, nil
	}
	// Numeric family: numbers and booleans coerce and compare numerically.
	if (a.Kind == VNumber || a.Kind == VBoolean) && (b.Kind == VNumber || b.Kind == VBoolean) {
		an, _ := a.AsNumber()
		bn, _ := b.AsNumber()
		return numCmp(an, bn), nil
	}
	if a.Kind == VString && b.Kind == VString {
		switch {
		case a.Str < b.Str:
			return -1, nil
		case a.Str > b.Str:
			return 1, nil
		}
		return 0, nil
	}
	// Different families: compare by kind rank (number < string < boolean).
	return int(rank(a.Kind)) - int(rank(b.Kind)), nil
}

func rank(k ValueKind) int {
	switch k {
	case VNumber:
		return 0
	case VString:
		return 1
	case VBoolean:
		return 2
	}
	return -1
}

func numCmp(a, b float64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// Equal compares two values for exact equality (used by MATCH / VLOOKUP
// exact mode and by logical operators).
func Equal(a, b Value) (bool, error) {
	c, err := Compare(a, b)
	return c == 0, err
}

// Matrix is the value of a range reference A1:B3: rows of values in
// row-major order.
type Matrix struct {
	Rows, Cols int
	Data       [][]Value // Data[r][c]
}

func NewMatrix(rows, cols int) *Matrix {
	m := &Matrix{Rows: rows, Cols: cols, Data: make([][]Value, rows)}
	for r := range m.Data {
		m.Data[r] = make([]Value, cols)
	}
	return m
}

func (m *Matrix) Get(r, c int) Value {
	if r < 0 || r >= m.Rows || c < 0 || c >= m.Cols {
		return ErrorValue(ErrRef)
	}
	return m.Data[r][c]
}

func (m *Matrix) Values() []Value {
	out := make([]Value, 0, m.Rows*m.Cols)
	for _, row := range m.Data {
		out = append(out, row...)
	}
	return out
}
