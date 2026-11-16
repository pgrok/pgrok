// Package inspector records the HTTP requests proxied through tunnels so that
// their owners can inspect and replay them. Records are kept in memory only.
package inspector

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"mime"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/pgrok/pgrok/internal/tunnels"
)

const (
	// MaxBodySize is the maximum number of bytes captured for each of the request
	// and response bodies.
	MaxBodySize = 64 << 10
	// MaxEntries is the number of most recent entries kept for each principal.
	MaxEntries = 200
)

// Entry is a recorded HTTP request and its response.
type Entry struct {
	ID          string
	PrincipalID int64
	TunnelID    string
	ReplayOf    string // The ID of the replayed entry, empty for original requests.
	Method      string
	Host        string
	URI         string // The path and query of the request.
	Status      int
	StartedAt   time.Time
	Duration    time.Duration

	// The headers are stored as received, and should be redacted with Redact
	// before being shown.
	RequestHeader         http.Header
	RequestBody           []byte
	RequestBodyTruncated  bool // Whether RequestBody is incomplete.
	ResponseHeader        http.Header
	ResponseBody          []byte
	ResponseBodyTruncated bool // Whether ResponseBody is incomplete.
	// Streaming indicates the response is a stream (server-sent events) or a
	// protocol upgrade (e.g. WebSocket), whose body is not captured.
	Streaming bool

	remoteAddr string
}

// sensitiveHeaders are the headers whose values are hidden by Redact.
var sensitiveHeaders = []string{"Authorization", "Proxy-Authorization", "Cookie", "Set-Cookie"}

// Redact returns a copy of the header with values of credential-bearing headers
// replaced.
func Redact(h http.Header) http.Header {
	h = h.Clone()
	for _, name := range sensitiveHeaders {
		for i := range h[name] {
			h[name][i] = "[redacted]"
		}
	}
	return h
}

var (
	// ErrNotFound is returned when the entry does not exist or is not owned by
	// the principal.
	ErrNotFound = errors.New("request not found")
	// ErrTunnelGone is returned when replaying an entry whose host is no longer
	// served by a tunnel of the same principal.
	ErrTunnelGone = errors.New("the tunnel of the request is no longer connected")
	// ErrNotReplayable is returned when the entry cannot be replayed faithfully.
	ErrNotReplayable = errors.New("the request cannot be replayed because its body was not fully captured or it upgraded the connection")
)

// Inspector records requests to HTTP tunnels before passing them to the next
// handler, which is expected to be the proxy.
type Inspector struct {
	registry *tunnels.Registry
	next     http.Handler

	mu    sync.RWMutex
	rings map[int64]*ring // Principal ID -> entries
}

// New returns a new Inspector that records requests to hosts of the tunnels in
// the registry and serves them with next.
func New(registry *tunnels.Registry, next http.Handler) *Inspector {
	return &Inspector{
		registry: registry,
		next:     next,
		rings:    make(map[int64]*ring),
	}
}

// ServeHTTP serves the request with the next handler, recording it when the
// host belongs to a tunnel.
func (in *Inspector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tunnel, ok := in.registry.GetByHost(r.Host)
	if !ok {
		in.next.ServeHTTP(w, r)
		return
	}
	in.serve(w, r, tunnel, "")
}

// serve serves and records the request, then returns the recorded entry.
func (in *Inspector) serve(w http.ResponseWriter, r *http.Request, tunnel tunnels.Tunnel, replayOf string) *Entry {
	e := &Entry{
		ID:            uuid.NewString(),
		PrincipalID:   tunnel.PrincipalID,
		TunnelID:      tunnel.ID,
		ReplayOf:      replayOf,
		Method:        r.Method,
		Host:          r.Host,
		URI:           r.URL.RequestURI(),
		StartedAt:     time.Now(),
		RequestHeader: r.Header.Clone(),
		Streaming:     r.Header.Get("Upgrade") != "",
		remoteAddr:    r.RemoteAddr,
	}

	var body *captureReader
	if r.Body != nil && r.Body != http.NoBody {
		body = &captureReader{ReadCloser: r.Body}
		r.Body = body
	}
	rec := &recorder{ResponseWriter: w, entry: e}

	// The proxy aborts with a panic when the client goes away, record what has
	// been seen so far nonetheless.
	defer func() {
		e.Duration = time.Since(e.StartedAt)
		if body != nil {
			e.RequestBody, e.RequestBodyTruncated = body.result(r.ContentLength)
		}
		e.ResponseBody = rec.body.Bytes()
		in.add(e)
	}()
	in.next.ServeHTTP(rec, r)
	return e
}

