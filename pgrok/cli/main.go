package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"

	"github.com/charmbracelet/log"
	"github.com/urfave/cli/v3"

	"github.com/pgrok/pgrok/internal/osutil"
)

var version = "0.0.0+dev"

func commonFlags(homeDir string) []cli.Flag {
	configPath := filepath.Join(homeDir, ".pgrok", "pgrok.yml")
	if !osutil.IsExist(configPath) {
		xdgConfigPath, err := xdg.ConfigFile(filepath.Join("pgrok", "pgrok.yml"))
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
			Action: func(_ context.Context, _ *cli.Command, b bool) error {
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

	app := &cli.Command{
		Name:           "pgrok",
		Usage:          "Poor man's ngrok",
		Version:        version,
		DefaultCommand: "http",
		Commands: []*cli.Command{
			commandInit(homeDir),
			commandHTTP(homeDir),
			commandTCP(homeDir),
		},
		Flags: commonFlags(homeDir),
	}
	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
