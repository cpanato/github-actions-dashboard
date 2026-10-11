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
Failed payloads and error text remain in PostgreSQL. This first PR retains handler
failures in `failed` state; bounded retries and processing metrics follow in
separate PRs. There is no automatic retention cleanup yet. Include this table in
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
