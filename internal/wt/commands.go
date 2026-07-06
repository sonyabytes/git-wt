package wt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sonyabytes/git-wt/internal/config"
)

// New creates a worktree for branch under the configured placement, provisions
// cloned/shared state, and runs the setup command. Returns the worktree path.
// The branch is created if it doesn't exist — from base when given, otherwise
// from HEAD of the main checkout.
func (r *Repo) New(branch, base string, logf func(string, ...any)) (string, error) {
	cfg := r.Cfg
	path := r.WorktreePath(branch)
	if _, err := os.Stat(path); err == nil {
		// Distinguish a repeat of the same branch from a sanitization
		// collision (feature/auth and feature-auth share a directory).
		if managed, err := r.managedWorktrees(); err == nil {
			if owner, ok := managed[path]; ok && owner != branch {
				return "", fmt.Errorf("worktree path %s already belongs to branch %q (%q maps to the same directory after sanitization)", path, owner, branch)
			}
		}
		return "", fmt.Errorf("worktree already exists: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := r.ensureExcluded(path); err != nil {
		return "", err
	}

	args := []string{"worktree", "add"}
	if !r.branchExists(branch) {
		args = append(args, "-b", branch, path)
		if base != "" {
			args = append(args, base)
		}
	} else {
		if base != "" {
			return "", fmt.Errorf("branch %q already exists; --from only applies when creating a new branch", branch)
		}
		args = append(args, path, branch)
	}
	if err := r.gitPassthrough(args...); err != nil {
		return "", err
	}

	if err := r.provision(cfg, path, logf); err != nil {
		return "", err
	}
	if cmd := setupCommand(cfg, path); cmd != "" {
		logf("running setup: %s", cmd)
		if err := runSetup(cmd, path); err != nil {
			return "", err
		}
	}
	return path, nil
}

// Ls returns managed worktrees as "path<TAB>branch" lines, sorted by path.
func (r *Repo) Ls() ([]string, error) {
	managed, err := r.managedWorktrees()
	if err != nil {
		return nil, err
	}
	var lines []string
	for path, branch := range managed {
		lines = append(lines, path+"\t"+branch)
	}
	sort.Strings(lines)
	return lines, nil
}

// Rm removes the worktree for branch. Unless force is set it refuses when
// the worktree has uncommitted changes or the branch has work not on its
// upstream (or, lacking an upstream, not merged into the main checkout's
// HEAD). The branch itself is never deleted.
func (r *Repo) Rm(branch string, force bool) error {
	path := r.WorktreePath(branch)
	managed, err := r.managedWorktrees()
	if err != nil {
		return err
	}
	if _, ok := managed[path]; !ok {
		return fmt.Errorf("no managed worktree for branch %q at %s", branch, path)
	}
	if !force {
		if err := r.checkSafeToRemove(path, managed[path]); err != nil {
			return fmt.Errorf("%w (use --force to override)", err)
		}
	}
	// Our own safety checks passed (or were overridden); git's dirty check
	// would trip on the ignored state we provisioned, so always pass --force.
	if _, err := r.git("worktree", "remove", "--force", path); err != nil {
		return err
	}
	return nil
}

// Prune removes managed worktrees whose branches are gone or fully merged
// into the main checkout's HEAD, skipping any with uncommitted changes.
// Returns the removed paths.
func (r *Repo) Prune(logf func(string, ...any)) ([]string, error) {
	managed, err := r.managedWorktrees()
	if err != nil {
		return nil, err
	}
	var removed []string
	for path, branch := range managed {
		if !r.branchExists(branch) {
			// A deleted ref under a live worktree leaves HEAD dangling and
			// makes every file look uncommitted — no safe way to tell real
			// work from the broken state, so never auto-remove.
			logf("skipping %s: branch %s was deleted; remove with git wt rm %s --force", path, branch, branch)
			continue
		}
		if _, err := r.git("merge-base", "--is-ancestor", branch, "HEAD"); err != nil {
			continue // unmerged work
		}
		dirty, err := r.isDirty(path)
		if err != nil {
			// Can't tell real work from none — never auto-remove blind.
			logf("skipping %s: %v", path, err)
			continue
		}
		if dirty {
			logf("skipping %s: uncommitted changes", path)
			continue
		}
		logf("removing %s (branch %s merged)", path, branch)
		if _, err := r.git("worktree", "remove", "--force", path); err != nil {
			return removed, err
		}
		removed = append(removed, path)
	}
	// Not reachable in practice: every git call above already succeeded
	// against the same repository.
	if _, err := r.git("worktree", "prune"); err != nil { // coverage-ignore
		return removed, err
	}
	return removed, nil
}

func (r *Repo) isDirty(wtPath string) (bool, error) {
	out, err := gitIn(wtPath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (r *Repo) checkSafeToRemove(wtPath, branch string) error {
	dirty, err := r.isDirty(wtPath)
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("worktree has uncommitted changes: %s", wtPath)
	}
	// With an upstream: refuse if ahead. Without: refuse unless merged into
	// the main checkout's HEAD.
	if out, err := gitIn(wtPath, "rev-list", "--count", "@{upstream}..HEAD"); err == nil {
		if strings.TrimSpace(out) != "0" {
			return fmt.Errorf("branch %q has commits not pushed to its upstream", branch)
		}
		return nil
	}
	if _, err := r.git("merge-base", "--is-ancestor", branch, "HEAD"); err != nil {
		return fmt.Errorf("branch %q has no upstream and is not merged into %s", branch, r.Name)
	}
	return nil
}

// DefaultConfigTOML is written by Init when no .worktree.toml exists. It
// restates the built-in defaults so teams have something concrete to edit.
func DefaultConfigTOML(placement string) string {
	if placement == "" {
		placement = "sibling"
	}
	return `# git-wt provisioning classification (first match wins; these precede built-in defaults)
# setup = "bun install"   # command run in every new worktree; default: auto-detect from lockfile

# worktrees: where git wt new places worktrees.
#   "sibling" (default)  ../<repo>.worktrees/<branch>
#   "inside"             .worktrees/<branch> inside the repo (auto-added to .git/info/exclude)
#   "home"               ~/.worktrees/<repo>/<branch>
#   or a path template with {repo} and {branch}, e.g. "~/wt/{repo}/{branch}"
worktrees = ` + fmt.Sprintf("%q", placement) + `

clone = ["node_modules", ".turbo", ".next/cache", ".venv", "target", "vendor"]  # CoW-cloned: private to the worktree
share = [".env", ".env.*"]                                                      # symlinked: write-through to main checkout
skip  = ["dist", "build", "out"]                                                # not provisioned; rebuilt in the worktree
`
}

const claudeMdLine = "\n## Worktrees\nCreate worktrees with `git wt new <branch>` (never raw `git worktree add`); remove with `git wt rm <branch>`. Worktrees are pre-provisioned with node_modules and .env — no install needed.\n"

// Init scaffolds .worktree.toml (with the chosen worktree placement) and
// appends usage guidance to CLAUDE.md. Existing files are respected: the
// config is not overwritten, and the CLAUDE.md line is not duplicated.
func (r *Repo) Init(placement string, logf func(string, ...any)) error {
	home, _ := os.UserHomeDir()
	tmpl, err := resolvePlacement(placement, r.Name, r.MainRoot, home)
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(r.MainRoot, config.FileName)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		if err := os.WriteFile(cfgPath, []byte(DefaultConfigTOML(placement)), 0o644); err != nil {
			return err
		}
		logf("wrote %s", config.FileName)
		// For in-repo placements, exclude the container now so git status
		// stays clean from the first worktree on.
		static := filepath.Clean(strings.ReplaceAll(tmpl, "{branch}", "x"))
		if err := r.ensureExcluded(static); err != nil {
			return err
		}
	} else {
		logf("%s already exists, leaving it alone", config.FileName)
	}

	mdPath := filepath.Join(r.MainRoot, "CLAUDE.md")
	existing, err := os.ReadFile(mdPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(existing), "git wt new") {
		logf("CLAUDE.md already mentions git wt, leaving it alone")
		return nil
	}
	f, err := os.OpenFile(mdPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	// Writes to a freshly opened append fd only fail on disk-level errors.
	if _, err := f.WriteString(claudeMdLine); err != nil { // coverage-ignore
		return err
	}
	logf("appended worktree guidance to CLAUDE.md")
	return nil
}
