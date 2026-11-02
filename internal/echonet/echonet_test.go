package echonet

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func hexBytes(t *testing.T, s string) []byte {
	t.Helper()
	var out []byte
	for i := 0; i+1 < len(s); i += 2 {
		var b byte
		for _, ch := range []byte{s[i], s[i+1]} {
			switch {
			case ch >= '0' && ch <= '9':
				b = b<<4 | (ch - '0')
			case ch >= 'a' && ch <= 'f':
				b = b<<4 | (ch - 'a' + 10)
			default:
				t.Fatalf("bad hex %q", s)
			}
		}
		out = append(out, b)
	}
	return out
}

func TestParseRecordedFrames(t *testing.T) {
	// The discovery response recorded from a KI-SX100.
	f, err := Parse(hexBytes(t, "108100010ef00105ff017201d60401013501"))
	if err != nil {
		t.Fatal(err)
	}
	if f.TID != 1 || f.SEOJ != NodeProfile || f.DEOJ != Controller || f.ESV != ESVGetRes {
		t.Fatalf("frame = %+v", f)
	}
	got := InstanceList(f.Props[0].EDT)
	if len(got) != 1 || !got[0].IsAirCleaner() || got[0] != AirCleaner {
		t.Fatalf("instances = %v", got)
	}

	// A recorded notification carrying three properties in one frame.
	inf := "1081000001350105ff017303a00141f2020020f31b00000000100000000000000000ff00ff0000000000000000000000"
	f, err = Parse(hexBytes(t, inf))
	if err != nil {
		t.Fatal(err)
	}
	if f.ESV != ESVInf || len(f.Props) != 3 || f.Props[2].EPC != 0xF3 || len(f.Props[2].EDT) != 27 {
		t.Fatalf("frame = %+v", f)
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	in := Frame{TID: 0x1234, SEOJ: Controller, DEOJ: AirCleaner, ESV: ESVSetC, Props: []Prop{{EPC: 0x80, EDT: []byte{0x30}}}}
	b := in.Marshal()
	want := hexBytes(t, "1081123405ff0101350161018001"+"30")
	if !bytes.Equal(b, want) {
		t.Fatalf("Marshal = %x, want %x", b, want)
	}
	out, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.TID != in.TID || out.ESV != in.ESV || !bytes.Equal(out.Props[0].EDT, []byte{0x30}) {
		t.Fatalf("round trip = %+v", out)
	}
}

func TestParseRejectsBadFrames(t *testing.T) {
	for name, b := range map[string][]byte{
		"empty":        nil,
		"short":        {0x10, 0x81, 0, 1},
		"wrong header": hexBytes(t, "108200010ef00105ff017201d60401013501"),
		"truncated":    hexBytes(t, "108100010ef00105ff017201d604010135"),
		"no room":      hexBytes(t, "108100010ef00105ff017202d600"),
	} {
		if _, err := Parse(b); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func TestPropertyMap(t *testing.T) {
	// The KI-SX100's own maps: its SET map is a list, its GET map a bitmap.
	if got := PropertyMap(hexBytes(t, "058081a0f3f4")); !bytes.Equal(got, []byte{0x80, 0x81, 0xA0, 0xF3, 0xF4}) {
		t.Fatalf("list map = %x", got)
	}
	bitmap := make([]byte, 17)
	bitmap[0] = 16
	bitmap[1] = 0x01 // row 0 (EPC x0), bit 0: 0x80
	bitmap[4] = 0x80 // row 3 (EPC x3), bit 7: 0xF3
	if got := PropertyMap(bitmap); !bytes.Equal(got, []byte{0x80, 0xF3}) {
		t.Fatalf("bitmap = %x", got)
	}
	if PropertyMap(nil) != nil || PropertyMap([]byte{3, 0x80}) != nil || PropertyMap(make([]byte, 16)) != nil && len(PropertyMap(make([]byte, 16))) != 0 {
		t.Fatal("short maps must not panic or invent entries")
	}
}

// fakeConn is a socket whose peer is a function.
type fakeConn struct {
	mu     sync.Mutex
	in     chan packet
	closed chan struct{}
	answer func(f Frame, n int) []Frame // n counts the frames so far
	sent   []Frame
	fail   error
}

type packet struct {
	b    []byte
	addr net.Addr
}

func newFake(answer func(f Frame, n int) []Frame) *fakeConn {
	return &fakeConn{in: make(chan packet, 16), closed: make(chan struct{}), answer: answer}
}

func (c *fakeConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case p := <-c.in:
		return copy(b, p.b), p.addr, nil
	case <-c.closed:
		return 0, nil, net.ErrClosed
	}
}

func (c *fakeConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail != nil {
		return 0, c.fail
	}
	f, err := Parse(b)
	if err != nil {
		return 0, err
	}
	c.sent = append(c.sent, f)
	for _, res := range c.answer(f, len(c.sent)) {
		c.in <- packet{res.Marshal(), &net.UDPAddr{IP: net.IPv4(192, 0, 2, 7), Port: Port}}
	}
	return len(b), nil
}

func (c *fakeConn) Close() error { close(c.closed); return nil }

var deviceIP = net.IPv4(192, 0, 2, 7)

func TestGet(t *testing.T) {
	c := NewClient(newFake(func(f Frame, _ int) []Frame {
		return []Frame{{TID: f.TID, SEOJ: f.DEOJ, DEOJ: f.SEOJ, ESV: ESVGetRes, Props: []Prop{{EPC: f.Props[0].EPC, EDT: []byte{0x30}}}}}
	}))
	defer c.Close()
	got, err := c.Get(context.Background(), deviceIP, AirCleaner, 0x80)
	if err != nil || !bytes.Equal(got, []byte{0x30}) {
		t.Fatalf("Get = %x, %v", got, err)
	}
}

func TestGetRefused(t *testing.T) {
	c := NewClient(newFake(func(f Frame, _ int) []Frame {
		return []Frame{{TID: f.TID, SEOJ: f.DEOJ, DEOJ: f.SEOJ, ESV: ESVGetSNA, Props: []Prop{{EPC: f.Props[0].EPC}}}}
	}))
	defer c.Close()
	if _, err := c.Get(context.Background(), deviceIP, AirCleaner, 0x8D); !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want ErrRefused", err)
	}
}

func TestSetRefused(t *testing.T) {
	c := NewClient(newFake(func(f Frame, _ int) []Frame {
		return []Frame{{TID: f.TID, SEOJ: f.DEOJ, DEOJ: f.SEOJ, ESV: ESVSetCSNA, Props: f.Props}}
	}))
	defer c.Close()
	if err := c.Set(context.Background(), deviceIP, AirCleaner, 0x80, []byte{0x30}); !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want ErrRefused", err)
	}
}

