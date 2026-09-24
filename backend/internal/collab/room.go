// Package collab implements the shared editing room: operation application,
// last-writer-wins conflict notices, per-user undo/redo stacks, presence
// (remote cursors) and broadcast fan-out over WebSocket sinks.
package collab

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"

	"collabsheet/internal/engine"
	"collabsheet/internal/workbook"
)

// Client is one connected participant; Send receives JSON-encoded S2C messages.
type Client interface {
	ID() string
	Send(payload []byte)
}

// Room is a single shared workbook with its live participants.
type Room struct {
	mu      sync.Mutex
	eng     *engine.Engine
	clients map[string]*session
	version int64

	// lastWriter[sheet|A1] = user id of the most recent edit, used to detect
	// last-writer-wins conflicts.
	lastWriter map[string]string

	undo map[string][]*undoEntry
	redo map[string][]*undoEntry

	persist Persister
}

type session struct {
	id    string
	name  string
	color string
	cell  string // selected cell address, empty if none
	sheet string
	c     Client
}

// Persister optionally snapshots the workbook after every mutation.
type Persister interface {
	Save(wb *workbook.Workbook) error
}

// NewRoom wraps an engine-backed workbook.
func NewRoom(eng *engine.Engine, p Persister) *Room {
	return &Room{
		eng:        eng,
		clients:    map[string]*session{},
		lastWriter: map[string]string{},
		undo:       map[string][]*undoEntry{},
		redo:       map[string][]*undoEntry{},
		persist:    p,
	}
}

// Engine exposes the compute engine.
func (r *Room) Engine() *engine.Engine { return r.eng }

// Version returns the current monotonically increasing state version.
func (r *Room) Version() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.version
}

var colors = []string{
	"#e6194B", "#3cb44b", "#4363d8", "#f58231", "#911eb4",
	"#42d4f4", "#f032e6", "#9A6324", "#808000", "#469990",
}

// NewClientID generates an opaque client identifier.
func NewClientID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Connect registers a participant and returns other participants for presence.
func (r *Room) Connect(c Client, name string) Presence {
	r.mu.Lock()
	defer r.mu.Unlock()
	color := colors[len(r.clients)%len(colors)]
	if name == "" {
		name = "用户" + shortID(c.ID())
	}
	s := &session{id: c.ID(), name: name, color: color, c: c}
	if len(r.eng.Workbook().Sheets) > 0 {
		s.sheet = r.eng.Workbook().Sheets[0].Name
	}
	r.clients[c.ID()] = s
	if _, ok := r.undo[c.ID()]; !ok {
		r.undo[c.ID()] = nil
		r.redo[c.ID()] = nil
	}
	// Announce the new participant to OTHER clients only; the joining client
	// receives its presence inside the snapshot reply that follows.
	for id, other := range r.clients {
		if id == c.ID() {
			continue
		}
		send(other.c, PresenceMsg{Type: "presence", Presence: r.buildPresenceLocked(id)})
	}
	return r.buildPresenceLocked(c.ID())
}

func shortID(id string) string {
	if len(id) > 4 {
		return id[:4]
	}
	return id
}

// Disconnect removes a participant.
func (r *Room) Disconnect(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, id)
	r.broadcastPresenceLocked()
}

// Snapshot sends the full workbook + values to one client (used on connect and
// after reconnect).
func (r *Room) Snapshot(c Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	msg := SnapshotMsg{
		Type:     "snapshot",
		Version:  r.version,
		Presence: r.buildPresenceLocked(c.ID()),
	}
	msg.Workbook = describeWorkbook(r.eng)
	send(c, msg)
}

// ---- cell editing ----

