// Command beepgrid runs Beepgrid's open-source engine, self-hosted workspace,
// HTTP client and MCP server.
//
// # Install
//
// Install the executable with Go:
//
//	go install github.com/beepgrid/beepgrid/cmd/beepgrid@latest
//
// Go downloads the module and its dependencies, builds the program and installs
// the beepgrid executable in GOBIN, or GOPATH/bin when GOBIN is unset. No repository
// clone or separate frontend build is needed. The standard build embeds the
// workspace assets. Installation does not start services or install PostgreSQL.
//
// # Use
//
//	beepgrid version
//	beepgrid help
//
// To host an engine, configure BEEPGRID_DATABASE_URL for a running PostgreSQL
// database, then run beepgrid serve --dev for local development. To use the CLI
// or MCP with an existing server, configure BEEPGRID_URL and a credential through
// BEEPGRID_TOKEN_FILE; no local database is needed.
//
// This is an executable, not the Go SDK. Go applications use the separate
// github.com/beepgrid/beepgrid/sdk/go module.
//
// Setup: https://github.com/beepgrid/beepgrid/blob/main/docs/PACKAGING.md
//
// CLI reference: https://github.com/beepgrid/beepgrid/blob/main/docs/CLI.md
//
// Website: https://beepgrid.com
package main
