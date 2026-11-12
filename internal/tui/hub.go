// Package tui implements the full-screen terminal UI of the pgrok client.
//
// Tunnel code reports what happens through a *Hub: connection status, HTTP
// requests, TCP connections and log lines. Every Hub method is safe to call on
// a nil *Hub and does nothing, so callers do not need to check whether the TUI
// is enabled. The TUI takes a snapshot of the Hub on a short tick instead of
// re-rendering on every event, which keeps CPU low under heavy traffic.
package tui

import (
	"bytes"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxRequests = 500
	maxConns    = 200
	maxLogLines = 1000
)

// Info describes the tunnel.
type Info struct {
	// Name is the name of the tunnel in the config file, empty for the tunnel
	// of "pgrok http" and "pgrok tcp".
	Name       string
	Version    string
	Protocol   string // "http" or "tcp"
	RemoteAddr string
	// ForwardAddr is the default local address requests are forwarded to.
	ForwardAddr string
	Subdomain   string
	ConfigPath  string

	// Routes are the dynamic forwards, as path prefix and address pairs.
	Routes [][2]string
	// InspectURL is the URL of the request inspector web UI, if it runs.
	InspectURL string
}

// Status is the state of the connection to the server.
type Status int

const (
	StatusConnecting Status = iota
	StatusOnline
	StatusReconnecting
)

// Hub collects the client's state for the TUI.
type Hub struct {
	info  Info
	retry chan struct{}

	mu      sync.Mutex
	version uint64 // bumped on every change, so the TUI can skip unchanged ticks

	status      Status
	hostURL     string
	started     time.Time
	onlineSince time.Time
	lastOnline  time.Time
	lastErr     string
	retryAt     time.Time
	backoff     time.Duration
	attempt     int // failed attempts since the last time the client was online
	reconnects  int
	rtt         time.Duration

	handler http.Handler // the tunnel's handler, for replays

	nextID   int
	requests []*Request // oldest first
	reqTotal int
	perSec   [60]int // requests started per second, indexed by unix second % 60
	perSecAt [60]int64

	conns     []*Conn // oldest first
	connTotal int
	connOpen  int
	closedIn  int64 // bytes of connections dropped from conns
	closedOut int64
}

// NewHub returns a Hub for the tunnel described by info.
func NewHub(info Info) *Hub {
	return &Hub{
		info:    info,
		retry:   make(chan struct{}, 1),
		started: time.Now(),
	}
}

func (h *Hub) changed() { h.version++ }

// SetHTTP records the dynamic forward routes of an HTTP tunnel, the URL of its
// request inspector, empty when it does not run, and the handler that serves
// the tunnel's requests, which replays go through. The handler must contain
// the one returned by Capture, so that replays show up.
func (h *Hub) SetHTTP(routes [][2]string, inspectURL string, handler http.Handler) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.info.Routes = routes
	h.info.InspectURL = inspectURL
	h.handler = handler
	h.changed()
}

// SetConnecting records that the client is dialing the server.
func (h *Hub) SetConnecting() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.status != StatusReconnecting {
		h.status = StatusConnecting
	}
	h.retryAt = time.Time{}
	h.changed()
}

// SetOnline records that the tunnel is live at hostURL.
func (h *Hub) SetOnline(hostURL string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.lastOnline.IsZero() {
		h.reconnects++
	}
	h.status = StatusOnline
	h.hostURL = hostURL
	h.onlineSince = time.Now()
	h.lastErr = ""
	h.attempt = 0
	h.changed()
}

// SetDisconnected records that the connection failed with err and that the
// client retries after backoff.
func (h *Hub) SetDisconnected(err error, backoff time.Duration) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.status == StatusOnline {
		h.lastOnline = time.Now()
	}
	h.onlineSince = time.Time{}
	h.status = StatusReconnecting
	h.lastErr = err.Error()
	h.backoff = backoff
	h.retryAt = time.Now().Add(backoff)
	h.attempt++
	h.rtt = 0
	h.changed()
}

// SetRTT records the latest round-trip time to the server.
func (h *Hub) SetRTT(d time.Duration) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rtt = d
	h.changed()
}

// Retry returns a channel that receives when the user asks to reconnect now.
// It is nil, and so never receives, on a nil Hub.
func (h *Hub) Retry() <-chan struct{} {
	if h == nil {
		return nil
	}
	return h.retry
}

func (h *Hub) requestRetry() {
	select {
	case h.retry <- struct{}{}:
	default:
	}
}

// Conn is a connection accepted through the tunnel.
type Conn struct {
	ID     int
	Remote string
	Opened time.Time

	in, out atomic.Int64 // bytes from the remote side, and to it
	closed  atomic.Int64 // unix nanoseconds, 0 while open
}

// In returns the bytes received from the public side.
func (c *Conn) In() int64 { return c.in.Load() }

// Out returns the bytes sent to the public side.
func (c *Conn) Out() int64 { return c.out.Load() }

