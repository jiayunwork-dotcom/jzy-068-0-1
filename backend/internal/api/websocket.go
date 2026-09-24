package api

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"collabsheet/internal/collab"
)

// session is one live WebSocket: it pumps Envelopes between the hub's client
// channel and the socket.
type session struct {
	server  *Server
	conn    *websocket.Conn
	hub     *collab.Hub
	id      string
	writeMu sync.Mutex
	done    chan struct{}
}

func (s *Server) handleWS(c *gin.Context) {
	ws, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	sess := &session{
		server: s, conn: ws, hub: s.hub, done: make(chan struct{}),
	}
	sess.run()
}

func (s *session) run() {
	defer func() {
		if s.id != "" {
			s.hub.Disconnect(s.id)
		}
		close(s.done)
		_ = s.conn.Close()
	}()

	// Reader loop.
	go s.writePump()

	for {
		var msg collab.Envelope
		if err := s.conn.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Type {
		case collab.MsgHello:
			id := msg.ClientID
			if id == "" {
				id = newID()
			}
			s.id = id
			cl, snap := s.hub.Connect(id, msg.Name)
			s.write(snap)
			go s.pumpChannel(cl)

		case collab.MsgSetCell:
			if s.id == "" {
				continue
			}
			// Synchronous: keeps this client's mutations ordered (the hub
			// lock serializes them against every other connection anyway).
			s.hub.ApplyCellEdit(s.id, msg.MsgID, msg.Sheet, msg.Col, msg.Row, msg.Value)

		case collab.MsgStructure:
			if s.id == "" {
				continue
			}
			s.hub.ApplyStructure(s.id, msg.MsgID, msg.Kind, msg.Sheet, msg.Index)

		case collab.MsgCursor:
			s.hub.SetCursor(s.id, msg.Sheet, msg.Col, msg.Row, msg.Col >= 0)

		case collab.MsgEditing:
			s.hub.SetEditing(s.id, msg.Sheet, msg.Col, msg.Row, msg.Editing)

		case collab.MsgUndo:
			s.hub.Undo(s.id, msg.MsgID)

		case collab.MsgRedo:
			s.hub.Redo(s.id, msg.MsgID)

		case collab.MsgPing:
			s.write(collab.Envelope{Type: "pong"})
		}
	}
}

// pumpChannel forwards every hub envelope for this client to the socket.
func (s *session) pumpChannel(c *collab.Client) {
	for {
		select {
		case <-s.done:
			return
		case msg, ok := <-c.Messages():
			if !ok {
				return
			}
			if !s.write(msg) {
				return
			}
		}
	}
}

func (s *session) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.writeMu.Lock()
			err := s.conn.WriteMessage(websocket.PingMessage, nil)
			s.writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (s *session) write(msg collab.Envelope) bool {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := s.conn.WriteJSON(msg); err != nil {
		return false
	}
	return true
}

// serveSPA serves static assets and falls back to index.html so the React
// app handles client-side routes.
func serveSPA(c *gin.Context, dir string) {
	p := strings.TrimPrefix(c.Request.URL.Path, "/")
	if p == "" {
		p = "index.html"
	}
	full := filepath.Join(dir, filepath.Clean("/"+p))
	if info, err := os.Stat(full); err == nil && !info.IsDir() {
		c.File(full)
		return
	}
	c.File(filepath.Join(dir, "index.html"))
}

var idCounter = time.Now().UnixNano()
var idMu sync.Mutex

func newID() string {
	idMu.Lock()
	idCounter++
	n := idCounter
	idMu.Unlock()
	const hex = "0123456789abcdef"
	b := make([]byte, 16)
	for i := range b {
		b[i] = hex[(n>>(uint(i)*4))&0xf]
		n = n*6364136223846793005 + 1442695040888963407
	}
	return string(b)
}
