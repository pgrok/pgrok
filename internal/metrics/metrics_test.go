package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstrumentHTTP(t *testing.T) {
	handler := InstrumentHTTP("alice", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("hello"))
	}))

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/", strings.NewReader("ping")),
		httptest.NewRequest(http.MethodGet, "/missing", nil),
		httptest.NewRequest("FOOBAR", "/", nil),
	} {
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	// Status codes are grouped by class and unknown methods are bounded to keep
	// the cardinality in check.
	assert.Equal(t, 1.0, testutil.ToFloat64(httpRequestsTotal.WithLabelValues("alice", "POST", "2xx")))
	assert.Equal(t, 1.0, testutil.ToFloat64(httpRequestsTotal.WithLabelValues("alice", "GET", "4xx")))
	assert.Equal(t, 1.0, testutil.ToFloat64(httpRequestsTotal.WithLabelValues("alice", "OTHER", "2xx")))
	assert.Equal(t, 4.0, testutil.ToFloat64(httpBytesTotal.WithLabelValues("alice", "in")))
	assert.Equal(t, 1, testutil.CollectAndCount(httpRequestDuration))
}

func TestInstrumentHTTP_Upgrade(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "Upgrade")
		w.Header().Set("Upgrade", "websocket")
		w.WriteHeader(http.StatusSwitchingProtocols)
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	require.NoError(t, err)
	proxy := httptest.NewServer(InstrumentHTTP("bob", httputil.NewSingleHostReverseProxy(backendURL)))
	defer proxy.Close()

	req, err := http.NewRequest(http.MethodGet, proxy.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	proxy.CloseClientConnections()
	backend.CloseClientConnections()

	// The reverse proxy writes the 101 response to the hijacked connection
	// instead of calling WriteHeader.
	assert.Eventually(t, func() bool {
		return testutil.ToFloat64(httpRequestsTotal.WithLabelValues("bob", "GET", "1xx")) == 1
	}, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, 0.0, testutil.ToFloat64(httpRequestsTotal.WithLabelValues("bob", "GET", "2xx")))
}
