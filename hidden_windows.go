//go:build windows

package main

import (
	"io/fs"
	"strings"
	"syscall"
)

// Windows carries hiddenness as a file attribute rather than in the
// name, so a file with no leading dot can still be hidden and must be
// treated the same way. Dotfiles are also honoured here: cross-platform
// tools and anything copied from a Unix box use them on Windows too.
func isHidden(name string, info fs.FileInfo) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return false
	}
	return data.FileAttributes&(syscall.FILE_ATTRIBUTE_HIDDEN|syscall.FILE_ATTRIBUTE_SYSTEM) != 0
}
