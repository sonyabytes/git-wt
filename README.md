# git-wt

Agent-friendly git worktrees. `git wt new <branch>` creates a worktree that is **ready to work in** — `node_modules` arrives via copy-on-write clone (near-zero time and disk on APFS), `.env` is symlinked, the setup command has already run — and it lands in one predictable place: `../<repo>.worktrees/<branch>`.

## Why

Plain `git worktree add` gives agents (and humans) a checkout that's missing all ignored state: no dependencies, no env file, no caches. Agents then burn time on reinstalls or chase phantom errors — and every agent invents its own worktree location and naming. `git-wt` fixes both.

The mechanism follows one rule: **state an experiment may mutate is CoW-cloned** (writes stay private to the worktree — APFS materializes a real copy only on first write), **state that must stay shared is symlinked** (writes go through). See [CONTEXT.md](CONTEXT.md) for the vocabulary and [docs/adr/0001](docs/adr/0001-cow-clones-over-symlinks-for-mutable-state.md) for why not symlinks-for-everything.

## Install

```sh
go install github.com/sonyabytes/git-wt/cmd/git-wt@latest
```

Or grab a prebuilt binary from [Releases](https://github.com/sonyabytes/git-wt/releases). Any `git-wt` on PATH is invokable as `git wt`.

## Usage

```sh
git wt new feature/auth       # create branch (if needed) + provisioned worktree; prints its path
git wt new feature/auth --porcelain   # path only on stdout, for scripts/agents
git wt ls                     # managed worktrees: path<TAB>branch
git wt rm feature/auth        # refuses on uncommitted/unpushed work; --force overrides; never deletes the branch
git wt prune                  # remove clean worktrees whose branches are merged into HEAD
git wt init                   # scaffold .worktree.toml + append agent guidance to CLAUDE.md
git wt init --hook            # …plus a Claude Code hook blocking raw 'git worktree add'
```

## Configuration — `.worktree.toml`

First match wins; repo rules precede built-in defaults.

```toml
# setup = "bun install"    # run in every new worktree; default: auto-detected from lockfile

clone = ["node_modules", ".turbo", ".next/cache"]  # CoW-cloned: private to the worktree
share = [".env", ".env.*"]                         # symlinked: write-through to main checkout
skip  = ["dist", "build", "out"]                   # not provisioned; rebuilt in the worktree
```

## Notes

- macOS/APFS-first. On filesystems without CoW cloning, `clone` degrades to a plain copy (correct, just slower). Linux reflink support is a planned drop-in.
- `prune` never touches a worktree whose branch ref was deleted underneath it — that state makes every file look uncommitted, so it's reported and left for `git wt rm --force`.
- Verification: `go test ./...` plus `scripts/e2e.sh` (full scenario suite incl. CoW disk-usage proof).
