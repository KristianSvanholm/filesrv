package files

import (
	"archive/zip"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	Name  string
	Path  string
	IsDir bool
	Size  string
}

type SearchEntry struct {
	Path   string `json:"path"`
	Parent string `json:"parent"`
	Dir    bool   `json:"dir"`
}

type Store struct {
	root        string
	index       []SearchEntry
	sizeCache   map[string]cachedSize
	sizeCacheMu sync.Mutex
}

type cachedSize struct {
	size      int64
	updatedAt time.Time
}

const sizeCacheTTL = time.Hour

func New(root string) (*Store, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	store := &Store{root: resolved, sizeCache: make(map[string]cachedSize)}
	store.index, err = store.buildIndex()
	return store, err
}

func (s *Store) Root() string         { return s.root }
func (s *Store) Index() []SearchEntry { return s.index }

func (s *Store) List(requestPath string) ([]Entry, error) {
	directory, requestedPath, err := s.localPath(requestPath)
	if err != nil {
		return nil, err
	}
	items, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		if item.Name() == ".env" {
			continue
		}
		itemPath := filepath.ToSlash(filepath.Join(requestedPath, item.Name()))
		size, _ := s.sizeOf(filepath.Join(directory, item.Name()))
		entries = append(entries, Entry{Name: item.Name(), Path: url.QueryEscape(itemPath), IsDir: item.IsDir(), Size: formatSize(size)})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

func (s *Store) Parent(requestPath string) string {
	parent := filepath.ToSlash(filepath.Dir(requestPath))
	if parent == "." {
		return ""
	}
	return url.QueryEscape(parent)
}

func (s *Store) Download(requestPath string, writer io.Writer) (string, string, error) {
	path, requestedPath, err := s.localPath(requestPath)
	if err != nil || filepath.Base(requestedPath) == ".env" {
		return "", "", os.ErrNotExist
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	name := filepath.Base(requestedPath)
	if !info.IsDir() {
		return path, name, nil
	}
	zipWriter := zip.NewWriter(writer)
	err = filepath.Walk(path, func(filePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		return addToZip(zipWriter, path, filePath)
	})
	if closeErr := zipWriter.Close(); err == nil {
		err = closeErr
	}
	return "", name + ".zip", err
}

func (s *Store) IsDirectory(requestPath string) bool {
	path, _, err := s.localPath(requestPath)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (s *Store) localPath(requestPath string) (string, string, error) {
	clean := filepath.Clean(strings.TrimPrefix(requestPath, "/"))
	if clean == "." {
		clean = ""
	}
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", os.ErrPermission
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(s.root, clean))
	if err != nil {
		return "", "", err
	}
	relative, err := filepath.Rel(s.root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", os.ErrPermission
	}
	return resolved, filepath.ToSlash(clean), nil
}

func (s *Store) buildIndex() ([]SearchEntry, error) {
	var entries []SearchEntry
	var scan func(string) error
	scan = func(directory string) error {
		items, err := os.ReadDir(directory)
		if err != nil {
			return nil
		}
		for _, item := range items {
			if item.Name() == ".env" {
				continue
			}
			path := filepath.Join(directory, item.Name())
			relative, err := filepath.Rel(s.root, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			parent := filepath.ToSlash(filepath.Dir(relative))
			if parent == "." {
				parent = ""
			}
			entries = append(entries, SearchEntry{Path: relative, Parent: parent, Dir: item.IsDir()})
			if item.IsDir() {
				if err := scan(path); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return entries, scan(s.root)
}

func (s *Store) sizeOf(path string) (int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	s.sizeCacheMu.Lock()
	cached, ok := s.sizeCache[path]
	s.sizeCacheMu.Unlock()
	if ok && time.Since(cached.updatedAt) < sizeCacheTTL {
		return cached.size, nil
	}
	var size int64
	err = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	if err == nil {
		s.sizeCacheMu.Lock()
		s.sizeCache[path] = cachedSize{size: size, updatedAt: time.Now()}
		s.sizeCacheMu.Unlock()
	}
	return size, err
}

func formatSize(size int64) string {
	units := []string{"B", "K", "M", "G", "T"}
	value, unit := float64(size), 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return strconv.FormatInt(size, 10) + units[unit]
	}
	return strconv.FormatFloat(math.Round(value*10)/10, 'f', -1, 64) + units[unit]
}

func addToZip(zipWriter *zip.Writer, directory, filePath string) error {
	relative, err := filepath.Rel(directory, filePath)
	if err != nil {
		return err
	}
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	entry, err := zipWriter.Create(filepath.ToSlash(relative))
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, file)
	return err
}
