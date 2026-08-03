//go:build windows

package main

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// On Windows a refused connection surfaces as WSAECONNREFUSED (10061).
// syscall.ECONNREFUSED there is an APPLICATION_ERROR-offset placeholder
// that no real socket error carries, and syscall.Errno.Is maps only the
// fs sentinels -- so the portable-looking check matches nothing and every
// refusal degrades to the generic "couldn't reach" message.
func isRefused(err error) bool {
	return errors.Is(err, windows.WSAECONNREFUSED) || errors.Is(err, syscall.ECONNREFUSED)
}

func isUnreachable(err error) bool {
	return errors.Is(err, windows.WSAEHOSTUNREACH) ||
		errors.Is(err, windows.WSAENETUNREACH) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH)
}
