package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	load := func(t *testing.T, content string) (*Config, error) {
		configPath := filepath.Join(t.TempDir(), "pgrok.yml")
		require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))
		return loadConfig(configPath)
	}

	t.Run("without tunnels", func(t *testing.T) {
		got, err := load(t, `
remote_addr: "example.com:2222"
forward_addr: "http://localhost:3000"
token: "secret"
dynamic_forwards: |
  /api http://localhost:8080
`)
		require.NoError(t, err)
		want := &Config{
			RemoteAddr:      "example.com:2222",
			ForwardAddr:     "http://localhost:3000",
			Token:           "secret",
			DynamicForwards: "/api http://localhost:8080\n",
		}
		assert.Equal(t, want, got)
	})

	t.Run("with tunnels", func(t *testing.T) {
		got, err := load(t, `
remote_addr: "example.com:2222"
token: "secret"
tunnels:
  web:
    protocol: http
    forward_addr: 3000
    subdomain: web
    dynamic_forwards: |
      /api http://localhost:8080
  db:
    protocol: tcp
    forward_addr: localhost:5432
`)
		require.NoError(t, err)
		want := &Config{
			RemoteAddr: "example.com:2222",
			Token:      "secret",
			Tunnels: map[string]TunnelConfig{
				"web": {
					Protocol:        "http",
					ForwardAddr:     "3000",
					Subdomain:       "web",
					DynamicForwards: "/api http://localhost:8080\n",
				},
				"db": {
					Protocol:    "tcp",
					ForwardAddr: "localhost:5432",
				},
			},
		}
		assert.Equal(t, want, got)
	})

	invalidTests := []struct {
		name    string
		tunnel  string
		wantErr string
	}{
		{
			name: "missing protocol",
			tunnel: `
    forward_addr: 3000`,
			wantErr: `tunnel "bad": unsupported protocol ""`,
		},
		{
			name: "unsupported protocol",
			tunnel: `
    protocol: udp
    forward_addr: 3000`,
			wantErr: `tunnel "bad": unsupported protocol "udp"`,
		},
		{
			name: "missing forward address",
			tunnel: `
    protocol: http`,
			wantErr: `tunnel "bad": "forward_addr" is required`,
		},
		{
			name: "subdomain for tcp",
			tunnel: `
    protocol: tcp
    forward_addr: 5432
    subdomain: db`,
			wantErr: `tunnel "bad": "subdomain" is only supported for http tunnels`,
		},
		{
			name: "dynamic forwards for tcp",
			tunnel: `
    protocol: tcp
    forward_addr: 5432
    dynamic_forwards: /api http://localhost:8080`,
			wantErr: `tunnel "bad": "dynamic_forwards" is only supported for http tunnels`,
		},
		{
			name: "invalid subdomain",
			tunnel: `
    protocol: http
    forward_addr: 3000
    subdomain: Web_App`,
			wantErr: `tunnel "bad": invalid "subdomain" "Web_App"`,
		},
		{
			name: "subdomain with trailing hyphen",
			tunnel: `
    protocol: http
    forward_addr: 3000
    subdomain: web-`,
			wantErr: `tunnel "bad": invalid "subdomain" "web-"`,
		},
	}
	for _, test := range invalidTests {
		t.Run(test.name, func(t *testing.T) {
			_, err := load(t, "tunnels:\n  bad:"+test.tunnel+"\n")
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantErr)
		})
	}
}
