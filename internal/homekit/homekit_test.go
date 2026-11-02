package homekit

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/brutella/hap"
	"github.com/brutella/hap/characteristic"

	"github.com/qzz0518/moko/internal/device"
	"github.com/qzz0518/moko/internal/sharp"
)

type fakeDevice struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (d *fakeDevice) record(s string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, s)
	return d.err
}

func (d *fakeDevice) SetPower(_ context.Context, on bool) error {
	if on {
		return d.record("power on")
	}
	return d.record("power off")
}

func (d *fakeDevice) SetMode(_ context.Context, m sharp.Mode) error {
	return d.record("mode " + map[sharp.Mode]string{
		sharp.ModeAuto: "auto", sharp.ModeSilent: "silent", sharp.ModeMedium: "medium",
		sharp.ModeHigh: "high", sharp.ModeNight: "night", sharp.ModePollen: "pollen",
	}[m])
}

func (d *fakeDevice) SetHumidify(_ context.Context, on bool) error {
	if on {
		return d.record("humidify on")
	}
	return d.record("humidify off")
}

func TestAccessoryShape(t *testing.T) {
	p := build("104", "ABC")
	if p.a.Type != 19 { // air purifier
		t.Errorf("category = %d", p.a.Type)
	}
	seen := map[uint64]string{}
	types := map[string]int{}
	for _, s := range p.a.Ss {
		types[s.Type]++
		if prev, dup := seen[s.Id]; dup && s.Id != 0 {
			t.Errorf("service ID %d used by %s and %s", s.Id, prev, s.Type)
		}
		seen[s.Id] = s.Type
		for _, c := range s.Cs {
			if prev, dup := seen[c.Id]; dup && c.Id != 0 {
				t.Errorf("characteristic ID %d used by %s and %s", c.Id, prev, c.Type)
			}
			seen[c.Id] = c.Type
		}
	}
	for typ, want := range map[string]int{"BB": 1, "BD": 1, "8D": 1, "8A": 1, "49": 2} {
		if types[typ] != want {
			t.Errorf("services of type %s = %d, want %d", typ, types[typ], want)
		}
	}
	if types["BA"] != 1 {
		t.Errorf("filter maintenance services = %d, want 1", types["BA"])
	}
	if l := p.a.Ss[1].Linked; len(l) != 1 || l[0].Type != "BA" {
		t.Errorf("the purifier links %v, want the filter", l)
	}
	if !p.a.Ss[1].Primary {
		// Ss[0] is the accessory information.
		t.Error("the purifier must be the primary service")
	}
	for _, c := range p.a.Ss[2].Cs {
		if c.Type == characteristic.TypeRelativeHumidityHumidifierThreshold {
			t.Error("no target humidity exists to offer")
		}
	}
}

func TestIDsAreStable(t *testing.T) {
	p := build("1", "A")
	want := map[*characteristic.C]uint64{
		p.active.C:    101,
		p.current.C:   102,
		p.target.C:    103,
		p.speed.C:     104,
		p.humidity.C:  201,
		p.humActive.C: 204,
		p.water.C:     205,
		p.air.C:       301,
		p.pm25.C:      302,
		p.temp.C:      401,
		p.night.C:     501,
		p.pollen.C:    601,
	}
	for c, id := range want {
		if c.Id != id {
			t.Errorf("characteristic %s has ID %d, want %d: automations remember these", c.Type, c.Id, id)
		}
	}
}

func state() device.State {
	return device.State{
		On: true, Mode: sharp.ModeAuto, Humidify: true, Humidifier: sharp.HumidifyActive,
		WaterOK: true, StatusOK: true, SensorsOK: true,
		Sensors: sharp.Sensors{TempC: 24, TempOK: true, Humidity: 51, HumidityOK: true, PM25: 3, PM25OK: true},
	}
}

