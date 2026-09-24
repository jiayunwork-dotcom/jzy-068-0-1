package formula

import (
	"fmt"
	"strconv"
	"strings"
)

// Parse converts formula text (with or without leading '=') into an AST.
func Parse(text string) (Node, error) {
	body := text
	if strings.HasPrefix(body, "=") {
		body = body[1:]
	}
	toks, err := Lex(body)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	if p.peek().Kind == TEOF {
		return nil, &ParseError{Msg: "empty formula"}
	}
	node, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.peek().Kind != TEOF {
		return nil, &ParseError{Pos: p.peek().Pos, Msg: "unexpected token '" + p.peek().Val + "'"}
	}
	return node, nil
}

// ParseError describes a syntax error.
type ParseError struct {
	Pos int
	Msg string
}

func (e *ParseError) Error() string {
	if e.Pos > 0 {
		return fmt.Sprintf("parse error at %d: %s", e.Pos, e.Msg)
	}
	return "parse error: " + e.Msg
}

type parser struct {
	toks []Token
	pos  int
}

func (p *parser) peek() Token  { return p.toks[p.pos] }
func (p *parser) next2() Token { t := p.toks[p.pos]; p.pos++; return t }
func (p *parser) accept(k TokenKind) bool {
	if p.peek().Kind == k {
		p.pos++
		return true
	}
	return false
}

func (p *parser) expect(k TokenKind, what string) (Token, error) {
	if p.peek().Kind != k {
		return Token{}, &ParseError{Pos: p.peek().Pos, Msg: "expected " + what + ", got '" + p.peek().Val + "'"}
	}
	return p.next2(), nil
}

// precedence: comparison < concat < add < mul < power < unary < percent < atom
func prec(k TokenKind) int {
	switch k {
	case TEQ, TNE, TLT, TLE, TGT, TGE:
		return 1
	case TConcat:
		return 2
	case TPlus, TMinus:
		return 3
	case TStar, TSlash:
		return 4
	case TCaret:
		return 5
	}
	return 0
}

func (p *parser) parseExpr() (Node, error) { return p.parseBinary(1) }

func (p *parser) parseBinary(minPrec int) (Node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		k := p.peek().Kind
		pr := prec(k)
		if pr == 0 || pr < minPrec {
			return left, nil
		}
		op := p.next2()
		// '^' is right-associative; everything else is left-associative.
		nextPrec := pr + 1
		if k == TCaret {
			nextPrec = pr
		}
		right, err := p.parseBinary(nextPrec)
		if err != nil {
			return nil, err
		}
		left = &BinaryNode{Op: op.Val, Left: left, Right: right}
	}
}

func (p *parser) parseUnary() (Node, error) {
	if p.peek().Kind == TMinus || p.peek().Kind == TPlus {
		op := p.next2()
		inner, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryNode{Op: op.Val, Inner: inner}, nil
	}
	return p.parsePercent()
}

func (p *parser) parsePercent() (Node, error) {
	node, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	for p.accept(TPercent) {
		node = &UnaryNode{Op: "%", Inner: node, Post: true}
	}
	return node, nil
}

func (p *parser) parseAtom() (Node, error) {
	t := p.peek()
	switch t.Kind {
	case TNumber:
		p.next2()
		v, err := strconv.ParseFloat(t.Val, 64)
		if err != nil {
			return nil, &ParseError{Pos: t.Pos, Msg: "bad number"}
		}
		return &NumberNode{Value: v}, nil
	case TString:
		p.next2()
		return &StringNode{Value: t.Val}, nil
	case TErr:
		p.next2()
		return &ErrNode{Value: Value{Typ: VError, Str: t.Val}}, nil
	case TLParen:
		p.next2()
		node, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TRParen, "')'"); err != nil {
			return nil, err
		}
		return node, nil
	case TMinus, TPlus:
		return p.parseUnary()
	case TIdent:
		return p.parseIdentOrCall()
	case TCell:
		// A cell-like token can still be a sheet name prefix: S1!A2.
		if p.pos+1 < len(p.toks) && p.toks[p.pos+1].Kind == TBang {
			return p.parseIdentOrCall()
		}
		return p.parseReference("")
	}
	return nil, &ParseError{Pos: t.Pos, Msg: "unexpected token '" + t.Val + "'"}
}

