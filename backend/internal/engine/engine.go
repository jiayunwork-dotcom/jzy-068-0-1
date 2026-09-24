// Package engine computes formula results: it maintains the dependency graph
// between cells, performs topological recalculation after edits and detects
// circular references.
package engine

import (
	"strings"
	"sync"

	"collabsheet/internal/formula"
	"collabsheet/internal/workbook"
)

// Key identifies a cell workbook-wide.
type Key struct {
	Sheet string
	Col   int
	Row   int
}

// CellResult is the computed state of a cell.
type CellResult struct {
	Value   formula.Value
	IsCycle bool
}

// Engine owns a workbook and all derived state.
type Engine struct {
	wb *workbook.Workbook

	mu sync.RWMutex

	// deps[x] = set of cells x's formula references.
	deps map[Key]map[Key]bool
	// rdeps[x] = set of formulas referencing x.
	rdeps map[Key]map[Key]bool

	parsed map[Key]formula.Node
	values map[Key]formula.Value
	cycles map[Key]bool

	// currentSheet implements same-sheet references (RefNode.Sheet == "").
	currentSheet string

	// OnEvaluate, when set, observes keys in the exact evaluation order.
	OnEvaluate func(k Key)
}

// New builds an engine and performs an initial full recalculation.
func New(wb *workbook.Workbook) *Engine {
	e := &Engine{
		wb:     wb,
		deps:   map[Key]map[Key]bool{},
		rdeps:  map[Key]map[Key]bool{},
		parsed: map[Key]formula.Node{},
		values: map[Key]formula.Value{},
		cycles: map[Key]bool{},
	}
	e.RecalcAll()
	return e
}

// Workbook exposes the underlying document.
func (e *Engine) Workbook() *workbook.Workbook { return e.wb }

func (e *Engine) lock()   { e.mu.Lock() }
func (e *Engine) unlock() { e.mu.Unlock() }

// ---- graph maintenance ----

func (e *Engine) setDeps(k Key, refs []formula.RefInfo) {
	want := map[Key]bool{}
	for _, r := range refs {
		sheet := r.Sheet
		if sheet == "" {
			sheet = k.Sheet
		}
		for c := r.Col1; c <= r.Col2; c++ {
			for rr := r.Row1; rr <= r.Row2; rr++ {
				want[Key{Sheet: sheet, Col: c, Row: rr}] = true
			}
		}
	}
	old := e.deps[k]
	for dep := range old {
		if !want[dep] {
			delete(e.rdeps[dep], k)
		}
	}
	for dep := range want {
		if !old[dep] {
			if e.rdeps[dep] == nil {
				e.rdeps[dep] = map[Key]bool{}
			}
			e.rdeps[dep][k] = true
		}
	}
	if len(want) == 0 {
		delete(e.deps, k)
	} else {
		e.deps[k] = want
	}
}

func (e *Engine) rebuildGraph() {
	e.deps = map[Key]map[Key]bool{}
	e.rdeps = map[Key]map[Key]bool{}
	e.parsed = map[Key]formula.Node{}
	for _, sh := range e.wb.Sheets {
		for a, c := range sh.Cells {
			if !strings.HasPrefix(c.Raw, "=") {
				continue
			}
			col, row, err := workbook.ParseAddr(a)
			if err != nil {
				continue
			}
			k := Key{sh.Name, col, row}
			ast, perr := formula.Parse(c.Raw)
			if perr == nil {
				e.parsed[k] = ast
				e.setDeps(k, formula.ReferencesOf(ast, sh.Name))
			} else {
				delete(e.parsed, k)
				e.setDeps(k, nil)
			}
		}
	}
}

// ---- evaluation ----

// CellValue implements formula.Evaler.
func (e *Engine) CellValue(sheet string, col, row int) formula.Value {
	if sheet == "" {
		sheet = e.currentSheet
	}
	if v, ok := e.values[Key{sheet, col, row}]; ok {
		return v
	}
	return formula.Blank()
}

