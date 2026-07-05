//go:build windows

package clone

import "testing"

// assertDistinctFiles is a no-op on Windows: the fallback there is a plain
// copy (never a hardlink), and inodes aren't exposed via syscall.Stat_t.
// The content-independence check in the main test still applies.
func assertDistinctFiles(t *testing.T, a, b string) {
	t.Helper()
}