func TestUpdateFollowsTheDevice(t *testing.T) {
	p := build("1", "A")
	p.update(state(), sharp.ModeMedium)
	if p.active.Value() != 1 || p.current.Value() != characteristic.CurrentAirPurifierStatePurifyingAir {
		t.Errorf("purifier = %d, %d", p.active.Value(), p.current.Value())
	}
	if p.target.Value() != characteristic.TargetAirPurifierStateAuto {
		t.Errorf("target = %d", p.target.Value())
	}
	if p.humActive.Value() != 1 || p.humCurrent.Value() != characteristic.CurrentHumidifierDehumidifierStateHumidifying {
		t.Errorf("humidifier = %d, %d", p.humActive.Value(), p.humCurrent.Value())
	}
	if p.humidity.Value() != 51 || p.temp.Value() != 24 || p.pm25.Value() != 3 || p.water.Value() != 100 {
		t.Errorf("readings = %v %v %v %v", p.humidity.Value(), p.temp.Value(), p.pm25.Value(), p.water.Value())
	}
	if p.air.Value() != characteristic.AirQualityExcellent || p.night.Value() || p.pollen.Value() {
		t.Errorf("air = %d, night = %v, pollen = %v", p.air.Value(), p.night.Value(), p.pollen.Value())
	}

	st := state()
	st.Mode = sharp.ModeHigh
	p.update(st, sharp.ModeHigh)
	if p.target.Value() != characteristic.TargetAirPurifierStateManual || p.speed.Value() != speedHigh {
		t.Errorf("manual: target = %d, speed = %v", p.target.Value(), p.speed.Value())
	}

	st = state()
	st.Mode = sharp.ModeNight
	p.update(st, sharp.ModeHigh)
	if !p.night.Value() || p.pollen.Value() {
		t.Error("night switch")
	}

	st = state()
	st.On = false
	st.Mode = sharp.ModeNight
	st.Humidifier = sharp.HumidifyOff
	p.update(st, sharp.ModeHigh)
	if p.active.Value() != 0 || p.current.Value() != characteristic.CurrentAirPurifierStateInactive {
		t.Error("off: purifier")
	}
	if p.humActive.Value() != 0 || p.humCurrent.Value() != characteristic.CurrentHumidifierDehumidifierStateInactive {
		t.Error("off: a humidifier cannot run while the device is off")
	}
	if p.night.Value() {
		t.Error("off: night switch")
	}

	st = state()
	st.Humidifier = sharp.HumidifyNoWater
	st.WaterOK = false
	p.update(st, sharp.ModeHigh)
	if p.water.Value() != 0 || p.humCurrent.Value() != characteristic.CurrentHumidifierDehumidifierStateIdle {
		t.Errorf("no water: %v, %d", p.water.Value(), p.humCurrent.Value())
	}

	st = state()
	st.Sensors.PM25OK = false
	p.update(st, sharp.ModeHigh)
	if p.air.Value() != characteristic.AirQualityUnknown {
		t.Errorf("unknown PM2.5: %d", p.air.Value())
	}
}

func TestAirQualityBands(t *testing.T) {
	for pm, want := range map[int]int{
		0: characteristic.AirQualityExcellent, 12: characteristic.AirQualityExcellent,
		13: characteristic.AirQualityGood, 35: characteristic.AirQualityGood,
		36: characteristic.AirQualityFair, 55: characteristic.AirQualityFair,
		56: characteristic.AirQualityInferior, 150: characteristic.AirQualityInferior,
		151: characteristic.AirQualityPoor,
	} {
		if got := AirQualityOf(pm); got != want {
			t.Errorf("PM2.5 %d: %d, want %d", pm, got, want)
		}
	}
}

func TestSpeeds(t *testing.T) {
	for v, want := range map[float64]sharp.Mode{
		1: sharp.ModeSilent, 33: sharp.ModeSilent, 49: sharp.ModeSilent,
		50: sharp.ModeMedium, 66: sharp.ModeMedium, 83: sharp.ModeMedium,
		84: sharp.ModeHigh, 100: sharp.ModeHigh,
	} {
		if got := modeOfSpeed(v); got != want {
			t.Errorf("speed %v: %#x, want %#x", v, got, want)
		}
	}
	for _, m := range []sharp.Mode{sharp.ModeSilent, sharp.ModeMedium, sharp.ModeHigh} {
		if got := modeOfSpeed(speedOf(m, 0)); got != m {
			t.Errorf("mode %#x does not survive the round trip: %#x", m, got)
		}
	}
}

func newService(t *testing.T) (*Service, *fakeDevice, *accessoryParts) {
	t.Helper()
	d := &fakeDevice{}
	s := New(filepath.Join(t.TempDir(), "homekit"), d, nil)
	p := build("1", "A")
	s.bindLocked(p)
	s.parts = p
	return s, d, p
}

