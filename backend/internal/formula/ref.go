package formula

import (
	"fmt"
	"strings"
)

// Ref identifies one cell or one cell range, possibly in another sheet.
// Column and row indices are 0-based. Abs* flags mark the $ anchors of the
// *written* reference; evaluation itself always resolves to absolute
// coordinates.
type Ref struct {
	Sheet string // empty means "same sheet as the formula"
	Col1  int
	Row1  int
	Col2  int
	Row2  int

	AbsCol1 bool
	AbsRow1 bool
	AbsCol2 bool
	AbsRow2 bool

	IsRange bool
	IsError bool // a literal #REF! produced when a reference is deleted
}

func (r Ref) Single() bool { return !r.IsRange && !r.IsError }

// String renders the reference in canonical spreadsheet notation.
func (r Ref) String() string {
	if r.IsError {
		return "#REF!"
	}
	var b strings.Builder
	if r.Sheet != "" {
		b.WriteString(quoteSheet(r.Sheet))
		b.WriteByte('!')
	}
	b.WriteString(coord(r.Col1, r.AbsCol1))
	b.WriteString(rowCoord(r.Row1, r.AbsRow1))
	if r.IsRange {
		b.WriteByte(':')
		b.WriteString(coord(r.Col2, r.AbsCol2))
		b.WriteString(rowCoord(r.Row2, r.AbsRow2))
	}
	return b.String()
}

func quoteSheet(name string) string {
	if needsQuoteSheet(name) {
		return "'" + strings.ReplaceAll(name, "'", "''") + "'"
	}
	return name
}

func needsQuoteSheet(name string) bool {
	if name == "" {
		return true
	}
	for i, r := range name {
		if i == 0 && (r >= '0' && r <= '9') {
			return true
		}
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return true
		}
	}
	return false
}

func coord(col int, abs bool) string {
	s := ""
	c := col
	for {
		s = string(rune('A'+c%26)) + s
		c = c/26 - 1
		if c < 0 {
			break
		}
	}
	if abs {
		return "$" + s
	}
	return s
}

func rowCoord(row int, abs bool) string {
	if abs {
		return fmt.Sprintf("$%d", row+1)
	}
	return fmt.Sprintf("%d", row+1)
}

// ColLetters converts a 0-based column index to its letter label (0 -> A,
// 25 -> Z, 26 -> AA).
func ColLetters(col int) string {
	if col < 0 {
		return "#REF"
	}
	s := ""
	c := col
	for {
		s = string(rune('A'+c%26)) + s
		c = c/26 - 1
		if c < 0 {
			break
		}
	}
	return s
}

// ParseCol converts a letter label back into a 0-based column index.
func ParseCol(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	c := 0
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			r = r - 'a' + 'A'
			fallthrough
		case r >= 'A' && r <= 'Z':
			c = c*26 + int(r-'A') + 1
		default:
			return 0, false
		}
	}
	return c - 1, true
}

// CellKey is the canonical identity of a cell in a workbook: "Sheet/col,row".
type CellKey struct {
	Sheet string
	Col   int
	Row   int
}

func (k CellKey) String() string { return fmt.Sprintf("%s/%d,%d", k.Sheet, k.Col, k.Row) }
