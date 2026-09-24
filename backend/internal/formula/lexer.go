// Package formula implements the spreadsheet formula language:
// tokenization, recursive-descent parsing, AST rewriting and evaluation.
package formula

import (
	"fmt"
	"strings"
	"unicode"
)

// TokenKind enumerates the kinds of lexical tokens in a formula.
type TokenKind int

const (
	TEOF TokenKind = iota
	TNumber
	TString
	TIdent // function name / TRUE / FALSE / bare sheet name (parser decides)
	TErr   // #DIV/0!, #REF!, ...
	TCell  // A1, $A$1, possibly followed by ':'
	TBang  // ! produced only after a sheet identifier
	TLParen
	TRParen
	TComma
	TColon
	TPlus
	TMinus
	TStar
	TSlash
	TCaret
	TPercent
	TConcat // &
	TEQ
	TNE
	TLT
	TLE
	TGT
	TGE
)

// Token is a single lexical token.
type Token struct {
	Kind TokenKind
	Val  string
	Pos  int
}

// LexError describes an illegal token at byte offset Pos.
type LexError struct {
	Pos int
	Msg string
}

func (e *LexError) Error() string { return fmt.Sprintf("lex error at %d: %s", e.Pos, e.Msg) }

// Lex tokenizes the formula body (without the leading '=').
func Lex(input string) ([]Token, error) {
	l := &lexer{input: input}
	var toks []Token
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		if t.Kind == TEOF {
			return toks, nil
		}
	}
}

type lexer struct {
	input string
	pos   int
}

func (l *lexer) skipSpace() {
	for l.pos < len(l.input) && unicode.IsSpace(rune(l.input[l.pos])) {
		l.pos++
	}
}

func (l *lexer) next() (Token, error) {
	l.skipSpace()
	if l.pos >= len(l.input) {
		return Token{Kind: TEOF, Pos: l.pos}, nil
	}
	start := l.pos
	c := l.input[l.pos]
	switch {
	case c >= '0' && c <= '9' || c == '.' && l.pos+1 < len(l.input) && l.input[l.pos+1] >= '0' && l.input[l.pos+1] <= '9':
		return l.lexNumber()
	case c == '"':
		return l.lexString()
	case c == '#':
		return l.lexError()
	case isLetter(c) || c == '_' || c == '$' || c >= 0x80:
		return l.lexIdentOrCell(start)
	case c == '(':
		l.pos++
		return Token{Kind: TLParen, Val: "(", Pos: start}, nil
	case c == ')':
		l.pos++
		return Token{Kind: TRParen, Val: ")", Pos: start}, nil
	case c == ',':
		l.pos++
		return Token{Kind: TComma, Val: ",", Pos: start}, nil
	case c == ':':
		l.pos++
		return Token{Kind: TColon, Val: ":", Pos: start}, nil
	case c == '!':
		l.pos++
		return Token{Kind: TBang, Val: "!", Pos: start}, nil
	case c == '+':
		l.pos++
		return Token{Kind: TPlus, Val: "+", Pos: start}, nil
	case c == '-':
		l.pos++
		return Token{Kind: TMinus, Val: "-", Pos: start}, nil
	case c == '*':
		l.pos++
		return Token{Kind: TStar, Val: "*", Pos: start}, nil
	case c == '/':
		l.pos++
		return Token{Kind: TSlash, Val: "/", Pos: start}, nil
	case c == '^':
		l.pos++
		return Token{Kind: TCaret, Val: "^", Pos: start}, nil
	case c == '%':
		l.pos++
		return Token{Kind: TPercent, Val: "%", Pos: start}, nil
	case c == '&':
		l.pos++
		return Token{Kind: TConcat, Val: "&", Pos: start}, nil
	case c == '=':
		l.pos++
		return Token{Kind: TEQ, Val: "=", Pos: start}, nil
	case c == '<':
		l.pos++
		if l.pos < len(l.input) && (l.input[l.pos] == '=' || l.input[l.pos] == '>') {
			op := string([]byte{c, l.input[l.pos]})
			l.pos++
			if op == "<=" {
				return Token{Kind: TLE, Val: op, Pos: start}, nil
			}
			return Token{Kind: TNE, Val: op, Pos: start}, nil
		}
		return Token{Kind: TLT, Val: "<", Pos: start}, nil
	case c == '>':
		l.pos++
		if l.pos < len(l.input) && l.input[l.pos] == '=' {
			l.pos++
			return Token{Kind: TGE, Val: ">=", Pos: start}, nil
		}
		return Token{Kind: TGT, Val: ">", Pos: start}, nil
	}
	return Token{}, &LexError{Pos: start, Msg: "unexpected character '" + string(c) + "'"}
}

