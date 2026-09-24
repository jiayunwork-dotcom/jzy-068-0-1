package engine

import "sort"

// This file finds strongly connected components with an iterative version of
// Tarjan's algorithm. SCCs are returned in *reverse* topological order of
// the condensation DAG (Tarjan's natural output order); callers that need
// dependency-first order use Kahn as implemented in recalc.go.
//
// Every SCC of size > 1, and every node with a self-loop, is a circular
// reference cluster and is flagged by the recalculation driver.

type tarjanState struct {
	index   map[uint64]int
	low     map[uint64]int
	onStack map[uint64]bool
	indices []uint64 // nodes in discovery order (index = slice position)
	stack   []uint64
	next    int

	out    map[uint64]map[uint64]bool
	sccs   [][]uint64
	compOf map[uint64]int
}

// frame represents one suspended DFS call for node v. pi is the position in
// its (sorted) successor list we should resume from.
type frame struct {
	v  uint64
	pi int
}

func tarjanSCC(out map[uint64]map[uint64]bool) ([][]uint64, map[uint64]int) {
	st := &tarjanState{
		index:   map[uint64]int{},
		low:     map[uint64]int{},
		onStack: map[uint64]bool{},
		out:     out,
		compOf:  map[uint64]int{},
	}

	// Deterministic root order.
	roots := make([]uint64, 0, len(out))
	for v := range out {
		roots = append(roots, v)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i] < roots[j] })

	for _, root := range roots {
		if _, seen := st.index[root]; seen {
			continue
		}
		st.push(root)

		var call []frame
		call = append(call, frame{v: root, pi: 0})
		for len(call) > 0 {
			top := &call[len(call)-1]
			v := top.v
			succ := st.sortedSucc(v)
			if top.pi < len(succ) {
				w := succ[top.pi]
				top.pi++
				if _, seen := st.index[w]; !seen {
					st.push(w)
					call = append(call, frame{v: w, pi: 0})
					continue
				}
				if st.onStack[w] {
					if st.index[w] < st.low[v] {
						st.low[v] = st.index[w]
					}
				}
				continue
			}
			// Finished v.
			call = call[:len(call)-1]
			if st.low[v] == st.index[v] {
				st.popSCC(v)
			}
			if len(call) > 0 {
				parent := call[len(call)-1].v
				if st.low[v] < st.low[parent] {
					st.low[parent] = st.low[v]
				}
			}
		}
	}
	return st.sccs, st.compOf
}

func (st *tarjanState) push(v uint64) {
	st.index[v] = st.next
	st.low[v] = st.next
	st.next++
	st.indices = append(st.indices, v)
	st.stack = append(st.stack, v)
	st.onStack[v] = true
}

func (st *tarjanState) popSCC(v uint64) {
	var comp []uint64
	for {
		n := len(st.stack) - 1
		w := st.stack[n]
		st.stack = st.stack[:n]
		st.onStack[w] = false
		comp = append(comp, w)
		if w == v {
			break
		}
	}
	id := len(st.sccs)
	st.sccs = append(st.sccs, comp)
	for _, w := range comp {
		st.compOf[w] = id
	}
}

func (st *tarjanState) sortedSucc(v uint64) []uint64 {
	succ := make([]uint64, 0, len(st.out[v]))
	for w := range st.out[v] {
		succ = append(succ, w)
	}
	sort.Slice(succ, func(i, j int) bool { return succ[i] < succ[j] })
	return succ
}
