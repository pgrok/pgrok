# Setting up HTTPS

pgrokd can serve HTTPS by itself, for both the web dashboard and all tunnels, without a reverse proxy. Each tunnel is exactly one label under `[proxy] DOMAIN` (e.g. `unknwon.example.com`), so a single wildcard certificate for `*.example.com` covers every tunnel. The dashboard host needs its own certificate unless it is one of those names.

> [!NOTE]
> All values used in this document are just examples, substitute based on your setup.

First of all, there are some common steps:

1. Add the following DNS records for your domain name (with **DNS only** if using Cloudflare):
    1. `A` record for `example.com` to `111.33.5.14`
    1. `A` record for `*.example.com` to `111.33.5.14`
1. Set up the server with the [single binary](./single-binary.md), [Docker](./docker.md#standalone-docker-container) or [Docker Compose](./docker.md#docker-compose).
1. Alter your network security policy (if applicable) to allow inbound requests to ports `443`, `80` (optional, to redirect HTTP to HTTPS) and `2222` from `0.0.0.0/0` (anywhere).
1. Set `EXTERNAL_URL` to use `https`, and configure the `[tls]` section of your `pgrokd.ini` with **one** of the options below for where the certificates come from. All options and their defaults are listed in [`config.ini`](../../pgrokd/cli/internal/embed/config.ini).
    ```ini
    [server]
    EXTERNAL_URL = https://example.com

    [tls]
    ; The HTTPS server serves the web server for the host of EXTERNAL_URL, and the
    ; proxy server for all other hosts. Defaults to 443.
    ;PORT = 443
    ; Redirect HTTP requests to HTTPS.
    HTTP_REDIRECT_PORT = 80
    ```

HTTPS is served when either `CERT_FILES` or `ACME_DNS_PROVIDER` is set. The tunnel URLs then use `https` regardless of `[proxy] SCHEME`, and tunneled applications receive `X-Forwarded-Proto: https`. The plain HTTP `[web] PORT` and `[proxy] PORT` keep serving as before, e.g. for health checks, so do not expose them to the internet. Native HTTPS is not available with the legacy `pgrokd.yml` config.

> [!IMPORTANT]
> Binding to ports below 1024 requires privileges:
> - Single binary with systemd: add `AmbientCapabilities=CAP_NET_BIND_SERVICE` to the `[Service]` section, or run `sudo setcap cap_net_bind_service=+ep ./pgrokd` once.
> - Docker with published ports (e.g. `--publish 443:443`): works out of the box.
> - Docker with `--network host`: lower the host's limit, e.g. `sysctl net.ipv4.ip_unprivileged_port_start=53`.

## Option 1: Certificate files

Use certificates you already have, e.g. issued by [certbot with a DNS plugin](https://eff-certbot.readthedocs.io/en/stable/using.html#dns-plugins), a [Cloudflare Origin CA certificate](https://developers.cloudflare.com/ssl/origin-configuration/origin-ca/) (requires the **Proxied** DNS records), or your corporate CA:

```ini
[tls]
; Comma-separated lists of certificate files and their keys, in the same order.
; Each host uses the first certificate that is valid for it.
CERT_FILES = /etc/letsencrypt/live/example.com/fullchain.pem
KEY_FILES = /etc/letsencrypt/live/example.com/privkey.pem
```

pgrokd checks the files every minute and reloads them when they change, so renewals take effect without a restart. A warning is logged at startup for any host that no certificate covers.

## Option 2: Automatic certificates through your DNS provider

pgrokd obtains and renews the certificates from [Let's Encrypt](https://letsencrypt.org/) through ACME DNS-01 challenges, which are the only way to get a wildcard certificate. It creates the challenge records using the API of your DNS provider:

```ini
[tls]
HTTP_REDIRECT_PORT = 80
; The email to receive notices from the certificate authority.
ACME_EMAIL = admin@example.com
; The directory URL of the ACME certificate authority, defaults to Let's Encrypt.
; Use https://acme-staging-v02.api.letsencrypt.org/directory while trying things out.
;ACME_CA = https://acme-v02.api.letsencrypt.org/directory
; The directory to store the certificates and the ACME account in, defaults to
; "acme" next to the config file.
;ACME_STORAGE_PATH = /var/opt/pgrokd/acme
ACME_DNS_PROVIDER = cloudflare

; The settings of the DNS provider.
[tls.acme_dns]
; An API token with the Zone.DNS:Edit permission, read from the environment.
API_TOKEN = ${CLOUDFLARE_API_TOKEN}
```

The supported providers and their settings in `[tls.acme_dns]`:

| `ACME_DNS_PROVIDER` | Settings |
| --- | --- |
| `cloudflare` | `API_TOKEN`, optional `ZONE_TOKEN` ([details](https://github.com/libdns/cloudflare#authenticating)) |
| `digitalocean` | `AUTH_TOKEN` |
| `hetzner` | `AUTH_API_TOKEN` |
| `porkbun` | `API_KEY`, `API_SECRET_KEY` |
| `route53` | Optional `REGION`, `PROFILE`, `ACCESS_KEY_ID`, `SECRET_ACCESS_KEY`, `SESSION_TOKEN`, `HOSTED_ZONE_ID`; defaults to the [AWS SDK credential chain](https://docs.aws.amazon.com/sdkref/latest/guide/standardized-credentials.html) |

If your provider is not listed, use [option 3](#option-3-automatic-certificates-through-the-builtin-dns-server), which works with any DNS provider.

Certificates are obtained in the background after startup, and failures are logged and retried with backoff. HTTPS requests fail until the certificates are obtained, usually within a minute.

## Option 3: Automatic certificates through the builtin DNS server

pgrokd runs a small authoritative DNS server that only answers the ACME challenges, and you delegate the challenges to it once. This needs no API credentials, and works with any DNS provider.

1. Add the following DNS records for your domain name, in addition to the common ones:
    1. `NS` record for `acme.example.com` to `example.com` (i.e. the pgrokd server)
    1. `CNAME` record for `_acme-challenge.example.com` to `_acme-challenge.acme.example.com`
    1. If the host of `EXTERNAL_URL` is neither `example.com` nor one label under it (e.g. `pgrok.other.com`), also add a `CNAME` record for `_acme-challenge.pgrok.other.com` to `_acme-challenge.acme.example.com`.
1. Alter your network security policy (if applicable) to allow inbound requests to port `53` (both TCP and UDP) from `0.0.0.0/0` (anywhere).
1. Use the following config:
    ```ini
    [tls]
    HTTP_REDIRECT_PORT = 80
    ACME_EMAIL = admin@example.com
    ACME_DNS_PROVIDER = builtin

    [tls.acme_dns]
    ; The zone delegated to pgrokd by the NS record.
    ZONE = acme.example.com
    ; The port for the DNS server to listen on, for both TCP and UDP. Defaults to 53.
    ;PORT = 53
    ```

The DNS server only answers queries within the zone and does not recurse.

## Docker

When using option 2 or 3 with Docker, store the certificates in the mounted volume so they survive container upgrades, and make the directory writable by the container user:

```sh
mkdir -p /srv/pgrokd/acme
chown 10000:10001 /srv/pgrokd/acme
```

Then publish the ports, e.g. `--publish 443:443 --publish 80:80`, plus `--publish 53:53/udp --publish 53:53/tcp` for option 3.

## Alternative: Caddy and Cloudflare

You may also leave `[tls]` unconfigured and terminate HTTPS with a reverse proxy, e.g. [Caddy](https://caddyserver.com) with [Cloudflare](https://www.cloudflare.com/) proxying the wildcard domain.

1. Add the following DNS records for your domain name:
    1. `A` record for `example.com` to `111.33.5.14` (with **DNS only**)
        > [!IMPORTANT]
        > Do **not** proxy the main domain (example.com) because Cloudflare proxied traffic does not work with SSH connections.
    1. `A` record for `*.example.com` to `111.33.5.14` (with **Proxied**)
1. Set `SCHEME = https` in the `[proxy]` section of your `pgrokd.ini`.
1. [Download and install Caddy 2](https://caddyserver.com/docs/install) on your server, and use the following Caddyfile config:
    ```caddyfile
    example.com {
        reverse_proxy * localhost:3320
    }

    http://*.example.com {
        reverse_proxy * localhost:3000
    }
    ```

This does **not** encrypt the traffic between Cloudflare and your server (the "Flexible" SSL mode), and tunneled applications receive `X-Forwarded-Proto: http`.
