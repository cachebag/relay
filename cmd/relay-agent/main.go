package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	// Pointer to command-line 'pool-token' flag
	poolToken := flag.String(
		"pool-token",
		"", "join token for the pool (required)")

	// Makes the 'flag' package inspect command-line argument
	flag.Parse()

	// Throws error and exits if pool token is not provided
	if *poolToken == "" {
		fmt.Fprintln(os.Stderr, "relay-agent: --pool-token is required")
		os.Exit(2)
	}

}
