package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hypernewbie/eta/internal/bindaddr"
)

//go:embed all:web
var embeddedWeb embed.FS

const previewLimit = 512 << 10

type rootsFlag []string

func (r *rootsFlag) String() string { return strings.Join(*r, ", ") }
func (r *rootsFlag) Set(value string) error {
	*r = append(*r, value)
	return nil
}

type root struct {
	Name string
	Path string
}

type server struct {
	roots []root
	web   fs.FS
}

type entry struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Kind     string    `json:"kind"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

func main() {
	var roots rootsFlag
	port := flag.Int("port", 7080, "HTTP port")
	ip := flag.String("ip", "lan", `bind address; "lan" matches Phi: loopback + private LAN + Tailnet`)
	flag.Var(&roots, "root", "directory to expose (repeatable; defaults to the current directory)")
	flag.Parse()

	if len(roots) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
		roots = append(roots, cwd)
	}

	s, err := newServer(roots)
	if err != nil {
		log.Fatal(err)
	}
	listeners, err := listen(*ip, *port)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()

	log.Printf("eta viewer — %d root(s), read-only", len(s.roots))
	for _, listener := range listeners {
		log.Printf("serving http://%s", listener.Addr())
	}

	httpServer := &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	}()

	errCh := make(chan error, len(listeners))
	for _, listener := range listeners {
		go func(listener net.Listener) { errCh <- httpServer.Serve(listener) }(listener)
	}
	for range listeners {
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server: %v", err)
		}
	}
}

func newServer(paths []string) (*server, error) {
	web, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		return nil, fmt.Errorf("load embedded web assets: %w", err)
	}
	s := &server{web: web}
	seen := map[string]bool{}
	for _, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve root %q: %w", path, err)
		}
		realPath, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, fmt.Errorf("resolve root %q: %w", path, err)
		}
		info, err := os.Stat(realPath)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("root %q is not a directory", path)
		}
		if seen[realPath] {
			continue
		}
		seen[realPath] = true
		s.roots = append(s.roots, root{Name: filepath.Base(realPath), Path: realPath})
	}
	return s, nil
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/roots", s.handleRoots)
	mux.HandleFunc("GET /api/list", s.handleList)
	mux.HandleFunc("GET /api/preview", s.handlePreview)
	mux.HandleFunc("GET /api/file", s.handleFile)
	mux.Handle("/", http.FileServer(http.FS(s.web)))
	return securityHeaders(mux)
}

func (s *server) handleRoots(w http.ResponseWriter, _ *http.Request) {
	type publicRoot struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	out := make([]publicRoot, len(s.roots))
	for i, root := range s.roots {
		out[i] = publicRoot{ID: i, Name: root.Name}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleList(w http.ResponseWriter, r *http.Request) {
	rootID, target, relative, err := s.target(r)
	if err != nil {
		writeError(w, err)
		return
	}
	info, err := os.Stat(target)
	if err != nil {
		writeError(w, err)
		return
	}
	if !info.IsDir() {
		writeJSON(w, http.StatusOK, map[string]any{"root": rootID, "path": relative, "entry": makeEntry(filepath.Base(target), relative, info)})
		return
	}
	items, err := os.ReadDir(target)
	if err != nil {
		writeError(w, err)
		return
	}
	entries := make([]entry, 0, len(items))
	for _, item := range items {
		info, err := item.Info()
		if err != nil {
			continue
		}
		childPath := item.Name()
		if relative != "" {
			childPath = filepath.ToSlash(filepath.Join(relative, childPath))
		}
		entries = append(entries, makeEntry(item.Name(), childPath, info))
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind == "directory" && entries[j].Kind != "directory" {
			return true
		}
		if entries[i].Kind != "directory" && entries[j].Kind == "directory" {
			return false
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	writeJSON(w, http.StatusOK, map[string]any{"root": rootID, "path": relative, "entries": entries})
}

func (s *server) handlePreview(w http.ResponseWriter, r *http.Request) {
	_, target, _, err := s.target(r)
	if err != nil {
		writeError(w, err)
		return
	}
	info, err := os.Stat(target)
	if err != nil {
		writeError(w, err)
		return
	}
	if !info.Mode().IsRegular() {
		writeError(w, errors.New("preview is only available for regular files"))
		return
	}
	file, err := os.Open(target)
	if err != nil {
		writeError(w, err)
		return
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, previewLimit+1))
	if err != nil {
		writeError(w, err)
		return
	}
	truncated := len(contents) > previewLimit
	if truncated {
		contents = contents[:previewLimit]
	}
	if strings.IndexByte(string(contents), 0) >= 0 {
		writeJSON(w, http.StatusOK, map[string]any{"binary": true, "truncated": truncated})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": string(contents), "truncated": truncated, "binary": false})
}

func (s *server) handleFile(w http.ResponseWriter, r *http.Request) {
	_, target, _, err := s.target(r)
	if err != nil {
		writeError(w, err)
		return
	}
	file, err := os.Open(target)
	if err != nil {
		writeError(w, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, errors.New("file is not a regular file"))
		return
	}
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": info.Name()}))
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (s *server) target(r *http.Request) (int, string, string, error) {
	rootID, err := strconv.Atoi(r.URL.Query().Get("root"))
	if err != nil || rootID < 0 || rootID >= len(s.roots) {
		return 0, "", "", errors.New("unknown root")
	}
	relative := filepath.Clean(filepath.FromSlash(r.URL.Query().Get("path")))
	if relative == "." {
		relative = ""
	}
	if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return 0, "", "", errors.New("path is outside the selected root")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(s.roots[rootID].Path, relative))
	if err != nil {
		return 0, "", "", err
	}
	withinRoot, err := filepath.Rel(s.roots[rootID].Path, resolved)
	if err != nil || withinRoot == ".." || strings.HasPrefix(withinRoot, ".."+string(filepath.Separator)) {
		return 0, "", "", errors.New("path resolves outside the selected root")
	}
	return rootID, resolved, filepath.ToSlash(relative), nil
}

func makeEntry(name, path string, info fs.FileInfo) entry {
	kind := "file"
	if info.IsDir() {
		kind = "directory"
	}
	return entry{Name: name, Path: filepath.ToSlash(path), Kind: kind, Size: info.Size(), Modified: info.ModTime()}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, fs.ErrNotExist) {
		status = http.StatusNotFound
	}
	if strings.Contains(err.Error(), "root") || strings.Contains(err.Error(), "regular") || strings.Contains(err.Error(), "preview") {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func listen(ip string, port int) ([]net.Listener, error) {
	var addresses []string
	if ip == "lan" {
		for _, addr := range bindaddr.Detect() {
			addresses = append(addresses, net.JoinHostPort(addr.IP.String(), strconv.Itoa(port)))
		}
	} else {
		addresses = []string{net.JoinHostPort(ip, strconv.Itoa(port))}
	}
	listeners := make([]net.Listener, 0, len(addresses))
	for _, address := range addresses {
		listener, err := listenWithRetry(address, 5*time.Second, 100*time.Millisecond)
		if err != nil {
			log.Printf("bind %s failed: %v", address, err)
			continue
		}
		listeners = append(listeners, listener)
	}
	if len(listeners) == 0 {
		return nil, fmt.Errorf("could not bind any address on port %d", port)
	}
	return listeners, nil
}

func listenWithRetry(address string, maxWait, interval time.Duration) (net.Listener, error) {
	deadline := time.Now().Add(maxWait)
	for {
		listener, err := net.Listen("tcp", address)
		if err == nil {
			return listener, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(interval)
	}
}
