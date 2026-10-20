# Local server qualification

This opt-in harness starts its own engine on ports 4014/4015 and creates/drops a generated database. It never uses an existing workspace. Run it with a PostgreSQL role allowed to create databases. Keep both ports free. The maximum is 25 independent authenticated worker processes, including replacements. These are deterministic SDK workers, not language-model inference jobs.

```sh
go build -buildvcs=false -o .local/notif ./cmd/notif
go build -buildvcs=false -o .local/notif-loadtest ./tools/loadtest
NOTIF_TEST_DATABASE_URL=postgres://notif:notif@localhost:5499/notif?sslmode=disable \
  .local/notif-loadtest --server .local/notif --agents 25 --rounds 8 \
  --payload 8192 --backlog 5000 --out .local/load-history
```

Use a dedicated local PostgreSQL container for resource comparisons. Set `NOTIF_BENCHMARK_POSTGRES_CONTAINER` to its name so cgroup readings describe that container. Its counters include PostgreSQL background work and two short cgroup-reading commands. Unisolated readings from the default playground container are diagnostic only. Preserve durable database defaults.

Useful cases:

- `--mode idle --agents 25 --idle-seconds 15`: connected fleet with browser reads.
- `--rtt-ms 80 --jitter-ms 40 --think --drop`: delayed SDK requests, varied agent processing and a deliberately lost response after a committed reply. The retry must produce one canonical message.
- `--payload 28000 --entropy --readers 3`: less-compressible, deterministic SHA256-derived payloads, with matching payload digests and dashboard-style readers.
- `--crash`: worker zero exits after claiming, before replying. A new instance replaces it after the old process exits. It waits for the real 30-second lease, then finishes the work. A stale receipt generation must be rejected.
- `--provider --rounds 2`: signed local HTTP delivery with 25 ms responses, 429/retry, definite rejection and ambiguous response loss. Checks terminal states, exact attempts, provider signatures and private policy stripping.
- `--profile`: CPU/heap development profiles. The CPU capture lasts ten seconds, so short serving phases and profiles have different time boundaries. Do not use profiled short runs as CPU qualification.

The browser reader follows the dashboard's two-second event debounce and fifteen-second presence fallback. It replays SSE cursors after reconnecting. Shutdown cancellation is excluded from transport errors. Keep failed trials and their reports.

Reports separate server CPU, allocation volume, lifetime peak RSS, final retained heap, worker lifetime CPU/peak-RSS sum, driver phase CPU, database counters and encoded response body bytes. Worker peak-RSS sums are an upper bound, not simultaneous fleet RSS. Bytes exclude requests, HTTP headers and TLS. PostgreSQL cumulative database statistics can lag the measured phase. No single ratio is a hosting-price or production-capacity result.

For baseline SDK comparisons, copy this harness into a clean checkout of the baseline engine/SDK and build there. Run identical flags with alternating order, new output folders and fresh generated databases. Compare server-only and combined SDK/server effects separately. See `docs/SERVER-PERFORMANCE.md` for the pinned campaign and results.

`--query-plans` captures claim/provider `EXPLAIN (ANALYZE, BUFFERS)` before warm-up and outside timing. `--provider-delay-ms 500` exposes the provider-bound case: batching cannot remove the provider's own response time. Every worker's observed sequence must match every submitted round exactly once, in addition to checking canonical messages and authenticated instances.
