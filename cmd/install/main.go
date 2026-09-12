// Command install serves the envi CLI install scripts over plain HTTP, so
// installing is a one-liner:
//
//	curl -fsSL https://install.envisecrets.com | sh
//	irm https://install.envisecrets.com/install.ps1 | iex
//
// It has no database, no auth, and no dependency on the main API — a script
// that never changes based on who's asking doesn't need a request to reach
// past this process. scripts/install.sh and scripts/install.ps1 are read
// once at startup; restart this process after editing either one.
package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	sh, err := os.ReadFile("scripts/install.sh")
	if err != nil {
		log.Fatalf("read install.sh: %v", err)
	}
	ps1, err := os.ReadFile("scripts/install.ps1")
	if err != nil {
		log.Fatalf("read install.ps1: %v", err)
	}

	serveScript := func(body []byte, contentType string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			w.Write(body)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})
	// The bare root is the *nix path — curl ... | sh doesn't care what the URL
	// path is, only the body, so "/" and "/install.sh" serve the same script.
	mux.HandleFunc("GET /", serveScript(sh, "text/x-sh; charset=utf-8"))
	mux.HandleFunc("GET /install.sh", serveScript(sh, "text/x-sh; charset=utf-8"))
	mux.HandleFunc("GET /install.ps1", serveScript(ps1, "text/plain; charset=utf-8"))

	port := os.Getenv("ENVI_SERVER_PORT")
	if port == "" {
		port = "8081"
	}
	addr := ":" + port
	log.Printf("install-script server listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
