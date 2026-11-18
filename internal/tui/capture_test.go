package tui

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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/dynamicforward"
)

// newCaptureServer serves the dynamic forward handler, wrapped by
// Hub.Capture, in front of a default upstream and an /api upstream.
func newCaptureServer(t *testing.T, def, api http.Handler) (*Hub, *httptest.Server) {
	t.Helper()
	defSrv := httptest.NewServer(def)
	t.Cleanup(defSrv.Close)
	apiSrv := httptest.NewServer(api)
	t.Cleanup(apiSrv.Close)

	forward, err := dynamicforward.New(logx.NewNoopLogger(), defSrv.URL, dynamicforward.Options{}, dynamicforward.Forward{Prefix: "/api", Address: apiSrv.URL})
	require.NoError(t, err)
	hub := NewHub(Info{Protocol: "http"})
	route := func(path string) (string, string) {
		if strings.HasPrefix(path, "/api") {
			return "/api", apiSrv.URL
		}
		return "", defSrv.URL
	}
	handler := hub.Capture(forward, route)
	hub.SetHTTP(nil, "", handler)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return hub, srv
}

func waitDone(t *testing.T, hub *Hub) Request {
	t.Helper()
	var r Request
	require.Eventually(t, func() bool {
		s := hub.Snapshot(time.Now())
		if len(s.Requests) == 0 {
			return false
		}
		r = s.Requests[0]
		return r.Done
	}, 2*time.Second, 10*time.Millisecond)
	return r
}

