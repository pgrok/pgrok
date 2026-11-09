package main

import (
	"os"
	"regexp"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

type Config struct {
	RemoteAddr      string                  `yaml:"remote_addr"`
	ForwardAddr     string                  `yaml:"forward_addr"`
	Token           string                  `yaml:"token"`
	DynamicForwards string                  `yaml:"dynamic_forwards"`
	Tunnels         map[string]TunnelConfig `yaml:"tunnels"`

	HostHeader           string   `yaml:"host_header"`
	RequestHeaders       []string `yaml:"request_headers"`
	RemoveRequestHeaders []string `yaml:"remove_request_headers"`
}

// TunnelConfig is a named tunnel to be run by the "start" subcommand, sharing
// the "remote_addr" and "token" of the top-level config.
type TunnelConfig struct {
	Protocol        string `yaml:"protocol"`
	ForwardAddr     string `yaml:"forward_addr"`
	Subdomain       string `yaml:"subdomain"`        // HTTP only
	DynamicForwards string `yaml:"dynamic_forwards"` // HTTP only
}

// subdomainPrefixRe mirrors the server-side validation of a custom subdomain
// prefix (see internal/sshd), so invalid values are reported before connecting.
var subdomainPrefixRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

func (c TunnelConfig) validate() error {
	switch c.Protocol {
	case protocolHTTP, protocolTCP:
	default:
		return errors.Errorf("unsupported protocol %q, must be %q or %q", c.Protocol, protocolHTTP, protocolTCP)
	}
	if c.ForwardAddr == "" {
		return errors.New(`"forward_addr" is required`)
	}
	if c.Protocol != protocolHTTP {
		if c.Subdomain != "" {
			return errors.New(`"subdomain" is only supported for http tunnels`)
		}
		if c.DynamicForwards != "" {
			return errors.New(`"dynamic_forwards" is only supported for http tunnels`)
		}
	}
	if c.Subdomain != "" && !subdomainPrefixRe.MatchString(c.Subdomain) {
		return errors.Errorf(`invalid "subdomain" %q: must contain only lowercase letters, digits, and hyphens, with no leading or trailing hyphen`, c.Subdomain)
	}
	return nil
}

func loadConfig(configPath string) (*Config, error) {
	p, err := os.ReadFile(configPath)
	if err != nil {
		return nil, errors.Wrap(err, "read file")
	}

	var config Config
	err = yaml.Unmarshal(p, &config)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal")
	}

	for name, tunnel := range config.Tunnels {
		err = tunnel.validate()
		if err != nil {
			return nil, errors.Wrapf(err, "tunnel %q", name)
		}
	}
	return &config, nil
}
