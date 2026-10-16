package goibus

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAddressFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    AddressInfo
		wantErr bool
	}{
		{
			name: "the format ibus-daemon writes",
			content: "# This file is created by ibus-daemon, please do not modify it.\n" +
				"# It allows processes on the machine to find the ibus session bus.\n" +
				"# If the IBUS_ADDRESS environment variable is set, it will\n" +
				"# be used rather than this file.\n" +
				"IBUS_ADDRESS=unix:path=/run/user/1000/ibus,guid=abc\n" +
				"IBUS_DAEMON_PID=1234\n",
			want: AddressInfo{Address: "unix:path=/run/user/1000/ibus,guid=abc", DaemonPID: 1234},
		},
		{
			name:    "no trailing newline",
			content: "IBUS_ADDRESS=unix:path=/run/ibus",
			want:    AddressInfo{Address: "unix:path=/run/ibus"},
		},
		{
			name:    "carriage returns are trimmed",
			content: "IBUS_ADDRESS=unix:path=/run/ibus\r\nIBUS_DAEMON_PID=7\r\n",
			want:    AddressInfo{Address: "unix:path=/run/ibus", DaemonPID: 7},
		},
		{
			name:    "commented out values are ignored",
			content: "#IBUS_ADDRESS=unix:path=/wrong\nIBUS_ADDRESS=unix:path=/right\n",
			want:    AddressInfo{Address: "unix:path=/right"},
		},
		{
			name:    "no address line",
			content: "IBUS_DAEMON_PID=99\n",
			want:    AddressInfo{DaemonPID: 99},
		},
		{
			name:    "malformed pid",
			content: "IBUS_ADDRESS=unix:path=/run/ibus\nIBUS_DAEMON_PID=not-a-number\n",
			wantErr: true,
		},
		{
			name:    "empty file",
			content: "",
			want:    AddressInfo{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAddressFile([]byte(tc.content))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseAddressFile(%q) = %+v, want an error", tc.content, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAddressFile(%q): %v", tc.content, err)
			}
			if got != tc.want {
				t.Errorf("ParseAddressFile(%q) = %+v, want %+v", tc.content, got, tc.want)
			}
		})
	}
}

func TestAddressInfoAlive(t *testing.T) {
	if (AddressInfo{DaemonPID: 0}).Alive() != true {
		t.Error("a file without a recorded pid must be treated as usable")
	}
	if (AddressInfo{DaemonPID: -1}).Alive() != true {
		t.Error("a negative pid means no pid was recorded")
	}
	if (AddressInfo{DaemonPID: livePID(t)}).Alive() != true {
		t.Error("a running daemon must be reported alive")
	}
	if (AddressInfo{DaemonPID: deadPID(t)}).Alive() != false {
		t.Error("a dead daemon must be reported as gone")
	}
}

func TestProcessAlive(t *testing.T) {
	if ProcessAlive(0) {
		t.Error("pid 0 is not a process")
	}
	if ProcessAlive(-1) {
		t.Error("a negative pid is not a process")
	}
	if !ProcessAlive(livePID(t)) {
		t.Error("a running process must be reported alive")
	}
	if ProcessAlive(deadPID(t)) {
		t.Error("a reaped process must be reported as gone")
	}
}

// IBUS_ADDRESS must win over the address file, like ibus_get_address().
func TestGetAddressEPrefersEnvVar(t *testing.T) {
	path := writeAddressFile(t, "unix:path=/from/file", livePID(t))
	setEnv(t, "IBUS_ADDRESS", "unix:path=/from/env")
	setEnv(t, "IBUS_ADDRESS_FILE", path)

	got, err := GetAddressE()
	if err != nil {
		t.Fatal(err)
	}
	if got != "unix:path=/from/env" {
		t.Errorf("GetAddressE() = %q, want the IBUS_ADDRESS value", got)
	}
}

func TestGetAddressEReadsLiveFile(t *testing.T) {
	bus := startTestBus(t)
	path := writeAddressFile(t, bus.Address, bus.PID)
	setEnv(t, "IBUS_ADDRESS", "")
	setEnv(t, "IBUS_ADDRESS_FILE", path)

	got, err := GetAddressE()
	if err != nil {
		t.Fatal(err)
	}
	if got != bus.Address {
		t.Errorf("GetAddressE() = %q, want %q", got, bus.Address)
	}
}

// This is the situation behind github.com/BambooEngine/ibus-bamboo#300: the
// address file survives a dead ibus-daemon, so dialing it fails with
// "connection refused". libibus refuses the address because the recorded pid is
// gone; goibus used to hand it out anyway.
func TestGetAddressERejectsStaleDaemon(t *testing.T) {
	stale := deadSocketPath(t)
	path := writeAddressFile(t, "unix:path="+stale, deadPID(t))
	setEnv(t, "IBUS_ADDRESS", "")
	setEnv(t, "IBUS_ADDRESS_FILE", path)

	got, err := GetAddressE()
	if err == nil {
		t.Fatalf("GetAddressE() = %q, want an error for a stale address file", got)
	}
	if !errors.Is(err, ErrDaemonNotRunning) {
		t.Errorf("error = %v, want ErrDaemonNotRunning", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q should mention the stale file %s", err, path)
	}
}

func TestGetAddressEMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nothing-here")
	setEnv(t, "IBUS_ADDRESS", "")
	setEnv(t, "IBUS_ADDRESS_FILE", missing)

	if _, err := GetAddressE(); !errors.Is(err, ErrNoAddress) {
		t.Errorf("error = %v, want ErrNoAddress", err)
	}
}

func TestGetAddressEFileWithoutAddressLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dbus-socket")
	if err := ioutil.WriteFile(path, []byte("IBUS_DAEMON_PID=%d\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(path, []byte("IBUS_DAEMON_PID=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	setEnv(t, "IBUS_ADDRESS", "")
	setEnv(t, "IBUS_ADDRESS_FILE", path)

	_, err := GetAddressE()
	if !errors.Is(err, ErrNoAddress) {
		t.Errorf("error = %v, want ErrNoAddress", err)
	}
	if !strings.Contains(err.Error(), "IBUS_ADDRESS") {
		t.Errorf("error %q should explain that the address line is missing", err)
	}
}

// A file with an address but no recorded pid is still worth trying, otherwise
// older or hand-written address files would stop working.
func TestGetAddressEFileWithoutDaemonPID(t *testing.T) {
	path := writeAddressFile(t, "unix:path=/run/ibus", 0)
	setEnv(t, "IBUS_ADDRESS", "")
	setEnv(t, "IBUS_ADDRESS_FILE", path)

	got, err := GetAddressE()
	if err != nil {
		t.Fatal(err)
	}
	if got != "unix:path=/run/ibus" {
		t.Errorf("GetAddressE() = %q, want unix:path=/run/ibus", got)
	}
}

// The deprecated accessor keeps its panic-on-failure behaviour.
func TestGetAddressPanicsOnFailure(t *testing.T) {
	setEnv(t, "IBUS_ADDRESS", "")
	setEnv(t, "IBUS_ADDRESS_FILE", filepath.Join(t.TempDir(), "nothing-here"))

	defer func() {
		if recover() == nil {
			t.Error("GetAddress() should panic when no address can be found")
		}
	}()
	GetAddress()
}

func TestGetSocketPathUsesAddressFileEnv(t *testing.T) {
	setEnv(t, "IBUS_ADDRESS_FILE", "/tmp/explicit")
	if got := GetSocketPath(); got != "/tmp/explicit" {
		t.Errorf("GetSocketPath() = %q, want /tmp/explicit", got)
	}
}

func TestGetSocketPathParsing(t *testing.T) {
	id := GetLocalMachineId()
	tests := []struct {
		wayland string
		display string
		want    string
	}{
		{wayland: "", display: ":0", want: id + "-unix-0"},
		{wayland: "", display: ":0.0", want: id + "-unix-0"},
		{wayland: "", display: ":1.2", want: id + "-unix-1"},
		{wayland: "", display: "host:3.0", want: id + "-host-3"},
		{wayland: "wayland-1", display: ":0", want: id + "-unix-wayland-1"},
	}

	for _, tc := range tests {
		setEnv(t, "IBUS_ADDRESS_FILE", "")
		setEnv(t, "WAYLAND_DISPLAY", tc.wayland)
		setEnv(t, "DISPLAY", tc.display)
		setEnv(t, "XDG_CONFIG_HOME", "/cfg")

		want := "/cfg/ibus/bus/" + tc.want
		if got := GetSocketPath(); got != want {
			t.Errorf("GetSocketPath() with DISPLAY=%q WAYLAND_DISPLAY=%q = %q, want %q",
				tc.display, tc.wayland, got, want)
		}
	}
}

// A DISPLAY without a colon used to panic with an index out of range.
func TestGetSocketPathDisplayWithoutColon(t *testing.T) {
	setEnv(t, "IBUS_ADDRESS_FILE", "")
	setEnv(t, "WAYLAND_DISPLAY", "")
	setEnv(t, "DISPLAY", "notacolon")
	setEnv(t, "XDG_CONFIG_HOME", "/cfg")

	want := "/cfg/ibus/bus/" + GetLocalMachineId() + "-unix-0"
	if got := GetSocketPath(); got != want {
		t.Errorf("GetSocketPath() = %q, want %q", got, want)
	}
}

func TestGetSocketPathEmptyDisplayFallsBackToZero(t *testing.T) {
	setEnv(t, "IBUS_ADDRESS_FILE", "")
	setEnv(t, "WAYLAND_DISPLAY", "")
	setEnv(t, "DISPLAY", "")
	setEnv(t, "XDG_CONFIG_HOME", "/cfg")

	want := "/cfg/ibus/bus/" + GetLocalMachineId() + "-unix-0"
	if got := GetSocketPath(); got != want {
		t.Errorf("GetSocketPath() = %q, want %q", got, want)
	}
}

func TestGetUserConfigDir(t *testing.T) {
	setEnv(t, "XDG_CONFIG_HOME", "/xdg")
	if got := GetUserConfigDir(); got != "/xdg" {
		t.Errorf("GetUserConfigDir() = %q, want /xdg", got)
	}
	setEnv(t, "XDG_CONFIG_HOME", "")
	setEnv(t, "HOME", "/home/someone")
	if got := GetUserConfigDir(); got != "/home/someone/.config" {
		t.Errorf("GetUserConfigDir() = %q, want /home/someone/.config", got)
	}
}

func TestGetLocalMachineId(t *testing.T) {
	id, err := GetLocalMachineIdE()
	if err != nil {
		t.Skipf("no machine id on this system: %v", err)
	}
	if id == "" {
		t.Error("machine id is empty")
	}
	if strings.TrimSpace(id) != id {
		t.Errorf("machine id %q has surrounding whitespace", id)
	}
	if GetLocalMachineId() != id {
		t.Error("the deprecated accessor disagrees with GetLocalMachineIdE")
	}
}

func TestGetUserAuth(t *testing.T) {
	setEnv(t, "DBUS_AUTH_UID", "")
	setEnv(t, "DBUS_AUTH_HOME", "")
	if got := GetUserAuth(); len(got) == 0 {
		t.Error("no auth methods returned")
	}
}

func TestMain(m *testing.M) {
	// Keep the tests independent of whatever DISPLAY the developer has set.
	os.Unsetenv("IBUS_ADDRESS")
	os.Exit(m.Run())
}
