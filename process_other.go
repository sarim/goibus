//go:build windows || plan9 || js
// +build windows plan9 js

package goibus

// ProcessAlive cannot be implemented on this platform, so it reports that the
// process is alive. That keeps address files without a recorded pid, and files
// that cannot be validated, working as before.
func ProcessAlive(pid int) bool {
	return pid > 0
}
