package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestTokenFuncKeepsRotatedTokenWhenSaveFails(t *testing.T) {
	// The server rotates refresh tokens and rejects any but the latest one.
	var mu sync.Mutex
	latest, n := "r0", 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.FormValue("refresh_token") != latest {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		n++
		latest = "r" + strconv.Itoa(n)
		// Already expired, so that every call refreshes.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "a" + strconv.Itoa(n), "token_type": "Bearer", "refresh_token": latest, "expires_in": 1,
		})
	}))
	defer server.Close()

	dir := filepath.Join(t.TempDir(), "pgrok")
	configPath := filepath.Join(dir, "pgrok.yml")
	require.NoError(t, saveCredentials(credentialsPath(configPath), &credentials{
		ServerURL: server.URL,
		Token:     &oauth2.Token{AccessToken: "a0", RefreshToken: "r0", Expiry: time.Now().Add(-time.Hour)},
	}))
	token, err := tokenFunc(context.Background(), "", configPath)
	require.NoError(t, err)

	// The credentials file cannot be written, so the rotated token is not saved.
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	_, err = token()
	require.Error(t, err)

	// Once the file can be written again, the next refresh must use the
	// rotated token kept in memory, not the already used one in the file.
	require.NoError(t, os.Chmod(dir, 0o700))
	got, err := token()
	require.NoError(t, err, "refresh after a failed save")
	assert.Equal(t, "a2", got)
}
