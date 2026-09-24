package formula

import (
	"fmt"
	"strings"
	"unicode"
)

type tokenKind int

const (
	tEOF tokenKind = iota
	tNumber
	tString
	tName     // function name or TRUE/FALSE
	tRef      // cell reference / range, value stored in tok.Ref
	tSheetRef // `Name!` or `'quoted'!` prefix handled at parser level; unused
	tErrorLit // #VALUE! etc.
	tPlus
	tMinus
	tStar
	tSlash
	tCaret
	tPercent
	tAmp
	tEq
	tNeq
	tLt
	tLe
	tGt
	tGe
	tLParen
	tRParen
	tComma
	tColon
	tBang
)

type token struct {
	kind tokenKind
	text string
	ref  Ref
	pos  int
}

type lexer struct {
	src  string
	pos  int
	toks []token
}

func lex(src string) ([]token, error) {
	l := &lexer{src: src}
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			l.pos++
		case c == '+':
			l.emit(tPlus, "+")
		case c == '-':
			l.emit(tMinus, "-")
		case c == '*':
			l.emit(tStar, "*")
		case c == '/':
			l.emit(tSlash, "/")
		case c == '^':
			l.emit(tCaret, "^")
		case c == '%':
			l.emit(tPercent, "%")
		case c == '&':
			l.emit(tAmp, "&")
		case c == '(':
			l.emit(tLParen, "(")
		case c == ')':
			l.emit(tRParen, ")")
		case c == ',':
			l.emit(tComma, ",")
		case c == ':':
			l.emit(tColon, ":")
		case c == '!':
			l.emit(tBang, "!")
		case c == '=':
			l.emit(tEq, "=")
		case c == '<':
			if l.peek(1) == '=' {
				l.pos += 2
				l.push(tLe, "<=")
			} else if l.peek(1) == '>' {
				l.pos += 2
				l.push(tNeq, "<>")
			} else {
				l.emit(tLt, "<")
			}
		case c == '>':
			if l.peek(1) == '=' {
				l.pos += 2
				l.push(tGe, ">=")
			} else {
				l.emit(tGt, ">")
			}
		case c == '"':
			if err := l.lexString(); err != nil {
				return nil, err
			}
		case c == '#':
			if err := l.lexError(); err != nil {
				return nil, err
			}
		case c >= '0' && c <= '9' || (c == '.' && l.peek(1) >= '0' && l.peek(1) <= '9'):
			l.lexNumber()
		case isNameStart(c):
			l.lexNameOrRef()
		case c == '\'':
			// Quoted sheet name; the lexer reads through the closing quote
			// and the following ! and remembers it as a special name token.
			if err := l.lexQuotedSheet(); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unexpected character %q at position %d", string(c), l.pos)
		}
	}
	l.toks = append(l.toks, token{kind: tEOF, pos: l.pos})
	return l.toks, nil
}

func (l *lexer) peek(n int) byte {
	if l.pos+n < len(l.src) {
		return l.src[l.pos+n]
	}
	return 0
}

func (l *lexer) emit(k tokenKind, text string) {
	l.push(k, text)
	l.pos += len(text)
}

func (l *lexer) push(k tokenKind, text string) {
	l.toks = append(l.toks, token{kind: k, text: text, pos: l.pos})
}

func (l *lexer) lexNumber() {
	start := l.pos
	dot := false
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c >= '0' && c <= '9' {
			l.pos++
		} else if c == '.' && !dot {
			dot = true
			l.pos++
		} else {
			break
		}
	}
	// Scientific notation 1e3 / 2.5E-4
	if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
		save := l.pos
		l.pos++
		if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
			l.pos++
		}
		if l.pos >= len(l.src) || l.src[l.pos] < '0' || l.src[l.pos] > '9' {
			l.pos = save
		} else {
			for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
				l.pos++
			}
		}
	}
	l.push(tNumber, l.src[start:l.pos])
}

func (l *lexer) lexString() error {
	start := l.pos
	l.pos++ // opening quote
	var b strings.Builder
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '"' {
			if l.peek(1) == '"' {
				b.WriteByte('"')
				l.pos += 2
				continue
			}
			l.pos++ // closing quote
			l.toks = append(l.toks, token{kind: tString, text: b.String(), pos: start})
			return nil
		}
		b.WriteByte(c)
		l.pos++
	}
	return fmt.Errorf("unterminated string starting at %d", start)
}

func (l *lexer) lexError() error {
	start := l.pos
	l.pos++
	begin := l.pos
	for l.pos < len(l.src) && isNameChar(l.src[l.pos]) {
		l.pos++
	}
	if l.pos < len(l.src) && l.src[l.pos] == '?' {
		l.pos++
	}
	word := "#" + l.src[begin:l.pos]
	// handle #DIV/0! which contains '/' and digits
	if word == "#DIV" && l.pos+2 < len(l.src) && l.src[l.pos] == '/' {
		end := strings.IndexByte(l.src[l.pos:], '!')
		if end >= 0 {
			l.pos += end + 1
			word = l.src[start:l.pos]
		}
	}
	switch Error(word) {
	case ErrValue, ErrDivZero, ErrName, ErrNA, ErrRef, ErrCircular, ErrNum:
		l.toks = append(l.toks, token{kind: tErrorLit, text: word, pos: start})
		return nil
	}
	return fmt.Errorf("unrecognized error literal %q", word)
}

