package sshd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompetingSubdomains(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		subdomain string
		want      []string
	}{
		{
			name:      "single label prefix",
			prefix:    "staging",
			subdomain: "alice",
			want:      []string{"staging-alice"},
		},
		{
			name:      "hyphenated prefix",
			prefix:    "x-staging",
			subdomain: "alice",
			want:      []string{"x-staging-alice", "staging-alice"},
		},
		{
			name:      "hyphenated subdomain",
			prefix:    "a-b-c",
			subdomain: "alice-bob",
			want:      []string{"a-b-c-alice-bob", "b-c-alice-bob", "c-alice-bob"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, competingSubdomains(test.prefix, test.subdomain))
		})
	}
}
