// Package wt implements the git-wt subcommands: worktree creation with
// state provisioning, listing, safe removal, pruning, and repo init.
package wt

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo locates the main checkout — the worktree that owns .git as a real
// directory. All provisioning sources come from here, and the sibling
// convention hangs off its parent directory.
type Repo struct {
	MainRoot string // absolute path to the main checkout
	Name     string // basename of the main checkout
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
	return &Repo{MainRoot: main, Name: filepath.Base(main)}, nil
}

// WorktreesDir is the sibling convention: ../<repo>.worktrees next to the
// main checkout.
func (r *Repo) WorktreesDir() string {
	return filepath.Join(filepath.Dir(r.MainRoot), r.Name+".worktrees")
}

// WorktreePath places a branch under the sibling convention.
func (r *Repo) WorktreePath(branch string) string {
	return filepath.Join(r.WorktreesDir(), SanitizeBranch(branch))
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

// managedWorktrees returns path->branch for worktrees under WorktreesDir.
func (r *Repo) managedWorktrees() (map[string]string, error) {
	out, err := r.git("worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	managed := map[string]string{}
	prefix := r.WorktreesDir() + string(filepath.Separator)
	var current string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			current = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch ") && strings.HasPrefix(current, prefix):
			managed[current] = strings.TrimPrefix(line, "branch refs/heads/")
		}
	}
	return managed, nil
}
