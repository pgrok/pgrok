package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v3"
	"golang.org/x/oauth2"
	"unknwon.dev/x/logx"
)

// cliClientID is the OAuth client ID of the pgrok CLI on pgrokd.
const cliClientID = "pgrok-cli"

// credentials are what "pgrok login" saves to sign in to the server.
type credentials struct {
	ServerURL string        `json:"server_url"`
	Token     *oauth2.Token `json:"token"`
}

// errLoginExpired is returned when the saved credentials can no longer be
// refreshed, so retrying cannot help.
var errLoginExpired = errors.New(`your login has expired or was revoked, run "pgrok login" again`)

// credentialsPath returns where the credentials are saved, next to the config
// file.
func credentialsPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "credentials.json")
}

func loadCredentials(path string) (*credentials, error) {
	p, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var creds credentials
	if err = json.Unmarshal(p, &creds); err != nil {
		return nil, errors.Wrap(err, "unmarshal")
	}
	if creds.ServerURL == "" || creds.Token == nil {
		return nil, errors.New("incomplete credentials")
	}
	return &creds, nil
}

func saveCredentials(path string, creds *credentials) error {
	p, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return errors.Wrap(err, "marshal")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return errors.Wrap(err, "create directory")
	}
	// Write to a temporary file first, so that an interrupted write never
	// leaves a truncated file behind.
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, p, 0o600); err != nil {
		return errors.Wrap(err, "write file")
	}
	return os.Rename(tmp, path)
}

func oauth2Config(serverURL string) *oauth2.Config {
	serverURL = strings.TrimSuffix(serverURL, "/")
	return &oauth2.Config{
		ClientID: cliClientID,
		Endpoint: oauth2.Endpoint{
			DeviceAuthURL: serverURL + "/oauth2/device_authorization",
			TokenURL:      serverURL + "/oauth2/token",
			AuthStyle:     oauth2.AuthStyleInParams,
		},
		Scopes: []string{"offline_access"},
	}
}

// savingTokenSource saves the credentials whenever the token is refreshed.
// The server rotates refresh tokens, so the previous one stops working. Other
// pgrok processes may share the credentials file, so it also picks up tokens
// that they refreshed.
type savingTokenSource struct {
	ctx   context.Context
	mu    sync.Mutex
	path  string
	creds *credentials
	// savedRefreshToken is the refresh token in the credentials file when this
	// process last read or wrote it. After a failed save the file holds an
	// older, already used one, which must not replace the token in memory.
	savedRefreshToken string
	// otherServer is whether the credentials file last held a login to another
	// server, which this process must not overwrite.
	otherServer bool
}

// reload adopts the token in the credentials file when another process has
// refreshed it, and reports whether it did.
func (s *savingTokenSource) reload() bool {
	creds, err := loadCredentials(s.path)
	s.otherServer = err == nil && creds.ServerURL != s.creds.ServerURL
	if err != nil || s.otherServer || creds.Token.RefreshToken == s.savedRefreshToken {
		return false
	}
	s.creds = creds
	s.savedRefreshToken = creds.Token.RefreshToken
	return true
}

func (s *savingTokenSource) refresh() (*oauth2.Token, error) {
	return oauth2Config(s.creds.ServerURL).TokenSource(s.ctx, s.creds.Token).Token()
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reload()
	token, err := s.refresh()
	var retrieveErr *oauth2.RetrieveError
	if errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "invalid_grant" {
		// Another process may have rotated the refresh token first. Wait for it
		// to save the new one.
		for i := 0; i < 10; i++ {
			time.Sleep(500 * time.Millisecond)
			if s.reload() {
				token, err = s.refresh()
				break
			}
		}
	}
	if err != nil {
		return nil, err
	}
	refreshed := token.AccessToken != s.creds.Token.AccessToken
	s.creds.Token = token
	if s.otherServer || (!refreshed && token.RefreshToken == s.savedRefreshToken) {
		return token, nil
	}
	// Save a new token, or retry a save that failed earlier.
	if err = saveCredentials(s.path, s.creds); err != nil {
		if refreshed {
			return nil, errors.Wrap(err, "save refreshed credentials")
		}
		// The failure was reported when the token was refreshed. Keep using
		// the token, and try to save it again on the next call.
		return token, nil
	}
	s.savedRefreshToken = token.RefreshToken
	return token, nil
}

