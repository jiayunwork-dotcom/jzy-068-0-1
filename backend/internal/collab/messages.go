package collab

// Client -> server messages (raw JSON dispatch by Type).
type (
	// HelloMsg is sent right after the (re)connected socket opens.
	HelloMsg struct {
		Type string `json:"type"` // hello
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	EditMsg struct {
		Type  string `json:"type"` // edit
		Sheet string `json:"sheet"`
		Cell  string `json:"cell"`
		Raw   string `json:"raw"`
	}

	StructMsg struct {
		Type string `json:"type"` // struct
		Op   StructOp
	}

	UndoMsg struct {
		Type string `json:"type"` // undo
	}

	RedoMsg struct {
		Type string `json:"type"` // redo
	}

	CursorMsg struct {
		Type  string `json:"type"` // cursor
		Sheet string `json:"sheet"`
		Cell  string `json:"cell"`
	}
)

// Server -> client messages.
type (
	CellValue struct {
		Sheet string `json:"sheet"`
		Cell  string `json:"cell"`
		Raw   string `json:"raw"`
		Value string `json:"value"`
		Kind  string `json:"kind"`
		Cycle bool   `json:"cycle"`
		Error bool   `json:"error"`
	}

	PatchMsg struct {
		Type    string      `json:"type"` // patch
		Version int64       `json:"version"`
		By      string      `json:"by"`
		Cause   string      `json:"cause,omitempty"`
		Cells   []CellValue `json:"cells"`
	}

	ConflictMsg struct {
		Type   string `json:"type"` // conflict
		Sheet  string `json:"sheet"`
		Cell   string `json:"cell"`
		By     string `json:"by"`
		ByName string `json:"byName"`
		Your   string `json:"your"`
		Theirs string `json:"theirs"`
	}

	UndoResultMsg struct {
		Type    string `json:"type"` // undo_result / redo_result
		OK      bool   `json:"ok"`
		CanUndo bool   `json:"canUndo"`
		CanRedo bool   `json:"canRedo"`
	}

	// StackStateMsg reports the acting user's own undo/redo availability after
	// any action (edit, struct, undo, redo).
	StackStateMsg struct {
		Type    string `json:"type"` // stack_state
		CanUndo bool   `json:"canUndo"`
		CanRedo bool   `json:"canRedo"`
	}

	Peer struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
		Sheet string `json:"sheet"`
		Cell  string `json:"cell"`
	}

	Presence struct {
		Me    string `json:"me"`
		Peers []Peer `json:"peers"`
	}

	PresenceMsg struct {
		Type     string   `json:"type"` // presence
		Presence Presence `json:"presence"`
	}

	SheetData struct {
		Name  string               `json:"name"`
		Rows  int                  `json:"rows"`
		Cols  int                  `json:"cols"`
		Cells map[string]CellValue `json:"cells"`
	}

	WorkbookData struct {
		Sheets []SheetData `json:"sheets"`
	}

	SnapshotMsg struct {
		Type     string       `json:"type"` // snapshot
		Version  int64        `json:"version"`
		Workbook WorkbookData `json:"workbook"`
		Presence Presence     `json:"presence"`
	}

	ErrorMsg struct {
		Type string `json:"type"` // error
		Msg  string `json:"msg"`
	}
)
