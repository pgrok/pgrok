# pgrok - Poor man's ngrok

## What?

The pgrok is a multi-tenant HTTP reverse tunnel solution through remote port forwarding from the SSH protocol.

This is intended for small teams that need to expose the local development environment to the public internet, and you need to bring your own domain name and SSO provider.

It gives stable subdomain for every user, and gated by your SSO through OIDC protocol.

Think this as a bare-bone alternative to the [ngrok's $65/user/month enterprise tier](https://ngrok.com/pricing). Try to put this behind a production system will blow up your SLA.

For individuals and production systems, just buy ngork, it is still my favorite.

## Why?

Stable subdomains and SSO are two things too expensive.

## How?

Before you get started, make sure you have the following:

1. A domain name (e.g. `pgork.dev`, this will be used as the example throughout this section).
1. A server (dedicated server, VPS) with a public IP address (e.g. `111.33.5.14`).
1. An SSO provider (e.g. Google, Okta, Keycloak) that allows you to create OIDC clients.
1. A PostgreSQL server ([bit.io](https://bit.io/), Cloud SQL, self-host).

> **Note**
>
> HTTPS for the web and proxy server is not required for this to work, while recommended if possible. Examples in the section all use HTTP.

### Set up the server (`pgrokd`)

1. Add the following DNS records for your domain name:
    1. `A` record for `pgrok.dev` to `111.33.5.14`
    1. `A` record for `*.pgrok.dev` to `111.33.5.14`
1. Create a `pgrokd.yml` file:
    ```yaml
    external_url: "http://pgork.dev"
    web:
      port: 3320
    proxy:
      port: 3000
      scheme: "http"
      domain: "pgork.dev"
    sshd:
      port: 2222

    database:
      host: "localhost"
      port: 5432
      user: "REDACTED"
      password: "REDACTED"
      database: "pgrokd"

    identity_provider:
      type: "oidc"
      display_name: "Google"
      issuer: "https://accounts.google.com"
        client_id: "REDACTED"
        client_secret: "REDACTED"
        field_mapping:
          identifier: "email"
          display_name: "name"
          email: "email"
      #  # The required domain name, "field_mapping.email" is required to set for this to work.
      #  required_domain: "example.com"
    ```
1. Download the latest version of the `pgrokd` archive from the [Releases](https://github.com/pgrok/pgrok/releases) page.
1. Launch the `pgrokd` in background (systemd, screen, nohop).
    1. By default, `pgrokd` expects the `pgrokd.yml` is available in the working directory. Use `--config` flag to specify a different path for the config file.
1. Alter your network security policy (if applicable) to allow inbound requests to port 2222 from `0.0.0.0/0` (anywhere).
1. [Download and install Caddy 2](https://caddyserver.com/docs/install) on your server, and use the following Caddyfile config:
    ```caddyfile
    http://pgrok.dev {
        reverse_proxy * localhost:3320
    }

    http://*.pgrok.dev {
        reverse_proxy * localhost:3000
    }
    ```
1. Create a new OIDC client in your SSO with the **Redirect URI** to be `http://pgrok.dev/-/oidc/callback`.

### Set up the client (`pgrok`)

1. Go to http://pgrok.dev, authenticate with your SSO to obtain the token and URL (e.g. `http://unknwon.pgrok.dev`).
1. Download the latest version of the `pgrok`:
  1. For Homebrew:
      ```sh
      brew install pgrok/tap/pgrok
      ```
  1. For others, download the archive from the [Releases](https://github.com/pgrok/pgrok/releases) page.
1. Initialize a `pgrok.yml` file with the following command (assuming you want to forward requests to `http://localhost:3000`):
    ```sh
    pgrok init --remote-addr pgrok.dev:2222 --forward-addr http://localhost:3000 --token {YOUR_TOKEN}
    ```
    By default, the config file is created at the working directory. Use `--config` flag to specify a different path for the config file.
1. Launch the client by executing the `pgrok` or `pgrok http` command.
    1. By default, `pgrok` expects the `pgrok.yml` is available in the working directory. Use `--config` flag to specify a different path for the config file.
    1. Use the `--debug` flag to turn on debug logging.
    1. Upon succesfully startup, you should see a log looks like:
        ```sh
        YYYY-MM-DD 12:34:56 INFO Tunneling connection established remote=pgrok.dev:2222
        ```
1. Now visit the URL.

#### Override config options

Following config options can be override through CLI flags:

- `--remote-addr` -> `remote_addr`
- `--forward-addr` -> `forward_addr`
- `--token` -> `token`

#### Dynamic forwards

In addition to traditional request forwarding to a single address, `pgrok` can be configured to have dynamic forward rules.

For example, if your local frontend is running at `http://localhost:3000` but some gRPC endpoints need talk to the backend directly at `http://localhost:8080`:

```yaml
dynamic_forwards: |
  /api http://localhost:8080
  /hook http://localhost:8080
```

Then all request prefixed with the path `/api` and `/hook` will be forwarded to `http://localhost:8080` and all the rest are forwarded to the `forward_addr` (`http://localhost:3000`).

### Vanilla SSH

Because the standard SSH protocol is used for tunneling, you may well just use the vanilla SSH client.

1. Go to http://pgrok.dev, authenticate with your SSO to obtain the token and URL (e.g. `http://unknwon.pgrok.dev`).
1. Launch the client by executing the `ssh -N -R 0::3000 pgrok.dev -p 2222` command:
    1. Enter the token as your password.
    1. Use the `-v` flag to turn on debug logging.
    1. Upon succesfully startup, you should see a log looks like:
        ```
        Allocated port 22487 for remote forward to :3000
        ```
1. Now visit the URL.

## Explain it to me

![pgrok network diagram](https://user-images.githubusercontent.com/2946214/224469633-4d03a2cb-8561-4584-a743-c70f3fda0aef.png)

## Credits

- The [logo](https://www.flaticon.com/free-icon/nat_9168228) is from [flaticon.com](https://www.flaticon.com/).
- The project wouldn't be possible without reading [function61/holepunch-server](https://github.com/function61/holepunch-server), [function61/holepunch-client](https://github.com/function61/holepunch-client), and [TCP/IP Port Forwarding](https://github.com/apache/mina-sshd/blob/master/docs/technical/tcpip-forwarding.md).

## License

This project is under the MIT License. See the [LICENSE](LICENSE) file for the full license text.
