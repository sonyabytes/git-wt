package wt

import (
	"path/filepath"
	"strings"
	"testing"
)

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
	const (
		mainRoot = "/Users/dev/src/myapp"
		repo     = "myapp"
		home     = "/Users/dev"
	)
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"empty is sibling", "", "/Users/dev/src/myapp.worktrees/{branch}"},
		{"sibling", "sibling", "/Users/dev/src/myapp.worktrees/{branch}"},
		{"inside", "inside", "/Users/dev/src/myapp/.worktrees/{branch}"},
		{"home", "home", "/Users/dev/.worktrees/myapp/{branch}"},
		{"custom absolute", "/mnt/wt/{repo}/{branch}", "/mnt/wt/myapp/{branch}"},
		{"custom without branch appends it", "/mnt/wt/{repo}", "/mnt/wt/myapp/{branch}"},
		{"tilde expansion", "~/wt/{repo}/{branch}", "/Users/dev/wt/myapp/{branch}"},
		{"relative resolves against main root", "worktrees/{branch}", "/Users/dev/src/myapp/worktrees/{branch}"},
	} {
		got, err := resolvePlacement(tc.raw, repo, mainRoot, home)
		if err != nil {
			t.Errorf("%s: resolvePlacement(%q) error: %v", tc.name, tc.raw, err)
			continue
		}
		if got != filepath.FromSlash(tc.want) {
			t.Errorf("%s: resolvePlacement(%q) = %q, want %q", tc.name, tc.raw, got, tc.want)
		}
	}
}

func TestResolvePlacementErrors(t *testing.T) {
	const (
		mainRoot = "/Users/dev/src/myapp"
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
		{"resolves to main checkout", ".", "/Users/dev", "main checkout itself"},
		{"resolves inside .git", ".git/wt/{branch}", "/Users/dev", "inside .git"},
	} {
		_, err := resolvePlacement(tc.raw, repo, mainRoot, tc.home)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: resolvePlacement(%q) error = %v, want containing %q", tc.name, tc.raw, err, tc.wantErr)
		}
	}
}
