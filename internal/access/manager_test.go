package access

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

func testHash() string {
	salt := bytes.Repeat([]byte{0x11}, passwordSaltBytes)
	verifier := bytes.Repeat([]byte{0x22}, passwordVerifierBytes)
	return passwordRecord{Salt: salt, Verifier: verifier}.encoded()
}

func proofFor(verifier []byte, challenge string) string {
	mac := hmac.New(sha256.New, verifier)
	_, _ = mac.Write([]byte(challenge))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestPasswordHashRoundTripAndRejectsInvalidValues(t *testing.T) {
	record, err := ParsePasswordHash(testHash())
	if err != nil {
		t.Fatalf("parse valid verifier: %v", err)
	}
	if record == nil || len(record.Salt) != passwordSaltBytes || len(record.Verifier) != passwordVerifierBytes {
		t.Fatalf("parsed invalid record: %#v", record)
	}
	if record.encoded() != testHash() {
		t.Errorf("round trip: got %q want %q", record.encoded(), testHash())
	}
	for _, invalid := range []string{
		"v1.pbkdf2-sha256.1.salt.verifier",
		"v2.pbkdf2-sha256.600000.salt.verifier",
		"v1.pbkdf2-sha1.600000.salt.verifier",
		"not-a-record",
	} {
		if _, err := ParsePasswordHash(invalid); err == nil {
			t.Errorf("ParsePasswordHash(%q) unexpectedly succeeded", invalid)
		}
	}
	if record, err := ParsePasswordHash(""); err != nil || record != nil {
		t.Errorf("empty verifier = disabled: record=%#v err=%v", record, err)
	}
}

// DeriveVerifier must match the browser's @noble/hashes pbkdf2Async byte
// for byte: it authenticates to a peer using the same challenge/proof
// protocol the browser uses against this server, so any drift here would
// mean Eta can talk to its own login but not a peer's. This vector was
// cross-checked directly against the vendored JS bundle before this
// package existed; the hex is fixed so a change to either side's KDF
// parameters breaks a test instead of silently failing at 3am.
func TestDeriveVerifierMatchesKnownVector(t *testing.T) {
	salt := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	got := DeriveVerifier("correct horse battery staple", salt)
	// Generated at PasswordHashIterations (600,000) from the vendored JS
	// bundle directly: node -e requiring noble-hashes.bundle.js and calling
	// pbkdf2Async with this exact salt and password, cross-checked against
	// golang.org/x/crypto/pbkdf2 with the same inputs before this package
	// existed. Both produced this hex.
	want, err := hex.DecodeString("0008e69b89ffac1aa7bb1f44289ba65afaa711dd450f0aab6c322e4cd57bb216")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("verifier mismatch:\n got  %x\n want %x", got, want)
	}
	// And the two derivation paths in this codebase must agree with each
	// other, not just with a hardcoded vector.
	direct := pbkdf2.Key([]byte("correct horse battery staple"), salt, PasswordHashIterations, passwordVerifierBytes, sha256.New)
	if !bytes.Equal(got, direct) {
		t.Fatalf("DeriveVerifier diverged from pbkdf2.Key: %x vs %x", got, direct)
	}
}

func TestDisabledManagerAcceptsNoLogin(t *testing.T) {
	m := NewManager()
	if m.Enabled() {
		t.Fatal("fresh manager reports enabled")
	}
	enabled, salt, challenge, err := m.StatusAndChallenge()
	if err != nil || enabled || salt != "" || challenge != "" {
		t.Fatalf("disabled manager returned a challenge: %v %v %v %v", enabled, salt, challenge, err)
	}
	if token, err := m.Login("anything", "anything", "127.0.0.1:1"); err != nil || token != "" {
		t.Fatalf("login against disabled manager: token=%q err=%v", token, err)
	}
}

