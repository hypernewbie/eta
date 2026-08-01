// Package tmux lists and creates tmux sessions on the machine Eta runs
// on. Eta only ever asks tmux about itself; attaching happens by
// running tmux inside a normal Eta terminal.
package tmux

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrUnavailable means tmux is not installed here. It is deliberately
// distinct from "no sessions": a machine that cannot run tmux should
// say so rather than appear to have an empty list.
var ErrUnavailable = errors.New("tmux is not installed")

type Session struct {
	Name     string    `json:"name"`
	Windows  int       `json:"windows"`
	Attached bool      `json:"attached"`
	Created  time.Time `json:"created"`
}

// Session names reach a command line, so they are restricted rather
// than escaped. tmux also treats ':' and '.' as target separators.
var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func ValidName(name string) bool { return validName.MatchString(name) }

const listFormat = "#{session_name}\t#{session_windows}\t#{session_attached}\t#{session_created}"

func List(ctx context.Context) ([]Session, error) {
	output, err := run(ctx, "list-sessions", "-F", listFormat)
	if err != nil {
		if errors.Is(err, errNoServer) {
			// A running tmux with no sessions and a tmux server that has
			// never started are the same thing to a user.
			return []Session{}, nil
		}
		return nil, err
	}
	return parseSessions(output), nil
}

// Create makes a detached session so the list is useful before anyone
// attaches to it.
func Create(ctx context.Context, name string) (Session, error) {
	if !ValidName(name) {
		return Session{}, fmt.Errorf("invalid session name %q", name)
	}
	if _, err := run(ctx, "new-session", "-d", "-s", name); err != nil {
		return Session{}, err
	}
	sessions, err := List(ctx)
	if err != nil {
		return Session{}, err
	}
	for _, session := range sessions {
		if session.Name == name {
			return session, nil
		}
	}
	return Session{Name: name, Windows: 1}, nil
}

// AttachArgv is the command a terminal runs to land inside a session.
// new-session -A attaches if it exists and creates it otherwise, so a
// session that disappeared between listing and opening does not leave
// the user staring at an error.
func AttachArgv(name string) ([]string, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("invalid session name %q", name)
	}
	return []string{"tmux", "new-session", "-A", "-s", name}, nil
}

var errNoServer = errors.New("no tmux server running")

func run(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "tmux", args...)
	output, err := command.Output()
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return "", ErrUnavailable
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr := string(exitErr.Stderr)
			if strings.Contains(stderr, "no server running") ||
				strings.Contains(stderr, "no current session") ||
				strings.Contains(stderr, "error connecting") {
				return "", errNoServer
			}
			return "", fmt.Errorf("tmux: %s", strings.TrimSpace(stderr))
		}
		return "", err
	}
	return string(output), nil
}

// parseSessions is separated from the command so the format handling is
// testable on machines without tmux.
func parseSessions(output string) []Session {
	sessions := []Session{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 4 {
			continue
		}
		windows, _ := strconv.Atoi(fields[1])
		created := time.Time{}
		if seconds, err := strconv.ParseInt(fields[3], 10, 64); err == nil {
			created = time.Unix(seconds, 0).UTC()
		}
		sessions = append(sessions, Session{
			Name:     fields[0],
			Windows:  windows,
			Attached: fields[2] != "0" && fields[2] != "",
			Created:  created,
		})
	}
	return sessions
}
