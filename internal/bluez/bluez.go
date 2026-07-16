// Package bluez watches and drives the AirPods' BlueZ Device1 object over
// the system D-Bus: event-driven Connected state (no polling) plus
// Connect/Disconnect actuation.
package bluez

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
)

const (
	busName        = "org.bluez"
	device1Iface   = "org.bluez.Device1"
	adapter1Iface  = "org.bluez.Adapter1"
	adapterPath    = dbus.ObjectPath("/org/bluez/hci0")
	propsIface     = "org.freedesktop.DBus.Properties"
	propsChangedSg = "org.freedesktop.DBus.Properties.PropertiesChanged"
)

// Watcher tracks the AirPods' live Connected state and can drive
// Connect/Disconnect on it.
type Watcher struct {
	conn       *dbus.Conn
	devicePath dbus.ObjectPath
}

// ControllerAddress returns the local hci0 Bluetooth controller address.
func (w *Watcher) ControllerAddress() (string, error) {
	v, err := w.conn.Object(busName, adapterPath).GetProperty(adapter1Iface + ".Address")
	if err != nil {
		return "", fmt.Errorf("read controller address: %w", err)
	}
	address, ok := v.Value().(string)
	if !ok {
		return "", fmt.Errorf("unexpected controller address type %T", v.Value())
	}
	return address, nil
}

// New connects to the system bus and prepares a watcher for the device at
// devicePath (see config.AirPodsDevicePath).
func New(devicePath string) (*Watcher, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect system bus: %w", err)
	}
	return &Watcher{conn: conn, devicePath: dbus.ObjectPath(devicePath)}, nil
}

// Close releases the D-Bus connection.
func (w *Watcher) Close() error {
	return w.conn.Close()
}

// Connected returns the device's current Connected property.
func (w *Watcher) Connected() (bool, error) {
	obj := w.conn.Object(busName, w.devicePath)
	v, err := obj.GetProperty(device1Iface + ".Connected")
	if err != nil {
		// Device object may not exist yet (never paired/seen on this adapter).
		return false, nil
	}
	b, ok := v.Value().(bool)
	if !ok {
		return false, fmt.Errorf("unexpected Connected property type %T", v.Value())
	}
	return b, nil
}

// Connect asks BlueZ to connect the device (blocks until connected or BlueZ
// gives up).
func (w *Watcher) Connect(ctx context.Context) error {
	obj := w.conn.Object(busName, w.devicePath)
	call := obj.CallWithContext(ctx, device1Iface+".Connect", 0)
	return call.Err
}

// Disconnect asks BlueZ to disconnect the device.
func (w *Watcher) Disconnect(ctx context.Context) error {
	obj := w.conn.Object(busName, w.devicePath)
	call := obj.CallWithContext(ctx, device1Iface+".Disconnect", 0)
	return call.Err
}

// Watch subscribes to PropertiesChanged on the device and calls onChange
// with the new Connected value each time it flips. Blocks until ctx is
// cancelled.
func (w *Watcher) Watch(ctx context.Context, onChange func(connected bool)) error {
	matchRule := fmt.Sprintf(
		"type='signal',interface='%s',member='PropertiesChanged',path='%s'",
		propsIface, w.devicePath,
	)
	if err := w.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.AddMatch", 0, matchRule).Err; err != nil {
		return fmt.Errorf("add match: %w", err)
	}
	defer w.conn.BusObject().Call("org.freedesktop.DBus.RemoveMatch", 0, matchRule)

	signals := make(chan *dbus.Signal, 16)
	w.conn.Signal(signals)
	defer w.conn.RemoveSignal(signals)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sig, ok := <-signals:
			if !ok {
				return nil
			}
			if sig.Path != w.devicePath || sig.Name != propsChangedSg {
				continue
			}
			if len(sig.Body) < 2 {
				continue
			}
			iface, _ := sig.Body[0].(string)
			if iface != device1Iface {
				continue
			}
			changed, ok := sig.Body[1].(map[string]dbus.Variant)
			if !ok {
				continue
			}
			v, ok := changed["Connected"]
			if !ok {
				continue
			}
			b, ok := v.Value().(bool)
			if !ok {
				continue
			}
			onChange(b)
		}
	}
}
