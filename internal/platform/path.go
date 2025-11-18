//go:build !windows

package platform

// LongPathname is a no-op on non-Windows platforms.
func LongPathname(path string) string {
	return path
}
