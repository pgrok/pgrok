package userutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeUsername(t *testing.T) {
	testCases := []struct {
		in      string
		out     string
		wantErr bool
	}{
		{in: "username", out: "username"},
		{in: "john@gmail.com", out: "john"},
		{in: "john.appleseed@gmail.com", out: "john.appleseed"},
		{in: "john+test@gmail.com", out: "john-test"},
		{in: "this@is@not-an-email", out: "this-is-not-an-email"},
		{in: "user.na$e", out: "user.na-e"},
		{in: "2039f0923f0", out: "2039f0923f0"},
		{in: "john(test)@gmail.com", out: "john-test-"},
		{in: "bob!", out: "bob-"},
		{in: "john_doe", out: "john_doe"},
		{in: "john__doe", out: "john__doe"},
		{in: "_john", out: "_john"},
		{in: "__john", out: "__john"},
		{in: "bob_", out: "bob_"},
		{in: "bob__", out: "bob__"},
		{in: "user_@name", out: "user_"},
		{in: "user_@name", out: "user_"},
		{in: "user_@name", out: "user_"},
		{in: "1", out: "1"},
		{in: "a", out: "a"},
		{in: "a-", out: "a-"},
		{in: "--username-", out: "username-"},
		{in: "bob.!bob", out: "bob-bob"},
		{in: "bob@@bob", out: "bob-bob"},
		{in: "username.", out: "username"},
		{in: ".username", out: "username"},
		{in: "user..name", out: "user-name"},
		{in: "user.-name", out: "user-name"},
		{in: ".", wantErr: true},
		{in: "-", wantErr: true},
	}

	for _, tc := range testCases {
		out, err := NormalizeIdentifier(tc.in)
		if tc.wantErr {
			assert.Error(t, err)
		} else {
			require.NoError(t, err)
			assert.Equal(t, tc.out, out)
		}
	}
}
