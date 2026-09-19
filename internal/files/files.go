package files

import (
	"archive/zip"
	"errors"
	"io"
	"log"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sahilm/fuzzy"
)

type Entry struct {
	Name        string
	Path        string
	RawPath     string
	IsDir       bool
	Size        string
	CanDownload bool
}

type SearchEntry struct {
	Path   string `json:"path"`
	Parent string `json:"parent"`
	Dir    bool   `json:"dir"`
}

type Store struct {
	root            string
	maxDownloadSize int64
	index           []SearchEntry
	sizeCache       map[string]cachedSize
	sizeCacheMu     sync.Mutex
}

type cachedSize struct {
	size       int64
	updatedAt  time.Time
	refreshing bool
}

const sizeCacheTTL = time.Hour

var ErrDownloadTooLarge = errors.New("download exceeds size limit")

func ParseSize(value string) (int64, error) {
	value = strings.TrimSpace(value)
	multiplier := int64(1)
	if len(value) > 0 {
		switch strings.ToUpper(value[len(value)-1:]) {
		case "K":
			multiplier = 1 << 10
		case "M":
			multiplier = 1 << 20
		case "G":
			multiplier = 1 << 30
		case "T":
			multiplier = 1 << 40
		}
		if multiplier > 1 {
			value = value[:len(value)-1]
		}
	}
	amount, err := strconv.ParseInt(value, 10, 64)
	if err != nil || amount < 0 || amount > (int64(^uint64(0)>>1))/multiplier {
		return 0, errors.New("invalid size")
	}
	return amount * multiplier, nil
}

func New(root string, maxDownloadSize int64) (*Store, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	store := &Store{root: resolved, maxDownloadSize: maxDownloadSize, sizeCache: make(map[string]cachedSize)}
	store.index, err = store.buildIndex()
	return store, err
}

func (s *Store) Root() string         { return s.root }
func (s *Store) Index() []SearchEntry { return s.index }

func (s *Store) Search(query string) []SearchEntry {
	if query == "" {
		return nil
	}
	paths := make([]string, len(s.index))
	for i, entry := range s.index {
		paths[i] = entry.Path
	}
	matches := fuzzy.Find(query, paths)
	if len(matches) > 12 {
		matches = matches[:12]
	}
	results := make([]SearchEntry, len(matches))
	for i, match := range matches {
		results[i] = s.index[match.Index]
	}
	return results
}

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
		itemPath := filepath.ToSlash(filepath.Join(requestedPath, item.Name()))
		size, _ := s.sizeOf(filepath.Join(directory, item.Name()), true)
		entries = append(entries, Entry{Name: item.Name(), Path: url.QueryEscape(itemPath), RawPath: itemPath, IsDir: item.IsDir(), Size: formatSize(size), CanDownload: s.maxDownloadSize == 0 || size <= s.maxDownloadSize})
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
	if err != nil {
		return "", "", os.ErrNotExist
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	if writer != nil {
		size, err := s.sizeOf(path, false)
		if err != nil {
			return "", "", err
		}
		if s.maxDownloadSize > 0 && size > s.maxDownloadSize {
			return "", "", ErrDownloadTooLarge
		}
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

func (s *Store) CanDownload(requestPath string) (bool, error) {
	path, _, err := s.localPath(requestPath)
	if err != nil {
		return false, os.ErrNotExist
	}
	size, err := s.sizeOf(path, false)
	if err != nil {
		return false, err
	}
	return s.maxDownloadSize == 0 || size <= s.maxDownloadSize, nil
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
	startedAt := time.Now()
	log.Printf("building search index for %s", s.root)
	var scan func(string) (int64, error)
	scan = func(directory string) (int64, error) {
		items, err := os.ReadDir(directory)
		if err != nil {
			return 0, nil
		}
		var total int64
		for _, item := range items {
			path := filepath.Join(directory, item.Name())
			relative, err := filepath.Rel(s.root, path)
			if err != nil {
				return 0, err
			}
			relative = filepath.ToSlash(relative)
			parent := filepath.ToSlash(filepath.Dir(relative))
			if parent == "." {
				parent = ""
			}
			entries = append(entries, SearchEntry{Path: relative, Parent: parent, Dir: item.IsDir()})
			if item.IsDir() {
				size, err := scan(path)
				if err != nil {
					return 0, err
				}
				total += size
				continue
			}
			if info, err := item.Info(); err == nil {
				total += info.Size()
			}
		}
		s.storeSize(directory, total)
		return total, nil
	}
	_, err := scan(s.root)
	if err == nil {
		log.Printf("search index ready: %d entries, %d directory sizes in %s", len(entries), len(s.sizeCache), time.Since(startedAt).Round(time.Millisecond))
	}
	return entries, err
}

func (s *Store) sizeOf(path string, useCache bool) (int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	if useCache {
		if size, ok := s.lookupSize(path); ok {
			return size, nil
		}
	}
	size, err := walkSize(path)
	if err == nil && useCache {
		s.storeSize(path, size)
	}
	return size, err
}

// lookupSize returns a cached directory size. A stale entry is still served so
// the page renders immediately; the rescan happens in the background.
func (s *Store) lookupSize(path string) (int64, bool) {
	s.sizeCacheMu.Lock()
	defer s.sizeCacheMu.Unlock()
	cached, ok := s.sizeCache[path]
	if !ok {
		return 0, false
	}
	if time.Since(cached.updatedAt) >= sizeCacheTTL && !cached.refreshing {
		cached.refreshing = true
		s.sizeCache[path] = cached
		go s.refreshSize(path)
	}
	return cached.size, true
}

func (s *Store) refreshSize(path string) {
	size, err := walkSize(path)
	if err != nil {
		log.Printf("directory size refresh failed for %s: %v", path, err)
		s.sizeCacheMu.Lock()
		cached := s.sizeCache[path]
		cached.refreshing = false
		s.sizeCache[path] = cached
		s.sizeCacheMu.Unlock()
		return
	}
	s.storeSize(path, size)
}

func (s *Store) storeSize(path string, size int64) {
	s.sizeCacheMu.Lock()
	s.sizeCache[path] = cachedSize{size: size, updatedAt: time.Now()}
	s.sizeCacheMu.Unlock()
}

func walkSize(path string) (int64, error) {
	startedAt := time.Now()
	log.Printf("scanning directory size: %s", path)
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	if err == nil {
		log.Printf("directory size scanned: %s (%s) in %s", path, formatSize(size), time.Since(startedAt).Round(time.Millisecond))
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
