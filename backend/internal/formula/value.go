package formula

import (
	"fmt"
	"math"
	"strings"
)

// ValueType classifies runtime values.
type ValueType int

const (
	VBlank ValueType = iota
	VNumber
	VString
	VBool
	VError
)

// Value is a runtime value. Blank cells are represented by VBlank with zero
// numeric value, which matches spreadsheet semantics (empty = 0 in math,
// "" in string context).
type Value struct {
	Typ ValueType
	Num float64
	Str string
}

// Sentinel error values.
var (
	RefErr      = Value{Typ: VError, Str: "#REF!"}
	NameErr     = Value{Typ: VError, Str: "#NAME?"}
	ValueErr    = Value{Typ: VError, Str: "#VALUE!"}
	Div0Err     = Value{Typ: VError, Str: "#DIV/0!"}
	NAErr       = Value{Typ: VError, Str: "#N/A"}
	CycleErr    = Value{Typ: VError, Str: "#CYCLE!"}
	ParseErrVal = Value{Typ: VError, Str: "#PARSE?"}
)

func Errorf(format string, a ...any) Value {
	return Value{Typ: VError, Str: fmt.Sprintf(format, a...)}
}

func Number(v float64) Value { return Value{Typ: VNumber, Num: v} }
func Text(s string) Value    { return Value{Typ: VString, Str: s} }
func Boolean(b bool) Value {
	if b {
		return Value{Typ: VBool, Num: 1}
	}
	return Value{Typ: VBool, Num: 0}
}
func Blank() Value { return Value{Typ: VBlank} }

// IsErr reports whether v (or any member when it is a matrix) is an error.
func (v Value) IsErr() bool { return v.Typ == VError }

// AsNumber coerces the value to a number. Booleans become 1/0, blanks 0,
// numeric strings are parsed; other strings yield #VALUE!.
func (v Value) AsNumber() (float64, Value) {
	switch v.Typ {
	case VNumber, VBlank:
		return v.Num, Value{}
	case VBool:
		if v.Num == 1 {
			return 1, Value{}
		}
		return 0, Value{}
	case VString:
		s := strings.TrimSpace(v.Str)
		if s == "" {
			return 0, Value{}
		}
		f, err := strconvParse(s)
		if err != nil {
			return 0, ValueErr
		}
		return f, Value{}
	}
	return 0, v // error propagates
}

// AsText renders the value as a string.
func (v Value) AsText() string {
	switch v.Typ {
	case VBlank:
		return ""
	case VString:
		return v.Str
	case VBool:
		if v.Num == 1 {
			return "TRUE"
		}
		return "FALSE"
	case VError:
		return v.Str
	default:
		return formatNumber(v.Num)
	}
}

// Truth coerces to boolean: numbers != 0, TRUE, non-empty strings are truthy.
func (v Value) Truth() (bool, Value) {
	switch v.Typ {
	case VBool:
		return v.Num == 1, Value{}
	case VNumber:
		return v.Num != 0, Value{}
	case VBlank:
		return false, Value{}
	case VString:
		return v.Str != "", Value{}
	}
	return false, v
}

func formatNumber(f float64) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return "#NUM!"
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconvFormatInt(int64(f))
	}
	return strconvFormatFloat(f)
}

// Display renders a value for the grid.
func (v Value) Display() string {
	if v.Typ == VError {
		return v.Str
	}
	return v.AsText()
}
