package clone

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// stubIoctl replaces the FICLONE ioctl for one test so both reflink outcomes
// are exercised regardless of the host filesystem (CI runners use ext4,
// where the real ioctl always fails).
func stubIoctl(t *testing.T, fn func(destFd, srcFd int) error) {
	t.Helper()
	orig := ioctlFileClone
	ioctlFileClone = fn
	t.Cleanup(func() { ioctlFileClone = orig })
}

func TestTreeReflinksWhenFICLONESucceeds(t *testing.T) {
	stubIoctl(t, func(destFd, srcFd int) error { return nil })
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "a.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst")
	cow, err := Tree(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if !cow {
		t.Error("Tree should report cow=true when every reflink succeeds")
	}
	// The stub clones no extents, so assert structure, not content.
	if _, err := os.Lstat(filepath.Join(dst, "nested", "a.txt")); err != nil {
		t.Errorf("reflinked file missing: %v", err)
	}
	if link, err := os.Readlink(filepath.Join(dst, "link")); err != nil || link != "target" {
		t.Errorf("symlink = %q, %v; want %q", link, err, "target")
	}
}

func TestTreeFallsBackWhenFICLONEFails(t *testing.T) {
	stubIoctl(t, func(destFd, srcFd int) error { return errors.New("EOPNOTSUPP") })
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst")
	cow, err := Tree(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if cow {
		t.Error("Tree should report cow=false when reflink is unsupported")
	}
	if got, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(got) != "data" {
		t.Errorf("fallback copy content = %q, %v", got, err)
	}
}

func TestReflinkFileOpenErrors(t *testing.T) {
	dir := t.TempDir()
	if err := reflinkFile(filepath.Join(dir, "missing"), filepath.Join(dir, "out"), 0o644); err == nil {
		t.Error("expected error for missing source")
	}
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	exists := filepath.Join(dir, "exists.txt")
	if err := os.WriteFile(exists, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := reflinkFile(src, exists, 0o644); err == nil {
		t.Error("expected error for existing destination (O_EXCL)")
	}
}
