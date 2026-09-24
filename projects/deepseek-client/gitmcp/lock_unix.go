//go:build !windows

package gitmcp

import (
	"golang.org/x/sys/unix"
	"os"
)

func lockSchedulerFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
