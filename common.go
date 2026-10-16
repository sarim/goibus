package goibus

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
)

const (
	BUS_DAEMON_NAME     = "org.freedesktop.DBus"
	BUS_DAEMON_PATH     = "/org/freedesktop/DBus"
	BUS_PROPERTIES_NAME = "org.freedesktop.DBus.Properties"

	IBUS_IFACE_IBUS   = "org.freedesktop.IBus"
	IBUS_PATH_IBUS    = "/org/freedesktop/IBus"
	IBUS_SERVICE_IBUS = "org.freedesktop.IBus"

	IBUS_IFACE_PANEL          = "org.freedesktop.IBus.Panel"
	IBUS_IFACE_CONFIG         = "org.freedesktop.IBus.Config"
	IBUS_IFACE_SERVICE        = "org.freedesktop.IBus.Service"
	IBUS_IFACE_ENGINE         = "org.freedesktop.IBus.Engine"
	IBUS_IFACE_ENGINE_FACTORY = "org.freedesktop.IBus.Factory"
	IBUS_IFACE_INPUT_CONTEXT  = "org.freedesktop.IBus.InputContext"
	IBUS_IFACE_NOTIFICATIONS  = "org.freedesktop.IBus.Notifications"

	IBUS_ENGINE_PREEDIT_CLEAR  uint32 = 0
	IBUS_ENGINE_PREEDIT_COMMIT uint32 = 1

	ORIENTATION_HORIZONTAL int32 = 0
	ORIENTATION_VERTICAL   int32 = 1
	ORIENTATION_SYSTEM     int32 = 2

	PROP_TYPE_NORMAL    uint32 = 0
	PROP_TYPE_TOGGLE    uint32 = 1
	PROP_TYPE_RADIO     uint32 = 2
	PROP_TYPE_MENU      uint32 = 3
	PROP_TYPE_SEPARATOR uint32 = 4

	PROP_STATE_UNCHECKED    uint32 = 0
	PROP_STATE_CHECKED      uint32 = 1
	PROP_STATE_INCONSISTENT uint32 = 2

	IBUS_ATTR_TYPE_NONE       uint32 = 0
	IBUS_ATTR_TYPE_UNDERLINE  uint32 = 1
	IBUS_ATTR_TYPE_FOREGROUND uint32 = 2
	IBUS_ATTR_TYPE_BACKGROUND uint32 = 3

	IBUS_ATTR_UNDERLINE_NONE   uint32 = 0
	IBUS_ATTR_UNDERLINE_SINGLE uint32 = 1
	IBUS_ATTR_UNDERLINE_DOUBLE uint32 = 2
	IBUS_ATTR_UNDERLINE_LOW    uint32 = 3
	IBUS_ATTR_UNDERLINE_ERROR  uint32 = 4
)

// ErrDaemonNotRunning is reported when the ibus address file points at a
// daemon that is no longer running, which is what a stale socket left behind by
// a crashed or restarted ibus-daemon looks like.
var ErrDaemonNotRunning = errors.New("ibus-daemon is not running")

// ErrNoAddress is reported when no ibus daemon address could be found at all.
var ErrNoAddress = errors.New("no ibus daemon address found")

// AddressInfo is the content of an ibus address file.
type AddressInfo struct {
	// Address is the value of the IBUS_ADDRESS line.
	Address string
	// DaemonPID is the value of the IBUS_DAEMON_PID line, or 0 when absent.
	DaemonPID int
}

// Alive reports whether the daemon recorded in the file still exists.
func (info AddressInfo) Alive() bool {
	if info.DaemonPID <= 0 {
		// Without a recorded pid the file cannot be validated; assume the
		// address is worth trying rather than refusing to connect.
		return true
	}
	return ProcessAlive(info.DaemonPID)
}

// GetAddressE returns the address of the ibus daemon.
//
// It mirrors ibus_get_address(): IBUS_ADDRESS wins, otherwise the address is
// read from the ibus socket file. Unlike GetAddress it reports failures instead
// of panicking, and it refuses to hand back an address whose recorded
// ibus-daemon pid is gone, so callers do not dial a socket nobody listens on.
func GetAddressE() (string, error) {
	if address := os.Getenv("IBUS_ADDRESS"); address != "" {
		return address, nil
	}

	path := GetSocketPath()
	info, err := ReadAddressFile(path)
	if err != nil {
		return "", err
	}
	if !info.Alive() {
		return "", fmt.Errorf("%w: pid %d recorded in %s is gone, "+
			"remove the stale address file or restart ibus-daemon",
			ErrDaemonNotRunning, info.DaemonPID, path)
	}
	if info.Address == "" {
		return "", fmt.Errorf("%w: %s contains no IBUS_ADDRESS line", ErrNoAddress, path)
	}
	return info.Address, nil
}

