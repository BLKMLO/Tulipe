package translate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"os"
	"path/filepath"
	"sort"
	"time"
)

// FileCache stores translated documents under a directory keyed by the book,
// the target language and the model, so that resuming a run reuses exactly the
// work that was already paid for and nothing else.
type FileCache struct {
	dir string
}

// RunInfo is the manifest written next to a cached run, so the interface can
// list resumable work without opening the source book.
type RunInfo struct {
	Dir            string    `json:"-"`
	Source         string    `json:"source"`
	BookTitle      string    `json:"book_title"`
	TargetLanguage string    `json:"target_language"`
	Model          string    `json:"model"`
	Documents      int       `json:"documents"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Done counts the documents already translated in this run.
func (r RunInfo) Done() int {
	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".part" {
			n++
		}
	}
	return n
}

// OpenCache opens, creating it if needed, the cache for one (book, language,
// model) triple. fingerprint identifies the source book, normally the SHA-256
// of the file.
func OpenCache(root, fingerprint, target, model string, info RunInfo) (*FileCache, error) {
	sum := sha256.Sum256([]byte(fingerprint + "\x00" + target + "\x00" + model))
	dir := filepath.Join(root, hex.EncodeToString(sum[:])[:16])
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	info.UpdatedAt = time.Now()
	if data, err := json.MarshalIndent(info, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "run.json"), data, 0o600)
	}
	return &FileCache{dir: dir}, nil
}

func (c *FileCache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:])[:24]+".part")
}

// Get implements Cache.
func (c *FileCache) Get(key string) ([]byte, bool) {
	data, err := os.ReadFile(c.path(key))
	if err != nil {
		return nil, false
	}
	return data, true
}

// Put implements Cache.
func (c *FileCache) Put(key string, data []byte) error {
	tmp := c.path(key) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path(key))
}

// Discard removes every cached document of this run.
func (c *FileCache) Discard() error { return os.RemoveAll(c.dir) }

// Dir is the directory backing the cache.
func (c *FileCache) Dir() string { return c.dir }

// ListRuns returns the cached runs found under root, most recent first.
func ListRuns(root string) ([]RunInfo, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var runs []RunInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		data, err := os.ReadFile(filepath.Join(dir, "run.json"))
		if err != nil {
			continue
		}
		var info RunInfo
		if err := json.Unmarshal(data, &info); err != nil {
			continue
		}
		info.Dir = dir
		runs = append(runs, info)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].UpdatedAt.After(runs[j].UpdatedAt) })
	return runs, nil
}

// Fingerprint is the SHA-256 of a file, used to tie a cache to its book.
func Fingerprint(name string) (string, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// CacheRoot is the default location of the cache.
func CacheRoot() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "tulipe")
	}
	return filepath.Join(dir, "tulipe", "runs")
}
