package main

import (
	"context"
	"os"
	"time"

	charmlog "charm.land/log/v2"
	"unknwon.dev/x/logx"
)

// handler is the underlying charm.land/log/v2 handler backing the package
// logger. It is retained so the --debug flag can raise the log level at runtime.
var handler = charmlog.NewWithOptions(
	os.Stderr,
	charmlog.Options{
		TimeFormat:      time.DateTime,
		ReportTimestamp: true,
	},
)

var logger = logx.New(handler)

// setDebug raises the log level to debug.
func setDebug() {
	handler.SetLevel(charmlog.DebugLevel)
}

// fatal logs at error level and exits the process with status code 1.
func fatal(msg string, args ...any) {
	logger.FatalContext(context.Background(), msg, args...)
}
