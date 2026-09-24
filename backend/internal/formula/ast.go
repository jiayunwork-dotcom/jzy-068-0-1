package formula

import (
	"strconv"
	"strings"
)

// Node is any expression in the AST.
type Node interface {
	// String renders the node back to canonical formula text (without '=').
	String() string
}

// NumberNode is a numeric literal.
type NumberNode struct{ Value float64 }

func (n *NumberNode) String() string {
	s := strconv.FormatFloat(n.Value, 'g', -1, 64)
	return s
}

// StringNode is a string literal (already unquoted).
type StringNode struct{ Value string }

func (n *StringNode) String() string { return `"` + strings.ReplaceAll(n.Value, `"`, `""`) + `"` }

// BoolNode is TRUE / FALSE.
type BoolNode struct{ Value bool }

func (n *BoolNode) String() string {
	if n.Value {
		return "TRUE"
	}
	return "FALSE"
}

// ErrNode is an error literal such as #REF! or a propagated error value.
type ErrNode struct{ Value Value }

func (n *ErrNode) String() string { return n.Value.Str }

// RefNode is a single cell reference, optionally absolute per axis,
// optionally on another sheet (Sheet != "" means cross-sheet).
type RefNode struct {
	Sheet  string // "" => same sheet as the containing formula
	Col    int    // 0-based
	Row    int    // 0-based
	AbsCol bool
	AbsRow bool
}

func colLetters(col int) string {
	col++
	var b []byte
	for col > 0 {
		col--
		b = append([]byte{byte('A' + col%26)}, b...)
		col /= 26
	}
	return string(b)
}

