package collab

import (
	"testing"

	"collabsheet/internal/engine"
)

func newTestHub(t *testing.T) *Hub {
	t.Helper()
	wb := engine.NewWorkbook("t")
	wb.AddSheet("S1", 100, 26)
	wb.AddSheet("S2", 100, 26)
	return NewHub(wb, nil)
}

// drain empties a client's queued messages.
func drain(c *Client) []Envelope {
	var out []Envelope
	for {
		select {
		case e := <-c.send:
			out = append(out, e)
		default:
			return out
		}
	}
}

func byType(es []Envelope, typ string) []Envelope {
	var out []Envelope
	for _, e := range es {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

// TestLastWriterWins: two clients write the same cell; the earlier writer
// must receive an explicit overwritten notice, and both must end up seeing
// the later value.
func TestLastWriterWins(t *testing.T) {
	h := newTestHub(t)
	a, _ := h.Connect("aaaa", "甲")
	b, _ := h.Connect("bbbb", "乙")
	drain(a)
	drain(b)

	h.ApplyCellEdit("aaaa", "m1", "S1", 0, 0, "10")
	drain(b) // b observes the first write
	drain(a)

	h.ApplyCellEdit("bbbb", "m2", "S1", 0, 0, "20")
	aMsg := drain(a)
	bMsg := drain(b)

	// 甲 gets one overwritten notice naming 乙.
	ow := byType(aMsg, MsgOverwritten)
	if len(ow) != 1 {
		t.Fatalf("甲 expected 1 overwritten, got %d (%v)", len(ow), aMsg)
	}
	if ow[0].Winner != "乙" || ow[0].NewValue != "20" || ow[0].OldValue != "10" {
		t.Errorf("notice = %+v", ow[0])
	}
	// 乙 does not get a notice about its own write.
	if len(byType(bMsg, MsgOverwritten)) != 0 {
		t.Errorf("winner should not be notified")
	}
	if got := h.wb.InputAt("S1", 0, 0); got != "20" {
		t.Errorf("final value = %q want 20", got)
	}
}

// TestCrossUserUndoRollback is the required boundary case:
//
//	甲 edits A1; 乙 edits formula B1 =A1 (based on the new A1); 甲 undoes.
//
// B1's computed result must roll back with the dependency graph, but the
// recomputation must NOT place anything on 乙's undo stack.
func TestCrossUserUndoRollback(t *testing.T) {
	h := newTestHub(t)
	a, _ := h.Connect("aaaa", "甲")
	b, _ := h.Connect("bbbb", "乙")
	drain(a)
	drain(b)

	// 甲: A1 = 100
	h.ApplyCellEdit("aaaa", "m1", "S1", 0, 0, "100")
	drain(a)
	drain(b)

	// 乙: B1 = A1*2  -> 200
	h.ApplyCellEdit("bbbb", "m2", "S1", 1, 0, "=A1*2")
	drain(a)
	drain(b)
	if got := h.wb.InputAt("S1", 1, 0); got != "=A1*2" {
		t.Fatalf("B1 formula = %q", got)
	}
	if got := cellDisplay(h, "S1", 1, 0); got != "200" {
		t.Fatalf("B1 = %q want 200", got)
	}

	// 甲 undoes her A1 edit: A1 -> blank, B1 must follow to 0.
	h.Undo("aaaa", "u1")
	if got := h.wb.InputAt("S1", 0, 0); got != "" {
		t.Errorf("A1 after undo = %q want blank", got)
	}
	if got := cellDisplay(h, "S1", 1, 0); got != "0" {
		t.Errorf("B1 did not roll back with dependency: %q want 0", got)
	}

	// Crucially, 乙's undo stack must contain only B1 (one entry), not the
	// recomputed B1 caused by 甲's undo.
	if len(h.undo["bbbb"]) != 1 {
		t.Errorf("乙 undo stack depth = %d want 1 (rollback must not enter it)",
			len(h.undo["bbbb"]))
	}
	if len(h.undo["aaaa"]) != 0 {
		t.Errorf("甲 undo stack depth = %d want 0", len(h.undo["aaaa"]))
	}

	// 乙 can still undo his own B1 edit independently.
	h.Undo("bbbb", "u2")
	if got := h.wb.InputAt("S1", 1, 0); got != "" {
		t.Errorf("B1 after 乙 undo = %q want blank", got)
	}
}

// TestUndoRedoIsolation: one user's undo cannot revert another's edit.
func TestUndoRedoIsolation(t *testing.T) {
	h := newTestHub(t)
	a, _ := h.Connect("aaaa", "甲")
	b, _ := h.Connect("bbbb", "乙")
	drain(a)
	drain(b)

	h.ApplyCellEdit("aaaa", "m1", "S1", 0, 0, "甲")
	h.ApplyCellEdit("bbbb", "m2", "S1", 2, 0, "乙")
	drain(a)
	drain(b)

	// 乙 pressing undo does nothing to 甲's cell.
	h.Undo("bbbb", "u")
	if got := h.wb.InputAt("S1", 0, 0); got != "甲" {
		t.Errorf("甲's cell changed by 乙 undo: %q", got)
	}
	if got := h.wb.InputAt("S1", 2, 0); got != "" {
		t.Errorf("乙's cell = %q want reverted", got)
	}

	// Redo restores it.
	h.Redo("bbbb", "r")
	if got := h.wb.InputAt("S1", 2, 0); got != "乙" {
		t.Errorf("redo = %q want 乙", got)
	}
}

// TestUndoSkippedWhenCellMovedOn: if a third party edited the same cell after
// the entry, the stale undo entry is skipped rather than clobbering.
func TestUndoSkippedWhenCellMovedOn(t *testing.T) {
	h := newTestHub(t)
	a, _ := h.Connect("aaaa", "甲")
	b, _ := h.Connect("bbbb", "乙")
	drain(a)
	drain(b)

	h.ApplyCellEdit("aaaa", "m1", "S1", 0, 0, "1")
	drain(a)
	drain(b)
	h.ApplyCellEdit("bbbb", "m2", "S1", 0, 0, "2")
	drain(a)
	drain(b)

	h.Undo("aaaa", "u") // A1 is 2 now, not the 1 she wrote -> skip
	if got := h.wb.InputAt("S1", 0, 0); got != "2" {
		t.Errorf("stale undo clobbered newer edit: %q want 2", got)
	}
}

// TestReconnectResyncs: a client that disappears, misses edits and reconnects
// receives a snapshot containing the changes.
func TestReconnectResyncs(t *testing.T) {
	h := newTestHub(t)
	a, _ := h.Connect("aaaa", "甲")
	drain(a)
	h.Disconnect("aaaa")

	// 乙 edits while 甲 is offline.
	b, _ := h.Connect("bbbb", "乙")
	drain(b)
	h.ApplyCellEdit("bbbb", "m1", "S1", 0, 0, "314")
	drain(b)

	// 甲 reconnects with the same id.
	a2, snap := h.Connect("aaaa", "")
	if snap.Type != MsgSnapshot {
		t.Fatalf("reconnect envelope type = %q", snap.Type)
	}
	found := false
	for _, s := range snap.Sheets {
		if s.Name != "S1" {
			continue
		}
		for _, c := range s.Cells {
			if c.Col == 0 && c.Row == 0 && c.Input == "314" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("reconnect snapshot missing offline edit")
	}
	if a == a2 {
		t.Errorf("reconnect should produce a fresh client channel")
	}
}

// TestStructureBroadcast: a row insert is delivered to everyone and shifts
// references (engine-level correctness is covered in package engine).
func TestStructureBroadcast(t *testing.T) {
	h := newTestHub(t)
	a, _ := h.Connect("aaaa", "甲")
	b, _ := h.Connect("bbbb", "乙")
	drain(a)
	drain(b)

	h.ApplyCellEdit("aaaa", "m1", "S1", 0, 2, "9")
	drain(a)
	drain(b)
	if !h.ApplyStructure("aaaa", "m2", "insertRow", "S1", 2) {
		t.Fatal("insertRow rejected")
	}
	gotB := byType(drain(b), MsgStructureDone)
	if len(gotB) != 1 {
		t.Fatalf("乙 expected structureDone, got %d", len(gotB))
	}
	if got := h.wb.InputAt("S1", 0, 3); got != "9" {
		t.Errorf("cell did not move: %q", got)
	}
}

func cellDisplay(h *Hub, sheet string, col, row int) string {
	for _, s := range h.wb.Snapshot() {
		if s.Name != sheet {
			continue
		}
		for _, c := range s.Cells {
			if c.Col == col && c.Row == row {
				return c.Display
			}
		}
	}
	return ""
}
