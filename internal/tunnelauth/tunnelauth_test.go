package tunnelauth

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientIP(t *testing.T) {
	trusted, err := ParsePrefixes([]string{"127.0.0.1", "10.0.0.0/8"})
	require.NoError(t, err)

	tests := []struct {
		name          string
		remoteAddr    string
		xForwardedFor []string
		want          string
	}{
		{
			name:          "untrusted peer cannot forge X-Forwarded-For",
			remoteAddr:    "198.51.100.1:1234",
			xForwardedFor: []string{"203.0.113.7"},
			want:          "198.51.100.1",
		},
		{
			name:       "trusted peer without X-Forwarded-For",
			remoteAddr: "127.0.0.1:1234",
			want:       "127.0.0.1",
		},
		{
			name:          "trusted peer",
			remoteAddr:    "127.0.0.1:1234",
			xForwardedFor: []string{"203.0.113.7"},
			want:          "203.0.113.7",
		},
		{
			name:          "rightmost untrusted hop wins over forged hops",
			remoteAddr:    "127.0.0.1:1234",
			xForwardedFor: []string{"203.0.113.7, 198.51.100.1", "10.1.2.3"},
			want:          "198.51.100.1",
		},
		{
			name:          "all hops trusted",
			remoteAddr:    "127.0.0.1:1234",
			xForwardedFor: []string{"10.0.0.2, 10.0.0.1"},
			want:          "10.0.0.2",
		},
		{
			name:          "malformed hop",
			remoteAddr:    "127.0.0.1:1234",
			xForwardedFor: []string{"bogus"},
			want:          "invalid IP",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = test.remoteAddr
			for _, v := range test.xForwardedFor {
				r.Header.Add("X-Forwarded-For", v)
			}
			assert.Equal(t, test.want, ClientIP(r, trusted).String())
		})
	}
}
