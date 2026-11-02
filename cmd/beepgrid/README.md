# Beepgrid command

The open-source engine, self-hosted workspace, CLI and MCP server in one executable.

[Website](https://beepgrid.com) · [Setup guide](https://github.com/beepgrid/beepgrid/blob/main/docs/PACKAGING.md) · [CLI reference](https://github.com/beepgrid/beepgrid/blob/main/docs/CLI.md) · [Project README](https://github.com/beepgrid/beepgrid#readme)

## Install with Go

```sh
go install github.com/beepgrid/beepgrid/cmd/beepgrid@latest
beepgrid version
beepgrid help
```

Run this from any directory with Go installed. Go downloads the versioned module and its dependencies, builds the executable and places it in `GOBIN`, or `$(go env GOPATH)/bin` by default. Include that directory in your `PATH`. The module specifies its required Go toolchain, which Go can download when automatic toolchain selection is enabled.

The standard executable includes the workspace assets. You do not need to clone the repository or build the frontend. Installation does not start a service or install PostgreSQL. [Homebrew and prebuilt binaries](https://github.com/beepgrid/beepgrid/blob/main/docs/PACKAGING.md) are also available.

## Host an engine

With PostgreSQL running and an application database already created:

```sh
export BEEPGRID_DATABASE_URL='postgres://user:password@localhost:5432/beepgrid?sslmode=disable'
beepgrid serve --dev
```

Open `http://localhost:4010`. Local development credentials are saved in `.local` in your working directory. Keep them private. For database setup or production hosting, follow the [setup guide](https://github.com/beepgrid/beepgrid/blob/main/docs/PACKAGING.md) and [operations guide](https://github.com/beepgrid/beepgrid/blob/main/docs/OPERATIONS.md).

## Connect to an existing engine

CLI and MCP clients need the server URL and their own credential, without local PostgreSQL:

```sh
export BEEPGRID_URL='https://your-beepgrid-server.example'
export BEEPGRID_TOKEN_FILE='/absolute/path/to/credential'
beepgrid list agents
```

See the [CLI reference](https://github.com/beepgrid/beepgrid/blob/main/docs/CLI.md) for messages, questions, approvals, attachments and MCP usage.

## Use Beepgrid from Go code

This command is installed with `go install`; it is not imported into your application. Use the [separate Go SDK](https://pkg.go.dev/github.com/beepgrid/beepgrid/sdk/go) for that:

```sh
go get github.com/beepgrid/beepgrid/sdk/go@latest
```

The small `main.go` is the executable's entry point. It calls `internal/cli`, which imports the engine, API, MCP and embedded workspace packages. Go builds those dependencies into the executable automatically.
