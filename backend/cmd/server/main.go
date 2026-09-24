// Command server runs the collaborative spreadsheet backend: Gin HTTP API,
// WebSocket hub and PostgreSQL persistence.
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"collabsheet/internal/collab"
	"collabsheet/internal/engine"
	"collabsheet/internal/store"
	"collabsheet/internal/workbook"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	addr := ":" + env("PORT", "8080")
	dsn := os.Getenv("DATABASE_DSN")
	bookID := env("BOOK_ID", "default")

	// Load the persisted workbook, or seed the sample so a fresh `docker
	// compose up` immediately shows live, cross-sheet formulas.
	var wb *workbook.Workbook
	var pg *store.Postgres
	if dsn != "" {
		var err error
		pg, err = store.New(dsn, bookID)
		if err != nil {
			log.Fatalf("postgres: %v", err)
		}
		loaded, err := pg.Load()
		if err == nil && loaded != nil && len(loaded.Sheets) > 0 {
			wb = loaded
			log.Printf("loaded workbook %s from postgres", bookID)
		} else {
			wb = workbook.SeedSample()
			if err := pg.Save(wb); err != nil {
				log.Printf("initial save failed: %v", err)
			}
		}
		defer pg.Close()
	} else {
		wb = workbook.SeedSample()
	}

	eng := engine.New(wb)
	room := collab.NewRoom(eng, persisterOrNil(pg))

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	r.GET("/api/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	// REST snapshot is handy for checks and for the very first paint before
	// the socket connects.
	r.GET("/api/workbook", func(c *gin.Context) {
		c.JSON(200, gin.H{"sheets": wb.Sheets, "version": room.Version()})
	})

	// Serve the built frontend (SPA). The dist directory is copied next to the
	// binary in the Docker image; in local development Vite serves on :5173.
	registerStatic(r, env("FRONTEND_DIST", "../frontend/dist"))

	var upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	r.GET("/ws", func(c *gin.Context) {
		id := c.Query("id")
		if id == "" {
			id = collab.NewClientID()
		}
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("upgrade: %v", err)
			return
		}
		ws := collab.NewWSClient(id, conn, 256)
		log.Printf("client %s connected (%s)", id, conn.RemoteAddr())
		room.ServeConn(ws)
		log.Printf("client %s disconnected", id)
	})

	log.Printf("listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}

func persisterOrNil(pg *store.Postgres) collab.Persister {
	if pg == nil {
		return nil
	}
	return pg
}
