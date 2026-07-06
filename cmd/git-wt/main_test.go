package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

// runWT drives run() with captured output. tty=false unless a test opts in.
func runWT(t *testing.T, stdin string, tty bool, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf strings.Builder
	code = run(args, strings.NewReader(stdin), &out, &errBuf, tty)
	return code, out.String(), errBuf.String()
}

// initRepo creates a git repo with one commit and chdirs into it, so run()
// discovers it. The repo sits at <tmp>/repo to contain sibling worktrees.
func initRepo(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, root, "init", "-b", "main")
	mustGit(t, root, "config", "user.email", "test@example.com")
	mustGit(t, root, "config", "user.name", "Test")
	mustGit(t, root, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, root, "add", ".")
	mustGit(t, root, "commit", "-m", "init")
	t.Chdir(root)
	return root
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestRunNoArgs(t *testing.T) {
	code, _, stderr := runWT(t, "", false)
	if code != 2 || !strings.Contains(stderr, "usage: git wt") {
		t.Fatalf("code=%d stderr=%q, want 2 + usage", code, stderr)
	}
}

func TestRunHelp(t *testing.T) {
	for _, arg := range []string{"-h", "--help", "help"} {
		code, stdout, _ := runWT(t, "", false, arg)
		if code != 0 || !strings.Contains(stdout, "usage: git wt") {
			t.Errorf("%s: code=%d stdout=%q, want 0 + usage", arg, code, stdout)
		}
	}
}

func TestRunVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version"} {
		code, stdout, _ := runWT(t, "", false, arg)
		if code != 0 || !strings.HasPrefix(stdout, "git-wt ") {
			t.Errorf("%s: code=%d stdout=%q, want 0 + git-wt prefix", arg, code, stdout)
		}
	}
}

func TestVersionStringPrefersStampedVersion(t *testing.T) {
	orig := version
	version = "v1.2.3"
	t.Cleanup(func() { version = orig })
	if got := versionString(); got != "v1.2.3" {
		t.Errorf("versionString() = %q, want stamped v1.2.3", got)
	}
}

func TestVersionStringFallsBackToBuildInfo(t *testing.T) {
	orig := readBuildInfo
	t.Cleanup(func() { readBuildInfo = orig })

	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "v9.9.9"}}, true
	}
	if got := versionString(); got != "v9.9.9" {
		t.Errorf("versionString() = %q, want module version v9.9.9", got)
	}

	readBuildInfo = func() (*debug.BuildInfo, bool) { return nil, false }
	if got := versionString(); got != "dev" {
		t.Errorf("versionString() without build info = %q, want dev", got)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	code, _, stderr := runWT(t, "", false, "frobnicate")
	if code != 2 || !strings.Contains(stderr, `unknown command "frobnicate"`) {
		t.Fatalf("code=%d stderr=%q, want 2 + unknown command", code, stderr)
	}
}

func TestRunOutsideRepo(t *testing.T) {
	t.Chdir(t.TempDir())
	code, _, stderr := runWT(t, "", false, "ls")
	if code != 1 || !strings.Contains(stderr, "not inside a git repository") {
		t.Fatalf("code=%d stderr=%q, want 1 + not-a-repo", code, stderr)
	}
}

func TestRunNewAndLsAndRm(t *testing.T) {
	initRepo(t)

	code, stdout, stderr := runWT(t, "", false, "new", "feat")
	if code != 0 {
		t.Fatalf("new: code=%d stderr=%q", code, stderr)
	}
	path := strings.TrimSpace(stdout)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("printed worktree path %q does not exist: %v", path, err)
	}

	code, stdout, _ = runWT(t, "", false, "ls")
	if code != 0 || !strings.Contains(stdout, path+"\tfeat") {
		t.Fatalf("ls: code=%d stdout=%q, want %q listed", code, stdout, path)
	}

	code, _, stderr = runWT(t, "", false, "rm", "feat")
	if code != 0 || !strings.Contains(stderr, "removed worktree for feat") {
		t.Fatalf("rm: code=%d stderr=%q", code, stderr)
	}
}