// RangeValues implements formula.Evaler.
func (e *Engine) RangeValues(sheet string, c1, r1, c2, r2 int) formula.Matrix {
	if sheet == "" {
		sheet = e.currentSheet
	}
	m := formula.Matrix{Rows: r2 - r1 + 1, Cols: c2 - c1 + 1}
	m.Values = make([]formula.Value, 0, m.Rows*m.Cols)
	for r := r1; r <= r2; r++ {
		for c := c1; c <= c2; c++ {
			m.Values = append(m.Values, e.CellValue(sheet, c, r))
		}
	}
	return m
}

// literalValue computes the value of a non-formula cell entry.
func literalValue(raw string) formula.Value {
	if f, ok := parseNumberLiteral(raw); ok {
		return formula.Number(f)
	}
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "TRUE":
		return formula.Boolean(true)
	case "FALSE":
		return formula.Boolean(false)
	}
	if strings.HasPrefix(strings.TrimSpace(raw), "#") {
		return formula.Value{Typ: formula.VError, Str: strings.TrimSpace(raw)}
	}
	return formula.Text(raw)
}

func (e *Engine) evalLiteralCell(sh *workbook.Sheet, col, row int) {
	k := Key{sh.Name, col, row}
	c := sh.Get(col, row)
	if c == nil {
		delete(e.values, k)
		return
	}
	e.values[k] = literalValue(c.Raw)
}

// RecalcAll recomputes every cell from scratch.
func (e *Engine) RecalcAll() {
	e.lock()
	defer e.unlock()
	e.values = map[Key]formula.Value{}
	e.cycles = map[Key]bool{}

	// Literal cells first.
	for _, sh := range e.wb.Sheets {
		for a, c := range sh.Cells {
			col, row, err := workbook.ParseAddr(a)
			if err != nil {
				continue
			}
			if strings.HasPrefix(c.Raw, "=") {
				continue
			}
			e.values[Key{sh.Name, col, row}] = literalValue(c.Raw)
		}
	}
	e.rebuildGraph()
	e.evaluateSet(e.formulaKeys(), true)
}

func (e *Engine) formulaKeys() []Key {
	keys := make([]Key, 0, len(e.parsed))
	for k := range e.parsed {
		keys = append(keys, k)
	}
	return keys
}

// RecalcAfterEdit reparses one edited cell and recalculates it plus every
// direct and indirect dependent, in dependency-first topological order.
func (e *Engine) RecalcAfterEdit(sheet string, col, row int) {
	e.lock()
	defer e.unlock()

	k := Key{sheet, col, row}
	sh := e.wb.SheetByName(sheet)
	c := sh.Get(col, row)

	// Refresh the cell's own value and graph edges.
	if c == nil {
		delete(e.parsed, k)
		delete(e.values, k)
		e.setDeps(k, nil)
	} else if strings.HasPrefix(c.Raw, "=") {
		ast, err := formula.Parse(c.Raw)
		if err != nil {
			delete(e.parsed, k)
			e.setDeps(k, nil)
			e.values[k] = formula.ParseErrVal
		} else {
			e.parsed[k] = ast
			e.setDeps(k, formula.ReferencesOf(ast, sheet))
		}
	} else {
		delete(e.parsed, k)
		e.setDeps(k, nil)
		e.values[k] = literalValue(c.Raw)
	}

	affected := e.affectedBy(k)
	// Clear stale cycle flags for every cell that could change membership this
	// turn; the SCC pass below re-flags cells that are still inside a ring.
	for _, kk := range affected {
		delete(e.cycles, kk)
	}
	e.evaluateSet(affected, false)
}

