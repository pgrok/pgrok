package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgrok/pgrok/pgrokd/cli/internal/web"
)

func TestLoadConfig(t *testing.T) {
	t.Setenv("PGROKD_TEST_PASSWORD", "p#ss;word")

	// The file only sets some options, all others keep the defaults.
	const testINI = `[server]
EXTERNAL_URL = http://example.com/

[proxy]
DOMAIN = example.com
TCP_PORT_END = 20000

[database]
PASSWORD = ${PGROKD_TEST_PASSWORD}

[identity_provider]
TYPE = oidc
DISPLAY_NAME = Google
CLAIM_DISPLAY_NAME = given_name
ALLOWED_EMAILS = a@example.com, b@example.com
`
	const testYAML = `external_url: "http://example.com/"
proxy:
  domain: "example.com"
  tcp:
    port_end: 20000
database:
  password: "p#ss;word"
identity_provider:
  display_name: "Google"
  field_mapping:
    display_name: "given_name"
  allowed_emails:
    - "a@example.com"
    - "b@example.com"
`

	want := &Config{}
	want.Server.ExternalURL = "http://example.com"
	want.Web.Port = 3320
	want.Proxy.Port = 3000
	want.Proxy.Scheme = "http"
	want.Proxy.Domain = "example.com"
	want.Proxy.TCPPortStart = 10000
	want.Proxy.TCPPortEnd = 20000
	want.SSHD.Port = 2222
	want.TLS.Port = 443
	want.TLS.ACMECA = "https://acme-v02.api.letsencrypt.org/directory"
	want.Database.Host = "localhost"
	want.Database.Port = 5432
	want.Database.User = "pgrokd"
	want.Database.Password = "p#ss;word"
	want.Database.Name = "pgrokd"
	want.IdentityProvider = web.IdentityProvider{
		Type:             "oidc",
		DisplayName:      "Google",
		ClaimIdentifier:  "email",
		ClaimDisplayName: "given_name",
		ClaimEmail:       "email",
		AllowedEmails:    []string{"a@example.com", "b@example.com"},
	}

	tests := []struct {
		name    string
		files   map[string]string
		wantErr string
	}{
		{
			name:  "ini",
			files: map[string]string{"pgrokd.ini": testINI},
		},
		{
			name:  "yaml when there is no ini",
			files: map[string]string{"pgrokd.yml": testYAML},
		},
		{
			name: "ini wins over yaml",
			files: map[string]string{
				"pgrokd.ini": testINI,
				"pgrokd.yml": `external_url: "http://yaml"`,
			},
		},
		{
			name:    "invalid value",
			files:   map[string]string{"pgrokd.ini": "[proxy]\nPORT = abc\n"},
			wantErr: `set field "PORT"`,
		},
		{
			name:    "tls with http external url",
			files:   map[string]string{"pgrokd.ini": "[tls]\nCERT_FILES = a.pem\nKEY_FILES = a.key\n"},
			wantErr: "EXTERNAL_URL must use https",
		},
		{
			name:    "tls with both certificate files and acme",
			files:   map[string]string{"pgrokd.ini": "[server]\nEXTERNAL_URL = https://example.com\n[tls]\nCERT_FILES = a.pem\nKEY_FILES = a.key\nACME_DNS_PROVIDER = builtin\n"},
			wantErr: "only one of CERT_FILES and ACME_DNS_PROVIDER",
		},
		{
			name:    "tls with more certificate files than keys",
			files:   map[string]string{"pgrokd.ini": "[server]\nEXTERNAL_URL = https://example.com\n[tls]\nCERT_FILES = a.pem, b.pem\nKEY_FILES = a.key\n"},
			wantErr: "same number of files",
		},
		{
			name:    "allowed emails without email claim",
			files:   map[string]string{"pgrokd.ini": "[identity_provider]\nCLAIM_EMAIL =\nALLOWED_EMAILS = a@example.com\n"},
			wantErr: "ALLOWED_EMAILS requires CLAIM_EMAIL",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range test.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
			}

			got, err := loadConfig(findConfigFile(filepath.Join(dir, "pgrokd.ini")))
			if test.wantErr != "" {
				assert.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}

	t.Run("tls with acme", func(t *testing.T) {
		t.Setenv("PGROKD_TEST_DNS_TOKEN", "secret")
		dir := t.TempDir()
		path := filepath.Join(dir, "pgrokd.ini")
		require.NoError(t, os.WriteFile(path, []byte(`[server]
EXTERNAL_URL = https://example.com

[proxy]
SCHEME = http

[tls]
HTTP_REDIRECT_PORT = 80
ACME_DNS_PROVIDER = cloudflare

[tls.acme_dns]
API_TOKEN = ${PGROKD_TEST_DNS_TOKEN}
`), 0644))

		got, err := loadConfig(path)
		require.NoError(t, err)
		assert.Equal(t, 443, got.TLS.Port)
		assert.Equal(t, 80, got.TLS.HTTPRedirectPort)
		assert.Equal(t, "cloudflare", got.TLS.ACMEDNSProvider)
		// Settings are only read from their own section, not inherited from [tls].
		assert.Equal(t, map[string]string{"API_TOKEN": "secret"}, got.TLS.ACMEDNSSettings)
		assert.Equal(t, filepath.Join(dir, "acme"), got.TLS.ACMEStoragePath)
		assert.Equal(t, "https", got.webConfig().ProxyScheme)
	})

	t.Run("no identity provider", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pgrokd.ini")
		require.NoError(t, os.WriteFile(path, nil, 0644))
		got, err := loadConfig(path)
		require.NoError(t, err)
		assert.Nil(t, got.webConfig().IdentityProvider)
	})
}
