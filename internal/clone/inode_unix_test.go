//go:build !windows

package clone

import (
	"os"
	"syscall"
	"testing"
)

// assertDistinctFiles fails if a and b share an inode (hardlink rather than
// a CoW clone or copy).
func assertDistinctFiles(t *testing.T, a, b string) {
	t.Helper()
	ai, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	bi, err := os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	if ai.Sys().(*syscall.Stat_t).Ino == bi.Sys().(*syscall.Stat_t).Ino {
		t.Errorf("%s and %s share an inode (hardlink, not CoW/copy)", a, b)
	}
}
