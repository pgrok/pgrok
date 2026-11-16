package inspector_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/inspector"
	"github.com/pgrok/pgrok/internal/reverseproxy"
	"github.com/pgrok/pgrok/internal/tunnels"
)

const (
	tunnelHost = "alice.example.com"
	owner      = int64(1)
	stranger   = int64(2)
)

// setup serves upstream through a tunnel of the owner behind an inspector, and
// returns the proxy server.
func setup(t *testing.T, upstream http.Handler) (*inspector.Inspector, *httptest.Server) {
	t.Helper()
	backend := httptest.NewServer(upstream)
	t.Cleanup(backend.Close)

	proxies := reverseproxy.NewCluster(logx.NewNoopLogger(), "")
	_, err := proxies.Set(tunnelHost, "alt-"+tunnelHost, strings.TrimPrefix(backend.URL, "http://"))
	require.NoError(t, err)
	registry := tunnels.NewRegistry()
	registry.Add(tunnels.Tunnel{PrincipalID: owner, Protocol: "http", Host: tunnelHost}, func() error { return nil })

	in := inspector.New(registry, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy, ok := proxies.Get(r.Host)
		if !ok {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	server := httptest.NewServer(in)
	t.Cleanup(server.Close)
	return in, server
}

func do(t *testing.T, server *httptest.Server, method, host, path, body string, header http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Host = host
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := server.Client().Do(req)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp
}

func TestCapture(t *testing.T) {
	var hits atomic.Int32
	var lastAuthorization, lastBody atomic.Value
	in, server := setup(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		lastAuthorization.Store(r.Header.Get("Authorization"))
		lastBody.Store(string(body))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "session=secret")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	header := http.Header{
		"Authorization": {"Bearer secret"},
		"Cookie":        {"session=secret"},
		"X-Custom":      {"kept"},
	}
	resp := do(t, server, http.MethodPost, tunnelHost, "/items?q=1", `{"name":"pgrok"}`, header)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Requests to hosts without a tunnel are never recorded.
	do(t, server, http.MethodGet, "unknown.example.com", "/", "", nil)

	entries := in.List(owner, "")
	require.Len(t, entries, 1)
	e := entries[0]
	assert.Equal(t, http.MethodPost, e.Method)
	assert.Equal(t, tunnelHost, e.Host)
	assert.Equal(t, "/items?q=1", e.URI)
	assert.Equal(t, http.StatusCreated, e.Status)
	assert.WithinDuration(t, time.Now(), e.StartedAt, time.Minute)
	assert.Positive(t, e.Duration)
	assert.Equal(t, `{"name":"pgrok"}`, string(e.RequestBody))
	assert.Equal(t, `{"ok":true}`, string(e.ResponseBody))
	assert.False(t, e.RequestBodyTruncated)
	assert.False(t, e.ResponseBodyTruncated)
	assert.Equal(t, "application/json", e.ResponseHeader.Get("Content-Type"))

	// Credentials are redacted for display but kept for replay.
	redacted := inspector.Redact(e.RequestHeader)
	assert.Equal(t, "[redacted]", redacted.Get("Authorization"))
	assert.Equal(t, "[redacted]", redacted.Get("Cookie"))
	assert.Equal(t, "kept", redacted.Get("X-Custom"))
	assert.Equal(t, "[redacted]", inspector.Redact(e.ResponseHeader).Get("Set-Cookie"))
	assert.Equal(t, "Bearer secret", e.RequestHeader.Get("Authorization"))

	// Only the owner can see and replay the entry.
	assert.Empty(t, in.List(stranger, ""))
	_, ok := in.Get(stranger, e.ID)
	assert.False(t, ok)
	_, err := in.Replay(context.Background(), stranger, e.ID)
	assert.ErrorIs(t, err, inspector.ErrNotFound)

	// Replaying sends the original request to the upstream again.
	lastAuthorization.Store("")
	lastBody.Store("")
	replay, err := in.Replay(context.Background(), owner, e.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 2, hits.Load())
	assert.Equal(t, "Bearer secret", lastAuthorization.Load())
	assert.Equal(t, `{"name":"pgrok"}`, lastBody.Load())
	assert.Equal(t, e.ID, replay.ReplayOf)
	assert.Equal(t, http.StatusCreated, replay.Status)
	assert.Equal(t, `{"ok":true}`, string(replay.ResponseBody))

	entries = in.List(owner, "")
	require.Len(t, entries, 2)
	assert.Equal(t, replay.ID, entries[0].ID, "newest first")
}

func TestCaptureBodyLimit(t *testing.T) {
	large := strings.Repeat("x", inspector.MaxBodySize+1)
	in, server := setup(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(large))
	}))

	do(t, server, http.MethodPost, tunnelHost, "/", large, nil)

	entries := in.List(owner, "")
	require.Len(t, entries, 1)
	e := entries[0]
	assert.Len(t, e.RequestBody, inspector.MaxBodySize)
	assert.True(t, e.RequestBodyTruncated)
	assert.Len(t, e.ResponseBody, inspector.MaxBodySize)
	assert.True(t, e.ResponseBodyTruncated)

	// A request whose body was not fully captured cannot be replayed faithfully.
	_, err := in.Replay(context.Background(), owner, e.ID)
	assert.ErrorIs(t, err, inspector.ErrNotReplayable)
}

func TestCaptureRetention(t *testing.T) {
	in, server := setup(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for i := 0; i < inspector.MaxEntries+5; i++ {
		do(t, server, http.MethodGet, tunnelHost, fmt.Sprintf("/%d", i), "", nil)
	}

	entries := in.List(owner, "")
	require.Len(t, entries, inspector.MaxEntries)
	assert.Equal(t, fmt.Sprintf("/%d", inspector.MaxEntries+4), entries[0].URI)
	assert.Equal(t, "/5", entries[len(entries)-1].URI)
}

func TestCaptureStreaming(t *testing.T) {
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	in, server := setup(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "" {
			conn, brw, err := http.NewResponseController(w).Hijack()
			if err != nil {
				return
			}
			defer func() { _ = conn.Close() }()
			_, _ = brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: echo\r\n\r\n")
			_ = brw.Flush()
			line, _ := brw.ReadString('\n')
			_, _ = brw.WriteString(line)
			_ = brw.Flush()
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: first\n\n"))
		_ = http.NewResponseController(w).Flush()
		<-release
	}))
	// Unblock the handler before the server is closed, even on failures.
	t.Cleanup(releaseOnce)

	// Server-sent events are flushed through before the stream ends.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events", nil)
	require.NoError(t, err)
	req.Host = tunnelHost
	resp, err := server.Client().Do(req)
	require.NoError(t, err)
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "data: first\n", line)
	releaseOnce()
	_ = resp.Body.Close()

	// Upgraded connections keep working end to end.
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = fmt.Fprintf(conn, "GET /ws HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: echo\r\n\r\n", tunnelHost)
	require.NoError(t, err)
	br := bufio.NewReader(conn)
	upgraded, err := http.ReadResponse(br, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, upgraded.StatusCode)
	_, err = conn.Write([]byte("ping\n"))
	require.NoError(t, err)
	echo, err := br.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "ping\n", echo)
	_ = conn.Close()

	require.Eventually(t, func() bool { return len(in.List(owner, "")) == 2 }, 5*time.Second, 10*time.Millisecond)
	for _, e := range in.List(owner, "") {
		assert.True(t, e.Streaming, e.URI)
		assert.Empty(t, e.ResponseBody, e.URI)
	}
}
