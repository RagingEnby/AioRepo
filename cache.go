package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The cache directory holds the last good body of every source and its
// health record. CI keeps it between runs with actions/cache. Losing it only
// costs one run of full downloads and resets the failure counts.
const (
	cacheDir = ".cache"
	// staleLimit is how long a cached body may stand in for a source that
	// fails to load.
	staleLimit = 3 * 24 * time.Hour
	// deadAfter is the number of failed runs in a row (7 days at 4 runs a
	// day) after which a source moves to dead.txt.
	deadAfter = 28
)

type cacheEntry struct {
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"lastModified,omitempty"`
	LastOK       time.Time `json:"lastOK,omitzero"`
	Failures     int       `json:"failures"`
	HasBody      bool      `json:"hasBody"`
}

type diskCache struct {
	dir string

	mu      sync.Mutex
	Entries map[string]*cacheEntry `json:"entries"`
}

func loadCache(dir string) (*diskCache, error) {
	c := &diskCache{dir: dir, Entries: map[string]*cacheEntry{}}
	data, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, c); err != nil {
		// A corrupt index is not worth failing the run over.
		c.Entries = map[string]*cacheEntry{}
	}
	if c.Entries == nil {
		c.Entries = map[string]*cacheEntry{}
	}
	return c, nil
}

func (c *diskCache) bodyPath(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, "bodies", hex.EncodeToString(sum[:12])+".json")
}

// entry returns a copy of the record for key, or a zero record.
func (c *diskCache) entry(key string) cacheEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.Entries[key]; ok {
		return *e
	}
	return cacheEntry{}
}

func (c *diskCache) body(key string) ([]byte, bool) {
	if !c.entry(key).HasBody {
		return nil, false
	}
	b, err := os.ReadFile(c.bodyPath(key))
	return b, err == nil
}

// success records a good fetch. body is nil when the server answered 304.
func (c *diskCache) success(key string, body []byte, etag, lastModified string, now time.Time) error {
	if body != nil {
		p := c.bodyPath(key)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, body, 0o644); err != nil {
			return err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.Entries[key]
	if e == nil {
		e = &cacheEntry{}
		c.Entries[key] = e
	}
	if body != nil {
		e.HasBody = true
		e.ETag, e.LastModified = etag, lastModified
	}
	e.LastOK = now
	e.Failures = 0
	return nil
}

// failure counts a failed fetch and returns the new count.
func (c *diskCache) failure(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.Entries[key]
	if e == nil {
		e = &cacheEntry{}
		c.Entries[key] = e
	}
	e.Failures++
	return e.Failures
}

func (c *diskCache) save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", " ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(c.dir, "index.json"), data)
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