func (l *lexer) lexNumber() (Token, error) {
	start := l.pos
	dot := false
	for l.pos < len(l.input) {
		c := l.input[l.pos]
		if c >= '0' && c <= '9' {
			l.pos++
		} else if c == '.' && !dot {
			dot = true
			l.pos++
		} else {
			break
		}
	}
	// Scientific notation: 1.2e-3
	if l.pos < len(l.input) && (l.input[l.pos] == 'e' || l.input[l.pos] == 'E') {
		save := l.pos
		l.pos++
		if l.pos < len(l.input) && (l.input[l.pos] == '+' || l.input[l.pos] == '-') {
			l.pos++
		}
		if l.pos >= len(l.input) || l.input[l.pos] < '0' || l.input[l.pos] > '9' {
			l.pos = save
		} else {
			for l.pos < len(l.input) && l.input[l.pos] >= '0' && l.input[l.pos] <= '9' {
				l.pos++
			}
		}
	}
	return Token{Kind: TNumber, Val: l.input[start:l.pos], Pos: start}, nil
}

func (l *lexer) lexString() (Token, error) {
	start := l.pos
	l.pos++ // opening quote
	var b strings.Builder
	for l.pos < len(l.input) {
		c := l.input[l.pos]
		if c == '"' {
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '"' {
				b.WriteByte('"')
				l.pos += 2
				continue
			}
			l.pos++
			return Token{Kind: TString, Val: b.String(), Pos: start}, nil
		}
		b.WriteByte(c)
		l.pos++
	}
	return Token{}, &LexError{Pos: start, Msg: "unterminated string"}
}

func (l *lexer) lexError() (Token, error) {
	start := l.pos
	for l.pos < len(l.input) && l.input[l.pos] != '?' && l.input[l.pos] != '!' && l.input[l.pos] != ' ' {
		l.pos++
	}
	if l.pos < len(l.input) && l.input[l.pos] == '?' {
		l.pos++
	}
	// #REF! style error ending in '!'
	if l.pos < len(l.input) && l.input[l.pos] == '!' && l.input[start:l.pos] != "" {
		l.pos++
	}
	val := l.input[start:l.pos]
	if val == "#" {
		return Token{}, &LexError{Pos: start, Msg: "invalid error literal"}
	}
	return Token{Kind: TErr, Val: val, Pos: start}, nil
}

func (l *lexer) lexIdentOrCell(start int) (Token, error) {
	for l.pos < len(l.input) && isIdentRune(l.input[l.pos]) {
		l.pos++
	}
	word := l.input[start:l.pos]
	// Optional '$' prefixes and digits make it a cell reference: $A$1 / A1 / $A1 / A$1.
	if col, absCol, ok := matchColumnPrefix(word); ok {
		i := col
		absRow := false
		if i < len(word) && word[i] == '$' {
			absRow = true
			i++
		}
		if i < len(word) && word[i] >= '1' && word[i] <= '9' {
			j := i
			for j < len(word) && word[j] >= '0' && word[j] <= '9' {
				j++
			}
			if j == len(word) && j > i {
				return Token{Kind: TCell, Val: word, Pos: start}, nil
			}
		}
		_ = absCol
		_ = absRow
	}
	return Token{Kind: TIdent, Val: word, Pos: start}, nil
}

func isLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

// isIdentRune accepts ASCII identifier characters plus any UTF-8 continuation
// byte so non-ASCII identifiers (e.g. a Chinese sheet name 设置!A1) scan whole.
func isIdentRune(c byte) bool {
	return isAlnum(c) || c == '_' || c == '$' || c >= 0x80
}

func isAlnum(c byte) bool {
	return isLetter(c) || c >= '0' && c <= '9'
}

// matchColumnPrefix returns the index just after the column letters and
// whether the column was absolute.
func matchColumnPrefix(word string) (int, bool, bool) {
	i := 0
	abs := false
	if i < len(word) && word[i] == '$' {
		abs = true
		i++
	}
	start := i
	for i < len(word) && word[i] >= 'A' && word[i] <= 'Z' ||
		i < len(word) && word[i] >= 'a' && word[i] <= 'z' {
		i++
	}
	if i == start || i > 3 {
		return 0, false, false
	}
	return i, abs, true
}
