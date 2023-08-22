package main

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/coreos/go-oidc"
	"github.com/flamego/flamego"
	"github.com/flamego/session"
	"github.com/flamego/session/postgres"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"

	"github.com/pgrok/pgrok/internal/conf"
	"github.com/pgrok/pgrok/internal/cryptoutil"
	"github.com/pgrok/pgrok/internal/database"
	"github.com/pgrok/pgrok/internal/strutil"
	"github.com/pgrok/pgrok/internal/userutil"
)

//go:embed *
var webAssets embed.FS

func startWebServer(config *conf.Config, db *database.DB) {
	f := flamego.New()
	f.Use(flamego.Logger())
	f.Use(flamego.Recovery())
	f.Use(flamego.Renderer())

	if flamego.Env() == flamego.EnvTypeProd {
		webFS, err := fs.Sub(webAssets, "dist")
		if err != nil {
			log.Fatal("Failed to load embedded web assets", "error", err.Error())
			return
		}
		f.Use(flamego.Static(
			flamego.StaticOptions{
				FileSystem: http.FS(webFS),
			},
		))

		// Make sure the page refresh works
		indexFile, err := webAssets.Open("dist/index.html")
		if err != nil {
			log.Fatal(`Failed to open "dist/index.html"`, "error", err.Error())
			return
		}
		indexFileStat, err := indexFile.Stat()
		if err != nil {
			log.Fatal(`Failed to stat "dist/index.html"`, "error", err.Error())
			return
		}
		index, err := webAssets.ReadFile("dist/index.html")
		if err != nil {
			log.Fatal(`Failed to read "dist/index.html"`, "error", err.Error())
			return
		}
		indexReader := bytes.NewReader(index)
		f.Get("/{**}", func(w http.ResponseWriter, r *http.Request) {
			http.ServeContent(w, r, "index.html", indexFileStat.ModTime(), indexReader)
		})
	} else {
		// Proxy all non-backend URLs to Vite
		viteURL, err := url.Parse("http://localhost:5173")
		if err != nil {
			log.Fatal("Failed to parse vite URL", "error", err.Error())
			return
		}
		viteProxy := httputil.NewSingleHostReverseProxy(viteURL)
		f.Get("/{**}", func(w http.ResponseWriter, r *http.Request) {
			viteProxy.ServeHTTP(w, r)
		})
	}

	var postgresDSN string
	// Check if the host is a UNIX domain socket
	if strings.HasPrefix(config.Database.Host, "/") {
		postgresDSN = fmt.Sprintf("postgres://%s:%s@localhost:%d/%s?host=%s",
			config.Database.User,
			config.Database.Password,
			config.Database.Port,
			config.Database.Database,
			config.Database.Host,
		)
	} else {
		postgresDSN = fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
			config.Database.User,
			config.Database.Password,
			config.Database.Host,
			config.Database.Port,
			config.Database.Database,
		)
	}
	f.Use(session.Sessioner(
		session.Options{
			Initer: postgres.Initer(),
			Config: postgres.Config{
				DSN:       postgresDSN,
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

	// Behind authentication
	f.Group("/api",
		func() {
			f.Get("/user-info", func(r flamego.Render, principle *database.Principal) {
				r.JSON(http.StatusOK, map[string]string{
					"displayName": principle.DisplayName,
					"token":       principle.Token,
					"url":         config.Proxy.Scheme + "://" + principle.Subdomain + "." + config.Proxy.Domain,
				})
			})
		},
		func(c flamego.Context, r flamego.Render, s session.Session) {
			userID, ok := s.Get("userID").(int64)
			if !ok || userID <= 0 {
				c.ResponseWriter().WriteHeader(http.StatusUnauthorized)
				return
			}

			principle, err := db.GetPrincipalByID(c.Request().Context(), userID)
			if err != nil {
				r.PlainText(http.StatusInternalServerError, fmt.Sprintf("Failed to get principle: %v", err))
				return
			}
			c.Map(principle)
		},
	)

	f.Get("/api/identity-provider", func(r flamego.Render) {
		if config.IdentityProvider == nil {
			r.JSON(http.StatusInternalServerError, map[string]string{
				"error": "No identity provider is configured, please ask your admin to configure an identity provider.",
			})
			return
		}
		r.JSON(http.StatusOK, map[string]string{
			"displayName": config.IdentityProvider.DisplayName,
			"authURL":     "/-/oidc/auth",
		})
	})

	f.Group("/-", func() {
		f.Get("/healthcheck", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(http.StatusText(http.StatusOK)))
		})

		f.Get("/oidc/auth", func(c flamego.Context, r flamego.Render, s session.Session) {
			if config.IdentityProvider == nil {
				r.PlainText(http.StatusBadRequest, "Sorry but ask your admin to configure an identity provider first")
				return
			}

			p, err := oidc.NewProvider(c.Request().Context(), config.IdentityProvider.Issuer)
			if err != nil {
				r.PlainText(http.StatusInternalServerError, fmt.Sprintf("Failed to create new provider: %v", err))
				return
			}

			nonce := strutil.MustRandomChars(10)
			s.Set("oidc::nonce", nonce)

			c.Redirect(
				fmt.Sprintf(p.Endpoint().AuthURL+"?client_id=%s&redirect_uri=%s&state=%s&nonce=%s&response_type=code&scope=%s&hd=%s",
					config.IdentityProvider.ClientID,
					config.ExternalURL+"/-/oidc/callback",
					nonce,
					nonce,
					url.QueryEscape("openid profile email"),
					config.IdentityProvider.RequiredDomain,
				),
			)
		})
		f.Get("/oidc/callback", func(c flamego.Context, r flamego.Render, s session.Session) {
			if config.IdentityProvider == nil {
				r.PlainText(http.StatusBadRequest, "Sorry but ask your admin to configure an identity provider first")
				return
			}

			defer func() {
				s.Delete("oidc::nonce")
			}()

			nonce, _ := s.Get("oidc::nonce").(string)
			if got := c.Query("state"); nonce != got {
				r.PlainText(http.StatusBadRequest, fmt.Sprintf("mismatched state, want %q but got %q", nonce, got))
				return
			}

			userInfo, err := handleOIDCCallback(
				c.Request().Context(),
				config.IdentityProvider,
				config.ExternalURL+"/-/oidc/callback",
				c.Query("code"),
				nonce,
			)
			if err != nil {
				r.PlainText(http.StatusInternalServerError, fmt.Sprintf("Failed to handle callback: %v", err))
				return
			}

			subdomain, err := userutil.NormalizeIdentifier(userInfo.Identifier)
			if err != nil {
				r.PlainText(http.StatusBadRequest, fmt.Sprintf("Failed to normalize identifier: %v", err))
				return
			}

			principle, err := db.UpsertPrincipal(
				c.Request().Context(),
				database.UpsertPrincipalOptions{
					Identifier:  userInfo.Identifier,
					DisplayName: userInfo.DisplayName,
					Token:       cryptoutil.SHA1(strutil.MustRandomChars(10)),
					Subdomain:   subdomain,
				},
			)
			if err != nil {
				r.PlainText(http.StatusInternalServerError, fmt.Sprintf("Failed to upsert principle: %v", err))
				return
			}

			s.Set("userID", principle.ID)
			c.Redirect("/")
		})
	})

	address := fmt.Sprintf("0.0.0.0:%d", config.Web.Port)
	log.Info("Web server listening on",
		"address", address,
		"env", flamego.Env(),
	)
	err := http.ListenAndServe(address, f)
	if err != nil {
		log.Fatal("Failed to start web server", "error", err)
	}
}

type idpUserInfo struct {
	Identifier  string
	DisplayName string
}

func handleOIDCCallback(ctx context.Context, idp *conf.IdentityProvider, redirectURL, code, nonce string) (*idpUserInfo, error) {
	p, err := oidc.NewProvider(ctx, idp.Issuer)
	if err != nil {
		return nil, errors.Wrap(err, "create new provider")
	}

	oauth2Config := oauth2.Config{
		ClientID:     idp.ClientID,
		ClientSecret: idp.ClientSecret,
		RedirectURL:  redirectURL,

		// Discovery returns the OAuth2 endpoints.
		Endpoint: p.Endpoint(),
		Scopes:   []string{oidc.ScopeOpenID, "profile", "email"},
	}

	token, err := oauth2Config.Exchange(ctx, code)
	if err != nil {
		return nil, errors.Wrap(err, "exchange token")
	}

	// Extract the ID Token from the access token, see http://openid.net/specs/openid-connect-core-1_0.html#TokenResponse.
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, errors.New(`missing "id_token" from the issuer's authorization response`)
	}

	verifier := p.Verifier(&oidc.Config{ClientID: oauth2Config.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, errors.Wrap(err, "verify raw ID Token")
	}
	if nonce != idToken.Nonce {
		return nil, errors.Errorf("mismatched nonce, want %q but got %q", nonce, idToken.Nonce)
	}

	rawUserInfo, err := p.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err != nil {
		return nil, errors.Wrap(err, "fetch user info")
	}

	var claims map[string]any
	err = rawUserInfo.Claims(&claims)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal claims")
	}
	log.Debug("User info", "claims", claims)

	userInfo := &idpUserInfo{}
	if v, ok := claims[idp.FieldMapping.Identifier].(string); ok {
		userInfo.Identifier = v
	}
	if userInfo.Identifier == "" {
		return nil, errors.Errorf("the field %q is not found in claims or has empty value", idp.FieldMapping.Identifier)
	}

	// Best effort to map optional fields
	if idp.FieldMapping.DisplayName != "" {
		if v, ok := claims[idp.FieldMapping.DisplayName].(string); ok {
			userInfo.DisplayName = v
		}
	}
	if userInfo.DisplayName == "" {
		userInfo.DisplayName = userInfo.Identifier
	}

	if idp.RequiredDomain != "" {
		email, _ := claims[idp.FieldMapping.Email].(string)
		if !strings.HasSuffix(email, "@"+idp.RequiredDomain) {
			return nil, errors.Errorf("the email %q does not have required domain %q", email, idp.RequiredDomain)
		}
	}
	return userInfo, nil
}
