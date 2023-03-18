# Deploy with Docker images

Visit [GitHub Container registry](https://github.com/pgrok/pgrok/pkgs/container/pgrokd) to see all available images and tags.

## Initial setup

> **Note**:
>
> 1. All values used here are just examples, substitute based on your setup.
> 1. HTTPS for the web and proxy server is not required for this to work, while recommended if possible. Examples in the section all use HTTP.

1. Pick a directory on the file system to store the configuration file (`pgrokd.yml`), e.g. `/srv/pgrokd`.
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
      --restart always \
      --volume /srv/pgrokd:/var/opt/pgrokd \
      --publish 3320:3320 \
      --publish 3000:3000 \
      --publish 2222:2222 \
      --name pgrokd \
      ghcr.io/pgrok/pgrokd:latest
    ```

## Upgrade

```sh
docker stop pgrokd
docker rm pgrokd
docker run ...
```

## Image versions

- Every released version has its own tag, e.g. `ghcr.io/pgrok/pgrokd:1.1.4`.
- The `latest` tag is an alias for the latest released version.
- The `insiders` tag is the image version built from the latest `main` branch.
