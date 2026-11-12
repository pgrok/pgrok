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

// newRotatingServer returns the URL of a token endpoint that rotates refresh
// tokens and rejects any but the latest one, issuing access tokens that last
// expiresIn seconds.
func newRotatingServer(t *testing.T, expiresIn int) string {
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
			"access_token": "a" + strconv.Itoa(n), "token_type": "Bearer", "refresh_token": latest, "expires_in": expiresIn,
		})
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// newTokenFunc saves expired credentials for serverURL and returns the token
// function of the tunnel commands and the credentials path.
func newTokenFunc(t *testing.T, serverURL string) (token func() (string, error), path string) {
	configPath := filepath.Join(t.TempDir(), "pgrok.yml")
	path = credentialsPath(configPath)
	require.NoError(t, saveCredentials(path, &credentials{
		ServerURL: serverURL,
		Token:     &oauth2.Token{AccessToken: "a0", RefreshToken: "r0", Expiry: time.Now().Add(-time.Hour)},
	}))
	token, err := tokenFunc(context.Background(), "", configPath)
	require.NoError(t, err)
	return token, path
}

func TestTokenFuncKeepsRotatedTokenWhenSaveFails(t *testing.T) {
	token, path := newTokenFunc(t, newRotatingServer(t, 3600))
	savedRefreshToken := func() string {
		creds, err := loadCredentials(path)
		require.NoError(t, err)
		return creds.Token.RefreshToken
	}

	// A directory where saving writes its temporary file makes the save fail on
	// every platform, so the rotated token is not saved, which is reported.
	require.NoError(t, os.Mkdir(path+".tmp", 0o700))
	_, err := token()
	require.Error(t, err)

	// The tunnel keeps working with the token in memory, not the already used
	// one in the file, while saving still fails.
	got, err := token()
	require.NoError(t, err)
	assert.Equal(t, "a1", got)
	assert.Equal(t, "r0", savedRefreshToken())

	// Once the file can be written again, the next call saves the token.
	require.NoError(t, os.Remove(path+".tmp"))
	got, err = token()
	require.NoError(t, err)
	assert.Equal(t, "a1", got)
	assert.Equal(t, "r1", savedRefreshToken())
}

func TestTokenFuncKeepsLoginToAnotherServer(t *testing.T) {
	token, path := newTokenFunc(t, newRotatingServer(t, 1))
	_, err := token()
	require.NoError(t, err)

	// The user logs in to another server with the same credentials file.
	other := &credentials{ServerURL: "https://other.example.com", Token: &oauth2.Token{AccessToken: "x", RefreshToken: "y"}}
	require.NoError(t, saveCredentials(path, other))

	// Refreshing the token of the first server must not overwrite that login.
	got, err := token()
	require.NoError(t, err)
	assert.Equal(t, "a2", got)
	creds, err := loadCredentials(path)
	require.NoError(t, err)
	assert.Equal(t, other.ServerURL, creds.ServerURL)
	assert.Equal(t, "y", creds.Token.RefreshToken)
}
