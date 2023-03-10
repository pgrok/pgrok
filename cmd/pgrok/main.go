package main

import (
	"os"

	"github.com/charmbracelet/log"
	"github.com/urfave/cli/v2"
)

var version = "0.0.0+dev"

var commonFlags = []cli.Flag{
	&cli.StringFlag{
		Name:    "config",
		Usage:   "The path to the config file",
		Value:   "pgrok.yml",
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

func main() {
	log.SetTimeFormat("2006-01-02 15:04:05")

	app := cli.NewApp()
	app.Name = "pgrok"
	app.Usage = "Poor man's ngrok"
	app.Version = version
	app.DefaultCommand = "http"
	app.Commands = []*cli.Command{
		commandHTTP,
	}
	app.Flags = commonFlags
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
