package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"unknwon.dev/x/anyx"
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

	forwardAddr := anyx.Coalesce(
		deriveTCPForwardAddress(cmd.Args().First()),
		cmd.String("forward-addr"),
		config.ForwardAddr,
	)
	logger.Info("Forward", "address", forwardAddr)

	cooldownAfter := time.Now().Add(time.Minute)
	for failed := 0; ; failed++ {
		err := tryConnect(
			logger,
			protocolTCP,
			anyx.Coalesce(cmd.String("remote-addr"), config.RemoteAddr),
			forwardAddr,
			anyx.Coalesce(cmd.String("token"), config.Token),
		)
		if err != nil {
			if time.Now().After(cooldownAfter) {
				failed = 0
			}
			backoff := time.Duration(2<<(failed/3+1)) * time.Second
			logger.Error(
				fmt.Sprintf("Failed to connect to server, will reconnect in %s", backoff.String()),
				"error", err.Error(),
			)
			if strings.Contains(err.Error(), "no supported methods remain") {
				logger.FatalContext(ctx, "Please double check your token and try again")
			}
			time.Sleep(backoff)
			cooldownAfter = time.Now().Add(time.Minute)
		}
	}
}
