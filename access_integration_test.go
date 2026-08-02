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

// peerVerifier derives the verifier a browser would send for password
// against the fixed salt setPassword always configures receivers with,
// base64url-encoded exactly as the client and the /api/peers* endpoints
// exchange it. Tests build request bodies with this instead of a
// "password" field, matching what actually crosses the wire now that
// derivation happens in the browser (see derivePeerVerifier in
// web/app.ts and handlePeerAuthStatus in main.go).
func peerVerifier(password string) string {
	salt := []byte("0123456789abcdef")
	return base64.RawURLEncoding.EncodeToString(access.DeriveVerifier(password, salt))
}

// TestAlreadyEnrolledPeerTurningOnAPasswordIsRecoverable is the exact
// scenario a password feature added after peers already exist has to get
// right: A and B are already linked with no password anywhere. B turns
// one on. A's next proxied call to B must fail *distinctly* — not a bare
// 401 — and there must be a way to fix it without removing and re-adding
// B, since handlePeerAdd only ever asks for a password once, at
// enrollment.
func TestAlreadyEnrolledPeerTurningOnAPasswordIsRecoverable(t *testing.T) {
	receiverRoot := t.TempDir()
	receiver, err := newServer([]string{receiverRoot})
	if err != nil {
		t.Fatal(err)
	}
	receiverHTTP := httptest.NewServer(receiver.routes())
	defer receiverHTTP.Close()

	coordinator, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.peers = peers.New(filepath.Join(t.TempDir(), "peers.json"))
	// Enrolled while the receiver had no password: exactly today's
	// existing peers once this feature ships.
	add := httptest.NewRequest(http.MethodPost, "/api/peers", strings.NewReader(`{"url":"`+receiverHTTP.URL+`"}`))
	w := httptest.NewRecorder()
	coordinator.routes().ServeHTTP(w, add)
	if w.Code != http.StatusCreated {
		t.Fatalf("enroll: got %d %s", w.Code, w.Body.String())
	}

	list := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/remote/list?peer="+receiverHTTP.URL+"&root=0&path=", nil)
		w := httptest.NewRecorder()
		coordinator.routes().ServeHTTP(w, req)
		return w
	}
	if w := list(); w.Code != http.StatusOK {
		t.Fatalf("before the receiver has a password: got %d %s", w.Code, w.Body.String())
	}

	// The linked remote now requires a password this side never learned.
	setPassword(t, receiver, "newly-added-secret")

	w = list()
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected the proxied call to fail once the peer needs a password, got %d", w.Code)
	}
	var flagged map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &flagged); err != nil {
		t.Fatalf("response was not the distinct signal, was: %s", w.Body.String())
	}
	if flagged["peer_auth_required"] != true {
		t.Fatalf("proxied 401 did not flag peer_auth_required: %v", flagged)
	}

	// Fixed without removing and re-adding the peer: the credential
	// endpoint updates the existing entry in place.
	credential := httptest.NewRequest(http.MethodPost, "/api/peers/credential", strings.NewReader(`{"url":"`+receiverHTTP.URL+`","verifier":"`+peerVerifier("newly-added-secret")+`"}`))
	credW := httptest.NewRecorder()
	coordinator.routes().ServeHTTP(credW, credential)
	if credW.Code != http.StatusOK {
		t.Fatalf("set credential: got %d %s", credW.Code, credW.Body.String())
	}
	items, err := coordinator.peers.List()
	if err != nil || len(items) != 1 || items[0].Verifier == "" {
		t.Fatalf("credential was not persisted on the existing entry: %#v err=%v", items, err)
	}

	if w := list(); w.Code != http.StatusOK {
		t.Fatalf("after setting the credential: got %d %s", w.Code, w.Body.String())
	}

	// A wrong password against the credential endpoint must not silently
	// "succeed" and must not corrupt the working credential already on
	// file... but note it does overwrite it, by design (this is an
	// explicit edit action) — verify it reports failure rather than 200.
	wrongCredential := httptest.NewRequest(http.MethodPost, "/api/peers/credential", strings.NewReader(`{"url":"`+receiverHTTP.URL+`","verifier":"`+peerVerifier("nope")+`"}`))
	wrongW := httptest.NewRecorder()
	coordinator.routes().ServeHTTP(wrongW, wrongCredential)
	if wrongW.Code == http.StatusOK {
		t.Fatal("wrong password against the credential endpoint reported success")
	}
}

