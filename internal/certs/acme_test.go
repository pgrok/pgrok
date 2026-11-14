package certs

import (
	"testing"
	"time"

	"github.com/libdns/route53"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetSettings(t *testing.T) {
	// INI values are strings, but provider fields may be of other types.
	var got route53.Provider
	err := setSettings(&got, map[string]string{
		"REGION":                "us-east-1",
		"max_retries":           "3",
		"ROUTE53_MAX_WAIT":      "2m",
		"WAIT_FOR_ROUTE53_SYNC": "true",
	})
	require.NoError(t, err)
	assert.Equal(t, "us-east-1", got.Region)
	assert.Equal(t, 3, got.MaxRetries)
	assert.Equal(t, 2*time.Minute, got.Route53MaxWait)
	assert.True(t, got.WaitForRoute53Sync)

	assert.ErrorContains(t, setSettings(&got, map[string]string{"API_TOKN": "x"}), `unknown setting "API_TOKN"`)
	assert.ErrorContains(t, setSettings(&got, map[string]string{"MAX_RETRIES": "many"}), `invalid setting "MAX_RETRIES"`)
	// Fields hidden from JSON are not settings.
	assert.ErrorContains(t, setSettings(&got, map[string]string{"LOGGER": "x"}), "unknown setting")
}
