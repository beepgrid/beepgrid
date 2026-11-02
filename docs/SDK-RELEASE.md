# SDK release process

Packages: `@beepgrid/sdk`, `@beepgrid/react`, Python `beepgrid`, and the standalone Go module `github.com/beepgrid/beepgrid/sdk/go`. Go SDK tags use `sdk/go/v<version>`; engine tags use `v<version>`.

Run `python3 scripts/sdk-package-check.py`. The Python interpreter selected with `BEEPGRID_PACKAGE_PYTHON` must have setuptools 77 or newer, build, wheel and pip. It builds the two npm tarballs, Python wheel and source distribution, then rebuilds the wheel from that source distribution, checks licenses/readmes/changelogs, and installs them into temporary consumers outside the repository. It exercises Node ESM exports, TypeScript declarations, React SSR/styles, a clean Python venv, and a separate Go module with an explicit local replace. It never publishes, modifies provider accounts, runs models or deploys. The local Go replace verifies consumer compatibility, not registry availability.

Artifacts and a SHA256/provenance manifest go to `.local/sdk-release` (or `--output`). The manifest records source commit and dirty state, artifact sizes/hashes and verification limits. A dirty source manifest cannot be presented as a release built from a clean commit. The manual `prepare SDK artifacts` GitHub workflow runs the same check and uploads development artifacts with read-only repository permissions. Release workflows run only when explicitly dispatched; publishing is separate from local artifact verification.

Use a consistent SemVer for the engine and npm packages until a separate compatibility policy is justified. Python versions must use the equivalent PEP 440 form. Keep wire changes additive within API v1; do not silently reinterpret operation keys, requests, responses, identities or worker lease generations. A breaking import/package rename needs migration instructions and a clean consumer test before publication. Pin React's SDK peer compatibility to the released client series when a release version is selected. Keep the CSS export marked as a side effect; SDK transport code is side-effect free.

Before a release, rerun real PostgreSQL default/headless race suites, the isolated real HTTP SDK journey, CLI/MCP smoke and packed consumers on the integrated commit. Reconcile shared file/membership wrappers with the main task's final engine contract. Qualification must separately record source/protocol fixtures, designated live provider round trip, native approval, and recovery/revoke. Provider acceptance is not human receipt or business execution. Publishing, pushes, deployments and live-account setup require explicit authorization; a local qualification check does not perform them.


## Publication

1. Run the artifact and engine qualification checks on the intended release commit.
2. Publish the Go SDK directory tag before the engine tag so the root module resolves the released SDK.
3. Publish the exact checked npm tarballs through the `beepgrid` account. Both packages now trust the dedicated `publish-npm.yml` workflow in the `npm` environment. Subsequent releases use OIDC and provenance.
4. Dispatch `publish-python.yml` with the reviewed engine tag. Only its `pypi` environment has OIDC publishing permission. The workflow builds and checks wheel/sdist before the separate publish job.
5. Build versioned default/headless archives with `python3 scripts/build-release.py --version <version>`, publish their checksums, and update the Homebrew tap with those exact hashes.
6. Install from each public registry with clean caches before announcing the release.

No payment integration, marketing analytics or private managed source belongs in any public artifact. Credentials remain outside source and packages. Never move published tags or replace released files; ship a new patch version.

## Upgrade to 0.2.0

This release finishes the Beepgrid namespace migration. Set `BEEPGRID_*` configuration variables, import the Go SDK directly from `github.com/beepgrid/beepgrid/sdk/go`, and use `BeepgridError`, `BeepgridEvent` and the `Beepgrid*` React exports. The compact-send helpers are `sendMinimal` in TypeScript, `send_minimal` in Python and `SendMinimal` in Go. Standard `send`/`Send` behavior is unchanged.

Webhook receivers verify `X-Beepgrid-Timestamp`, `X-Beepgrid-Signature` and `X-Beepgrid-Event-ID`. Update receiver configuration with the server release. Existing browser sessions sign in again after the cookie namespace change. Migration 38 upgrades persisted receipt fields and the metering function while preserving canonical messages, operation keys, permissions and existing migration checksums. Current packages contain no compatibility package under the retired name. Previous immutable registry versions and historical migration SQL remain historical records.

## Patch 0.2.1

This patch improves command-line setup errors, Go command documentation, package discovery and standalone Agent connection instructions. Engine API v1 and SDK APIs are unchanged. Use the current installation commands in [Packaging](PACKAGING.md).

## Patch 0.2.2

This patch includes clearer command-line setup guidance, package documentation and standalone Agent installation instructions. SDK APIs and engine runtime behavior remain compatible with the 0.2 series.

## Patch 0.2.3

This patch refreshes release metadata and installation documentation. Engine runtime behavior and public SDK APIs are unchanged. Use the current installation commands in [Packaging](PACKAGING.md).
