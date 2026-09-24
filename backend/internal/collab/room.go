package collab

// This file contains the room mutation logic. Everything runs under h.mu so
// that concurrent edits are serialized; the engine itself is only touched
// here.

import "collabsheet/internal/engine"

// Connect registers a reconnecting or new client and returns its Client
// handle plus a full snapshot. An existing client with the same ID (a
// reconnect) is replaced and its buffered channel closed.
func (h *Hub) Connect(id, name string) (*Client, Envelope) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if old := h.clients[id]; old != nil {
		old.close()
	}
	if name == "" {
		if a := h.actors[id]; a != nil {
			name = a.Name
		} else {
			name = "用户-" + id[:min(4, len(id))]
		}
	}
	color := h.colors.take(id)
	cl := newClient(id, name, color)
	h.clients[id] = cl
	h.actors[id] = &Actor{ID: id, Name: name, Color: color}

	u := h.pres[id]
	if u == nil {
		u = &UserState{ID: id}
		h.pres[id] = u
	}
	u.Name, u.Color = name, color

	snap := h.snapshotLocked(id)
	h.broadcastPresenceLocked()
	return cl, snap
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Disconnect removes a client; cursor presence disappears but the workbook
// and undo history stay (the same client can reconnect).
func (h *Hub) Disconnect(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c := h.clients[id]; c != nil {
		c.close()
		delete(h.clients, id)
	}
	if u := h.pres[id]; u != nil {
		u.HasCursor = false
		u.Editing = false
	}
	h.broadcastPresenceLocked()
}

func (h *Hub) snapshotLocked(forID string) Envelope {
	snaps := h.wb.Snapshot()
	sheets := make([]SheetState, len(snaps))
	for i, s := range snaps {
		sheets[i] = SheetState{
			Name: s.Name, Rows: s.Rows, Cols: s.Cols,
			Cells: convertCells(s.Cells),
		}
	}
	users := h.presenceListLocked()
	return Envelope{
		Type: MsgSnapshot, Version: h.version,
		Sheets: sheets, Users: users,
	}
}

func convertCells(in []engine.CellSnapshot) []CellState {
	out := make([]CellState, len(in))
	for i, c := range in {
		out[i] = CellState{
			Col: c.Col, Row: c.Row, Input: c.Input, IsFormula: c.IsFormula,
			Display: c.Display, Kind: c.Kind, Num: c.Num, Str: c.Str,
			Bool: c.Bool, Error: c.Error, Circular: c.Circular,
		}
	}
	return out
}

func changesFromEngine(in []engine.CellChange) []CellState {
	out := make([]CellState, len(in))
	for i, c := range in {
		out[i] = CellState{
			Col: c.Col, Row: c.Row, Input: c.Input, IsFormula: c.IsFormula,
			Display: c.Display, Kind: c.Kind, Num: c.Num, Str: c.Str,
			Bool: c.Bool, Error: c.Error, Circular: c.Circular,
		}
	}
	return out
}

func (h *Hub) presenceListLocked() []UserState {
	out := make([]UserState, 0, len(h.pres))
	for _, u := range h.pres {
		out = append(out, *u)
	}
	return out
}

func (h *Hub) broadcastPresenceLocked() {
	users := h.presenceListLocked()
	e := Envelope{Type: MsgPresence, Presence: users}
	for _, c := range h.clients {
		c.Send(e)
	}
}

// SetCursor updates one collaborator's selection.
func (h *Hub) SetCursor(id, sheet string, col, row int, has bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	u := h.pres[id]
	if u == nil {
		return
	}
	u.Sheet, u.Col, u.Row, u.HasCursor = sheet, col, row, has
	h.broadcastPresenceLocked()
}

// SetEditing marks the cell a collaborator is currently typing in.
func (h *Hub) SetEditing(id, sheet string, col, row int, editing bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	u := h.pres[id]
	if u == nil {
		return
	}
	if editing {
		u.EditSheet, u.EditCol, u.EditRow = sheet, col, row
	} else {
		u.EditSheet = ""
	}
	u.Editing = editing
	h.broadcastPresenceLocked()
}
