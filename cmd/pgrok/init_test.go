package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeriveForwardAddress(t *testing.T) {
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
			want: "http://localhost:8080",
		},
		{
			name: "port with colon",
			addr: ":8080",
			want: "http://localhost:8080",
		},
		{
			name: "host with port",
			addr: "localhost:8080",
			want: "http://localhost:8080",
		},
		{
			name: "host without port",
			addr: "localhost",
			want: "http://localhost",
		},
		{
			name: "full address with port",
			addr: "http://localhost:8080",
			want: "http://localhost:8080",
		},
		{
			name: "full address without port",
			addr: "http://localhost",
			want: "http://localhost",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := deriveForwardAddress(test.addr)
			assert.Equal(t, test.want, got)
		})
	}
}
