package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hypernewbie/eta/internal/access"
	"github.com/hypernewbie/eta/internal/peers"
)

// setPassword configures s directly (bypassing the derive-in-the-browser
// step, since these are Go-side tests) and returns the raw password's
// verifier so a test can log in as if it were the browser.
func setPassword(t *testing.T, s *server, password string) []byte {
	t.Helper()
	salt := []byte("0123456789abcdef")
	verifier := access.DeriveVerifier(password, salt)
	hash := strings.Join([]string{
		access.PasswordHashVersion,
		access.PasswordHashAlgorithm,
		"600000",
		base64.RawURLEncoding.EncodeToString(salt),
		base64.RawURLEncoding.EncodeToString(verifier),
	}, ".")
	if err := s.access.Configure(hash); err != nil {
		t.Fatal(err)
	}
	return verifier
}

func hmacProof(verifier []byte, challenge string) string {
	mac := hmac.New(sha256.New, verifier)
	_, _ = mac.Write([]byte(challenge))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// loginAs drives the real HTTP status -> login handshake through
// routes(), the same one the browser drives, and returns the session
// cookie.
func loginAs(t *testing.T, s *server, verifier []byte) *http.Cookie {
	t.Helper()
	statusW := httptest.NewRecorder()
	s.routes().ServeHTTP(statusW, httptest.NewRequest(http.MethodGet, "/api/auth/status", nil))
	var status struct {
		Challenge string `json:"challenge"`
	}
	if err := json.NewDecoder(statusW.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	body := `{"challenge":"` + status.Challenge + `","proof":"` + hmacProof(verifier, status.Challenge) + `"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	loginW := httptest.NewRecorder()
	s.routes().ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("login: %d %s", loginW.Code, loginW.Body.String())
	}
	cookies := loginW.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %#v", cookies)
	}
	return cookies[0]
}

func TestAccessMiddlewareGatesAPIButNotTheShell(t *testing.T) {
	s, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	setPassword(t, s, "correct-horse-battery")

	for _, path := range []string{"/api/roots", "/api/peers", "/api/state"} {
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("protected %s: got %d want 401", path, w.Code)
		}
	}
	for _, path := range []string{"/", "/api/auth/status", "/api/auth/login", "/api/identity", "/api/healthz"} {
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code == http.StatusUnauthorized {
			t.Errorf("public %s was gated", path)
		}
	}
}

func TestLoginRoundTripThroughRoutesAuthorizesProtectedRequest(t *testing.T) {
	s, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	verifier := setPassword(t, s, "hunter2!!")
	cookie := loginAs(t, s, verifier)

	protected := httptest.NewRequest(http.MethodGet, "/api/roots", nil)
	protected.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, protected)
	if w.Code != http.StatusOK {
		t.Fatalf("authenticated request: got %d %s", w.Code, w.Body.String())
	}
}

func TestAuthPasswordHandlerSetsUpdatesAndClears(t *testing.T) {
	s, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	s.accessPath = filepath.Join(t.TempDir(), "access.json")

	salt := []byte("abcdefghijklmnop")
	verifier1 := access.DeriveVerifier("first-password", salt)
	hash1 := strings.Join([]string{
		access.PasswordHashVersion, access.PasswordHashAlgorithm, "600000",
		base64.RawURLEncoding.EncodeToString(salt), base64.RawURLEncoding.EncodeToString(verifier1),
	}, ".")

	setW := httptest.NewRecorder()
	s.routes().ServeHTTP(setW, httptest.NewRequest(http.MethodPost, "/api/auth/password", strings.NewReader(`{"password_hash":"`+hash1+`"}`)))
	if setW.Code != http.StatusOK {
		t.Fatalf("set password: %d %s", setW.Code, setW.Body.String())
	}
	cookies := setW.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected a session cookie from setting the password, got %#v", cookies)
	}

	// Persisted to disk, not just applied in memory.
	saved, err := access.Load(s.accessPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.PasswordHash != hash1 {
		t.Fatalf("password hash not persisted: got %q", saved.PasswordHash)
	}

	// An unauthenticated request cannot change an already-set password.
	verifier2 := access.DeriveVerifier("second-password", salt)
	hash2 := strings.Join([]string{
		access.PasswordHashVersion, access.PasswordHashAlgorithm, "600000",
		base64.RawURLEncoding.EncodeToString(salt), base64.RawURLEncoding.EncodeToString(verifier2),
	}, ".")
	unauthChange := httptest.NewRequest(http.MethodPost, "/api/auth/password", strings.NewReader(`{"password_hash":"`+hash2+`"}`))
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, unauthChange)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized password change: got %d want 401", w.Code)
	}

	// With the session from setting it, clearing succeeds and the API is
	// open again.
	clearReq := httptest.NewRequest(http.MethodPost, "/api/auth/password", strings.NewReader(`{"password_hash":""}`))
	clearReq.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	s.routes().ServeHTTP(w, clearReq)
	if w.Code != http.StatusOK {
		t.Fatalf("clear password: %d %s", w.Code, w.Body.String())
	}
	if s.access.Enabled() {
		t.Fatal("manager still enabled after clearing")
	}
	openReq := httptest.NewRequest(http.MethodGet, "/api/roots", nil)
	w = httptest.NewRecorder()
	s.routes().ServeHTTP(w, openReq)
	if w.Code != http.StatusOK {
		t.Fatalf("api should be open once the password is cleared: got %d", w.Code)
	}
}

// Each phase below enrolls against its own fresh receiver instance,
// deliberately: a wrong-password attempt arms that peer's own rate
// limiter (see access.Manager's recordFailureLocked), and chaining a
// correct attempt right after it on the *same* peer would be testing the
// rate limiter, not enrollment.
func newPasswordProtectedReceiver(t *testing.T) *httptest.Server {
	t.Helper()
	receiver, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	setPassword(t, receiver, "receiver-secret")
	httpServer := httptest.NewServer(receiver.routes())
	t.Cleanup(httpServer.Close)
	return httpServer
}

func TestPeerAddWithoutAPasswordIsFlaggedDistinctly(t *testing.T) {
	receiverHTTP := newPasswordProtectedReceiver(t)
	coordinator, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.peers = peers.New(filepath.Join(t.TempDir(), "peers.json"))

	noPassword := httptest.NewRequest(http.MethodPost, "/api/peers", strings.NewReader(`{"url":"`+receiverHTTP.URL+`"}`))
	w := httptest.NewRecorder()
	coordinator.routes().ServeHTTP(w, noPassword)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a password, got %d %s", w.Code, w.Body.String())
	}
	var flagged map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &flagged); err != nil {
		t.Fatal(err)
	}
	if flagged["peer_password_required"] != true {
		t.Fatalf("response did not flag peer_password_required: %v", flagged)
	}
	if items, _ := coordinator.peers.List(); len(items) != 0 {
		t.Fatalf("no enrollment should have happened: %#v", items)
	}
}

func TestPeerAddWithWrongPasswordFailsAndEnrollsNothing(t *testing.T) {
	receiverHTTP := newPasswordProtectedReceiver(t)
	coordinator, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.peers = peers.New(filepath.Join(t.TempDir(), "peers.json"))

	wrong := httptest.NewRequest(http.MethodPost, "/api/peers", strings.NewReader(`{"url":"`+receiverHTTP.URL+`","password":"nope"}`))
	w := httptest.NewRecorder()
	coordinator.routes().ServeHTTP(w, wrong)
	if w.Code == http.StatusCreated {
		t.Fatal("wrong password should not enroll the peer")
	}
	if items, _ := coordinator.peers.List(); len(items) != 0 {
		t.Fatalf("a failed enrollment left an entry behind: %#v", items)
	}
}

func TestPeerAddWithCorrectPasswordEnrollsAndStoresOnlyAVerifier(t *testing.T) {
	receiverHTTP := newPasswordProtectedReceiver(t)
	coordinator, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.peers = peers.New(filepath.Join(t.TempDir(), "peers.json"))

	right := httptest.NewRequest(http.MethodPost, "/api/peers", strings.NewReader(`{"url":"`+receiverHTTP.URL+`","password":"receiver-secret"}`))
	w := httptest.NewRecorder()
	coordinator.routes().ServeHTTP(w, right)
	if w.Code != http.StatusCreated {
		t.Fatalf("correct password: got %d %s", w.Code, w.Body.String())
	}
	items, err := coordinator.peers.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("peers = %#v err=%v", items, err)
	}
	if items[0].Verifier == "" {
		t.Fatal("enrollment did not persist a verifier")
	}
	if strings.Contains(items[0].Verifier, "receiver-secret") {
		t.Fatal("verifier leaked the raw password")
	}
}

// TestProxiedCallReauthenticatesAfterPeerSessionIsLost exercises the one
// path that is easy to get wrong: a peer that requires a password, whose
// session this server cached, going stale — which happens on every
// restart of that peer, since sessions are deliberately in-memory only
// (see access.Manager's doc comment). A proxied call must recover on its
// own rather than surface a 401 to the browser for a credential that is
// actually still valid.
func TestProxiedCallReauthenticatesAfterPeerSessionIsLost(t *testing.T) {
	receiverRoot := t.TempDir()
	receiver, err := newServer([]string{receiverRoot})
	if err != nil {
		t.Fatal(err)
	}
	setPassword(t, receiver, "receiver-secret")
	receiverHTTP := httptest.NewServer(receiver.routes())
	defer receiverHTTP.Close()

	coordinator, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.peers = peers.New(filepath.Join(t.TempDir(), "peers.json"))
	add := httptest.NewRequest(http.MethodPost, "/api/peers", strings.NewReader(`{"url":"`+receiverHTTP.URL+`","password":"receiver-secret"}`))
	w := httptest.NewRecorder()
	coordinator.routes().ServeHTTP(w, add)
	if w.Code != http.StatusCreated {
		t.Fatalf("enroll: got %d %s", w.Code, w.Body.String())
	}

	list := func() int {
		req := httptest.NewRequest(http.MethodGet, "/api/remote/list?peer="+receiverHTTP.URL+"&root=0&path=", nil)
		w := httptest.NewRecorder()
		coordinator.routes().ServeHTTP(w, req)
		return w.Code
	}
	if code := list(); code != http.StatusOK {
		t.Fatalf("first proxied call: got %d", code)
	}

	// Simulate the receiver restarting: its in-memory sessions are gone,
	// but its password (and thus what a correct verifier proves) is
	// unchanged. Re-Configure with the same hash reproduces exactly
	// that: a fresh Manager state with the same record.
	setPassword(t, receiver, "receiver-secret")

	if code := list(); code != http.StatusOK {
		t.Fatalf("proxied call after the peer's sessions were dropped: got %d, want automatic relogin to recover it", code)
	}
}
