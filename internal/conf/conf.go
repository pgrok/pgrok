package conf

import (
	"os"
	"strings"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

type Config struct {
	ExternalURL string `yaml:"external_url"`
	Web         struct {
		Port int `yaml:"port"`
	} `yaml:"web"`
	Proxy Proxy `yaml:"proxy"`
	SSHD  struct {
		Port int `yaml:"port"`
	} `yaml:"sshd"`
	Database         *Database         `yaml:"database"`
	IdentityProvider *IdentityProvider `yaml:"identity_provider"`
	OIDCProvider     OIDCProvider      `yaml:"oidc_provider"`
}

// OIDCProvider configures pgrokd as an OpenID Provider, with "external_url" as
// its issuer.
type OIDCProvider struct {
	// Clients are the OAuth 2.0 clients allowed to use the provider in addition
	// to the built-in pgrok CLI client.
	Clients []OIDCClient `yaml:"clients"`
}

// OIDCClient is an OAuth 2.0 client registered with the OpenID Provider.
type OIDCClient struct {
	ID string `yaml:"id"`
	// Name is shown to users when they are asked to approve the client.
	Name string `yaml:"name"`
	// Secret authenticates a confidential client. Leave empty for a public client,
	// such as a single-page or native app, which must then use PKCE.
	Secret                 string   `yaml:"secret"`
	RedirectURIs           []string `yaml:"redirect_uris"`
	PostLogoutRedirectURIs []string `yaml:"post_logout_redirect_uris"`
	// GrantTypes defaults to "authorization_code" and "refresh_token". The others
	// supported are "client_credentials" (confidential clients only) and
	// "urn:ietf:params:oauth:grant-type:device_code".
	GrantTypes []string `yaml:"grant_types"`
	// Trusted skips the consent screen, for first-party apps.
	Trusted bool `yaml:"trusted"`
}

type Proxy struct {
	Port   int    `yaml:"port"`
	Scheme string `yaml:"scheme"`
	Domain string `yaml:"domain"`
	TCP    struct {
		Domain    string `yaml:"domain"`
		PortStart int    `yaml:"port_start"`
		PortEnd   int    `yaml:"port_end"`
	} `yaml:"tcp"`
}

type Database struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
}

type IdentityProvider struct {
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
}

// Load returns the config loaded from the given path.
func Load(configPath string) (*Config, error) {
	p, err := os.ReadFile(configPath)
	if err != nil {
		return nil, errors.Wrap(err, "read file")
	}

	var config Config

	if config.Proxy.TCP.PortStart <= 0 {
		config.Proxy.TCP.PortStart = 10000
	}

	if config.Proxy.TCP.PortEnd <= 0 {
		config.Proxy.TCP.PortEnd = 15000
	}

	err = yaml.Unmarshal(p, &config)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal")
	}

	config.ExternalURL = strings.TrimSuffix(config.ExternalURL, "/")

	if idp := config.IdentityProvider; idp != nil {
		if idp.RequiredDomain != "" && idp.FieldMapping.Email == "" {
			return nil, errors.New("cannot require email domain without field mapping for email")
		}
		if len(idp.AllowedEmails) > 0 && idp.FieldMapping.Email == "" {
			return nil, errors.New("cannot allow emails without field mapping for email")
		}
	}

	seen := make(map[string]bool)
	for _, client := range config.OIDCProvider.Clients {
		if client.ID == "" {
			return nil, errors.New("oidc_provider: client without id")
		}
		if seen[client.ID] {
			return nil, errors.Errorf("oidc_provider: duplicate client id %q", client.ID)
		}
		seen[client.ID] = true
	}
	return &config, nil
}
