package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"
	"unknwon.dev/x/logx"
)

func commandInit(homeDir string, logger *logx.Logger) *cli.Command {
	return &cli.Command{
		Name:  "init",
		Usage: "Initialize a config file",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return actionInit(ctx, cmd, logger)
		},
		Flags: append(
			commonFlags(homeDir, logger),
			&cli.StringFlag{
				Name:     "remote-addr",
				Usage:    "The address of the remote SSH server",
				Required: true,
				Aliases:  []string{"r"},
			},
			&cli.StringFlag{
				Name:     "forward-addr",
				Usage:    "The address to forward requests to",
				Required: true,
				Aliases:  []string{"f"},
				Action: func(_ context.Context, cmd *cli.Command, s string) error {
					return cmd.Set("forward-addr", deriveHTTPForwardAddress(s))
				},
			},
			&cli.StringFlag{
				Name:     "token",
				Usage:    "The authentication token",
				Required: true,
				Aliases:  []string{"t"},
			},
		),
	}
}

// deriveHTTPForwardAddress tries to be smart about deriving the full HTTP
// address from incomplete forward host and port information.
func deriveHTTPForwardAddress(addr string) string {
	if addr == "" {
		return ""
	}

	// Check if it's just a port number
	port, err := strconv.Atoi(addr)
	if err == nil {
		return fmt.Sprintf("http://localhost:%d", port)
	}

	// Check if it's omitting the hostname
	port, err = strconv.Atoi(addr[1:])
	if err == nil {
		return fmt.Sprintf("http://localhost:%d", port)
	}

	// Check if it's omitting the scheme
	if !strings.Contains(addr, "://") {
		return "http://" + addr
	}
	return addr
}

func actionInit(ctx context.Context, cmd *cli.Command, logger *logx.Logger) error {
	const configTemplate = `# The address of the remote SSH server.
remote_addr: "%s"
# The address to forward requests to.
forward_addr: "%s"

# The authentication token.
token: "%s"

# Dynamic forward rules and use "forward_addr" as catch-all.
#dynamic_forwards: |
#  /api http://localhost:8080`
	config := fmt.Sprintf(
		configTemplate,
		cmd.String("remote-addr"),
		cmd.String("forward-addr"),
		cmd.String("token"),
	)
	configPath := cmd.String("config")
	configDir := filepath.Dir(configPath)
	err := os.MkdirAll(configDir, os.ModePerm)
	if err != nil {
		logger.FatalContext(ctx, "Failed to create config directory", "path", configDir, "error", err)
	}
	err = os.WriteFile(configPath, []byte(config), 0644)
	if err != nil {
		logger.FatalContext(ctx, "Failed to save config file", "path", configPath, "error", err)
	}
	logger.Info("Config file saved", "path", configPath)
	return nil
}
