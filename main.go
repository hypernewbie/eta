package main

import (
	"context"
	"crypto/rand"
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
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hypernewbie/eta/internal/bindaddr"
	"github.com/hypernewbie/eta/internal/diskcache"
	"github.com/hypernewbie/eta/internal/fileops"
	"github.com/hypernewbie/eta/internal/hostid"
	"github.com/hypernewbie/eta/internal/peers"
	"github.com/hypernewbie/eta/internal/rangecache"
	"github.com/hypernewbie/eta/internal/remotefile"
	"github.com/hypernewbie/eta/internal/transfer"
	"github.com/hypernewbie/eta/internal/uistate"
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
	roots       []root
	web         fs.FS
	thumbs      *thumbnailCache
	identity    hostid.Identity
	state       *uistate.Store
	peers       *peers.Store
	remoteCache *diskcache.Cache
	hotRanges   *rangecache.Cache
	transfers   *transfer.Store
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
	thumbnailCacheDir := flag.String("thumbnail-cache-dir", "", "directory for generated image thumbnails (default: user cache directory)")
	thumbnailCacheSize := flag.String("thumbnail-cache-size", "2GB", "maximum thumbnail cache size (for example: 512MB, 2GB)")
	identityFile := flag.String("identity-file", "", "persistent host identity file (default: user config directory)")
	accent := flag.String("accent", "", "host accent override (one of Eta's Phi accent names)")
	stateFile := flag.String("state-file", "", "persistent UI state file (default: user config directory)")
	peersFile := flag.String("peers-file", "", "explicit coordinator peer inventory file (default: user config directory)")
	remoteCacheDir := flag.String("remote-cache-dir", "", "directory for cached remote byte ranges (default: user cache directory)")
	remoteCacheSize := flag.String("remote-cache-size", "4GB", "maximum remote byte-range cache size")
	hotRangeCacheSize := flag.String("hot-range-cache-size", "64MB", "maximum RAM used by hot remote ranges")
	transferDir := flag.String("transfer-dir", "", "directory for resumable transfer staging (default: user cache directory)")
	flag.Var(&roots, "root", "directory to expose (repeatable; defaults to the current directory)")
	flag.Parse()

	if len(roots) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
		roots = append(roots, cwd)
	}

	identityPath := *identityFile
	if identityPath == "" {
		defaultPath, err := hostid.DefaultPath()
		if err != nil {
			log.Fatal(err)
		}
		identityPath = defaultPath
	}
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}
	identity, err := hostid.LoadOrCreate(identityPath, hostname, *accent)
	if err != nil {
		log.Fatal(err)
	}

	s, err := newServer(roots)
	if err != nil {
		log.Fatal(err)
	}
	s.identity = identity
	statePath := *stateFile
	if statePath == "" {
		statePath, err = uistate.DefaultPath()
		if err != nil {
			log.Fatal(err)
		}
	}
	s.state = uistate.New(statePath)
	peerPath := *peersFile
	if peerPath == "" {
		peerPath, err = peers.DefaultPath()
		if err != nil {
			log.Fatal(err)
		}
	}
	s.peers = peers.New(peerPath)
	remoteDir := *remoteCacheDir
	if remoteDir == "" {
		remoteDir, err = diskcache.DefaultPath()
		if err != nil {
			log.Fatal(err)
		}
	}
	remoteBytes, err := parseCacheBytes(*remoteCacheSize)
	if err != nil {
		log.Fatal(err)
	}
	s.remoteCache, err = diskcache.New(remoteDir, remoteBytes)
	if err != nil {
		log.Fatal(err)
	}
	hotRangeBytes, err := parseCacheBytes(*hotRangeCacheSize)
	if err != nil {
		log.Fatal(err)
	}
	s.hotRanges = rangecache.New(hotRangeBytes)
	stageDir := *transferDir
	if stageDir == "" {
		base, cacheErr := os.UserCacheDir()
		if cacheErr != nil {
			log.Fatal(cacheErr)
		}
		stageDir = filepath.Join(base, "eta", "transfers")
	}
	s.transfers, err = transfer.NewStore(stageDir)
	if err != nil {
		log.Fatal(err)
	}
	cacheDir := *thumbnailCacheDir
	if cacheDir == "" {
		cacheDir, err = defaultThumbnailCacheDir()
		if err != nil {
			log.Fatal(err)
		}
	}
	cacheBytes, err := parseCacheBytes(*thumbnailCacheSize)
	if err != nil {
		log.Fatal(err)
	}
	s.thumbs, err = newThumbnailCache(cacheDir, cacheBytes)
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
	s := &server{web: web, identity: hostid.For("test-host", "eta")}
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
	mux.HandleFunc("GET /api/identity", s.handleIdentity)
	mux.HandleFunc("GET /api/state", s.handleStateGet)
	mux.HandleFunc("POST /api/state", s.handleStatePut)
	mux.HandleFunc("PUT /api/state", s.handleStatePut)
	mux.HandleFunc("GET /api/roots", s.handleRoots)
	mux.HandleFunc("GET /api/peers", s.handlePeers)
	mux.HandleFunc("POST /api/transfers", s.handleTransferCreate)
	mux.HandleFunc("POST /api/transfers/send", s.handleTransferSend)
	mux.HandleFunc("GET /api/transfers/{id}", s.handleTransferStatus)
	mux.HandleFunc("PUT /api/transfers/{id}/chunks/{chunk}", s.handleTransferChunk)
	mux.HandleFunc("POST /api/transfers/{id}/finalize", s.handleTransferFinalize)
	mux.HandleFunc("GET /api/remote/roots", s.handleRemoteRoots)
	mux.HandleFunc("GET /api/remote/list", s.handleRemoteList)
	mux.HandleFunc("GET /api/remote/file", s.handleRemoteFile)
	mux.HandleFunc("GET /api/remote/preview", s.handleRemotePreview)
	mux.HandleFunc("GET /api/remote/thumbnail", s.handleRemoteThumbnail)
	mux.HandleFunc("POST /api/peers", s.handlePeerAdd)
	mux.HandleFunc("DELETE /api/peers", s.handlePeerDelete)
	mux.HandleFunc("POST /api/rename", s.handleRename)
	mux.HandleFunc("POST /api/delete", s.handleDelete)
	mux.HandleFunc("GET /api/list", s.handleList)
	mux.HandleFunc("GET /api/preview", s.handlePreview)
	mux.HandleFunc("GET /api/thumbnail", s.handleThumbnail)
	mux.HandleFunc("GET /api/file", s.handleFile)
	mux.Handle("/", http.FileServer(http.FS(s.web)))
	return securityHeaders(mux)
}

