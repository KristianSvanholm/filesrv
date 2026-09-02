package main

import (
	"log"
	"net/http"
	"os"

	"files/internal/files"
	"files/internal/server"
)

func main() {
	root := os.Getenv("FILES_ROOT")
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
	}

	store, err := files.New(root)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving %s at http://localhost:3000", store.Root())
	log.Fatal(http.ListenAndServe(":3000", server.New(store)))
}