func TestHomeWritesReachTheDevice(t *testing.T) {
	s, d, p := newService(t)
	s.Update(device.Snapshot{HasState: true, State: state()})
	for _, tc := range []struct {
		name string
		call func() (any, int)
		want string
	}{
		{"on", func() (any, int) { return p.active.SetValueRequestFunc(1, nil) }, "power on"},
		{"off", func() (any, int) { return p.active.SetValueRequestFunc(0, nil) }, "power off"},
		{"auto", func() (any, int) {
			return p.target.SetValueRequestFunc(characteristic.TargetAirPurifierStateAuto, nil)
		}, "mode auto"},
		{"manual", func() (any, int) {
			return p.target.SetValueRequestFunc(characteristic.TargetAirPurifierStateManual, nil)
		}, "mode medium"},
		{"slider low", func() (any, int) { return p.speed.SetValueRequestFunc(20.0, nil) }, "mode silent"},
		{"slider high", func() (any, int) { return p.speed.SetValueRequestFunc(95.0, nil) }, "mode high"},
		{"humidify", func() (any, int) { return p.humActive.SetValueRequestFunc(1, nil) }, "humidify on"},
		{"no humidify", func() (any, int) { return p.humActive.SetValueRequestFunc(0, nil) }, "humidify off"},
		{"night", func() (any, int) { return p.night.SetValueRequestFunc(true, nil) }, "mode night"},
		{"pollen", func() (any, int) { return p.pollen.SetValueRequestFunc(true, nil) }, "mode pollen"},
		{"pollen off", func() (any, int) { return p.pollen.SetValueRequestFunc(false, nil) }, "mode auto"},
	} {
		d.mu.Lock()
		d.calls = nil
		d.mu.Unlock()
		if _, code := tc.call(); code != 0 {
			t.Errorf("%s: code %d", tc.name, code)
		}
		d.mu.Lock()
		if len(d.calls) != 1 || d.calls[0] != tc.want {
			t.Errorf("%s: calls = %v, want %s", tc.name, d.calls, tc.want)
		}
		d.mu.Unlock()
	}
}

func TestSliderToZeroDoesNotChangeTheMode(t *testing.T) {
	_, d, p := newService(t)
	if _, code := p.speed.SetValueRequestFunc(0.0, nil); code != 0 {
		t.Errorf("code %d", code)
	}
	if len(d.calls) != 0 {
		t.Errorf("calls = %v", d.calls)
	}
}

func TestManualRemembersTheLastSpeed(t *testing.T) {
	s, d, p := newService(t)
	st := state()
	st.Mode = sharp.ModeHigh
	s.Update(device.Snapshot{HasState: true, State: st})
	st.Mode = sharp.ModeAuto
	s.Update(device.Snapshot{HasState: true, State: st})
	if p.speed.Value() != speedHigh {
		t.Errorf("speed in auto = %v", p.speed.Value())
	}
	p.target.SetValueRequestFunc(characteristic.TargetAirPurifierStateManual, nil)
	if d.calls[0] != "mode high" {
		t.Errorf("calls = %v", d.calls)
	}
}

func TestFailureShowsInHome(t *testing.T) {
	_, d, p := newService(t)
	d.err = errors.New("offline")
	if _, code := p.active.SetValueRequestFunc(1, nil); code != hap.JsonStatusServiceCommunicationFailure {
		t.Errorf("code = %d", code)
	}
}

func TestSetupURI(t *testing.T) {
	// The example in the HAP specification: a code, category 19, IP only.
	got := setupURI("12345678", 19, "ABCD")
	if !strings.HasPrefix(got, "X-HM://") || !strings.HasSuffix(got, "ABCD") || len(got) != len("X-HM://")+9+4 {
		t.Fatalf("URI = %q", got)
	}
}

func TestCodesLastUntilReset(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "homekit")
	store := hap.NewFsStore(dir)
	pin, id := setupCodes(store)
	if len(pin) != 8 || len(id) != 4 || hap.InvalidPins[pin] {
		t.Fatalf("pin = %q, id = %q", pin, id)
	}
	if pin2, id2 := setupCodes(store); pin2 != pin || id2 != id {
		t.Error("codes changed")
	}
	s := New(dir, &fakeDevice{}, nil)
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	if pin3, _ := setupCodes(hap.NewFsStore(dir)); pin3 == pin {
		t.Error("the code survived a reset")
	}
}

func TestStartAndStop(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "homekit"), &fakeDevice{}, nil)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if !st.Running || st.Paired || len(st.Code) != 10 || !strings.HasPrefix(st.SetupURI, "X-HM://") {
		t.Errorf("status = %+v", st)
	}
	s.Update(device.Snapshot{HasState: true, State: state()}) // while running
	if err := s.Restart(); err != nil {
		t.Fatal(err)
	}
	s.Stop()
	if s.Status().Running {
		t.Error("still running")
	}
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
}

func TestFilterLifeFollowsTheDustFilter(t *testing.T) {
	p := build("1", "x")
	st := device.State{SensorsOK: true, LimitsOK: true}
	st.Limits = sharp.Limits{Dust: 3000}
	st.Sensors.Used = sharp.Supplies{Dust: 83}
	p.update(st, sharp.ModeSilent)
	if got := p.filterLife.Value(); got != 97 {
		t.Errorf("life = %v", got)
	}
	if got := p.filterDue.Value(); got != characteristic.FilterChangeIndicationFilterOK {
		t.Errorf("indication = %v", got)
	}
	st.Sensors.Used = sharp.Supplies{Dust: 2900}
	p.update(st, sharp.ModeSilent)
	if got := p.filterDue.Value(); got != characteristic.FilterChangeIndicationChangeFilter {
		t.Errorf("nearly worn out: indication = %v", got)
	}
}
