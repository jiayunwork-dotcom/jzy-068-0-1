package engine

import (
	"strconv"
	"strings"

	"collabsheet/internal/formula"
)

// evalContext implements formula.EvalContext over the workbook. During a
// recalculation pass all reads hit values already computed in that pass (or
// pre-existing values for nodes outside the affected set), so evaluation
// never triggers recursive recalculation.
type evalContext struct {
	wb    *Workbook
	sheet *Sheet
}

func (c *evalContext) CurrentSheet() string { return c.sheet.Name }

func (c *evalContext) GetCell(sheetName string, col, row int) (formula.Value, bool) {
	s := c.wb.Sheet(sheetName)
	if s == nil {
		return formula.ErrorValue(formula.ErrRef), true
	}
	if cell := s.cell(col, row); cell != nil {
		return cell.Value, true
	}
	return formula.BlankValue(), true
}

func (c *evalContext) GetRange(r formula.Ref) (*formula.Matrix, error) {
	s := c.wb.Sheet(r.Sheet)
	if s == nil {
		return nil, errRef
	}
	rows := r.Row2 - r.Row1 + 1
	cols := r.Col2 - r.Col1 + 1
	if rows <= 0 || cols <= 0 {
		return nil, errRef
	}
	m := formula.NewMatrix(rows, cols)
	for rr := 0; rr < rows; rr++ {
		for cc := 0; cc < cols; cc++ {
			if cell := s.cell(r.Col1+cc, r.Row1+rr); cell != nil {
				m.Data[rr][cc] = cell.Value
			}
		}
	}
	return m, nil
}

type plainError string

func (e plainError) Error() string { return string(e) }

const errRef = plainError("bad range")

// SetCell stores user input in one cell, updates its dependency edges and
// recalculates it together with every direct/indirect dependent in strict
// topological order. Returns the changed cells (including recomputed
// dependents) for broadcasting.
func (wb *Workbook) SetCell(sheetName string, col, row int, input string) []CellChange {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	s := wb.Sheet(sheetName)
	if s == nil {
		return nil
	}
	if col < 0 || row < 0 || col >= s.Cols || row >= s.Rows {
		return nil
	}

	key := pack(col, row)
	node := s.nodeKey(col, row)
	input = strings.TrimSpace(input)

	if input == "" {
		delete(s.Cells, key)
		wb.graph.clearOut(node)
	} else {
		c := s.Cells[key]
		if c == nil {
			c = &Cell{Col: col, Row: row}
			s.Cells[key] = c
		}
		c.Input = input
		c.IsFormula = strings.HasPrefix(input, "=")
		c.AST = nil
		c.Circular = false
		if c.IsFormula {
			ast, err := formula.Parse(input)
			if err != nil {
				c.AST = nil
				c.IsFormula = false // parse failure shows as error value, literal text kept
				c.Value = formula.ErrorValue(formula.ErrParse)
				c.Display = string(formula.ErrParse)
				c.parseFailed = true
				wb.graph.clearOut(node)
			} else {
				c.AST = ast
				c.parseFailed = false
				refs := formula.ExtractDeps(ast, sheetName)
				wb.graph.setDeps(wb, node, refs)
			}
		} else {
			c.parseFailed = false
			c.Value = literalValue(input)
			c.Display = c.Value.Display()
			wb.graph.clearOut(node)
		}
	}

	changed := wb.recalcFrom([]uint64{node})
	return changed
}

// CellChange describes one recomputed cell, as broadcast to collaborators.
type CellChange struct {
	Sheet     string
	Col       int
	Row       int
	Input     string
	IsFormula bool
	Display   string
	Kind      string // "blank","number","string","boolean","error"
	Num       float64
	Str       string
	Bool      bool
	Error     string
	Circular  bool
}

