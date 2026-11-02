package device

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/qzz0518/moko/internal/echonet"
	"github.com/qzz0518/moko/internal/sharp"
)

var testIP = net.IPv4(192, 0, 2, 7)

// fake is a simulated KI-SX100.
type fake struct {
	mu       sync.Mutex
	on       bool
	mode     byte
	humidify bool
	gone     bool
	nodes    bool
	energy   uint32
	inf      chan echonet.Notification

	sets      []byte // EPCs written
	ignore80  bool   // the standard power property does nothing
	ignoreAll bool   // writes are accepted and do nothing
	refuseSet bool
	maker     []byte
	discover  error
}

func newFake() *fake {
	return &fake{on: true, mode: 0x10, humidify: true, nodes: true, energy: 126879,
		inf: make(chan echonet.Notification, 8), maker: []byte{0, 0, 5}}
}

func (f *fake) Notifications() <-chan echonet.Notification { return f.inf }

func (f *fake) Discover(ctx context.Context) ([]echonet.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.discover != nil {
		return nil, f.discover
	}
	if f.gone || !f.nodes {
		return nil, nil
	}
	return []echonet.Node{{IP: testIP, Instances: []echonet.EOJ{echonet.AirCleaner}}}, nil
}

func (f *fake) f3() []byte {
	b := make([]byte, 27)
	b[4] = f.mode
	if f.on {
		b[13] = 0xFF
	}
	if f.humidify {
		b[15] = 0xFF
	}
	return b
}

func (f *fake) Get(ctx context.Context, ip net.IP, eoj echonet.EOJ, epc byte) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gone {
		return nil, echonet.ErrTimeout
	}
	if eoj == echonet.NodeProfile {
		switch epc {
		case 0xD6:
			return []byte{1, 0x01, 0x35, 0x01}, nil
		case 0x8C:
			return []byte("HW-A04\x00\x00\x00\x00\x00\x00"), nil
		case 0x83:
			return []byte{0xFE, 0, 0, 5}, nil
		}
		return nil, echonet.ErrRefused
	}
	switch epc {
	case sharp.EPCMaker:
		return f.maker, nil
	case sharp.EPCModel:
		return []byte("KISX100\x00\x00\x00\x00\x00"), nil
	case sharp.EPCFirmware:
		return []byte("104"), nil
	case sharp.EPCPower:
		if f.on {
			return []byte{0x30}, nil
		}
		return []byte{0x31}, nil
	case sharp.EPCSettings:
		return f.f3(), nil
	case sharp.EPCStatus:
		b := make([]byte, 40)
		b[19], b[27] = 0xFF, f.mode
		if f.humidify {
			b[24] = 0x81
		}
		return b, nil
	case sharp.EPCSensors:
		b := make([]byte, 40)
		b[3], b[4] = 24, 51
		return b, nil
	case sharp.EPCWatts:
		return []byte{0, 13}, nil
	case sharp.EPCEnergy:
		e := f.energy
		return []byte{byte(e >> 24), byte(e >> 16), byte(e >> 8), byte(e)}, nil
	case sharp.EPCPollution, sharp.EPCFault:
		return []byte{0x42}, nil
	}
	return nil, echonet.ErrRefused
}

func (f *fake) Set(ctx context.Context, ip net.IP, eoj echonet.EOJ, epc byte, edt []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gone {
		return echonet.ErrTimeout
	}
	f.sets = append(f.sets, epc)
	if f.refuseSet {
		return echonet.ErrRefused
	}
	if f.ignoreAll {
		return nil
	}
	switch epc {
	case sharp.EPCPower:
		if !f.ignore80 {
			f.on = edt[0] == 0x30
		}
	case sharp.EPCSettings:
		switch {
		case edt[0] == 0x01 && edt[1] == 0x01:
			f.mode = edt[4]
		case edt[1] == 0x09:
			f.humidify = edt[15] == 0xFF
		case edt[1] == 0x03:
			f.on = edt[13] == 0xFF
		}
	}
	return nil
}

// harness runs a manager over a fake.
type harness struct {
	t      *testing.T
	f      *fake
	m      *Manager
	snaps  chan Snapshot
	cancel context.CancelFunc
	done   chan struct{}
}

