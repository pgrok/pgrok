package database

import (
	"bytes"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/assert"
)

func TestGORMLogger(t *testing.T) {
	tests := []struct {
		name   string
		format string
		want   string
	}{
		{
			name:   "normal",
			format: "something",
			want:   "DEBUG something\n",
		},
		{
			name:   "error",
			format: "[error] oops",
			want:   "ERROR [error] oops\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := &gormLogger{
				Logger: log.New(
					log.WithOutput(&buf),
					log.WithLevel(log.DebugLevel),
				),
			}
			logger.Printf(test.format)
			assert.Equal(t, test.want, buf.String())
		})
	}
}
