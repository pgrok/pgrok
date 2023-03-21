# Deploy with Docker images

Visit [GitHub Container registry](https://github.com/pgrok/pgrok/pkgs/container/pgrokd) to see all available images and tags.

Image versions:
  - Every released version has its own tag, e.g. `ghcr.io/pgrok/pgrokd:1.1.4`.
  - The `latest` tag is an alias for the latest released version.
  - The `insiders` tag is the image version built from the latest `main` branch.

> **Note**:
>
> 1. All values used in this document are just examples, substitute based on your setup.
> 1. HTTPS for the web and proxy server is not required for this to work, while recommended if possible. Examples in this document all use HTTP.

## Standalone Docker container

1. Pick a directory on the file system to store the configuration file (`pgrokd.yml`), e.g. `/srv/pgrokd`:
    ```sh
    mkdir -p /srv/pgrokd
    ```
1. Create the configuration file (`/srv/pgrokd/pgrokd.yml`):
    ```yaml
    external_url: "http://example.com"
    web:
      port: 3320
    proxy:
      port: 3000
      scheme: "http"
      domain: "example.com"
    sshd:
      port: 2222

    database:
      # Use "host.docker.internal" if your PostgreSQL is running locally on the same host.
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
    # # The required domain name, "field_mapping.email" is required to set for this to work.
    #  required_domain: "example.com"
    ```
1. Start a Docker container:
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

### Upgrade

```sh
docker stop pgrokd
docker rm pgrokd
docker run ...
```

## Docker Compose

1. Create the directory to store the configuration file (`pgrokd.yml`):
    ```sh
    mkdir -p ./pgrokd
    ```
1. Create the configuration file (`./pgrokd/pgrokd.yml`):
    ```yaml
    external_url: "http://example.com"
    web:
      port: 3320
    proxy:
      port: 3000
      scheme: "http"
      domain: "example.com"
    sshd:
      port: 2222

    database:
      # This connects to the "postgres" service.
      host: "postgres"
      port: 5432
      # Make sure to match the value of the environment variable "POSTGRES_USER"
      user: "REDACTED"
      # Make sure to match the value of the environment variable "POSTGRES_PASSWORD"
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
    # # The required domain name, "field_mapping.email" is required to set for this to work.
    #  required_domain: "example.com"
    ```
1. Start the cluster:
    ```sh
    POSTGRES_USER=REDACTED \
    POSTGRES_PASSWORD=REDACTED \
    docker-compose up --detach
    ```
