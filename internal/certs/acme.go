package certs

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/cloudflare"
	"github.com/libdns/digitalocean"
	"github.com/libdns/hetzner"
	"github.com/libdns/porkbun"
	"github.com/libdns/route53"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"unknwon.dev/x/logx"
)

// dnsProviders are the DNS providers whose APIs can solve DNS-01 challenges,
// configured by the JSON fields of their libdns provider.
var dnsProviders = map[string]func() certmagic.DNSProvider{
	"cloudflare":   func() certmagic.DNSProvider { return &cloudflare.Provider{} },
	"digitalocean": func() certmagic.DNSProvider { return &digitalocean.Provider{} },
	"hetzner":      func() certmagic.DNSProvider { return &hetzner.Provider{} },
	"porkbun":      func() certmagic.DNSProvider { return &porkbun.Provider{} },
	"route53":      func() certmagic.DNSProvider { return &route53.Provider{} },
}

func startACME(ctx context.Context, logger *logx.Logger, config *Config, hosts []string) (*tls.Config, error) {
	zapLogger := zap.New(&slogCore{handler: logger.Handler()})

	solver := &certmagic.DNS01Solver{
		DNSManager: certmagic.DNSManager{Logger: zapLogger},
	}
	if config.ACMEDNSProvider == builtinProvider {
		var settings struct {
			Zone string `json:"zone"`
			Port int    `json:"port"`
		}
		if err := setSettings(&settings, config.ACMEDNSSettings); err != nil {
			return nil, err
		}
		if settings.Zone == "" {
			return nil, errors.New("[tls.acme_dns] ZONE is required for the builtin DNS provider")
		}
		if settings.Port <= 0 {
			settings.Port = 53
		}

		server := newBuiltinDNS(settings.Zone)
		if err := server.start(ctx, logger, settings.Port); err != nil {
			return nil, errors.Wrap(err, "start builtin DNS server")
		}
		solver.DNSProvider = server
		// Every challenge name is a CNAME to the same name in the delegated zone.
		// The zone and its records are served by this process, so look up the zone
		// from it and do not wait for propagation.
		solver.OverrideDomain = "_acme-challenge." + settings.Zone
		solver.Resolvers = []string{net.JoinHostPort("127.0.0.1", strconv.Itoa(settings.Port))}
		solver.PropagationTimeout = -1
	} else {
		newProvider, ok := dnsProviders[config.ACMEDNSProvider]
		if !ok {
			return nil, errors.Errorf("unsupported DNS provider %q", config.ACMEDNSProvider)
		}
		provider := newProvider()
		if err := setSettings(provider, config.ACMEDNSSettings); err != nil {
			return nil, err
		}
		solver.DNSProvider = provider
	}

	var cfg *certmagic.Config
	cache := certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) { return cfg, nil },
		Logger:           zapLogger,
	})
	cfg = certmagic.New(cache, certmagic.Config{
		Storage: &certmagic.FileStorage{Path: config.ACMEStoragePath},
		Logger:  zapLogger,
	})
	cfg.Issuers = []certmagic.Issuer{
		certmagic.NewACMEIssuer(cfg, certmagic.ACMEIssuer{
			CA:                      config.ACMECA,
			Email:                   config.ACMEEmail,
			Agreed:                  true,
			DisableHTTPChallenge:    true,
			DisableTLSALPNChallenge: true,
			DNS01Solver:             solver,
			Logger:                  zapLogger,
		}),
	}

	// Certificates are obtained and renewed in the background with retries, so a
	// DNS misconfiguration does not stop the rest of the server.
	if err := cfg.ManageAsync(ctx, hosts); err != nil {
		cache.Stop()
		return nil, errors.Wrap(err, "manage certificates")
	}
	go func() {
		<-ctx.Done()
		cache.Stop()
	}()

	tlsConfig := cfg.TLSConfig()
	tlsConfig.NextProtos = []string{"h2", "http/1.1"}
	return tlsConfig, nil
}

// setSettings sets the fields of the struct v by the settings, whose keys are
// the upper snake case of the JSON field names of the fields, e.g. "API_TOKEN"
// for `json:"api_token"`. Unknown settings are rejected.
func setSettings(v any, settings map[string]string) error {
	fields := make(map[string]reflect.Value)
	val := reflect.ValueOf(v).Elem()
	for i := 0; i < val.NumField(); i++ {
		name, _, _ := strings.Cut(val.Type().Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			fields[strings.ToUpper(name)] = val.Field(i)
		}
	}

	for key, value := range settings {
		field, ok := fields[strings.ToUpper(key)]
		if !ok {
			return errors.Errorf("[tls.acme_dns] unknown setting %q", key)
		}

		var err error
		switch {
		case field.Type() == reflect.TypeFor[time.Duration]():
			var d time.Duration
			d, err = time.ParseDuration(value)
			field.SetInt(int64(d))
		case field.Kind() == reflect.String:
			field.SetString(value)
		case field.Kind() == reflect.Bool:
			var b bool
			b, err = strconv.ParseBool(value)
			field.SetBool(b)
		case field.CanInt():
			var n int64
			n, err = strconv.ParseInt(value, 10, 64)
			field.SetInt(n)
		default:
			err = errors.Errorf("unsupported type %s", field.Type())
		}
		if err != nil {
			return errors.Wrapf(err, "[tls.acme_dns] invalid setting %q", key)
		}
	}
	return nil
}

// slogCore is a zapcore.Core that writes to the slog handler, so that logs of
// certmagic look the same as the rest.
type slogCore struct {
	handler slog.Handler
	fields  []zapcore.Field
}

func slogLevel(l zapcore.Level) slog.Level {
	switch {
	case l <= zapcore.DebugLevel:
		return slog.LevelDebug
	case l == zapcore.InfoLevel:
		return slog.LevelInfo
	case l == zapcore.WarnLevel:
		return slog.LevelWarn
	default:
		return slog.LevelError
	}
}

func (c *slogCore) Enabled(l zapcore.Level) bool {
	return c.handler.Enabled(context.Background(), slogLevel(l))
}

func (c *slogCore) With(fields []zapcore.Field) zapcore.Core {
	return &slogCore{
		handler: c.handler,
		fields:  append(c.fields[:len(c.fields):len(c.fields)], fields...),
	}
}

func (c *slogCore) Check(e zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(e.Level) {
		return ce.AddCore(e, c)
	}
	return ce
}

func (c *slogCore) Write(e zapcore.Entry, fields []zapcore.Field) error {
	encoder := zapcore.NewMapObjectEncoder()
	for _, f := range append(c.fields[:len(c.fields):len(c.fields)], fields...) {
		f.AddTo(encoder)
	}

	r := slog.NewRecord(e.Time, slogLevel(e.Level), e.Message, 0)
	if e.LoggerName != "" {
		r.AddAttrs(slog.String("logger", e.LoggerName))
	}
	for k, v := range encoder.Fields {
		r.AddAttrs(slog.Any(k, v))
	}
	return c.handler.Handle(context.Background(), r)
}

func (*slogCore) Sync() error { return nil }
