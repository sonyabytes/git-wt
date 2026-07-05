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

## Real-world measurements

Measured on macOS/APFS (Apple Silicon) by creating a fresh project per ecosystem, installing dependencies, then running `git wt new` against it. **Apparent size** is what `du` reports for the new worktree (CoW clones look like full copies); **physical cost** is the actual drop in volume free space (`df`), i.e. what a plain copy would have cost you but didn't. Physical numbers include the source checkout and setup-command writes, ±a few MB of measurement noise.

| Ecosystem | Cloned state | `git wt new` time | Apparent size | Physical cost | Worktree verified by |
|---|---|---:|---:|---:|---|
| bun monorepo (real app) | `node_modules` 842 MB | 1.2 s | 1.1 GB | ~19 MB | `bun install` no-op (70 ms) |
| npm 10.9 | `node_modules` 86 MB | 0.6 s | 87 MB | ~6 MB | `require()` + `.bin/tsc` run |
| yarn 1.22 | `node_modules` 91 MB | 0.8 s | 91 MB | ~1 MB | `yarn install` → "Already up-to-date" |
| cargo 1.93 (Rust) | `target` 138 MB | 0.05 s | 138 MB | ~1 MB | `cargo build` no-op rebuild in **0.12 s** |
| go 1.26 (vendored) | `vendor` 41 MB | 0.09 s | 41 MB | ~0 MB | `go build ./...` |
| bundler 4.0 (Ruby) | `vendor` + `.bundle` 45 MB | 0.1 s | 45 MB | ~3 MB | `bundle check` satisfied |
| python 3.14 (venv) | `.venv` 126 MB | 0.13 s | 126 MB | ~1 MB | `import numpy, pandas`; `sys.prefix` points at worktree |

Non-default ecosystems used a two-line `.worktree.toml` (`clone = ["target"]`, `clone = ["vendor"]`, `clone = [".venv"]`, …). A stock `git worktree add` on the same repos produced a 20–200 KB checkout with none of this state.

Caveats found during testing:

- **Python entry-point scripts** (`.venv/bin/pip` etc.) keep shebangs pointing at the *main checkout's* venv, so `pip install` run in a worktree mutates the main venv. Use `python -m pip` inside the worktree, or rebuild the venv. Direct `python` invocation and imports resolve correctly to the worktree.
- **Monorepo workspaces**: `clone = ["node_modules"]` matches only the repo root; nested workspace dirs (`apps/*/node_modules`, `packages/*/node_modules`) need their own glob entries until nested matching lands. Cloned root state can also make the package manager's install report "no changes" without materializing the nested dirs.
- **Setup failure leaves the worktree in place**: if the setup command fails (e.g. auto-detected `yarn install` with no yarn on PATH), `git wt new` exits non-zero but the provisioned worktree and branch survive — fix the setup (e.g. `setup = "npx -y yarn@1.22 install"`) and re-run it manually, or `git wt rm` and retry.

## Notes

- macOS/APFS-first. On filesystems without CoW cloning, `clone` degrades to a plain copy (correct, just slower). Linux reflink support is a planned drop-in.
- `prune` never touches a worktree whose branch ref was deleted underneath it — that state makes every file look uncommitted, so it's reported and left for `git wt rm --force`.
- Verification: `go test ./...` plus `scripts/e2e.sh` (full scenario suite incl. CoW disk-usage proof).
