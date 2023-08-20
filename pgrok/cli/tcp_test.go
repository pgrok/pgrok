package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeriveTCPForwardAddress(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{
			name: "empty",
			addr: "",
			want: "",
		},
		{
			name: "port",
			addr: "8080",
			want: "localhost:8080",
		},
		{
			name: "port with colon",
			addr: ":8080",
			want: "localhost:8080",
		},
		{
			name: "host with port",
			addr: "localhost:8080",
			want: "localhost:8080",
		},
		{
			name: "host without port",
			addr: "localhost",
			want: "localhost",
		},
		{
			name: "full address with port",
			addr: "localhost:8080",
			want: "localhost:8080",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := deriveTCPForwardAddress(test.addr)
			assert.Equal(t, test.want, got)
		})
	}
}
