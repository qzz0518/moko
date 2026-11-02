package echonet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

var multicastGroup = net.IPv4(224, 0, 23, 0)

// Timeouts of a request. UDP loses datagrams and the device is slow at times.
const (
	requestTimeout = 1500 * time.Millisecond
	requestTries   = 3
)

// Errors of requests.
var (
	ErrTimeout = errors.New("echonet: no response")
	ErrRefused = errors.New("echonet: the device refused the request")
)

// Conn is what the client needs of a socket.
type Conn interface {
	ReadFrom(b []byte) (int, net.Addr, error)
	WriteTo(b []byte, addr net.Addr) (int, error)
	Close() error
}

// Notification is a frame the device sent of its own accord.
type Notification struct {
	IP    net.IP
	Frame Frame
}

// Node is a device found by Discover.
type Node struct {
	IP        net.IP
	Instances []EOJ
}

// Client sends requests and matches their responses by TID.
type Client struct {
	conn Conn
	pc   *ipv4.PacketConn // nil when conn is not a real socket

	mu       sync.Mutex
	tid      uint16
	pending  map[uint16]chan Frame
	searches map[uint16]chan Notification

	notify chan Notification
	done   chan struct{}
	once   sync.Once
}

// Listen binds UDP 3610 and joins the ECHONET Lite multicast group on every
// usable interface. Listening on a multicast address makes Go set the address
// reuse options, so that another controller on this computer can bind the port,
// though a unicast reply reaches only one of them.
func Listen() (*Client, error) {
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: multicastGroup, Port: Port})
	if err != nil {
		return nil, fmt.Errorf("echonet: listen on UDP %d: %w", Port, err)
	}
	pc := ipv4.NewPacketConn(udp)
	joined := 0
	for _, ifi := range multicastInterfaces() {
		if pc.JoinGroup(&ifi, &net.UDPAddr{IP: multicastGroup}) == nil {
			joined++
		}
	}
	if joined == 0 {
		udp.Close()
		return nil, errors.New("echonet: no usable network interface")
	}
	_ = pc.SetMulticastLoopback(false)
	c := newClient(udp)
	c.pc = pc
	return c, nil
}

// Rejoin joins the multicast group on interfaces that came up since Listen, as after
// the computer wakes or moves to another network.
func (c *Client) Rejoin() {
	if c.pc == nil {
		return
	}
	for _, ifi := range multicastInterfaces() {
		_ = c.pc.JoinGroup(&ifi, &net.UDPAddr{IP: multicastGroup}) // fails when joined already
	}
}

// NewClient makes a client over any socket, for tests.
func NewClient(conn Conn) *Client { return newClient(conn) }

func newClient(conn Conn) *Client {
	c := &Client{
		conn:     conn,
		pending:  map[uint16]chan Frame{},
		searches: map[uint16]chan Notification{},
		notify:   make(chan Notification, 32),
		done:     make(chan struct{}),
	}
	go c.read()
	return c
}

// Close stops the client.
func (c *Client) Close() error {
	var err error
	c.once.Do(func() {
		close(c.done)
		err = c.conn.Close()
	})
	return err
}

// Notifications delivers INF frames. Late ones are dropped, as they only tell
// that something changed.
func (c *Client) Notifications() <-chan Notification { return c.notify }

func (c *Client) read() {
	buf := make([]byte, 2048)
	for {
		n, addr, err := c.conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-c.done:
				return
			default:
			}
			// Windows reports ICMP port unreachable as a read error, and a
			// socket whose network went away fails until it is back.
			time.Sleep(100 * time.Millisecond)
			continue
		}
		f, err := Parse(buf[:n])
		if err != nil {
			continue
		}
		c.dispatch(f, addrIP(addr))
	}
}

func (c *Client) dispatch(f Frame, ip net.IP) {
	c.mu.Lock()
	ch, isPending := c.pending[f.TID]
	search, isSearch := c.searches[f.TID]
	c.mu.Unlock()
	switch {
	case f.ESV == ESVInf || f.ESV == ESVInfC:
		select {
		case c.notify <- Notification{IP: ip, Frame: f}:
		default:
		}
	case isSearch:
		select {
		case search <- Notification{IP: ip, Frame: f}:
		default:
		}
	case isPending:
		select {
		case ch <- f:
		default:
		}
	}
}

func addrIP(a net.Addr) net.IP {
	if u, ok := a.(*net.UDPAddr); ok {
		return u.IP
	}
	return nil
}

func (c *Client) register(pending bool) (uint16, chan Frame, chan Notification) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		c.tid++
		if c.tid == 0 {
			continue
		}
		if _, used := c.pending[c.tid]; used {
			continue
		}
		if _, used := c.searches[c.tid]; used {
			continue
		}
		break
	}
	if pending {
		ch := make(chan Frame, 1)
		c.pending[c.tid] = ch
		return c.tid, ch, nil
	}
	ch := make(chan Notification, 16)
	c.searches[c.tid] = ch
	return c.tid, nil, ch
}

func (c *Client) unregister(tid uint16) {
	c.mu.Lock()
	delete(c.pending, tid)
	delete(c.searches, tid)
	c.mu.Unlock()
}

