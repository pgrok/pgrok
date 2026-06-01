package main

import (
	"context"

	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/conf"
	"github.com/pgrok/pgrok/internal/database"
	"github.com/pgrok/pgrok/internal/reverseproxy"
	"github.com/pgrok/pgrok/internal/sshd"
)

func runSSHServer(
	ctx context.Context,
	logger *logx.Logger,
	sshdPort int,
	proxy conf.Proxy,
	db *database.DB,
	proxies *reverseproxy.Cluster,
) error {
	return sshd.Start(
		ctx,
		logger,
		sshdPort,
		proxy,
		db,
		proxies,
	)
}
