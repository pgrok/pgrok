package tui

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
)

// maxBody is how much of each request and response body is kept for display
// and replay.
const maxBody = 16 << 10

// Request is an HTTP request forwarded through the tunnel.
type Request struct {
	ID       int
	ReplayOf int // the ID of the request this one replays, 0 otherwise
	Start    time.Time
	Method   string
	Host     string
	URI      string // path and query
	Proto    string
	Client   string // the public client address, from X-Forwarded-For
	Upstream string // the local address the request was routed to
	Route    string // the dynamic forward prefix, empty for the default address
	Kind     string // "ws" for protocol upgrades, "sse" for event streams

	ReqHeader http.Header
	ReqBody   []byte
	ReqSize   int64
	// ReqRead reports whether the request body was read to the end, which
	// it is not when the local address fails or answers before reading it.
	ReqRead    bool
	RespHeader http.Header
	RespBody   []byte
	RespSize   int64

	Status   int
	TTFB     time.Duration // until the response header was written
	Duration time.Duration
	Done     bool

	// Upstream timing, as offsets from Start, from the reverse proxy's
	// connection to the local address. Zero when the phase did not happen.
	Trace Trace
}

// Trace holds the phases of the upstream round trip, as offsets from the
// start of the request.
type Trace struct {
	ConnectStart, ConnectDone time.Duration // zero when a connection was reused
	ConnectErr                string
	GotConn                   time.Duration
	Reused                    bool
	WroteRequest              time.Duration
	FirstByte                 time.Duration
}

// Replayable reports whether r can be sent again: its request body was fully
// captured and it was not a protocol upgrade.
func (r Request) Replayable() bool {
	return r.Done && r.Kind != "ws" && r.ReqRead && int64(len(r.ReqBody)) == r.ReqSize
}

type replayKey struct{}

// Capture returns next wrapped so that every request it serves shows up in the
// TUI. route reports the dynamic forward prefix and local address a path is
// sent to. It returns next unchanged on a nil Hub.
func (h *Hub) Capture(next http.Handler, route func(path string) (prefix, addr string)) http.Handler {
	if h == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix, addr := route(r.URL.Path)
		replayOf, _ := r.Context().Value(replayKey{}).(int)
		req := &Request{
			ReplayOf:  replayOf,
			Start:     time.Now(),
			Method:    r.Method,
			Host:      r.Host,
			URI:       r.URL.RequestURI(),
			Proto:     r.Proto,
			Client:    clientAddr(r),
			Upstream:  addr,
			Route:     prefix,
			ReqHeader: r.Header.Clone(),
		}
		if r.Header.Get("Upgrade") != "" {
			req.Kind = "ws"
		}
		h.addRequest(req)
		r = r.WithContext(httptrace.WithClientTrace(r.Context(), h.trace(req)))

		body := &teeBody{eof: true}
		if r.Body != nil && r.Body != http.NoBody {
			body = &teeBody{ReadCloser: r.Body}
			r.Body = body
		}
		cw := &captureWriter{ResponseWriter: w, h: h, req: req}
		defer func() {
			reqBody, reqSize, reqRead := body.snapshot()
			h.mu.Lock()
			defer h.mu.Unlock()
			req.ReqBody, req.ReqSize, req.ReqRead = reqBody, reqSize, reqRead
			req.RespBody, req.RespSize = cw.body.b, cw.body.n
			if req.Status == 0 {
				req.Status = http.StatusOK
			}
			req.Duration = time.Since(req.Start)
			req.Done = true
			h.changed()
		}()
		next.ServeHTTP(cw, r)
	})
}

// trace records the reverse proxy's upstream round trip of req.
func (h *Hub) trace(req *Request) *httptrace.ClientTrace {
	mark := func(f func(d time.Duration)) {
		h.mu.Lock()
		defer h.mu.Unlock()
		// Zero means a phase did not happen, so a phase that did is never
		// zero, even where the clock is too coarse to measure it.
		f(max(time.Since(req.Start), 1))
		h.changed()
	}
	return &httptrace.ClientTrace{
		// The dialer may try several addresses, e.g. ::1 before 127.0.0.1 for
		// localhost: the phase spans all attempts, and fails only when none
		// succeeds.
		ConnectStart: func(string, string) {
			mark(func(d time.Duration) {
				if req.Trace.ConnectStart == 0 {
					req.Trace.ConnectStart = d
				}
			})
		},
		ConnectDone: func(_, _ string, err error) {
			mark(func(d time.Duration) {
				switch {
				case err == nil:
					req.Trace.ConnectDone, req.Trace.ConnectErr = d, ""
				case req.Trace.ConnectDone == 0 || req.Trace.ConnectErr != "": // no attempt succeeded yet
					req.Trace.ConnectDone, req.Trace.ConnectErr = d, err.Error()
				}
			})
		},
		GotConn: func(info httptrace.GotConnInfo) {
			mark(func(d time.Duration) { req.Trace.GotConn, req.Trace.Reused = d, info.Reused })
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			mark(func(d time.Duration) { req.Trace.WroteRequest = d })
		},
		GotFirstResponseByte: func() {
			mark(func(d time.Duration) { req.Trace.FirstByte = d })
		},
	}
}