func (s *server) handleIdentity(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.identity)
}

func (s *server) handleStateGet(w http.ResponseWriter, _ *http.Request) {
	if s.state == nil {
		writeError(w, errors.New("UI state is unavailable"))
		return
	}
	state, err := s.state.Load()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *server) handleStatePut(w http.ResponseWriter, r *http.Request) {
	if s.state == nil {
		writeError(w, errors.New("UI state is unavailable"))
		return
	}
	defer r.Body.Close()
	var state uistate.State
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&state); err != nil {
		writeError(w, fmt.Errorf("decode UI state: %w", err))
		return
	}
	if err := s.state.Save(state); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleTransferSend(w http.ResponseWriter, r *http.Request) {
	if s.peers == nil {
		writeError(w, errors.New("peer inventory is unavailable"))
		return
	}
	var request struct {
		Peer            string `json:"peer"`
		SourceRoot      int    `json:"sourceRoot"`
		SourcePath      string `json:"sourcePath"`
		DestinationRoot int    `json:"destinationRoot"`
		DestinationPath string `json:"destinationPath"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	peer, found, err := s.peers.Find(request.Peer)
	if err != nil {
		writeError(w, err)
		return
	}
	if !found {
		writeError(w, errors.New("unknown peer"))
		return
	}
	sourceRequest, _ := http.NewRequest(http.MethodGet, "/?"+url.Values{"root": {strconv.Itoa(request.SourceRoot)}, "path": {request.SourcePath}}.Encode(), nil)
	_, source, _, err := s.target(sourceRequest)
	if err != nil {
		writeError(w, err)
		return
	}
	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, errors.New("source is not a regular file"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	id, err := transfer.SendFile(ctx, &http.Client{Timeout: 30 * time.Second}, peer.URL, request.DestinationRoot, request.DestinationPath, source)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *server) handleTransferCreate(w http.ResponseWriter, r *http.Request) {
	if s.transfers == nil {
		writeError(w, errors.New("transfer staging is unavailable"))
		return
	}
	var request struct {
		Root     int               `json:"root"`
		Path     string            `json:"path"`
		Manifest transfer.Manifest `json:"manifest"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	if _, err := s.transferDestination(request.Root, request.Path); err != nil {
		writeError(w, err)
		return
	}
	if request.Manifest.ChunkSize <= 0 || request.Manifest.Size < 0 || len(request.Manifest.Chunks) == 0 || len(request.Manifest.Chunks) > 1<<16 {
		writeError(w, errors.New("invalid transfer manifest"))
		return
	}
	id, err := newTransferID()
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.transfers.Open(id, request.Manifest); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}
func (s *server) handleTransferStatus(w http.ResponseWriter, r *http.Request) {
	if s.transfers == nil {
		writeError(w, errors.New("transfer staging is unavailable"))
		return
	}
	missing, err := s.transfers.Missing(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"missing": missing})
}
func (s *server) handleTransferChunk(w http.ResponseWriter, r *http.Request) {
	if s.transfers == nil {
		writeError(w, errors.New("transfer staging is unavailable"))
		return
	}
	index, err := strconv.Atoi(r.PathValue("chunk"))
	if err != nil {
		writeError(w, errors.New("invalid chunk index"))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, transfer.DefaultChunkSize+1))
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.transfers.Write(r.PathValue("id"), index, body); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *server) handleTransferFinalize(w http.ResponseWriter, r *http.Request) {
	if s.transfers == nil {
		writeError(w, errors.New("transfer staging is unavailable"))
		return
	}
	var request struct {
		Root int    `json:"root"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	destination, err := s.transferDestination(request.Root, request.Path)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.transfers.Finalize(r.PathValue("id"), destination); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func newTransferID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", raw), nil
}
func (s *server) transferDestination(rootID int, raw string) (string, error) {
	if rootID < 0 || rootID >= len(s.roots) {
		return "", errors.New("unknown root")
	}
	relative := filepath.Clean(filepath.FromSlash(raw))
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("path is outside the selected root")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(filepath.Join(s.roots[rootID].Path, relative)))
	if err != nil {
		return "", err
	}
	within, err := filepath.Rel(s.roots[rootID].Path, parent)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", errors.New("path resolves outside the selected root")
	}
	return filepath.Join(parent, filepath.Base(relative)), nil
}

func (s *server) handleRemoteRoots(w http.ResponseWriter, r *http.Request) {
	s.proxyPeer(w, r, "/api/roots")
}
func (s *server) handleRemoteFile(w http.ResponseWriter, r *http.Request) {
	s.proxyPeer(w, r, "/api/file")
}
func (s *server) handleRemotePreview(w http.ResponseWriter, r *http.Request) {
	s.proxyPeer(w, r, "/api/preview")
}
func (s *server) handleRemoteThumbnail(w http.ResponseWriter, r *http.Request) {
	s.proxyPeer(w, r, "/api/thumbnail")
}
func (s *server) handleRemoteList(w http.ResponseWriter, r *http.Request) {
	s.proxyPeer(w, r, "/api/list")
}
func (s *server) proxyPeer(w http.ResponseWriter, r *http.Request, route string) {
	if s.peers == nil {
		writeError(w, errors.New("peer inventory is unavailable"))
		return
	}
	peer, found, err := s.peers.Find(r.URL.Query().Get("peer"))
	if err != nil {
		writeError(w, err)
		return
	}
	if !found {
		writeError(w, errors.New("unknown peer"))
		return
	}
	if route == "/api/file" && s.remoteCache != nil && r.Header.Get("Range") != "" {
		if s.proxyCachedPeerRange(w, r, peer) {
			return
		}
	}
	remoteURL, err := url.Parse(peer.URL)
	if err != nil {
		writeError(w, err)
		return
	}
	remoteURL.Path = strings.TrimSuffix(remoteURL.Path, "/") + route
	query := remoteURL.Query()
	query.Set("root", r.URL.Query().Get("root"))
	query.Set("path", r.URL.Query().Get("path"))
	for _, key := range []string{"size", "download", "embed"} {
		if value := r.URL.Query().Get(key); value != "" {
			query.Set(key, value)
		}
	}
	remoteURL.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, remoteURL.String(), nil)
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		request.Header.Set("Range", rangeHeader)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		writeError(w, err)
		return
	}
	defer response.Body.Close()
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if contentRange := response.Header.Get("Content-Range"); contentRange != "" {
		w.Header().Set("Content-Range", contentRange)
	}
	if length := response.Header.Get("Content-Length"); length != "" {
		w.Header().Set("Content-Length", length)
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

const maxCachedRemoteRange = 8 << 20

// proxyCachedPeerRange handles a single explicit byte range. Unsupported or
// oversized ranges return false and deliberately fall through to streaming.
func (s *server) proxyCachedPeerRange(w http.ResponseWriter, r *http.Request, peer peers.Peer) bool {
	match := regexp.MustCompile(`^bytes=(\d+)-(\d+)$`).FindStringSubmatch(r.Header.Get("Range"))
	if match == nil {
		return false
	}
	start, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return false
	}
	end, err := strconv.ParseInt(match[2], 10, 64)
	if err != nil || end < start || end-start+1 > maxCachedRemoteRange {
		return false
	}
	rootID, err := strconv.Atoi(r.URL.Query().Get("root"))
	if err != nil {
		writeError(w, errors.New("invalid root"))
		return true
	}
	source := &remotefile.HTTPSource{BaseURL: peer.URL, Root: rootID, Client: &http.Client{Timeout: 30 * time.Second}}
	body, info, err := remotefile.ReadHotCachedRange(r.Context(), s.hotRanges, s.remoteCache, source, r.URL.Query().Get("path"), start, end-start+1)
	if err != nil {
		writeError(w, err)
		return true
	}
	if len(body) == 0 && info.Size > 0 {
		writeError(w, errors.New("empty remote range"))
		return true
	}
	last := start + int64(len(body)) - 1
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(int64(len(body)), 10))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, last, info.Size))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(body)
	return true
}

func (s *server) handlePeers(w http.ResponseWriter, _ *http.Request) {
	if s.peers == nil {
		writeError(w, errors.New("peer inventory is unavailable"))
		return
	}
	items, err := s.peers.List()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
func (s *server) handlePeerAdd(w http.ResponseWriter, r *http.Request) {
	if s.peers == nil {
		writeError(w, errors.New("peer inventory is unavailable"))
		return
	}
	var peer peers.Peer
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&peer); err != nil {
		writeError(w, err)
		return
	}
	identity, err := probePeer(r.Context(), peer.URL)
	if err != nil {
		writeError(w, fmt.Errorf("probe peer identity: %w", err))
		return
	}
	peer.ID, peer.Name, peer.Accent, peer.Glyph = identity.ID, identity.Hostname, identity.Accent, identity.Glyph
	if err := s.peers.Add(peer); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, peer)
}

type peerIdentity struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Accent   string `json:"accent"`
	Glyph    string `json:"glyph"`
}

func probePeer(ctx context.Context, rawURL string) (peerIdentity, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(rawURL, "/")+"/api/identity", nil)
	if err != nil {
		return peerIdentity{}, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return peerIdentity{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return peerIdentity{}, fmt.Errorf("%s", response.Status)
	}
	var identity peerIdentity
	if err := json.NewDecoder(response.Body).Decode(&identity); err != nil {
		return peerIdentity{}, err
	}
	if identity.ID == "" || identity.Hostname == "" || identity.Accent == "" || identity.Glyph == "" {
		return peerIdentity{}, errors.New("incomplete identity")
	}
	return identity, nil
}
func (s *server) handlePeerDelete(w http.ResponseWriter, r *http.Request) {
	if s.peers == nil {
		writeError(w, errors.New("peer inventory is unavailable"))
		return
	}
	if err := s.peers.Remove(r.URL.Query().Get("url")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleRename(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Root   int    `json:"root"`
		Path   string `json:"path"`
		Target string `json:"target"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	if request.Root < 0 || request.Root >= len(s.roots) {
		writeError(w, errors.New("invalid root"))
		return
	}
	operations, err := fileops.New(s.roots[request.Root].Path)
	if err == nil {
		err = operations.Rename(request.Path, request.Target)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleDelete(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Root int    `json:"root"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	if request.Root < 0 || request.Root >= len(s.roots) {
		writeError(w, errors.New("invalid root"))
		return
	}
	operations, err := fileops.New(s.roots[request.Root].Path)
	if err == nil {
		err = operations.Delete(request.Path)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

func (s *server) handleThumbnail(w http.ResponseWriter, r *http.Request) {
	if s.thumbs == nil {
		writeError(w, errors.New("thumbnail cache is unavailable"))
		return
	}
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
	edge, err := thumbnailSize(r.URL.Query().Get("size"))
	if err != nil {
		writeError(w, err)
		return
	}
	thumbnail, err := s.thumbs.get(target, info, edge)
	if err != nil {
		writeError(w, err)
		return
	}
	etag := `"` + thumbnail.etag + `"`
	w.Header().Set("Cache-Control", "private, max-age=600")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	file, err := os.Open(thumbnail.path)
	if err != nil {
		writeError(w, err)
		return
	}
	defer file.Close()
	cachedInfo, err := file.Stat()
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", thumbnail.contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(cachedInfo.Size(), 10))
	http.ServeContent(w, r, filepath.Base(thumbnail.path), cachedInfo.ModTime(), file)
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
	if contentType := mediaType(info.Name()); contentType != "" {
		w.Header().Set("Content-Type", contentType)
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

func mediaType(name string) string {
	// Go's extension table can be platform dependent. These ensure browser
	// media controls receive the expected type on a minimal NAS install.
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp3":
		return "audio/mpeg"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".opus":
		return "audio/ogg; codecs=opus"
	case ".wav":
		return "audio/wav"
	case ".flac":
		return "audio/flac"
	case ".m4a":
		return "audio/mp4"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".ogv":
		return "video/ogg"
	case ".m4v":
		return "video/x-m4v"
	}
	return mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
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
		if r.URL.Path != "/api/file" || r.URL.Query().Get("embed") != "1" {
			w.Header().Set("X-Frame-Options", "DENY")
		}
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
	var apiErr *apiError
	switch {
	case errors.As(err, &apiErr):
		status = apiErr.status
	case errors.Is(err, fs.ErrNotExist):
		status = http.StatusNotFound
	case strings.Contains(err.Error(), "root") || strings.Contains(err.Error(), "regular") || strings.Contains(err.Error(), "preview"):
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
