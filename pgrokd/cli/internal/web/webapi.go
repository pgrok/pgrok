package web

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/flamego/flamego"
	"github.com/pkg/errors"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/database"
	"github.com/pgrok/pgrok/internal/inspector"
	"github.com/pgrok/pgrok/internal/tunnels"
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

// postRegenerateToken replaces the signed-in principal's token with a new one
// and returns it. Clients using the old token can no longer establish new
// connections, but already established SSH sessions are not terminated.
func postRegenerateToken(db *database.DB) flamego.Handler {
	return func(c *Context, r flamego.Render) {
		token, err := db.RegeneratePrincipalToken(c.Request().Context(), c.Principal.ID)
		if err != nil {
			r.JSON(http.StatusInternalServerError, map[string]string{
				"error": "Failed to regenerate token: " + err.Error(),
			})
			return
		}
		r.JSON(http.StatusOK, map[string]string{
			"token": token,
		})
	}
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

type tunnelInfo struct {
	ID          string    `json:"id"`
	Protocol    string    `json:"protocol"`
	URL         string    `json:"url"`
	RemoteAddr  string    `json:"remoteAddr"`
	ConnectedAt time.Time `json:"connectedAt"`
}

// getTunnels lists the active tunnels of the signed-in principal.
func getTunnels(registry *tunnels.Registry) flamego.Handler {
	return func(c *Context, r flamego.Render) {
		list := registry.ListByPrincipal(c.Principal.ID)
		resp := make([]tunnelInfo, 0, len(list))
		for _, t := range list {
			resp = append(resp, tunnelInfo{
				ID:          t.ID,
				Protocol:    t.Protocol,
				URL:         t.URL,
				RemoteAddr:  t.RemoteAddr,
				ConnectedAt: t.ConnectedAt,
			})
		}
		r.JSON(http.StatusOK, resp)
	}
}

// deleteTunnel closes an active tunnel of the signed-in principal by
// disconnecting its client. Tunnels of other principals are reported as not
// found.
func deleteTunnel(logger *logx.Logger, registry *tunnels.Registry) flamego.Handler {
	return func(c *Context, r flamego.Render) {
		t, ok := registry.Get(c.Param("id"))
		if !ok || t.PrincipalID != c.Principal.ID {
			r.JSON(http.StatusNotFound, map[string]string{
				"error": "Tunnel not found",
			})
			return
		}

		// The connection may already be going away on its own, in which case the
		// error is benign because the tunnel ends either way.
		if err := t.Close(); err != nil {
			logger.Debug("Failed to close tunnel", "id", t.ID, "error", err)
		}
		c.ResponseWriter().WriteHeader(http.StatusNoContent)
	}
}

// requireInspector responds 404 when the requests inspector is disabled.
func requireInspector(requests *inspector.Inspector) flamego.Handler {
	return func(r flamego.Render) {
		if requests == nil {
			r.JSON(http.StatusNotFound, map[string]string{
				"error": "The request inspector is disabled on this server",
			})
		}
	}
}

type requestSummary struct {
	ID         string    `json:"id"`
	TunnelID   string    `json:"tunnelId"`
	ReplayOf   string    `json:"replayOf,omitempty"`
	Method     string    `json:"method"`
	Host       string    `json:"host"`
	Path       string    `json:"path"`
	Status     int       `json:"status"`
	StartedAt  time.Time `json:"startedAt"`
	DurationMs float64   `json:"durationMs"`
	Streaming  bool      `json:"streaming"`
}

func newRequestSummary(e *inspector.Entry) requestSummary {
	return requestSummary{
		ID:         e.ID,
		TunnelID:   e.TunnelID,
		ReplayOf:   e.ReplayOf,
		Method:     e.Method,
		Host:       e.Host,
		Path:       e.URI,
		Status:     e.Status,
		StartedAt:  e.StartedAt,
		DurationMs: float64(e.Duration.Microseconds()) / 1000,
		Streaming:  e.Streaming,
	}
}

type requestDetails struct {
	requestSummary
	RequestHeaders        http.Header `json:"requestHeaders"`
	RequestBody           []byte      `json:"requestBody"`
	RequestBodyTruncated  bool        `json:"requestBodyTruncated"`
	ResponseHeaders       http.Header `json:"responseHeaders"`
	ResponseBody          []byte      `json:"responseBody"`
	ResponseBodyTruncated bool        `json:"responseBodyTruncated"`
}

// newRequestDetails returns the details of the entry with credentials redacted
// from headers.
func newRequestDetails(e *inspector.Entry) requestDetails {
	return requestDetails{
		requestSummary:        newRequestSummary(e),
		RequestHeaders:        inspector.Redact(e.RequestHeader),
		RequestBody:           e.RequestBody,
		RequestBodyTruncated:  e.RequestBodyTruncated,
		ResponseHeaders:       inspector.Redact(e.ResponseHeader),
		ResponseBody:          e.ResponseBody,
		ResponseBodyTruncated: e.ResponseBodyTruncated,
	}
}

// getRequests lists recorded requests to tunnels of the signed-in principal,
// newest first, optionally filtered by the "tunnel" query parameter.
func getRequests(requests *inspector.Inspector) flamego.Handler {
	return func(c *Context, r flamego.Render) {
		entries := requests.List(c.Principal.ID, c.Query("tunnel"))
		resp := make([]requestSummary, 0, len(entries))
		for _, e := range entries {
			resp = append(resp, newRequestSummary(e))
		}
		r.JSON(http.StatusOK, resp)
	}
}

// getRequest returns the details of a recorded request of the signed-in
// principal. Requests of other principals are reported as not found.
func getRequest(requests *inspector.Inspector) flamego.Handler {
	return func(c *Context, r flamego.Render) {
		e, ok := requests.Get(c.Principal.ID, c.Param("id"))
		if !ok {
			r.JSON(http.StatusNotFound, map[string]string{
				"error": inspector.ErrNotFound.Error(),
			})
			return
		}
		r.JSON(http.StatusOK, newRequestDetails(e))
	}
}

// postReplayRequest sends a recorded request of the signed-in principal again
// through its tunnel and returns the details of the new recorded request.
func postReplayRequest(logger *logx.Logger, requests *inspector.Inspector) flamego.Handler {
	return func(c *Context, r flamego.Render) {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 30*time.Second)
		defer cancel()

		e, err := requests.Replay(ctx, c.Principal.ID, c.Param("id"))
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, inspector.ErrNotFound):
				status = http.StatusNotFound
			case errors.Is(err, inspector.ErrTunnelGone):
				status = http.StatusConflict
			case errors.Is(err, inspector.ErrNotReplayable):
				status = http.StatusUnprocessableEntity
			default:
				logger.Error("Failed to replay request", "id", c.Param("id"), "error", err)
			}
			r.JSON(status, map[string]string{"error": err.Error()})
			return
		}
		r.JSON(http.StatusCreated, newRequestDetails(e))
	}
}
