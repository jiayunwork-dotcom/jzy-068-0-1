package collab

import (
	"fmt"
	"sync"

	"collabsheet/internal/engine"
)

// Store is the persistence seam. Tests use an in-memory implementation; the
// API server supplies the PostgreSQL-backed one.
type Store interface {
	WriteCell(sheet string, col, row int, value string) error
	WriteVersion(v int64) error
	// ReplaceWorkbook persists the entire workbook after a structural edit
	// moved/deleted many cells at once.
	ReplaceWorkbook(version int64) error
}

// Client is one WebSocket connection. Its send channel is drained by the
// transport (the API layer or a test harness).
type Client struct {
	ID     string
	Name   string
	Color  string
	send   chan Envelope
	closed bool
	mu     sync.Mutex
}

func newClient(id, name, color string) *Client {
	return &Client{ID: id, Name: name, Color: color, send: make(chan Envelope, 256)}
}

// Messages returns the receive side of this client's outbound queue.
func (c *Client) Messages() <-chan Envelope { return c.send }
func (c *Client) Send(e Envelope) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	select {
	case c.send <- e:
		return true
	default:
		return false
	}
}

func (c *Client) close() {
	c.mu.Lock()
	c.closed = true
	close(c.send)
	c.mu.Unlock()
}

// Hub is the single collaboration room around one workbook.
type Hub struct {
	wb      *engine.Workbook
	store   Store
	mu      sync.Mutex
	clients map[string]*Client // id -> live client (id can reconnect)
	actors  map[string]*Actor
	pres    map[string]*UserState

	// lastAuthor keys "sheet/col,row" -> id of the client that last wrote it.
	lastAuthor map[string]string

	// per-client undo/redo histories of their own cell edits
	undo map[string][]*editEntry
	redo map[string][]*editEntry

	version int64
	colors  *colorWheel
}

type editEntry struct {
	sheet         string
	col, row      int
	before, after string
}

func cellCoord(sheet string, col, row int) string {
	return fmt.Sprintf("%s/%d,%d", sheet, col, row)
}

// Workbook returns the underlying engine workbook.
func (h *Hub) Workbook() *engine.Workbook { return h.wb }

// NewHub wraps a workbook; store may be nil for ephemeral runs/tests.
func NewHub(wb *engine.Workbook, st Store) *Hub {
	return &Hub{
		wb:         wb,
		store:      st,
		clients:    map[string]*Client{},
		actors:     map[string]*Actor{},
		pres:       map[string]*UserState{},
		lastAuthor: map[string]string{},
		undo:       map[string][]*editEntry{},
		redo:       map[string][]*editEntry{},
		colors:     newColorWheel(),
	}
}