func TestLoginRoundTripAndWrongPasswordRejected(t *testing.T) {
	m := NewManager()
	salt := bytes.Repeat([]byte{0x33}, passwordSaltBytes)
	verifier := DeriveVerifier("hunter2!!", salt)
	if err := m.Configure(passwordRecord{Salt: salt, Verifier: verifier}.encoded()); err != nil {
		t.Fatal(err)
	}
	if !m.Enabled() {
		t.Fatal("configured manager reports disabled")
	}

	enabled, gotSalt, challenge, err := m.StatusAndChallenge()
	if err != nil || !enabled {
		t.Fatalf("status: enabled=%v err=%v", enabled, err)
	}
	if gotSalt != base64.RawURLEncoding.EncodeToString(salt) {
		t.Fatalf("salt not echoed back: got %q", gotSalt)
	}

	// Wrong password: a client deriving from a different verifier must
	// fail, and the challenge must not be reusable afterward. clientIP
	// strips the port, so a distinct *host* (not just port) is used for
	// the reuse check below — a failure immediately arms this client's own
	// rate limit (see TestRepeatedFailuresAreRateLimited), which would
	// otherwise turn this into a rate-limit test by accident.
	wrongProof := proofFor(DeriveVerifier("not-it", salt), challenge)
	if _, err := m.Login(challenge, wrongProof, "127.0.0.1:1"); err != ErrUnauthorized {
		t.Fatalf("wrong password: got err=%v want ErrUnauthorized", err)
	}
	if _, err := m.Login(challenge, wrongProof, "127.0.0.2:1"); err != ErrUnauthorized {
		t.Fatalf("reused challenge after failure should still fail, got %v", err)
	}

	_, _, challenge2, err := m.StatusAndChallenge()
	if err != nil {
		t.Fatal(err)
	}
	rightProof := proofFor(verifier, challenge2)
	token, err := m.Login(challenge2, rightProof, "127.0.0.3:1")
	if err != nil || token == "" {
		t.Fatalf("correct password: token=%q err=%v", token, err)
	}
	if !m.ValidSession(token) {
		t.Fatal("session from successful login is not valid")
	}
	// One-time challenge: replaying it must fail even with the right proof.
	if _, err := m.Login(challenge2, rightProof, "127.0.0.3:1"); err != ErrUnauthorized {
		t.Fatalf("replayed challenge: got %v want ErrUnauthorized", err)
	}
}

// A single wrong attempt arms a cooldown immediately (1s, doubling per
// further failure) — the backoff starts on the first miss rather than
// after some threshold of misses, which is deliberate: a script trying
// passwords in a loop hits the wall on its very first wrong guess.
func TestRepeatedFailuresAreRateLimited(t *testing.T) {
	m := NewManager()
	salt := bytes.Repeat([]byte{0x44}, passwordSaltBytes)
	verifier := DeriveVerifier("swordfish", salt)
	if err := m.Configure(passwordRecord{Salt: salt, Verifier: verifier}.encoded()); err != nil {
		t.Fatal(err)
	}

	_, _, challenge, err := m.StatusAndChallenge()
	if err != nil {
		t.Fatal(err)
	}
	wrongProof := proofFor(DeriveVerifier("nope", salt), challenge)
	if _, err := m.Login(challenge, wrongProof, "10.0.0.5:9"); err != ErrUnauthorized {
		t.Fatalf("first wrong attempt: got %v want ErrUnauthorized", err)
	}

	// Immediately retrying — even with the *correct* password — is rate
	// limited, because the cooldown gates the client, not the guess.
	_, _, challenge2, err := m.StatusAndChallenge()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Login(challenge2, proofFor(verifier, challenge2), "10.0.0.5:9"); err != ErrRateLimited {
		t.Fatalf("should be rate limited after one failure, got %v", err)
	}

	// A different client address is unaffected by another client's
	// failures.
	_, _, challenge3, err := m.StatusAndChallenge()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Login(challenge3, proofFor(verifier, challenge3), "10.0.0.6:9"); err != nil {
		t.Fatalf("different client should not be rate limited: %v", err)
	}

	// Once the cooldown has actually elapsed, the same client can log in
	// again. The clock is moved by editing the recorded RetryAt directly
	// rather than sleeping, so this test does not need to wait out a real
	// backoff window.
	m.mu.Lock()
	attempt := m.attempts["10.0.0.5"]
	attempt.RetryAt = time.Now().Add(-time.Millisecond)
	m.attempts["10.0.0.5"] = attempt
	m.mu.Unlock()
	_, _, challenge4, err := m.StatusAndChallenge()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Login(challenge4, proofFor(verifier, challenge4), "10.0.0.5:9"); err != nil {
		t.Fatalf("login after cooldown elapsed: got %v", err)
	}
}

