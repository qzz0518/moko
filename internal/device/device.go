// Package device owns the connection to the purifier. One goroutine does all the
// talking, because the device handles one request at a time: it finds the device,
// polls its state, reacts to notifications and runs commands, each of which is
// read back before it counts as done.
package device

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/qzz0518/moko/internal/echonet"
	"github.com/qzz0518/moko/internal/sharp"
)

// Phase is where the connection is.
type Phase int

// The phases.
const (
	PhaseSearching Phase = iota // never reached it since starting
	PhaseOnline
	PhaseOffline // was reached, and stopped answering
)

// Errors of commands.
var (
	ErrOffline     = errors.New("device: not connected")
	ErrUnconfirmed = errors.New("device: the device did not confirm the change")
)

// Info identifies the device.
type Info struct {
	IP       string
	Model    string // of the purifier, e.g. "KISX100"
	Module   string // of its wireless module, e.g. "HW-A04"
	NodeID   string
	Firmware string
}

// State is what the device last reported.
type State struct {
	On         bool
	Mode       sharp.Mode
	Humidify   bool
	Humidifier sharp.HumidifyState
	WaterOK    bool
	StatusOK   bool // Humidifier and WaterOK were read

	Sensors   sharp.Sensors
	SensorsOK bool
	Limits    sharp.Limits // of the consumables
	LimitsOK  bool

	NeedsCleaning bool

	Watts    int
	WattsOK  bool
	EnergyWh uint32
	EnergyOK bool

	Polluted bool
	Fault    bool
}

// Snapshot is the connection and the state at a moment.
type Snapshot struct {
	Phase     Phase
	Info      Info
	State     State
	HasState  bool
	UpdatedAt time.Time // of State
	Err       string    // why the device cannot be reached, when known
}

// Transport is what the manager needs of an ECHONET Lite client.
type Transport interface {
	Get(ctx context.Context, ip net.IP, eoj echonet.EOJ, epc byte) ([]byte, error)
	Set(ctx context.Context, ip net.IP, eoj echonet.EOJ, epc byte, edt []byte) error
	Discover(ctx context.Context) ([]echonet.Node, error)
	Notifications() <-chan echonet.Notification
}

// Config sets up a manager. Zero durations take their defaults.
type Config struct {
	ManualIP string // skips discovery
	LastIP   string // tried when discovery finds nothing
	NodeID   string // preferred when several purifiers answer

	// OnChange gets every new snapshot, on the manager's goroutine.
	OnChange func(Snapshot)

	PollVisible    time.Duration // while a window shows the readings
	PollHidden     time.Duration
	RequestGap     time.Duration // between two requests
	DiscoverTime   time.Duration
	RetryMin       time.Duration // between attempts to find the device
	RetryMax       time.Duration
	ConfirmTimeout time.Duration
	ConfirmEvery   time.Duration
	EnergyEvery    time.Duration
}

func (c *Config) defaults() {
	def := func(d *time.Duration, v time.Duration) {
		if *d == 0 {
			*d = v
		}
	}
	def(&c.PollVisible, 10*time.Second)
	def(&c.PollHidden, 30*time.Second)
	def(&c.RequestGap, 100*time.Millisecond)
	def(&c.DiscoverTime, 1500*time.Millisecond)
	def(&c.RetryMin, 5*time.Second)
	def(&c.RetryMax, time.Minute)
	def(&c.ConfirmTimeout, 6*time.Second)
	def(&c.ConfirmEvery, 300*time.Millisecond)
	def(&c.EnergyEvery, 5*time.Minute)
}

type command struct {
	run  func(ctx context.Context) error
	done chan error
}

// Manager is the connection to one device.
type Manager struct {
	t   Transport
	cfg Config

	cmds    chan command
	refresh chan struct{}
	visible chan bool
	target  chan string

	// The rest belongs to Run's goroutine.
	ip         net.IP
	eoj        echonet.EOJ
	snap       Snapshot
	failures   int
	lastEnergy time.Time
	fast       bool
}

// New makes a manager. Call Run to start it.
func New(t Transport, cfg Config) *Manager {
	cfg.defaults()
	return &Manager{
		t: t, cfg: cfg,
		cmds:    make(chan command),
		refresh: make(chan struct{}, 1),
		visible: make(chan bool, 1),
		target:  make(chan string, 1),
	}
}

// Refresh asks for fresh readings, or a new search if the device is lost: after
// the computer wakes up or the network changes.
func (m *Manager) Refresh() {
	select {
	case m.refresh <- struct{}{}:
	default:
	}
}

// SetVisible says whether a window shows the readings, to poll faster while one does.
func (m *Manager) SetVisible(v bool) {
	select {
	case <-m.visible:
	default:
	}
	m.visible <- v
}

