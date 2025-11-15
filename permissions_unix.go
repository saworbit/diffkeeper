//go:build !windows

package main

import (
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// ensureReadable returns an error if the current user would normally be denied
// read access to the file even if elevated privileges would allow reading it.
func ensureReadable(path string, info fs.FileInfo) error {
	if info == nil {
		var err error
		info, err = os.Stat(path)
		if err != nil {
			return err
		}
	}

	perms := info.Mode().Perm()

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}

	fileUID := int(stat.Uid)
	fileGID := int(stat.Gid)
	euid := os.Geteuid()
	egid := os.Getegid()

	if fileUID == euid {
		if perms&0400 == 0 {
			return fmt.Errorf("permission denied reading %s: owner has no read bit", path)
		}
		return nil
	}

	if fileGID == egid {
		if perms&0040 == 0 {
			return fmt.Errorf("permission denied reading %s: group has no read bit", path)
		}
		return nil
	}

	if groups, err := syscall.Getgroups(); err == nil {
		for _, g := range groups {
			if int(g) == fileGID {
				if perms&0040 == 0 {
					return fmt.Errorf("permission denied reading %s: group has no read bit", path)
				}
				return nil
			}
		}
	}

	if perms&0004 == 0 {
		return fmt.Errorf("permission denied reading %s: others have no read bit", path)
	}

	return nil
}
