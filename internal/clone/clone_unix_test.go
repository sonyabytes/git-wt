//go:build !windows

package clone

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// The plain-copy fallback behaviors below need Unix primitives (symlinks
// without privilege, FIFOs, permission bits), so they are Unix-only.

func TestCopyTreePreservesSymlinks(t *testing.T) {
	stubCoW(t, func(src, dst string) error { return errFake })
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst")
	if _, err := Tree(src, dst); err != nil {
		t.Fatal(err)
	}
	link, err := os.Readlink(filepath.Join(dst, "link"))
	if err != nil || link != "target" {
		t.Fatalf("copied symlink = %q, %v; want %q", link, err, "target")
	}
}

func TestCopyTreeSkipsIrregularFiles(t *testing.T) {
	stubCoW(t, func(src, dst string) error { return errFake })
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(src, "pipe"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst")
	if _, err := Tree(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dst, "pipe")); !os.IsNotExist(err) {
		t.Errorf("FIFO should be skipped, lstat err = %v", err)
	}
}

func TestCopyTreeReportsWalkErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits are ignored when running as root")
	}
	stubCoW(t, func(src, dst string) error { return errFake })
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	sealed := filepath.Join(src, "sealed")
	if err := os.MkdirAll(sealed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sealed, "a.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sealed, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(sealed, 0o755) })
	if _, err := Tree(src, filepath.Join(dir, "dst")); err == nil {
		t.Fatal("expected walk error for unreadable subdirectory")
	}
}

func TestTreeFallbackCleanupFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits are ignored when running as root")
	}
	// Simulate a per-file clone that dies mid-tree leaving a destination the
	// fallback's cleanup cannot list.
	stubCoW(t, func(src, dst string) error {
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, "partial"), nil, 0o644); err != nil {
			return err
		}
		if err := os.Chmod(dst, 0); err != nil {
			return err
		}
		return errFake
	})
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst")
	t.Cleanup(func() { os.Chmod(dst, 0o755) })
	if _, err := Tree(src, dst); err == nil {
		t.Fatal("expected cleanup error when the partial destination is sealed")
	}
}

func TestCopyFileReadFailure(t *testing.T) {
	// A directory opens fine but fails on read, forcing the io.Copy error.
	dir := t.TempDir()
	src := filepath.Join(dir, "srcdir")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, filepath.Join(dir, "out"), 0o644); err == nil {
		t.Fatal("expected read error when source is a directory")
	}
}
