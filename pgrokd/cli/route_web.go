package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/coreos/go-oidc"
	"github.com/flamego/flamego"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"

	"github.com/pgrok/pgrok/internal/conf"
	"github.com/pgrok/pgrok/internal/cryptoutil"
	"github.com/pgrok/pgrok/internal/database"
	"github.com/pgrok/pgrok/internal/strutil"
	"github.com/pgrok/pgrok/internal/userutil"
)

// webHealthcheck reports server liveness.
func webHealthcheck(w http.ResponseWriter) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(http.StatusText(http.StatusOK)))
}

// webOIDCAuth kicks off the OIDC authorization code flow.
func webOIDCAuth(c *Context, r flamego.Render) {
	if c.Config.IdentityProvider == nil {
		r.PlainText(http.StatusBadRequest, "Sorry but ask your admin to configure an identity provider first")
		return
	}

	p, err := oidc.NewProvider(c.Request().Context(), c.Config.IdentityProvider.Issuer)
	if err != nil {
		r.PlainText(http.StatusInternalServerError, fmt.Sprintf("Failed to create new provider: %v", err))
		return
	}

	nonce := strutil.MustRandomChars(10)
	c.Session.Set("oidc::nonce", nonce)

	c.Redirect(
		fmt.Sprintf(p.Endpoint().AuthURL+"?client_id=%s&redirect_uri=%s&state=%s&nonce=%s&response_type=code&scope=%s&hd=%s",
			c.Config.IdentityProvider.ClientID,
			c.Config.ExternalURL+"/-/oidc/callback",
			nonce,
			nonce,
			url.QueryEscape("openid profile email"),
			c.Config.IdentityProvider.RequiredDomain,
		),
	)
}

// webOIDCCallback completes the OIDC flow: verifies the callback, upserts the
// principal, and establishes the session.
func webOIDCCallback(c *Context, r flamego.Render, db *database.DB) {
	if c.Config.IdentityProvider == nil {
		r.PlainText(http.StatusBadRequest, "Sorry but ask your admin to configure an identity provider first")
		return
	}

	defer func() {
		c.Session.Delete("oidc::nonce")
	}()

	nonce, _ := c.Session.Get("oidc::nonce").(string)
	if got := c.Query("state"); nonce != got {
		r.PlainText(http.StatusBadRequest, fmt.Sprintf("mismatched state, want %q but got %q", nonce, got))
		return
	}

	userInfo, err := handleOIDCCallback(
		c.Request().Context(),
		c.Config.IdentityProvider,
		c.Config.ExternalURL+"/-/oidc/callback",
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

	principal, err := db.UpsertPrincipal(
		c.Request().Context(),
		database.UpsertPrincipalOptions{
			Identifier:  userInfo.Identifier,
			DisplayName: userInfo.DisplayName,
			Token:       cryptoutil.SHA1(strutil.MustRandomChars(10)),
			Subdomain:   subdomain,
		},
	)
	if err != nil {
		r.PlainText(http.StatusInternalServerError, fmt.Sprintf("Failed to upsert principal: %v", err))
		return
	}

	c.Session.Set("userID", principal.ID)
	c.Redirect("/")
}

// webSignOut clears the session and returns to the home page.
func webSignOut(c *Context) {
	c.Session.Delete("userID")
	c.Redirect("/")
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
