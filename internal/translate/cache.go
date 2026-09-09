package translate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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

// Pending counts the passages this run left in the source language, across
// every document.
func (r RunInfo) Pending() int {
	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		return 0
	}
	total := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".pending" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(r.Dir, e.Name()))
		if err != nil {
			continue
		}
		var idx []int
		if json.Unmarshal(data, &idx) == nil {
			total += len(idx)
		}
	}
	return total
}

// Recipe is everything that changes what a translation comes out as. Two runs
// that agree on all of it may share cached documents; two runs that differ on
// any of it must not — otherwise fixing a glossary and running again would
// silently hand back the old translation.
type Recipe struct {
	Provider       string
	Model          string
	Effort         string
	TargetLanguage string
	TargetCode     string
	SourceLanguage string
	SourceCode     string
	Glossary       string
	StyleNotes     string
	About          string
	// KeepOriginalTitles belongs here because it changes what comes back: a
	// chapter cached with its heading translated must not be served to a run
	// that asked for the original titles.
	KeepOriginalTitles bool
}

// key derives the cache identity of a book translated under this recipe.
func (r Recipe) key(fingerprint string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		fingerprint, r.Provider, r.Model, r.Effort,
		r.TargetLanguage, r.TargetCode, r.SourceLanguage, r.SourceCode,
		r.Glossary, r.StyleNotes, r.About,
		strconv.FormatBool(r.KeepOriginalTitles),
	}, "\x00")))
	return hex.EncodeToString(sum[:])[:16]
}

// OpenCache opens, creating it if needed, the cache for one book translated
// under one recipe. fingerprint identifies the source book, normally the
// SHA-256 of the file.
func OpenCache(root, fingerprint string, recipe Recipe, info RunInfo) (*FileCache, error) {
	dir := filepath.Join(root, recipe.key(fingerprint))
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

// pendingPath is where the list of untranslated segments of a document lives,
// next to the document itself.
func (c *FileCache) pendingPath(key string) string {
	return c.path(key) + ".pending"
}

// GetPending returns the segments an earlier pass left in the source language.
// The second result is false when nothing was recorded — which is not the same
// as "nothing is pending": a cache written before this was tracked simply does
// not know, and must not be retried blindly.
func (c *FileCache) GetPending(key string) ([]int, bool) {
	data, err := os.ReadFile(c.pendingPath(key))
	if err != nil {
		return nil, false
	}
	var out []int
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, false
	}
	return out, true
}

// PutPending records the segments left in the source language.
func (c *FileCache) PutPending(key string, indices []int) error {
	if indices == nil {
		indices = []int{}
	}
	data, err := json.Marshal(indices)
	if err != nil {
		return err
	}
	return os.WriteFile(c.pendingPath(key), data, 0o600)
}

// Put implements Cache.
func (c *FileCache) Put(key string, data []byte) error {
	tmp := c.path(key) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path(key))
}

// PendingStore is an optional Cache capability: remembering which passages of
// a document stayed in the source language, so a later pass can retry exactly
// those.
type PendingStore interface {
	GetPending(key string) ([]int, bool)
	PutPending(key string, indices []int) error
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
