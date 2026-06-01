package main

import (
	"context"
	"errors"
	"flag"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/flamego/flamego"
	"github.com/sourcegraph/conc"

	"github.com/pgrok/pgrok/internal/conf"
	"github.com/pgrok/pgrok/internal/database"
	"github.com/pgrok/pgrok/internal/reverseproxy"
	"github.com/pgrok/pgrok/pgrokd/cli/internal/web"
)

var version = "0.0.0+dev"

func main() {
	if strings.Contains(version, "+dev") {
		log.SetLevel(log.DebugLevel)
	} else {
		flamego.SetEnv(flamego.EnvTypeProd)
	}
	log.SetTimeFormat(time.DateTime)

	configPath := flag.String("config", "pgrokd.yml", "the path to the config file")
	flag.Parse()

	config, err := conf.Load(*configPath)
	if err != nil {
		log.Fatal("Failed to load config",
			"config", *configPath,
			"error", err.Error(),
		)
	}

	db, err := database.New(os.Stdout, config.Database)
	if err != nil {
		log.Fatal("Failed to connect to database", "error", err.Error())
	}

	webServer, err := web.NewServer(config, db)
	if err != nil {
		log.Fatal("Failed to set up web server", "error", err.Error())
	}

	proxies := reverseproxy.NewCluster()
	proxyServer := newProxyServer(log.Default(), config.Proxy.Port, proxies)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var routines conc.WaitGroup
	routines.Go(func() {
		if err := runSSHServer(ctx, log.Default(), config.SSHD.Port, config.Proxy, db, proxies); err != nil && !isBenignShutdown(err, ctx) {
			log.Error("SSH server exited unexpectedly", "error", err)
			stop()
		}
	})
	routines.Go(func() {
		if err := proxyServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("Proxy server exited unexpectedly", "error", err)
			stop()
		}
	})
	routines.Go(func() {
		if err := webServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("Web server exited unexpectedly", "error", err)
			stop()
		}
	})
	routines.Go(func() {
		<-ctx.Done()
		log.Warn("Shutdown requested")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := webServer.Shutdown(shutdownCtx); err != nil {
			log.Error("Failed to shut down web server gracefully", "error", err)
		}
		if err := proxyServer.Shutdown(shutdownCtx); err != nil {
			log.Error("Failed to shut down proxy server gracefully", "error", err)
		}
	})

	if r := routines.WaitAndRecover(); r != nil {
		log.Fatal("Server panicked",
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
