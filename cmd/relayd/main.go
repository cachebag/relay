package main

import (
	"flag"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/cachebag/relay/internal/api"
	"github.com/cachebag/relay/internal/store"
)

// Package level variable that stores relayd's version number.
var Version = "0.0.0"

// Prints relayd's version number
func printVersion(w io.Writer) {
	logger := log.New(w, "", 0)
	logger.Println("relayd", Version)
}

func main() {
	// Creation of version flag
	version := flag.Bool(
		"version",
		false,
		"Prints what version of relayd is being used")
	// Makes the 'flag' package inspect cli arguments.
	flag.Parse()

	// Creates an in-memory registry to store node information.
	reg := store.NewMem()

	// Expected output if the server is running correctly: "relayd listening on :8080"
	log.Println("relayd listening on :8080")

	// Tells the printVersion function to print relayd's version number
	// if the --version flag is passed while the program is running.
	if *version {
		printVersion(os.Stdout)
		return
	}

	// Saves any error as a variable called err, and if an error occurs
	// it will print the error and stop the program
	if err := http.ListenAndServe(":8080", api.NewHandler(reg)); err != nil {
		log.Fatal(err)
	}

}
