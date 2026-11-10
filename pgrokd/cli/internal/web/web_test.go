package web

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgrok/pgrok/internal/conf"
)

func TestCheckEmail(t *testing.T) {
	newIdP := func(requiredDomain string, allowedEmails ...string) *conf.IdentityProvider {
		idp := &conf.IdentityProvider{
			RequiredDomain: requiredDomain,
			AllowedEmails:  allowedEmails,
		}
		idp.FieldMapping.Email = "email"
		return idp
	}

	tests := []struct {
		name    string
		idp     *conf.IdentityProvider
		claims  map[string]any
		wantErr string
	}{
		{
			name:   "no restrictions",
			idp:    newIdP(""),
			claims: map[string]any{"email": "eve@evil.com"},
		},
		{
			name:   "allowed email",
			idp:    newIdP("", "alice@example.com", "bob@example.com"),
			claims: map[string]any{"email": "bob@example.com"},
		},
		{
			name:   "allowed email is case-insensitive",
			idp:    newIdP("", " Alice@Example.com"),
			claims: map[string]any{"email": "alice@example.COM"},
		},
		{
			name:    "email not in the list",
			idp:     newIdP("", "alice@example.com"),
			claims:  map[string]any{"email": "eve@example.com"},
			wantErr: `the email "eve@example.com" is not in the allowed emails`,
		},
		{
			name:    "email shares a suffix with an allowed email",
			idp:     newIdP("", "alice@example.com"),
			claims:  map[string]any{"email": "malice@example.com"},
			wantErr: "is not in the allowed emails",
		},
		{
			name:    "missing email claim",
			idp:     newIdP("", "alice@example.com"),
			claims:  map[string]any{},
			wantErr: "is not in the allowed emails",
		},
		{
			name:    "unverified email",
			idp:     newIdP("", "alice@example.com"),
			claims:  map[string]any{"email": "alice@example.com", "email_verified": false},
			wantErr: "is not verified",
		},
		{
			name:   "verified email",
			idp:    newIdP("", "alice@example.com"),
			claims: map[string]any{"email": "alice@example.com", "email_verified": true},
		},
		{
			name:    "allowed email outside the required domain",
			idp:     newIdP("example.com", "contractor@other.com"),
			claims:  map[string]any{"email": "contractor@other.com"},
			wantErr: "does not have required domain",
		},
		{
			name:    "required domain but not in the list",
			idp:     newIdP("example.com", "alice@example.com"),
			claims:  map[string]any{"email": "eve@example.com"},
			wantErr: "is not in the allowed emails",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := checkEmail(test.idp, test.claims)
			if test.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, test.wantErr)
			}
		})
	}
}
