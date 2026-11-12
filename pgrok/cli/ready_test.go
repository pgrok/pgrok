package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOnNewURL(t *testing.T) {
	var buf bytes.Buffer
	onReady := onNewURL(&buf)

	// No URL from the server, nothing to print yet.
	onReady("")
	assert.Empty(t, buf.String())

	onReady("http://unknwon.example.com")
	first := buf.String()
	assert.NotEmpty(t, first)

	// Reconnects with the same URL must not print the QR code again.
	onReady("http://unknwon.example.com")
	assert.Equal(t, first, buf.String())

	// Reconnects with a different URL print the new QR code.
	onReady("http://other.example.com")
	assert.Greater(t, buf.Len(), len(first))
}
