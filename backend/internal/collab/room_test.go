package collab

import (
	"encoding/json"
	"sync"
	"testing"

	"collabsheet/internal/engine"
	"collabsheet/internal/workbook"
)

// fakeClient records every outbound JSON message in memory.
type fakeClient struct {
	id   string
	mu   sync.Mutex
	sent [][]byte
}

func newFake(id string) *fakeClient { return &fakeClient{id: id} }

func (f *fakeClient) ID() string { return f.id }
func (f *fakeClient) Send(b []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, b)
}

func (f *fakeClient) messagesOf(typ string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, b := range f.sent {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil && m["type"] == typ {
			out = append(out, m)
		}
	}
	return out
}

func (f *fakeClient) lastPatchCells() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	patches := map[string]string{}
	for _, b := range f.sent {
		var m map[string]any
		if json.Unmarshal(b, &m) != nil || m["type"] != "patch" {
			continue
		}
		if cells, ok := m["cells"].([]any); ok {
			for _, ci := range cells {
				c := ci.(map[string]any)
				patches[c["cell"].(string)] = c["value"].(string)
			}
		}
	}
	return patches
}

func setupRoom(t *testing.T) (*Room, *fakeClient, *fakeClient) {
	t.Helper()
	wb := workbook.New()
	s := workbook.NewSheet("S1", 100)
	wb.Sheets = []*workbook.Sheet{s}
	eng := engine.New(wb)
	room := NewRoom(eng, nil)
	a := newFake("userA")
	b := newFake("userB")
	room.Connect(a, "甲")
	room.Connect(b, "乙")
	a.sent, b.sent = nil, nil // ignore connect chatter
	return room, a, b
}

// TestLastWriterWins: A and B write the same cell; the later write wins and
// the earlier writer receives an explicit conflict notice.
func TestLastWriterWins(t *testing.T) {
	room, a, b := setupRoom(t)

	room.ApplyEdit("userA", "S1", "A1", "10")
	a.sent, b.sent = nil, nil

	room.ApplyEdit("userB", "S1", "A1", "20")

	if v := room.Engine().Result("S1", 0, 0).Value.Num; v != 20 {
		t.Fatalf("A1 should be 20 (last writer wins), got %v", v)
	}
	conflicts := a.messagesOf("conflict")
	if len(conflicts) == 0 {
		t.Fatal("overwritten user A must receive a conflict notice")
	}
	c0 := conflicts[len(conflicts)-1]
	if c0["cell"] != "A1" || c0["theirs"] != "20" || c0["your"] != "10" {
		t.Fatalf("unexpected conflict payload: %+v", c0)
	}
	// B is the winner, no conflict for B.
	if len(b.messagesOf("conflict")) != 0 {
		t.Fatal("winner should not be notified of a conflict")
	}
}