func start(t *testing.T, f *fake, edit func(*Config)) *harness {
	t.Helper()
	h := &harness{t: t, f: f, snaps: make(chan Snapshot, 256), done: make(chan struct{})}
	cfg := Config{
		OnChange:       func(s Snapshot) { h.snaps <- s },
		PollVisible:    20 * time.Millisecond,
		PollHidden:     20 * time.Millisecond,
		RequestGap:     -1,
		DiscoverTime:   time.Millisecond,
		RetryMin:       10 * time.Millisecond,
		RetryMax:       20 * time.Millisecond,
		ConfirmTimeout: 80 * time.Millisecond,
		ConfirmEvery:   5 * time.Millisecond,
	}
	if edit != nil {
		edit(&cfg)
	}
	h.m = New(f, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() { h.m.Run(ctx); close(h.done) }()
	t.Cleanup(func() { cancel(); <-h.done })
	return h
}

func (h *harness) wait(what string, ok func(Snapshot) bool) Snapshot {
	h.t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case s := <-h.snaps:
			if ok(s) {
				return s
			}
		case <-deadline:
			h.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func (h *harness) online() Snapshot {
	h.t.Helper()
	return h.wait("online", func(s Snapshot) bool { return s.Phase == PhaseOnline && s.HasState })
}

func TestConnectsAndReads(t *testing.T) {
	h := start(t, newFake(), nil)
	s := h.online()
	if s.Info.Model != "KISX100" || s.Info.Module != "HW-A04" || s.Info.IP != testIP.String() || s.Info.Firmware != "104" || s.Info.NodeID == "" {
		t.Errorf("info = %+v", s.Info)
	}
	st := s.State
	if !st.On || st.Mode != sharp.ModeAuto || !st.Humidify || st.Humidifier != sharp.HumidifyActive || !st.WaterOK {
		t.Errorf("state = %+v", st)
	}
	if !st.SensorsOK || st.Sensors.TempC != 24 || st.Sensors.Humidity != 51 {
		t.Errorf("sensors = %+v", st.Sensors)
	}
	if !st.WattsOK || st.Watts != 13 || !st.EnergyOK || st.EnergyWh != 126879 || st.Polluted || st.Fault {
		t.Errorf("readings = %+v", st)
	}
}

func TestModeCommandIsConfirmed(t *testing.T) {
	f := newFake()
	h := start(t, f, nil)
	h.online()
	if err := h.m.SetMode(context.Background(), sharp.ModeSilent); err != nil {
		t.Fatal(err)
	}
	s := h.wait("the new mode", func(s Snapshot) bool { return s.State.Mode == sharp.ModeSilent })
	if !s.State.Humidify {
		t.Error("changing the mode changed humidifying")
	}
}

func TestUnconfirmedCommandFails(t *testing.T) {
	f := newFake()
	f.ignoreAll = true
	h := start(t, f, nil)
	h.online()
	err := h.m.SetMode(context.Background(), sharp.ModeSilent)
	if !errors.Is(err, ErrUnconfirmed) {
		t.Fatalf("err = %v", err)
	}
}

func TestHumidifyCommand(t *testing.T) {
	f := newFake()
	h := start(t, f, nil)
	h.online()
	if err := h.m.SetHumidify(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	h.wait("humidifier off", func(s Snapshot) bool { return !s.State.Humidify && s.State.Humidifier == sharp.HumidifyOff })
}

func TestCommandsTurnTheDeviceOnFirst(t *testing.T) {
	f := newFake()
	f.on = false
	h := start(t, f, nil)
	h.online()
	if err := h.m.SetMode(context.Background(), sharp.ModeNight); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.on || f.mode != byte(sharp.ModeNight) {
		t.Errorf("on = %v, mode = %#x", f.on, f.mode)
	}
}

func TestHumidifyOffDoesNotTurnOn(t *testing.T) {
	f := newFake()
	f.on = false
	h := start(t, f, nil)
	h.online()
	if err := h.m.SetHumidify(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.on {
		t.Error("turning humidifying off turned the device on")
	}
}

func TestPowerUsesStandardProperty(t *testing.T) {
	f := newFake()
	h := start(t, f, nil)
	h.online()
	if err := h.m.SetPower(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.on || len(f.sets) != 1 || f.sets[0] != sharp.EPCPower {
		t.Errorf("on = %v, sets = %x", f.on, f.sets)
	}
}

func TestPowerFallsBackToSettings(t *testing.T) {
	f := newFake()
	f.ignore80 = true
	h := start(t, f, nil)
	h.online()
	if err := h.m.SetPower(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.on {
		t.Error("still on")
	}
	if len(f.sets) != 2 || f.sets[1] != sharp.EPCSettings {
		t.Errorf("sets = %x", f.sets)
	}
}

func TestGoesOfflineKeepingReadingsAndReturns(t *testing.T) {
	f := newFake()
	h := start(t, f, nil)
	h.online()
	f.mu.Lock()
	f.gone = true
	f.mu.Unlock()
	s := h.wait("offline", func(s Snapshot) bool { return s.Phase == PhaseOffline })
	if !s.HasState || s.State.Sensors.TempC != 24 {
		t.Errorf("readings lost: %+v", s)
	}
	if err := h.m.SetPower(context.Background(), true); !errors.Is(err, ErrOffline) {
		t.Errorf("command while offline: %v", err)
	}
	f.mu.Lock()
	f.gone = false
	f.mu.Unlock()
	h.m.Refresh()
	h.online()
}

func TestReportsWhyItCannotFind(t *testing.T) {
	f := newFake()
	f.nodes = false
	h := start(t, f, nil)
	s := h.wait("an error", func(s Snapshot) bool { return s.Err != "" })
	if s.Phase != PhaseSearching {
		t.Errorf("phase = %v", s.Phase)
	}
	f.mu.Lock()
	f.discover = errors.New("no route to host")
	f.mu.Unlock()
	h.m.Refresh()
	h.wait("the network error", func(s Snapshot) bool { return s.Err == "no route to host" })
}

func TestNotSharpIsRejected(t *testing.T) {
	f := newFake()
	f.maker = []byte{0, 0, 6}
	h := start(t, f, nil)
	s := h.wait("an error", func(s Snapshot) bool { return s.Err != "" })
	if s.Phase == PhaseOnline {
		t.Error("connected to another maker's purifier")
	}
}

func TestManualAddressSkipsDiscovery(t *testing.T) {
	f := newFake()
	f.nodes = false
	h := start(t, f, func(c *Config) { c.ManualIP = testIP.String() })
	if s := h.online(); s.Info.IP != testIP.String() {
		t.Errorf("info = %+v", s.Info)
	}
}

func TestLastAddressIsTriedWhenMulticastIsFiltered(t *testing.T) {
	f := newFake()
	f.nodes = false
	h := start(t, f, func(c *Config) { c.LastIP = testIP.String() })
	h.online()
}

func TestSetManualIPReconnects(t *testing.T) {
	f := newFake()
	h := start(t, f, nil)
	h.online()
	f.mu.Lock()
	f.nodes = false
	f.mu.Unlock()
	h.m.SetManualIP("not an address")
	h.wait("the bad address", func(s Snapshot) bool { return s.Err != "" && s.Phase != PhaseOnline })
	h.m.SetManualIP(testIP.String())
	h.online()
}

func TestNotificationTriggersRead(t *testing.T) {
	f := newFake()
	h := start(t, f, func(c *Config) { c.PollVisible, c.PollHidden = time.Hour, time.Hour })
	h.online()
	f.mu.Lock()
	f.mode = byte(sharp.ModeHigh)
	f.mu.Unlock()
	f.inf <- echonet.Notification{IP: testIP, Frame: echonet.Frame{ESV: echonet.ESVInf}}
	h.wait("the change", func(s Snapshot) bool { return s.State.Mode == sharp.ModeHigh })
}

func TestNotificationFromElsewhereIsIgnored(t *testing.T) {
	f := newFake()
	h := start(t, f, func(c *Config) { c.PollVisible, c.PollHidden = time.Hour, time.Hour })
	h.online()
	f.inf <- echonet.Notification{IP: net.IPv4(192, 0, 2, 99)}
	select {
	case s := <-h.snaps:
		t.Fatalf("read after someone else's notification: %+v", s)
	case <-time.After(700 * time.Millisecond):
	}
}
