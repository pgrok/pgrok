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
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "a" + strconv.Itoa(n), "token_type": "Bearer", "refresh_token": latest, "expires_in": 3600,
		})
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "pgrok.yml")
	path := credentialsPath(configPath)
	require.NoError(t, saveCredentials(path, &credentials{
		ServerURL: server.URL,
		Token:     &oauth2.Token{AccessToken: "a0", RefreshToken: "r0", Expiry: time.Now().Add(-time.Hour)},
	}))
	token, err := tokenFunc(context.Background(), "", configPath)
	require.NoError(t, err)

	// A directory where saving writes its temporary file makes the save fail on
	// every platform, so the rotated token is not saved.
	require.NoError(t, os.Mkdir(path+".tmp", 0o700))
	_, err = token()
	require.Error(t, err)

	// Once the file can be written again, the next call uses the rotated token
	// kept in memory, not the already used one in the file, and saves it.
	require.NoError(t, os.Remove(path+".tmp"))
	got, err := token()
	require.NoError(t, err)
	assert.Equal(t, "a1", got)
	saved, err := loadCredentials(path)
	require.NoError(t, err)
	assert.Equal(t, "r1", saved.Token.RefreshToken)
}
