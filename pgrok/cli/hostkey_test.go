package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	charmlog "charm.land/log/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	"unknwon.dev/x/logx"
)

func newTestHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	key, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)
	return key
}

func TestHostKeyCallback(t *testing.T) {
	logger := logx.New(charmlog.New(io.Discard))
	remote := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2222}
	serverKey := newTestHostKey(t)
	attackerKey := newTestHostKey(t)

	t.Run("trust on first use", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "pgrok.yml")
		knownHostsPath := filepath.Join(dir, "known_hosts")

		callback, err := newHostKeyCallback(logger, configPath, "")
		require.NoError(t, err)

		// First connection records the key
		require.NoError(t, callback("example.com:2222", remote, serverKey))
		p, err := os.ReadFile(knownHostsPath)
		require.NoError(t, err)
		assert.Equal(t, "[example.com]:2222 "+string(ssh.MarshalAuthorizedKey(serverKey)), string(p))

		// Reconnecting with the same key succeeds without recording it again
		require.NoError(t, callback("example.com:2222", remote, serverKey))
		got, err := os.ReadFile(knownHostsPath)
		require.NoError(t, err)
		assert.Equal(t, string(p), string(got))

		// A different key is rejected and not recorded
		err = callback("example.com:2222", remote, attackerKey)
		var mismatch *hostKeyMismatchError
		require.ErrorAs(t, err, &mismatch)
		assert.Contains(t, err.Error(), "Expected fingerprint: "+ssh.FingerprintSHA256(serverKey))
		assert.Contains(t, err.Error(), "Received fingerprint: "+ssh.FingerprintSHA256(attackerKey))
		assert.Contains(t, err.Error(), "remove line 1 from "+`"`+knownHostsPath+`"`)
		got, err = os.ReadFile(knownHostsPath)
		require.NoError(t, err)
		assert.Equal(t, string(p), string(got))

		// A different server address is trusted on its own
		require.NoError(t, callback("other.example.com:2222", remote, attackerKey))
		got, err = os.ReadFile(knownHostsPath)
		require.NoError(t, err)
		assert.Equal(t, 2, strings.Count(string(got), "\n"))
	})

	t.Run("pinned fingerprint", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "pgrok.yml")

		callback, err := newHostKeyCallback(logger, configPath, ssh.FingerprintSHA256(serverKey))
		require.NoError(t, err)

		require.NoError(t, callback("example.com:2222", remote, serverKey))

		err = callback("example.com:2222", remote, attackerKey)
		var mismatch *hostKeyMismatchError
		require.ErrorAs(t, err, &mismatch)
		assert.Contains(t, err.Error(), "Expected fingerprint: "+ssh.FingerprintSHA256(serverKey))
		assert.Contains(t, err.Error(), "Received fingerprint: "+ssh.FingerprintSHA256(attackerKey))

		// Pinning explicitly does not use the known_hosts file
		assert.NoFileExists(t, filepath.Join(dir, "known_hosts"))
	})

	t.Run("invalid fingerprint", func(t *testing.T) {
		_, err := newHostKeyCallback(logger, filepath.Join(t.TempDir(), "pgrok.yml"), "abc")
		assert.Error(t, err)
	})
}