// tokenFunc returns what the tunnel commands authenticate with on each
// connection attempt: the token from the flag or config file, or else a fresh
// access token from the credentials saved by "pgrok login".
func tokenFunc(ctx context.Context, staticToken, configPath string) (func() (string, error), error) {
	if staticToken != "" {
		return func() (string, error) { return staticToken, nil }, nil
	}

	path := credentialsPath(configPath)
	creds, err := loadCredentials(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New(`no token is configured, run "pgrok login" or set "token" in the config file`)
		}
		return nil, errors.Wrapf(err, "load credentials from %q", path)
	}
	source := &savingTokenSource{ctx: ctx, path: path, creds: creds, savedRefreshToken: creds.Token.RefreshToken}
	// The server also answers "invalid_grant" when it fails to look up the
	// refresh token, such as while its database restarts, so only a repeated one
	// means that the login expired.
	const loginExpiredAfter = 5
	invalidGrants := 0
	return func() (string, error) {
		token, err := source.Token()
		if err != nil {
			var retrieveErr *oauth2.RetrieveError
			if errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "invalid_grant" {
				invalidGrants++
				if invalidGrants >= loginExpiredAfter {
					return "", errLoginExpired
				}
			}
			return "", errors.Wrap(err, "refresh access token")
		}
		invalidGrants = 0
		return token.AccessToken, nil
	}, nil
}

func serverURLFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    "server-url",
		Usage:   `The URL of the pgrokd web server, defaults to "server_url" in the config file`,
		Aliases: []string{"u"},
	}
}

func commandLogin(homeDir string, logger *logx.Logger) *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: "Sign in to the server in a browser",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return actionLogin(ctx, cmd, logger)
		},
		Flags: append(commonFlags(homeDir, logger), serverURLFlag()),
	}
}

func actionLogin(ctx context.Context, cmd *cli.Command, logger *logx.Logger) error {
	configPath := cmd.String("config")
	var configServerURL string
	if config, err := loadConfig(configPath); err == nil {
		configServerURL = config.ServerURL
	}
	serverURL := cmp.Or(cmd.String("server-url"), configServerURL)
	if serverURL == "" {
		logger.FatalContext(ctx, `Please specify the server with "--server-url" or "server_url" in the config file`)
	}

	config := oauth2Config(serverURL)
	auth, err := config.DeviceAuth(ctx)
	if err != nil {
		logger.FatalContext(ctx, "Failed to start signing in", "server", serverURL, "error", err)
	}
	verificationURL := cmp.Or(auth.VerificationURIComplete, auth.VerificationURI)
	fmt.Fprintf(os.Stderr, "To sign in, open this page in your browser:\n\n    %s\n\nand check that it shows the code %s.\n\n", verificationURL, auth.UserCode)

	token, err := config.DeviceAccessToken(ctx, auth)
	if err != nil {
		logger.FatalContext(ctx, "Failed to sign in", "error", err)
	}
	path := credentialsPath(configPath)
	err = saveCredentials(path, &credentials{ServerURL: serverURL, Token: token})
	if err != nil {
		logger.FatalContext(ctx, "Failed to save credentials", "path", path, "error", err)
	}
	logger.Info("Signed in", "credentials", path)
	return nil
}

func commandLogout(homeDir string, logger *logx.Logger) *cli.Command {
	return &cli.Command{
		Name:  "logout",
		Usage: "Sign out and revoke the saved credentials",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return actionLogout(ctx, cmd, logger)
		},
		Flags: commonFlags(homeDir, logger),
	}
}

func actionLogout(ctx context.Context, cmd *cli.Command, logger *logx.Logger) error {
	path := credentialsPath(cmd.String("config"))
	creds, err := loadCredentials(path)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info("Not signed in")
			return nil
		}
		logger.FatalContext(ctx, "Failed to load credentials", "path", path, "error", err)
	}

	// Revoking the refresh token also revokes the access tokens issued with it.
	// Sign out locally even when the server cannot be reached.
	if err = revokeToken(ctx, creds.ServerURL, creds.Token.RefreshToken); err != nil {
		logger.Warn("Failed to revoke credentials on the server", "error", err)
	}
	if err = os.Remove(path); err != nil {
		logger.FatalContext(ctx, "Failed to delete credentials", "path", path, "error", err)
	}
	logger.Info("Signed out")
	return nil
}

// revokeToken revokes the token on the server (RFC 7009).
func revokeToken(ctx context.Context, serverURL, token string) error {
	form := url.Values{"client_id": {cliClientID}, "token": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(serverURL, "/")+"/oauth2/revoke", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}
