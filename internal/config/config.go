// Package config loads the worktree provisioning classification: which
// ignored paths are cloned, shared, or skipped, and what setup command runs
// after provisioning. Built-in defaults are overridden per path by a
// .worktree.toml committed at the repo root.
package config

import (
	"errors"
	"os"
	"path/filepath"

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
	// Setup is the command run (via sh -c, in the new worktree) after
	// provisioning. Empty means auto-detect a package-manager install.
	Setup string
}

// FileName is the repo-root config file read by Load.
const FileName = ".worktree.toml"

// Defaults ship with the tool; conservative direction is clone (forked state
// is annoying, corrupted main-checkout state is dangerous).
var Defaults = []Rule{
	{Pattern: "node_modules", Action: Clone},
	{Pattern: ".turbo", Action: Clone},
	{Pattern: ".next/cache", Action: Clone},
	{Pattern: ".env", Action: Share},
	{Pattern: ".env.*", Action: Share},
	{Pattern: "dist", Action: Skip},
	{Pattern: "build", Action: Skip},
	{Pattern: "out", Action: Skip},
}

type tomlFile struct {
	Setup string            `toml:"setup"`
	Clone []string          `toml:"clone"`
	Share []string          `toml:"share"`
	Skip  []string          `toml:"skip"`
	Paths map[string]string `toml:"paths"` // legacy/explicit form: pattern -> action
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
	for _, p := range f.Clone {
		cfg.Rules = append(cfg.Rules, Rule{Pattern: p, Action: Clone})
	}
	for _, p := range f.Share {
		cfg.Rules = append(cfg.Rules, Rule{Pattern: p, Action: Share})
	}
	for _, p := range f.Skip {
		cfg.Rules = append(cfg.Rules, Rule{Pattern: p, Action: Skip})
	}
	for p, a := range f.Paths {
		cfg.Rules = append(cfg.Rules, Rule{Pattern: p, Action: Action(a)})
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
