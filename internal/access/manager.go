package access

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

const (
	PasswordHashVersion    = "v1"
	PasswordHashAlgorithm  = "pbkdf2-sha256"
	PasswordHashIterations = 600_000
	passwordSaltBytes      = 16
	passwordVerifierBytes  = 32
	challengeTTL           = 5 * time.Minute
	SessionTTL             = 365 * 24 * time.Hour
	SessionCookie          = "eta_access_session"
	maxChallenges          = 1024
)

var (
	ErrUnauthorized = errors.New("access authentication required")
	ErrRateLimited  = errors.New("too many failed password attempts; try again shortly")
)

type passwordRecord struct {
	Salt     []byte
	Verifier []byte
}

func (r passwordRecord) encoded() string {
	return strings.Join([]string{
		PasswordHashVersion,
		PasswordHashAlgorithm,
		strconv.Itoa(PasswordHashIterations),
		base64.RawURLEncoding.EncodeToString(r.Salt),
		base64.RawURLEncoding.EncodeToString(r.Verifier),
	}, ".")
}

// ParsePasswordHash accepts the only verifier format this package writes.
// The browser derives the verifier; the server stores it without ever
// receiving the raw password. The salt and iteration count are public KDF
// parameters; the verifier is the secret and is never returned by an API.
func ParsePasswordHash(encoded string) (*passwordRecord, error) {
	if encoded == "" {
		return nil, nil
	}
	parts := strings.Split(encoded, ".")
	if len(parts) != 5 || parts[0] != PasswordHashVersion || parts[1] != PasswordHashAlgorithm {
		return nil, errors.New("invalid access password hash format")
	}
	iterations, err := strconv.Atoi(parts[2])
	if err != nil || iterations != PasswordHashIterations {
		return nil, errors.New("unsupported access password hash parameters")
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(salt) != passwordSaltBytes {
		return nil, errors.New("invalid access password hash salt")
	}
	verifier, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil || len(verifier) != passwordVerifierBytes {
		return nil, errors.New("invalid access password hash verifier")
	}
	return &passwordRecord{Salt: salt, Verifier: verifier}, nil
}

// DeriveVerifier runs the same PBKDF2-HMAC-SHA256 the browser runs, for
// the one caller that has to do it server-side: logging in to a peer on
// the user's behalf (see internal/peers' credential cache). It is never
// used against Eta's own login, which only ever sees a verifier the
// browser already derived — the raw password does not pass through this
// server for its own login, only for a peer's.
func DeriveVerifier(password string, salt []byte) []byte {
	return pbkdf2.Key([]byte(password), salt, PasswordHashIterations, passwordVerifierBytes, sha256.New)
}

type loginAttempt struct {
	Failures    int
	RetryAt     time.Time
	LastFailure time.Time
}

// Manager holds one instance's access password state: the configured
// verifier, in-flight login challenges, live sessions, and a per-client
// failed-attempt backoff. All in memory — a restart resets sessions and
// backoff state, matching the no-account, no-database design; only the
// password verifier itself persists, via Config.
type Manager struct {
	mu         sync.Mutex
	record     *passwordRecord
	challenges map[string]time.Time
	sessions   map[string]time.Time
	attempts   map[string]loginAttempt
}

func NewManager() *Manager {
	return &Manager{
		challenges: make(map[string]time.Time),
		sessions:   make(map[string]time.Time),
		attempts:   make(map[string]loginAttempt),
	}
}

func (m *Manager) Configure(encoded string) error {
	record, err := ParsePasswordHash(encoded)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record = record
	m.challenges = make(map[string]time.Time)
	m.sessions = make(map[string]time.Time)
	m.attempts = make(map[string]loginAttempt)
	return nil
}

func (m *Manager) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.record != nil
}

// StatusAndChallenge returns public KDF parameters and a single-use login
// challenge from one locked snapshot, so a concurrent password change
// cannot pair an old salt with a challenge for the new verifier.
func (m *Manager) StatusAndChallenge() (enabled bool, salt, challenge string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.record == nil {
		return false, "", "", nil
	}
	now := time.Now()
	m.cleanExpiredLocked(now)
	if len(m.challenges) >= maxChallenges {
		return false, "", "", errors.New("too many pending login attempts")
	}
	challenge, err = randomToken()
	if err != nil {
		return false, "", "", err
	}
	m.challenges[challenge] = now.Add(challengeTTL)
	return true, base64.RawURLEncoding.EncodeToString(m.record.Salt), challenge, nil
}

