package web

import (
	"net/http"

	"github.com/flamego/flamego"
	"github.com/flamego/session"

	"github.com/pgrok/pgrok/internal/conf"
	"github.com/pgrok/pgrok/internal/database"
)

// Context is the request-scoped context shared by the web and API handlers,
// modeled after the *context.Context that Gogs threads through its routers. It
// carries the server config and, when authenticated, the current principal.
type Context struct {
	flamego.Context

	Config    *conf.Config
	Session   session.Session
	Principal *database.Principal // The authenticated principal, or nil when signed out.
}

// Authenticated reports whether the request has an authenticated principal.
func (c *Context) Authenticated() bool {
	return c.Principal != nil
}

// contexter builds and injects a *Context for every request, loading the
// signed-in principal from the session (best effort) so downstream handlers can
// rely on c.Principal / c.IsSignedIn.
func contexter(config *conf.Config, db *database.DB) flamego.Handler {
	return func(ctx flamego.Context, s session.Session) {
		c := &Context{
			Context: ctx,
			Config:  config,
			Session: s,
		}

		if userID, ok := s.Get("userID").(int64); ok && userID > 0 {
			if principal, err := db.GetPrincipalByID(ctx.Request().Context(), userID); err == nil {
				c.Principal = principal
			}
		}

		ctx.Map(c)
	}
}

// authenticate aborts unauthenticated requests with 401.
func authenticate(c *Context) {
	if !c.Authenticated() {
		c.ResponseWriter().WriteHeader(http.StatusUnauthorized)
	}
}
