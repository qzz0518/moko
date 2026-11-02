// Package homekit publishes the purifier to Apple Home as one HAP accessory.
//
// HAP, rather than Matter, because Apple Home has a humidifier only in HAP: see
// docs/apple-home.md.
package homekit

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brutella/hap"
	"github.com/brutella/hap/accessory"
	"github.com/brutella/hap/characteristic"
	hlog "github.com/brutella/hap/log"
	"github.com/brutella/hap/service"

	"github.com/qzz0518/moko/internal/device"
	"github.com/qzz0518/moko/internal/i18n"
	"github.com/qzz0518/moko/internal/sharp"
)

func init() { hlog.Info.Disable() }

// Device is what Home controls. Its methods block until the device confirms.
type Device interface {
	SetPower(ctx context.Context, on bool) error
	SetMode(ctx context.Context, mode sharp.Mode) error
	SetHumidify(ctx context.Context, on bool) error
}

// How long Home waits for a change before it shows a failure. The longest path is
// turning on, then changing the mode, each read back.
const writeTimeout = 12 * time.Second

// Speeds of the Rotation Speed characteristic, one for each of the device's fan
// speeds.
const (
	speedSilent = 33.0
	speedMedium = 66.0
	speedHigh   = 100.0
)

// Status is what the app shows of the accessory.
type Status struct {
	Running  bool
	Paired   bool
	Code     string // "123-45-678"
	SetupURI string // what Home's camera reads, X-HM://…
	Err      string
}

// accessoryParts is the accessory and the characteristics Update changes.
type accessoryParts struct {
	a *accessory.A

	active     *characteristic.Active
	current    *characteristic.CurrentAirPurifierState
	target     *characteristic.TargetAirPurifierState
	speed      *characteristic.RotationSpeed
	humActive  *characteristic.Active
	humCurrent *characteristic.CurrentHumidifierDehumidifierState
	humTarget  *characteristic.TargetHumidifierDehumidifierState
	humidity   *characteristic.CurrentRelativeHumidity
	water      *characteristic.WaterLevel
	air        *characteristic.AirQuality
	pm25       *characteristic.PM2_5Density
	temp       *characteristic.CurrentTemperature
	night      *characteristic.On
	pollen     *characteristic.On
	filterLife *characteristic.FilterLifeLevel
	filterDue  *characteristic.FilterChangeIndication
}

// named gives a service the name Home shows for it. Since iOS 16 Home reads
// Configured Name.
func named(s *service.S, name string) {
	n, cn := characteristic.NewName(), characteristic.NewConfiguredName()
	n.SetValue(name)
	cn.SetValue(name)
	s.AddC(n.C)
	s.AddC(cn.C)
}

// fixIDs gives a service and its characteristics IDs that do not depend on what
// else the accessory has. Automations remember them, so they must never change:
// add new services under new bases and new characteristics at the end.
func fixIDs(s *service.S, base uint64) {
	s.Id = base
	for i, c := range s.Cs {
		c.Id = base + 1 + uint64(i)
	}
}