func TestUpdatePasswordRequiresAuthorizationOnceEnabled(t *testing.T) {
	m := NewManager()
	// Enabling from empty requires no prior authorization.
	salt := bytes.Repeat([]byte{0x55}, passwordSaltBytes)
	hash := passwordRecord{Salt: salt, Verifier: DeriveVerifier("first-password", salt)}.encoded()
	token, revoke, err := m.UpdatePassword(hash, false)
	if err != nil || token == "" || !revoke {
		t.Fatalf("initial enable: token=%q revoke=%v err=%v", token, revoke, err)
	}

	// Changing an already-set password without authorization must fail.
	newHash := passwordRecord{Salt: salt, Verifier: DeriveVerifier("second-password", salt)}.encoded()
	if _, _, err := m.UpdatePassword(newHash, false); err != ErrUnauthorized {
		t.Fatalf("unauthorized change: got %v want ErrUnauthorized", err)
	}
	// The old session must still work: an unauthorized attempt did not
	// silently apply.
	if !m.ValidSession(token) {
		t.Fatal("old session invalidated by a rejected update")
	}

	// With authorization, the change succeeds and invalidates old sessions.
	newToken, revoke, err := m.UpdatePassword(newHash, true)
	if err != nil || newToken == "" || !revoke {
		t.Fatalf("authorized change: token=%q revoke=%v err=%v", newToken, revoke, err)
	}
	if m.ValidSession(token) {
		t.Fatal("old session survived a password change")
	}
	if !m.ValidSession(newToken) {
		t.Fatal("new session from the update is not valid")
	}

	// Clearing the password (empty hash) disables and revokes.
	clearedToken, revoke, err := m.UpdatePassword("", true)
	if err != nil || clearedToken != "" || !revoke {
		t.Fatalf("clear: token=%q revoke=%v err=%v", clearedToken, revoke, err)
	}
	if m.Enabled() {
		t.Fatal("manager still enabled after clearing password")
	}
	if m.ValidSession(newToken) {
		t.Fatal("session survived clearing the password")
	}
}

func TestExpiredSessionAndChallengeAreRejected(t *testing.T) {
	m := NewManager()
	salt := bytes.Repeat([]byte{0x66}, passwordSaltBytes)
	verifier := DeriveVerifier("time-bomb", salt)
	if err := m.Configure(passwordRecord{Salt: salt, Verifier: verifier}.encoded()); err != nil {
		t.Fatal(err)
	}
	// Force a session to have already expired, then confirm cleanup drops
	// it rather than treating a missing expiry as forever-valid.
	m.mu.Lock()
	m.sessions["stale"] = time.Now().Add(-time.Second)
	m.mu.Unlock()
	if m.ValidSession("stale") {
		t.Fatal("expired session reported valid")
	}

	m.mu.Lock()
	m.challenges["stale-challenge"] = time.Now().Add(-time.Second)
	m.mu.Unlock()
	proof := proofFor(verifier, "stale-challenge")
	if _, err := m.Login("stale-challenge", proof, "127.0.0.1:1"); err != ErrUnauthorized {
		t.Fatalf("expired challenge: got %v want ErrUnauthorized", err)
	}
}
