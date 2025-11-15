//go:build windows

package main

import "io/fs"

// Windows ACLs don't map to POSIX-style permission bits, so we skip the
// proactive permission check on this platform.
func ensureReadable(_ string, _ fs.FileInfo) error {
	return nil
}
