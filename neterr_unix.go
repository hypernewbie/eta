//go:build !windows

package main

import (
	"errors"
	"syscall"
)

// isRefused and isUnreachable exist because the errno constants differ by
// platform: on Windows, syscall.ECONNREFUSED is a placeholder value that a
// real refused connection never carries (a genuine one is WSAECONNREFUSED),
// so matching it there silently classifies nothing.
func isRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }

func isUnreachable(err error) bool {
	return errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH)
}
