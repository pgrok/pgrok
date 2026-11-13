package reverseproxy

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/dynamicforward"
)

func TestCluster_ErrorPages(t *testing.T) {
	const host = `<script>alert(1)</script>.localhost`
	browserAccept := "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

	serve := func(c *Cluster, accept string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = host
		if accept != "" {
			r.Header.Set("Accept", accept)
		}
		w := httptest.NewRecorder()
		if proxy, ok := c.Get(r.Host); ok {
			proxy.ServeHTTP(w, r)
		} else {
			c.ServeNoTunnel(w, r)
		}
		return w
	}

	t.Run("no tunnel", func(t *testing.T) {
		c := NewCluster(logx.NewNoopLogger(), "https://pgrok.example.com")

		w := serve(c, browserAccept)
		assert.Equal(t, http.StatusBadGateway, w.Code)
		assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
		body := w.Body.String()
		assert.Contains(t, body, "This tunnel is offline")
		assert.Contains(t, body, "&lt;script&gt;alert(1)&lt;/script&gt;.localhost")
		assert.NotContains(t, body, "<script>")
		assert.Contains(t, body, `href="https://pgrok.example.com"`)

		for _, accept := range []string{"", "*/*", "application/json"} {
			w = serve(c, accept)
			assert.Equal(t, http.StatusBadGateway, w.Code, accept)
			assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"), accept)
			assert.Equal(t, "No reverse proxy is available for the host: "+host+"\n", w.Body.String(), accept)
		}
	})

	t.Run("upstream refuses connection", func(t *testing.T) {
		// Grab a free port and close it so that dialing it is refused.
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		forward := l.Addr().String()
		require.NoError(t, l.Close())

		c := NewCluster(logx.NewNoopLogger(), "https://pgrok.example.com")
		_, err = c.Set(host, "alt-"+host, forward)
		require.NoError(t, err)

		w := serve(c, browserAccept)
		assert.Equal(t, http.StatusBadGateway, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, "the local service at the other end is not responding")
		assert.NotContains(t, body, forward)

		w = serve(c, "")
		assert.Equal(t, http.StatusBadGateway, w.Code)
		assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
		assert.NotContains(t, w.Body.String(), forward)
	})

	t.Run("client reports local service down", func(t *testing.T) {
		// Stands in for the pgrok client whose local service is down.
		client := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(dynamicforward.UpstreamErrorHeader, "1")
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer client.Close()

		c := NewCluster(logx.NewNoopLogger(), "https://pgrok.example.com")
		_, err := c.Set(host, "alt-"+host, client.Listener.Addr().String())
		require.NoError(t, err)

		w := serve(c, browserAccept)
		assert.Equal(t, http.StatusBadGateway, w.Code)
		assert.Empty(t, w.Header().Get(dynamicforward.UpstreamErrorHeader))
		assert.Contains(t, w.Body.String(), "the local service at the other end is not responding")
	})

	t.Run("local service's own 502 passes through", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("app error"))
		}))
		defer upstream.Close()

		c := NewCluster(logx.NewNoopLogger(), "https://pgrok.example.com")
		_, err := c.Set(host, "alt-"+host, upstream.Listener.Addr().String())
		require.NoError(t, err)

		w := serve(c, browserAccept)
		assert.Equal(t, http.StatusBadGateway, w.Code)
		assert.Equal(t, "app error", w.Body.String())
	})
}
