package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/sync/singleflight"
)

const (
	defaultThumbnailEdge  = 320
	maxThumbnailPixels    = 40_000_000
	maxThumbnailSourceLen = 100 << 20
	thumbnailVersion      = "v1"
	negativeCacheTTL      = 2 * time.Minute
)

type apiError struct {
	status int
	err    error
}

func (e *apiError) Error() string { return e.err.Error() }
func (e *apiError) Unwrap() error { return e.err }

func newAPIError(status int, message string) error {
	return &apiError{status: status, err: errors.New(message)}
}

type thumbnailCache struct {
	dir      string
	maxBytes int64
	group    singleflight.Group
	decode   chan struct{}

	mu       sync.Mutex
	negative map[string]time.Time
}

type thumbnailResult struct {
	path        string
	contentType string
	etag        string
}

func defaultThumbnailCacheDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}
	return filepath.Join(dir, "eta", "thumbnails"), nil
}

func newThumbnailCache(dir string, maxBytes int64) (*thumbnailCache, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("thumbnail cache size must be positive")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create thumbnail cache: %w", err)
	}
	workers := min(runtime.NumCPU(), 4)
	if workers < 1 {
		workers = 1
	}
	cache := &thumbnailCache{
		dir:      dir,
		maxBytes: maxBytes,
		decode:   make(chan struct{}, workers),
		negative: make(map[string]time.Time),
	}
	if err := cache.evict(); err != nil {
		return nil, err
	}
	return cache, nil
}

func thumbnailSize(raw string) (int, error) {
	if raw == "" {
		return defaultThumbnailEdge, nil
	}
	size, err := strconv.Atoi(raw)
	if err != nil {
		return 0, newAPIError(http.StatusBadRequest, "thumbnail size must be 160, 320, or 640")
	}
	switch size {
	case 160, 320, 640:
		return size, nil
	default:
		return 0, newAPIError(http.StatusBadRequest, "thumbnail size must be 160, 320, or 640")
	}
}

func parseCacheBytes(raw string) (int64, error) {
	return parseBytes(raw, "cache size")
}

// parseBytes parses a human-readable byte size like "4GB" / "512MB"
// / "64 KiB". The label is used only in the error message so the
// caller can identify which flag produced the bad value when more
// than one cache size is configured.
func parseBytes(raw string, label string) (int64, error) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	multipliers := []struct {
		suffix string
		bytes  int64
	}{
		{"GIB", 1 << 30}, {"GB", 1 << 30}, {"MIB", 1 << 20}, {"MB", 1 << 20},
		{"KIB", 1 << 10}, {"KB", 1 << 10}, {"B", 1},
	}
	for _, unit := range multipliers {
		if !strings.HasSuffix(value, unit.suffix) {
			continue
		}
		number := strings.TrimSpace(strings.TrimSuffix(value, unit.suffix))
		parsed, err := strconv.ParseInt(number, 10, 64)
		if err != nil || parsed <= 0 || parsed > (1<<63-1)/unit.bytes {
			return 0, fmt.Errorf("invalid %s %q", label, raw)
		}
		return parsed * unit.bytes, nil
	}
	return 0, fmt.Errorf("invalid %s %q", label, raw)
}

func (c *thumbnailCache) get(source string, info fs.FileInfo, edge int) (thumbnailResult, error) {
	if !info.Mode().IsRegular() {
		return thumbnailResult{}, newAPIError(http.StatusBadRequest, "thumbnail is only available for regular files")
	}
	if info.Size() > maxThumbnailSourceLen {
		return thumbnailResult{}, newAPIError(http.StatusRequestEntityTooLarge, "image is too large to thumbnail")
	}
	key := thumbnailKey(source, info, edge)
	if result, ok := c.lookup(key); ok {
		return result, nil
	}
	if c.isNegative(key) {
		return thumbnailResult{}, newAPIError(http.StatusUnsupportedMediaType, "image cannot be decoded")
	}

	value, err, _ := c.group.Do(key, func() (any, error) {
		if result, ok := c.lookup(key); ok {
			return result, nil
		}
		c.decode <- struct{}{}
		defer func() { <-c.decode }()

		image, alpha, err := decodeThumbnail(source, edge)
		if err != nil {
			c.rememberNegative(key)
			return nil, err
		}
		result, err := c.store(key, image, alpha)
		if err != nil {
			return nil, err
		}
		return result, nil
	})
	if err != nil {
		return thumbnailResult{}, err
	}
	return value.(thumbnailResult), nil
}

