package engine

// tarjan runs Tarjan's strongly connected components algorithm.
//
// It returns:
//   - order: vertex indices in evaluation order (pure dependencies first,
//     dependents after) — Tarjan emits SCCs sink-first, so component id 0 is
//     the deepest-dependency SCC and ascending id gives a valid topological
//     evaluation order;
//   - comp: SCC id per vertex.
//
// A vertex forms a cycle if its SCC has >1 vertex or it has a self-edge.
func tarjan(adj [][]int) (order []int, comp []int) {
	n := len(adj)
	index := make([]int, n)
	low := make([]int, n)
	onStack := make([]bool, n)
	stack := make([]int, 0, n)
	comp = make([]int, n)
	for i := range index {
		index[i] = -1
		comp[i] = -1
	}
	nextIndex := 0
	compCount := 0

	// Iterative DFS frames to avoid recursion depth problems on large sheets.
	type frame struct {
		v, pi int
	}
	popOrder := make([]int, 0, n)

	var strong func(v int)
	strong = func(v int) {
		work := []frame{{v, 0}}
		index[v] = nextIndex
		low[v] = nextIndex
		nextIndex++
		stack = append(stack, v)
		onStack[v] = true

		for len(work) > 0 {
			top := &work[len(work)-1]
			u := top.v
			if top.pi < len(adj[u]) {
				w := adj[u][top.pi]
				top.pi++
				if index[w] == -1 {
					index[w] = nextIndex
					low[w] = nextIndex
					nextIndex++
					stack = append(stack, w)
					onStack[w] = true
					work = append(work, frame{w, 0})
				} else if onStack[w] {
					if index[w] < low[u] {
						low[u] = index[w]
					}
				}
			} else {
				if low[u] == index[u] {
					for {
						w := stack[len(stack)-1]
						stack = stack[:len(stack)-1]
						onStack[w] = false
						comp[w] = compCount
						if w == u {
							break
						}
					}
					compCount++
				}
				popOrder = append(popOrder, u)
				work = work[:len(work)-1]
				if len(work) > 0 {
					parent := work[len(work)-1].v
					if low[u] < low[parent] {
						low[parent] = low[u]
					}
				}
			}
		}
	}

	for v := 0; v < n; v++ {
		if index[v] == -1 {
			strong(v)
		}
	}

	// comp id 0 is the sink SCC (deepest dependencies); iterate ascending so
	// referenced cells are computed before cells that reference them.
	buckets := make([][]int, compCount)
	for v := 0; v < n; v++ {
		buckets[comp[v]] = append(buckets[comp[v]], v)
	}
	for c := 0; c < compCount; c++ {
		order = append(order, buckets[c]...)
	}
	return order, comp
}
