# git-wt

Agent-friendly git worktrees. `git wt new <branch>` creates a worktree that is **ready to work in** — `node_modules` arrives via copy-on-write clone (near-zero time and disk on APFS), `.env` is symlinked, the setup command has already run — and it lands in one predictable, [configurable](#configuration--worktreetoml) place (default: `../<repo>.worktrees/<branch>`).

## Why

Plain `git worktree add` gives agents (and humans) a checkout that's missing all ignored state: no dependencies, no env file, no caches. Agents then burn time on reinstalls or chase phantom errors — and every agent invents its own worktree location and naming. `git-wt` fixes both.

The mechanism follows one rule: **state an experiment may mutate is CoW-cloned** (writes stay private to the worktree — APFS materializes a real copy only on first write), **state that must stay shared is symlinked** (writes go through). See [CONTEXT.md](CONTEXT.md) for the vocabulary and the reasoning behind CoW clones over symlinks-for-everything.

## Install

**Homebrew** (macOS, Linuxbrew):

```sh
brew install sonyabytes/tap/git-wt
```

**Go**:

```sh
go install github.com/sonyabytes/git-wt/cmd/git-wt@latest
```

**Debian/Ubuntu, Fedora/RHEL, Alpine**: `.deb`, `.rpm`, and `.apk` packages are attached to each [release](https://github.com/sonyabytes/git-wt/releases) — install with `dpkg -i` / `rpm -i` / `apk add --allow-untrusted`.

Or grab a prebuilt binary tarball from [Releases](https://github.com/sonyabytes/git-wt/releases). Any `git-wt` on PATH is invokable as `git wt`.

## Usage

```sh
git wt new feature/auth       # create branch (if needed) + provisioned worktree; prints its path
git wt new feature/auth --porcelain   # path only on stdout, for scripts/agents
git wt ls                     # managed worktrees: path<TAB>branch
git wt rm feature/auth        # refuses on uncommitted/unpushed work; --force overrides; never deletes the branch
git wt prune                  # remove clean worktrees whose branches are merged into HEAD
git wt init                   # scaffold .worktree.toml + append agent guidance to CLAUDE.md;
                              # prompts for worktree placement when run at a terminal
git wt init --placement=inside   # skip the prompt (sibling|inside|home|<template>)
git wt init --hook            # …plus a Claude Code hook blocking raw 'git worktree add'
```

## Configuration — `.worktree.toml`

### Placement — where worktrees live

```toml
worktrees = "sibling"   # preset name or path template
```

| Value | Worktree path |
|---|---|
| `"sibling"` (default) | `../<repo>.worktrees/<branch>` next to the main checkout |
| `"inside"` | `.worktrees/<branch>` inside the repo (auto-added to `.git/info/exclude`) |
| `"home"` | `~/.worktrees/<repo>/<branch>` |
| custom template | e.g. `"~/wt/{repo}/{branch}"` — `{repo}`/`{branch}` substituted, `~` expanded, relative paths resolve against the main checkout; `{branch}` is appended if missing |

`git wt init` asks which placement to use (or take `--placement=<value>`). Worktrees created under a previous placement stay valid git worktrees but drop out of `ls`/`rm`/`prune` management. Placements on a different volume than the repo lose APFS copy-on-write: provisioning falls back to a full copy.

### Provisioning classification

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
