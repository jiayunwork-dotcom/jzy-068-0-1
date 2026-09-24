//go:build e2e

// Command e2e is a WebSocket end-to-end smoke client for a running server.
// It is excluded from normal builds; run it explicitly with
//
//	go run -tags e2e ./cmd/e2e
//
// It expects a fresh server on localhost:8080 and verifies: seed values,
// live broadcast, last-writer-wins conflict notice, dependency rollback on
// cross-user undo, structural row insert and reconnect snapshot.

package main

import (
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

type Env = map[string]any

type Peer struct {
	c  *websocket.Conn
	in chan Env
}

func dial(name string) *Peer {
	u := url.URL{Scheme: "ws", Host: "localhost:8080", Path: "/ws"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	p := &Peer{c: c, in: make(chan Env, 64)}
	go func() {
		for {
			var m Env
			if err := c.ReadJSON(&m); err != nil {
				return
			}
			p.in <- m
		}
	}()
	c.WriteJSON(Env{"type": "hello", "clientId": name, "name": name})
	return p
}

func (p *Peer) send(m Env) {
	if err := p.c.WriteJSON(m); err != nil {
		log.Fatal("write:", err)
	}
}

func (p *Peer) collect(d time.Duration, types ...string) []Env {
	want := map[string]bool{}
	for _, t := range types {
		want[t] = true
	}
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	var out []Env
	for {
		select {
		case m := <-p.in:
			if want[m["type"].(string)] {
				out = append(out, m)
			}
		case <-deadline.C:
			return out
		}
	}
}

func (p *Peer) drain(d time.Duration) { p.collect(d) }

func (p *Peer) snapshot() map[string]any {
	ms := p.collect(time.Second, "snapshot")
	if len(ms) == 0 {
		log.Fatal("no snapshot")
	}
	return ms[0]
}

func cellDisplay(changes []any, col, row int) string {
	for _, cc := range changes {
		c := cc.(map[string]any)
		if c["col"].(float64) == float64(col) && c["row"].(float64) == float64(row) {
			if v, ok := c["display"].(string); ok {
				return v
			}
		}
	}
	return "<missing>"
}

func main() {
	a := dial("aaaa")
	snap := a.snapshot()
	sheets := snap["sheets"].([]any)
	fmt.Println("snapshot sheets:", len(sheets))
	first := sheets[0].(map[string]any)
	sheetName := first["name"].(string)

	if got := cellDisplay(first["cells"].([]any), 4, 14); got != "超标" {
		log.Fatalf("seed E15 status = %q", got)
	}
	fmt.Println("seed E15 status: 超标")

	b := dial("bbbb")
	b.snapshot()
	a.drain(400 * time.Millisecond)
	b.drain(400 * time.Millisecond)

	// A edits A1 -> B observes a broadcast authored by aaaa.
	a.send(Env{"type": "setCell", "msgId": "1", "sheet": sheetName, "col": 0, "row": 0, "value": "42"})
	bChanges := b.collect(800*time.Millisecond, "changes")
	if len(bChanges) == 0 {
		log.Fatal("B did not receive A's change")
	}
	fmt.Println("B observed author:", bChanges[0]["author"].(map[string]any)["name"])
	a.drain(800 * time.Millisecond)

	// B edits the same cell -> A gets the overwritten notice.
	b.send(Env{"type": "setCell", "msgId": "2", "sheet": sheetName, "col": 0, "row": 0, "value": "77"})
	ow := a.collect(800*time.Millisecond, "overwritten")
	if len(ow) == 0 {
		log.Fatal("A did not receive overwritten notice")
	}
	o := ow[0]
	fmt.Println("A overwritten by:", o["winner"], "old:", o["oldValue"], "new:", o["newValue"])
	if o["newValue"] != "77" || o["oldValue"] != "42" {
		log.Fatal("bad LWW payload")
	}
	b.drain(400 * time.Millisecond)

	// Separate scenario (no conflict on this cell):
	// A writes C1 = 100; B adds D1 = C1*2 (200); A undoes her C1 edit and
	// B's formula result D1 must roll back to 0 through the dependency graph.
	a.send(Env{"type": "setCell", "msgId": "4", "sheet": sheetName, "col": 2, "row": 0, "value": "100"})
	a.drain(600 * time.Millisecond)
	b.drain(600 * time.Millisecond)
	b.send(Env{"type": "setCell", "msgId": "5", "sheet": sheetName, "col": 3, "row": 0, "value": "=C1*2"})
	b.drain(600 * time.Millisecond)
	a.drain(600 * time.Millisecond)

	a.send(Env{"type": "undo", "msgId": "u1"})
	ar := a.collect(800*time.Millisecond, "changes")
	if len(ar) == 0 {
		log.Fatal("A undo produced no changes")
	}
	if got := cellDisplay(ar[0]["changes"].([]any), 3, 0); got != "0" {
		log.Fatalf("D1 after A undo = %q want 0 (dependency rollback)", got)
	}
	fmt.Println("D1 (B's formula) rolled back via dependency after A undo: 0")
	b.drain(300 * time.Millisecond)

	// Structure: insert a row before row 3.
	a.send(Env{"type": "structure", "msgId": "s1", "kind": "insertRow", "sheet": sheetName, "index": 2})
	sd := a.collect(800*time.Millisecond, "structureDone")
	if len(sd) == 0 {
		log.Fatal("no structureDone")
	}
	fmt.Println("structureDone rows:", sd[0]["rows"])

	// A fresh connection sees the current committed state (A1 = 77).
	c := dial("cccc")
	snap2 := c.snapshot()
	for _, sh := range snap2["sheets"].([]any) {
		sm := sh.(map[string]any)
		if sm["name"] == sheetName {
			if got := cellDisplay(sm["cells"].([]any), 0, 0); got != "77" {
				log.Fatalf("reconnect A1 = %q want 77", got)
			}
		}
	}
	fmt.Println("reconnect sees current A1 = 77")

	fmt.Println("E2E_OK")
}