// Replay sends the request with the given ID through the handler given to
// SetHTTP again, so that the local address receives it like it did the first time. The
// replay shows up in the TUI as a new request.
func (h *Hub) Replay(ctx context.Context, id int) error {
	h.mu.Lock()
	var orig *Request
	for _, r := range h.requests {
		if r.ID == id {
			orig = r
			break
		}
	}
	handler := h.handler
	var replayable bool
	if orig != nil {
		replayable = orig.Replayable()
	}
	h.mu.Unlock()
	switch {
	case orig == nil:
		return errors.Errorf("request #%d is no longer kept", id)
	case !replayable:
		return errors.Errorf("request #%d cannot be replayed", id)
	case handler == nil:
		return errors.New("no handler to replay through")
	}

	// The fields read below are not changed once the request is done.
	r, err := http.NewRequestWithContext(context.WithValue(ctx, replayKey{}, id), orig.Method, orig.URI, bytes.NewReader(orig.ReqBody))
	if err != nil {
		return errors.Wrap(err, "build request")
	}
	r.Host = orig.Host
	r.Header = orig.ReqHeader.Clone()
	r.RequestURI = orig.URI
	r.Proto = orig.Proto
	handler.ServeHTTP(&discardWriter{header: make(http.Header)}, r)
	return nil
}

type discardWriter struct{ header http.Header }

func (w *discardWriter) Header() http.Header         { return w.header }
func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *discardWriter) WriteHeader(int)             {}

func clientAddr(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(first)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (h *Hub) addRequest(req *Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	req.ID = h.nextID
	h.requests = append(h.requests, req)
	if n := len(h.requests) - maxRequests; n > 0 {
		h.requests = append(h.requests[:0:0], h.requests[n:]...)
	}
	h.reqTotal++
	sec := req.Start.Unix()
	if h.perSecAt[sec%60] != sec {
		h.perSecAt[sec%60] = sec
		h.perSec[sec%60] = 0
	}
	h.perSec[sec%60]++
	h.changed()
}

// capBuffer keeps the first maxBody bytes written to it and counts the rest.
type capBuffer struct {
	b    []byte
	n    int64
	skip bool
}

func (c *capBuffer) write(p []byte) {
	c.n += int64(len(p))
	if c.skip {
		return
	}
	if room := maxBody - len(c.b); room > 0 {
		c.b = append(c.b, p[:min(room, len(p))]...)
	}
}

// teeBody captures a request body as it is read. The reverse proxy's transport
// may still read it after the handler returns, hence the mutex.
type teeBody struct {
	io.ReadCloser
	mu  sync.Mutex
	buf capBuffer
	eof bool
}

func (t *teeBody) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.write(p[:n])
	if err == io.EOF {
		t.eof = true
	}
	return n, err
}

// snapshot returns the body captured so far, its size, and whether it was
// read to the end.
func (t *teeBody) snapshot() ([]byte, int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.buf.b), t.buf.n, t.eof
}

// captureWriter records the response. It implements http.Flusher and
// http.Hijacker directly, because the dynamic forward handler checks for them
// with type assertions; without them, event streams would be buffered and
// WebSocket upgrades would fail.
type captureWriter struct {
	http.ResponseWriter
	h           *Hub
	req         *Request
	body        capBuffer
	wroteHeader bool
}

func (w *captureWriter) WriteHeader(code int) {
	// 1xx responses such as 100 Continue are not the final status.
	if !w.wroteHeader && code >= 200 {
		w.recordHeader(code)
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *captureWriter) recordHeader(code int) {
	w.wroteHeader = true
	w.h.mu.Lock()
	defer w.h.mu.Unlock()
	w.req.Status = code
	w.req.TTFB = time.Since(w.req.Start)
	w.req.RespHeader = w.Header().Clone()
	if strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") {
		w.req.Kind = "sse"
		w.body.skip = true
	}
	w.h.changed()
}

func (w *captureWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	w.body.write(p[:n])
	return n, err
}

func (w *captureWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *captureWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err != nil {
		return nil, nil, errors.Wrap(err, "hijack")
	}
	// The reverse proxy hijacks only after the upstream switched protocols, and
	// writes the 101 response to the hijacked connection itself.
	if !w.wroteHeader {
		w.recordHeader(http.StatusSwitchingProtocols)
	}
	w.body.skip = true
	return conn, rw, nil
}

func (w *captureWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