// ApplyEdit applies a cell edit for userID and broadcasts the resulting
// recalculated patch. If the previous writer was a different user, that user
// receives a conflict notice. Returns the computed patches.
func (r *Room) ApplyEdit(userID, sheetName, addr, raw string) []CellValue {
	r.mu.Lock()
	defer r.mu.Unlock()
	sess := r.clients[userID]
	if sess == nil {
		return nil
	}
	col, row, ok := validateAddr(r.eng.Workbook(), sheetName, addr)
	if !ok {
		return nil
	}

	prevRaw := ""
	if old := r.eng.Workbook().SheetByName(sheetName).Get(col, row); old != nil {
		prevRaw = old.Raw
	}
	// A no-op commit (Enter without changing anything) must not steal the
	// last-writer slot or produce a spurious conflict notice.
	if prevRaw == raw {
		return nil
	}
	prevWriter := r.lastWriter[cellKey(sheetName, addr)]

	sheet := r.eng.Workbook().SheetByName(sheetName)
	sheet.SetRaw(col, row, raw)
	r.version++
	r.eng.RecalcAfterEdit(sheetName, col, row)

	r.lastWriter[cellKey(sheetName, addr)] = userID

	// Per-user undo entry (even formula edits). Formula cells that merely
	// *recompute* because someone else changed an input never enter any stack.
	r.undo[userID] = append(r.undo[userID], &undoEntry{
		kind:   undoEdit,
		sheet:  sheetName,
		addr:   addr,
		prev:   prevRaw,
		post:   raw,
		writer: prevWriter,
	})
	r.redo[userID] = nil

	patches := r.collectPatchesLocked(r.eng, affectedPatchKeys(sheetName, addr, r.eng))

	// Broadcast the edit + recomputed values.
	r.broadcastLocked(PatchMsg{
		Type:    "patch",
		Version: r.version,
		By:      userID,
		Cells:   patches,
	})

	// Notify the overwritten user (last-writer-wins).
	if prevWriter != "" && prevWriter != userID {
		if loser := r.clients[prevWriter]; loser != nil {
			send(loser.c, ConflictMsg{
				Type:   "conflict",
				Sheet:  sheetName,
				Cell:   addr,
				By:     sess.name,
				ByName: sess.name,
				Your:   prevRaw,
				Theirs: raw,
			})
		}
	}

	r.persistLocked()
	r.sendStackStateLocked(userID)
	return patches
}

// affectedPatchKeys returns the edited cell plus every recomputed dependent
// reported by the engine for this turn.
func affectedPatchKeys(sheetName, addr string, eng *engine.Engine) []engine.Key {
	col, row, err := workbook.ParseAddr(addr)
	if err != nil {
		return nil
	}
	keys := []engine.Key{{Sheet: sheetName, Col: col, Row: row}}
	// Walk reverse deps transitively.
	return append(keys, transitiveDependents(eng, engine.Key{Sheet: sheetName, Col: col, Row: row})...)
}

func transitiveDependents(eng *engine.Engine, start engine.Key) []engine.Key {
	// Engine does not expose rdeps; instead recompute all formula values by
	// scanning: the engine already returned values via Result, so collab
	// gathers the whole affected set via the engine's exported graph helper.
	return eng.Dependents(start)
}

// ---- structure ops ----

// StructOp is an insert/delete row/col request.
type StructOp struct {
	Kind  string `json:"kind"` // insert_row|delete_row|insert_col|delete_col
	Sheet string `json:"sheet"`
	Index int    `json:"index"` // 0-based
	Count int    `json:"count"`
}

// ApplyStruct performs a structural change, records its inverse for undo and
// sends everyone a fresh snapshot (addresses shift wholesale).
func (r *Room) ApplyStruct(userID string, op StructOp) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := applyStruct(r.eng.Workbook(), op); err != nil {
		return false
	}
	r.version++
	r.eng.RecalcAll()
	// Writer map keys are stale after a shift; reset to keep conflict notices
	// honest after structure changes.
	r.lastWriter = map[string]string{}

	inv := inverseOp(op)
	r.undo[userID] = append(r.undo[userID], &undoEntry{kind: undoStruct, forward: op, inverse: inv})
	r.redo[userID] = nil

	r.broadcastSnapshotLocked()
	r.persistLocked()
	r.sendStackStateLocked(userID)
	return true
}

func applyStruct(wb *workbook.Workbook, op StructOp) error {
	n := op.Count
	if n <= 0 {
		n = 1
	}
	switch op.Kind {
	case "insert_row":
		return wb.InsertRows(op.Sheet, op.Index, n)
	case "delete_row":
		return wb.DeleteRows(op.Sheet, op.Index, n)
	case "insert_col":
		return wb.InsertCols(op.Sheet, op.Index, n)
	case "delete_col":
		return wb.DeleteCols(op.Sheet, op.Index, n)
	}
	return errUnknownOp
}

// ---- undo / redo ----

const (
	undoEdit   = "edit"
	undoStruct = "struct"
)

type undoEntry struct {
	kind   string
	sheet  string
	addr   string
	prev   string
	post   string
	writer string // writer before the edit, restored on undo

	forward StructOp
	inverse StructOp
}