func colIndex(letters string) int {
	v := 0
	for i := 0; i < len(letters); i++ {
		c := letters[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		v = v*26 + int(c-'A'+1)
	}
	return v - 1
}

func (r *RefNode) String() string {
	var b strings.Builder
	if r.Sheet != "" {
		b.WriteString(r.Sheet)
		b.WriteByte('!')
	}
	if r.AbsCol {
		b.WriteByte('$')
	}
	b.WriteString(colLetters(r.Col))
	if r.AbsRow {
		b.WriteByte('$')
	}
	b.WriteString(strconv.Itoa(r.Row + 1))
	return b.String()
}

// RangeNode is a rectangular A1:B10 reference.
type RangeNode struct {
	Sheet                  string
	Col1, Row1, Col2, Row2 int
	AbsCol1, AbsRow1       bool
	AbsCol2, AbsRow2       bool
}

func (g *RangeNode) end(r *RefNode, absCol, absRow bool) {
	g.Col2, g.Row2 = r.Col, r.Row
	g.AbsCol2, g.AbsRow2 = absCol, absRow
}

func (g *RangeNode) String() string {
	var b strings.Builder
	if g.Sheet != "" {
		b.WriteString(g.Sheet)
		b.WriteByte('!')
	}
	if g.AbsCol1 {
		b.WriteByte('$')
	}
	b.WriteString(colLetters(g.Col1))
	if g.AbsRow1 {
		b.WriteByte('$')
	}
	b.WriteString(strconv.Itoa(g.Row1 + 1))
	b.WriteByte(':')
	if g.AbsCol2 {
		b.WriteByte('$')
	}
	b.WriteString(colLetters(g.Col2))
	if g.AbsRow2 {
		b.WriteByte('$')
	}
	b.WriteString(strconv.Itoa(g.Row2 + 1))
	return b.String()
}

// UnaryNode applies Op to Inner. Op is "-" or "+" or "%" (postfix).
type UnaryNode struct {
	Op    string
	Inner Node
	Post  bool
}

func (n *UnaryNode) String() string {
	if n.Post {
		return wrap(n.Inner) + n.Op
	}
	return n.Op + wrap(n.Inner)
}

// BinaryNode applies Op to Left and Right.
type BinaryNode struct {
	Op          string
	Left, Right Node
}

func (n *BinaryNode) String() string {
	return child(n.Op, n.Left, false) + n.Op + child(n.Op, n.Right, true)
}

// opPrecedence mirrors the parser's precedence table.
func opPrecedence(op string) int {
	switch op {
	case "=", "<>", "<", "<=", ">", ">=":
		return 1
	case "&":
		return 2
	case "+", "-":
		return 3
	case "*", "/":
		return 4
	case "^":
		return 5
	}
	return 0
}

// child renders a binary operand, adding parentheses only where the child's
// precedence requires grouping. '^' is right-associative.
func child(parentOp string, n Node, rightSide bool) string {
	b, ok := n.(*BinaryNode)
	if !ok {
		return n.String()
	}
	cp, pp := opPrecedence(b.Op), opPrecedence(parentOp)
	need := cp < pp
	if cp == pp && (parentOp != "^" && rightSide || parentOp == "^" && !rightSide) {
		need = true
	}
	if need {
		return "(" + b.String() + ")"
	}
	return b.String()
}

// CallNode is a function call: Name(Args...).
type CallNode struct {
	Name string
	Args []Node
}

func (n *CallNode) String() string {
	parts := make([]string, len(n.Args))
	for i, a := range n.Args {
		parts[i] = a.String()
	}
	return strings.ToUpper(n.Name) + "(" + strings.Join(parts, ",") + ")"
}

func wrap(n Node) string {
	if _, ok := n.(*BinaryNode); ok {
		return "(" + n.String() + ")"
	}
	return n.String()
}

// RefVisitor is called once for every reference inside an AST.
type RefVisitor func(sheet string, col, row int, isRange bool, c2, r2 int)

// WalkRefs enumerates every cell/range reference inside an AST.
func WalkRefs(n Node, v RefVisitor) {
	switch x := n.(type) {
	case *RefNode:
		sheet := x.Sheet
		v(sheet, x.Col, x.Row, false, x.Col, x.Row)
	case *RangeNode:
		v(x.Sheet, x.Col1, x.Row1, true, x.Col2, x.Row2)
	case *UnaryNode:
		WalkRefs(x.Inner, v)
	case *BinaryNode:
		WalkRefs(x.Left, v)
		WalkRefs(x.Right, v)
	case *CallNode:
		for _, a := range x.Args {
			WalkRefs(a, v)
		}
	}
}

// MapRefs rewrites every reference node inside an AST via fn.
// Returning nil deletes the reference (replaced with the #REF! error literal).
func MapRefs(n Node, fn func(*RefNode) *RefNode) Node {
	switch x := n.(type) {
	case *RefNode:
		if nr := fn(x); nr != nil {
			return nr
		}
		return &ErrNode{Value: RefErr}
	case *RangeNode:
		// Ranges are rewritten as two endpoint refs for convenience of fn.
		a := &RefNode{Sheet: x.Sheet, Col: x.Col1, Row: x.Row1, AbsCol: x.AbsCol1, AbsRow: x.AbsRow1}
		b := &RefNode{Sheet: x.Sheet, Col: x.Col2, Row: x.Row2, AbsCol: x.AbsCol2, AbsRow: x.AbsRow2}
		na, nb := fn(a), fn(b)
		if na == nil || nb == nil {
			return &ErrNode{Value: RefErr}
		}
		return &RangeNode{
			Sheet: x.Sheet,
			Col1:  na.Col, Row1: na.Row, AbsCol1: na.AbsCol, AbsRow1: na.AbsRow,
			Col2: nb.Col, Row2: nb.Row, AbsCol2: nb.AbsCol, AbsRow2: nb.AbsRow,
		}
	case *UnaryNode:
		x.Inner = MapRefs(x.Inner, fn)
		return x
	case *BinaryNode:
		x.Left = MapRefs(x.Left, fn)
		x.Right = MapRefs(x.Right, fn)
		return x
	case *CallNode:
		for i, a := range x.Args {
			x.Args[i] = MapRefs(a, fn)
		}
		return x
	}
	return n
}
