package web

import (
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/flamego/flamego"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/database"
	"github.com/pgrok/pgrok/internal/tunnels"
)

type adminPrincipalInfo struct {
	ID            int64     `json:"id"`
	DisplayName   string    `json:"displayName"`
	Identifier    string    `json:"identifier"`
	Subdomain     string    `json:"subdomain"`
	CreatedAt     time.Time `json:"createdAt"`
	Blocked       bool      `json:"blocked"`
	IsAdmin       bool      `json:"isAdmin"`
	IsSelf        bool      `json:"isSelf"` // Whether it is the signed-in admin.
	ActiveTunnels int       `json:"activeTunnels"`
}

// listAdminPrincipals lists all principals with their number of active tunnels.
func listAdminPrincipals(db *database.DB, registry *tunnels.Registry) flamego.Handler {
	return func(c *Context, r flamego.Render) {
		principals, err := db.ListPrincipals(c.Request().Context())
		if err != nil {
			r.JSON(http.StatusInternalServerError, map[string]string{
				"error": "Failed to list principals: " + err.Error(),
			})
			return
		}

		activeTunnels := make(map[int64]int)
		for _, t := range registry.List() {
			activeTunnels[t.PrincipalID]++
		}

		resp := make([]adminPrincipalInfo, 0, len(principals))
		for _, p := range principals {
			resp = append(resp, adminPrincipalInfo{
				ID:            p.ID,
				DisplayName:   p.DisplayName,
				Identifier:    p.Identifier,
				Subdomain:     p.Subdomain,
				CreatedAt:     p.CreatedAt,
				Blocked:       p.Blocked,
				IsAdmin:       slices.Contains(c.Config.Admins, p.Identifier),
				IsSelf:        p.ID == c.Principal.ID,
				ActiveTunnels: activeTunnels[p.ID],
			})
		}
		r.JSON(http.StatusOK, resp)
	}
}

// updateAdminPrincipal blocks or unblocks a principal. Blocking disconnects all
// active tunnels of the principal, and an admin cannot block themselves.
func updateAdminPrincipal(logger *logx.Logger, db *database.DB, registry *tunnels.Registry) flamego.Handler {
	return func(c *Context, r flamego.Render) {
		var form struct {
			Blocked *bool `json:"blocked"`
		}
		if err := json.NewDecoder(c.Request().Request.Body).Decode(&form); err != nil || form.Blocked == nil {
			r.JSON(http.StatusBadRequest, map[string]string{
				"error": `The request body must be a JSON object with a boolean "blocked" field`,
			})
			return
		}

		ctx := c.Request().Context()
		principal, err := db.GetPrincipalByID(ctx, c.ParamInt64("id"))
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				r.JSON(http.StatusNotFound, map[string]string{"error": "User not found"})
				return
			}
			r.JSON(http.StatusInternalServerError, map[string]string{
				"error": "Failed to get principal: " + err.Error(),
			})
			return
		}
		if *form.Blocked && principal.ID == c.Principal.ID {
			r.JSON(http.StatusBadRequest, map[string]string{"error": "You cannot block yourself"})
			return
		}

		err = db.UpdatePrincipalBlocked(ctx, principal.ID, *form.Blocked)
		if err != nil {
			r.JSON(http.StatusInternalServerError, map[string]string{
				"error": "Failed to update principal: " + err.Error(),
			})
			return
		}
		logger.Info("Updated principal blocked state",
			"id", principal.ID,
			"identifier", principal.Identifier,
			"blocked", *form.Blocked,
			"by", c.Principal.Identifier,
		)

		// New SSH connections are rejected from now on, so closing the existing
		// ones is enough to cut off the principal.
		if *form.Blocked {
			for _, t := range registry.ListByPrincipal(principal.ID) {
				if err := t.Close(); err != nil {
					logger.Debug("Failed to close tunnel", "id", t.ID, "error", err)
				}
			}
		}
		c.ResponseWriter().WriteHeader(http.StatusNoContent)
	}
}

