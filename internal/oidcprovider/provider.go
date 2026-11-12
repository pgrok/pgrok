// Package oidcprovider makes pgrokd an OpenID Provider, with zitadel/oidc
// handling the protocol and the pgrokd database storing its state.
package oidcprovider

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/zitadel/oidc/v3/pkg/op"
	"golang.org/x/text/language"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/conf"
	"github.com/pgrok/pgrok/internal/database"
)

// PathPrefix is the prefix of every provider endpoint other than discovery.
const PathPrefix = "/oauth2/"

// New returns the OpenID Provider for the config, creating its keys on first
// use.
func New(ctx context.Context, config *conf.Config, db *database.DB) (*op.Provider, error) {
	clients, err := newClients(config.ExternalURL, config.OIDCProvider.Clients)
	if err != nil {
		return nil, errors.Wrap(err, "load clients")
	}
	signingKeys, accessTokenKey, err := loadKeys(ctx, db)
	if err != nil {
		return nil, errors.Wrap(err, "load keys")
	}
	storage := &Storage{
		db:          db,
		clients:     clients,
		signingKeys: signingKeys,
	}

	var opts []op.Option
	if strings.HasPrefix(config.ExternalURL, "http://") {
		opts = append(opts, op.WithAllowInsecure())
	}
	endpoint := func(name string) *op.Endpoint { return op.NewEndpoint(PathPrefix + name) }
	opts = append(opts,
		op.WithCustomEndpoints(
			endpoint("authorize"),
			endpoint("token"),
			endpoint("userinfo"),
			endpoint("revoke"),
			endpoint("end_session"),
			endpoint("keys"),
		),
		op.WithCustomIntrospectionEndpoint(endpoint("introspect")),
		op.WithCustomDeviceAuthorizationEndpoint(endpoint("device_authorization")),
	)

	provider, err := op.NewProvider(
		&op.Config{
			CryptoKey:                accessTokenKey,
			CryptoKeyId:              accessTokenKeyID,
			DefaultLogoutRedirectURI: config.ExternalURL + "/",
			CodeMethodS256:           true,
			AuthMethodPost:           true,
			GrantTypeRefreshToken:    true,
			SupportedUILocales:       []language.Tag{language.English},
			DeviceAuthorization: op.DeviceAuthorizationConfig{
				Lifetime:     deviceAuthorizationLifetime,
				PollInterval: devicePollInterval,
				UserFormPath: DevicePath,
				UserCode:     op.UserCodeBase20,
			},
		},
		storage,
		op.StaticIssuer(config.ExternalURL),
		opts...,
	)
	if err != nil {
		return nil, errors.Wrap(err, "create provider")
	}
	return provider, nil
}

// Handles reports whether the provider serves the request's path.
func Handles(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, PathPrefix) || strings.HasPrefix(r.URL.Path, "/.well-known/")
}

// DeleteExpiredRecords deletes expired provider records every interval until
// ctx is done.
func DeleteExpiredRecords(ctx context.Context, logger *logx.Logger, db *database.DB, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := db.DeleteExpiredOIDCRecords(ctx); err != nil && ctx.Err() == nil {
			logger.Error("Failed to delete expired OIDC provider records", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// PrincipalIDFromAccessToken returns the principal of an unexpired access token
// that the provider issued to the pgrok CLI. Tokens of other clients are
// rejected, so that apps the user signed in to cannot open tunnels.
func PrincipalIDFromAccessToken(ctx context.Context, provider *op.Provider, db *database.DB, accessToken string) (int64, error) {
	tokenIDSubject, err := provider.Crypto().Decrypt(accessToken)
	if err != nil {
		return 0, errors.New("invalid access token")
	}
	tokenID, subject, ok := strings.Cut(tokenIDSubject, ":")
	if !ok {
		return 0, errors.New("invalid access token")
	}
	token, err := db.GetOIDCAccessToken(ctx, tokenID)
	if err != nil {
		return 0, errors.Wrap(err, "get access token")
	}
	if token.Subject != subject || token.ClientID != CLIClientID {
		return 0, errors.New("access token was not issued to the pgrok CLI")
	}
	return strconv.ParseInt(token.Subject, 10, 64)
}
