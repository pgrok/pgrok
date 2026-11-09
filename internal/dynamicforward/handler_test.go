package dynamicforward

import (
	"net/http"
	"net/http/httptest"
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
}

func TestNew_LongestPrefixWins(t *testing.T) {
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
}
