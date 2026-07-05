package wt

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/sonyabytes/git-wt/internal/clone"
	"github.com/sonyabytes/git-wt/internal/config"
)

// provision walks the classification rules and materializes cloned and
// shared state from the main checkout into the new worktree. Paths already
// present in the worktree (tracked files) are left untouched.
func (r *Repo) provision(cfg *config.Config, wtPath string, logf func(string, ...any)) error {
	seen := map[string]bool{}
	for _, rule := range cfg.Rules {
		matches, err := filepath.Glob(filepath.Join(r.MainRoot, rule.Pattern))
		if err != nil {
			return fmt.Errorf("bad pattern %q: %w", rule.Pattern, err)
		}
		sort.Strings(matches)
		for _, src := range matches {
			rel, err := filepath.Rel(r.MainRoot, src)
			if err != nil || seen[rel] {
				continue
			}
			seen[rel] = true

			// First matching rule wins, so re-classify through the full
			// rule list (repo overrides precede defaults).
			action, ok := cfg.Classify(rel)
			if !ok || action == config.Skip {
				continue
			}
			dst := filepath.Join(wtPath, rel)
			if _, err := os.Lstat(dst); err == nil {
				continue // tracked file already checked out
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			switch action {
			case config.Clone:
				logf("cloning  %s", rel)
				if err := clone.Tree(src, dst); err != nil {
					return fmt.Errorf("clone %s: %w", rel, err)
				}
			case config.Share:
				logf("sharing  %s", rel)
				if err := os.Symlink(src, dst); err != nil {
					return fmt.Errorf("share %s: %w", rel, err)
				}
			}
		}
	}
	return nil
}

// setupCommand resolves the configured setup command, or detects a package
// manager install from lockfiles in the worktree. Empty means nothing to run.
func setupCommand(cfg *config.Config, wtPath string) string {
	if cfg.Setup != "" {
		return cfg.Setup
	}
	for _, probe := range []struct{ file, cmd string }{
		{"bun.lock", "bun install"},
		{"bun.lockb", "bun install"},
		{"pnpm-lock.yaml", "pnpm install"},
		{"yarn.lock", "yarn install"},
		{"package-lock.json", "npm install"},
	} {
		if _, err := os.Stat(filepath.Join(wtPath, probe.file)); err == nil {
			return probe.cmd
		}
	}
	return ""
}

// runSetup executes the setup command in the worktree, streaming output to
// stderr so stdout stays machine-readable.
func runSetup(cmdline, wtPath string) error {
	cmd := exec.Command("sh", "-c", cmdline)
	cmd.Dir = wtPath
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("setup command %q failed: %w", cmdline, err)
	}
	return nil
}
