package oidcprovider

import (
	"net/url"

	"github.com/zitadel/oidc/v3/pkg/oidc"

	"github.com/pgrok/pgrok/internal/database"
)

// CallbackURL is where the login UI sends the browser once the authorization
// request is approved, so that the provider issues the code to the client.
func CallbackURL(issuer, authRequestID string) string {
	return issuer + PathPrefix + "authorize/callback?id=" + url.QueryEscape(authRequestID)
}

// DeniedRedirectURL is where the login UI sends the browser when the user
// denies the authorization request: the client's redirect URI with an
// "access_denied" error (RFC 6749, section 4.1.2.1). The redirect URI was
// validated against the client when the request was created.
func DeniedRedirectURL(issuer string, req *database.OIDCAuthRequest) (string, error) {
	u, err := url.Parse(req.RedirectURI)
	if err != nil {
		return "", err
	}
	params := url.Values{
		"error":             {"access_denied"},
		"error_description": {"The user denied the request."},
		"iss":               {issuer},
	}
	if req.State != "" {
		params.Set("state", req.State)
	}

	if oidc.ResponseMode(req.ResponseMode) == oidc.ResponseModeFragment {
		u.Fragment = params.Encode()
		return u.String(), nil
	}
	query := u.Query()
	for key, values := range params {
		query[key] = values
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}
