package main

import (
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestJPEG(t *testing.T, path string, width, height int) {
	t.Helper()
	image := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			image.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 180, A: 255})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := jpeg.Encode(file, image, nil); err != nil {
		t.Fatal(err)
	}
}

func TestThumbnailCacheGeneratesSquareThumbnail(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.jpg")
	writeTestJPEG(t, source, 800, 400)

	cache, err := newThumbnailCache(filepath.Join(dir, "cache"), 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	result, err := cache.get(source, info, 160)
	if err != nil {
		t.Fatal(err)
	}
	if result.contentType != "image/jpeg" {
		t.Fatalf("content type = %q, want image/jpeg", result.contentType)
	}
	file, err := os.Open(result.path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 160 || config.Height != 160 {
		t.Fatalf("thumbnail dimensions = %dx%d, want 160x160", config.Width, config.Height)
	}

	hit, err := cache.get(source, info, 160)
	if err != nil {
		t.Fatal(err)
	}
	if hit.path != result.path || hit.etag != result.etag {
		t.Fatalf("cache hit = %+v, want %+v", hit, result)
	}
}

func TestThumbnailCacheEvictsLeastRecentlyUsedFile(t *testing.T) {
	dir := t.TempDir()
	cache, err := newThumbnailCache(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "old.jpg")
	newer := filepath.Join(dir, "newer.png")
	if err := os.WriteFile(old, make([]byte, 8), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, make([]byte, 8), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(old, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := cache.evict(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old thumbnail should be evicted, stat err = %v", err)
	}
	if _, err := os.Stat(newer); err != nil {
		t.Fatalf("newer thumbnail should remain: %v", err)
	}
}

func TestThumbnailEndpointUsesETag(t *testing.T) {
	root := t.TempDir()
	writeTestJPEG(t, filepath.Join(root, "photo.jpg"), 32, 32)
	s, err := newServer([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	s.thumbs, err = newThumbnailCache(filepath.Join(t.TempDir(), "cache"), 4<<20)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/thumbnail?root=0&path=photo.jpg&size=160", nil)
	response := httptest.NewRecorder()
	s.routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("thumbnail status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("content type = %q, want image/jpeg", got)
	}
	etag := response.Header().Get("ETag")
	if etag == "" {
		t.Fatal("thumbnail response has no ETag")
	}

	cachedRequest := httptest.NewRequest(http.MethodGet, "/api/thumbnail?root=0&path=photo.jpg&size=160", nil)
	cachedRequest.Header.Set("If-None-Match", etag)
	cachedResponse := httptest.NewRecorder()
	s.routes().ServeHTTP(cachedResponse, cachedRequest)
	if cachedResponse.Code != http.StatusNotModified {
		t.Fatalf("conditional thumbnail status = %d, want 304", cachedResponse.Code)
	}
}
