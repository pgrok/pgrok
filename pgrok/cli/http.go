package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v3"
	"golang.org/x/crypto/ssh"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/dynamicforward"
)

func commandHTTP(homeDir string, logger *logx.Logger) *cli.Command {
	return &cli.Command{
		Name:  "http",
		Usage: "Start a HTTP proxy to local endpoints",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return actionHTTP(ctx, cmd, logger)
		},
		// Header values may contain commas, e.g., "Accept: text/html, */*".
		DisableSliceFlagSeparator: true,
		Flags: append(
			commonFlags(homeDir, logger),
			&cli.StringFlag{
				Name:    "remote-addr",
				Usage:   "The address of the remote SSH server",
				Aliases: []string{"r"},
			},
			&cli.StringFlag{
				Name:    "forward-addr",
				Usage:   "The address to forward requests to",
				Aliases: []string{"f"},
				Action: func(_ context.Context, cmd *cli.Command, s string) error {
					return cmd.Set("forward-addr", deriveHTTPForwardAddress(s))
				},
			},
			&cli.StringFlag{
				Name:    "token",
				Usage:   "The authentication token",
				Aliases: []string{"t"},
			},
			&cli.StringFlag{
				Name:    "subdomain",
				Usage:   "specify a custom subdomain prefix for a deterministic tunnel URL",
				Aliases: []string{"s"},
			},
			&cli.StringFlag{
				Name:  "host-header",
				Usage: `The Host header to forward requests with, "rewrite" to use the host of the forward address`,
			},
			&cli.StringSliceFlag{
				Name:  "request-header",
				Usage: `A header in the form of "Name: value" to set on forwarded requests, can be repeated`,
			},
			&cli.StringSliceFlag{
				Name:  "remove-request-header",
				Usage: "The name of a header to remove from forwarded requests, can be repeated",
			},
		),
	}
}

func actionHTTP(ctx context.Context, cmd *cli.Command, logger *logx.Logger) error {
	configPath := cmd.String("config")
	config, err := loadConfig(configPath)
	if err != nil {
		logger.FatalContext(ctx, "Failed to load config",
			"config", configPath,
			"error", err,
		)
	}
	logger.Debug("Loaded config", "file", configPath)

	defaultForwardAddr := cmp.Or(
		deriveHTTPForwardAddress(cmd.Args().First()),
		cmd.String("forward-addr"),
		config.ForwardAddr,
	)
	err = runTunnel(logger, tunnel{
		protocol:        protocolHTTP,
		remoteAddr:      cmp.Or(cmd.String("remote-addr"), config.RemoteAddr),
		forwardAddr:     defaultForwardAddr,
		token:           cmp.Or(cmd.String("token"), config.Token),
		subdomain:       cmd.String("subdomain"),
		dynamicForwards: config.DynamicForwards,
		headers: dynamicforward.Options{
			HostHeader:           cmp.Or(cmd.String("host-header"), config.HostHeader),
			RequestHeaders:       append(config.RequestHeaders, cmd.StringSlice("request-header")...),
			RemoveRequestHeaders: append(config.RemoveRequestHeaders, cmd.StringSlice("remove-request-header")...),
		},
	})
	fatalTunnelError(ctx, logger, err)
	return nil
}

const (
	protocolHTTP string = "http"
	protocolTCP  string = "tcp"
)

// tryConnect establishes the tunnel and blocks until it fails. The onReady
// callback is invoked with the public URL reported by the server (which may be
// empty) every time the tunnel is up.
func tryConnect(logger *logx.Logger, protocol, remoteAddr, forwardAddr, token, subdomain string, onReady func(hostURL string)) error {
	client, err := ssh.Dial(
		"tcp",
		remoteAddr,
		&ssh.ClientConfig{
			User: "pgrok",
			Auth: []ssh.AuthMethod{
				ssh.Password(token),
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
	)
	if err != nil {
		return errors.Wrap(err, "dial remote server")
	}

	// Hint the server before establishing the reverse tunnel
	payload, err := json.Marshal(map[string]string{"protocol": protocol, "subdomain": subdomain})
	if err != nil {
		return errors.Wrap(err, "marshal payload")
	}
	ok, reply, err := client.SendRequest("hint", true, payload)
	if err != nil {
		return errors.Wrap(err, "hint server")
	}
	if !ok {
		return errors.Errorf("server rejected hint: %s", reply)
	}

	remoteListener, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return errors.Wrap(err, "open port on remote connection")
	}
	defer func() { _ = remoteListener.Close() }()

	// Query the server info after the reverse tunnel is established
	var serverInfo struct {
		HostURL string `json:"host_url"`
	}
	ok, reply, err = client.SendRequest("server-info", true, payload)
	if err != nil {
		return errors.Wrap(err, "query server info")
	} else if !ok {
		return errors.Errorf("query server info rejected: %s", reply)
	}
	err = json.Unmarshal(reply, &serverInfo)
	if err != nil {
		return errors.Wrap(err, "unmarshal server info")
	}

	message := "🎉 You're ready to go live!"
	if serverInfo.HostURL != "" {
		message = fmt.Sprintf("🎉 You're ready to go live at %s!", serverInfo.HostURL)
	}
	logger.Info(message, "remote", remoteAddr)
	onReady(serverInfo.HostURL)
	for {
		remote, err := remoteListener.Accept()
		if err != nil {
			return errors.Wrap(err, "accept connection from server")
		}

		forward, err := net.Dial("tcp", forwardAddr)
		if err != nil {
			_ = remote.Close()
			logger.Error("Failed to dial local forward", "error", err)
			continue
		}
		logger.Debug("Forwarding connection", "remote", remote.RemoteAddr(), "protocol", protocol)

		go func(remote, forward net.Conn) {
			defer func() {
				_ = remote.Close()
				_ = forward.Close()
				logger.Debug("Forwarding connection closed", "remote", remote.RemoteAddr(), "protocol", protocol)
			}()

			ctx, done := context.WithCancel(context.Background())
			go func() {
				_, _ = io.Copy(forward, remote)
				done()
			}()
			go func() {
				_, _ = io.Copy(remote, forward)
				done()
			}()
			<-ctx.Done()
		}(remote, forward)
	}
}
