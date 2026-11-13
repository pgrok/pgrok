package main

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
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

	t.Run("known_hosts without trailing newline", func(t *testing.T) {
		dir := t.TempDir()
		knownHostsPath := filepath.Join(dir, "known_hosts")
		otherLine := "[other.example.com]:2222 " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(attackerKey)))
		require.NoError(t, os.WriteFile(knownHostsPath, []byte(otherLine), 0644))

		callback, err := newHostKeyCallback(logger, filepath.Join(dir, "pgrok.yml"), "")
		require.NoError(t, err)
		require.NoError(t, callback("example.com:2222", remote, serverKey))

		got, err := os.ReadFile(knownHostsPath)
		require.NoError(t, err)
		want := otherLine + "\n" + "[example.com]:2222 " + string(ssh.MarshalAuthorizedKey(serverKey))
		assert.Equal(t, want, string(got))
	})

	t.Run("malformed known_hosts", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "known_hosts"), []byte("garbage\n"), 0644))

		callback, err := newHostKeyCallback(logger, filepath.Join(dir, "pgrok.yml"), "")
		require.NoError(t, err)
		err = callback("example.com:2222", remote, serverKey)
		var fileErr *hostKeyFileError
		assert.ErrorAs(t, err, &fileErr)
	})

	t.Run("invalid fingerprint", func(t *testing.T) {
		_, err := newHostKeyCallback(logger, filepath.Join(t.TempDir(), "pgrok.yml"), "abc")
		assert.Error(t, err)
	})
}

// startTestSSHServer starts an SSH server with given host keys that accepts any
// password, and returns its address.
func startTestSSHServer(t *testing.T, signers ...ssh.Signer) string {
	t.Helper()
	config := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
			return nil, nil
		},
	}
	for _, signer := range signers {
		config.AddHostKey(signer)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_, chans, reqs, err := ssh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					_ = ch.Reject(ssh.Prohibited, "")
				}
			}()
		}
	}()
	return listener.Addr().String()
}

func TestHostKeyAlgorithms(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	rsaSigner, err := ssh.NewSignerFromKey(rsaKey)
	require.NoError(t, err)
	_, ed25519Key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	ed25519Signer, err := ssh.NewSignerFromKey(ed25519Key)
	require.NoError(t, err)
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	ecdsaSigner, err := ssh.NewSignerFromKey(ecdsaKey)
	require.NoError(t, err)

	tests := []struct {
		name    string
		signers []ssh.Signer
		want    ssh.PublicKey
	}{
		{
			name:    "prefers RSA",
			signers: []ssh.Signer{ed25519Signer, ecdsaSigner, rsaSigner},
			want:    rsaSigner.PublicKey(),
		},
		{
			name:    "falls back to Ed25519 for servers before v1.1.0",
			signers: []ssh.Signer{ed25519Signer},
			want:    ed25519Signer.PublicKey(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			addr := startTestSSHServer(t, test.signers...)

			var got ssh.PublicKey
			client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
				User: "pgrok",
				Auth: []ssh.AuthMethod{ssh.Password("token")},
				HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
					got = key
					return nil
				},
				HostKeyAlgorithms: hostKeyAlgorithms,
			})
			require.NoError(t, err)
			_ = client.Close()
			assert.Equal(t, ssh.FingerprintSHA256(test.want), ssh.FingerprintSHA256(got))
		})
	}
}
