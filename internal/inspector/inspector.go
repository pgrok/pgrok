// Package inspector captures HTTP request/response pairs passing through a
// handler and serves a local web UI to browse and replay them.
package inspector

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/flamego/flamego"
)

const (
	// MaxBodySize is the maximum number of bytes captured for each request and
	// response body.
	MaxBodySize = 1 << 20
	// MaxEntries is the maximum number of captured exchanges kept in memory.
	MaxEntries = 500
)

// Summary is the list view of a captured exchange.
type Summary struct {
	ID         int64         `json:"id"`
	ReplayOf   int64         `json:"replay_of,omitempty"`
	Time       time.Time     `json:"time"`
	Method     string        `json:"method"`
	Path       string        `json:"path"`
	Status     int           `json:"status"`
	Duration   time.Duration `json:"duration"`
	Replayable bool          `json:"replayable"`
}

// Body is a captured, possibly truncated, message body.
type Body struct {
	Size      int64 `json:"size"`
	Truncated bool  `json:"truncated"`
	// Skipped is true when the body was not captured on purpose, e.g. for
	// streaming responses.
	Skipped bool `json:"skipped"`
	Binary  bool `json:"binary"`
	data    []byte
}

// MarshalJSON implements json.Marshaler to render textual bodies as strings.
func (b Body) MarshalJSON() ([]byte, error) {
	type alias Body
	v := struct {
		alias
		Text string `json:"text"`
	}{alias: alias(b)}
	if !b.Binary {
		v.Text = string(b.data)
	}
	return json.Marshal(v)
}

// Entry is a captured request/response exchange.
type Entry struct {
	Summary
	Host           string      `json:"host"`
	RequestHeader  http.Header `json:"request_header"`
	RequestBody    Body        `json:"request_body"`
	ResponseHeader http.Header `json:"response_header"`
	ResponseBody   Body        `json:"response_body"`
}

// Inspector records exchanges served by the wrapped handler.
type Inspector struct {
	next http.Handler

	mu          sync.RWMutex
	lastID      int64
	entries     []*Entry // Oldest first, at most MaxEntries
	subscribers map[chan Summary]struct{}
}

// New returns an Inspector that captures exchanges served by next.
func New(next http.Handler) *Inspector {
	return &Inspector{
		next:        next,
		subscribers: make(map[chan Summary]struct{}),
	}
}

// ServeHTTP serves the request with the wrapped handler and records the
// exchange.
func (i *Inspector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	i.serve(w, r, 0)
}

func (i *Inspector) serve(w http.ResponseWriter, r *http.Request, replayOf int64) *Entry {
	started := time.Now()
	e := &Entry{
		Summary: Summary{
			ReplayOf: replayOf,
			Time:     started,
			Method:   r.Method,
			Path:     r.URL.RequestURI(),
		},
		Host:          r.Host,
		RequestHeader: r.Header.Clone(),
	}

	var reqBody *teeReadCloser
	if r.Body != nil && r.Body != http.NoBody {
		reqBody = &teeReadCloser{ReadCloser: r.Body}
		r.Body = reqBody
	}
	isUpgrade := r.Header.Get("Upgrade") != ""
	cw := &captureWriter{ResponseWriter: w, skip: isUpgrade}
	i.next.ServeHTTP(cw, r)

	e.Duration = time.Since(started)
	e.Status = cw.status
	if e.Status == 0 {
		e.Status = http.StatusOK
	}
	e.ResponseHeader = cw.Header().Clone()
	e.ResponseBody = cw.buf.body(cw.skip)
	requestComplete := true
	if reqBody != nil {
		e.RequestBody, requestComplete = reqBody.snapshot()
	}
	// A request can only be replayed when we have its full body.
	e.Replayable = !isUpgrade && requestComplete && !e.RequestBody.Truncated
	i.add(e)
	return e
}

func (i *Inspector) add(e *Entry) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.lastID++
	e.ID = i.lastID
	if len(i.entries) >= MaxEntries {
		copy(i.entries, i.entries[1:])
		i.entries = i.entries[:len(i.entries)-1]
	}
	i.entries = append(i.entries, e)
	for ch := range i.subscribers {
		select {
		case ch <- e.Summary:
		default: // Drop for slow subscribers, they can refetch the list.
		}
	}
}

// Entries returns all captured exchanges, oldest first.
func (i *Inspector) Entries() []*Entry {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return append([]*Entry(nil), i.entries...)
}

func (i *Inspector) entry(id int64) *Entry {
	i.mu.RLock()
	defer i.mu.RUnlock()
	for _, e := range i.entries {
		if e.ID == id {
			return e
		}
	}
	return nil
}

