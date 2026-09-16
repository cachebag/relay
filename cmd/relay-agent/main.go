package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/cachebag/relay/internal/agent"
)

func main() {
	// Pointer to command-line 'scheduler' flag
	scheduler := flag.String(
		"scheduler",
		"http://localhost:8080",
		"scheduler base URL")

	// Makes the 'flag' package inspect command-line argument
	flag.Parse()

	// Repeatedly sends a heartbeat every 10 seconds to scheduler
	if err := agent.Run(
		context.Background(),
		*scheduler,
		10*time.Second); err != nil {
		log.Fatal(err)
	}
}
