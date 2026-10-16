package goibus

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestWaitForAddressImmediate(t *testing.T) {
	setEnv(t, "IBUS_ADDRESS", "unix:path=/run/ibus")

	got, err := WaitForAddress(context.Background(), time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got != "unix:path=/run/ibus" {
		t.Errorf("WaitForAddress() = %q, want unix:path=/run/ibus", got)
	}
}

func TestWaitForAddressRespectsContext(t *testing.T) {
	setEnv(t, "IBUS_ADDRESS", "")
	setEnv(t, "IBUS_ADDRESS_FILE", filepath.Join(t.TempDir(), "nothing-here"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := WaitForAddress(ctx, 10*time.Second)
	if err == nil {
		t.Fatal("WaitForAddress() should fail once the context is done")
	}
	if !errors.Is(err, ErrNoAddress) {
		t.Errorf("error = %v, want it to wrap ErrNoAddress", err)
	}
	if waited := time.Since(start); waited > time.Second {
		t.Errorf("WaitForAddress() kept polling for %v after cancellation", waited)
	}
}

// A component spawned by ibus-daemon can start before the daemon has written
// its address file. Waiting is what keeps it from dying on the first miss.
func TestWaitForAddressWaitsForFileToAppear(t *testing.T) {
	bus := startTestBus(t)
	path := filepath.Join(t.TempDir(), "dbus-socket")
	setEnv(t, "IBUS_ADDRESS", "")
	setEnv(t, "IBUS_ADDRESS_FILE", path)

	go func() {
		time.Sleep(300 * time.Millisecond)
		writeAddressFileContent(t, path, "IBUS_ADDRESS="+bus.Address+"\nIBUS_DAEMON_PID="+strconv.Itoa(bus.PID)+"\n")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	got, err := WaitForAddress(ctx, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForAddress(): %v", err)
	}
	if got != bus.Address {
		t.Errorf("WaitForAddress() = %q, want %q", got, bus.Address)
	}
}

// Restarting ibus-daemon rewrites the address file with a new socket and a new
// pid. A caller that rejected the stale file must start accepting it.
func TestWaitForAddressRecoversFromStaleFile(t *testing.T) {
	bus := startTestBus(t)
	path := writeAddressFile(t, "unix:path="+deadSocketPath(t), deadPID(t))
	setEnv(t, "IBUS_ADDRESS", "")
	setEnv(t, "IBUS_ADDRESS_FILE", path)

	go func() {
		time.Sleep(300 * time.Millisecond)
		writeAddressFileContent(t, path, "IBUS_ADDRESS="+bus.Address+"\nIBUS_DAEMON_PID="+strconv.Itoa(bus.PID)+"\n")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	got, err := WaitForAddress(ctx, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForAddress(): %v", err)
	}
	if got != bus.Address {
		t.Errorf("WaitForAddress() = %q, want %q", got, bus.Address)
	}
}

func TestDialWhenAvailable(t *testing.T) {
	bus := startTestBus(t)
	setEnv(t, "IBUS_ADDRESS", "")
	setEnv(t, "IBUS_ADDRESS_FILE", writeAddressFile(t, bus.Address, bus.PID))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := DialWhenAvailable(ctx, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := conn.RequestNameChecked("org.example.GoibusDial", 0); err != nil {
		t.Errorf("the dialled bus is not usable: %v", err)
	}
}

// The address must be resolved again on every attempt, so a daemon that appears
// later is picked up. This is the case where ibus-daemon has not written its
// address file yet while the component is already starting.
func TestDialWhenAvailableWaitsForDaemon(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "late-bus")
	addressFile := filepath.Join(t.TempDir(), "dbus-socket")
	setEnv(t, "IBUS_ADDRESS", "")
	setEnv(t, "IBUS_ADDRESS_FILE", addressFile)
	// The daemon is started from a goroutine so the caller gets to observe the
	// retries, but it is handed back so only the test goroutine ever stops it.
	ready := make(chan *testBus, 1)
	go func() {
		time.Sleep(500 * time.Millisecond)
		bus := spawnTestBusAt(t, socket)
		writeAddressFileContent(t, addressFile,
			"IBUS_ADDRESS="+bus.Address+"\nIBUS_DAEMON_PID="+strconv.Itoa(bus.PID)+"\n")
		ready <- bus
	}()

	var bus *testBus
	defer func() {
		select {
		case bus = <-ready:
		default:
			return
		}
		bus.Stop()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, err := DialWhenAvailable(ctx, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("DialWhenAvailable(): %v", err)
	}
	defer conn.Close()

	select {
	case bus = <-ready:
	default:
		t.Fatal("the daemon was never started")
	}
	if err := conn.RequestNameChecked("org.example.GoibusLate", 0); err != nil {
		t.Errorf("the dialled bus is not usable: %v", err)
	}
}

func TestDialWhenAvailableRespectsContext(t *testing.T) {
	setEnv(t, "IBUS_ADDRESS", "")
	setEnv(t, "IBUS_ADDRESS_FILE", filepath.Join(t.TempDir(), "nothing-here"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	conn, err := DialWhenAvailable(ctx, 10*time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("DialWhenAvailable() should fail once the context is done")
	}
	if conn != nil {
		t.Error("DialWhenAvailable() returned a bus alongside an error")
	}
	if !errors.Is(err, ErrNoAddress) {
		t.Errorf("error = %v, want it to wrap ErrNoAddress", err)
	}
}

// WatchAddress is the stand-in for the g_file_monitor_file() watch libibus
// installs, which is how a component learns the daemon came back.
func TestWatchAddressFiresOnRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dbus-socket")
	writeAddressFileContent(t, path, "IBUS_ADDRESS=unix:path=/one\nIBUS_DAEMON_PID=1\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	changed := make(chan struct{}, 8)
	go WatchAddress(ctx, path, 20*time.Millisecond, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})

	// Give the watcher a chance to record the initial state.
	time.Sleep(150 * time.Millisecond)
	select {
	case <-changed:
		t.Fatal("WatchAddress() fired without the file changing")
	default:
	}

	writeAddressFileContent(t, path, "IBUS_ADDRESS=unix:path=/two\nIBUS_DAEMON_PID=2\n")

	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("WatchAddress() did not fire when the file was rewritten")
	}
}

func TestWatchAddressFiresOnRemoval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dbus-socket")
	writeAddressFileContent(t, path, "IBUS_ADDRESS=unix:path=/one\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	changed := make(chan struct{}, 8)
	go WatchAddress(ctx, path, 20*time.Millisecond, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})

	time.Sleep(150 * time.Millisecond)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("WatchAddress() did not fire when the file disappeared")
	}
}

func TestWatchAddressStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		WatchAddress(ctx, filepath.Join(t.TempDir(), "nothing"), 10*time.Millisecond, func() {})
		close(stopped)
	}()

	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Error("WatchAddress() did not return after the context was cancelled")
	}
}

func TestAddressFileStampDetectsContentChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dbus-socket")
	if _, exists := addressFileStamp(path); exists {
		t.Error("a missing file must be reported as absent")
	}

	writeAddressFileContent(t, path, "IBUS_ADDRESS=unix:path=/one\n")
	first, exists := addressFileStamp(path)
	if !exists {
		t.Fatal("an existing file must be reported as present")
	}

	// Same length, different content: only a content check can tell them apart.
	writeAddressFileContent(t, path, "IBUS_ADDRESS=unix:path=/two\n")
	second, _ := addressFileStamp(path)
	if first == second {
		t.Error("addressFileStamp() did not notice a content change")
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, exists := addressFileStamp(path); exists {
		t.Error("a removed file must be reported as absent")
	}
}

func TestRetryUsesDefaultInterval(t *testing.T) {
	setEnv(t, "IBUS_ADDRESS", "")
	setEnv(t, "IBUS_ADDRESS_FILE", filepath.Join(t.TempDir(), "nothing-here"))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// A zero interval must fall back to DefaultRetryInterval rather than spin.
	start := time.Now()
	if _, err := WaitForAddress(ctx, 0); err == nil {
		t.Fatal("expected an error")
	}
	if waited := time.Since(start); waited > 5*time.Second {
		t.Errorf("WaitForAddress() ran for %v, want it bounded by the context", waited)
	}
}
