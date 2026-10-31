package reverseproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProxy(t *testing.T) {
	for _, name := range []string{"forward request", "replace client forwarding headers"} {
		t.Run(name, func(t *testing.T) {
			requests := make(chan *http.Request, 1)
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.Clone(r.Context())
				w.WriteHeader(http.StatusCreated)
				_, _ = io.Copy(w, r.Body)
			}))
			t.Cleanup(backend.Close)

			proxy := newProxy(strings.TrimPrefix(backend.URL, "http://"))
			req := httptest.NewRequest(http.MethodPost, "https://tunnel.example.com/echo?key=value&key=other", strings.NewReader("request body"))
			req.RemoteAddr = "192.0.2.1:1234"
			if name == "replace client forwarding headers" {
				req.Header.Set("Forwarded", "for=198.51.100.1")
				req.Header.Set("X-Forwarded-For", "198.51.100.1")
				req.Header.Set("X-Forwarded-Host", "spoofed.example.com")
				req.Header.Set("X-Forwarded-Proto", "http")
			}
			w := httptest.NewRecorder()
			proxy.ServeHTTP(w, req)

			require.Equal(t, http.StatusCreated, w.Code)
			assert.Equal(t, "request body", w.Body.String())
			forwarded := <-requests
			assert.Equal(t, http.MethodPost, forwarded.Method)
			assert.Equal(t, "tunnel.example.com", forwarded.Host)
			assert.Equal(t, "/echo", forwarded.URL.Path)
			assert.Equal(t, "key=value&key=other", forwarded.URL.RawQuery)
			assert.Empty(t, forwarded.Header.Get("Forwarded"))
			assert.Equal(t, "192.0.2.1", forwarded.Header.Get("X-Forwarded-For"))
			assert.Equal(t, "tunnel.example.com", forwarded.Header.Get("X-Forwarded-Host"))
			assert.Equal(t, "https", forwarded.Header.Get("X-Forwarded-Proto"))
		})
	}
}
