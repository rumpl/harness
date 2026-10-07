//go:build !unix

package harness

import "os/exec"

func configureProcess(_ *exec.Cmd) {}
