package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
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
	"sync"
	"syscall"
	"time"

	"github.com/hypernewbie/eta/internal/access"
	"github.com/hypernewbie/eta/internal/bindaddr"
	"github.com/hypernewbie/eta/internal/diskcache"
	"github.com/hypernewbie/eta/internal/fileops"
	"github.com/hypernewbie/eta/internal/hostid"
	"github.com/hypernewbie/eta/internal/mdns"
	"github.com/hypernewbie/eta/internal/peers"
	"github.com/hypernewbie/eta/internal/rangecache"
	"github.com/hypernewbie/eta/internal/remotefile"
	"github.com/hypernewbie/eta/internal/remotepc"
	"github.com/hypernewbie/eta/internal/roots"
	"github.com/hypernewbie/eta/internal/terminal"
	"github.com/hypernewbie/eta/internal/tmux"
	"github.com/hypernewbie/eta/internal/transfer"
	"github.com/hypernewbie/eta/internal/uistate"
)

//go:embed all:web
var embeddedWeb embed.FS

//go:embed CHANGELOG.md
var embeddedChangelog []byte

// Overridden at build time via
// -ldflags "-X main.Version=v0.2.0 -X main.Commit=$(git rev-parse --short HEAD) -X main.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.BuildSource=release".
// A plain `go build` (what every dev loop in this repo uses) leaves
// these as the obvious "this is not a tagged release" defaults — no
// release pipeline exists yet to set them automatically, so an unset
// value should read as unset, not as a plausible-looking fake version.
var (
	Version     = "dev"
	Commit      = "none"
	Date        = "unknown"
	BuildSource = "source"
)

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
	// Removed roots keep their slot rather than being spliced out (see
	// internal/roots's doc comment): a root's index is its ID, referenced
	// by every persisted shortcut, window and transfer job, and reusing a
	// removed root's index for a different directory would silently
	// repoint every one of those at the wrong place.
	Removed bool
}

type server struct {
	roots        []root
	rootsStore   *roots.Store
	rootsPath    string
	rootsMu      sync.Mutex
	web          fs.FS
	thumbs       *thumbnailCache
	identity     hostid.Identity
	identityPath string
	identityMu   sync.RWMutex
	state        *uistate.Store
	peers        *peers.Store
	access       *access.Manager
	accessPath   string
	peerSessions *peerSessionCache
	remotePCs    *remotepc.Manager
	remoteCache  *diskcache.Cache
	hotRanges    *rangecache.Cache
	transfers    *transfer.Store
	transferJobs *transfer.Jobs
	treeStores   []*transfer.TreeStore
	terminals    *terminal.Manager
	advertiseURL string
	// shutdown cancels long-running background goroutines so they
	// don't outlive the http.Server's 10s shutdown grace period.
	shutdown       context.Context
	shutdownCancel context.CancelFunc
}

