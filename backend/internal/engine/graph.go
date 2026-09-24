package engine

import (
	"collabsheet/internal/formula"
)

// Key layout inside one uint64:
//
//	bits 48..63 sheet id (0..65535)
//	bits 32..47 column   (0..65535)
//	bits  0..31 row      (0..4294967295)
const (
	keyRowMask    = 0xffffffff
	keyColShift   = 32
	keySheetShift = 48
)

func makeKey(sheetID, col, row int) uint64 {
	return uint64(sheetID)<<keySheetShift | uint64(col)<<keyColShift | uint64(uint32(row))
}

func keyParts(k uint64) (sheetID, col, row int) {
	return int(k >> keySheetShift),
		int((k >> keyColShift) & 0xffff),
		int(k & keyRowMask)
}

// keyCompare orders global keys deterministically (sheet, then row, then col)
// so recalculation is reproducible.
func keyCompare(a, b uint64) bool {
	sa, ca, ra := keyParts(a)
	sb, cb, rb := keyParts(b)
	if sa != sb {
		return sa < sb
	}
	if ra != rb {
		return ra < rb
	}
	return ca < cb
}

// Graph is the cross-sheet cell dependency graph.
//
//	out[cell] = set of cells its formula reads   (this -> deps)
//	in[cell] = set of formulas that read it      (reverse edges)
type Graph struct {
	out map[uint64]map[uint64]bool
	in  map[uint64]map[uint64]bool
}

func NewGraph() *Graph {
	return &Graph{
		out: map[uint64]map[uint64]bool{},
		in:  map[uint64]map[uint64]bool{},
	}
}

func (g *Graph) addEdge(from, to uint64) {
	ms, ok := g.out[from]
	if !ok {
		ms = map[uint64]bool{}
		g.out[from] = ms
	}
	ms[to] = true
	ms2, ok := g.in[to]
	if !ok {
		ms2 = map[uint64]bool{}
		g.in[to] = ms2
	}
	ms2[from] = true
}

// setDeps replaces the outgoing edges of node with refs resolved through wb.
func (g *Graph) setDeps(wb *Workbook, node uint64, refs []formula.Ref) {
	g.clearOut(node)
	for _, r := range refs {
		target := wb.Sheet(r.Sheet)
		if target == nil {
			continue
		}
		col2, row2 := r.Col2, r.Row2
		if !r.IsRange {
			col2, row2 = r.Col1, r.Row1
		}
		for row := r.Row1; row <= row2; row++ {
			for col := r.Col1; col <= col2; col++ {
				if col < 0 || row < 0 || col >= target.Cols || row >= target.Rows {
					continue
				}
				g.addEdge(node, makeKey(target.ID, col, row))
			}
		}
	}
}

func (g *Graph) clearOut(node uint64) {
	for to := range g.out[node] {
		delete(g.in[to], node)
		if len(g.in[to]) == 0 {
			delete(g.in, to)
		}
	}
	delete(g.out, node)
}

// downstream returns nodes reachable via reverse edges from roots plus the
// roots themselves.
func (g *Graph) downstream(roots []uint64) map[uint64]bool {
	seen := map[uint64]bool{}
	var stack []uint64
	for _, r := range roots {
		if !seen[r] {
			seen[r] = true
			stack = append(stack, r)
		}
	}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for d := range g.in[n] {
			if !seen[d] {
				seen[d] = true
				stack = append(stack, d)
			}
		}
	}
	return seen
}
