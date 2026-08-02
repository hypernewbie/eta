// Package access is Eta's optional browser-session login. It is off by
// default: an Eta instance with no password configured behaves exactly as
// before, matching the LAN/Tailscale trust boundary the rest of the
// product assumes. Setting a password gates the desktop UI and this
// machine's API behind a login, for the case where the LAN itself is not
// fully trusted (a shared network, a guest VLAN, a NAS closet someone
// else can plug into).
//
// The password never reaches the server. The browser derives a PBKDF2
// verifier client-side and the server only ever sees that verifier, so a
// disk read or a request log can't leak the password itself. See
// web/app.ts's auth section for the client half, and Manager.Login for
// the challenge/response the two sides share.
package access

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the on-disk shape. PasswordHash is the encoded verifier
// record (see ParsePasswordHash), never the raw password.
type Config struct {
	PasswordHash string `json:"password_hash,omitempty"`
}

// DefaultPath returns the per-user config path, alongside identity.json
// and state.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, "eta", "access.json"), nil
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes atomically: a partial write from a killed process must
// never leave access.json holding half a JSON document, which would lock
// the owner out on the next start with no way back in except deleting
// the file by hand.
func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
