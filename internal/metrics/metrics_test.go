package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
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
