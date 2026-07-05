#!/bin/bash
# End-to-end test for git-wt against a scratch repo.
set -euo pipefail

SCRATCH="$(mktemp -d "${TMPDIR:-/tmp}/gitwt-e2e.XXXXXX")"
SCRATCH="$(cd "$SCRATCH" && pwd -P)"   # resolve /tmp -> /private/tmp so path comparisons match git's view
trap 'rm -rf "$SCRATCH"' EXIT
PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); echo "  ok: $1"; }
fail() { FAIL=$((FAIL+1)); echo "FAIL: $1"; }
assert() { if eval "$2"; then ok "$1"; else fail "$1"; fi; }

# Build and put git-wt on PATH so `git wt` resolves.
go build -o "$SCRATCH/bin/git-wt" ./cmd/git-wt
export PATH="$SCRATCH/bin:$PATH"

# --- scratch main checkout ---------------------------------------------
REPO="$SCRATCH/myapp"
mkdir -p "$REPO" && cd "$REPO"
git init -q -b main
git config user.email t@t && git config user.name t
echo '{"name":"myapp"}' > package.json
printf 'node_modules/\n.env\ndist/\n.setup-ran\n' > .gitignore
cat > .worktree.toml <<'EOF'
setup = "touch .setup-ran"
EOF
echo 'src' > main.go
git add -A && git commit -qm init
# ignored state
mkdir -p node_modules/pkg dist
echo 'module.exports = 1' > node_modules/pkg/index.js
dd if=/dev/zero of=node_modules/pkg/big.bin bs=1024k count=100 2>/dev/null
echo 'SECRET=hunter2' > .env
echo 'junk' > dist/out.js

echo "== git wt new =="
AVAIL_BEFORE=$(df -k . | awk 'NR==2{print $4}')
WT_PATH=$(git wt new feature/x --porcelain)
AVAIL_AFTER=$(df -k . | awk 'NR==2{print $4}')

assert "worktree at sibling convention path" '[ "$WT_PATH" = "$SCRATCH/myapp.worktrees/feature-x" ]'
assert "worktree directory exists" '[ -d "$WT_PATH" ]'
assert "branch created" 'git rev-parse --verify -q refs/heads/feature/x >/dev/null'
assert "node_modules cloned with content" '[ "$(cat "$WT_PATH/node_modules/pkg/index.js")" = "module.exports = 1" ]'
INODE_MAIN=$(ls -i node_modules/pkg/index.js | awk '{print $1}')
INODE_WT=$(ls -i "$WT_PATH/node_modules/pkg/index.js" | awk '{print $1}')
assert "clone has distinct inode (not hardlink)" '[ "$INODE_MAIN" != "$INODE_WT" ]'
assert ".env is a symlink to main checkout" '[ "$(readlink "$WT_PATH/.env")" = "$REPO/.env" ]'
assert "dist skipped" '[ ! -e "$WT_PATH/dist" ]'
assert "setup command ran" '[ -f "$WT_PATH/.setup-ran" ]'
DELTA_KB=$(( AVAIL_BEFORE - AVAIL_AFTER ))
if [ "$(uname)" = "Darwin" ]; then
  # Only APFS guarantees CoW; elsewhere Tree falls back to a real copy.
  assert "CoW engaged: <20MB disk used for 100MB clone (delta ${DELTA_KB}KB)" '[ "$DELTA_KB" -lt 20480 ]'
else
  echo "  (CoW disk assertion skipped on $(uname): plain-copy fallback expected, delta ${DELTA_KB}KB)"
fi

echo "== CoW isolation =="
echo 'mutated' > "$WT_PATH/node_modules/pkg/index.js"
assert "write in worktree does not touch main checkout" '[ "$(cat node_modules/pkg/index.js)" = "module.exports = 1" ]'
echo 'SHARED=yes' >> "$WT_PATH/.env"
assert "write to shared .env goes through to main" 'grep -q SHARED .env'

echo "== ls =="
assert "ls shows managed worktree with branch" 'git wt ls | grep -q "feature-x	feature/x"'

