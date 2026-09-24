package formula

// Node is one node of a parsed formula AST.
type Node interface {
	nodeMarker()
}

// NumberNode is a numeric literal.
type NumberNode struct{ Value float64 }

// StringNode is a double-quoted string literal (unquoted value).
type StringNode struct{ Value string }

// BoolNode is TRUE / FALSE.
type BoolNode struct{ Value bool }

// ErrorNode is a literal error token like #DIV/0!.
type ErrorNode struct{ Value Error }

// RefNode is a cell or range reference, optionally qualified by sheet.
type RefNode struct{ Ref Ref }

// UnaryNode is +x, -x or x% (percent).
type UnaryNode struct {
	Op      string
	Expr    Node
	Postfix bool // true for %
}

// BinaryNode covers + - * / ^ & and comparisons.
type BinaryNode struct {
	Op       string
	Lhs, Rhs Node
}

// CallNode is a function call. Args are evaluated lazily only when needed
// (IF evaluates just one branch), so they stay AST nodes.
type CallNode struct {
	Name string
	Args []Node
}

// RefErrNode appears when a reference has been deleted structurally.
type RefErrNode struct{}

func (*NumberNode) nodeMarker() {}
func (*StringNode) nodeMarker() {}
func (*BoolNode) nodeMarker()   {}
func (*ErrorNode) nodeMarker()  {}
func (*RefNode) nodeMarker()    {}
func (*UnaryNode) nodeMarker()  {}
func (*BinaryNode) nodeMarker() {}
func (*CallNode) nodeMarker()   {}
func (*RefErrNode) nodeMarker() {}

// Print serializes an AST back to canonical formula text.
func Print(n Node) string { return printNode(n, true) }

func printNode(n Node, top bool) string {
	switch x := n.(type) {
	case *NumberNode:
		return FormatNumber(x.Value)
	case *StringNode:
		return "\"" + strings_escape(x.Value) + "\""
	case *BoolNode:
		if x.Value {
			return "TRUE"
		}
		return "FALSE"
	case *ErrorNode:
		return string(x.Value)
	case *RefNode:
		return x.Ref.String()
	case *RefErrNode:
		return "#REF!"
	case *UnaryNode:
		if x.Postfix {
			return "(" + printNode(x.Expr, false) + "%)"
		}
		return x.Op + "(" + printNode(x.Expr, false) + ")"
	case *BinaryNode:
		s := printNode(x.Lhs, false) + x.Op + printNode(x.Rhs, false)
		if top {
			return s
		}
		return "(" + s + ")"
	case *CallNode:
		s := x.Name + "("
		for i, a := range x.Args {
			if i > 0 {
				s += ","
			}
			s += printNode(a, false)
		}
		return s + ")"
	}
	return ""
}

func strings_escape(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '"' {
			out = append(out, '"', '"')
		} else {
			out = append(out, r)
		}
	}
	return string(out)
}

// WalkRefs invokes fn for every reference node in the AST (for dependency
// extraction and for reference shifting on structural edits).
func WalkRefs(n Node, fn func(*RefNode)) {
	switch x := n.(type) {
	case *RefNode:
		fn(x)
	case *UnaryNode:
		WalkRefs(x.Expr, fn)
	case *BinaryNode:
		WalkRefs(x.Lhs, fn)
		WalkRefs(x.Rhs, fn)
	case *CallNode:
		for _, a := range x.Args {
			WalkRefs(a, fn)
		}
	}
}
