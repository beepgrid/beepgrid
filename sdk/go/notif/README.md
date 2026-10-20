# Go client

The public import path is a development publishing placeholder. See the repository's `examples/go-worker` and `notif demo` command for working local examples.

Use `Enroll` once per process, then use the returned client with its worker credential. `Ask` immediately returns a durable request. `Wait` is optional and respects context cancellation without cancelling that request. Use stable operation keys across application restarts. `Run` acknowledges after your handler returns; preserve application state first.
