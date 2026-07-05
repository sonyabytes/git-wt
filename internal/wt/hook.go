package wt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// hookCommand blocks raw `git worktree add` in Claude Code Bash calls.
// PreToolUse hooks receive the tool input as JSON on stdin; exit code 2
// blocks the call and shows stderr to the agent.
const hookCommand = `if grep -qE 'git[[:space:]]+worktree[[:space:]]+add' <<< "$(cat)"; then echo 'Use "git wt new <branch>" instead of raw "git worktree add" — it provisions node_modules/.env and places the worktree by convention.' >&2; exit 2; fi`

// InstallHook merges a PreToolUse hook into .claude/settings.json in the
// main checkout, creating the file if needed. Idempotent.
func (r *Repo) InstallHook(logf func(string, ...any)) error {
	dir := filepath.Join(r.MainRoot, ".claude")
	path := filepath.Join(dir, "settings.json")

	settings := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("existing %s is not valid JSON: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if raw, _ := json.Marshal(settings); containsHook(raw) {
		logf("hook already installed in .claude/settings.json")
		return nil
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	pre, _ := hooks["PreToolUse"].([]any)
	pre = append(pre, map[string]any{
		"matcher": "Bash",
		"hooks": []any{
			map[string]any{"type": "command", "command": hookCommand},
		},
	})
	hooks["PreToolUse"] = pre
	settings["hooks"] = hooks

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Unreachable: settings came from json.Unmarshal, so it marshals.
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil { // coverage-ignore
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	logf("installed git-worktree-add guard hook in .claude/settings.json")
	return nil
}

func containsHook(settingsJSON []byte) bool {
	var probe struct {
		Hooks struct {
			PreToolUse []struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(settingsJSON, &probe); err != nil {
		return false
	}
	for _, m := range probe.Hooks.PreToolUse {
		for _, h := range m.Hooks {
			if h.Command == hookCommand {
				return true
			}
		}
	}
	return false
}
