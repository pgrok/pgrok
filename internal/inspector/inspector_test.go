package inspector

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/dynamicforward"
)

func TestInspector(t *testing.T) {
	var upstreamHits atomic.Int32
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		switch r.URL.Path {
		case "/large":
			_, _ = w.Write([]byte(strings.Repeat("a", MaxBodySize+10)))
		case "/events":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: hello\n\n"))
			w.(http.Flusher).Flush()
			select {
			case <-release:
			case <-r.Context().Done():
			}
		default:
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Upstream", "yes")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"echo":` + string(body) + `}`))
		}
	}))
	defer upstream.Close()

	forward, err := dynamicforward.New(logx.NewNoopLogger(), upstream.URL, dynamicforward.Options{})
	require.NoError(t, err)
	ins := New(forward)
	tunnel := httptest.NewServer(ins)
	defer tunnel.Close()
	ui := httptest.NewServer(ins.Handler())
	defer ui.Close()

	t.Run("captures request and response", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, tunnel.URL+"/api/items?debug=1", strings.NewReader(`{"name":"pgrok"}`))
		require.NoError(t, err)
		req.Header.Set("X-Client", "test")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		assert.Equal(t, `{"echo":{"name":"pgrok"}}`, string(body))

		entries := ins.Entries()
		require.Len(t, entries, 1)
		e := entries[0]
		assert.Equal(t, http.MethodPost, e.Method)
		assert.Equal(t, "/api/items?debug=1", e.Path)
		assert.Equal(t, http.StatusCreated, e.Status)
		assert.Positive(t, e.Duration)
		assert.False(t, e.Time.IsZero())
		assert.Equal(t, "test", e.RequestHeader.Get("X-Client"))
		assert.Equal(t, "yes", e.ResponseHeader.Get("X-Upstream"))
		assert.Equal(t, `{"name":"pgrok"}`, string(e.RequestBody.data))
		assert.Equal(t, `{"echo":{"name":"pgrok"}}`, string(e.ResponseBody.data))
		assert.True(t, e.Replayable)
	})

	t.Run("caps captured body size", func(t *testing.T) {
		resp, err := http.Get(tunnel.URL + "/large")
		require.NoError(t, err)
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		assert.Len(t, body, MaxBodySize+10, "client must still get the full body")

		entries := ins.Entries()
		e := entries[len(entries)-1]
		assert.Len(t, e.ResponseBody.data, MaxBodySize)
		assert.EqualValues(t, MaxBodySize+10, e.ResponseBody.Size)
		assert.True(t, e.ResponseBody.Truncated)
	})

	t.Run("streams event streams without capturing them", func(t *testing.T) {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(tunnel.URL + "/events")
		require.NoError(t, err)
		// The first event must arrive while the upstream is still streaming.
		line, err := bufio.NewReader(resp.Body).ReadString('\n')
		require.NoError(t, err)
		assert.Equal(t, "data: hello\n", line)
		close(release)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		require.Eventually(t, func() bool {
			entries := ins.Entries()
			return entries[len(entries)-1].Path == "/events"
		}, 5*time.Second, 10*time.Millisecond)
		entries := ins.Entries()
		assert.True(t, entries[len(entries)-1].ResponseBody.Skipped)
	})

	t.Run("replay records a new entry hitting the upstream", func(t *testing.T) {
		original := ins.Entries()[0]
		hits := upstreamHits.Load()

		resp, err := http.Post(ui.URL+"/api/requests/1/replay", "", nil)
		require.NoError(t, err)
		var s Summary
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&s))
		_ = resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, hits+1, upstreamHits.Load())

		entries := ins.Entries()
		replayed := entries[len(entries)-1]
		assert.Equal(t, s.ID, replayed.ID)
		assert.Equal(t, original.ID, replayed.ReplayOf)
		assert.Equal(t, original.Method, replayed.Method)
		assert.Equal(t, original.Path, replayed.Path)
		assert.Equal(t, http.StatusCreated, replayed.Status)
		assert.Equal(t, "test", replayed.RequestHeader.Get("X-Client"))
		assert.Equal(t, `{"echo":{"name":"pgrok"}}`, string(replayed.ResponseBody.data))
	})

	t.Run("rejects cross-origin replay", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, ui.URL+"/api/requests/1/replay", nil)
		require.NoError(t, err)
		req.Header.Set("Origin", "http://evil.example.com")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}
