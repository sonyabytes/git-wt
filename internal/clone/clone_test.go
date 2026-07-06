package clone

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

var errFake = errors.New("cow unavailable")

func TestTreeClonesContentWithIndependentWrites(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := []byte("original")
	if err := os.WriteFile(filepath.Join(src, "nested", "a.txt"), orig, 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dst")
	if err := Tree(src, dst); err != nil {
		t.Fatal(err)
	}

	cloned := filepath.Join(dst, "nested", "a.txt")
	got, err := os.ReadFile(cloned)
	if err != nil || string(got) != "original" {
		t.Fatalf("cloned content = %q, %v", got, err)
	}

	// Writing to the clone must not affect the source (the CoW guarantee,
	// also upheld by the plain-copy fallback).
	if err := os.WriteFile(cloned, []byte("mutated"), 0o644); err != nil {
		t.Fatal(err)
	}
	back, _ := os.ReadFile(filepath.Join(src, "nested", "a.txt"))
	if string(back) != "original" {
		t.Fatalf("write to clone leaked into source: %q", back)
	}

	// Clone and source must be distinct files, not hardlinks.
	assertDistinctFiles(t, filepath.Join(src, "nested", "a.txt"), cloned)
}

// stubCoW replaces the platform CoW clone for one test so both the CoW-hit
// and plain-copy-fallback paths of Tree are exercised on every OS.
func stubCoW(t *testing.T, fn func(src, dst string) error) {
	t.Helper()
	orig := doCoW
	doCoW = fn
	t.Cleanup(func() { doCoW = orig })
}

func TestTreeReturnsOnCoWSuccess(t *testing.T) {
	stubCoW(t, func(src, dst string) error { return nil })
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Tree(src, filepath.Join(dir, "dst")); err != nil {
		t.Fatalf("Tree with successful CoW = %v", err)
	}
}

func TestTreeFallsBackToPlainCopy(t *testing.T) {
	stubCoW(t, func(src, dst string) error { return errFake })
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "a.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst")
	if err := Tree(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "nested", "a.txt"))
	if err != nil || string(got) != "data" {
		t.Fatalf("copied content = %q, %v", got, err)
	}
}

func TestTreeFailsWhenDestinationParentIsFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.MkdirAll(src, 0o755)
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Tree(src, filepath.Join(file, "sub", "dst")); err == nil {
		t.Fatal("expected error when destination parent path crosses a file")
	}
}

func TestCopyFileErrors(t *testing.T) {
	dir := t.TempDir()
	if err := copyFile(filepath.Join(dir, "missing"), filepath.Join(dir, "out"), 0o644); err == nil {
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
	if err := copyFile(src, exists, 0o644); err == nil {
		t.Error("expected error for existing destination (O_EXCL)")
	}
}

func TestTreeRefusesExistingDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	os.MkdirAll(src, 0o755)
	os.MkdirAll(dst, 0o755)
	if err := Tree(src, dst); err == nil {
		t.Fatal("expected error for existing destination")
	}
}
