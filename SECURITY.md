# Security

Report suspected vulnerabilities privately to [support@beepgrid.com](mailto:support@beepgrid.com) with the subject `Security report`. Include the affected version, reproduction steps and expected versus observed behavior. Avoid sending real credentials or customer data. Do not disclose exploit details in a public issue while a report is being investigated.

The development server binds to localhost. Never expose a development bootstrap token or an unauthenticated database publicly. Provider delivery evidence and worker-reported execution are distinct from independently verified business effects.

Use the patched Node.js release recorded in `.nvmrc` for builds and local tooling. Node.js is not included in the production server image. Go builds use the patched toolchain from `go.mod`. `make verify` checks known Go vulnerabilities and all three JavaScript lockfiles, including development dependencies. Keep these versions and the multi-architecture production base-image digest current when upstream security fixes are released. A clean scan is a dated check, not a guarantee against undisclosed vulnerabilities.

The [24 October 2026 review](docs/security/2026-10-24-review.md) records advisory applicability, tests and scan limitations.
