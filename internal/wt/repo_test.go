package wt

import (
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
