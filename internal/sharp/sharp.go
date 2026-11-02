// Package sharp decodes the proprietary ECHONET Lite properties of Sharp's air
// purifiers and builds the commands that change them.
//
// The layouts are undocumented. Fields used here were read from a KI-SX100; see
// docs/device-echonet.md for what has been confirmed on the device itself.
package sharp

import (
	"fmt"

	"github.com/qzz0518/moko/internal/echonet"
)

// Properties of the air cleaner object.
const (
	EPCPower     = 0x80
	EPCFault     = 0x88
	EPCWatts     = 0x84
	EPCEnergy    = 0x85
	EPCModel     = 0x8C
	EPCFirmware  = 0x8B
	EPCMaker     = 0x8A
	EPCNodeID    = 0x83
	EPCAirflow   = 0xA0
	EPCPollution = 0xC0
	EPCLimits    = 0xF0
	EPCSensors   = 0xF1
	EPCStatus    = 0xF2
	EPCSettings  = 0xF3
)

// MakerSharp is the manufacturer code of Sharp (EPC 8A).
var MakerSharp = [3]byte{0x00, 0x00, 0x05}

// Mode is an operating mode (byte 4 of F3).
type Mode byte

// The modes. AI auto exists on other models and in Sharp's cloud API; the app does not
// offer it, but shows it if the device reports one. Boost is 効果実感, droplet is
// 飛沫粒子: read from the device while COCORO AIR switched to them.
const (
	ModeAuto    Mode = 0x10
	ModeNight   Mode = 0x11
	ModePollen  Mode = 0x13
	ModeSilent  Mode = 0x14
	ModeMedium  Mode = 0x15
	ModeHigh    Mode = 0x16
	ModeAIAuto  Mode = 0x20
	ModeDroplet Mode = 0x30
	ModeBoost   Mode = 0x40
)

// IsManual reports whether the mode is one of the fixed fan speeds.
func (m Mode) IsManual() bool { return m == ModeSilent || m == ModeMedium || m == ModeHigh }

// HumidifyState is what the humidifier is doing (byte 24 of F2).
type HumidifyState int

// The humidifier's states.
const (
	HumidifyOff HumidifyState = iota
	HumidifyIdle
	HumidifyActive
	HumidifyNoWater
)

// F1 and F2 are 40 bytes, F3 27, on this family of models. Anything else is a
// model whose layout is unknown.
const (
	lenF1 = 40
	lenF2 = 40
	lenF3 = 27
)

// Sensors is property F1.
type Sensors struct {
	TempC      int
	TempOK     bool
	Humidity   int
	HumidityOK bool
	PM25       int
	PM25OK     bool
	AirCleaned uint32 // m³ since new, as far as its rate shows
	Used       Supplies
}

// Supplies is a number for each consumable: what F1 counts of its use, what F0 allows,
// or the percent left.
type Supplies struct {
	Dust, Odor, Humidifying, Silver, Ion int
}

// Limits is property F0, what each consumable may use. It is the same for a model.
type Limits Supplies

const minLenF0 = 21 // F0 is longer, and what else it holds is unknown

// DecodeLimits decodes F0.
func DecodeLimits(b []byte) (Limits, error) {
	if len(b) < minLenF0 {
		return Limits{}, fmt.Errorf("sharp: F0 is %d bytes, want at least %d", len(b), minLenF0)
	}
	word := func(i int) int { return int(b[i])<<8 | int(b[i+1]) }
	return Limits{Dust: word(12), Odor: word(14), Humidifying: word(16), Silver: int(b[18]), Ion: word(19)}, nil
}

// Left is the percent of each consumable left, rounded down as COCORO AIR does:
// the two agree on all five of them on the device this was read from. A limit of
// zero, which says nothing, reads as 100.
func (l Limits) Left(used Supplies) Supplies {
	left := func(used, limit int) int {
		if limit <= 0 {
			return 100
		}
		return max(0, min(100, (limit-used)*100/limit))
	}
	return Supplies{
		Dust:        left(used.Dust, l.Dust),
		Odor:        left(used.Odor, l.Odor),
		Humidifying: left(used.Humidifying, l.Humidifying),
		Silver:      left(used.Silver, l.Silver),
		Ion:         left(used.Ion, l.Ion),
	}
}

