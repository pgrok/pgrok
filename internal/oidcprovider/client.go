package oidcprovider

import (
	"crypto/subtle"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/pgrok/pgrok/internal/conf"
)

// CLIClientID is the ID of the built-in public client the pgrok CLI uses to
// sign in with the device authorization grant.
const CLIClientID = "pgrok-cli"

// LoginPath is where the provider sends the browser to sign in and approve an
// authorization request, with the request ID in the "authRequestID" parameter.
const LoginPath = "/-/oidc-provider/login"

// Client is an OAuth 2.0 client of the provider.
type Client struct {
	id                     string
	name                   string
	secret                 string
	redirectURIs           []string
	postLogoutRedirectURIs []string
	grantTypes             []oidc.GrantType
	trusted                bool
	devMode                bool
}

var _ op.Client = (*Client)(nil)

func newClients(externalURL string, configs []conf.OIDCClient) (map[string]*Client, error) {
	clients := map[string]*Client{
		CLIClientID: {
			id:         CLIClientID,
			name:       "pgrok CLI",
			grantTypes: []oidc.GrantType{oidc.GrantTypeDeviceCode, oidc.GrantTypeRefreshToken},
		},
	}
	// Allow "http" redirect URIs for clients when pgrokd itself is served over
	// plain HTTP, such as in local development.
	devMode := false
	if u, err := url.Parse(externalURL); err == nil && u.Scheme == "http" {
		devMode = true
	}
	for _, c := range configs {
		if _, ok := clients[c.ID]; ok {
			return nil, errors.Errorf("client ID %q is reserved", c.ID)
		}
		// Client credentials tokens have the client ID as their subject, so it
		// must not look like a user's subject, which is the principal ID.
		// Opaque access tokens join the token ID and subject with a colon.
		if strings.Trim(c.ID, "0123456789") == "" || strings.Contains(c.ID, ":") {
			return nil, errors.Errorf("client ID %q must not be all digits or contain a colon", c.ID)
		}

		client := &Client{
			id:                     c.ID,
			name:                   c.Name,
			secret:                 c.Secret,
			redirectURIs:           c.RedirectURIs,
			postLogoutRedirectURIs: c.PostLogoutRedirectURIs,
			trusted:                c.Trusted,
			devMode:                devMode,
		}
		if client.name == "" {
			client.name = c.ID
		}
		grantTypes := c.GrantTypes
		if len(grantTypes) == 0 {
			grantTypes = []string{string(oidc.GrantTypeCode), string(oidc.GrantTypeRefreshToken)}
		}
		for _, grantType := range grantTypes {
			switch oidc.GrantType(grantType) {
			case oidc.GrantTypeCode, oidc.GrantTypeRefreshToken, oidc.GrantTypeDeviceCode:
			case oidc.GrantTypeClientCredentials:
				if c.Secret == "" {
					return nil, errors.Errorf("client %q: grant type %q requires a secret", c.ID, grantType)
				}
			default:
				return nil, errors.Errorf("client %q: unsupported grant type %q", c.ID, grantType)
			}
			client.grantTypes = append(client.grantTypes, oidc.GrantType(grantType))
		}
		if slices.Contains(client.grantTypes, oidc.GrantTypeCode) && len(c.RedirectURIs) == 0 {
			return nil, errors.Errorf("client %q: grant type %q requires redirect_uris", c.ID, oidc.GrantTypeCode)
		}
		clients[c.ID] = client
	}
	return clients, nil
}

// Name is the client's display name.
func (c *Client) Name() string { return c.name }

// Trusted reports whether users approve the client without a consent screen.
func (c *Client) Trusted() bool { return c.trusted }

// authenticate reports whether the secret is the client's secret.
func (c *Client) authenticate(secret string) bool {
	return c.secret != "" && subtle.ConstantTimeCompare([]byte(c.secret), []byte(secret)) == 1
}

func (c *Client) GetID() string                    { return c.id }
func (c *Client) RedirectURIs() []string           { return c.redirectURIs }
func (c *Client) PostLogoutRedirectURIs() []string { return c.postLogoutRedirectURIs }

func (c *Client) ApplicationType() op.ApplicationType {
	if c.secret == "" {
		return op.ApplicationTypeNative
	}
	return op.ApplicationTypeWeb
}

func (c *Client) AuthMethod() oidc.AuthMethod {
	if c.secret == "" {
		return oidc.AuthMethodNone
	}
	return oidc.AuthMethodBasic
}

func (c *Client) ResponseTypes() []oidc.ResponseType {
	if slices.Contains(c.grantTypes, oidc.GrantTypeCode) {
		return []oidc.ResponseType{oidc.ResponseTypeCode}
	}
	return nil
}

func (c *Client) GrantTypes() []oidc.GrantType { return c.grantTypes }

func (c *Client) LoginURL(authRequestID string) string {
	return LoginPath + "?authRequestID=" + url.QueryEscape(authRequestID)
}

// AccessTokenType is opaque for every client, so that revoking a token takes
// effect immediately. Resource servers validate them at the introspection or
// userinfo endpoint.
func (c *Client) AccessTokenType() op.AccessTokenType { return op.AccessTokenTypeBearer }

func (c *Client) IDTokenLifetime() time.Duration { return idTokenLifetime }
func (c *Client) DevMode() bool                  { return c.devMode }

func (c *Client) RestrictAdditionalIdTokenScopes() func([]string) []string {
	return func(scopes []string) []string { return scopes }
}

func (c *Client) RestrictAdditionalAccessTokenScopes() func([]string) []string {
	return func(scopes []string) []string { return scopes }
}

// IsScopeAllowed allows no scopes beyond the standard OpenID Connect ones.
func (c *Client) IsScopeAllowed(string) bool { return false }

func (c *Client) IDTokenUserinfoClaimsAssertion() bool { return false }
func (c *Client) ClockSkew() time.Duration             { return 0 }