// TestUndoRollsBackDependentFormulaWithoutEnteringOtherUsersStack is the core
// collaboration edge case:
//
//	A edits input A1; B edits formula B1 =A1*2; A then undoes A1.
//
// B1's displayed result must roll back with the dependency, but B1 must not
// appear in B's undo stack (B's only undoable action is B1 itself).
func TestUndoRollsBackDependentFormulaWithoutEnteringOtherUsersStack(t *testing.T) {
	room, a, b := setupRoom(t)

	// Start with A1 = 5 (A's first action), B1 formula authored by B.
	room.ApplyEdit("userA", "S1", "A1", "5")
	room.ApplyEdit("userB", "S1", "B1", "=A1*2")

	cb, rb := mustAddrColRow(t, "B1")
	if v := room.Engine().Result("S1", cb, rb).Value.Num; v != 10 {
		t.Fatalf("B1 initially = %v, want 10", v)
	}

	// A changes the input: A1 = 40 -> B1 must recompute to 80.
	room.ApplyEdit("userA", "S1", "A1", "40")
	if v := room.Engine().Result("S1", cb, rb).Value.Num; v != 80 {
		t.Fatalf("B1 after A1=40 = %v, want 80", v)
	}

	a.sent, b.sent = nil, nil

	// A undoes the A1 change: A1 returns to 5 and B1 must roll back to 10.
	if !room.Undo("userA") {
		t.Fatal("A should be able to undo")
	}
	if v := room.Engine().Result("S1", 0, 0).Value.Num; v != 5 {
		t.Fatalf("A1 after undo = %v, want 5", v)
	}
	if v := room.Engine().Result("S1", cb, rb).Value.Num; v != 10 {
		t.Fatalf("B1 must roll back to 10 via dependency, got %v", v)
	}

	// B received the rolled-back B1 value in a patch...
	cells := b.lastPatchCells()
	if cells["B1"] != "10" {
		t.Fatalf("B should see B1 roll back to 10, patch=%v", cells)
	}

	// ...but B1 recomputation never entered B's undo stack: B's undo must
	// reverse B's OWN formula edit, not A's input change.
	b.sent = nil
	if !room.Undo("userB") {
		t.Fatal("B must have exactly one undoable action (its B1 edit)")
	}
	// After B's undo, B1 is empty; A1 stays at 5 (A's undo independent).
	if v := room.Engine().Result("S1", 0, 0).Value.Num; v != 5 {
		t.Fatalf("A1 must remain 5 after B undoes its own edit, got %v", v)
	}
	if raw := rawCell(room, "S1", cb, rb); raw != "" {
		t.Fatalf("B1 should be empty after B undoes its own edit, got %q", raw)
	}

	// A's undo stack contained its two own edits; after undoing the second it
	// must still be able to undo the first (which never touched B).
	if !room.Undo("userA") {
		t.Fatal("A should still have its first A1 edit on the stack")
	}
	if v := room.Engine().Result("S1", 0, 0).Value.Display(); v != "" {
		t.Fatalf("after undoing both A edits A1 should be empty, got %v", v)
	}
	if room.Undo("userA") {
		t.Fatal("A's stack should now be empty")
	}
}

// TestIndependentUndoStacks verifies A's undo never touches B's literal edits.
func TestIndependentUndoStacks(t *testing.T) {
	room, _, _ := setupRoom(t)
	room.ApplyEdit("userA", "S1", "A1", "1")
	room.ApplyEdit("userB", "S1", "A2", "2")
	room.ApplyEdit("userA", "S1", "A1", "11")

	room.Undo("userA") // 11 -> 1
	if v := room.Engine().Result("S1", 0, 0).Value.Num; v != 1 {
		t.Fatalf("A1=%v want 1", v)
	}
	if v := room.Engine().Result("S1", 0, 1).Value.Num; v != 2 {
		t.Fatalf("A2 must stay 2, got %v", v)
	}
	if !room.Undo("userB") {
		t.Fatal("B should still be able to undo independently")
	}
	if v := room.Engine().Result("S1", 0, 1).Value.Display(); v != "" {
		t.Fatalf("A2 should be cleared by B undo, got %v", v)
	}
}

// TestRedoReappliesOwnEdit checks the redo counterpart.
func TestRedoReappliesOwnEdit(t *testing.T) {
	room, _, _ := setupRoom(t)
	room.ApplyEdit("userA", "S1", "A1", "7")
	room.Undo("userA")
	if v := room.Engine().Result("S1", 0, 0).Value.Display(); v != "" {
		t.Fatalf("A1 should be empty after undo, got %v", v)
	}
	room.Redo("userA")
	if v := room.Engine().Result("S1", 0, 0).Value.Num; v != 7 {
		t.Fatalf("A1 should be 7 after redo, got %v", v)
	}
}

func mustAddrColRow(t *testing.T, addr string) (int, int) {
	t.Helper()
	c, r, err := workbook.ParseAddr(addr)
	if err != nil {
		t.Fatal(err)
	}
	return c, r
}

func rawCell(room *Room, sheet string, col, row int) string {
	if c := room.Engine().Workbook().SheetByName(sheet).Get(col, row); c != nil {
		return c.Raw
	}
	return ""
}
