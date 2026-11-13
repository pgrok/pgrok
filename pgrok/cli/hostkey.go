package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"unknwon.dev/x/logx"
)

// hostKeyAlgorithms are the host key algorithms the client accepts, in order of
// preference, so the pinned key and its fingerprint are deterministic:
//   - RSA is served by pgrokd v1.1.0 and later.
//   - Ed25519 is the only host key served by pgrokd before v1.1.0. It comes after
//     RSA because some later releases serve one of two Ed25519 keys at random
//     across restarts.
//
// ECDSA is not needed, as every pgrokd release serves an RSA or Ed25519 host
// key.
var hostKeyAlgorithms = []string{ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoED25519}

// hostKeyMismatchError is returned when the server presents a host key that is
// different from the pinned one. Retrying cannot fix it.
type hostKeyMismatchError struct {
	remoteAddr string
	want       string
	got        string
	fix        string
}

func (e *hostKeyMismatchError) Error() string {
	return fmt.Sprintf(`
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
@       WARNING: SERVER HOST KEY HAS CHANGED!             @
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!
Someone could be intercepting your connection to %q (man-in-the-middle attack).
It is also possible that the server's host key has just been changed.
Expected fingerprint: %s
Received fingerprint: %s
Refusing to connect. If the change is legitimate, %s.`,
		e.remoteAddr, e.want, e.got, e.fix,
	)
}

// hostKeyFileError is returned when the known_hosts file cannot be used to
// verify the host key. Retrying cannot fix it.
type hostKeyFileError struct {
	error
}

func (e *hostKeyFileError) Unwrap() error {
	return e.error
}

// exitOnHostKeyError exits the program when err is a host key verification
// error, as retrying the connection cannot fix it.
func exitOnHostKeyError(ctx context.Context, logger *logx.Logger, err error) {
	var mismatch *hostKeyMismatchError
	if errors.As(err, &mismatch) {
		logger.FatalContext(ctx, mismatch.Error())
	}
	var fileErr *hostKeyFileError
	if errors.As(err, &fileErr) {
		logger.FatalContext(ctx, "Failed to verify server host key", "error", fileErr.Error())
	}
}

// newHostKeyCallback returns a callback that verifies the server's host key.
// When fingerprint is not empty, the key must have that SHA256 fingerprint.
// Otherwise, the key is checked against the known_hosts file next to the config
// file, and recorded there on the first connection to the address (trust on
// first use).
func newHostKeyCallback(logger *logx.Logger, configPath, fingerprint string) (ssh.HostKeyCallback, error) {
	if fingerprint != "" {
		if !strings.HasPrefix(fingerprint, "SHA256:") {
			return nil, errors.Errorf("host key fingerprint %q must start with \"SHA256:\"", fingerprint)
		}
		return func(hostname string, _ net.Addr, key ssh.PublicKey) error {
			got := ssh.FingerprintSHA256(key)
			if got != fingerprint {
				return &hostKeyMismatchError{
					remoteAddr: hostname,
					want:       fingerprint,
					got:        got,
					fix:        `update the "--host-key-fingerprint" flag or the "host_key_fingerprint" config option`,
				}
			}
			return nil
		}, nil
	}

	knownHostsPath := filepath.Join(filepath.Dir(configPath), "known_hosts")
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := verifyKnownHost(logger, knownHostsPath, hostname, remote, key)
		if err != nil {
			var mismatch *hostKeyMismatchError
			if !errors.As(err, &mismatch) {
				err = &hostKeyFileError{error: err}
			}
		}
		return err
	}, nil
}

// verifyKnownHost verifies the host key against the known_hosts file, and
// records it when the file has no entry for the hostname.
func verifyKnownHost(logger *logx.Logger, knownHostsPath, hostname string, remote net.Addr, key ssh.PublicKey) error {
	f, err := os.OpenFile(knownHostsPath, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
	if err != nil {
		return errors.Wrap(err, "open known_hosts file")
	}
	defer func() { _ = f.Close() }()

	callback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return errors.Wrap(err, "load known_hosts file")
	}

	err = callback(hostname, remote, key)
	var keyErr *knownhosts.KeyError
	if !errors.As(err, &keyErr) {
		return err
	}

	got := ssh.FingerprintSHA256(key)
	if len(keyErr.Want) > 0 {
		want := keyErr.Want[0]
		return &hostKeyMismatchError{
			remoteAddr: hostname,
			want:       ssh.FingerprintSHA256(want.Key),
			got:        got,
			fix: fmt.Sprintf(
				`remove line %d from "%s", or pin the new key with the "--host-key-fingerprint" flag`,
				want.Line, want.Filename,
			),
		}
	}

	line := knownhosts.Line([]string{hostname}, key) + "\n"

	// Do not append to the last line when the file was edited by hand and saved
	// without a trailing newline.
	fi, err := f.Stat()
	if err != nil {
		return errors.Wrap(err, "stat known_hosts file")
	}
	if fi.Size() > 0 {
		last := make([]byte, 1)
		_, err = f.ReadAt(last, fi.Size()-1)
		if err != nil {
			return errors.Wrap(err, "read known_hosts file")
		}
		if last[0] != '\n' {
			line = "\n" + line
		}
	}

	_, err = f.WriteString(line)
	if err != nil {
		return errors.Wrap(err, "record host key")
	}
	logger.Warn("Trusting server host key on first use, verify the fingerprint with your server administrator",
		"remote", hostname,
		"fingerprint", got,
		"file", knownHostsPath,
	)
	return nil
}
