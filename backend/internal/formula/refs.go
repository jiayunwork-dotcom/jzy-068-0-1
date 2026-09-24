package formula

// Axis selects rows or columns for structural shifts.
type Axis int

const (
	AxisRow Axis = iota
	AxisCol
)

// Shift describes inserting or deleting Count rows/columns at 0-based Index
// on TargetSheet. Delete=false means insert; true means delete.
type Shift struct {
	Axis        Axis
	TargetSheet string
	Index       int
	Count       int
	Delete      bool
}

// shiftIndex maps a single 0-based coordinate through the shift.
// deleted=true means the referenced row/column was removed entirely.
func shiftIndex(coord, index, count int, del bool) (int, bool) {
	return ShiftIndex(coord, index, count, del)
}

// ShiftIndex is the exported variant used by content shifting in workbook.
func ShiftIndex(coord, index, count int, del bool) (int, bool) {
	if !del {
		if coord >= index {
			return coord + count, false
		}
		return coord, false
	}
	end := index + count
	if coord >= index && coord < end {
		return 0, true
	}
	if coord >= end {
		return coord - count, false
	}
	return coord, false
}

// AdjustReferences rewrites all references in ast to survive the structural
// change described by op. homeSheet is the sheet containing the formula:
// same-sheet references carry an empty Sheet field.
//
// Rules:
//   - absolute row/col coordinates do not shift when content is inserted or
//     when content elsewhere moves;
//   - but if the absolute row/column is itself deleted, the reference is
//     invalidated like any other (#REF!), since it can no longer point at a
//     live cell;
//   - references to other sheets only move when the target sheet is the one
//     being modified.
func AdjustReferences(ast Node, homeSheet string, op Shift) Node {
	return MapRefs(ast, func(r *RefNode) *RefNode {
		sheet := r.Sheet
		if sheet == "" {
			sheet = homeSheet
		}
		if !sameSheet(sheet, op.TargetSheet) {
			return r
		}
		abs := op.Axis == AxisRow && r.AbsRow || op.Axis == AxisCol && r.AbsCol
		coord := r.Row
		if op.Axis == AxisCol {
			coord = r.Col
		}
		// Absolute coordinates ignore inserts and upward shifts; they only
		// care whether the exact coordinate was deleted.
		if abs {
			if op.Delete && coord >= op.Index && coord < op.Index+op.Count {
				return nil
			}
			return r
		}
		nv, deleted := shiftIndex(coord, op.Index, op.Count, op.Delete)
		if deleted {
			return nil
		}
		cp := *r
		if op.Axis == AxisRow {
			cp.Row = nv
		} else {
			cp.Col = nv
		}
		return &cp
	})
}

func sameSheet(a, b string) bool { return a == b }

// ExtractRefs lists every reference in an AST as resolved coordinates
// (sheet name is filled with homeSheet for same-sheet refs).
type RefInfo struct {
	Sheet                  string
	Col1, Row1, Col2, Row2 int
	IsRange                bool
}

// ReferencesOf returns all references of a parsed formula.
func ReferencesOf(ast Node, homeSheet string) []RefInfo {
	var out []RefInfo
	WalkRefs(ast, func(sheet string, c, r int, isRange bool, c2, r2 int) {
		if sheet == "" {
			sheet = homeSheet
		}
		out = append(out, RefInfo{Sheet: sheet, Col1: c, Row1: r, Col2: c2, Row2: r2, IsRange: isRange})
	})
	return out
}