// SetManualIP connects to this address from now on, or searches again when empty.
func (m *Manager) SetManualIP(ip string) {
	select {
	case <-m.target:
	default:
	}
	m.target <- ip
}

// SetPower turns the device on or off and waits until it reports so.
func (m *Manager) SetPower(ctx context.Context, on bool) error {
	return m.do(ctx, func(ctx context.Context) error { return m.setPower(ctx, on) })
}

// SetMode changes the mode, turning the device on first if it is off.
func (m *Manager) SetMode(ctx context.Context, mode sharp.Mode) error {
	return m.do(ctx, func(ctx context.Context) error { return m.setMode(ctx, mode) })
}

// SetHumidify turns humidifying on, turning the device on first if it is off, or
// off, which an off device already is.
func (m *Manager) SetHumidify(ctx context.Context, on bool) error {
	return m.do(ctx, func(ctx context.Context) error { return m.setHumidify(ctx, on) })
}

func (m *Manager) do(ctx context.Context, run func(ctx context.Context) error) error {
	c := command{run: run, done: make(chan error, 1)}
	select {
	case m.cmds <- c:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-c.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Run talks to the device until the context ends.
func (m *Manager) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if m.ip == nil {
			m.find(ctx)
			continue
		}
		m.serve(ctx)
	}
}

// find looks for the device until it is found, serving commands with an error
// meanwhile.
func (m *Manager) find(ctx context.Context) {
	wait := m.cfg.RetryMin
	for ctx.Err() == nil {
		err := m.connect(ctx)
		if err == nil {
			m.failures = 0
			m.lastEnergy = time.Time{}
			m.snap.Err = ""
			m.poll(ctx)
			return
		}
		m.snap.Err = err.Error()
		m.publish()
		timer := time.NewTimer(wait)
		wait = min(wait*2, m.cfg.RetryMax)
	waiting:
		for {
			select {
			case c := <-m.cmds:
				c.done <- ErrOffline
			case v := <-m.visible:
				m.fast = v
			case ip := <-m.target:
				m.cfg.ManualIP = ip
				wait = m.cfg.RetryMin
				break waiting
			case <-m.refresh:
				wait = m.cfg.RetryMin
				break waiting
			case <-m.t.Notifications():
				// The device may just have come back.
			case <-timer.C:
				break waiting
			case <-ctx.Done():
				timer.Stop()
				return
			}
		}
		timer.Stop()
	}
}

// serve polls and runs commands until the device stops answering.
func (m *Manager) serve(ctx context.Context) {
	timer := time.NewTimer(m.pollEvery())
	defer timer.Stop()
	var soon <-chan time.Time // a notification's refresh, batched
	for m.ip != nil && ctx.Err() == nil {
		select {
		case c := <-m.cmds:
			// Read before answering, so that whoever waits sees the new state as soon
			// as it hears the command finished.
			err := c.run(ctx)
			m.poll(ctx)
			c.done <- err
		case <-m.refresh:
			m.poll(ctx)
		case v := <-m.visible:
			wasFast := m.fast
			m.fast = v
			if v && !wasFast {
				m.poll(ctx)
			}
		case ip := <-m.target:
			m.cfg.ManualIP = ip
			m.lose("")
			return
		case n := <-m.t.Notifications():
			if n.IP != nil && n.IP.Equal(m.ip) && soon == nil {
				soon = time.After(500 * time.Millisecond)
			}
			continue
		case <-soon:
			soon = nil
			m.poll(ctx)
		case <-timer.C:
			m.poll(ctx)
		case <-ctx.Done():
			return
		}
		timer.Reset(m.pollEvery())
	}
}

func (m *Manager) pollEvery() time.Duration {
	if m.fast {
		return m.cfg.PollVisible
	}
	return m.cfg.PollHidden
}

// lose forgets the connection and keeps the last readings on show.
func (m *Manager) lose(reason string) {
	m.ip = nil
	if m.snap.Phase == PhaseOnline {
		m.snap.Phase = PhaseOffline
	}
	m.snap.Err = reason
	m.publish()
}

func (m *Manager) publish() {
	if m.cfg.OnChange != nil {
		m.cfg.OnChange(m.snap)
	}
}

