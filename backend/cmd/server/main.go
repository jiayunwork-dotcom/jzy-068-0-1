// Command collabsheet runs the collaboration spreadsheet server: it loads
// (or seeds) the workbook from PostgreSQL, starts the WebSocket hub and
// serves the built React bundle.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"collabsheet/internal/api"
	"collabsheet/internal/collab"
	"collabsheet/internal/engine"
	"collabsheet/internal/storage"
)

func main() {
	addr := env("ADDR", ":8080")
	dsn := env("DATABASE_DSN",
		"postgres://collab:collab@localhost:5432/collabsheet?sslmode=disable")
	staticDir := env("STATIC_DIR", "./web/dist")
	bookID := env("WORKBOOK_ID", "default")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	st, err := storage.Open(ctx, dsn)
	cancel()
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer st.Close()

	wb, version, err := st.LoadWorkbook(context.Background(), bookID, engine.SeedWorkbook())
	if err != nil {
		log.Fatalf("load workbook: %v", err)
	}
	log.Printf("workbook %q loaded (version %d, %d sheets)",
		bookID, version, len(wb.Snapshot()))

	adapter := &api.PgStoreAdapter{Store: st, BookID: bookID, WB: wb}
	hub := collab.NewHub(wb, adapter)

	handler := api.NewServer(hub, staticDirIfExists(staticDir))

	srv := &http.Server{Addr: addr, Handler: handler}
	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	_ = srv.Shutdown(shutdownCtx)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// staticDirIfExists returns "" when the bundle isn't built yet, so the API
// still runs (e.g. during `go test` / backend-only development).
func staticDirIfExists(dir string) string {
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return dir
}
