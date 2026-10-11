package main

import "golang.org/x/sys/windows"

// The recording painter manages VT output on its own console, then restores it.
func enableHW29Console() func() {
	h, e := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if e != nil {
		return func() {}
	}
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return func() {}
	}
	if windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) != nil {
		return func() {}
	}
	return func() { _ = windows.SetConsoleMode(h, mode) }
}
