package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/charmbracelet/log"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
)

func main() {
	remoteAddr := flag.String("remote-addr", "127.0.0.1:2222", "the remote SSH server address")
	forwardAddr := flag.String("forward-addr", "127.0.0.1:2830", "the local forward address")
	flag.Parse()

	log.SetLevel(log.DebugLevel)

	backoff := 3 * time.Second
	for {
		err := tryConnect(*remoteAddr, *forwardAddr)
		if err != nil {
			log.Error("Failed to connect to server, will reconnect in "+backoff.String(), "error", err.Error())
		}
		time.Sleep(3 * time.Second)
	}
}

func tryConnect(remoteAddr, forwardAddr string) error {
	client, err := ssh.Dial(
		"tcp",
		remoteAddr,
		&ssh.ClientConfig{
			User: "pgrok",
			Auth: []ssh.AuthMethod{
				ssh.Password("token"),
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
	)
	if err != nil {
		return errors.Wrap(err, "dial remote server")
	}

	remoteListener, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal("Failed to open port on remote connection", "error", err)
	}
	defer func() { _ = remoteListener.Close() }()
	log.Info("Tunneling connection established", "remote", remoteAddr)

	for {
		remote, err := remoteListener.Accept()
		if err != nil {
			return errors.Wrap(err, "accept connection from server")
		}

		forward, err := net.Dial("tcp", forwardAddr)
		if err != nil {
			log.Error("Failed to dial local forward", "error", err)
			continue
		}
		log.Debug("Forwarding connection", "remote", remote.RemoteAddr())

		go func() {
			defer func() {
				_ = remote.Close()
				_ = forward.Close()
				log.Debug("Forwarding connection closed", "remote", remote.RemoteAddr())
			}()

			started := time.Now()
			var reqBuf, respBuf bytes.Buffer

			ctx, done := context.WithCancel(context.Background())
			go func() {
				w := io.MultiWriter(&headerWriter{W: &reqBuf}, forward)
				_, _ = io.Copy(w, remote)
				done()
			}()
			go func() {
				w := io.MultiWriter(&headerWriter{W: &respBuf}, remote)
				_, _ = io.Copy(w, forward)
				done()
			}()
			<-ctx.Done()

			req, err := http.ReadRequest(bufio.NewReader(&reqBuf))
			if err != nil {
				log.Error("Failed to read request",
					"remote", remote.RemoteAddr(),
					"error", err,
				)
				return
			}
			resp, err := http.ReadResponse(bufio.NewReader(&respBuf), req)
			if err != nil {
				log.Error("Failed to read response",
					"remote", remote.RemoteAddr(),
					"error", err,
				)
				return
			}
			log.Info("Forwarded request",
				"remote", remote.RemoteAddr(),
				"path", req.URL.Path,
				"status", resp.StatusCode,
				"duration", time.Since(started),
			)
		}()
	}
}

// A headerWriter writes to W until the request/response header has been
// written.
type headerWriter struct {
	W    io.Writer
	done bool
}

func (w *headerWriter) Write(p []byte) (int, error) {
	if w.done {
		return len(p), nil
	}

	r := false
	lines := 0
	for i := range p {
		if p[i] == '\r' {
			r = true
		} else if r && p[i] == '\n' {
			lines++
		} else {
			r = false
			lines = 0
		}

		if lines >= 2 {
			w.done = true
			break
		}
	}
	return w.W.Write(p)
}
