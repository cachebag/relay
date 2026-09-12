package main

import (
	"log"
	"net/http"

	"github.com/cachebag/relay/internal/api"

	"flag"
)

// Package level variable that stores relayd's version number.
var Version = "0.0.0"

func main() {
	// Creation of version flag, which will print the version of relayd when the program is run with the --version flag
	version := flag.Bool("version", false, "Prints what version of relayd is being used")
	// Makes the 'flag' package inspect cli arguments.
	flag.Parse()

	//Expected output if the server is running correctly: "relayd listening on :8080"
	log.Println("relayd listening on :8080")

	//Saves any error as a variable called err, and if an error occurs it will print the error and stop the program
	if err := http.ListenAndServe(":8080", api.NewHandler()); err != nil {
		log.Fatal(err)
	}

	// Prints the version of relayd if the --version flag is passed while the program is running.
	if *version {
		log.Println("relayd", Version)
		return
	}
}
