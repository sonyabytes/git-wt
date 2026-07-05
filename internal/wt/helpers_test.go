package wt

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// initRepo creates a real git repository with one commit under a fresh temp
// dir. The repo sits at <tmp>/repo so sibling worktree placements land inside
// the temp dir and are cleaned up with it.
func initRepo(t *testing.T) *Repo {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, root, "init", "-b", "main")
	mustGit(t, root, "config", "user.email", "test@example.com")
	mustGit(t, root, "config", "user.name", "Test")
	mustGit(t, root, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, root, "add", ".")
	mustGit(t, root, "commit", "-m", "init")
	return discover(t, root)
}

// discover wraps Discover with a fatal on error.
func discover(t *testing.T, dir string) *Repo {
	t.Helper()
	r, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover(%s): %v", dir, err)
	}
	return r
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// writeConfig replaces the repo's .worktree.toml and re-discovers, so the
// placement template picks up the new value.
func writeConfig(t *testing.T, r *Repo, toml string) *Repo {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.MainRoot, ".worktree.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	return discover(t, r.MainRoot)
}

// needsUnix skips tests relying on Unix semantics: permission bits, symlink
// creation without privilege, `sh`, or $HOME.
func needsUnix(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix filesystem semantics")
	}
}

// needsPermissionChecks skips permission-bit tests when running as root.
func needsPermissionChecks(t *testing.T) {
	t.Helper()
	needsUnix(t)
	if os.Geteuid() == 0 {
		t.Skip("permission bits are ignored when running as root")
	}
}

// discardLog is a logf sink; grabLog collects formatted lines for assertions.
func discardLog(string, ...any) {}

func grabLog(lines *[]string) func(string, ...any) {
	return func(format string, a ...any) {
		*lines = append(*lines, fmt.Sprintf(format, a...))
	}
}
