package main

import (
	"context"
	"encoding/gob"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/coreos/go-oidc"
	"github.com/flamego/flamego"
	"github.com/flamego/session"
	"github.com/flamego/session/postgres"
	"github.com/flamego/template"
	"github.com/pelletier/go-toml/v2"
	"github.com/pkg/errors"
	"github.com/puzpuzpuz/xsync/v2"
	"golang.org/x/oauth2"

	"github.com/pgrok/pgrok/internal/cryptoutil"
	"github.com/pgrok/pgrok/internal/reverseproxy"
	"github.com/pgrok/pgrok/internal/sshd"
	"github.com/pgrok/pgrok/internal/strutil"
)

func main() {
	listenAddr := flag.String("listen-addr", "0.0.0.0:3320", "the web server listen address")
	proxyAddr := flag.String("proxy-addr", "0.0.0.0:3000", "the proxy server listen address")
	sshdPort := flag.Int("sshd-port", 2222, "the port number of the SSH server")
	configPath := flag.String("config", "app.toml", "the path to the config file")
	flag.Parse()

	if flamego.Env() == flamego.EnvTypeDev {
		log.SetLevel(log.DebugLevel)
	}
	log.SetTimeFormat("2006-01-02 15:04:05")

	config, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal("Failed to load config",
			"config", *configPath,
			"error", err.Error(),
		)
	}

	tokenToHost := xsync.NewMap()
	proxies := reverseproxy.NewCluster()
	go startSSHServer(*sshdPort, tokenToHost, proxies)
	go startProxyServer(*proxyAddr, proxies)
	go startWebServer(*listenAddr, config, tokenToHost)

	select {}
}

type Config struct {
	ExternalURL string `toml:"external_url"`
	Database    struct {
		Host     string `toml:"host"`
		Port     int    `toml:"port"`
		User     string `toml:"user"`
		Password string `toml:"password"`
		Database string `toml:"database"`
	} `toml:"database"`
	IdentityProvider *ConfigIdentityProvider `toml:"identity_provider"`
}

type ConfigIdentityProvider struct {
	Type         string `toml:"type"`
	DisplayName  string `toml:"display_name"`
	Issuer       string `toml:"issuer"`
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`
	FieldMapping struct {
		Identifier  string `toml:"identifier"`
		DisplayName string `toml:"display_name"`
		Email       string `toml:"email"`
	} `toml:"field_mapping"`
}

func loadConfig(configPath string) (*Config, error) {
	p, err := os.ReadFile(configPath)
	if err != nil {
		return nil, errors.Wrap(err, "read file")
	}

	var config Config
	err = toml.Unmarshal(p, &config)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal")
	}

	config.ExternalURL = strings.TrimSuffix(config.ExternalURL, "/")
	return &config, nil
}

func startSSHServer(sshdPort int, tokenToHost *xsync.Map, proxies *reverseproxy.Cluster) {
	logger := log.New(
		log.WithTimestamp(),
		log.WithTimeFormat("2006-01-02 15:04:05"),
		log.WithPrefix("sshd"),
		log.WithLevel(log.DebugLevel),
	)

	err := sshd.Start(
		logger,
		sshdPort,
		func(token, forward string) {
			host, _ := tokenToHost.Load(token)
			proxies.Set(host.(string), forward)
		},
		func(token string) {
			host, _ := tokenToHost.Load(token)
			proxies.Remove(host.(string))
		},
	)
	if err != nil {
		logger.Fatal("Failed to start server", "error", err)
	}
}

func startProxyServer(proxyAddr string, proxies *reverseproxy.Cluster) {
	logger := log.New(
		log.WithTimestamp(),
		log.WithTimeFormat("2006-01-02 15:04:05"),
		log.WithPrefix("proxy"),
		log.WithLevel(log.DebugLevel),
	)

	f := flamego.New()
	f.Use(flamego.Recovery())
	f.Any("/{**}", func(w http.ResponseWriter, r *http.Request) {
		proxy, ok := proxies.Get(r.Host)
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("No reverse proxy is available for the host: " + r.Host))
			return
		}
		proxy.ServeHTTP(w, r)
	})

	logger.Info("Server listening on", "address", proxyAddr)
	err := http.ListenAndServe(proxyAddr, f)
	if err != nil {
		logger.Fatal("Failed to start server", "error", err)
	}
}

func startWebServer(listenAddr string, config *Config, tokenToHost *xsync.Map) {
	f := flamego.New()
	f.Use(flamego.Logger())
	f.Use(flamego.Recovery())
	f.Use(flamego.Renderer())
	f.Use(session.Sessioner(
		session.Options{
			Initer: postgres.Initer(),
			Config: postgres.Config{
				DSN: fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
					config.Database.User,
					config.Database.Password,
					config.Database.Host,
					config.Database.Port,
					config.Database.Database,
				),
				Table:     "sessions",
				InitTable: true,
			},
			ErrorFunc: func(err error) {
				log.Error("session", "error", err)
			},
		},
	))
	f.Use(template.Templater())

	f.Group("/-", func() {
		f.Get("/sign-in", func(t template.Template, data template.Data) {
			if config.IdentityProvider != nil {
				data["DisplayName"] = config.IdentityProvider.DisplayName
			}
			t.HTML(http.StatusOK, "sign-in")
		})

		gob.Register(UserInfo{}) // todo
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
				fmt.Sprintf(p.Endpoint().AuthURL+"?client_id=%s&redirect_uri=%s&state=%s&nonce=%s&response_type=code&scope=%s",
					config.IdentityProvider.ClientID,
					config.ExternalURL+"/-/oidc/callback",
					nonce,
					nonce,
					url.QueryEscape("openid profile email"),
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
			s.Set("userID", int64(1))   // todo
			s.Set("userInfo", userInfo) // todo

			token := cryptoutil.SHA1(strutil.MustRandomChars(10))
			s.Set("token", token)

			subdomain := strings.NewReplacer("@", "-", ".", "-").Replace(userInfo.Identifier)
			tokenToHost.Store(token, subdomain+".localhost:3000")
			c.Redirect("/")
		})
	})

	f.Group("/",
		func() {
			f.Get("", func(s session.Session, t template.Template, data template.Data) {
				userInfo := s.Get("userInfo").(UserInfo)
				data["DisplayName"] = userInfo.DisplayName

				token := s.Get("token").(string)
				data["Token"] = token

				host, _ := tokenToHost.Load(token)
				data["URL"] = "http://" + host.(string) // todo
				t.HTML(http.StatusOK, "home")
			})
		},
		func(c flamego.Context, s session.Session) {
			userID, ok := s.Get("userID").(int64)
			if !ok || userID <= 0 {
				c.Redirect("/-/sign-in")
				return
			}

			// TODO: Get user by ID
		},
	)

	log.Info("Web server listening on",
		"address", listenAddr,
		"env", flamego.Env(),
	)
	err := http.ListenAndServe(listenAddr, f)
	if err != nil {
		log.Fatal("Failed to start web server", "error", err)
	}
}

type UserInfo struct {
	Identifier  string
	DisplayName string
	Email       string
}

func handleOIDCCallback(ctx context.Context, idp *ConfigIdentityProvider, redirectURL, code, nonce string) (*UserInfo, error) {
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

	userInfo := &UserInfo{}
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
	if idp.FieldMapping.Email != "" {
		if v, ok := claims[idp.FieldMapping.Email].(string); ok {
			userInfo.Email = v
		}
	}
	return userInfo, nil
}
