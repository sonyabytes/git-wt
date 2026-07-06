package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsWhenNoFile(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]Action{
		"node_modules": Clone,
		".venv":        Clone,
		"target":       Clone,
		"vendor":       Clone,
		".env":         Share,
		".env.local":   Share,
		"dist":         Skip,
	} {
		got, ok := cfg.Classify(rel)
		if !ok || got != want {
			t.Errorf("Classify(%q) = %v, %v; want %v, true", rel, got, ok, want)
		}
	}
	if _, ok := cfg.Classify("random-dir"); ok {
		t.Error("unmatched path should not classify")
	}
}

func TestRepoConfigOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	toml := `
setup = "make deps"
clone = ["dist"]
share = ["secrets"]
skip  = ["node_modules"]
`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Setup != "make deps" {
		t.Errorf("Setup = %q, want %q", cfg.Setup, "make deps")
	}
	if got, _ := cfg.Classify("dist"); got != Clone {
		t.Errorf("repo rule should override default: dist = %v, want clone", got)
	}
	if got, _ := cfg.Classify("node_modules"); got != Skip {
		t.Errorf("repo rule should override default: node_modules = %v, want skip", got)
	}
	if got, _ := cfg.Classify("secrets"); got != Share {
		t.Errorf("share rule should apply: secrets = %v, want share", got)
	}
	if got, ok := cfg.Classify(".env"); !ok || got != Share {
		t.Errorf("defaults should still apply: .env = %v, %v", got, ok)
	}
}

func TestPathsTable(t *testing.T) {
	dir := t.TempDir()
	toml := `
[paths]
"vendor" = "clone"
".cache" = "skip"
`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := cfg.Classify("vendor"); got != Clone {
		t.Errorf("vendor = %v, want clone", got)
	}
	if got, _ := cfg.Classify(".cache"); got != Skip {
		t.Errorf(".cache = %v, want skip", got)
	}
}

func TestPathsTableRejectsUnknownAction(t *testing.T) {
	dir := t.TempDir()
	toml := `
[paths]
"vendor" = "clon"
`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	for _, want := range []string{"clon", "vendor"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestPathsTableOrderIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	// Both patterns match ".envrc"; first match must win the same way on
	// every run, and rules sort by pattern (".env*" < ".envrc").
	toml := `
[paths]
".envrc" = "skip"
".env*"  = "share"
`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	for range 20 {
		cfg, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := cfg.Classify(".envrc"); got != Share {
			t.Fatalf(".envrc = %v, want share (sorted-pattern order)", got)
		}
	}
}

func TestLoadInvalidTOML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("not = [valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected decode error for invalid TOML")
	}
}

func TestLoadUnreadableFile(t *testing.T) {
	// A directory named like the config file makes ReadFile fail with an
	// error that is not ErrNotExist.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, FileName), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected read error when config path is a directory")
	}
}

func TestWorktreesKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(`worktrees = "inside"`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Worktrees != "inside" {
		t.Errorf("Worktrees = %q, want %q", cfg.Worktrees, "inside")
	}

	cfg, err = Load(t.TempDir()) // no file
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Worktrees != "" {
		t.Errorf("Worktrees with no file = %q, want empty", cfg.Worktrees)
	}
}
