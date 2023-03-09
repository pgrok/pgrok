package main

import (
	"context"
	"flag"
	"io"
	"net"

	"github.com/charmbracelet/log"
	"golang.org/x/crypto/ssh"
)

func main() {
	sshdAddr := flag.String("sshd-addr", "127.0.0.1:2222", "the remote SSH server address")
	forwardAddr := flag.String("forward-addr", "127.0.0.1:2830", "the local forward address")
	flag.Parse()

	log.SetLevel(log.DebugLevel)

	client, err := ssh.Dial(
		"tcp",
		*sshdAddr,
		&ssh.ClientConfig{
			User: "pgrok",
			Auth: []ssh.AuthMethod{
				ssh.Password("token"),
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
	)
	if err != nil {
		log.Fatal("Failed to dial remote server", "error", err)
	}

	// todo retry

	remoteListener, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal("Failed to open port on remote connection", "error", err)
	}
	defer func() { _ = remoteListener.Close() }()
	log.Info("Tunneling connection established", "remote", *sshdAddr)

	for {
		remote, err := remoteListener.Accept()
		if err != nil {
			log.Fatal("Failed to accept connection from server", "error", err)
		}

		forward, err := net.Dial("tcp", *forwardAddr)
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

			ctx, done := context.WithCancel(context.Background())
			go func() {
				_, _ = io.Copy(remote, forward)
				done()
			}()
			go func() {
				_, _ = io.Copy(forward, remote)
				done()
			}()
			<-ctx.Done()
		}()
	}
}