// connect finds the device and identifies it.
func (m *Manager) connect(ctx context.Context) error {
	if m.cfg.ManualIP != "" {
		ip := net.ParseIP(m.cfg.ManualIP)
		if ip == nil {
			return fmt.Errorf("invalid address %q", m.cfg.ManualIP)
		}
		return m.identifyAny(ctx, ip)
	}
	dctx, cancel := context.WithTimeout(ctx, m.cfg.DiscoverTime)
	nodes, err := m.t.Discover(dctx)
	cancel()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var found []candidate
	for _, n := range nodes {
		for _, eoj := range n.Instances {
			if eoj.IsAirCleaner() {
				found = append(found, candidate{n.IP, eoj})
			}
		}
	}
	var lastErr error
	for _, c := range found {
		info, err := m.identify(ctx, c.ip, c.eoj)
		if err != nil {
			lastErr = err
			continue
		}
		if m.cfg.NodeID != "" && info.NodeID != m.cfg.NodeID && len(found) > 1 {
			continue
		}
		m.adopt(c.ip, c.eoj, info)
		return nil
	}
	// Several purifiers, none the remembered one: take the first that answers.
	for _, c := range found {
		if info, err := m.identify(ctx, c.ip, c.eoj); err == nil {
			m.adopt(c.ip, c.eoj, info)
			return nil
		}
	}
	// Some networks filter multicast: try where it was last time.
	if m.cfg.LastIP != "" {
		if ip := net.ParseIP(m.cfg.LastIP); ip != nil {
			if m.identifyAny(ctx, ip) == nil {
				return nil
			}
		}
	}
	if err != nil {
		return err
	}
	if lastErr != nil {
		return lastErr
	}
	return errNotFound
}

var errNotFound = errors.New("no Sharp air purifier answered on the network")

type candidate struct {
	ip  net.IP
	eoj echonet.EOJ
}

// identifyAny connects to a known address, whose instance is asked for.
func (m *Manager) identifyAny(ctx context.Context, ip net.IP) error {
	list, err := m.t.Get(ctx, ip, echonet.NodeProfile, 0xD6)
	if err != nil {
		return err
	}
	for _, eoj := range echonet.InstanceList(list) {
		if eoj.IsAirCleaner() {
			info, err := m.identify(ctx, ip, eoj)
			if err != nil {
				return err
			}
			m.adopt(ip, eoj, info)
			return nil
		}
	}
	return fmt.Errorf("%s is not an air purifier", ip)
}

func (m *Manager) adopt(ip net.IP, eoj echonet.EOJ, info Info) {
	m.ip, m.eoj = ip, eoj
	m.snap.Info = info
}

func (m *Manager) identify(ctx context.Context, ip net.IP, eoj echonet.EOJ) (Info, error) {
	maker, err := m.t.Get(ctx, ip, eoj, sharp.EPCMaker)
	if err != nil {
		return Info{}, err
	}
	if len(maker) != 3 || [3]byte(maker) != sharp.MakerSharp {
		return Info{}, fmt.Errorf("%s is not made by Sharp", ip)
	}
	info := Info{IP: ip.String()}
	if b, err := m.t.Get(ctx, ip, eoj, sharp.EPCModel); err == nil {
		info.Model = text(b)
	}
	if b, err := m.t.Get(ctx, ip, eoj, sharp.EPCFirmware); err == nil {
		info.Firmware = text(b)
	}
	if b, err := m.t.Get(ctx, ip, echonet.NodeProfile, sharp.EPCModel); err == nil {
		info.Module = text(b)
	}
	if b, err := m.t.Get(ctx, ip, echonet.NodeProfile, sharp.EPCNodeID); err == nil {
		info.NodeID = fmt.Sprintf("%x", b)
	}
	return info, nil
}

func text(b []byte) string { return strings.TrimRight(string(b), "\x00 ") }

// get reads a property of the device, leaving the device room between requests.
func (m *Manager) get(ctx context.Context, epc byte) ([]byte, error) {
	b, err := m.t.Get(ctx, m.ip, m.eoj, epc)
	if m.cfg.RequestGap > 0 {
		select {
		case <-time.After(m.cfg.RequestGap):
		case <-ctx.Done():
		}
	}
	return b, err
}

// poll reads the state and publishes it. A property the device refuses or
// reports in an unknown layout leaves that part unknown; no answer at all, twice
// in a row, means the device is gone.
func (m *Manager) poll(ctx context.Context) {
	st, ok := m.readState(ctx)
	if ctx.Err() != nil {
		return
	}
	if !ok {
		m.failures++
		if m.failures >= 2 {
			m.lose(ErrOffline.Error())
		}
		return
	}
	m.failures = 0
	m.snap.Phase = PhaseOnline
	m.snap.State = st
	m.snap.HasState = true
	m.snap.UpdatedAt = time.Now()
	m.snap.Err = ""
	m.publish()
}

