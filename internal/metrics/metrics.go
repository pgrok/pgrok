// Package metrics defines the Prometheus metrics exposed by pgrokd.
//
// Per-user dimensions use the "subdomain" label, which is the principal's
// stable subdomain (one value per user). The full host is never used as a label
// value because custom subdomain prefixes and random fallback hosts would make
// the cardinality unbounded.
package metrics

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "pgrokd"

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "http_requests_total",
		Help:      "Total number of HTTP requests proxied through HTTP tunnels.",
	}, []string{"subdomain", "method", "code"})
	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "http_request_duration_seconds",
		Help:      "Duration of HTTP requests proxied through HTTP tunnels.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"subdomain"})
	httpBytesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "http_bytes_total",
		Help:      `Total number of HTTP body bytes proxied through HTTP tunnels, "in" for request bodies and "out" for response bodies.`,
	}, []string{"subdomain", "direction"})

	// TunnelsActive is the number of active tunnels by protocol.
	TunnelsActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "tunnels_active",
		Help:      "Number of active tunnels.",
	}, []string{"protocol"})
	// TCPTunnelBytesTotal is the number of bytes proxied through TCP tunnels,
	// "in" from visitors to the tunnel client and "out" for the reverse.
	TCPTunnelBytesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "tcp_tunnel_bytes_total",
		Help:      `Total number of bytes proxied through TCP tunnels, "in" from visitors to the tunnel client and "out" for the reverse.`,
	}, []string{"subdomain", "direction"})
	// SSHConnectionsActive is the number of authenticated SSH connections.
	SSHConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "ssh_connections_active",
		Help:      "Number of active authenticated SSH connections.",
	})
	// SSHAuthFailuresTotal is the number of rejected SSH authentication attempts.
	SSHAuthFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "ssh_auth_failures_total",
		Help:      "Total number of rejected SSH token authentication attempts.",
	})
)

// knownMethods bounds the "method" label, because the request method is an
// arbitrary token chosen by the visitor.
var knownMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodPost:    true,
	http.MethodPut:     true,
	http.MethodPatch:   true,
	http.MethodDelete:  true,
	http.MethodConnect: true,
	http.MethodOptions: true,
	http.MethodTrace:   true,
}

// InstrumentHTTP wraps the handler to record HTTP metrics labeled by the given
// principal subdomain.
func InstrumentHTTP(subdomain string, next http.Handler) http.Handler {
	bytesIn := httpBytesTotal.WithLabelValues(subdomain, "in")
	bytesOut := httpBytesTotal.WithLabelValues(subdomain, "out")
	duration := httpRequestDuration.WithLabelValues(subdomain)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		if r.Body != nil && r.Body != http.NoBody {
			r.Body = &countingReadCloser{ReadCloser: r.Body, counter: bytesIn}
		}
		rw := &responseWriter{ResponseWriter: w, counter: bytesOut}
		// Record in a defer because the reverse proxy aborts the handler with a
		// panic when copying the response body fails.
		defer func() {
			method := r.Method
			if !knownMethods[method] {
				method = "OTHER"
			}
			status := rw.status
			if status == 0 {
				status = http.StatusOK
			}
			httpRequestsTotal.WithLabelValues(subdomain, method, strconv.Itoa(status/100)+"xx").Inc()
			duration.Observe(time.Since(start).Seconds())
		}()
		next.ServeHTTP(rw, r)
	})
}

type countingReadCloser struct {
	io.ReadCloser
	counter prometheus.Counter
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	c.counter.Add(float64(n))
	return n, err
}

// responseWriter records the status code and counts the body bytes written. It
// implements Unwrap so that http.ResponseController can reach the underlying
// Flusher and Hijacker, which the reverse proxy needs for streaming and protocol
// upgrades.
type responseWriter struct {
	http.ResponseWriter
	counter prometheus.Counter
	status  int
}

func (w *responseWriter) WriteHeader(code int) {
	// Informational responses (e.g. 103 Early Hints) are followed by the final
	// response, except for 101 Switching Protocols.
	if w.status == 0 && (code >= 200 || code == http.StatusSwitchingProtocols) {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.counter.Add(float64(n))
	return n, err
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// CountingWriter returns a writer that adds the number of bytes written to w to
// the counter as they are written.
func CountingWriter(w io.Writer, counter prometheus.Counter) io.Writer {
	return &countingWriter{Writer: w, counter: counter}
}

type countingWriter struct {
	io.Writer
	counter prometheus.Counter
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.counter.Add(float64(n))
	return n, err
}
