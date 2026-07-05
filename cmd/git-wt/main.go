// git-wt is a git extension (invoked as `git wt`) that creates worktrees
// pre-provisioned with ignored state — node_modules via copy-on-write
// clones, .env via symlinks — under one sibling-directory convention.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sonyabytes/git-wt/internal/wt"
)

const usage = `usage: git wt <command>

commands:
  new <branch> [--porcelain]   create + provision a worktree (branch is created if missing)
  ls                           list managed worktrees (path<TAB>branch)
  rm <branch> [--force]        remove a worktree; refuses on unsaved/unpushed work
  prune                        remove worktrees whose branches are merged or deleted
  init [--hook]                scaffold .worktree.toml + CLAUDE.md line; --hook also
                               installs a Claude Code guard against raw 'git worktree add'
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	logf := func(format string, a ...any) {
		fmt.Fprintf(os.Stderr, "git-wt: "+format+"\n", a...)
	}

	// Help and unknown commands must work outside a git repository.
	switch os.Args[1] {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	case "new", "ls", "rm", "prune", "init":
	default:
		fmt.Fprintf(os.Stderr, "git-wt: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	repo, err := wt.Discover(cwd)
	if err != nil {
		fatal(err)
	}

	switch cmd := os.Args[1]; cmd {
	case "new":
		fs := flag.NewFlagSet("new", flag.ExitOnError)
		porcelain := fs.Bool("porcelain", false, "print only the worktree path on stdout")
		fs.Parse(rest())
		if fs.NArg() != 1 {
			fatal(fmt.Errorf("usage: git wt new <branch>"))
		}
		quiet := logf
		if *porcelain {
			quiet = func(string, ...any) {}
		}
		path, err := repo.New(fs.Arg(0), quiet)
		if err != nil {
			fatal(err)
		}
		fmt.Println(path)

	case "ls":
		lines, err := repo.Ls()
		if err != nil {
			fatal(err)
		}
		for _, l := range lines {
			fmt.Println(l)
		}

	case "rm":
		fs := flag.NewFlagSet("rm", flag.ExitOnError)
		force := fs.Bool("force", false, "remove even with uncommitted or unpushed work")
		fs.Parse(rest())
		if fs.NArg() != 1 {
			fatal(fmt.Errorf("usage: git wt rm <branch>"))
		}
		if err := repo.Rm(fs.Arg(0), *force); err != nil {
			fatal(err)
		}
		logf("removed worktree for %s", fs.Arg(0))

	case "prune":
		removed, err := repo.Prune(logf)
		if err != nil {
			fatal(err)
		}
		logf("pruned %d worktree(s)", len(removed))

	case "init":
		fs := flag.NewFlagSet("init", flag.ExitOnError)
		hook := fs.Bool("hook", false, "install Claude Code PreToolUse guard hook")
		fs.Parse(rest())
		if err := repo.Init(logf); err != nil {
			fatal(err)
		}
		if *hook {
			if err := repo.InstallHook(logf); err != nil {
				fatal(err)
			}
		}
	}
}

// rest returns the args after the subcommand, flags first — the flag pkg
// stops at the first positional, and `git wt rm mybranch --force` should work.
func rest() []string {
	var flags, positional []string
	for _, a := range os.Args[2:] {
		if len(a) > 1 && a[0] == '-' {
			flags = append(flags, a)
		} else {
			positional = append(positional, a)
		}
	}
	return append(flags, positional...)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "git-wt: %v\n", err)
	os.Exit(1)
}
