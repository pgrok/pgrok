package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/flamego/flamego"
	"github.com/sourcegraph/conc"

	"github.com/pgrok/pgrok/internal/conf"
	"github.com/pgrok/pgrok/internal/database"
	"github.com/pgrok/pgrok/internal/reverseproxy"
	"github.com/pgrok/pgrok/pgrokd/cli/internal/web"
)

var version = "0.0.0+dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	level := slog.LevelInfo
	if strings.Contains(version, "+dev") {
		level = slog.LevelDebug
	} else {
		flamego.SetEnv(flamego.EnvTypeProd)
	}
	logger := setupLogging(level)

	configPath := flag.String("config", "pgrokd.yml", "the path to the config file")
	flag.Parse()

	config, err := conf.Load(*configPath)
	if err != nil {
		logger.FatalContext(ctx, "Failed to load config",
			"config", *configPath,
			"error", err,
		)
	}

	db, err := database.New(logger.Scoped("database"), config.Database)
	if err != nil {
		logger.FatalContext(ctx, "Failed to connect to database", "error", err)
	}

	webServer, err := web.NewServer(logger.Scoped("web"), config, db)
	if err != nil {
		logger.FatalContext(ctx, "Failed to set up web server", "error", err)
	}

	proxies := reverseproxy.NewCluster()
	proxyServer := newProxyServer(logger.Scoped("proxy"), config.Proxy.Port, proxies)

	var routines conc.WaitGroup
	routines.Go(func() {
		if err := runSSHServer(ctx, logger.Scoped("sshd"), config.SSHD.Port, config.Proxy, db, proxies); err != nil && !isBenignShutdown(err, ctx) {
			logger.ErrorContext(ctx, "SSH server exited unexpectedly", "error", err)
			stop()
		}
	})
	routines.Go(func() {
		if err := proxyServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.ErrorContext(ctx, "Proxy server exited unexpectedly", "error", err)
			stop()
		}
	})
	routines.Go(func() {
		if err := webServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.ErrorContext(ctx, "Web server exited unexpectedly", "error", err)
			stop()
		}
	})
	routines.Go(func() {
		<-ctx.Done()
		logger.WarnContext(ctx, "Shutdown requested")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := webServer.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(shutdownCtx, "Failed to shut down web server gracefully", "error", err)
		}
		if err := proxyServer.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(shutdownCtx, "Failed to shut down proxy server gracefully", "error", err)
		}
	})

	if r := routines.WaitAndRecover(); r != nil {
		logger.FatalContext(ctx, "Server panicked",
			"panic", r.Value,
			"stack", string(r.Stack),
		)
	}
}

func isBenignShutdown(err error, ctx context.Context) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return true
	}
	return ctx.Err() != nil
}
