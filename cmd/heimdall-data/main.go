package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	appName := "heimdall-data-plane"
	port := "50051"

	log.Printf("Starting %s service...", appName)
	log.Printf("Simulating gRPC server on port %s", port)

	// Create a channel to listen for OS signals
	// (e.g., when 'docker stop' is run, Docker sends a SIGTERM)
	sigChan := make(chan os.Signal, 1)

	// Notify the channel on SIGINT (Ctrl+C) and SIGTERM (Docker Stop)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Blocks here waiting for a signal
	log.Println("Service is ready. Waiting for shutdown signal...")
	<-sigChan

	// A signal was received.
	// In the future, we will close DB connections and gRPC servers here.
	log.Println("Shutdown signal received. Shutting down service...")
}
