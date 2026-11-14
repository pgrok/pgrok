package main

import (
	"cmp"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	"unknwon.dev/ini/v2"

	"github.com/pgrok/pgrok/internal/certs"
	"github.com/pgrok/pgrok/internal/database"
	"github.com/pgrok/pgrok/internal/osutil"
	"github.com/pgrok/pgrok/internal/sshd"
	"github.com/pgrok/pgrok/pgrokd/cli/internal/embed"
	"github.com/pgrok/pgrok/pgrokd/cli/internal/web"
)

// Config is the pgrokd config. All options and their defaults are listed in
// internal/embed/config.ini.
type Config struct {
	Server struct {
		ExternalURL string `ini:"EXTERNAL_URL"`
	} `ini:"server"`
	Web struct {
		Port int `ini:"PORT"`
	} `ini:"web"`
	Proxy struct {
		Port             int `ini:"PORT"`
		sshd.ProxyConfig `ini:",extends"`
	} `ini:"proxy"`
	SSHD struct {
		Port int `ini:"PORT"`
	} `ini:"sshd"`
	TLS struct {
		Port             int `ini:"PORT"`
		HTTPRedirectPort int `ini:"HTTP_REDIRECT_PORT"`
		certs.Config     `ini:",extends"`
	} `ini:"tls"`
	Database         database.Config      `ini:"database"`
	IdentityProvider web.IdentityProvider `ini:"identity_provider"`
}

func (c *Config) webConfig() *web.Config {
	idp := &c.IdentityProvider
	if idp.Type == "" {
		idp = nil
	}
	return &web.Config{
		Port:             c.Web.Port,
		ExternalURL:      c.Server.ExternalURL,
		ProxyScheme:      c.Proxy.Scheme,
		ProxyDomain:      c.Proxy.Domain,
		SSHDPort:         c.SSHD.Port,
		IdentityProvider: idp,
	}
}

func isYAML(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yml" || ext == ".yaml"
}

// findConfigFile returns the given path, or the YAML file next to it (e.g.,
// "pgrokd.yml" for "pgrokd.ini") when only the YAML file exists.
func findConfigFile(path string) string {
	if osutil.IsExist(path) || isYAML(path) {
		return path
	}
	base := strings.TrimSuffix(path, filepath.Ext(path))
	for _, ext := range []string{".yml", ".yaml"} {
		if osutil.IsExist(base + ext) {
			return base + ext
		}
	}
	return path
}

// loadConfig returns the embedded default config overridden by the config file
// at the given path. A YAML file is read as the legacy config.
func loadConfig(path string) (*Config, error) {
	f, err := ini.Load(
		embed.Config,
		ini.ValueMapping(os.ExpandEnv),
		// Values like passwords may contain "#" and ";".
		ini.IgnoreInlineComment(true),
	)
	if err != nil {
		return nil, errors.Wrap(err, "load embedded config")
	}

	var legacy *legacyConfig
	if isYAML(path) {
		legacy, err = loadLegacyConfig(path)
		if err != nil {
			return nil, errors.Wrapf(err, "load legacy config %q", path)
		}
	} else if err = f.Append(path); err != nil {
		return nil, errors.Wrapf(err, "load config %q", path)
	}

	var c Config
	if err = f.StrictMapTo(&c); err != nil {
		return nil, errors.Wrap(err, "map config")
	}
	if legacy != nil {
		legacy.applyTo(&c)
	}

	c.Server.ExternalURL = strings.TrimSuffix(c.Server.ExternalURL, "/")

	if c.TLS.Enabled() {
		if !strings.HasPrefix(c.Server.ExternalURL, "https://") {
			return nil, errors.New("[server] EXTERNAL_URL must use https when [tls] is configured")
		}
		c.TLS.ACMEStoragePath = cmp.Or(c.TLS.ACMEStoragePath, filepath.Join(filepath.Dir(path), "acme"))
		c.TLS.ACMEDNSSettings = make(map[string]string)
		for _, key := range f.Section("tls.acme_dns").Keys() {
			c.TLS.ACMEDNSSettings[key.Name()] = key.String()
		}
		if err = c.TLS.Validate(); err != nil {
			return nil, errors.Wrap(err, "[tls]")
		}
		c.Proxy.Scheme = "https"
	}

	if idp := c.IdentityProvider; idp.ClaimEmail == "" {
		if idp.RequiredDomain != "" {
			return nil, errors.New("[identity_provider] REQUIRED_DOMAIN requires CLAIM_EMAIL")
		}
		if len(idp.AllowedEmails) > 0 {
			return nil, errors.New("[identity_provider] ALLOWED_EMAILS requires CLAIM_EMAIL")
		}
	}
	return &c, nil
}

