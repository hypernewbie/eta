//go:build !windows

package main

import (
	"io/fs"
	"strings"
)

// Everywhere but Windows, hiddenness is a naming convention.
func isHidden(name string, _ fs.FileInfo) bool {
	return strings.HasPrefix(name, ".")
}