func TestRunNewPorcelain(t *testing.T) {
	initRepo(t)
	code, stdout, stderr := runWT(t, "", false, "new", "feat", "--porcelain")
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if strings.Contains(stderr, "git-wt:") {
		t.Errorf("porcelain must silence logf, stderr=%q", stderr)
	}
	if lines := strings.Split(strings.TrimSpace(stdout), "\n"); len(lines) != 1 {
		t.Errorf("porcelain stdout should be exactly the path, got %q", stdout)
	}
}

func TestRunNewArgErrors(t *testing.T) {
	initRepo(t)
	if code, _, _ := runWT(t, "", false, "new"); code != 1 {
		t.Errorf("new without branch: code=%d, want 1", code)
	}
	if code, _, _ := runWT(t, "", false, "new", "--bogus", "feat"); code != 2 {
		t.Errorf("new with bad flag: code=%d, want 2", code)
	}
	if code, _, _ := runWT(t, "", false, "new", "bad..name"); code != 1 {
		t.Errorf("new with invalid ref: code=%d, want 1", code)
	}
	// A dangling --from swallows the branch as its value, leaving no
	// positional args — rejected with the usage error.
	if code, _, _ := runWT(t, "", false, "new", "feat", "--from"); code != 1 {
		t.Errorf("new with dangling --from: code=%d, want 1", code)
	}
}

