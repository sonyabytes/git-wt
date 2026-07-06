package wt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewProvisionsCloneAndShare(t *testing.T) {
	needsUnix(t) // .env is provisioned via symlink
	r := initRepo(t)
	if err := os.MkdirAll(filepath.Join(r.MainRoot, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.MainRoot, "node_modules", "pkg", "index.js"), []byte("js"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.MainRoot, ".env"), []byte("K=V"), 0o644); err != nil {
		t.Fatal(err)
	}

	var lines []string
	path, err := r.New("feature/auth", "", grabLog(&lines))
	if err != nil {
		t.Fatal(err)
	}
	if want := r.WorktreePath("feature/auth"); path != want {
		t.Errorf("New path = %q, want %q", path, want)
	}
	if got, err := os.ReadFile(filepath.Join(path, "node_modules", "pkg", "index.js")); err != nil || string(got) != "js" {
		t.Errorf("cloned node_modules content = %q, %v", got, err)
	}
	if fi, err := os.Lstat(filepath.Join(path, ".env")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf(".env should be a symlink, got %v, %v", fi, err)
	}
	joined := strings.Join(lines, "\n")
	// "cloning" where CoW is available (APFS), "copying" on the fallback.
	if !strings.Contains(joined, "cloning") && !strings.Contains(joined, "copying") {
		t.Errorf("log should mention cloning or copying, got:\n%s", joined)
	}
	if !strings.Contains(joined, "sharing") {
		t.Errorf("log should mention sharing, got:\n%s", joined)
	}
}

func TestNewUsesExistingBranch(t *testing.T) {
	r := initRepo(t)
	mustGit(t, r.MainRoot, "branch", "existing")
	path, err := r.New("existing", "", discardLog)
	if err != nil {
		t.Fatal(err)
	}
	if out := mustGit(t, path, "rev-parse", "--abbrev-ref", "HEAD"); strings.TrimSpace(out) != "existing" {
		t.Errorf("worktree HEAD = %q, want existing", out)
	}
}

func TestNewRefusesExistingWorktree(t *testing.T) {
	r := initRepo(t)
	if _, err := r.New("dup", "", discardLog); err != nil {
		t.Fatal(err)
	}
	if _, err := r.New("dup", "", discardLog); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second New = %v, want already-exists error", err)
	}
}

func TestNewReportsSanitizationCollision(t *testing.T) {
	r := initRepo(t)
	if _, err := r.New("feature/auth", "", discardLog); err != nil {
		t.Fatal(err)
	}
	_, err := r.New("feature-auth", "", discardLog)
	if err == nil || !strings.Contains(err.Error(), "sanitization") || !strings.Contains(err.Error(), `"feature/auth"`) {
		t.Fatalf("New = %v, want collision error naming feature/auth", err)
	}
}

