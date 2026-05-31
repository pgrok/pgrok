package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/flamego/flamego"
	"github.com/flamego/session"
	"github.com/flamego/session/postgres"
	"github.com/pkg/errors"

	"github.com/pgrok/pgrok/internal/conf"
	"github.com/pgrok/pgrok/internal/database"
)

func newWebServer(config *conf.Config, db *database.DB) (*http.Server, error) {
	f := flamego.New()
	f.Use(flamego.Logger())
	f.Use(flamego.Recovery())
	f.Use(flamego.Renderer())

	// Serve the web app. In prod builds (-tags prod) the assets are embedded;
	// in dev builds requests are proxied to the live Vite server.
	if err := setupWebAssets(f); err != nil {
		return nil, errors.Wrap(err, "set up web assets")
	}

	f.Use(session.Sessioner(
		session.Options{
			Initer: postgres.Initer(),
			Config: postgres.Config{
				DSN:       postgresDSN(config.Database),
				Table:     "sessions",
				InitTable: true,
			},
			Cookie: session.CookieOptions{
				Name: "pgrokd_session",
			},
			ErrorFunc: func(err error) {
				log.Error("session", "error", err)
			},
		},
	))

	// Build the request-scoped context (loads the signed-in principal) before any
	// route handler runs, mirroring Gogs' top-level context middleware.
	f.Use(contexter(config, db))

	// JSON API routes, kept separate from the human-facing web routes the way
	// Gogs splits its api and web handlers.
	f.Group("/api", func() {
		f.Get("/user-info", requireSignIn, apiUserInfo)
		f.Get("/identity-provider", apiIdentityProvider)
	})

	// Human-facing web routes, namespaced under "/-".
	f.Group("/-", func() {
		f.Get("/healthcheck", webHealthcheck)
		f.Get("/oidc/auth", webOIDCAuth)
		f.Get("/oidc/callback", webOIDCCallback(db))
		f.Get("/sign-out", webSignOut)
	})

	address := fmt.Sprintf("0.0.0.0:%d", config.Web.Port)
	log.Info("Web server listening on",
		"address", address,
		"env", flamego.Env(),
	)
	return &http.Server{
		Addr:    address,
		Handler: f,
	}, nil
}

// postgresDSN builds the session store DSN, handling both TCP hosts and UNIX
// domain sockets.
func postgresDSN(config *conf.Database) string {
	// Check if the host is a UNIX domain socket
	if strings.HasPrefix(config.Host, "/") {
		return fmt.Sprintf("postgres://%s:%s@localhost:%d/%s?host=%s",
			config.User,
			config.Password,
			config.Port,
			config.Database,
			config.Host,
		)
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		config.User,
		config.Password,
		config.Host,
		config.Port,
		config.Database,
	)
}
