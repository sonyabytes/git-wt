package wt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sonyabytes/git-wt/internal/config"
)

// fakeRepo builds a Repo over a plain directory tree — provision needs no
// git, only MainRoot and rules.
func fakeRepo(t *testing.T) (*Repo, string) {
	t.Helper()
	tmp := t.TempDir()
	main := filepath.Join(tmp, "main")
	wt := filepath.Join(tmp, "wt")
	for _, d := range []string{main, wt} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &Repo{MainRoot: main, Name: "main"}, wt
}

func rules(rs ...config.Rule) *config.Config { return &config.Config{Rules: rs} }

func TestProvisionSkipsDuplicateMatches(t *testing.T) {
	r, wt := fakeRepo(t)
	if err := os.MkdirAll(filepath.Join(r.MainRoot, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Both patterns match node_modules; the second hit is skipped via seen.
	cfg := rules(
		config.Rule{Pattern: "node_modules", Action: config.Clone},
		config.Rule{Pattern: "node_*", Action: config.Clone},
	)
	if err := r.provision(cfg, wt, discardLog); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wt, "node_modules")); err != nil {
		t.Errorf("node_modules not provisioned: %v", err)
	}
}

func TestProvisionHonorsSkipRules(t *testing.T) {
	r, wt := fakeRepo(t)
	if err := os.MkdirAll(filepath.Join(r.MainRoot, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := rules(config.Rule{Pattern: "dist", Action: config.Skip})
	if err := r.provision(cfg, wt, discardLog); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wt, "dist")); !os.IsNotExist(err) {
		t.Errorf("dist should be skipped, stat err = %v", err)
	}
}

func TestProvisionLeavesExistingPathsAlone(t *testing.T) {
	r, wt := fakeRepo(t)
	if err := os.MkdirAll(filepath.Join(r.MainRoot, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "node_modules"), []byte("tracked"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := rules(config.Rule{Pattern: "node_modules", Action: config.Clone})
	if err := r.provision(cfg, wt, discardLog); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(wt, "node_modules"))
	if string(got) != "tracked" {
		t.Errorf("existing path overwritten: %q", got)
	}
}

func TestProvisionMkdirFails(t *testing.T) {
	needsPermissionChecks(t)
	r, wt := fakeRepo(t)
	if err := os.MkdirAll(filepath.Join(r.MainRoot, "sub", "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(wt, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(wt, 0o755) })
	cfg := rules(config.Rule{Pattern: "sub/data", Action: config.Clone})
	if err := r.provision(cfg, wt, discardLog); err == nil {
		t.Fatal("expected MkdirAll error in read-only worktree")
	}
}

func TestProvisionCloneFails(t *testing.T) {
	needsPermissionChecks(t)
	r, wt := fakeRepo(t)
	if err := os.MkdirAll(filepath.Join(r.MainRoot, "x", "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	// wt/x exists but is read-only: MkdirAll succeeds (already there), the
	// clone itself cannot create its destination.
	if err := os.MkdirAll(filepath.Join(wt, "x"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(wt, "x"), 0o755) })
	cfg := rules(config.Rule{Pattern: "x/node_modules", Action: config.Clone})
	if err := r.provision(cfg, wt, discardLog); err == nil || !strings.Contains(err.Error(), "clone") {
		t.Fatalf("provision = %v, want clone error", err)
	}
}

func TestProvisionShareFails(t *testing.T) {
	needsPermissionChecks(t)
	r, wt := fakeRepo(t)
	if err := os.MkdirAll(filepath.Join(r.MainRoot, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.MainRoot, "x", ".env"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wt, "x"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(wt, "x"), 0o755) })
	cfg := rules(config.Rule{Pattern: "x/.env", Action: config.Share})
	if err := r.provision(cfg, wt, discardLog); err == nil || !strings.Contains(err.Error(), "share") {
		t.Fatalf("provision = %v, want share error", err)
	}
}

func TestSetupCommand(t *testing.T) {
	dir := t.TempDir()
	if got := setupCommand(&config.Config{Setup: "make deps"}, dir); got != "make deps" {
		t.Errorf("explicit setup = %q", got)
	}
	if got := setupCommand(&config.Config{}, dir); got != "" {
		t.Errorf("no lockfile should mean no setup, got %q", got)
	}
	for file, want := range map[string]string{
		"bun.lock":          "bun install",
		"bun.lockb":         "bun install",
		"pnpm-lock.yaml":    "pnpm install",
		"yarn.lock":         "yarn install",
		"package-lock.json": "npm install",
	} {
		probe := t.TempDir()
		if err := os.WriteFile(filepath.Join(probe, file), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := setupCommand(&config.Config{}, probe); got != want {
			t.Errorf("%s -> %q, want %q", file, got, want)
		}
	}
}

func TestRunSetup(t *testing.T) {
	needsUnix(t)
	dir := t.TempDir()
	if err := runSetup("echo hi > out.txt", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "out.txt")); err != nil {
		t.Errorf("setup did not run in dir: %v", err)
	}
	if err := runSetup("false", dir); err == nil || !strings.Contains(err.Error(), "failed") {
		t.Errorf("runSetup(false) = %v, want failure", err)
	}
}
