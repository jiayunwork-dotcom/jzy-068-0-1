package collab

import "collabsheet/internal/engine"

// ApplyCellEdit is the heart of the collaboration protocol.
//
//  1. The engine stores the new input and recomputes the whole downstream
//     subgraph in topological order (including other people's formula cells
//     that read this cell).
//  2. If the cell's previous author was a *different* connected user, that
//     user receives an "overwritten" notice — last writer wins.
//  3. The mutation is pushed onto the acting user's private undo stack and
//     their redo stack is cleared. Nobody else's history is touched.
func (h *Hub) ApplyCellEdit(id, msgID, sheet string, col, row int, value string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	actor := h.actors[id]
	if actor == nil {
		return
	}

	coord := cellCoord(sheet, col, row)
	before := h.currentInputLocked(sheet, col, row)
	prevAuthor := h.lastAuthor[coord]

	changes := h.wb.SetCell(sheet, col, row, value)
	if changes == nil {
		h.sendToLocked(id, Envelope{Type: MsgError, Message: "cell out of range"})
		return
	}
	h.version++

	h.lastAuthor[coord] = id
	h.undo[id] = append(h.undo[id], &editEntry{
		sheet: sheet, col: col, row: row, before: before, after: value,
	})
	h.redo[id] = nil

	h.persistLocked(sheet, col, row, value)

	msg := Envelope{
		Type: MsgChanges, MsgID: msgID, Version: h.version,
		Sheet: sheet, Changes: changesFromEngine(changes),
		Author: actor,
	}
	h.broadcastLocked(msg)
	h.ackLocked(id, msgID)

	// LWW conflict: tell the previous author they were overwritten.
	if prevAuthor != "" && prevAuthor != id {
		h.sendToLocked(prevAuthor, Envelope{
			Type: MsgOverwritten, Sheet: sheet, Col: col, Row: row,
			Winner: actor.Name, OldValue: before, NewValue: value,
		})
	}
	h.undoStateLocked(id)
}

// Undo reverts one of *this* user's own edits. The undo entry is applied as
// a plain engine mutation, so every dependent formula — including formula
// cells owned by someone else — recomputes through the normal dependency
// graph, but no entry is pushed onto any undo stack.
//
// If the cell has been edited again by anyone since this entry, reverting it
// would clobber newer work; such an entry is skipped (popped silently).
func (h *Hub) Undo(id, msgID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.undo[id]) == 0 {
		return
	}
	entry := h.undo[id][len(h.undo[id])-1]
	h.undo[id] = h.undo[id][:len(h.undo[id])-1]

	coord := cellCoord(entry.sheet, entry.col, entry.row)
	if h.currentInputLocked(entry.sheet, entry.col, entry.row) != entry.after {
		// The cell moved on; this entry can't be cleanly reverted.
		h.undoStateLocked(id)
		return
	}
	h.applyHistoryLocked(id, msgID, entry, entry.before)
	h.redo[id] = append(h.redo[id], entry)
	// The reverted value predates tracked authorship; drop the marker so a
	// future overwrite doesn't notify a stale "winner".
	delete(h.lastAuthor, coord)
	h.undoStateLocked(id)
}

// Redo reapplies an undone own edit, again without touching any undo stack.
func (h *Hub) Redo(id, msgID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.redo[id]) == 0 {
		return
	}
	entry := h.redo[id][len(h.redo[id])-1]
	h.redo[id] = h.redo[id][:len(h.redo[id])-1]
	h.applyHistoryLocked(id, msgID, entry, entry.after)
	h.undo[id] = append(h.undo[id], entry)
	h.undoStateLocked(id)
}

// applyHistoryLocked writes value for entry and broadcasts the recomputed
// changes without recording history and without an overwrite notice.
func (h *Hub) applyHistoryLocked(id, msgID string, entry *editEntry, value string) {
	actor := h.actors[id]
	changes := h.wb.SetCell(entry.sheet, entry.col, entry.row, value)
	if changes == nil {
		return
	}
	h.version++
	h.persistLocked(entry.sheet, entry.col, entry.row, value)
	msg := Envelope{
		Type: MsgChanges, MsgID: msgID, Version: h.version,
		Sheet: entry.sheet, Changes: changesFromEngine(changes), Author: actor,
	}
	h.broadcastLocked(msg)
}

// ApplyStructure handles row/column insertion and deletion. It is a shared
// structural event: everyone gets the new grid snapshot region as changes.
// Structural edits are deliberately not part of per-user undo (they rewrite
// many users' cells); they still trigger a full dependency-driven recalc.
func (h *Hub) ApplyStructure(id, msgID, kind, sheet string, index int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	var changes []engine.CellChange
	switch kind {
	case "insertRow":
		changes = h.wb.InsertRow(sheet, index)
	case "deleteRow":
		changes = h.wb.DeleteRow(sheet, index)
	case "insertCol":
		changes = h.wb.InsertCol(sheet, index)
	case "deleteCol":
		changes = h.wb.DeleteCol(sheet, index)
	default:
		return false
	}
	if changes == nil {
		return false
	}
	h.version++
	// Structural edits move/delete many cells at once; rewrite the whole
	// persisted workbook from the current in-memory state.
	if h.store != nil {
		_ = h.store.ReplaceWorkbook(h.version)
	}
	rows, cols := 0, 0
	if s := h.wb.Sheet(sheet); s != nil {
		rows, cols = s.Rows, s.Cols
	}
	done := Envelope{
		Type: MsgStructureDone, MsgID: msgID, Version: h.version,
		Kind: kind, Sheet: sheet, Index: index,
		Rows: rows, Cols: cols,
		Changes: changesFromEngine(changes),
	}
	h.broadcastLocked(done)
	return true
}

func (h *Hub) currentInputLocked(sheet string, col, row int) string {
	return h.wb.InputAt(sheet, col, row)
}

func (h *Hub) broadcastLocked(e Envelope) {
	for _, c := range h.clients {
		c.Send(e)
	}
}

func (h *Hub) sendToLocked(id string, e Envelope) {
	if c := h.clients[id]; c != nil {
		c.Send(e)
	}
}

func (h *Hub) ackLocked(id, msgID string) {
	h.sendToLocked(id, Envelope{Type: MsgAck, MsgID: msgID, Version: h.version})
}

func (h *Hub) undoStateLocked(id string) {
	h.sendToLocked(id, Envelope{
		Type:    MsgUndoState,
		CanUndo: len(h.undo[id]) > 0,
		CanRedo: len(h.redo[id]) > 0,
	})
}

func (h *Hub) persistLocked(sheet string, col, row int, value string) {
	if h.store != nil {
		_ = h.store.WriteCell(sheet, col, row, value)
		_ = h.store.WriteVersion(h.version)
	}
}