// DecodeSensors decodes F1. Readings Sharp's own apps clip are reported as not
// available rather than as a number.
func DecodeSensors(b []byte) (Sensors, error) {
	if len(b) != lenF1 {
		return Sensors{}, fmt.Errorf("sharp: F1 is %d bytes, want %d", len(b), lenF1)
	}
	var s Sensors
	s.TempC = int(int8(b[3]))
	s.TempOK = s.TempC >= 0 && s.TempC <= 50
	s.Humidity = int(b[4])
	s.HumidityOK = s.Humidity > 6 && s.Humidity < 99
	word := uint16(b[27])<<8 | uint16(b[28])
	s.PM25 = int(word & 0x03FF)
	s.PM25OK = word&0x8000 == 0 && s.PM25 < 500
	s.AirCleaned = uint32(b[21])<<24 | uint32(b[22])<<16 | uint32(b[23])<<8 | uint32(b[24])
	word2 := func(i int) int { return int(b[i])<<8 | int(b[i+1]) }
	s.Used = Supplies{
		Dust:        word2(29),
		Odor:        word2(31),
		Humidifying: word2(35),
		Silver:      int(b[37]),
		Ion:         max(word2(15), word2(17)), // hours; two counters that have stayed equal
	}
	return s, nil
}

// Status is property F2.
type Status struct {
	WaterOK  bool
	Humidify HumidifyState
	Mode     Mode
	// NeedsCleaning is byte 23. It was set while COCORO AIR asked for three parts to be
	// cleaned; it has not been seen to clear.
	NeedsCleaning bool
}

// DecodeStatus decodes F2.
func DecodeStatus(b []byte) (Status, error) {
	if len(b) != lenF2 {
		return Status{}, fmt.Errorf("sharp: F2 is %d bytes, want %d", len(b), lenF2)
	}
	s := Status{WaterOK: b[19] == 0xFF, Mode: Mode(b[27]), NeedsCleaning: b[23] == 0x01}
	switch h := b[24]; {
	case h == 0x00:
		s.Humidify = HumidifyOff
	case h == 0x82:
		s.Humidify = HumidifyNoWater
	case h&0x01 != 0:
		s.Humidify = HumidifyActive
	default:
		s.Humidify = HumidifyIdle
	}
	return s, nil
}

// Settings is property F3 as a Get returns it.
type Settings struct {
	Mode     Mode
	Humidify bool
}

// DecodeSettings decodes F3 from a Get. Its first bytes, a mask of what to
// change in a command, are zero there; notifications set them, and commands
// always do, so a nonzero mask is not a state to believe.
func DecodeSettings(b []byte) (Settings, error) {
	if len(b) != lenF3 {
		return Settings{}, fmt.Errorf("sharp: F3 is %d bytes, want %d", len(b), lenF3)
	}
	if b[0] != 0 || b[1] != 0 || b[2] != 0 || b[3] != 0 {
		return Settings{}, fmt.Errorf("sharp: F3 has a nonzero mask: %x", b[:4])
	}
	return Settings{Mode: Mode(b[4]), Humidify: b[15] == 0xFF}, nil
}

// ModeCommand builds the F3 command that changes the mode and nothing else.
func ModeCommand(m Mode) []byte {
	b := make([]byte, lenF3)
	b[0], b[1], b[4] = 0x01, 0x01, byte(m)
	return b
}

// HumidifyCommand builds the F3 command that turns humidifying on or off and
// changes nothing else.
func HumidifyCommand(on bool) []byte {
	b := make([]byte, lenF3)
	b[1] = 0x09
	if on {
		b[15] = 0xFF
	}
	return b
}

// PowerEDT is the value of the standard operation status property.
func PowerEDT(on bool) []byte {
	if on {
		return []byte{0x30}
	}
	return []byte{0x31}
}

// Object is the object all of these belong to.
var Object = echonet.AirCleaner

// PowerCommand builds the F3 command that turns the device on or off. The
// standard property 80 is tried first; this is what Sharp's cloud API sends.
func PowerCommand(on bool) []byte {
	b := make([]byte, lenF3)
	b[1] = 0x03
	if on {
		b[13] = 0xFF
	}
	return b
}
