//go:build unix

package harness

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var once sync.Once
	var cancelErr error
	cmd.Cancel = func() error {
		once.Do(func() {
			// Kill tools as well as the shell/CLI, including inherited pipe owners.
			cancelErr = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			if errors.Is(cancelErr, syscall.ESRCH) {
				cancelErr = os.ErrProcessDone
			}
		})
		return cancelErr
	}
}
