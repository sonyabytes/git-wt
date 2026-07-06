// git-wt is a git extension (invoked as `git wt`) that creates worktrees
// pre-provisioned with ignored state — node_modules via copy-on-write
// clones, .env via symlinks — under one sibling-directory convention.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/sonyabytes/git-wt/internal/config"
	"github.com/sonyabytes/git-wt/internal/wt"
)

const usage = `usage: git wt <command>

commands:
  new <branch> [--porcelain] [--from=<ref>]
                               create + provision a worktree; the branch is created if
                               missing (from <ref> when --from is given, else from HEAD)
  ls                           list managed worktrees (path<TAB>branch)
  rm <branch> [--force]        remove a worktree; refuses on unsaved/unpushed work
  prune                        remove worktrees whose branches are merged or deleted
  init [--hook] [--placement=<value>]
                               scaffold .worktree.toml + CLAUDE.md line; prompts for
                               worktree placement (sibling|inside|home|<template>) unless
                               --placement is given or stdin is not a terminal; --hook also
                               installs a Claude Code guard against raw 'git worktree add'
  version                      print the git-wt version
`

// version is stamped by goreleaser on release builds (-X main.version);
// source builds fall back to the module version `go install` records.
var version = "dev"

// readBuildInfo is a seam over debug.ReadBuildInfo so tests can exercise
// every version-resolution branch.
var readBuildInfo = debug.ReadBuildInfo

func versionString() string {
	if version != "dev" {
		return version
	}
	if bi, ok := readBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

func main() { // coverage-ignore
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, stdinIsTTY()))
}

// run is the whole CLI behind an exit code, with the process globals (args,
// streams, terminal-ness) injected so tests can drive every path.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer, tty bool) int {
	if len(args) < 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	logf := func(format string, a ...any) {
		fmt.Fprintf(stderr, "git-wt: "+format+"\n", a...)
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "git-wt: %v\n", err)
		return 1
	}

	// Help, version, and unknown commands must work outside a git repository.
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "version", "--version":
		fmt.Fprintln(stdout, "git-wt "+versionString())
		return 0
	case "new", "ls", "rm", "prune", "init":
	default:
		fmt.Fprintf(stderr, "git-wt: unknown command %q\n\n%s", args[0], usage)
		return 2
	}

	// Unreachable in tests: the working directory always exists there.
	cwd, err := os.Getwd()
	if err != nil { // coverage-ignore
		return fail(err)
	}
	repo, err := wt.Discover(cwd)
	if err != nil {
		return fail(err)
	}

	switch cmd := args[0]; cmd {
	case "new":
		fs := flag.NewFlagSet("new", flag.ContinueOnError)
		fs.SetOutput(stderr)
		porcelain := fs.Bool("porcelain", false, "print only the worktree path on stdout")
		from := fs.String("from", "", "base ref for a newly created branch (default: HEAD)")
		if err := fs.Parse(rest(args)); err != nil {
			return 2
		}
		if fs.NArg() != 1 {
			return fail(fmt.Errorf("usage: git wt new <branch> [--from=<ref>]"))
		}
		quiet := logf
		if *porcelain {
			quiet = func(string, ...any) {}
		}
		path, err := repo.New(fs.Arg(0), *from, quiet)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, path)

	case "ls":
		lines, err := repo.Ls()
		// Unreachable in practice: Discover just succeeded on this repo.
		if err != nil { // coverage-ignore
			return fail(err)
		}
		for _, l := range lines {
			fmt.Fprintln(stdout, l)
		}

	case "rm":
		fs := flag.NewFlagSet("rm", flag.ContinueOnError)
		fs.SetOutput(stderr)
		force := fs.Bool("force", false, "remove even with uncommitted or unpushed work")
		if err := fs.Parse(rest(args)); err != nil {
			return 2
		}
		if fs.NArg() != 1 {
			return fail(fmt.Errorf("usage: git wt rm <branch>"))
		}
		if err := repo.Rm(fs.Arg(0), *force); err != nil {
			return fail(err)
		}
		logf("removed worktree for %s", fs.Arg(0))

	case "prune":
		removed, err := repo.Prune(logf)
		if err != nil {
			return fail(err)
		}
		logf("pruned %d worktree(s)", len(removed))

	case "init":
		fs := flag.NewFlagSet("init", flag.ContinueOnError)
		fs.SetOutput(stderr)
		hook := fs.Bool("hook", false, "install Claude Code PreToolUse guard hook")
		placement := fs.String("placement", "", "worktree placement: sibling|inside|home|<template> (use --placement=<value>)")
		if err := fs.Parse(rest(args)); err != nil {
			return 2
		}
		// Only prompt when the answer will be used: config not yet scaffolded
		// and someone is at the terminal.
		if _, err := os.Stat(filepath.Join(repo.MainRoot, config.FileName)); *placement == "" && os.IsNotExist(err) && tty {
			*placement = promptPlacement(stdin, stderr)
		}
		if err := repo.Init(*placement, logf); err != nil {
			return fail(err)
		}
		if *hook {
			if err := repo.InstallHook(logf); err != nil {
				return fail(err)
			}
		}
	}
	return 0
}

// rest returns the args after the subcommand, flags first — the flag pkg
// stops at the first positional, and `git wt rm mybranch --force` should
// work. Value-taking flags keep their space-separated argument attached so
// `git wt new mybranch --from origin/main` reorders correctly.
func rest(args []string) []string {
	var flags, positional []string
	tail := args[1:]
	for i := 0; i < len(tail); i++ {
		a := tail[i]
		switch {
		case a == "--from" || a == "-from":
			flags = append(flags, a)
			if i+1 < len(tail) {
				i++
				flags = append(flags, tail[i])
			}
		case len(a) > 1 && a[0] == '-':
			flags = append(flags, a)
		default:
			positional = append(positional, a)
		}
	}
	return append(flags, positional...)
}

func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// promptPlacement asks (on stderr, keeping stdout clean) where worktrees
// should live. Empty input or a read error falls back to the sibling default;
// invalid custom templates are caught by Init's validation.
func promptPlacement(stdin io.Reader, stderr io.Writer) string {
	fmt.Fprint(stderr, `Where should git wt put worktrees?
  1) sibling   ../<repo>.worktrees/<branch>   [default]
  2) inside    .worktrees/<branch> inside the repo (auto-ignored)
  3) home      ~/.worktrees/<repo>/<branch>
Note: placements on a different volume than the repo lose copy-on-write provisioning.
Enter 1-3, or a custom path template with {repo}/{branch}: `)
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && line == "" {
		return "sibling"
	}
	switch ans := strings.TrimSpace(line); ans {
	case "", "1":
		return "sibling"
	case "2":
		return "inside"
	case "3":
		return "home"
	default:
		return ans
	}
}
