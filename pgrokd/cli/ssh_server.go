package main

import (
	"context"

	"github.com/charmbracelet/log"

	"github.com/pgrok/pgrok/internal/conf"
	"github.com/pgrok/pgrok/internal/database"
	"github.com/pgrok/pgrok/internal/reverseproxy"
	"github.com/pgrok/pgrok/internal/sshd"
)

func runSSHServer(
	ctx context.Context,
	logger *log.Logger,
	sshdPort int,
	proxy conf.Proxy,
	db *database.DB,
	proxies *reverseproxy.Cluster,
) error {
	return sshd.Start(
		ctx,
		logger.WithPrefix("sshd"),
		sshdPort,
		proxy,
		db,
		proxies,
	)
}