func TestNewFromBaseRef(t *testing.T) {
	r := initRepo(t)
	base := strings.TrimSpace(mustGit(t, r.MainRoot, "rev-parse", "HEAD"))
	// A second commit moves HEAD past the base ref.
	if err := os.WriteFile(filepath.Join(r.MainRoot, "more.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, r.MainRoot, "add", ".")
	mustGit(t, r.MainRoot, "commit", "-m", "second")
	path, err := r.New("feat", base, discardLog)
	if err != nil {
		t.Fatal(err)
	}
	if head := strings.TrimSpace(mustGit(t, path, "rev-parse", "HEAD")); head != base {
		t.Errorf("worktree HEAD = %s, want base %s", head, base)
	}
}

func TestNewFromRejectsExistingBranch(t *testing.T) {
	r := initRepo(t)
	mustGit(t, r.MainRoot, "branch", "existing")
	if _, err := r.New("existing", "main", discardLog); err == nil || !strings.Contains(err.Error(), "--from") {
		t.Fatalf("New = %v, want error explaining --from needs a new branch", err)
	}
}

func TestNewFailsWhenPlacementParentIsFile(t *testing.T) {
	r := initRepo(t)
	if err := os.WriteFile(filepath.Join(r.MainRoot, "container"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r = writeConfig(t, r, `worktrees = "container/{branch}"`)
	if _, err := r.New("feat", "", discardLog); err == nil {
		t.Fatal("expected MkdirAll error when placement parent is a file")
	}
}

func TestNewFailsWhenExcludeFileUnwritable(t *testing.T) {
	r := initRepo(t)
	r = writeConfig(t, r, `worktrees = "inside"`)
	// .git/info as a file makes ensureExcluded's MkdirAll fail.
	info := filepath.Join(r.MainRoot, ".git", "info")
	if err := os.RemoveAll(info); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(info, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.New("feat", "", discardLog); err == nil {
		t.Fatal("expected ensureExcluded error")
	}
}

func TestNewFailsOnInvalidBranchName(t *testing.T) {
	r := initRepo(t)
	if _, err := r.New("bad..name", "", discardLog); err == nil {
		t.Fatal("expected git worktree add to fail on invalid ref name")
	}
}

func TestNewFailsOnBadProvisionPattern(t *testing.T) {
	r := initRepo(t)
	r = writeConfig(t, r, `clone = ["["]`)
	if _, err := r.New("feat", "", discardLog); err == nil || !strings.Contains(err.Error(), "bad pattern") {
		t.Fatalf("New = %v, want bad-pattern error", err)
	}
}

func TestNewRunsSetupCommand(t *testing.T) {
	needsUnix(t) // setup runs via sh
	r := initRepo(t)
	r = writeConfig(t, r, `setup = "echo ok > setup-ran"`)
	path, err := r.New("feat", "", discardLog)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, "setup-ran")); err != nil {
		t.Errorf("setup command did not run in worktree: %v", err)
	}
}

func TestNewFailsWhenSetupFails(t *testing.T) {
	needsUnix(t)
	r := initRepo(t)
	r = writeConfig(t, r, `setup = "false"`)
	if _, err := r.New("feat", "", discardLog); err == nil || !strings.Contains(err.Error(), "setup command") {
		t.Fatalf("New = %v, want setup failure", err)
	}
}

func TestLs(t *testing.T) {
	r := initRepo(t)
	if lines, err := r.Ls(); err != nil || len(lines) != 0 {
		t.Fatalf("Ls on fresh repo = %v, %v; want empty", lines, err)
	}
	path, err := r.New("feat", "", discardLog)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := r.Ls()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !strings.HasPrefix(lines[0], path+"\t") || !strings.HasSuffix(lines[0], "\tfeat") {
		t.Errorf("Ls = %v, want [%s\\tfeat]", lines, path)
	}
}

func TestLsIgnoresUnmanagedWorktrees(t *testing.T) {
	r := initRepo(t)
	other := filepath.Join(filepath.Dir(r.MainRoot), "elsewhere")
	mustGit(t, r.MainRoot, "worktree", "add", "-b", "stray", other)
	lines, err := r.Ls()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Errorf("Ls should not list unmanaged worktrees, got %v", lines)
	}
}

func TestLsErrorOnBrokenRepo(t *testing.T) {
	r := &Repo{MainRoot: filepath.Join(t.TempDir(), "gone"), Name: "gone", pathTmpl: "{branch}"}
	if _, err := r.Ls(); err == nil {
		t.Fatal("expected error when repo dir does not exist")
	}
}

func TestRmUnmanagedBranch(t *testing.T) {
	r := initRepo(t)
	if err := r.Rm("nope", false); err == nil || !strings.Contains(err.Error(), "no managed worktree") {
		t.Fatalf("Rm = %v, want no-managed-worktree error", err)
	}
}

func TestRmErrorOnBrokenRepo(t *testing.T) {
	r := &Repo{MainRoot: filepath.Join(t.TempDir(), "gone"), Name: "gone", pathTmpl: "{branch}"}
	if err := r.Rm("x", false); err == nil {
		t.Fatal("expected error when repo dir does not exist")
	}
}

func TestRmRefusesDirtyWorktree(t *testing.T) {
	r := initRepo(t)
	path, err := r.New("feat", "", discardLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "wip.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err = r.Rm("feat", false)
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("Rm = %v, want dirty refusal mentioning --force", err)
	}
}

func TestRmForceRemovesDirtyWorktree(t *testing.T) {
	r := initRepo(t)
	path, err := r.New("feat", "", discardLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "wip.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Rm("feat", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("worktree should be gone, stat err = %v", err)
	}
}

func TestRmCleanMergedWorktree(t *testing.T) {
	r := initRepo(t)
	if _, err := r.New("feat", "", discardLog); err != nil {
		t.Fatal(err)
	}
	if err := r.Rm("feat", false); err != nil {
		t.Fatalf("Rm of clean merged worktree = %v", err)
	}
}

func TestRmRefusesUnpushedUpstreamCommits(t *testing.T) {
	r := initRepo(t)
	path, err := r.New("feat", "", discardLog)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, path, "branch", "--set-upstream-to=main")
	if err := os.WriteFile(filepath.Join(path, "new.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, path, "add", ".")
	mustGit(t, path, "commit", "-m", "work")
	if err := r.Rm("feat", false); err == nil || !strings.Contains(err.Error(), "not pushed") {
		t.Fatalf("Rm = %v, want unpushed refusal", err)
	}
}

func TestRmAllowsWhenUpstreamCaughtUp(t *testing.T) {
	r := initRepo(t)
	path, err := r.New("feat", "", discardLog)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, path, "branch", "--set-upstream-to=main")
	if err := r.Rm("feat", false); err != nil {
		t.Fatalf("Rm with caught-up upstream = %v", err)
	}
}

func TestRmRefusesUnmergedWithoutUpstream(t *testing.T) {
	r := initRepo(t)
	path, err := r.New("feat", "", discardLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "new.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, path, "add", ".")
	mustGit(t, path, "commit", "-m", "work")
	if err := r.Rm("feat", false); err == nil || !strings.Contains(err.Error(), "not merged") {
		t.Fatalf("Rm = %v, want not-merged refusal", err)
	}
}

func TestRmErrorWhenStatusFails(t *testing.T) {
	r := initRepo(t)
	path, err := r.New("feat", "", discardLog)
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt the worktree's .git link: it stays registered (managed) but
	// git commands inside it fail, surfacing isDirty's error path.
	if err := os.Remove(filepath.Join(path, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := r.Rm("feat", false); err == nil {
		t.Fatal("expected error when git status fails in the worktree")
	}
}

func TestRmErrorWhenWorktreeLocked(t *testing.T) {
	r := initRepo(t)
	path, err := r.New("feat", "", discardLog)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, r.MainRoot, "worktree", "lock", path)
	if err := r.Rm("feat", true); err == nil {
		t.Fatal("expected git worktree remove to fail on a locked worktree")
	}
}

func TestPruneRemovesMergedSkipsDirtyAndDeleted(t *testing.T) {
	r := initRepo(t)
	merged, err := r.New("merged", "", discardLog)
	if err != nil {
		t.Fatal(err)
	}
	dirty, err := r.New("dirty", "", discardLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirty, "wip.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ahead, err := r.New("ahead", "", discardLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ahead, "new.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, ahead, "add", ".")
	mustGit(t, ahead, "commit", "-m", "work")
	if _, err := r.New("deleted", "", discardLog); err != nil {
		t.Fatal(err)
	}
	mustGit(t, r.MainRoot, "update-ref", "-d", "refs/heads/deleted")

	var lines []string
	removed, err := r.Prune(grabLog(&lines))
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != merged {
		t.Errorf("Prune removed %v, want [%s]", removed, merged)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"was deleted", "uncommitted changes", "merged"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Prune log missing %q:\n%s", want, joined)
		}
	}
	if _, err := os.Stat(dirty); err != nil {
		t.Errorf("dirty worktree should survive: %v", err)
	}
	if _, err := os.Stat(ahead); err != nil {
		t.Errorf("unmerged worktree should survive: %v", err)
	}
}

func TestPruneSkipsWorktreeWhoseStatusFails(t *testing.T) {
	r := initRepo(t)
	path, err := r.New("vanished", "", discardLog)
	if err != nil {
		t.Fatal(err)
	}
	// A manually deleted worktree directory makes the dirty check error out;
	// prune must skip rather than treat it as clean and removable.
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	var lines []string
	removed, err := r.Prune(grabLog(&lines))
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Errorf("Prune removed %v, want none", removed)
	}
	if joined := strings.Join(lines, "\n"); !strings.Contains(joined, "skipping "+path) {
		t.Errorf("Prune log should say it skipped %s:\n%s", path, joined)
	}
}

func TestPruneErrorOnBrokenRepo(t *testing.T) {
	r := &Repo{MainRoot: filepath.Join(t.TempDir(), "gone"), Name: "gone", pathTmpl: "{branch}"}
	if _, err := r.Prune(discardLog); err == nil {
		t.Fatal("expected error when repo dir does not exist")
	}
}

func TestPruneErrorWhenRemoveFails(t *testing.T) {
	r := initRepo(t)
	path, err := r.New("merged", "", discardLog)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, r.MainRoot, "worktree", "lock", path)
	if _, err := r.Prune(discardLog); err == nil {
		t.Fatal("expected error when locked worktree cannot be removed")
	}
}

func TestDefaultConfigTOML(t *testing.T) {
	if got := DefaultConfigTOML(""); !strings.Contains(got, `worktrees = "sibling"`) {
		t.Errorf("empty placement should default to sibling:\n%s", got)
	}
	if got := DefaultConfigTOML("inside"); !strings.Contains(got, `worktrees = "inside"`) {
		t.Errorf("placement should be embedded:\n%s", got)
	}
}

func TestInitScaffoldsConfigAndClaudeMd(t *testing.T) {
	r := initRepo(t)
	var lines []string
	if err := r.Init("", grabLog(&lines)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.MainRoot, ".worktree.toml")); err != nil {
		t.Errorf(".worktree.toml not written: %v", err)
	}
	md, err := os.ReadFile(filepath.Join(r.MainRoot, "CLAUDE.md"))
	if err != nil || !strings.Contains(string(md), "git wt new") {
		t.Errorf("CLAUDE.md = %q, %v; want worktree guidance", md, err)
	}

	// Second run leaves both files alone.
	lines = nil
	if err := r.Init("", grabLog(&lines)); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "already exists") || !strings.Contains(joined, "already mentions") {
		t.Errorf("second Init should report existing files:\n%s", joined)
	}
}