// affectedBy returns the edited key and all transitive dependents.
func (e *Engine) affectedBy(start Key) []Key {
	seen := map[Key]bool{start: true}
	stack := []Key{start}
	for len(stack) > 0 {
		k := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for d := range e.rdeps[k] {
			if !seen[d] {
				seen[d] = true
				stack = append(stack, d)
			}
		}
	}
	out := make([]Key, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

// evaluateSet evaluates the given formula keys dependencies-first, marking
// cyclic SCCs with #CYCLE!. Literal cells in the set are refreshed too.
func (e *Engine) evaluateSet(keys []Key, allGraph bool) {
	index := map[Key]int{}
	for i, k := range keys {
		index[k] = i
	}

	// Induced edges restricted to the set (full graph mode has no restriction).
	adj := make([][]int, len(keys))
	for i, k := range keys {
		for dep := range e.deps[k] {
			if allGraph {
				// external dep: rely on cached value; do not traverse
				if j, ok := index[dep]; ok {
					adj[i] = append(adj[i], j)
				}
			} else if j, ok := index[dep]; ok {
				adj[i] = append(adj[i], j)
			}
		}
	}

	// Tarjan SCC; pop order is sink (deepest dependency) first.
	order, sccOf := tarjan(adj)

	// Mark every cell inside a cyclic SCC.
	for _, members := range groupBySCC(len(keys), sccOf) {
		cyclic := len(members) > 1
		if !cyclic && len(members) == 1 {
			u := members[0]
			for _, v := range adj[u] {
				if v == u {
					cyclic = true
					break
				}
			}
		}
		if cyclic {
			for _, u := range members {
				k := keys[u]
				e.cycles[k] = true
				e.values[k] = formula.CycleErr
			}
			continue
		}
	}

	// Evaluate in Tarjan pop order (dependencies first).
	for _, u := range order {
		k := keys[u]
		if e.cycles[k] {
			continue
		}
		// A non-cycle node depending on a cycle node propagates #CYCLE!
		// naturally through evaluation; clear stale cycle flag first.
		delete(e.cycles, k)
		ast, isFormula := e.parsed[k]
		if !isFormula {
			// Could be a literal cell included only in full mode.
			if sh := e.wb.SheetByName(k.Sheet); sh != nil {
				e.evalLiteralCell(sh, k.Col, k.Row)
			}
			continue
		}
		e.currentSheet = k.Sheet
		v := formula.Eval(ast, e)
		e.values[k] = v
		if e.OnEvaluate != nil {
			e.OnEvaluate(k)
		}
	}
}

// groupBySCC groups vertex indices by SCC id.
func groupBySCC(n int, comp []int) map[int][]int {
	m := map[int][]int{}
	for v := 0; v < n; v++ {
		m[comp[v]] = append(m[comp[v]], v)
	}
	return m
}

// ---- public queries ----

// Result returns the computed result of a cell.
func (e *Engine) Result(sheet string, col, row int) CellResult {
	e.mu.RLock()
	defer e.mu.RUnlock()
	k := Key{sheet, col, row}
	v, ok := e.values[k]
	if !ok {
		v = formula.Blank()
	}
	return CellResult{Value: v, IsCycle: e.cycles[k]}
}

// IsCycle reports whether the cell is part of a circular reference.
func (e *Engine) IsCycle(sheet string, col, row int) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cycles[Key{sheet, col, row}]
}

// CycleCells returns all currently cyclic cell keys.
func (e *Engine) CycleCells() []Key {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Key, 0, len(e.cycles))
	for k := range e.cycles {
		out = append(out, k)
	}
	return out
}

// Dependents returns every formula cell that transitively depends on start
// (does not include start itself).
func (e *Engine) Dependents(start Key) []Key {
	e.mu.RLock()
	defer e.mu.RUnlock()
	seen := map[Key]bool{}
	stack := []Key{start}
	for len(stack) > 0 {
		k := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for d := range e.rdeps[k] {
			if !seen[d] {
				seen[d] = true
				stack = append(stack, d)
			}
		}
	}
	out := make([]Key, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}
