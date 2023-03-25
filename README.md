![pgrok banner](https://user-images.githubusercontent.com/2946214/227126410-3e9dae19-d0c0-4a96-9040-1322e389c8db.png)

<div align="center">
  <h3>Poor man's ngrok</h3>
  <a href="https://sourcegraph.com/github.com/pgrok/pgrok"><img src="https://img.shields.io/badge/view%20on-Sourcegraph-brightgreen.svg?style=for-the-badge&logo=sourcegraph" alt="Sourcegraph"></a>
</div>

## What?

The pgrok is a multi-tenant HTTP reverse tunnel solution through remote port forwarding from the SSH protocol.

This is intended for small teams that need to expose the local development environment to the public internet, and you need to bring your own domain name and SSO provider.

It gives stable subdomain for every user, and gated by your SSO through OIDC protocol.

Think of this as a bare-bones alternative to the [ngrok's $65/user/month enterprise tier](https://ngrok.com/pricing). Trying to put this behind a production system will blow up your SLA.

For individuals and production systems, just buy ngrok, it is still my favorite.

## Why?

Stable subdomains and SSO are two things too expensive.

Why not just pick one from the [Awesome Tunneling](https://github.com/anderspitman/awesome-tunneling)? Think broader. Not everyone is a dev who knows about server operations. For people working as community managers, sales, and PMs, booting up something locally could already be a stretch and requiring them to understand how to set up and fix server problems is a waste of team's productivity.

Copy, paste, and run is the best UX for everyone.

## How?

Before you get started, make sure you have the following:

1. A domain name (e.g. `example.com`, this will be used as the example throughout this section).
1. A server (dedicated server, VPS) with a public IP address (e.g. `111.33.5.14`).
1. An SSO provider (e.g. Google, Okta, Keycloak) that allows you to create OIDC clients.
1. A PostgreSQL server ([bit.io](https://bit.io/), Cloud SQL, self-host).

> **Note**:
>
> 1. All values used in this document are just examples, substitute based on your setup.
> 1. HTTPS for the web and proxy server is not required for this to work, while recommended if possible. Examples in this document all use HTTP.

### Set up the server (`pgrokd`)

1. Add the following DNS records for your domain name:
    1. `A` record for `example.com` to `111.33.5.14`
    1. `A` record for `*.example.com` to `111.33.5.14`
1. Set up the server with the [single binary](./docs/admin/single-binary.md), [Docker](./docs/admin/docker.md#standalone-docker-container) or [Docker Compose](./docs/admin/docker.md#docker-compose).
1. Alter your network security policy (if applicable) to allow inbound requests to port 2222 from `0.0.0.0/0` (anywhere).
1. [Download and install Caddy 2](https://caddyserver.com/docs/install) on your server, and use the following Caddyfile config:
    ```caddyfile
    http://example.com {
        reverse_proxy * localhost:3320
    }

    http://*.example.com {
        reverse_proxy * localhost:3000
    }
    ```
1. Create a new OIDC client in your SSO with the **Redirect URI** to be `http://example.com/-/oidc/callback`.

### Set up the client (`pgrok`)

1. Go to http://example.com, authenticate with your SSO to obtain the token and URL (e.g. `http://unknwon.example.com`).
1. Download the latest version of the `pgrok`:
    1. For Homebrew:
        ```sh
        brew install pgrok/tap/pgrok
        ```
    1. For others, download the archive from the [Releases](https://github.com/pgrok/pgrok/releases) page.
1. Initialize a `pgrok.yml` file with the following command (assuming you want to forward requests to `http://localhost:3000`):
    ```sh
    pgrok init --remote-addr example.com:2222 --forward-addr http://localhost:3000 --token {YOUR_TOKEN}
    ```
    By default, the config file is created under the home directory (`~/.pgrok/pgrok.yml`). Use `--config` flag to specify a different path for the config file.
1. Launch the client by executing the `pgrok` or `pgrok http` command.
    1. By default, `pgrok` expects the `pgrok.yml` is available under the home directory (`~/.pgrok/pgrok.yml`). Use `--config` flag to specify a different path for the config file.
    1. Use the `--debug` flag to turn on debug logging.
    1. Upon successful startup, you should see a log looks like:
        ```sh
        YYYY-MM-DD 12:34:56 INFO 🎉 You're ready to go live! remote=example.com:2222
        ```
1. Now visit the URL.

#### Override config options

Following config options can be overridden through CLI flags:

- `--remote-addr, -r` -> `remote_addr`
- `--forward-addr, -f` -> `forward_addr`
- `--token, -t` -> `token`

As a special case, the first argument of the `pgrok http` can be used to specify forward address, e.g.

```
pgrok http 8080
```

#### Dynamic forwards

In addition to traditional request forwarding to a single address, `pgrok` can be configured to have dynamic forward rules.

For example, if your local frontend is running at `http://localhost:3000` but some gRPC endpoints need to talk to the backend directly at `http://localhost:8080`:

```yaml
dynamic_forwards: |
  /api http://localhost:8080
  /hook http://localhost:8080
```

Then all requests prefixed with the path `/api` and `/hook` will be forwarded to `http://localhost:8080` and all the rest are forwarded to the `forward_addr` (`http://localhost:3000`).

### Vanilla SSH

Because the standard SSH protocol is used for tunneling, you may well just use the vanilla SSH client.

1. Go to http://example.com, authenticate with your SSO to obtain the token and URL (e.g. `http://unknwon.example.com`).
1. Launch the client by executing the `ssh -N -R 0::3000 example.com -p 2222` command:
    1. Enter the token as your password.
    1. Use the `-v` flag to turn on debug logging.
    1. Upon successful startup, you should see a log looks like:
        ```
        Allocated port 22487 for remote forward to :3000
        ```
1. Now visit the URL.

## Explain it to me

![pgrok network diagram](https://user-images.githubusercontent.com/2946214/224469633-4d03a2cb-8561-4584-a743-c70f3fda0aef.png)

## Sponsors

<p>
  <a href="https://www.bytebase.com">
    <img src="https://www.bytebase.com/_nuxt/img/logo-full.79b60e4.svg" width=300>
  </a>
</p>

## Credits

The project wouldn't be possible without reading [function61/holepunch-server](https://github.com/function61/holepunch-server), [function61/holepunch-client](https://github.com/function61/holepunch-client), and [TCP/IP Port Forwarding](https://github.com/apache/mina-sshd/blob/master/docs/technical/tcpip-forwarding.md).

## License

This project is under the MIT License. See the [LICENSE](LICENSE) file for the full license text.