// legacyConfig is the YAML config used before the INI config. It is still read
// for existing setups but gets no new options.
type legacyConfig struct {
	ExternalURL string `yaml:"external_url"`
	Web         struct {
		Port int `yaml:"port"`
	} `yaml:"web"`
	Proxy struct {
		Port   int    `yaml:"port"`
		Scheme string `yaml:"scheme"`
		Domain string `yaml:"domain"`
		TCP    struct {
			Domain    string `yaml:"domain"`
			PortStart int    `yaml:"port_start"`
			PortEnd   int    `yaml:"port_end"`
		} `yaml:"tcp"`
	} `yaml:"proxy"`
	SSHD struct {
		Port int `yaml:"port"`
	} `yaml:"sshd"`
	Database *struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		User     string `yaml:"user"`
		Password string `yaml:"password"`
		Database string `yaml:"database"`
	} `yaml:"database"`
	IdentityProvider *struct {
		Type         string `yaml:"type"`
		DisplayName  string `yaml:"display_name"`
		Issuer       string `yaml:"issuer"`
		ClientID     string `yaml:"client_id"`
		ClientSecret string `yaml:"client_secret"`
		FieldMapping struct {
			Identifier  string `yaml:"identifier"`
			DisplayName string `yaml:"display_name"`
			Email       string `yaml:"email"`
		} `yaml:"field_mapping"`
		RequiredDomain string   `yaml:"required_domain"`
		AllowedEmails  []string `yaml:"allowed_emails"`
	} `yaml:"identity_provider"`
}

func loadLegacyConfig(path string) (*legacyConfig, error) {
	p, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(err, "read file")
	}
	var l legacyConfig
	if err = yaml.Unmarshal(p, &l); err != nil {
		return nil, errors.Wrap(err, "unmarshal")
	}
	return &l, nil
}

// applyTo overrides the defaults in c with the options set in l.
func (l *legacyConfig) applyTo(c *Config) {
	c.Server.ExternalURL = cmp.Or(l.ExternalURL, c.Server.ExternalURL)
	c.Web.Port = cmp.Or(l.Web.Port, c.Web.Port)
	c.Proxy.Port = cmp.Or(l.Proxy.Port, c.Proxy.Port)
	c.Proxy.Scheme = cmp.Or(l.Proxy.Scheme, c.Proxy.Scheme)
	c.Proxy.Domain = cmp.Or(l.Proxy.Domain, c.Proxy.Domain)
	c.Proxy.TCPDomain = cmp.Or(l.Proxy.TCP.Domain, c.Proxy.TCPDomain)
	c.Proxy.TCPPortStart = cmp.Or(l.Proxy.TCP.PortStart, c.Proxy.TCPPortStart)
	c.Proxy.TCPPortEnd = cmp.Or(l.Proxy.TCP.PortEnd, c.Proxy.TCPPortEnd)
	c.SSHD.Port = cmp.Or(l.SSHD.Port, c.SSHD.Port)

	if db := l.Database; db != nil {
		c.Database.Host = cmp.Or(db.Host, c.Database.Host)
		c.Database.Port = cmp.Or(db.Port, c.Database.Port)
		c.Database.User = cmp.Or(db.User, c.Database.User)
		c.Database.Password = cmp.Or(db.Password, c.Database.Password)
		c.Database.Name = cmp.Or(db.Database, c.Database.Name)
	}

	if idp := l.IdentityProvider; idp != nil {
		// The legacy config never checked the type, so any configured identity
		// provider was OIDC.
		c.IdentityProvider.Type = cmp.Or(idp.Type, "oidc")
		c.IdentityProvider.DisplayName = idp.DisplayName
		c.IdentityProvider.Issuer = idp.Issuer
		c.IdentityProvider.ClientID = idp.ClientID
		c.IdentityProvider.ClientSecret = idp.ClientSecret
		c.IdentityProvider.ClaimIdentifier = cmp.Or(idp.FieldMapping.Identifier, c.IdentityProvider.ClaimIdentifier)
		c.IdentityProvider.ClaimDisplayName = cmp.Or(idp.FieldMapping.DisplayName, c.IdentityProvider.ClaimDisplayName)
		c.IdentityProvider.ClaimEmail = cmp.Or(idp.FieldMapping.Email, c.IdentityProvider.ClaimEmail)
		c.IdentityProvider.RequiredDomain = idp.RequiredDomain
		c.IdentityProvider.AllowedEmails = idp.AllowedEmails
	}
}
