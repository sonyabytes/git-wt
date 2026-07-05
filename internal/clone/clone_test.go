package clone

import (
	"os"
	"path/filepath"
	"testing"
)

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
