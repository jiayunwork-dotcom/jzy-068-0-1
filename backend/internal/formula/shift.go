package formula

// This file rewrites formula references when rows or columns are inserted
// or deleted ("sheet surgery").
//
// Rules (identical in spirit to desktop spreadsheets):
//
//   - Relative anchors move with the cells they point at: everything at or
//     beyond the insertion index shifts by one; a deleted anchor turns the
//     whole reference into #REF!.
//   - Absolute anchors ($-prefixed) stay put on insertion.
//   - Range endpoints each follow the rule that matches their own anchor.
//   - References into other sheets are rewritten the same way whenever the
//     surgery happens on the sheet they point to.

// ShiftKind describes a structural edit.
type ShiftKind int

const (
	ShiftInsertRow ShiftKind = iota
	ShiftDeleteRow
	ShiftInsertCol
	ShiftDeleteCol
)

// ShiftRef rewrites one reference under a structural edit on sheet target.
// The second return value is false when the reference is destroyed by a
// deletion (caller renders #REF!).
func ShiftRef(r Ref, kind ShiftKind, target string, index int) (Ref, bool) {
	if r.IsError {
		return r, true
	}
	if r.Sheet != target {
		return r, true
	}
	row := kind == ShiftInsertRow || kind == ShiftDeleteRow
	delta := 1
	if kind == ShiftDeleteRow || kind == ShiftDeleteCol {
		delta = -1
	}

	var abs1, abs2 bool
	if row {
		abs1, abs2 = r.AbsRow1, r.AbsRow2
	} else {
		abs1, abs2 = r.AbsCol1, r.AbsCol2
	}

	if row {
		var alive bool
		if r.Row1, alive = shiftOne(r.Row1, abs1, kind, index, delta); !alive {
			return Ref{IsError: true}, false
		}
		if r.Row2, alive = shiftOne(r.Row2, abs2, kind, index, delta); !alive {
			return Ref{IsError: true}, false
		}
	} else {
		var alive bool
		if r.Col1, alive = shiftOne(r.Col1, abs1, kind, index, delta); !alive {
			return Ref{IsError: true}, false
		}
		if r.Col2, alive = shiftOne(r.Col2, abs2, kind, index, delta); !alive {
			return Ref{IsError: true}, false
		}
	}
	return normalizeRange(r), true
}

// shiftOne maps a single anchor coordinate. absolute anchors never move on
// insert/delete (they name a fixed position).
func shiftOne(coord int, absolute bool, kind ShiftKind, index, delta int) (int, bool) {
	switch {
	case absolute:
		// Absolute references name fixed row/column numbers. After a
		// deletion the target coordinate simply still names that position;
		// insertion keeps it too. If it points past the new sheet edge the
		// engine treats it as blank, not an error.
		return coord, true
	case kind == ShiftInsertRow || kind == ShiftInsertCol:
		if coord >= index {
			return coord + 1, true
		}
		return coord, true
	default: // delete
		if coord == index {
			return 0, false
		}
		if coord > index {
			return coord - 1, true
		}
		return coord, true
	}
}

func normalizeRange(r Ref) Ref {
	if !r.IsRange {
		return r
	}
	if r.Row1 > r.Row2 {
		r.Row1, r.Row2 = r.Row2, r.Row1
		r.AbsRow1, r.AbsRow2 = r.AbsRow2, r.AbsRow1
	}
	if r.Col1 > r.Col2 {
		r.Col1, r.Col2 = r.Col2, r.Col1
		r.AbsCol1, r.AbsCol2 = r.AbsCol2, r.AbsCol1
	}
	return r
}

// ShiftAST returns a new AST with every reference affected by the surgery
// rewritten. ownerSheet is the sheet the formula itself lives on: an
// unqualified reference belongs there and moves exactly when target is that
// sheet; qualified references move when their own sheet is the target.
// Destroyed references become RefErrNode and serialize as #REF!.
func ShiftAST(n Node, kind ShiftKind, ownerSheet, target string, index int) Node {
	switch x := n.(type) {
	case *RefNode:
		resolved := x.Ref.Sheet
		if resolved == "" {
			resolved = ownerSheet
		}
		if resolved != target {
			return x
		}
		nr, alive := ShiftRef(withSheet(x.Ref, resolved), kind, target, index)
		if !alive {
			return &RefErrNode{}
		}
		if x.Ref.Sheet == "" {
			nr.Sheet = "" // keep unqualified refs unqualified after moving
		}
		return &RefNode{Ref: nr}
	case *UnaryNode:
		return &UnaryNode{Op: x.Op, Expr: ShiftAST(x.Expr, kind, ownerSheet, target, index), Postfix: x.Postfix}
	case *BinaryNode:
		return &BinaryNode{Op: x.Op,
			Lhs: ShiftAST(x.Lhs, kind, ownerSheet, target, index),
			Rhs: ShiftAST(x.Rhs, kind, ownerSheet, target, index)}
	case *CallNode:
		args := make([]Node, len(x.Args))
		for i, a := range x.Args {
			args[i] = ShiftAST(a, kind, ownerSheet, target, index)
		}
		return &CallNode{Name: x.Name, Args: args}
	}
	return n
}

func withSheet(r Ref, sheet string) Ref {
	if r.Sheet == "" {
		r.Sheet = sheet
	}
	return r
}
