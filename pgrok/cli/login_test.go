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

// newTokenFunc saves expired credentials for serverURL next to configPath and
// returns the token function of a tunnel command and the credentials path.
func newTokenFunc(t *testing.T, configPath, serverURL string) (token func() (string, error), path string) {
	path = credentialsPath(configPath, serverURL)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		require.NoError(t, saveCredentials(path, &credentials{
			ServerURL: serverURL,
			Token:     &oauth2.Token{AccessToken: "a0", RefreshToken: "r0", Expiry: time.Now().Add(-time.Hour)},
		}))
	}
	token, err := tokenFunc(context.Background(), "", configPath, serverURL)
	require.NoError(t, err)
	return token, path
}

func savedRefreshToken(t *testing.T, path string) string {
	creds, err := loadCredentials(path)
	require.NoError(t, err)
	return creds.Token.RefreshToken
}

func TestTokenFuncKeepsRotatedTokenWhenSaveFails(t *testing.T) {
	token, path := newTokenFunc(t, filepath.Join(t.TempDir(), "pgrok.yml"), newRotatingServer(t, 3600))

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
	assert.Equal(t, "r0", savedRefreshToken(t, path))

	// Once the file can be written again, the next call saves the token.
	require.NoError(t, os.Remove(path+".tmp"))
	got, err = token()
	require.NoError(t, err)
	assert.Equal(t, "a1", got)
	assert.Equal(t, "r1", savedRefreshToken(t, path))
}

func TestTokenFuncSharesRotatedTokensAfterLoginToAnotherServer(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "pgrok.yml")
	serverURL := newRotatingServer(t, 1)
	tunnel1, _ := newTokenFunc(t, configPath, serverURL)
	tunnel2, _ := newTokenFunc(t, configPath, serverURL)

	// The user logs in to another server next to the same config file.
	otherPath := credentialsPath(configPath, "https://other.example.com")
	other := &credentials{ServerURL: "https://other.example.com", Token: &oauth2.Token{AccessToken: "x", RefreshToken: "y"}}
	require.NoError(t, saveCredentials(otherPath, other))

	// Both tunnels keep refreshing the shared login in turn, which works only
	// when each picks up the refresh token the other rotated.
	for _, want := range []string{"a1", "a2", "a3"} {
		tunnel := tunnel1
		if want == "a2" {
			tunnel = tunnel2
		}
		got, err := tunnel()
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	assert.Equal(t, "y", savedRefreshToken(t, otherPath))
}

func TestTokenFuncAfterLogout(t *testing.T) {
	token, path := newTokenFunc(t, filepath.Join(t.TempDir(), "pgrok.yml"), newRotatingServer(t, 1))
	_, err := token()
	require.NoError(t, err)

	// "pgrok logout" deletes the credentials, though the server may not have
	// revoked them. The running tunnel keeps its token but does not save it back.
	require.NoError(t, os.Remove(path))
	got, err := token()
	require.NoError(t, err)
	assert.Equal(t, "a2", got)
	assert.NoFileExists(t, path)
}

func TestConfigCredentialsPath(t *testing.T) {
	// Brackets are not a pattern in the directory name.
	configPath := filepath.Join(t.TempDir(), "proj[1", "pgrok.yml")
	_, err := configCredentialsPath(configPath, "")
	assert.True(t, os.IsNotExist(err))

	// Without a server in the config, the only login is used.
	one := credentialsPath(configPath, "https://one.example.com/")
	require.NoError(t, saveCredentials(one, &credentials{ServerURL: "https://one.example.com/", Token: &oauth2.Token{}}))
	got, err := configCredentialsPath(configPath, "")
	require.NoError(t, err)
	assert.Equal(t, one, got)
	got, err = configCredentialsPath(configPath, "https://one.example.com")
	require.NoError(t, err)
	assert.Equal(t, one, got, "the trailing slash does not matter")

	two := credentialsPath(configPath, "http://localhost:3320")
	require.NoError(t, saveCredentials(two, &credentials{ServerURL: "http://localhost:3320", Token: &oauth2.Token{}}))
	_, err = configCredentialsPath(configPath, "")
	assert.Error(t, err)
}
