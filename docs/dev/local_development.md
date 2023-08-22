# Set up your development environment

The pgrok is built and runs as a single binary and meant to be cross platform. Therefore, you should be able to develop pgrok in any major platforms you prefer. However, this guide will focus on macOS only.

## Step 1: Install dependencies

The development of pgrok has the following dependencies:

- [Git](https://git-scm.com/book/en/v2/Getting-Started-Installing-Git) (v2 or higher)
- [Go](https://go.dev/doc/install) (v1.20 or higher)
- [pnpm](https://pnpm.io/installation) (v8 or higher)
- [Task](https://taskfile.dev/installation/) (v3)
- [Overmind](https://github.com/DarthSim/overmind#installation) (v2)
- [PostgreSQL](https://wiki.postgresql.org/wiki/Detailed_installation_guides) (v10 or higher)

1. Install [Homebrew](https://brew.sh/).
1. Install dependencies:

    ```bash
    brew install git go pnpm go-task overmind postgresql@15
    ```

1. Configure PostgreSQL to start automatically:

    ```bash
    brew services start postgresql@15
    ```

1.  Ensure `psql`, the PostgreSQL command line client, is on your `$PATH`.

## Step 2: Initialize your database

You need a fresh Postgres database and a database user that has full ownership of that database.

1. Create a database for the current Unix user:

    ```bash
    createdb
    ```

2. Create the user and password:

    ```bash
    createuser --superuser pgrokd
    psql -c "ALTER USER pgrokd WITH PASSWORD 'pgrokd';"
    ```

3. Create the database:

    ```bash
    createdb --owner=pgrokd --encoding=UTF8 --template=template0 pgrokd
    ```

## Step 3: Get the code

Generally, you don't need a full clone, so set `--depth` to `10`:

```bash
# HTTPS
git clone --depth 10 https://github.com/pgrok/pgrok.git

# or SSH
git clone --depth 10 git@github.com:pgrok/pgrok.git
```

> [!NOTE]
> The repository has Go modules enabled, please clone to somewhere outside of your `$GOPATH`.

## Step 4: Initialize `pgrokd.yml`

Create a `pgrokd.yml` file under the repository root and put the following configuration:

```yaml
external_url: "http://localhost:3320"
web:
  port: 3320
proxy:
  port: 3000
  scheme: "http"
  domain: "localhost:3000"
sshd:
  port: 2222

database:
  host: "localhost"
  port: 5432
  user: "pgrokd"
  password: "pgrokd"
  database: "pgrokd"

identity_provider:
  type: "oidc"
  display_name: "OIDC"
  issuer: "http://localhost:9833"
  client_id: "winnerwinner"
  client_secret: "chickendinner"
  field_mapping:
    identifier: "email"
    display_name: "name"
    email: "email"
```

## Step 5: Start the servers

The following command will start processes defined in the [`Procfile`](../../Procfile) and automatically recompile and restart these servers if related files are changed:

```bash
overmind start
```

Then, visit http://localhost:3320!

Few things to note:

- The web, proxy and SSHD servers of the pgrokd are started
- No need to access the Vite server for the pgrokd web app as all requests to it are proxyed by the pgrokd web server
- A [mock OIDC server](../../integration-tests/oidc-server/) is started for your convenience
