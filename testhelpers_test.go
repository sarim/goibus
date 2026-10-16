package goibus

import (
	"bufio"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// testBus is a private D-Bus daemon used as a stand-in for ibus-daemon.
type testBus struct {
	Address string
	PID     int
	cmd     *exec.Cmd
	dir     string
	once    sync.Once
}

// startTestBus launches a private session bus for the duration of the test.
func startTestBus(t *testing.T) *testBus {
	t.Helper()
	return startTestBusAt(t, "")
}

// startTestBusAt is startTestBus with an optional explicit socket path, so the
// address is known before the daemon is running.
func startTestBusAt(t *testing.T, socketPath string) *testBus {
	t.Helper()
	bus := spawnTestBusAt(t, socketPath)
	t.Cleanup(bus.Stop)
	return bus
}

// spawnTestBusAt starts a bus without registering cleanup. Tests that start the
// daemon from another goroutine use this and hand the bus back to the test
// goroutine, so that Stop is never called concurrently with cleanup.
func spawnTestBusAt(t *testing.T, socketPath string) *testBus {
	t.Helper()

	dir, err := ioutil.TempDir("", "goibus-test-bus")
	if err != nil {
		t.Fatal(err)
	}
	// --nofork keeps the daemon as a child we can reap, --print-address tells us
	// the socket before it blocks.
	args := []string{"--session", "--nofork", "--print-address"}
	if socketPath != "" {
		args = append(args, "--address=unix:path="+socketPath)
	}
	cmd := exec.Command("dbus-daemon", args...)
	cmd.Env = append(os.Environ(), "DBUS_VERBOSE=0")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		os.RemoveAll(dir)
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		t.Skipf("cannot start dbus-daemon: %v", err)
	}

	bus := &testBus{cmd: cmd, dir: dir, PID: cmd.Process.Pid}

	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		ch <- result{strings.TrimSpace(line), err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			bus.Stop()
			t.Skipf("cannot read address from dbus-daemon: %v", r.err)
		}
		bus.Address = r.line
	case <-time.After(10 * time.Second):
		bus.Stop()
		t.Skip("dbus-daemon did not print an address")
	}
	if bus.Address == "" {
		bus.Stop()
		t.Skip("dbus-daemon printed an empty address")
	}
	return bus
}

// Stop kills the daemon and removes its scratch directory. It is safe to call
// more than once and from either a test goroutine or its cleanup.
func (b *testBus) Stop() {
	b.once.Do(func() {
		if b.cmd.Process != nil {
			b.cmd.Process.Kill()
			b.cmd.Wait()
		}
		os.RemoveAll(b.dir)
	})
}

// writeAddressFile writes an ibus address file and returns its path. A
// daemonPID of 0 omits the IBUS_DAEMON_PID line.
func writeAddressFile(t *testing.T, address string, daemonPID int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dbus-socket")
	var body strings.Builder
	body.WriteString("# This file is created by ibus-daemon, please do not modify it.\n")
	body.WriteString("# It allows processes on the machine to find the ibus session bus.\n")
	body.WriteString("IBUS_ADDRESS=" + address + "\n")
	if daemonPID != 0 {
		body.WriteString("IBUS_DAEMON_PID=" + strconv.Itoa(daemonPID) + "\n")
	}
	writeFile(t, path, body.String())
	return path
}

// writeAddressFileContent writes raw content to path. Unlike t.Fatal, the error
// path is safe to reach from another goroutine.
func writeAddressFileContent(t *testing.T, path, content string) {
	t.Helper()
	writeFile(t, path, content)
}

func writeFile(t *testing.T, path, content string) {
	if err := ioutil.WriteFile(path, []byte(content), 0600); err != nil {
		t.Errorf("cannot write %s: %v", path, err)
	}
}

// deadPID returns a pid that is known not to be running.
func deadPID(t *testing.T) int {
	t.Helper()
	// Run a trivial process and reap it, so the pid is as good as free.
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn a helper process: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

// livePID returns a pid that is known to be running.
func livePID(t *testing.T) int {
	t.Helper()
	// A started-but-unreaped child is still a live pid.
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn a helper process: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	return cmd.Process.Pid
}

// deadSocketPath returns a path that looks like a socket file but has nothing
// listening on it.
func deadSocketPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dbus-stale")
	writeFile(t, path, "")
	return path
}

// startRudePeer listens on a unix socket and drops every connection without
// speaking D-Bus. Dialling it succeeds at the transport level, so the failure
// only shows up during Auth or Hello, which is where a leaked connection would
// otherwise be created.
func startRudePeer(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dbus-rude")

	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("cannot listen on a unix socket: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	return path
}

// setEnv sets an environment variable for the duration of the test.
func setEnv(t *testing.T, key, value string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			os.Setenv(key, old)
		} else {
			os.Unsetenv(key)
		}
	})
}

// countOpenFDs reports how many file descriptors this process holds.
func countOpenFDs(t *testing.T) int {
	t.Helper()
	entries, err := ioutil.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("cannot count open file descriptors: %v", err)
	}
	return len(entries)
}
