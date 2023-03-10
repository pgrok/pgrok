package strutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRandomChars(t *testing.T) {
	cache := make(map[string]bool)
	for i := 0; i < 100; i++ {
		require.NotPanics(t, func() {
			chars := MustRandomChars(10)
			assert.False(t, cache[chars], "duplicated chars %q", chars)
			cache[chars] = true
		})
	}
}

func TestCoalesce(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{[]string{"a", "b"}, "a"},
		{[]string{"", "b", "c"}, "b"},
		{[]string{"", "", ""}, ""},
	}
	for _, test := range tests {
		t.Run(test.want, func(t *testing.T) {
			got := Coalesce(test.in...)
			assert.Equal(t, test.want, got)
		})
	}
}
