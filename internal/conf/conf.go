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
}

type Proxy struct {
	Port   int    `yaml:"port"`
	Scheme string `yaml:"scheme"`
	Domain string `yaml:"domain"`
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
	RequiredDomain string `yaml:"required_domain"`
}

// Load returns the config loaded from the given path.
func Load(configPath string) (*Config, error) {
	p, err := os.ReadFile(configPath)
	if err != nil {
		return nil, errors.Wrap(err, "read file")
	}

	var config Config
	err = yaml.Unmarshal(p, &config)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal")
	}

	config.ExternalURL = strings.TrimSuffix(config.ExternalURL, "/")

	if idp := config.IdentityProvider; idp != nil {
		if idp.RequiredDomain != "" && idp.FieldMapping.Email == "" {
			return nil, errors.New("cannot require email domain without field mapping for email")
		}
	}
	return &config, nil
}