func TestRunNewFromRef(t *testing.T) {
	initRepo(t)
	// The space-separated form exercises rest()'s value-flag reordering.
	code, stdout, stderr := runWT(t, "", false, "new", "feat", "--from", "main")
	if code != 0 {
		t.Fatalf("new --from main: code=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(strings.TrimSpace(stdout)); err != nil {
		t.Fatalf("printed worktree path does not exist: %v", err)
	}
}

func TestRunRmErrors(t *testing.T) {
	initRepo(t)
	if code, _, _ := runWT(t, "", false, "rm"); code != 1 {
		t.Errorf("rm without branch: code=%d, want 1", code)
	}
	if code, _, _ := runWT(t, "", false, "rm", "--bogus", "x"); code != 2 {
		t.Errorf("rm with bad flag: code=%d, want 2", code)
	}
	if code, _, stderr := runWT(t, "", false, "rm", "ghost"); code != 1 || !strings.Contains(stderr, "no managed worktree") {
		t.Errorf("rm unknown branch: code=%d stderr=%q, want 1", code, stderr)
	}
}

func TestRunRmForceFlagAfterPositional(t *testing.T) {
	root := initRepo(t)
	code, stdout, _ := runWT(t, "", false, "new", "feat", "--porcelain")
	if code != 0 {
		t.Fatal("new failed")
	}
	path := strings.TrimSpace(stdout)
	if err := os.WriteFile(filepath.Join(path, "wip.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Flag after the positional still parses (rest() reorders).
	if code, _, stderr := runWT(t, "", false, "rm", "feat", "--force"); code != 0 {
		t.Fatalf("rm --force after positional: code=%d stderr=%q", code, stderr)
	}
	_ = root
}

func TestRunPrune(t *testing.T) {
	initRepo(t)
	if code, _, _ := runWT(t, "", false, "new", "feat", "--porcelain"); code != 0 {
		t.Fatal("new failed")
	}
	code, _, stderr := runWT(t, "", false, "prune")
	if code != 0 || !strings.Contains(stderr, "pruned 1 worktree(s)") {
		t.Fatalf("prune: code=%d stderr=%q", code, stderr)
	}
}

func TestRunPruneError(t *testing.T) {
	root := initRepo(t)
	code, stdout, _ := runWT(t, "", false, "new", "feat", "--porcelain")
	if code != 0 {
		t.Fatal("new failed")
	}
	mustGit(t, root, "worktree", "lock", strings.TrimSpace(stdout))
	if code, _, _ := runWT(t, "", false, "prune"); code != 1 {
		t.Errorf("prune with locked worktree: code=%d, want 1", code)
	}
}

func TestRunInit(t *testing.T) {
	root := initRepo(t)
	code, _, stderr := runWT(t, "", false, "init", "--placement=inside")
	if code != 0 {
		t.Fatalf("init: code=%d stderr=%q", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(root, ".worktree.toml"))
	if err != nil || !strings.Contains(string(data), `worktrees = "inside"`) {
		t.Fatalf("config = %q, %v", data, err)
	}
}

func TestRunInitFlagError(t *testing.T) {
	initRepo(t)
	if code, _, _ := runWT(t, "", false, "init", "--bogus"); code != 2 {
		t.Errorf("init with bad flag: code=%d, want 2", code)
	}
}

func TestRunInitPromptsWhenTTY(t *testing.T) {
	root := initRepo(t)
	code, _, stderr := runWT(t, "2\n", true, "init")
	if code != 0 {
		t.Fatalf("init: code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "Where should git wt put worktrees?") {
		t.Errorf("prompt not shown: %q", stderr)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".worktree.toml"))
	if !strings.Contains(string(data), `worktrees = "inside"`) {
		t.Errorf("prompt answer not applied: %q", data)
	}
}

func TestRunInitSkipsPromptWhenConfigExists(t *testing.T) {
	root := initRepo(t)
	if err := os.WriteFile(filepath.Join(root, ".worktree.toml"), []byte(`worktrees = "sibling"`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runWT(t, "2\n", true, "init")
	if code != 0 || strings.Contains(stderr, "Where should") {
		t.Fatalf("init must not prompt when config exists: code=%d stderr=%q", code, stderr)
	}
}

func TestRunInitError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on clearing $HOME")
	}
	initRepo(t)
	t.Setenv("HOME", "")
	if code, _, _ := runWT(t, "", false, "init", "--placement=home"); code != 1 {
		t.Errorf("init with unresolvable placement: code=%d, want 1", code)
	}
}

func TestRunInitHook(t *testing.T) {
	root := initRepo(t)
	code, _, stderr := runWT(t, "", false, "init", "--hook", "--placement=sibling")
	if code != 0 {
		t.Fatalf("init --hook: code=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "settings.json")); err != nil {
		t.Errorf("hook settings not written: %v", err)
	}
}

func TestRunInitHookError(t *testing.T) {
	root := initRepo(t)
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runWT(t, "", false, "init", "--hook", "--placement=sibling"); code != 1 {
		t.Errorf("init --hook with corrupt settings: code=%d, want 1", code)
	}
}

func TestRest(t *testing.T) {
	got := rest([]string{"rm", "feat", "--force"})
	want := []string{"--force", "feat"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("rest = %v, want %v", got, want)
	}
	if got := rest([]string{"ls"}); len(got) != 0 {
		t.Errorf("rest(ls) = %v, want empty", got)
	}
}

func TestStdinIsTTY(t *testing.T) {
	// What stdin is under `go test` varies by environment (/dev/null is a
	// char device); assert consistency with the mode probe, not a value.
	fi, err := os.Stdin.Stat()
	want := err == nil && fi.Mode()&os.ModeCharDevice != 0
	if got := stdinIsTTY(); got != want {
		t.Errorf("stdinIsTTY() = %v, want %v", got, want)
	}
}

func TestPromptPlacement(t *testing.T) {
	var stderr strings.Builder
	for in, want := range map[string]string{
		"1\n":                    "sibling",
		"\n":                     "sibling",
		"2\n":                    "inside",
		"3\n":                    "home",
		"~/wt/{repo}/{branch}\n": "~/wt/{repo}/{branch}",
	} {
		if got := promptPlacement(strings.NewReader(in), &stderr); got != want {
			t.Errorf("promptPlacement(%q) = %q, want %q", in, got, want)
		}
	}
	// EOF before any input falls back to the default.
	if got := promptPlacement(strings.NewReader(""), &stderr); got != "sibling" {
		t.Errorf("promptPlacement(EOF) = %q, want sibling", got)
	}
}
