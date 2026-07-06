// Package config loads the worktree provisioning classification: which
// ignored paths are cloned, shared, or skipped, and what setup command runs
// after provisioning. Built-in defaults are overridden per path by a
// .worktree.toml committed at the repo root.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

// Action says how a matched path is provisioned into a new worktree.
type Action string

const (
	// Clone copies via filesystem copy-on-write; writes in the worktree
	// never affect the main checkout.
	Clone Action = "clone"
	// Share symlinks back to the main checkout; writes go through.
	Share Action = "share"
	// Skip leaves the path unprovisioned.
	Skip Action = "skip"
)

// Rule maps a path glob (relative to the repo root, matched against each
// candidate path with filepath.Match) to an Action.
type Rule struct {
	Pattern string
	Action  Action
}

// Config is the resolved classification for one repository.
type Config struct {
	// Rules are checked in order; first match wins. Repo rules precede defaults.
	Rules []Rule
	// Setup is the command run (via the platform shell, in the new worktree)
	// after provisioning. Empty means auto-detect a package-manager install.
	Setup string
	// Worktrees is the raw placement value: a preset name ("sibling",
	// "inside", "home") or a path template with {repo}/{branch}
	// placeholders. Empty means sibling. Resolution lives in the wt package.
	Worktrees string
}

// FileName is the repo-root config file read by Load.
const FileName = ".worktree.toml"

// Toolchain describes one supported ecosystem in a single place: the
// lockfiles that identify it, the install command it needs, and the ignored
// paths it generates and how they are provisioned into a worktree.
type Toolchain struct {
	// Name identifies the toolchain in logs and docs.
	Name string
	// Lockfiles detect the toolchain; any one present in the worktree counts.
	Lockfiles []string
	// Setup is the install command run after provisioning when a lockfile is
	// present and no explicit setup is configured. Empty means the toolchain
	// contributes provisioning rules but no setup step.
	Setup string
	// Rules classify the ignored paths this toolchain produces.
	Rules []Rule
}

var (
	nodeRules = []Rule{
		{Pattern: "node_modules", Action: Clone},
		{Pattern: ".turbo", Action: Clone},
		{Pattern: ".next/cache", Action: Clone},
	}
	pythonRules = []Rule{{Pattern: ".venv", Action: Clone}}
	vendorRules = []Rule{{Pattern: "vendor", Action: Clone}}
)

// Toolchains is ordered: DetectSetup returns the first entry with a present
// lockfile and a non-empty Setup.
var Toolchains = []Toolchain{
	{Name: "bun", Lockfiles: []string{"bun.lock", "bun.lockb"}, Setup: "bun install", Rules: nodeRules},
	{Name: "pnpm", Lockfiles: []string{"pnpm-lock.yaml"}, Setup: "pnpm install", Rules: nodeRules},
	{Name: "yarn", Lockfiles: []string{"yarn.lock"}, Setup: "yarn install", Rules: nodeRules},
	{Name: "npm", Lockfiles: []string{"package-lock.json"}, Setup: "npm install", Rules: nodeRules},
	{Name: "uv", Lockfiles: []string{"uv.lock"}, Setup: "uv sync", Rules: pythonRules},
	{Name: "poetry", Lockfiles: []string{"poetry.lock"}, Setup: "poetry install", Rules: pythonRules},
	{Name: "cargo", Lockfiles: []string{"Cargo.lock"}, Rules: []Rule{{Pattern: "target", Action: Clone}}},
	{Name: "go", Lockfiles: []string{"go.sum"}, Rules: vendorRules},
	{Name: "bundler", Lockfiles: []string{"Gemfile.lock"}, Setup: "bundle install", Rules: vendorRules},
	{Name: "composer", Lockfiles: []string{"composer.lock"}, Setup: "composer install", Rules: vendorRules},
}

