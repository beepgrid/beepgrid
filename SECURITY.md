# Security

Do not disclose credentials or exploit details in a public issue. A private reporting address will be configured before public launch. Until then, report privately to the repository owner.

The development server binds to localhost. Never expose a development bootstrap token or an unauthenticated database publicly. Provider delivery evidence and worker-reported execution are distinct from independently verified business effects.

Use the patched Node.js release recorded in `.nvmrc` for builds and local tooling. Node.js is not included in the production server image. Go builds use the patched toolchain from `go.mod`. `make verify` checks known Go vulnerabilities and all three JavaScript lockfiles, including development dependencies. Keep these versions and the multi-architecture production base-image digest current when upstream security fixes are released. A clean scan is a dated check, not a guarantee against undisclosed vulnerabilities.

The [24 October 2026 review](docs/security/2026-10-24-review.md) records advisory applicability, tests and scan limitations.
