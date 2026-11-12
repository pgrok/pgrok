package web

import (
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/flamego/flamego"
)

func getUserInfo(c *Context, r flamego.Render) {
	// Assume clients reach the SSH server at the host of the external URL on
	// sshd.port, matching the documented setups (e.g., "external_url:
	// http://example.com" with "pgrok init --remote-addr example.com:2222").
	var sshHost string
	if u, err := url.Parse(c.Config.ExternalURL); err == nil {
		sshHost = u.Hostname()
	}

	r.JSON(http.StatusOK, map[string]string{
		"displayName": c.Principal.DisplayName,
		"token":       c.Principal.Token,
		"url":         c.Config.Proxy.Scheme + "://" + c.Principal.Subdomain + "." + c.Config.Proxy.Domain,
		"remoteAddr":  net.JoinHostPort(sshHost, strconv.Itoa(c.Config.SSHD.Port)),
	})
}

// getIdentityProvider reports the configured identity provider so the sign-in
// page can render the correct "Continue with ..." action.
func getIdentityProvider(c *Context, r flamego.Render) {
	if c.Config.IdentityProvider == nil {
		r.JSON(http.StatusInternalServerError, map[string]string{
			"error": "No identity provider is configured, please ask your admin to configure an identity provider.",
		})
		return
	}
	r.JSON(http.StatusOK, map[string]string{
		"displayName": c.Config.IdentityProvider.DisplayName,
		"authURL":     "/-/oidc/auth",
	})
}
