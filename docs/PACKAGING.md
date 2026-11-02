# Install the engine and optional workspace

Beep Grid keeps the public engine, CLI, MCP server, SDKs and community workspace in one repository. The workspace is an authenticated API client. Authorization, requests, conversation history and delivery workers live in the Go engine. No private enterprise source is needed by either public distribution.

## Prebuilt releases

```sh
brew install beepgrid/tap/beepgrid
beepgrid version
```

The Homebrew tap installs a versioned binary on macOS and Linux. It does not start services or install a database. PostgreSQL is required to host the engine; use your existing PostgreSQL installation or a container. CLI and MCP clients need only a reachable Beepgrid server and their own credential.

For Windows and manual installs, download your platform's archive and `SHA256SUMS` from [GitHub Releases](https://github.com/beepgrid/beepgrid/releases). Check its SHA256, extract it, and move `beepgrid` (`beepgrid.exe` on Windows) to a directory on `PATH`. Standard archives include the community workspace. Archives ending in `-headless` omit browser assets.

For a local server with PostgreSQL already running:

```sh
export NOTIF_DATABASE_URL='postgres://user:password@localhost:5432/beepgrid?sslmode=disable'
beepgrid serve --dev
```

Open `http://localhost:4010`. The local owner credential is saved in `.local/owner-token` in your working directory. Keep it private. `--dev` is loopback-only. Production requires explicit credentials, encryption configuration and migrations, as described in [operations](OPERATIONS.md).

The canonical command is `beepgrid`. Existing `notif` source builds remain compatible. A source install is also available with `go install github.com/beepgrid/beepgrid/cmd/beepgrid@v0.1.1`.

## Build targets

| Target | Command | Output | Frontend required? |
| --- | --- | --- | --- |
| Community workspace | `make build` or `make build-workspace` | `.local/notif` | Uses committed assets; Node only to rebuild them |
| Headless engine | `make build-headless` | `.local/notif-headless` | No frontend source, assets or Node build |
| Community image | `make image-workspace` | `beepgrid:workspace` | Docker builds workspace assets |
| Headless image | `make image-headless` | `beepgrid:headless` | Go-only build branch |

Without Make, run `go build -buildvcs=false -trimpath -o notif ./cmd/notif` for the workspace or add `-tags headless` for the asset-free binary. `docker build .` still builds the workspace image. The explicit image targets are `workspace` and `headless`.

Both binaries include the same CLI, HTTP API, MCP tools, health checks, migrations, policy engine and delivery workers. They use the same database schema. Switching binaries neither migrates identities nor copies history. PostgreSQL remains required. The binary releases contain the public engine only. Managed billing, private account provisioning and marketing analytics are excluded. Container build targets remain available from source.

## Run

The normal `notif serve --dev` includes the workspace. `notif serve --dev --no-ui` disables browser pages at runtime while retaining APIs and workers. The runtime switch does not shrink an already built binary; the `headless` build tag omits the assets. A headless binary returns 404 for workspace paths even without `--no-ui`.

Production still requires explicit credentials, encryption configuration and `notif migrate`; `--dev` is loopback-only. See [operations](OPERATIONS.md). Disabling browser pages does not disable authentication or change permissions. OIDC and other authentication API routes are retained.

Go hosts composing the public `runtime` can set `Config.DisableUI`. Default behavior remains the integrated workspace. The headless build tag also applies to hosts importing the runtime.

## Keep the workspace and engine together

Develop the workspace in `workspace/`, run its tests and rebuild with `make workspace`. The embedded production build serves the workspace and `/api/v1` from the same origin with HttpOnly session cookies. The workspace's development proxy targets the same API. No separate UI server is needed in the installed distribution.

Alternative clients can use the [versioned HTTP contract](../api/openapi.json), [integration guide](INTEGRATIONS.md) and language SDKs. They receive the same current authorization checks as our workspace. Keep credentials on the server or in protected client files. A separate-origin browser deployment needs an explicit authentication/proxy design; this change adds no permissive CORS policy or public plugin framework.

## Verification

`make test` and `make test-headless` exercise both compositions against real PostgreSQL. `python3 scripts/headless-build.py` builds from a temporary source tree without frontend inputs. CI also builds and smoke-tests both non-root images, including authenticated API access and the presence or absence of workspace routes. The runtime regression sends a request through the headless API, answers through the UI-enabled API and reads the same canonical result.
