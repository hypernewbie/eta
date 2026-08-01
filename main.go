package main

import (
	"bytes"
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
	"github.com/hypernewbie/eta/internal/terminal"
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
	roots        []root
	web          fs.FS
	thumbs       *thumbnailCache
	identity     hostid.Identity
	state        *uistate.Store
	peers        *peers.Store
	remoteCache  *diskcache.Cache
	hotRanges    *rangecache.Cache
	transfers    *transfer.Store
	transferJobs *transfer.Jobs
	treeStores   []*transfer.TreeStore
	terminals    *terminal.Manager
	advertiseURL string
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
	// Cache defaults were consciously chosen, not silently accepted
	// (journal 2026-07-30 "Known gaps" #5). Rough budget per coordinator
	// instance: 64MB RAM hot ranges, 4GB on-disk remote byte ranges
	// (requesting-side cache, sized for re-browsing large remote media
	// without refetch), 2GB on-disk thumbnails (host-generated per source).
	// Override per-deployment with --remote-cache-size / --hot-range-cache-size
	// / --thumbnail-cache-size. Serving-host hot ranges are not enabled by
	// default; add only if benchmarks justify them.
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
	advertiseURL := flag.String("advertise-url", "", "public http(s) URL peers use to send files here (default: request host)")
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
	s.advertiseURL = strings.TrimSuffix(*advertiseURL, "/")
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
	s.transferJobs, err = transfer.NewPersistentJobs(filepath.Join(stageDir, "jobs.json"))
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
	s := &server{web: web, identity: hostid.For("test-host", "eta"), terminals: terminal.NewManager(), transferJobs: transfer.NewJobs()}
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
	s.treeStores = make([]*transfer.TreeStore, len(s.roots))
	for i, r := range s.roots {
		s.treeStores[i] = transfer.NewTreeStore(r.Path)
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
	mux.HandleFunc("POST /api/terminals", s.handleTerminalStart)
	mux.HandleFunc("GET /api/terminals/{id}", s.handleTerminalOutput)
	mux.HandleFunc("POST /api/terminals/{id}/input", s.handleTerminalInput)
	mux.HandleFunc("POST /api/terminals/{id}/resize", s.handleTerminalResize)
	mux.HandleFunc("DELETE /api/terminals/{id}", s.handleTerminalClose)
	mux.HandleFunc("POST /api/remote/terminals", s.handleRemoteTerminalStart)
	mux.HandleFunc("GET /api/remote/terminals/{id}", s.handleRemoteTerminalOutput)
	mux.HandleFunc("POST /api/remote/terminals/{id}/input", s.handleRemoteTerminalInput)
	mux.HandleFunc("POST /api/remote/terminals/{id}/resize", s.handleRemoteTerminalResize)
	mux.HandleFunc("DELETE /api/remote/terminals/{id}", s.handleRemoteTerminalClose)
	mux.HandleFunc("POST /api/transfers", s.handleTransferCreate)
	mux.HandleFunc("POST /api/transfers/send", s.handleTransferSend)
	mux.HandleFunc("POST /api/remote/transfers/send", s.handleRemoteTransferSend)
	mux.HandleFunc("GET /api/remote/transfer-jobs", s.handleRemoteTransferJob)
	mux.HandleFunc("POST /api/remote/delete", s.handleRemoteDelete)
	mux.HandleFunc("GET /api/transfer-jobs", s.handleTransferJobs)
	mux.HandleFunc("GET /api/transfer-jobs/{id}", s.handleTransferJob)
	mux.HandleFunc("GET /api/transfers/{id}", s.handleTransferStatus)
	mux.HandleFunc("PUT /api/transfers/{id}/chunks/{chunk}", s.handleTransferChunk)
	mux.HandleFunc("POST /api/transfers/{id}/finalize", s.handleTransferFinalize)
	mux.HandleFunc("POST /api/transfer-trees", s.handleTransferTreeCreate)
	mux.HandleFunc("GET /api/transfer-trees/{id}", s.handleTransferTreeStatus)
	mux.HandleFunc("POST /api/transfer-trees/{id}/commit", s.handleTransferTreeCommit)
	mux.HandleFunc("DELETE /api/transfer-trees/{id}", s.handleTransferTreeAbort)
	mux.HandleFunc("GET /api/remote/roots", s.handleRemoteRoots)
	mux.HandleFunc("GET /api/remote/list", s.handleRemoteList)
	mux.HandleFunc("GET /api/remote/file", s.handleRemoteFile)
	mux.HandleFunc("GET /api/remote/preview", s.handleRemotePreview)
	mux.HandleFunc("GET /api/remote/thumbnail", s.handleRemoteThumbnail)
	mux.HandleFunc("POST /api/peers", s.handlePeerAdd)
	mux.HandleFunc("DELETE /api/peers", s.handlePeerDelete)
	mux.HandleFunc("POST /api/directories", s.handleDirectoryCreate)
	mux.HandleFunc("POST /api/copy", s.handleCopy)
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

func (s *server) handleTerminalStart(w http.ResponseWriter, r *http.Request) {
	if s.terminals == nil {
		writeError(w, errors.New("terminal service is unavailable"))
		return
	}
	var request struct {
		Root    int    `json:"root"`
		Path    string `json:"path"`
		Columns uint16 `json:"columns"`
		Rows    uint16 `json:"rows"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	targetRequest, _ := http.NewRequest(http.MethodGet, "/?"+url.Values{"root": {strconv.Itoa(request.Root)}, "path": {request.Path}}.Encode(), nil)
	_, target, _, err := s.target(targetRequest)
	if err != nil {
		writeError(w, err)
		return
	}
	if info, err := os.Stat(target); err != nil {
		writeError(w, err)
		return
	} else if !info.IsDir() {
		target = filepath.Dir(target)
	}
	id, err := s.terminals.Start(target, request.Columns, request.Rows)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}
func (s *server) handleTerminalOutput(w http.ResponseWriter, r *http.Request) {
	s.terminalSession(w, r, func(session *terminal.Session) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		output, next, closed := session.Output(offset)
		writeJSON(w, http.StatusOK, map[string]any{"output": string(output), "offset": next, "closed": closed})
	})
}
func (s *server) handleTerminalInput(w http.ResponseWriter, r *http.Request) {
	s.terminalSession(w, r, func(session *terminal.Session) {
		var request struct {
			Input string `json:"input"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
			writeError(w, err)
			return
		}
		if err := session.Input([]byte(request.Input)); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
func (s *server) handleTerminalResize(w http.ResponseWriter, r *http.Request) {
	s.terminalSession(w, r, func(session *terminal.Session) {
		var request struct {
			Columns uint16 `json:"columns"`
			Rows    uint16 `json:"rows"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
			writeError(w, err)
			return
		}
		if err := session.Resize(request.Columns, request.Rows); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
func (s *server) handleTerminalClose(w http.ResponseWriter, r *http.Request) {
	if s.terminals == nil {
		writeError(w, errors.New("terminal service is unavailable"))
		return
	}
	s.terminals.Close(r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}
func (s *server) handleRemoteTerminalStart(w http.ResponseWriter, r *http.Request) {
	s.proxyRemoteTerminal(w, r, "/api/terminals")
}
func (s *server) handleRemoteTerminalOutput(w http.ResponseWriter, r *http.Request) {
	s.proxyRemoteTerminal(w, r, "/api/terminals/"+url.PathEscape(r.PathValue("id")))
}
func (s *server) handleRemoteTerminalInput(w http.ResponseWriter, r *http.Request) {
	s.proxyRemoteTerminal(w, r, "/api/terminals/"+url.PathEscape(r.PathValue("id"))+"/input")
}
func (s *server) handleRemoteTerminalResize(w http.ResponseWriter, r *http.Request) {
	s.proxyRemoteTerminal(w, r, "/api/terminals/"+url.PathEscape(r.PathValue("id"))+"/resize")
}
func (s *server) handleRemoteTerminalClose(w http.ResponseWriter, r *http.Request) {
	s.proxyRemoteTerminal(w, r, "/api/terminals/"+url.PathEscape(r.PathValue("id")))
}
func (s *server) proxyRemoteTerminal(w http.ResponseWriter, r *http.Request, route string) {
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
	remote, err := url.Parse(peer.URL)
	if err != nil {
		writeError(w, err)
		return
	}
	remote.Path = strings.TrimSuffix(remote.Path, "/") + route
	if offset := r.URL.Query().Get("offset"); offset != "" {
		query := remote.Query()
		query.Set("offset", offset)
		remote.RawQuery = query.Encode()
	}
	request, err := http.NewRequestWithContext(r.Context(), r.Method, remote.String(), http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		writeError(w, err)
		return
	}
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		request.Header.Set("Content-Type", contentType)
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
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (s *server) terminalSession(w http.ResponseWriter, r *http.Request, action func(*terminal.Session)) {
	if s.terminals == nil {
		writeError(w, errors.New("terminal service is unavailable"))
		return
	}
	session, found := s.terminals.Get(r.PathValue("id"))
	if !found {
		writeError(w, errors.New("unknown terminal"))
		return
	}
	action(session)
}

func (s *server) handleTransferSend(w http.ResponseWriter, r *http.Request) {
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
	// A managed source may not itself keep the coordinator's peer inventory.
	// Its caller supplies the direct destination URL; the Tailnet/LAN boundary
	// is the authorization boundary for this capability.
	peer := peers.Peer{URL: strings.TrimSuffix(request.Peer, "/")}
	if parsed, err := url.Parse(peer.URL); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		writeError(w, errors.New("invalid destination peer"))
		return
	}
	sourceRequest, _ := http.NewRequest(http.MethodGet, "/?"+url.Values{"root": {strconv.Itoa(request.SourceRoot)}, "path": {request.SourcePath}}.Encode(), nil)
	_, source, _, err := s.target(sourceRequest)
	if err != nil {
		writeError(w, err)
		return
	}
	info, err := os.Stat(source)
	if err != nil || (!info.Mode().IsRegular() && !info.IsDir()) {
		writeError(w, errors.New("source is not a regular file or directory"))
		return
	}
	if s.transferJobs == nil {
		s.transferJobs = transfer.NewJobs()
	}
	var tree transfer.Tree
	totalChunks := 0
	if info.IsDir() {
		tree, err = transfer.BuildTree(source)
		if err != nil {
			writeError(w, err)
			return
		}
		totalChunks = tree.TotalChunks
	} else {
		manifestFile, openErr := os.Open(source)
		if openErr != nil {
			writeError(w, openErr)
			return
		}
		manifest, manifestErr := transfer.BuildManifest(manifestFile, transfer.DefaultChunkSize)
		_ = manifestFile.Close()
		if manifestErr != nil {
			writeError(w, manifestErr)
			return
		}
		totalChunks = len(manifest.Chunks)
	}
	job := s.transferJobs.StartNamed(totalChunks, transfer.SourceName(source))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		client := &http.Client{Timeout: 30 * time.Second}
		var transferErr error
		if info.IsDir() {
			transferErr = transfer.SendTreeWithProgress(ctx, client, peer.URL, request.DestinationRoot, request.DestinationPath, source, tree, func(completed, _ int) { s.transferJobs.Progress(job.ID, completed) })
		} else {
			_, transferErr = transfer.SendFileWithProgress(ctx, client, peer.URL, request.DestinationRoot, request.DestinationPath, source, func(completed, _ int) { s.transferJobs.Progress(job.ID, completed) })
		}
		s.transferJobs.Finish(job.ID, transferErr)
	}()
	writeJSON(w, http.StatusAccepted, job)
}

// handleRemoteTransferSend asks an enrolled source host to transfer directly
// to another Eta location. The coordinator moves control messages only; file
// chunks still travel from source host to destination host.
func (s *server) handleRemoteTransferSend(w http.ResponseWriter, r *http.Request) {
	if s.peers == nil {
		writeError(w, errors.New("peer inventory is unavailable"))
		return
	}
	var request struct {
		SourcePeer      string `json:"sourcePeer"`
		DestinationPeer string `json:"destinationPeer"`
		SourceRoot      int    `json:"sourceRoot"`
		SourcePath      string `json:"sourcePath"`
		DestinationRoot int    `json:"destinationRoot"`
		DestinationPath string `json:"destinationPath"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	source, found, err := s.peers.Find(request.SourcePeer)
	if err != nil {
		writeError(w, err)
		return
	}
	if !found {
		writeError(w, errors.New("unknown source peer"))
		return
	}
	destinationURL := s.advertiseURL
	if request.DestinationPeer != "" {
		destination, found, err := s.peers.Find(request.DestinationPeer)
		if err != nil {
			writeError(w, err)
			return
		}
		if !found {
			writeError(w, errors.New("unknown destination peer"))
			return
		}
		destinationURL = destination.URL
	}
	if destinationURL == "" {
		destinationURL = "http://" + r.Host
	}
	destination, err := url.Parse(destinationURL)
	if err != nil || (destination.Scheme != "http" && destination.Scheme != "https") || destination.Host == "" {
		writeError(w, errors.New("invalid destination URL"))
		return
	}
	body, err := json.Marshal(map[string]any{"peer": destinationURL, "sourceRoot": request.SourceRoot, "sourcePath": request.SourcePath, "destinationRoot": request.DestinationRoot, "destinationPath": request.DestinationPath})
	if err != nil {
		writeError(w, err)
		return
	}
	endpoint := strings.TrimSuffix(source.URL, "/") + "/api/transfers/send"
	response, err := (&http.Client{Timeout: 10 * time.Second}).Post(endpoint, "application/json", strings.NewReader(string(body)))
	if err != nil {
		writeError(w, err)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		writeError(w, fmt.Errorf("source transfer request failed: %s: %s", response.Status, strings.TrimSpace(string(data))))
		return
	}
	var job transfer.Job
	if err := json.NewDecoder(response.Body).Decode(&job); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"peer": source.URL, "id": job.ID})
}

func (s *server) handleRemoteDelete(w http.ResponseWriter, r *http.Request) {
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
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		writeError(w, err)
		return
	}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimSuffix(peer.URL, "/")+"/api/delete", bytes.NewReader(body))
	if err != nil {
		writeError(w, err)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		writeError(w, err)
		return
	}
	defer response.Body.Close()
	w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (s *server) handleRemoteTransferJob(w http.ResponseWriter, r *http.Request) {
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
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, errors.New("missing transfer job ID"))
		return
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Get(strings.TrimSuffix(peer.URL, "/") + "/api/transfer-jobs/" + url.PathEscape(id))
	if err != nil {
		writeError(w, err)
		return
	}
	defer response.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (s *server) handleTransferJobs(w http.ResponseWriter, _ *http.Request) {
	if s.transferJobs == nil {
		writeError(w, errors.New("transfer service is unavailable"))
		return
	}
	writeJSON(w, http.StatusOK, s.transferJobs.List())
}

func (s *server) handleTransferJob(w http.ResponseWriter, r *http.Request) {
	if s.transferJobs == nil {
		writeError(w, errors.New("transfer service is unavailable"))
		return
	}
	job, found := s.transferJobs.Get(r.PathValue("id"))
	if !found {
		writeError(w, errors.New("unknown transfer"))
		return
	}
	writeJSON(w, http.StatusOK, job)
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
	if request.Manifest.ChunkSize <= 0 || request.Manifest.Size < 0 || len(request.Manifest.Chunks) > 1<<16 || (request.Manifest.Size > 0 && len(request.Manifest.Chunks) == 0) {
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
	// If this finalize landed inside an in-flight tree session, refresh
	// the session's LastProgress so the crash-recovery sweep doesn't
	// mistake a slow per-file stage for an abandoned transfer.
	s.touchTreeForDestination(request.Root, destination)
	w.WriteHeader(http.StatusNoContent)
}

// touchTreeForDestination refreshes the LastProgress of any tree
// session whose staging path is a prefix of `destination`. No-op when
// no tree is active on this root or destination is outside staging.
func (s *server) touchTreeForDestination(root int, destination string) {
	if root < 0 || root >= len(s.treeStores) {
		return
	}
	store := s.treeStores[root]
	if store == nil {
		return
	}
	intents, err := store.ListIntents()
	if err != nil {
		return
	}
	rootPath := s.roots[root].Path
	for id := range intents {
		staging := filepath.Join(rootPath, ".eta", "staging", id)
		rel, err := filepath.Rel(staging, destination)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		_ = store.Touch(id)
		return
	}
}

// handleTransferTreeCreate reserves a tree session, builds the staging
// tree, and persists the intent record. The actual file bytes flow
// through the existing /api/transfers endpoints targeting paths under
// the staging prefix returned via the response.
func (s *server) handleTransferTreeCreate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Root        int      `json:"root"`
		Destination string   `json:"destination"`
		Directories []string `json:"directories"`
		Files       []struct {
			Path string `json:"path"`
			Size int64  `json:"size"`
		} `json:"files"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	if request.Root < 0 || request.Root >= len(s.treeStores) {
		writeError(w, errors.New("unknown root"))
		return
	}
	// Validate every declared path up front; refuse anything that
	// already exists in the destination.
	for _, dir := range request.Directories {
		if err := transfer.ValidateRelative(dir); err != nil {
			writeError(w, fmt.Errorf("directory %q: %w", dir, err))
			return
		}
	}
	tree := transfer.Tree{Directories: request.Directories}
	for _, file := range request.Files {
		if err := transfer.ValidateRelative(file.Path); err != nil {
			writeError(w, fmt.Errorf("file %q: %w", file.Path, err))
			return
		}
		tree.Files = append(tree.Files, transfer.TreeFile{
			Path:     file.Path,
			Manifest: transfer.Manifest{Size: file.Size},
		})
	}
	id, err := s.treeStores[request.Root].Create(request.Destination, tree)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":          id,
		"root":        request.Root,
		"destination": request.Destination,
	})
}

// handleTransferTreeStatus returns the per-session intent plus a
// complete map derived from the staging filesystem. Cheap to query
// (no chunk manifest in the intent) so a sender can poll before
// resuming without paying for rescan work.
func (s *server) handleTransferTreeStatus(w http.ResponseWriter, r *http.Request) {
	store, ok := s.resolveTreeStore(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	intent, complete, err := store.Status(id)
	if err != nil {
		writeError(w, err)
		return
	}
	paths := make(map[string]bool, len(complete))
	for k, v := range complete {
		paths[k] = v
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"intent":   intent,
		"complete": paths,
	})
}

// handleTransferTreeCommit verifies every file is present in staging
// at the expected size, then performs a single os.Rename of the
// staging tree to the destination. POSIX-atomic on the destination
// filesystem: the destination tree is either complete or absent.
func (s *server) handleTransferTreeCommit(w http.ResponseWriter, r *http.Request) {
	store, ok := s.resolveTreeStore(w, r)
	if !ok {
		return
	}
	if err := store.Commit(r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTransferTreeAbort removes the staging tree and intent record
// without committing. Errors are non-fatal: callers may retry.
func (s *server) handleTransferTreeAbort(w http.ResponseWriter, r *http.Request) {
	store, ok := s.resolveTreeStore(w, r)
	if !ok {
		return
	}
	if err := store.Abort(r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveTreeStore picks the right TreeStore for a request by reading
// the root query parameter. Centralized so handlers stay in lockstep
// about the resolution rule. Returns (store, true) on success; on
// failure it has already written a 4xx response and returns (_, false).
func (s *server) resolveTreeStore(w http.ResponseWriter, r *http.Request) (*transfer.TreeStore, bool) {
	rootQuery := r.URL.Query().Get("root")
	if rootQuery == "" {
		writeError(w, errors.New("missing root"))
		return nil, false
	}
	var root int
	if _, err := fmt.Sscanf(rootQuery, "%d", &root); err != nil {
		writeError(w, fmt.Errorf("invalid root: %w", err))
		return nil, false
	}
	if root < 0 || root >= len(s.treeStores) || s.treeStores[root] == nil {
		writeError(w, errors.New("unknown root"))
		return nil, false
	}
	return s.treeStores[root], true
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

func (s *server) handleDirectoryCreate(w http.ResponseWriter, r *http.Request) {
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
		err = operations.EnsureDirectory(request.Path)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *server) handleCopy(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SourceRoot      int    `json:"sourceRoot"`
		SourcePath      string `json:"sourcePath"`
		DestinationRoot int    `json:"destinationRoot"`
		DestinationPath string `json:"destinationPath"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	if request.SourceRoot < 0 || request.SourceRoot >= len(s.roots) || request.DestinationRoot < 0 || request.DestinationRoot >= len(s.roots) {
		writeError(w, errors.New("invalid root"))
		return
	}
	source, err := fileops.New(s.roots[request.SourceRoot].Path)
	if err != nil {
		writeError(w, err)
		return
	}
	destination, err := fileops.New(s.roots[request.DestinationRoot].Path)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := destination.Copy(source, request.SourcePath, request.DestinationPath); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
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
		// Hide Eta's hidden staging directory; never expose the
		// .eta/ root (or anything beneath it) to listings or
		// traversals. User-created files named .eta are refused at
		// the validation layer, so this filter is purely defensive
		// against odd filesystem states.
		if item.Name() == ".eta" {
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