func (m *Manager) Authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(SessionCookie)
	if err != nil || cookie.Value == "" {
		return false
	}
	return m.ValidSession(cookie.Value)
}

// ValidSession checks a bare session token, for callers that carry it
// somewhere other than this server's own cookie jar — a peer's proxied
// request forwards the token it was issued, not a browser cookie read
// from this request.
func (m *Manager) ValidSession(token string) bool {
	if token == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	expiresAt, ok := m.sessions[token]
	if !ok || !expiresAt.After(time.Now()) {
		delete(m.sessions, token)
		return false
	}
	return true
}

func (m *Manager) Login(challenge, proof, remoteAddr string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.cleanExpiredLocked(now)
	if m.record == nil {
		return "", nil
	}

	client := clientIP(remoteAddr)
	if attempt, ok := m.attempts[client]; ok && attempt.RetryAt.After(now) {
		return "", ErrRateLimited
	}

	expiresAt, ok := m.challenges[challenge]
	delete(m.challenges, challenge) // one-time, including bad attempts
	if !ok || !expiresAt.After(now) {
		m.recordFailureLocked(client, now)
		return "", ErrUnauthorized
	}
	got, err := base64.RawURLEncoding.DecodeString(proof)
	if err != nil || len(got) != sha256.Size {
		m.recordFailureLocked(client, now)
		return "", ErrUnauthorized
	}
	mac := hmac.New(sha256.New, m.record.Verifier)
	_, _ = mac.Write([]byte(challenge))
	want := mac.Sum(nil)
	if subtle.ConstantTimeCompare(got, want) != 1 {
		m.recordFailureLocked(client, now)
		return "", ErrUnauthorized
	}

	delete(m.attempts, client)
	return m.newSessionLocked(now)
}

// UpdatePassword atomically prevents an unauthenticated caller from
// replacing a password that became enabled between middleware evaluation
// and this handler running. A new verifier invalidates all prior
// sessions.
func (m *Manager) UpdatePassword(encoded string, authorized bool) (token string, revokeLiveConnections bool, err error) {
	record, err := ParsePasswordHash(encoded)
	if err != nil {
		return "", false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.record != nil && !authorized {
		return "", false, ErrUnauthorized
	}
	revokeLiveConnections = m.record != nil || record != nil
	m.record = record
	m.challenges = make(map[string]time.Time)
	m.sessions = make(map[string]time.Time)
	m.attempts = make(map[string]loginAttempt)
	if record == nil {
		return "", revokeLiveConnections, nil
	}
	token, err = m.newSessionLocked(time.Now())
	return token, revokeLiveConnections, err
}

func (m *Manager) newSessionLocked(now time.Time) (string, error) {
	m.cleanExpiredLocked(now)
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	m.sessions[token] = now.Add(SessionTTL)
	return token, nil
}

func (m *Manager) recordFailureLocked(client string, now time.Time) {
	attempt := m.attempts[client]
	attempt.Failures++
	// 1, 2, 4, ... seconds, capped near a minute. Deliberately in-memory:
	// a restart resets it, matching the no-account design.
	delay := time.Second << min(attempt.Failures-1, 6)
	attempt.RetryAt = now.Add(delay)
	attempt.LastFailure = now
	m.attempts[client] = attempt
}

func (m *Manager) cleanExpiredLocked(now time.Time) {
	for challenge, expiresAt := range m.challenges {
		if !expiresAt.After(now) {
			delete(m.challenges, challenge)
		}
	}
	for session, expiresAt := range m.sessions {
		if !expiresAt.After(now) {
			delete(m.sessions, session)
		}
	}
	for client, attempt := range m.attempts {
		if !attempt.LastFailure.Add(15 * time.Minute).After(now) {
			delete(m.attempts, client)
		}
	}
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
