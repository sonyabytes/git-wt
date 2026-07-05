# 0001 — CoW clones over symlinks for mutable state

Status: accepted (2026-07-05)

## Context

A fresh `git worktree` is born without ignored state: `node_modules`, `.env`, build caches. Agents then waste time reinstalling, or hit baffling errors. The original product intuition was "link files instead of copying, and only port them for real once the agent/human edits one."

We considered how to provision that state from the Main Checkout:

1. **Symlinks** — instant, zero disk, but writes go *through* the link. An agent running `bun add` in a worktree would silently mutate the Main Checkout's `node_modules`.
2. **Hardlinks** — same write-through hazard unless every tool does atomic replace-on-write, which cannot be guaranteed.
3. **Watcher/interposition layer** — symlink first, detect writes, materialize a copy on demand. Reimplements copy-on-write in userspace: complex, racy, fragile.
4. **Filesystem copy-on-write clones** — APFS `clonefile(2)` (Linux: reflink on btrfs/XFS). The clone is ~free in time and disk; the filesystem materializes a private copy at first write. This is option 3's semantics, implemented natively and race-free.

The complication: some files are singletons where write-through is *desired* — `.env`, a local dev database. Cloning those forks state that should stay shared.

## Decision

Hybrid, per-category (the Classification in `CONTEXT.md`):

- **Cloned State** (mutable per-worktree: `node_modules`, caches) → CoW clones via `clonefile`. Plain copy fallback where CoW is unsupported.
- **Shared State** (write-through singletons: `.env*`, local DBs) → symlinks.
- **Skipped State** (regenerable: `dist`) → not provisioned.

Defaults ship built-in; a committed `.worktree.toml` overrides per path.

Because a cloned `node_modules` may not match the branch's lockfile, `git wt new` always runs the Setup Command after provisioning — a no-op when in sync, the delta otherwise.

## Consequences

- macOS/APFS-first. Linux reflink can slot behind the same interface; ext4 degrades to plain copy (correct, just slower).
- Deleting a worktree is safe naively: `rm -rf` does not follow symlinks, so Shared State survives removal.
- Misclassification is the main operational risk (e.g. sharing something mutable). Mitigated by conservative defaults: when unsure, clone — forked state is annoying, corrupted Main Checkout state is dangerous.
