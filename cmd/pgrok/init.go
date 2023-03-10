package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/log"
	"github.com/urfave/cli/v2"
)

var commandInit = &cli.Command{
	Name:        "init",
	Description: "Initialize a config file",
	Action:      actionInit,
	Flags: append(commonFlags,
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
	config := fmt.Sprintf(configTemplate, c.String("remote-addr"), c.String("forward-addr"), c.String("token"))
	configPath := c.String("config")
	err := os.WriteFile(configPath, []byte(config), 0644)
	if err != nil {
		log.Fatal("Failed to save config file", "path", configPath, "error", err.Error())
	}
	log.Info("Config file saved", "path", configPath)
	return nil
}
