package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	port := "8080"
	appName := "heimdall-control-plane"

	log.Printf("Starting %s service...", appName)
	log.Printf("Listening on port %s", port)

	// Minimal HTTP server to keep the process alive and bind to the port
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "OK")
	})

	// If ListenAndServe fails, log the error and exit
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Printf("Failed to start server: %v", err)
		os.Exit(1)
	}
}
