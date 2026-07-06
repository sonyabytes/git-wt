package wt

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// abs turns a slash-style fixture path into a real absolute path for the
// platform: on Windows a drive letter is required for filepath.IsAbs.
func abs(p string) string {
	if runtime.GOOS == "windows" {
		return `C:` + filepath.FromSlash(p)
	}
	return filepath.FromSlash(p)
}

func TestDiscoverFromRepoAndWorktree(t *testing.T) {
	r := initRepo(t)
	sub := filepath.Join(r.MainRoot, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	fromSub := discover(t, sub)
	if !samePath(fromSub.MainRoot, r.MainRoot) {
		t.Errorf("Discover from subdir found %q, want %q", fromSub.MainRoot, r.MainRoot)
	}

	wtPath, err := r.New("feat", discardLog)
	if err != nil {
		t.Fatal(err)
	}
	fromWt := discover(t, wtPath)
	if !samePath(fromWt.MainRoot, r.MainRoot) {
		t.Errorf("Discover from worktree found %q, want %q", fromWt.MainRoot, r.MainRoot)
	}
}

func TestDiscoverOutsideRepo(t *testing.T) {
	if _, err := Discover(t.TempDir()); err == nil || !strings.Contains(err.Error(), "not inside a git repository") {
		t.Fatalf("Discover = %v, want not-a-repo error", err)
	}
}

func TestDiscoverBareRepo(t *testing.T) {
	dir := t.TempDir()
	mustGit(t, dir, "init", "--bare")
	if _, err := Discover(dir); err == nil || !strings.Contains(err.Error(), "bare repositories") {
		t.Fatalf("Discover = %v, want bare-repo error", err)
	}
}

func TestDiscoverInvalidConfig(t *testing.T) {
	r := initRepo(t)
	if err := os.WriteFile(filepath.Join(r.MainRoot, ".worktree.toml"), []byte("not = [valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(r.MainRoot); err == nil {
		t.Fatal("expected error for invalid .worktree.toml")
	}
}

func TestDiscoverInvalidPlacement(t *testing.T) {
	needsUnix(t) // clears $HOME to make the home preset unresolvable
	r := initRepo(t)
	if err := os.WriteFile(filepath.Join(r.MainRoot, ".worktree.toml"), []byte(`worktrees = "home"`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", "")
	if _, err := Discover(r.MainRoot); err == nil {
		t.Fatal("expected error for home placement without home dir")
	}
}

func TestGitInErrorWithoutStderr(t *testing.T) {
	// A nonexistent dir fails before git can write to stderr, so the error
	// message falls back to the exec error.
	_, err := gitIn(filepath.Join(t.TempDir(), "gone"), "status")
	if err == nil || !strings.Contains(err.Error(), "git status:") {
		t.Fatalf("gitIn = %v, want wrapped exec error", err)
	}
}

func TestEnsureExcluded(t *testing.T) {
	r := initRepo(t)
	// Paths at or outside the main checkout are no-ops.
	if err := r.ensureExcluded(r.MainRoot); err != nil {
		t.Errorf("ensureExcluded(root) = %v", err)
	}
	if err := r.ensureExcluded(filepath.Dir(r.MainRoot)); err != nil {
		t.Errorf("ensureExcluded(outside) = %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(r.MainRoot, ".git", "info", "exclude")); err == nil && strings.Contains(string(data), "//") {
		t.Errorf("no-op calls must not write entries: %q", data)
	}

	// New creates the container dir before excluding it; the directory must
	// exist for git to match the trailing-slash exclude pattern.
	inside := filepath.Join(r.MainRoot, ".worktrees", "feat")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := r.ensureExcluded(inside); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(r.MainRoot, ".git", "info", "exclude"))
	if err != nil || !strings.Contains(string(data), "/.worktrees/") {
		t.Fatalf("exclude = %q, %v; want /.worktrees/ entry", data, err)
	}

	// Already ignored: a second call must not duplicate the entry.
	if err := r.ensureExcluded(inside); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(r.MainRoot, ".git", "info", "exclude"))
	if strings.Count(string(after), "/.worktrees/") != 1 {
		t.Errorf("exclude entry duplicated: %q", after)
	}
}

func TestEnsureExcludedOpenFails(t *testing.T) {
	needsPermissionChecks(t)
	r := initRepo(t)
	exclude := filepath.Join(r.MainRoot, ".git", "info", "exclude")
	if err := os.WriteFile(exclude, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(exclude, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(exclude, 0o644) })
	if err := r.ensureExcluded(filepath.Join(r.MainRoot, ".worktrees", "feat")); err == nil {
		t.Fatal("expected error opening read-only exclude file")
	}
}

func TestSanitizeBranch(t *testing.T) {
	for in, want := range map[string]string{
		"main":            "main",
		"feature/auth":    "feature-auth",
		"fix/login/bug":   "fix-login-bug",
		"no-slashes-here": "no-slashes-here",
	} {
		if got := SanitizeBranch(in); got != want {
			t.Errorf("SanitizeBranch(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolvePlacement(t *testing.T) {
	var (
		mainRoot = abs("/Users/dev/src/myapp")
		repo     = "myapp"
		home     = abs("/Users/dev")
	)
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"empty is sibling", "", abs("/Users/dev/src/myapp.worktrees/{branch}")},
		{"sibling", "sibling", abs("/Users/dev/src/myapp.worktrees/{branch}")},
		{"inside", "inside", abs("/Users/dev/src/myapp/.worktrees/{branch}")},
		{"home", "home", abs("/Users/dev/.worktrees/myapp/{branch}")},
		{"custom absolute", abs("/mnt/wt/{repo}/{branch}"), abs("/mnt/wt/myapp/{branch}")},
		{"custom without branch appends it", abs("/mnt/wt/{repo}"), abs("/mnt/wt/myapp/{branch}")},
		{"tilde expansion", "~/wt/{repo}/{branch}", abs("/Users/dev/wt/myapp/{branch}")},
		{"relative resolves against main root", "worktrees/{branch}", abs("/Users/dev/src/myapp/worktrees/{branch}")},
	} {
		got, err := resolvePlacement(tc.raw, repo, mainRoot, home)
		if err != nil {
			t.Errorf("%s: resolvePlacement(%q) error: %v", tc.name, tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: resolvePlacement(%q) = %q, want %q", tc.name, tc.raw, got, tc.want)
		}
	}
}

func TestResolvePlacementErrors(t *testing.T) {
	var (
		mainRoot = abs("/Users/dev/src/myapp")
		repo     = "myapp"
	)
	for _, tc := range []struct {
		name    string
		raw     string
		home    string
		wantErr string
	}{
		{"home preset without home dir", "home", "", "home directory"},
		{"tilde without home dir", "~/wt/{branch}", "", "no home directory"},
		{"resolves to main checkout", ".", abs("/Users/dev"), "main checkout itself"},
		{"resolves inside .git", ".git/wt/{branch}", abs("/Users/dev"), "inside .git"},
	} {
		_, err := resolvePlacement(tc.raw, repo, mainRoot, tc.home)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: resolvePlacement(%q) error = %v, want containing %q", tc.name, tc.raw, err, tc.wantErr)
		}
	}
}