// build makes the accessory. The name is plain ASCII: it becomes the Bonjour name,
// and hap has an open issue about names in Chinese. What people see comes from
// the services' Configured Names.
func build(firmware, serial string) *accessoryParts {
	if firmware == "" {
		firmware = "1"
	}
	if serial == "" {
		serial = "KISX100"
	}
	a := accessory.New(accessory.Info{
		Name: "Moko", Manufacturer: "SHARP", Model: "KI-SX100",
		SerialNumber: serial, Firmware: firmware,
	}, accessory.TypeAirPurifier)
	a.Id = 1
	p := &accessoryParts{a: a}

	purifier := service.NewAirPurifier()
	purifier.Primary = true
	p.active, p.current, p.target = purifier.Active, purifier.CurrentAirPurifierState, purifier.TargetAirPurifierState
	p.speed = characteristic.NewRotationSpeed()
	purifier.AddC(p.speed.C)
	named(purifier.S, i18n.T("空气净化", "空気清浄", "Air Purifier"))
	fixIDs(purifier.S, 100)
	a.AddS(purifier.S)

	// Without a Relative Humidity Humidifier Threshold, because the device has no
	// target humidity to set: Home would show a slider that does nothing.
	hum := service.NewHumidifierDehumidifier()
	p.humActive, p.humCurrent, p.humTarget, p.humidity = hum.Active, hum.CurrentHumidifierDehumidifierState, hum.TargetHumidifierDehumidifierState, hum.CurrentRelativeHumidity
	p.humCurrent.ValidVals = []int{
		characteristic.CurrentHumidifierDehumidifierStateInactive,
		characteristic.CurrentHumidifierDehumidifierStateIdle,
		characteristic.CurrentHumidifierDehumidifierStateHumidifying,
	}
	p.humTarget.ValidVals = []int{characteristic.TargetHumidifierDehumidifierStateHumidifier}
	_ = p.humTarget.SetValue(characteristic.TargetHumidifierDehumidifierStateHumidifier)
	p.water = characteristic.NewWaterLevel()
	hum.AddC(p.water.C)
	named(hum.S, i18n.T("加湿", "加湿", "Humidifier"))
	fixIDs(hum.S, 200)
	a.AddS(hum.S)

	air := service.NewAirQualitySensor()
	p.air = air.AirQuality
	p.pm25 = characteristic.NewPM2_5Density()
	air.AddC(p.pm25.C)
	named(air.S, i18n.T("空气质量", "空気質", "Air Quality"))
	fixIDs(air.S, 300)
	a.AddS(air.S)

	temp := service.NewTemperatureSensor()
	p.temp = temp.CurrentTemperature
	named(temp.S, i18n.T("室温", "室温", "Temperature"))
	fixIDs(temp.S, 400)
	a.AddS(temp.S)

	night := service.NewSwitch()
	p.night = night.On
	named(night.S, i18n.T("夜间模式", "おやすみ", "Night Mode"))
	fixIDs(night.S, 500)
	a.AddS(night.S)

	pollen := service.NewSwitch()
	p.pollen = pollen.On
	named(pollen.S, i18n.T("花粉模式", "花粉", "Pollen Mode"))
	fixIDs(pollen.S, 600)
	a.AddS(pollen.S)

	// The dust filter stands for them all: Home shows one filter for a purifier.
	filter := service.NewFilterMaintenance()
	p.filterDue = filter.FilterChangeIndication
	p.filterLife = characteristic.NewFilterLifeLevel()
	filter.AddC(p.filterLife.C)
	named(filter.S, i18n.T("滤网", "フィルター", "Filter"))
	fixIDs(filter.S, 700)
	purifier.AddS(filter.S) // Home shows a filter with the purifier it is linked to
	a.AddS(filter.S)
	return p
}

// update sets the characteristics from the device's state. hap ignores a write
// equal to the value it holds, so these must follow the device, or Home's
// buttons stop doing anything.
func (p *accessoryParts) update(st device.State, lastManual sharp.Mode) {
	boolInt := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	_ = p.active.SetValue(boolInt(st.On))
	if st.On {
		_ = p.current.SetValue(characteristic.CurrentAirPurifierStatePurifyingAir)
	} else {
		_ = p.current.SetValue(characteristic.CurrentAirPurifierStateInactive)
	}
	if st.Mode.IsManual() {
		_ = p.target.SetValue(characteristic.TargetAirPurifierStateManual)
	} else {
		_ = p.target.SetValue(characteristic.TargetAirPurifierStateAuto)
	}
	p.speed.SetValue(speedOf(st.Mode, lastManual))

	active := st.On && st.Humidify
	_ = p.humActive.SetValue(boolInt(active))
	switch {
	case !active:
		_ = p.humCurrent.SetValue(characteristic.CurrentHumidifierDehumidifierStateInactive)
	case st.StatusOK && st.Humidifier == sharp.HumidifyActive:
		_ = p.humCurrent.SetValue(characteristic.CurrentHumidifierDehumidifierStateHumidifying)
	default:
		_ = p.humCurrent.SetValue(characteristic.CurrentHumidifierDehumidifierStateIdle)
	}
	if st.StatusOK {
		water := 100.0
		if !st.WaterOK || st.Humidifier == sharp.HumidifyNoWater {
			water = 0
		}
		p.water.SetValue(water)
	}
	if st.SensorsOK {
		if st.Sensors.HumidityOK {
			p.humidity.SetValue(float64(st.Sensors.Humidity))
		}
		if st.Sensors.TempOK {
			p.temp.SetValue(float64(st.Sensors.TempC))
		}
		if st.Sensors.PM25OK {
			_ = p.air.SetValue(AirQualityOf(st.Sensors.PM25))
			p.pm25.SetValue(float64(st.Sensors.PM25))
		} else {
			_ = p.air.SetValue(characteristic.AirQualityUnknown)
		}
	}
	p.night.SetValue(st.On && st.Mode == sharp.ModeNight)
	p.pollen.SetValue(st.On && st.Mode == sharp.ModePollen)
	if st.SensorsOK && st.LimitsOK {
		left := st.Limits.Left(st.Sensors.Used).Dust
		p.filterLife.SetValue(float64(left))
		due := characteristic.FilterChangeIndicationFilterOK
		if left <= filterDueAt {
			due = characteristic.FilterChangeIndicationChangeFilter
		}
		_ = p.filterDue.SetValue(due)
	}
}

// filterDueAt is the percent left of the dust filter at which Home says to change it.
const filterDueAt = 10

