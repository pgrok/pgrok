# Setting HTTPS with Caddy and Cloudflare

This document walks through setting up HTTPS for pgrokd using [Caddy](https://caddyserver.com) as the reverse proxy on your server, and [Cloudflare](https://www.cloudflare.com/) as the DNS provider.

There are two approaches to set up HTTPS with different level of security with their corresponding trade-offs. Both approaches offer HTTPS to the end user's browser, and the main difference is whether the traffic between Cloudflare and your server is encrypted or not.

> [!NOTE]
> All values used in this document are just examples, substitute based on your setup.

First of all, there are some common steps:

1. Add the following DNS records for your domain name:
    1. `A` record for `example.com` to `111.33.5.14` (with **DNS only**)
        > [!IMPORTANT]
        > Do **not** proxy the main domain (example.com) becasue Cloudflare proxyed traffic does not work with SSH connections.
    1. `A` record for `*.example.com` to `111.33.5.14` (with **Proxied**)
1. Set up the server with the [single binary](./single-binary.md), [Docker](./docker.md#standalone-docker-container) or [Docker Compose](./docker.md#docker-compose).
1. Alter your network security policy (if applicable) to allow inbound requests to port `2222` from `0.0.0.0/0` (anywhere).

## Approach 1: Caddy + Cloudflare Flexible

This approach does **not** encrypt the traffic between Cloudflare and your server. It is easier to set up but less secure.

[Download and install Caddy 2](https://caddyserver.com/docs/install) on your server, and use the following Caddyfile config:

```caddyfile
example.com {
    reverse_proxy * localhost:3320
}

http://*.example.com {
    reverse_proxy * localhost:3000
}
```

That's it!

## Approach 2: Caddy + Cloudflare Full (strict)

This approach encrypts the traffic between Cloudflare and your server, making it more secure.

To generate a wildcard certificate you will need to use the DNS-01 challenge type which requires using a [supported DNS provider](https://community.letsencrypt.org/t/dns-providers-who-easily-integrate-with-lets-encrypt-dns-validation/86438) (e.g. Cloudflare).

Here comes the extra cumbersome, the default build of Caddy does not contain any DNS modules (including when you install from the system package managers). These need to be added to your [download from caddyserver.com or built manually using the `xcaddy` tool](https://caddy.community/t/how-to-use-dns-provider-modules-in-caddy-2/8148), here is the [link to include the Cloudflare DNS module](https://caddyserver.com/download?package=github.com%2Fcaddy-dns%2Fcloudflare) on the download page.

Get your Cloudflare API key from your Cloudflare account page and set it as the environment variable `CLOUDFLARE_API_TOKEN`.

Then, use the following Caddyfile config:

```caddyfile
example.com {
    reverse_proxy * localhost:3320
}

*.example.com {
    tls {
        dns cloudflare {env.CLOUDFLARE_API_TOKEN}
    }
    reverse_proxy * localhost:3000
}
```

_Credit: [Wildcard Certificates in Caddy Server](https://reinhardt.dev/posts/caddy-server-wildcards/)_

## IP allowlists behind reverse proxies

Clients can restrict their HTTP tunnels to visitors from specific IP addresses (`pgrok http --allow-ip`). Behind Caddy, pgrokd sees every request as coming from Caddy, so it needs to know which peers are trusted to report the visitor IP in the `X-Forwarded-For` header. Set `proxy.trusted_proxies` in `pgrokd.yml` to the addresses Caddy connects from:

```yaml
proxy:
  trusted_proxies: ["127.0.0.1/32", "::1/128"]
```

- When pgrokd runs in Docker, Caddy connects from the Docker network gateway (e.g. `172.17.0.1/32`) instead of the loopback address.
- pgrokd uses the rightmost address in `X-Forwarded-For` that is not a trusted proxy, so visitors cannot forge their IP address. Without `trusted_proxies`, the header is ignored and the direct peer address is used, which means allowlisted tunnels deny everyone when pgrokd is behind a reverse proxy.
- With Cloudflare proxied DNS in front of Caddy (both approaches above), the direct peer of Caddy is a Cloudflare edge server. Add the [Cloudflare IP ranges](https://www.cloudflare.com/ips/) to both the [`trusted_proxies` of Caddy](https://caddyserver.com/docs/caddyfile/options#trusted-proxies) and `proxy.trusted_proxies` of pgrokd, otherwise the visitor IP would be a Cloudflare address.

## Other resources

- [Configuring Caddy with Wildcard Subdomains](https://sirfitz.medium.com/configuring-caddy-with-wildcard-subdomains-eadcd7ad9cff) through DigitalOcean DNS
