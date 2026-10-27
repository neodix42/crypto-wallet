package sessionwatch

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type fixtureManager struct{ path dbus.ObjectPath }

func (m fixtureManager) GetSessionByPID(pid uint32) (dbus.ObjectPath, *dbus.Error) {
	if pid != uint32(os.Getpid()) {
		return "", dbus.NewError("org.test.InvalidPID", nil)
	}
	return m.path, nil
}

type fixtureProperties struct {
	uid       uint32
	locked    bool
	sleep     bool
	shutdown  bool
	malformed string
	denied    string
}

func (p fixtureProperties) Get(iface, key string) (dbus.Variant, *dbus.Error) {
	if key == p.malformed {
		return dbus.MakeVariant("PUBLIC-INVALID-PROPERTY-TYPE"), nil
	}
	if key == p.denied {
		return dbus.MakeVariant(false), dbus.NewError("org.test.UnavailableProperty", nil)
	}
	switch iface + "." + key {
	case session + ".User":
		return dbus.MakeVariant(struct {
			UID  uint32
			Path dbus.ObjectPath
		}{p.uid, "/org/freedesktop/login1/user/_1000"}), nil
	case session + ".LockedHint":
		return dbus.MakeVariant(p.locked), nil
	case manager + ".PreparingForSleep":
		return dbus.MakeVariant(p.sleep), nil
	case manager + ".PreparingForShutdown":
		return dbus.MakeVariant(p.shutdown), nil
	}
	return dbus.MakeVariant(false), dbus.NewError("org.test.UnknownProperty", nil)
}

func privateBus(t *testing.T) (string, *exec.Cmd) {
	t.Helper()
	tool, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("BLOCKED: private synthetic bus requires dbus-daemon test tool")
	}
	directory := t.TempDir()
	address := "unix:path=" + filepath.Join(directory, "owned-bus")
	command := exec.Command(tool, "--session", "--nofork", "--nopidfile", "--print-address=1", "--address="+address)
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + directory}
	command.Stderr = io.Discard
	output, err := command.StdoutPipe()
	if err != nil || command.Start() != nil {
		t.Fatal("private bus did not start", err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	ready := make(chan bool, 1)
	go func() { _, err := bufio.NewReader(output).ReadString('\n'); ready <- err == nil }()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("private bus did not announce readiness")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("private bus readiness deadline exceeded")
	}
	return address, command
}

func fixture(t *testing.T, p fixtureProperties, path dbus.ObjectPath) (string, *dbus.Conn, *exec.Cmd) {
	t.Helper()
	address, daemon := privateBus(t)
	connection, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	if connection.Export(fixtureManager{path}, managerPath, manager) != nil ||
		connection.Export(p, managerPath, properties) != nil || connection.Export(p, path, properties) != nil {
		t.Fatal("synthetic logind service export failed")
	}
	owned, err := connection.RequestName(service, dbus.NameFlagDoNotQueue)
	if err != nil || owned != dbus.RequestNameReplyPrimaryOwner {
		t.Fatal("private synthetic service name unavailable", err)
	}
	return address, connection, daemon
}

func watchFixture(t *testing.T, address string) (*Watcher, chan string) {
	t.Helper()
	events := make(chan string, 64)
	watcher, err := start(context.Background(), Callbacks{
		Lock: func() { events <- "lock" }, Fault: func() { events <- "fault" },
	}, address, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = watcher.Close() })
	return watcher, events
}

