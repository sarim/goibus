package goibus

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestDialLiveBus(t *testing.T) {
	bus := startTestBus(t)

	conn, err := Dial(bus.Address)
	if err != nil {
		t.Fatalf("Dial(%s): %v", bus.Address, err)
	}
	defer conn.Close()

	if conn.GetDbusConn() == nil {
		t.Fatal("GetDbusConn() returned nil")
	}
	// A completed Hello() means the connection owns a unique bus name, so the
	// bus must answer a round trip.
	var id string
	if err := conn.GetDbusConn().Object(BUS_DAEMON_NAME, dbus.ObjectPath(BUS_DAEMON_PATH)).
		Call("org.freedesktop.DBus.GetId", 0).Store(&id); err != nil {
		t.Fatalf("GetId on a freshly dialled bus: %v", err)
	}
	if id == "" {
		t.Error("the bus returned an empty id")
	}
}

// A stale socket must produce an error naming the address, not a panic.
func TestDialDeadSocketReturnsError(t *testing.T) {
	stale := deadSocketPath(t)

	conn, err := Dial("unix:path=" + stale)
	if err == nil {
		conn.Close()
		t.Fatalf("Dial() to a dead socket succeeded, want an error")
	}
	if !strings.Contains(err.Error(), stale) {
		t.Errorf("error %q should mention the address %q", err, stale)
	}
}

func TestDialRejectsMalformedAddress(t *testing.T) {
	for _, address := range []string{"", "nonsense", "unix:path=/nonexistent/ibus/socket", "tcp:host=127.0.0.1,port=1"} {
		conn, err := Dial(address)
		if err == nil {
			conn.Close()
			t.Errorf("Dial(%q) succeeded, want an error", address)
		}
	}
}

func TestNewBusEUsesAddressFile(t *testing.T) {
	bus := startTestBus(t)
	setEnv(t, "IBUS_ADDRESS", "")
	setEnv(t, "IBUS_ADDRESS_FILE", writeAddressFile(t, bus.Address, bus.PID))

	conn, err := NewBusE()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
}

// Issue #300: the engine resolved the address of a daemon that no longer exists
// and died with a panic from inside NewBus.
func TestNewBusERejectsStaleDaemon(t *testing.T) {
	stale := deadSocketPath(t)
	setEnv(t, "IBUS_ADDRESS", "")
	setEnv(t, "IBUS_ADDRESS_FILE", writeAddressFile(t, "unix:path="+stale, deadPID(t)))

	conn, err := NewBusE()
	if err == nil {
		conn.Close()
		t.Fatal("NewBusE() succeeded against a stale address file")
	}
	if !errors.Is(err, ErrDaemonNotRunning) {
		t.Errorf("error = %v, want ErrDaemonNotRunning", err)
	}
}

// The deprecated constructor keeps its historical behaviour so existing callers
// do not change semantics on upgrade.
func TestNewBusPanicsOnFailure(t *testing.T) {
	stale := deadSocketPath(t)
	setEnv(t, "IBUS_ADDRESS", "")
	setEnv(t, "IBUS_ADDRESS_FILE", writeAddressFile(t, "unix:path="+stale, deadPID(t)))

	defer func() {
		if recover() == nil {
			t.Error("NewBus() should panic, as it always has")
		}
	}()
	NewBus()
}

// A failed Dial used to leave the connection open, so a component that retried
// in a loop leaked a socket and a goroutine on every attempt. The peer here
// accepts and hangs up without speaking D-Bus, so the transport connects
// successfully and the failure lands in Auth or Hello, where the leaked
// connection used to be created.
func TestDialDoesNotLeakConnections(t *testing.T) {
	address := "unix:path=" + startRudePeer(t)

	// Warm up so one-off allocations do not show up as a delta.
	for i := 0; i < 3; i++ {
		if _, err := Dial(address); err == nil {
			t.Fatal("Dial() to a dead socket succeeded")
		}
	}

	before := countOpenFDs(t)
	const attempts = 20
	for i := 0; i < attempts; i++ {
		if _, err := Dial(address); err == nil {
			t.Fatal("Dial() to a dead socket succeeded")
		}
	}
	after := countOpenFDs(t)

	if after > before+2 {
		t.Errorf("open file descriptors grew from %d to %d over %d failed dials",
			before, after, attempts)
	}
}

// Done must fire when the daemon disappears, so components can react instead of
// blocking forever on a dead connection.
func TestDoneFiresOnDisconnect(t *testing.T) {
	bus := startTestBus(t)

	conn, err := Dial(bus.Address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	select {
	case <-conn.Done():
		t.Fatal("Done() fired while the connection was healthy")
	case <-time.After(100 * time.Millisecond):
	}

	bus.Stop()

	select {
	case <-conn.Done():
	case <-time.After(10 * time.Second):
		t.Error("Done() did not fire after the daemon exited")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	bus := startTestBus(t)
	conn, err := Dial(bus.Address)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Errorf("first Close(): %v", err)
	}
	// godbus uses sync.Once, so a second Close must not panic.
	if err := conn.Close(); err != nil {
		t.Errorf("second Close(): %v", err)
	}
}

func TestRequestNameChecked(t *testing.T) {
	bus := startTestBus(t)

	first, err := Dial(bus.Address)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	if err := first.RequestNameChecked("org.example.GoibusTest", 0); err != nil {
		t.Fatalf("acquiring a free name failed: %v", err)
	}
	// Asking again from the same connection is still the primary owner.
	if err := first.RequestNameChecked("org.example.GoibusTest", 0); err != nil {
		t.Errorf("re-acquiring our own name failed: %v", err)
	}

	second, err := Dial(bus.Address)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	// Without the reply check a component would believe it registered while
	// ibus can only reach the other owner.
	if err := second.RequestNameChecked("org.example.GoibusTest", 0); err == nil {
		t.Error("RequestNameChecked() succeeded for a name owned by another connection")
	}
}

func TestCallMethodReachesTheBus(t *testing.T) {
	bus := startTestBus(t)

	conn, err := Dial(bus.Address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// ListNames is on the bus itself, not on IBus, so reach it through the raw
	// connection rather than the ibus object.
	var names []string
	if err := conn.GetDbusConn().Object(BUS_DAEMON_NAME, dbus.ObjectPath(BUS_DAEMON_PATH)).
		Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		t.Fatalf("ListNames: %v", err)
	}
	if len(names) == 0 {
		t.Error("the bus reported no names")
	}
}

// The ibus object only makes sense on a real ibus daemon, but calling it on a
// plain dbus-daemon must fail with a D-Bus error rather than panic.
func TestCallMethodOnNonIbusDaemon(t *testing.T) {
	bus := startTestBus(t)

	conn, err := Dial(bus.Address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	call := conn.CallMethod("SetGlobalEngine", 0, "Bamboo")
	if call == nil {
		t.Fatal("CallMethod returned no call")
	}
	if call.Err == nil {
		t.Error("SetGlobalEngine on a plain dbus-daemon should fail")
	}
}
