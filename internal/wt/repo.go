// Package wt implements the git-wt subcommands: worktree creation with
// state provisioning, listing, safe removal, pruning, and repo init.
package wt

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sonyabytes/git-wt/internal/config"
)

// Repo locates the main checkout — the worktree that owns .git as a real
// directory. All provisioning sources come from here, and worktree placement
// is resolved relative to it.
type Repo struct {
	MainRoot string         // absolute path to the main checkout
	Name     string         // basename of the main checkout
	Cfg      *config.Config // loaded once at Discover
	pathTmpl string         // absolute worktree path template, {branch} unexpanded
}

// Discover resolves the Repo from any directory inside it, including from
// within a linked worktree.
func Discover(dir string) (*Repo, error) {
	out, err := gitIn(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, fmt.Errorf("not inside a git repository: %w", err)
	}
	commonDir := strings.TrimSpace(out)
	if filepath.Base(commonDir) != ".git" {
		return nil, fmt.Errorf("bare repositories are not supported (git common dir: %s)", commonDir)
	}
	main := filepath.Dir(commonDir)
	cfg, err := config.Load(main)
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir() // empty home only errors if the placement uses ~
	tmpl, err := resolvePlacement(cfg.Worktrees, filepath.Base(main), main, home)
	if err != nil {
		return nil, err
	}
	return &Repo{MainRoot: main, Name: filepath.Base(main), Cfg: cfg, pathTmpl: tmpl}, nil
}

// resolvePlacement turns the raw `worktrees` config value — a preset name or
// a path template — into an absolute path template with {branch} unexpanded.
func resolvePlacement(raw, repoName, mainRoot, home string) (string, error) {
	var tmpl string
	switch raw {
	case "", "sibling":
		tmpl = filepath.Join(filepath.Dir(mainRoot), repoName+".worktrees", "{branch}")
	case "inside":
		tmpl = filepath.Join(mainRoot, ".worktrees", "{branch}")
	case "home":
		if home == "" {
			return "", fmt.Errorf("placement %q requires a home directory", raw)
		}
		tmpl = filepath.Join(home, ".worktrees", repoName, "{branch}")
	default:
		tmpl = strings.ReplaceAll(raw, "{repo}", repoName)
		if tmpl == "~" || strings.HasPrefix(tmpl, "~/") {
			if home == "" {
				return "", fmt.Errorf("placement %q uses ~ but no home directory is available", raw)
			}
			tmpl = filepath.Join(home, strings.TrimPrefix(tmpl, "~"))
		}
		if !filepath.IsAbs(tmpl) {
			tmpl = filepath.Join(mainRoot, tmpl)
		}
		if !strings.Contains(tmpl, "{branch}") {
			tmpl = filepath.Join(tmpl, "{branch}")
		}
	}
	// Sanity-check the static part: with {branch} stripped, the placement
	// must not collapse onto the main checkout or land inside .git.
	static := filepath.Clean(strings.ReplaceAll(tmpl, "{branch}", ""))
	if static == filepath.Clean(mainRoot) {
		return "", fmt.Errorf("placement %q resolves to the main checkout itself", raw)
	}
	gitDir := filepath.Join(mainRoot, ".git")
	if static == gitDir || strings.HasPrefix(static, gitDir+string(filepath.Separator)) {
		return "", fmt.Errorf("placement %q resolves inside .git", raw)
	}
	return tmpl, nil
}

// WorktreePath renders the placement template for a branch.
func (r *Repo) WorktreePath(branch string) string {
	return filepath.Clean(strings.ReplaceAll(r.pathTmpl, "{branch}", SanitizeBranch(branch)))
}

// SanitizeBranch flattens a branch name into a directory name: slashes
// become dashes (feature/auth -> feature-auth).
func SanitizeBranch(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

// git runs a git command in the main checkout and returns stdout.
func (r *Repo) git(args ...string) (string, error) {
	return gitIn(r.MainRoot, args...)
}

func gitIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return string(out), nil
}

// gitPassthrough runs git with output wired to the user's terminal, for
// commands whose progress matters (worktree add).
func (r *Repo) gitPassthrough(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.MainRoot
	cmd.Stdout = os.Stderr // keep stdout clean for --porcelain output
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// branchExists reports whether a local branch exists.
func (r *Repo) branchExists(branch string) bool {
	_, err := r.git("rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// managedWorktrees returns path->branch for worktrees whose path round-trips
// through the placement template: a worktree is managed exactly when it sits
// where WorktreePath would put its branch. Worktrees created under a previous
// placement remain plain git worktrees but drop out of ls/rm/prune.
func (r *Repo) managedWorktrees() (map[string]string, error) {
	out, err := r.git("worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	managed := map[string]string{}
	var current string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			// Clean normalizes git's forward-slash output to native
			// separators, so Rm's WorktreePath lookup matches on Windows.
			current = filepath.Clean(strings.TrimPrefix(line, "worktree "))
		case strings.HasPrefix(line, "branch ") && current != "":
			branch := strings.TrimPrefix(line, "branch refs/heads/")
			if samePath(current, r.WorktreePath(branch)) {
				managed[current] = branch
			}
		}
	}
	return managed, nil
}

// samePath compares two paths after cleaning and symlink resolution (where
// the paths exist), so /tmp vs /private/tmp on macOS compare equal.
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return a == b
}

// ensureExcluded git-ignores dir's first path segment under MainRoot by
// appending to .git/info/exclude — per-clone, nothing to commit. No-op when
// dir is outside the main checkout or the segment is already ignored.
func (r *Repo) ensureExcluded(dir string) error {
	rel, err := filepath.Rel(r.MainRoot, dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return nil
	}
	seg := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	if _, err := r.git("check-ignore", "-q", seg); err == nil {
		return nil // already ignored
	}
	infoDir := filepath.Join(r.MainRoot, ".git", "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(infoDir, "exclude"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("/" + seg + "/\n")
	return err
}
