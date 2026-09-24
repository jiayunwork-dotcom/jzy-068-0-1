package collab

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSClient adapts a gorilla websocket connection to the Client interface.
// Outbound messages are queued and written by a single writer goroutine, which
// is required by the WebSocket protocol.
type WSClient struct {
	id   string
	conn *websocket.Conn
	send chan []byte
	once sync.Once
	done chan struct{}
}

// NewWSClient wraps a connection. id is the client's persistent identity
// (generated on first visit, reused across reconnects).
func NewWSClient(id string, conn *websocket.Conn, buffer int) *WSClient {
	if buffer <= 0 {
		buffer = 256
	}
	return &WSClient{id: id, conn: conn, send: make(chan []byte, buffer), done: make(chan struct{})}
}

// ID implements Client.
func (c *WSClient) ID() string { return c.id }

// Send enqueues an outbound message; drops silently if the client is gone.
func (c *WSClient) Send(payload []byte) {
	select {
	case c.send <- payload:
	case <-c.done:
	}
}

// RunWritePump runs in its own goroutine until the connection closes.
func (c *WSClient) RunWritePump() {
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	defer c.Close()
	for {
		select {
		case msg := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ping.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

// Close terminates the client (idempotent).
func (c *WSClient) Close() {
	c.once.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

// wireMessage is the generic envelope used for dispatch.
type wireMessage struct {
	Type string `json:"type"`
}

// ServeConn runs the read loop for one connection. It blocks until disconnect.
//
// Reconnect semantics: the browser keeps a stable id and immediately sends
// "hello"; the server answers with a fresh snapshot so missed changes are
// caught up, after which queued local edits arrive as normal edit messages and
// go through last-writer-wins like any other.
func (r *Room) ServeConn(c *WSClient) {
	go c.RunWritePump()
	defer func() {
		r.Disconnect(c.ID())
		c.Close()
	}()

	connected := false
	c.conn.SetReadLimit(1 << 20)

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var env wireMessage
		if err := json.Unmarshal(data, &env); err != nil {
			send(c, ErrorMsg{Type: "error", Msg: "bad json"})
			continue
		}
		switch env.Type {
		case "hello":
			var m HelloMsg
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			_ = r.Connect(c, m.Name)
			r.Snapshot(c)
			connected = true
		case "edit":
			if !connected {
				continue
			}
			var m EditMsg
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			r.ApplyEdit(c.ID(), m.Sheet, m.Cell, m.Raw)
		case "struct":
			if !connected {
				continue
			}
			var m StructMsg
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			if !r.ApplyStruct(c.ID(), m.Op) {
				send(c, ErrorMsg{Type: "error", Msg: "invalid structural operation"})
			}
		case "undo":
			if connected {
				r.Undo(c.ID())
			}
		case "redo":
			if connected {
				r.Redo(c.ID())
			}
		case "cursor":
			if !connected {
				continue
			}
			var m CursorMsg
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			r.MoveCursor(c.ID(), m.Sheet, m.Cell)
		}
	}
}
