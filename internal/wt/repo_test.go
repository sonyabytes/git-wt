package wt

import "testing"

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
