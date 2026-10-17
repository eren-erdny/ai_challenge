//go:build !windows

package main

func enableHW29Console() func() { return func() {} }