type entry struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Kind     string    `json:"kind"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	// Reported rather than filtered here: the client decides whether to
	// show them, and only the server can see a Windows hidden attribute.
	Hidden bool `json:"hidden,omitempty"`
}

func main() {
	// Named rootPaths, not roots: this package now also imports
	// internal/roots, and main is exactly where both are needed.
	var rootPaths rootsFlag
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
	accessFile := flag.String("access-file", "", "persistent access-password file (default: user config directory)")
	remoteCacheDir := flag.String("remote-cache-dir", "", "directory for cached remote byte ranges (default: user cache directory)")
	remoteCacheSize := flag.String("remote-cache-size", "4GB", "maximum remote byte-range cache size")
	hotRangeCacheSize := flag.String("hot-range-cache-size", "64MB", "maximum RAM used by hot remote ranges")
	transferDir := flag.String("transfer-dir", "", "directory for resumable transfer staging (default: user cache directory)")
	advertiseURL := flag.String("advertise-url", "", "public http(s) URL peers use to send files here (default: request host)")
	versionFlag := flag.Bool("version", false, "print version and exit")
	rootsFile := flag.String("roots-file", "", "persistent root-directory inventory file (default: user config directory)")
	exitOnStdinClose := flag.Bool("exit-on-stdin-close", false, "shut down when stdin reaches EOF; set when eta is started over an SSH session so it exits with that connection")
	flag.Var(&rootPaths, "root", "directory to expose (repeatable; defaults to the current directory; ignored once --roots-file already has entries, since roots are then managed from Settings)")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("eta %s (commit: %s, built: %s, source: %s)\n", Version, Commit, Date, BuildSource)
		return
	}

	if len(rootPaths) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
		rootPaths = append(rootPaths, cwd)
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

	s, err := newServer(rootPaths)
	if err != nil {
		log.Fatal(err)
	}
	s.identity = identity
	s.identityPath = identityPath
	s.advertiseURL = strings.TrimSuffix(*advertiseURL, "/")
	rootsPath := *rootsFile
	if rootsPath == "" {
		rootsPath, err = roots.DefaultPath()
		if err != nil {
			log.Fatal(err)
		}
	}
	s.rootsPath = rootsPath
	s.rootsStore = roots.New(rootsPath)
	persistedRoots, err := s.rootsStore.Load()
	if err != nil {
		log.Fatal(err)
	}
	if len(persistedRoots) == 0 {
		// First run: seed the persisted list from what newServer just
		// validated from -root flags (or the default cwd), so the file
		// becomes the source of truth for every run after this one
		// rather than being CLI-driven forever.
		for _, r := range s.roots {
			persistedRoots = append(persistedRoots, roots.Root{Name: r.Name, Path: r.Path})
		}
		if err := s.rootsStore.SaveAll(persistedRoots); err != nil {
			log.Fatal(err)
		}
	} else {
		// The persisted list is authoritative once it exists: someone may
		// have since added or removed a root from Settings, and a stale
		// -root flag left over from the first run should not silently
		// reintroduce a root that was removed, or fail to reflect one
		// that was added.
		log.Printf("using the root inventory at %s; --root is only used to seed it on first run", rootsPath)
	}
	s.applyPersistedRoots(persistedRoots)
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
	accessPath := *accessFile
	if accessPath == "" {
		accessPath, err = access.DefaultPath()
		if err != nil {
			log.Fatal(err)
		}
	}
	s.accessPath = accessPath
	accessCfg, err := access.Load(accessPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := s.access.Configure(accessCfg.PasswordHash); err != nil {
		log.Fatal(err)
	}
	remoteDir := *remoteCacheDir
	if remoteDir == "" {
		remoteDir, err = diskcache.DefaultPath()
		if err != nil {
			log.Fatal(err)
		}
	}
	remoteBytes, err := parseBytes(*remoteCacheSize, "--remote-cache-size")
	if err != nil {
		log.Fatal(err)
	}
	s.remoteCache, err = diskcache.New(remoteDir, remoteBytes)
	if err != nil {
		log.Fatal(err)
	}
	hotRangeBytes, err := parseBytes(*hotRangeCacheSize, "--hot-range-cache-size")
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
	cacheBytes, err := parseBytes(*thumbnailCacheSize, "--thumbnail-cache-size")
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

	log.Printf("eta viewer — %d root(s)", len(s.roots))
	for _, listener := range listeners {
		log.Printf("serving http://%s", listener.Addr())
	}

	// Auto-resume any outbound transfers left in-flight by a previous
	// process. Jobs without recorded source/destination are already
	// marked interrupted by NewPersistentJobs; here we pick up the
	// ones we can actually attempt to retry.
	go s.resumePendingJobs(s.shutdown)

	// Sweep stale tree-transfer staging on startup and then hourly.
	// Without this, a sender that crashed mid-resume leaves its
	// receiver-side staging tree orphaned under {root}/.eta/staging/.
	// TreeStore.Sweep was implemented and tested but never wired
	// into the server lifecycle, so the orphans accumulated until
	// the disk filled.
	go s.sweepStaleTreeSessions(s.shutdown)

	httpServer := &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	if *exitOnStdinClose {
		// The leash for an eta started on another machine over SSH: when
		// that connection ends, stdin hits EOF and this exits with it, so
		// no daemon, service or unit is needed on the remote.
		//
		// Stdin EOF is the portable "my client went away" — same contract
		// as a language server or `docker system dial-stdio`. A pty plus
		// SIGHUP would need `ssh -t`, and Windows has no SIGHUP and would
		// give ConPTY screen-repaint output instead of readable lines.
		//
		// Feeds the same stop channel as a real signal, so there's one
		// shutdown path, not a second that could drift.
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			stop <- syscall.SIGTERM
		}()
	}
	go func() {
		<-stop
		// Cancel in-flight transfer and sweep goroutines so they
		// don't outlive the http.Server's 10s shutdown grace period.
		// Without this, a graceful shutdown can exit while a
		// resume goroutine is mid-tree-commit, leaving the
		// receiver with a staging tree that nothing will sweep up.
		s.shutdownCancel()
		// Every eta this instance started on another PC dies with its
		// ssh connection anyway, once that connection's stdin closes.
		// Closing them here makes it prompt and deliberate rather than
		// leaving remote processes to a server-side timeout.
		s.remotePCs.StopAll()
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
	ctx, cancel := context.WithCancel(context.Background())
	s := &server{
		web:          web,
		identity:     hostid.For("test-host", "eta"),
		terminals:    terminal.NewManager(),
		transferJobs: transfer.NewJobs(),
		access:       access.NewManager(),
		peerSessions: newPeerSessionCache(),
		remotePCs:    remotepc.NewManager(),
		shutdown:     ctx,
	}
	s.shutdownCancel = cancel
	seen := map[string]bool{}
	for _, path := range paths {
		realPath, err := resolveRootPath(path)
		if err != nil {
			return nil, err
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

// resolveRootPath validates and canonicalizes a candidate root
// directory: absolute, symlinks resolved, must exist and be a
// directory. Shared between -root flag parsing at startup (via
// newServer) and the POST /api/roots handler, so both enforce exactly
// the same rule rather than two copies that could drift.
func resolveRootPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve root %q: %w", path, err)
	}
	realPath, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve root %q: %w", path, err)
	}
	info, err := os.Stat(realPath)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("root %q is not a directory", path)
	}
	return realPath, nil
}

// validRoot reports whether id both exists and has not been removed.
// Every handler that takes a root ID from a request checks this instead
// of a bare bounds check, so a request naming a removed root's old
// index is refused rather than still quietly working.
func (s *server) validRoot(id int) bool {
	return id >= 0 && id < len(s.roots) && !s.roots[id].Removed
}

// applyPersistedRoots rebuilds s.roots and s.treeStores from a
// roots.Root list, position for position, so the in-memory arrays this
// server has always read directly stay in lockstep with what is on
// disk. Called at startup and after every add/remove.
//
// The swap is locked; the many existing read sites (s.roots[i], as
// they were before this feature) are not, and were not retrofitted
// with per-call-site locking. That is a deliberate, disclosed tradeoff:
// root mutation is a rare, explicit admin action through Settings, not
// a hot path, and touching every one of the two dozen existing read
// sites risked missing one for a race that has no realistic trigger in
// how this feature is actually used.
func (s *server) applyPersistedRoots(list []roots.Root) {
	built := make([]root, len(list))
	stores := make([]*transfer.TreeStore, len(list))
	s.rootsMu.Lock()
	oldRoots, oldStores := s.roots, s.treeStores
	for i, r := range list {
		built[i] = root{Name: r.Name, Path: r.Path, Removed: r.Removed}
		if r.Removed {
			continue
		}
		// Reuse the existing TreeStore when this index's path is
		// unchanged, so adding or removing a *different* root cannot
		// drop another root's in-progress tree-transfer session —
		// rebuilding every slot unconditionally on every mutation would
		// do exactly that.
		if i < len(oldRoots) && oldRoots[i].Path == r.Path && !oldRoots[i].Removed && oldStores[i] != nil {
			stores[i] = oldStores[i]
		} else {
			stores[i] = transfer.NewTreeStore(r.Path)
		}
	}
	s.roots = built
	s.treeStores = stores
	s.rootsMu.Unlock()
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("POST /api/auth/password", s.handleAuthPassword)
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"version":      Version,
			"commit":       Commit,
			"date":         Date,
			"build_source": BuildSource,
		})
	})
	// Served from the embedded copy, not disk — same reason the web
	// assets are go:embed rather than a static directory: a single
	// binary with no external files it depends on to run correctly.
	mux.HandleFunc("GET /api/changelog", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(embeddedChangelog)
	})
	mux.HandleFunc("/api/identity", s.handleIdentity)
	mux.HandleFunc("GET /api/state", s.handleStateGet)
	mux.HandleFunc("POST /api/state", s.handleStatePut)
	mux.HandleFunc("PUT /api/state", s.handleStatePut)
	mux.HandleFunc("GET /api/roots", s.handleRoots)
	mux.HandleFunc("POST /api/roots", s.handleRootAdd)
	mux.HandleFunc("DELETE /api/roots", s.handleRootRemove)
	mux.HandleFunc("GET /api/peers", s.handlePeers)
	mux.HandleFunc("POST /api/terminals", s.handleTerminalStart)
	mux.HandleFunc("GET /api/terminals/{id}", s.handleTerminalOutput)
	mux.HandleFunc("GET /api/terminals/{id}/stream", s.handleTerminalStream)
	mux.HandleFunc("POST /api/terminals/{id}/input", s.handleTerminalInput)
	mux.HandleFunc("POST /api/terminals/{id}/resize", s.handleTerminalResize)
	mux.HandleFunc("DELETE /api/terminals/{id}", s.handleTerminalClose)
	mux.HandleFunc("GET /api/tmux", s.handleTmuxList)
	mux.HandleFunc("POST /api/tmux", s.handleTmuxCreate)
	mux.HandleFunc("GET /api/remote/tmux", s.handleRemoteTmuxList)
	mux.HandleFunc("POST /api/remote/tmux", s.handleRemoteTmuxCreate)
	mux.HandleFunc("POST /api/remote/terminals", s.handleRemoteTerminalStart)
	mux.HandleFunc("GET /api/remote/terminals/{id}", s.handleRemoteTerminalOutput)
	mux.HandleFunc("GET /api/remote/terminals/{id}/stream", s.handleRemoteTerminalStream)
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
	mux.HandleFunc("POST /api/remote/roots", s.handleRemoteRoots)
	mux.HandleFunc("DELETE /api/remote/roots", s.handleRemoteRoots)
	mux.HandleFunc("GET /api/remote/list", s.handleRemoteList)
	mux.HandleFunc("GET /api/remote/file", s.handleRemoteFile)
	mux.HandleFunc("GET /api/remote/preview", s.handleRemotePreview)
	mux.HandleFunc("GET /api/remote/thumbnail", s.handleRemoteThumbnail)
	mux.HandleFunc("POST /api/remote-pc", s.handleRemotePCConnect)
	mux.HandleFunc("GET /api/remote-pc", s.handleRemotePCStatus)
	mux.HandleFunc("DELETE /api/remote-pc", s.handleRemotePCDisconnect)
	mux.HandleFunc("POST /api/remote-pc/cleanup", s.handleRemotePCCleanup)
	mux.HandleFunc("GET /api/peers/auth-status", s.handlePeerAuthStatus)
	mux.HandleFunc("POST /api/peers", s.handlePeerAdd)
	mux.HandleFunc("PATCH /api/peers", s.handlePeerUpdate)
	mux.HandleFunc("POST /api/peers/credential", s.handlePeerCredential)
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
	return securityHeaders(s.accessAuthMiddleware(mux))
}

func (s *server) handleIdentity(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.identityMu.RLock()
		identity := s.identity
		s.identityMu.RUnlock()
		writeJSON(w, http.StatusOK, identity)
	case http.MethodPost:
		s.handleIdentityUpdate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleIdentityUpdate persists a new accent for the local host. The
// server is the source of truth across reloads; the webapp mirrors
// the choice to localStorage as a write-through cache so the prepaint
// script can render the right colors before the first /api/identity
// fetch returns. A POST without an identity file on disk (test
// servers) responds 501 so callers can detect and degrade.
func (s *server) handleIdentityUpdate(w http.ResponseWriter, r *http.Request) {
	if s.identityPath == "" {
		writeError(w, errors.New("identity persistence is unavailable"))
		return
	}
	var request struct {
		Accent string `json:"accent"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&request); err != nil {
		writeError(w, fmt.Errorf("decode identity update: %w", err))
		return
	}
	updated, err := hostid.SetAccent(s.identityPath, request.Accent)
	if err != nil {
		writeError(w, err)
		return
	}
	s.identityMu.Lock()
	s.identity = updated
	s.identityMu.Unlock()
	writeJSON(w, http.StatusOK, updated)
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