func TestInitInsidePlacementExcludesContainer(t *testing.T) {
	r := initRepo(t)
	if err := r.Init("inside", discardLog); err != nil {
		t.Fatal(err)
	}
	exclude, err := os.ReadFile(filepath.Join(r.MainRoot, ".git", "info", "exclude"))
	if err != nil || !strings.Contains(string(exclude), "/.worktrees/") {
		t.Errorf("exclude = %q, %v; want /.worktrees/ entry", exclude, err)
	}
}

func TestInitInvalidPlacement(t *testing.T) {
	needsUnix(t) // clears $HOME to make the home preset unresolvable
	r := initRepo(t)
	t.Setenv("HOME", "")
	if err := r.Init("home", discardLog); err == nil {
		t.Fatal("expected error for home placement without home dir")
	}
}

func TestInitConfigWriteFails(t *testing.T) {
	needsPermissionChecks(t)
	r := initRepo(t)
	if err := os.Chmod(r.MainRoot, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(r.MainRoot, 0o755) })
	if err := r.Init("", discardLog); err == nil {
		t.Fatal("expected error writing config into read-only repo root")
	}
}

func TestInitExcludeFails(t *testing.T) {
	r := initRepo(t)
	info := filepath.Join(r.MainRoot, ".git", "info")
	if err := os.RemoveAll(info); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(info, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Init("inside", discardLog); err == nil {
		t.Fatal("expected ensureExcluded error for inside placement")
	}
}

func TestInitClaudeMdReadFails(t *testing.T) {
	r := initRepo(t)
	if err := os.Mkdir(filepath.Join(r.MainRoot, "CLAUDE.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := r.Init("", discardLog); err == nil {
		t.Fatal("expected error when CLAUDE.md is unreadable")
	}
}

func TestInitClaudeMdAppendFails(t *testing.T) {
	needsPermissionChecks(t)
	r := initRepo(t)
	if err := os.WriteFile(filepath.Join(r.MainRoot, "CLAUDE.md"), []byte("# notes\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := r.Init("", discardLog); err == nil {
		t.Fatal("expected error appending to read-only CLAUDE.md")
	}
}

func TestInitAppendsToExistingClaudeMd(t *testing.T) {
	r := initRepo(t)
	if err := os.WriteFile(filepath.Join(r.MainRoot, "CLAUDE.md"), []byte("# notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Init("", discardLog); err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(filepath.Join(r.MainRoot, "CLAUDE.md"))
	if !strings.HasPrefix(string(md), "# notes\n") || !strings.Contains(string(md), "git wt new") {
		t.Errorf("CLAUDE.md should keep existing content and append guidance:\n%s", md)
	}
}
