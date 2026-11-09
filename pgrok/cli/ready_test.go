package main

import (
	"bytes"
	"sync"
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

func TestOnNewURL_Concurrent(t *testing.T) {
	render := func(hostURL string) string {
		var buf bytes.Buffer
		onNewURL(&buf)(hostURL)
		return buf.String()
	}
	web := render("http://web.example.com")
	db := render("tcp://example.com:10001")

	// Tunnels of "pgrok start" share stdout and become ready concurrently, their
	// QR codes must not interleave.
	var buf bytes.Buffer
	var wg sync.WaitGroup
	for _, hostURL := range []string{"http://web.example.com", "tcp://example.com:10001"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			onNewURL(&buf)(hostURL)
		}()
	}
	wg.Wait()
	assert.Contains(t, []string{web + db, db + web}, buf.String())
}
