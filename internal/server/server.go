package server

import (
	"encoding/json"
	"errors"
	"log"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strings"

	"files/internal/files"
	"files/internal/view"
)

func New(store *files.Store, trustedProxyCIDRs []netip.Prefix) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", browse(store, trustedProxyCIDRs))
	mux.HandleFunc("/open", open(store))
	mux.HandleFunc("/download", download(store, trustedProxyCIDRs))
	mux.HandleFunc("/search", search(store))
	if len(trustedProxyCIDRs) > 0 {
		return requireTrustedProxy(mux, trustedProxyCIDRs)
	}
	return mux
}

func requireTrustedProxy(next http.Handler, trustedProxyCIDRs []netip.Prefix) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !fromTrustedProxy(r, trustedProxyCIDRs) {
			http.Error(w, "access must be through a trusted proxy", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
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

func browse(store *files.Store, trustedProxyCIDRs []netip.Prefix) http.HandlerFunc {
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
		if err := view.Page(view.PageData{Path: path, Parent: store.Parent(path), Username: username(r, trustedProxyCIDRs), AtRoot: path == "", Entries: entries}).Render(r.Context(), w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func download(store *files.Store, trustedProxyCIDRs []netip.Prefix) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		allowed, err := store.CanDownload(path)
		if err != nil {
			log.Printf("download failed for %q: %v", path, err)
			http.NotFound(w, r)
			return
		}
		if !allowed {
			http.Error(w, "download exceeds size limit", http.StatusRequestEntityTooLarge)
			return
		}
		log.Printf("download user=%q ip=%q path=%q", username(r, trustedProxyCIDRs), clientIP(r, trustedProxyCIDRs), path)
		isDirectory := store.IsDirectory(path)
		name := filepath.Base(path)
		if isDirectory {
			w.Header().Set("Content-Type", "application/zip")
			name += ".zip"
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		filePath, _, err := store.Download(path, w)
		if err != nil {
			log.Printf("download failed for %q: %v", path, err)
			if errors.Is(err, files.ErrDownloadTooLarge) {
				http.Error(w, "download exceeds size limit", http.StatusRequestEntityTooLarge)
				return
			}
			http.NotFound(w, r)
			return
		}
		if filePath != "" {
			http.ServeFile(w, r, filePath)
		}
	}
}

func search(store *files.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(store.Search(r.URL.Query().Get("q")))
	}
}

func username(r *http.Request, trustedProxyCIDRs []netip.Prefix) string {
	if !fromTrustedProxy(r, trustedProxyCIDRs) {
		return ""
	}
	for _, header := range []string{"Remote-User", "X-Forwarded-User", "X-Auth-Request-User"} {
		if value := r.Header.Get(header); value != "" {
			return value
		}
	}
	return ""
}

func clientIP(r *http.Request, trustedProxyCIDRs []netip.Prefix) string {
	if forwardedFor := r.Header.Get("X-Forwarded-For"); forwardedFor != "" && fromTrustedProxy(r, trustedProxyCIDRs) {
		return strings.TrimSpace(strings.Split(forwardedFor, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func ParseTrustedProxyCIDRs(value string) ([]netip.Prefix, error) {
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

func fromTrustedProxy(r *http.Request, trustedProxyCIDRs []netip.Prefix) bool {
	if len(trustedProxyCIDRs) == 0 {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, prefix := range trustedProxyCIDRs {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
