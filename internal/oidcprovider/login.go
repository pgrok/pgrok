package oidcprovider

import (
	"context"
	"net/url"
	"time"

	"github.com/zitadel/oidc/v3/pkg/oidc"

	"github.com/pgrok/pgrok/internal/database"
)

// ApproveAuthRequest marks the authorization request as approved by the
// principal, who signed in at authTime, and returns where the login UI sends
// the browser next so that the provider issues the code to the client. The URL
// carries a one-time secret instead of the request ID, which the request's
// creator knows, so that only the approving browser can collect the code.
func ApproveAuthRequest(ctx context.Context, db *database.DB, issuer, authRequestID string, principalID int64, authTime time.Time) (callbackURL string, err error) {
	secret, err := randomToken()
	if err != nil {
		return "", err
	}
	err = db.CompleteOIDCAuthRequest(ctx, authRequestID, principalID, authTime, hashToken(secret))
	if err != nil {
		return "", err
	}
	return issuer + PathPrefix + "authorize/callback?id=" + url.QueryEscape(secret), nil
}

// DeniedResponse is where the login UI sends the browser when the user denies
// the authorization request: the client's redirect URI with an
// "access_denied" error (RFC 6749, section 4.1.2.1). For response_mode
// form_post, formPost holds the parameters to POST to redirectURL instead of
// encoding them in it. The redirect URI was validated against the client when
// the request was created.
func DeniedResponse(issuer string, req *database.OIDCAuthRequest) (redirectURL string, formPost url.Values, err error) {
	u, err := url.Parse(req.RedirectURI)
	if err != nil {
		return "", nil, err
	}
	params := url.Values{
		"error":             {"access_denied"},
		"error_description": {"The user denied the request."},
		"iss":               {issuer},
	}
	if req.State != "" {
		params.Set("state", req.State)
	}

	switch oidc.ResponseMode(req.ResponseMode) {
	case oidc.ResponseModeFormPost:
		return u.String(), params, nil
	case oidc.ResponseModeFragment:
		// Setting only Fragment would make String() escape the already
		// encoded parameters a second time.
		u.RawFragment = params.Encode()
		u.Fragment, _ = url.PathUnescape(u.RawFragment)
		return u.String(), nil, nil
	}
	query := u.Query()
	for key, values := range params {
		query[key] = values
	}
	u.RawQuery = query.Encode()
	return u.String(), nil, nil
}