// Closed returns when the connection closed, or the zero time while it is open.
func (c *Conn) Closed() time.Time {
	if n := c.closed.Load(); n != 0 {
		return time.Unix(0, n)
	}
	return time.Time{}
}

type trackedConn struct {
	net.Conn
	c    *Conn
	h    *Hub
	once sync.Once
}

func (t *trackedConn) Read(p []byte) (int, error) {
	n, err := t.Conn.Read(p)
	t.c.in.Add(int64(n))
	return n, err
}

func (t *trackedConn) Write(p []byte) (int, error) {
	n, err := t.Conn.Write(p)
	t.c.out.Add(int64(n))
	return n, err
}

func (t *trackedConn) Close() error {
	t.once.Do(func() {
		t.c.closed.Store(time.Now().UnixNano())
		t.h.mu.Lock()
		t.h.connOpen--
		t.h.changed()
		t.h.mu.Unlock()
	})
	return t.Conn.Close()
}

// TrackConn returns conn wrapped so that the TUI shows it with its byte
// counts until it is closed. It returns conn unchanged on a nil Hub.
func (h *Hub) TrackConn(conn net.Conn) net.Conn {
	if h == nil {
		return conn
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	c := &Conn{ID: h.nextID, Remote: conn.RemoteAddr().String(), Opened: time.Now()}
	h.conns = append(h.conns, c)
	h.connTotal++
	h.connOpen++
	// Drop the oldest closed connections beyond the limit.
	for i := 0; len(h.conns) > maxConns && i < len(h.conns); {
		if h.conns[i].Closed().IsZero() {
			i++
			continue
		}
		h.closedIn += h.conns[i].In()
		h.closedOut += h.conns[i].Out()
		h.conns = append(h.conns[:i], h.conns[i+1:]...)
	}
	h.changed()
	return &trackedConn{Conn: conn, c: c, h: h}
}

// Snapshot is a consistent copy of the Hub's state.
type Snapshot struct {
	Info        Info
	Version     uint64
	Status      Status
	HostURL     string
	Started     time.Time
	OnlineSince time.Time
	LastOnline  time.Time
	LastErr     string
	RetryAt     time.Time
	Backoff     time.Duration
	Attempt     int
	Reconnects  int
	RTT         time.Duration

	Requests []Request // newest first
	ReqTotal int
	// PerSec holds the requests started in each of the last 60 seconds,
	// oldest first, ending with the current second.
	PerSec []int

	Conns     []*Conn // newest first
	ConnTotal int
	ConnOpen  int
	BytesIn   int64
	BytesOut  int64
}

// Version returns a counter that changes whenever the state changes.
func (h *Hub) Version() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.version
}

// Snapshot returns a copy of the current state at time now.
func (h *Hub) Snapshot(now time.Time) Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := Snapshot{
		Info:        h.info,
		Version:     h.version,
		Status:      h.status,
		HostURL:     h.hostURL,
		Started:     h.started,
		OnlineSince: h.onlineSince,
		LastOnline:  h.lastOnline,
		LastErr:     h.lastErr,
		RetryAt:     h.retryAt,
		Backoff:     h.backoff,
		Attempt:     h.attempt,
		Reconnects:  h.reconnects,
		RTT:         h.rtt,
		ReqTotal:    h.reqTotal,
		ConnTotal:   h.connTotal,
		ConnOpen:    h.connOpen,
		BytesIn:     h.closedIn,
		BytesOut:    h.closedOut,
	}
	s.Requests = make([]Request, len(h.requests))
	for i, r := range h.requests {
		s.Requests[len(h.requests)-1-i] = *r
	}
	s.Conns = make([]*Conn, len(h.conns))
	for i, c := range h.conns {
		s.Conns[len(h.conns)-1-i] = c
		s.BytesIn += c.In()
		s.BytesOut += c.Out()
	}
	sec := now.Unix()
	s.PerSec = make([]int, 60)
	for i := range s.PerSec {
		t := sec - int64(59-i)
		if h.perSecAt[t%60] == t {
			s.PerSec[i] = h.perSec[t%60]
		}
	}
	return s
}

// Logs keeps the client's log lines for the Logs view.
type Logs struct {
	mu      sync.Mutex
	version uint64
	lines   []string
	partial []byte
}

// Write adds each complete line in p. It implements io.Writer.
func (l *Logs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.partial = append(l.partial, p...)
	for {
		i := bytes.IndexByte(l.partial, '\n')
		if i < 0 {
			break
		}
		l.lines = append(l.lines, strings.TrimRight(string(l.partial[:i]), "\r"))
		l.partial = l.partial[i+1:]
	}
	if n := len(l.lines) - maxLogLines; n > 0 {
		l.lines = append(l.lines[:0:0], l.lines[n:]...)
	}
	l.version++
	return len(p), nil
}

func (l *Logs) snapshot() (uint64, []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.version, append([]string(nil), l.lines...)
}

func (l *Logs) currentVersion() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.version
}