func TestRequestRetriesWithNewTID(t *testing.T) {
	fake := newFake(func(f Frame, n int) []Frame {
		if n == 1 { // the first datagram is lost
			return nil
		}
		return []Frame{{TID: f.TID, SEOJ: f.DEOJ, DEOJ: f.SEOJ, ESV: ESVSetRes, Props: []Prop{{EPC: f.Props[0].EPC}}}}
	})
	c := NewClient(fake)
	defer c.Close()
	if err := c.Set(context.Background(), deviceIP, AirCleaner, 0x80, []byte{0x30}); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.sent) != 2 || fake.sent[0].TID == fake.sent[1].TID {
		t.Fatalf("sent = %+v", fake.sent)
	}
}

func TestRequestContextCancels(t *testing.T) {
	c := NewClient(newFake(func(Frame, int) []Frame { return nil }))
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Get(ctx, deviceIP, AirCleaner, 0x80); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestStaleResponseIsIgnored(t *testing.T) {
	var fake *fakeConn
	fake = newFake(func(f Frame, _ int) []Frame {
		return []Frame{
			{TID: f.TID + 100, SEOJ: f.DEOJ, DEOJ: f.SEOJ, ESV: ESVGetRes, Props: []Prop{{EPC: 0x80, EDT: []byte{0x31}}}},
			{TID: f.TID, SEOJ: f.DEOJ, DEOJ: f.SEOJ, ESV: ESVGetRes, Props: []Prop{{EPC: 0x80, EDT: []byte{0x30}}}},
		}
	})
	c := NewClient(fake)
	defer c.Close()
	got, err := c.Get(context.Background(), deviceIP, AirCleaner, 0x80)
	if err != nil || got[0] != 0x30 {
		t.Fatalf("Get = %x, %v", got, err)
	}
}

func TestNotifications(t *testing.T) {
	fake := newFake(func(Frame, int) []Frame { return nil })
	c := NewClient(fake)
	defer c.Close()
	inf := Frame{TID: 9, SEOJ: AirCleaner, DEOJ: NodeProfile, ESV: ESVInf, Props: []Prop{{EPC: 0x80, EDT: []byte{0x31}}}}
	fake.in <- packet{inf.Marshal(), &net.UDPAddr{IP: deviceIP, Port: Port}}
	select {
	case n := <-c.Notifications():
		if !n.IP.Equal(deviceIP) || n.Frame.ESV != ESVInf {
			t.Fatalf("notification = %+v", n)
		}
	case <-time.After(time.Second):
		t.Fatal("no notification")
	}
}

func TestDiscover(t *testing.T) {
	c := NewClient(newFake(func(f Frame, _ int) []Frame {
		return []Frame{{TID: f.TID, SEOJ: NodeProfile, DEOJ: f.SEOJ, ESV: ESVGetRes, Props: []Prop{{EPC: 0xD6, EDT: []byte{1, 0x01, 0x35, 0x01}}}}}
	}))
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	nodes, err := c.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || !nodes[0].IP.Equal(deviceIP) || nodes[0].Instances[0] != AirCleaner {
		t.Fatalf("nodes = %+v", nodes) // the same node answered twice
	}
}

func TestDiscoverSendFailure(t *testing.T) {
	fake := newFake(func(Frame, int) []Frame { return nil })
	fake.fail = errors.New("no route to host")
	c := NewClient(fake)
	defer c.Close()
	if _, err := c.Discover(context.Background()); err == nil {
		t.Fatal("no error")
	}
}
