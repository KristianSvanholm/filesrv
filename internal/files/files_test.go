package files

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadSizeLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "directory", "large.txt"), []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := New(root, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"large.txt", "directory"} {
		allowed, err := store.CanDownload(path)
		if err != nil {
			t.Fatalf("CanDownload(%q): %v", path, err)
		}
		if allowed {
			t.Errorf("CanDownload(%q) = true, want false", path)
		}
		if _, _, err := store.Download(path, &bytes.Buffer{}); !errors.Is(err, ErrDownloadTooLarge) {
			t.Errorf("Download(%q) error = %v, want ErrDownloadTooLarge", path, err)
		}
	}
	entries, err := store.List("")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.CanDownload {
			t.Errorf("List entry %q is downloadable, want false", entry.Name)
		}
	}
}

func TestParseSize(t *testing.T) {
	tests := map[string]int64{
		"0":     0,
		"10":    10,
		"10K":   10 << 10,
		"10m":   10 << 20,
		"10G":   10 << 30,
		"1T":    1 << 40,
		"1000G": 1000 << 30,
	}
	for input, want := range tests {
		got, err := ParseSize(input)
		if err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d, nil", input, got, err, want)
		}
	}
	for _, input := range []string{"", "-1", "10MB", "1.5G", "999999999999999999999T"} {
		if _, err := ParseSize(input); err == nil {
			t.Errorf("ParseSize(%q) succeeded, want error", input)
		}
	}
}
