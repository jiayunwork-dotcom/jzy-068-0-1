package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// registerStatic serves the built SPA from dir if it exists. API and WebSocket
// routes take precedence; anything else falls back to index.html so the client
// router can take over.
func registerStatic(r *gin.Engine, dir string) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return
	}
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/ws") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		full := filepath.Join(dir, filepath.Clean("/"+p))
		if fi, statErr := os.Stat(full); statErr == nil && !fi.IsDir() {
			c.File(full)
			return
		}
		c.File(filepath.Join(dir, "index.html"))
	})
}
