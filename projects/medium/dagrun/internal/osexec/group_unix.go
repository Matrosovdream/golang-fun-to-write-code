//go:build unix

package osexec

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// ownGroup puts the child in its own process group, so one signal to -pgid
// reaches the shell and everything it spawned. It also means a terminal Ctrl+C
// goes to dagrun alone, which then shuts the children down in an orderly way.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}

func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
