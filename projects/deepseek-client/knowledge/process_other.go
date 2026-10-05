//go:build !windows

package knowledge

import "os/exec"

func hideProcess(cmd *exec.Cmd) {}
