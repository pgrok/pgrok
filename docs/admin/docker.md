# Deploy with Docker images

Visit [GitHub Container registry](https://github.com/pgrok/pgrok/pkgs/container/pgrokd) to see all available images and tags.

Image versions:
  - Every released version has its own tag, e.g. `ghcr.io/pgrok/pgrokd:1.1.4`.
  - The `latest` tag is an alias for the latest released version.
  - The `insiders` tag is the image version built from the latest `main` branch.

## Standalone Docker container

> **Note**: Upgrading from an older version? See [`pgrokd.example.ini`](../../pgrokd.example.ini) for the current config layout.

1. Pick a directory on the file system to store the configuration file (`pgrokd.ini`), e.g. `/srv/pgrokd`:
    ```sh
    mkdir -p /srv/pgrokd
    ```
1. Create the configuration file (`/srv/pgrokd/pgrokd.ini`). Only set the options to change, all options and their defaults are listed in [`config.ini`](../../pgrokd/cli/internal/embed/config.ini).
    ```ini
    [server]
    EXTERNAL_URL = http://example.com

    [proxy]
    DOMAIN = example.com

    [database]
    ; Use "host.docker.internal" if your PostgreSQL is running locally on the same host.
    HOST = localhost
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
1. Start a Docker container.
   1. To only allow HTTP tunneling:
       ```sh
       docker run \
         --detach \
         --restart always \
         --volume /srv/pgrokd:/var/opt/pgrokd \
         --publish 3320:3320 \
         --publish 3000:3000 \
         --publish 2222:2222 \
         --name pgrokd \
         ghcr.io/pgrok/pgrokd:latest
       ```
   1. If you want to allow tunneling raw TCP traffic (this only works on Linux, but [expose port range in Docker is just too slow](https://github.com/moby/moby/issues/14288)):
       ```sh
       docker run \
         --detach \
         --restart always \
         --volume /srv/pgrokd:/var/opt/pgrokd \
         --network host \
         --name pgrokd \
         ghcr.io/pgrok/pgrokd:latest
       ```

### Upgrade

```sh
docker stop pgrokd
docker rm pgrokd
docker run ...
```

## Docker Compose

> **Note**: The [`docker-compose.yml`](../../docker-compose.yml) file lives under the repository root.

1. Create the directory to store the configuration file (`pgrokd.ini`):
    ```sh
    mkdir -p ./pgrokd
    ```
1. Create the configuration file (`./pgrokd/pgrokd.ini`). Only set the options to change, all options and their defaults are listed in [`config.ini`](../../pgrokd/cli/internal/embed/config.ini).
    ```ini
    [server]
    EXTERNAL_URL = http://example.com

    [proxy]
    DOMAIN = example.com

    [database]
    ; This connects to the "postgres" service.
    HOST = postgres
    ; Make sure to match the value of the environment variable "POSTGRES_USER".
    USER = REDACTED
    ; Make sure to match the value of the environment variable "POSTGRES_PASSWORD".
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
1. Start the cluster:
    ```sh
    POSTGRES_USER=REDACTED \
    POSTGRES_PASSWORD=REDACTED \
    docker-compose up --detach
    ```
