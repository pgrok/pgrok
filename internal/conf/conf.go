package conf

import (
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pkg/errors"
)

type Config struct {
	ExternalURL string `toml:"external_url"`
	Web         struct {
		Port int `toml:"port"`
	} `toml:"web"`
	Proxy struct {
		Port   int    `toml:"port"`
		Scheme string `toml:"scheme"`
		Domain string `toml:"domain"`
	} `toml:"proxy"`
	SSHD struct {
		Port int `toml:"port"`
	} `toml:"sshd"`
	Database         *Database         `toml:"database"`
	IdentityProvider *IdentityProvider `toml:"identity_provider"`
}

type Database struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	User     string `toml:"user"`
	Password string `toml:"password"`
	Database string `toml:"database"`
}

type IdentityProvider struct {
	Type         string `toml:"type"`
	DisplayName  string `toml:"display_name"`
	Issuer       string `toml:"issuer"`
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`
	FieldMapping struct {
		Identifier  string `toml:"identifier"`
		DisplayName string `toml:"display_name"`
	} `toml:"field_mapping"`
}

// Load returns the config loaded from the given path.
func Load(configPath string) (*Config, error) {
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
