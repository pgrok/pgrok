package database

import (
	"bytes"
	"testing"

	charmlog "charm.land/log/v2"
	"github.com/stretchr/testify/assert"
	"unknwon.dev/x/logx"
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
			want:   "DEBU something\n",
		},
		{
			name:   "error",
			format: "[error] oops",
			want:   "ERRO [error] oops\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var buf bytes.Buffer

			logger := &gormLogger{
				Logger: logx.New(charmlog.NewWithOptions(&buf, charmlog.Options{Level: charmlog.DebugLevel})),
			}
			logger.Printf("%s", test.format)
			assert.Equal(t, test.want, buf.String())
		})
	}
}