func (m *Manager) readState(ctx context.Context) (State, bool) {
	prev := m.snap.State
	var st State
	timedOut := false
	read := func(epc byte) []byte {
		b, err := m.get(ctx, epc)
		if err != nil {
			if !errors.Is(err, echonet.ErrRefused) {
				timedOut = true
			}
			return nil
		}
		return b
	}
	b := read(sharp.EPCPower)
	if timedOut {
		return prev, false
	}
	st.On = len(b) == 1 && b[0] == 0x30
	if b := read(sharp.EPCSettings); b != nil {
		if s, err := sharp.DecodeSettings(b); err == nil {
			st.Mode, st.Humidify = s.Mode, s.Humidify
		} else {
			log.Print(err)
			st.Mode, st.Humidify = prev.Mode, prev.Humidify
		}
	}
	if b := read(sharp.EPCStatus); b != nil {
		if s, err := sharp.DecodeStatus(b); err == nil {
			st.StatusOK, st.WaterOK, st.Humidifier, st.NeedsCleaning = true, s.WaterOK, s.Humidify, s.NeedsCleaning
		} else {
			log.Print(err)
		}
	}
	if b := read(sharp.EPCSensors); b != nil {
		if s, err := sharp.DecodeSensors(b); err == nil {
			st.SensorsOK, st.Sensors = true, s
		} else {
			log.Print(err)
		}
	}
	if b := read(sharp.EPCWatts); len(b) == 2 {
		st.WattsOK, st.Watts = true, int(b[0])<<8|int(b[1])
	}
	if b := read(sharp.EPCPollution); len(b) == 1 {
		st.Polluted = b[0] == 0x41
	}
	if b := read(sharp.EPCFault); len(b) == 1 {
		st.Fault = b[0] == 0x41
	}
	st.LimitsOK, st.Limits = prev.LimitsOK, prev.Limits
	if !st.LimitsOK { // they are the same for a model: once is enough
		if b := read(sharp.EPCLimits); b != nil {
			if l, err := sharp.DecodeLimits(b); err == nil {
				st.LimitsOK, st.Limits = true, l
			} else {
				log.Print(err)
			}
		}
	}
	st.EnergyOK, st.EnergyWh = prev.EnergyOK, prev.EnergyWh
	if m.lastEnergy.IsZero() || time.Since(m.lastEnergy) >= m.cfg.EnergyEvery {
		if b := read(sharp.EPCEnergy); len(b) == 4 {
			st.EnergyOK = true
			st.EnergyWh = uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
			m.lastEnergy = time.Now()
		}
	}
	return st, !timedOut
}

// confirm reads back until check holds.
func (m *Manager) confirm(ctx context.Context, epc byte, check func([]byte) bool) error {
	ctx, cancel := context.WithTimeout(ctx, m.cfg.ConfirmTimeout)
	defer cancel()
	for {
		b, err := m.t.Get(ctx, m.ip, m.eoj, epc)
		if err == nil && check(b) {
			return nil
		}
		select {
		case <-time.After(m.cfg.ConfirmEvery):
		case <-ctx.Done():
			return ErrUnconfirmed
		}
	}
}

func (m *Manager) setPower(ctx context.Context, on bool) error {
	isOn := func(b []byte) bool { return len(b) == 1 && (b[0] == 0x30) == on }
	err := m.t.Set(ctx, m.ip, m.eoj, sharp.EPCPower, sharp.PowerEDT(on))
	if err == nil {
		err = m.confirm(ctx, sharp.EPCPower, isOn)
	}
	if err == nil {
		m.snap.State.On = on
		return nil
	}
	if !errors.Is(err, ErrUnconfirmed) && !errors.Is(err, echonet.ErrRefused) {
		return err
	}
	// The standard property did not do it: try the way Sharp's cloud API does.
	if err := m.t.Set(ctx, m.ip, m.eoj, sharp.EPCSettings, sharp.PowerCommand(on)); err != nil {
		return err
	}
	if err := m.confirm(ctx, sharp.EPCPower, isOn); err != nil {
		return err
	}
	m.snap.State.On = on
	return nil
}

func (m *Manager) ensureOn(ctx context.Context) error {
	b, err := m.t.Get(ctx, m.ip, m.eoj, sharp.EPCPower)
	if err != nil {
		return err
	}
	if len(b) == 1 && b[0] == 0x30 {
		return nil
	}
	return m.setPower(ctx, true)
}

func (m *Manager) setMode(ctx context.Context, mode sharp.Mode) error {
	if err := m.ensureOn(ctx); err != nil {
		return err
	}
	if err := m.t.Set(ctx, m.ip, m.eoj, sharp.EPCSettings, sharp.ModeCommand(mode)); err != nil {
		return err
	}
	return m.confirm(ctx, sharp.EPCSettings, func(b []byte) bool {
		s, err := sharp.DecodeSettings(b)
		return err == nil && s.Mode == mode
	})
}

func (m *Manager) setHumidify(ctx context.Context, on bool) error {
	if on {
		if err := m.ensureOn(ctx); err != nil {
			return err
		}
	}
	if err := m.t.Set(ctx, m.ip, m.eoj, sharp.EPCSettings, sharp.HumidifyCommand(on)); err != nil {
		return err
	}
	return m.confirm(ctx, sharp.EPCSettings, func(b []byte) bool {
		s, err := sharp.DecodeSettings(b)
		return err == nil && s.Humidify == on
	})
}
