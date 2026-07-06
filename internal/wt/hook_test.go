package wt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hookRepo(t *testing.T) *Repo {
	t.Helper()
	return &Repo{MainRoot: t.TempDir(), Name: "repo"}
}

func settingsPath(r *Repo) string {
	return filepath.Join(r.MainRoot, ".claude", "settings.json")
}

func TestInstallHookFreshAndIdempotent(t *testing.T) {
	r := hookRepo(t)
	var lines []string
	if err := r.InstallHook(grabLog(&lines)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(settingsPath(r))
	if err != nil {
		t.Fatal(err)
	}
	if !containsHook(data) {
		t.Fatalf("settings.json missing hook:\n%s", data)
	}

	lines = nil
	if err := r.InstallHook(grabLog(&lines)); err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(lines, "\n"); !strings.Contains(joined, "already installed") {
		t.Errorf("second install should be a no-op:\n%s", joined)
	}
	after, _ := os.ReadFile(settingsPath(r))
	if string(after) != string(data) {
		t.Error("idempotent install rewrote settings.json")
	}
}

func TestInstallHookPreservesExistingSettings(t *testing.T) {
	r := hookRepo(t)
	existing := `{
  "model": "opus",
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "echo hi"}]}
    ]
  }
}`
	if err := os.MkdirAll(filepath.Dir(settingsPath(r)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath(r), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.InstallHook(discardLog); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(settingsPath(r))
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["model"] != "opus" {
		t.Error("unrelated settings key lost")
	}
	if !strings.Contains(string(data), "echo hi") {
		t.Error("pre-existing hook lost")
	}
	if !containsHook(data) {
		t.Error("guard hook not added")
	}
}

func TestInstallHookRejectsInvalidJSON(t *testing.T) {
	r := hookRepo(t)
	if err := os.MkdirAll(filepath.Dir(settingsPath(r)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath(r), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.InstallHook(discardLog); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("InstallHook = %v, want invalid-JSON error", err)
	}
}

func TestInstallHookReadFails(t *testing.T) {
	r := hookRepo(t)
	// settings.json as a directory: ReadFile fails with a non-NotExist error.
	if err := os.MkdirAll(settingsPath(r), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := r.InstallHook(discardLog); err == nil {
		t.Fatal("expected read error when settings.json is a directory")
	}
}

func TestInstallHookMkdirFails(t *testing.T) {
	needsPermissionChecks(t)
	r := hookRepo(t)
	if err := os.Chmod(r.MainRoot, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(r.MainRoot, 0o755) })
	if err := r.InstallHook(discardLog); err == nil {
		t.Fatal("expected error creating .claude in read-only root")
	}
}

func TestInstallHookWriteFails(t *testing.T) {
	needsPermissionChecks(t)
	r := hookRepo(t)
	if err := os.MkdirAll(filepath.Dir(settingsPath(r)), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Dir(settingsPath(r)), 0o755) })
	if err := r.InstallHook(discardLog); err == nil {
		t.Fatal("expected error writing settings.json into read-only .claude")
	}
}

func TestContainsHookInvalidJSON(t *testing.T) {
	if containsHook([]byte("{nope")) {
		t.Error("invalid JSON should not contain the hook")
	}
}
