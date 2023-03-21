package dynamicforward

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/require"
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
		log.Default(),
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
