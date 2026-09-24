package collab

import (
	"encoding/json"

	"collabsheet/internal/engine"
	"collabsheet/internal/formula"
	"collabsheet/internal/workbook"
)

func send(c Client, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.Send(b)
}

func (r *Room) broadcastLocked(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	for _, s := range r.clients {
		s.c.Send(b)
	}
}

func (r *Room) buildPresenceLocked(self string) Presence {
	peers := make([]Peer, 0, len(r.clients))
	for _, s := range sortedSessions(r.clients) {
		peers = append(peers, Peer{
			ID: s.id, Name: s.name, Color: s.color, Sheet: s.sheet, Cell: s.cell,
		})
	}
	return Presence{Me: self, Peers: peers}
}

func (r *Room) broadcastPresenceLocked() {
	// Each client gets presence with its own id as "me".
	presences := map[string][]byte{}
	for id := range r.clients {
		b, _ := json.Marshal(PresenceMsg{Type: "presence", Presence: r.buildPresenceLocked(id)})
		presences[id] = b
	}
	for id, s := range r.clients {
		s.c.Send(presences[id])
	}
}

func (r *Room) broadcastSnapshotLocked() {
	for _, s := range r.clients {
		msg := SnapshotMsg{
			Type:     "snapshot",
			Version:  r.version,
			Presence: r.buildPresenceLocked(s.id),
		}
		msg.Workbook = describeWorkbook(r.eng)
		send(s.c, msg)
	}
}

func (r *Room) sendUndoResultLocked(userID string, ok bool) {
	if s := r.clients[userID]; s != nil {
		send(s.c, UndoResultMsg{
			Type: "undo_result", OK: ok,
			CanUndo: len(r.undo[userID]) > 0,
			CanRedo: len(r.redo[userID]) > 0,
		})
	}
}

func (r *Room) sendRedoResultLocked(userID string, ok bool) {
	if s := r.clients[userID]; s != nil {
		send(s.c, UndoResultMsg{
			Type: "redo_result", OK: ok,
			CanUndo: len(r.undo[userID]) > 0,
			CanRedo: len(r.redo[userID]) > 0,
		})
	}
}

// sendStackStateLocked tells the acting user whether undo/redo are available.
func (r *Room) sendStackStateLocked(userID string) {
	if s := r.clients[userID]; s != nil {
		send(s.c, StackStateMsg{
			Type:    "stack_state",
			CanUndo: len(r.undo[userID]) > 0,
			CanRedo: len(r.redo[userID]) > 0,
		})
	}
}

// describeWorkbook builds the full serializable workbook state with computed
// values and cycle flags.
func describeWorkbook(eng *engine.Engine) WorkbookData {
	wb := eng.Workbook()
	data := WorkbookData{Sheets: make([]SheetData, 0, len(wb.Sheets))}
	for _, sh := range wb.Sheets {
		sd := SheetData{
			Name: sh.Name, Rows: sh.Rows, Cols: sh.Cols,
			Cells: map[string]CellValue{},
		}
		for addr, c := range sh.Cells {
			col, row, err := workbook.ParseAddr(addr)
			if err != nil {
				continue
			}
			res := eng.Result(sh.Name, col, row)
			cv := cellToValue(sh.Name, addr, c.Raw, res)
			sd.Cells[addr] = cv
		}
		data.Sheets = append(data.Sheets, sd)
	}
	return data
}

func (r *Room) collectPatchesLocked(eng *engine.Engine, keys []engine.Key) []CellValue {
	out := make([]CellValue, 0, len(keys))
	seen := map[engine.Key]bool{}
	for _, k := range keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		sh := eng.Workbook().SheetByName(k.Sheet)
		if sh == nil {
			continue
		}
		addr := workbook.Addr(k.Col, k.Row)
		raw := ""
		if c := sh.Get(k.Col, k.Row); c != nil {
			raw = c.Raw
		}
		res := eng.Result(k.Sheet, k.Col, k.Row)
		out = append(out, cellToValue(k.Sheet, addr, raw, res))
	}
	return out
}

func cellToValue(sheet, addr, raw string, res engine.CellResult) CellValue {
	cv := CellValue{
		Sheet: sheet, Cell: addr, Raw: raw,
		Value: res.Value.Display(),
		Kind:  kindOf(res.Value),
		Cycle: res.IsCycle,
		Error: res.Value.Typ == formula.VError,
	}
	return cv
}

func kindOf(v formula.Value) string {
	switch v.Typ {
	case formula.VNumber:
		return "number"
	case formula.VString:
		return "string"
	case formula.VBool:
		return "bool"
	case formula.VError:
		return "error"
	}
	return "blank"
}
