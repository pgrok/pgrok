package web

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsLocalPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/", want: true},
		{path: "/device?user_code=ABCD-EFGH", want: true},
		{path: "/-/oidc-provider/login?authRequestID=x", want: true},
		{path: "", want: false},
		{path: "https://evil.example", want: false},
		{path: "//evil.example", want: false},
		{path: "/\\evil.example", want: false},
		// Browsers drop tabs and newlines, which turns these into "//evil.example".
		{path: "/\t/evil.example", want: false},
		{path: "/\n/evil.example", want: false},
		{path: "/\r/evil.example", want: false},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			assert.Equal(t, test.want, isLocalPath(test.path))
		})
	}
}
