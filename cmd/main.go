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

	maxDownloadSize := int64(0)
	if value := os.Getenv("FILES_MAX_DOWNLOAD_SIZE"); value != "" {
		parsedSize, err := files.ParseSize(value)
		if err != nil {
			log.Fatal("FILES_MAX_DOWNLOAD_SIZE must be a non-negative byte value or use a K, M, G, or T suffix")
		}
		maxDownloadSize = parsedSize
	}

	store, err := files.New(root, maxDownloadSize)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving %s at http://localhost:3000", store.Root())
	log.Fatal(http.ListenAndServe(":3000", server.New(store)))
}
