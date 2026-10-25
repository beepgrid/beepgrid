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

`npm test` runs independent tests. `npm run build` validates TypeScript and produces static assets in `workspace/dist/`. That output does not modify the existing embedded bundle. No production cutover has happened.

See [qualification checklist](../docs/WORKSPACE-UI-CHECKLIST.md). A rendered control does not mean its complete requirement has passed qualification. Remaining feature, permission, integration and browser checks stay explicitly open there.
