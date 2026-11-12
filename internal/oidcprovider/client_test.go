package oidcprovider

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgrok/pgrok/internal/conf"
)

func TestNewClientsRejectsUnsafeIDs(t *testing.T) {
	for _, id := range []string{"2", "007", "svc:prod", CLIClientID} {
		t.Run(id, func(t *testing.T) {
			_, err := newClients("https://pgrok.example.com", []conf.OIDCClient{
				{ID: id, Secret: "secret", GrantTypes: []string{"client_credentials"}},
			})
			assert.Error(t, err)
		})
	}

	_, err := newClients("https://pgrok.example.com", []conf.OIDCClient{
		{ID: "svc-2", Secret: "secret", GrantTypes: []string{"client_credentials"}},
	})
	assert.NoError(t, err)
}