// genericRules apply regardless of toolchain: secrets are shared with the
// main checkout, build output is left for the worktree to regenerate.
var genericRules = []Rule{
	{Pattern: ".env", Action: Share},
	{Pattern: ".env.*", Action: Share},
	{Pattern: "dist", Action: Skip},
	{Pattern: "build", Action: Skip},
	{Pattern: "out", Action: Skip},
}

// Defaults ship with the tool: every toolchain's rules (deduplicated by
// pattern, in Toolchains order) followed by genericRules. Conservative
// direction is clone (forked state is annoying, corrupted main-checkout
// state is dangerous). Tracked paths that happen to share these names (a
// committed vendor/ or target/) are safe: provisioning never touches paths
// already present in the worktree.
var Defaults = defaultRules()

func defaultRules() []Rule {
	seen := map[string]bool{}
	var rules []Rule
	for _, tc := range Toolchains {
		for _, r := range tc.Rules {
			if seen[r.Pattern] {
				continue
			}
			seen[r.Pattern] = true
			rules = append(rules, r)
		}
	}
	return append(rules, genericRules...)
}

// DetectSetup returns the setup command of the first toolchain with a
// lockfile present in dir, or "" when none is detected.
func DetectSetup(dir string) string {
	for _, tc := range Toolchains {
		if tc.Setup == "" {
			continue
		}
		for _, lf := range tc.Lockfiles {
			if _, err := os.Stat(filepath.Join(dir, lf)); err == nil {
				return tc.Setup
			}
		}
	}
	return ""
}

type tomlFile struct {
	Setup     string            `toml:"setup"`
	Worktrees string            `toml:"worktrees"`
	Clone     []string          `toml:"clone"`
	Share     []string          `toml:"share"`
	Skip      []string          `toml:"skip"`
	Paths     map[string]string `toml:"paths"` // legacy/explicit form: pattern -> action
}

// Load reads repoRoot/.worktree.toml if present and layers it over Defaults.
// A missing file is not an error: defaults apply.
func Load(repoRoot string) (*Config, error) {
	cfg := &Config{}
	path := filepath.Join(repoRoot, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		cfg.Rules = append(cfg.Rules, Defaults...)
		return cfg, nil
	}

	var f tomlFile
	if _, err := toml.Decode(string(data), &f); err != nil {
		return nil, err
	}
	cfg.Setup = f.Setup
	cfg.Worktrees = f.Worktrees
	for _, p := range f.Clone {
		cfg.Rules = append(cfg.Rules, Rule{Pattern: p, Action: Clone})
	}
	for _, p := range f.Share {
		cfg.Rules = append(cfg.Rules, Rule{Pattern: p, Action: Share})
	}
	for _, p := range f.Skip {
		cfg.Rules = append(cfg.Rules, Rule{Pattern: p, Action: Skip})
	}
	// Sorted so first-match-wins stays deterministic across runs — Go map
	// iteration order would otherwise reshuffle overlapping patterns.
	patterns := make([]string, 0, len(f.Paths))
	for p := range f.Paths {
		patterns = append(patterns, p)
	}
	sort.Strings(patterns)
	for _, p := range patterns {
		a := Action(f.Paths[p])
		if a != Clone && a != Share && a != Skip {
			return nil, fmt.Errorf("%s: invalid action %q for pattern %q (want %q, %q, or %q)", FileName, f.Paths[p], p, Clone, Share, Skip)
		}
		cfg.Rules = append(cfg.Rules, Rule{Pattern: p, Action: a})
	}
	cfg.Rules = append(cfg.Rules, Defaults...)
	return cfg, nil
}

// Classify returns the Action for a path relative to the repo root, or Skip
// with ok=false when no rule matches (unmatched ignored paths are left alone).
func (c *Config) Classify(rel string) (Action, bool) {
	for _, r := range c.Rules {
		if ok, _ := filepath.Match(r.Pattern, rel); ok {
			return r.Action, true
		}
	}
	return Skip, false
}
