// Package diskcache provides a bounded persistent cache for remote file blocks.
package diskcache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Cache struct {
	dir   string
	limit int64
	mu    sync.Mutex
}

func DefaultPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "eta", "remote-files"), nil
}

func New(dir string, limit int64) (*Cache, error) {
	if limit < 0 {
		return nil, errors.New("negative cache limit")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Cache{dir: dir, limit: limit}, nil
}
func key(k string) string             { h := sha256.Sum256([]byte(k)); return hex.EncodeToString(h[:]) }
func (c *Cache) path(k string) string { return filepath.Join(c.dir, key(k)) }
func (c *Cache) Get(k string) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.path(k)
	b, e := os.ReadFile(p)
	if os.IsNotExist(e) {
		return nil, false, nil
	}
	if e != nil {
		return nil, false, e
	}
	now := time.Now()
	_ = os.Chtimes(p, now, now)
	return b, true, nil
}
func (c *Cache) Put(k string, b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if int64(len(b)) > c.limit {
		return nil
	}
	tmp, e := os.CreateTemp(c.dir, ".cache-")
	if e != nil {
		return e
	}
	n := tmp.Name()
	defer os.Remove(n)
	if _, e = tmp.Write(b); e == nil {
		e = tmp.Close()
	}
	if e != nil {
		return e
	}
	if e = os.Rename(n, c.path(k)); e != nil {
		return e
	}
	return c.evict()
}
func (c *Cache) evict() error {
	es, e := os.ReadDir(c.dir)
	if e != nil {
		return e
	}
	type f struct {
		p string
		t time.Time
		s int64
	}
	var fs []f
	var total int64
	for _, x := range es {
		if x.IsDir() || x.Name()[0] == '.' {
			continue
		}
		i, e := x.Info()
		if e != nil {
			return e
		}
		total += i.Size()
		fs = append(fs, f{filepath.Join(c.dir, x.Name()), i.ModTime(), i.Size()})
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].t.Before(fs[j].t) })
	for _, x := range fs {
		if total <= c.limit {
			break
		}
		if e := os.Remove(x.p); e != nil {
			return e
		}
		total -= x.s
	}
	return nil
}