// The list reports availability rather than failing, so the UI can say
// "tmux is not installed on this PC" instead of showing a machine with
// no sessions and no explanation.
func (s *server) handleTmuxList(w http.ResponseWriter, r *http.Request) {
	sessions, err := tmux.List(r.Context())
	if errors.Is(err, tmux.ErrUnavailable) {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "sessions": []tmux.Session{}})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "sessions": sessions})
}

func (s *server) handleTmuxCreate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	session, err := tmux.Create(r.Context(), request.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func (s *server) handleRemoteTmuxList(w http.ResponseWriter, r *http.Request) {
	s.proxyRemoteTerminal(w, r, "/api/tmux")
}

func (s *server) handleRemoteTmuxCreate(w http.ResponseWriter, r *http.Request) {
	s.proxyRemoteTerminal(w, r, "/api/tmux")
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
		Tmux    string `json:"tmux"`
		// Edit opens the file at Path directly in vim inside the new
		// terminal, instead of a shell sitting in its directory. The
		// simplest possible "editor": no browser-side editor component,
		// no save endpoint, no conflict-with-external-edits problem to
		// solve — the terminal already has all of that solved.
		Edit bool `json:"edit"`
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
	info, err := os.Stat(target)
	if err != nil {
		writeError(w, err)
		return
	}
	var editArgv []string
	if request.Edit && !info.IsDir() {
		// vim runs in the file's own directory with just its base name,
		// not the absolute path — an absolute path would work too, but a
		// relative one is what :w naturally saves back to and matches
		// what someone would type by hand.
		editArgv = []string{"vim", filepath.Base(target)}
		target = filepath.Dir(target)
	} else if !info.IsDir() {
		target = filepath.Dir(target)
	}
	start := func() (string, error) {
		if editArgv != nil {
			return s.terminals.StartCommand(target, request.Columns, request.Rows, editArgv)
		}
		if request.Tmux == "" {
			return s.terminals.Start(target, request.Columns, request.Rows)
		}
		argv, err := tmux.AttachArgv(request.Tmux)
		if err != nil {
			return "", err
		}
		return s.terminals.StartCommand(target, request.Columns, request.Rows, argv)
	}
	id, err := start()
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

// handleTerminalStream streams PTY output as text/event-stream,
// reducing end-to-end output latency from the 180 ms client-polling
// window to ~20 ms. Each event carries the new bytes since the last
// offset plus the new offset; the final event has `closed: true`.
// Clients (re)connect with `?offset=N` and reconnect transparently
// across disconnects.
func (s *server) handleTerminalStream(w http.ResponseWriter, r *http.Request) {
	if s.terminals == nil {
		writeError(w, errors.New("terminal service is unavailable"))
		return
	}
	session, ok := s.terminals.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "unknown terminal", http.StatusNotFound)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		// Streaming requires a flushable response writer; fall back
		// to polling on the existing endpoint rather than half-streaming.
		s.terminalSession(w, r, func(sess *terminal.Session) {
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			output, next, closed := sess.Output(offset)
			writeJSON(w, http.StatusOK, map[string]any{"output": string(output), "offset": next, "closed": closed})
		})
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
		output, next, closed := session.Output(offset)
		if len(output) > 0 {
			body, _ := json.Marshal(map[string]any{"output": string(output), "offset": next, "closed": closed})
			fmt.Fprintf(w, "data: %s\n\n", body)
			flusher.Flush()
			offset = next
		}
		if closed {
			body, _ := json.Marshal(map[string]any{"closed": true, "offset": offset})
			fmt.Fprintf(w, "data: %s\n\n", body)
			flusher.Flush()
			return
		}
	}
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

// The terminal stream is long-lived and must not be buffered or timed
// out like the request/response endpoints: proxyRemoteTerminal uses a
// 10s client and a plain io.Copy, which would cut a working terminal
// off after ten seconds and withhold output until then anyway.
func (s *server) handleRemoteTerminalStream(w http.ResponseWriter, r *http.Request) {
	s.streamRemoteTerminal(w, r, "/api/terminals/"+url.PathEscape(r.PathValue("id"))+"/stream")
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
	response, err := s.peerClient(peer, 10*time.Second).Do(request)
	if err != nil {
		writeError(w, friendlyPeerError(peer, err))
		return
	}
	defer response.Body.Close()
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (s *server) streamRemoteTerminal(w http.ResponseWriter, r *http.Request, route string) {
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
	target := *remote
	target.Path = route
	query := url.Values{}
	if offset := r.URL.Query().Get("offset"); offset != "" {
		query.Set("offset", offset)
	}
	target.RawQuery = query.Encode()
	// No client timeout: the stream stays open for the life of the
	// terminal. The request context ends it when the browser goes away.
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		writeError(w, err)
		return
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err := s.peerClient(peer, 0).Do(request)
	if err != nil {
		writeError(w, friendlyPeerError(peer, err))
		return
	}
	defer response.Body.Close()
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(response.StatusCode)
	flusher, _ := w.(http.Flusher)
	buffer := make([]byte, 4<<10)
	for {
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return
			}
			// Without this the events sit in the response buffer and the
			// remote terminal appears frozen.
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr != nil {
			return
		}
	}
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
	job := s.transferJobs.StartWith(transfer.JobSpec{
		Name:            transfer.SourceName(source),
		Total:           totalChunks,
		SourceRoot:      request.SourceRoot,
		SourcePath:      request.SourcePath,
		DestinationPeer: peer.URL,
		DestinationRoot: request.DestinationRoot,
		DestinationPath: request.DestinationPath,
	})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		client := s.peerClientForURL(peer.URL, 30*time.Second)
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

// sweepStaleTreeSessions removes abandoned tree-transfer staging
// directories on every root. Runs immediately on startup so any
// orphans from a previous process are cleaned before the next
// sender attempt, then loops once an hour. Stale = no Touch in
// the last 24 hours, matching the test threshold in
// internal/transfer/treestore_test.go.
//
// Sweep is best-effort; a single failure logs and continues so
// one bad root can't stop the others from being cleaned.
func (s *server) sweepStaleTreeSessions(ctx context.Context) {
	const ttl = 24 * time.Hour
	sweep := func() {
		for _, store := range s.treeStores {
			if store == nil {
				continue
			}
			n, err := store.Sweep(ttl, time.Now())
			if err != nil {
				log.Printf("tree sweep: %v", err)
				continue
			}
			if n > 0 {
				log.Printf("tree sweep: removed %d stale session(s)", n)
			}
		}
	}
	sweep()
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}

// resumePendingJobs kicks off a retry goroutine for each non-done
// outbound transfer captured at startup. Only local-source +
// peer-destination jobs are eligible; cross-source retries are
// deferred until we have a path to ask the source peer to re-scan.
// Each goroutine updates the existing Job in place — no new Job is
// recorded — so the taskbar shows one entry per original transfer.
func (s *server) resumePendingJobs(ctx context.Context) {
	if s.transferJobs == nil {
		return
	}
	for _, job := range s.transferJobs.List() {
		if job.Done {
			continue
		}
		if job.SourcePath == "" || job.DestinationPath == "" {
			continue
		}
		if !s.validRoot(job.SourceRoot) {
			continue
		}
		if job.SourcePeer != "" {
			// Peer-source resume needs to fetch source listings from
			// the peer; defer until that's wired.
			continue
		}
		if job.DestinationPeer == "" {
			// Local destination. The server can issue /api/copy to
			// itself, but at-restart the user already saw this as
			// interrupted; leave that path manual.
			continue
		}
		go s.attemptResume(ctx, job)
	}
}

func (s *server) attemptResume(ctx context.Context, job transfer.Job) {
	srcPath := filepath.Join(s.roots[job.SourceRoot].Path, filepath.FromSlash(job.SourcePath))
	info, err := os.Stat(srcPath)
	if err != nil || (!info.Mode().IsRegular() && !info.IsDir()) {
		// Auto-resume failure, distinct from the original restart
		// interruption that landed us here. Wording must not
		// duplicate the "interrupted by Eta restart" message that
		// the user already saw, or they'll think the restart
		// message just updated.
		s.transferJobs.Finish(job.ID, fmt.Errorf("auto-resume failed: source %q unavailable", job.SourcePath))
		return
	}
	transferCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	client := s.peerClientForURL(job.DestinationPeer, 30*time.Second)
	var transferErr error
	if info.IsDir() {
		tree, err := transfer.BuildTree(srcPath)
		if err != nil {
			s.transferJobs.Finish(job.ID, fmt.Errorf("auto-resume failed: %w", err))
			return
		}
		transferErr = transfer.SendTreeWithProgress(transferCtx, client, job.DestinationPeer, job.DestinationRoot, job.DestinationPath, srcPath, tree, func(completed, _ int) {
			s.transferJobs.Progress(job.ID, completed)
		})
	} else {
		_, transferErr = transfer.SendFileWithProgress(transferCtx, client, job.DestinationPeer, job.DestinationRoot, job.DestinationPath, srcPath, func(completed, _ int) {
			s.transferJobs.Progress(job.ID, completed)
		})
	}
	s.transferJobs.Finish(job.ID, transferErr)
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
	response, err := s.peerClient(source, 10*time.Second).Post(endpoint, "application/json", strings.NewReader(string(body)))
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
	response, err := s.peerClient(peer, 10*time.Second).Do(request)
	if err != nil {
		writeError(w, friendlyPeerError(peer, err))
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
	response, err := s.peerClient(peer, 10*time.Second).Get(strings.TrimSuffix(peer.URL, "/") + "/api/transfer-jobs/" + url.PathEscape(id))
	if err != nil {
		writeError(w, friendlyPeerError(peer, err))
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
	if !s.validRoot(rootID) {
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
	for k, v := range r.URL.Query() {
		if k == "peer" {
			continue
		}
		query[k] = v
	}
	remoteURL.RawQuery = query.Encode()

	var reqBody io.Reader
	if r.Body != nil {
		reqBody = r.Body
	}
	request, err := http.NewRequestWithContext(r.Context(), r.Method, remoteURL.String(), reqBody)
	if err != nil {
		writeError(w, err)
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		request.Header.Set("Content-Type", ct)
	}
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		request.Header.Set("Range", rangeHeader)
	}
	response, err := s.peerClient(peer, 10*time.Second).Do(request)
	if err != nil {
		writeError(w, friendlyPeerError(peer, err))
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
	source := &remotefile.HTTPSource{BaseURL: peer.URL, Root: rootID, Client: s.peerClient(peer, 30*time.Second)}
	body, info, err := remotefile.ReadHotCachedRange(r.Context(), s.hotRanges, s.remoteCache, source, r.URL.Query().Get("path"), start, end-start+1)
	if err != nil {
		// A cache miss or corrupt range read comes back from this same
		// call as a non-network error (a decode failure, a size
		// mismatch); friendlyPeerError's "is offline" wording would be
		// wrong for those. But this function exists to shortcut *around*
		// proxyPeer for one case — an explicit byte range on /api/file —
		// and every other path through it already gets the friendly
		// wrapping, so a raw network error surfacing here only from this
		// one shortcut would be an inconsistent, confusing exception.
		// net.Error covers exactly the transport-level failures (refused,
		// timeout, DNS) that are actually the offline case.
		var netErr net.Error
		if errors.As(err, &netErr) {
			writeError(w, friendlyPeerError(peer, err))
		} else {
			writeError(w, err)
		}
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

// redactPeer strips Verifier before a peer record crosses into a browser
// response. It is this server's own credential for logging in to that
// peer on the browser's behalf (see peer_auth.go) — a bearer-equivalent
// secret, not display data — and the browser has no legitimate use for
// it. It is fine at rest in peers.json (same trust level as an access
// password hash) and fine over the wire between this server and that
// peer; it should never reach further than that.
func redactPeer(peer peers.Peer) peers.Peer {
	peer.Verifier = ""
	return peer
}
func redactPeers(items []peers.Peer) []peers.Peer {
	redacted := make([]peers.Peer, len(items))
	for i, item := range items {
		redacted[i] = redactPeer(item)
	}
	return redacted
}

func (s *server) handlePeers(w http.ResponseWriter, r *http.Request) {
	if s.peers == nil {
		writeError(w, errors.New("peer inventory is unavailable"))
		return
	}
	items, err := s.peers.List()
	if err != nil {
		writeError(w, err)
		return
	}
	// Probing is opt-in per request. Every caller doing it would mean a
	// burst of identity requests to every PC on every listing; the
	// client asks for it once per page load instead.
	if r.URL.Query().Get("refresh") == "1" {
		s.refreshPeerIdentities(r.Context(), items)
		if refreshed, err := s.peers.List(); err == nil {
			items = refreshed
		}
	}
	writeJSON(w, http.StatusOK, redactPeers(items))
}

// A peer's name, colour and glyph are copied into the inventory when it
// is enrolled, so renaming a PC or changing its accent left every other
// machine showing the old identity indefinitely.
func (s *server) refreshPeerIdentities(ctx context.Context, items []peers.Peer) {
	var wait sync.WaitGroup
	for _, item := range items {
		wait.Add(1)
		go func(peer peers.Peer) {
			defer wait.Done()
			probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			identity, err := probePeer(probeCtx, peer.URL)
			if err != nil {
				// Unreachable is not "changed": keep the last known
				// identity so an offline PC stays recognisable.
				return
			}
			if identity.ID == peer.ID && identity.Hostname == peer.Name &&
				identity.Accent == peer.Accent && identity.Glyph == peer.Glyph {
				return
			}
			peer.ID, peer.Name, peer.Accent, peer.Glyph =
				identity.ID, identity.Hostname, identity.Accent, identity.Glyph
			_ = s.peers.Update(peer)
		}(item)
	}
	wait.Wait()
}

// handlePeerAuthStatus lets the browser check whether an *enrolled or
// about-to-be-enrolled* peer needs a password, and fetch the KDF
// parameters (salt, iterations) to derive a verifier against — without
// the browser ever contacting that peer directly, which would need CORS
// on a login endpoint and would try to set that peer's session cookie
// across origins, defeating the SameSite=Strict this server issues its
// own sessions with. This server proxies the one read; the derivation
// still happens client-side, same as this server's own login.
func (s *server) handlePeerAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	peerURL := r.URL.Query().Get("url")
	if peerURL == "" {
		writeError(w, errors.New("missing url"))
		return
	}
	status, err := fetchPeerAuthStatus(r.Context(), peerURL)
	if err != nil {
		writeError(w, fmt.Errorf("check peer access protection: %w", err))
		return
	}
	// The challenge is this endpoint's own one-time login challenge for
	// that peer, unused by anything here (enrollment logs in separately,
	// with its own fresh one) and not secret — but omitted anyway, since
	// handing it out for no reason invites a caller to wonder what it is
	// for.
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":    status.Enabled,
		"iterations": status.Iterations,
		"salt":       status.Salt,
	})
}

func (s *server) handlePeerAdd(w http.ResponseWriter, r *http.Request) {
	if s.peers == nil {
		writeError(w, errors.New("peer inventory is unavailable"))
		return
	}
	// Verifier here is one the *browser* already derived from that peer's
	// password (see handlePeerAuthStatus) — never the password itself, so
	// it is transient in the same sense as request.Peer.Verifier below:
	// this handler only ever accepts a credential it can attribute to its
	// own successful login, not one carried in from the request.
	var request struct {
		peers.Peer
		Verifier string `json:"verifier,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	peer := request.Peer
	// Verifier is only ever set by this handler's own successful login
	// below, never accepted from the request as-is: a client that could
	// plant an arbitrary value here would gain nothing against a peer
	// that actually checks it, but it should not be possible to persist
	// an unverified credential at all.
	peer.Verifier = ""
	identity, err := probePeer(r.Context(), peer.URL)
	if err != nil {
		writeError(w, explainPeerProbe(peer.URL, err))
		return
	}
	peer.ID, peer.Name, peer.Accent, peer.Glyph = identity.ID, identity.Hostname, identity.Accent, identity.Glyph

	peer, ok := s.authenticateToPeer(w, r, peer, request.Verifier)
	if !ok {
		return
	}
	if err := s.peers.Add(peer); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, redactPeer(peer))
}

// authenticateToPeer checks whether peer needs a password and, if a
// password was supplied, logs in and sets peer.Verifier. It writes its
// own error response and returns ok=false on any failure — including the
// "needs one but none was given" case, flagged distinctly so a client
// can prompt rather than just report failure. Shared by handlePeerAdd
// (a new peer) and handlePeerCredential (an already-enrolled one whose
// peer turned password protection on after it was added).
// authenticateToPeer takes a verifier the *browser* already derived, not
// a password: the browser fetches this peer's salt from
// handlePeerAuthStatus below and runs the same PBKDF2 it runs for this
// server's own login, so a peer's password never crosses the wire in
// plaintext either — including to this server itself, not just beyond
// it.
func (s *server) authenticateToPeer(w http.ResponseWriter, r *http.Request, peer peers.Peer, verifierB64 string) (peers.Peer, bool) {
	// /api/identity stays public regardless of a peer's password (see
	// access_handlers.go's public-path list), so probing it during
	// enrollment never told us whether this peer needs a login too. Ask
	// separately, on the one endpoint that is also always public for
	// exactly this reason.
	status, err := fetchPeerAuthStatus(r.Context(), peer.URL)
	if err != nil {
		writeError(w, fmt.Errorf("check peer access protection: %w", err))
		return peer, false
	}
	if !status.Enabled {
		// This peer does not require a password (or no longer does): clear
		// any stale verifier rather than leave a credential on file for a
		// password that does not exist, in case it is ever set again with
		// a different one.
		peer.Verifier = ""
		return peer, true
	}
	if verifierB64 == "" {
		// Distinguishable from a generic failure so the caller can reveal
		// a password field and resubmit, instead of just reporting that
		// the PC could not be reached.
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":                  "that PC requires its access password",
			"peer_password_required": true,
		})
		return peer, false
	}
	verifier, err := base64.RawURLEncoding.DecodeString(verifierB64)
	if err != nil || len(verifier) != 32 {
		writeError(w, errors.New("invalid peer credential"))
		return peer, false
	}
	token, err := peerLogin(r.Context(), peer.URL, verifier)
	if err != nil {
		writeError(w, fmt.Errorf("log in to that PC: %w", err))
		return peer, false
	}
	// peer.Verifier is what makes every later proxied call to this peer
	// able to re-authenticate on its own — see peerAuthTransport.
	peer.Verifier = verifierB64
	if token != "" {
		s.peerSessions.set(strings.TrimSuffix(peer.URL, "/"), token)
	}
	return peer, true
}

// handlePeerCredential updates the stored credential for an *already
// enrolled* peer, without re-adding it. handlePeerAdd only ever asks for
// a peer's password once, at enrollment — a peer that turns its own
// password on afterward left every other machine's proxied calls to it
// failing with no way to fix it short of removing and re-adding the
// entry. This is that fix: same login, applied to the existing record.
func (s *server) handlePeerCredential(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.peers == nil {
		writeError(w, errors.New("peer inventory is unavailable"))
		return
	}
	// Verifier, not password — see handlePeerAuthStatus and
	// authenticateToPeer.
	var request struct {
		URL      string `json:"url"`
		Verifier string `json:"verifier"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	peer, found, err := s.peers.Find(request.URL)
	if err != nil {
		writeError(w, err)
		return
	}
	if !found {
		writeError(w, errors.New("unknown peer"))
		return
	}
	peer, ok := s.authenticateToPeer(w, r, peer, request.Verifier)
	if !ok {
		return
	}
	if err := s.peers.Update(peer); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, redactPeer(peer))
}

func (s *server) handlePeerUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.peers == nil {
		writeError(w, errors.New("peer inventory is unavailable"))
		return
	}
	var request struct {
		URL    string `json:"url"`
		Accent string `json:"accent,omitempty"`
		Name   string `json:"name,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	peer, found, err := s.peers.Find(request.URL)
	if err != nil {
		writeError(w, err)
		return
	}
	if !found {
		writeError(w, errors.New("unknown peer"))
		return
	}
	if request.Accent != "" {
		peer.Accent = request.Accent
	}
	if request.Name != "" {
		peer.Name = request.Name
	}
	if err := s.peers.Update(peer); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, redactPeer(peer))
}

type peerIdentity struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Accent   string `json:"accent"`
	Glyph    string `json:"glyph"`
}

// explainPeerProbe turns a failed probe into something a person can act
// on. Go's own error is accurate and useless here: a user adding a PC by
// address is told about 127.0.0.53:53 and a misbehaving server, neither
// of which is theirs, while the actual diagnosis is not stated anywhere.
//
// Each branch says what happened and what to try, because at this point
// the user has typed an address and has no other information.
func explainPeerProbe(rawURL string, err error) error {
	host := rawURL
	port := ""
	if parsed, parseErr := url.Parse(rawURL); parseErr == nil && parsed.Host != "" {
		host = parsed.Hostname()
		port = parsed.Port()
	}
	where := host
	if port != "" {
		where = host + ":" + port
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		// .local is reserved for mDNS by RFC 6762, so ordinary DNS servers
		// answer it with a refusal or nothing -- which is what Go reports
		// as "server misbehaving". The resolver is not broken and neither
		// is the peer: this computer just isn't doing mDNS. Naming that is
		// the difference between an unactionable error and a fix.
		if mdns.IsLocal(host) {
			return newAPIError(http.StatusBadRequest, fmt.Sprintf(
				"Can't look up %q. Names ending in .local are resolved by mDNS, "+
					"which this computer isn't set up for. Use that PC's IP address "+
					"instead, or its plain hostname if your network's DNS knows it.", host))
		}
		if dnsErr.IsNotFound {
			return newAPIError(http.StatusBadRequest, fmt.Sprintf(
				"No computer named %q could be found. Check the spelling, or use its IP address.", host))
		}
		return newAPIError(http.StatusBadRequest, fmt.Sprintf(
			"Couldn't look up %q: this computer's DNS didn't answer. Try its IP address instead.", host))
	}
	if isRefused(err) {
		return newAPIError(http.StatusBadRequest, fmt.Sprintf(
			"%s refused the connection. Eta doesn't appear to be running there%s.",
			host, portHint(port)))
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return newAPIError(http.StatusBadRequest, fmt.Sprintf(
			"%s didn't respond in time. It may be off, or a firewall may be blocking it.", where))
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return newAPIError(http.StatusBadRequest, fmt.Sprintf(
			"%s didn't respond in time. It may be off, or a firewall may be blocking it.", where))
	}
	if isUnreachable(err) {
		return newAPIError(http.StatusBadRequest, fmt.Sprintf(
			"%s can't be reached from this computer. Check they're on the same network or VPN.", host))
	}
	// Something answered but wasn't Eta: a different service on that port,
	// or the right machine on the wrong one. Worth separating from "not
	// reachable", since the fix is completely different.
	var identityErr *peerNotEtaError
	if errors.As(err, &identityErr) {
		return newAPIError(http.StatusBadRequest, fmt.Sprintf(
			"Something is running at %s, but it isn't Eta (%s). Check the port.",
			where, identityErr.detail))
	}
	return newAPIError(http.StatusBadRequest, fmt.Sprintf("Couldn't reach %s: %v", where, err))
}

func portHint(port string) string {
	if port == "" {
		return ""
	}
	return " on port " + port
}

// peerNotEtaError marks a reachable address that answered with something
// other than an Eta identity.
type peerNotEtaError struct{ detail string }

func (e *peerNotEtaError) Error() string { return e.detail }

func probePeer(ctx context.Context, rawURL string) (peerIdentity, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(rawURL, "/")+"/api/identity", nil)
	if err != nil {
		return peerIdentity{}, err
	}
	// Same transport as every other peer call, so a .local address that
	// can be added is also one that can be browsed.
	client := &http.Client{Timeout: 5 * time.Second, Transport: peerBaseTransport()}
	response, err := client.Do(request)
	if err != nil {
		return peerIdentity{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return peerIdentity{}, &peerNotEtaError{detail: "it answered " + response.Status}
	}
	var identity peerIdentity
	if err := json.NewDecoder(response.Body).Decode(&identity); err != nil {
		return peerIdentity{}, &peerNotEtaError{detail: "its reply wasn't Eta's"}
	}
	if identity.ID == "" || identity.Hostname == "" || identity.Accent == "" || identity.Glyph == "" {
		return peerIdentity{}, &peerNotEtaError{detail: "its reply was missing identity fields"}
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
	if !s.validRoot(request.Root) {
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
	if !s.validRoot(request.SourceRoot) || !s.validRoot(request.DestinationRoot) {
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
	if !s.validRoot(request.Root) {
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
	if !s.validRoot(request.Root) {
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

type publicRoot struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (s *server) handleRoots(w http.ResponseWriter, _ *http.Request) {
	// A removed root's index is never reused (see internal/roots), so
	// skipping it here rather than compacting the list still leaves
	// every remaining root's ID meaning the same thing it always did.
	out := make([]publicRoot, 0, len(s.roots))
	for i, root := range s.roots {
		if root.Removed {
			continue
		}
		out = append(out, publicRoot{ID: i, Name: root.Name})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRootAdd validates and appends a new root directory. Local paths
// are not shown to the browser anywhere else (see hostid.Identity's own
// doc comment on this) and this endpoint keeps that: the response, like
// GET /api/roots, is {id, name} only — never the path the caller sent.
func (s *server) handleRootAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.rootsStore == nil {
		writeError(w, errors.New("root inventory is unavailable"))
		return
	}
	var request struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request); err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(request.Path) == "" {
		writeError(w, errors.New("path is required"))
		return
	}
	realPath, err := resolveRootPath(request.Path)
	if err != nil {
		writeError(w, err)
		return
	}
	list, err := s.rootsStore.Add(filepath.Base(realPath), realPath)
	if err != nil {
		writeError(w, err)
		return
	}
	s.applyPersistedRoots(list)
	// The new root's ID is wherever Add put it — appended, or a
	// reactivated slot — not necessarily len(list)-1's neighbor, so it is
	// found the same way handleRoots reports IDs: by position, skipping
	// removed entries alongside it.
	id := -1
	for i, item := range list {
		if !item.Removed && item.Path == realPath {
			id = i
		}
	}
	writeJSON(w, http.StatusCreated, publicRoot{ID: id, Name: filepath.Base(realPath)})
}

func (s *server) handleRootRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.rootsStore == nil {
		writeError(w, errors.New("root inventory is unavailable"))
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil {
		writeError(w, errors.New("invalid root id"))
		return
	}
	list, err := s.rootsStore.Remove(id)
	if err != nil {
		writeError(w, err)
		return
	}
	s.applyPersistedRoots(list)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
	if err != nil || !s.validRoot(rootID) {
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
	return entry{Name: name, Path: filepath.ToSlash(path), Kind: kind, Size: info.Size(), Modified: info.ModTime(), Hidden: isHidden(name, info)}
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
	// Every JSON endpoint here answers with mutable control-plane state:
	// desktop windows, directory listings, peer inventory, transfer jobs.
	// None of it carries a validator, so a client or intermediary that
	// applies heuristic freshness may reuse an old body and show a stale
	// desktop or a directory that no longer looks like that. Byte and
	// thumbnail responses are cached deliberately elsewhere, with ETags;
	// they do not pass through here.
	w.Header().Set("Cache-Control", "no-store")
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
