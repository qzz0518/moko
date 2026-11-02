package sharp

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Recorded from a KI-SX100 on 2026-11-02.
const (
	recF1      = "6200c41833070000f06540000d1e7637e737e702d0001a4204000300000052005200000056560068"
	recF2      = "00200000000000000000000000000000000001ffff00000181ff001001000000000000ff00ff0000"
	recF2HumOf = "00240000000000000000000000000000000002ffff00000100ff001001000000000000ff00ff0000"
	recF3      = "00000000100000000000000000ff00ff0000000000000000000000"
	recF3Inf   = "01080000100000000000000000ff00000000000000000000000000"
)

func TestDecodeSensorsRecorded(t *testing.T) {
	s, err := DecodeSensors(unhex(t, recF1))
	if err != nil {
		t.Fatal(err)
	}
	if !s.TempOK || s.TempC != 24 {
		t.Errorf("temperature = %d, %v", s.TempC, s.TempOK)
	}
	if !s.HumidityOK || s.Humidity != 51 {
		t.Errorf("humidity = %d, %v", s.Humidity, s.HumidityOK)
	}
	if !s.PM25OK || s.PM25 != 0 {
		t.Errorf("PM2.5 = %d, %v", s.PM25, s.PM25OK)
	}
	if s.AirCleaned != 0x001A4204 {
		t.Errorf("air cleaned = %#x", s.AirCleaned)
	}
}

func TestDecodeSensorsLimits(t *testing.T) {
	base := unhex(t, recF1)
	for _, tc := range []struct {
		name  string
		edit  func(b []byte)
		check func(s Sensors) bool
	}{
		{"cold", func(b []byte) { b[3] = 0xFF }, func(s Sensors) bool { return !s.TempOK && s.TempC == -1 }},
		{"hot", func(b []byte) { b[3] = 51 }, func(s Sensors) bool { return !s.TempOK }},
		{"dry floor", func(b []byte) { b[4] = 6 }, func(s Sensors) bool { return !s.HumidityOK }},
		{"wet ceiling", func(b []byte) { b[4] = 99 }, func(s Sensors) bool { return !s.HumidityOK }},
		{"pm valid", func(b []byte) { b[27], b[28] = 0x00, 0x23 }, func(s Sensors) bool { return s.PM25OK && s.PM25 == 35 }},
		{"pm invalid flag", func(b []byte) { b[27], b[28] = 0x80, 0x23 }, func(s Sensors) bool { return !s.PM25OK }},
		{"pm out of range", func(b []byte) { b[27], b[28] = 0x01, 0xF4 }, func(s Sensors) bool { return !s.PM25OK }},
	} {
		b := bytes.Clone(base)
		tc.edit(b)
		s, err := DecodeSensors(b)
		if err != nil || !tc.check(s) {
			t.Errorf("%s: %+v, %v", tc.name, s, err)
		}
	}
}

// What the device's F0 and F1 said on 2026-11-03, against what COCORO AIR showed at the
// same time: 97, 97, 93, 61 and 24 percent left. Only the limits of F0 are kept here.
func TestSuppliesLeftMatchTheOfficialApp(t *testing.T) {
	f0 := make([]byte, 32)
	copy(f0[12:], unhex(t, "0bb80bb804e2e14a38"))
	limits, err := DecodeLimits(f0)
	if err != nil {
		t.Fatal(err)
	}
	s, err := DecodeSensors(unhex(t, "6400c51931090000f06540000d1fe737ed37ed02d0001a46ad020000000053005300000056560069"))
	if err != nil {
		t.Fatal(err)
	}
	if want := (Supplies{Dust: 97, Odor: 97, Humidifying: 93, Silver: 61, Ion: 24}); limits.Left(s.Used) != want {
		t.Errorf("left = %+v, want %+v", limits.Left(s.Used), want)
	}
	if s.AirCleaned != 1722029 {
		t.Errorf("air cleaned = %d", s.AirCleaned)
	}
}

func TestSuppliesLeftStayInRange(t *testing.T) {
	l := Limits{Dust: 100, Odor: 0}
	got := l.Left(Supplies{Dust: 150, Odor: 5})
	if got.Dust != 0 || got.Odor != 100 {
		t.Errorf("left = %+v", got)
	}
	if _, err := DecodeLimits(make([]byte, 20)); err == nil {
		t.Error("a short F0 was taken")
	}
}

func TestDecodeStatusRecorded(t *testing.T) {
	s, err := DecodeStatus(unhex(t, recF2))
	if err != nil {
		t.Fatal(err)
	}
	if !s.WaterOK || s.Humidify != HumidifyActive || s.Mode != ModeAuto {
		t.Errorf("status = %+v", s)
	}
	s, err = DecodeStatus(unhex(t, recF2HumOf))
	if err != nil {
		t.Fatal(err)
	}
	if s.Humidify != HumidifyOff {
		t.Errorf("humidifier off reads %v", s.Humidify)
	}
}

func TestDecodeStatusHumidifierStates(t *testing.T) {
	for b, want := range map[byte]HumidifyState{
		0x00: HumidifyOff, 0x80: HumidifyIdle, 0x81: HumidifyActive, 0x82: HumidifyNoWater,
	} {
		raw := unhex(t, recF2)
		raw[24] = b
		s, err := DecodeStatus(raw)
		if err != nil || s.Humidify != want {
			t.Errorf("%#x reads %v, want %v (%v)", b, s.Humidify, want, err)
		}
	}
}

func TestDecodeSettings(t *testing.T) {
	s, err := DecodeSettings(unhex(t, recF3))
	if err != nil {
		t.Fatal(err)
	}
	if s.Mode != ModeAuto || !s.Humidify {
		t.Errorf("settings = %+v", s)
	}
	if _, err := DecodeSettings(unhex(t, recF3Inf)); err == nil {
		t.Error("a notification's F3, with its mask, was taken for a state")
	}
}

func TestWrongLengthsAreErrors(t *testing.T) {
	if _, err := DecodeSensors(make([]byte, 39)); err == nil {
		t.Error("F1")
	}
	if _, err := DecodeStatus(make([]byte, 41)); err == nil {
		t.Error("F2")
	}
	if _, err := DecodeSettings(make([]byte, 26)); err == nil {
		t.Error("F3")
	}
}

// The commands sent in the write test on the device itself, byte for byte.
func TestCommandsMatchVerifiedBytes(t *testing.T) {
	for _, tc := range []struct {
		got  []byte
		want string
	}{
		{ModeCommand(ModeSilent), "010100001400000000000000000000000000000000000000000000"},
		{ModeCommand(ModeAuto), "010100001000000000000000000000000000000000000000000000"},
		{HumidifyCommand(false), "000900000000000000000000000000000000000000000000000000"},
		{HumidifyCommand(true), "000900000000000000000000000000ff0000000000000000000000"},
	} {
		if hex.EncodeToString(tc.got) != tc.want {
			t.Errorf("got %x, want %s", tc.got, tc.want)
		}
	}
}

func TestPowerEDT(t *testing.T) {
	if !bytes.Equal(PowerEDT(true), []byte{0x30}) || !bytes.Equal(PowerEDT(false), []byte{0x31}) {
		t.Error("power values")
	}
}
