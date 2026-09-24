// Package collab implements real-time collaboration on top of the engine:
// WebSocket fan-out, live presence (cursors / who-is-editing), last-writer
// wins conflict notifications and per-user undo/redo.
package collab

// Wire message types. Client -> server messages carry `type` plus the fields
// they need; the server answers/broadcasts with the same envelope.

const (
	// Client -> server
	MsgHello     = "hello"     // {clientId, name, lastVersion}
	MsgSetCell   = "setCell"   // {msgId, sheet, col, row, value}
	MsgStructure = "structure" // {msgId, kind, sheet, index}
	MsgCursor    = "cursor"    // {sheet, col, row}
	MsgEditing   = "editing"   // {sheet, col, row, editing}
	MsgUndo      = "undo"
	MsgRedo      = "redo"
	MsgPing      = "ping"

	// Server -> client
	MsgSnapshot      = "snapshot" // full workbook + version + presence
	MsgChanges       = "changes"  // broadcast cell recomputations
	MsgStructureDone = "structureDone"
	MsgPresence      = "presence"    // users / cursors / editing
	MsgOverwritten   = "overwritten" // LWW conflict notice to the loser
	MsgUndoState     = "undoState"   // {canUndo, canRedo}
	MsgAck           = "ack"
	MsgError         = "error"
)

// Envelope is the single WebSocket payload shape.
type Envelope struct {
	Type string `json:"type"`

	// hello
	ClientID    string `json:"clientId,omitempty"`
	Name        string `json:"name,omitempty"`
	LastVersion int64  `json:"lastVersion,omitempty"`

	// setCell / changes
	MsgID string `json:"msgId,omitempty"`
	Sheet string `json:"sheet,omitempty"`
	Col   int    `json:"col,omitempty"`
	Row   int    `json:"row,omitempty"`
	Value string `json:"value,omitempty"`

	// structure
	Kind  string `json:"kind,omitempty"` // insertRow,deleteRow,insertCol,deleteCol
	Index int    `json:"index,omitempty"`
	Rows  int    `json:"rows,omitempty"`
	Cols  int    `json:"cols,omitempty"`

	// server payloads
	Version  int64        `json:"version,omitempty"`
	Sheets   []SheetState `json:"sheets,omitempty"`
	Changes  []CellState  `json:"changes,omitempty"`
	Users    []UserState  `json:"users,omitempty"`
	Presence []UserState  `json:"presence,omitempty"`
	Author   *Actor       `json:"author,omitempty"`

	// overwritten notice
	Cell     string `json:"cell,omitempty"`
	Winner   string `json:"winner,omitempty"`
	OldValue string `json:"oldValue,omitempty"`
	NewValue string `json:"newValue,omitempty"`

	CanUndo bool   `json:"canUndo,omitempty"`
	CanRedo bool   `json:"canRedo,omitempty"`
	Message string `json:"message,omitempty"`
	Editing bool   `json:"editing,omitempty"`
}

// Actor identifies who authored a mutation.
type Actor struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// CellState is one computed cell on the wire.
type CellState struct {
	Col       int     `json:"col"`
	Row       int     `json:"row"`
	Input     string  `json:"input"`
	IsFormula bool    `json:"isFormula"`
	Display   string  `json:"display"`
	Kind      string  `json:"kind"`
	Num       float64 `json:"num,omitempty"`
	Str       string  `json:"str,omitempty"`
	Bool      bool    `json:"bool,omitempty"`
	Error     string  `json:"error,omitempty"`
	Circular  bool    `json:"circular,omitempty"`
}

// SheetState is one sheet in a snapshot / structure update.
type SheetState struct {
	Name  string      `json:"name"`
	Rows  int         `json:"rows"`
	Cols  int         `json:"cols"`
	Cells []CellState `json:"cells"`
}

// UserState is a collaborator's live presence.
type UserState struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Sheet     string `json:"sheet,omitempty"`
	Col       int    `json:"col"`
	Row       int    `json:"row"`
	HasCursor bool   `json:"hasCursor"`
	EditSheet string `json:"editSheet,omitempty"`
	EditCol   int    `json:"editCol"`
	EditRow   int    `json:"editRow"`
	Editing   bool   `json:"editing"`
}
