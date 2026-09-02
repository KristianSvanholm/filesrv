package server

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"

	"files/internal/files"
	"files/internal/view"
)

func New(store *files.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", browse(store))
	mux.HandleFunc("/open", open(store))
	mux.HandleFunc("/download", download(store))
	mux.HandleFunc("/search", search(store))
	return mux
}

func open(store *files.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if store.IsDirectory(path) {
			http.NotFound(w, r)
			return
		}
		filePath, _, err := store.Download(path, nil)
		if err != nil || filePath == "" {
			log.Printf("open failed for %q: %v", path, err)
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filePath)
	}
}

func browse(store *files.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		path := r.URL.Query().Get("path")
		entries, err := store.List(path)
		if err != nil {
			log.Printf("browse failed for %q: %v", path, err)
			http.NotFound(w, r)
			return
		}
		if err := view.Page(view.PageData{Path: path, Parent: store.Parent(path), Username: username(r), AtRoot: path == "", Entries: entries}).Render(r.Context(), w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func download(store *files.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		isDirectory := store.IsDirectory(path)
		name := filepath.Base(path)
		if isDirectory {
			w.Header().Set("Content-Type", "application/zip")
			name += ".zip"
		}
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		filePath, _, err := store.Download(path, w)
		if err != nil {
			log.Printf("download failed for %q: %v", path, err)
			http.NotFound(w, r)
			return
		}
		if filePath != "" {
			http.ServeFile(w, r, filePath)
		}
	}
}

func search(store *files.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(store.Index())
	}
}

func username(r *http.Request) string {
	for _, header := range []string{"Remote-User", "X-Forwarded-User", "X-Auth-Request-User"} {
		if value := r.Header.Get(header); value != "" {
			return value
		}
	}
	return ""
}
