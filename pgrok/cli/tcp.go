package main

import (
	"cmp"
	"context"
	"fmt"
	"strconv"

	"github.com/urfave/cli/v3"
	"unknwon.dev/x/logx"
)

func commandTCP(homeDir string, logger *logx.Logger) *cli.Command {
	return &cli.Command{
		Name:  "tcp",
		Usage: "Start a TCP proxy to a local address",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return actionTCP(ctx, cmd, logger)
		},
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
					return cmd.Set("forward-addr", deriveTCPForwardAddress(s))
				},
			},
			&cli.StringFlag{
				Name:    "token",
				Usage:   "The authentication token",
				Aliases: []string{"t"},
			},
		),
	}
}

// deriveTCPForwardAddress tries to be smart about deriving the full TCP address
// from incomplete forward host and port information.
func deriveTCPForwardAddress(addr string) string {
	if addr == "" {
		return ""
	}

	// Check if it's just a port number
	port, err := strconv.Atoi(addr)
	if err == nil {
		return fmt.Sprintf("localhost:%d", port)
	}

	// Check if it's omitting the hostname
	port, err = strconv.Atoi(addr[1:])
	if err == nil {
		return fmt.Sprintf("localhost:%d", port)
	}
	return addr
}

func actionTCP(ctx context.Context, cmd *cli.Command, logger *logx.Logger) error {
	configPath := cmd.String("config")
	config, err := loadConfig(configPath)
	if err != nil {
		logger.FatalContext(ctx, "Failed to load config",
			"config", configPath,
			"error", err,
		)
	}
	logger.Debug("Loaded config", "file", configPath)

	forwardAddr := cmp.Or(
		deriveTCPForwardAddress(cmd.Args().First()),
		cmd.String("forward-addr"),
		config.ForwardAddr,
	)
	err = runTunnel(logger, tunnel{
		protocol:    protocolTCP,
		remoteAddr:  cmp.Or(cmd.String("remote-addr"), config.RemoteAddr),
		forwardAddr: forwardAddr,
		token:       cmp.Or(cmd.String("token"), config.Token),
	})
	fatalTunnelError(ctx, logger, err)
	return nil
}
