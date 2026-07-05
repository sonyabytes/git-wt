package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsWhenNoFile(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]Action{
		"node_modules": Clone,
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
	if got, ok := cfg.Classify(".env"); !ok || got != Share {
		t.Errorf("defaults should still apply: .env = %v, %v", got, ok)
	}
}
