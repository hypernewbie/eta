package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hypernewbie/eta/internal/access"
	"github.com/hypernewbie/eta/internal/mdns"
	"github.com/hypernewbie/eta/internal/peers"
)

// friendlyPeerError turns a transport-level failure to reach a peer
// (connection refused, DNS failure, timeout) into a message meant to be
// read — not Go's own error text, which names the exact internal request
// URL and the raw dial error ("dial tcp 100.92.136.40:7080: connect:
// connection refused"). http.Client.Do/Get/Post only ever errors this
// way; an HTTP status the peer itself returned, even a 5xx, arrives as a
// normal response, not an error — so every err != nil right after one of
// those calls against a peer is exactly this case, never anything else.
// The original error is still logged server-side, just not shown.
func friendlyPeerError(peer peers.Peer, err error) error {
	log.Printf("peer %s unreachable: %v", peer.URL, err)
	name := peer.Name
	if name == "" {
		name = peer.URL
	}
	return newAPIError(http.StatusBadGateway, name+" is offline or unreachable")
}

// A peer that has set its own access password is, from this server's
// point of view, just another login: someone here typed that peer's
// password once when adding it (see handlePeerAdd), this server derived
// and kept the same PBKDF2 verifier the peer's own browser would have
// derived, and every proxied request presents that peer's own session
// cookie. There is no separate "peer key" concept — a peer is a client
// who knows the password, same as a person at a browser.

// peerSessionCache holds session tokens this server has obtained by
// logging in to *other* Eta instances' access passwords, keyed by peer
// URL. It is purely a performance cache: everything needed to rebuild an
// entry lives in the persisted peer record's Verifier field, so losing
// this cache — a restart, or the peer itself restarting and dropping its
// own in-memory sessions — costs one extra login round trip, not a lost
// connection or a re-prompt for the password.
type peerSessionCache struct {
	mu       sync.Mutex
	sessions map[string]string
}

func newPeerSessionCache() *peerSessionCache {
	return &peerSessionCache{sessions: make(map[string]string)}
}

func (c *peerSessionCache) get(peerURL string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessions[peerURL]
}

func (c *peerSessionCache) set(peerURL, token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if token == "" {
		delete(c.sessions, peerURL)
		return
	}
	c.sessions[peerURL] = token
}

// peerClient returns an *http.Client whose outbound requests to this one
// peer carry a cached session, and transparently re-authenticate once on
// a 401 if a verifier is on file for it. Every server-to-server call this
// server makes on a peer's own /api/* surface should be built with this,
// not a bare &http.Client{} — that surface is exactly what a peer with a
// password now rejects anonymously.
func (s *server) peerClient(peer peers.Peer, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: s.peerTransport(peer.URL, peer.Verifier),
	}
}

// peerClientForURL is for the handful of call sites that address a
// destination by bare URL rather than an internal.peers.Peer — a managed
// source pushing to a caller-supplied destination, or a persisted
// transfer job that only kept the URL. It looks the URL up against the
// peer inventory on a best-effort basis to recover a stored verifier; a
// destination that both requires a password and was never enrolled here
// has no credential to use and proceeds anonymously, exactly as every
// destination did before this feature existed.
func (s *server) peerClientForURL(peerURL string, timeout time.Duration) *http.Client {
	var verifier string
	if s.peers != nil {
		if peer, found, err := s.peers.Find(peerURL); err == nil && found {
			verifier = peer.Verifier
		}
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: s.peerTransport(peerURL, verifier),
	}
}

func (s *server) peerTransport(peerURL, verifierB64 string) http.RoundTripper {
	var verifier []byte
	if verifierB64 != "" {
		if decoded, err := base64.RawURLEncoding.DecodeString(verifierB64); err == nil {
			verifier = decoded
		}
	}
	return &peerAuthTransport{
		base:     peerBaseTransport(),
		peerURL:  strings.TrimSuffix(peerURL, "/"),
		verifier: verifier,
		cache:    s.peerSessions,
	}
}

// peerAuthTransport attaches this server's cached session for a peer to
// every outbound request, and re-authenticates once on a 401 if a
// verifier is on file for that peer.
type peerAuthTransport struct {
	base     http.RoundTripper
	peerURL  string
	verifier []byte // nil: peer has no password on file, or none is known
	cache    *peerSessionCache
}

