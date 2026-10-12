# Durable webhook processing

The receiver validates GitHub signatures before the scheduler stores handled
webhooks. It acknowledges only a committed inbox row. Unsupported events remain
ignored by the dispatcher and are not stored. The receiver accepts POST only,
requires a delivery ID (up to 255 bytes) and caps payloads at 10 MiB.

The inbox uses PostgreSQL `FOR UPDATE SKIP LOCKED` to claim work across workers
and replicas. A 60-second lease covers a handler's 30-second deadline. Completion
updates include the attempt number so an expired worker cannot overwrite a newer
lease. A crash before completion leaves work available after lease expiry; pending
rows are available immediately on restart. Business changes and inbox completion
are separate transactions, so repeated processing and broadcasts are possible.
Existing handlers must remain idempotent; lifecycle-event ordering limitations
still apply.

Successful payloads are cleared; delivery IDs, hashes and status are retained.
Failed payloads and error text (up to 4096 characters) remain in PostgreSQL.
Handler errors, panics and handler timeouts retry after 5, 10, 20, 40, 80, 160
and 300 seconds. The eighth failure moves a delivery to terminal `failed` state.
The next eligible time is stored in PostgreSQL, so restarts and duplicate
redeliveries of pending work cannot bypass the backoff. Shutdown cancellation
returns work to pending without counting a failure. Lease recovery also does not
consume the failure budget; `attempts` counts all claims.

To retry a terminal failure after fixing its cause, redeliver the event from
GitHub's webhook delivery interface. A verified matching delivery ID, event type
and payload resets its failure budget and schedules it immediately. A conflicting
payload still returns 409 and leaves the failed record unchanged. Pending,
processing and successful duplicates keep their existing state. Cumulative
`failure_total` and `attempts` survive operator redelivery; `failure_count` tracks
the current retry cycle. Existing failed rows from the earlier inbox implementation
can also be redelivered; the additive schema upgrade does not requeue them.

Processing metrics follow in a separate PR. There is no automatic retention cleanup yet. Include this table in
database backup/restore and storage planning.

Shutdown rejects new scheduling, stops claiming pending work and waits for active
handlers. After 30 seconds it cancels them and returns their deliveries to pending.
The database closes only after workers exit. Persisted queued work survives forced
termination; active work resumes after its lease expires.

## Implementation order

1. Durable storage, deduplication, leased claims and restart/shutdown recovery.
2. Bounded retries with backoff, terminal failures and operator redelivery rules.
3. Processing metrics and operational guidance.

Commits and commit-rewriting rebases require explicit user approval.
