# Beep Grid workspace replacement

A new workspace app, written independently of the legacy `web/` interface. It uses the public Go engine contracts and shares no UI modules, styles or fixtures with that application. It is not the public website.

## Local development

Run the engine on loopback port 4010 with `NOTIF_PUBLIC_URL=http://127.0.0.1:4040`, then:

```sh
cd workspace
npm ci
npm run dev
```

Open http://127.0.0.1:4040. Person credentials become HttpOnly browser sessions. Never place an enrollment, worker or service token in the browser. The Vite proxy keeps browser API calls on the same origin.

`npm test` runs independent tests. `npm run build` validates TypeScript, produces code-split static assets in `workspace/dist/`, and enforces compressed asset budgets. `npm run embed` replaces `internal/web/dist/` with the qualified build and precompressed files, omitting source maps. `make workspace` performs all four steps. Both core and managed Docker builds embed this same app. Rebuild the Go binary after embedding.

The backend HTTP asset server serves the embedded build. See [capabilities and verification](../docs/COVERAGE.md) for the checks and their limits. Frontend changes are tested and embedded through the repository's normal verification workflow.
