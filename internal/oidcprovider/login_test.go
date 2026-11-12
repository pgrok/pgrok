package oidcprovider

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgrok/pgrok/internal/database"
)

func TestDeniedResponse(t *testing.T) {
	const issuer = "https://pgrok.example.com"
	wantParams := url.Values{
		"error":             {"access_denied"},
		"error_description": {"The user denied the request."},
		"iss":               {issuer},
		"state":             {"s"},
	}
	request := func(mode string) *database.OIDCAuthRequest {
		return &database.OIDCAuthRequest{RedirectURI: "https://app.example.com/cb?x=1", State: "s", ResponseMode: mode}
	}

	t.Run("query", func(t *testing.T) {
		got, formPost, err := DeniedResponse(issuer, request(""))
		require.NoError(t, err)
		assert.Nil(t, formPost)
		u, err := url.Parse(got)
		require.NoError(t, err)
		want := url.Values{"x": {"1"}}
		for k, v := range wantParams {
			want[k] = v
		}
		assert.Equal(t, want, u.Query())
	})

	t.Run("fragment", func(t *testing.T) {
		got, formPost, err := DeniedResponse(issuer, request("fragment"))
		require.NoError(t, err)
		assert.Nil(t, formPost)
		u, err := url.Parse(got)
		require.NoError(t, err)
		params, err := url.ParseQuery(u.EscapedFragment())
		require.NoError(t, err)
		assert.Equal(t, wantParams, params)
	})

	t.Run("form_post", func(t *testing.T) {
		got, formPost, err := DeniedResponse(issuer, request("form_post"))
		require.NoError(t, err)
		assert.Equal(t, "https://app.example.com/cb?x=1", got)
		assert.Equal(t, wantParams, formPost)
	})
}
