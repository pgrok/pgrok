package dynamicforward

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"unknwon.dev/x/logx"
)

func TestNew(t *testing.T) {
	apiRequested := false
	apiServer := httptest.NewServer(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			apiRequested = true
		}),
	)
	hookRequested := false
	hookServer := httptest.NewServer(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			hookRequested = true
		}),
	)
	defaultRequested := false
	defaultServer := httptest.NewServer(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			defaultRequested = true
		}),
	)

	h, err := New(
		logx.NewNoopLogger(),
		defaultServer.URL,
		Options{},
		Forward{
			Prefix:  "/api",
			Address: apiServer.URL,
		},
		Forward{
			Prefix:  "/hook",
			Address: hookServer.URL,
		},
	)
	require.NoError(t, err)

	t.Run("forward to default", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/echo", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		require.True(t, defaultRequested)
		require.False(t, apiRequested)
		require.False(t, hookRequested)
	})

	t.Run("forward to /api", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/echo", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		require.True(t, defaultRequested)
		require.True(t, apiRequested)
		require.False(t, hookRequested)
	})

	t.Run("forward to /hook", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/hook/echo", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		require.True(t, defaultRequested)
		require.True(t, apiRequested)
		require.True(t, hookRequested)
	})

	t.Run("longest prefix wins", func(t *testing.T) {
		newServer := func(name string) *httptest.Server {
			return httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(name))
				}),
			)
		}
		defaultServer := newServer("default")
		apiServer := newServer("api")
		apiV2Server := newServer("api-v2")

		// The shorter prefix is listed first so that config order alone cannot
		// produce the right answer.
		h, err := New(
			logx.NewNoopLogger(),
			defaultServer.URL,
			Options{},
			Forward{Prefix: "/api", Address: apiServer.URL},
			Forward{Prefix: "/api/v2", Address: apiV2Server.URL},
		)
		require.NoError(t, err)

		tests := []struct {
			path string
			want string
		}{
			{path: "/api/v2/users", want: "api-v2"},
			{path: "/api/v1/users", want: "api"},
			{path: "/apix", want: "api"},
			{path: "/web", want: "default"},
		}
		for _, test := range tests {
			t.Run(test.path, func(t *testing.T) {
				// Repeat to catch nondeterministic rule selection.
				for range 50 {
					w := httptest.NewRecorder()
					h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, test.path, nil))
					require.Equal(t, test.want, w.Body.String())
				}
			})
		}
	})
}

func TestNew_RequestHeaders(t *testing.T) {
	type received struct {
		Host           string
		ForwardedHost  string
		Custom         string
		Accept         string
		Authorization  string
		ForwardedProto string
		ForwardedFor   string
	}
	newServer := func() *url.URL {
		s := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(received{
					Host:           r.Host,
					ForwardedHost:  r.Header.Get("X-Forwarded-Host"),
					Custom:         r.Header.Get("X-Custom"),
					Accept:         r.Header.Get("Accept"),
					Authorization:  r.Header.Get("Authorization"),
					ForwardedProto: r.Header.Get("X-Forwarded-Proto"),
					ForwardedFor:   r.Header.Get("X-Forwarded-For"),
				})
			}),
		)
		t.Cleanup(s.Close)
		u, err := url.Parse(s.URL)
		require.NoError(t, err)
		return u
	}
	defaultURL := newServer()
	apiURL := newServer()
	// Use a different hostname than the default forward address so that a
	// rewrite that ignores the matched rule cannot pass.
	apiURL.Host = "localhost:" + apiURL.Port()

	const publicHost = "unknwon.example.com"
	do := func(t *testing.T, opts Options, path string) received {
		h, err := New(logx.NewNoopLogger(), defaultURL.String(), opts, Forward{Prefix: "/api", Address: apiURL.String()})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = publicHost
		req.Header.Set("Accept", "text/plain")
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-Forwarded-For", "203.0.113.1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var got received
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		return got
	}

	t.Run("keep public host by default", func(t *testing.T) {
		for _, path := range []string{"/", "/api/users"} {
			got := do(t, Options{}, path)
			require.Equal(t, publicHost, got.Host, path)
			require.Equal(t, "text/plain", got.Accept, path)
			require.Equal(t, "Bearer secret", got.Authorization, path)
		}
	})

	t.Run("rewrite to host of the matched forward address", func(t *testing.T) {
		opts := Options{HostHeader: HostHeaderRewrite}

		got := do(t, opts, "/")
		require.Equal(t, defaultURL.Host, got.Host)
		require.Equal(t, publicHost, got.ForwardedHost)

		got = do(t, opts, "/api/users")
		require.Equal(t, apiURL.Host, got.Host)
		require.Equal(t, publicHost, got.ForwardedHost)
		require.Equal(t, "https", got.ForwardedProto)
	})

	t.Run("literal host", func(t *testing.T) {
		for _, path := range []string{"/", "/api/users"} {
			got := do(t, Options{HostHeader: "app.test"}, path)
			require.Equal(t, "app.test", got.Host, path)
			require.Equal(t, publicHost, got.ForwardedHost, path)
		}
	})

	t.Run("set and remove headers", func(t *testing.T) {
		opts := Options{
			RequestHeaders: []string{
				"X-Custom: one, two",
				"accept: application/json",
			},
			RemoveRequestHeaders: []string{"authorization", "x-forwarded-for"},
		}
		for _, path := range []string{"/", "/api/users"} {
			got := do(t, opts, path)
			require.Equal(t, publicHost, got.Host, path)
			require.Equal(t, "one, two", got.Custom, path)
			require.Equal(t, "application/json", got.Accept, path)
			require.Empty(t, got.Authorization, path)
			require.Empty(t, got.ForwardedFor, path)
		}
	})

	t.Run("invalid request headers", func(t *testing.T) {
		for _, header := range []string{"X-Custom", "Bad Name: value", "Host: app.test"} {
			_, err := New(logx.NewNoopLogger(), defaultURL.String(), Options{RequestHeaders: []string{header}})
			require.Error(t, err, header)
		}
	})
}
