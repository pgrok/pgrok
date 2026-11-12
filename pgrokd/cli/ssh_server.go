package main

import (
	"context"

	"github.com/zitadel/oidc/v3/pkg/op"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/conf"
	"github.com/pgrok/pgrok/internal/database"
	"github.com/pgrok/pgrok/internal/oidcprovider"
	"github.com/pgrok/pgrok/internal/reverseproxy"
	"github.com/pgrok/pgrok/internal/sshd"
)

func runSSHServer(
	ctx context.Context,
	logger *logx.Logger,
	sshdPort int,
	proxy conf.Proxy,
	db *database.DB,
	provider *op.Provider,
	proxies *reverseproxy.Cluster,
) error {
	return sshd.Start(
		ctx,
		logger,
		sshdPort,
		proxy,
		db,
		func(ctx context.Context, accessToken string) (int64, error) {
			return oidcprovider.PrincipalIDFromAccessToken(ctx, provider, db, accessToken)
		},
		proxies,
	)
}
