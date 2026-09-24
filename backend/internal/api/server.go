// Package api wires the HTTP/WebSocket transport onto the collaboration hub.
package api

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"collabsheet/internal/collab"
)

// Server holds shared HTTP dependencies.
type Server struct {
	hub      *collab.Hub
	upgrader websocket.Upgrader

	// conns tracks live *session handles by client id for reconnect replace.
	mu sync.Mutex
}

// NewServer builds the Gin handler around one collaboration hub. staticDir,
// when non-empty, serves the built React bundle.
func NewServer(hub *collab.Hub, staticDir string) http.Handler {
	srv := &Server{
		hub: hub,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.GET("/ws", srv.handleWS)

	if staticDir != "" {
		r.NoRoute(func(c *gin.Context) {
			serveSPA(c, staticDir)
		})
	}
	return r
}
