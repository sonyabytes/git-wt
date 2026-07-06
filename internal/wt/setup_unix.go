//go:build !windows

package wt

import "os/exec"

// shellCommand wraps a setup command line for the platform shell.
func shellCommand(cmdline string) *exec.Cmd {
	return exec.Command("sh", "-c", cmdline)
}
