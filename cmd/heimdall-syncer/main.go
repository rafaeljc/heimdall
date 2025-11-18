package main

import (
	"log"
	"time"
)

func main() {
	appName := "heimdall-syncer"

	log.Printf("Starting %s worker...", appName)

	// Loop forever to simulate worker processing
	for {
		log.Printf("%s is waiting for jobs...", appName)
		time.Sleep(10 * time.Second)
	}
}