// Replay re-sends the captured request with the given ID through the wrapped
// handler and records the result as a new entry.
func (i *Inspector) Replay(id int64) (*Entry, error) {
	e := i.entry(id)
	if e == nil {
		return nil, fmt.Errorf("request %d not found", id)
	} else if !e.Replayable {
		return nil, fmt.Errorf("request %d cannot be replayed", id)
	}

	r, err := http.NewRequest(e.Method, e.Path, bytes.NewReader(e.RequestBody.data))
	if err != nil {
		return nil, fmt.Errorf("build request: %v", err)
	}
	r.Host = e.Host
	r.Header = e.RequestHeader.Clone()
	r.RequestURI = e.Path
	return i.serve(&discardWriter{header: make(http.Header)}, r, e.ID), nil
}

//go:embed index.html
var indexHTML []byte

// Handler returns the http.Handler serving the inspector web UI and its API.
func (i *Inspector) Handler() http.Handler {
	f := flamego.New()
	// Captured exchanges may contain credentials, so reject DNS rebinding (only
	// IP or "localhost" hosts) and cross-site replays (Origin must match).
	f.Use(func(c flamego.Context, w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		host = strings.Trim(host, "[]")
		origin := r.Header.Get("Origin")
		if (host != "localhost" && net.ParseIP(host) == nil) ||
			(r.Method != http.MethodGet && origin != "" && origin != "http://"+r.Host) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		c.Next()
	})
	f.Get("/", func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})
	f.Group("/api/requests", func() {
		f.Get("", func(w http.ResponseWriter) {
			entries := i.Entries()
			summaries := make([]Summary, 0, len(entries))
			for _, e := range entries {
				summaries = append(summaries, e.Summary)
			}
			writeJSON(w, http.StatusOK, summaries)
		})
		f.Get("/{id}", func(c flamego.Context, w http.ResponseWriter) {
			e := i.entry(c.ParamInt64("id"))
			if e == nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "request not found"})
				return
			}
			writeJSON(w, http.StatusOK, e)
		})
		f.Post("/{id}/replay", func(c flamego.Context, w http.ResponseWriter) {
			e, err := i.Replay(c.ParamInt64("id"))
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, e.Summary)
		})
	})
	f.Get("/api/events", func(w http.ResponseWriter, r *http.Request) {
		ch := make(chan Summary, 64)
		i.mu.Lock()
		i.subscribers[ch] = struct{}{}
		i.mu.Unlock()
		defer func() {
			i.mu.Lock()
			delete(i.subscribers, ch)
			i.mu.Unlock()
		}()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		rc := http.NewResponseController(w)
		_ = rc.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case s := <-ch:
				p, _ := json.Marshal(s)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", p)
				_ = rc.Flush()
			}
		}
	})
	return f
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// captureBuffer keeps the first MaxBodySize bytes written to it and counts the
// rest.
type captureBuffer struct {
	data []byte
	size int64
}

func (b *captureBuffer) Write(p []byte) {
	b.size += int64(len(p))
	if n := MaxBodySize - len(b.data); n > 0 {
		b.data = append(b.data, p[:min(n, len(p))]...)
	}
}

func (b *captureBuffer) body(skipped bool) Body {
	return Body{
		Size:      b.size,
		Truncated: b.size > int64(len(b.data)),
		Skipped:   skipped,
		Binary:    !utf8.Valid(b.data),
		data:      b.data,
	}
}

// teeReadCloser captures a request body as it is read. The body may be read by
// a transport goroutine that outlives the handler, hence the mutex.
type teeReadCloser struct {
	io.ReadCloser
	mu       sync.Mutex
	buf      captureBuffer
	complete bool
}

func (t *teeReadCloser) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	t.mu.Lock()
	t.buf.Write(p[:n])
	if err == io.EOF {
		t.complete = true
	}
	t.mu.Unlock()
	return n, err
}

// snapshot returns the body captured so far and whether it was read in full.
func (t *teeReadCloser) snapshot() (Body, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.buf.body(false)
	b.data = append([]byte(nil), b.data...)
	return b, t.complete
}

// captureWriter records the status and body of a response while passing
// everything through, including flushes and hijacks needed by streaming
// responses and protocol upgrades.
type captureWriter struct {
	http.ResponseWriter
	status int
	skip   bool
	buf    captureBuffer
}

func (w *captureWriter) WriteHeader(status int) {
	if w.status < 200 {
		w.status = status
		if strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") {
			w.skip = true
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *captureWriter) Write(p []byte) (int, error) {
	// Like net/http, writing a body without a final status (including after
	// a 1xx informational one) implies 200.
	if w.status < 200 {
		w.WriteHeader(http.StatusOK)
	}
	if !w.skip {
		w.buf.Write(p)
	}
	return w.ResponseWriter.Write(p)
}

func (w *captureWriter) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *captureWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.skip = true
	if w.status == 0 {
		w.status = http.StatusSwitchingProtocols
	}
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *captureWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// discardWriter is the http.ResponseWriter for replayed requests, whose
// responses are only recorded.
type discardWriter struct {
	header http.Header
}

func (w *discardWriter) Header() http.Header         { return w.header }
func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *discardWriter) WriteHeader(int)             {}
