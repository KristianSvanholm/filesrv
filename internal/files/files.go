package files

import (
	"archive/zip"
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
)

type Entry struct {
	Name    string
	Path    string
	RawPath string
	IsDir   bool
	Size    string
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

func (s *Store) Search(query string) []SearchEntry {
	query = strings.ToLower(query)
	if query == "" {
		return nil
	}
	type match struct {
		entry SearchEntry
		score float64
	}
	matches := make([]match, 0)
	for _, entry := range s.index {
		if score := fuzzyScore(entry.Path, query); score >= 0 {
			matches = append(matches, match{entry, score})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].score < matches[j].score })
	if len(matches) > 12 {
		matches = matches[:12]
	}
	results := make([]SearchEntry, len(matches))
	for i, match := range matches {
		results[i] = match.entry
	}
	return results
}

func fuzzyScore(path, query string) float64 {
	path = strings.ToLower(path)
	name := path[strings.LastIndex(path, "/")+1:]
	if position := strings.Index(name, query); position >= 0 {
		return float64(position)
	}
	if position := strings.Index(path, query); position >= 0 {
		return 100 + float64(position)
	}
	last, gaps := -1, 0
	for _, character := range query {
		position := strings.IndexRune(path[last+1:], character)
		if position < 0 {
			return -1
		}
		position += last + 1
		gaps += position - last - 1
		last = position
	}
	return 1000 + float64(gaps) + float64(len(path))/1000
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
		if item.Name() == ".env" {
			continue
		}
		itemPath := filepath.ToSlash(filepath.Join(requestedPath, item.Name()))
		size, _ := s.sizeOf(filepath.Join(directory, item.Name()))
		entries = append(entries, Entry{Name: item.Name(), Path: url.QueryEscape(itemPath), RawPath: itemPath, IsDir: item.IsDir(), Size: formatSize(size)})
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
	startedAt := time.Now()
	log.Printf("building search index for %s", s.root)
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
	err := scan(s.root)
	if err == nil {
		log.Printf("search index ready: %d entries in %s", len(entries), time.Since(startedAt).Round(time.Millisecond))
	}
	return entries, err
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
	startedAt := time.Now()
	log.Printf("scanning directory size: %s", path)
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
		log.Printf("directory size cached: %s (%s) in %s", path, formatSize(size), time.Since(startedAt).Round(time.Millisecond))
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