func (p *parser) parseIdentOrCall() (Node, error) {
	name := p.next2()
	// Cross-sheet reference: Sheet2!A1 or 'Sheet X'!A1 (we only support bare
	// identifiers as sheet names).
	if p.peek().Kind == TBang {
		p.next2()
		return p.parseReference(name.Val)
	}
	upper := strings.ToUpper(name.Val)
	if upper == "TRUE" || upper == "FALSE" {
		return &BoolNode{Value: upper == "TRUE"}, nil
	}
	if p.peek().Kind != TLParen {
		return nil, &ParseError{Pos: name.Pos, Msg: "unknown name '" + name.Val + "'"}
	}
	p.next2() // '('
	var args []Node
	if p.peek().Kind != TRParen {
		for {
			arg, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			if !p.accept(TComma) {
				break
			}
		}
	}
	if _, err := p.expect(TRParen, "')'"); err != nil {
		return nil, err
	}
	return &CallNode{Name: upper, Args: args}, nil
}

// parseReference parses A1 / $A$1 / A1:B2 after an optional sheet prefix.
func (p *parser) parseReference(sheet string) (Node, error) {
	t, err := p.expect(TCell, "cell reference")
	if err != nil {
		return nil, err
	}
	ref, err := parseRefToken(t.Val)
	if err != nil {
		return nil, &ParseError{Pos: t.Pos, Msg: err.Error()}
	}
	ref.Sheet = sheet
	if p.peek().Kind != TColon {
		return ref, nil
	}
	p.next2()
	t2, err := p.expect(TCell, "end of range")
	if err != nil {
		return nil, err
	}
	end, err := parseRefToken(t2.Val)
	if err != nil {
		return nil, &ParseError{Pos: t2.Pos, Msg: err.Error()}
	}
	if end.Sheet != "" && end.Sheet != sheet {
		return nil, &ParseError{Pos: t2.Pos, Msg: "range endpoints must be on the same sheet"}
	}
	r := &RangeNode{
		Sheet: sheet,
		Col1:  ref.Col, Row1: ref.Row, AbsCol1: ref.AbsCol, AbsRow1: ref.AbsRow,
		Col2: end.Col, Row2: end.Row, AbsCol2: end.AbsCol, AbsRow2: end.AbsRow,
	}
	if r.Col2 < r.Col1 || r.Row2 < r.Row1 {
		return nil, &ParseError{Pos: t.Pos, Msg: "invalid range (end precedes start)"}
	}
	return r, nil
}

func parseRefToken(s string) (*RefNode, error) {
	i := 0
	absCol := false
	if i < len(s) && s[i] == '$' {
		absCol = true
		i++
	}
	colStart := i
	for i < len(s) && s[i] >= 'A' && s[i] <= 'Z' || i < len(s) && s[i] >= 'a' && s[i] <= 'z' {
		i++
	}
	if i == colStart {
		return nil, fmt.Errorf("bad cell reference %q", s)
	}
	col := colIndex(s[colStart:i])
	absRow := false
	if i < len(s) && s[i] == '$' {
		absRow = true
		i++
	}
	rowStart := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i != len(s) || i == rowStart {
		return nil, fmt.Errorf("bad cell reference %q", s)
	}
	row, err := strconv.Atoi(s[rowStart:i])
	if err != nil || row < 1 {
		return nil, fmt.Errorf("bad row in %q", s)
	}
	return &RefNode{Col: col, Row: row - 1, AbsCol: absCol, AbsRow: absRow}, nil
}