// request sends a frame, retrying with a new TID until a response comes.
func (c *Client) request(ctx context.Context, ip net.IP, deoj EOJ, esv ESV, props []Prop) (Frame, error) {
	addr := &net.UDPAddr{IP: ip, Port: Port}
	var lastErr error = ErrTimeout
	for range requestTries {
		tid, ch, _ := c.register(true)
		f := Frame{TID: tid, SEOJ: Controller, DEOJ: deoj, ESV: esv, Props: props}
		if _, err := c.conn.WriteTo(f.Marshal(), addr); err != nil {
			c.unregister(tid)
			lastErr = fmt.Errorf("echonet: send to %s: %w", ip, err)
			// The first send may fail while macOS asks for local network access.
			select {
			case <-ctx.Done():
				return Frame{}, ctx.Err()
			case <-time.After(requestTimeout):
			}
			continue
		}
		timer := time.NewTimer(requestTimeout)
		select {
		case res := <-ch:
			timer.Stop()
			c.unregister(tid)
			return res, nil
		case <-timer.C:
			c.unregister(tid)
		case <-ctx.Done():
			timer.Stop()
			c.unregister(tid)
			return Frame{}, ctx.Err()
		case <-c.done:
			timer.Stop()
			c.unregister(tid)
			return Frame{}, net.ErrClosed
		}
	}
	return Frame{}, lastErr
}

// Get reads one property. A device that does not have it answers ErrRefused.
func (c *Client) Get(ctx context.Context, ip net.IP, eoj EOJ, epc byte) ([]byte, error) {
	res, err := c.request(ctx, ip, eoj, ESVGet, []Prop{{EPC: epc}})
	if err != nil {
		return nil, err
	}
	if res.ESV != ESVGetRes {
		return nil, fmt.Errorf("%w: Get %02X of %s answered %02X", ErrRefused, epc, eoj, byte(res.ESV))
	}
	for _, p := range res.Props {
		if p.EPC == epc {
			return p.EDT, nil
		}
	}
	return nil, fmt.Errorf("%w: Get %02X of %s had no such property", ErrRefused, epc, eoj)
}

// Set writes one property and waits for the device to accept it. Accepting is
// not doing: read the property back to know that it took effect.
func (c *Client) Set(ctx context.Context, ip net.IP, eoj EOJ, epc byte, edt []byte) error {
	res, err := c.request(ctx, ip, eoj, ESVSetC, []Prop{{EPC: epc, EDT: edt}})
	if err != nil {
		return err
	}
	if res.ESV != ESVSetRes {
		return fmt.Errorf("%w: Set %02X of %s answered %02X", ErrRefused, epc, eoj, byte(res.ESV))
	}
	return nil
}

// Discover asks every node on the network for its instances, by multicast on
// every usable interface, and returns those that answered within the context's
// time. A send that fails on every interface is an error: on macOS it is what
// a missing local network permission looks like.
func (c *Client) Discover(ctx context.Context) ([]Node, error) {
	tid, _, ch := c.register(false)
	defer c.unregister(tid)
	f := Frame{TID: tid, SEOJ: Controller, DEOJ: NodeProfile, ESV: ESVGet, Props: []Prop{{EPC: 0xD6}}}
	if err := c.sendMulticast(f.Marshal()); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var nodes []Node
	retry := time.NewTimer(600 * time.Millisecond)
	defer retry.Stop()
	for {
		select {
		case n := <-ch:
			if n.Frame.ESV != ESVGetRes || n.IP == nil || seen[n.IP.String()] {
				continue
			}
			for _, p := range n.Frame.Props {
				if p.EPC == 0xD6 {
					seen[n.IP.String()] = true
					nodes = append(nodes, Node{IP: n.IP, Instances: InstanceList(p.EDT)})
				}
			}
		case <-retry.C:
			_ = c.sendMulticast(f.Marshal())
		case <-ctx.Done():
			return nodes, nil
		case <-c.done:
			return nodes, net.ErrClosed
		}
	}
}

func (c *Client) sendMulticast(b []byte) error {
	dst := &net.UDPAddr{IP: multicastGroup, Port: Port}
	if c.pc == nil { // a test socket
		_, err := c.conn.WriteTo(b, dst)
		return err
	}
	var lastErr error
	sent := 0
	for _, ifi := range multicastInterfaces() {
		if err := c.pc.SetMulticastInterface(&ifi); err != nil {
			lastErr = err
			continue
		}
		if _, err := c.pc.WriteTo(b, nil, dst); err != nil {
			lastErr = err
			continue
		}
		sent++
	}
	if sent == 0 {
		if lastErr == nil {
			lastErr = errors.New("no usable network interface")
		}
		return fmt.Errorf("echonet: multicast: %w", lastErr)
	}
	return nil
}

// multicastInterfaces lists the interfaces that are up, have an IPv4 address and
// can multicast. VPNs are point to point and do not count.
func multicastInterfaces() []net.Interface {
	all, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.Interface
	for _, ifi := range all {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 ||
			ifi.Flags&net.FlagLoopback != 0 || ifi.Flags&net.FlagPointToPoint != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				out = append(out, ifi)
				break
			}
		}
	}
	return out
}