// AirQualityOf is the Air Quality characteristic's value for a PM2.5 reading in
// µg/m³.
func AirQualityOf(pm25 int) int {
	switch {
	case pm25 <= 12:
		return characteristic.AirQualityExcellent
	case pm25 <= 35:
		return characteristic.AirQualityGood
	case pm25 <= 55:
		return characteristic.AirQualityFair
	case pm25 <= 150:
		return characteristic.AirQualityInferior
	}
	return characteristic.AirQualityPoor
}

// speedOf shows a fan speed. The automatic modes have none of their own: they show
// the last speed chosen, so that dragging the slider starts from it.
func speedOf(m, lastManual sharp.Mode) float64 {
	if !m.IsManual() {
		m = lastManual
	}
	switch m {
	case sharp.ModeSilent:
		return speedSilent
	case sharp.ModeHigh:
		return speedHigh
	}
	return speedMedium
}

// modeOfSpeed is the fan speed nearest to where Home's slider stopped.
func modeOfSpeed(v float64) sharp.Mode {
	switch {
	case v <= (speedSilent+speedMedium)/2:
		return sharp.ModeSilent
	case v <= (speedMedium+speedHigh)/2:
		return sharp.ModeMedium
	}
	return sharp.ModeHigh
}

// Service runs the accessory.
type Service struct {
	dir string
	dev Device

	mu       sync.Mutex
	parts    *accessoryParts
	server   *hap.Server
	cancel   context.CancelFunc
	done     chan struct{} // closed when the last server has stopped
	state    device.State
	hasState bool
	manual   sharp.Mode // the last fan speed
	info     device.Info
	code     string
	setup    string
	lastErr  string
	onChange func()
}

// New makes a service keeping its pairings in dir. onChange runs, on any
// goroutine, when Status may have changed.
func New(dir string, dev Device, onChange func()) *Service {
	return &Service{dir: dir, dev: dev, manual: sharp.ModeMedium, onChange: onChange}
}

func (s *Service) changed() {
	if s.onChange != nil {
		s.onChange()
	}
}

// Start publishes the accessory, if it is not published already. Identity and
// pairings come from the store, so that a restart is the same accessory.
func (s *Service) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return nil
	}
	return s.startLocked()
}

func (s *Service) startLocked() error {
	store := hap.NewFsStore(s.dir)
	pin, setupID := setupCodes(store)
	p := build(s.info.Firmware, serialOf(s.info.NodeID))
	s.parts = p
	s.bindLocked(p)
	if s.hasState {
		p.update(s.state, s.manual)
	}
	server, err := hap.NewServer(store, p.a)
	if err != nil {
		s.lastErr = err.Error()
		return fmt.Errorf("homekit: %w", err)
	}
	server.Pin, server.SetupId = pin, setupID
	s.server = server
	s.code = pin[:3] + "-" + pin[3:5] + "-" + pin[5:]
	s.setup = setupURI(pin, p.a.Type, setupID)
	s.lastErr = ""

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	prev, done := s.done, make(chan struct{})
	s.done = done
	go func() {
		defer close(done)
		// Wait for the last server to go. The goodbye it sends over Bonjour has
		// the same name as this one, and arriving late it would remove this
		// announcement from the iPhone's cache: Home would not find the accessory.
		if prev != nil {
			<-prev
		}
		if ctx.Err() != nil {
			return
		}
		if err := server.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
			log.Printf("homekit: %v", err)
			s.mu.Lock()
			if s.server == server {
				s.server, s.parts = nil, nil
				s.lastErr = err.Error()
			}
			s.mu.Unlock()
			s.changed()
		}
	}()
	return nil
}

// serialOf is a short serial number from the device's ID, which is long and is not
// a secret, but need not be shown whole.
func serialOf(nodeID string) string {
	if len(nodeID) > 12 {
		return strings.ToUpper(nodeID[len(nodeID)-12:])
	}
	return strings.ToUpper(nodeID)
}

// Stop takes the accessory off the network, keeping its pairings.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

func (s *Service) stopLocked() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.server, s.parts = nil, nil
}

// Restart publishes the accessory again, as after the computer wakes or the network
// changes: dnssd does not notice those outside Linux.
func (s *Service) Restart() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil {
		return nil
	}
	s.stopLocked()
	return s.startLocked()
}

// Reset removes the accessory from every Home: its pairings, code and identity go.
// Otherwise, after removing it in Home while it was off, it would still think it was
// paired and refuse a new pairing. The code changes because the old one may have been
// seen, and the identity because a Home that still lists the old accessory conflicts.
func (s *Service) Reset() error {
	s.mu.Lock()
	s.stopLocked()
	done := s.done
	s.code, s.setup, s.lastErr = "", "", ""
	s.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			return errors.New("homekit: the accessory did not stop")
		}
	}
	return os.RemoveAll(s.dir)
}