// Undo reverses the user's own latest action. Key requirement: when an input
// cell reverts, dependent formula cells recompute and roll back their values,
// but those formula cells never enter the undoing or anyone else's stack.
func (r *Room) Undo(userID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	stack := r.undo[userID]
	if len(stack) == 0 {
		r.sendUndoResultLocked(userID, false)
		return false
	}
	entry := stack[len(stack)-1]
	r.undo[userID] = stack[:len(stack)-1]

	switch entry.kind {
	case undoEdit:
		col, row, ok := validateAddr(r.eng.Workbook(), entry.sheet, entry.addr)
		if !ok {
			r.sendUndoResultLocked(userID, false)
			return false
		}
		r.eng.Workbook().SheetByName(entry.sheet).SetRaw(col, row, entry.prev)
		r.version++
		r.eng.RecalcAfterEdit(entry.sheet, col, row)
		r.lastWriter[cellKey(entry.sheet, entry.addr)] = entry.writer

		r.redo[userID] = append(r.redo[userID], entry)

		patches := r.collectPatchesLocked(r.eng, affectedPatchKeys(entry.sheet, entry.addr, r.eng))
		r.broadcastLocked(PatchMsg{Type: "patch", Version: r.version, By: userID, Cells: patches, Cause: "undo"})
	case undoStruct:
		if err := applyStruct(r.eng.Workbook(), entry.inverse); err != nil {
			r.sendUndoResultLocked(userID, false)
			return false
		}
		r.version++
		r.eng.RecalcAll()
		r.lastWriter = map[string]string{}
		r.redo[userID] = append(r.redo[userID], entry)
		r.broadcastSnapshotLocked()
	}
	r.persistLocked()
	r.sendUndoResultLocked(userID, true)
	return true
}

// Redo replays the user's own last undone action.
func (r *Room) Redo(userID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	stack := r.redo[userID]
	if len(stack) == 0 {
		r.sendRedoResultLocked(userID, false)
		return false
	}
	entry := stack[len(stack)-1]
	r.redo[userID] = stack[:len(stack)-1]

	switch entry.kind {
	case undoEdit:
		col, row, ok := validateAddr(r.eng.Workbook(), entry.sheet, entry.addr)
		if !ok {
			r.sendRedoResultLocked(userID, false)
			return false
		}
		r.eng.Workbook().SheetByName(entry.sheet).SetRaw(col, row, entry.post)
		r.version++
		r.eng.RecalcAfterEdit(entry.sheet, col, row)
		r.lastWriter[cellKey(entry.sheet, entry.addr)] = userID
		r.undo[userID] = append(r.undo[userID], entry)
		patches := r.collectPatchesLocked(r.eng, affectedPatchKeys(entry.sheet, entry.addr, r.eng))
		r.broadcastLocked(PatchMsg{Type: "patch", Version: r.version, By: userID, Cells: patches, Cause: "redo"})
	case undoStruct:
		if err := applyStruct(r.eng.Workbook(), entry.forward); err != nil {
			r.sendRedoResultLocked(userID, false)
			return false
		}
		r.version++
		r.eng.RecalcAll()
		r.lastWriter = map[string]string{}
		r.undo[userID] = append(r.undo[userID], entry)
		r.broadcastSnapshotLocked()
	}
	r.persistLocked()
	r.sendRedoResultLocked(userID, true)
	return true
}

func inverseOp(op StructOp) StructOp {
	inv := StructOp{Sheet: op.Sheet, Index: op.Index, Count: op.Count}
	switch op.Kind {
	case "insert_row":
		inv.Kind = "delete_row"
	case "delete_row":
		inv.Kind = "insert_row"
	case "insert_col":
		inv.Kind = "delete_col"
	case "delete_col":
		inv.Kind = "insert_col"
	}
	return inv
}

// ---- cursor presence ----

// MoveCursor records a participant's selection and broadcasts the update.
func (r *Room) MoveCursor(userID, sheetName, addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sess := r.clients[userID]
	if sess == nil {
		return
	}
	sess.sheet = sheetName
	sess.cell = addr
	r.broadcastPresenceLocked()
}

// ---- helpers ----

func cellKey(sheet, addr string) string { return sheet + "|" + addr }

func validateAddr(wb *workbook.Workbook, sheetName, addr string) (int, int, bool) {
	sh := wb.SheetByName(sheetName)
	if sh == nil {
		return 0, 0, false
	}
	col, row, err := workbook.ParseAddr(addr)
	if err != nil || col < 0 || col >= sh.Cols || row < 0 || row >= sh.Rows {
		return 0, 0, false
	}
	return col, row, true
}

func (r *Room) persistLocked() {
	if r.persist != nil {
		_ = r.persist.Save(r.eng.Workbook())
	}
}

// sortedSessions returns sessions ordered by id for deterministic presence.
func sortedSessions(m map[string]*session) []*session {
	out := make([]*session, 0, len(m))
	for _, s := range m {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}
