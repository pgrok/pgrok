package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/urfave/cli/v2"
)

func commandInit(homeDir string) *cli.Command {
	return &cli.Command{
		Name:        "init",
		Description: "Initialize a config file",
		Action:      actionInit,
		Flags: append(
			commonFlags(homeDir),
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

func ensureForwardURL(s string) string {
	if s == "" {
		return ""
	}

	// Check if it's just a port number
	port, err := strconv.Atoi(s)
	if err == nil {
		return fmt.Sprintf("http://localhost:%d", port)
	}

	// Check if it;s omitting the hostname
	port, err = strconv.Atoi(s[1:])
	if err == nil {
		return fmt.Sprintf("http://localhost:%d", port)
	}

	// Check if it;s omitting the scheme
	if !strings.Contains("://", s) {
		return "http://" + s
	}
	return s
}

func actionInit(c *cli.Context) error {
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
		c.String("remote-addr"),
		ensureForwardURL(c.String("forward-addr")),
		c.String("token"),
	)
	configPath := c.String("config")
	configDir := filepath.Dir(configPath)
	err := os.MkdirAll(configDir, os.ModePerm)
	if err != nil {
		log.Fatal("Failed to create config directory", "path", configDir, "error", err.Error())
	}
	err = os.WriteFile(configPath, []byte(config), 0644)
	if err != nil {
		log.Fatal("Failed to save config file", "path", configPath, "error", err.Error())
	}
	log.Info("Config file saved", "path", configPath)
	return nil
}