// TestPeerVerifierNeverReachesTheBrowser guards against the credential
// this server uses to log in to a peer (see peer_auth.go) leaking into
// a response the browser can read — it authorizes against that peer the
// same way a password would, so exposing it is exposing a credential,
// not display data.
func TestPeerVerifierNeverReachesTheBrowser(t *testing.T) {
	receiverHTTP := newPasswordProtectedReceiver(t)
	coordinator, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.peers = peers.New(filepath.Join(t.TempDir(), "peers.json"))

	add := httptest.NewRequest(http.MethodPost, "/api/peers", strings.NewReader(`{"url":"`+receiverHTTP.URL+`","verifier":"`+peerVerifier("receiver-secret")+`"}`))
	addW := httptest.NewRecorder()
	coordinator.routes().ServeHTTP(addW, add)
	if addW.Code != http.StatusCreated {
		t.Fatalf("enroll: got %d %s", addW.Code, addW.Body.String())
	}
	if strings.Contains(addW.Body.String(), "verifier") {
		t.Fatalf("POST /api/peers response leaked a verifier: %s", addW.Body.String())
	}

	// The persisted record still has one — redaction is only ever applied
	// to the browser-facing response, never to disk.
	stored, err := coordinator.peers.List()
	if err != nil || len(stored) != 1 || stored[0].Verifier == "" {
		t.Fatalf("verifier should still be persisted server-side: %#v err=%v", stored, err)
	}

	listW := httptest.NewRecorder()
	coordinator.routes().ServeHTTP(listW, httptest.NewRequest(http.MethodGet, "/api/peers", nil))
	if strings.Contains(listW.Body.String(), "verifier") {
		t.Fatalf("GET /api/peers response leaked a verifier: %s", listW.Body.String())
	}

	credential := httptest.NewRequest(http.MethodPost, "/api/peers/credential", strings.NewReader(`{"url":"`+receiverHTTP.URL+`","verifier":"`+peerVerifier("receiver-secret")+`"}`))
	credW := httptest.NewRecorder()
	coordinator.routes().ServeHTTP(credW, credential)
	if strings.Contains(credW.Body.String(), "verifier") {
		t.Fatalf("POST /api/peers/credential response leaked a verifier: %s", credW.Body.String())
	}
}

// TestPeerAddIgnoresAPlaintextPasswordField locks in the fix: a
// "password" field alone must not authenticate a peer add. Only a
// pre-derived "verifier" does — the browser derives it (see
// derivePeerVerifier in web/app.ts), and this server never accepts nor
// looks for a plaintext password in this request at all.
func TestPeerAddIgnoresAPlaintextPasswordField(t *testing.T) {
	receiverHTTP := newPasswordProtectedReceiver(t)
	coordinator, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.peers = peers.New(filepath.Join(t.TempDir(), "peers.json"))

	req := httptest.NewRequest(http.MethodPost, "/api/peers", strings.NewReader(`{"url":"`+receiverHTTP.URL+`","password":"receiver-secret"}`))
	w := httptest.NewRecorder()
	coordinator.routes().ServeHTTP(w, req)
	// A correct plaintext password with no "verifier" field is treated
	// exactly like no credential at all: flagged, not accepted.
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("a plaintext password field authenticated the peer add: got %d %s", w.Code, w.Body.String())
	}
	var flagged map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &flagged); err != nil || flagged["peer_password_required"] != true {
		t.Fatalf("expected peer_password_required, got %s", w.Body.String())
	}
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

	wrong := httptest.NewRequest(http.MethodPost, "/api/peers", strings.NewReader(`{"url":"`+receiverHTTP.URL+`","verifier":"`+peerVerifier("nope")+`"}`))
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

	right := httptest.NewRequest(http.MethodPost, "/api/peers", strings.NewReader(`{"url":"`+receiverHTTP.URL+`","verifier":"`+peerVerifier("receiver-secret")+`"}`))
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
	add := httptest.NewRequest(http.MethodPost, "/api/peers", strings.NewReader(`{"url":"`+receiverHTTP.URL+`","verifier":"`+peerVerifier("receiver-secret")+`"}`))
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
