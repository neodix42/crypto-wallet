// Package sessionwatch observes logind without requesting any OS state change.
// Only value-free lock/failure notifications leave this package.
package sessionwatch

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	service       = "org.freedesktop.login1"
	manager       = "org.freedesktop.login1.Manager"
	session       = "org.freedesktop.login1.Session"
	properties    = "org.freedesktop.DBus.Properties"
	managerPath   = dbus.ObjectPath("/org/freedesktop/login1")
	busPath       = dbus.ObjectPath("/org/freedesktop/DBus")
	busService    = "org.freedesktop.DBus"
	systemAddress = "unix:path=/run/dbus/system_bus_socket"
)

var ErrUnavailable = errors.New("OS session monitoring unavailable")

// Callbacks must return promptly. Lock should terminate secret-bearing wallet
// processes immediately, then dispatch GTK changes onto its owning thread.
// Fault follows Lock when previously established monitoring becomes unavailable.
type Callbacks struct{ Lock, Fault func() }

type Watcher struct {
	connection *dbus.Conn
	cancel     context.CancelFunc
	initial    bool
	stop       sync.Once
}

// InitiallyLocked includes an existing screen lock or preparation for sleep or
// shutdown observed during setup. No signal ever automatically unlocks a wallet.
func (w *Watcher) InitiallyLocked() bool { return w.initial }

func (w *Watcher) Close() error {
	w.stop.Do(func() { w.cancel(); _ = w.connection.Close() })
	return nil
}

type boundedSignals struct {
	values   chan *dbus.Signal
	overflow chan struct{}
	once     sync.Once
}

func (s *boundedSignals) DeliverSignal(_, _ string, signal *dbus.Signal) {
	select {
	case s.values <- signal:
	default:
		s.once.Do(func() { close(s.overflow) })
	}
}

// Start connects directly to the standard system socket, ignoring environment
// overrides. A private, owned connection is closed when monitoring stops. Setup
// errors return a fixed message and must be made visible by the application.
func Start(ctx context.Context, callbacks Callbacks) (*Watcher, error) {
	return start(ctx, callbacks, systemAddress, 0)
}

func start(parent context.Context, callbacks Callbacks, address string, expectedServiceUID uint32) (*Watcher, error) {
	if parent == nil || callbacks.Lock == nil || callbacks.Fault == nil {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithCancel(parent)
	signals := &boundedSignals{values: make(chan *dbus.Signal, 32), overflow: make(chan struct{})}
	// The startup timer also closes a connection stuck in authentication.
	startup, endStartup := context.WithTimeout(ctx, 3*time.Second)
	defer endStartup()
	connecting := time.AfterFunc(3*time.Second, cancel)
	connection, err := dbus.Connect(address, dbus.WithContext(ctx), dbus.WithSignalHandler(signals))
	if err != nil {
		connecting.Stop()
		cancel()
		return nil, ErrUnavailable
	}
	fail := func() (*Watcher, error) {
		connecting.Stop()
		cancel()
		_ = connection.Close()
		return nil, ErrUnavailable
	}
	var owner string
	if connection.BusObject().CallWithContext(startup, busService+".GetNameOwner", 0, service).Store(&owner) != nil || !strings.HasPrefix(owner, ":") {
		return fail()
	}
	var ownerUID uint32
	if connection.BusObject().CallWithContext(startup, busService+".GetConnectionUnixUser", 0, owner).Store(&ownerUID) != nil || ownerUID != expectedServiceUID {
		return fail()
	}
	if connection.AddMatchSignalContext(startup, dbus.WithMatchSender(busService), dbus.WithMatchObjectPath(busPath),
		dbus.WithMatchInterface(busService), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, service)) != nil {
		return fail()
	}
	var path dbus.ObjectPath
	if connection.Object(owner, managerPath).CallWithContext(startup, manager+".GetSessionByPID", 0, uint32(os.Getpid())).Store(&path) != nil ||
		!path.IsValid() || !strings.HasPrefix(string(path), string(managerPath)+"/session/") ||
		strings.HasSuffix(string(path), "/self") || strings.HasSuffix(string(path), "/auto") {
		return fail()
	}
	var user dbus.Variant
	if connection.Object(owner, path).CallWithContext(startup, properties+".Get", 0, session, "User").Store(&user) != nil || user.Signature().String() != "(uo)" {
		return fail()
	}
	var identity struct {
		UID  uint32
		Path dbus.ObjectPath
	}
	if dbus.Store([]any{user.Value()}, &identity) != nil || uint64(identity.UID) != uint64(os.Getuid()) {
		return fail()
	}
	for _, rule := range [][]dbus.MatchOption{
		{dbus.WithMatchSender(owner), dbus.WithMatchObjectPath(path), dbus.WithMatchInterface(session), dbus.WithMatchMember("Lock")},
		{dbus.WithMatchSender(owner), dbus.WithMatchObjectPath(path), dbus.WithMatchInterface(properties), dbus.WithMatchMember("PropertiesChanged")},
		{dbus.WithMatchSender(owner), dbus.WithMatchObjectPath(managerPath), dbus.WithMatchInterface(manager), dbus.WithMatchMember("PrepareForSleep")},
		{dbus.WithMatchSender(owner), dbus.WithMatchObjectPath(managerPath), dbus.WithMatchInterface(manager), dbus.WithMatchMember("PrepareForShutdown")},
		{dbus.WithMatchSender(owner), dbus.WithMatchObjectPath(managerPath), dbus.WithMatchInterface(manager), dbus.WithMatchMember("SessionRemoved")},
	} {
		if connection.AddMatchSignalContext(startup, rule...) != nil {
			return fail()
		}
	}
	initial := false
	for _, query := range []struct {
		path       dbus.ObjectPath
		iface, key string
	}{{path, session, "LockedHint"}, {managerPath, manager, "PreparingForSleep"}, {managerPath, manager, "PreparingForShutdown"}} {
		var value dbus.Variant
		if connection.Object(owner, query.path).CallWithContext(startup, properties+".Get", 0, query.iface, query.key).Store(&value) != nil {
			return fail()
		}
		state, ok := value.Value().(bool)
		if !ok {
			return fail()
		}
		initial = initial || state
	}
	if !connecting.Stop() || ctx.Err() != nil {
		return fail()
	}
	w := &Watcher{connection: connection, cancel: cancel, initial: initial}
	go w.run(ctx, callbacks, signals, owner, path)
	return w, nil
}

