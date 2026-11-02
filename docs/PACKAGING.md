# Install the engine and optional workspace

Beep Grid keeps the engine, CLI, MCP server, SDKs and community workspace in one repository. The workspace is an authenticated API client. Authorization, requests, conversation history and delivery workers live in the Go engine.

## Prebuilt releases

```sh
brew install beepgrid/tap/beepgrid
beepgrid version
```

The Homebrew tap installs a versioned binary on macOS and Linux. It does not start services or install a database. PostgreSQL is required to host the engine; use your existing PostgreSQL installation or a container. CLI and MCP clients need only a reachable Beepgrid server and their own credential.

Run `beepgrid` to see commands and setup guidance. It does not start the engine or create local state. If `beepgrid serve` cannot connect to PostgreSQL, it exits with a short setup message in the terminal. The workspace is available only after startup succeeds.

For Windows and manual installs, download your platform's archive and `SHA256SUMS` from [GitHub Releases](https://github.com/beepgrid/beepgrid/releases). Check its SHA256, extract it, and move `beepgrid` (`beepgrid.exe` on Windows) to a directory on `PATH`. Standard archives include the community workspace. Archives ending in `-headless` omit browser assets.

For a local server with PostgreSQL already running:

```sh
export BEEPGRID_DATABASE_URL='postgres://user:password@localhost:5432/beepgrid?sslmode=disable'
beepgrid serve --dev
```

Open `http://localhost:4010`. The local owner credential is saved in `.local/owner-token` in your working directory. Keep it private. `--dev` is loopback-only. Production requires explicit credentials, encryption configuration and migrations, as described in [operations](OPERATIONS.md).

The engine, CLI and local MCP share the `beepgrid` command. A source install is also available with `go install github.com/beepgrid/beepgrid/cmd/beepgrid@v0.2.2`.

## Troubleshooting

Startup errors explain the problem, suggest a next step and link to the relevant guide. No database or service is installed automatically. Use `beepgrid --json serve` when a script needs a structured startup error.

### Database connection

Use an existing PostgreSQL database by setting `BEEPGRID_DATABASE_URL` to its connection URL. Check the hostname, port, database name and credentials. PostgreSQL must be running before Beepgrid starts. The application database must already exist; local `--dev` mode creates and upgrades Beepgrid's schema inside it.

For a new local Homebrew PostgreSQL installation:

```sh
brew install postgresql@17
brew services start postgresql@17
"$(brew --prefix postgresql@17)/bin/createdb" beepgrid
export BEEPGRID_DATABASE_URL="postgres://$(whoami)@localhost:5432/beepgrid?sslmode=disable"
beepgrid serve --dev
```

If PostgreSQL is already configured, use its existing connection settings instead. Other platforms can use the [PostgreSQL installation guide](https://www.postgresql.org/download/) or their existing database provider. Stopping Beepgrid does not stop a PostgreSQL service started through Homebrew.

For production, initialize or upgrade the schema with `beepgrid migrate` using a deployment database credential. The serving credential can have restricted runtime permissions. A newer database schema needs a compatible newer Beepgrid executable; do not downgrade the database. See [operations](OPERATIONS.md#existing-postgresql).

### Server configuration

For local use, start with `beepgrid serve --dev`. It listens only on loopback and saves local credentials in the current directory. Hosting without `--dev` requires `BEEPGRID_BOOTSTRAP_TOKEN` and `BEEPGRID_SECRET_KEY`, as described in [operations](OPERATIONS.md#existing-postgresql). The encryption key must contain 64 hexadecimal characters. Keep the original key and credential files when reopening an existing database.

`BEEPGRID_PUBLIC_URL` is an HTTP(S) origin such as `http://localhost:4010`, without a path, embedded credentials, query or fragment. Optional AI, Coordinator, profiling and OIDC configuration is not needed for a basic local server; configure these only when using those features.

### Listen address

If another process already uses the listen address, stop that process or choose another port. Keep the public URL consistent with the address users access:

```sh
BEEPGRID_ADDR=127.0.0.1:4020 BEEPGRID_PUBLIC_URL=http://localhost:4020 beepgrid serve --dev
```

Local development uses a loopback address. Public hosting needs production configuration. The optional `BEEPGRID_PROFILE_ADDR` also needs an available literal loopback address.

### Local files

Run local development from a directory where your account can read and write `.local`. This directory holds the owner token, encryption key and default attachment storage. Keep its contents private and preserve the original credentials for an existing workspace. Beepgrid does not overwrite an existing credential just to get past a setup error.

## Build targets

| Target | Command | Output | Frontend required? |
| --- | --- | --- | --- |
| Community workspace | `make build` or `make build-workspace` | `.local/beepgrid` | Uses committed assets; Node only to rebuild them |
| Headless engine | `make build-headless` | `.local/beepgrid-headless` | No frontend source, assets or Node build |
| Community image | `make image-workspace` | `beepgrid:workspace` | Docker builds workspace assets |
| Headless image | `make image-headless` | `beepgrid:headless` | Go-only build branch |

Without Make, run `go build -buildvcs=false -trimpath -o beepgrid ./cmd/beepgrid` for the workspace or add `-tags headless` for the asset-free binary. `docker build .` still builds the workspace image. The explicit image targets are `workspace` and `headless`.

Both binaries include the same CLI, HTTP API, MCP tools, health checks, migrations, policy engine and delivery workers. They use the same database schema. Switching binaries neither migrates identities nor copies history. PostgreSQL remains required. Container build targets remain available from source.

## Run

The normal `beepgrid serve --dev` includes the workspace. `beepgrid serve --dev --no-ui` disables browser pages at runtime while retaining APIs and workers. The runtime switch does not shrink an already built binary; the `headless` build tag omits the assets. A headless binary returns 404 for workspace paths even without `--no-ui`.

Production still requires explicit credentials, encryption configuration and `beepgrid migrate`; `--dev` is loopback-only. See [operations](OPERATIONS.md). Disabling browser pages does not disable authentication or change permissions. OIDC and other authentication API routes are retained.

Go hosts composing the public `runtime` can set `Config.DisableUI`. Default behavior remains the integrated workspace. The headless build tag also applies to hosts importing the runtime.

## Keep the workspace and engine together

Develop the workspace in `workspace/`, run its tests and rebuild with `make workspace`. The embedded production build serves the workspace and `/api/v1` from the same origin with HttpOnly session cookies. The workspace's development proxy targets the same API. No separate UI server is needed in the installed distribution.

Alternative clients can use the [versioned HTTP contract](../api/openapi.json), [integration guide](INTEGRATIONS.md) and language SDKs. They receive the same current authorization checks as our workspace. Keep credentials on the server or in protected client files. A separate-origin browser deployment needs an explicit authentication/proxy design; this change adds no permissive CORS policy or public plugin framework.

## Verification

`make test` and `make test-headless` exercise both compositions against real PostgreSQL. `python3 scripts/headless-build.py` builds from a temporary source tree without frontend inputs. CI also builds and smoke-tests both non-root images, including authenticated API access and the presence or absence of workspace routes. The runtime regression sends a request through the headless API, answers through the UI-enabled API and reads the same canonical result.
