// Command install serves the envi CLI install scripts over plain HTTP, so installing is a one-liner.
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
	// curl | sh ignores the path, so / and /install.sh serve the same script.
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
