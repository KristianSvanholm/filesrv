package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"files/internal/files"
)

func TestDownloadRejectsFilesOverLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := files.New(root, 4)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/download?path=large.txt", nil)
	response := httptest.NewRecorder()
	New(store).ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		forwarded  string
		want       string
	}{
		{name: "forwarded client", remoteAddr: "10.0.0.1:1234", forwarded: "203.0.113.10, 10.0.0.1", want: "203.0.113.10"},
		{name: "direct client", remoteAddr: "203.0.113.10:1234", want: "203.0.113.10"},
		{name: "malformed remote address", remoteAddr: "unknown", want: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.RemoteAddr = test.remoteAddr
			request.Header.Set("X-Forwarded-For", test.forwarded)
			if got := clientIP(request); got != test.want {
				t.Errorf("clientIP() = %q, want %q", got, test.want)
			}
		})
	}
}