func (t *peerAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if token := t.cache.get(t.peerURL); token != "" {
		req.Header.Set("Cookie", access.SessionCookie+"="+token)
	}
	response, err := t.base.RoundTrip(req)
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		return response, err
	}
	// No verifier on file at all: nothing to retry with. This is the
	// common new case — a peer enrolled before it had a password, which
	// then turned one on. Every handler here just relays the peer's
	// status and body verbatim, so replacing the body with a signal the
	// client can act on (see web/app.ts's api()) reaches the browser
	// through all ~9 call sites for free, rather than needing each one
	// updated to recognise this case individually.
	if len(t.verifier) == 0 {
		response.Body.Close()
		return peerAuthRequiredResponse(req), nil
	}
	// Exactly one retry, not a loop: a peer that rejects a *correct*
	// verifier — its password changed since this one was cached — must
	// fail fast rather than being hammered with relogin attempts.
	if req.Body != nil && req.GetBody == nil {
		// This request's body was already consumed and cannot be safely
		// replayed (a caller built it from a reader that isn't one of the
		// GetBody-populating types net/http recognises). Surface the
		// signal rather than resend a request with an empty body.
		response.Body.Close()
		return peerAuthRequiredResponse(req), nil
	}
	response.Body.Close()
	token, loginErr := peerLogin(req.Context(), t.peerURL, t.verifier)
	if loginErr != nil || token == "" {
		return peerAuthRequiredResponse(req), nil
	}
	t.cache.set(t.peerURL, token)
	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return peerAuthRequiredResponse(req), nil
		}
		retry.Body = body
	}
	retry.Header.Set("Cookie", access.SessionCookie+"="+token)
	retryResponse, err := t.base.RoundTrip(retry)
	if err == nil && retryResponse.StatusCode == http.StatusUnauthorized {
		// The cached verifier itself is now wrong — the peer's password
		// changed since it was recorded here — rather than just its
		// session having expired. Same signal: there is no password this
		// server can retry with on its own.
		retryResponse.Body.Close()
		return peerAuthRequiredResponse(req), nil
	}
	return retryResponse, err
}

// peerAuthRequiredResponse tells the browser this peer needs a
// password this server does not have, distinctly from every other
// reason a proxied call can fail: peer_auth_required is what
// web/app.ts's api() looks for to prompt for the peer's password
// in place, instead of surfacing a bare "Request failed (401)".
func peerAuthRequiredResponse(req *http.Request) *http.Response {
	body := `{"error":"that PC now requires its access password","peer_auth_required":true}`
	return &http.Response{
		Status:        "401 Unauthorized",
		StatusCode:    http.StatusUnauthorized,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}

// peerAuthStatus mirrors the JSON /api/auth/status answers with — just
// the fields this server needs to log in.
type peerAuthStatus struct {
	Enabled    bool   `json:"enabled"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Challenge  string `json:"challenge"`
}

func fetchPeerAuthStatus(ctx context.Context, peerURL string) (peerAuthStatus, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(peerURL, "/")+"/api/auth/status", nil)
	if err != nil {
		return peerAuthStatus{}, err
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return peerAuthStatus{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return peerAuthStatus{}, fmt.Errorf("peer auth status: %s", response.Status)
	}
	var status peerAuthStatus
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		return peerAuthStatus{}, err
	}
	return status, nil
}

// peerLogin authenticates to a peer using a verifier already derived —
// either one just computed from a freshly typed password (enrolling a
// peer) or one loaded back from the persisted peer record (routine
// re-authentication) — and returns the session token the peer issues. It
// never sees or stores the peer's plaintext password.
func peerLogin(ctx context.Context, peerURL string, verifier []byte) (string, error) {
	status, err := fetchPeerAuthStatus(ctx, peerURL)
	if err != nil {
		return "", err
	}
	if !status.Enabled {
		return "", nil // this peer has no password: nothing to log in to
	}
	mac := hmac.New(sha256.New, verifier)
	_, _ = mac.Write([]byte(status.Challenge))
	proof := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	body, err := json.Marshal(map[string]string{"challenge": status.Challenge, "proof": proof})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(peerURL, "/")+"/api/auth/login", strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", access.ErrUnauthorized
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == access.SessionCookie {
			return cookie.Value, nil
		}
	}
	return "", errors.New("peer did not issue a session")
}

// peerBaseTransport is the transport under every server-to-server call.
// It differs from http.DefaultTransport in one way: it can dial a .local
// peer, which Go's own resolver cannot (see internal/mdns).
//
// Shared, because a transport per request would discard connection
// pooling and start a fresh handshake for every file listing.
var peerBaseTransport = sync.OnceValue(func() http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = mdns.DialContext
	return transport
})
