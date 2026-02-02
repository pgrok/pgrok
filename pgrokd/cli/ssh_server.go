package main

import (
	"github.com/charmbracelet/log"

	"github.com/pgrok/pgrok/internal/conf"
	"github.com/pgrok/pgrok/internal/database"
	"github.com/pgrok/pgrok/internal/reverseproxy"
	"github.com/pgrok/pgrok/internal/sshd"
)

func startSSHServer(logger *log.Logger, sshdPort int, proxy conf.Proxy, db *database.DB, proxies *reverseproxy.Cluster) {
	logger = logger.WithPrefix("sshd")
	err := sshd.Start(
		logger,
		sshdPort,
		proxy,
		db,
		proxies,
	)
	if err != nil {
		logger.Fatal("Failed to start server", "error", err)
	}
}
