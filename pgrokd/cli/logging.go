package main

import (
	"log/slog"
	"os"
	"time"

	"charm.land/lipgloss/v2"
	charmlog "charm.land/log/v2"
	"github.com/flamego/flamego"
	"unknwon.dev/x/logx"
)

// setupLogging builds the application logger backed by charm.land/log/v2 as an
// slog.Handler. In the production environment the output is JSON formatted,
// otherwise it is the human-friendly text formatter.
func setupLogging(level slog.Level) *logx.Logger {
	opts := charmlog.Options{
		TimeFormat:      time.DateTime,
		Level:           charmlog.Level(level),
		ReportTimestamp: true,
	}
	if flamego.Env() == flamego.EnvTypeProd {
		opts.Formatter = charmlog.JSONFormatter
	}
	handler := charmlog.NewWithOptions(os.Stderr, opts)

	// Override warn level color to amber so it is less visually "green"-ish.
	styles := charmlog.DefaultStyles()
	styles.Levels[charmlog.WarnLevel] = lipgloss.NewStyle().
		SetString("WARN").
		Bold(true).
		Foreground(lipgloss.Color("226"))
	handler.SetStyles(styles)

	return logx.New(handler)
}