echo "== rm safety =="
echo 'dirty' >> "$WT_PATH/main.go"
assert "rm refuses dirty worktree" '! git wt rm feature/x 2>/dev/null'
git wt rm feature/x --force
assert "rm --force removes dirty worktree" '[ ! -d "$WT_PATH" ]'
assert "rm never deletes the branch" 'git rev-parse --verify -q refs/heads/feature/x >/dev/null'

echo "== rm clean (merged, no upstream) =="
WT2=$(git wt new feature/y --porcelain)
git wt rm feature/y
assert "rm accepts clean merged worktree" '[ ! -d "$WT2" ]'

echo "== rm refuses unmerged commits =="
WT3=$(git wt new feature/z --porcelain)
(cd "$WT3" && echo change >> main.go && git commit -qam "wip")
assert "rm refuses branch with unmerged commits" '! git wt rm feature/z 2>/dev/null'
git wt rm feature/z --force

echo "== prune =="
WT4=$(git wt new feature/done --porcelain)
(cd "$WT4" && echo done >> main.go && git commit -qam "done")
git merge -q feature/done
WT5=$(git wt new feature/wip --porcelain)
(cd "$WT5" && echo wip >> main.go && git commit -qam "wip")
git wt prune 2>/dev/null
assert "prune removes merged-branch worktree" '[ ! -d "$WT4" ]'
assert "prune keeps unmerged-branch worktree" '[ -d "$WT5" ]'
git wt rm feature/wip --force

echo "== init =="
git wt init --hook </dev/null 2>/dev/null
assert "init leaves existing .worktree.toml alone" 'grep -q setup-ran .worktree.toml'
assert "init appends CLAUDE.md guidance" 'grep -q "git wt new" CLAUDE.md'
assert "init --hook writes settings.json with guard" 'grep -q "git wt new" .claude/settings.json'
git wt init --hook </dev/null 2>/dev/null
assert "init is idempotent (no duplicate hook)" '[ "$(grep -c "worktree" .claude/settings.json)" -le 2 ]'

echo "== placement: inside =="
REPO2="$SCRATCH/insideapp"
mkdir -p "$REPO2" && cd "$REPO2"
git init -q -b main
git config user.email t@t && git config user.name t
echo 'src' > main.go
git add -A && git commit -qm init
git wt init --placement=inside </dev/null 2>/dev/null
assert "init writes chosen placement" 'grep -q "worktrees = \"inside\"" .worktree.toml'
git add -A && git commit -qm "add worktree config"
WT_IN=$(git wt new feature/x --porcelain)
assert "worktree lands inside the repo" '[ "$WT_IN" = "$REPO2/.worktrees/feature-x" ]'
assert "container is git-ignored (clean status)" '[ -z "$(git status --porcelain)" ]'
assert "ls sees inside worktree" 'git wt ls | grep -q "feature-x	feature/x"'
git wt rm feature/x
assert "rm removes inside worktree" '[ ! -d "$WT_IN" ]'

echo "== placement: custom template =="
REPO3="$SCRATCH/customapp"
mkdir -p "$REPO3" && cd "$REPO3"
git init -q -b main
git config user.email t@t && git config user.name t
echo 'src' > main.go
printf 'worktrees = "%s/wt/{repo}/{branch}"\n' "$SCRATCH" > .worktree.toml
git add -A && git commit -qm init
WT_TPL=$(git wt new feature/y --porcelain)
assert "custom template renders repo and branch" '[ "$WT_TPL" = "$SCRATCH/wt/customapp/feature-y" ]'
assert "ls sees template worktree" 'git wt ls | grep -q "feature-y	feature/y"'
git wt rm feature/y
assert "rm removes template worktree" '[ ! -d "$WT_TPL" ]'

echo "== placement: rejected values =="
REPO4="$SCRATCH/badapp"
mkdir -p "$REPO4" && cd "$REPO4"
git init -q -b main
git config user.email t@t && git config user.name t
echo 'src' > main.go && git add -A && git commit -qm init
assert "init rejects placement inside .git" '! git wt init --placement=".git/wt" </dev/null 2>/dev/null'
printf 'worktrees = "."\n' > .worktree.toml
assert "commands fail fast on placement = main checkout" '! git wt ls 2>/dev/null'

echo
echo "passed $PASS, failed $FAIL"
exit $((FAIL > 0))