// recalcFrom recomputes the affected subgraph (the roots plus everything
// downstream). The subgraph is evaluated as SCCs in topological order, so
// every dependency's fresh value is available; every cell in a non-trivial
// SCC (or with a self-loop) is marked circular.
func (wb *Workbook) recalcFrom(roots []uint64) []CellChange {
	affected := wb.graph.downstream(roots)

	// Build the affected subgraph using existing outgoing edges.
	subOut := make(map[uint64]map[uint64]bool, len(affected))
	for n := range affected {
		subOut[n] = map[uint64]bool{}
	}
	for n := range affected {
		for dep := range wb.graph.out[n] {
			if affected[dep] {
				subOut[n][dep] = true
			}
		}
	}

	sccs, compOf := tarjanSCC(subOut)

	// Edges between SCCs; Kahn order = dependency first.
	sccEdges := make(map[int]map[int]bool, len(sccs))
	indeg := make([]int, len(sccs))
	for u, deps := range subOut {
		cu := compOf[u]
		for v := range deps {
			cv := compOf[v]
			if cu == cv {
				continue
			}
			if sccEdges[cv] == nil {
				sccEdges[cv] = map[int]bool{}
			}
			if !sccEdges[cv][cu] {
				sccEdges[cv][cu] = true
				indeg[cu]++
			}
		}
	}

	// Kahn: SCC with indeg 0 has no dependency inside the subgraph; it may
	// still depend on unaffected cells, whose values are current in the map.
	var queue []int
	for i, d := range indeg {
		if d == 0 {
			queue = append(queue, i)
		}
	}
	order := queue[:0:0]
	for len(queue) > 0 {
		cid := queue[0]
		queue = queue[1:]
		order = append(order, cid)
		for next := range sccEdges[cid] {
			indeg[next]--
			if indeg[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	var changes []CellChange
	for _, cid := range order {
		members := sccs[cid]
		cyclic := len(members) > 1 || subOut[members[0]][members[0]]
		for _, node := range members {
			ch := wb.evalNode(node, cyclic)
			changes = append(changes, ch)
		}
	}
	return changes
}

// evalNode computes one cell's value; cyclic cells get #CIRCULAR! without
// being evaluated (which is what prevents infinite recursion).
func (wb *Workbook) evalNode(node uint64, cyclic bool) CellChange {
	sheetID, col, row := keyParts(node)
	s := wb.sheetByID(sheetID)
	c := s.cell(col, row)

	mk := func(v formula.Value, circ bool) CellChange {
		if c != nil {
			c.Value = v
			c.Display = v.Display()
			c.Circular = circ
		}
		return changeOf(s, col, row, c, v, circ)
	}

	if cyclic {
		return mk(formula.ErrorValue(formula.ErrCircular), true)
	}
	if c == nil {
		// An empty root (e.g. a deleted cell whose downstream still exists)
		// contributes no change for itself.
		return CellChange{Sheet: s.Name, Col: col, Row: row, Kind: "blank"}
	}
	c.Circular = false
	if c.parseFailed {
		return mk(formula.ErrorValue(formula.ErrParse), false)
	}
	if !c.IsFormula || c.AST == nil {
		return mk(c.Value, false)
	}
	ev := formula.NewEvaluator(&evalContext{wb: wb, sheet: s})
	v := ev.Eval(c.AST)
	return mk(v, false)
}

func changeOf(s *Sheet, col, row int, c *Cell, v formula.Value, circ bool) CellChange {
	ch := CellChange{Sheet: s.Name, Col: col, Row: row, Display: v.Display(), Circular: circ}
	if c != nil {
		ch.Input = c.Input
		ch.IsFormula = c.IsFormula
	}
	switch v.Kind {
	case formula.VBlank:
		ch.Kind = "blank"
	case formula.VNumber:
		ch.Kind = "number"
		ch.Num = v.Num
	case formula.VString:
		ch.Kind = "string"
		ch.Str = v.Str
	case formula.VBoolean:
		ch.Kind = "boolean"
		ch.Bool = v.Bool
	case formula.VError:
		ch.Kind = "error"
		ch.Error = string(v.Error)
	}
	return ch
}

func (wb *Workbook) sheetByID(id int) *Sheet {
	for _, s := range wb.Sheets {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// literalValue parses text typed into a non-formula cell: numbers become
// numbers, TRUE/FALSE become booleans, everything else is text.
func literalValue(input string) formula.Value {
	if f, err := strconv.ParseFloat(input, 64); err == nil {
		return formula.NumberValue(f)
	}
	switch strings.ToUpper(input) {
	case "TRUE":
		return formula.BoolValue(true)
	case "FALSE":
		return formula.BoolValue(false)
	}
	return formula.StringValue(input)
}

// RecalcAll recomputes every formula in the workbook; used at load time and
// after structural edits.
func (wb *Workbook) RecalcAll() []CellChange {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	return wb.recalcAllLocked()
}

func (wb *Workbook) recalcAllLocked() []CellChange {
	// Nodes = every stored cell. Literal nodes have no outgoing edges and
	// evaluate trivially; ordering still honors dependencies.
	all := map[uint64]bool{}
	for _, s := range wb.Sheets {
		for k, c := range s.Cells {
			if c.Input != "" {
				col, row := unpack(k)
				all[s.nodeKey(col, row)] = true
			}
		}
	}
	subOut := make(map[uint64]map[uint64]bool, len(all))
	for n := range all {
		subOut[n] = map[uint64]bool{}
	}
	for n := range all {
		for dep := range wb.graph.out[n] {
			if all[dep] {
				subOut[n][dep] = true
			}
		}
	}
	sccs, compOf := tarjanSCC(subOut)
	sccEdges := make(map[int]map[int]bool, len(sccs))
	indeg := make([]int, len(sccs))
	for u, deps := range subOut {
		cu := compOf[u]
		for v := range deps {
			cv := compOf[v]
			if cu == cv {
				continue
			}
			if sccEdges[cv] == nil {
				sccEdges[cv] = map[int]bool{}
			}
			if !sccEdges[cv][cu] {
				sccEdges[cv][cu] = true
				indeg[cu]++
			}
		}
	}
	var queue []int
	for i, d := range indeg {
		if d == 0 {
			queue = append(queue, i)
		}
	}
	var changes []CellChange
	for len(queue) > 0 {
		cid := queue[0]
		queue = queue[1:]
		members := sccs[cid]
		cyclic := len(members) > 1 || subOut[members[0]][members[0]]
		for _, node := range members {
			changes = append(changes, wb.evalNode(node, cyclic))
		}
		for next := range sccEdges[cid] {
			indeg[next]--
			if indeg[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	return changes
}

// RebuildGraph reparses every formula and reconstructs dependency edges from
// scratch. Used after structural surgery changes coordinates wholesale.
func (wb *Workbook) rebuildGraph() {
	wb.graph = NewGraph()
	for _, s := range wb.Sheets {
		for k, c := range s.Cells {
			if !c.IsFormula || c.AST == nil {
				continue
			}
			col, row := unpack(k)
			refs := formula.ExtractDeps(c.AST, s.Name)
			wb.graph.setDeps(wb, s.nodeKey(col, row), refs)
		}
	}
}
