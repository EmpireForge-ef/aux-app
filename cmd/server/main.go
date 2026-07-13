package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	addr := os.Getenv("AUX_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Serve the built frontend; AUX_STATIC_DIR is set by the Nix wrapper,
	// during development it falls back to the local Vite build output.
	staticDir := os.Getenv("AUX_STATIC_DIR")
	if staticDir == "" {
		staticDir = "frontend/dist"
	}
	mux.Handle("/", http.FileServer(http.Dir(staticDir)))

	log.Printf("aux listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
