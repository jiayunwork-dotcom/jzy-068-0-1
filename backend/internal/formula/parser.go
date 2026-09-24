package formula

import (
	"fmt"
	"strconv"
	"strings"
)

// Parse parses formula text (with or without a leading '=') into an AST.
func Parse(text string) (Node, error) {
	s := strings.TrimSpace(text)
	s = strings.TrimPrefix(s, "=")
	toks, err := lex(s)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	node := p.parseExpr()
	if p.err != nil {
		return nil, p.err
	}
	if p.peek().kind != tEOF {
		return nil, fmt.Errorf("unexpected token %q", p.peek().text)
	}
	return node, nil
}

type parser struct {
	toks []token
	i    int
	err  error
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token { t := p.toks[p.i]; p.i++; return t }
func (p *parser) failf(f string, a ...any) {
	if p.err == nil {
		p.err = fmt.Errorf(f, a...)
	}
}

// Expression grammar (lowest to highest precedence):
//
//	expr      := compare
//	compare   := concat ( (=|<>|<|<=|>|>=) concat )*
//	concat    := addsub ( & addsub )*
//	addsub    := muldiv ( (+|-) muldiv )*
//	muldiv    := power ( (*|/) power )*
//	power     := percent ^ power | percent        (right associative)
//	percent   := unary %?
//	unary     := (+|-) unary | primary
//	primary   := number | string | TRUE|FALSE | error | ref | call | (expr)
func (p *parser) parseExpr() Node { return p.parseCompare() }

func (p *parser) parseCompare() Node {
	lhs := p.parseConcat()
	for {
		var op string
		switch p.peek().kind {
		case tEq:
			op = "="
		case tNeq:
			op = "<>"
		case tLt:
			op = "<"
		case tLe:
			op = "<="
		case tGt:
			op = ">"
		case tGe:
			op = ">="
		default:
			return lhs
		}
		p.next()
		rhs := p.parseConcat()
		lhs = &BinaryNode{Op: op, Lhs: lhs, Rhs: rhs}
	}
}

func (p *parser) parseConcat() Node {
	lhs := p.parseAddSub()
	for p.peek().kind == tAmp {
		p.next()
		rhs := p.parseAddSub()
		lhs = &BinaryNode{Op: "&", Lhs: lhs, Rhs: rhs}
	}
	return lhs
}

func (p *parser) parseAddSub() Node {
	lhs := p.parseMulDiv()
	for p.peek().kind == tPlus || p.peek().kind == tMinus {
		op := p.next().text
		rhs := p.parseMulDiv()
		lhs = &BinaryNode{Op: op, Lhs: lhs, Rhs: rhs}
	}
	return lhs
}

func (p *parser) parseMulDiv() Node {
	lhs := p.parsePower()
	for p.peek().kind == tStar || p.peek().kind == tSlash {
		op := p.next().text
		rhs := p.parsePower()
		lhs = &BinaryNode{Op: op, Lhs: lhs, Rhs: rhs}
	}
	return lhs
}

func (p *parser) parsePower() Node {
	base := p.parsePercent()
	if p.peek().kind == tCaret {
		p.next()
		exp := p.parsePower() // right associative
		return &BinaryNode{Op: "^", Lhs: base, Rhs: exp}
	}
	return base
}

func (p *parser) parsePercent() Node {
	inner := p.parseUnary()
	if p.peek().kind == tPercent {
		p.next()
		return &UnaryNode{Op: "%", Expr: inner, Postfix: true}
	}
	return inner
}

func (p *parser) parseUnary() Node {
	t := p.peek()
	if t.kind == tMinus || t.kind == tPlus {
		p.next()
		return &UnaryNode{Op: t.text, Expr: p.parseUnary()}
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() Node {
	t := p.peek()
	switch t.kind {
	case tNumber:
		p.next()
		f, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			p.failf("bad number %q", t.text)
			return &NumberNode{}
		}
		return &NumberNode{Value: f}
	case tString:
		p.next()
		return &StringNode{Value: t.text}
	case tErrorLit:
		p.next()
		return &ErrorNode{Value: Error(t.text)}
	case tLParen:
		p.next()
		inner := p.parseExpr()
		if p.peek().kind != tRParen {
			p.failf("missing closing parenthesis")
			return inner
		}
		p.next()
		return inner
	case tName:
		return p.parseNameOrCall()
	case tRef:
		p.next()
		return &RefNode{Ref: t.ref}
	case tEOF:
		p.failf("unexpected end of formula")
		return &ErrorNode{Value: ErrParse}
	default:
		p.failf("unexpected token %q", t.text)
		p.next()
		return &ErrorNode{Value: ErrParse}
	}
}

func (p *parser) parseNameOrCall() Node {
	name := p.next().text
	upper := strings.ToUpper(name)

	// Sheet-qualified reference: NAME ! ref
	if p.peek().kind == tBang {
		p.next()
		if p.peek().kind != tRef {
			p.failf("expected cell reference after %s!", name)
			return &RefErrNode{}
		}
		r := p.next().ref
		r.Sheet = name
		if p.peek().kind == tColon {
			p.next()
			if p.peek().kind != tRef {
				p.failf("expected second range endpoint after ':'")
				return &RefErrNode{}
			}
			r2 := p.next().ref
			r.IsRange = true
			r.Col2, r.Row2 = r2.Col1, r2.Row1
			r.AbsCol2, r.AbsRow2 = r2.AbsCol1, r2.AbsRow1
			// Note: endpoints after ! are never sheet-qualified.
		}
		return &RefNode{Ref: r}
	}

	// Function call?
	if p.peek().kind == tLParen {
		p.next()
		var args []Node
		if p.peek().kind != tRParen {
			for {
				if p.peek().kind == tComma {
					args = append(args, &BlankArgNode{}) // tolerate empty args
					p.next()
					continue
				}
				args = append(args, p.parseExpr())
				if p.peek().kind == tComma {
					p.next()
					continue
				}
				break
			}
		}
		if p.peek().kind != tRParen {
			p.failf("missing ')' after arguments to %s", name)
		} else {
			p.next()
		}
		return &CallNode{Name: upper, Args: args}
	}

	switch upper {
	case "TRUE":
		return &BoolNode{Value: true}
	case "FALSE":
		return &BoolNode{Value: false}
	}
	p.failf("%s: unrecognized name", name)
	return &ErrorNode{Value: ErrName}
}

// BlankArgNode marks a missing argument (IF(A1>0,)). It evaluates to blank.
type BlankArgNode struct{}

func (*BlankArgNode) nodeMarker() {}
