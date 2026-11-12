![pgrok banner](assets/banner.png)

<div align="center">
  <h3>Programmable Networking</h3>
  <a href="https://sourcegraph.com/github.com/pgrok/pgrok"><img src="https://img.shields.io/badge/view%20on-Sourcegraph-brightgreen.svg?style=for-the-badge&logo=sourcegraph" alt="Sourcegraph"></a>
</div>

## What?

pgrok is a self-hosted, multi-tenant networking platform for exposing HTTP services and raw TCP connections through SSH reverse tunnels.

Run it on your own infrastructure with your own domain and OIDC-compatible SSO provider. Users sign in through your team's SSO to obtain their tunnel credentials and stable HTTP subdomains.

Use the pgrok CLI to create tunnels, choose predictable subdomains, and route HTTP requests to different local services by path—or connect with a standard SSH client.

## Why?

Stable subdomains and SSO are two things too expensive.

Why not just pick one from the [Awesome Tunneling](https://github.com/anderspitman/awesome-tunneling)? Think broader. Not everyone is a dev who knows about server operations. For people working as community managers, sales, and PMs, booting up something locally could already be a stretch and requiring them to understand how to set up and fix server problems is a waste of team's productivity.

Copy, paste, and run is the best UX for everyone.

## How?

Before you get started, make sure you have the following:

1. A domain name (e.g., `example.com`, this will be used as the example throughout this section).
1. A server (dedicated server, VPS) with a public IP address (e.g., `111.33.5.14`).
1. An SSO provider (e.g., Google, JumpCloud, Okta, GitLab, Keycloak) that allows you to create OIDC clients.
1. A PostgreSQL server, v12 or higher (Render, Vercel, Cloud SQL, self-host).

> [!NOTE]
> 1. All values used in this document are just examples, substitute based on your setup.
> 1. All examples in this document use HTTP for brevity, you may refer to our example walkthrough of [setting HTTPS with Caddy and Cloudflare](https://github.com/pgrok/pgrok/blob/main/docs/admin/https.md).

### Set up the server (`pgrokd`)

1. Add the following DNS records for your domain name:
    1. `A` record for `example.com` to `111.33.5.14` (with **DNS only** if using Cloudflare)
    1. `A` record for `*.example.com` to `111.33.5.14` (with **DNS only** if using Cloudflare)
1. Set up the server with the [single binary](./docs/admin/single-binary.md), [Docker](./docs/admin/docker.md#standalone-docker-container) or [Docker Compose](./docs/admin/docker.md#docker-compose).
1. Alter your network security policy (if applicable) to allow inbound requests to port `2222` from `0.0.0.0/0` (anywhere).
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

1. Go to http://example.com, authenticate with your SSO to obtain the token and URL (e.g., `http://unknwon.example.com`). The "Get started" section of the dashboard has the following commands ready to copy with your token and server address filled in.
    - If your token leaks, click **Regenerate** next to it on the dashboard. The old token stops working for new connections immediately, so update every client with the new token: edit `token` in `pgrok.yml`, or pass `--token` to clients started with that flag. Re-running `pgrok init ... --token` also works, but rewrites the whole `pgrok.yml`, including any `dynamic_forwards`. Tunnels that are already connected keep running until they disconnect.
1. Download the latest version of the `pgrok`:
    - For Homebrew:
        ```sh
        brew install pgrok
        ```
    - For others, download the archive from the [Releases](https://github.com/pgrok/pgrok/releases) page.
1. Initialize a `pgrok.yml` file with the following command (assuming you want to forward requests to `http://localhost:3000`):
    ```sh
    pgrok init --remote-addr example.com:2222 --forward-addr http://localhost:3000 --token {YOUR_TOKEN}
    ```
    - By default, the config file is created under the [standard user configuration directory (`XDG_CONFIG_HOME`)](https://github.com/adrg/xdg):
        - macOS: `~/Library/Application Support/pgrok/pgrok.yml`
        - Linux: `~/.config/pgrok/pgrok.yml`
        - Windows: `%LOCALAPPDATA%\pgrok\pgrok.yml`
    - Use `--config` flag to specify a different path for the config file.
1. Launch the client by executing the `pgrok` or `pgrok http` command.
    - By default, `pgrok` expects the `pgrok.yml` is available under the standard user configuration directory, or under the home directory (`~/.pgrok/pgrok.yml`). Use `--config` flag to specify a different path for the config file.
    - Use the `--debug` flag to turn on debug logging.
    - Upon successful startup, you should see a log looks like:
        ```
        🎉 You're ready to go live at http://unknwon.example.com! remote=example.com:2222
        ```
1. Now visit the URL.

As a special case, the first argument of the `pgrok http` can be used to specify forward address, e.g.,

```
pgrok http 8080
```

#### Raw TCP tunnels

> [!IMPORTANT]
> You need to alter the server network security policy (if applicable) to allow additional inbound requests to port range 10000-15000 from `0.0.0.0/0` (anywhere).

Use the `tcp` subcommand to tunnel raw TCP traffic:

```
pgrok tcp 5432
```

Upon successful startup, you should see a log looks like:

```
🎉 You're ready to go live at tcp://example.com:10086! remote=example.com:2222
```

The assigned TCP port on the server side is semi-stable, such that the same port number is used when still available.

#### Override config options

Following config options can be overridden through CLI flags for both `http` and `tcp` subcommands:

- `--remote-addr, -r` -> `remote_addr`
- `--forward-addr, -f` -> `forward_addr`
- `--token, -t` -> `token`

#### QR code

To open the tunnel on your phone right away, `pgrok` prints a QR code of the URL once the tunnel is up, for both `http` and `tcp` subcommands. It is printed again only when a reconnect gets a different URL, and only when stdout is a terminal.

#### HTTP dynamic forwards

Typical HTTP reverse tunnel solutions only support forwarding requests to a single address, `pgrok` can be configured to have dynamic forward rules when tunneling HTTP requests.

For example, if your local frontend is running at `http://localhost:3000` but some gRPC endpoints need to talk to the backend directly at `http://localhost:8080`:

```yaml
dynamic_forwards: |
  /api http://localhost:8080
  /hook http://localhost:8080
```

Then all requests prefixed with the path `/api` and `/hook` will be forwarded to `http://localhost:8080` and all the rest are forwarded to the `forward_addr` (`http://localhost:3000`).

When prefixes overlap (e.g., `/api` and `/api/v2`), the longest matching prefix wins.

#### Custom subdomain prefix

By default, each user gets a single stable subdomain (e.g., `unknwon.example.com`), and when it is already taken `pgrok` falls back to a host prefixed with a server-generated UUID (e.g., `7fa4f0d0...-unknwon.example.com`). Use the `--subdomain, -s` flag of the `http` subcommand to choose that prefix yourself, so you get a deterministic URL and can run multiple tunnels under predictable hosts:

```
pgrok http --subdomain staging 3000
```

This goes live at `http://staging-unknwon.example.com`, forwarding to `http://localhost:3000`.

- The value may contain lowercase letters, digits, and hyphens, with no leading or trailing hyphen. It shares the leftmost DNS label with your subdomain (`<prefix>-<subdomain>`), so the combined length must stay within 63 characters. Invalid values are rejected by the server.
- The resulting URL is deterministic: if the prefixed host is already in use, the client fails with an error instead of falling back to a randomized one.
- This flag only applies to HTTP tunnels. It has no effect on `pgrok tcp`.

### Vanilla SSH

Because the standard SSH protocol is used for tunneling, you may well just use the vanilla SSH client.

1. Go to http://example.com, authenticate with your SSO to obtain the token and URL (e.g., `http://unknwon.example.com`).
1. Launch the client by executing the `ssh -N -R 0:localhost:3000 example.com -p 2222` command:
    1. Enter the token as your password.
    1. Use the `-v` flag to turn on debug logging.
    1. Upon successful startup, you should see a log looks like:
        ```
        Allocated port 22487 for remote forward to localhost:3000
        ```
1. Now visit the URL.

## Explain it to me

![pgrok network diagram](https://user-images.githubusercontent.com/2946214/229048941-cc12139d-f250-49fa-806d-19c27996ee09.png)

## Contributing

Please read through our [contributing guide](.github/contributing.md) and [set up your development environment](docs/dev/local_development.md).

## Sponsors

<p>
  <a href="https://www.bytebase.com?ref=unknwon">
    <img src="https://raw.githubusercontent.com/sqlchat/sqlchat/main/public/bytebase.webp" width=300>
  </a>
</p>

## Credits

The project wouldn't be possible without reading [function61/holepunch-server](https://github.com/function61/holepunch-server), [function61/holepunch-client](https://github.com/function61/holepunch-client), and [TCP/IP Port Forwarding](https://github.com/apache/mina-sshd/blob/master/docs/technical/tcpip-forwarding.md).

## License

This project is under the MIT License. See the [LICENSE](LICENSE) file for the full license text.
