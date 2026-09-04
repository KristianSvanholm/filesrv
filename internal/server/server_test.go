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
	New(store, nil).ServeHTTP(response, request)
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
			if got := clientIP(request, nil); got != test.want {
				t.Errorf("clientIP() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTrustedProxyHeaders(t *testing.T) {
	trusted, err := ParseTrustedProxyCIDRs("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.10")
	request.Header.Set("Remote-User", "spoofed")
	if got := clientIP(request, trusted); got != "192.0.2.10" {
		t.Errorf("clientIP() = %q, want direct client IP", got)
	}
	if got := username(request, trusted); got != "" {
		t.Errorf("username() = %q, want empty", got)
	}
	request.RemoteAddr = "10.0.0.10:1234"
	if got := clientIP(request, trusted); got != "203.0.113.10" {
		t.Errorf("clientIP() = %q, want forwarded client IP", got)
	}
	if got := username(request, trusted); got != "spoofed" {
		t.Errorf("username() = %q, want forwarded username", got)
	}
}

func TestTrustedProxyBlocksDirectRequests(t *testing.T) {
	root := t.TempDir()
	store, err := files.New(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := ParseTrustedProxyCIDRs("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	response := httptest.NewRecorder()
	New(store, trusted).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", response.Code, http.StatusForbidden)
	}

	request.RemoteAddr = "10.0.0.10:1234"
	response = httptest.NewRecorder()
	New(store, trusted).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestParseTrustedProxyCIDRs(t *testing.T) {
	prefixes, err := ParseTrustedProxyCIDRs("10.0.0.0/8, 2001:db8::/32")
	if err != nil || len(prefixes) != 2 {
		t.Fatalf("ParseTrustedProxyCIDRs() = %v, %v", prefixes, err)
	}
	if _, err := ParseTrustedProxyCIDRs("not-a-cidr"); err == nil {
		t.Error("ParseTrustedProxyCIDRs() succeeded, want error")
	}
}
