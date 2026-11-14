# Deploy with single binary

> **Note**: Upgrading from an older version? See [`pgrokd.example.ini`](../../pgrokd.example.ini) for the current config layout.

1. Create a `pgrokd.ini` file. Only set the options to change, all options and their defaults are listed in [`config.ini`](../../pgrokd/cli/internal/embed/config.ini).
    ```ini
    [server]
    EXTERNAL_URL = http://example.com

    [proxy]
    DOMAIN = example.com

    [database]
    USER = REDACTED
    PASSWORD = REDACTED

    [identity_provider]
    TYPE = oidc
    DISPLAY_NAME = Google
    ISSUER = https://accounts.google.com
    CLIENT_ID = REDACTED
    CLIENT_SECRET = REDACTED
    ; Only allow emails of this domain to sign in.
    ;REQUIRED_DOMAIN = example.com
    ; Only allow these emails to sign in (case-insensitive). When REQUIRED_DOMAIN is also set, both must match.
    ;ALLOWED_EMAILS = alice@example.com, bob@example.com
    ```
1. Download the latest version of the `pgrokd` archive from the [Releases](https://github.com/pgrok/pgrok/releases) page.
1. Launch the `pgrokd` in background (systemd, screen, nohup).
    1. By default, `pgrokd` expects the `pgrokd.ini` is available in the working directory. Use `--config` flag to specify a different path for the config file.
