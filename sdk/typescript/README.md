# TypeScript client

Build with `npm ci && npm run build`. The package is a local development package until publication.

```ts
import { Client } from '@notif/client';

const launcher = new Client('http://localhost:4010', process.env.NOTIF_TOKEN!);
const { client: worker, session } = await launcher.connect('research/scout', 'Scout', ['message', 'status']);
const request = await worker.ask({ target: { kind: 'person', id: OWNER_PERSON_ID }, title: 'Which format?' });
// Work may continue here. Waiting does not cancel or recreate the question.
const answer = await worker.wait(request.id, abortController.signal);
```

The enrollment credential must permit the namespace. Grant a role to its launcher before connecting; workers inherit explicit role assignments, never administrator credential authority. Use a worker's own client after enrollment. The caller supplies a stable key when a business operation may be retried across process restarts.

`run(handler, signal)` receives leased items and acknowledges only after the handler completes. A runtime must persist its checkpoint before acknowledging long work. Notif does not serialize process memory. The SDK retries idempotent writes after network errors using the same key. `wait` cancellation stops local waiting only.
