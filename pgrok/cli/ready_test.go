package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"unknwon.dev/x/logx"
)

func TestOnFirstReady(t *testing.T) {
	var buf bytes.Buffer
	onReady := onFirstReady(logx.NewNoopLogger(), &buf, true, false)

	// No URL from the server, nothing to print yet.
	onReady("")
	assert.Empty(t, buf.String())

	onReady("http://unknwon.example.com")
	first := buf.String()
	assert.NotEmpty(t, first)

	// Reconnects must not print the QR code again.
	onReady("http://unknwon.example.com")
	assert.Equal(t, first, buf.String())
}
