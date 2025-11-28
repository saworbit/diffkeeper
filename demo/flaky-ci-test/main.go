package main

import (
	"log"
	"os"
	"time"
)

func main() {
	log.Println("Test Suite Started...")
	_ = os.WriteFile("status.log", []byte("INIT: System OK"), 0o644)

	time.Sleep(1 * time.Second)
	log.Println("Running database migrations...")
	_ = os.WriteFile("db.lock", []byte("LOCKED"), 0o644)

	time.Sleep(1 * time.Second)
	// SIMULATE BUG: File gets corrupted silently
	log.Println("CRITICAL: Race condition triggered!")
	_ = os.WriteFile("status.log", []byte("ERROR: Connection Lost"), 0o644)

	time.Sleep(1 * time.Second)
	log.Fatal("Test Failed due to timeout waiting for DB.")
}
