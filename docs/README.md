# Contributor documentation

For installation, primitives, SDK methods and integration walkthroughs, use the [Beepgrid documentation](https://beepgrid.com/docs/) or its [Markdown index](https://beepgrid.com/llms.txt).

## Understand the engine

- [Architecture and product boundaries](ARCHITECTURE.md)
- [Capabilities and verification](COVERAGE.md)
- [Implementation and maintenance notes](IMPLEMENTATION.md)
- [HTTP API contract](../api/openapi.json)
- [SDK coverage](SDK-COVERAGE.md) and [method inventories](developer/)
- [Contribution workflow](../CONTRIBUTING.md) and [security reports](../SECURITY.md)

## Run and integrate

- [Packaging and headless builds](PACKAGING.md)
- [Operations, migrations and recovery](OPERATIONS.md)
- [CLI](CLI.md), [MCP](LOCAL-MCP.md) and [runtime examples](../examples/README.md)
- [Live input, Steer/Queue and headless runtime setup](LIVE-INPUT.md)
- [Shared conversations](SHARED-CHATS.md), [request replacement](REQUEST-REPLACEMENT.md) and [Coordinator](ASSISTANCE.md)
- [Integration contracts](INTEGRATIONS.md), [connection recipes](CONNECT-AGENT-RECIPES.md), [Discord](DISCORD.md) and [Telegram](TELEGRAM.md)
- [Channel support and qualification](OSS-CHANNEL-READINESS.md)
- [Local populated review workspace](REVIEW-WORKSPACE.md)
- [SDK packaging and release process](SDK-RELEASE.md)

## Reproduce evidence

- [Server performance](SERVER-PERFORMANCE.md), [portable delivery](PORTABLE-PERFORMANCE.md) and [byte/memory measurements](BYTE-MEMORY-PERFORMANCE.md)
- [Dashboard event measurements](DASHBOARD-LIVE-PERFORMANCE.md) and [retained failure audit](FAILURE-AUDIT.md)
- [Live channel checks](LIVE-CHANNEL-QUALIFICATION.md), [channel recovery](CHANNEL-RECOVERY-QUALIFICATION.md) and [Coordinator recovery](COORDINATOR-RECOVERY-QUALIFICATION.md)

Reports preserve their measured source identities and limitations. The normal test suite is the current functional check; an old benchmark is not evidence that every later version has the same performance.