// Replay sends the request of the given entry again through the tunnel serving
// its host, and returns the recorded entry of the replay.
func (in *Inspector) Replay(ctx context.Context, principalID int64, id string) (*Entry, error) {
	e, ok := in.Get(principalID, id)
	if !ok {
		return nil, ErrNotFound
	}
	if e.RequestBodyTruncated || e.RequestHeader.Get("Upgrade") != "" {
		return nil, ErrNotReplayable
	}
	tunnel, ok := in.registry.GetByHost(e.Host)
	if !ok || tunnel.PrincipalID != principalID {
		return nil, ErrTunnelGone
	}

	r, err := http.NewRequestWithContext(ctx, e.Method, "http://"+e.Host+e.URI, bytes.NewReader(e.RequestBody))
	if err != nil {
		return nil, errors.Wrap(err, "new request")
	}
	r.Header = e.RequestHeader.Clone()
	r.RemoteAddr = e.remoteAddr
	r.RequestURI = e.URI
	if len(e.RequestBody) == 0 {
		r.Body = http.NoBody
	}
	return in.serve(&discardWriter{header: make(http.Header)}, r, tunnel, e.ID), nil
}

// List returns entries of the principal, newest first. When tunnelID is not
// empty, only entries of that tunnel are returned.
func (in *Inspector) List(principalID int64, tunnelID string) []*Entry {
	in.mu.RLock()
	defer in.mu.RUnlock()
	ring := in.rings[principalID]
	if ring == nil {
		return nil
	}

	entries := make([]*Entry, 0, ring.size)
	for i := 1; i <= ring.size; i++ {
		e := ring.entries[(ring.next-i+MaxEntries)%MaxEntries]
		if tunnelID == "" || e.TunnelID == tunnelID {
			entries = append(entries, e)
		}
	}
	return entries
}

// Get returns the entry with the given ID owned by the principal. It returns a
// boolean to indicate whether such entry exists.
func (in *Inspector) Get(principalID int64, id string) (*Entry, bool) {
	for _, e := range in.List(principalID, "") {
		if e.ID == id {
			return e, true
		}
	}
	return nil, false
}

func (in *Inspector) add(e *Entry) {
	in.mu.Lock()
	defer in.mu.Unlock()
	r := in.rings[e.PrincipalID]
	if r == nil {
		r = &ring{}
		in.rings[e.PrincipalID] = r
	}
	r.entries[r.next] = e
	r.next = (r.next + 1) % MaxEntries
	r.size = min(r.size+1, MaxEntries)
}

// ring is a fixed-size circular buffer of entries.
type ring struct {
	entries [MaxEntries]*Entry
	next    int // The index to write the next entry to.
	size    int
}

// captureReader captures up to MaxBodySize bytes read from the request body.
// The proxy may still be reading the body when the handler returns, hence the
// lock.
type captureReader struct {
	io.ReadCloser

	mu   sync.Mutex
	buf  bytes.Buffer
	size int64
	eof  bool
}

func (r *captureReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.mu.Lock()
	defer r.mu.Unlock()
	if room := MaxBodySize - r.buf.Len(); room > 0 {
		r.buf.Write(p[:min(n, room)])
	}
	r.size += int64(n)
	r.eof = r.eof || err == io.EOF
	return n, err
}

// result returns the captured body and whether it is incomplete, either because
// it exceeds MaxBodySize or because it has not been fully read.
func (r *captureReader) result(contentLength int64) ([]byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	complete := r.eof || (contentLength >= 0 && r.size == contentLength)
	return bytes.Clone(r.buf.Bytes()), r.size > MaxBodySize || !complete
}

// recorder captures the status, headers and up to MaxBodySize bytes of the
// response. It preserves flushing and hijacking of the underlying writer.
type recorder struct {
	http.ResponseWriter
	entry *Entry
	body  bytes.Buffer
}

func (w *recorder) WriteHeader(code int) {
	// Informational responses (other than switching protocols) precede the final
	// one.
	if w.entry.Status == 0 && (code >= 200 || code == http.StatusSwitchingProtocols) {
		w.entry.Status = code
		w.entry.ResponseHeader = w.Header().Clone()
		mediaType, _, _ := mime.ParseMediaType(w.Header().Get("Content-Type"))
		if mediaType == "text/event-stream" {
			w.entry.Streaming = true
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *recorder) Write(p []byte) (int, error) {
	if w.entry.Status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if !w.entry.Streaming {
		room := MaxBodySize - w.body.Len()
		w.body.Write(p[:min(len(p), room)])
		if len(p) > room {
			w.entry.ResponseBodyTruncated = true
		}
	}
	return w.ResponseWriter.Write(p)
}

func (w *recorder) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, brw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		// The proxy writes the "101 Switching Protocols" response to the hijacked
		// connection directly.
		w.entry.Streaming = true
		if w.entry.Status == 0 {
			w.entry.Status = http.StatusSwitchingProtocols
		}
	}
	return conn, brw, err
}

func (w *recorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// discardWriter is the response writer of replayed requests, whose responses
// are only recorded.
type discardWriter struct {
	header http.Header
}

func (w *discardWriter) Header() http.Header         { return w.header }
func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *discardWriter) WriteHeader(int)             {}
