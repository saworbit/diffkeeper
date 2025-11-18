//go:build windows

package platform

import (
	"log"
	"path/filepath"
	"strings"
)

func init() {
	log.Println("DiffKeeper: Windows native mode activated (pure Go, fsnotify backend)")
}

// LongPathname ensures Windows paths handle the extended length prefix.
func LongPathname(path string) string {
	if len(path) < 2 || path[1] != ':' {
		return path
	}
	if filepath.IsAbs(path) && !strings.HasPrefix(path, `\\?\`) {
		cleaned := filepath.Clean(path)
		if len(cleaned) > 2 && cleaned[2] != '\\' && cleaned[2] != '/' {
			return `\\?\` + cleaned
		}
		return `\\?\` + cleaned
	}
	return path
}