func isNameStart(c byte) bool {
	return c == '_' || c == '$' || unicode.IsLetter(rune(c))
}

func isNameChar(c byte) bool {
	return c == '_' || c == '$' || c == '.' || unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c))
}

// isIdentRune accepts a leading byte of a UTF-8 letter/digit rune (sheet
// names may be e.g. 中文), in addition to the ASCII identifier characters.
func isIdentRune(c byte) bool {
	if c == '_' || c == '.' || c == '$' {
		return true
	}
	if c < 0x80 {
		return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
	}
	return true // continuation/lead byte of a multibyte rune
}

// lexNameOrRef reads a run like A1, $A$1, AA12, SUM or _foo, optionally
// followed by `!` (a sheet qualifier). A following `:B2` makes it a range.
func (l *lexer) lexNameOrRef() {
	start := l.pos

	// A sheet name can itself look like a reference (S2!, Sheet1!). When a
	// reference-shaped token is immediately followed by '!', treat the whole
	// identifier run as a sheet name instead.
	if ref, n, ok := scanRefAt(l.src, l.pos); ok && (l.pos+n >= len(l.src) || l.src[l.pos+n] != '!') {
		l.pos += n
		if l.pos < len(l.src) && l.src[l.pos] == ':' {
			colon := l.pos
			l.pos++
			if ref2, n2, ok2 := scanRefAt(l.src, l.pos); ok2 {
				l.pos += n2
				ref.IsRange = true
				ref.Col2, ref.Row2 = ref2.Col1, ref2.Row1
				ref.AbsCol2, ref.AbsRow2 = ref2.AbsCol1, ref2.AbsRow1
				l.toks = append(l.toks, token{kind: tRef, ref: ref, pos: start})
				return
			}
			l.pos = colon
		}
		l.toks = append(l.toks, token{kind: tRef, ref: ref, pos: start})
		return
	}

	for l.pos < len(l.src) && isIdentRune(l.src[l.pos]) {
		l.pos++
	}
	word := l.src[start:l.pos]

	// Sheet prefix: NAME !
	if l.pos < len(l.src) && l.src[l.pos] == '!' {
		l.pos++
		l.toks = append(l.toks, token{kind: tName, text: word, pos: start})
		l.toks = append(l.toks, token{kind: tBang, text: "!", pos: l.pos})
		return
	}

	l.toks = append(l.toks, token{kind: tName, text: word, pos: start})
}

// scanRefAt tries to parse a possibly-$-anchored cell reference exactly at
// src[i:] (e.g. "$AB$12"). Returns the reference and the bytes consumed.
func scanRefAt(src string, i int) (Ref, int, bool) {
	p := i
	absCol := false
	if p < len(src) && src[p] == '$' {
		absCol = true
		p++
	}
	cs := p
	for p < len(src) {
		c := src[p]
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
			break
		}
		p++
	}
	letters := src[cs:p]
	if letters == "" {
		return Ref{}, 0, false
	}
	absRow := false
	if p < len(src) && src[p] == '$' {
		absRow = true
		p++
	}
	rs := p
	for p < len(src) && src[p] >= '0' && src[p] <= '9' {
		p++
	}
	digits := src[rs:p]
	if digits == "" {
		return Ref{}, 0, false
	}
	if p < len(src) && isNameStart(src[p]) {
		return Ref{}, 0, false
	}
	col, ok := ParseCol(toUpper(letters))
	if !ok {
		return Ref{}, 0, false
	}
	return Ref{Col1: col, Row1: atoi(digits) - 1, Col2: col, Row2: atoi(digits) - 1,
		AbsCol1: absCol, AbsRow1: absRow}, p - i, true
}

func toUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func (l *lexer) lexQuotedSheet() error {
	start := l.pos
	l.pos++ // opening '
	var b strings.Builder
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\'' {
			if l.peek(1) == '\'' {
				b.WriteByte('\'')
				l.pos += 2
				continue
			}
			l.pos++
			// Must be followed by !
			if l.pos < len(l.src) && l.src[l.pos] == '!' {
				name := b.String()
				l.toks = append(l.toks, token{kind: tName, text: name, pos: start})
				l.pos++
				l.toks = append(l.toks, token{kind: tBang, text: "!", pos: l.pos})
				return nil
			}
			return fmt.Errorf("expected '!' after quoted sheet name at %d", start)
		}
		b.WriteByte(c)
		l.pos++
	}
	return fmt.Errorf("unterminated quoted sheet name at %d", start)
}
