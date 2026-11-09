package web

import (
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/flamego/flamego"
)

func getUserInfo(c *Context, r flamego.Render) {
	// Clients reach the SSH server at the host of the external URL, the same
	// address the docs tell users to pass to "pgrok init --remote-addr".
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