func thumbnailKey(source string, info fs.FileInfo, edge int) string {
	input := strings.Join([]string{
		thumbnailVersion,
		source,
		strconv.FormatInt(info.Size(), 10),
		strconv.FormatInt(info.ModTime().UnixNano(), 10),
		strconv.Itoa(edge),
	}, "\x00")
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func (c *thumbnailCache) lookup(key string) (thumbnailResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for extension, contentType := range map[string]string{".jpg": "image/jpeg", ".png": "image/png"} {
		path := filepath.Join(c.dir, key+extension)
		if _, err := os.Stat(path); err == nil {
			now := time.Now()
			_ = os.Chtimes(path, now, now)
			return thumbnailResult{path: path, contentType: contentType, etag: key}, true
		}
	}
	return thumbnailResult{}, false
}

func (c *thumbnailCache) isNegative(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	expires, ok := c.negative[key]
	if !ok {
		return false
	}
	if time.Now().After(expires) {
		delete(c.negative, key)
		return false
	}
	return true
}

func (c *thumbnailCache) rememberNegative(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.negative[key] = time.Now().Add(negativeCacheTTL)
}

func decodeThumbnail(path string, edge int) (image.Image, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()

	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return nil, false, newAPIError(http.StatusUnsupportedMediaType, "image cannot be decoded")
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxThumbnailPixels {
		return nil, false, newAPIError(http.StatusRequestEntityTooLarge, "image dimensions are too large to thumbnail")
	}
	if _, err := file.Seek(0, 0); err != nil {
		return nil, false, err
	}
	source, _, err := image.Decode(file)
	if err != nil {
		return nil, false, newAPIError(http.StatusUnsupportedMediaType, "image cannot be decoded")
	}

	bounds := source.Bounds()
	side := min(bounds.Dx(), bounds.Dy())
	crop := image.Rect(
		bounds.Min.X+(bounds.Dx()-side)/2,
		bounds.Min.Y+(bounds.Dy()-side)/2,
		bounds.Min.X+(bounds.Dx()-side)/2+side,
		bounds.Min.Y+(bounds.Dy()-side)/2+side,
	)
	thumbnail := image.NewNRGBA(image.Rect(0, 0, edge, edge))
	draw.CatmullRom.Scale(thumbnail, thumbnail.Bounds(), source, crop, draw.Over, nil)
	return thumbnail, hasAlpha(thumbnail), nil
}

func hasAlpha(image image.Image) bool {
	bounds := image.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := image.At(x, y).RGBA()
			if alpha != 0xffff {
				return true
			}
		}
	}
	return false
}

func (c *thumbnailCache) store(key string, image image.Image, alpha bool) (thumbnailResult, error) {
	extension, contentType := ".jpg", "image/jpeg"
	if alpha {
		extension, contentType = ".png", "image/png"
	}
	temporary, err := os.CreateTemp(c.dir, ".thumbnail-*")
	if err != nil {
		return thumbnailResult{}, err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)

	if alpha {
		err = png.Encode(temporary, image)
	} else {
		err = jpeg.Encode(temporary, image, &jpeg.Options{Quality: 82})
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return thumbnailResult{}, err
	}

	final := filepath.Join(c.dir, key+extension)
	if err := os.Rename(temporaryName, final); err != nil {
		return thumbnailResult{}, err
	}
	if err := c.evict(); err != nil {
		return thumbnailResult{}, err
	}
	return thumbnailResult{path: final, contentType: contentType, etag: key}, nil
}

func (c *thumbnailCache) evict() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}
	type candidate struct {
		path string
		info fs.FileInfo
	}
	var files []candidate
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || (filepath.Ext(entry.Name()) != ".jpg" && filepath.Ext(entry.Name()) != ".png") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		total += info.Size()
		files = append(files, candidate{path: filepath.Join(c.dir, entry.Name()), info: info})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].info.ModTime().Before(files[j].info.ModTime()) })
	for _, file := range files {
		if total <= c.maxBytes {
			break
		}
		if err := os.Remove(file.path); err == nil || errors.Is(err, os.ErrNotExist) {
			total -= file.info.Size()
		}
	}
	return nil
}