// GetAddress returns the address of the ibus daemon.
//
// Deprecated: use GetAddressE, which reports failures instead of panicking.
func GetAddress() string {
	address, err := GetAddressE()
	if err != nil {
		panic(err)
	}
	return address
}

// ReadAddressFile reads and parses an ibus address file.
func ReadAddressFile(path string) (AddressInfo, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AddressInfo{}, fmt.Errorf("%w: %s does not exist", ErrNoAddress, path)
		}
		return AddressInfo{}, fmt.Errorf("cannot read ibus address file %s: %v", path, err)
	}
	return ParseAddressFile(data)
}

// ParseAddressFile parses the content of an ibus address file. Comment lines
// are ignored, matching ibus_get_address().
func ParseAddressFile(data []byte) (AddressInfo, error) {
	var info AddressInfo
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "IBUS_ADDRESS=") {
			info.Address = trimLineEnding(line[len("IBUS_ADDRESS="):])
		}
		if strings.HasPrefix(line, "IBUS_DAEMON_PID=") {
			pid, err := strconv.Atoi(strings.TrimSpace(trimLineEnding(line[len("IBUS_DAEMON_PID="):])))
			if err != nil {
				return AddressInfo{}, fmt.Errorf("malformed IBUS_DAEMON_PID line %q: %v", line, err)
			}
			info.DaemonPID = pid
		}
	}
	return info, nil
}

func trimLineEnding(s string) string {
	return strings.TrimRight(s, "\r\n")
}

// GetSocketPath returns the path of the file that records the ibus daemon
// address. It mirrors ibus_get_socket_path().
func GetSocketPath() string {
	path := os.Getenv("IBUS_ADDRESS_FILE")
	if path != "" {
		return path
	}
	display := os.Getenv("WAYLAND_DISPLAY")
	isWayland := true
	if display == "" {
		isWayland = false
		display = os.Getenv("DISPLAY")
	}
	if display == "" {
		fmt.Fprintln(os.Stderr, "DISPLAY is empty! We use default DISPLAY (:0.0)")
		display = ":0.0"
	}
	hostname := "unix"
	displayNumber := ""
	if isWayland {
		displayNumber = display
	} else {
		// format is {hostname}:{displaynumber}.{screennumber}
		HDS := strings.SplitN(display, ":", 2)
		if len(HDS) == 2 {
			if HDS[0] != "" {
				hostname = HDS[0]
			}
			displayNumber = strings.SplitN(HDS[1], ".", 2)[0]
		} else {
			// No colon at all, so there is no display number to parse.
			displayNumber = "0"
		}
	}
	p := fmt.Sprintf("%s-%s-%s", GetLocalMachineId(), hostname, displayNumber)
	path = GetUserConfigDir() + "/ibus/bus/" + p

	return path
}

// GetLocalMachineIdE returns the local machine id.
//
// Deprecated in favour of the machine id lookup used by GetSocketPath; kept
// because it is part of the exported API.
func GetLocalMachineIdE() (string, error) {
	for _, path := range []string{"/var/lib/dbus/machine-id", "/etc/machine-id"} {
		data, err := ioutil.ReadFile(path)
		if err == nil {
			return strings.TrimSpace(string(data)), nil
		}
	}
	return "", fmt.Errorf("cannot read the machine id from /var/lib/dbus/machine-id or /etc/machine-id")
}

// GetLocalMachineId returns the local machine id.
//
// Deprecated: use GetLocalMachineIdE, which reports failures instead of
// panicking.
func GetLocalMachineId() string {
	id, err := GetLocalMachineIdE()
	if err != nil {
		panic(err)
	}
	return id
}

func GetUserConfigDir() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		return os.Getenv("HOME") + "/.config"
	}
	return dir
}

func GetUserAuth() []dbus.Auth {
	uid := os.Getenv("DBUS_AUTH_UID")
	if uid == "" {
		uid = strconv.Itoa(os.Getuid())
	}
	home := os.Getenv("DBUS_AUTH_HOME")
	if home == "" {
		home = os.Getenv("HOME")
	}
	return []dbus.Auth{dbus.AuthExternal(uid), dbus.AuthCookieSha1(uid, home)}
}
