package wt

import "os/exec"

// shellCommand wraps a setup command line for the platform shell — cmd.exe;
// there is no sh on stock Windows.
func shellCommand(cmdline string) *exec.Cmd {
	return exec.Command("cmd", "/c", cmdline)
}
