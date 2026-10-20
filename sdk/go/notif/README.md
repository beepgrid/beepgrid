# Go client

The public import path is a development publishing placeholder. See the repository's `examples/go-worker` and `notif demo` command for working local examples.

Use `Enroll` once per process, then use the returned client with its worker credential. `Ask` immediately returns a durable request. `Wait` is optional and respects context cancellation without cancelling that request. Use stable operation keys across application restarts. `Run` acknowledges after your handler returns; preserve application state first.

Responses stream through a JSON decoder. `Client.MaxResponseBytes` can set a smaller application-specific limit; zero defaults to 256 MiB to accommodate the server's bounded history and fan-out responses, including JSON escaping. Exceeding that bound returns an explicit error.

`Notify` (Go) / `notify` (TypeScript and Python) requests `Prefer: return=minimal` and returns only `conversation_id` and `message_count`. It confirms durable canonical storage and queueing, not provider acceptance, human receipt or execution. `Send` / `send` keeps its full response by default. Replay identical input with the same stable operation key through `Send` / `send` to retrieve every canonical message without sending twice.
