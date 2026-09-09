package main

import (
	"log"
	"net/http"

	"github.com/cachebag/relay/internal/api"
)

func main() {

	//Expected output if the server is running correctly: "relayd listening on :8080"
	log.Println("relayd listening on :8080")

	//Saves any error as a variable called err, and if an error occurs it will print the error and stop the program
	if err := http.ListenAndServe(":8080", api.NewHandler()); err != nil {
		log.Fatal(err)
	}
}