// deleteAdminPrincipal deletes a principal and disconnects all of their active
// tunnels. It is not a ban: the person gets a new account with a new token on
// their next sign-in if the identity provider still lets them in. An admin
// cannot delete themselves.
func deleteAdminPrincipal(logger *logx.Logger, db *database.DB, registry *tunnels.Registry) flamego.Handler {
	return func(c *Context, r flamego.Render) {
		ctx := c.Request().Context()
		principal, err := db.GetPrincipalByID(ctx, c.ParamInt64("id"))
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				r.JSON(http.StatusNotFound, map[string]string{"error": "User not found"})
				return
			}
			r.JSON(http.StatusInternalServerError, map[string]string{
				"error": "Failed to get principal: " + err.Error(),
			})
			return
		}
		if principal.ID == c.Principal.ID {
			r.JSON(http.StatusBadRequest, map[string]string{"error": "You cannot delete yourself"})
			return
		}

		// Delete the principal before closing the tunnels, so that its token is
		// already rejected when the clients try to reconnect. Existing dashboard
		// sessions are treated as signed out because the principal no longer
		// exists, and IDs are never reused.
		err = db.DeletePrincipalByID(ctx, principal.ID)
		if err != nil {
			r.JSON(http.StatusInternalServerError, map[string]string{
				"error": "Failed to delete principal: " + err.Error(),
			})
			return
		}
		logger.Info("Deleted principal",
			"id", principal.ID,
			"identifier", principal.Identifier,
			"by", c.Principal.Identifier,
		)

		for _, t := range registry.ListByPrincipal(principal.ID) {
			if err := t.Close(); err != nil {
				logger.Debug("Failed to close tunnel", "id", t.ID, "error", err)
			}
		}
		c.ResponseWriter().WriteHeader(http.StatusNoContent)
	}
}

type adminTunnelInfo struct {
	tunnelInfo
	OwnerID          int64  `json:"ownerId"`
	OwnerDisplayName string `json:"ownerDisplayName"`
	OwnerIdentifier  string `json:"ownerIdentifier"`
}

// listAdminTunnels lists active tunnels of all principals with their owners.
func listAdminTunnels(db *database.DB, registry *tunnels.Registry) flamego.Handler {
	return func(c *Context, r flamego.Render) {
		principals, err := db.ListPrincipals(c.Request().Context())
		if err != nil {
			r.JSON(http.StatusInternalServerError, map[string]string{
				"error": "Failed to list principals: " + err.Error(),
			})
			return
		}
		owners := make(map[int64]*database.Principal, len(principals))
		for _, p := range principals {
			owners[p.ID] = p
		}

		list := registry.List()
		resp := make([]adminTunnelInfo, 0, len(list))
		for _, t := range list {
			info := adminTunnelInfo{
				tunnelInfo: tunnelInfo{
					ID:          t.ID,
					Protocol:    t.Protocol,
					URL:         t.URL,
					RemoteAddr:  t.RemoteAddr,
					ConnectedAt: t.ConnectedAt,
				},
				OwnerID: t.PrincipalID,
			}
			if owner, ok := owners[t.PrincipalID]; ok {
				info.OwnerDisplayName = owner.DisplayName
				info.OwnerIdentifier = owner.Identifier
			}
			resp = append(resp, info)
		}
		r.JSON(http.StatusOK, resp)
	}
}

// deleteAdminTunnel closes any active tunnel by disconnecting its client.
func deleteAdminTunnel(logger *logx.Logger, registry *tunnels.Registry) flamego.Handler {
	return func(c *Context, r flamego.Render) {
		t, ok := registry.Get(c.Param("id"))
		if !ok {
			r.JSON(http.StatusNotFound, map[string]string{"error": "Tunnel not found"})
			return
		}

		// The connection may already be going away on its own, in which case the
		// error is benign because the tunnel ends either way.
		if err := t.Close(); err != nil {
			logger.Debug("Failed to close tunnel", "id", t.ID, "error", err)
		}
		logger.Info("Admin closed tunnel", "id", t.ID, "owner", t.PrincipalID, "by", c.Principal.Identifier)
		c.ResponseWriter().WriteHeader(http.StatusNoContent)
	}
}