func expect(t *testing.T, events chan string, wanted string) {
	t.Helper()
	select {
	case actual := <-events:
		if actual != wanted {
			t.Fatalf("value-free event = %q, wanted %q", actual, wanted)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing synthetic lifecycle event", wanted)
	}
}

func quiet(t *testing.T, events chan string) {
	t.Helper()
	select {
	case actual := <-events:
		t.Fatal("unrelated signal or intentional close changed wallet lifecycle", actual)
	case <-time.After(80 * time.Millisecond):
	}
}

func TestPrivateBusOwnSessionLockSuspendShutdownAndSenderIsolation(t *testing.T) {
	path := dbus.ObjectPath("/org/freedesktop/login1/session/fixture")
	address, servicePeer, _ := fixture(t, fixtureProperties{uid: uint32(os.Getuid())}, path)
	watcher, events := watchFixture(t, address)
	if watcher.InitiallyLocked() {
		t.Fatal("fresh unlocked fixture reported locked")
	}
	foreign, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	_ = foreign.Emit(path, session+".Lock")
	_ = servicePeer.Emit("/org/freedesktop/login1/session/foreign", session+".Lock")
	_ = servicePeer.Emit(managerPath, manager+".PrepareForSleep", false)
	_ = servicePeer.Emit(path, properties+".PropertiesChanged", session,
		map[string]dbus.Variant{"LockedHint": dbus.MakeVariant(false)}, []string{})
	quiet(t, events)
	for _, action := range []func() error{
		func() error { return servicePeer.Emit(path, session+".Lock") },
		func() error {
			return servicePeer.Emit(path, properties+".PropertiesChanged", session,
				map[string]dbus.Variant{"LockedHint": dbus.MakeVariant(true)}, []string{})
		},
		func() error { return servicePeer.Emit(managerPath, manager+".PrepareForSleep", true) },
		func() error { return servicePeer.Emit(managerPath, manager+".PrepareForShutdown", true) },
	} {
		if err := action(); err != nil {
			t.Fatal(err)
		}
		expect(t, events, "lock")
	}
	_ = watcher.Close()
	quiet(t, events)
}

func TestPrivateBusInitialLockedStateAndPreparingPowerStates(t *testing.T) {
	for name, state := range map[string]fixtureProperties{
		"locked":   {uid: uint32(os.Getuid()), locked: true},
		"sleep":    {uid: uint32(os.Getuid()), sleep: true},
		"shutdown": {uid: uint32(os.Getuid()), shutdown: true},
	} {
		t.Run(name, func(t *testing.T) {
			address, _, _ := fixture(t, state, "/org/freedesktop/login1/session/fixture")
			watcher, events := watchFixture(t, address)
			if !watcher.InitiallyLocked() {
				t.Fatal("startup missed existing lock/power preparation")
			}
			expect(t, events, "lock")
		})
	}
}

func TestPrivateBusDisconnectOwnerLossSessionLossAndInvalidatedStateFailClosed(t *testing.T) {
	for _, mode := range []string{"disconnect", "owner", "session", "malformed", "invalidated"} {
		t.Run(mode, func(t *testing.T) {
			path := dbus.ObjectPath("/org/freedesktop/login1/session/fixture")
			address, peer, daemon := fixture(t, fixtureProperties{uid: uint32(os.Getuid())}, path)
			_, events := watchFixture(t, address)
			switch mode {
			case "disconnect":
				_ = daemon.Process.Kill()
			case "owner":
				_, _ = peer.ReleaseName(service)
			case "session":
				_ = peer.Emit(managerPath, manager+".SessionRemoved", "fixture", path)
			case "malformed":
				_ = peer.Emit(managerPath, manager+".PrepareForSleep", "PUBLIC-INVALID-BOOLEAN")
			case "invalidated":
				_ = peer.Emit(path, properties+".PropertiesChanged", session, map[string]dbus.Variant{}, []string{"LockedHint"})
			}
			expect(t, events, "lock")
			expect(t, events, "fault")
			quiet(t, events)
		})
	}
}

func TestPrivateBusWrongUIDConveniencePathAndMissingServiceNeverClaimMonitoring(t *testing.T) {
	for _, mode := range []string{"uid", "owner-uid", "self", "missing", "user-type", "lock-type", "property-error"} {
		t.Run(mode, func(t *testing.T) {
			path := dbus.ObjectPath("/org/freedesktop/login1/session/fixture")
			state := fixtureProperties{uid: uint32(os.Getuid())}
			if mode == "uid" {
				state.uid++
			} else if mode == "self" {
				path = "/org/freedesktop/login1/session/self"
			} else if mode == "user-type" {
				state.malformed = "User"
			} else if mode == "lock-type" {
				state.malformed = "LockedHint"
			} else if mode == "property-error" {
				state.denied = "PreparingForSleep"
			}
			address, peer, _ := fixture(t, state, path)
			if mode == "missing" {
				_, _ = peer.ReleaseName(service)
			}
			expectedOwner := uint32(os.Getuid())
			if mode == "owner-uid" {
				expectedOwner++
			}
			watcher, err := start(context.Background(), Callbacks{Lock: func() { t.Error("startup error called Lock") }, Fault: func() { t.Error("startup error called Fault") }}, address, expectedOwner)
			if watcher != nil || !errors.Is(err, ErrUnavailable) {
				t.Fatal("unsupported session claimed lifecycle protection", err)
			}
		})
	}
}

func TestBoundedSignalQueueAndInvalidCallbackConfiguration(t *testing.T) {
	signals := &boundedSignals{values: make(chan *dbus.Signal, 1), overflow: make(chan struct{})}
	signals.DeliverSignal("", "", &dbus.Signal{})
	signals.DeliverSignal("", "", &dbus.Signal{})
	select {
	case <-signals.overflow:
	default:
		t.Fatal("excess signal delivery did not fail closed")
	}
	if watcher, err := start(context.Background(), Callbacks{}, "unused", uint32(os.Getuid())); watcher != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing value-free lifecycle handlers were accepted", err)
	}
	if watcher, err := Start(nil, Callbacks{}); watcher != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatal("invalid public startup configuration was accepted", err)
	}
	if watcher, err := start(context.Background(), Callbacks{Lock: func() {}, Fault: func() {}},
		"unix:path="+filepath.Join(t.TempDir(), "absent-private-bus"), uint32(os.Getuid())); watcher != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatal("unreachable private bus claimed lifecycle protection", err)
	}
}