// Status tells how the accessory is doing.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{Running: s.server != nil, Err: s.lastErr, Code: s.code, SetupURI: s.setup}
	if s.server != nil {
		st.Paired = s.server.IsPaired()
	}
	return st
}

// Update passes the device's state on to Home. Setting a characteristic writes to the
// connections of paired iPhones, which a stalled one could hold up, so it happens
// outside the lock that Status, and with it the UI, needs.
func (s *Service) Update(snap device.Snapshot) {
	if !snap.HasState {
		return
	}
	s.mu.Lock()
	s.state, s.hasState, s.info = snap.State, true, snap.Info
	if snap.State.Mode.IsManual() {
		s.manual = snap.State.Mode
	}
	parts, manual := s.parts, s.manual
	s.mu.Unlock()
	if parts != nil {
		parts.update(snap.State, manual)
	}
}

func (s *Service) current() (device.State, sharp.Mode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.manual
}

// bindLocked connects Home's writes to the device. Each write does the change and
// waits for the device to confirm it, so that a failure shows in Home.
func (s *Service) bindLocked(p *accessoryParts) {
	write := func(do func(ctx context.Context) error) (any, int) {
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		defer cancel()
		if err := do(ctx); err != nil {
			log.Printf("homekit: %v", err)
			return nil, hap.JsonStatusServiceCommunicationFailure
		}
		return nil, 0
	}
	p.active.SetValueRequestFunc = func(v any, _ *http.Request) (any, int) {
		return write(func(ctx context.Context) error { return s.dev.SetPower(ctx, asInt(v) == 1) })
	}
	p.target.SetValueRequestFunc = func(v any, _ *http.Request) (any, int) {
		return write(func(ctx context.Context) error {
			if asInt(v) == characteristic.TargetAirPurifierStateAuto {
				return s.dev.SetMode(ctx, sharp.ModeAuto)
			}
			_, manual := s.current()
			return s.dev.SetMode(ctx, manual)
		})
	}
	p.speed.SetValueRequestFunc = func(v any, _ *http.Request) (any, int) {
		speed := asFloat(v)
		return write(func(ctx context.Context) error {
			// Home sends a speed of zero as it turns a fan off, along with Active.
			if speed <= 0 {
				return nil
			}
			return s.dev.SetMode(ctx, modeOfSpeed(speed))
		})
	}
	p.humActive.SetValueRequestFunc = func(v any, _ *http.Request) (any, int) {
		return write(func(ctx context.Context) error { return s.dev.SetHumidify(ctx, asInt(v) == 1) })
	}
	modeSwitch := func(mode sharp.Mode) func(any, *http.Request) (any, int) {
		return func(v any, _ *http.Request) (any, int) {
			return write(func(ctx context.Context) error {
				if on, _ := v.(bool); on {
					return s.dev.SetMode(ctx, mode)
				}
				return s.dev.SetMode(ctx, sharp.ModeAuto)
			})
		}
	}
	p.night.SetValueRequestFunc = modeSwitch(sharp.ModeNight)
	p.pollen.SetValueRequestFunc = modeSwitch(sharp.ModePollen)
}

// Writes arrive in whichever JSON number type hap decoded them to.
func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case bool:
		if n {
			return 1
		}
	}
	return 0
}

func asFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

// setupCodes returns the pairing code and Setup ID of this computer: made at random
// once, then kept in the store, until Reset.
func setupCodes(st hap.Store) (pin, setupID string) {
	if b, err := st.Get("kisx100.pin"); err == nil && len(b) == 8 {
		pin = string(b)
	} else {
		for pin == "" || hap.InvalidPins[pin] {
			n, _ := rand.Int(rand.Reader, big.NewInt(1e8))
			pin = fmt.Sprintf("%08d", n)
		}
		_ = st.Set("kisx100.pin", []byte(pin))
	}
	if b, err := st.Get("kisx100.setupid"); err == nil && len(b) == 4 {
		setupID = string(b)
	} else {
		const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
		for range 4 {
			n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
			setupID += string(alphabet[n.Int64()])
		}
		_ = st.Set("kisx100.setupid", []byte(setupID))
	}
	return pin, setupID
}

// setupURI is what Home's camera reads (HAP specification): X-HM:// and nine base 36
// digits holding the accessory's category, an IP-only flag and the code, then the
// Setup ID, whose hash the accessory advertises so that Home knows which one it is.
func setupURI(pin string, category byte, setupID string) string {
	n, _ := strconv.ParseUint(pin, 10, 27)
	s := strings.ToUpper(strconv.FormatUint(uint64(category)<<31|2<<27|n, 36))
	return "X-HM://" + strings.Repeat("0", max(0, 9-len(s))) + s + setupID
}
