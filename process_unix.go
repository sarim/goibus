//go:build !windows && !plan9 && !js
// +build !windows,!plan9,!js

package goibus

import "syscall"

// ProcessAlive reports whether a process with the given pid exists.
//
// It uses signal 0, which performs the permission and existence checks without
// actually delivering a signal. EPERM means the process exists but belongs to
// another user, which still counts as alive.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return err == syscall.EPERM
}
