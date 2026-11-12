package web

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/flamego/flamego"
	"github.com/pkg/errors"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/pgrok/pgrok/internal/database"
	"github.com/pgrok/pgrok/internal/oidcprovider"
)

// isLocalPath reports whether p is a path on this server, which is safe to
// redirect to. Browsers drop tabs and newlines from URLs and read backslashes
// as slashes, so "/\t/evil.example" and "/\\evil.example" lead off-site and are
// rejected along with every other control character and backslash.
func isLocalPath(p string) bool {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.ContainsRune(p, '\\') {
		return false
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	u, err := url.Parse(p)
	return err == nil && u.Scheme == "" && u.Host == ""
}

// signedInRecentlyEnough reports whether the user signed in recently enough for
// the authorization request: after it was created when it asks for a fresh
// sign-in ("prompt=login", which sets max_age to 0), or within its max_age.
func signedInRecentlyEnough(req *database.OIDCAuthRequest, authTime time.Time) bool {
	if req.MaxAge == nil {
		return true
	}
	return !authTime.Before(req.CreatedAt.Truncate(time.Second)) ||
		time.Since(authTime) <= time.Duration(*req.MaxAge)*time.Second
}

// sameOrigin rejects requests that a page on another origin could have sent,
// to protect state-changing API routes from cross-site request forgery. Forms
// cannot send JSON, and cross-origin scripts cannot send it without a CORS
// preflight, which pgrokd never allows.
func sameOrigin(c *Context) {
	mediaType, _, _ := mime.ParseMediaType(c.Request().Header.Get("Content-Type"))
	origin := c.Request().Header.Get("Origin")
	if mediaType != "application/json" || (origin != "" && origin != c.Config.ExternalURL) {
		http.Error(c.ResponseWriter(), "Cross-origin request rejected", http.StatusForbidden)
	}
}

// oidcProviderClient returns the client of the authorization request.
func oidcProviderClient(c *Context, provider *op.Provider, req *database.OIDCAuthRequest) (*oidcprovider.Client, error) {
	client, err := provider.Storage().GetClientByClientID(c.Request().Context(), req.ClientID)
	if err != nil {
		return nil, err
	}
	return client.(*oidcprovider.Client), nil
}

// getOIDCProviderLogin is where the OpenID Provider sends the browser to sign in
// and approve an authorization request. Signed-out users sign in first. Trusted
// clients are approved right away unless they ask for consent, and the others
// go to the consent page.
func getOIDCProviderLogin(db *database.DB, provider *op.Provider) flamego.Handler {
	return func(c *Context, r flamego.Render) {
		id := c.Query("authRequestID")
		req, err := db.GetOIDCAuthRequestByID(c.Request().Context(), id)
		if err != nil || req.Done {
			r.PlainText(http.StatusBadRequest, "The sign-in request is invalid or has expired. Please go back to the app and try again.")
			return
		}

		authTime, ok := c.AuthTime()
		if !c.Authenticated() || !ok || !signedInRecentlyEnough(req, authTime) {
			c.Redirect("/-/oidc/auth?returnTo=" + url.QueryEscape(c.Request().URL.RequestURI()))
			return
		}

		client, err := oidcProviderClient(c, provider, req)
		if err != nil {
			r.PlainText(http.StatusInternalServerError, "Failed to get client: "+err.Error())
			return
		}
		if client.Trusted() && !slices.Contains(req.Prompt, oidc.PromptConsent) {
			err = db.CompleteOIDCAuthRequest(c.Request().Context(), req.ID, c.Principal.ID, authTime)
			if err != nil {
				r.PlainText(http.StatusInternalServerError, "Failed to approve request: "+err.Error())
				return
			}
			c.Redirect(oidcprovider.CallbackURL(c.Config.ExternalURL, req.ID))
			return
		}
		c.Redirect("/consent?authRequestID=" + url.QueryEscape(req.ID))
	}
}

// getOIDCProviderAuthRequest returns what the consent page shows about an
// authorization request.
func getOIDCProviderAuthRequest(db *database.DB, provider *op.Provider) flamego.Handler {
	return func(c *Context, r flamego.Render) {
		req, err := db.GetOIDCAuthRequestByID(c.Request().Context(), c.Param("id"))
		if err != nil || req.Done {
			r.JSON(http.StatusNotFound, map[string]string{"error": "The sign-in request is invalid or has expired."})
			return
		}
		client, err := oidcProviderClient(c, provider, req)
		if err != nil {
			r.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		r.JSON(http.StatusOK, map[string]any{
			"id":          req.ID,
			"clientName":  client.Name(),
			"redirectURI": req.RedirectURI,
			"scopes":      req.Scopes,
		})
	}
}

// postOIDCProviderAuthRequest records the user's decision on an authorization
// request and returns where to send the browser next.
func postOIDCProviderAuthRequest(db *database.DB) flamego.Handler {
	return func(c *Context, r flamego.Render) {
		var body struct {
			Approve bool `json:"approve"`
		}
		if err := json.NewDecoder(c.Request().Request.Body).Decode(&body); err != nil {
			r.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body."})
			return
		}

		ctx := c.Request().Context()
		req, err := db.GetOIDCAuthRequestByID(ctx, c.Param("id"))
		if err != nil || req.Done {
			r.JSON(http.StatusNotFound, map[string]string{"error": "The sign-in request is invalid or has expired."})
			return
		}

		redirectURL, err := decideOIDCProviderAuthRequest(c, db, req, body.Approve)
		if err != nil {
			r.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		r.JSON(http.StatusOK, map[string]string{"redirectURL": redirectURL})
	}
}

func decideOIDCProviderAuthRequest(c *Context, db *database.DB, req *database.OIDCAuthRequest, approve bool) (string, error) {
	ctx := c.Request().Context()
	if approve {
		authTime, ok := c.AuthTime()
		if !ok || !signedInRecentlyEnough(req, authTime) {
			return "", errors.New("please sign in again")
		}
		if err := db.CompleteOIDCAuthRequest(ctx, req.ID, c.Principal.ID, authTime); err != nil {
			return "", errors.Wrap(err, "approve request")
		}
		return oidcprovider.CallbackURL(c.Config.ExternalURL, req.ID), nil
	}

	redirectURL, err := oidcprovider.DeniedRedirectURL(c.Config.ExternalURL, req)
	if err != nil {
		return "", errors.Wrap(err, "build redirect URL")
	}
	if err = db.DeleteOIDCAuthRequest(ctx, req.ID); err != nil {
		return "", errors.Wrap(err, "delete request")
	}
	return redirectURL, nil
}

// userCodeGuessLimiter limits how many wrong user codes each principal can
// enter, because user codes are short enough to guess (RFC 8628, section 5.1).
type userCodeGuessLimiter struct {
	mu       sync.Mutex
	failures map[int64][]time.Time
}

const (
	userCodeGuessWindow = 10 * time.Minute
	userCodeGuessMax    = 10
)

// allow reports whether the principal may enter another user code.
func (l *userCodeGuessLimiter) allow(principalID int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-userCodeGuessWindow)
	l.failures[principalID] = slices.DeleteFunc(l.failures[principalID], func(t time.Time) bool { return t.Before(cutoff) })
	if len(l.failures[principalID]) == 0 {
		delete(l.failures, principalID)
	}
	return len(l.failures[principalID]) < userCodeGuessMax
}

// fail records a wrong user code entered by the principal.
func (l *userCodeGuessLimiter) fail(principalID int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[principalID] = append(l.failures[principalID], time.Now())
}

// lookupDeviceAuthorization returns the undecided device authorization with the
// user code in the "userCode" route parameter, writing an error response when
// there is none.
func lookupDeviceAuthorization(c *Context, r flamego.Render, db *database.DB, limiter *userCodeGuessLimiter) *database.OIDCDeviceAuthorization {
	if !limiter.allow(c.Principal.ID) {
		r.JSON(http.StatusTooManyRequests, map[string]string{"error": "Too many wrong codes. Please wait a few minutes and try again."})
		return nil
	}
	auth, err := db.GetOIDCDeviceAuthorizationByUserCode(c.Request().Context(), oidcprovider.NormalizeUserCode(c.Param("userCode")))
	if errors.Is(err, database.ErrOIDCRecordNotFound) {
		limiter.fail(c.Principal.ID)
		r.JSON(http.StatusNotFound, map[string]string{"error": "The code is invalid or has expired. Please check it and try again."})
		return nil
	} else if err != nil {
		r.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return nil
	}
	return auth
}

// getOIDCProviderDeviceAuthorization returns what the device page shows about
// the device authorization with a user code.
func getOIDCProviderDeviceAuthorization(db *database.DB, provider *op.Provider, limiter *userCodeGuessLimiter) flamego.Handler {
	return func(c *Context, r flamego.Render) {
		auth := lookupDeviceAuthorization(c, r, db, limiter)
		if auth == nil {
			return
		}
		client, err := provider.Storage().GetClientByClientID(c.Request().Context(), auth.ClientID)
		if err != nil {
			r.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		r.JSON(http.StatusOK, map[string]any{
			"clientName": client.(*oidcprovider.Client).Name(),
			"scopes":     auth.Scopes,
		})
	}
}

// postOIDCProviderDeviceAuthorization records the user's decision on the device
// authorization with a user code. The device learns it on its next poll.
func postOIDCProviderDeviceAuthorization(db *database.DB, limiter *userCodeGuessLimiter) flamego.Handler {
	return func(c *Context, r flamego.Render) {
		var body struct {
			Approve bool `json:"approve"`
		}
		if err := json.NewDecoder(c.Request().Request.Body).Decode(&body); err != nil {
			r.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body."})
			return
		}
		auth := lookupDeviceAuthorization(c, r, db, limiter)
		if auth == nil {
			return
		}
		authTime, ok := c.AuthTime()
		if !ok {
			r.JSON(http.StatusUnauthorized, map[string]string{"error": "Please sign out and sign in again."})
			return
		}
		err := db.DecideOIDCDeviceAuthorization(c.Request().Context(), auth.UserCode, oidcprovider.Subject(c.Principal.ID), authTime, body.Approve)
		if err != nil {
			r.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		r.JSON(http.StatusOK, map[string]bool{"approved": body.Approve})
	}
}