func TestMalformedLifecycleMessagesCannotBecomeUnlockOrSilentSuccess(t *testing.T) {
	owner := ":1.100"
	path := dbus.ObjectPath("/org/freedesktop/login1/session/fixture")
	for _, test := range []struct {
		name   string
		value  *dbus.Signal
		failed bool
	}{
		{"nil", nil, true},
		{"owner-body", &dbus.Signal{Sender: busService, Path: busPath, Name: busService + ".NameOwnerChanged"}, true},
		{"power-body", &dbus.Signal{Sender: owner, Path: managerPath, Name: manager + ".PrepareForSleep"}, true},
		{"session-body", &dbus.Signal{Sender: owner, Path: managerPath, Name: manager + ".SessionRemoved"}, true},
		{"properties-body", &dbus.Signal{Sender: owner, Path: path, Name: properties + ".PropertiesChanged"}, true},
		{"properties-interface", &dbus.Signal{Sender: owner, Path: path, Name: properties + ".PropertiesChanged", Body: []any{1, nil, nil}}, true},
		{"foreign-interface", &dbus.Signal{Sender: owner, Path: path, Name: properties + ".PropertiesChanged", Body: []any{"org.test.Foreign", nil, nil}}, false},
		{"properties-changed", &dbus.Signal{Sender: owner, Path: path, Name: properties + ".PropertiesChanged", Body: []any{session, nil, nil}}, true},
		{"properties-invalidated", &dbus.Signal{Sender: owner, Path: path, Name: properties + ".PropertiesChanged", Body: []any{session, map[string]dbus.Variant{}, nil}}, true},
		{"unrelated-property", &dbus.Signal{Sender: owner, Path: path, Name: properties + ".PropertiesChanged", Body: []any{session, map[string]dbus.Variant{}, []string{"Other"}}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			lock, failed := event(test.value, owner, path)
			if lock || failed != test.failed {
				t.Fatal("malformed/unrelated lifecycle message changed its authority", lock, failed)
			}
		})
	}
}
