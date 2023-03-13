package main

import (
	"github.com/adrg/xdg"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/log"
	"github.com/urfave/cli/v2"
)

var version = "0.0.0+dev"

func commonFlags(homeDir string) []cli.Flag {
	configPath := filepath.Join(homeDir, ".pgrok", "pgrok.yml")

	_, err := os.Stat(configPath)
	if err != nil {
		xdgConfigPath, err := xdg.ConfigFile("pgrok/pgrok.yml")
		if err == nil {
			configPath = xdgConfigPath
		}
	}

	return []cli.Flag{
		&cli.StringFlag{
			Name:    "config",
			Usage:   "The path to the config file",
			Value:   configPath,
			Aliases: []string{"c"},
		},
		&cli.BoolFlag{
			Name:    "debug",
			Usage:   "Whether to enable debug mode",
			Aliases: []string{"d"},
			Action: func(c *cli.Context, b bool) error {
				if b {
					log.SetLevel(log.DebugLevel)
				}
				return nil
			},
		},
	}
}

func main() {
	log.SetTimeFormat(time.DateTime)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal("Failed to home directory", "error", err.Error())
	}

	app := cli.NewApp()
	app.Name = "pgrok"
	app.Usage = "Poor man's ngrok"
	app.Version = version
	app.DefaultCommand = "http"
	app.Commands = []*cli.Command{
		commandHTTP(homeDir),
		commandInit(homeDir),
	}
	app.Flags = commonFlags(homeDir)
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
