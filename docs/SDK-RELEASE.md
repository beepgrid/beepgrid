# SDK release process

Packages: `@beepgrid/sdk`, `@beepgrid/react`, Python `beepgrid`, and the standalone Go module `github.com/beepgrid/beepgrid/sdk/go`. Go SDK tags use `sdk/go/v<version>`; engine tags use `v<version>`.

Run `python3 scripts/sdk-package-check.py`. The Python interpreter selected with `NOTIF_PACKAGE_PYTHON` must have setuptools 77 or newer, build, wheel and pip. It builds the two npm tarballs, Python wheel and source distribution, then rebuilds the wheel from that source distribution, checks licenses/readmes/changelogs, and installs them into temporary consumers outside the repository. It exercises Node ESM exports, TypeScript declarations, React SSR/styles, a clean Python venv, and a separate Go module with an explicit local replace. It never publishes, modifies provider accounts, runs models or deploys. The local Go replace verifies consumer compatibility, not registry availability.

Artifacts and a SHA256/provenance manifest go to `.local/sdk-release` (or `--output`). The manifest records source commit and dirty state, artifact sizes/hashes and verification limits. A dirty source manifest cannot be presented as a release built from a clean commit. The manual `prepare SDK artifacts` GitHub workflow runs the same check and uploads development artifacts with read-only repository permissions. Remote workflow execution remains unverified until explicitly dispatched later.

Use a consistent SemVer for the engine and npm packages until a separate compatibility policy is justified. Python versions must use the equivalent PEP 440 form. Keep wire changes additive within API v1; do not silently reinterpret operation keys, requests, responses, identities or worker lease generations. A breaking import/package rename needs migration instructions and a clean consumer test before publication. Pin React's SDK peer compatibility to the released client series when a release version is selected. Keep the CSS export marked as a side effect; SDK transport code is side-effect free.

Before a release, rerun real PostgreSQL default/headless race suites, the isolated real HTTP SDK journey, CLI/MCP smoke and packed consumers on the integrated commit. Reconcile shared file/membership wrappers with the main task's final engine contract. Qualification must separately record source/protocol fixtures, designated live provider round trip, native approval, and recovery/revoke. Provider acceptance is not human receipt or business execution. Final names/versions, registry publication, pushes, deployments and live-account setup remain explicit later actions.


## Publication

1. Run the artifact and engine qualification checks on the intended release commit.
2. Publish the Go SDK directory tag before the engine tag so the root module resolves the released SDK.
3. Publish the exact checked npm tarballs through the `beepgrid` account. Subsequent npm releases may use the dedicated `publish-npm.yml` workflow once trusted publishing is configured for both packages.
4. Dispatch `publish-python.yml` with the reviewed engine tag. Only its `pypi` environment has OIDC publishing permission. The workflow builds and checks wheel/sdist before the separate publish job.
5. Build versioned default/headless archives with `python3 scripts/build-release.py --version <version>`, publish their checksums, and update the Homebrew tap with those exact hashes.
6. Install from each public registry with clean caches before announcing the release.

No payment integration, marketing analytics or private managed source belongs in any public artifact. Credentials remain outside source and packages. Never move published tags or replace released files; ship a new patch version.