func TestCapture(t *testing.T) {
	t.Run("body not read by the local address", func(t *testing.T) {
		// Nothing listens on port 1, so the body is never sent upstream.
		forward, err := dynamicforward.New(logx.NewNoopLogger(), "http://127.0.0.1:1", dynamicforward.Options{})
		require.NoError(t, err)
		hub := NewHub(Info{Protocol: "http"})
		handler := hub.Capture(forward, func(string) (string, string) { return "", "http://127.0.0.1:1" })
		hub.SetHTTP(nil, "", handler)
		srv := httptest.NewServer(handler)
		t.Cleanup(srv.Close)

		resp, err := http.Post(srv.URL+"/checkout", "application/json", strings.NewReader(`{"cart":[1,2]}`))
		require.NoError(t, err)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusBadGateway, resp.StatusCode)

		r := waitDone(t, hub)
		require.False(t, r.ReqRead)
		require.NotEmpty(t, r.Trace.ConnectErr)
		// Replaying would send the request without its body.
		require.False(t, r.Replayable())
		require.Error(t, hub.Replay(context.Background(), r.ID))
	})

	t.Run("replay", func(t *testing.T) {
		var mu sync.Mutex
		var got []string
		api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			got = append(got, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("X-Token")+" "+string(body))
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
		})
		hub, srv := newCaptureServer(t, http.NotFoundHandler(), api)

		req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/items/7?dry=1", strings.NewReader("payload"))
		require.NoError(t, err)
		req.Header.Set("X-Token", "abc")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		orig := waitDone(t, hub)

		require.NoError(t, hub.Replay(context.Background(), orig.ID))
		replay := waitDone(t, hub)
		require.NotEqual(t, orig.ID, replay.ID)
		require.Equal(t, orig.ID, replay.ReplayOf)
		require.Equal(t, http.StatusAccepted, replay.Status)
		require.Equal(t, "/api", replay.Route)
		mu.Lock()
		require.Equal(t, []string{"PUT /api/items/7?dry=1 abc payload", "PUT /api/items/7?dry=1 abc payload"}, got)
		mu.Unlock()

		// The upstream round trip is traced. The transport reads the response
		// while it writes the request, so the first byte may come before the
		// request is fully written.
		tr := replay.Trace
		require.Positive(t, tr.GotConn)
		require.GreaterOrEqual(t, tr.WroteRequest, tr.GotConn)
		require.GreaterOrEqual(t, tr.FirstByte, tr.GotConn)
		require.LessOrEqual(t, tr.FirstByte, replay.Duration)

		// A request whose body was not fully captured cannot be replayed.
		resp, err = http.Post(srv.URL+"/api/big", "text/plain", strings.NewReader(strings.Repeat("x", maxBody+1)))
		require.NoError(t, err)
		_ = resp.Body.Close()
		big := waitDone(t, hub)
		require.False(t, big.Replayable())
		require.Error(t, hub.Replay(context.Background(), big.ID))
		require.Error(t, hub.Replay(context.Background(), 999))
	})

	t.Run("request and response", func(t *testing.T) {
		api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"echo":%s}`, body)
		})
		hub, srv := newCaptureServer(t, http.NotFoundHandler(), api)

		resp, err := http.Post(srv.URL+"/api/users?x=1", "application/json", strings.NewReader(`{"name":"Ada"}`))
		require.NoError(t, err)
		got, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		require.Equal(t, `{"echo":{"name":"Ada"}}`, string(got))

		r := waitDone(t, hub)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/users?x=1", r.URI)
		require.Equal(t, "/api", r.Route)
		require.Equal(t, http.StatusCreated, r.Status)
		require.Equal(t, `{"name":"Ada"}`, string(r.ReqBody))
		require.Equal(t, `{"echo":{"name":"Ada"}}`, string(r.RespBody))
		require.EqualValues(t, len(got), r.RespSize)
		require.Equal(t, "application/json", r.RespHeader.Get("Content-Type"))
	})

	t.Run("large body is capped but fully forwarded", func(t *testing.T) {
		big := strings.Repeat("x", maxBody+1000)
		def := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, big)
		})
		hub, srv := newCaptureServer(t, def, http.NotFoundHandler())

		resp, err := http.Get(srv.URL + "/")
		require.NoError(t, err)
		got, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		require.Len(t, got, len(big))

		r := waitDone(t, hub)
		require.Len(t, r.RespBody, maxBody)
		require.EqualValues(t, len(big), r.RespSize)
	})

	t.Run("event stream is flushed while it runs", func(t *testing.T) {
		release := make(chan struct{})
		def := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: first\n\n")
			w.(http.Flusher).Flush()
			<-release
		})
		hub, srv := newCaptureServer(t, def, http.NotFoundHandler())
		defer close(release)

		// Without flushing, the first event never arrives: fail instead of hanging.
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(srv.URL + "/events")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		line, err := bufio.NewReader(resp.Body).ReadString('\n')
		require.NoError(t, err)
		require.Equal(t, "data: first\n", line)

		r := hub.Snapshot(time.Now()).Requests[0]
		require.False(t, r.Done)
		require.Equal(t, "sse", r.Kind)
		require.Equal(t, http.StatusOK, r.Status)
	})

	t.Run("protocol upgrade", func(t *testing.T) {
		def := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			conn, rw, err := http.NewResponseController(w).Hijack()
			if err != nil {
				return
			}
			defer func() { _ = conn.Close() }()
			_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			_ = rw.Flush()
			msg, _ := rw.ReadString('\n')
			_, _ = rw.WriteString("echo " + msg)
			_ = rw.Flush()
		})
		hub, srv := newCaptureServer(t, def, http.NotFoundHandler())

		conn, err := net.Dial("tcp", srv.Listener.Addr().String())
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()
		_, err = io.WriteString(conn, "GET /ws HTTP/1.1\r\nHost: x\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		require.NoError(t, err)
		br := bufio.NewReader(conn)
		resp, err := http.ReadResponse(br, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

		_, err = io.WriteString(conn, "hello\n")
		require.NoError(t, err)
		line, err := br.ReadString('\n')
		require.NoError(t, err)
		require.Equal(t, "echo hello\n", line)

		r := hub.Snapshot(time.Now()).Requests[0]
		require.Equal(t, "ws", r.Kind)
		require.Equal(t, http.StatusSwitchingProtocols, r.Status)
	})
}

func TestFilterRequests(t *testing.T) {
	rows := []Request{
		{ID: 1, Method: "GET", URI: "/api/users", Status: 200},
		{ID: 2, Method: "POST", URI: "/api/users", Status: 201},
		{ID: 3, Method: "GET", URI: "/api/orders/1", Status: 404},
		{ID: 4, Method: "POST", URI: "/checkout", Status: 502},
		{ID: 5, Method: "GET", URI: "/", Status: 304},
	}
	ids := func(rs []Request) []int {
		var out []int
		for _, r := range rs {
			out = append(out, r.ID)
		}
		return out
	}
	for _, tc := range []struct {
		q    string
		want []int
	}{
		{"", []int{1, 2, 3, 4, 5}},
		{"status:>=400", []int{3, 4}},
		{"status:<300", []int{1, 2}},
		{"status:4xx", []int{3}},
		{"status:304", []int{5}},
		{"method:post", []int{2, 4}},
		{"method:POST /API", []int{2}},
		{"status:>=", []int{1, 2, 3, 4, 5}}, // still typing
	} {
		t.Run(tc.q, func(t *testing.T) {
			require.Equal(t, tc.want, ids(filterRequests(rows, tc.q)))
		})
	}
}