func (w *Watcher) run(ctx context.Context, callbacks Callbacks, signals *boundedSignals, owner string, path dbus.ObjectPath) {
	defer w.Close()
	if w.initial {
		callbacks.Lock()
	}
	fault := func() { callbacks.Lock(); callbacks.Fault() }
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.connection.Context().Done():
			if ctx.Err() == nil {
				fault()
			}
			return
		case <-signals.overflow:
			fault()
			return
		case signal := <-signals.values:
			lock, failed := event(signal, owner, path)
			if lock {
				callbacks.Lock()
			}
			if failed {
				if !lock {
					callbacks.Lock()
				}
				callbacks.Fault()
				return
			}
		}
	}
}

func event(signal *dbus.Signal, owner string, path dbus.ObjectPath) (lock, failed bool) {
	if signal == nil {
		return false, true
	}
	if signal.Sender == busService && signal.Path == busPath && signal.Name == busService+".NameOwnerChanged" {
		if len(signal.Body) != 3 {
			return false, true
		}
		name, ok := signal.Body[0].(string)
		return false, !ok || name == service
	}
	if signal.Sender != owner {
		return false, false
	}
	switch {
	case signal.Path == path && signal.Name == session+".Lock":
		return true, len(signal.Body) != 0
	case signal.Path == managerPath && (signal.Name == manager+".PrepareForSleep" || signal.Name == manager+".PrepareForShutdown"):
		if len(signal.Body) != 1 {
			return false, true
		}
		state, ok := signal.Body[0].(bool)
		return state, !ok
	case signal.Path == managerPath && signal.Name == manager+".SessionRemoved":
		if len(signal.Body) != 2 {
			return false, true
		}
		removed, ok := signal.Body[1].(dbus.ObjectPath)
		return removed == path, !ok || removed == path
	case signal.Path == path && signal.Name == properties+".PropertiesChanged":
		if len(signal.Body) != 3 {
			return false, true
		}
		iface, ok := signal.Body[0].(string)
		if !ok {
			return false, true
		}
		if iface != session {
			return false, false
		}
		changed, ok := signal.Body[1].(map[string]dbus.Variant)
		if !ok {
			return false, true
		}
		invalidated, ok := signal.Body[2].([]string)
		if !ok {
			return false, true
		}
		for _, key := range invalidated {
			if key == "LockedHint" {
				return false, true
			}
		}
		if value, exists := changed["LockedHint"]; exists {
			state, ok := value.Value().(bool)
			return state, !ok
		}
	}
	return false, false
}
