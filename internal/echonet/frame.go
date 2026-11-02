// Package echonet implements the small part of ECHONET Lite this app needs:
// frames, a UDP client, discovery and notifications. It knows nothing about
// Sharp's proprietary properties.
package echonet

import (
	"errors"
	"fmt"
)

// Port is ECHONET Lite's UDP port. Devices reply to it whatever port a request
// came from, so a controller has to listen on it.
const Port = 3610

// EOJ is an ECHONET object: class group, class and instance.
type EOJ [3]byte

// Objects this app talks to or as.
var (
	NodeProfile = EOJ{0x0E, 0xF0, 0x01}
	Controller  = EOJ{0x05, 0xFF, 0x01}
	AirCleaner  = EOJ{0x01, 0x35, 0x01}
)

// IsAirCleaner reports whether e is an instance of the air cleaner class.
func (e EOJ) IsAirCleaner() bool { return e[0] == 0x01 && e[1] == 0x35 }

func (e EOJ) String() string { return fmt.Sprintf("%02X%02X%02X", e[0], e[1], e[2]) }

// ESV is a frame's service code.
type ESV byte

// Services this app sends or receives.
const (
	ESVSetC    ESV = 0x61
	ESVGet     ESV = 0x62
	ESVSetRes  ESV = 0x71
	ESVGetRes  ESV = 0x72
	ESVInf     ESV = 0x73
	ESVInfC    ESV = 0x74
	ESVSetCSNA ESV = 0x51
	ESVGetSNA  ESV = 0x52
)

// Prop is one property of a frame.
type Prop struct {
	EPC byte
	EDT []byte
}

// Frame is an ECHONET Lite frame in the specified message format (EHD 0x1081).
type Frame struct {
	TID        uint16
	SEOJ, DEOJ EOJ
	ESV        ESV
	Props      []Prop
}

// Marshal encodes the frame.
func (f Frame) Marshal() []byte {
	b := make([]byte, 0, 14+len(f.Props)*4)
	b = append(b, 0x10, 0x81, byte(f.TID>>8), byte(f.TID))
	b = append(b, f.SEOJ[:]...)
	b = append(b, f.DEOJ[:]...)
	b = append(b, byte(f.ESV), byte(len(f.Props)))
	for _, p := range f.Props {
		b = append(b, p.EPC, byte(len(p.EDT)))
		b = append(b, p.EDT...)
	}
	return b
}

// ErrNotEchonet is a datagram that is not a specified-format ECHONET Lite frame.
var ErrNotEchonet = errors.New("echonet: not an ECHONET Lite frame")

// Parse decodes a datagram. The frame's property data are copies.
func Parse(b []byte) (Frame, error) {
	if len(b) < 12 || b[0] != 0x10 || b[1] != 0x81 {
		return Frame{}, ErrNotEchonet
	}
	f := Frame{TID: uint16(b[2])<<8 | uint16(b[3]), ESV: ESV(b[10])}
	copy(f.SEOJ[:], b[4:7])
	copy(f.DEOJ[:], b[7:10])
	opc := int(b[11])
	f.Props = make([]Prop, 0, opc)
	i := 12
	for range opc {
		if i+2 > len(b) {
			return Frame{}, fmt.Errorf("echonet: truncated frame: %d bytes, property header at %d", len(b), i)
		}
		epc, pdc := b[i], int(b[i+1])
		i += 2
		if i+pdc > len(b) {
			return Frame{}, fmt.Errorf("echonet: truncated frame: EPC %02X needs %d bytes at %d of %d", epc, pdc, i, len(b))
		}
		f.Props = append(f.Props, Prop{EPC: epc, EDT: append([]byte(nil), b[i:i+pdc]...)})
		i += pdc
	}
	return f, nil
}

// PropertyMap decodes a property map (EPC 9D, 9E, 9F). Maps of 16 or more
// properties are bitmaps; shorter ones list their EPCs.
func PropertyMap(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	n := int(b[0])
	if n < 16 {
		if len(b) < 1+n {
			return nil
		}
		return append([]byte(nil), b[1:1+n]...)
	}
	if len(b) < 17 {
		return nil
	}
	var out []byte
	for i, x := range b[1:17] {
		for j := range 8 {
			if x>>j&1 != 0 {
				out = append(out, byte((j+8)<<4|i))
			}
		}
	}
	return out
}

// InstanceList decodes a node profile's instance list (EPC D6).
func InstanceList(b []byte) []EOJ {
	if len(b) == 0 {
		return nil
	}
	var out []EOJ
	for i := 1; i+3 <= len(b) && len(out) < int(b[0]); i += 3 {
		out = append(out, EOJ{b[i], b[i+1], b[i+2]})
	}
	return out
}
