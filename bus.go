package goibus

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

type Bus struct {
	dbusConn   *dbus.Conn
	dbusObject dbus.BusObject
	ibusObject dbus.BusObject
}

// NewBus connects to the ibus daemon.
//
// Deprecated: use NewBusE or DialWhenAvailable. This function panics if the
// daemon cannot be reached, which takes the whole process down and makes an
// engine that ibus-daemon spawns crash-loop instead of reporting the problem.
func NewBus() *Bus {
	bus, err := NewBusE()
	if err != nil {
		panic(err)
	}
	return bus
}

// NewBusE connects to the ibus daemon and reports failures as an error.
func NewBusE() (*Bus, error) {
	address, err := GetAddressE()
	if err != nil {
		return nil, err
	}
	return Dial(address)
}

// Dial connects to an ibus daemon listening on the given address. The address
// must be a valid D-Bus address such as "unix:path=/run/ibus" or "unix:abstract=...".
func Dial(address string) (*Bus, error) {
	conn, err := dbus.Dial(address)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to the ibus daemon at %s: %v", address, err)
	}

	if err := conn.Auth(GetUserAuth()); err != nil {
		conn.Close()
		return nil, fmt.Errorf("authentication with the ibus daemon at %s failed: %v", address, err)
	}

	if err := conn.Hello(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("handshake with the ibus daemon at %s failed: %v", address, err)
	}

	return &Bus{
		dbusConn:   conn,
		dbusObject: conn.Object(BUS_DAEMON_NAME, dbus.ObjectPath(BUS_DAEMON_PATH)),
		ibusObject: conn.Object(IBUS_SERVICE_IBUS, dbus.ObjectPath(IBUS_PATH_IBUS)),
	}, nil
}

func (bus *Bus) CallMethod(name string, flags dbus.Flags, args ...interface{}) *dbus.Call {
	return bus.ibusObject.Call(bus.ibusObject.Destination()+"."+name, flags, args...)
}

func (bus *Bus) RequestName(name string, flags dbus.RequestNameFlags) (dbus.RequestNameReply, error) {
	return bus.dbusConn.RequestName(name, flags)
}

// RequestNameChecked requests a bus name and fails when the name is already
// owned by somebody else. Without the reply check a component silently believes
// it registered while ibus cannot reach it.
func (bus *Bus) RequestNameChecked(name string, flags dbus.RequestNameFlags) error {
	reply, err := bus.RequestName(name, flags)
	if err != nil {
		return fmt.Errorf("cannot acquire bus name %s: %v", name, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner && reply != dbus.RequestNameReplyAlreadyOwner {
		return fmt.Errorf("bus name %s is already owned by another process", name)
	}
	return nil
}

func (bus *Bus) RegisterComponent(component *Component) *dbus.Call {
	return bus.CallMethod("RegisterComponent", 0, dbus.MakeVariant(component))
}

func (bus *Bus) GetDbusConn() *dbus.Conn {
	return bus.dbusConn
}

// Done is closed when the connection to the ibus daemon is lost, for example
// because ibus-daemon was restarted. Long lived components should watch it so
// they can exit instead of blocking forever on a dead connection.
func (bus *Bus) Done() <-chan struct{} {
	return bus.dbusConn.Context().Done()
}

// Close disconnects from the ibus daemon.
func (bus *Bus) Close() error {
	return bus.dbusConn.Close()
}
